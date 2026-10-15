package cassette_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/cassette"
)

const sseReply = "event:message_start\ndata:{\"type\":\"message_start\"}\n\nevent:message_stop\ndata:{\"type\":\"message_stop\"}\n\n"

// newServer returns a server that answers every POST with sseReply and
// counts the requests it gets.
func newServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Request-Id", "req-123")
		_, _ = io.WriteString(w, sseReply)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func post(t *testing.T, rt http.RoundTripper, url, body string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "secret-key-value")
	req.Header.Set("Authorization", "Bearer secret-token-value")
	return rt.RoundTrip(req)
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

// record writes a cassette with the given request bodies and returns its path.
func record(t *testing.T, url string, bodies ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case.yaml")
	rec, err := cassette.New(path, cassette.Record)
	require.NoError(t, err)
	for _, b := range bodies {
		resp, err := post(t, rec, url, b)
		require.NoError(t, err)
		readBody(t, resp)
	}
	return path
}

func TestRecordThenReplayDoesNotCallTheServer(t *testing.T) {
	srv, hits := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"a":1}`, `{"a":2}`)
	require.EqualValues(t, 2, hits.Load())

	rec, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	for _, b := range []string{`{"a":1}`, `{"a":2}`} {
		resp, err := post(t, rec, srv.URL+"/v1/messages", b)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, sseReply, readBody(t, resp))
	}
	assert.EqualValues(t, 2, hits.Load(), "replay must not reach the server")
	assert.NoError(t, rec.Err())
	assert.NoError(t, rec.Done())
}

func TestRecordDropsSecretsAndVolatileHeaders(t *testing.T) {
	srv, _ := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"a":1}`)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(raw)
	for _, s := range []string{"secret-key-value", "secret-token-value", "X-Api-Key", "Authorization", "Request-Id", "Date"} {
		assert.NotContains(t, text, s)
	}
	assert.Contains(t, text, "text/event-stream")
}

func TestReplayIgnoresJSONKeyOrder(t *testing.T) {
	srv, _ := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"a":1,"b":{"x":1,"y":2}}`)
	rec, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	resp, err := post(t, rec, srv.URL+"/v1/messages", `{"b":{"y":2,"x":1},"a":1}`)
	require.NoError(t, err)
	readBody(t, resp)
	assert.NoError(t, rec.Err())
}

func TestReplayBodyMismatchShowsDiff(t *testing.T) {
	srv, _ := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"tool":"add","desc":"Add two numbers."}`)
	rec, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	_, err = post(t, rec, srv.URL+"/v1/messages", `{"tool":"add","desc":"Add numbers."}`)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "request 1")
	assert.Contains(t, msg, `-  "desc": "Add two numbers."`)
	assert.Contains(t, msg, `+  "desc": "Add numbers."`)
	assert.Contains(t, msg, "ASK_RECORD=1")
	assert.Equal(t, err, rec.Err())
}

func TestReplayExtraRequestFails(t *testing.T) {
	srv, _ := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"a":1}`)
	rec, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	resp, err := post(t, rec, srv.URL+"/v1/messages", `{"a":1}`)
	require.NoError(t, err)
	readBody(t, resp)
	_, err = post(t, rec, srv.URL+"/v1/messages", `{"a":2}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request 2")
	assert.Contains(t, err.Error(), "has 1 request")
}

func TestReplayUnusedInteractionsFailDone(t *testing.T) {
	srv, _ := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"a":1}`, `{"a":2}`)
	rec, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	resp, err := post(t, rec, srv.URL+"/v1/messages", `{"a":1}`)
	require.NoError(t, err)
	readBody(t, resp)
	err = rec.Done()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2")
}

func TestReplayMissingCassetteFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := cassette.New(path, cassette.Replay)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "ASK_RECORD=1")
}

func TestRecordBudgetStopsTheRun(t *testing.T) {
	srv, hits := newServer(t)
	path := filepath.Join(t.TempDir(), "budget.yaml")
	rec, err := cassette.New(path, cassette.Record, cassette.WithBudget(1))
	require.NoError(t, err)
	resp, err := post(t, rec, srv.URL+"/v1/messages", `{"a":1}`)
	require.NoError(t, err)
	readBody(t, resp)
	_, err = post(t, rec, srv.URL+"/v1/messages", `{"a":2}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "budget")
	assert.EqualValues(t, 1, hits.Load())
	assert.Equal(t, err, rec.Err())
}

func TestRecordSavesAfterEachRequest(t *testing.T) {
	srv, _ := newServer(t)
	path := filepath.Join(t.TempDir(), "live.yaml")
	rec, err := cassette.New(path, cassette.Record)
	require.NoError(t, err)
	resp, err := post(t, rec, srv.URL+"/v1/messages", `{"a":1}`)
	require.NoError(t, err)
	readBody(t, resp)
	// No Stop call: a captured session that is killed keeps what it saw.
	_, err = os.Stat(path)
	require.NoError(t, err)

	replay, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	resp, err = post(t, replay, srv.URL+"/v1/messages", `{"a":1}`)
	require.NoError(t, err)
	assert.Equal(t, sseReply, readBody(t, resp))
}

func TestRecordRemovesTheOldCassetteFirst(t *testing.T) {
	srv, _ := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"a":1}`)
	_, err := cassette.New(path, cassette.Record)
	require.NoError(t, err)
	// A recording that fails before its first response must not leave the
	// old cassette for the next replay.
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "the old cassette must be gone")
}

func TestPathMustEndInYAML(t *testing.T) {
	_, err := cassette.New(filepath.Join(t.TempDir(), "case"), cassette.Record)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".yaml")
}

func TestReplayIsStrictlyInOrder(t *testing.T) {
	srv, _ := newServer(t)
	path := record(t, srv.URL+"/v1/messages", `{"a":1}`, `{"a":2}`)
	rec, err := cassette.New(path, cassette.Replay)
	require.NoError(t, err)
	// The second recorded request, sent first, must not match.
	_, err = post(t, rec, srv.URL+"/v1/messages", `{"a":2}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request 1")
	assert.Contains(t, err.Error(), `-  "a": 1`)
	assert.Contains(t, err.Error(), `+  "a": 2`)
}
