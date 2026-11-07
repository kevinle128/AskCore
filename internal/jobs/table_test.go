//go:build unix

package jobs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/sandbox"
	"AskCore/pkg/protocol"
)

func TestWaitReturnsDoneAndRemovesJob(t *testing.T) {
	r := newRig(t)
	j := r.bash("echo hi")

	assert.Equal(t, Done, r.tb.Wait(background(), j.ID(), 5*time.Second))
	assert.Equal(t, StatusCompleted, j.Status())
	require.NotNil(t, j.Result().ExitCode)
	assert.Equal(t, 0, *j.Result().ExitCode)
	out := j.Snapshot()
	assert.Equal(t, "hi\n", string(out.Head))
	assert.Empty(t, r.tb.List(r.gen()))
	assert.Empty(t, r.n.all(), "a job whose end the caller saw sends no notice")
}

func TestWaitMovesToBackgroundAfterBudget(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	j := r.bash(command)

	start := time.Now()
	assert.Equal(t, Background, r.tb.Wait(background(), j.ID(), 100*time.Millisecond))
	assert.GreaterOrEqual(t, time.Since(start), 90*time.Millisecond)
	assert.Equal(t, StatusRunning, j.Status())
	require.Len(t, r.tb.List(r.gen()), 1)

	open()
	waitDone(t, j)
	waitFor(t, 2*time.Second, "completion notice", func() bool { return len(r.n.followUps()) == 1 })
}

func TestWaitZeroBudgetStartsInBackground(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	j := r.bash(command)

	start := time.Now()
	assert.Equal(t, Background, r.tb.Wait(background(), j.ID(), 0))
	assert.Less(t, time.Since(start), 50*time.Millisecond)
	assert.Equal(t, StatusRunning, j.Status())
	open()
	waitDone(t, j)
}

func TestWaitCancelledKillsAndWaits(t *testing.T) {
	r := newRig(t)
	command, _ := r.gate("never")
	j := r.bash(command)

	ctx, cancel := context.WithCancel(background())
	time.AfterFunc(50*time.Millisecond, cancel)
	assert.Equal(t, Cancelled, r.tb.Wait(ctx, j.ID(), time.Minute))

	select {
	case <-j.Done():
	default:
		t.Fatal("Wait returned before the job ended")
	}
	assert.Equal(t, StatusKilled, j.Status())
	assert.Empty(t, r.tb.List(r.gen()))
	requireGroupGone(t, j.proc.PGID())
	assert.Empty(t, r.n.all(), "a cancelled call sends no notice")
}

func TestWaitUnknownJob(t *testing.T) {
	r := newRig(t)
	assert.Equal(t, NotFound, r.tb.Wait(background(), "bash-9", time.Second))
}

func TestJobEndingAtBudgetIsReportedOnce(t *testing.T) {
	r := newRig(t)
	const runs = 200

	outcomes := make(map[string]WaitOutcome, runs)
	for i := range runs {
		// The budget sits around the time that a shell needs to exit, so the
		// end and the budget race.
		j := r.bash("exit 0")
		budget := time.Duration(1+i%6) * time.Millisecond
		outcomes[j.ID()] = r.tb.Wait(background(), j.ID(), budget)
	}

	moved := 0
	for _, o := range outcomes {
		if o == Background {
			moved++
		}
	}
	waitFor(t, 5*time.Second, "every background job notice", func() bool {
		return len(r.n.followUps()) >= moved
	})
	// Let a wrong second notice arrive before the count is checked.
	time.Sleep(100 * time.Millisecond)

	counts := map[string]int{}
	for _, m := range r.n.followUps() {
		for id := range outcomes {
			if strings.Contains(m.text, "Background job "+id+" ") {
				counts[id]++
			}
		}
	}
	for id, o := range outcomes {
		switch o {
		case Done:
			assert.Zero(t, counts[id], "%s was reported as a result and as a notice", id)
		case Background:
			assert.Equal(t, 1, counts[id], "%s must get exactly one notice", id)
		default:
			t.Fatalf("%s: unexpected outcome %v", id, o)
		}
	}
}

