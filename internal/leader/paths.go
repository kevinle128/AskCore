package leader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	// ErrUnsafePath means a path or file failed an owner, type or mode check.
	ErrUnsafePath = errors.New("leader: unsafe path")
	// ErrSocketPathTooLong means the socket path does not fit in a Unix socket address.
	ErrSocketPathTooLong = errors.New("leader: socket path too long")
)

// Paths are the files of one leader. All of them live in one private directory.
// The lock file also holds the PID of the leader.
type Paths struct {
	Home   string
	Socket string
	Lock   string
	Log    string
}

// ResolvePaths returns the paths for an Ask home. An empty home means ~/.ask.
// A relative home is refused.
func ResolvePaths(home string) (Paths, error) {
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		home = filepath.Join(user, ".ask")
	}
	if !filepath.IsAbs(home) {
		return Paths{}, fmt.Errorf("%w: ASK_HOME must be an absolute path", ErrUnsafePath)
	}
	home = filepath.Clean(home)
	return Paths{
		Home:   home,
		Socket: filepath.Join(home, "leader.sock"),
		Lock:   filepath.Join(home, "leader.lock"),
		Log:    filepath.Join(home, "leader.log"),
	}, nil
}

// EnsureHome makes the Ask home a private directory: a real directory (no
// symlink) that the current user owns, with mode 0700.
func EnsureHome(home string) error {
	// Same rule as the credential store, which shares this home: the parent is a
	// real directory, not a symlink.
	if err := requireRealDirectory(filepath.Dir(home)); err != nil {
		return err
	}
	if err := os.Mkdir(home, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	dir, err := openPrivateDir(home)
	if err != nil {
		return err
	}
	return dir.Close()
}

// openPrivateDir pins the checked home inode for descriptor-relative operations.
func openPrivateDir(home string) (*os.File, error) {
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: open directory %s: %w", ErrUnsafePath, home, err)
	}
	dir := os.NewFile(uintptr(fd), home)
	info, err := dir.Stat()
	if err == nil && !ownedByCurrentUser(info) {
		err = fmt.Errorf("%w: %s has another owner", ErrUnsafePath, home)
	}
	if err == nil && info.Mode().Perm() != 0700 {
		err = dir.Chmod(0700)
	}
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	return dir, nil
}

func requireRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is not a real directory", ErrUnsafePath, path)
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// CheckSocketPath refuses a path that is too long for a Unix socket address.
func CheckSocketPath(path string) error {
	limit := 104 // sun_path size on macOS and the BSDs, with the end byte
	if runtime.GOOS == "linux" {
		limit = 108
	}
	if len(path) >= limit {
		return fmt.Errorf("%w: %d bytes, the limit is %d; set ASK_HOME to a shorter path", ErrSocketPathTooLong, len(path), limit-1)
	}
	return nil
}

// OpenPrivate opens a lock, log or similar file for the leader. It never
// follows a symlink and never blocks on a FIFO. The file must be a regular file
// that the current user owns. Truncate, append and a wide mode change apply only
// after those checks pass.
func OpenPrivate(path string, flags int) (*os.File, error) {
	dir, err := openPrivateDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	return openPrivateAt(dir, filepath.Base(path), flags)
}

func openPrivateAt(dir *os.File, name string, flags int) (*os.File, error) {
	path := filepath.Join(dir.Name(), name)
	open := (flags &^ (os.O_TRUNC | os.O_APPEND)) | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
	fd, err := openArtifact(int(dir.Fd()), name, open)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%w: %s is a symlink", ErrUnsafePath, path)
		}
		if errors.Is(err, syscall.ENXIO) {
			return nil, fmt.Errorf("%w: %s is not a regular file", ErrUnsafePath, path)
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	if err := checkPrivateFile(f, path); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := finishOpen(f, flags); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func checkPrivateFile(f *os.File, path string) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrUnsafePath, path)
	}
	if !ownedByCurrentUser(info) {
		return fmt.Errorf("%w: %s has another owner", ErrUnsafePath, path)
	}
	if info.Mode().Perm() != 0600 {
		return f.Chmod(0600)
	}
	return nil
}

func finishOpen(f *os.File, flags int) error {
	fd := int(f.Fd())
	status, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	status &^= syscall.O_NONBLOCK
	if flags&os.O_APPEND != 0 {
		status |= syscall.O_APPEND
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, status); err != nil {
		return err
	}
	if flags&os.O_TRUNC != 0 {
		return f.Truncate(0)
	}
	return nil
}

func rootForDir(dir *os.File) (*os.Root, error) {
	root, err := os.OpenRoot(dir.Name())
	if err != nil {
		return nil, err
	}
	before, err := dir.Stat()
	after, statErr := root.Stat(".")
	if err != nil || statErr != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, fmt.Errorf("%w: directory changed", ErrUnsafePath)
	}
	return root, nil
}

// Exclusive creation handles concurrent macOS openat creators. An existing file
// is opened separately, so its owner and type are checked before any change.
func openArtifact(dirfd int, name string, flags int) (int, error) {
	if flags&os.O_CREATE == 0 || flags&os.O_EXCL != 0 {
		return unix.Openat(dirfd, name, flags, 0600)
	}
	for {
		fd, err := unix.Openat(dirfd, name, flags&^os.O_CREATE, 0600)
		if !errors.Is(err, unix.ENOENT) {
			return fd, err
		}
		fd, err = unix.Openat(dirfd, name, flags|os.O_EXCL, 0600)
		if !errors.Is(err, unix.EEXIST) {
			return fd, err
		}
	}
}
