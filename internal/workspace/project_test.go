package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/workspace"
)

// canonical returns the path with every symbolic link resolved, as the
// functions under test compare paths in that form.
func canonical(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(p)
	require.NoError(t, err)
	return got
}

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(p, 0o755))
}

func touch(t *testing.T, p string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(p))
	require.NoError(t, os.WriteFile(p, []byte("{}"), 0o644))
}

func TestProjectRootIsCanonicalCwd(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	mkdirAll(t, target)
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(target, link))
	askHome := filepath.Join(t.TempDir(), "askhome")

	root, hasLayer, err := workspace.ProjectRoot(link, askHome)
	require.NoError(t, err)
	assert.Equal(t, canonical(t, target), root)
	assert.True(t, hasLayer)

	missing := filepath.Join(base, "gone", "..", "missing", ".")
	root, hasLayer, err = workspace.ProjectRoot(missing, askHome)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "missing"), root)
	assert.True(t, hasLayer)

	_, _, err = workspace.ProjectRoot("", askHome)
	require.Error(t, err)
}

func TestAskHomeIsNotAProject(t *testing.T) {
	user := t.TempDir()
	askHome := filepath.Join(user, ".ask")
	touch(t, filepath.Join(askHome, "settings.json"))

	root, hasLayer, err := workspace.ProjectRoot(user, askHome)
	require.NoError(t, err)
	assert.Equal(t, canonical(t, user), root)
	assert.False(t, hasLayer, "the Ask home is not the project layer of its parent")

	// The user settings file of the Ask home must not make the parent a
	// project that needs a decision.
	gated, err := workspace.GatedFiles(root, hasLayer, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, gated)

	// An ancestor skills folder still gates the folder.
	mkdirAll(t, filepath.Join(user, ".agents", "skills"))
	gated, err = workspace.GatedFiles(root, hasLayer, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, []string{".agents/skills"}, gated)

	// The same rule applies when ASK_HOME names the home through a symbolic link.
	link := filepath.Join(t.TempDir(), "home-link")
	require.NoError(t, os.Symlink(askHome, link))
	_, hasLayer, err = workspace.ProjectRoot(user, link)
	require.NoError(t, err)
	assert.False(t, hasLayer)

	// A project .ask that is a symbolic link to the Ask home is not a layer.
	project := t.TempDir()
	require.NoError(t, os.Symlink(askHome, filepath.Join(project, ".ask")))
	_, hasLayer, err = workspace.ProjectRoot(project, askHome)
	require.NoError(t, err)
	assert.False(t, hasLayer)

	// A different .ask folder is a project layer.
	other := t.TempDir()
	mkdirAll(t, filepath.Join(other, ".ask"))
	_, hasLayer, err = workspace.ProjectRoot(other, askHome)
	require.NoError(t, err)
	assert.True(t, hasLayer)
}

