package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestTwoTurnEventOrder(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	rec := &recorder{}

	msgs, err := agent.Run(context.Background(), []protocol.Message{user("echo hi")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, nil), rec.emit)
	require.NoError(t, err)

	for _, l := range rec.eventLabels() {
		t.Log(l)
	}
	assert.Equal(t, []string{
		"agent_start",
		"cycle_start",
		"turn_start",
		"message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
		"attempt_start", "message_start(assistant)",
		"message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)",
		"message_end(assistant)", "attempt_end",
		"tool_execution_start(c1)", "tool_execution_end(c1)",
		"message_start(toolResult:c1)", "message_end(toolResult:c1)",
		"turn_end",
		"turn_start",
		"attempt_start", "message_start(assistant)",
		"message_update(text_start)", "message_update(text_delta)", "message_update(text_end)",
		"message_end(assistant)", "attempt_end",
		"turn_end", "cycle_end",
		"agent_end",
	}, rec.eventLabels())

	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:c1", "assistant"}, roles(msgs))
	assert.Equal(t, "hi", resultText(toolResults(msgs)["c1"].Content))
	assert.Equal(t, "done", assistantText(lastAssistant(t, msgs)))
	reqs := p.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:c1"}, roles(reqs[1].Transcript.Messages),
		"the partial was replaced, not appended, and the result joined the context")
	end := rec.events[len(rec.events)-1].(*protocol.AgentEnd)
	assert.Equal(t, roles(msgs), roles(end.Messages))
}

func TestPrepareRequestRunsAfterTurnStartAndBeforeAttempt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	rec := &recorder{}
	reg := pipeline.NewRegistry()
	reg.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		rec.note("prepare")
		return nil, nil
	})
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, reg), rec.emit)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"agent_start", "cycle_start", "turn_start", "prepare", "message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
		"attempt_start", "message_start(assistant)", "message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)", "message_end(assistant)", "attempt_end",
		"tool_execution_start(c1)", "tool_execution_end(c1)", "message_start(toolResult:c1)", "message_end(toolResult:c1)",
		"turn_end",
		"turn_start",
		"prepare",
		"attempt_start", "message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)", "attempt_end",
		"turn_end",
		"cycle_end",
		"agent_end",
	}, rec.labels())
}

func TestFinishTurnContinue(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("first"), faux.Say("second"))
	rec := &recorder{}
	decisions := []pipeline.TurnDecision{pipeline.Continue, pipeline.Proceed}
	reg := pipeline.NewRegistry()
	reg.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		d := decisions[0]
		decisions = decisions[1:]
		return d, nil
	})
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{}, config(p, m, reg), rec.emit)
	require.NoError(t, err)

	assert.Equal(t, 2, p.Calls(), "Continue with no tool call gives exactly one more request")
	assert.Equal(t, []string{"user", "assistant"}, roles(p.Requests()[1].Transcript.Messages),
		"the extra request carries the current context and no new message")
	assert.Equal(t, []string{"user", "assistant", "assistant"}, roles(msgs))
	assert.Equal(t, []string{
		"agent_start", "cycle_start", "turn_start", "message_start(user)", "message_end(user)",
		"attempt_start", "message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)", "attempt_end",
		"turn_end",
		"turn_start",
		"attempt_start", "message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)", "attempt_end",
		"turn_end", "cycle_end",
		"agent_end",
	}, rec.eventLabels())
}

func TestFinishTurnContinueSatisfiedByToolResults(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"})), faux.Say("done"), faux.Say("unused"))
	first := true
	reg := pipeline.NewRegistry()
	reg.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		if first {
			first = false
			return pipeline.Continue, nil
		}
		return pipeline.Proceed, nil
	})
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, reg), (&recorder{}).emit)
	require.NoError(t, err)
	assert.Equal(t, 2, p.Calls(), "the tool-result request satisfies Continue")
}

func TestFinishTurnEnd(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("unused"))
	rec := &recorder{}
	reg := pipeline.NewRegistry()
	reg.OnCompleteStep(func(_ context.Context, turn pipeline.Turn) (pipeline.TurnDecision, error) {
		rec.note("finish:" + strings.Join(roles(turn.NewMessages), ","))
		return pipeline.End, nil
	})
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, reg), rec.emit)
	require.NoError(t, err)

	assert.Equal(t, 1, p.Calls())
	assert.Equal(t, []string{
		"agent_start", "cycle_start", "turn_start", "message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
		"attempt_start", "message_start(assistant)", "message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)", "message_end(assistant)", "attempt_end",
		"tool_execution_start(c1)", "tool_execution_end(c1)", "message_start(toolResult:c1)", "message_end(toolResult:c1)",
		"finish:system,user,assistant,toolResult:c1",
		"turn_end", "cycle_end",
		"agent_end",
	}, rec.labels(), "no poll runs after End")
}