func TestCapRefusesEleventhActiveJob(t *testing.T) {
	r := newRig(t)
	opens := make([]func(), 10)
	jobs := make([]*Job, 10)
	for i := range jobs {
		command, open := r.gate(fmt.Sprintf("go-%d", i))
		opens[i] = open
		jobs[i] = r.bash(command)
	}

	_, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: "echo no", Cwd: r.dir})
	require.ErrorIs(t, err, ErrTooManyJobs)
	assert.EqualError(t, err, "Too many active jobs (10). Stop one with job_kill or wait for one to finish.")

	// A finished job does not count, even when nobody read it.
	opens[0]()
	waitDone(t, jobs[0])
	j := r.bash("echo yes")
	assert.Equal(t, Done, r.tb.Wait(background(), j.ID(), 5*time.Second))

	for _, open := range opens {
		open()
	}
}

func TestCapCountsMonitors(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	for range 9 {
		r.bash(command)
	}
	r.monitor(MonitorSpec{Command: command, Persistent: true})

	_, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: "echo no", Cwd: r.dir})
	require.ErrorIs(t, err, ErrTooManyJobs)
	_, err = r.tb.StartMonitor(MonitorSpec{Owner: r.owner(), Command: "echo no", Cwd: r.dir, Description: "x", Persistent: true})
	require.ErrorIs(t, err, ErrTooManyJobs)
	open()
}

func TestJobIDsAndLabels(t *testing.T) {
	r := newRig(t)
	long := strings.Repeat("x", 120)
	a := r.bash("echo first\necho second")
	b := r.bash(long)
	m := r.monitor(MonitorSpec{Command: "echo m", Persistent: true})

	assert.Equal(t, "bash-1", a.ID())
	assert.Equal(t, "bash-2", b.ID())
	assert.Equal(t, "monitor-1", m.ID())
	assert.Equal(t, KindBash, a.Kind())
	assert.Equal(t, KindMonitor, m.Kind())
	assert.Equal(t, "echo first", a.Label())
	assert.Equal(t, strings.Repeat("x", 80), b.Label())
	for _, j := range []*Job{a, b, m} {
		waitDone(t, j)
	}
}

func TestListReportsJobs(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	running := r.bash(command)
	finished := r.bash("exit 3")
	waitDone(t, finished)

	infos := r.tb.List(r.gen())
	require.Len(t, infos, 2)
	assert.Equal(t, running.ID(), infos[0].ID)
	assert.Equal(t, KindBash, infos[0].Kind)
	assert.Equal(t, StatusRunning, infos[0].Status)
	assert.Equal(t, finished.ID(), infos[1].ID)
	assert.Equal(t, StatusCompleted, infos[1].Status)
	require.NotNil(t, infos[1].ExitCode)
	assert.Equal(t, 3, *infos[1].ExitCode)
	open()
}

func TestListElapsedFollowsClock(t *testing.T) {
	r := newRig(t, withFakeClock())
	command, open := r.gate("go")
	r.bash(command)

	r.clock.Advance(12 * time.Second)
	infos := r.tb.List(r.gen())
	require.Len(t, infos, 1)
	assert.Equal(t, 12*time.Second, infos[0].Elapsed)
	open()
}

func TestKillByModelSendsNoNotice(t *testing.T) {
	r := newRig(t)
	command, _ := r.gate("never")
	j := r.bash(command)

	got, err := r.tb.Kill(r.gen(), j.ID(), "no longer needed")
	require.NoError(t, err)
	assert.Same(t, j, got)
	assert.Equal(t, StatusKilled, j.Status())
	assert.Equal(t, "[status: killed, reason: no longer needed]", j.StatusLine())
	assert.Empty(t, r.tb.List(r.gen()))
	requireGroupGone(t, j.proc.PGID())
	assert.Empty(t, r.n.all())
	assert.Empty(t, r.errors())

	_, err = r.tb.Kill(r.gen(), j.ID(), "")
	assert.ErrorIs(t, err, ErrUnknownJob)
}

func TestKillShowsStoppingStatus(t *testing.T) {
	r := newRig(t)
	// The shell ignores SIGTERM, so the job stays in the stopping state until
	// the grace ends.
	j := r.bash(ignoreTerm)
	waitReady(t, j)

	killed := make(chan struct{})
	go func() {
		defer close(killed)
		_, _ = r.tb.Kill(r.gen(), j.ID(), "stop")
	}()
	waitFor(t, 2*time.Second, "stopping status", func() bool { return j.Status() == StatusStopping })
	infos := r.tb.List(r.gen())
	require.Len(t, infos, 1)
	assert.Equal(t, StatusStopping, infos[0].Status)
	<-killed
	assert.Equal(t, StatusKilled, j.Status())
}

