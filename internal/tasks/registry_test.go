package tasks_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

// rig wires a Registry to recorders. It must be built inside the synctest
// bubble of the test that uses it.
type rig struct {
	reg *tasks.Registry
	dir string

	mu       sync.Mutex
	events   []protocol.Event
	stamps   []time.Time
	notices  []tasks.Notice
	atNotice []int // number of events seen when each notice arrived
	deliver  func(tasks.Notice) (string, error)

	work atomic.Int64
	next atomic.Int64
}

func newRig(t *testing.T, mod ...func(*tasks.Config)) *rig {
	t.Helper()
	rg := &rig{dir: t.TempDir()}
	cfg := tasks.Config{
		Root: context.Background(),
		Deliver: func(n tasks.Notice) (string, error) {
			rg.mu.Lock()
			rg.notices = append(rg.notices, n)
			rg.atNotice = append(rg.atNotice, len(rg.events))
			fn := rg.deliver
			count := len(rg.notices)
			rg.mu.Unlock()
			if fn != nil {
				return fn(n)
			}
			return fmt.Sprintf("in-%d", count), nil
		},
		Emit: func(ev protocol.Event) {
			rg.mu.Lock()
			defer rg.mu.Unlock()
			rg.events = append(rg.events, ev)
			rg.stamps = append(rg.stamps, time.Now())
		},
		Dir:  rg.dir,
		Gen:  7,
		Work: func() { rg.work.Add(1) },
		NextID: func(k tasks.Kind) string {
			return fmt.Sprintf("%s-%d", k, rg.next.Add(1))
		},
	}
	for _, m := range mod {
		m(&cfg)
	}
	rg.reg = tasks.New(cfg)
	return rg
}

func (rg *rig) setDeliver(fn func(tasks.Notice) (string, error)) {
	rg.mu.Lock()
	rg.deliver = fn
	rg.mu.Unlock()
}

func (rg *rig) types() []string {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	out := make([]string, 0, len(rg.events))
	for _, ev := range rg.events {
		out = append(out, ev.EventType())
	}
	return out
}

func (rg *rig) noticeList() []tasks.Notice {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	return append([]tasks.Notice(nil), rg.notices...)
}

func (rg *rig) info(t *testing.T, id string) tasks.Info {
	t.Helper()
	for _, in := range rg.reg.List() {
		if in.ID == id {
			return in
		}
	}
	require.FailNow(t, "task not listed", id)
	return tasks.Info{}
}

// waitForStop is a body that runs until its context is done.
func waitForStop(ctx context.Context, _ *tasks.Sink) tasks.Result {
	<-ctx.Done()
	return tasks.Result{Err: context.Cause(ctx)}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestOneTerminalStateAndOneNotice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		zero := 0
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Title: "demo", Body: func(_ context.Context, out *tasks.Sink) tasks.Result {
			_, _ = out.Write([]byte("hello\n"))
			return tasks.Result{ExitCode: &zero, Summary: "done"}
		}})
		require.NoError(t, err)
		assert.Equal(t, "shell-1", h.ID)
		<-h.Done

		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusCompleted, info.Status)
		assert.Equal(t, tasks.NoticeDelivered, info.Notice)
		assert.Equal(t, "in-1", info.InputID)
		assert.NotEmpty(t, info.OutputFile)
		assert.Equal(t, []string{protocol.TypeTaskStart, protocol.TypeTaskEnd}, rg.types())

		end, ok := rg.events[1].(*protocol.TaskEnd)
		require.True(t, ok)
		assert.Equal(t, h.ID, end.TaskID)
		assert.Equal(t, "completed", end.Status)
		require.NotNil(t, end.ExitCode)
		assert.Equal(t, 0, *end.ExitCode)
		assert.Equal(t, info.OutputFile, end.OutputFile)

		got := rg.noticeList()
		require.Len(t, got, 1)
		assert.Equal(t, "task", got[0].Origin)
		assert.Equal(t, h.ID, got[0].TaskID)
		assert.Equal(t, uint64(7), got[0].Gen)
		assert.Equal(t, tasks.NoticeWake, got[0].Policy)
		_, isCustom := got[0].Message.(protocol.CustomMessage)
		assert.True(t, isCustom)
		assert.Equal(t, 2, rg.atNotice[0], "task_end is emitted before Deliver")
		assert.EqualValues(t, 2, rg.work.Load(), "Work runs on start and on end")
	})
}

func TestStopGivesStoppedAndContextPolicy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: waitForStop})
		require.NoError(t, err)
		synctest.Wait()
		require.NoError(t, rg.reg.Stop(h.ID))
		<-h.Done

		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusStopped, info.Status)
		assert.ErrorIs(t, info.Cause, tasks.ErrStopped)
		got := rg.noticeList()
		require.Len(t, got, 1)
		assert.Equal(t, tasks.NoticeContext, got[0].Policy)

		assert.NoError(t, rg.reg.Stop(h.ID), "stop of an ended task is a no-op")
		assert.ErrorIs(t, rg.reg.Stop("nope"), tasks.ErrUnknownTask)
	})
}

