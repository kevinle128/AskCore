package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/settings"
)

type authCommandRT func(*http.Request) (*http.Response, error)

func (f authCommandRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The helper runs the same command composition as the production wrapper.
func TestAuthCommandSubprocessHelper(t *testing.T) {
	if os.Getenv("ASK_AUTH_COMMAND_HELPER") != "1" {
		return
	}
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 {
		os.Exit(2)
	}
	if os.Getenv("ASK_AUTH_EXTERNAL_SERVER") != "" {
		os.Exit(runOAuthCommandHelper(os.Args[index:]))
	}
	external := authCommandRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.anthropic.com/v1/messages" {
			return nil, fmt.Errorf("unexpected inference destination")
		}
		if r.Header.Get("X-Api-Key") != "test-private-key" {
			return nil, fmt.Errorf("wrong inference credential")
		}
		body := `event: message_start
 data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}

 event: content_block_start
 data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

 event: content_block_delta
 data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"connected"}}

 event: content_block_stop
 data: {"type":"content_block_stop","index":0}

 event: message_delta
 data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

 event: message_stop
 data: {"type":"message_stop"}

`
		body = strings.ReplaceAll(body, "\n ", "\n")
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	lists := os.Getenv(authCommandListsEnv) == "1"
	private := authCommandRT(func(r *http.Request) (*http.Response, error) {
		// With the model refresh on, a sign-in reads the account list; that
		// GET is the only auth HTTP that a key command may send.
		if lists && r.Method == http.MethodGet && r.URL.Host == "api.anthropic.com" && r.URL.Path == "/v1/models" && r.Header.Get("X-Api-Key") == "test-private-key" {
			body := `{"data":[{"id":"claude-opus-4-7","display_name":"Claude Opus 4.7"}],"has_more":false,"last_id":"claude-opus-4-7"}`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		return nil, fmt.Errorf("key command must not use auth HTTP")
	})
	code := runWithDependencies(os.Args[index:], os.Stdin, os.Stdout, os.Stderr, runDependencies{getenv: os.Getenv, authHTTP: &http.Client{Transport: private}, inferenceHTTP: &http.Client{Transport: external}, modelRefresh: lists})
	os.Exit(code)
}

// authCommandListsEnv turns the model refresh on in the helper process, with a
// fake Anthropic model list on the auth HTTP.
const authCommandListsEnv = "ASK_AUTH_COMMAND_LISTS"

func authCommand(t *testing.T, home, input string, args ...string) (string, string, error) {
	t.Helper()
	return authCommandEnv(t, home, input, nil, args...)
}

// authCommandEnv runs the helper with extra environment. A model refresh test
// passes ASK_DISABLE_MODEL_REFRESH= to undo the switch of TestMain.
func authCommandEnv(t *testing.T, home, input string, extra []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestAuthCommandSubprocessHelper$", "--"}, args...)...)
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "ASK_HOME=") && !strings.HasPrefix(v, "ASK_AUTH_COMMAND_HELPER=") && !strings.HasPrefix(v, "ANTHROPIC_API_KEY=") {
			env = append(env, v)
		}
	}
	env = append(env, extra...)
	cmd.Env = append(env, "ASK_AUTH_COMMAND_HELPER=1", "ASK_HOME="+home, "ANTHROPIC_API_KEY=")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestAuthKeyCommandNextPromptAndLogout(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	out, errout, err := authCommand(t, home, "test-private-key\n", "auth", "login", "--provider", "anthropic", "--method", "api-key")
	if err != nil {
		t.Fatalf("login: %v %s", err, errout)
	}
	if strings.Contains(out+errout, "test-private-key") {
		t.Fatal("private input leaked")
	}
	out, errout, err = authCommand(t, home, "", "--provider", "anthropic", "-p", "hello")
	if err != nil || !strings.Contains(out, "connected") {
		t.Fatalf("next prompt: %v stdout=%s stderr=%s", err, out, errout)
	}
	_, errout, err = authCommand(t, home, "", "auth", "logout", "--provider", "anthropic")
	if err != nil {
		t.Fatalf("logout: %v %s", err, errout)
	}
	_, _, err = authCommand(t, home, "", "--provider", "anthropic", "-p", "hello")
	if err == nil {
		t.Fatal("logged-out prompt used old credential")
	}
}

// A catalog model that only the generated catalog has resolves offline in
// print mode: the run sends the model request and nothing else.
func TestPrintResolvesGeneratedModelOffline(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	_, errout, err := authCommand(t, home, "test-private-key\n", "auth", "login", "--provider", "anthropic", "--method", "api-key")
	require.NoError(t, err, errout)
	_, statErr := os.Stat(filepath.Join(home, "model-cache.json"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "with the refresh off a sign-in creates no cache")
	out, errout, err := authCommand(t, home, "", "--provider", "anthropic", "--model", "claude-opus-4-7", "-p", "hello")
	require.NoError(t, err, errout)
	require.Contains(t, out, "connected")
}

// ask auth login reads the account list in its own process, and ask -p then
// selects from it; ask auth logout deletes it.
func TestAuthLoginRefreshesAccountListAndLogoutDeletesIt(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	on := []string{disableModelRefreshEnv + "=", authCommandListsEnv + "=1"}
	_, errout, err := authCommandEnv(t, home, "test-private-key\n", on, "auth", "login", "--provider", "anthropic", "--method", "api-key")
	require.NoError(t, err, errout)
	require.NotContains(t, errout, "Warning")

	cache, err := settings.NewModelCache(home)
	require.NoError(t, err)
	data, _, err := cache.Read(context.Background())
	require.NoError(t, err)
	var ids []string
	for _, entry := range data.Accounts {
		require.Equal(t, "anthropic", entry.Provider)
		require.Equal(t, settings.AccountModelsKnown, entry.State)
		for _, item := range entry.Items {
			ids = append(ids, item.ID)
		}
	}
	require.Equal(t, []string{"claude-opus-4-7"}, ids)
	raw, err := os.ReadFile(cache.Path())
	require.NoError(t, err)
	require.NotContains(t, string(raw), "test-private-key", "the cache holds no key")

	out, errout, err := authCommand(t, home, "", "--provider", "anthropic", "--model", "claude-opus-4-7", "-p", "hello")
	require.NoError(t, err, errout)
	require.Contains(t, out, "connected")
	_, errout, err = authCommand(t, home, "", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "-p", "hello")
	require.Error(t, err, "the account list does not have the model")
	require.Contains(t, errout, "unknown model")

	_, errout, err = authCommandEnv(t, home, "", on, "auth", "logout", "--provider", "anthropic")
	require.NoError(t, err, errout)
	data, _, err = cache.Read(context.Background())
	require.NoError(t, err)
	require.Empty(t, data.Accounts, "sign-out deletes the lists of the provider")
}

func TestAuthCommandMissingMethodAndOversizeRetainRecord(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	_, _, err := authCommand(t, home, "test-private-key\n", "auth", "login", "--provider", "anthropic", "--method", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		args  []string
	}{
		{"", []string{"auth", "login", "--provider", "anthropic"}},
		{strings.Repeat("a", authInputLimit+1), []string{"auth", "login", "--provider", "anthropic", "--method", "api-key"}},
	} {
		_, _, err := authCommand(t, home, tc.input, tc.args...)
		if err == nil {
			t.Fatal("invalid command succeeded")
		}
	}
	after, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed command replaced saved credential")
	}
}
