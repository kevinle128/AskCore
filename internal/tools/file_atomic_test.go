package tools_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteFileAtomicKeepsModeAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.sh")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o755))
	require.NoError(t, os.Chmod(path, 0o755))

	require.NoError(t, tools.WriteFileAtomic(path, []byte("new content"), 0o600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(got))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	assert.Equal(t, []string{"run.sh"}, dirNames(t, dir))
}

func TestWriteFileAtomicLeavesNoTempOnFailure(t *testing.T) {
	dir := t.TempDir()
	// The target is a non-empty directory, so the final rename fails after the
	// temp file was written.
	target := filepath.Join(dir, "target")
	require.NoError(t, os.Mkdir(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "keep"), []byte("k"), 0o600))

	err := tools.WriteFileAtomic(target, []byte("data"), 0o644)
	require.Error(t, err)

	assert.Equal(t, []string{"target"}, dirNames(t, dir))
	assert.Equal(t, []string{"keep"}, dirNames(t, target))
}

func TestWriteFileAtomicCreatesNewFileWithPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	require.NoError(t, tools.WriteFileAtomic(path, []byte("hi"), 0o640))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	assert.Equal(t, []string{"new.txt"}, dirNames(t, dir))
}

func TestWriteFileAtomicWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, tools.WriteFileAtomic(link, []byte("new"), 0o600))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "link must stay a link")
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
}

func TestWriteFileAtomicFailsWhenDirectoryIsMissing(t *testing.T) {
	err := tools.WriteFileAtomic(filepath.Join(t.TempDir(), "missing", "a.txt"), []byte("x"), 0o644)
	assert.Error(t, err)
}
