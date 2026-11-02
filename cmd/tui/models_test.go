package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const modelsTestKey = "test-openai-models-key"

// modelsVendor answers the OpenAI model list and the models.dev payload in
// memory. Any other request fails, so a test cannot reach the network.
type modelsVendor struct {
	mu       sync.Mutex
	listCode int
	listIDs  []string
	lists    int
	devTags  []string
	devCode  int // 0 answers the payload
	// onList runs before a list answer.
	onList func()
}

func (v *modelsVendor) RoundTrip(r *http.Request) (*http.Response, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case r.URL.Host == "api.openai.com" && r.URL.Path == "/v1/models":
		if r.Header.Get("Authorization") != "Bearer "+modelsTestKey {
			return nil, errors.New("wrong list credential")
		}
		v.lists++
		if v.onList != nil {
			v.onList()
		}
		data := make([]map[string]string, 0, len(v.listIDs))
		for _, id := range v.listIDs {
			data = append(data, map[string]string{"id": id})
		}
		body, _ := json.Marshal(map[string]any{"data": data})
		if v.listCode != http.StatusOK {
			// A vendor message must never reach the output.
			body = []byte(`{"error":{"message":"vendor secret text"}}`)
		}
		return modelsResponse(r, v.listCode, string(body), ""), nil
	case r.URL.Host == "models.dev":
		v.devTags = append(v.devTags, r.Header.Get("If-None-Match"))
		if v.devCode != 0 {
			return modelsResponse(r, v.devCode, "", ""), nil
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			return modelsResponse(r, http.StatusNotModified, "", `"v1"`), nil
		}
		return modelsResponse(r, http.StatusOK, `{"openai":{"models":{}}}`, `"v1"`), nil
	}
	return nil, fmt.Errorf("unexpected request to %s", r.URL.Host)
}

