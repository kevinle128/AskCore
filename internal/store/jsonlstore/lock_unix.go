//go:build unix

package jsonlstore

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var errLockBusy = errors.New("jsonlstore: lock is held")

// openFile opens a file of the store. os.OpenFile sets close-on-exec, so a
// child of the holder (a bash tool, for example) never inherits a descriptor.
// A descriptor that leaked would keep the lock after the holder died.
func openFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, perm)
}

// lockFile creates the lock file when it is missing and takes an exclusive
// flock on it at once. It returns errLockBusy when another open file
// description holds the lock, also one in this process. The kernel drops the
// lock when the holder dies. The lock file itself is never removed.
func lockFile(path string) (*os.File, error) {
	f, err := openFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLockBusy
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}

// unlockFile drops the lock and closes the file.
func unlockFile(f *os.File) error {
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return errors.Join(unlockErr, f.Close())
}
