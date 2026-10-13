//go:build unix

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireGroupGone polls kill(-pgid, 0) until it reports ESRCH.
func requireGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(-pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still exists: kill(-pgid, 0) = %v", pgid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// startShell starts command with bash in a fresh directory. The cleanup kills
// the group so that a failed test leaves no process behind.
func startShell(t *testing.T, command string, mutate ...func(*Spec)) *Process {
	t.Helper()
	spec := Spec{
		Argv: ShellArgv(command),
		Dir:  t.TempDir(),
		Env:  ChildEnv(os.Environ(), nil),
	}
	for _, m := range mutate {
		m(&spec)
	}
	p, err := Start(spec)
	require.NoError(t, err)
	t.Cleanup(func() {
		p.KillNow()
		<-p.Done()
	})
	return p
}

func waitDone(t *testing.T, p *Process, limit time.Duration) {
	t.Helper()
	select {
	case <-p.Done():
	case <-time.After(limit):
		t.Fatalf("process did not finish within %s", limit)
	}
}

func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", limit, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitOutput(t *testing.T, p *Process, want string) {
	t.Helper()
	waitFor(t, 5*time.Second, fmt.Sprintf("output %q", want), func() bool {
		out := p.Snapshot()
		return strings.Contains(string(out.Head)+string(out.Tail), want)
	})
}

func outputText(out Output) string {
	return string(out.Head) + string(out.Tail)
}

func TestRunnerReportsExitCodeAndOutput(t *testing.T) {
	p := startShell(t, "echo hi; echo oops >&2; exit 3")
	waitDone(t, p, 5*time.Second)
	requireGroupGone(t, p.PGID())

	res := p.Result()
	require.NotNil(t, res.ExitCode)
	assert.Equal(t, 3, *res.ExitCode)
	assert.Empty(t, res.Signal)
	assert.NoError(t, res.Err)
	out := p.Snapshot()
	assert.Contains(t, outputText(out), "hi\n")
	assert.Contains(t, outputText(out), "oops\n")
	assert.Equal(t, int64(len("hi\noops\n")), out.Total)
	assert.Zero(t, out.Dropped)
}

func TestRunnerUsesDirAndClosedStdin(t *testing.T) {
	dir := t.TempDir()
	p := startShell(t, "pwd -P; cat", func(s *Spec) { s.Dir = dir })
	waitDone(t, p, 2*time.Second)
	requireGroupGone(t, p.PGID())

	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, want+"\n", outputText(p.Snapshot()))
}

func TestStartRejectsBadSpec(t *testing.T) {
	_, err := Start(Spec{Dir: t.TempDir()})
	assert.Error(t, err, "empty argv")
	_, err = Start(Spec{Argv: []string{"/bin/true"}})
	assert.Error(t, err, "empty dir")
	_, err = Start(Spec{Argv: []string{"/nonexistent/ask-sandbox-test"}, Dir: t.TempDir()})
	assert.Error(t, err, "missing executable")
}