func modelsResponse(r *http.Request, code int, body, etag string) *http.Response {
	h := http.Header{"Content-Type": {"application/json"}}
	if etag != "" {
		h.Set("ETag", etag)
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

// runModels runs the command in process with the product composition. The
// automatic refresh is off in two ways, so the run also proves that the
// explicit command ignores both.
func runModels(t *testing.T, home string, v *modelsVendor, withKey bool, args ...string) (int, string, string) {
	t.Helper()
	vars := map[string]string{"ASK_HOME": home, disableModelRefreshEnv: "1"}
	if withKey {
		vars["OPENAI_API_KEY"] = modelsTestKey
	}
	client := &http.Client{Transport: v}
	deps := runDependencies{getenv: func(k string) string { return vars[k] }, authHTTP: client, modelHTTP: client}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(append([]string{"models"}, args...), strings.NewReader(""), &stdout, &stderr, deps)
	require.NotContains(t, stdout.String()+stderr.String(), modelsTestKey, "no credential in the output")
	require.NotContains(t, stdout.String()+stderr.String(), "vendor secret text", "no vendor text in the output")
	return code, stdout.String(), stderr.String()
}

func modelsHome(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ask")
}

// modelLine finds the line of name and returns its count and state.
func modelLine(t *testing.T, out, name string) (string, string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + ` +(\d+) models  (known|unknown|no credential)$`)
	m := re.FindStringSubmatch(out)
	require.NotNil(t, m, "no line for %s in:\n%s", name, out)
	return m[1], m[2]
}

func TestModelsRefreshKeyProviderKnownAndMetadataNotModified(t *testing.T) {
	home := modelsHome(t)
	v := &modelsVendor{listCode: http.StatusOK, listIDs: []string{"gpt-5.5", "gpt-5.4", "gpt-5-mini"}}
	code, out, errout := runModels(t, home, v, true, "refresh", "--provider", "openai")
	require.Equal(t, 0, code, errout)
	count, state := modelLine(t, out, "openai")
	require.Equal(t, "known", state)
	require.Equal(t, "3", count, "the list has the three listed catalog models")
	require.Regexp(t, `(?m)^metadata +updated$`, out)
	require.Equal(t, 2, strings.Count(out, "\n"), "one provider line and one metadata line")
	require.FileExists(t, filepath.Join(home, "model-cache.json"))

	code, out, errout = runModels(t, home, v, true, "refresh", "--provider", "openai")
	require.Equal(t, 0, code, errout)
	require.Regexp(t, `(?m)^metadata +not modified$`, out)
	require.Equal(t, []string{"", `"v1"`}, v.devTags, "the second run sends the stored ETag")
	_, state = modelLine(t, out, "openai")
	require.Equal(t, "known", state)
}

func TestModelsRefreshMetadataFailureKeepsExitZero(t *testing.T) {
	home := modelsHome(t)
	v := &modelsVendor{listCode: http.StatusOK, listIDs: []string{"gpt-5.5"}, devCode: http.StatusInternalServerError}
	code, out, errout := runModels(t, home, v, true, "refresh", "--provider", "openai")
	require.Equal(t, 0, code, errout)
	require.Regexp(t, `(?m)^metadata +failed$`, out)
	count, state := modelLine(t, out, "openai")
	require.Equal(t, []string{"1", "known"}, []string{count, state})
}

func TestModelsRefreshAllProvidersWithoutCredential(t *testing.T) {
	home := modelsHome(t)
	v := &modelsVendor{listCode: http.StatusOK}
	code, out, errout := runModels(t, home, v, false, "refresh")
	require.Equal(t, 0, code, errout)
	for _, provider := range []string{"anthropic", "openai", "xai", "alibaba-token-plan"} {
		count, state := modelLine(t, out, provider)
		require.Equal(t, "no credential", state, provider)
		require.NotEqual(t, "0", count, "%s keeps its catalog models", provider)
	}
	require.Zero(t, v.lists, "no list request without a credential")
}

func TestModelsRefreshListFailureIsUnknown(t *testing.T) {
	home := modelsHome(t)
	v := &modelsVendor{listCode: http.StatusInternalServerError}
	code, out, errout := runModels(t, home, v, true, "refresh", "--provider", "openai")
	require.Equal(t, 0, code, errout)
	count, state := modelLine(t, out, "openai")
	require.Equal(t, "unknown", state)
	require.NotEqual(t, "0", count, "an unknown list keeps every catalog model")
	require.Empty(t, errout)

	// A failure after a known list keeps that list but reports this run.
	v.listCode, v.listIDs = http.StatusOK, []string{"gpt-5.5", "gpt-5.4"}
	code, out, errout = runModels(t, home, v, true, "refresh", "--provider", "openai")
	require.Equal(t, 0, code, errout)
	count, state = modelLine(t, out, "openai")
	require.Equal(t, []string{"2", "known"}, []string{count, state})
	v.listCode = http.StatusInternalServerError
	code, out, errout = runModels(t, home, v, true, "refresh", "--provider", "openai")
	require.Equal(t, 0, code, errout)
	count, state = modelLine(t, out, "openai")
	require.Equal(t, []string{"2", "unknown"}, []string{count, state}, "the last good list stays")
}

func TestModelsRefreshCacheWriteErrorExitsOne(t *testing.T) {
	home := modelsHome(t)
	v := &modelsVendor{listCode: http.StatusOK, listIDs: []string{"gpt-5.5"}}
	// The list answer arrives after the cache file and its lock exist. A
	// directory in place of the lock file then makes the write of the list
	// fail: the home repairs its own mode, so a mode change cannot do it.
	lock := filepath.Join(home, "model-cache.json.lock")
	// onList runs on a refresher goroutine, so it must not stop the test.
	v.onList = func() {
		assert.NoError(t, os.Remove(lock))
		assert.NoError(t, os.Mkdir(lock, 0o700))
	}
	code, out, errout := runModels(t, home, v, true, "refresh", "--provider", "openai")
	require.Equal(t, 1, code, "stdout=%s stderr=%s", out, errout)
	require.Empty(t, out)
	require.Contains(t, errout, "model cache")
}

func TestModelsUsageErrors(t *testing.T) {
	home := modelsHome(t)
	v := &modelsVendor{listCode: http.StatusOK}
	for _, args := range [][]string{{}, {"list"}, {"refresh", "--bogus"}, {"refresh", "--provider", "faux"}, {"refresh", "extra"}} {
		code, out, errout := runModels(t, home, v, true, args...)
		require.Equal(t, 1, code, "args %v", args)
		require.Empty(t, out)
		require.Contains(t, errout, "Error:", "args %v", args)
	}
	code, out, _ := runModels(t, home, v, true, "--help")
	require.Equal(t, 0, code)
	require.Contains(t, out, "ask models refresh [--provider <id>]")
	require.Zero(t, v.lists)
	require.Empty(t, v.devTags)
	_, err := os.Stat(filepath.Join(home, "model-cache.json"))
	require.ErrorIs(t, err, os.ErrNotExist, "a usage error writes nothing")
}

const (
	modelsHelperEnv = "ASK_MODELS_COMMAND_HELPER"
	modelsServerEnv = "ASK_MODELS_TEST_SERVER"
)

// redirectTransport sends every request to a local test server.
type redirectTransport struct{ target *url.URL }

func (r redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = r.target.Scheme, r.target.Host
	out.Host = ""
	return http.DefaultTransport.RoundTrip(out)
}

// The helper runs the product composition in a child process, so that a test
// can send it a real signal. Every request goes to the server of the parent.
func TestModelsCommandSubprocessHelper(t *testing.T) {
	if os.Getenv(modelsHelperEnv) != "1" {
		return
	}
	target, err := url.Parse(os.Getenv(modelsServerEnv))
	if err != nil || target.Host == "" {
		os.Exit(2)
	}
	client := &http.Client{Transport: redirectTransport{target: target}}
	os.Exit(runWithDependencies([]string{"models", "refresh", "--provider", "openai"}, os.Stdin, os.Stdout, os.Stderr,
		runDependencies{getenv: os.Getenv, authHTTP: client, modelHTTP: client}))
}

func TestModelsRefreshSignals(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  os.Signal
		code int
	}{{"interrupt", os.Interrupt, 130}, {"terminate", syscall.SIGTERM, 143}, {"hangup", syscall.SIGHUP, 129}} {
		t.Run(tc.name, func(t *testing.T) {
			arrived := make(chan struct{}, 4)
			// Each request waits until the child ends, so the signal always
			// comes during the refresh.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				arrived <- struct{}{}
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)
			cmd := exec.Command(os.Args[0], "-test.run=^TestModelsCommandSubprocessHelper$")
			env := []string{}
			for _, kv := range os.Environ() {
				if !strings.HasPrefix(kv, "ASK_HOME=") && !strings.Contains(strings.SplitN(kv, "=", 2)[0], "API_KEY") {
					env = append(env, kv)
				}
			}
			cmd.Env = append(env, modelsHelperEnv+"=1", modelsServerEnv+"="+srv.URL, "ASK_HOME="+modelsHome(t), "OPENAI_API_KEY="+modelsTestKey)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			require.NoError(t, cmd.Start())
			select {
			case <-arrived:
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("no refresh request: %s", stderr.String())
			}
			require.NoError(t, cmd.Process.Signal(tc.sig))
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, "stderr=%s", stderr.String())
				require.Equal(t, tc.code, exit.ExitCode(), "stderr=%s", stderr.String())
			case <-time.After(8 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatal("models command did not stop")
			}
			require.NotContains(t, stdout.String()+stderr.String(), modelsTestKey)
		})
	}
}
