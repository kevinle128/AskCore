package agent_test

import (
	"context"
	"strings"
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

func TestAbortKeepsTasksAndMovesNotices(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		long := faux.Say(strings.Repeat("slow words ", 500)).Pace(100)
		p, m := newFaux(t, faux.WithChunk(2, 2))
		p.Set(
			faux.Reply(timerCall("c1", "long", "24h", "wake"), timerCall("c2", "short", "1s", "wake")),
			long,
			faux.Say("after abort"),
		)
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool()) })
		rec := &recorder{}
		a.Subscribe(rec.emit)
		streaming := make(chan struct{})
		a.Subscribe(signalOn("message_update(text_delta)", streaming))

		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		<-streaming
		mustSteer(t, a, "human")
		time.Sleep(2 * time.Second) // the short timer fires while the reply streams
		a.Abort()
		require.NoError(t, <-done)
		require.NoError(t, a.WaitForIdle(context.Background()))

		assert.Equal(t, 2, p.Calls(), "the queued notice did not wake the agent")
		var running int
		for _, info := range a.Tasks().List() {
			if info.Status == tasks.StatusRunning {
				running++
			}
		}
		assert.Equal(t, 1, running, "Abort left the long timer running")

		require.NoError(t, a.Prompt(context.Background(), user("next")))
		users := requestUsers(p, 2)
		assert.Len(t, noticeUsers(p, 2), 1, "the notice rides with the next prompt once")
		assert.NotContains(t, users, "human", "the human input was dropped")
		require.NoError(t, a.Dispose())
	})
}

func TestResetDropsLateNoticesAndEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var ended atomic.Bool
		p, m := newFaux(t)
		p.Set(faux.Reply(bodyCall("c1")), faux.Say("started"), faux.Say("fresh"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, bodyTool(release, false, &ended)) })
		rec := &recorder{}
		a.Subscribe(rec.emit)

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		old := a.Tasks()
		require.Len(t, old.List(), 1)

		// The body ignores its context, so the old registry cannot close until
		// release: Reset swaps the registry and then waits.
		reset := make(chan error, 1)
		go func() { reset <- a.Reset() }()
		synctest.Wait()
		fresh := rec.eventLabels()
		require.NoError(t, a.Prompt(context.Background(), user("new epoch")))
		boundary := len(rec.eventLabels())

		close(release)
		require.NoError(t, <-reset)
		require.NoError(t, a.WaitForIdle(context.Background()))

		assert.True(t, ended.Load())
		after := rec.eventLabels()[len(fresh):]
		assert.Len(t, rec.eventLabels(), boundary, "no event of the old registry came after the new epoch began")
		assert.NotContains(t, after, "task_end")
		assert.Equal(t, 3, p.Calls(), "the old notice woke nothing")
		infos := old.List()
		require.Len(t, infos, 1)
		assert.Equal(t, tasks.NoticeDiscarded, infos[0].Notice, "the stale notice was discarded")
		assert.Empty(t, a.Tasks().List(), "the new registry holds no task of the old one")
		require.NoError(t, a.Dispose())
	})
}

func TestDisposeStopsAndWaitsTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var ended atomic.Bool
		p, m := newFaux(t)
		p.Set(faux.Reply(bodyCall("c1"), timerCall("c2", "later", "1h", "wake")), faux.Say("started"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, bodyTool(release, true, &ended), timerTool()) })
		require.NoError(t, a.Prompt(context.Background(), user("go")))
		control := a.Tasks()

		require.NoError(t, a.Dispose())

		assert.True(t, ended.Load(), "Dispose returned after the body returned")
		for _, info := range control.List() {
			assert.Equal(t, tasks.StatusStopped, info.Status)
			assert.Equal(t, tasks.NoticeDiscarded, info.Notice, "a notice after Dispose is discarded")
		}
		_, err := control.Start(tasks.Spec{Kind: tasks.KindTimer, Title: "late", After: time.Second})
		require.ErrorIs(t, err, tasks.ErrClosed)
		assert.Equal(t, 2, p.Calls(), "the stopped tasks woke nothing")
	})
}

func TestDisposeEventsPrecedeAgentDisposed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Reply(timerCall("c1", "later", "1h", "wake")), faux.Say("started"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool()) })
		rec := &recorder{}
		a.Subscribe(rec.emit)
		require.NoError(t, a.Prompt(context.Background(), user("go")))

		require.NoError(t, a.Dispose())

		labels := rec.eventLabels()
		end := nthIndex(labels, "task_end", 1)
		disposed := nthIndex(labels, "agent_disposed", 1)
		require.NotEqual(t, -1, end, "Dispose published the task_end of the stopped timer")
		assert.Less(t, end, disposed)
	})
}

func TestNoNestedLocks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(
			faux.Reply(timerCall("c1", "a", "1s", "wake"), timerCall("c2", "b", "1s", "context"), sleepCall("c3")),
			faux.Say("seen"),
		)
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool(), sleepTool(2*time.Second)) })
		var calls atomic.Int32
		a.Subscribe(func(protocol.Event) error {
			_ = a.Tasks().List()
			_ = a.State()
			calls.Add(1)
			return nil
		})

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		require.NoError(t, a.WaitForIdle(context.Background()))

		assert.Positive(t, calls.Load())
		require.NoError(t, a.Dispose())
	})
}

// Reset closes the old registry after it released every lock. Dispose must wait
// for that close, so no task of the Agent runs after Dispose returned.
func TestDisposeWaitsForResetThatClosesOldTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var ended atomic.Bool
		p, m := newFaux(t)
		p.Set(faux.Reply(bodyCall("c1")), faux.Say("started"))
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, bodyTool(release, false, &ended)) })
		require.NoError(t, a.Prompt(context.Background(), user("go")))

		// The body ignores its context, so the old registry cannot close until
		// release.
		reset := make(chan error, 1)
		go func() { reset <- a.Reset() }()
		synctest.Wait()
		disposed := make(chan error, 1)
		go func() { disposed <- a.Dispose() }()
		synctest.Wait()

		select {
		case <-disposed:
			t.Fatal("Dispose returned while a task of the old registry still ran")
		default:
		}
		assert.False(t, ended.Load())

		close(release)
		require.NoError(t, <-reset)
		require.NoError(t, <-disposed)
		assert.True(t, ended.Load(), "the body returned before Dispose did")
	})
}
