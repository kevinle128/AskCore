package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func owner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func ensureHome(home string) error {
	parent := filepath.Dir(home)
	if err := checkDirectory(parent); err != nil {
		return err
	}
	info, err := os.Lstat(home)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.Mkdir(home, 0700); err != nil {
			return err
		}
		info, err = os.Lstat(home)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !owner(info) {
		return fmt.Errorf("%w: home owner or type", ErrUnsafeStore)
	}
	if info.Mode().Perm() != 0700 {
		if err = os.Chmod(home, 0700); err != nil {
			return err
		}
	}
	return nil
}

func checkDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: parent directory", ErrUnsafeStore)
	}
	return nil
}

func checkEntry(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !owner(info) || info.Mode().Perm() != mode {
		return fmt.Errorf("%w: file owner, type or mode", ErrUnsafeStore)
	}
	return nil
}
