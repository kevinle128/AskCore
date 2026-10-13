package tools

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lockEntries() int {
	pathLocks.mu.Lock()
	defer pathLocks.mu.Unlock()
	return len(pathLocks.entries)
}

// holdsWhileAliasWaits locks first, then starts a goroutine that locks second.
// It reports that the goroutine stayed blocked while first was held and ran
// after first was released.
func holdsWhileAliasWaits(t *testing.T, first, second string) {
	t.Helper()
	unlock := PathLock(first)
	var acquired atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		release := PathLock(second)
		acquired.Store(true)
		release()
	}()
	time.Sleep(50 * time.Millisecond)
	assert.False(t, acquired.Load(), "alias %q acquired while %q was held", second, first)
	unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("alias lock was not released after unlock")
	}
	assert.True(t, acquired.Load())
}

func TestPathLockSerializesSymlinkAliases(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o600))
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.Symlink(target, link))

	holdsWhileAliasWaits(t, link, target)
	holdsWhileAliasWaits(t, target, link)
}

func TestPathLockSerializesNewFileUnderSymlinkedDir(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(real, 0o700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(real, link))

	holdsWhileAliasWaits(t, filepath.Join(link, "new.txt"), filepath.Join(real, "new.txt"))
}

func TestPathLockLetsDifferentPathsRunTogether(t *testing.T) {
	dir := t.TempDir()
	unlockA := PathLock(filepath.Join(dir, "a"))
	defer unlockA()
	done := make(chan struct{})
	go func() {
		PathLock(filepath.Join(dir, "b"))()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lock of another path blocked")
	}
}

func TestPathLockRemovesEntriesAndToleratesDoubleUnlock(t *testing.T) {
	before := lockEntries()
	dir := t.TempDir()
	unlock := PathLock(filepath.Join(dir, "a"))
	assert.Equal(t, before+1, lockEntries())
	unlock()
	unlock()
	assert.Equal(t, before, lockEntries())

	// The lock is usable again after a double unlock.
	PathLock(filepath.Join(dir, "a"))()
	assert.Equal(t, before, lockEntries())
}