func TestErrorTail(t *testing.T) {
	cases := []struct {
		name   string
		step   faux.Step
		reason protocol.StopReason
		text   string
	}{
		{"error", faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))).Stop(protocol.StopError).Error("boom"), protocol.StopError, "boom"},
		{"aborted", faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))).Stop(protocol.StopAborted).Error("stopped"), protocol.StopAborted, "stopped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(tc.step, faux.Say("unused"))
			rec := &recorder{}
			executed := false
			echo := &funcTool{name: "echo", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
				executed = true
				return textResult("x"), nil
			}}
			reg := pipeline.NewRegistry()
			reg.OnCompleteStep(func(_ context.Context, turn pipeline.Turn) (pipeline.TurnDecision, error) {
				rec.note("finish")
				return pipeline.Continue, nil
			})
			msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
				pipeline.AgentContext{Tools: registry(t, echo)}, config(p, m, reg), rec.emit)
			require.NoError(t, err)

			assert.Equal(t, []string{
				"agent_start", "cycle_start", "turn_start", "message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
				"attempt_start", "message_start(assistant)", "message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)", "message_end(assistant)", "attempt_end",
				"finish",
				"turn_end", "cycle_end",
				"agent_end",
			}, rec.labels())
			assert.False(t, executed)
			assert.Equal(t, 1, p.Calls(), "the Continue decision is ignored")
			turnEnd := rec.events[len(rec.events)-3].(*protocol.TurnEnd)
			assert.Equal(t, []protocol.ToolResultMessage{}, turnEnd.ToolResults)
			last := lastAssistant(t, msgs)
			assert.Equal(t, tc.reason, last.StopReason)
			require.NotNil(t, last.ErrorMessage)
			assert.Equal(t, tc.text, *last.ErrorMessage)
		})
	}
}

func TestContinue(t *testing.T) {
	p, m := newFaux(t)
	cfg := config(p, m, nil)

	t.Run("empty context", func(t *testing.T) {
		rec := &recorder{}
		_, err := agent.Continue(context.Background(), pipeline.AgentContext{}, cfg, rec.emit)
		require.ErrorIs(t, err, agent.ErrContinueEmpty)
		assert.Empty(t, rec.eventLabels())
	})

	t.Run("assistant tail", func(t *testing.T) {
		rec := &recorder{}
		tail := protocol.AssistantMessage{StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{protocol.Text{Text: "x"}}}
		_, err := agent.Continue(context.Background(), pipeline.AgentContext{Messages: []protocol.Message{user("a"), tail}}, cfg, rec.emit)
		require.ErrorIs(t, err, agent.ErrContinueFromAssistant)
		assert.Empty(t, rec.eventLabels())
		assert.Equal(t, 0, p.Calls())
	})

	t.Run("user tail", func(t *testing.T) {
		p.Set(faux.Say("again"))
		rec := &recorder{}
		history := make([]protocol.Message, 1, 4)
		history[0] = user("a")
		msgs, err := agent.Continue(context.Background(), pipeline.AgentContext{Messages: history}, cfg, rec.emit)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"agent_start", "cycle_start", "turn_start",
			"attempt_start", "message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)", "attempt_end",
			"turn_end", "cycle_end", "agent_end",
		}, rec.eventLabels())
		assert.Equal(t, []string{"assistant"}, roles(msgs), "the result holds only what the run added")
		assert.Equal(t, "again", assistantText(lastAssistant(t, msgs)))
		assert.Nil(t, history[:2][1], "the loop never writes into the caller's spare capacity")
	})
}

func TestHookErrorReturned(t *testing.T) {
	boom := errors.New("hook failed")
	cases := []struct {
		name  string
		setup func(cfg *agent.LoopConfig, reg *pipeline.Registry)
	}{
		{"ConvertToLLM", func(cfg *agent.LoopConfig, _ *pipeline.Registry) {
			cfg.ConvertToLLM = func([]protocol.Message) ([]protocol.Message, error) { return nil, boom }
		}},
		{"GetAPIKey", func(cfg *agent.LoopConfig, _ *pipeline.Registry) {
			cfg.GetAPIKey = func(context.Context, string) (string, error) { return "", boom }
		}},
		{"PrepareRequest", func(_ *agent.LoopConfig, reg *pipeline.Registry) {
			reg.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				return nil, boom
			})
		}},
		{"ExecuteModel", func(_ *agent.LoopConfig, reg *pipeline.Registry) {
			reg.OnExecuteModel(func(context.Context, pipeline.ModelCall, pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
				return nil, boom
			})
		}},
		{"CompleteStep", func(_ *agent.LoopConfig, reg *pipeline.Registry) {
			reg.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) { return pipeline.Proceed, boom })
		}},
		{"AdmitStep", func(_ *agent.LoopConfig, reg *pipeline.Registry) {
			reg.OnAdmitStep(func(context.Context, pipeline.AdmitInput, pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
				return pipeline.AdmitDecision{}, boom
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Say("hi"))
			rec := &recorder{}
			reg := pipeline.NewRegistry()
			cfg := config(p, m, reg)
			tc.setup(&cfg, reg)
			msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
				pipeline.AgentContext{}, cfg, rec.emit)
			require.ErrorIs(t, err, boom)
			assert.Same(t, boom, err, "the error is returned unchanged")
			assert.Nil(t, msgs)
			assert.NotContains(t, rec.eventLabels(), "agent_end")
		})
	}
}

