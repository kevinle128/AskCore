package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces the content of path with data. It writes a temp
// file in the same directory, syncs it and renames it over path, so a reader
// sees the old or the new content and never a mix. An existing regular file
// keeps its mode; a new file gets perm. When path is a symbolic link, the file
// it points to is replaced and the link stays. The temp file is removed on
// every error.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	target := canonicalPath(path)
	mode := perm.Perm()
	if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return fmt.Errorf("tools: write %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("tools: write %s: %w", path, err)
	}
	if err = tmp.Chmod(mode); err != nil {
		return fmt.Errorf("tools: write %s: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("tools: write %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("tools: write %s: %w", path, err)
	}
	if err = os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("tools: write %s: %w", path, err)
	}
	return nil
}
