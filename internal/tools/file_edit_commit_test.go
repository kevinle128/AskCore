package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// editSetup returns an Edit that has read path, and the path.
func editSetup(t *testing.T, content string) (*Edit, *FileState, string, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	path := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	st, err := os.Stat(path)
	require.NoError(t, err)
	state := NewFileState()
	state.Record(path, st)
	return newEdit(state), state, dir, path
}

func runEdit(e *Edit, dir string) error {
	args, _ := json.Marshal(map[string]any{
		"path":  "a.txt",
		"edits": []map[string]string{{"oldText": "hello", "newText": "world"}},
	})
	_, err := e.Execute(context.Background(), Context{CallID: "c1", Cwd: dir}, args)
	return err
}

func TestEditFailsWhenFileChangesBeforeRename(t *testing.T) {
	t.Run("same size and a new mtime", func(t *testing.T) {
		e, _, dir, path := editSetup(t, "hello there")
		e.beforeCommit = func(p string) {
			require.NoError(t, os.WriteFile(p, []byte("HELLO there"), 0o644))
			require.NoError(t, os.Chtimes(p, time.Now(), time.Now().Add(time.Hour)))
		}

		err := runEdit(e, dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "changed on disk since you last read it")
		got, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		assert.Equal(t, "HELLO there", string(got), "the other writer's content stays")
		entries, derr := os.ReadDir(dir)
		require.NoError(t, derr)
		assert.Len(t, entries, 1, "the temp file is removed")
	})

	t.Run("a new size and the same mtime", func(t *testing.T) {
		e, _, dir, path := editSetup(t, "hello there")
		before, err := os.Stat(path)
		require.NoError(t, err)
		e.beforeCommit = func(p string) {
			require.NoError(t, os.WriteFile(p, []byte("hello there, longer"), 0o644))
			require.NoError(t, os.Chtimes(p, before.ModTime(), before.ModTime()))
		}

		err = runEdit(e, dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "changed on disk since you last read it")
		got, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		assert.Equal(t, "hello there, longer", string(got))
	})

	t.Run("no change writes the edit", func(t *testing.T) {
		e, _, dir, path := editSetup(t, "hello there")
		called := false
		e.beforeCommit = func(string) { called = true }

		require.NoError(t, runEdit(e, dir))

		assert.True(t, called, "the check ran")
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "world there", string(got))
	})
}
