package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/providers"
)

// printWithDefault runs ask -p in dir with a private Ask home whose user
// settings save the default model saved. The environment is closed: only
// ASK_HOME and the keys in env exist, so a key of the developer cannot change
// the result. The provider HTTP goes to model.
func printWithDefault(t *testing.T, dir, home string, model *scriptedModel, env map[string]string, argv ...string) result {
	t.Helper()
	t.Chdir(dir)
	getenv := func(k string) string {
		if k == "ASK_HOME" {
			return home
		}
		return env[k]
	}
	var out, errb bytes.Buffer
	deps := runDependencies{getenv: getenv, inferenceHTTP: &http.Client{Transport: model}}
	code := runWithDependencies(argv, strings.NewReader(""), &out, &errb, deps)
	return result{out.String(), errb.String(), code}
}

// homeWithDefault makes a private Ask home whose user settings hold
// defaultModel id.
func homeWithDefault(t *testing.T, id string) string {
	t.Helper()
	home := privateHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"defaultModel":"`+id+`"}`), 0o600))
	return home
}

func (s *scriptedModel) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

// fauxModelID is the model id of the demo model in the form of defaultModel.
func fauxModelID(t *testing.T) string {
	t.Helper()
	_, m, err := fauxStream(defaultModel, func(string) string { return "" })
	require.NoError(t, err)
	return acp.ModelID(m)
}

const fixtureKey = "fixture-key"

var anthropicKey = map[string]string{"ANTHROPIC_API_KEY": fixtureKey}

func TestPrintModeUsesSavedDefaultModel(t *testing.T) {
	sonnet := providers.AnthropicSonnet46()
	home := homeWithDefault(t, acp.ModelID(sonnet))
	model := &scriptedModel{answers: []string{sseText("from the default")}}

	got := printWithDefault(t, t.TempDir(), home, model, anthropicKey, "-p", "hello")

	assert.Equal(t, result{"from the default\n", "", 0}, got)
	assert.Equal(t, sonnet.ID, model.body(t, 0)["model"])
	assert.NotContains(t, got.stdout+got.stderr, fixtureKey)
}

func TestPrintModeSavedDefaultWithoutCredentialFallsBackToFaux(t *testing.T) {
	id := acp.ModelID(providers.AnthropicSonnet46())
	home := homeWithDefault(t, id)
	model := &scriptedModel{}
	note := "Warning: default model " + id + " is not usable (no credential); using " + fauxModelID(t) + ".\n"

	got := printWithDefault(t, t.TempDir(), home, model, nil, "-p", "hello")
	assert.Equal(t, result{"hello\n", note, 0}, got)
	assert.Zero(t, model.requests())

	// JSON mode keeps stdout for events; the note goes to stderr.
	got = printWithDefault(t, t.TempDir(), home, model, nil, "--mode", "json", "hello")
	assert.Equal(t, 0, got.code)
	assert.Equal(t, note, got.stderr)
	assert.NotContains(t, got.stdout, "not usable")
}

func TestPrintModeInvalidSavedDefaultFallsBackToFaux(t *testing.T) {
	model := &scriptedModel{}
	const gone = "openai/gpt-0@openai-responses"
	got := printWithDefault(t, t.TempDir(), homeWithDefault(t, gone), model, anthropicKey, "-p", "hello")
	note := "Warning: default model " + gone + " is not usable (not in the catalog); using " + fauxModelID(t) + ".\n"
	assert.Equal(t, result{"hello\n", note, 0}, got)

	// The settings layer drops a value that is not a model id and reports it,
	// so the run has no saved default and no second line.
	home := homeWithDefault(t, "not a model id")
	got = printWithDefault(t, t.TempDir(), home, model, anthropicKey, "-p", "hello")
	diag := "Warning: settings: " + filepath.Join(home, "settings.json") + ": defaultModel: not a valid model id; the default is used\n"
	assert.Equal(t, result{"hello\n", diag, 0}, got)
	assert.Zero(t, model.requests())
}

func TestPrintModeExplicitModelBeatsSavedDefault(t *testing.T) {
	home := homeWithDefault(t, acp.ModelID(providers.AnthropicSonnet46()))
	model := &scriptedModel{}
	for _, argv := range [][]string{
		{"--provider", "faux", "-p", "hello"},
		{"--model", "faux-1", "-p", "hello"},
	} {
		got := printWithDefault(t, t.TempDir(), home, model, anthropicKey, argv...)
		assert.Equal(t, result{"hello\n", "", 0}, got, argv)
	}
	assert.Zero(t, model.requests())
}

func TestPrintModeNoSavedDefaultKeepsFaux(t *testing.T) {
	model := &scriptedModel{}
	got := printWithDefault(t, t.TempDir(), privateHome(t), model, anthropicKey, "-p", "hello")
	assert.Equal(t, result{"hello\n", "", 0}, got)
	assert.Zero(t, model.requests())
}

func TestPrintModeProjectDefaultModelNeedsTrust(t *testing.T) {
	project := t.TempDir()
	root, err := filepath.EvalSymlinks(project)
	require.NoError(t, err)
	sonnet := providers.AnthropicSonnet46()
	require.NoError(t, os.Mkdir(filepath.Join(project, ".ask"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, ".ask", "settings.json"), []byte(`{"defaultModel":"`+acp.ModelID(sonnet)+`"}`), 0o644))
	home := privateHome(t)
	model := &scriptedModel{answers: []string{sseText("from the project default")}}

	got := printWithDefault(t, project, home, model, anthropicKey, "-p", "hello")
	untrusted := "Warning: project settings ignored: " + root + " is not trusted. Use --approve, or save a decision in the TUI with /trust.\n"
	assert.Equal(t, result{"hello\n", untrusted, 0}, got, "an untrusted project cannot select the model")
	assert.Zero(t, model.requests())

	got = printWithDefault(t, project, home, model, anthropicKey, "--approve", "-p", "hello")
	assert.Equal(t, result{"from the project default\n", "", 0}, got)
	assert.Equal(t, sonnet.ID, model.body(t, 0)["model"])
}
