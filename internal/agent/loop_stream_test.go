package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"
)

func TestStreamTruncatedSettlesIncomplete(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hello").Truncate(2))
	rec := &recorder{}
	var msgs []protocol.Message
	var err error
	within(t, 2*time.Second, func() {
		msgs, err = agent.Run(context.Background(), []protocol.Message{user("go")},
			pipeline.AgentContext{}, config(p, m, pipeline.Hooks{}), rec.emit)
	})
	require.NoError(t, err)
	last := lastAssistant(t, msgs)
	assert.Equal(t, protocol.StopError, last.StopReason)
	require.NotNil(t, last.ErrorMessage)
	assert.Equal(t, providers.ErrStreamIncomplete.Error(), *last.ErrorMessage)
	assert.Equal(t, 1, strings.Count(strings.Join(rec.eventLabels(), " "), "message_start(assistant)"))
	assert.Equal(t, "agent_end", rec.eventLabels()[len(rec.eventLabels())-1])
}

func TestStreamWithoutStartEmitsFinalOnce(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hello").Truncate(0))
	rec := &recorder{}
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{}, config(p, m, pipeline.Hooks{}), rec.emit)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"agent_start", "turn_start", "message_start(user)", "message_end(user)",
		"message_start(assistant)", "message_end(assistant)",
		"turn_end", "agent_end",
	}, rec.eventLabels())
	assert.Equal(t, []string{"user", "assistant"}, roles(msgs))
}

func TestStreamAbortDuringPacedStream(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say(strings.Repeat("slow ", 200)).Pace(100))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recorder{}
	emit := func(e protocol.Event) error {
		if u, ok := e.(*protocol.MessageUpdate); ok && u.AssistantMessageEvent.EventType() == protocol.StreamTypeTextStart {
			cancel()
		}
		return rec.emit(e)
	}
	var msgs []protocol.Message
	var err error
	within(t, 2*time.Second, func() {
		msgs, err = agent.Run(ctx, []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, pipeline.Hooks{}), emit)
	})
	require.NoError(t, err)
	last := lastAssistant(t, msgs)
	assert.Equal(t, protocol.StopAborted, last.StopReason)
	labels := rec.eventLabels()
	assert.Equal(t, []string{"message_end(assistant)", "turn_end", "agent_end"}, labels[len(labels)-3:])
	assert.Equal(t, 1, strings.Count(strings.Join(labels, " "), "message_start(assistant)"))
}

func TestStreamThinkingLevelStamp(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("a"), faux.Say("b"))
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{}, config(p, m, pipeline.Hooks{}), (&recorder{}).emit)
	require.NoError(t, err)
	level := lastAssistant(t, msgs).ThinkingLevel
	require.NotNil(t, level)
	assert.Equal(t, protocol.ThinkingOff, *level)

	cfg := config(p, m, pipeline.Hooks{})
	cfg.Options.Reasoning = protocol.ThinkingLow
	msgs, err = agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, cfg, (&recorder{}).emit)
	require.NoError(t, err)
	level = lastAssistant(t, msgs).ThinkingLevel
	require.NotNil(t, level)
	assert.Equal(t, protocol.ThinkingLow, *level)
}

func TestStreamTransformAndConvertOrder(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("a"))
	rec := &recorder{}
	hooks := pipeline.Hooks{
		TransformContext: func(_ context.Context, msgs []protocol.Message) ([]protocol.Message, error) {
			rec.note("transform")
			return append(msgs, user("injected")), nil
		},
		ConvertToLLM: func(msgs []protocol.Message) ([]protocol.Message, error) {
			rec.note("convert")
			assert.Len(t, msgs, 2)
			return msgs, nil
		},
		GetAPIKey: func(context.Context, string) (string, error) { rec.note("key"); return "", nil },
	}
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, hooks), rec.emit)
	require.NoError(t, err)
	labels := rec.labels()
	assert.Equal(t, []string{"transform", "convert", "key", "message_start(assistant)"}, labels[4:8])
	assert.Equal(t, []string{"user", "user"}, roles(p.Requests()[0].Transcript.Messages))
	assert.Equal(t, []string{"user", "assistant"}, roles(msgs), "the transform changes only the request")
}
