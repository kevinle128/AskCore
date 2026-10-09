package leader

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var (
	// ErrLeaderRunning means another process holds the leader lock.
	ErrLeaderRunning = errors.New("leader: another leader holds the lock")
	// ErrSocketInUse means a live process listens on the socket path.
	ErrSocketInUse = errors.New("leader: socket is in use")
	// ErrLockReleased means the lock is no longer held.
	ErrLockReleased = errors.New("leader: lock released")
)

// Lock is the lifetime lock of a leader. The holder is the only process that
// may write the PID or remove a stale socket. The lock file is never removed,
// so two processes can never lock two different files at the same path.
type Lock struct {
	paths Paths
	mu    sync.Mutex
	file  *os.File
	dir   *os.File
	root  *os.Root
}

// Acquire takes the lifetime lock without waiting.
func Acquire(p Paths) (*Lock, error) {
	if err := EnsureHome(p.Home); err != nil {
		return nil, err
	}
	dir, err := openPrivateDir(p.Home)
	if err != nil {
		return nil, err
	}
	f, err := openPrivateAt(dir, filepath.Base(p.Lock), os.O_RDWR|os.O_CREATE)
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		_ = dir.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLeaderRunning
		}
		return nil, err
	}
	root, err := rootForDir(dir)
	if err != nil {
		_ = f.Close()
		_ = dir.Close()
		return nil, err
	}
	return &Lock{paths: p, file: f, dir: dir, root: root}, nil
}

// Owner is the metadata that the lock holder writes into the lock file. It is
// diagnostic data: it is not proof of ownership and gives no right to send a signal.
type Owner struct {
	PID int
	// Start is when the process started, in Unix seconds.
	Start int64
	// Instance is the instance id that the leader reports in its status.
	Instance string
}

// WriteOwner stores the owner metadata in the lock file.
func (l *Lock) WriteOwner(o Owner) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return ErrLockReleased
	}
	if err := l.file.Truncate(0); err != nil {
		return err
	}
	text := strconv.Itoa(o.PID) + "\n" + strconv.FormatInt(o.Start, 10) + "\n" + o.Instance + "\n"
	_, err := l.file.WriteAt([]byte(text), 0)
	return err
}

// WritePID stores only the PID of the holder.
func (l *Lock) WritePID(pid int) error { return l.WriteOwner(Owner{PID: pid}) }

// Release clears the PID and frees the lock. The file stays. It is safe to call twice.
func (l *Lock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	truncErr := f.Truncate(0)
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	rootErr := l.root.Close()
	l.root = nil
	dirErr := l.dir.Close()
	l.dir = nil
	return errors.Join(truncErr, unlockErr, closeErr, dirErr, rootErr)
}

// ReadOwner returns the owner metadata of the lock file. A missing, empty or
// unreadable record gives the zero value.
func ReadOwner(p Paths) (Owner, error) {
	f, err := OpenPrivate(p.Lock, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return Owner{}, nil
	}
	if err != nil {
		return Owner{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 512))
	if err != nil {
		return Owner{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var o Owner
	if pid, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil && pid > 0 {
		o.PID = pid
	} else {
		return Owner{}, nil
	}
	if len(lines) > 1 {
		o.Start, _ = strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	}
	if len(lines) > 2 {
		o.Instance = strings.TrimSpace(lines[2])
	}
	return o, nil
}

// ReadPID returns the PID stored in the lock file, or 0 when there is none.
func ReadPID(p Paths) (int, error) {
	o, err := ReadOwner(p)
	return o.PID, err
}

// RemoveStaleSocket removes the socket file when no process listens on it. It
// removes only a socket that the current user owns. A regular file, a symlink
// or a live endpoint stays. Only the lock holder may call it.
func (l *Lock) RemoveStaleSocket() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return false, ErrLockReleased
	}
	path := l.paths.Socket
	before, err := l.socketInfo()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if before.Mode()&os.ModeSocket == 0 || before.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(before) {
		return false, fmt.Errorf("%w: %s is not a socket of this user", ErrUnsafePath, path)
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		_ = conn.Close()
		return false, ErrSocketInUse
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return false, fmt.Errorf("leader: socket check failed: %w", err)
	}
	// The path must still name the same file, so a new socket is not removed.
	after, err := l.socketInfo()
	if err != nil || !os.SameFile(before, after) {
		return false, ErrSocketInUse
	}
	if err := unix.Unlinkat(int(l.dir.Fd()), filepath.Base(path), 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}

// socketInfo reads the entry without following a symlink, in the held directory.
func (l *Lock) socketInfo() (os.FileInfo, error) {
	return l.root.Lstat(filepath.Base(l.paths.Socket))
}

// RemoveSocket removes only the recorded socket inode while the lock is held.
func (l *Lock) RemoveSocket(identity os.FileInfo) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return ErrLockReleased
	}
	current, err := l.socketInfo()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if identity == nil || !os.SameFile(identity, current) || current.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(current) {
		return ErrSocketInUse
	}
	return unix.Unlinkat(int(l.dir.Fd()), filepath.Base(l.paths.Socket), 0)
}
