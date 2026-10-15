package leader

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	// A short path keeps the socket address under the sun_path limit on macOS.
	dir, err := os.MkdirTemp("/tmp", "askl-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// macOS keeps /tmp as a symlink, and the home policy refuses a symlinked parent.
	dir, err = filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	p, err := ResolvePaths(dir)
	require.NoError(t, err)
	return p
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Ino
}

func TestLockContentionGoroutines(t *testing.T) {
	p := testPaths(t)
	first, err := Acquire(p)
	require.NoError(t, err)
	_, err = Acquire(p)
	require.ErrorIs(t, err, ErrLeaderRunning)
	require.NoError(t, first.Release())

	second, err := Acquire(p)
	require.NoError(t, err)
	require.NoError(t, second.Release())
}

// TestLockHelperProcess is the child side of TestLockContentionChildProcess.
// It does nothing in a normal test run.
func TestLockHelperProcess(t *testing.T) {
	home := os.Getenv("ASK_LEADER_LOCK_HELPER_HOME")
	if home == "" {
		t.Skip("child process helper")
	}
	p, err := ResolvePaths(home)
	require.NoError(t, err)
	l, err := Acquire(p)
	require.NoError(t, err)
	require.NoError(t, l.WritePID(os.Getpid()))
	_, _ = os.Stdout.WriteString("locked\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n') // wait until the parent closes stdin
	require.NoError(t, l.Release())
}

func TestLockContentionChildProcess(t *testing.T) {
	p := testPaths(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), "ASK_LEADER_LOCK_HELPER_HOME="+p.Home)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked", strings.TrimSpace(line))

	_, err = Acquire(p)
	require.ErrorIs(t, err, ErrLeaderRunning, "another process holds the lock")
	pid, err := ReadPID(p)
	require.NoError(t, err)
	require.Equal(t, cmd.Process.Pid, pid, "the holder wrote its PID")

	require.NoError(t, stdin.Close())
	require.NoError(t, cmd.Wait())
	l, err := Acquire(p)
	require.NoError(t, err, "the lock is free once the holder exits")
	require.NoError(t, l.Release())
}

func TestLockInodeStableAcrossRestart(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	before := inode(t, p.Lock)
	require.NoError(t, l.Release())
	require.FileExists(t, p.Lock, "release never removes the lock file")
	require.Equal(t, before, inode(t, p.Lock))

	l, err = Acquire(p)
	require.NoError(t, err)
	require.Equal(t, before, inode(t, p.Lock))
	require.NoError(t, l.Release())
	require.NoError(t, l.Release(), "release is idempotent")
}

func TestLockPIDMetadata(t *testing.T) {
	p := testPaths(t)
	pid, err := ReadPID(p)
	require.NoError(t, err)
	require.Zero(t, pid, "no lock file means no PID")

	l, err := Acquire(p)
	require.NoError(t, err)
	require.NoError(t, l.WritePID(4242))
	pid, err = ReadPID(p)
	require.NoError(t, err)
	require.Equal(t, 4242, pid)
	require.NoError(t, l.Release())

	pid, err = ReadPID(p)
	require.NoError(t, err)
	require.Zero(t, pid, "release clears the PID but keeps the file")
}

func TestLockRefusesUnsafeLockFile(t *testing.T) {
	p := testPaths(t)
	target := filepath.Join(p.Home, "elsewhere")
	require.NoError(t, os.WriteFile(target, nil, 0600))
	require.NoError(t, os.Symlink(target, p.Lock))
	_, err := Acquire(p)
	require.ErrorIs(t, err, ErrUnsafePath)
}

func listenStale(t *testing.T, path string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())
	require.FileExists(t, path)
}

func TestStaleSocketRemovedOnlyByOwner(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()

	removed, err := l.RemoveStaleSocket()
	require.NoError(t, err)
	require.False(t, removed, "nothing to remove")

	listenStale(t, p.Socket)
	removed, err = l.RemoveStaleSocket()
	require.NoError(t, err)
	require.True(t, removed)
	require.NoFileExists(t, p.Socket)
}

func TestStaleSocketRefusesRegularFileLiveListenerAndSymlink(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()

	require.NoError(t, os.WriteFile(p.Socket, []byte("data"), 0600))
	_, err = l.RemoveStaleSocket()
	require.ErrorIs(t, err, ErrUnsafePath)
	require.FileExists(t, p.Socket, "a regular file is never removed")
	require.NoError(t, os.Remove(p.Socket))

	live, err := net.Listen("unix", p.Socket)
	require.NoError(t, err)
	defer func() { _ = live.Close() }()
	go func() {
		for {
			c, err := live.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, err = l.RemoveStaleSocket()
	require.ErrorIs(t, err, ErrSocketInUse)
	require.FileExists(t, p.Socket, "a live endpoint is never removed")
	_ = live.Close() // closing the listener unlinks the socket path
	require.NoFileExists(t, p.Socket)

	target := filepath.Join(p.Home, "elsewhere")
	require.NoError(t, os.WriteFile(target, nil, 0600))
	require.NoError(t, os.Symlink(target, p.Socket))
	_, err = l.RemoveStaleSocket()
	require.ErrorIs(t, err, ErrUnsafePath)
	require.FileExists(t, target)
}

func TestReleasedLockCannotRemoveSocket(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	listenStale(t, p.Socket)
	require.NoError(t, l.Release())
	_, err = l.RemoveStaleSocket()
	require.ErrorIs(t, err, ErrLockReleased, "only the lock holder removes a socket")
	require.FileExists(t, p.Socket)
}

func TestSocketCleanupUsesHeldDirectory(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()
	listenStale(t, p.Socket)
	identity, err := os.Lstat(p.Socket)
	require.NoError(t, err)
	old := p.Home + "-moved"
	require.NoError(t, os.Rename(p.Home, old))
	t.Cleanup(func() { _ = os.RemoveAll(old) })
	require.NoError(t, os.Mkdir(p.Home, 0700))
	listenStale(t, p.Socket)
	replacement, err := os.Lstat(p.Socket)
	require.NoError(t, err)
	require.NoError(t, l.RemoveSocket(identity))
	require.NoFileExists(t, filepath.Join(old, "leader.sock"))
	current, err := os.Lstat(p.Socket)
	require.NoError(t, err)
	require.True(t, os.SameFile(replacement, current), "a swapped path cannot redirect cleanup")
}

func TestSocketCleanupRefusesReplacementAndReleasedLock(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()
	listenStale(t, p.Socket)
	identity, err := os.Lstat(p.Socket)
	require.NoError(t, err)
	require.NoError(t, os.Remove(p.Socket))
	require.NoError(t, os.WriteFile(p.Socket, []byte("keep"), 0600))
	require.ErrorIs(t, l.RemoveSocket(identity), ErrSocketInUse)
	require.FileExists(t, p.Socket)
	require.NoError(t, l.Release())
	require.ErrorIs(t, l.RemoveSocket(identity), ErrLockReleased)
}
