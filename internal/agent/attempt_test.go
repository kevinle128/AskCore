package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareFailureCommitsNothing(t *testing.T) {
	for _, panicCall := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicCall], func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Say("ok"))
			log := &sessions.MemoryLog{}
			reg := pipeline.NewRegistry()
			remove := reg.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				if panicCall {
					panic("prepare failed")
				}
				return nil, errors.New("prepare failed")
			})
			a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.Pipeline = reg }))
			require.Error(t, a.Prompt(context.Background(), user("bad")))
			assert.Empty(t, a.State().Messages)
			assert.Empty(t, logged[sessions.RequestDelta](log))
			assert.Zero(t, p.Calls())
			remove()
			require.NoError(t, a.Prompt(context.Background(), user("good")))
			assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
			assert.Equal(t, "good", resultText(p.Requests()[0].Transcript.Messages[len(p.Requests()[0].Transcript.Messages)-1].(protocol.UserMessage).Content))
		})
	}
}

func TestModelMiddlewareTimeoutCoversStreamConsumption(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("slow answer with several chunks").Delay(time.Second))
	reg := pipeline.NewRegistry()
	var stopped bool
	reg.OnExecuteModel(func(ctx context.Context, c pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Millisecond)
		defer cancel()
		s, err := next(ctx, c)
		stopped = ctx.Err() != nil
		return s, err
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = reg })
	require.ErrorIs(t, a.Prompt(context.Background(), user("go")), context.DeadlineExceeded)
	assert.True(t, stopped)
	assert.Equal(t, protocol.StopError, lastAssistant(t, a.State().Messages).StopReason)
}

func TestAbortDuringRequestPreparationSendsNoRequest(t *testing.T) {
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	var a *agent.Agent
	a = newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		registryOf(c).OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			a.Abort()
			return nil, nil
		})
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Zero(t, p.Calls())
	assert.Empty(t, a.State().Messages)
	assert.Empty(t, logged[sessions.RequestDelta](log))
	assert.Empty(t, eventsOf[*protocol.AttemptStart](rec))
	assert.Equal(t, "aborted", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
	assert.Equal(t, "user", eventsOf[*protocol.CycleEnd](rec)[0].Cause)
}
func TestAbortBeforeRequestSendsNoRequest(t *testing.T) {
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	var a *agent.Agent
	a = newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		registryOf(c).OnAdmitStep(func(ctx context.Context, in pipeline.AdmitInput, next pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
			a.Abort()
			return next(ctx, in)
		})
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Zero(t, p.Calls())
	assert.Empty(t, a.State().Messages)
	assert.Empty(t, eventsOf[*protocol.AttemptStart](rec))
	assert.Equal(t, "aborted", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
}
func TestDisposeDuringRequestPreparationSendsNoRequest(t *testing.T) {
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	entered := make(chan struct{})
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		registryOf(c).OnPrepareRequest(func(ctx context.Context, _ pipeline.Request, _ pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	}))
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-entered
	require.NoError(t, a.Dispose())
	require.NoError(t, <-done)
	assert.Zero(t, p.Calls())
	assert.Empty(t, logged[sessions.RequestDelta](log))
	assert.Empty(t, log.Messages())
}
func TestPromptAfterRequestPreparationErrorRunsNormally(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("normal"))
	fail := true
	a := newAgent(t, p, m, func(c *agent.Config) {
		registryOf(c).OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			if fail {
				return nil, errors.New("prepare failed")
			}
			return next(ctx, in)
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.Error(t, a.Prompt(context.Background(), user("bad")))
	assert.Empty(t, a.State().Messages)
	assert.Zero(t, p.Calls())
	assert.Equal(t, "error", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
	fail = false
	require.NoError(t, a.Prompt(context.Background(), user("good")))
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
	assert.Equal(t, "normal", assistantText(lastAssistant(t, a.State().Messages)))
}
func TestUnknownProviderIsNotRetriedAndEndsWithErrorMessage(t *testing.T) {
	reg := providers.NewRegistry()
	m := providers.Model{Provider: "unknown-provider", API: "unknown-api", ID: "unknown-model"}
	a, err := agent.New(agent.Config{Registry: reg, LoopConfig: agent.LoopConfig{Model: m}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, a.Dispose()) })
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Empty(t, eventsOf[*protocol.AutoRetryStart](rec))
	msg := lastAssistant(t, a.State().Messages)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Contains(t, errorText(msg), "unknown-api")
	assert.Contains(t, errorText(msg), "unknown-provider")
	assert.Equal(t, "error", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
}
func TestExecuteModelMayReturnSettledOutcomeWithoutDispatch(t *testing.T) {
	p, m := newFaux(t)
	reg := pipeline.NewRegistry()
	reg.OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, _ pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		return &pipeline.ModelOutcome{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: "provided"}}, StopReason: protocol.StopStop}}, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = reg })
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Zero(t, p.Calls())
	assert.Empty(t, eventsOf[*protocol.AttemptStart](rec))
	assert.Equal(t, "provided", assistantText(lastAssistant(t, a.State().Messages)))
	assert.Len(t, eventsOf[*protocol.MessageStart](rec), 2)
}
func TestExecuteModelMayReplaceSettledOutcome(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("original"))
	reg := pipeline.NewRegistry()
	reg.OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		out, err := next(ctx, in)
		if err != nil {
			return nil, err
		}
		out.Message.Content = []protocol.AssistantBlock{protocol.Text{Text: "replacement"}}
		return out, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = reg })
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, p.Calls())
	assert.Equal(t, "replacement", assistantText(lastAssistant(t, a.State().Messages)))
}