func TestEmitErrorEndsRun(t *testing.T) {
	for _, failOn := range []string{"agent_start", "message_update(text_delta)", "tool_execution_end(c1)", "message_end(toolResult:c1)", "agent_end"} {
		t.Run(failOn, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(faux.Text("x"), faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
			rec := &recorder{failOn: failOn}
			_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
				pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, nil), rec.emit)
			require.ErrorIs(t, err, errEmit)
			labels := rec.eventLabels()
			assert.Equal(t, failOn, labels[len(labels)-1], "nothing is emitted after the failing event")
		})
	}
}

func TestPrepareRequestReplacesContext(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	other := m
	other.ID = "faux-2"
	calls := 0
	reg := pipeline.NewRegistry()
	reg.OnPrepareRequest(func(_ context.Context, r pipeline.Request, _ pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		calls++
		if calls > 1 {
			return nil, nil
		}
		assert.Equal(t, protocol.ThinkingOff, r.ThinkingLevel)
		replaced := r.Context
		replaced.Messages = []protocol.Message{user("projected")}
		return &pipeline.RequestUpdate{Context: &replaced, ThinkingLevel: protocol.ThinkingHigh}, nil
	})
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, reg), (&recorder{}).emit)
	require.NoError(t, err)

	reqs := p.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "projected", resultText(reqs[0].Transcript.Messages[0].(protocol.UserMessage).Content))
	assert.Equal(t, []string{"user", "assistant", "toolResult:c1", "system"}, roles(reqs[1].Transcript.Messages),
		"the replacement stays for later requests; it dropped the tool declaration, so the next turn declares the tools again")
	assert.Equal(t, protocol.ThinkingHigh, reqs[1].Options.Reasoning)
	level := lastAssistant(t, msgs).ThinkingLevel
	require.NotNil(t, level)
	assert.Equal(t, protocol.ThinkingHigh, *level)
	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:c1", "system", "assistant"}, roles(msgs),
		"the run still reports the prompt it added")
}

func TestGetAPIKeyFallback(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("a"), faux.Say("b"))
	keys := []string{"", "fresh"}
	reg := pipeline.NewRegistry()
	reg.OnCompleteStep(func(_ context.Context, turn pipeline.Turn) (pipeline.TurnDecision, error) {
		if len(turn.NewMessages) == 2 {
			return pipeline.Continue, nil
		}
		return pipeline.Proceed, nil
	})
	cfg := agent.LoopConfig{
		Model: m, Stream: p.Stream, Options: providers.StreamOptions{APIKey: "static"},
		GetAPIKey: func(_ context.Context, provider string) (string, error) {
			assert.Equal(t, "faux", provider)
			k := keys[0]
			keys = keys[1:]
			return k, nil
		},
		Pipeline: reg,
	}
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, cfg, (&recorder{}).emit)
	require.NoError(t, err)
	reqs := p.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "static", reqs[0].Options.APIKey)
	assert.Equal(t, "fresh", reqs[1].Options.APIKey)
}

func TestPrepareRequestModelSwitchStaysForLaterRequests(t *testing.T) {
	p, m := newFaux(t, faux.WithModels(faux.ModelDef{ID: "faux-1"}, faux.ModelDef{ID: "faux-2"}))
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	other, ok := p.Model("faux-2")
	require.True(t, ok)
	calls := 0
	reg := pipeline.NewRegistry()
	reg.OnPrepareRequest(func(_ context.Context, r pipeline.Request, _ pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		calls++
		if calls == 1 {
			return &pipeline.RequestUpdate{Model: &other}, nil
		}
		assert.Equal(t, "faux-2", r.Model.ID, "the second request starts from the switched model")
		return nil, nil
	})

	_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, reg), (&recorder{}).emit)
	require.NoError(t, err)

	reqs := p.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "faux-2", reqs[0].Model.ID, "the switch applies to the request it was made for")
	assert.Equal(t, "faux-2", reqs[1].Model.ID, "and stays for the later request with no new update")
	assert.Equal(t, 2, calls)
}