func TestIdleAfterDeliver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		gate := make(chan struct{})
		rg.setDeliver(func(tasks.Notice) (string, error) {
			<-gate
			return "in", nil
		})
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(context.Context, *tasks.Sink) tasks.Result {
			return tasks.Result{}
		}})
		require.NoError(t, err)
		idle := rg.reg.Idle()
		synctest.Wait()
		assert.Equal(t, tasks.StatusCompleted, rg.info(t, h.ID).Status, "status is visible before Deliver ends")
		assert.False(t, isClosed(idle), "idle waits for Deliver")
		assert.False(t, isClosed(rg.reg.Done(h.ID)))
		assert.False(t, isClosed(h.Done))

		close(gate)
		synctest.Wait()
		assert.True(t, isClosed(idle))
		assert.True(t, isClosed(rg.reg.Done(h.ID)))
		assert.True(t, isClosed(h.Done))
		assert.True(t, isClosed(rg.reg.Done("unknown")), "an unknown task has nothing to wait for")
	})
}

func TestCloseWaitsForRunners(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		cause := errors.New("shutting down")
		var ids []string
		for range 2 {
			h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindWatch, Body: waitForStop})
			require.NoError(t, err)
			ids = append(ids, h.ID)
		}
		synctest.Wait()
		rg.reg.Close(cause)
		for _, id := range ids {
			info := rg.info(t, id)
			assert.Equal(t, tasks.StatusStopped, info.Status)
			assert.ErrorIs(t, info.Cause, cause)
			assert.Equal(t, tasks.NoticeDelivered, info.Notice, "Close waits for the hand-off")
		}
		assert.Len(t, rg.noticeList(), 2)

		_, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindWatch, Body: waitForStop})
		assert.ErrorIs(t, err, tasks.ErrClosed)
		rg.reg.Close(cause) // a second Close is safe
	})
}

func TestRootCancelStopsTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, cancel := context.WithCancelCause(context.Background())
		rg := newRig(t, func(c *tasks.Config) { c.Root = root })
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: waitForStop})
		require.NoError(t, err)
		synctest.Wait()
		reason := errors.New("agent disposed")
		cancel(reason)
		<-h.Done
		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusStopped, info.Status)
		assert.ErrorIs(t, info.Cause, reason)
	})
}

func TestDiscardOnlyForDisposedOrStale(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		bug := errors.New("unexpected")
		rg.setDeliver(func(n tasks.Notice) (string, error) {
			switch n.TaskID {
			case "shell-1":
				return "", fmt.Errorf("agent disposed: %w", tasks.ErrNotDelivered)
			case "shell-2":
				return "", fmt.Errorf("old generation: %w", tasks.ErrNotDelivered)
			}
			return "", bug
		})
		var hs []tasks.Handle
		for range 3 {
			h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(context.Context, *tasks.Sink) tasks.Result {
				return tasks.Result{}
			}})
			require.NoError(t, err)
			hs = append(hs, h)
		}
		for _, h := range hs {
			<-h.Done
		}
		assert.Equal(t, tasks.NoticeDiscarded, rg.info(t, "shell-1").Notice)
		assert.Equal(t, tasks.NoticeDiscarded, rg.info(t, "shell-2").Notice)
		third := rg.info(t, "shell-3")
		assert.Equal(t, tasks.NoticeFailed, third.Notice)
		assert.Contains(t, third.NoticeError, "unexpected")
		assert.Equal(t, tasks.StatusCompleted, third.Status, "a Deliver error does not change the task state")
		// Delivery results never appear in events.
		assert.Equal(t, []string{
			protocol.TypeTaskStart, protocol.TypeTaskStart, protocol.TypeTaskStart,
			protocol.TypeTaskEnd, protocol.TypeTaskEnd, protocol.TypeTaskEnd,
		}, sortedTypes(rg.types()))
	})
}

// sortedTypes puts all task_start entries before all task_end entries, which
// is the only ordering that the test needs across three tasks.
func sortedTypes(in []string) []string {
	var starts, ends []string
	for _, s := range in {
		if s == protocol.TypeTaskStart {
			starts = append(starts, s)
		} else {
			ends = append(ends, s)
		}
	}
	return append(starts, ends...)
}

func TestStopAllSilentDeliversNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		_, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: waitForStop})
		require.NoError(t, err)
		_, err = rg.reg.Start(tasks.Spec{Kind: tasks.KindWatch, Body: waitForStop})
		require.NoError(t, err)
		_, err = rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Hour})
		require.NoError(t, err)
		synctest.Wait()

		reason := errors.New("reset")
		rg.reg.StopAll(reason, tasks.NoticeSilent)
		<-rg.reg.Idle()
		synctest.Wait()

		assert.Empty(t, rg.noticeList())
		for _, in := range rg.reg.List() {
			assert.Equal(t, tasks.StatusStopped, in.Status, in.ID)
			assert.Equal(t, tasks.NoticeSkipped, in.Notice, in.ID)
			assert.ErrorIs(t, in.Cause, reason, in.ID)
		}
		ends := 0
		for _, ty := range rg.types() {
			if ty == protocol.TypeTaskEnd {
				ends++
			}
		}
		assert.Equal(t, 3, ends)
	})
}

