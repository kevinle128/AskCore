package tasks_test

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

func TestTimerWaitsWithoutGoroutine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, Title: "remind", After: 10 * time.Minute})
		require.NoError(t, err)
		// Wait returns only when every goroutine of the bubble is blocked, so
		// a timer that kept a goroutine busy would hang here.
		synctest.Wait()
		assert.Equal(t, tasks.StatusRunning, rg.info(t, h.ID).Status)
		assert.Empty(t, rg.noticeList())

		time.Sleep(10*time.Minute - time.Second)
		synctest.Wait()
		assert.Equal(t, tasks.StatusRunning, rg.info(t, h.ID).Status)

		time.Sleep(time.Second)
		<-h.Done
		info := rg.info(t, h.ID)
		assert.Equal(t, tasks.StatusCompleted, info.Status)
		assert.Empty(t, info.OutputFile, "a timer has no output file")
		assert.Equal(t, []string{protocol.TypeTaskStart, protocol.TypeTaskEnd}, rg.types())
		assert.Len(t, rg.noticeList(), 1)
		assert.True(t, isClosed(rg.reg.Idle()))
	})
}

func TestTimerStopBeforeFire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		defer rg.reg.Close(nil)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Minute})
		require.NoError(t, err)
		synctest.Wait()
		require.NoError(t, rg.reg.Stop(h.ID))
		<-h.Done
		assert.Equal(t, tasks.StatusStopped, rg.info(t, h.ID).Status)

		time.Sleep(2 * time.Minute)
		synctest.Wait()
		assert.Equal(t, tasks.StatusStopped, rg.info(t, h.ID).Status, "the callback never ran")
		assert.Equal(t, []string{protocol.TypeTaskStart, protocol.TypeTaskEnd}, rg.types())
		got := rg.noticeList()
		require.Len(t, got, 1)
		assert.Equal(t, tasks.NoticeContext, got[0].Policy)
	})
}

func TestTimerOwnershipBothOrders(t *testing.T) {
	// A real hour-long timer never fires in this test. FireTimer runs the
	// callback by hand, so the test controls the order.
	wantEvents := []string{protocol.TypeTaskStart, protocol.TypeTaskEnd}
	t.Run("fire then stop", func(t *testing.T) {
		rg := newRig(t)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Hour})
		require.NoError(t, err)
		tasks.FireTimer(rg.reg, h.ID)
		require.NoError(t, rg.reg.Stop(h.ID))
		<-h.Done
		assert.Equal(t, tasks.StatusCompleted, rg.info(t, h.ID).Status)
		assert.Len(t, rg.noticeList(), 1)
		rg.reg.Close(nil) // returns only if the wait group balances
		assert.Equal(t, wantEvents, rg.types())
	})
	t.Run("stop then fire", func(t *testing.T) {
		rg := newRig(t)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Hour})
		require.NoError(t, err)
		require.NoError(t, rg.reg.Stop(h.ID))
		tasks.FireTimer(rg.reg, h.ID)
		<-h.Done
		assert.Equal(t, tasks.StatusStopped, rg.info(t, h.ID).Status)
		assert.Len(t, rg.noticeList(), 1)
		rg.reg.Close(nil)
		assert.Equal(t, wantEvents, rg.types())
	})
	t.Run("concurrent", func(t *testing.T) {
		for range 50 {
			rg := newRig(t)
			h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Hour})
			require.NoError(t, err)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); tasks.FireTimer(rg.reg, h.ID) }()
			go func() { defer wg.Done(); _ = rg.reg.Stop(h.ID) }()
			wg.Wait()
			<-h.Done
			rg.reg.Close(nil)
			assert.Len(t, rg.noticeList(), 1)
			assert.Equal(t, wantEvents, rg.types())
		}
	})
}

func TestCloseWithPendingTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rg := newRig(t)
		h, err := rg.reg.Start(tasks.Spec{Kind: tasks.KindTimer, After: time.Hour})
		require.NoError(t, err)
		synctest.Wait()
		rg.reg.Close(nil)
		assert.True(t, isClosed(h.Done))
		assert.Equal(t, tasks.StatusStopped, rg.info(t, h.ID).Status)

		time.Sleep(2 * time.Hour)
		synctest.Wait()
		assert.Equal(t, tasks.StatusStopped, rg.info(t, h.ID).Status, "no callback runs after Close")
		assert.Equal(t, []string{protocol.TypeTaskStart, protocol.TypeTaskEnd}, rg.types())
		assert.Len(t, rg.noticeList(), 1)
	})
}
