//go:build unix

package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sandbox"
	"AskCore/pkg/protocol"
)

func withStall(d time.Duration) rigOption {
	return withOptions(func(o *Options) { o.StallAfter = d })
}

// stallNotices returns the stall notices that the owner of the rig received.
func (r *rig) stallNotices() []string {
	var out []string
	for _, m := range r.n.followUps() {
		if strings.Contains(m.text, "printed no output") {
			out = append(out, m.text)
		}
	}
	return out
}

// emit writes a line into f and waits until the table saw the chunk. The clock
// must have moved since the previous chunk, or the wait cannot tell the chunks
// apart.
func (r *rig) emit(f *fifo, j *Job, line string) {
	r.t.Helper()
	want := max(int64(r.clock.Now().Sub(j.started)), 1)
	require.NotEqual(r.t, want, j.lastOut.Load(), "advance the clock between two chunks")
	f.line(r.t, line)
	waitFor(r.t, 5*time.Second, "output to reach the job", func() bool { return j.lastOut.Load() == want })
}

func waitPending(t *testing.T, r *rig, want int) {
	t.Helper()
	waitFor(t, 5*time.Second, fmt.Sprintf("%d pending timers", want), func() bool { return r.clock.Pending() == want })
}

func TestStallNoticeAfterSilence(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	f := r.fifo()
	j := r.bash(f.command())
	require.Equal(t, 1, r.clock.Pending())

	r.clock.Advance(time.Minute)

	want := fmt.Sprintf("Background command %q (job %s) printed no output for 1 minute. It still runs. Read it with job_output, stop it with job_kill, or leave it running.", j.Label(), j.ID())
	assert.Equal(t, []string{protocol.SystemReminder(want)}, r.stallNotices())
	assert.Equal(t, StatusRunning, j.Status())
}

func TestStallNoticeOncePerSilencePeriod(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	r.bash(r.gateForever())

	r.clock.Advance(time.Minute)
	require.Len(t, r.stallNotices(), 1)
	r.clock.Advance(10 * time.Minute)

	assert.Len(t, r.stallNotices(), 1)
	assert.Equal(t, 0, r.clock.Pending(), "no timer runs after the notice")
}

// gateForever returns a command that prints nothing and does not end soon.
func (r *rig) gateForever() string {
	command, _ := r.gate("never")
	return command
}

func TestOutputStartsNewSilencePeriod(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	f := r.fifo()
	j := r.bash(f.command())

	r.clock.Advance(30 * time.Second)
	r.emit(f, j, "a")
	r.clock.Advance(30 * time.Second)
	assert.Empty(t, r.stallNotices(), "output at 30 s moved the end of the silence to 90 s")
	r.clock.Advance(30 * time.Second)
	require.Len(t, r.stallNotices(), 1)

	r.clock.Advance(5 * time.Minute)
	require.Len(t, r.stallNotices(), 1)
	r.emit(f, j, "b")
	waitPending(t, r, 1)
	r.clock.Advance(time.Minute)
	assert.Len(t, r.stallNotices(), 2, "new output starts a new silence period")
}

func TestForegroundJobInBudgetGetsNoStallNotice(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	f := r.fifo()
	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: f.command(), Cwd: r.dir, Foreground: true})
	require.NoError(t, err)
	r.track(j)
	out := make(chan WaitOutcome, 1)
	go func() { out <- r.tb.Wait(background(), j.ID(), 5*time.Minute) }()
	waitPending(t, r, 1)

	r.clock.Advance(4 * time.Minute)
	r.emit(f, j, "late line")
	assert.Equal(t, 1, r.clock.Pending(), "only the wait budget runs")
	r.clock.Advance(time.Minute)
	require.Equal(t, Background, <-out)
	assert.Empty(t, r.stallNotices())

	r.clock.Advance(time.Minute)
	assert.Len(t, r.stallNotices(), 1, "the silence counts from the move, not from the line in the foreground")
}

func TestNoStallNoticeWhileJobOutputWaits(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	j := r.bash(r.gateForever())
	ctx, cancel := context.WithCancel(background())
	defer cancel()
	read := make(chan error, 1)
	go func() {
		_, err := r.tb.Read(ctx, r.gen(), j.ID(), true, 30*time.Minute)
		read <- err
	}()
	waitFor(t, 5*time.Second, "the read to register", func() bool {
		r.tb.mu.Lock()
		defer r.tb.mu.Unlock()
		return j.waiters > 0
	})

	r.clock.Advance(time.Minute)
	assert.Empty(t, r.stallNotices(), "a job_output wait watches the job")

	cancel()
	require.NoError(t, <-read)
	r.clock.Advance(time.Minute)
	assert.Len(t, r.stallNotices(), 1, "the timer counted a full period again")
}

func TestMonitorGetsNoStallNotice(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	j := r.monitor(MonitorSpec{Command: "sleep 300", Persistent: true})

	assert.Equal(t, 0, r.clock.Pending())
	r.clock.Advance(10 * time.Minute)
	assert.Empty(t, r.stallNotices())
	assert.Equal(t, StatusRunning, j.Status())
}

