package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

// headerRecorder keeps the URL and the headers of each request and passes the
// request on to next.
type headerRecorder struct {
	next http.RoundTripper

	mu      sync.Mutex
	urls    []string
	headers []http.Header
}

func (h *headerRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.urls = append(h.urls, r.URL.String())
	h.headers = append(h.headers, r.Header.Clone())
	h.mu.Unlock()
	return h.next.RoundTrip(r)
}

func (h *headerRecorder) seen(t *testing.T, i int) (string, http.Header) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Greater(t, len(h.urls), i, "request %d was not sent", i)
	return h.urls[i], h.headers[i]
}

// printWithTransport runs ask -p as printWithDefault does, with the provider
// HTTP on transport.
func printWithTransport(t *testing.T, home string, transport http.RoundTripper, env map[string]string, argv ...string) result {
	t.Helper()
	t.Chdir(t.TempDir())
	getenv := func(k string) string {
		if k == "ASK_HOME" {
			return home
		}
		return env[k]
	}
	var out, errb bytes.Buffer
	deps := runDependencies{getenv: getenv, inferenceHTTP: &http.Client{Transport: transport}}
	code := runWithDependencies(argv, strings.NewReader(""), &out, &errb, deps)
	return result{out.String(), errb.String(), code}
}

// The key that `ask auth login` saved in auth.json reaches the Anthropic
// Messages endpoint, and the level of the pattern reaches the body. No
// environment key exists.
func TestPrintModelPatternWithLevelUsesSavedAnthropicKey(t *testing.T) {
	const savedKey = "saved-anthropic-key-0001"
	home := privateHome(t)
	store, err := settings.NewAuthStore(home)
	require.NoError(t, err)
	_, err = store.Replace(context.Background(), "anthropic", 0, settings.Credential{Method: "api-key", APIKey: savedKey})
	require.NoError(t, err)
	model := &scriptedModel{answers: []string{sseText("from the saved key")}}
	rec := &headerRecorder{next: model}
	sonnet := providers.AnthropicSonnet46()

	got := printWithTransport(t, home, rec, nil, "-p", "--model", "anthropic/"+sonnet.ID+":high", "hello")

	require.Equal(t, result{"from the saved key\n", "", 0}, got)
	url, header := rec.seen(t, 0)
	assert.Equal(t, "https://api.anthropic.com/v1/messages", url)
	assert.Equal(t, savedKey, header.Get("x-api-key"))
	assert.Empty(t, header.Get("Authorization"))
	body := model.body(t, 0)
	assert.Equal(t, sonnet.ID, body["model"])
	cfg, _ := body["output_config"].(map[string]any)
	assert.Equal(t, "high", cfg["effort"], "the level of the pattern reaches the Messages body")
	assert.NotContains(t, got.stdout+got.stderr, savedKey)
}

// localServer is an OpenAI-compatible chat completions server. It keeps the
// Authorization header and the body of each request.
type localServer struct {
	srv *httptest.Server

	mu     sync.Mutex
	auths  []string
	bodies []map[string]any
}

func newLocalServer(t *testing.T) *localServer {
	t.Helper()
	s := &localServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		s.bodies = append(s.bodies, body)
		s.mu.Unlock()
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, completionsText("from the local server"))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *localServer) requests() (auths []string, bodies []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auths...), append([]map[string]any(nil), s.bodies...)
}

// localServerHome is a private Ask home whose user models.json points the
// provider "local" at srv with the given apiKey value.
func localServerHome(t *testing.T, srv *localServer, apiKey string) string {
	t.Helper()
	home := privateHome(t)
	body := `{
  // A local OpenAI-compatible server; the defaults fill the model fields.
  "providers": {
    "local": {
      "baseUrl": "` + srv.srv.URL + `/v1",
      "api": "openai-completions",
      "apiKey": "` + apiKey + `",
      "models": [{"id": "qwen3-coder:30b"}],
    },
  },
}`
	require.NoError(t, os.WriteFile(filepath.Join(home, "models.json"), []byte(body), 0o600))
	return home
}

// A provider that only the user models.json defines answers ask -p: the
// request goes to its baseUrl with the file key, and the row has the default
// output limit.
func TestPrintLocalProviderFromModelsFile(t *testing.T) {
	srv := newLocalServer(t)
	home := localServerHome(t, srv, "local-file-key")

	got := printWithTransport(t, home, http.DefaultTransport, nil, "-p", "--model", "local/qwen3-coder:30b", "hello")

	require.Equal(t, result{"from the local server\n", "", 0}, got)
	auths, bodies := srv.requests()
	require.Len(t, bodies, 1)
	assert.Equal(t, "Bearer local-file-key", auths[0])
	assert.Equal(t, "qwen3-coder:30b", bodies[0]["model"])
	assert.EqualValues(t, 16384, bodies[0]["max_tokens"], "a custom row has the default output limit")
}

// A "$NAME" key reads the environment. When the variable is not set, the run
// exits 1 and the error names the variable, never a value.
func TestPrintModelsFileEnvTemplateKey(t *testing.T) {
	srv := newLocalServer(t)
	home := localServerHome(t, srv, "$LOCAL_KEY")

	got := printWithTransport(t, home, http.DefaultTransport, map[string]string{"LOCAL_KEY": "env-local-key"}, "-p", "--model", "local/qwen3-coder:30b", "hello")
	require.Equal(t, result{"from the local server\n", "", 0}, got)
	auths, _ := srv.requests()
	require.Equal(t, []string{"Bearer env-local-key"}, auths)

	got = printWithTransport(t, home, http.DefaultTransport, nil, "-p", "--model", "local/qwen3-coder:30b", "hello")
	assert.Equal(t, 1, got.code)
	assert.Empty(t, got.stdout)
	assert.Contains(t, got.stderr, "LOCAL_KEY")
	assert.NotContains(t, got.stderr, "env-local-key")
	auths, _ = srv.requests()
	assert.Len(t, auths, 1, "no request goes out without the key")
}
