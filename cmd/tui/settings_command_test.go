package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/bootstrap"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

// settingsProjectDir makes a project folder with a .ask/settings.json file
// and returns the folder and its canonical root.
func settingsProjectDir(t *testing.T, content string, mode os.FileMode) (string, string) {
	t.Helper()
	project := t.TempDir()
	root, err := filepath.EvalSymlinks(project)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(project, ".ask"), 0o755))
	path := filepath.Join(project, ".ask", "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, os.Chmod(path, mode))
	return project, root
}

func decodeSettingsDump(t *testing.T, out string) settingsDump {
	t.Helper()
	var d settingsDump
	require.NoError(t, json.Unmarshal([]byte(out), &d), out)
	return d
}

func settingSource(d settingsDump, key string) (string, string) {
	for _, v := range d.Settings {
		if v.Key == key {
			return v.Value, v.Source
		}
	}
	return "", ""
}

// clearKeyEnv hides the provider key variables of the developer, so the key
// sources of the output do not depend on them.
func clearKeyEnv(t *testing.T) {
	t.Helper()
	for _, m := range providers.GeneratedCatalog() {
		for _, name := range providers.KeyEnvNames(m.Provider) {
			t.Setenv(name, "")
		}
	}
}

func TestSettingsShowNamesSourceLayer(t *testing.T) {
	// The skills of the user home of the developer must not reach the golden file.
	t.Setenv("HOME", t.TempDir())
	clearKeyEnv(t)
	// The golden file is relative to the package folder, and the runs change
	// the working directory to the project.
	project, root := settingsProjectDir(t, `{"defaultModel":"openai/gpt-5.5@responses","env":{"SECRET_VALUE":"x"}}`, 0o644)
	home := privateHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"defaultModel":"anthropic/claude-sonnet-4-6@messages","thinkingFold":"collapsed"}`), 0o600))
	store, err := settings.NewTrustStore(home)
	require.NoError(t, err)
	require.NoError(t, store.Update(context.Background(), map[string]*bool{root: new(true)}))

	got := printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	assert.Empty(t, got.stderr)
	assert.NotContains(t, got.stdout, "SECRET_VALUE")
	d := decodeSettingsDump(t, got.stdout)
	value, source := settingSource(d, "defaultModel")
	assert.Equal(t, "openai/gpt-5.5@responses", value)
	assert.Equal(t, "project", source)
	value, source = settingSource(d, "thinkingFold")
	assert.Equal(t, "collapsed", value)
	assert.Equal(t, "user", source)
	value, source = settingSource(d, "defaultProjectTrust")
	assert.Equal(t, "ask", value)
	assert.Equal(t, "default", source)

	// The JSON shape is a public contract: the golden file pins it. The
	// temporary paths are replaced with fixed names.
	normalized := strings.ReplaceAll(got.stdout, root, "/project")
	normalized = strings.ReplaceAll(normalized, home, "/home/.ask")
	t.Chdir(packageDir)
	golden.RequireEqual(t, normalized)

	text := printInProject(t, project, home, "settings", "show")
	require.Equal(t, 0, text.code, text.stderr)
	assert.Contains(t, text.stdout, "Project trust:    trusted (saved, entry "+root+")\n")
	assert.Contains(t, text.stdout, "Project settings: loaded\n")
	assert.Regexp(t, `\n  defaultModel +openai/gpt-5\.5@responses +project\n`, text.stdout)
	assert.Regexp(t, `\n  thinkingFold +collapsed +user\n`, text.stdout)
	assert.Regexp(t, `\n  defaultProjectTrust +ask +default\n`, text.stdout)
	assert.Contains(t, text.stdout, "  project "+filepath.Join(root, ".ask", "settings.json")+": env: a project file cannot set this key; it is ignored\n")
	assert.NotContains(t, text.stdout, "SECRET_VALUE")
}

func TestSettingsShowFlagOverridesTrust(t *testing.T) {
	project, _ := settingsProjectDir(t, `{"thinkingFold":"truncated"}`, 0o644)
	home := privateHome(t)

	got := printInProject(t, project, home, "settings", "show", "--json", "-a")
	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.True(t, d.Project.Trusted)
	assert.Equal(t, "flag", d.Project.Source)
	assert.Equal(t, "loaded", d.Project.Layer)
	_, source := settingSource(d, "thinkingFold")
	assert.Equal(t, "project", source)

	got = printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	d = decodeSettingsDump(t, got.stdout)
	assert.False(t, d.Project.Trusted)
	assert.Equal(t, "no_ui", d.Project.Source)
	_, source = settingSource(d, "thinkingFold")
	assert.Equal(t, "default", source)

	_, err := os.Stat(filepath.Join(home, "trust.json"))
	assert.True(t, os.IsNotExist(err), "the command saves no decision")
}

func TestSettingsShowNeverReadsUntrustedProject(t *testing.T) {
	// Mode 000: an open of the file fails, so a read of it would give a
	// diagnostic.
	project, _ := settingsProjectDir(t, `{"defaultModel":"openai/gpt-5.5@responses"}`, 0o000)
	home := privateHome(t)

	got := printInProject(t, project, home, "settings", "show")
	require.Equal(t, 0, got.code, got.stderr)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, "Project settings: not read (untrusted)\n")
	assert.Contains(t, got.stdout, "Gated files:      .ask/settings.json\n")
	assert.NotContains(t, got.stdout, "Problems:")
	assert.NotContains(t, got.stdout, "cannot open")

	got = printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.Equal(t, "untrusted", d.Project.Layer)
	assert.Equal(t, []string{".ask/settings.json"}, d.Project.Gated)
	assert.Empty(t, d.Diagnostics)
	assert.NotNil(t, d.Diagnostics)

	// Once trusted, the file that cannot be read gives a diagnostic. Root
	// ignores file modes, so root can read the file.
	if os.Geteuid() == 0 {
		return
	}
	got = printInProject(t, project, home, "settings", "show", "--json", "--approve")
	require.Equal(t, 0, got.code, got.stderr)
	d = decodeSettingsDump(t, got.stdout)
	require.Len(t, d.Diagnostics, 1)
	assert.Equal(t, "project", d.Diagnostics[0].Layer)
}

func TestSettingsShowReportsCorruptTrustStore(t *testing.T) {
	project, _ := settingsProjectDir(t, `{}`, 0o644)
	home := privateHome(t)
	trustPath := filepath.Join(home, "trust.json")
	require.NoError(t, os.WriteFile(trustPath, []byte(`{"secret-looking`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"defaultProjectTrust":"always"}`), 0o600))

	got := printInProject(t, project, home, "settings", "show")
	require.Equal(t, 0, got.code, got.stderr)
	assert.Contains(t, got.stdout, "\nWarning: trust store "+trustPath+" is corrupt or unsafe; no project is trusted until you fix or remove it\n")
	assert.Contains(t, got.stdout, "Project trust:    untrusted (store_error)\n")
	assert.NotContains(t, got.stdout, "secret-looking")

	got = printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.Contains(t, d.Project.Warning, trustPath)
	assert.Equal(t, "store_error", d.Project.Source)
	b, err := os.ReadFile(trustPath)
	require.NoError(t, err)
	assert.Equal(t, `{"secret-looking`, string(b), "the store keeps its bytes")
}

