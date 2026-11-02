package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modelsDevServer serves one handler for the models.dev URL. The client
// rewrites each request to the test server, so FetchModelsDev keeps its fixed
// URL; the recorded request is what the code sent to models.dev.
type modelsDevServer struct {
	client *http.Client

	mu   sync.Mutex
	reqs []*http.Request
}

func newModelsDevServer(t *testing.T, h http.HandlerFunc) *modelsDevServer {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s := &modelsDevServer{}
	inner := srv.Client().Transport
	s.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		s.mu.Lock()
		s.reqs = append(s.reqs, req.Clone(req.Context()))
		s.mu.Unlock()
		out := req.Clone(req.Context())
		out.URL.Scheme = "http"
		out.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		out.Host = ""
		return inner.RoundTrip(out)
	})}
	return s
}

func (s *modelsDevServer) only(t *testing.T) *http.Request {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.reqs, 1)
	return s.reqs[0]
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fetchCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

const fetchFixture = `{
  "openai": {"id": "openai", "npm": "@ai-sdk/openai", "models": {
    "gpt-x": {"id": "gpt-x", "name": "GPT X", "tool_call": true, "provider": {"npm": "x"}, "headers": {"A": "b"}}
  }},
  "google": {"models": {"gemini": {"id": "gemini"}}},
  "fetchedAt": "2026-10-28T00:00:00Z",
  "count": 3,
  "list": [1, {"a": null}]
}`

func TestFetchModelsDevKeepsNamedProvidersAndMapperFields(t *testing.T) {
	t.Parallel()
	s := newModelsDevServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(fetchFixture))
	})
	payload, etag, notModified, err := FetchModelsDev(fetchCtx(t), s.client, "", []string{ProviderOpenAI, ProviderAnthropic})
	require.NoError(t, err)
	assert.False(t, notModified)
	assert.Equal(t, `"v1"`, etag)
	assert.JSONEq(t, `{"openai": {"models": {"gpt-x": {"id": "gpt-x", "name": "GPT X", "tool_call": true}}}}`, string(payload))

	req := s.only(t)
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, modelsDevURL, req.URL.String())
	assert.Empty(t, req.Header.Values("If-None-Match"), "no etag, no If-None-Match")
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
	assert.Equal(t, modelsDevUserAgent, req.Header.Get("User-Agent"))
	for name := range req.Header {
		assert.Contains(t, []string{"Accept", "User-Agent"}, name, "the request sends no other header")
	}
}

