//go:build unix

package jobs

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadRunningJobAnswersAtOnce(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	j := r.bash("echo start; " + command)
	waitFor(t, 5*time.Second, "first line", func() bool { return j.Snapshot().Total > 0 })

	start := time.Now()
	res, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 50*time.Millisecond)

	assert.True(t, res.Running)
	assert.Equal(t, StatusRunning, res.Status)
	assert.Equal(t, "start\n", res.Output)
	assert.Contains(t, res.Text, "[status: running]")
	assert.Contains(t, res.Text, "The job is still running. A notice will arrive when it finishes; do not poll.")
	assert.Len(t, r.tb.List(r.gen()), 1, "a running job stays in the table")
	open()
}

func TestReadAdvancesCursorAndReportsSkippedBytes(t *testing.T) {
	r := newRig(t)
	gate1, open1 := r.gate("one")
	gate2, open2 := r.gate("two")
	j := r.bash("echo first; " + gate1 + "; echo second; " + gate2 + "; head -c 200000 /dev/zero | tr '\\0' z")

	waitFor(t, 5*time.Second, "first", func() bool { return j.Snapshot().Total >= 6 })
	res, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, "first\n", res.Output)
	assert.Zero(t, res.Skipped)

	// Nothing new: the cursor moved.
	res, err = r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Empty(t, res.Output)

	open1()
	waitFor(t, 5*time.Second, "second", func() bool { return j.Snapshot().Total >= 13 })
	res, err = r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, "second\n", res.Output)

	open2()
	waitDone(t, j)
	res, err = r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, res.Status)

	// 200 KB do not fit in memory: the note names the gap and the spill file.
	assert.Positive(t, res.Skipped)
	require.NotEmpty(t, res.SpillPath)
	_, statErr := os.Stat(res.SpillPath)
	require.NoError(t, statErr)
	note := regexp.MustCompile(`\[(\d+) bytes not kept in memory; see (\S+)\]`).FindStringSubmatch(res.Text)
	require.NotNil(t, note, res.Text)
	assert.Equal(t, res.SpillPath, note[2])
	assert.Contains(t, res.Text, "[status: completed, exit code: 0]")
	// The kept bytes are the head and the tail around the note.
	assert.Equal(t, 200000-int(res.Skipped), strings.Count(res.Output, "z"))
}

func TestReadWithWaitReturnsFinalStatusAndRemovesJob(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	j := r.bash("echo done; " + command)
	time.AfterFunc(100*time.Millisecond, open)

	res, err := r.tb.Read(background(), r.gen(), j.ID(), true, 10*time.Second)
	require.NoError(t, err)
	assert.False(t, res.Running)
	assert.Equal(t, StatusCompleted, res.Status)
	assert.Equal(t, "done\n", res.Output)
	assert.Contains(t, res.Text, "[status: completed, exit code: 0]")
	assert.NotContains(t, res.Text, "do not poll")
	assert.Empty(t, r.tb.List(r.gen()))

	_, err = r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	assert.ErrorIs(t, err, ErrUnknownJob)
}

func TestReadWaitTimeoutCapsAt600Seconds(t *testing.T) {
	r := newRig(t, withFakeClock())
	command, open := r.gate("go")
	j := r.bash(command)

	read := func(timeout time.Duration) <-chan ReadResult {
		out := make(chan ReadResult, 1)
		go func() {
			res, err := r.tb.Read(background(), r.gen(), j.ID(), true, timeout)
			assert.NoError(t, err)
			out <- res
		}()
		waitFor(t, 5*time.Second, "read timer", func() bool { return r.clock.Pending() == 1 })
		return out
	}

	out := read(900 * time.Second)
	r.clock.Advance(599 * time.Second)
	select {
	case <-out:
		t.Fatal("Read returned before the 600 s cap")
	case <-time.After(50 * time.Millisecond):
	}
	r.clock.Advance(time.Second)
	res := <-out
	assert.True(t, res.Running)
	assert.Contains(t, res.Text, "[status: running]")

	// A timeout of zero or less selects the default of 30 s.
	out = read(0)
	r.clock.Advance(29 * time.Second)
	select {
	case <-out:
		t.Fatal("Read returned before the default timeout")
	case <-time.After(50 * time.Millisecond):
	}
	r.clock.Advance(time.Second)
	assert.True(t, (<-out).Running)
	open()
}

