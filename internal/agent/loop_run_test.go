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
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, pipeline.Hooks{}), rec.emit)
	require.NoError(t, err)

	for _, l := range rec.eventLabels() {
		t.Log(l)
	}
	assert.Equal(t, []string{
		"agent_start",
		"turn_start",
		"message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
		"message_start(assistant)",
		"message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)",
		"message_end(assistant)",
		"tool_execution_start(c1)", "tool_execution_end(c1)",
		"message_start(toolResult:c1)", "message_end(toolResult:c1)",
		"turn_end",
		"turn_start",
		"message_start(assistant)",
		"message_update(text_start)", "message_update(text_delta)", "message_update(text_end)",
		"message_end(assistant)",
		"turn_end",
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

func TestSteeringPollPoints(t *testing.T) {
	t.Run("empty queues", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
		rec := &recorder{}
		hooks := pipeline.Hooks{
			GetSteeringMessages: func(context.Context) ([]protocol.Message, error) { rec.note("steer"); return nil, nil },
			GetFollowUpMessages: func(context.Context) ([]protocol.Message, error) { rec.note("follow"); return nil, nil },
			PrepareRequest: func(context.Context, pipeline.Request) (*pipeline.RequestUpdate, error) {
				rec.note("prepare")
				return nil, nil
			},
		}
		_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
			pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, hooks), rec.emit)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"agent_start", "turn_start", "message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
			"steer",
			"prepare",
			"message_start(assistant)", "message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)", "message_end(assistant)",
			"tool_execution_start(c1)", "tool_execution_end(c1)", "message_start(toolResult:c1)", "message_end(toolResult:c1)",
			"turn_end",
			"steer",
			"steer",
			"turn_start",
			"prepare",
			"message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)",
			"turn_end",
			"steer",
			"follow",
			"agent_end",
		}, rec.labels())
	})

	t.Run("steering message skips the second poll", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("two"))
		rec := &recorder{}
		polls := 0
		hooks := pipeline.Hooks{
			GetSteeringMessages: func(context.Context) ([]protocol.Message, error) {
				polls++
				rec.note("steer")
				if polls == 2 {
					return []protocol.Message{user("steer")}, nil
				}
				return nil, nil
			},
		}
		msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
			pipeline.AgentContext{}, config(p, m, hooks), rec.emit)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"agent_start", "turn_start", "message_start(user)", "message_end(user)",
			"steer",
			"message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)",
			"turn_end",
			"steer",
			"turn_start",
			"message_start(user)", "message_end(user)",
			"message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)",
			"turn_end",
			"steer",
			"agent_end",
		}, rec.labels())
		assert.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(msgs))
	})

	t.Run("follow-up starts a new turn", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("two"))
		rec := &recorder{}
		follows := 0
		hooks := pipeline.Hooks{
			GetFollowUpMessages: func(context.Context) ([]protocol.Message, error) {
				follows++
				if follows == 1 {
					return []protocol.Message{user("more")}, nil
				}
				return nil, nil
			},
		}
		msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
			pipeline.AgentContext{}, config(p, m, hooks), rec.emit)
		require.NoError(t, err)
		assert.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(msgs))
		assert.Equal(t, 2, follows)
		assert.Equal(t, 2, p.Calls())
	})
}

func TestFinishTurnContinue(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("first"), faux.Say("second"))
	rec := &recorder{}
	decisions := []pipeline.TurnDecision{pipeline.Continue, pipeline.Proceed}
	hooks := pipeline.Hooks{
		FinishTurn: func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			d := decisions[0]
			decisions = decisions[1:]
			return d, nil
		},
	}
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{}, config(p, m, hooks), rec.emit)
	require.NoError(t, err)

	assert.Equal(t, 2, p.Calls(), "Continue with no tool call gives exactly one more request")
	assert.Equal(t, []string{"user", "assistant"}, roles(p.Requests()[1].Transcript.Messages),
		"the extra request carries the current context and no new message")
	assert.Equal(t, []string{"user", "assistant", "assistant"}, roles(msgs))
	assert.Equal(t, []string{
		"agent_start", "turn_start", "message_start(user)", "message_end(user)",
		"message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)",
		"turn_end",
		"turn_start",
		"message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)",
		"turn_end",
		"agent_end",
	}, rec.eventLabels())
}

func TestFinishTurnContinueSatisfiedByToolResults(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"})), faux.Say("done"), faux.Say("unused"))
	first := true
	hooks := pipeline.Hooks{
		FinishTurn: func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			if first {
				first = false
				return pipeline.Continue, nil
			}
			return pipeline.Proceed, nil
		},
	}
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, hooks), (&recorder{}).emit)
	require.NoError(t, err)
	assert.Equal(t, 2, p.Calls(), "the tool-result request satisfies Continue")
}

