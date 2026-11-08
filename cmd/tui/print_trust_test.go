package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// printInProject runs ask -p in a project folder with its own Ask home.
func printInProject(t *testing.T, project, home string, argv ...string) result {
	t.Helper()
	t.Chdir(project)
	getenv := func(k string) string {
		if k == "ASK_HOME" {
			return home
		}
		return os.Getenv(k)
	}
	var out, errb bytes.Buffer
	code := runWithDependencies(argv, strings.NewReader(""), &out, &errb, runDependencies{getenv: getenv})
	return result{out.String(), errb.String(), code}
}

func privateHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(home, 0o700))
	return home
}

func TestPrintModeUntrustedWithoutApprove(t *testing.T) {
	project := t.TempDir()
	root, err := filepath.EvalSymlinks(project)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(project, ".ask"), 0o755))
	settingsPath := filepath.Join(root, ".ask", "settings.json")
	// A key that a project cannot set gives a diagnostic only when the project
	// layer is read, so the diagnostic shows that the layer loaded.
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{"env":{"SECRET_VALUE":"x"}}`), 0o644))
	home := privateHome(t)
	untrusted := "Warning: project settings ignored: " + root + " is not trusted. Use --approve, or save a decision in the TUI with /trust.\n"

	got := printInProject(t, project, home, "-p", "hello")
	assert.Equal(t, result{"hello\n", untrusted, 0}, got)

	for _, flag := range []string{"--approve", "-a"} {
		got = printInProject(t, project, home, flag, "-p", "hello")
		assert.Equal(t, 0, got.code, flag)
		assert.Equal(t, "hello\n", got.stdout, flag)
		assert.Equal(t, "Warning: settings: "+settingsPath+": env: a project file cannot set this key; it is ignored\n", got.stderr, flag)
		assert.NotContains(t, got.stderr, "SECRET_VALUE")
	}

	// JSON mode keeps stdout for events; the warning goes to stderr.
	got = printInProject(t, project, home, "--mode", "json", "hello")
	assert.Equal(t, 0, got.code)
	assert.Equal(t, untrusted, got.stderr)
	assert.NotContains(t, got.stdout, "not trusted")

	// --no-approve denies even when the user default trusts every project.
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"defaultProjectTrust":"always"}`), 0o600))
	got = printInProject(t, project, home, "-p", "hello")
	assert.Equal(t, "Warning: settings: "+settingsPath+": env: a project file cannot set this key; it is ignored\n", got.stderr, "always trusts the project")
	got = printInProject(t, project, home, "--no-approve", "-p", "hello")
	assert.Equal(t, untrusted, got.stderr)

	_, err = os.Stat(filepath.Join(home, "trust.json"))
	assert.True(t, os.IsNotExist(err), "print mode saves no decision")

	got = printInProject(t, project, home, "-a", "-na", "-p", "hello")
	assert.Equal(t, result{"", "Error: use only one of --approve and --no-approve\n", 1}, got)
}

func TestPrintModeCorruptTrustStoreWarnsOnce(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(project, ".agents", "skills"), 0o755))
	home := privateHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"defaultProjectTrust":"always"}`), 0o600))
	const bad = `{"broken`
	require.NoError(t, os.WriteFile(filepath.Join(home, "trust.json"), []byte(bad), 0o600))

	got := printInProject(t, project, home, "-p", "hello")
	assert.Equal(t, 0, got.code)
	assert.Equal(t, "hello\n", got.stdout)
	lines := strings.Split(strings.TrimSuffix(got.stderr, "\n"), "\n")
	require.Len(t, lines, 1, "%q", got.stderr)
	assert.Contains(t, lines[0], filepath.Join(home, "trust.json"))
	assert.NotContains(t, lines[0], "broken")
	b, err := os.ReadFile(filepath.Join(home, "trust.json"))
	require.NoError(t, err)
	assert.Equal(t, bad, string(b))

	// A plain folder is trusted with no question; a saved decision applies.
	plain := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "trust.json"), []byte("{}\n"), 0o600))
	got = printInProject(t, plain, home, "-p", "hello")
	assert.Equal(t, result{"hello\n", "", 0}, got)
	root, err := filepath.EvalSymlinks(project)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, "trust.json"), []byte(`{"`+root+`":true}`), 0o600))
	got = printInProject(t, project, home, "-p", "hello")
	assert.Equal(t, result{"hello\n", "", 0}, got)
}