func newSpillFile(dir string) func() (*os.File, error) {
	return func() (*os.File, error) {
		return os.OpenFile(filepath.Join(dir, "spill.log"), os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	}
}

func TestRunnerSpillStartsOnOverflowWithMode0600(t *testing.T) {
	spillDir := t.TempDir()
	p := startShell(t, "seq 1 40000", func(s *Spec) { s.SpillFile = newSpillFile(spillDir) })
	waitDone(t, p, 10*time.Second)
	requireGroupGone(t, p.PGID())

	var want strings.Builder
	for i := 1; i <= 40000; i++ {
		want.WriteString(strconv.Itoa(i))
		want.WriteByte('\n')
	}
	out := p.Snapshot()
	require.NoError(t, out.SpillErr)
	require.NotEmpty(t, out.SpillPath)
	assert.False(t, out.SpillCapped)
	info, err := os.Stat(out.SpillPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	got, err := os.ReadFile(out.SpillPath)
	require.NoError(t, err)
	assert.Equal(t, want.String(), string(got), "spill file must hold every byte")
	assert.Equal(t, int64(want.Len()), out.Total)
	assert.Equal(t, out.Total-int64(len(out.Head))-int64(len(out.Tail)), out.Dropped)
}

func TestRunnerWithoutSpillFileKeepsHeadAndTailOnly(t *testing.T) {
	p := startShell(t, "seq 1 40000")
	waitDone(t, p, 10*time.Second)
	out := p.Snapshot()
	assert.Empty(t, out.SpillPath)
	assert.NoError(t, out.SpillErr)
	assert.Positive(t, out.Dropped)
}

func TestRunnerSpillOpenErrorKeepsProcess(t *testing.T) {
	boom := errors.New("disk is full")
	p := startShell(t, "seq 1 40000", func(s *Spec) {
		s.SpillFile = func() (*os.File, error) { return nil, boom }
	})
	waitDone(t, p, 10*time.Second)
	out := p.Snapshot()
	assert.ErrorIs(t, out.SpillErr, boom)
	assert.Empty(t, out.SpillPath)
	require.NotNil(t, p.Result().ExitCode)
	assert.Equal(t, 0, *p.Result().ExitCode)
}

func TestRunnerSpillStopsAtCap(t *testing.T) {
	const limit = 1 << 20
	spillDir := t.TempDir()
	p := startShell(t, "yes", func(s *Spec) {
		s.SpillFile = newSpillFile(spillDir)
		s.spillLimit = limit
	})
	waitFor(t, 10*time.Second, "spill cap", func() bool { return p.Snapshot().SpillCapped })

	select {
	case <-p.Done():
		t.Fatal("the process must keep running after the spill cap")
	default:
	}
	out := p.Snapshot()
	require.NoError(t, out.SpillErr)
	info, err := os.Stat(out.SpillPath)
	require.NoError(t, err)
	assert.Equal(t, int64(limit), info.Size())

	p.Kill()
	waitDone(t, p, 5*time.Second)
	requireGroupGone(t, p.PGID())
	info, err = os.Stat(out.SpillPath)
	require.NoError(t, err)
	assert.Equal(t, int64(limit), info.Size(), "file size never passes the cap")
}

func TestRunnerKillStopsGroupWithGrandchild(t *testing.T) {
	p := startShell(t, "sleep 300 & echo ready; sleep 300")
	waitOutput(t, p, "ready")

	p.Kill()
	waitDone(t, p, 5*time.Second)
	requireGroupGone(t, p.PGID())
	assert.Equal(t, "SIGTERM", p.Result().Signal)
	assert.Nil(t, p.Result().ExitCode)
}

func TestRunnerKillEscalatesWhenTermIgnored(t *testing.T) {
	p := startShell(t, "trap '' TERM; echo ready; while :; do echo x; done")
	waitOutput(t, p, "ready")

	start := time.Now()
	p.Kill()
	waitDone(t, p, 4*time.Second)
	assert.GreaterOrEqual(t, time.Since(start), 2*time.Second, "SIGKILL waits for the grace")
	requireGroupGone(t, p.PGID())
	assert.Equal(t, "SIGKILL", p.Result().Signal)
}

func TestRunnerKillReachesGrandchildThatIgnoresTerm(t *testing.T) {
	p := startShell(t, "(trap '' TERM; sleep 300) & echo ready; wait")
	waitOutput(t, p, "ready")

	p.Kill()
	waitDone(t, p, 5*time.Second)
	requireGroupGone(t, p.PGID())
}

func TestRunnerKillTimerFollowsClock(t *testing.T) {
	clock := newFakeClock()
	p := startShell(t, "trap '' TERM; echo ready; while :; do sleep 0.05; done",
		func(s *Spec) { s.Clock = clock })
	waitOutput(t, p, "ready")

	p.Kill()
	p.Kill() // idempotent: one signal, one timer
	assert.Equal(t, 1, clock.Pending())
	select {
	case <-p.Done():
		t.Fatal("process ended before the grace passed")
	case <-time.After(300 * time.Millisecond):
	}

	clock.Advance(killGrace)
	waitDone(t, p, 3*time.Second)
	requireGroupGone(t, p.PGID())
	assert.Equal(t, "SIGKILL", p.Result().Signal)
}

func TestRunnerDrainIsBoundedWhenGrandchildHoldsPipe(t *testing.T) {
	start := time.Now()
	p := startShell(t, "(sleep 30 &); echo started")
	waitDone(t, p, 6*time.Second)
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 2*time.Second)
	assert.Less(t, elapsed, 5*time.Second)
	assert.Equal(t, "started\n", outputText(p.Snapshot()))
	requireGroupGone(t, p.PGID())
}

func TestRunnerDrainBoundFollowsClock(t *testing.T) {
	clock := newFakeClock()
	p := startShell(t, "(sleep 30 &); echo started", func(s *Spec) { s.Clock = clock })
	waitFor(t, 5*time.Second, "drain timer", func() bool { return clock.Pending() == 1 })
	select {
	case <-p.Done():
		t.Fatal("Done closed while a grandchild holds the pipe")
	case <-time.After(300 * time.Millisecond):
	}

	clock.Advance(drainBound)
	waitDone(t, p, 3*time.Second)
	assert.Equal(t, "started\n", outputText(p.Snapshot()))
	requireGroupGone(t, p.PGID())
}

func TestRunnerBackgroundChildWithoutPipeIsSweptAtExit(t *testing.T) {
	p := startShell(t, "sleep 300 >/dev/null 2>&1 & echo started")
	waitDone(t, p, 5*time.Second)
	requireGroupGone(t, p.PGID())
}

func TestRunnerKillNowSendsSigkillWithoutWait(t *testing.T) {
	p := startShell(t, "trap '' TERM; sleep 300 & echo ready; wait")
	waitOutput(t, p, "ready")

	start := time.Now()
	p.KillNow()
	assert.Less(t, time.Since(start), 50*time.Millisecond)
	waitDone(t, p, 3*time.Second)
	requireGroupGone(t, p.PGID())
	assert.Equal(t, "SIGKILL", p.Result().Signal)
}

func TestRunnerSignalsAfterDoneAreNoOps(t *testing.T) {
	p := startShell(t, "exit 0")
	waitDone(t, p, 5*time.Second)
	p.Kill()
	p.KillNow()
	p.Kill()
	require.NotNil(t, p.Result().ExitCode)
	assert.Equal(t, 0, *p.Result().ExitCode)
}

func TestRunnerFloodStaysBounded(t *testing.T) {
	var base runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&base)

	p := startShell(t, "yes")
	var peak uint64
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		peak = max(peak, m.HeapAlloc)
		out := p.Snapshot() // snapshots must keep working under a flood
		assert.LessOrEqual(t, len(out.Head)+len(out.Tail), headSize+tailSize)
		time.Sleep(100 * time.Millisecond)
	}
	assert.Less(t, int64(peak)-int64(base.HeapAlloc), int64(16<<20))

	start := time.Now()
	p.Kill()
	waitDone(t, p, 3*time.Second)
	assert.Less(t, time.Since(start), time.Second)
	requireGroupGone(t, p.PGID())
	out := p.Snapshot()
	assert.Greater(t, out.Total, int64(1<<20))
	assert.Equal(t, out.Total-int64(len(out.Head))-int64(len(out.Tail)), out.Dropped)
}