func TestFetchModelsDevNotModified(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, respETag, want string
	}{
		{name: "response etag", respETag: `"v2"`, want: `"v2"`},
		{name: "sent etag when the response has none", want: `"v1"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newModelsDevServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-None-Match") != `"v1"` {
					http.Error(w, "want If-None-Match", http.StatusBadRequest)
					return
				}
				if tc.respETag != "" {
					w.Header().Set("ETag", tc.respETag)
				}
				w.WriteHeader(http.StatusNotModified)
			})
			payload, etag, notModified, err := FetchModelsDev(fetchCtx(t), s.client, `"v1"`, modelsDevProviders)
			require.NoError(t, err)
			assert.True(t, notModified)
			assert.Nil(t, payload)
			assert.Equal(t, tc.want, etag)
			assert.Equal(t, `"v1"`, s.only(t).Header.Get("If-None-Match"))
		})
	}
}

func TestFetchModelsDevStatusErrorHasOnlyTheCode(t *testing.T) {
	t.Parallel()
	s := newModelsDevServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "secret server detail", http.StatusInternalServerError)
	})
	payload, etag, notModified, err := FetchModelsDev(fetchCtx(t), s.client, "", modelsDevProviders)
	require.Error(t, err)
	assert.Equal(t, "models.dev fetch: status 500", err.Error())
	assert.Nil(t, payload)
	assert.Empty(t, etag)
	assert.False(t, notModified)
}

func TestFetchModelsDevRejectsOversizeBody(t *testing.T) {
	t.Parallel()
	s := newModelsDevServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// A valid JSON document one byte over the cap, so only the cap can
		// fail it.
		head, tail := []byte(`{"x":"`), []byte(`"}`)
		filler := maxModelsDevBody + 1 - len(head) - len(tail)
		if _, err := w.Write(head); err != nil {
			return
		}
		chunk := bytes.Repeat([]byte("a"), 64<<10)
		for filler > 0 {
			n := min(filler, len(chunk))
			// Stop when the client has gone, so the server can close.
			if _, err := w.Write(chunk[:n]); err != nil {
				return
			}
			filler -= n
		}
		_, _ = w.Write(tail)
	})
	payload, _, _, err := FetchModelsDev(fetchCtx(t), s.client, "", modelsDevProviders)
	require.ErrorIs(t, err, errModelsDevTooLarge)
	assert.Nil(t, payload)
}

func TestFilterModelsDevAcceptsBodyAtTheCap(t *testing.T) {
	t.Parallel()
	body := []byte(`{"openai":{"models":{}}}`)
	top, err := filterModelsDev(&cappedReader{r: bytes.NewReader(body), left: int64(len(body))}, []string{ProviderOpenAI})
	require.NoError(t, err)
	assert.JSONEq(t, `{"models":{}}`, string(top[ProviderOpenAI]))

	_, err = filterModelsDev(&cappedReader{r: bytes.NewReader(body), left: int64(len(body) - 1)}, []string{ProviderOpenAI})
	require.ErrorIs(t, err, errModelsDevTooLarge)
}

func TestFetchModelsDevRejectsBadJSON(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"truncated":         `{"openai": {"models": {`,
		"not an object":     `[1, 2]`,
		"empty":             ``,
		"bad provider":      `{"openai": {"models": []}}`,
		"data after object": `{"openai": {"models": {}}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newModelsDevServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("ETag", `"v1"`)
				_, _ = w.Write([]byte(body))
			})
			payload, etag, _, err := FetchModelsDev(fetchCtx(t), s.client, "", modelsDevProviders)
			require.Error(t, err)
			assert.Nil(t, payload)
			assert.Empty(t, etag, "no ETag for a payload that was not kept")
		})
	}
}

func TestFetchModelsDevRejectsBadArguments(t *testing.T) {
	t.Parallel()
	_, _, _, err := FetchModelsDev(fetchCtx(t), nil, "", modelsDevProviders)
	require.Error(t, err)
	_, _, _, err = FetchModelsDev(fetchCtx(t), http.DefaultClient, "", nil)
	require.Error(t, err)
}

// TestFetchModelsDevRoundTripMatchesSnapshot serves the embedded snapshot
// with an extra provider and extra model fields. The fetched payload must map
// to the same rows as the snapshot, and the extra data must not reach it.
func TestFetchModelsDevRoundTripMatchesSnapshot(t *testing.T) {
	t.Parallel()
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(catalogSnapshot, &top))
	var openai struct {
		Models map[string]map[string]json.RawMessage `json:"models"`
	}
	require.NoError(t, json.Unmarshal(top[ProviderOpenAI], &openai))
	for _, fields := range openai.Models {
		fields["provider"] = json.RawMessage(`{"npm":"extra-field-marker"}`)
	}
	var err error
	top[ProviderOpenAI], err = json.Marshal(openai)
	require.NoError(t, err)
	top["google"] = json.RawMessage(`{"models":{"extra-provider-marker":{"id":"extra-provider-marker"}}}`)
	served, err := json.Marshal(top)
	require.NoError(t, err)

	s := newModelsDevServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(served)
	})
	payload, _, _, err := FetchModelsDev(fetchCtx(t), s.client, "", modelsDevProviders)
	require.NoError(t, err)
	assert.NotContains(t, string(payload), "extra-field-marker")
	assert.NotContains(t, string(payload), "extra-provider-marker")
	assert.NotContains(t, string(payload), "fetchedAt", "the run-time payload has no snapshot header")

	want, err := MapModelsDev(catalogSnapshot)
	require.NoError(t, err)
	got, err := MapModelsDev(payload)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
