package agent_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

const fixedMillis = 1700000000000

func fixedClock() time.Time { return time.UnixMilli(fixedMillis) }

func newAgent(t testing.TB, p *faux.Provider, m providers.Model, edit func(*agent.Config)) *agent.Agent {
	t.Helper()
	cfg := agent.Config{
		LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream},
		Tools:      registry(t, tools.Echo{}),
		SessionID:  "s1",
		Clock:      fixedClock,
	}
	if edit != nil {
		edit(&cfg)
	}
	a, err := agent.New(cfg)
	require.NoError(t, err)
	return a
}

func failureTail() []string {
	return []string{"message_start(assistant)", "message_end(assistant)", "turn_end", "agent_end", "agent_settled"}
}

func errorText(m protocol.AssistantMessage) string {
	if m.ErrorMessage == nil {
		return ""
	}
	return *m.ErrorMessage
}

// signalOn returns a listener that closes ch at the first event with label l.
func signalOn(l string, ch chan struct{}) func(protocol.Event) error {
	var once sync.Once
	return func(ev protocol.Event) error {
		if label(ev) == l {
			once.Do(func() { close(ch) })
		}
		return nil
	}
}

func TestAgentPromptSettles(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("echo hi")))

	for _, ev := range rec.events {
		t.Logf("seq=%d %s", ev.Env().Seq, label(ev))
	}
	assert.Equal(t, []string{
		"agent_start",
		"turn_start",
		"message_start(user)", "message_end(user)",
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
		"agent_settled",
	}, rec.eventLabels())

	runID := rec.events[0].Env().RunID
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), runID)
	for i, ev := range rec.events {
		assert.Equal(t, protocol.Envelope{Seq: uint64(i + 1), TS: fixedMillis, SessionID: "s1", RunID: runID}, *ev.Env())
	}

	st := a.State()
	assert.Equal(t, agent.Idle, st.Status)
	assert.Equal(t, []string{"user", "assistant", "toolResult:c1", "assistant"}, roles(st.Messages))
	assert.Equal(t, "done", assistantText(lastAssistant(t, st.Messages)))
	assert.Equal(t, "s1", p.Requests()[0].Options.SessionID)
}

func TestAgentEnvelopeAcrossPrompts(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("a")))
	first := len(rec.events)
	require.NoError(t, a.Prompt(context.Background(), user("b")))

	require.Equal(t, 2*first, len(rec.events))
	for i, ev := range rec.events {
		assert.Equal(t, uint64(i+1), ev.Env().Seq)
	}
	assert.Equal(t, "agent_settled", label(rec.events[first-1]))
	assert.Equal(t, "agent_start", label(rec.events[first]))
	assert.Equal(t, rec.events[0].Env().RunID, rec.events[first-1].Env().RunID)
	assert.NotEqual(t, rec.events[0].Env().RunID, rec.events[first].Env().RunID)
}

func TestAgentBusy(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(faux.Say(strings.Repeat("slow words ", 50)).Pace(200))
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))

	assert.Equal(t, agent.Running, a.State().Status)
	require.ErrorIs(t, a.Prompt(context.Background(), user("again")), agent.ErrBusy)
	require.ErrorIs(t, a.Continue(context.Background()), agent.ErrBusy)
	require.ErrorIs(t, a.Reset(), agent.ErrBusy)

	a.Abort()
	require.NoError(t, <-done)
	assert.Equal(t, 1, p.Calls(), "the busy prompt made no request")
	st := a.State()
	assert.Equal(t, agent.Idle, st.Status)
	assert.Equal(t, protocol.StopAborted, lastAssistant(t, st.Messages).StopReason)

	require.NoError(t, a.Reset())
	assert.Empty(t, a.State().Messages)
}

func TestAgentListenerOrder(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	var calls []string
	listen := func(name string) func(protocol.Event) error {
		return func(ev protocol.Event) error {
			calls = append(calls, name+":"+ev.EventType())
			return nil
		}
	}
	a.Subscribe(listen("1"))
	unsubscribe := a.Subscribe(listen("2"))
	a.Subscribe(listen("3"))

	require.NoError(t, a.Prompt(context.Background(), user("a")))
	require.Equal(t, "1:agent_start", calls[0])
	for i := 0; i < len(calls); i += 3 {
		typ := strings.SplitN(calls[i], ":", 2)[1]
		assert.Equal(t, []string{"1:" + typ, "2:" + typ, "3:" + typ}, calls[i:i+3])
	}
	assert.Equal(t, "3:agent_settled", calls[len(calls)-1])

	unsubscribe()
	unsubscribe()
	calls = nil
	require.NoError(t, a.Prompt(context.Background(), user("b")))
	assert.Equal(t, []string{"1:agent_start", "3:agent_start", "1:turn_start", "3:turn_start"}, calls[:4])
}

