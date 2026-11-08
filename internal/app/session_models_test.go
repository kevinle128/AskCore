package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/leader"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/internal/testsupport"
	"AskCore/pkg/protocol"
)

// completionsServer is an OpenAI-compatible chat completions endpoint. It
// records the key and the X-Trace header of each request. fail401 requests
// get a 401 first.
type completionsServer struct {
	srv *httptest.Server

	mu      sync.Mutex
	keys    []string
	traces  []string
	fail401 int
}

func newCompletionsServer(t *testing.T) *completionsServer {
	t.Helper()
	c := &completionsServer{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		c.mu.Lock()
		c.keys = append(c.keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		c.traces = append(c.traces, r.Header.Get("X-Trace"))
		fail := c.fail401 > 0
		if fail {
			c.fail401--
		}
		c.mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			http.NotFound(w, r)
			return
		}
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"invalid key","type":"invalid_request_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`+"\n\n"+
			"data: "+`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: [DONE]\n\n")
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *completionsServer) url() string { return c.srv.URL + "/v1" }

func (c *completionsServer) seen() (keys, traces []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.keys...), append([]string{}, c.traces...)
}

// writeModelsFile writes the user models.json with the mode that the loader
// requires. Each write sets a newer modification time, so a same-size edit is
// a new file state.
func writeModelsFile(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, "models.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	require.NoError(t, os.Chmod(path, 0o600))
	stamp := time.Now().Add(time.Duration(writeCount.Add(1)) * time.Second)
	require.NoError(t, os.Chtimes(path, stamp, stamp))
}

var writeCount atomic.Int64

// localProvider is a models.json with one OpenAI-compatible provider that only
// the file defines.
func localProvider(baseURL, apiKey, trace string, ids ...string) string {
	rows := make([]string, len(ids))
	for i, id := range ids {
		rows[i] = fmt.Sprintf(`{"id": %q}`, id)
	}
	return fmt.Sprintf(`{
  // A local server.
  "providers": {
    "local": {
      "baseUrl": %q,
      "api": "openai-completions",
      "apiKey": %q,
      "headers": {"X-Trace": %q},
      "models": [%s],
    },
  },
}`, baseURL, apiKey, trace, strings.Join(rows, ","))
}

// modelsLeaderParams are the parameters of a real leader whose sessions run
// the native agent: real credential service, real model view, real wires.
// No provider other than the file has a credential.
func modelsLeaderParams(t *testing.T, home string) LeaderParams {
	t.Helper()
	p := testACPParams(t)
	p.Home = home
	p.Initial = providers.Model{}
	guard := testsupport.LoopbackOnly(t)
	p.NewAgent = func(service *auth.Service, spec AgentSpec) (*agent.Agent, error) {
		return NewNativeAgent(NativeAgentConfig{
			Config: agent.Config{SessionID: spec.SessionID, LoopConfig: agent.LoopConfig{
				Model: spec.Start, Cwd: spec.Cwd,
				Wait: func(context.Context, time.Duration) error { return nil },
			}},
			Auth: service, Models: spec.Models, Home: home, HTTPClient: guard,
		})
	}
	return LeaderParams{
		ACPParams: p,
		Server:    leader.ServerConfig{InstanceID: "inst-models", Build: "dev", RegisterTimeout: 2 * time.Second},
	}
}

const localRow = "local/m1@openai-completions"

func modelsOf(t *testing.T, c *routingClient, sid string, usableOnly bool) protocol.ACPModelsResult {
	t.Helper()
	return decodeInto[protocol.ACPModelsResult](t, c.must(protocol.ACPModels, map[string]any{"sessionId": sid, "usableOnly": usableOnly}))
}

func rowOf(res protocol.ACPModelsResult, id string) (protocol.ACPModel, bool) {
	for _, m := range res.Models {
		if m.ModelID == id {
			return m, true
		}
	}
	return protocol.ACPModel{}, false
}

func TestUserModelsFileReloadsWhenListIsRead(t *testing.T) {
	home := privateHome(t)
	srv := newCompletionsServer(t)
	writeModelsFile(t, home, localProvider(srv.url(), "file-key", "one", "m1"))
	l := startLeader(t, modelsLeaderParams(t, home))
	c := l.client(nil)
	sid := c.newSession(t.TempDir())
	_, ok := rowOf(modelsOf(t, c, sid, false), "local/m2@openai-completions")
	require.False(t, ok)

	writeModelsFile(t, home, localProvider(srv.url(), "file-key", "one", "m1", "m2"))
	_, ok = rowOf(modelsOf(t, c, sid, false), "local/m2@openai-completions")
	require.True(t, ok, "opening the model list reads the edited file")
}

func TestRequestPathDoesNotReloadModelsFile(t *testing.T) {
	home := privateHome(t)
	srv := newCompletionsServer(t)
	writeModelsFile(t, home, localProvider(srv.url(), "key-one", "one", "m1"))
	l := startLeader(t, modelsLeaderParams(t, home))
	c := l.client(nil)
	sid := c.newSession(t.TempDir())
	st := decodeInto[protocol.ACPStateResult](t, c.must(protocol.ACPState, map[string]any{"sessionId": sid}))
	require.Equal(t, localRow, st.ModelID, "a provider with only a file key is the start choice")

	c.must("session/prompt", promptArgs(sid))
	writeModelsFile(t, home, localProvider(srv.url(), "key-two", "two", "m1"))
	c.must("session/prompt", promptArgs(sid))
	keys, traces := srv.seen()
	require.Equal(t, []string{"key-one", "key-one"}, keys, "a request reads the stored snapshot, not the edited file")
	require.Equal(t, []string{"one", "one"}, traces)

	modelsOf(t, c, sid, false)
	c.must("session/prompt", promptArgs(sid))
	keys, traces = srv.seen()
	require.Equal(t, "key-two", keys[2], "the model list rebuilds the snapshot")
	require.Equal(t, "two", traces[2])
}

func TestConfigKeyReachesCustomEndpoint(t *testing.T) {
	home := privateHome(t)
	srv := newCompletionsServer(t)
	writeModelsFile(t, home, localProvider(srv.url(), "$LOCAL_TEST_KEY", "trace-1", "m1"))
	p := modelsLeaderParams(t, home)
	p.Env = func(k string) (string, bool) { return "env-file-key", k == "LOCAL_TEST_KEY" }
	l := startLeader(t, p)
	c := l.client(nil)
	sid := c.newSession(t.TempDir())
	c.must("session/prompt", promptArgs(sid))
	keys, traces := srv.seen()
	require.Equal(t, []string{"env-file-key"}, keys, "the file key reaches the endpoint of the file as a bearer key")
	require.Equal(t, []string{"trace-1"}, traces)
}

func TestEditedProviderAsksForModelChoice(t *testing.T) {
	home := privateHome(t)
	oldHost, newHost := newCompletionsServer(t), newCompletionsServer(t)
	writeModelsFile(t, home, localProvider(oldHost.url(), "old-key", "old", "m1"))
	l := startLeader(t, modelsLeaderParams(t, home))
	c := l.client(nil)
	sid := c.newSession(t.TempDir())
	c.must("session/prompt", promptArgs(sid))

	writeModelsFile(t, home, localProvider(newHost.url(), "new-key", "new", "m1"))
	list := modelsOf(t, c, sid, false)
	require.Equal(t, localRow, list.Current)
	row, ok := rowOf(list, localRow)
	require.True(t, ok)
	require.True(t, row.NotInConfig, "the session row no longer matches the file")
	require.False(t, row.NotInAccount)

	_, e := c.call("session/prompt", promptArgs(sid))
	require.NotNil(t, e)
	require.Equal(t, string(protocol.ACPErrConfig), e.Data.Kind)
	oldKeys, _ := oldHost.seen()
	newKeys, _ := newHost.seen()
	require.Len(t, oldKeys, 1, "the old host gets no request with the new key or header")
	require.Empty(t, newKeys, "the old row never goes to the new host")

	c.must(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": localRow})
	row, _ = rowOf(modelsOf(t, c, sid, false), localRow)
	require.False(t, row.NotInConfig)
	c.must("session/prompt", promptArgs(sid))
	newKeys, newTraces := newHost.seen()
	require.Equal(t, []string{"new-key"}, newKeys)
	require.Equal(t, []string{"new"}, newTraces)
}

func TestConfigCommandForgottenBefore401IsSeen(t *testing.T) {
	home := privateHome(t)
	srv := newCompletionsServer(t)
	runs := filepath.Join(t.TempDir(), "runs")
	command := fmt.Sprintf("!printf x >> %s; printf command-key", runs)
	writeModelsFile(t, home, localProvider(srv.url(), command, "one", "m1"))
	l := startLeader(t, modelsLeaderParams(t, home))
	c := l.client(nil)
	sid := c.newSession(t.TempDir())
	countRuns := func() int {
		b, err := os.ReadFile(runs)
		if os.IsNotExist(err) {
			return 0
		}
		require.NoError(t, err)
		return len(b)
	}
	require.Equal(t, 0, countRuns(), "the usable checks of the session start run no command")

	srv.mu.Lock()
	srv.fail401 = 1
	srv.mu.Unlock()
	_, e := c.call("session/prompt", promptArgs(sid))
	require.NotNil(t, e, "the 401 ends the run")
	require.Equal(t, 1, countRuns())

	c.must("session/prompt", promptArgs(sid))
	require.Equal(t, 2, countRuns(), "the 401 forgot the cached output, so the next request ran the command again")
	c.must("session/prompt", promptArgs(sid))
	require.Equal(t, 2, countRuns(), "a success keeps the cached output")
	keys, _ := srv.seen()
	require.Equal(t, []string{"command-key", "command-key", "command-key"}, keys)

	// An edit of the file forgets every cached command output.
	writeModelsFile(t, home, localProvider(srv.url(), command, "two", "m1"))
	modelsOf(t, c, sid, false)
	c.must("session/prompt", promptArgs(sid))
	require.Equal(t, 3, countRuns())
}

func TestFileKeyProviderIsUsable(t *testing.T) {
	home := privateHome(t)
	srv := newCompletionsServer(t)
	writeModelsFile(t, home, localProvider(srv.url(), "file-key", "one", "m1", "m2"))
	l := startLeader(t, modelsLeaderParams(t, home))
	c := l.client(nil)

	// With no saved default, the start choice takes the only usable provider.
	first := c.newSession(t.TempDir())
	st := decodeInto[protocol.ACPStateResult](t, c.must(protocol.ACPState, map[string]any{"sessionId": first}))
	require.True(t, strings.HasPrefix(st.ModelID, "local/"), st.ModelID)

	// The saved default on a file-only provider starts the session.
	prefs, err := settings.NewSettingsStore(home)
	require.NoError(t, err)
	require.NoError(t, prefs.SetDefaultModel(context.Background(), "local/m2@openai-completions"))
	sid := c.newSession(t.TempDir())
	st = decodeInto[protocol.ACPStateResult](t, c.must(protocol.ACPState, map[string]any{"sessionId": sid}))
	require.Equal(t, "local/m2@openai-completions", st.ModelID)

	usable := modelsOf(t, c, sid, true)
	require.True(t, usable.UsableOnly)
	_, ok := rowOf(usable, localRow)
	require.True(t, ok)
	list := decodeInto[protocol.ACPProvidersResult](t, c.must(protocol.ACPProvidersList, map[string]any{}))
	found := false
	for _, p := range list.Providers {
		if p.ProviderID == "local" {
			found = true
			require.True(t, p.Usable)
		}
	}
	require.True(t, found)

	c.must(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": localRow})

}

func TestBadUserModelsFileKeepsLastValidRows(t *testing.T) {
	home := privateHome(t)
	writeModelsFile(t, home, localProvider("http://127.0.0.1:9/v1", "file-key", "one", "m1"))
	p := testACPParams(t)
	p.Home = home
	p.NewAgent = func(*auth.Service, AgentSpec) (*agent.Agent, error) { return nil, nil }
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Cleanup(context.Background()) })
	has := func() bool {
		for _, m := range rt.Config.Models() {
			if acp.ModelID(m) == localRow {
				return true
			}
		}
		return false
	}
	require.True(t, has())

	writeModelsFile(t, home, `{"providers": {`)
	require.True(t, has(), "a broken file keeps the last valid rows")
	_, err = rt.Config.Factory(context.Background(), acp.SessionRequest{ID: "s1", Cwd: t.TempDir()})
	require.NoError(t, err)
	trust := rt.Config.StartTrust("s1")
	require.NotNil(t, trust)
	var texts []string
	for _, d := range trust.Diagnostics {
		if d.Path == filepath.Join(home, "models.json") {
			texts = append(texts, d.Text)
		}
	}
	require.Contains(t, texts, "the last valid copy is used", "the session diagnostics show the file problem")
}

func TestModelRefreshKeepsOverrideAndSessionModel(t *testing.T) {
	home := privateHome(t)
	srv := newCompletionsServer(t)
	writeModelsFile(t, home, `{"providers": {
  "local": {"baseUrl": "`+srv.url()+`", "api": "openai-completions", "apiKey": "file-key", "models": [{"id": "m1"}]},
  "openai": {"modelOverrides": {"gpt-5.5": {"name": "Five", "cost": {"output": 9}}}},
}}`)
	prefs, err := settings.NewSettingsStore(home)
	require.NoError(t, err)
	require.NoError(t, prefs.SetDefaultModel(context.Background(), localRow))
	p := modelsLeaderParams(t, home)
	p.Env = func(k string) (string, bool) { return "sk-other", k == "OPENAI_API_KEY" }
	l := startLeader(t, p)
	c := l.client(nil)
	sid := c.newSession(t.TempDir())
	before := modelsOf(t, c, sid, false)
	require.Equal(t, localRow, before.Current)
	other := ""
	for _, m := range before.Models {
		if m.Provider == providers.ProviderOpenAI && m.ID != providers.ModelGPT55 {
			other = m.ModelID
			break
		}
	}
	require.NotEmpty(t, other, "no account list yet: every OpenAI row")

	// Another process writes the known list of the OpenAI key; the model list
	// sees the write.
	seedAccount(t, home, "sk-other", providers.ModelGPT55)
	after := modelsOf(t, c, sid, false)
	_, ok := rowOf(after, other)
	require.False(t, ok, "the cache write narrowed the OpenAI rows")
	require.Equal(t, localRow, after.Current, "a cache write never changes the session model")
	row, ok := rowOf(after, "openai/gpt-5.5@openai-responses")
	require.True(t, ok)
	require.Equal(t, "Five", row.Name, "the override wins after the cache write")
	st := decodeInto[protocol.ACPStateResult](t, c.must(protocol.ACPState, map[string]any{"sessionId": sid}))
	require.Equal(t, localRow, st.ModelID)
}

func TestCustomSavedDefaultKeepsCatalogReason(t *testing.T) {
	home := privateHome(t)
	writeModelsFile(t, home, localProvider("http://127.0.0.1:9/v1", "file-key", "one", "m1"))
	prefs, err := settings.NewSettingsStore(home)
	require.NoError(t, err)
	require.NoError(t, prefs.SetDefaultModel(context.Background(), localRow))
	p := testACPParams(t)
	p.Home = home
	p.Initial = providers.Model{}
	var start providers.Model
	p.NewAgent = func(_ *auth.Service, spec AgentSpec) (*agent.Agent, error) {
		start = spec.Start
		return nil, nil
	}
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Cleanup(context.Background()) })
	_, err = rt.Config.Factory(context.Background(), acp.SessionRequest{ID: "s1", Cwd: t.TempDir()})
	require.NoError(t, err)
	require.Equal(t, localRow, acp.ModelID(start), "the saved default on a custom row starts the session")
	require.Nil(t, rt.Config.StartFallback("s1"))

	writeModelsFile(t, home, localProvider("http://127.0.0.1:9/v1", "file-key", "one", "m2"))
	_, err = rt.Config.Factory(context.Background(), acp.SessionRequest{ID: "s2", Cwd: t.TempDir()})
	require.NoError(t, err)
	fb := rt.Config.StartFallback("s2")
	require.NotNil(t, fb)
	require.Equal(t, protocol.ACPFallbackNotInCatalog, fb.Reason, "a removed custom row is not in the catalog")
}

func TestModelsFileProblemsNameFilePositions(t *testing.T) {
	home := privateHome(t)
	writeModelsFile(t, home, `{"providers": {
  "Bad_ID": {"apiKey": "x"},
  "local": {
    "baseUrl": "http://127.0.0.1:9/v1", "api": "openai-completions", "apiKey": "k",
    "models": [{"id": ""}, {"id": "m1", "compat": {"supportsStore": true, "noSuchKey": true}}],
    "modelOverrides": {"m1": {"api": "openai-responses"}, "ghost": {"name": "G"}},
  },
}}`)
	view, err := LoadModelView(context.Background(), home, nil)
	require.NoError(t, err)
	keys := map[string]string{}
	for _, d := range view.Problems() {
		keys[d.Key] = d.Text
	}
	assert.Contains(t, keys, "providers[0]", "the dropped provider keeps its position")
	assert.Contains(t, keys, "providers[1].models[0].id")
	assert.Equal(t, "unknown compat key for this api; it is ignored", keys["providers[1].models[1].compat"])
	assert.Equal(t, "an override cannot change the api of a model", keys["providers[1].modelOverrides.api"], "a problem of the model list names the file position")
	assert.Equal(t, "the override names no model of this provider; it is ignored", keys["providers[1].modelOverrides"])
	m, err := view.Set().Find(providers.Ref{Provider: "local", ID: "m1"})
	require.NoError(t, err)
	compat, ok := m.Completions()
	require.True(t, ok)
	require.True(t, compat.SupportsStore, "a known compat key applies")
	for _, d := range view.Problems() {
		require.NotContains(t, d.Key+d.Text, "ghost")
		require.NotContains(t, d.Key+d.Text, "Bad_ID")
	}
}

func TestOverrideCostMergesWithBaseRowPrice(t *testing.T) {
	home := privateHome(t)
	writeModelsFile(t, home, `{"providers": {"openai": {"modelOverrides": {
  "gpt-5.5": {"cost": {"output": 9}},
}}}}`)
	view, err := LoadModelView(context.Background(), home, nil)
	require.NoError(t, err)
	require.Empty(t, view.Problems())
	m, err := view.Set().Find(providers.Ref{Provider: "openai", ID: providers.ModelGPT55})
	require.NoError(t, err)
	want := providers.OpenAIGPT55().Cost.Tiers[0].Price
	want.Output = 9_000_000
	require.Equal(t, providers.Cost{Tiers: []providers.Tier{{Price: want}}}, m.Cost)
}

func TestNameOverrideOnBuiltInRowKeepsBuiltInKey(t *testing.T) {
	home := privateHome(t)
	writeModelsFile(t, home, `{"providers": {"openai": {"modelOverrides": {"gpt-5.5": {"name": "Five"}}}}}`)
	service, err := NewNativeAuth(home, func(k string) (string, bool) { return "sk-env", k == "OPENAI_API_KEY" }, auth.NativeOptions{})
	require.NoError(t, err)
	view, err := LoadModelView(context.Background(), home, service)
	require.NoError(t, err)
	m, err := view.Set().Find(providers.Ref{Provider: "openai", ID: providers.ModelGPT55})
	require.NoError(t, err)
	require.Equal(t, "Five", m.Name)
	require.NoError(t, nativeAuthReady(context.Background(), service, view.snapshot(), m, "", ""), "a row with only a name override binds as a built-in row")
	sources := view.KeySources(context.Background(), service)
	for _, s := range sources {
		if s.Provider == "openai" {
			require.Equal(t, "env:OPENAI_API_KEY", s.Source)
		}
	}
	raw, err := json.Marshal(sources)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "sk-env")
}

func TestKeySourcesWithNilServiceReportsNone(t *testing.T) {
	view, err := LoadModelView(context.Background(), privateHome(t), nil)
	require.NoError(t, err)
	sources := view.KeySources(context.Background(), nil)
	require.NotEmpty(t, sources)
	for _, s := range sources {
		require.Equal(t, "none", s.Source, s.Provider)
		require.Empty(t, s.Problem, s.Provider)
	}
}

func TestBuiltInProviderWithOnlyFileKeyIsUsable(t *testing.T) {
	home := privateHome(t)
	writeModelsFile(t, home, `{"providers": {"xai": {"apiKey": "file-key"}}}`)
	l := startLeader(t, modelsLeaderParams(t, home))
	c := l.client(nil)
	list := decodeInto[protocol.ACPProvidersResult](t, c.must(protocol.ACPProvidersList, map[string]any{}))
	usable := map[string]bool{}
	for _, p := range list.Providers {
		usable[p.ProviderID] = p.Usable
	}
	require.True(t, usable["xai"], "a file key makes a provider with a status row usable")
	require.False(t, usable["anthropic"])
}

func TestFileKeyIsRefusedForRowOfAnotherEndpoint(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	require.NoError(t, err)
	service := &auth.Service{Store: store, Env: func(string) (string, bool) { return "", false }}
	const fileURL = "https://proxy.example/v1"
	file := settings.ModelsFile{Providers: map[string]settings.ProviderEntry{
		providers.ProviderOpenAI: {BaseURL: fileURL, APIKey: "file-key"},
	}}
	row := providers.OpenAIGPT55()
	row.BaseURL = fileURL
	u := userTables(userFile{file: file}, []providers.Model{row}, nil)
	entry, ok := u.entries[keyOf(row)]
	require.True(t, ok)

	snap, err := service.ResolveWith(ctx, auth.ResolveRequest{Model: row, Config: entry.configKey()})
	require.NoError(t, err)
	require.Equal(t, "config", snap.Source)
	require.Equal(t, fileURL, snap.Endpoint)

	other := row
	other.BaseURL = "https://other.example/v1"
	_, err = service.ResolveWith(ctx, auth.ResolveRequest{Model: other, Config: entry.configKey()})
	require.ErrorIs(t, err, auth.ErrNoCredential, "the file key binds only to the endpoint the file wrote it for")
}
