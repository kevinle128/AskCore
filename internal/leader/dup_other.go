//go:build !linux

package leader

import (
	"os"

	"golang.org/x/sys/unix"
)

// RedirectStderr makes file the standard error of the process.
func RedirectStderr(file *os.File) error { return unix.Dup2(int(file.Fd()), 2) }