func TestAgentListenerError(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("never"))
	a := newAgent(t, p, m, nil)
	first, third := &recorder{}, &recorder{}
	a.Subscribe(first.emit)
	a.Subscribe((&recorder{failOn: "turn_start"}).emit)
	a.Subscribe(third.emit)

	err := a.Prompt(context.Background(), user("hi"))
	require.ErrorIs(t, err, errEmit)

	assert.Equal(t, append([]string{"agent_start", "turn_start"}, failureTail()...), first.eventLabels())
	assert.Equal(t, append([]string{"agent_start"}, failureTail()...), third.eventLabels(),
		"a failing listener stops the later listeners for that event only")
	assert.Equal(t, 0, p.Calls())

	st := a.State()
	assert.Equal(t, agent.Idle, st.Status)
	require.Len(t, st.Messages, 1)
	msg := st.Messages[0].(protocol.AssistantMessage)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, "listener failed", errorText(msg))
	assert.Equal(t, []protocol.AssistantBlock{protocol.Text{Text: ""}}, msg.Content)
	assert.Equal(t, int64(fixedMillis), msg.Timestamp)
	assert.Equal(t, m.Provider, msg.Provider)
	assert.Equal(t, m.ID, msg.Model)
}

func TestAgentRunFailure(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(*agent.Config)
		listen func(protocol.Event) error
		want   []string
		text   string
	}{
		{
			name: "PrepareRequest error",
			edit: func(c *agent.Config) {
				c.Hooks.PrepareRequest = func(context.Context, pipeline.Request) (*pipeline.RequestUpdate, error) {
					return nil, errors.New("prepare failed")
				}
			},
			want: append([]string{"agent_start", "turn_start", "message_start(user)", "message_end(user)"}, failureTail()...),
			text: "prepare failed",
		},
		{
			name: "PrepareRequest panic",
			edit: func(c *agent.Config) {
				c.Hooks.PrepareRequest = func(context.Context, pipeline.Request) (*pipeline.RequestUpdate, error) {
					panic("prepare exploded")
				}
			},
			want: append([]string{"agent_start", "turn_start", "message_start(user)", "message_end(user)"}, failureTail()...),
			text: "panic: prepare exploded",
		},
		{
			name: "stream setup error",
			edit: func(c *agent.Config) {
				c.Hooks.GetAPIKey = func(context.Context, string) (string, error) {
					return "", errors.New("no api key for faux")
				}
			},
			want: append([]string{"agent_start", "turn_start", "message_start(user)", "message_end(user)"}, failureTail()...),
			text: "no api key for faux",
		},
		{
			name: "stream function panic",
			edit: func(c *agent.Config) {
				c.Stream = func(context.Context, providers.Model, providers.TranscriptRequest, providers.StreamOptions) *providers.Stream {
					panic(errors.New("dial refused"))
				}
			},
			want: append([]string{"agent_start", "turn_start", "message_start(user)", "message_end(user)"}, failureTail()...),
			text: "panic: dial refused",
		},
		{
			name: "listener panic in a tool batch",
			listen: func(ev protocol.Event) error {
				if _, ok := ev.(*protocol.ToolExecutionEnd); ok {
					panic("listener exploded")
				}
				return nil
			},
			want: append([]string{
				"agent_start", "turn_start", "message_start(user)", "message_end(user)",
				"message_start(assistant)",
				"message_update(toolcall_start)", "message_update(toolcall_delta)", "message_update(toolcall_end)",
				"message_end(assistant)",
				"tool_execution_start(c1)", "tool_execution_end(c1)",
			}, failureTail()...),
			text: "panic: listener exploded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
			a := newAgent(t, p, m, tc.edit)
			rec := &recorder{}
			a.Subscribe(rec.emit)
			if tc.listen != nil {
				a.Subscribe(tc.listen)
			}

			err := a.Prompt(context.Background(), user("hi"))
			require.EqualError(t, err, tc.text)

			assert.Equal(t, tc.want, rec.eventLabels())
			end := rec.events[len(rec.events)-2].(*protocol.AgentEnd)
			require.Len(t, end.Messages, 1)
			msg := end.Messages[0].(protocol.AssistantMessage)
			assert.Equal(t, protocol.StopError, msg.StopReason)
			assert.Equal(t, tc.text, errorText(msg))
			st := a.State()
			assert.Equal(t, agent.Idle, st.Status)
			assert.Equal(t, msg, st.Messages[len(st.Messages)-1])
		})
	}
}

