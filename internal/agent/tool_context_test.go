package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestAfterToolContextEntersNextTurnWithoutWaking(t *testing.T) {
	for _, withContext := range []bool{false, true} {
		t.Run(map[bool]string{true: "context", false: "terminate"}[withContext], func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(call("echo", "a", map[string]any{"text": "ok"})), faux.Say("done"))
			h := pipeline.NewRegistry()
			h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
				r := &pipeline.AfterToolCallResult{Terminate: ptr(true)}
				if withContext {
					r.AddedContext = []protocol.UserMessage{user("added context").(protocol.UserMessage)}
				}
				return r, nil
			})
			a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = h })
			defer func() { require.NoError(t, a.Dispose()) }()
			require.NoError(t, a.Prompt(context.Background(), user("go")))
			if !withContext {
				require.Equal(t, 1, p.Calls())
				return
			}
			require.Equal(t, 2, p.Calls())
			require.Contains(t, userTexts(p.Requests()[1].Transcript.Messages), "added context")
			require.Equal(t, agent.Idle, a.State().Status)
		})
	}
}
func TestAfterToolContextAppendedAfterAllResults(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("echo", "a", map[string]any{"text": "a"}), call("echo", "b", map[string]any{"text": "b"})), faux.Say("done"))
	log := &sessions.MemoryLog{}
	h := pipeline.NewRegistry()
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		return &pipeline.AfterToolCallResult{Terminate: ptr(true), AddedContext: []protocol.UserMessage{user("context " + in.Call.ID).(protocol.UserMessage)}}, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = h; c.NewContext = func() sessions.Writer { return log } })
	defer func() { require.NoError(t, a.Dispose()) }()
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	var ordered []string
	for _, m := range log.Messages() {
		if r, ok := m.(protocol.ToolResultMessage); ok {
			ordered = append(ordered, "result "+r.ToolCallID)
		}
		if text := messageTextForTest(m); text == "context a" || text == "context b" {
			ordered = append(ordered, text)
		}
	}
	require.Equal(t, []string{"result a", "result b", "context a", "context b"}, ordered)
}
func TestContextAfterToolAbortWaitsForNextSend(t *testing.T) {
	for _, atPost := range []bool{false, true} {
		t.Run(map[bool]string{true: "post", false: "body"}[atPost], func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(call("t", "a", nil)), faux.Say("next cycle"))
			log := &sessions.MemoryLog{}
			var a *agent.Agent
			tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
				if !atPost {
					a.Abort()
				}
				return textResult("late body"), nil
			}}
			h := pipeline.NewRegistry()
			h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
				if atPost {
					a.Abort()
				}
				return &pipeline.AfterToolCallResult{Content: []protocol.UserBlock{protocol.Text{Text: "late post"}}, IsError: ptr(false), AddedContext: []protocol.UserMessage{user("late context").(protocol.UserMessage)}}, nil
			})
			a = newAgent(t, p, m, func(c *agent.Config) {
				c.Tools = registry(t, tool)
				c.Pipeline = h
				c.NewContext = func() sessions.Writer { return log }
			})
			defer func() { require.NoError(t, a.Dispose()) }()
			require.NoError(t, a.Prompt(context.Background(), user("go")))
			require.NoError(t, a.WaitForIdle(context.Background()))
			require.Equal(t, 1, p.Calls())
			require.NotContains(t, userTexts(log.Messages()), "late context")
			require.Equal(t, "Operation aborted", resultText(toolResults(log.Messages())["a"].Content))
			require.NoError(t, a.Prompt(context.Background(), user("next")))
			require.Equal(t, 2, p.Calls())
			require.Contains(t, userTexts(p.Requests()[1].Transcript.Messages), "late context")
		})
	}
}
func TestQueuedToolContextClearsOnAbortResetAndDispose(t *testing.T) {
	for _, action := range []string{"abort", "reset", "dispose"} {
		t.Run(action, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(call("t", "a", nil)), faux.Say("next"))
			var a *agent.Agent
			tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
				a.Abort()
				return textResult("late"), nil
			}}
			h := pipeline.NewRegistry()
			h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
				return &pipeline.AfterToolCallResult{AddedContext: []protocol.UserMessage{user("queued").(protocol.UserMessage)}}, nil
			})
			a = newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool); c.Pipeline = h })
			defer func() { require.NoError(t, a.Dispose()) }()
			require.NoError(t, a.Prompt(context.Background(), user("go")))
			switch action {
			case "abort":
				a.Abort()
			case "reset":
				require.NoError(t, a.Reset())
			case "dispose":
				require.NoError(t, a.Dispose())
				require.ErrorIs(t, a.Prompt(context.Background(), user("next")), agent.ErrDisposed)
				return
			}
			require.NoError(t, a.Prompt(context.Background(), user("next")))
			require.NotContains(t, userTexts(p.Requests()[1].Transcript.Messages), "queued")
		})
	}
}
func TestDisposeDropsContextProducedByLatePostHook(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("echo", "a", map[string]any{"text": "hi"})))
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	h := pipeline.NewRegistry()
	h.OnAfterTool(func(ctx context.Context, _ pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		require.Error(t, ctx.Err())
		return &pipeline.AfterToolCallResult{AddedContext: []protocol.UserMessage{user("late").(protocol.UserMessage)}}, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = h })
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-entered
	disposed := make(chan error, 1)
	go func() { disposed <- a.Dispose() }()
	<-cancelled
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, <-disposed)
	require.NotContains(t, userTexts(a.State().Messages), "late")
}