func TestFinishTurnEnd(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("unused"))
	rec := &recorder{}
	hooks := pipeline.Hooks{
		GetSteeringMessages: func(context.Context) ([]protocol.Message, error) { rec.note("steer"); return nil, nil },
		GetFollowUpMessages: func(context.Context) ([]protocol.Message, error) { rec.note("follow"); return nil, nil },
		FinishTurn: func(_ context.Context, turn pipeline.Turn) (pipeline.TurnDecision, error) {
			rec.note("finish:" + strings.Join(roles(turn.NewMessages), ","))
			return pipeline.End, nil
		},
	}
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, hooks), rec.emit)
	require.NoError(t, err)

	assert.Equal(t, 1, p.Calls())
	assert.Equal(t, []string{
		"agent_start", "turn_start", "message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
		"steer",
		"message_start(assistant)", "message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)", "message_end(assistant)",
		"tool_execution_start(c1)", "tool_execution_end(c1)", "message_start(toolResult:c1)", "message_end(toolResult:c1)",
		"finish:system,user,assistant,toolResult:c1",
		"turn_end",
		"agent_end",
	}, rec.labels(), "no poll hook runs after End")
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
			hooks := pipeline.Hooks{
				GetSteeringMessages: func(context.Context) ([]protocol.Message, error) { rec.note("steer"); return nil, nil },
				GetFollowUpMessages: func(context.Context) ([]protocol.Message, error) { rec.note("follow"); return nil, nil },
				FinishTurn: func(_ context.Context, turn pipeline.Turn) (pipeline.TurnDecision, error) {
					rec.note("finish")
					return pipeline.Continue, nil
				},
			}
			msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
				pipeline.AgentContext{Tools: registry(t, echo)}, config(p, m, hooks), rec.emit)
			require.NoError(t, err)

			assert.Equal(t, []string{
				"agent_start", "turn_start", "message_start(system)", "message_end(system)", "message_start(user)", "message_end(user)",
				"steer",
				"message_start(assistant)", "message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)", "message_end(assistant)",
				"finish",
				"turn_end",
				"agent_end",
			}, rec.labels())
			assert.False(t, executed)
			assert.Equal(t, 1, p.Calls(), "the Continue decision is ignored")
			turnEnd := rec.events[len(rec.events)-2].(*protocol.TurnEnd)
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
	cfg := config(p, m, pipeline.Hooks{})

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
			"agent_start", "turn_start",
			"message_start(assistant)", "message_update(text_start)", "message_update(text_delta)", "message_update(text_end)", "message_end(assistant)",
			"turn_end", "agent_end",
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
		hooks pipeline.Hooks
	}{
		{"TransformContext", pipeline.Hooks{TransformContext: func(context.Context, []protocol.Message) ([]protocol.Message, error) { return nil, boom }}},
		{"ConvertToLLM", pipeline.Hooks{ConvertToLLM: func([]protocol.Message) ([]protocol.Message, error) { return nil, boom }}},
		{"GetAPIKey", pipeline.Hooks{GetAPIKey: func(context.Context, string) (string, error) { return "", boom }}},
		{"PrepareRequest", pipeline.Hooks{PrepareRequest: func(context.Context, pipeline.Request) (*pipeline.RequestUpdate, error) { return nil, boom }}},
		{"FinishTurn", pipeline.Hooks{FinishTurn: func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) { return pipeline.Proceed, boom }}},
		{"GetSteeringMessages", pipeline.Hooks{GetSteeringMessages: func(context.Context) ([]protocol.Message, error) { return nil, boom }}},
		{"GetFollowUpMessages", pipeline.Hooks{GetFollowUpMessages: func(context.Context) ([]protocol.Message, error) { return nil, boom }}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Say("hi"))
			rec := &recorder{}
			msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
				pipeline.AgentContext{}, config(p, m, tc.hooks), rec.emit)
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
				pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, pipeline.Hooks{}), rec.emit)
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
	hooks := pipeline.Hooks{
		PrepareRequest: func(_ context.Context, r pipeline.Request) (*pipeline.RequestUpdate, error) {
			calls++
			if calls > 1 {
				return nil, nil
			}
			assert.Equal(t, protocol.ThinkingOff, r.ThinkingLevel)
			replaced := r.Context
			replaced.Messages = []protocol.Message{user("projected")}
			return &pipeline.RequestUpdate{Context: &replaced, ThinkingLevel: protocol.ThinkingHigh}, nil
		},
	}
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, hooks), (&recorder{}).emit)
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
	cfg := agent.LoopConfig{
		Model: m, Stream: p.Stream, Options: providers.StreamOptions{APIKey: "static"},
		Hooks: pipeline.Hooks{
			GetAPIKey: func(_ context.Context, provider string) (string, error) {
				assert.Equal(t, "faux", provider)
				k := keys[0]
				keys = keys[1:]
				return k, nil
			},
			FinishTurn: func(_ context.Context, turn pipeline.Turn) (pipeline.TurnDecision, error) {
				if len(turn.NewMessages) == 2 {
					return pipeline.Continue, nil
				}
				return pipeline.Proceed, nil
			},
		},
	}
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, cfg, (&recorder{}).emit)
	require.NoError(t, err)
	reqs := p.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "static", reqs[0].Options.APIKey)
	assert.Equal(t, "fresh", reqs[1].Options.APIKey)
}