func TestAgentContextSource(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("first"), faux.Say(strings.Repeat("second ", 20)))
	log := &sessions.MemoryLog{}
	var seen [][]string
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.SystemPrompt = "be brief"
		c.NewContext = func() agent.ContextSource { return log }
		c.Hooks.PrepareRequest = func(_ context.Context, r pipeline.Request) (*pipeline.RequestUpdate, error) {
			seen = append(seen, roles(r.Context.Messages))
			return nil, nil
		}
	})
	var during [][]string
	a.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.MessageUpdate); ok {
			during = append(during, roles(a.State().Messages))
		}
		return nil
	})

	require.NoError(t, a.Prompt(context.Background(), user("one")))
	during = nil
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	reqs := p.Requests()
	require.Len(t, reqs, 2)
	second := reqs[1].Transcript.Messages
	assert.Equal(t, []string{"system", "user", "assistant", "user"}, roles(second))
	assert.Equal(t, "first", assistantText(second[2].(protocol.AssistantMessage)))
	assert.Equal(t, [][]string{{"system", "user"}, {"system", "user", "assistant", "user"}}, seen,
		"the user hook runs after the projection")

	require.NotEmpty(t, during)
	for _, r := range during {
		assert.Equal(t, []string{"user", "assistant", "user"}, r, "the partial message never enters the log")
	}
	assert.Equal(t, []string{"user", "assistant", "user", "assistant"}, roles(log.Messages()))
}

func TestAgentContextSourceUserUpdateWins(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Hooks.PrepareRequest = func(_ context.Context, r pipeline.Request) (*pipeline.RequestUpdate, error) {
			r.Context.Messages = append(r.Context.Messages, user("injected"))
			return &pipeline.RequestUpdate{Context: &r.Context}, nil
		}
	})
	require.NoError(t, a.Prompt(context.Background(), user("hi")))
	assert.Equal(t, []string{"system", "user", "user"}, roles(p.Requests()[0].Transcript.Messages))
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages), "the injection is not logged")
}

func TestAgentSystemMessage(t *testing.T) {
	t.Run("prompt and tools", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("ok"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.SystemPrompt = "be brief" })
		require.NoError(t, a.Prompt(context.Background(), user("hi")))
		assert.Equal(t, protocol.SystemMessage{
			Content:    []protocol.Text{{Text: "be brief"}},
			ToolsAdded: []protocol.ToolDecl{tools.Echo{}.Decl()},
			Timestamp:  0,
		}, p.Requests()[0].Transcript.Messages[0])
		assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages), "the system message is not logged")
	})
	t.Run("neither", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("ok"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = nil })
		require.NoError(t, a.Prompt(context.Background(), user("hi")))
		assert.Equal(t, []string{"user"}, roles(p.Requests()[0].Transcript.Messages))
	})
}

func TestAgentContinue(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("answer"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.NewContext = func() agent.ContextSource { return log }
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.ErrorIs(t, a.Continue(context.Background()), agent.ErrContinueEmpty)
	assert.Empty(t, rec.eventLabels())

	require.NoError(t, log.Append(user("pending")))
	require.NoError(t, a.Continue(context.Background()))
	assert.Equal(t, []string{
		"agent_start", "turn_start",
		"message_start(assistant)",
		"message_update(text_start)", "message_update(text_delta)", "message_update(text_end)",
		"message_end(assistant)",
		"turn_end", "agent_end", "agent_settled",
	}, rec.eventLabels())

	require.ErrorIs(t, a.Continue(context.Background()), agent.ErrContinueFromAssistant)
	assert.Len(t, rec.eventLabels(), 10)
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
}

func TestAgentAbortStalledListener(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(faux.Say(strings.Repeat("slow words ", 200)).Pace(40))
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	var once sync.Once
	a.Subscribe(func(ev protocol.Event) error {
		if label(ev) == "message_update(text_delta)" {
			once.Do(func() {
				close(streaming)
				time.Sleep(200 * time.Millisecond)
			})
		}
		return nil
	})

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))

	start := time.Now()
	go a.Abort()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, a.WaitForIdle(ctx))
	t.Logf("idle %s after Abort", time.Since(start))
	require.NoError(t, <-done)

	st := a.State()
	assert.Equal(t, agent.Idle, st.Status)
	assert.Equal(t, protocol.StopAborted, lastAssistant(t, st.Messages).StopReason)
}

func TestAgentIdleCalls(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	a.Abort()
	require.NoError(t, a.WaitForIdle(context.Background()))
	require.NoError(t, a.Reset())
	st := a.State()
	assert.Equal(t, agent.Idle, st.Status)
	assert.Empty(t, st.Messages)

	_, err := agent.New(agent.Config{})
	require.ErrorIs(t, err, agent.ErrNoStream)
}

func TestAgentWaitForIdleContext(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(faux.Say(strings.Repeat("slow words ", 50)).Pace(200))
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, a.WaitForIdle(ctx), context.DeadlineExceeded)
	a.Abort()
	require.NoError(t, <-done)
}

func BenchmarkAgentPrompt(b *testing.B) {
	p, m := newFaux(b)
	a := newAgent(b, p, m, func(c *agent.Config) {
		c.Hooks.FinishTurn = func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			return pipeline.End, nil
		}
	})
	a.Subscribe(func(protocol.Event) error { return nil })
	prompt := user("go")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		p.Set(faux.Reply(faux.Text("calling"), faux.ToolCall("echo", map[string]any{"text": "hi"})))
		if err := a.Prompt(context.Background(), prompt); err != nil {
			b.Fatal(err)
		}
		if err := a.Reset(); err != nil {
			b.Fatal(err)
		}
	}
}
