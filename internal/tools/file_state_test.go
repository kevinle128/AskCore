package tools_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func TestFileStateRejectsChangeSinceRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	state := tools.NewFileState()
	state.Record(path, info)
	assert.NoError(t, state.Check(path))

	// Size changes.
	require.NoError(t, os.WriteFile(path, []byte("one two"), 0o600))
	err = state.Check(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, tools.ErrFileChanged)

	// A new record accepts the new content.
	info, err = os.Stat(path)
	require.NoError(t, err)
	state.Record(path, info)
	assert.NoError(t, state.Check(path))

	// Only the modification time changes; the size stays.
	require.NoError(t, os.Chtimes(path, time.Now(), info.ModTime().Add(time.Hour)))
	err = state.Check(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, tools.ErrFileChanged)
}

func TestFileStateRejectsDeletedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	state := tools.NewFileState()
	state.Record(path, info)
	require.NoError(t, os.Remove(path))
	assert.ErrorIs(t, state.Check(path), tools.ErrFileChanged)
}

func TestFileStateAllowsFileNeverRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))

	state := tools.NewFileState()
	assert.NoError(t, state.Check(path))
	assert.NoError(t, state.Check(filepath.Join(filepath.Dir(path), "missing.txt")))
}

func TestFileStateFollowsSymlinkAlias(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("one"), 0o600))
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.Symlink(target, link))
	info, err := os.Stat(target)
	require.NoError(t, err)

	state := tools.NewFileState()
	state.Record(link, info)
	require.NoError(t, os.WriteFile(target, []byte("changed"), 0o600))
	assert.ErrorIs(t, state.Check(target), tools.ErrFileChanged)
}

func TestFileStateNilInfoForgetsRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	state := tools.NewFileState()
	state.Record(path, info)
	state.Record(path, nil)
	require.NoError(t, os.WriteFile(path, []byte("changed"), 0o600))
	assert.NoError(t, state.Check(path))
}
