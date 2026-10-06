package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

type nextAdmit = pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]

// admitWith returns a registry with one admission handler.
func admitWith(h func(context.Context, pipeline.AdmitInput, nextAdmit) (pipeline.AdmitDecision, error)) *pipeline.Registry {
	reg := pipeline.NewRegistry()
	reg.OnAdmitStep(h)
	return reg
}

func rejectAll(context.Context, pipeline.AdmitInput, nextAdmit) (pipeline.AdmitDecision, error) {
	return pipeline.AdmitDecision{Reject: true}, nil
}

func TestRejectedInputClosesCycleWithNoTurn(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = admitWith(rejectAll) })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, []string{"agent_start", "cycle_start", "cycle_end", "agent_end", "agent_settled"}, rec.eventLabels())
	assert.Equal(t, []string{"blocked"}, cycleReasons(rec))
	assert.Zero(t, p.Calls(), "no request")
	assert.Empty(t, userTexts(a.State().Messages), "the rejected input was not committed")
}

func TestEmptyAdmittedInputClosesCycleWithNoTurn(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	empty := admitWith(func(context.Context, pipeline.AdmitInput, nextAdmit) (pipeline.AdmitDecision, error) {
		return pipeline.AdmitDecision{}, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = empty })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, []string{"completed"}, cycleReasons(rec))
	assert.NotContains(t, rec.eventLabels(), "turn_start")
	assert.Zero(t, p.Calls())
}

func TestRejectedInputIsAcknowledgedOnce(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("fine"))
	var rejected sync.Once
	hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
		reject := false
		rejected.Do(func() { reject = true })
		if reject {
			return pipeline.AdmitDecision{Reject: true}, nil
		}
		return next(ctx, in)
	})
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Pipeline = hooks
		c.NewContext = func() sessions.Writer { return log }
	})

	require.NoError(t, a.Prompt(context.Background(), user("a"), user("b")))
	outcomes := logged[sessions.InputOutcome](log)
	require.Len(t, outcomes, 2, "one acknowledgment for each claimed message")
	assert.NotEqual(t, outcomes[0].InputID, outcomes[1].InputID)
	for _, o := range outcomes {
		assert.False(t, o.Accepted)
		assert.NotEmpty(t, o.Reason)
	}

	require.NoError(t, a.Prompt(context.Background(), user("c")))

	outcomes = logged[sessions.InputOutcome](log)
	require.Len(t, outcomes, 3, "the rejected messages are not claimed again")
	assert.True(t, outcomes[2].Accepted, "the next prompt is acknowledged as accepted")
	assert.Equal(t, []string{"c"}, requestUsers(p, 0))
}

func TestAdmissionErrorEndsRunWithError(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	boom := errors.New("admission failed")
	hooks := admitWith(func(context.Context, pipeline.AdmitInput, nextAdmit) (pipeline.AdmitDecision, error) {
		return pipeline.AdmitDecision{}, boom
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	err := a.Prompt(context.Background(), user("go"))

	require.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"error"}, cycleReasons(rec))
	assert.Zero(t, p.Calls())
	assert.Equal(t, agent.Idle, a.State().Status)
	last := lastAssistant(t, a.State().Messages)
	assert.Equal(t, protocol.StopError, last.StopReason)
}

func TestAdmissionErrorKeepsQueuedSteering(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("unused"))
	boom := errors.New("admission failed")
	var calls int
	var mu sync.Mutex
	var a *agent.Agent
	hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 2 {
			// The second run claims "old" with its prompt, and the handler
			// queues one more message after the claim.
			mustFollowUp(t, a, "still queued")
			return pipeline.AdmitDecision{}, boom
		}
		return next(ctx, in)
	})
	a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	mustSteer(t, a, "old")
	a.Abort(agent.KeepQueued)
	require.NoError(t, <-done)
	idle(t, a)

	require.ErrorIs(t, a.Prompt(context.Background(), user("p")), boom, "the claimed batch holds p and old")
	p.Set(faux.Say("ok"), faux.Say("ok2"))
	require.NoError(t, a.Prompt(context.Background(), user("q")))
	idle(t, a)

	for i := range p.Requests() {
		assert.NotContains(t, requestUsers(p, i), "old", "the failed admission consumed the claimed message")
	}
	last := requestUsers(p, len(p.Requests())-1)
	assert.Contains(t, last, "still queued", "the message queued after the claim was kept and ran")
}

