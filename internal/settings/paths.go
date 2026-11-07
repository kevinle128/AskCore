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
		// Another process can create the directory between Lstat and Mkdir.
		// Check the same type, owner and mode rules for the winner's directory.
		if err = os.Mkdir(home, 0700); err != nil && !errors.Is(err, os.ErrExist) {
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

// ResolveHome returns the Ask home directory. A non-empty home is used as it is;
// otherwise ASK_HOME, otherwise ~/.ask. The result must be an absolute path.
func ResolveHome(home string) (string, error) {
	if home == "" {
		home = os.Getenv("ASK_HOME")
	}
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(dir, ".ask")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: home must be absolute", ErrUnsafeStore)
	}
	return home, nil
}

// EnsurePrivateDir makes sure that path is a directory that only the current
// user can use. It creates the directory with mode 0700 when it is missing. It
// does not create a parent. It refuses, with ErrUnsafeStore, a parent that is
// not a directory, a path that is a symbolic link or not a directory, and a
// directory of another user. It corrects a mode other than 0700 of a directory
// that the user owns, as ensureHome does. A caller that needs several levels
// calls it for each level, from the top.
func EnsurePrivateDir(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: directory must be absolute", ErrUnsafeStore)
	}
	if err := checkDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		err = os.Mkdir(path, 0700)
		if err == nil {
			// The mask of the process can remove owner bits from Mkdir.
			err = os.Chmod(path, 0700)
		}
		if err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !owner(info) {
		return fmt.Errorf("%w: %s owner or type", ErrUnsafeStore, filepath.Base(path))
	}
	if info.Mode().Perm() != 0700 {
		// The user owns the directory, so the mode is corrected as ensureHome
		// does, instead of a refusal that would disable the feature.
		if err := os.Chmod(path, 0700); err != nil {
			return err
		}
	}
	return nil
}

// CheckPrivateDir reports whether path is a directory that only the current
// user can use, and it changes nothing: it never creates the directory and
// never corrects a mode. A missing path gives (false, nil). A path that is a
// symbolic link or not a directory, a directory of another user, or a mode
// other than 0700 gives ErrUnsafeStore. A caller that is about to remove files
// under path uses it to skip a location that it does not own.
func CheckPrivateDir(path string) (exists bool, err error) {
	if !filepath.IsAbs(path) {
		return false, fmt.Errorf("%w: directory must be absolute", ErrUnsafeStore)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !owner(info) {
		return false, fmt.Errorf("%w: %s owner or type", ErrUnsafeStore, filepath.Base(path))
	}
	if info.Mode().Perm() != 0700 {
		return false, fmt.Errorf("%w: %s mode", ErrUnsafeStore, filepath.Base(path))
	}
	return true, nil
}
