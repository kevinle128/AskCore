package jsonlstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
)

// A failed folder sync after the link leaves nothing behind: the caller was
// told that the first write failed, so List must not show the session.
func TestFirstWriteSyncFailureLeavesNoSession(t *testing.T) {
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	errSync := errors.New("sync failed")
	s.syncer = func(dir string) error {
		if dir == s.dir(cwd) {
			return errSync
		}
		return syncDir(dir)
	}

	_, err := s.Create(context.Background(), testHeader(cwd, sessions.NewID()), store.CreateNow)

	require.ErrorIs(t, err, errSync)
	entries, readErr := os.ReadDir(s.dir(cwd))
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.NotEqual(t, logExt, filepath.Ext(e.Name()), "no session file stays: %s", e.Name())
	}
	list, err := s.List(context.Background(), cwd)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// A folder that the first write makes is an entry of its parent, so each of
// those parents is synced. A folder that existed already is not.
func TestFirstWriteSyncsParentsOfNewFoldersOnly(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "home", "sessions")
	s, cwd := mustNew(t, root), t.TempDir()
	var synced []string
	s.syncer = func(dir string) error {
		synced = append(synced, dir)
		return syncDir(dir)
	}

	w, err := s.Create(context.Background(), testHeader(cwd, sessions.NewID()), store.CreateNow)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	assert.Equal(t, []string{root, filepath.Dir(root), base, s.dir(cwd)}, synced,
		"the parent of each of the three new folders, then the folder of the file")

	synced = nil
	w, err = s.Create(context.Background(), testHeader(cwd, sessions.NewID()), store.CreateNow)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	assert.Equal(t, []string{s.dir(cwd)}, synced, "existing folders are not synced again")
}
