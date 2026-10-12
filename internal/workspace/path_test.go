package workspace_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/workspace"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestResolvePathStripsAtAndExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()

	got, err := workspace.ResolvePath(cwd, "@src/a.go")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cwd, "src", "a.go"), got)

	got, err = workspace.ResolvePath(cwd, "~/x")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "x"), got)

	got, err = workspace.ResolvePath(cwd, "~")
	require.NoError(t, err)
	assert.Equal(t, home, got)

	got, err = workspace.ResolvePath(cwd, "@~/y")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "y"), got)

	// Only one leading @ is stripped.
	got, err = workspace.ResolvePath(cwd, "@@b")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cwd, "@b"), got)

	// ~user is not a home reference; it stays a relative name.
	got, err = workspace.ResolvePath(cwd, "~other/x")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cwd, "~other", "x"), got)
}

func TestResolvePathAcceptsFileURL(t *testing.T) {
	cwd := t.TempDir()

	got, err := workspace.ResolvePath(cwd, "file:///tmp/a%20b")
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/tmp/a b"), got)

	got, err = workspace.ResolvePath(cwd, "file://localhost/tmp/c")
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/tmp/c"), got)

	_, err = workspace.ResolvePath(cwd, "file://remote.example/tmp/c")
	assert.Error(t, err)
}

func TestResolvePathJoinsRelativeWithCwd(t *testing.T) {
	cwd := t.TempDir()

	got, err := workspace.ResolvePath(cwd, "a/../b")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cwd, "b"), got)

	got, err = workspace.ResolvePath(cwd, "/abs/./dir/../f")
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/abs/f"), got)
}

func TestResolvePathRejectsEmptyAndCwdless(t *testing.T) {
	_, err := workspace.ResolvePath(t.TempDir(), "")
	assert.Error(t, err)

	_, err = workspace.ResolvePath(t.TempDir(), "@")
	assert.Error(t, err)

	_, err = workspace.ResolvePath("", "rel/x")
	assert.Error(t, err)

	got, err := workspace.ResolvePath("", "/abs/x")
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/abs/x"), got)
}

func TestPathVariantsCoverScreenshotNames(t *testing.T) {
	const narrow = " "

	screenshot := "/Users/x/Screenshot 2026-10-10 at 9.41.12 PM.png"
	assert.Contains(t, workspace.PathVariants(screenshot), "/Users/x/Screenshot 2026-10-10 at 9.41.12"+narrow+"PM.png")

	// "é" typed as one rune; macOS stores the decomposed form.
	assert.Contains(t, workspace.PathVariants("/d/café.png"), "/d/café.png")

	assert.Contains(t, workspace.PathVariants("/d/Bob's file.png"), "/d/Bob’s file.png")

	// Variants combine: a name with both a curly quote and a narrow space.
	assert.Contains(t, workspace.PathVariants("/d/Bob's shot at 1.00.00 AM.png"), "/d/Bob’s shot at 1.00.00"+narrow+"AM.png")

	assert.Contains(t, workspace.PathVariants(`/d/my\ file\ (1).txt`), "/d/my file (1).txt")
}

func TestPathVariantsExcludeOriginalAndDuplicates(t *testing.T) {
	assert.Empty(t, workspace.PathVariants("/d/plain.txt"))

	got := workspace.PathVariants("/d/Bob's file.png")
	seen := map[string]bool{}
	for _, v := range got {
		assert.NotEqual(t, "/d/Bob's file.png", v)
		assert.False(t, seen[v], "duplicate variant %q", v)
		seen[v] = true
	}
}
