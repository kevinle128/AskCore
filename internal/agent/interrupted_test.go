package agent_test

import (
	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func interruptedCase(t *testing.T, body func(*providers.Assembler), abortEvent string, configure ...func(*agent.Config)) (*agent.Agent, *faux.Provider, *sessions.MemoryLog, *recorder) {
	t.Helper()
	p, m := newFaux(t)
	p.Set(faux.Say("next"))
	log := &sessions.MemoryLog{}
	first := true
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Stream = func(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
			if !first {
				return p.Stream(ctx, m, req, opts)
			}
			first = false
			return providers.NewStream(ctx, 0, protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID}, body)
		}
		for _, edit := range configure {
			edit(c)
		}
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	remove := a.Subscribe(func(e protocol.Event) error {
		if update, ok := e.(*protocol.MessageUpdate); ok && update.AssistantMessageEvent.EventType() == abortEvent {
			a.Abort()
		}
		if abortEvent == "attempt_start" {
			if _, ok := e.(*protocol.AttemptStart); ok {
				a.Abort()
			}
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	remove()
	return a, p, log, rec
}
func TestCancelMidStreamKeepsTextAndDropsUnfinishedToolCall(t *testing.T) {
	a, _, _, _ := interruptedCase(t, func(s *providers.Assembler) {
		s.Start()
		i := s.TextStart("")
		s.TextDelta(i, "reading the file")
		s.TextEnd(i, "reading the file", nil)
		j := s.ToolStart("c1", "echo", nil, nil, nil)
		s.ToolDelta(j, `{"text":`)
		<-s.Context().Done()
	}, "toolcall_delta")
	msg := lastAssistant(t, a.State().Messages)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "reading the file", assistantText(msg))
}
func TestInterruptedMessageIsSentToModelOnNextRequest(t *testing.T) {
	a, p, _, _ := interruptedCase(t, func(s *providers.Assembler) {
		s.Start()
		i := s.TextStart("")
		s.TextDelta(i, "reading the file")
		<-s.Context().Done()
	}, "text_delta")
	require.NoError(t, a.Prompt(context.Background(), user("continue")))
	req := p.Requests()[0].Transcript.Messages
	var found bool
	for _, m := range req {
		if msg, ok := m.(protocol.AssistantMessage); ok && assistantText(msg) == "reading the file" {
			found = true
		}
	}
	assert.True(t, found)
}
func TestCancelDuringReasoningKeepsThinkingBlockOnly(t *testing.T) {
	a, _, _, _ := interruptedCase(t, func(s *providers.Assembler) {
		s.Start()
		i := s.ThinkingStart("", nil, nil)
		s.ThinkingDelta(i, "reasoning prefix")
		<-s.Context().Done()
	}, "thinking_delta")
	msg := lastAssistant(t, a.State().Messages)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	require.Len(t, msg.Content, 1)
	b, ok := msg.Content[0].(protocol.Thinking)
	require.True(t, ok)
	assert.Equal(t, "reasoning prefix", b.Thinking)
}
func TestCancelBeforeAnyContentCommitsAbortedMessage(t *testing.T) {
	a, p, _, rec := interruptedCase(t, func(s *providers.Assembler) { <-s.Context().Done() }, "attempt_start", func(c *agent.Config) {
		model := c.Model
		c.ConvertToLLM = func(messages []protocol.Message) ([]protocol.Message, error) {
			return providers.TransformMessages(providers.ConvertToLLM(messages), model, nil, nil), nil
		}
	})
	msg := lastAssistant(t, a.State().Messages)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	assert.Empty(t, assistantText(msg))
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
	var wire []protocol.AssistantMessage
	for _, end := range eventsOf[*protocol.MessageEnd](rec) {
		if message, ok := end.Message.(protocol.AssistantMessage); ok {
			wire = append(wire, message)
		}
	}
	require.Len(t, wire, 1)
	var starts []protocol.AssistantMessage
	for _, start := range eventsOf[*protocol.MessageStart](rec) {
		if message, ok := start.Message.(protocol.AssistantMessage); ok {
			starts = append(starts, message)
		}
	}
	require.Len(t, starts, 1)
	assert.Equal(t, protocol.StopAborted, starts[0].StopReason)
	assert.Empty(t, assistantText(starts[0]))
	assert.Equal(t, protocol.StopAborted, wire[0].StopReason)
	assert.Empty(t, assistantText(wire[0]))
	require.NoError(t, a.Prompt(context.Background(), user("next")))
	for _, message := range p.Requests()[0].Transcript.Messages {
		_, ok := message.(protocol.AssistantMessage)
		assert.False(t, ok, "empty aborted wrapper must not replay")
	}
}

func TestInterruptedMessageDropsWhitespaceOnlyBlocks(t *testing.T) {
	a, _, _, _ := interruptedCase(t, func(s *providers.Assembler) {
		s.Start()
		i := s.TextStart("answer")
		s.TextEnd(i, "answer", nil)
		j := s.TextStart("")
		s.TextDelta(j, " \n\t")
		<-s.Context().Done()
	}, "text_delta")
	msg := lastAssistant(t, a.State().Messages)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "answer", assistantText(msg))
}
func TestAbortedAttemptKeepsObservedUsage(t *testing.T) {
	u := protocol.Usage{Input: 12, Output: 4, TotalTokens: 16}
	_, _, log, _ := interruptedCase(t, func(s *providers.Assembler) {
		s.Start()
		s.SetUsage(u)
		i := s.TextStart("")
		s.TextDelta(i, "partial")
		<-s.Context().Done()
	}, "text_delta")
	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 1)
	require.NotNil(t, settled[0].Usage)
	assert.Equal(t, u, *settled[0].Usage)
	assert.Equal(t, "aborted", settled[0].Outcome)
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hello"})).WithUsage(u), faux.Say("done"))
	toolLog := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(toolLog), func(c *agent.Config) { c.Tools = registry(t, tools.Echo{}) }))
	var firstTurn *sessions.AttemptSettled
	a.Subscribe(func(e protocol.Event) error {
		if turn, ok := e.(*protocol.TurnEnd); ok && firstTurn == nil {
			message, ok := turn.Message.(protocol.AssistantMessage)
			require.True(t, ok)
			assert.Equal(t, protocol.StopToolUse, message.StopReason)
			require.Len(t, turn.ToolResults, 1)
			entries := logged[sessions.AttemptSettled](toolLog)
			require.Len(t, entries, 1)
			value := entries[0]
			firstTurn = &value
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("tool")))
	require.NotNil(t, firstTurn)
	assert.Equal(t, "completed", firstTurn.Outcome)
	require.NotNil(t, firstTurn.Usage)
	assert.Equal(t, u, *firstTurn.Usage)
}

