package leader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"AskCore/pkg/protocol"

	"golang.org/x/sys/unix"
)

var (
	// ErrAskNotFound means no `ask` binary was found next to the caller or on the PATH.
	ErrAskNotFound = errors.New("leader: the ask binary was not found")
	// ErrAskUnusable means an `ask` binary exists but cannot be run safely.
	ErrAskUnusable = errors.New("leader: the ask binary cannot be used")
	// ErrAskIncompatible means the `ask` binary does not report a supported version.
	ErrAskIncompatible = errors.New("leader: the ask binary is not compatible")
	// ErrNotLeaderProcess means a process could not be shown to be `ask leader` of this user.
	ErrNotLeaderProcess = errors.New("leader: the process is not a verified ask leader")
)

const (
	// probeTimeout bounds one `ask version --json` run. The first run of a new
	// binary can take seconds on a loaded macOS, so the bound is not tight. The
	// deadline of the whole connect call bounds it further.
	probeTimeout = 15 * time.Second
	// probeMaxOutput bounds the output of that run.
	probeMaxOutput = 64 << 10
	// startTolerance is how far the recorded start of a leader may differ from
	// the start that the system reports. The leader records its time when it
	// begins, and the system rounds to a second.
	startTolerance = 10 * time.Second
)

// ---- lock probe and wait ----

// LockHeld tells whether a process holds the leader lock. It takes a shared lock
// for an instant, which only conflicts with an exclusive holder, and gives it
// back at once. A missing lock file means nobody holds it.
func LockHeld(p Paths) (bool, error) {
	f, err := OpenPrivate(p.Lock, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	switch {
	case err == nil:
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return false, nil
	case errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN):
		return true, nil
	}
	return false, err
}

// AcquireWait takes the lifetime lock, and waits up to wait while another
// process holds it. A caller probes the lock for an instant, so a short busy
// answer is not proof of a leader. It gives up at once when serving reports
// that a leader already answers on the socket.
func AcquireWait(p Paths, wait time.Duration, serving func() bool) (*Lock, error) {
	deadline := time.Now().Add(wait)
	for {
		l, err := Acquire(p)
		if !errors.Is(err, ErrLeaderRunning) {
			return l, err
		}
		if serving() || !time.Now().Before(deadline) {
			return nil, ErrLeaderRunning
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---- log ----

// OpenLog opens the leader log for appending. It checks owner, type and mode first.
func OpenLog(p Paths) (*os.File, error) {
	return OpenPrivate(p.Log, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
}

// RotateLog moves the log to leader.log.1 when it is above max bytes. The older
// copy is replaced, and the failure that is in the log is kept. Only the holder
// of the lifetime lock may call it, because a rotation moves the file that a
// running leader writes to.
func RotateLog(p Paths, max int64) (bool, error) {
	dir, err := openPrivateDir(p.Home)
	if err != nil {
		return false, err
	}
	defer func() { _ = dir.Close() }()
	f, err := openPrivateAt(dir, filepath.Base(p.Log), os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	info, err := f.Stat()
	_ = f.Close()
	if err != nil {
		return false, err
	}
	if info.Size() <= max {
		return false, nil
	}
	if err := unix.Renameat(int(dir.Fd()), filepath.Base(p.Log), int(dir.Fd()), filepath.Base(p.Log)+".1"); err != nil {
		return false, err
	}
	return true, nil
}

// LogTail returns up to n bytes from the end of the log, for an error message.
func LogTail(p Paths, n int64) string {
	f, err := OpenPrivate(p.Log, os.O_RDONLY)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	off := info.Size() - n
	if off < 0 {
		off = 0
	}
	buf := make([]byte, info.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	return strings.TrimSpace(string(buf))
}

// ---- finding and probing the ask binary ----

// FindAsk returns the `ask` binary to start a leader from. A binary next to the
// caller comes first, then the PATH. A sibling that exists but is not usable is
// an error: the caller does not silently run a different executable.
func FindAsk(callerExe string, lookPath func(string) (string, error)) (string, error) {
	if callerExe != "" {
		sibling := filepath.Join(filepath.Dir(callerExe), "ask")
		if _, err := os.Lstat(sibling); err == nil {
			return checkExecutable(sibling)
		}
	}
	path, err := lookPath("ask")
	if err != nil {
		return "", fmt.Errorf("%w: it is not next to the caller and not on the PATH", ErrAskNotFound)
	}
	return checkExecutable(path)
}

// checkExecutable returns the absolute path of a file that the current user can
// run safely: a regular file, executable, not writable by others, owned by the
// current user or by root.
func checkExecutable(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrAskUnusable, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrAskUnusable, abs, err)
	}
	info, err := os.Stat(real)
	switch {
	case err != nil:
		return "", fmt.Errorf("%w: %s: %v", ErrAskUnusable, real, err)
	case !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0:
		return "", fmt.Errorf("%w: %s is not an executable file", ErrAskUnusable, real)
	case info.Mode().Perm()&0002 != 0:
		return "", fmt.Errorf("%w: %s is writable by everyone", ErrAskUnusable, real)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != uint32(os.Geteuid()) && st.Uid != 0 {
		return "", fmt.Errorf("%w: %s belongs to another user", ErrAskUnusable, real)
	}
	return abs, nil
}

// limitedBuffer keeps at most probeMaxOutput bytes and fails above that.
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > probeMaxOutput {
		return 0, errors.New("output too large")
	}
	return b.Buffer.Write(p)
}

// ProbeAsk runs `ask version --json` and checks that the binary speaks wantProtocol.
// A child that hangs is killed with its process group and reaped.
func ProbeAsk(ctx context.Context, exe string, wantProtocol int) (protocol.VersionInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "version", "--json")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var out limitedBuffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return protocol.VersionInfo{}, fmt.Errorf("%w: %s did not report its version: %v", ErrAskIncompatible, exe, err)
	}
	var info protocol.VersionInfo
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &info); err != nil {
		return protocol.VersionInfo{}, fmt.Errorf("%w: %s printed no version: %v", ErrAskIncompatible, exe, err)
	}
	switch {
	case info.Name != "ask":
		return protocol.VersionInfo{}, fmt.Errorf("%w: %s is not ask", ErrAskIncompatible, exe)
	case info.LeaderProtocol < 1 || info.LeaderProtocol != wantProtocol:
		return protocol.VersionInfo{}, fmt.Errorf("%w: %s speaks leader protocol %d, this client needs %d; upgrade the older side", ErrAskIncompatible, exe, info.LeaderProtocol, wantProtocol)
	}
	return info, nil
}

// ---- starting a leader ----

// StartConfig says how to start a leader process.
type StartConfig struct {
	// Executable is the absolute path of the ask binary (see FindAsk).
	Executable string
	Paths      Paths
	// Env is the environment of the child. Nil means the current one.
	Env []string
}

// Child is a leader process that this process started.
type Child struct {
	cmd    *exec.Cmd
	exited chan struct{}
	err    error
}

// Exited is closed when the child has exited and was reaped.
func (c *Child) Exited() <-chan struct{} { return c.exited }

// Err returns how the child ended. Call it after Exited is closed.
func (c *Child) Err() error { return c.err }

// PID returns the process id of the child.
func (c *Child) PID() int { return c.cmd.Process.Pid }

// StartLeader starts `ask leader --spawned-by-client` in its own session. Its
// input and output are the null device, and its error output is appended to the
// leader log. The caller never rotates the log: only the process that wins the
// lifetime lock does. A goroutine reaps the child.
func StartLeader(cfg StartConfig) (*Child, error) {
	if err := EnsureHome(cfg.Paths.Home); err != nil {
		return nil, err
	}
	logFile, err := OpenLog(cfg.Paths)
	if err != nil {
		return nil, err
	}
	defer func() { _ = logFile.Close() }()
	env := cfg.Env
	if env == nil {
		env = os.Environ()
	}
	env = append(filterEnv(env, "ASK_HOME"), "ASK_HOME="+cfg.Paths.Home)
	cmd := exec.Command(cfg.Executable, "leader", "--spawned-by-client")
	cmd.Dir = cfg.Paths.Home
	cmd.Env = env
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("leader: start %s: %w", cfg.Executable, err)
	}
	c := &Child{cmd: cmd, exited: make(chan struct{})}
	go func() {
		c.err = cmd.Wait()
		close(c.exited)
	}()
	return c, nil
}

func filterEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// ---- process identity ----

// VerifyLeaderProcess checks that pid is an `ask leader` process of the current
// user that started at the recorded time. A PID alone proves nothing, because
// the system reuses PIDs. When the check cannot be done, it fails.
func VerifyLeaderProcess(pid int, recordedStart int64) error {
	if pid <= 1 || recordedStart <= 0 {
		return fmt.Errorf("%w: no recorded identity", ErrNotLeaderProcess)
	}
	uid, start, cmdline, err := processInfo(pid)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotLeaderProcess, err)
	}
	switch {
	case uid != uint32(os.Geteuid()):
		return fmt.Errorf("%w: another user", ErrNotLeaderProcess)
	case !isLeaderCommandLine(cmdline):
		return fmt.Errorf("%w: command is not `ask leader`", ErrNotLeaderProcess)
	case absDuration(time.Duration(start-recordedStart)*time.Second) > startTolerance:
		return fmt.Errorf("%w: the process started at another time than the leader record", ErrNotLeaderProcess)
	}
	return nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// isLeaderCommandLine tells whether a command line is `ask leader ...`, also
// when a shell started a script that is named ask.
func isLeaderCommandLine(line string) bool {
	f := strings.Fields(line)
	i := 0
	if len(f) > 0 {
		switch filepath.Base(f[0]) {
		case "sh", "bash", "dash", "zsh", "env":
			i = 1
		}
	}
	return len(f) > i+1 && filepath.Base(f[i]) == "ask" && f[i+1] == "leader"
}

// verifySignalOwner checks that the lifetime owner did not change while a
// stable process handle was opened and verified.
func verifySignalOwner(paths Paths, expected Owner) error {
	if expected.Instance == "" {
		return fmt.Errorf("%w: no recorded instance", ErrNotLeaderProcess)
	}
	held, err := LockHeld(paths)
	if err != nil || !held {
		return fmt.Errorf("%w: lifetime lock is not held", ErrNotLeaderProcess)
	}
	current, err := ReadOwner(paths)
	if err != nil || current != expected {
		return fmt.Errorf("%w: lifetime owner changed", ErrNotLeaderProcess)
	}
	return nil
}
