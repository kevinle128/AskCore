package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"AskCore/internal/leader"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// leaderEnv is a process environment with an isolated Ask home and a PATH that
// holds only the given directories plus the system ones.
func leaderEnv(home string, pathDirs ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ASK_HOME=") || strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "ASK_HOME="+home, "PATH="+strings.Join(append(pathDirs, "/usr/bin", "/bin"), ":"))
}

func e2eConnect(home, bin string) leader.ConnectConfig {
	paths, _ := leader.ResolvePaths(home)
	return leader.ConnectConfig{
		Paths:     paths,
		Hello:     protocol.LeaderRegister{ClientKind: "e2e", Build: "e2e"},
		CallerExe: bin,
		Env:       leaderEnv(home),
		Warn:      func(string) {},
		Deadline:  30 * time.Second,
	}
}

// stopLeaderAtEnd stops the leader of a home when the test ends. Only the
// leader of this test home is touched.
func stopLeaderAtEnd(t *testing.T, bin, home string) {
	t.Helper()
	t.Cleanup(func() {
		cmd := exec.Command(bin, "leader", "stop")
		cmd.Env = leaderEnv(home)
		_ = cmd.Run()
	})
}

// leaderProcesses lists the running processes of the form "<bin> leader ...".
// The binary path is unique to the test run, so no other leader matches.
func leaderProcesses(bin string) []string {
	out, err := exec.Command("ps", "-eo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	var rows []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, bin+" leader") {
			rows = append(rows, strings.TrimSpace(line))
		}
	}
	return rows
}

func TestLeaderE2EAutoStartStatusStop(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)

	c1, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	defer func() { _ = c1.Close() }()
	instance := c1.Info.InstanceID
	require.NotEmpty(t, instance)

	// Files and modes: private directory, private socket, lock and log.
	paths, _ := leader.ResolvePaths(home)
	for path, want := range map[string]os.FileMode{home: 0o700, paths.Socket: 0o600, paths.Lock: 0o600, paths.Log: 0o600} {
		info, err := os.Lstat(path)
		require.NoError(t, err, path)
		require.Equal(t, want, info.Mode().Perm(), path)
	}
	sockInfo, _ := os.Lstat(paths.Socket)
	require.NotZero(t, sockInfo.Mode()&os.ModeSocket)
	lockInfo, err := os.Stat(paths.Lock)
	require.NoError(t, err)

	// The owner record names a process that really is `ask leader` of this user.
	owner, err := leader.ReadOwner(paths)
	require.NoError(t, err)
	require.NotZero(t, owner.PID)
	require.Equal(t, instance, owner.Instance)
	require.NoError(t, leader.VerifyLeaderProcess(owner.PID, owner.Start), "the verification accepts the real leader")
	held, err := leader.LockHeld(paths)
	require.NoError(t, err)
	require.True(t, held)

	// A second client reaches the same leader.
	c2, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	defer func() { _ = c2.Close() }()
	require.Equal(t, instance, c2.Info.InstanceID)

	// The built binary reports the same leader, and only identity and counts.
	cmd := exec.Command(bin, "leader", "status", "--json")
	cmd.Env = leaderEnv(home)
	out, err := cmd.Output()
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(out, &raw))
	var names []string
	for k := range raw {
		names = append(names, k)
	}
	require.ElementsMatch(t, []string{"instanceId", "build", "protocolVersion", "pid", "clients", "sessions", "activeRuns", "spawnedByClient"}, names,
		"the status holds identity and counts, no prompt, directory, tool argument or credential")
	require.Equal(t, instance, raw["instanceId"])
	require.Equal(t, true, raw["spawnedByClient"])
	require.GreaterOrEqual(t, raw["clients"], float64(2))

	cmd = exec.Command(bin, "leader", "list")
	cmd.Env = leaderEnv(home)
	out, err = cmd.Output()
	require.NoError(t, err)
	require.Contains(t, string(out), instance)

	// Stop through the built binary.
	cmd = exec.Command(bin, "leader", "stop")
	cmd.Env = leaderEnv(home)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "leader stopped")

	require.NoFileExists(t, paths.Socket, "the owner removed its socket")
	after, err := os.Stat(paths.Lock)
	require.NoError(t, err)
	require.True(t, os.SameFile(lockInfo, after), "the lock file keeps its inode across a stop")
	held, _ = leader.LockHeld(paths)
	require.False(t, held)
	cleared, err := leader.ReadOwner(paths)
	require.NoError(t, err)
	require.Equal(t, leader.Owner{}, cleared, "the PID record is cleared")
	require.Eventually(t, func() bool { return syscall.Kill(owner.PID, 0) != nil }, 5*time.Second, 20*time.Millisecond, "the process is gone")

	// The clients were told, and then disconnected.
	f, err := c1.Reader.Next()
	require.NoError(t, err)
	require.Equal(t, protocol.LeaderFrameError, f.Type)
	_, err = c1.Reader.Next()
	require.Error(t, err)
}