func TestRunnerObserveSeesEveryChunkInOrder(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	p := startShell(t, "printf a; sleep 0.1; printf b >&2; sleep 0.1; printf c", func(s *Spec) {
		s.Observe = func(stream Stream, chunk []byte) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, fmt.Sprintf("%s:%s", stream, chunk))
		}
	})
	waitDone(t, p, 5*time.Second)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"stdout:a", "stderr:b", "stdout:c"}, seen)
}

func TestRunnerReadFromFollowsOutput(t *testing.T) {
	p := startShell(t, "printf one; sleep 0.2; printf two")
	waitOutput(t, p, "one")
	data, next, skipped := p.ReadFrom(0)
	assert.Equal(t, "one", string(data))
	assert.Zero(t, skipped)

	waitDone(t, p, 5*time.Second)
	data, next, skipped = p.ReadFrom(next)
	assert.Equal(t, "two", string(data))
	assert.Equal(t, int64(6), next)
	assert.Zero(t, skipped)
}

func TestRunnerChildEnvIsUsed(t *testing.T) {
	p := startShell(t, `printf '%s|%s' "$TERM" "${OPENAI_API_KEY-unset}"`, func(s *Spec) {
		s.Env = ChildEnv([]string{"PATH=" + os.Getenv("PATH"), "OPENAI_API_KEY=sk-test", "TERM=xterm"}, nil)
	})
	waitDone(t, p, 5*time.Second)
	assert.Equal(t, "dumb|unset", outputText(p.Snapshot()))
}

func TestRunCancelledContextKillsAndWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	spec := Spec{
		Argv: ShellArgv("echo $$; sleep 300"),
		Dir:  t.TempDir(),
		Env:  ChildEnv(os.Environ(), nil),
		Observe: func(Stream, []byte) {
			once.Do(func() { close(ready) })
		},
	}
	type result struct {
		out Output
		res Result
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		out, res, err := Run(ctx, spec)
		resCh <- result{out, res, err}
	}()

	<-ready
	cancel()
	var r result
	select {
	case r = <-resCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}

	assert.ErrorIs(t, r.err, context.Canceled)
	assert.Equal(t, "SIGTERM", r.res.Signal)
	pgid, err := strconv.Atoi(strings.TrimSpace(outputText(r.out)))
	require.NoError(t, err)
	requireGroupGone(t, pgid)
}

func TestRunReturnsOutputAndResult(t *testing.T) {
	out, res, err := Run(context.Background(), Spec{
		Argv: ShellArgv("echo found; exit 1"),
		Dir:  t.TempDir(),
		Env:  ChildEnv(os.Environ(), nil),
	})
	require.NoError(t, err)
	assert.Equal(t, "found\n", outputText(out))
	require.NotNil(t, res.ExitCode)
	assert.Equal(t, 1, *res.ExitCode)
}

func TestRunReportsStartError(t *testing.T) {
	_, _, err := Run(context.Background(), Spec{Argv: []string{"/nonexistent/ask-sandbox-test"}, Dir: t.TempDir()})
	assert.Error(t, err)
}
