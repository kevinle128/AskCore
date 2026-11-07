package tasks_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

func TestStartReturnsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		release := make(chan struct{})
		entered := make(chan struct{})
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(context.Context, *tasks.Sink) tasks.Result {
			close(entered)
			<-release
			return tasks.Result{}
		}})
		require.NoError(t, err, "Start returns while the body is blocked")
		<-entered
		synctest.Wait()
		assert.Equal(t, tasks.StatusRunning, rg.info(t, h.ID).Status)
		assert.False(t, isClosed(h.Done))
		assert.Empty(t, rg.noticeList())
		assert.Equal(t, []string{protocol.TypeTaskStart}, rg.types())

		close(release)
		<-h.Done
		assert.Equal(t, tasks.StatusCompleted, rg.info(t, h.ID).Status)
	})
}

func TestDeadlineGivesTimedOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Deadline: 5 * time.Second, Body: waitForStop})
		require.NoError(t, err)
		time.Sleep(4 * time.Second)
		synctest.Wait()
		assert.Equal(t, tasks.StatusRunning, rg.info(t, h.ID).Status)

		time.Sleep(time.Second)
		<-h.Done
		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusTimedOut, info.Status)
		assert.ErrorIs(t, info.Cause, tasks.ErrDeadline)
		end := rg.events[len(rg.events)-1].(*protocol.TaskEnd)
		assert.Equal(t, "timed_out", end.Status)
		assert.Equal(t, int64(5000), end.DurationMS)
		got := rg.noticeList()
		require.Len(t, got, 1)
		assert.Equal(t, tasks.NoticeWake, got[0].Policy, "a deadline keeps the policy of the spec")
	})
}

func TestBodyErrorGivesFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		code := 3
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(context.Context, *tasks.Sink) tasks.Result {
			return tasks.Result{ExitCode: &code, Err: fmt.Errorf("exit status 3")}
		}})
		require.NoError(t, err)
		<-h.Done
		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusFailed, info.Status)
		assert.Equal(t, "exit status 3", info.Error)
		require.NotNil(t, info.ExitCode)
		assert.Equal(t, 3, *info.ExitCode)
	})
}

func TestBodyPanicGivesFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(_ context.Context, out *tasks.Sink) tasks.Result {
			_, _ = out.Write([]byte("before\n"))
			panic("boom")
		}})
		require.NoError(t, err)
		<-h.Done
		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusFailed, info.Status)
		assert.Contains(t, info.Error, "boom")
		require.NotEmpty(t, info.OutputFile)
		data, err := os.ReadFile(info.OutputFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), "before\n")
		assert.Contains(t, string(data), "panic: boom")
		assert.Contains(t, string(data), "goroutine", "the stack trace is in the sink")
		assert.Len(t, rg.noticeList(), 1)
	})
}

func TestTaskUpdateThrottled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(_ context.Context, out *tasks.Sink) tasks.Result {
			for range 350 {
				_, _ = out.Write([]byte("line\n"))
				time.Sleep(10 * time.Millisecond)
			}
			return tasks.Result{}
		}})
		require.NoError(t, err)
		<-h.Done

		var stamps []time.Time
		var lastBytes int64
		for i, ev := range rg.events {
			up, ok := ev.(*protocol.TaskUpdate)
			if !ok {
				continue
			}
			stamps = append(stamps, rg.stamps[i])
			assert.Equal(t, h.ID, up.TaskID)
			assert.GreaterOrEqual(t, up.OutputBytes, lastBytes)
			assert.Equal(t, up.OutputBytes/5, up.OutputLines)
			lastBytes = up.OutputBytes
		}
		assert.GreaterOrEqual(t, len(stamps), 3)
		assert.LessOrEqual(t, len(stamps), 4, "3.5 seconds of writes give at most one update per second")
		for i := 1; i < len(stamps); i++ {
			assert.GreaterOrEqual(t, stamps[i].Sub(stamps[i-1]), time.Second)
		}
	})
}

func TestThrottleNeverAfterEnd(t *testing.T) {
	for _, run := range []time.Duration{999 * time.Millisecond, time.Second, 1001 * time.Millisecond} {
		t.Run(run.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rg := newRig(t)
				defer rg.reg.Close(nil)
				h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(_ context.Context, out *tasks.Sink) tasks.Result {
					_, _ = out.Write([]byte("x"))
					time.Sleep(run)
					return tasks.Result{}
				}})
				require.NoError(t, err)
				<-h.Done
				// Let any armed throttle timer expire.
				time.Sleep(5 * time.Second)
				synctest.Wait()

				types := rg.types()
				end := slices.Index(types, protocol.TypeTaskEnd)
				require.GreaterOrEqual(t, end, 0)
				for i := end + 1; i < len(types); i++ {
					assert.NotEqual(t, protocol.TypeTaskUpdate, types[i], "no task_update after task_end")
				}
				updates := 0
				for _, ty := range types {
					if ty == protocol.TypeTaskUpdate {
						updates++
					}
				}
				switch run {
				case 999 * time.Millisecond:
					assert.Zero(t, updates, "the task ended before the throttle fired")
				case 1001 * time.Millisecond:
					assert.Equal(t, 1, updates)
				}
			})
		})
	}
}

// A body that returned without an error did its work. A stop that lands while
// the body ends does not change that result.
func TestBodySuccessAfterStopStaysCompleted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindShell, Body: func(ctx context.Context, _ *tasks.Sink) tasks.Result {
			<-ctx.Done()
			return tasks.Result{Summary: "finished its work"}
		}})
		require.NoError(t, err)
		synctest.Wait()
		require.NoError(t, rg.reg.Stop(h.ID))
		<-h.Done

		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusCompleted, info.Status)
		assert.NoError(t, info.Cause)
	})
}
