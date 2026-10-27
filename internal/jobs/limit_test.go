//go:build unix

package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func withLimit(def, max time.Duration) rigOption {
	return withOptions(func(o *Options) { o.BackgroundLimit = BackgroundLimit{Default: def, Max: max} })
}

// blockingOwner holds every Notify until release closes and reports entry.
type blockingOwner struct {
	*fakeNotifier
	entered chan struct{}
	release chan struct{}
}

func (b *blockingOwner) Notify(m protocol.Message) (string, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return b.fakeNotifier.Notify(m)
}

// The table stays busy until the notice of a finished job is handed off, so a
// quiescence check cannot see the end of a job before its notice is queued.
func TestTableIdleWaitsForNoticeHandOff(t *testing.T) {
	r := newRig(t)
	owner := &blockingOwner{fakeNotifier: r.n, entered: make(chan struct{}, 1), release: make(chan struct{})}
	assert.True(t, isClosedChan(r.tb.Idle()), "an empty table is idle")

	j := r.startFor(owner, "true")
	waitDone(t, j)
	<-owner.entered
	assert.False(t, isClosedChan(r.tb.Idle()), "the job counts until its notice is handed off")

	close(owner.release)
	select {
	case <-r.tb.Idle():
	case <-time.After(5 * time.Second):
		t.Fatal("the table did not turn idle after the hand-off")
	}
	assert.Len(t, r.n.followUps(), 1)
}

// A wait that comes after the notice of a background job started must not
// report the end a second time.
func TestWaitAfterNoticeStartedDoesNotReportAgain(t *testing.T) {
	r := newRig(t)
	owner := &blockingOwner{fakeNotifier: r.n, entered: make(chan struct{}, 1), release: make(chan struct{})}
	j := r.startFor(owner, "true")
	waitDone(t, j)
	<-owner.entered

	assert.Equal(t, Background, r.tb.Wait(background(), j.ID(), time.Minute))
	close(owner.release)
	waitFor(t, 5*time.Second, "the notice", func() bool { return len(r.n.followUps()) == 1 })
}

func TestTableIdleAfterAwaitedJob(t *testing.T) {
	r := newRig(t)
	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: "true", Cwd: r.dir, Foreground: true})
	require.NoError(t, err)
	r.track(j)
	require.Equal(t, Done, r.tb.Wait(background(), j.ID(), time.Minute))
	select {
	case <-r.tb.Idle():
	case <-time.After(5 * time.Second):
		t.Fatal("an awaited job left the table busy")
	}
	assert.Empty(t, r.n.all(), "an awaited job sends no notice")
}

func TestBackgroundLimitStopsJobWithNotice(t *testing.T) {
	r := newRig(t, withFakeClock(), withLimit(time.Second, time.Hour))
	gate, open := r.gate("never")
	defer open()
	j := r.bash(gate)

	r.clock.Advance(time.Second)
	waitDone(t, j)
	waitFor(t, 5*time.Second, "limit notice", func() bool { return len(r.n.followUps()) == 1 })
	text := r.n.followUps()[0].text
	assert.Contains(t, text, `Background command "while [ ! -f`)
	assert.Contains(t, text, "was stopped after reaching its background time limit")
	assert.Contains(t, text, j.ID())
}

func TestRequestedBackgroundLimitIsCappedAtMax(t *testing.T) {
	r := newRig(t, withFakeClock(), withLimit(time.Second, 2*time.Second))
	gate, open := r.gate("never")
	defer open()
	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: gate, Cwd: r.dir, BackgroundLimit: time.Hour})
	require.NoError(t, err)
	r.track(j)

	r.clock.Advance(time.Second)
	assert.Equal(t, StatusRunning, j.Status(), "the request is longer than the default")
	r.clock.Advance(time.Second)
	waitDone(t, j)
	assert.Equal(t, limitReason, j.KillReason())
}

func TestMovedJobGetsDefaultLimitFromTheMove(t *testing.T) {
	r := newRig(t, withFakeClock(), withLimit(time.Second, time.Hour))
	gate, open := r.gate("never")
	defer open()
	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: gate, Cwd: r.dir, Foreground: true})
	require.NoError(t, err)
	r.track(j)

	moved := make(chan WaitOutcome, 1)
	go func() { moved <- r.tb.Wait(background(), j.ID(), 500*time.Millisecond) }()
	waitFor(t, 5*time.Second, "wait budget timer", func() bool { return r.clock.Pending() > 0 })
	r.clock.Advance(500 * time.Millisecond)
	require.Equal(t, Background, <-moved)

	r.clock.Advance(900 * time.Millisecond)
	assert.Equal(t, StatusRunning, j.Status(), "the limit counts from the move")
	r.clock.Advance(100 * time.Millisecond)
	waitDone(t, j)
	assert.Equal(t, limitReason, j.KillReason())
}

func TestNoBackgroundLimitWithoutOptions(t *testing.T) {
	r := newRig(t, withFakeClock())
	gate, open := r.gate("later")
	j := r.bash(gate)
	r.clock.Advance(100 * time.Hour)
	assert.Equal(t, StatusRunning, j.Status(), "the TUI and the leader have no limit")
	open()
	waitDone(t, j)
}

func TestMonitorDeadlineCappedByBackgroundLimit(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		r := newRig(t, withFakeClock(), withLimit(time.Second, time.Hour))
		j := r.monitor(MonitorSpec{Command: "sleep 300", Persistent: persistent})
		r.clock.Advance(time.Second)
		waitDone(t, j)
		assert.Equal(t, timeoutReason, j.KillReason(), "persistent=%v", persistent)
	}
}

func TestDisabledBackgroundStopsAtBudgetAndRefusesBackground(t *testing.T) {
	r := newRig(t, withFakeClock(), withOptions(func(o *Options) { o.DisableBackground = true }))
	gate, open := r.gate("never")
	defer open()

	_, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: gate, Cwd: r.dir})
	require.ErrorIs(t, err, ErrBackgroundDisabled)
	_, err = r.tb.StartMonitor(MonitorSpec{Owner: r.owner(), Command: gate, Cwd: r.dir})
	require.ErrorIs(t, err, ErrBackgroundDisabled)

	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: gate, Cwd: r.dir, Foreground: true})
	require.NoError(t, err)
	r.track(j)
	out := make(chan WaitOutcome, 1)
	go func() { out <- r.tb.Wait(background(), j.ID(), 500*time.Millisecond) }()
	waitFor(t, 5*time.Second, "wait budget timer", func() bool { return r.clock.Pending() > 0 })
	r.clock.Advance(500 * time.Millisecond)
	assert.Equal(t, Done, <-out, "the command stops at its budget")
	assert.Equal(t, timeoutReason, j.KillReason())
}

func isClosedChan(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
