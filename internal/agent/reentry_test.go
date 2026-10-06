package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func TestResetFromListenerDuringRunReturnsBusy(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hi"))
	a := newAgent(t, p, m, nil)
	var resetErr error
	a.Subscribe(onEvent("turn_start", 1, func() { resetErr = a.Reset() }))

	within(t, 5*time.Second, func() { require.NoError(t, a.Prompt(context.Background(), user("go"))) })

	require.ErrorIs(t, resetErr, agent.ErrBusy)
	assert.Equal(t, []string{"go"}, userTexts(a.State().Messages), "the conversation is kept")
}

func TestPromptFromQueueUpdateListenerWhileIdleDoesNotDeadlock(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	id := keepOneQueued(t, a)
	called := make(chan struct{})
	var promptErr error
	var once sync.Once
	a.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.QueueUpdate); ok {
			once.Do(func() {
				promptErr = a.Prompt(context.Background(), user("nested"))
				close(called)
			})
		}
		return nil
	})

	require.True(t, a.Remove(id))

	require.NoError(t, waitOrFail(context.Background(), called, "the listener"))
	require.ErrorIs(t, promptErr, agent.ErrBusy, "a listener cannot start a run")
	idle(t, a)
}

// keepOneQueued leaves one follow-up message queued in an idle Agent and
// returns its ID. It runs a prompt that the test aborts with KeepQueued.
func keepOneQueued(t *testing.T, a *agent.Agent) string {
	t.Helper()
	return keepQueued(t, a, "kept")[0]
}

// keepQueued leaves one follow-up message for each text queued in an idle
// Agent and returns their IDs.
func keepQueued(t *testing.T, a *agent.Agent, texts ...string) []string {
	t.Helper()
	reached, release := make(chan struct{}), make(chan struct{})
	a.Subscribe(gate("agent_start", reached, release))
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("first")) }()
	require.NoError(t, waitOrFail(context.Background(), reached, "the run"))
	var ids []string
	for _, text := range texts {
		ids = append(ids, mustFollowUp(t, a, text))
	}
	a.Abort(agent.KeepQueued)
	close(release)
	require.NoError(t, <-done)
	idle(t, a)
	return ids
}

func TestAbortWithBlockedListenerReturnsPromptly(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep())
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))
	block, release := make(chan struct{}), make(chan struct{})
	a.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.QueueUpdate); ok {
			select {
			case <-block:
			default:
				close(block)
			}
			<-release
		}
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	mustSteer(t, a, "s")
	require.NoError(t, waitOrFail(context.Background(), block, "the listener"))

	within(t, 2*time.Second, func() {
		a.Abort()
		mustSteer(t, a, "more")
		assert.False(t, a.Remove("none"))
	})
	close(release)
	require.NoError(t, <-done)
	idle(t, a)
}

func TestPromptWhileSlowListenerDeliversNeverHangs(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("done"))
	a := newAgent(t, p, m, nil)
	id := keepOneQueued(t, a)
	reached, release := make(chan struct{}), make(chan struct{})
	a.Subscribe(gate("queue_update", reached, release))
	require.True(t, a.Remove(id))
	require.NoError(t, waitOrFail(context.Background(), reached, "the slow listener"))

	// The documented behavior: a listener is being called, so Prompt returns
	// ErrBusy at once instead of waiting for it. After the delivery it works.
	within(t, 2*time.Second, func() {
		require.ErrorIs(t, a.Prompt(context.Background(), user("other")), agent.ErrBusy)
		require.ErrorIs(t, a.Reset(), agent.ErrBusy)
	})
	close(release)
	idle(t, a)
	waitFor(t, func() bool { return a.Prompt(context.Background(), user("again")) == nil })
}

func TestResetPublishesOneQueueUpdateWhenQueuesCleared(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	keepOneQueued(t, a)
	before := len(queueStates(rec))

	require.NoError(t, a.Reset())
	require.NoError(t, a.Reset(), "an empty reset publishes nothing")
	waitFor(t, func() bool { return len(queueStates(rec)) > before })

	states := queueStates(rec)
	assert.Equal(t, "|", states[len(states)-1])
	assert.Len(t, states, before+1, "exactly one update for the clear")
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestQueueUpdatesStayInOrderWhileListenerHoldsPublisher(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	ids := keepQueued(t, a, "k1", "k2")
	reached, release := make(chan struct{}), make(chan struct{})
	var resetErr error
	var once sync.Once
	a.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.QueueUpdate); ok {
			once.Do(func() {
				close(reached)
				<-release
				resetErr = a.Reset()
			})
		}
		return nil
	})
	require.True(t, a.Remove(ids[0]))
	require.NoError(t, waitOrFail(context.Background(), reached, "the publisher"))
	require.True(t, a.Remove(ids[1])) // posted behind the update that the listener holds
	close(release)

	waitFor(t, func() bool { return len(queueStates(rec)) >= 4 })
	require.ErrorIs(t, resetErr, agent.ErrBusy, "a listener cannot reset the Agent")
	assert.Equal(t, []string{"|k1", "|k1,k2", "|k2", "|"}, queueStates(rec))
}