func TestGatedFilesList(t *testing.T) {
	userHome := t.TempDir()
	cases := []struct {
		create string
		dir    bool
		want   string
	}{
		{".ask/settings.json", false, ".ask/settings.json"},
		{".ask/SYSTEM.md", false, ".ask/SYSTEM.md"},
		{".ask/APPEND_SYSTEM.md", false, ".ask/APPEND_SYSTEM.md"},
		{".ask/mcp.json", false, ".ask/mcp.json"},
		{".ask/models.json", false, ".ask/models.json"},
		{".ask/skills", true, ".ask/skills"},
		{".ask/prompts", true, ".ask/prompts"},
		{".ask/extensions", true, ".ask/extensions"},
		{".agents/skills", true, ".agents/skills"},
	}
	for _, tc := range cases {
		t.Run(tc.create, func(t *testing.T) {
			root := canonical(t, t.TempDir())
			p := filepath.Join(root, filepath.FromSlash(tc.create))
			if tc.dir {
				mkdirAll(t, p)
			} else {
				touch(t, p)
			}
			gated, err := workspace.GatedFiles(root, true, userHome)
			require.NoError(t, err)
			assert.Equal(t, []string{tc.want}, gated)
		})
	}

	t.Run("not gated", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		touch(t, filepath.Join(root, "AGENTS.md"))
		touch(t, filepath.Join(root, "CLAUDE.md"))
		touch(t, filepath.Join(root, ".ask", "notes.txt"))
		touch(t, filepath.Join(root, ".agents", "README.md"))
		gated, err := workspace.GatedFiles(root, true, userHome)
		require.NoError(t, err)
		assert.Empty(t, gated)
	})

	t.Run("empty .ask", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		mkdirAll(t, filepath.Join(root, ".ask"))
		gated, err := workspace.GatedFiles(root, true, userHome)
		require.NoError(t, err)
		assert.Empty(t, gated)
	})

	t.Run("a file named .ask or .agents", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		touch(t, filepath.Join(root, ".ask"))
		touch(t, filepath.Join(root, ".agents"))
		gated, err := workspace.GatedFiles(root, true, userHome)
		require.NoError(t, err)
		assert.Empty(t, gated)
	})

	t.Run("type does not matter", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		mkdirAll(t, filepath.Join(root, ".ask", "settings.json"))
		require.NoError(t, os.Symlink("/nonexistent", filepath.Join(root, ".ask", "mcp.json")))
		gated, err := workspace.GatedFiles(root, true, userHome)
		require.NoError(t, err)
		assert.Equal(t, []string{".ask/mcp.json", ".ask/settings.json"}, gated)
	})

	t.Run("all sorted", func(t *testing.T) {
		parent := canonical(t, t.TempDir())
		root := filepath.Join(parent, "repo")
		mkdirAll(t, filepath.Join(parent, ".agents", "skills"))
		mkdirAll(t, filepath.Join(root, ".agents", "skills"))
		mkdirAll(t, filepath.Join(root, ".ask", "skills"))
		touch(t, filepath.Join(root, ".ask", "settings.json"))
		gated, err := workspace.GatedFiles(root, true, userHome)
		require.NoError(t, err)
		assert.Equal(t, []string{
			".agents/skills",
			".ask/settings.json",
			".ask/skills",
			filepath.Join(parent, ".agents", "skills"),
		}, gated)
	})
}

func TestAgentsFileIsNotGated(t *testing.T) {
	root := canonical(t, t.TempDir())
	touch(t, filepath.Join(root, "AGENTS.md"))
	touch(t, filepath.Join(root, "CLAUDE.md"))
	gated, err := workspace.GatedFiles(root, true, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, gated)
}

func TestGatedFilesSkipsUserAgentsSkills(t *testing.T) {
	userHome := canonical(t, t.TempDir())
	mkdirAll(t, filepath.Join(userHome, ".agents", "skills"))
	root := filepath.Join(userHome, "code", "repo")
	mkdirAll(t, root)

	gated, err := workspace.GatedFiles(root, true, userHome)
	require.NoError(t, err)
	assert.Empty(t, gated, "the user skills folder is not a project resource")

	// The user home given through a symbolic link is skipped too.
	link := filepath.Join(t.TempDir(), "home-link")
	require.NoError(t, os.Symlink(userHome, link))
	gated, err = workspace.GatedFiles(root, true, link)
	require.NoError(t, err)
	assert.Empty(t, gated)

	ancestor := filepath.Join(userHome, "code")
	mkdirAll(t, filepath.Join(ancestor, ".agents", "skills"))
	gated, err = workspace.GatedFiles(root, true, userHome)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(ancestor, ".agents", "skills")}, gated)
}

func TestGatedFilesNeverOpensFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can open a file with mode 000")
	}
	root := canonical(t, t.TempDir())
	p := filepath.Join(root, ".ask", "settings.json")
	touch(t, p)
	require.NoError(t, os.Chmod(p, 0o000))
	gated, err := workspace.GatedFiles(root, true, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, []string{".ask/settings.json"}, gated)
}

func TestGatedFilesReportsStatError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can search a folder with mode 000")
	}
	root := canonical(t, t.TempDir())
	ask := filepath.Join(root, ".ask")
	mkdirAll(t, ask)
	require.NoError(t, os.Chmod(ask, 0o000))
	t.Cleanup(func() { _ = os.Chmod(ask, 0o755) })
	_, err := workspace.GatedFiles(root, true, t.TempDir())
	require.Error(t, err, "a folder that cannot be searched must not look empty")
}