func TestLeaderE2ESpawnRace(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)

	var wg sync.WaitGroup
	ids := make([]string, 4)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
			if err != nil {
				t.Errorf("client %d: %v", i, err)
				return
			}
			ids[i] = c.Info.InstanceID
			_ = c.Close()
		}()
	}
	wg.Wait()
	for _, id := range ids {
		require.NotEmpty(t, id)
		require.Equal(t, ids[0], id, "every client adopts the winner")
	}
	require.Eventually(t, func() bool { return len(leaderProcesses(bin)) == 1 }, 10*time.Second, 50*time.Millisecond,
		"one lifetime lock owner; the losing children exit: %v", leaderProcesses(bin))
	paths, _ := leader.ResolvePaths(home)
	require.FileExists(t, paths.Socket)
}

// compileSpawnCaller compiles the helper that stands in for ask-server.
func compileSpawnCaller(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "ask-server")
	b, err := exec.Command("go", "build", "-o", out, "./testdata/spawncaller").CombinedOutput()
	require.NoError(t, err, string(b))
	return out
}

func runCaller(t *testing.T, caller, cwd string, env []string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(caller)
	cmd.Dir = cwd
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return strings.TrimSpace(out.String()), errOut.String(), err
}

// A second binary that is not ask starts the leader. It finds ask next to
// itself, then on the PATH, from another working directory, and it fails with a
// clear message when ask is nowhere.
func TestLeaderE2ETwoBinaryResolver(t *testing.T) {
	ask := acpBinary(t)
	root := t.TempDir()
	cwd := t.TempDir()

	t.Run("next to the caller, ask missing from PATH", func(t *testing.T) {
		dir := filepath.Join(root, "sibling")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		caller := compileSpawnCaller(t, dir)
		require.NoError(t, os.Symlink(ask, filepath.Join(dir, "ask")))
		home := leaderHome(t)
		stopLeaderAtEnd(t, ask, home)
		out, errOut, err := runCaller(t, caller, cwd, leaderEnv(home))
		require.NoError(t, err, errOut)
		require.NotEmpty(t, out)
		paths, _ := leader.ResolvePaths(home)
		require.FileExists(t, paths.Socket)
	})
	t.Run("on the PATH only", func(t *testing.T) {
		dir := filepath.Join(root, "alone")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		caller := compileSpawnCaller(t, dir)
		pathDir := filepath.Join(root, "bin")
		require.NoError(t, os.MkdirAll(pathDir, 0o755))
		require.NoError(t, os.Symlink(ask, filepath.Join(pathDir, "ask")))
		home := leaderHome(t)
		stopLeaderAtEnd(t, ask, home)
		out, errOut, err := runCaller(t, caller, cwd, leaderEnv(home, pathDir))
		require.NoError(t, err, errOut)
		require.NotEmpty(t, out)
	})
	t.Run("ask nowhere", func(t *testing.T) {
		dir := filepath.Join(root, "none")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		caller := compileSpawnCaller(t, dir)
		home := leaderHome(t)
		_, errOut, err := runCaller(t, caller, cwd, leaderEnv(home))
		require.Error(t, err)
		require.Contains(t, errOut, "ask binary was not found")
		require.Contains(t, errOut, "PATH")
		paths, _ := leader.ResolvePaths(home)
		require.NoFileExists(t, paths.Socket, "nothing was started")
	})
}

func TestLeaderE2EHeadlessStartsNoLeader(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	cmd := exec.Command(bin, "-p", "hello")
	cmd.Env = leaderEnv(home)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	paths, _ := leader.ResolvePaths(home)
	require.NoFileExists(t, paths.Socket, "headless mode starts no leader")
	require.NoFileExists(t, paths.Lock)
	require.Empty(t, leaderProcesses(bin))
}

// A leader that cannot start ends with an error, and the client reports the
// reason from the log within its deadline. ASK_FAUX_TPS is a real setting of the
// product; a value that is not a number makes the agent host fail to compose.
func TestLeaderE2EStartupFailureIsReported(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	cfg := e2eConnect(home, bin)
	cfg.Env = append(leaderEnv(home), "ASK_FAUX_TPS=fast")
	cfg.Deadline = 20 * time.Second
	start := time.Now()
	_, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, leader.ErrStartupFailed)
	require.Contains(t, err.Error(), "ASK_FAUX_TPS", "the reason comes from the leader log")
	require.Less(t, time.Since(start), 15*time.Second)

	paths, _ := leader.ResolvePaths(home)
	require.NoFileExists(t, paths.Socket, "a leader that cannot start never opens its socket")
	held, _ := leader.LockHeld(paths)
	require.False(t, held)
	require.Empty(t, leaderProcesses(bin))
}

