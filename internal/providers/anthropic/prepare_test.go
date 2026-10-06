package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// sentBody streams one request and returns the body that the server got.
func sentBody(t *testing.T, opts providers.StreamOptions) map[string]any {
	t.Helper()
	got := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1<<20)
		n, _ := r.Body.Read(b)
		got <- b[:n]
		writeSSE(w, textSSE("end_turn"))
	}))
	t.Cleanup(srv.Close)
	_, err := resultOf(t, testProvider(srv).Stream(context.Background(), Model(), helloReq(), opts))
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(<-got, &body))
	return body
}

func TestStreamUsesPreparedValuesOnly(t *testing.T) {
	// The options ask for other values. Stream must send the prepared ones.
	temp := 0.9
	body := sentBody(t, providers.StreamOptions{
		MaxTokens:   999,
		Reasoning:   protocol.ThinkingHigh,
		Temperature: &temp,
		Prepared: &providers.Prepared{
			Provider: providers.ProviderTokenPlan, API: APIID, Model: ModelID,
			MaxTokens: 123, Thinking: protocol.ThinkingLow, Effort: "low",
		},
	})
	assert.EqualValues(t, 123, body["max_tokens"])
	assert.Equal(t, map[string]any{"effort": "low"}, body["output_config"])
	assert.NotContains(t, body, "temperature")
}

func TestPrepareValuesAreTheValuesStreamSendsWithoutPrepared(t *testing.T) {
	opts := providers.StreamOptions{MaxTokens: 500, Reasoning: protocol.ThinkingHigh}
	pr, err := New(WithNow(func() int64 { return 1 })).(providers.Preparer).Prepare(Model(), helloReq(), opts)
	require.NoError(t, err)
	assert.Equal(t, 500, pr.MaxTokens)
	assert.Equal(t, protocol.ThinkingHigh, pr.Thinking)
	assert.Equal(t, "high", pr.Effort)

	// A stream with no prepared values computes them itself, to the same result.
	fromOpts := sentBody(t, opts)
	opts.Prepared = pr
	fromPrepared := sentBody(t, opts)
	assert.Equal(t, fromOpts, fromPrepared)
}

func TestPrepareClampsDefaultLimitToTheModel(t *testing.T) {
	pr, err := New().(providers.Preparer).Prepare(Model(), helloReq(), providers.StreamOptions{})
	require.NoError(t, err)
	assert.Positive(t, pr.MaxTokens, "a request with no limit still sends one")
	assert.LessOrEqual(t, pr.MaxTokens, Model().MaxTokens)
	assert.Equal(t, protocol.ThinkingMedium, pr.Thinking, "the default level is applied")
	assert.Contains(t, pr.HeaderNames, "anthropic-version")
	assert.Equal(t, "2023-06-01", pr.Headers["anthropic-version"])
	assert.Equal(t, providers.TokenPlanMessagesURL+"/v1/messages", pr.Endpoint)
}

func TestPreparedHoldsNoCredentialValue(t *testing.T) {
	m := Model()
	m.Headers = map[string]string{"X-Account": "CANARY-account-header"}
	pr, err := New(WithEnv(func(string) (string, bool) { return "CANARY-key-in-env", true })).(providers.Preparer).
		Prepare(m, helloReq(), providers.StreamOptions{APIKey: "CANARY-explicit-key"})
	require.NoError(t, err)
	raw, err := json.Marshal(pr)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "CANARY")
	assert.Contains(t, pr.HeaderNames, "x-api-key", "the name of a credential header is kept")
	assert.NotContains(t, pr.Headers, "x-api-key", "its value is not")
}

func TestPrepareRejectsAForcedToolThatIsNotDeclared(t *testing.T) {
	_, err := New().(providers.Preparer).Prepare(Model(), helloReq(), providers.StreamOptions{ToolChoice: "missing"})
	assert.ErrorContains(t, err, "not declared")
}