func TestAbortAfterDispatchEndsCycleAbortedWithUserCause(t *testing.T) {
	_, _, _, rec := interruptedCase(t, func(s *providers.Assembler) {
		s.Start()
		i := s.TextStart("")
		s.TextDelta(i, "partial")
		<-s.Context().Done()
	}, "text_delta")
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "aborted", ends[0].Reason)
	assert.Equal(t, "user", ends[0].Cause)
}
func TestAbortOnFinalChunkKeepsReplayState(t *testing.T) {
	sig := "signature"
	a, p, _, _ := interruptedCase(t, func(s *providers.Assembler) {
		s.Start()
		i := s.ThinkingStart("reasoning", nil, nil)
		s.ThinkingEnd(i, "reasoning", &sig, nil)
		s.Done(protocol.StopStop)
	}, "thinking_end")
	msg := lastAssistant(t, a.State().Messages)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, &sig, msg.Content[0].(protocol.Thinking).ThinkingSignature)
	require.NoError(t, a.Prompt(context.Background(), user("next")))
	req := p.Requests()[0].Transcript.Messages
	var found bool
	for _, m := range req {
		if msg, ok := m.(protocol.AssistantMessage); ok {
			for _, b := range msg.Content {
				if think, ok := b.(protocol.Thinking); ok && think.ThinkingSignature != nil {
					assert.Equal(t, sig, *think.ThinkingSignature)
					found = true
				}
			}
		}
	}
	assert.True(t, found)
}
