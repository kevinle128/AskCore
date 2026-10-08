package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ref     Ref
		wantID  string
		wantAPI API
		errSub  []string
	}{
		{
			name:   "token plan empty API names both wires",
			ref:    Ref{Provider: ProviderTokenPlan, ID: ModelDeepSeekFlash},
			errSub: []string{string(APIAnthropicMessages), string(APIOpenAICompletions)},
		},
		{
			name:    "token plan messages by API",
			ref:     Ref{Provider: ProviderTokenPlan, ID: ModelDeepSeekFlash, API: APIAnthropicMessages},
			wantID:  ModelDeepSeekFlash,
			wantAPI: APIAnthropicMessages,
		},
		{
			name:    "token plan completions by API",
			ref:     Ref{Provider: ProviderTokenPlan, ID: ModelDeepSeekFlash, API: APIOpenAICompletions},
			wantID:  ModelDeepSeekFlash,
			wantAPI: APIOpenAICompletions,
		},
		{
			name:    "gpt-5.5 empty API is unique",
			ref:     Ref{Provider: ProviderOpenAI, ID: ModelGPT55},
			wantID:  ModelGPT55,
			wantAPI: APIOpenAIResponses,
		},
		{
			name:   "unknown model",
			ref:    Ref{Provider: "nope", ID: "ghost"},
			errSub: []string{"unknown"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Find(tc.ref)
			if len(tc.errSub) > 0 {
				require.Error(t, err)
				for _, s := range tc.errSub {
					assert.Contains(t, err.Error(), s)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.wantAPI, got.API)
		})
	}
}

func TestLookupKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		provider string
		env      map[string]string
		want     string
	}{
		{
			name:     "openai key only",
			provider: ProviderOpenAI,
			env:      map[string]string{"OPENAI_API_KEY": "sk-live", "OPENAI_BASE_URL": "https://evil.example"},
			want:     "sk-live",
		},
		{
			name:     "openai ignores base URL",
			provider: ProviderOpenAI,
			env:      map[string]string{"OPENAI_BASE_URL": "https://evil.example"},
			want:     "",
		},
		{
			name:     "token plan prefers ASK_ prefix",
			provider: ProviderTokenPlan,
			env: map[string]string{
				"ASK_ALIBABA_TOKEN_PLAN_API_KEY": "ask-key",
				"ALIBABA_TOKEN_PLAN_API_KEY":     "alt-key",
			},
			want: "ask-key",
		},
		{
			name:     "token plan falls back",
			provider: ProviderTokenPlan,
			env:      map[string]string{"ALIBABA_TOKEN_PLAN_API_KEY": "alt-key"},
			want:     "alt-key",
		},
		{
			name:     "unknown provider",
			provider: "other",
			env:      map[string]string{"OPENAI_API_KEY": "sk"},
			want:     "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := LookupKey(tc.provider, func(k string) (string, bool) {
				v, ok := tc.env[k]
				return v, ok
			})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTokenPlanCompletionsRequiresFinishReason(t *testing.T) {
	t.Parallel()
	c, ok := TokenPlanCompletions().Completions()
	require.True(t, ok)
	assert.True(t, c.SupportsFinishReason, "zero must not silently skip the finish_reason check")
}

func TestValidateCompatMismatch(t *testing.T) {
	t.Parallel()
	m := TokenPlanMessages()
	m.Compat = CompletionsCompat{ThinkingFormat: "qwen"}
	err := Validate(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compat")
}

func TestNativeCatalogAndKeys(t *testing.T) {
	for _, row := range []struct {
		provider, id, endpoint, env string
		api                         API
	}{
		{"anthropic", "claude-sonnet-4-6", "https://api.anthropic.com", "ANTHROPIC_API_KEY", APIAnthropicMessages},
		{"xai", "grok-4.7", "https://api.x.ai/v1", "XAI_API_KEY", APIOpenAIResponses},
		{"openai", "gpt-5.6-sol", "https://api.openai.com/v1", "OPENAI_API_KEY", APIOpenAIResponses},
	} {
		m, err := Find(Ref{Provider: row.provider, ID: row.id})
		require.NoError(t, err)
		require.Equal(t, row.endpoint, m.BaseURL)
		require.Equal(t, row.api, m.API)
		require.NoError(t, Validate(m))
		require.NotZero(t, m.ContextWindow)
		require.NotZero(t, m.MaxTokens)
		require.Equal(t, "native-key", LookupKey(row.provider, func(name string) (string, bool) { return "native-key", name == row.env }))
	}
}

func TestAvailableModelsReturnsIndependentRows(t *testing.T) {
	rows := AvailableModels()
	if len(rows) != 6 {
		t.Fatalf("rows: %d", len(rows))
	}
	for _, m := range rows {
		if m.Provider == "faux" {
			t.Fatalf("faux in catalog: %s", m.ID)
		}
	}
	rows[0].ID = "changed"
	if AvailableModels()[0].ID == "changed" {
		t.Fatal("rows share storage")
	}
}
