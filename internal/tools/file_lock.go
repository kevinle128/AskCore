package tools

import (
	"path/filepath"
	"sync"
)

// pathLocks is the process-wide lock table. One leader holds many sessions, so
// two sessions that change the same file take the same lock.
var pathLocks = struct {
	mu      sync.Mutex
	entries map[string]*pathLock
}{entries: map[string]*pathLock{}}

type pathLock struct {
	mu sync.Mutex
	// refs counts the holders and waiters; it is guarded by pathLocks.mu.
	refs int
}

// PathLock blocks until it holds the lock of path, then returns the function
// that releases it. Paths that name the same file through symbolic links share
// one lock. A path that does not exist yet is keyed by its resolved parent
// directory and its base name. The table keeps an entry only while a holder or
// a waiter uses it. Calling unlock again does nothing.
func PathLock(path string) (unlock func()) {
	key := canonicalPath(path)

	pathLocks.mu.Lock()
	l := pathLocks.entries[key]
	if l == nil {
		l = &pathLock{}
		pathLocks.entries[key] = l
	}
	l.refs++
	pathLocks.mu.Unlock()

	l.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Unlock()
			pathLocks.mu.Lock()
			if l.refs--; l.refs == 0 {
				delete(pathLocks.entries, key)
			}
			pathLocks.mu.Unlock()
		})
	}
}

// canonicalPath resolves symbolic links of path. When path does not exist, it
// resolves the parent directory and joins the base name. When that fails too,
// it returns the cleaned path.
func canonicalPath(path string) string {
	p := filepath.Clean(path)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(dir, filepath.Base(p))
	}
	return p
}
