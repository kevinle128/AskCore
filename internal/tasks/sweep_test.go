package tasks_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tasks"
)

func writeAged(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("log"), 0o600))
	when := now.Add(-age)
	require.NoError(t, os.Chtimes(path, when, when))
}

// ageDir sets the time of a directory after its files, because writing a file
// changes the time of the directory.
func ageDir(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	when := now.Add(-age)
	require.NoError(t, os.Chtimes(path, when, when))
}

func TestSweepRemovesOldSessionDirs(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	week := tasks.DefaultRetention
	mkdir := func(name string) string {
		dir := filepath.Join(root, name)
		require.NoError(t, os.Mkdir(dir, 0o700))
		return dir
	}

	old := mkdir("old")
	writeAged(t, filepath.Join(old, "a.log"), week+time.Hour, now)
	ageDir(t, old, week+time.Hour, now)

	fresh := mkdir("fresh")
	writeAged(t, filepath.Join(fresh, "a.log"), time.Hour, now)

	mixed := mkdir("mixed") // old file, new file: the newest entry decides
	writeAged(t, filepath.Join(mixed, "a.log"), week+time.Hour, now)
	writeAged(t, filepath.Join(mixed, "b.log"), time.Minute, now)
	ageDir(t, mixed, week+time.Hour, now)

	empty := mkdir("empty-old") // no entry: the directory time decides
	ageDir(t, empty, week+time.Hour, now)

	kept := mkdir("current")
	writeAged(t, filepath.Join(kept, "a.log"), 3*week, now)
	ageDir(t, kept, 3*week, now)

	stray := filepath.Join(root, "stray.log") // not a directory
	writeAged(t, stray, 3*week, now)

	outside := t.TempDir() // a symlink to it must not be followed
	writeAged(t, filepath.Join(outside, "keep.log"), 3*week, now)
	ageDir(t, outside, 3*week, now)
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(outside, link))

	require.NoError(t, tasks.Sweep(root, week, now, "current"))

	assert.NoDirExists(t, old)
	assert.NoDirExists(t, empty)
	assert.DirExists(t, fresh)
	assert.DirExists(t, mixed)
	assert.DirExists(t, kept)
	assert.FileExists(t, stray)
	assert.FileExists(t, filepath.Join(outside, "keep.log"))
	_, err := os.Lstat(link)
	assert.NoError(t, err, "the symlink is left alone")
}

func TestSweepEdgeCases(t *testing.T) {
	now := time.Now()
	assert.NoError(t, tasks.Sweep(filepath.Join(t.TempDir(), "missing"), time.Hour, now, ""), "a missing root has nothing to remove")
	assert.Error(t, tasks.Sweep(t.TempDir(), 0, now, ""), "a zero age would remove everything")
	assert.Error(t, tasks.Sweep(t.TempDir(), -time.Hour, now, ""))

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.Error(t, tasks.Sweep(file, time.Hour, now, ""), "a root that is not a directory is an error")
}

// session makes a session directory with the given files, all older than the
// retention, and returns its path.
func session(t *testing.T, root, name string, files []string, now time.Time) string {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.Mkdir(dir, 0o700))
	for _, f := range files {
		writeAged(t, filepath.Join(dir, f), 3*tasks.DefaultRetention, now)
	}
	ageDir(t, dir, 3*tasks.DefaultRetention, now)
	return dir
}

func TestSweepRefusesSymlinkedRoot(t *testing.T) {
	now := time.Now()
	real := t.TempDir()
	important := session(t, real, "important-work", []string{"notes.txt"}, now)
	old := session(t, real, "old-session", []string{"a.log"}, now)
	link := filepath.Join(t.TempDir(), "tasks")
	require.NoError(t, os.Symlink(real, link))

	err := tasks.Sweep(link, tasks.DefaultRetention, now, "")

	require.Error(t, err, "a root that is a link is refused")
	assert.DirExists(t, important)
	assert.DirExists(t, old, "nothing under a linked root is removed")
}

func TestSweepKeepsDirectoriesThatAreNotSessionShaped(t *testing.T) {
	now := time.Now()
	root := t.TempDir()

	foreignFile := session(t, root, "foreign-file", []string{"a.log", "notes.txt"}, now)
	withSubdir := session(t, root, "with-subdir", []string{"a.log"}, now)
	nested := filepath.Join(withSubdir, "nested")
	require.NoError(t, os.Mkdir(nested, 0o700))
	ageDir(t, nested, 3*tasks.DefaultRetention, now)
	ageDir(t, withSubdir, 3*tasks.DefaultRetention, now)

	withLink := session(t, root, "with-link", []string{"a.log"}, now)
	outside := filepath.Join(t.TempDir(), "outside.log")
	writeAged(t, outside, 3*tasks.DefaultRetention, now)
	require.NoError(t, os.Symlink(outside, filepath.Join(withLink, "b.log")))
	ageDir(t, withLink, 3*tasks.DefaultRetention, now)

	openMode := session(t, root, "open-mode", []string{"a.log"}, now)
	require.NoError(t, os.Chmod(openMode, 0o755))

	valid := session(t, root, "valid-old", []string{"a.log", "b.log"}, now)

	require.NoError(t, tasks.Sweep(root, tasks.DefaultRetention, now, ""))

	assert.DirExists(t, foreignFile, "a file that is not a log keeps the directory")
	assert.FileExists(t, filepath.Join(foreignFile, "a.log"), "no file of a kept directory is removed")
	assert.DirExists(t, withSubdir, "a subdirectory keeps the directory")
	assert.DirExists(t, withLink, "a link keeps the directory")
	assert.FileExists(t, outside, "a link is never followed")
	assert.DirExists(t, openMode, "a mode other than 0700 keeps the directory")
	assert.NoDirExists(t, valid, "a session directory with only logs is removed")
}