func TestSettingsShowReportsCorruptUserSettings(t *testing.T) {
	project := t.TempDir()
	home := privateHome(t)
	userPath := filepath.Join(home, "settings.json")
	require.NoError(t, os.WriteFile(userPath, []byte(`{"defaultModel":`), 0o600))

	got := printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.Equal(t, "no_gated_files", d.Project.Source)
	require.Len(t, d.Diagnostics, 1)
	assert.Equal(t, settingsDiagnostic{Layer: "user", Path: userPath, Text: "unsafe or invalid file: invalid JSON"}, d.Diagnostics[0])
	b, err := os.ReadFile(userPath)
	require.NoError(t, err)
	assert.Equal(t, `{"defaultModel":`, string(b))
}

func TestSettingsShowStartsNoLeader(t *testing.T) {
	project, _ := settingsProjectDir(t, `{}`, 0o644)
	home := privateHome(t)
	for _, argv := range [][]string{{"settings", "show"}, {"settings", "show", "--json", "--approve"}} {
		got := printInProject(t, project, home, argv...)
		require.Equal(t, 0, got.code, got.stderr)
	}
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	assert.Empty(t, entries, "the command creates no file in the Ask home: no leader socket, no lock, no database")

	// A missing home stays missing.
	missing := filepath.Join(t.TempDir(), "absent")
	got := printInProject(t, project, missing, "settings", "show")
	require.Equal(t, 0, got.code, got.stderr)
	_, err = os.Stat(missing)
	assert.True(t, os.IsNotExist(err))
}

