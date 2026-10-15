package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
)

func TestKeysAndEnvDoNotCrossHosts(t *testing.T) {
	t.Setenv("OPENAI_ORG_ID", "org-evil")
	t.Setenv("OPENAI_PROJECT_ID", "proj-evil")
	t.Setenv("OPENAI_BASE_URL", "https://evil.example/v1")
	t.Setenv("OPENAI_CUSTOM_HEADERS", "X-Evil: 1")

	tp := newCaptureServer(t, completionsSSE())
	oa := newCaptureServer(t, responsesSSE())

	cp := NewCompletions(
		WithHTTPClient(tp.srv.Client()),
		WithEnv(func(k string) (string, bool) {
			if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
				return "tp-secret-key", true
			}
			return "", false
		}),
		WithNow(func() int64 { return 1 }),
	)
	rp := NewResponses(
		WithHTTPClient(oa.srv.Client()),
		WithEnv(func(k string) (string, bool) {
			if k == "OPENAI_API_KEY" {
				return "sk-openai-live", true
			}
			return "", false
		}),
		WithNow(func() int64 { return 1 }),
	)

	cs := cp.Stream(context.Background(), completionsModel(tp.srv.URL), helloReq(), providers.StreamOptions{Reasoning: "off"})
	_, err := resultOf(t, cs)
	require.NoError(t, err)
	first := tp.take(t)
	assert.Contains(t, first.headers.Get("Authorization"), "tp-secret-key")
	assert.NotContains(t, first.url, "evil.example")
	assert.Equal(t, "", first.headers.Get("OpenAI-Organization"))
	assert.Equal(t, "", first.headers.Get("Openai-Organization"))

	rs := rp.Stream(context.Background(), responsesModel(oa.srv.URL), helloReq(), providers.StreamOptions{})
	_, err = resultOf(t, rs)
	require.NoError(t, err)
	second := oa.take(t)
	auth := second.headers.Get("Authorization")
	assert.Contains(t, auth, "sk-openai-live")
	assert.NotContains(t, auth, "tp-secret-key")
	assert.NotContains(t, second.url, "evil.example")
	assert.True(t, strings.HasPrefix(second.url, "/") || strings.Contains(second.req.Host, "127.0.0.1") || strings.Contains(second.req.Host, "localhost"))
	assert.Equal(t, "", second.headers.Get("OpenAI-Organization"))
	assert.Equal(t, "", second.headers.Get("Openai-Organization"))
	assert.Equal(t, "", second.headers.Get("OpenAI-Project"))
	assert.Equal(t, "", second.headers.Get("X-Evil"))
}

func TestIsolateDropsOrgAndUnknownHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	rt := Isolate(srv.Client().Transport, map[string]string{"X-Ask": "1"})
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer k")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Organization", "org-evil")
	req.Header.Set("OpenAI-Project", "proj-evil")
	req.Header.Set("X-Stainless-Lang", "go")
	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "Bearer k", got.Get("Authorization"))
	assert.Equal(t, "1", got.Get("X-Ask"))
	assert.Equal(t, "", got.Get("OpenAI-Organization"))
	assert.Equal(t, "", got.Get("OpenAI-Project"))
	assert.Equal(t, "", got.Get("X-Stainless-Lang"))
}
