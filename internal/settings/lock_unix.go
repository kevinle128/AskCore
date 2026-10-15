//go:build unix || darwin || linux

package settings

import (
	"errors"
	"os"
	"syscall"
)

var errLockBusy = errors.New("credential lock busy")

func tryFileLock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
		return errLockBusy
	}
	return err
}
func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

func openNoFollow(path string, flags int, perm uint32) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW, perm)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
