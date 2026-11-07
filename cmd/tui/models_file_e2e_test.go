package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"
)

// localFileRow is the model id of the custom row that localModelsFile defines.
const localFileRow = "local/m1@openai-completions"

// localModelsFile is a user models.json with one OpenAI-compatible provider
// that only the file defines, with a literal key. No request goes to its URL
// in these tests.
const localModelsFile = `{"providers": {"local": {
  "baseUrl": "http://127.0.0.1:9/v1", "api": "openai-completions", "apiKey": "local-file-key",
  "models": [{"id": "m1"}],
}}}`

// brokenModelsFile is not valid JSONC.
const brokenModelsFile = `{"providers": {`

var modelsFileWrites atomic.Int64

// writeUserModelsFile writes <home>/models.json with the mode that the loader
// requires. Each write has a newer modification time, so the reader sees a new
// file state also when the size does not change.
func writeUserModelsFile(t *testing.T, home, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(home, 0o700))
	path := filepath.Join(home, "models.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	require.NoError(t, os.Chmod(path, 0o600))
	stamp := time.Now().Add(time.Duration(modelsFileWrites.Add(1)) * time.Second)
	require.NoError(t, os.Chtimes(path, stamp, stamp))
	return path
}

func hasModelRow(res protocol.ACPModelsResult, id string) bool {
	for _, m := range res.Models {
		if m.ModelID == id {
			return true
		}
	}
	return false
}

// diagnosticTexts returns the texts of the diagnostics of one file.
func diagnosticTexts(diags []protocol.ACPDiagnostic, path string) []string {
	var out []string
	for _, d := range diags {
		if d.Path == path {
			out = append(out, d.Text)
		}
	}
	return out
}

func leaderModels(t *testing.T, c *lineClient, sid string, usableOnly bool) protocol.ACPModelsResult {
	t.Helper()
	return rawCall[protocol.ACPModelsResult](t, c, protocol.ACPModels, map[string]any{"sessionId": sid, "usableOnly": usableOnly})
}

