package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestCompletionsStreamUsesPreparedValuesOnly(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	compat, _ := m.Completions()
	field := compat.MaxTokensField
	if field == "" {
		field = "max_tokens"
	}
	_, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{
		MaxTokens: 999,
		Reasoning: protocol.ThinkingHigh,
		Prepared:  &providers.Prepared{Provider: m.Provider, API: string(m.API), Model: m.ID, MaxTokens: 123, Thinking: protocol.ThinkingLow, Effort: "low"},
	}))
	require.NoError(t, err)
	body := srv.take(t).body(t)
	assert.EqualValues(t, 123, body[field])
	assert.Equal(t, "low", body["reasoning_effort"])
}

func TestResponsesStreamUsesPreparedValuesOnly(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	m := responsesModel(srv.srv.URL)
	_, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{
		MaxTokens: 999,
		Reasoning: protocol.ThinkingHigh,
		SessionID: "ignored-session",
		Prepared:  &providers.Prepared{Provider: m.Provider, API: string(m.API), Model: m.ID, MaxTokens: 123, Thinking: protocol.ThinkingLow, Effort: "low", PromptCacheKey: "prepared-key"},
	}))
	require.NoError(t, err)
	body := srv.take(t).body(t)
	assert.EqualValues(t, 123, body["max_output_tokens"])
	assert.Equal(t, "prepared-key", body["prompt_cache_key"])
	assert.Equal(t, map[string]any{"effort": "low"}, subset(body["reasoning"], "effort"))
}

func subset(v any, keys ...string) map[string]any {
	m, _ := v.(map[string]any)
	out := map[string]any{}
	for _, k := range keys {
		if val, ok := m[k]; ok {
			out[k] = val
		}
	}
	return out
}

func TestPrepareClampsCompletionsLimitAndKeepsExplicitOne(t *testing.T) {
	m := providers.TokenPlanCompletions()
	pr, err := NewCompletions().(providers.Preparer).Prepare(m, helloReq(), providers.StreamOptions{})
	require.NoError(t, err)
	assert.Positive(t, pr.MaxTokens)
	assert.LessOrEqual(t, pr.MaxTokens, m.MaxTokens)

	pr, err = NewCompletions().(providers.Preparer).Prepare(m, helloReq(), providers.StreamOptions{MaxTokens: 77})
	require.NoError(t, err)
	assert.Equal(t, 77, pr.MaxTokens)
	assert.Contains(t, pr.HeaderNames, "authorization")
}

func TestPrepareResponsesAppliesProviderMinimumAndCacheKey(t *testing.T) {
	m := providers.OpenAIGPT55()
	pr, err := NewResponses().(providers.Preparer).Prepare(m, helloReq(), providers.StreamOptions{MaxTokens: 5, SessionID: "sess-1"})
	require.NoError(t, err)
	assert.Equal(t, 16, pr.MaxTokens)
	assert.Equal(t, "sess-1", pr.PromptCacheKey)

	pr, err = NewResponses().(providers.Preparer).Prepare(m, helloReq(), providers.StreamOptions{SessionID: "sess-1", CacheRetention: providers.CacheRetentionNone})
	require.NoError(t, err)
	assert.Empty(t, pr.PromptCacheKey, "no cache key without cache retention")
	assert.Zero(t, pr.MaxTokens, "no limit is sent when none was asked")
	raw, err := json.Marshal(pr)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "sk-")
}