func TestDisposeBeforeQueuedRunStartsStartsNoCycle(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("unused"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	reached, release := make(chan struct{}), make(chan struct{})
	a.Subscribe(gate("message_update(text_delta)", reached, release))
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), reached, "the stream"))
	a.Abort(agent.KeepQueued)
	mustFollowUp(t, a, "latched")
	disposed := make(chan error, 1)
	go func() { disposed <- a.Dispose() }()
	waitFor(t, func() bool { _, err := a.FollowUp(user("x")); return err != nil })
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, <-disposed)

	assert.Equal(t, 1, countOf(rec.eventLabels(), "cycle_start"), "no cycle after Dispose began")
	assert.Equal(t, 1, p.Calls())
}

func TestDisposeEndsFollowers(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	f := a.Follow(agent.Cursor{})

	require.NoError(t, a.Dispose())

	timeout := time.After(5 * time.Second)
	var last []byte
	for open := true; open; {
		select {
		case it, ok := <-f.Events.Events():
			if !ok {
				open = false
				break
			}
			last = it.Data
		case <-timeout:
			t.Fatal("the follower stayed open after agent_disposed")
		}
	}
	assert.Contains(t, string(last), "agent_disposed")
	assert.NoError(t, f.Events.Err())
}

func TestResetClosesOldLogAndDisposeClosesCurrent(t *testing.T) {
	p, m := newFaux(t)
	var logs []*closingLog
	var mu sync.Mutex
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.NewContext = func() sessions.Writer {
			mu.Lock()
			defer mu.Unlock()
			l := &closingLog{MemoryLog: &sessions.MemoryLog{}}
			logs = append(logs, l)
			return l
		}
	})

	require.NoError(t, a.Reset())
	require.Len(t, logs, 2)
	assert.EqualValues(t, 1, logs[0].closes.Load(), "Reset closed the replaced log")
	assert.EqualValues(t, 0, logs[1].closes.Load())
	require.NoError(t, a.Dispose())
	assert.EqualValues(t, 1, logs[1].closes.Load(), "Dispose closed the current log")
}

func TestEndLeavesQueuedInputQueued(t *testing.T) {
	setup := func(t *testing.T) (*agent.Agent, *faux.Provider, *recorder, string) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("two"), faux.Say("three"))
		var calls int
		hooks := pipeline.NewRegistry()
		hooks.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			calls++
			if calls == 1 {
				return pipeline.End, nil
			}
			return pipeline.Proceed, nil
		})
		a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
		rec := &recorder{}
		a.Subscribe(rec.emit)
		var id string
		a.Subscribe(onEvent("turn_start", 1, func() { id = mustFollowUp(t, a, "queued") }))
		require.NoError(t, a.Prompt(context.Background(), user("go")))
		idle(t, a)
		return a, p, rec, id
	}
	t.Run("stays queued and publishes the update", func(t *testing.T) {
		a, p, rec, id := setup(t)
		assert.Equal(t, 1, p.Calls(), "End starts no new run for the queued input")
		assert.Equal(t, agent.Idle, a.State().Status)
		assert.Equal(t, []string{"|queued"}, queueStates(rec), "clients see it pending")
		assert.True(t, a.Remove(id), "the input is still pending")
	})
	t.Run("the next run delivers it at its cycle boundary", func(t *testing.T) {
		a, p, rec, _ := setup(t)
		require.NoError(t, a.Prompt(context.Background(), user("next")))
		idle(t, a)
		require.Equal(t, 3, p.Calls())
		assert.Equal(t, []string{"go", "next", "queued"}, requestUsers(p, 2))
		assert.Equal(t, []string{"completed", "completed", "completed"}, cycleEnds(rec))
	})
}

func TestAcceptedInputIsRecordedWithItsMessage(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	a.Subscribe(onEvent("turn_start", 1, func() { mustSteer(t, a, "steered") }))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	var withID []sessions.MessageEntry
	for _, e := range logged[sessions.MessageEntry](log) {
		if e.InputID != "" {
			withID = append(withID, e)
		}
	}
	outcomes := logged[sessions.InputOutcome](log)
	require.Len(t, withID, 2, "the prompt and the steering message carry their input IDs")
	require.Len(t, outcomes, 2)
	for i, o := range outcomes {
		assert.True(t, o.Accepted)
		assert.Equal(t, withID[i].InputID, o.InputID, "one acceptance for each committed input")
	}
	assert.NotEqual(t, outcomes[0].InputID, outcomes[1].InputID)
}

func TestInputRewrittenToFewerMessagesIsAcknowledgedAsDropped(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"))
	log := &sessions.MemoryLog{}
	hooks := admitWith(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
		return pipeline.AdmitDecision{Messages: in.Messages[:1]}, nil
	})
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.Pipeline = hooks }))

	require.NoError(t, a.Prompt(context.Background(), user("a"), user("b")))

	outcomes := logged[sessions.InputOutcome](log)
	require.Len(t, outcomes, 2)
	accepted, dropped := 0, 0
	for _, o := range outcomes {
		if o.Accepted {
			accepted++
		} else if o.Reason == "dropped" {
			dropped++
		}
	}
	assert.Equal(t, 1, accepted)
	assert.Equal(t, 1, dropped)
}