func TestAdmissionFailureKeepsSteeringUntilNextSend(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("reply"))
	boom := errors.New("admission failed")
	var once sync.Once
	var a *agent.Agent
	hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
		fail := false
		once.Do(func() { fail = true })
		if fail {
			mustSteer(t, a, "kept")
			return pipeline.AdmitDecision{}, boom
		}
		return next(ctx, in)
	})
	a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.ErrorIs(t, a.Prompt(context.Background(), user("first")), boom)
	idle(t, a)
	assert.Equal(t, 1, countOf(rec.eventLabels(), "cycle_start"), "the queued message started no cycle")
	assert.Zero(t, p.Calls())

	mustFollowUp(t, a, "next")
	idle(t, a)

	require.Equal(t, 1, p.Calls())
	assert.Equal(t, []string{"kept", "next"}, requestUsers(p, 0), "the next cycle carries the kept message")
}

func TestAdmissionHookFailureEndsCycleKeepsQueue(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	boom := errors.New("admission failed")
	var once sync.Once
	var a *agent.Agent
	hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
		fail := false
		once.Do(func() { fail = true })
		if fail {
			mustFollowUp(t, a, "adjacent")
			return pipeline.AdmitDecision{}, boom
		}
		return next(ctx, in)
	})
	a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.ErrorIs(t, a.Prompt(context.Background(), user("first")), boom)
	idle(t, a)
	assert.Equal(t, []string{"error"}, cycleEnds(rec))
	assert.Zero(t, p.Calls(), "no model call")
	states := queueStates(rec)
	assert.Equal(t, "|adjacent", states[len(states)-1], "the adjacent follow-up is still queued")

	require.NoError(t, a.Prompt(context.Background(), user("next")))
	idle(t, a)

	assert.Equal(t, 2, p.Calls(), "the next prompt completed and the kept follow-up ran after it")
	assert.Equal(t, []string{"error", "completed", "completed"}, cycleEnds(rec))
}

func TestOuterAdmissionHandlerKeepsDownstreamRejection(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	hooks := pipeline.NewRegistry()
	var outerSaw pipeline.AdmitDecision
	hooks.OnAdmitStep(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
		d, err := next(ctx, in)
		outerSaw = d
		return d, err
	})
	hooks.OnAdmitStep(rejectAll)
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.True(t, outerSaw.Reject)
	assert.Equal(t, []string{"blocked"}, cycleReasons(rec))
	assert.Zero(t, p.Calls())
}

func TestAdmissionPointRunsBeforeEachTurn(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	rec := &recorder{}
	type call struct {
		cycleID    string
		turn       int
		messages   int
		turnStarts int
		live       bool
	}
	var mu sync.Mutex
	var calls []call
	hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
		mu.Lock()
		calls = append(calls, call{in.CycleID, in.Turn, len(in.Messages), countOf(rec.eventLabels(), "turn_start"), ctx.Err() == nil})
		mu.Unlock()
		return next(ctx, in)
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	cycleID := eventsOf[*protocol.CycleStart](rec)[0].CycleID
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []call{
		{cycleID, 1, 1, 0, true},
		{cycleID, 2, 0, 1, true},
	}, calls, "each call comes before the turn_start of its turn")
}

func TestInputDuringAdmissionWaitsForNextTurn(t *testing.T) {
	t.Run("batch enters", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("two"))
		var once sync.Once
		var a *agent.Agent
		hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
			once.Do(func() { mustSteer(t, a, "late") })
			return next(ctx, in)
		})
		a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })

		require.NoError(t, a.Prompt(context.Background(), user("go")))

		require.Equal(t, 2, p.Calls())
		assert.Equal(t, []string{"go"}, requestUsers(p, 0), "the late message is not in the claimed batch")
		assert.Equal(t, []string{"go", "late"}, requestUsers(p, 1))
	})
	t.Run("batch rejected", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"))
		var once sync.Once
		var a *agent.Agent
		hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
			reject := false
			once.Do(func() { reject = true })
			if reject {
				mustSteer(t, a, "late")
				return pipeline.AdmitDecision{Reject: true}, nil
			}
			return next(ctx, in)
		})
		a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
		rec := &recorder{}
		a.Subscribe(rec.emit)

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		idle(t, a)
		assert.Equal(t, []string{"blocked"}, cycleEnds(rec), "the queued message starts no cycle")
		assert.Zero(t, p.Calls())

		mustFollowUp(t, a, "next")
		idle(t, a)

		require.Equal(t, 1, p.Calls())
		assert.Equal(t, []string{"late", "next"}, requestUsers(p, 0), "the kept message enters the first request of the next cycle")
	})
}
