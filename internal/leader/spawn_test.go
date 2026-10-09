package leader

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// fakeAsk writes an executable script named "ask" that behaves as body says.
func fakeAsk(t *testing.T, dir, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, "ask")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755))
	return path
}

func versionScript(build string, proto int) string {
	return fmt.Sprintf(`if [ "$1" = version ]; then echo '{"name":"ask","build":%q,"leaderProtocol":%d,"acpVersion":1}'; exit 0; fi; exit 3`, build, proto)
}

func TestLockHeld(t *testing.T) {
	p := testPaths(t)
	held, err := LockHeld(p)
	require.NoError(t, err)
	require.False(t, held, "no lock file")

	l, err := Acquire(p)
	require.NoError(t, err)
	held, err = LockHeld(p)
	require.NoError(t, err)
	require.True(t, held)
	require.NoError(t, l.Release())

	held, err = LockHeld(p)
	require.NoError(t, err)
	require.False(t, held)
}

func TestLockHeldDoesNotBlockAnAcquire(t *testing.T) {
	p := testPaths(t)
	for range 50 {
		_, err := LockHeld(p)
		require.NoError(t, err)
	}
	l, err := Acquire(p)
	require.NoError(t, err, "a probe leaves nothing behind")
	require.NoError(t, l.Release())
}

func TestAcquireWaitTakesTheLockWhenItFrees(t *testing.T) {
	p := testPaths(t)
	first, err := Acquire(p)
	require.NoError(t, err)
	go func() { time.Sleep(80 * time.Millisecond); _ = first.Release() }()
	l, err := AcquireWait(p, 2*time.Second, func() bool { return false })
	require.NoError(t, err)
	require.NoError(t, l.Release())
}

func TestAcquireWaitYieldsToALeaderThatServes(t *testing.T) {
	p := testPaths(t)
	first, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = first.Release() }()
	start := time.Now()
	_, err = AcquireWait(p, 5*time.Second, func() bool { return true })
	require.ErrorIs(t, err, ErrLeaderRunning)
	require.Less(t, time.Since(start), time.Second, "a loser does not wait when the winner already serves")
}

func TestAcquireWaitGivesUp(t *testing.T) {
	p := testPaths(t)
	first, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = first.Release() }()
	_, err = AcquireWait(p, 60*time.Millisecond, func() bool { return false })
	require.ErrorIs(t, err, ErrLeaderRunning)
}

func TestOwnerMetadata(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	require.NoError(t, l.WriteOwner(Owner{PID: 77, Start: 1234567, Instance: "inst-9"}))
	o, err := ReadOwner(p)
	require.NoError(t, err)
	require.Equal(t, Owner{PID: 77, Start: 1234567, Instance: "inst-9"}, o)
	pid, err := ReadPID(p)
	require.NoError(t, err)
	require.Equal(t, 77, pid)
	require.NoError(t, l.Release())
	o, err = ReadOwner(p)
	require.NoError(t, err)
	require.Equal(t, Owner{}, o, "release clears the owner")
}

func TestRotateLogOnlyAboveSize(t *testing.T) {
	p := testPaths(t)
	rotated, err := RotateLog(p, 100)
	require.NoError(t, err)
	require.False(t, rotated, "no log yet")

	require.NoError(t, os.WriteFile(p.Log, []byte(strings.Repeat("a", 50)), 0600))
	rotated, err = RotateLog(p, 100)
	require.NoError(t, err)
	require.False(t, rotated)

	require.NoError(t, os.WriteFile(p.Log, []byte(strings.Repeat("b", 150)), 0600))
	rotated, err = RotateLog(p, 100)
	require.NoError(t, err)
	require.True(t, rotated)
	require.NoFileExists(t, p.Log)
	old, err := os.ReadFile(p.Log + ".1")
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("b", 150), string(old))

	// The previous failure is kept: a second rotation replaces only the older copy.
	require.NoError(t, os.WriteFile(p.Log, []byte(strings.Repeat("c", 150)), 0600))
	_, err = RotateLog(p, 100)
	require.NoError(t, err)
	old, _ = os.ReadFile(p.Log + ".1")
	require.Equal(t, strings.Repeat("c", 150), string(old))
}

