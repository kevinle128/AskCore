package agent_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

func TestIdleChannelFollowsRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"))
		a := newAgent(t, p, m, nil)
		assert.True(t, isClosed(a.Idle()), "a new agent is idle")

		reached, release := make(chan struct{}), make(chan struct{})
		a.Subscribe(gateNth("agent_start", 1, reached, release))
		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		<-reached
		busy := a.Idle()
		assert.False(t, isClosed(busy), "a run is active")
		assert.Equal(t, busy, a.Idle(), "a second call returns the same channel")

		close(release)
		require.NoError(t, <-done)
		assert.True(t, isClosed(busy), "the channel closed when the run ended")
		assert.True(t, isClosed(a.Idle()))
		require.NoError(t, a.Dispose())
	})
}

func TestIdleNoOrphanOnWakeChain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("two"))
		a := newAgent(t, p, m, nil)
		reached, release := make(chan struct{}), make(chan struct{})
		// agent_end comes after the claim that marked the first run as closing.
		a.Subscribe(gateNth("agent_end", 1, reached, release))
		var secondStart atomic.Bool
		var closedAtSecondStart atomic.Bool
		var busy <-chan struct{}
		a.Subscribe(func(ev protocol.Event) error {
			if label(ev) == "agent_start" && secondStart.Swap(true) {
				closedAtSecondStart.Store(isClosed(busy))
			}
			return nil
		})

		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		<-reached
		busy = a.Idle()
		var wakes atomic.Int32
		go func() { <-busy; wakes.Add(1) }()
		_, err := a.Steer(user("again")) // the run is closing, so a second run follows
		require.NoError(t, err)

		close(release)
		require.NoError(t, <-done)
		require.NoError(t, a.WaitForIdle(context.Background()))
		synctest.Wait()

		assert.Equal(t, 2, p.Calls(), "the wake chain ran a second run")
		assert.False(t, closedAtSecondStart.Load(), "the hand-off did not close the channel")
		assert.Equal(t, int32(1), wakes.Load(), "the waiter woke once, at the end of the last run")
		assert.True(t, isClosed(a.Idle()))
		require.NoError(t, a.Dispose())
	})
}

func TestIdleCoversPublisherDispatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"))
		a := newAgent(t, p, m, nil)
		settledReached, settledRelease := make(chan struct{}), make(chan struct{})
		a.Subscribe(gateNth("agent_settled", 1, settledReached, settledRelease))
		queueReached, queueRelease := make(chan struct{}), make(chan struct{})
		a.Subscribe(gateNth("queue_update", 1, queueReached, queueRelease))

		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		<-settledReached
		// The run is closing. Steer and Remove post queue_update events that the
		// publisher goroutine delivers after the run goroutine is done.
		id, err := a.Steer(user("x"))
		require.NoError(t, err)
		require.True(t, a.Remove(id))

		close(settledRelease)
		<-queueReached
		busy := a.Idle()
		require.NoError(t, <-done)
		synctest.Wait()
		assert.False(t, isClosed(busy), "the end of the run during a publisher dispatch did not close the channel")

		close(queueRelease)
		require.NoError(t, a.WaitForIdle(context.Background()))
		synctest.Wait()
		assert.True(t, isClosed(busy), "it closed when the dispatch ended")
		assert.True(t, isClosed(a.Idle()))
		require.NoError(t, a.Dispose())
	})
}

func TestWaitForQuiescenceBlocksWithoutSpin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Reply(timerCall("c1", "ping", "10s", "wake")), faux.Say("started"), faux.Say("noticed"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool()) })
		require.NoError(t, a.Prompt(context.Background(), user("go")))

		q := make(chan error, 1)
		go func() { q <- a.WaitForQuiescence(context.Background()) }()
		synctest.Wait() // a spin would never be durably blocked
		select {
		case <-q:
			t.Fatal("returned while a task still ran")
		default:
		}
		assert.Equal(t, 2, p.Calls())

		require.NoError(t, <-q)
		assert.Equal(t, 3, p.Calls(), "it returned after the notice run ended")
		assert.True(t, isClosed(a.Idle()))
		require.NoError(t, a.Dispose())
	})
}

// The registry and the Agent are both idle at the two checks, and a task then
// starts, ends, and wakes a run before the call reads the work counter again.
// Only the counter shows that work came in, so the call must check again and
// wait for that run.
func TestWaitForQuiescenceRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("noticed").Delay(time.Second))
		a := newAgent(t, p, m, nil)

		var once sync.Once
		a.SetQuiescenceProbe(func() {
			once.Do(func() {
				h, err := a.Tasks().Start(tasks.Spec{
					Kind:   tasks.KindShell,
					Title:  "instant",
					Notice: tasks.NoticeWake,
					Body:   func(context.Context, *tasks.Sink) tasks.Result { return tasks.Result{Summary: "done"} },
				})
				require.NoError(t, err)
				<-h.Done // the notice is handed over: it started a run
			})
		})

		require.NoError(t, a.WaitForQuiescence(context.Background()))

		assert.Equal(t, 1, p.Calls(), "the notice run made its request")
		assert.Equal(t, agent.Idle, a.State().Status, "the call returned after the notice run settled")
		assert.True(t, isClosed(a.Idle()))
		require.NoError(t, a.Dispose())
	})
}

func TestWaitForQuiescenceIgnoresContextQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Reply(timerCall("c1", "quiet", "1s", "context")), faux.Say("started"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool()) })
		require.NoError(t, a.Prompt(context.Background(), user("go")))

		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		// The context notice is left in the queue when the timer ends. It does
		// not block the call and it starts no run.
		require.NoError(t, a.WaitForQuiescence(ctx))

		assert.Equal(t, 2, p.Calls())
		assert.True(t, isClosed(a.Idle()))
		require.NoError(t, a.Dispose())
	})
}

func TestWaitForQuiescenceReturnsContextError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Reply(timerCall("c1", "far", "24h", "wake")), faux.Say("started"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool()) })
		require.NoError(t, a.Prompt(context.Background(), user("go")))

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		require.ErrorIs(t, a.WaitForQuiescence(ctx), context.DeadlineExceeded)
		require.NoError(t, a.Dispose())
	})
}