func TestSettingsUsage(t *testing.T) {
	home := privateHome(t)
	project := t.TempDir()
	for _, argv := range [][]string{{"settings", "--help"}, {"settings", "-h"}, {"settings", "show", "--help"}} {
		got := printInProject(t, project, home, argv...)
		assert.Equal(t, result{settingsUsage, "", 0}, got, argv)
	}
	assert.Contains(t, usage, "ask settings --help")

	for _, tc := range []struct {
		argv   []string
		stderr string
	}{
		{[]string{"settings"}, "Error: settings requires show; use ask settings --help\n"},
		{[]string{"settings", "dump"}, "Error: unknown settings command \"dump\"; use ask settings --help\n"},
		{[]string{"settings", "show", "--yaml"}, "Error: settings show does not accept \"--yaml\"; use ask settings --help\n"},
		{[]string{"settings", "show", "extra"}, "Error: settings show does not accept \"extra\"; use ask settings --help\n"},
		{[]string{"settings", "show", "-a", "-na"}, "Error: use only one of --approve and --no-approve\n"},
	} {
		got := printInProject(t, project, home, tc.argv...)
		assert.Equal(t, result{"", tc.stderr, 1}, got, tc.argv)
	}
}

func TestSettingsShowListsContextFilesAndSkillProblems(t *testing.T) {
	project, root := settingsProjectDir(t, `{}`, 0o644)
	require.NoError(t, os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("agents rules"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "CLAUDE.md"), []byte(strings.Repeat("x", bootstrap.MaxFileBytes+1)), 0o644))
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	broken := filepath.Join(userHome, ".agents", "skills", "broken", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(broken), 0o755))
	require.NoError(t, os.WriteFile(broken, []byte("no frontmatter"), 0o644))
	home := privateHome(t)

	got := printInProject(t, project, home, "settings", "show", "--json")

	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.Equal(t, []settingsContextFile{
		{Path: filepath.Join(root, "AGENTS.md"), Bytes: len("agents rules")},
		{Path: filepath.Join(root, "CLAUDE.md"), Bytes: bootstrap.MaxFileBytes + 1, Skipped: "too_large"},
	}, d.Context)
	assert.Equal(t, []settingsDiagnostic{{Layer: "user", Path: broken, Key: "skills", Text: "no frontmatter"}}, d.SkillDiagnostics)
	assert.Empty(t, d.Diagnostics)

	text := printInProject(t, project, home, "settings", "show")

	require.Equal(t, 0, text.code, text.stderr)
	assert.Contains(t, text.stdout, "\nContext files:\n  "+filepath.Join(root, "AGENTS.md")+"  12 bytes\n")
	assert.Contains(t, text.stdout, "  "+filepath.Join(root, "CLAUDE.md")+"  1048577 bytes  too_large\n")
	assert.Contains(t, text.stdout, "\nSkill problems:\n  user "+broken+": no frontmatter\n")
}