func TestRotateLogRefusesSymlink(t *testing.T) {
	p := testPaths(t)
	target := filepath.Join(p.Home, "elsewhere")
	require.NoError(t, os.WriteFile(target, []byte(strings.Repeat("x", 500)), 0600))
	require.NoError(t, os.Symlink(target, p.Log))
	_, err := RotateLog(p, 10)
	require.ErrorIs(t, err, ErrUnsafePath)
	require.FileExists(t, target)
}

func TestOpenLogAppendsAndIsPrivate(t *testing.T) {
	p := testPaths(t)
	f, err := OpenLog(p)
	require.NoError(t, err)
	_, _ = f.WriteString("one\n")
	require.NoError(t, f.Close())
	f, err = OpenLog(p)
	require.NoError(t, err)
	_, _ = f.WriteString("two\n")
	require.NoError(t, f.Close())
	data, _ := os.ReadFile(p.Log)
	require.Equal(t, "one\ntwo\n", string(data))
	info, _ := os.Stat(p.Log)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestLogTail(t *testing.T) {
	p := testPaths(t)
	require.NoError(t, os.WriteFile(p.Log, []byte(strings.Repeat("x", 5000)+"the end\n"), 0600))
	tail := LogTail(p, 100)
	require.LessOrEqual(t, len(tail), 100)
	require.Contains(t, tail, "the end")
	require.Empty(t, LogTail(Paths{Log: filepath.Join(p.Home, "none")}, 100))
}

func TestFindAskPrefersSiblingThenPath(t *testing.T) {
	root := t.TempDir()
	sibDir, pathDir := filepath.Join(root, "sib"), filepath.Join(root, "path")
	pathAsk := fakeAsk(t, pathDir, versionScript("path", 1))
	lookPath := func(name string) (string, error) {
		require.Equal(t, "ask", name)
		return pathAsk, nil
	}
	caller := filepath.Join(sibDir, "ask-server")

	got, err := FindAsk(caller, lookPath)
	require.NoError(t, err)
	require.Equal(t, pathAsk, got, "no sibling: the PATH is used")

	sibAsk := fakeAsk(t, sibDir, versionScript("sibling", 1))
	got, err = FindAsk(caller, lookPath)
	require.NoError(t, err)
	require.Equal(t, sibAsk, got, "a sibling comes before the PATH")
}

func TestFindAskMissing(t *testing.T) {
	_, err := FindAsk(filepath.Join(t.TempDir(), "ask-server"), func(string) (string, error) { return "", exec.ErrNotFound })
	require.ErrorIs(t, err, ErrAskNotFound)
	require.Contains(t, err.Error(), "PATH")
}

func TestFindAskRefusesUnusableSiblingWithoutFallingBack(t *testing.T) {
	root := t.TempDir()
	sibDir := filepath.Join(root, "sib")
	require.NoError(t, os.MkdirAll(sibDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sibDir, "ask"), []byte("not executable"), 0644))
	pathAsk := fakeAsk(t, filepath.Join(root, "path"), versionScript("path", 1))
	_, err := FindAsk(filepath.Join(sibDir, "ask-server"), func(string) (string, error) { return pathAsk, nil })
	require.ErrorIs(t, err, ErrAskUnusable, "a sibling that is not usable is an error, not a reason to run another binary")

	wide := fakeAsk(t, filepath.Join(root, "wide"), versionScript("w", 1))
	require.NoError(t, os.Chmod(wide, 0777))
	_, err = FindAsk(filepath.Join(root, "wide", "ask-server"), func(string) (string, error) { return pathAsk, nil })
	require.ErrorIs(t, err, ErrAskUnusable, "a binary that anyone can write is refused")
}

