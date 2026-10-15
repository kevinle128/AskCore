package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestAbortGivesEveryCallAnOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		cancel()
		return textResult("late"), nil
	}}
	r := runTools(t, ctx, registry(t, tool), nil, nil, faux.Reply(call("t", "a", nil), call("t", "b", nil), call("t", "c", nil)))
	res := toolResults(r.msgs)
	require.Len(t, res, 3)
	require.Equal(t, "Operation aborted", resultText(res["a"].Content))
	for _, id := range []string{"b", "c"} {
		require.Equal(t, "Tool call aborted before dispatch", resultText(res[id].Content))
		require.True(t, res[id].IsError)
	}
	require.Equal(t, 1, r.p.Calls())
}
func TestAbortClassifiesByBodyInvocationNotCallRecord(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil), call("t", "b", nil)))
	log := &sessions.MemoryLog{}
	var a *agent.Agent
	var bodies int
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if in.Call.ID == "b" {
			var intent bool
			for _, e := range log.Entries() {
				if c, ok := e.(sessions.ToolCall); ok && c.CallID == "b" {
					intent = true
				}
			}
			require.True(t, intent)
			a.Abort()
		}
		return nil, nil
	})
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies++
		return textResult("ok"), nil
	}}
	a = newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tool)
		c.Pipeline = h
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	require.Equal(t, 1, bodies)
	require.Equal(t, "Tool call aborted before dispatch", resultText(toolResults(log.Messages())["b"].Content))
}
func TestAbortBeforeBatchStartsNoBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var bodies int
	tool := okTool("t")
	tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies++
		return textResult("bad"), nil
	}
	r := runTools(t, ctx, registry(t, tool), nil, func(next agent.Emit) agent.Emit {
		return func(e protocol.Event) error {
			if end, ok := e.(*protocol.MessageEnd); ok {
				if m, ok := end.Message.(protocol.AssistantMessage); ok && m.StopReason == protocol.StopToolUse {
					cancel()
				}
			}
			return next(e)
		}
	}, faux.Reply(call("t", "a", nil), call("t", "b", nil)))
	require.Zero(t, bodies)
	for _, r := range toolResults(r.msgs) {
		require.Equal(t, "Tool call aborted before dispatch", resultText(r.Content))
	}
	require.Len(t, toolResults(r.msgs), 2)
}
func TestAbortDrainsStartedBodiesBeforeSettled(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil), call("t", "b", nil)))
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	returned := make(chan struct{}, 2)
	tool := safeTool{funcTool: &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		started <- struct{}{}
		<-release
		returned <- struct{}{}
		return textResult("late"), nil
	}}}
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })
	defer func() { require.NoError(t, a.Dispose()) }()
	settled := make(chan struct{})
	a.Subscribe(signalOn("agent_settled", settled))
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-started
	<-started
	a.Abort()
	select {
	case <-settled:
		t.Error("settled before body drain")
	default:
	}
	require.Equal(t, agent.Running, a.State().Status)
	close(release)
	require.NoError(t, <-done)
	require.Len(t, returned, 2)
	<-settled
}
func TestAbortWaitsForUncooperativeToolAndRejectsNewPrompt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil)))
	started := make(chan struct{})
	release := make(chan struct{})
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		close(started)
		<-release
		return textResult("late"), nil
	}}
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })
	defer func() { require.NoError(t, a.Dispose()) }()
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-started
	a.Abort()
	require.ErrorIs(t, a.Prompt(context.Background(), user("second")), agent.ErrBusy)
	select {
	case <-done:
		t.Error("uncooperative tool was detached")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-done)
	require.Equal(t, agent.Idle, a.State().Status)
}
func TestRepeatedAbortNeverExceedsMaxParallelTools(t *testing.T) {
	p, m := newFaux(t)
	steps := make([]faux.Step, 3)
	for i := range steps {
		steps[i] = faux.Reply(call("t", fmt.Sprint(i, "a"), nil), call("t", fmt.Sprint(i, "b"), nil), call("t", fmt.Sprint(i, "c"), nil))
	}
	p.Set(steps...)
	var active, peak atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{}, 6)
	tool := safeTool{funcTool: &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		n := active.Add(1)
		for x := peak.Load(); n > x; x = peak.Load() {
			if peak.CompareAndSwap(x, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return textResult("late"), nil
	}}}
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool); c.MaxParallelTools = 2 })
	defer func() { require.NoError(t, a.Dispose()) }()
	for range 3 {
		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		<-started
		<-started
		for range 5 {
			a.Abort()
		}
		require.ErrorIs(t, a.Prompt(context.Background(), user("busy")), agent.ErrBusy)
		release <- struct{}{}
		release <- struct{}{}
		require.NoError(t, <-done)
		require.Zero(t, active.Load())
	}
	require.Equal(t, int32(2), peak.Load())
}
func TestAbortDuringToolBatchEndsWithAbortedAssistantMessageAndNoModelCall(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil)), faux.Say("unused"))
	var a *agent.Agent
	log := &sessions.MemoryLog{}
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		a.Abort()
		return textResult("late"), nil
	}}
	a = newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tool)
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	var attempts, turns, aborted int
	a.Subscribe(func(e protocol.Event) error {
		switch e := e.(type) {
		case *protocol.AttemptStart:
			attempts++
		case *protocol.TurnStart:
			turns++
		case *protocol.CycleEnd:
			if e.Reason == "aborted" {
				aborted++
				require.Equal(t, "user", e.Cause)
			}
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	require.Equal(t, 1, p.Calls())
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, turns)
	require.Equal(t, 1, aborted)
	require.Equal(t, protocol.StopAborted, lastAssistant(t, log.Messages()).StopReason)
}
func TestAbortMessageMatchesRunFailureMessageShape(t *testing.T) {
	var messages []protocol.AssistantMessage
	for _, failure := range []bool{false, true} {
		p, m := newFaux(t)
		p.Set(faux.Reply(call("t", "a", nil)))
		var a *agent.Agent
		tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			a.Abort()
			return textResult("late"), nil
		}}
		h := pipeline.NewRegistry()
		if failure {
			h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
				a.Abort()
				return nil, nil
			})
			h.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
				return pipeline.Proceed, errors.New("aborted by the user")
			})
		}
		a = newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool); c.Pipeline = h })
		err := a.Prompt(context.Background(), user("go"))
		if failure {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		msg := lastAssistant(t, a.State().Messages)
		messages = append(messages, msg)
		require.NoError(t, a.Dispose())
	}
	require.Equal(t, messages[0], messages[1])
	require.Equal(t, []protocol.AssistantBlock{protocol.Text{Text: ""}}, messages[0].Content)
	require.Equal(t, int64(fixedMillis), messages[0].Timestamp)
}
func TestPairingHoldsAfterEveryAbortPath(t *testing.T) {
	for _, path := range []string{"pre", "before", "execute", "body", "after"} {
		t.Run(path, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := pipeline.NewRegistry()
			tool := okTool("t")
			wrap := func(next agent.Emit) agent.Emit {
				return func(e protocol.Event) error {
					if path == "pre" {
						if end, ok := e.(*protocol.MessageEnd); ok && end.Message.Role() == "assistant" {
							cancel()
						}
					}
					return next(e)
				}
			}
			switch path {
			case "before":
				h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
					cancel()
					return nil, nil
				})
			case "execute":
				h.OnExecuteTool(func(context.Context, pipeline.ExecuteToolInput, pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
					cancel()
					return textResult("late"), nil
				})
			case "body":
				tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
					cancel()
					return textResult("late"), nil
				}
			case "after":
				h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
					cancel()
					return nil, nil
				})
			}
			r := runTools(t, ctx, registry(t, tool), h, wrap, faux.Reply(call("t", "a", nil), call("t", "b", nil), call("t", "c", nil)))
			res := toolResults(r.msgs)
			require.Len(t, res, 3)
			for _, id := range []string{"a", "b", "c"} {
				require.Contains(t, res, id)
			}
			require.Equal(t, protocol.StopAborted, lastAssistant(t, r.msgs).StopReason)
		})
	}
}