// The process that wins the lock rotates a large log and keeps the old failure.
func TestLeaderE2ELogRotation(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	paths, _ := leader.ResolvePaths(home)
	require.NoError(t, leader.EnsureHome(home))
	old := bytes.Repeat([]byte("old failure\n"), 9<<20/12+1)
	require.NoError(t, os.WriteFile(paths.Log, old, 0o600))

	c, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	_ = c.Close()

	kept, err := os.ReadFile(paths.Log + ".1")
	require.NoError(t, err)
	require.Equal(t, old, kept, "the previous log is kept whole")
	fresh, err := os.ReadFile(paths.Log)
	require.NoError(t, err)
	require.Less(t, len(fresh), 1<<20)
	require.Contains(t, string(fresh), "listening on", "the leader writes to the new log")
	info, _ := os.Stat(paths.Log)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// A leader that a person starts keeps the terminal, leaves on SIGTERM with the
// code 143 and cleans up. A second one finds the first and leaves with 0.
func TestLeaderE2EForegroundLeaderSIGTERM(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	paths, _ := leader.ResolvePaths(home)

	first := exec.Command(bin, "leader")
	first.Env = leaderEnv(home)
	var firstErr bytes.Buffer
	first.Stderr = &firstErr
	require.NoError(t, first.Start())
	exited := make(chan error, 1)
	go func() { exited <- first.Wait() }()
	t.Cleanup(func() { _ = first.Process.Kill() })
	require.Eventually(t, func() bool { return leader.Serving(paths) }, 15*time.Second, 25*time.Millisecond, "the leader serves")

	st, err := leader.Status(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	require.False(t, st.SpawnedByClient, "a leader that a person starts is not replaced by a client")
	require.Equal(t, first.Process.Pid, st.PID)

	second := exec.Command(bin, "leader")
	second.Env = leaderEnv(home)
	out, err := second.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "already runs")

	require.NoError(t, first.Process.Signal(syscall.SIGTERM))
	select {
	case werr := <-exited:
		var ee *exec.ExitError
		require.ErrorAs(t, werr, &ee)
		require.Equal(t, 143, ee.ExitCode())
	case <-time.After(15 * time.Second):
		t.Fatal("the leader did not stop on SIGTERM")
	}
	require.NoFileExists(t, paths.Socket)
	held, _ := leader.LockHeld(paths)
	require.False(t, held)
	owner, _ := leader.ReadOwner(paths)
	require.Equal(t, leader.Owner{}, owner)
	require.Contains(t, firstErr.String(), "listening on")
}

// Freeze the real leader so the control path times out. Linux uses a stable
// process handle to signal the verified leader. macOS refuses the fallback,
// because it has no stable process handle, and leaves that leader untouched.
func TestLeaderE2EStopFallsBackToVerifiedSignal(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	c, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	_ = c.Close()
	paths, _ := leader.ResolvePaths(home)
	owner, err := leader.ReadOwner(paths)
	require.NoError(t, err)
	require.NoError(t, leader.VerifyLeaderProcess(owner.PID, owner.Start))

	require.NoError(t, syscall.Kill(owner.PID, syscall.SIGSTOP))
	t.Cleanup(func() { _ = syscall.Kill(owner.PID, syscall.SIGCONT); _ = syscall.Kill(owner.PID, syscall.SIGKILL) })

	cfg := e2eConnect(home, bin)
	cfg.Deadline = 30 * time.Second
	res, err := leader.Stop(context.Background(), cfg, time.Second)
	if runtime.GOOS != "linux" {
		require.False(t, res.Signalled)
		require.ErrorIs(t, err, leader.ErrNotLeaderProcess)
		require.NoError(t, syscall.Kill(owner.PID, syscall.SIGCONT))
		require.Eventually(t, func() bool { return leader.Serving(paths) }, 5*time.Second, 20*time.Millisecond)
		held, lockErr := leader.LockHeld(paths)
		require.NoError(t, lockErr)
		require.True(t, held, "a refused fallback leaves the leader alive")
		return
	}
	require.True(t, res.Signalled, "the control path was dead, so the verified process got SIGTERM")
	require.Error(t, err, "a frozen process cannot handle the signal yet")

	require.NoError(t, syscall.Kill(owner.PID, syscall.SIGCONT))
	require.Eventually(t, func() bool { return syscall.Kill(owner.PID, 0) != nil }, 15*time.Second, 50*time.Millisecond, "the leader handled SIGTERM and left")
	require.NoFileExists(t, paths.Socket)
	held, _ := leader.LockHeld(paths)
	require.False(t, held)
}