func TestNoStallNoticeAfterKill(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	j := r.bash(r.gateForever())

	_, err := r.tb.Kill(r.gen(), j.ID(), "")
	require.NoError(t, err)

	waitPending(t, r, 0)
	r.clock.Advance(10 * time.Minute)
	assert.Empty(t, r.stallNotices())
}

func TestDisabledBackgroundGetsNoStallNotice(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute), withOptions(func(o *Options) { o.DisableBackground = true }))
	j, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: r.gateForever(), Cwd: r.dir, Foreground: true})
	require.NoError(t, err)
	r.track(j)

	require.Equal(t, Background, r.tb.Wait(background(), j.ID(), 0))

	assert.Equal(t, 0, r.clock.Pending())
	r.clock.Advance(10 * time.Minute)
	assert.Empty(t, r.stallNotices())
}

func TestStallRefusalMutesJob(t *testing.T) {
	stale := errors.New("stale conversation")
	r := newRig(t, withFakeClock(), withStall(time.Minute), withOptions(func(o *Options) {
		o.IsExpectedDrop = func(err error) bool { return errors.Is(err, stale) }
	}))
	owner := &refusingOwner{gen: r.gen(), err: stale}
	f := r.fifo()
	j := r.startFor(owner, f.command())

	r.clock.Advance(time.Minute)
	require.Equal(t, int32(1), owner.attempts.Load())
	r.emit(f, j, "again")
	waitPending(t, r, 1)
	r.clock.Advance(time.Minute)

	assert.Equal(t, int32(1), owner.attempts.Load(), "a muted job sends no more notices")
	assert.Empty(t, r.errors(), "an expected refusal is not reported")
}

func TestStallNoticeThenCompletionNotice(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	gate, open := r.gate("end")
	j := r.bash(gate)

	r.clock.Advance(time.Minute)
	require.Len(t, r.stallNotices(), 1)
	open()
	waitDone(t, j)

	waitFor(t, 5*time.Second, "completion notice", func() bool {
		n := 0
		for _, m := range r.n.followUps() {
			if strings.Contains(m.text, "finished [status:") {
				n++
			}
		}
		return n == 1
	})
	assert.Len(t, r.stallNotices(), 1)
	assert.Len(t, r.n.followUps(), 2)
}

// armedJob is a job with an armed stall timer state and no process.
func armedJob() *Job {
	tb := New(Options{StallAfter: time.Minute})
	j := &Job{t: tb, started: time.Now(), done: make(chan struct{})}
	j.stall.Store(stallArmed)
	return j
}

func TestNoteOutputDoesNotAllocate(t *testing.T) {
	j := armedJob()
	chunk := []byte("some output\n")

	allocs := testing.AllocsPerRun(1000, func() { j.noteOutput(sandbox.StreamStdout, chunk) })

	assert.Zero(t, allocs)
	assert.NotZero(t, j.lastOut.Load())
}

func BenchmarkNoteOutput(b *testing.B) {
	j := armedJob()
	chunk := []byte("some output\n")
	b.ReportAllocs()
	for b.Loop() {
		j.noteOutput(sandbox.StreamStdout, chunk)
	}
}

func TestChattyJobKeepsOneStallTimer(t *testing.T) {
	r := newRig(t, withFakeClock(), withStall(time.Minute))
	f := r.fifo()
	j := r.bash(f.command())

	for i := range 100 {
		r.clock.Advance(time.Second)
		r.emit(f, j, fmt.Sprintf("line %d", i))
		require.Equal(t, 1, r.clock.Pending(), "line %d", i)
	}
	assert.Empty(t, r.stallNotices())
}

func TestStallTimerReleasedAtEnd(t *testing.T) {
	t.Run("end", func(t *testing.T) {
		r := newRig(t, withFakeClock(), withStall(time.Minute))
		gate, open := r.gate("end")
		j := r.bash(gate)
		require.Equal(t, 1, r.clock.Pending())
		open()
		waitDone(t, j)
		waitPending(t, r, 0)
	})
	t.Run("kill", func(t *testing.T) {
		r := newRig(t, withFakeClock(), withStall(time.Minute))
		j := r.bash(r.gateForever())
		_, err := r.tb.Kill(r.gen(), j.ID(), "")
		require.NoError(t, err)
		waitPending(t, r, 0)
	})
	t.Run("close", func(t *testing.T) {
		r := newRig(t, withFakeClock(), withStall(time.Minute))
		r.bash(r.gateForever())
		require.Equal(t, 1, r.clock.Pending())
		require.NoError(t, r.tb.Close())
		waitPending(t, r, 0)
	})
}

func TestNoStallWorkWhenOff(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.bash(f.command())
	assert.Equal(t, 0, r.clock.Pending())

	f.line(t, "x")
	waitFor(t, 5*time.Second, "output in the buffer", func() bool { return j.Snapshot().Total > 0 })

	assert.Zero(t, j.lastOut.Load(), "no observe hook runs for a table without a stall notice")
}

func TestDurationText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Minute:             "1 minute",
		5 * time.Minute:         "5 minutes",
		time.Second:             "1 second",
		90 * time.Second:        "90 seconds",
		200 * time.Millisecond:  "200ms",
		1500 * time.Millisecond: "1.5s",
		time.Hour:               "60 minutes",
	} {
		assert.Equal(t, want, durationText(d))
	}
}
