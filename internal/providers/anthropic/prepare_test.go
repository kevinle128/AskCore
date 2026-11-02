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

// generatedRow returns the generated catalog row of an Anthropic model.
func generatedRow(t *testing.T, id string) providers.Model {
	t.Helper()
	for _, m := range providers.GeneratedCatalog() {
		if m.Provider == providers.ProviderAnthropic && m.ID == id && m.API == providers.APIAnthropicMessages {
			return m
		}
	}
	t.Fatalf("no generated row %s", id)
	return providers.Model{}
}

// prepareAndEncode runs the real Prepare and Encode and returns the request
// document that Stream would send.
func prepareAndEncode(t *testing.T, m providers.Model, opts providers.StreamOptions) (*providers.Prepared, map[string]any) {
	t.Helper()
	p := New().(providers.Preparer)
	pr, err := p.Prepare(m, helloReq(), opts)
	require.NoError(t, err)
	enc, err := p.Encode(context.Background(), m, pr, helloReq(), providers.AuthBinding{})
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(enc.Body, &body))
	return pr, body
}

func TestPrepareReasoningFalseRowSendsThinkingDisabled(t *testing.T) {
	// The 4.5 models refuse adaptive thinking and an effort word. Without the
	// clamp, an empty level became medium and sent output_config.effort.
	m := generatedRow(t, "claude-haiku-4-5")
	require.False(t, m.Reasoning)
	for _, level := range []protocol.ThinkingLevel{"", protocol.ThinkingHigh, protocol.ThinkingMax} {
		pr, body := prepareAndEncode(t, m, providers.StreamOptions{Reasoning: level})
		assert.Equal(t, protocol.ThinkingOff, pr.Thinking, "level %q", level)
		assert.Empty(t, pr.Effort, "level %q", level)
		assert.Equal(t, map[string]any{"type": "disabled"}, body["thinking"], "level %q", level)
		assert.NotContains(t, body, "output_config", "level %q", level)
	}
}

func TestPrepareRowThatRefusesOffSendsItsLowestEffort(t *testing.T) {
	// These models refuse thinking disabled, so off becomes the lowest effort.
	m := generatedRow(t, "claude-fable-5")
	for _, level := range []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingMinimal} {
		pr, body := prepareAndEncode(t, m, providers.StreamOptions{Reasoning: level})
		assert.Equal(t, protocol.ThinkingLow, pr.Thinking, "level %q", level)
		assert.Equal(t, "low", pr.Effort, "level %q", level)
		assert.Equal(t, map[string]any{"type": "adaptive"}, body["thinking"], "level %q", level)
		assert.Equal(t, map[string]any{"effort": "low"}, body["output_config"], "level %q", level)
	}
}

func TestPrepareForcedToolChoiceWithThinking(t *testing.T) {
	// A model that refuses off cannot take a forced tool: Anthropic refuses a
	// forced tool with thinking on.
	_, err := New().(providers.Preparer).Prepare(generatedRow(t, "claude-opus-5-5"), helloReq(), providers.StreamOptions{ToolChoice: "echo"})
	require.ErrorIs(t, err, providers.ErrUnsupportedRequest)

	// A model with Reasoning false runs with thinking off whatever level is asked.
	pr, body := prepareAndEncode(t, generatedRow(t, "claude-opus-4-5"), providers.StreamOptions{ToolChoice: "echo", Reasoning: protocol.ThinkingHigh})
	assert.Equal(t, protocol.ThinkingOff, pr.Thinking)
	assert.Equal(t, map[string]any{"type": "disabled"}, body["thinking"])
}

func TestPrepareUnknownLimitsUseTheFamilyDefault(t *testing.T) {
	// A row with no limit data used to send max_tokens 1.
	m := generatedRow(t, "claude-sonnet-4-6")
	m.MaxTokens, m.ContextWindow = 0, 0
	pr, body := prepareAndEncode(t, m, providers.StreamOptions{})
	assert.Equal(t, 128_000, pr.MaxTokens)
	assert.EqualValues(t, 128_000, body["max_tokens"])
}
