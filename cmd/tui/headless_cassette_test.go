package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/cassette"
)

// recordEnv makes useCassette record from the real model instead of
// replaying. recordBudgetEnv changes the request limit of one recording.
const (
	recordEnv       = "ASK_RECORD"
	recordBudgetEnv = "ASK_RECORD_MAX_REQUESTS"
	defaultBudget   = 10
)

// useCassette returns the options and getenv for a Token Plan run of model
// that goes through testdata/cassettes/<test name>.yaml. It replays by
// default, with a placeholder API key. With ASK_RECORD=1 it records from the
// real model with the key from the environment. The test fails at cleanup
// when the cassette reports a mismatch or an unused recorded request.
func useCassette(t *testing.T, model string) (options, func(string) string) {
	t.Helper()
	path := filepath.Join("testdata", "cassettes", strings.ReplaceAll(t.Name(), "/", "_")+".yaml")
	mode, opts := cassette.Replay, []cassette.Option(nil)
	getenv := func(k string) string {
		if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "replay-placeholder-key"
		}
		return ""
	}
	if os.Getenv(recordEnv) == "1" {
		budget := defaultBudget
		if v := os.Getenv(recordBudgetEnv); v != "" {
			n, err := strconv.Atoi(v)
			require.NoError(t, err, "%s must be a number", recordBudgetEnv)
			budget = n
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		mode, opts, getenv = cassette.Record, []cassette.Option{cassette.WithBudget(budget)}, os.Getenv
		t.Cleanup(http.DefaultClient.CloseIdleConnections)
	}
	rec, err := cassette.New(path, mode, opts...)
	require.NoError(t, err, "re-record with: %s=1 go test -run '^%s$' ./cmd/tui", recordEnv, t.Name())
	t.Cleanup(func() {
		if err := rec.Done(); err != nil {
			t.Errorf("%v\nre-record with: %s=1 go test -run '^%s$' ./cmd/tui", err, recordEnv, t.Name())
		}
	})
	return options{provider: anthropic.ProviderID, model: model, transport: rec}, getenv
}

// rtFunc is an http.RoundTripper made from a function.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// sseResponse answers every request with status and an SSE body.
func sseResponse(status int, body string) rtFunc {
	return func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	}
}

// sseText is an Anthropic Messages stream that says text and ends the turn.
func sseText(text string) string {
	return `event:message_start
data:{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"qwen3.7-max","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}

event:content_block_start
data:{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event:content_block_delta
data:{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}}

event:content_block_stop
data:{"type":"content_block_stop","index":0}

event:message_delta
data:{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}

event:message_stop
data:{"type":"message_stop"}

`
}

func TestTokenPlanUsesInjectedTransport(t *testing.T) {
	var seen *http.Request
	rt := sseResponse(http.StatusOK, sseText("pong"))
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		seen = r
		return rt(r)
	})}
	ag, err := newHeadlessAgent(o, func(k string) string {
		if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "test-key"
		}
		return ""
	})
	require.NoError(t, err)

	var out, errb bytes.Buffer
	code := runHeadless(ag, []string{"ping"}, modePrint, &out, &errb, nil)
	require.Equal(t, 0, code, errb.String())
	assert.Equal(t, "pong\n", out.String())
	require.NotNil(t, seen, "the provider must send through the injected transport")
	assert.Equal(t, "/apps/anthropic/v1/messages", seen.URL.Path)
}

func TestCaptureWritesCassette(t *testing.T) {
	dir := t.TempDir()
	var errb bytes.Buffer
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID}
	getenv := func(k string) string {
		switch k {
		case captureEnv:
			return dir
		case "ASK_ALIBABA_TOKEN_PLAN_API_KEY":
			return "test-key"
		}
		return ""
	}
	path, err := startCapture(&o, getenv, &errb, sseResponse(http.StatusOK, sseText("pong")))
	require.NoError(t, err)
	assert.Contains(t, errb.String(), path)

	ag, err := newHeadlessAgent(o, getenv)
	require.NoError(t, err)
	var out bytes.Buffer
	require.Equal(t, 0, runHeadless(ag, []string{"ping"}, modePrint, &out, &errb, nil), errb.String())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "pong")
	assert.NotContains(t, string(raw), "test-key")

	// The captured file replays the same run.
	rec, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	o2 := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rec}
	ag, err = newHeadlessAgent(o2, getenv)
	require.NoError(t, err)
	out.Reset()
	require.Equal(t, 0, runHeadless(ag, []string{"ping"}, modePrint, &out, &errb, nil), errb.String())
	assert.Equal(t, "pong\n", out.String())
	assert.NoError(t, rec.Done())
}

func TestCaptureOffWithoutEnv(t *testing.T) {
	o := options{}
	path, err := startCapture(&o, func(string) string { return "" }, io.Discard, nil)
	require.NoError(t, err)
	assert.Empty(t, path)
	assert.Nil(t, o.transport)
}

// keyLike matches common API key shapes.
var keyLike = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`)

// TestCassettesHoldNoSecrets fails when a committed cassette has an auth
// header, a key-like string or a Token Plan key from the environment.
func TestCassettesHoldNoSecrets(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "cassettes", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	var keys []string
	for _, k := range []string{"ASK_ALIBABA_TOKEN_PLAN_API_KEY", "ALIBABA_TOKEN_PLAN_API_KEY"} {
		if v := os.Getenv(k); v != "" {
			keys = append(keys, v)
		}
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		text := string(raw)
		lower := strings.ToLower(text)
		for _, h := range []string{"authorization:", "x-api-key:", "api-key:", "cookie:"} {
			assert.NotContains(t, lower, h, "%s has a %s header", f, h)
		}
		assert.False(t, keyLike.MatchString(text), "%s has a key-like string", f)
		for _, k := range keys {
			// Do not print the key in the failure message.
			assert.False(t, strings.Contains(text, k), "%s has the API key from the environment", f)
		}
	}
}

func TestCaptureSkipsProvidersWithoutHTTP(t *testing.T) {
	var errb bytes.Buffer
	o := options{provider: defaultProvider}
	path, err := startCapture(&o, func(k string) string {
		if k == captureEnv {
			return t.TempDir()
		}
		return ""
	}, &errb, nil)
	require.NoError(t, err)
	assert.Empty(t, path)
	assert.Nil(t, o.transport)
	assert.Contains(t, errb.String(), "ignored")
}
