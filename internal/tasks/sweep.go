package tasks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// DefaultRetention is how long the output files of a session stay on disk
// after the last write.
const DefaultRetention = 7 * 24 * time.Hour

// Sweep removes session directories under root whose newest entry is older
// than maxAge, measured from now. It keeps the directory named keep.
//
// A directory counts as a session directory only if it has the shape that the
// sink makes: a real directory (not a link), owned by the current user, mode
// 0700, with only regular files whose names end in ".log". Sweep skips every
// other entry, so a root that holds foreign data loses nothing. It removes the
// files one by one and then the empty directory; it never calls RemoveAll and
// never follows a symbolic link. A root that is a symbolic link is refused.
//
// Sweep runs once and returns. It never schedules itself. A missing root is
// not an error. Sweep tries every directory and returns the joined errors.
func Sweep(root string, maxAge time.Duration, now time.Time, keep string) error {
	if maxAge <= 0 {
		return fmt.Errorf("tasks: sweep age must be positive, got %s", maxAge)
	}
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("tasks: sweep root is not a real directory: %s", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	cutoff := now.Add(-maxAge)
	var errs []error
	for _, e := range entries {
		if e.Name() == keep {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, ok, err := sessionFiles(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok {
			continue
		}
		newest, err := newestEntry(dir, files)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if newest.Before(cutoff) {
			errs = append(errs, removeSession(dir, files)...)
		}
	}
	return errors.Join(errs...)
}

// sessionFiles lists dir and reports whether it has the shape of a session
// directory. Lstat does not follow a link, and a directory entry reports its
// own type, so a link in dir is never a regular file here. A directory that
// vanished while Sweep looked is not a session directory and not an error.
func sessionFiles(dir string) (files []fs.DirEntry, ok bool, err error) {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return nil, false, nil
	}
	files, err = os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	for _, f := range files {
		if !f.Type().IsRegular() || !strings.HasSuffix(f.Name(), ".log") {
			return nil, false, nil
		}
	}
	return files, true, nil
}

// ownedByCurrentUser reports whether the user of the process owns the file.
func ownedByCurrentUser(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// removeSession removes the files of a session directory one by one, then the
// directory. os.Remove fails on a directory that gained an entry meanwhile, so
// a late file is never deleted by a recursive call.
func removeSession(dir string, files []fs.DirEntry) []error {
	var errs []error
	for _, f := range files {
		if err := os.Remove(filepath.Join(dir, f.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	return errs
}

// newestEntry returns the latest modification time of a session directory and
// of its files. Sinks write files only, so it does not recurse.
func newestEntry(dir string, files []fs.DirEntry) (time.Time, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return time.Time{}, err
	}
	newest := info.ModTime()
	for _, f := range files {
		fi, err := f.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed while Sweep looked
		}
		if err != nil {
			return time.Time{}, err
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest, nil
}