func TestProbeAsk(t *testing.T) {
	dir := t.TempDir()
	ask := fakeAsk(t, dir, versionScript("abc123", protocol.LeaderProtocolVersion))
	info, err := ProbeAsk(context.Background(), ask, protocol.LeaderProtocolVersion)
	require.NoError(t, err)
	require.Equal(t, "abc123", info.Build)
}

func TestProbeAskRejects(t *testing.T) {
	for name, body := range map[string]string{
		"other protocol": versionScript("x", 99),
		"not json":       `echo hello`,
		"wrong name":     `echo '{"name":"other","build":"x","leaderProtocol":1,"acpVersion":1}'`,
		"fails":          `exit 7`,
		"zero protocol":  `echo '{"name":"ask","build":"x","leaderProtocol":0,"acpVersion":1}'`,
		"float protocol": `echo '{"name":"ask","build":"x","leaderProtocol":1.5,"acpVersion":1}'`,
		"huge output":    `yes '{"name":"ask"}' | head -c 200000`,
	} {
		t.Run(name, func(t *testing.T) {
			ask := fakeAsk(t, t.TempDir(), body)
			_, err := ProbeAsk(context.Background(), ask, protocol.LeaderProtocolVersion)
			require.ErrorIs(t, err, ErrAskIncompatible)
		})
	}
}

func TestProbeAskHungChildIsKilledAndReaped(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "pid")
	ask := fakeAsk(t, dir, fmt.Sprintf(`echo $$ > %s; sleep 60`, marker))
	// Wait for the child to start before cancellation. macOS can delay the first
	// execution of a new file while other package tests build their binaries.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		_, err := ProbeAsk(ctx, ask, protocol.LeaderProtocolVersion)
		finished <- err
	}()
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(marker)
		var pid int
		_, _ = fmt.Sscan(string(data), &pid)
		return err == nil && pid > 0
	}, 10*time.Second, 10*time.Millisecond, "the child must start before its cancellation is tested")
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, ErrAskIncompatible)
	case <-time.After(5 * time.Second):
		t.Fatal("the probe did not end after cancellation")
	}

	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	var pid int
	_, _ = fmt.Sscan(string(data), &pid)
	require.NotZero(t, pid)
	require.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil }, 2*time.Second, 10*time.Millisecond, "the hung child and its process group are gone")
}

func TestVerifyLeaderProcessRefusesOthers(t *testing.T) {
	sleeper := exec.Command("sleep", "30")
	require.NoError(t, sleeper.Start())
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })

	err := VerifyLeaderProcess(sleeper.Process.Pid, time.Now().Unix())
	require.ErrorIs(t, err, ErrNotLeaderProcess, "a process that is not `ask leader` is never a target")
	require.ErrorIs(t, VerifyLeaderProcess(os.Getpid(), time.Now().Unix()), ErrNotLeaderProcess)
	require.ErrorIs(t, VerifyLeaderProcess(0, time.Now().Unix()), ErrNotLeaderProcess)
	require.ErrorIs(t, VerifyLeaderProcess(1<<22+12345, time.Now().Unix()), ErrNotLeaderProcess, "a dead pid")
	require.ErrorIs(t, VerifyLeaderProcess(sleeper.Process.Pid, 0), ErrNotLeaderProcess, "no recorded start: fail closed")
}

func TestLeaderCommandLine(t *testing.T) {
	for line, want := range map[string]bool{
		"/usr/local/bin/ask leader --spawned-by-client": true,
		"ask leader":                         true,
		"/bin/sh /tmp/x/ask leader":          true,
		"/usr/local/bin/ask connect":         false,
		"/usr/bin/vim ask leader":            false,
		"/usr/local/bin/ask":                 false,
		"/usr/local/bin/ask-server leader":   false,
		"":                                   false,
		"/usr/local/bin/ask --home x leader": false,
	} {
		require.Equal(t, want, isLeaderCommandLine(line), line)
	}
}