func TestBackgroundJobEndSendsFollowUpNotice(t *testing.T) {
	r := newRig(t)
	j := r.bash("exit 1")

	assert.Equal(t, Background, r.tb.Wait(background(), j.ID(), 0))
	waitFor(t, 5*time.Second, "completion notice", func() bool { return len(r.n.followUps()) == 1 })

	msg := r.n.followUps()[0]
	want := "Background job bash-1 (bash: exit 1) finished [status: completed, exit code: 1]. Read its output with job_output."
	assert.Equal(t, protocol.SystemReminder(want), msg.text)
	assert.Equal(t, "user", msg.message.Role())
	require.Len(t, msg.message.Content, 1)
	assert.True(t, protocol.IsSystemReminder(msg.message))
	assert.Empty(t, r.n.steers())
}

func TestNoNoticeAfterAwaitedRead(t *testing.T) {
	r := newRig(t)
	command, open := r.gate("go")
	j := r.bash(command)
	time.AfterFunc(100*time.Millisecond, open)

	res, err := r.tb.Read(background(), r.gen(), j.ID(), true, 10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, res.Status)
	waitDone(t, j)
	assert.Empty(t, r.n.all())
	assert.Empty(t, r.errors())
}

func TestNoNoticeWhileClosing(t *testing.T) {
	r := newRig(t)
	command, _ := r.gate("never")
	for range 3 {
		r.bash(command)
	}
	require.NoError(t, r.tb.Close())
	assert.Empty(t, r.n.all())
	assert.Empty(t, r.errors())
}

func TestRefusedNoticeGoesToOnErrorAndJobStaysReadable(t *testing.T) {
	r := newRig(t)
	refusal := fmt.Errorf("queue is full")
	r.n.err = refusal
	j := r.bash("echo kept")

	assert.Equal(t, Background, r.tb.Wait(background(), j.ID(), 0))
	waitFor(t, 5*time.Second, "OnError", func() bool { return len(r.errors()) == 1 })
	err := r.errors()[0]
	assert.ErrorIs(t, err, refusal)
	assert.Contains(t, err.Error(), "bash-1")

	res, readErr := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
	require.NoError(t, readErr)
	assert.Equal(t, "kept\n", res.Output)
	assert.Equal(t, StatusCompleted, res.Status)
}

func TestNoticeBeforeNotifierSetIsReported(t *testing.T) {
	r := newRig(t, withoutNotifier())
	j := r.bash("echo early")

	assert.Equal(t, Background, r.tb.Wait(background(), j.ID(), 0))
	waitFor(t, 5*time.Second, "OnError", func() bool { return len(r.errors()) == 1 })
	assert.ErrorIs(t, r.errors()[0], ErrNoOwner)
	assert.Contains(t, r.errors()[0].Error(), "bash-1")
	assert.Empty(t, r.n.all())
}

func TestCloseKillsAllJobsWithSharedGrace(t *testing.T) {
	r := newRig(t)
	// Each job writes enough to open the spill file, then ignores SIGTERM.
	command := "head -c 100000 /dev/zero | tr '\\0' x; " + ignoreTerm
	jobs := make([]*Job, 10)
	for i := range jobs {
		jobs[i] = r.bash(command)
	}
	for _, j := range jobs {
		waitReady(t, j)
		require.NotEmpty(t, j.Snapshot().SpillPath)
	}
	dir := r.tb.spillDirPath()
	require.NotEmpty(t, dir)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	start := time.Now()
	require.NoError(t, r.tb.Close())
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 7*time.Second)
	assert.Greater(t, elapsed, time.Second, "the jobs ignore SIGTERM, so the close waits for the grace")

	for _, j := range jobs {
		requireGroupGone(t, j.proc.PGID())
		select {
		case <-j.Done():
		default:
			t.Fatalf("%s is not done after Close", j.ID())
		}
	}
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "the spill directory is removed")

	// A second call is a no-op.
	require.NoError(t, r.tb.Close())
}

