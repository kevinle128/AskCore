package agent_test

import (
	"context"
	"slices"
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
			pipeline.AgentContext{}, config(p, m, nil), rec.emit)
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
		pipeline.AgentContext{}, config(p, m, nil), rec.emit)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"agent_start", "cycle_start", "turn_start", "message_start(user)", "message_end(user)",
		"attempt_start", "message_start(assistant)", "message_end(assistant)", "attempt_end",
		"turn_end", "cycle_end", "agent_end",
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
		msgs, err = agent.Run(ctx, []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, nil), emit)
	})
	require.NoError(t, err)
	last := lastAssistant(t, msgs)
	assert.Equal(t, protocol.StopAborted, last.StopReason)
	labels := rec.eventLabels()
	assert.Equal(t, []string{"message_end(assistant)", "attempt_end", "turn_end", "cycle_end", "agent_end"}, labels[len(labels)-5:])
	assert.Equal(t, 1, strings.Count(strings.Join(labels, " "), "message_start(assistant)"))
}

func TestStreamThinkingLevelStamp(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("a"), faux.Say("b"))
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{}, config(p, m, nil), (&recorder{}).emit)
	require.NoError(t, err)
	level := lastAssistant(t, msgs).ThinkingLevel
	require.NotNil(t, level)
	assert.Equal(t, protocol.ThinkingOff, *level)

	cfg := config(p, m, nil)
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
	reg := pipeline.NewRegistry()
	reg.OnPrepareRequest(func(ctx context.Context, r pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		rec.note("transform")
		upd, err := next(ctx, r)
		if err != nil {
			return nil, err
		}
		if upd == nil {
			upd = &pipeline.RequestUpdate{}
		}
		upd.RequestMessages = append(slices.Clip(r.Context.Messages), user("injected"))
		return upd, nil
	})
	cfg := config(p, m, reg)
	cfg.ConvertToLLM = func(msgs []protocol.Message) ([]protocol.Message, error) {
		rec.note("convert")
		assert.Len(t, msgs, 2)
		return msgs, nil
	}
	cfg.GetAPIKey = func(context.Context, string) (string, error) { rec.note("key"); return "", nil }
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, cfg, rec.emit)
	require.NoError(t, err)
	labels := rec.labels()
	assert.Equal(t, []string{"transform", "convert", "message_start(user)", "message_end(user)", "key", "attempt_start", "message_start(assistant)"}, labels[3:10])
	assert.Equal(t, []string{"user", "user"}, roles(p.Requests()[0].Transcript.Messages))
	assert.Equal(t, []string{"user", "assistant"}, roles(msgs), "the transform changes only the request")
}

// A second next inside ExecuteModel is rejected and sends no second request.
func TestNextCalledTwiceRunsTerminalOnce(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("a"), faux.Say("unused"))
	var second error
	reg := pipeline.NewRegistry()
	reg.OnExecuteModel(func(ctx context.Context, call pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		first, err := next(ctx, call)
		if err != nil {
			return nil, err
		}
		_, second = next(ctx, call)
		return first, nil
	})
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, reg), (&recorder{}).emit)
	require.NoError(t, err)
	assert.ErrorIs(t, second, pipeline.ErrNextReused)
	assert.Equal(t, 1, p.Calls(), "the provider got one request")
	assert.Equal(t, []string{"user", "assistant"}, roles(msgs))
}

func TestExecuteModelHandlerThatDropsTheStreamFailsTheRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("a"))
	reg := pipeline.NewRegistry()
	reg.OnExecuteModel(func(ctx context.Context, call pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		_, err := next(ctx, call)
		return nil, err
	})
	var msgs []protocol.Message
	var err error
	require.NotPanics(t, func() {
		msgs, err = agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, reg), (&recorder{}).emit)
	})
	require.ErrorIs(t, err, pipeline.ErrInvalidResult)
	assert.Nil(t, msgs)
}

func TestExecuteModelHandlerCannotDetachStreamFromAbort(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say(strings.Repeat("slow words ", 50)).Pace(200))
	reg := pipeline.NewRegistry()
	reg.OnExecuteModel(func(ctx context.Context, call pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		return next(context.WithoutCancel(ctx), call)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	rec := &recorder{}
	var msgs []protocol.Message
	var err error
	within(t, 5*time.Second, func() {
		msgs, err = agent.Run(ctx, []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, reg), rec.emit)
	})
	require.NoError(t, err)
	assert.Equal(t, protocol.StopAborted, lastAssistant(t, msgs).StopReason)
}

func TestPrepareRequestMessagesNilKeepsAndEmptySendsNothing(t *testing.T) {
	run := func(msgs []protocol.Message) []string {
		p, m := newFaux(t)
		p.Set(faux.Say("a"))
		reg := pipeline.NewRegistry()
		reg.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			return &pipeline.RequestUpdate{RequestMessages: msgs}, nil
		})
		_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, reg), (&recorder{}).emit)
		require.NoError(t, err)
		return roles(p.Requests()[0].Transcript.Messages)
	}
	assert.Equal(t, []string{"user"}, run(nil), "nil keeps the context messages")
	assert.Empty(t, run([]protocol.Message{}), "an empty slice sends an empty request")
}