func TestStderrRedirect(t *testing.T) {
	if os.Getenv("ASK_LEADER_STDERR_HELPER") == "1" {
		p, _ := ResolvePaths(os.Getenv("ASK_LEADER_STDERR_HOME"))
		f, err := OpenLog(p)
		if err != nil {
			os.Exit(2)
		}
		if err := RedirectStderr(f); err != nil {
			os.Exit(3)
		}
		_, _ = fmt.Fprintln(os.Stderr, "from the leader")
		os.Exit(0)
	}
	p := testPaths(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestStderrRedirect$")
	cmd.Env = append(os.Environ(), "ASK_LEADER_STDERR_HELPER=1", "ASK_LEADER_STDERR_HOME="+p.Home)
	require.NoError(t, cmd.Run())
	data, err := os.ReadFile(p.Log)
	require.NoError(t, err)
	require.Contains(t, string(data), "from the leader")
}

// ---- starting a leader ----

func TestStartLeaderUsesNullStdioSetsidAndLog(t *testing.T) {
	p := testPaths(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	// The fake leader reports what it sees, then exits.
	ask := fakeAsk(t, dir, fmt.Sprintf(`
if [ "$1" = leader ]; then
  echo "args:$*" > %[1]s
  echo "home:$ASK_HOME" >> %[1]s
  if read line; then echo "stdin:open" >> %[1]s; else echo "stdin:eof" >> %[1]s; fi
  echo "to-stderr" >&2
  if [ -r /proc/$$/stat ]; then set -- $(cat /proc/$$/stat); echo "ids:$1 $5" >> %[1]s; else echo "ids:$(ps -o pid=,pgid= -p $$ | tr -s ' ')" >> %[1]s; fi
fi`, out))
	child, err := StartLeader(StartConfig{Executable: ask, Paths: p})
	require.NoError(t, err)
	select {
	case <-child.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("the child did not exit")
	}
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "args:leader --spawned-by-client")
	require.Contains(t, text, "home:"+p.Home)
	require.Contains(t, text, "stdin:eof", "stdin is the null device")
	var pid, pgid int
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, "ids:"); ok {
			_, _ = fmt.Sscan(rest, &pid, &pgid)
		}
	}
	require.NotZero(t, pid)
	require.Equal(t, pid, pgid, "the leader starts its own session, so it leads its process group")
	logData, err := os.ReadFile(p.Log)
	require.NoError(t, err)
	require.Contains(t, string(logData), "to-stderr", "stderr goes to the leader log")
	info, _ := os.Stat(p.Log)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestStartLeaderReportsExit(t *testing.T) {
	p := testPaths(t)
	ask := fakeAsk(t, t.TempDir(), `echo boom >&2; exit 5`)
	child, err := StartLeader(StartConfig{Executable: ask, Paths: p})
	require.NoError(t, err)
	<-child.Exited()
	require.Error(t, child.Err())
	require.Contains(t, LogTail(p, 200), "boom")
}

func TestStartLeaderDoesNotRotateTheLog(t *testing.T) {
	p := testPaths(t)
	big := strings.Repeat("old failure\n", 20000)
	require.NoError(t, os.WriteFile(p.Log, []byte(big), 0600))
	ask := fakeAsk(t, t.TempDir(), `exit 0`)
	child, err := StartLeader(StartConfig{Executable: ask, Paths: p})
	require.NoError(t, err)
	<-child.Exited()
	data, err := os.ReadFile(p.Log)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), big), "only the lock winner rotates; the caller appends")
	require.NoFileExists(t, p.Log+".1")
}

func TestSignalFallbackRefusesChangedLifetimeOwner(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()
	owner := Owner{PID: os.Getpid(), Start: time.Now().Unix(), Instance: "first"}
	require.NoError(t, l.WriteOwner(owner))
	require.NoError(t, verifySignalOwner(p, owner))
	replacement := owner
	replacement.Instance = "second"
	require.NoError(t, l.WriteOwner(replacement))
	require.ErrorIs(t, verifySignalOwner(p, owner), ErrNotLeaderProcess)
	require.NoError(t, l.Release())
	require.ErrorIs(t, verifySignalOwner(p, replacement), ErrNotLeaderProcess)
}