func TestKillRemainingSendsSigkillWithoutWait(t *testing.T) {
	r := newRig(t)
	jobs := make([]*Job, 3)
	for i := range jobs {
		jobs[i] = r.bash(ignoreTerm)
	}
	for _, j := range jobs {
		waitReady(t, j)
	}

	start := time.Now()
	r.tb.KillRemaining()
	assert.Less(t, time.Since(start), 50*time.Millisecond)
	for _, j := range jobs {
		waitDone(t, j)
		requireGroupGone(t, j.proc.PGID())
	}
	// The sweep ends the session of the table: no notice reaches an Agent
	// that is going away.
	assert.Empty(t, r.n.all())
	_, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: "true", Cwd: r.dir})
	assert.ErrorIs(t, err, ErrClosed)
}

func TestStartAfterCloseFails(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.tb.Close())

	_, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: "true", Cwd: r.dir})
	assert.ErrorIs(t, err, ErrClosed)
	_, err = r.tb.StartMonitor(MonitorSpec{Owner: r.owner(), Command: "true", Cwd: r.dir, Description: "x"})
	assert.ErrorIs(t, err, ErrClosed)
}

func TestUnusedTableOwnsNoFileOrGoroutine(t *testing.T) {
	tb := New(Options{})
	require.NoError(t, tb.Close())
	assert.Empty(t, tb.spillDirPath(), "no spill directory before a spill")
	goleak.VerifyNone(t)
}

func TestStartRejectsBadSpec(t *testing.T) {
	r := newRig(t)
	_, err := r.tb.Start(StartSpec{Owner: r.owner(), Command: "", Cwd: r.dir})
	assert.Error(t, err)
	_, err = r.tb.Start(StartSpec{Owner: r.owner(), Command: "true", Cwd: ""})
	assert.Error(t, err)
	_, err = r.tb.Start(StartSpec{Owner: r.owner(), Kind: KindMonitor, Command: "true", Cwd: r.dir})
	assert.Error(t, err)
	assert.Empty(t, r.tb.List(r.gen()))

	// A refused start does not use up an id.
	j := r.bash("true")
	assert.Equal(t, "bash-1", j.ID())
}

func TestJobUsesEnvOption(t *testing.T) {
	var calls int
	var mu sync.Mutex
	r := newRig(t, withOptions(func(o *Options) {
		o.Env = func() []string {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return sandbox.ChildEnv([]string{"PATH=/usr/bin:/bin"}, map[string]string{"ASK_MODEL": "m1"})
		}
	}))
	j := r.bash(`echo "$ASK_MODEL"`)
	assert.Equal(t, Done, r.tb.Wait(background(), j.ID(), 5*time.Second))
	assert.Equal(t, "m1\n", string(j.Snapshot().Head))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, calls)
}

func TestSpillFilesLiveInOneDirectoryUntilClose(t *testing.T) {
	r := newRig(t)
	j := r.bash("head -c 100000 /dev/zero | tr '\\0' x")
	assert.Equal(t, Done, r.tb.Wait(background(), j.ID(), 10*time.Second))

	path := j.Snapshot().SpillPath
	require.NotEmpty(t, path)
	info, err := os.Stat(path)
	require.NoError(t, err, "the spill file of a finished job stays until close")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.True(t, strings.HasSuffix(path, "bash-1.log"), path)

	require.NoError(t, r.tb.Close())
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestNoticeRechecksReadAndCloseAfterTimerStop(t *testing.T) {
	for _, action := range []string{"read", "close"} {
		t.Run(action, func(t *testing.T) {
			r := newRig(t)
			command, open := r.gate("finish")
			j := r.monitor(MonitorSpec{Command: command, Persistent: true})
			stopping, release := make(chan struct{}), make(chan struct{})
			r.tb.mu.Lock()
			j.stopTimeout = func() bool { close(stopping); <-release; return true }
			r.tb.mu.Unlock()
			open()
			select {
			case <-stopping:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("timeout stop did not start")
			}
			if action == "read" {
				_, err := r.tb.Read(background(), r.gen(), j.ID(), false, 0)
				assert.NoError(t, err)
			} else {
				r.tb.KillRemaining()
			}
			close(release)
			r.tb.owners.Wait()
			assert.Empty(t, r.n.all())
		})
	}
}

func TestKillRemainingRemovesSpillDirectory(t *testing.T) {
	r := newRig(t)
	j := r.bash("head -c 100000 /dev/zero; " + ignoreTerm)
	waitReady(t, j)
	dir := r.tb.spillDirPath()
	require.NotEmpty(t, dir)
	r.tb.KillRemaining()
	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err))
	waitDone(t, j)
}
