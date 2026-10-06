package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func TestCycleEndCarriesProviderFailureCode(t *testing.T) {
	p, m := newFaux(t)
	p.Set(
		faux.Say("denied").Err(providers.NewFailure(providers.CodeAuth, 401, 0, "invalid credentials", nil)),
		faux.Fail("plain failure without provider facts"),
		faux.Say("fine"),
	)
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log)))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("one")))
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "error", ends[0].Reason)
	assert.Equal(t, providers.CodeAuth, ends[0].Code)
	assert.Equal(t, 1, len(eventsOf[*protocol.TurnEnd](rec)), "one turn_end for the failed turn")

	// The log keeps the same code for the attempt and for the cycle.
	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 1)
	require.NotNil(t, settled[0].Failure)
	assert.Equal(t, providers.CodeAuth, settled[0].Failure.Code)
	closed := logged[sessions.CycleClosed](log)
	require.Len(t, closed, 1)
	assert.Equal(t, providers.CodeAuth, closed[0].Code)

	// An error with no provider fact has the code UNKNOWN.
	require.NoError(t, a.Prompt(context.Background(), user("two")))
	ends = eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 2)
	assert.Equal(t, "error", ends[1].Reason)
	assert.Equal(t, providers.CodeUnknown, ends[1].Code)

	// The next prompt runs a completed cycle, and a completed cycle has no code.
	require.NoError(t, a.Prompt(context.Background(), user("three")))
	ends = eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 3)
	assert.Equal(t, "completed", ends[2].Reason)
	assert.Empty(t, ends[2].Code)
}

func TestCycleEndRendersCodedHookError(t *testing.T) {
	typed := providers.NewFailure(providers.CodeRateLimit, 429, 0, "slow down", nil)
	for _, tc := range []struct {
		name     string
		hook     func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error)
		wantCode string
		wantText string
	}{
		{
			name: "typed error",
			hook: func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				return nil, typed
			},
			wantCode: providers.CodeRateLimit,
			wantText: "slow down",
		},
		{
			name: "wrapped typed error",
			hook: func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				return nil, errors.Join(errors.New("hook failed"), typed)
			},
			wantCode: providers.CodeRateLimit,
			wantText: "hook failed",
		},
		{
			name: "panic with a value that is no error",
			hook: func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				panic(42)
			},
			wantCode: providers.CodeUnknown,
			wantText: "panic: 42",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Say("unused"))
			log := &sessions.MemoryLog{}
			a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
				registryOf(c).OnPrepareRequest(tc.hook)
			}))
			rec := &recorder{}
			a.Subscribe(rec.emit)

			require.Error(t, a.Prompt(context.Background(), user("go")))
			ends := eventsOf[*protocol.CycleEnd](rec)
			require.Len(t, ends, 1)
			assert.Equal(t, "error", ends[0].Reason)
			assert.Equal(t, tc.wantCode, ends[0].Code)

			end := rec.events[len(rec.events)-2].(*protocol.AgentEnd)
			assert.Contains(t, errorText(end.Messages[0].(protocol.AssistantMessage)), tc.wantText)

			closed := logged[sessions.CycleClosed](log)
			require.Len(t, closed, 1)
			assert.Equal(t, tc.wantCode, closed[0].Code)
		})
	}
}

func TestAbortedCycleHasNoFailureCode(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hello"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Subscribe(rec.emit)
	_ = a.Prompt(ctx, user("go"))
	for _, e := range eventsOf[*protocol.CycleEnd](rec) {
		assert.Empty(t, e.Code, "only a cycle that ended in error has a code")
	}
}

func TestPrepareErrorIsRequestPreparationFailure(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Prepare = func(providers.Model, providers.TranscriptRequest, providers.StreamOptions) (*providers.Prepared, error) {
			return nil, providers.NewFailure(providers.CodeInvalidRequest, 0, 0, "limit is not valid", nil)
		}
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.Error(t, a.Prompt(context.Background(), user("go")))
	assert.Empty(t, eventsOf[*protocol.AttemptStart](rec), "no model request started, so no attempt")
	assert.Zero(t, p.Calls(), "the stream was never called")
	assert.Empty(t, logged[sessions.RequestDelta](log), "nothing was sent, so nothing is logged as sent")
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, providers.CodeInvalidRequest, ends[0].Code)
}

func TestRequestLogCapturesPolicyForProviderWithoutPreparer(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hi"))
	reg := providers.NewRegistry()
	reg.Register(m.API, p.Stream)
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.Registry = reg }))
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	deltas := logged[sessions.RequestDelta](log)
	require.Len(t, deltas, 1)
	require.NotNil(t, deltas[0].Prepared)
	assert.Equal(t, providers.DefaultRetryPolicy().Key, deltas[0].Prepared.RetryPolicy.Key)
}