func TestIdleEdges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		assert.True(t, isClosed(rg.reg.Idle()), "an empty registry is idle")

		release := make(chan struct{})
		body := func(context.Context, *tasks.Sink) tasks.Result {
			<-release
			return tasks.Result{}
		}
		a, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: body})
		require.NoError(t, err)
		first := rg.reg.Idle()
		assert.False(t, isClosed(first))

		b, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(ctx context.Context, _ *tasks.Sink) tasks.Result {
			<-ctx.Done()
			return tasks.Result{}
		}})
		require.NoError(t, err)
		assert.Equal(t, first, rg.reg.Idle(), "a second start does not make a new channel")

		close(release)
		<-a.Done
		assert.False(t, isClosed(first), "the first end does not close it")
		require.NoError(t, rg.reg.Stop(b.ID))
		<-b.Done
		assert.True(t, isClosed(first))
		assert.True(t, isClosed(rg.reg.Idle()))

		c, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: waitForStop})
		require.NoError(t, err)
		second := rg.reg.Idle()
		assert.NotEqual(t, first, second, "a new busy period gets a new channel")
		assert.False(t, isClosed(second))
		require.NoError(t, rg.reg.Stop(c.ID))
		<-c.Done
		assert.True(t, isClosed(second))
	})
}

func TestNoLockDuringDeliver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		var once sync.Once
		var inner tasks.Handle
		var innerErr error
		rg.setDeliver(func(tasks.Notice) (string, error) {
			once.Do(func() {
				_ = rg.reg.List()
				inner, innerErr = rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Second})
			})
			return "in", nil
		})
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(context.Context, *tasks.Sink) tasks.Result {
			return tasks.Result{}
		}})
		require.NoError(t, err)
		<-h.Done
		require.NoError(t, innerErr)
		time.Sleep(time.Second)
		<-inner.Done
		assert.Equal(t, tasks.StatusCompleted, rg.info(t, inner.ID).Status)
	})
}

func TestStartFailsWithoutDir(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t, func(c *tasks.Config) { c.Dir = "" })
		defer rg.reg.Close(nil)
		ran := atomic.Bool{}
		_, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(context.Context, *tasks.Sink) tasks.Result {
			ran.Store(true)
			return tasks.Result{}
		}})
		assert.ErrorIs(t, err, tasks.ErrNoTaskDir)
		synctest.Wait()
		assert.False(t, ran.Load())
		assert.Empty(t, rg.reg.List())
		assert.Empty(t, rg.types())
		assert.EqualValues(t, 0, rg.work.Load())
		assert.True(t, isClosed(rg.reg.Idle()))

		// A timer needs no file, so it starts without a directory.
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Second})
		require.NoError(t, err)
		time.Sleep(time.Second)
		<-h.Done
		assert.Empty(t, rg.info(t, h.ID).OutputFile)
	})
}

func TestStartRejectsInvalidSpec(t *testing.T) {
	body := func(context.Context, *tasks.Sink) tasks.Result { return tasks.Result{} }
	cases := []struct {
		name string
		spec tasks.Spec
		id   string
	}{
		{"unknown kind", tasks.Spec{Kind: "bogus", Body: body}, ""},
		{"body missing", tasks.Spec{Kind: tasks.KindShell}, ""},
		{"timer with body", tasks.Spec{Kind: tasks.KindTimer, Body: body}, ""},
		{"negative delay", tasks.Spec{Kind: tasks.KindTimer, After: -time.Second}, ""},
		{"negative deadline", tasks.Spec{Kind: tasks.KindShell, Deadline: -time.Second, Body: body}, ""},
		{"path in id", tasks.Spec{Kind: tasks.KindShell, Body: body}, "../escape"},
		{"trailing slash in id", tasks.Spec{Kind: tasks.KindShell, Body: body}, "x/"},
		{"dot id", tasks.Spec{Kind: tasks.KindShell, Body: body}, ".."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rg := newRig(t, func(c *tasks.Config) {
					if tc.id != "" {
						c.NextID = func(tasks.Kind) string { return tc.id }
					}
				})
				defer rg.reg.Close(nil)
				_, err := rg.reg.Start(tc.spec)
				assert.ErrorIs(t, err, tasks.ErrInvalidSpec)
				assert.Empty(t, rg.reg.List())
			})
		})
	}

	t.Run("duplicate id", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			rg := newRig(t, func(c *tasks.Config) { c.NextID = func(tasks.Kind) string { return "same" } })
			defer rg.reg.Close(nil)
			_, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindWatch, Body: waitForStop})
			require.NoError(t, err)
			_, err = rg.reg.Start(tasks.Spec{Kind: tasks.KindWatch, Body: waitForStop})
			assert.ErrorIs(t, err, tasks.ErrInvalidSpec)
			assert.Len(t, rg.reg.List(), 1)
		})
	})
}
