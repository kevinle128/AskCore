package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"
)

// ErrFileChanged is returned by FileState.Check when a file differs from the
// state recorded when the session last read or wrote it.
var ErrFileChanged = errors.New("file changed since it was last read")

// FileState remembers the modification time and size of the files a session
// has read or written, so a later change can be refused when something else
// changed the file in between. It is safe for concurrent use.
type FileState struct {
	mu    sync.Mutex
	files map[string]fileStamp
}

type fileStamp struct {
	mtime int64 // UnixNano
	size  int64
}

// NewFileState returns an empty state for one session.
func NewFileState() *FileState {
	return &FileState{files: map[string]fileStamp{}}
}

// Record stores the state of path from info, which is the result of a stat
// after a read, write or edit. A nil info forgets the record. Paths that name
// the same file through symbolic links share one record.
func (s *FileState) Record(path string, info os.FileInfo) {
	key := canonicalPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if info == nil {
		delete(s.files, key)
		return
	}
	s.files[key] = fileStamp{mtime: info.ModTime().UnixNano(), size: info.Size()}
}

// Check returns an error that wraps ErrFileChanged when the modification time
// or the size of path differs from the record, or when the file is gone. It
// returns nil when the path has no record.
func (s *FileState) Check(path string) error {
	key := canonicalPath(path)
	s.mu.Lock()
	want, ok := s.files[key]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	info, err := os.Stat(key)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s no longer exists; read it again before you change it", ErrFileChanged, path)
	case err != nil:
		return fmt.Errorf("tools: check %s: %w", path, err)
	}
	if info.ModTime().UnixNano() != want.mtime || info.Size() != want.size {
		return fmt.Errorf("%w: %s was modified at %s; read it again before you change it",
			ErrFileChanged, path, info.ModTime().Format(time.RFC3339))
	}
	return nil
}