// A provider with only a key in the user models.json is usable in a leader
// process: it is the start choice, it is in the usableOnly list, set_model
// accepts it and saves it as the default, and the next session starts on it.
func TestLeaderFileKeyProviderIsUsable(t *testing.T) {
	bin, home := newE2EHome(t)
	writeUserModelsFile(t, home, localModelsFile)
	c := openRawLeader(t, bin, home, leaderEnvWithoutFaux(home))

	sid := rawSession(t, c)
	st := rawCall[protocol.ACPStateResult](t, c, protocol.ACPState, map[string]any{"sessionId": sid})
	require.Equal(t, localFileRow, st.ModelID, "the only usable provider is the start choice")
	usable := leaderModels(t, c, sid, true)
	require.True(t, hasModelRow(usable, localFileRow))

	set := rawCall[protocol.ACPSetModelResult](t, c, protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": localFileRow, "saveDefault": true})
	require.Equal(t, localFileRow, set.ModelID)
	require.True(t, set.DefaultSaved)

	next := rawSession(t, c)
	st = rawCall[protocol.ACPStateResult](t, c, protocol.ACPState, map[string]any{"sessionId": next})
	require.Equal(t, localFileRow, st.ModelID, "the saved default on a file-only provider starts the session")
	require.Nil(t, st.ModelFallback)
}

// A leader that loaded a valid user models.json keeps its rows after an edit
// breaks the file, and the model list names the problem.
func TestLeaderKeepsLastValidModelsFile(t *testing.T) {
	bin, home := newE2EHome(t)
	path := writeUserModelsFile(t, home, localModelsFile)
	c := openRawLeader(t, bin, home, leaderEnvWithoutFaux(home))
	sid := rawSession(t, c)

	before := leaderModels(t, c, sid, false)
	require.True(t, hasModelRow(before, localFileRow))
	require.Empty(t, diagnosticTexts(before.Diagnostics, path))

	writeUserModelsFile(t, home, brokenModelsFile)
	after := leaderModels(t, c, sid, false)
	assert.True(t, hasModelRow(after, localFileRow), "the last valid copy keeps the custom row")
	texts := diagnosticTexts(after.Diagnostics, path)
	assert.Contains(t, texts, "the last valid copy is used")
	assert.Len(t, texts, 2, "the parse problem and the last-valid-copy note")
}

// A leader that starts while the file is broken never loaded a valid copy: a
// newer build replaces the leader that read the file, and the new leader lists
// no custom row and shows the file problem. The saved default on the custom
// row falls back with the reason not in the catalog.
func TestReplacedLeaderShowsModelsFileProblem(t *testing.T) {
	bin, home := newE2EHome(t)
	path := writeUserModelsFile(t, home, localModelsFile)
	prefs, err := settings.NewSettingsStore(home)
	require.NoError(t, err)
	require.NoError(t, prefs.SetDefaultModel(context.Background(), localFileRow))
	env := withoutCredentialEnv(leaderEnv(home))
	old := openRawLeader(t, bin, home, env)
	sid := rawSession(t, old)
	st := rawCall[protocol.ACPStateResult](t, old, protocol.ACPState, map[string]any{"sessionId": sid})
	require.Equal(t, localFileRow, st.ModelID)
	paths, err := leader.ResolvePaths(home)
	require.NoError(t, err)
	owner, err := leader.ReadOwner(paths)
	require.NoError(t, err)

	writeUserModelsFile(t, home, brokenModelsFile)
	wrapper := versionWrapper(t, bin)
	cfg := e2eConnect(home, wrapper)
	cfg.CallerIsAsk = true
	cfg.Env = env
	cfg.Hello.CommitTime = versionOf(t, wrapper).CommitTime
	cl, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	require.NotEqual(t, owner.Instance, cl.Info.InstanceID, "a new leader serves")
	c := newLineClient(cl, io.Discard)
	go c.readLoop()
	t.Cleanup(c.close)
	_, f := c.call("initialize", map[string]any{"protocolVersion": 1})
	require.Nil(t, f)

	next := rawSession(t, c)
	st = rawCall[protocol.ACPStateResult](t, c, protocol.ACPState, map[string]any{"sessionId": next})
	require.NotEqual(t, localFileRow, st.ModelID)
	require.NotNil(t, st.ModelFallback)
	assert.Equal(t, localFileRow, st.ModelFallback.SavedDefault)
	assert.Equal(t, protocol.ACPFallbackNotInCatalog, st.ModelFallback.Reason)
	models := leaderModels(t, c, next, false)
	assert.False(t, hasModelRow(models, localFileRow), "a process with no valid copy has no custom row")
	texts := diagnosticTexts(models.Diagnostics, path)
	assert.Len(t, texts, 1, "the parse problem only")
	assert.NotContains(t, texts, "the last valid copy is used")
}

func acpModels(p *e2ePeer, sid string) protocol.ACPModelsResult {
	p.t.Helper()
	var res protocol.ACPModelsResult
	p.ok(protocol.ACPModels, map[string]any{"sessionId": sid}, &res)
	return res
}

// An ask acp process that loaded a valid user models.json keeps its rows
// after an edit breaks the file, and the model list names the problem.
func TestACPKeepsLastValidModelsFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask-home")
	path := writeUserModelsFile(t, home, localModelsFile)
	p := startE2E(t, e2eOptions{home: home, env: fastEnv()})
	sid := p.start()

	before := acpModels(p, sid)
	require.True(t, hasModelRow(before, localFileRow))
	require.Empty(t, diagnosticTexts(before.Diagnostics, path))

	writeUserModelsFile(t, home, brokenModelsFile)
	after := acpModels(p, sid)
	assert.True(t, hasModelRow(after, localFileRow), "the last valid copy keeps the custom row")
	assert.Contains(t, diagnosticTexts(after.Diagnostics, path), "the last valid copy is used")
}

// Ask reads no project models.json, also in a trusted project. The user file
// of the same process is read.
func TestProjectModelsFileIsNotRead(t *testing.T) {
	project, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	projectFile := filepath.Join(project, ".ask", "models.json")
	writeFiles(t, project, map[string]string{".ask/models.json": `{"providers": {"projectlocal": {
  "baseUrl": "http://127.0.0.1:9/v1", "api": "openai-completions", "apiKey": "project-key",
  "models": [{"id": "p1"}],
}}}`})
	home := filepath.Join(t.TempDir(), "ask-home")
	writeUserModelsFile(t, home, localModelsFile)
	p := startE2E(t, e2eOptions{home: home, env: fastEnv(), args: []string{"--approve"}})
	p.initialize()
	var res struct {
		SessionID string `json:"sessionId"`
	}
	p.ok("session/new", map[string]any{"cwd": project, "mcpServers": []any{}}, &res)
	require.NotEmpty(t, res.SessionID)

	var st protocol.ACPStateResult
	p.ok(protocol.ACPState, map[string]any{"sessionId": res.SessionID}, &st)
	require.NotNil(t, st.Trust)
	require.True(t, st.Trust.Trusted, "the project is trusted")
	models := acpModels(p, res.SessionID)
	require.True(t, hasModelRow(models, localFileRow), "the user file is read")
	for _, m := range models.Models {
		assert.NotEqual(t, "projectlocal", m.Provider)
	}
	for _, d := range append(models.Diagnostics, st.Diagnostics...) {
		assert.NotEqual(t, projectFile, d.Path, "no problem of the project file: it is not read")
	}
}