func TestToolWriterPanicDrainsBodiesBeforeRepairAndFailureTail(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil), call("t", "b", nil)))
	second := make(chan struct{})
	release := make(chan struct{})
	cancelled := make(chan struct{})
	log := &panicResultLog{}
	tool := safeTool{funcTool: &funcTool{name: "t", run: func(ctx context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if tc.CallID == "a" {
			<-second
		} else {
			close(second)
			<-ctx.Done()
			close(cancelled)
			<-release
		}
		return textResult("late"), nil
	}}}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tool)
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-cancelled
	require.Equal(t, agent.Running, a.State().Status)
	require.ErrorIs(t, a.Prompt(context.Background(), user("busy")), agent.ErrBusy)
	require.Empty(t, toolResults(log.Messages()))
	select {
	case <-done:
		t.Error("writer panic escaped before drain")
	default:
	}
	close(release)
	err := <-done
	require.Contains(t, err.Error(), "writer panic")
	res := toolResults(log.Messages())
	require.Len(t, res, 2)
	for _, res := range res {
		require.Equal(t, "outcome unknown", resultText(res.Content))
	}
}

type panicResultLog struct {
	sessions.MemoryLog
	panicked bool
}

func (l *panicResultLog) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	for _, e := range entries {
		if m, ok := e.(sessions.MessageEntry); ok {
			if _, ok := m.Message.(protocol.ToolResultMessage); ok && !l.panicked {
				l.panicked = true
				panic("writer panic")
			}
		}
	}
	return l.MemoryLog.Append(entries...)
}