func TestReadWithWaitStopsWhenContextEnds(t *testing.T) {
	r := newRig(t)
	command, _ := r.gate("never")
	j := r.bash(command)

	ctx, cancel := context.WithCancel(background())
	time.AfterFunc(50*time.Millisecond, cancel)
	res, err := r.tb.Read(ctx, r.gen(), j.ID(), true, time.Minute)
	require.NoError(t, err)
	assert.True(t, res.Running, "ending the read does not end the job")
	assert.Len(t, r.tb.List(r.gen()), 1)
}

func TestReadUnknownJob(t *testing.T) {
	r := newRig(t)
	_, err := r.tb.Read(background(), r.gen(), "bash-1", false, 0)
	assert.ErrorIs(t, err, ErrUnknownJob)
}

func TestReadReportsKilledAndFailedStatus(t *testing.T) {
	r := newRig(t)
	killedByShell := r.bash("kill -KILL $$")
	waitDone(t, killedByShell)
	res, err := r.tb.Read(background(), r.gen(), killedByShell.ID(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, res.Status)
	assert.Contains(t, res.Text, "[status: failed, signal: SIGKILL]")

	command, _ := r.gate("never")
	running := r.bash(command)
	_, err = r.tb.Kill(r.gen(), running.ID(), "enough")
	require.NoError(t, err)
	assert.Equal(t, "[status: killed, reason: enough]", running.StatusLine())
}

func TestCancelledReadKeepsUnreadOutput(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("finish")
	j := r.bash("echo kept; " + command)
	waitFor(t, 5*time.Second, "output", func() bool { return j.Snapshot().Total > 0 })
	ctx, cancel := context.WithCancel(background())
	cancel()
	_, err := r.tb.Read(ctx, r.gen(), j.ID(), true, time.Minute)
	require.NoError(t, err)
	res, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, "kept\n", res.Output)
	open()
}

func TestCancelledReadKeepsFinishedJob(t *testing.T) {
	r := newRig(t)
	j := r.bash("echo kept")
	waitDone(t, j)
	ctx, cancel := context.WithCancel(background())
	cancel()
	_, err := r.tb.Read(ctx, r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	res, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, "kept\n", res.Output)
	assert.Equal(t, StatusCompleted, res.Status)
}

func TestReadReportsCappedSpillAfterCursorPassesGap(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("finish")
	j := r.bash("head -c 68157440 /dev/zero; echo ready; " + command)
	waitReady(t, j)
	first, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.True(t, first.SpillCapped)
	assert.Contains(t, first.Text, "[spill file capped at 64 MB]")
	second, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Zero(t, second.Skipped)
	assert.True(t, second.SpillCapped)
	assert.Contains(t, second.Text, "[spill file capped at 64 MB]")
	open()
}

func TestCancelledReadAtCompletionRestoresNotice(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("finish")
	j := r.monitor(MonitorSpec{Command: "echo kept; " + command, Persistent: true})
	stopping, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	r.tb.mu.Lock()
	j.stopTimeout = func() bool { close(stopping); <-release; return true }
	r.tb.mu.Unlock()
	ctx, cancel := context.WithCancel(background())
	defer cancel()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		_, err := r.tb.Read(ctx, r.gen(), j.ID(), true, time.Minute)
		assert.NoError(t, err)
	}()
	waitFor(t, 5*time.Second, "reader", func() bool {
		r.tb.mu.Lock()
		defer r.tb.mu.Unlock()
		return j.waiters == 1
	})
	// Hold the table lock until both completion and cancellation are ready.
	r.tb.mu.Lock()
	cancel()
	open()
	<-j.proc.Done()
	r.tb.mu.Unlock()
	<-readDone
	<-stopping
	close(release)
	waitFor(t, 5*time.Second, "completion notice", func() bool { return len(r.n.followUps()) == 1 })
	res, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, err)
	assert.Equal(t, "kept\n", res.Output)
}