func TestSettingsShowNamesAContextFileLinkedOutsideTheProject(t *testing.T) {
	project, root := settingsProjectDir(t, `{}`, 0o644)
	require.NoError(t, os.Mkdir(filepath.Join(project, ".git"), 0o755))
	outside := filepath.Join(t.TempDir(), "rules.md")
	require.NoError(t, os.WriteFile(outside, []byte("outside rules"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(project, "AGENTS.md")))
	t.Setenv("HOME", t.TempDir())
	home := privateHome(t)

	got := printInProject(t, project, home, "settings", "show", "--json")

	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	require.Len(t, d.Context, 1)
	assert.Equal(t, filepath.Join(root, "AGENTS.md"), d.Context[0].Path)
	assert.Equal(t, "outside_project", d.Context[0].Skipped)

	text := printInProject(t, project, home, "settings", "show")

	require.Equal(t, 0, text.code, text.stderr)
	assert.Contains(t, text.stdout, filepath.Join(root, "AGENTS.md")+"  ")
	assert.Contains(t, text.stdout, "outside_project\n")
	assert.NotContains(t, text.stdout+text.stderr, "outside rules")
}

func TestSettingsShowKeepsEverySkillProblemAndShowsTheSkillsBudget(t *testing.T) {
	project, _ := settingsProjectDir(t, `{}`, 0o644)
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		path := filepath.Join(userHome, ".agents", "skills", n, "SKILL.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("no frontmatter"), 0o644))
	}
	home := privateHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"skillsMaxContextTokens":1234}`), 0o600))

	got := printInProject(t, project, home, "settings", "show", "--json")

	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.Len(t, d.SkillDiagnostics, 6)
	value, source := settingSource(d, "skillsMaxContextTokens")
	assert.Equal(t, "1234", value)
	assert.Equal(t, "user", source)
	text := printInProject(t, project, home, "settings", "show")
	assert.Contains(t, text.stdout, "skillsMaxContextTokens")
	assert.NotContains(t, text.stdout, "more skill problems")
}

func TestSettingsShowNamesKeySourceNotValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearKeyEnv(t)
	t.Setenv("LOCAL_SHOW_KEY", "env-secret-value")
	project, _ := settingsProjectDir(t, `{}`, 0o644)
	home := privateHome(t)
	runs := filepath.Join(t.TempDir(), "runs")
	models := `{"providers": {
  "local": {"baseUrl": "http://127.0.0.1:9/v1", "api": "openai-completions", "apiKey": "$LOCAL_SHOW_KEY", "models": [{"id": "m1"}]},
  "cmdp": {"baseUrl": "http://127.0.0.1:9/v2", "api": "openai-completions", "apiKey": "!printf x >> ` + runs + `; printf cmd-secret-value", "models": [{"id": "m1"}]},
  "lit": {"baseUrl": "http://127.0.0.1:9/v3", "api": "openai-completions", "apiKey": "literal-secret-value", "models": [{"id": "m1"}]},
  "unset": {"baseUrl": "http://127.0.0.1:9/v4", "api": "openai-completions", "apiKey": "$LOCAL_UNSET_KEY", "models": [{"id": "m1"}]},
}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, "models.json"), []byte(models), 0o600))
	store, err := settings.NewAuthStore(home)
	require.NoError(t, err)
	_, err = store.Replace(context.Background(), "openai", 0, settings.Credential{Method: "api-key", APIKey: "saved-secret-value"})
	require.NoError(t, err)
	before, err := os.ReadDir(home)
	require.NoError(t, err)

	got := printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.Equal(t, settingsModelsFile{Path: filepath.Join(home, "models.json"), State: "loaded"}, d.ModelsFile)
	sources := map[string]settingsKeySource{}
	for _, k := range d.KeySources {
		sources[k.Provider] = k
	}
	assert.Equal(t, "config:$LOCAL_SHOW_KEY", sources["local"].Source)
	assert.Equal(t, "config:command", sources["cmdp"].Source)
	assert.Equal(t, "config:literal", sources["lit"].Source)
	assert.Equal(t, "problem", sources["unset"].Source)
	assert.Contains(t, sources["unset"].Problem, "LOCAL_UNSET_KEY")
	assert.Equal(t, "saved:api-key", sources["openai"].Source)
	assert.Equal(t, "none", sources["anthropic"].Source)

	text := printInProject(t, project, home, "settings", "show")
	require.Equal(t, 0, text.code, text.stderr)
	assert.Regexp(t, `\n  local +config:\$LOCAL_SHOW_KEY\n`, text.stdout)
	assert.Contains(t, text.stdout, "User models:      "+filepath.Join(home, "models.json")+" (loaded)\n")
	for _, out := range []string{got.stdout, text.stdout, got.stderr, text.stderr} {
		for _, secret := range []string{"env-secret-value", "cmd-secret-value", "literal-secret-value", "saved-secret-value"} {
			assert.NotContains(t, out, secret)
		}
	}
	_, err = os.Stat(runs)
	assert.True(t, os.IsNotExist(err), "the key source is a dry run: no command runs")
	after, err := os.ReadDir(home)
	require.NoError(t, err)
	assert.Equal(t, len(before), len(after), "the command writes nothing in the Ask home")
}

func TestSettingsShowListsModelsFileProblems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project, _ := settingsProjectDir(t, `{}`, 0o644)
	home := privateHome(t)
	path := filepath.Join(home, "models.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"providers": {`), 0o600))
	got := printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	d := decodeSettingsDump(t, got.stdout)
	assert.Equal(t, "problem", d.ModelsFile.State)
	found := false
	for _, diag := range d.Diagnostics {
		found = found || diag.Path == path
	}
	assert.True(t, found, "the problem of the user models.json is listed")
}
