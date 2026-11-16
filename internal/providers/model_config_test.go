package providers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

const localURL = "http://127.0.0.1:11434/v1"

func ptr[T any](v T) *T { return &v }

func localConfig(models ...providers.ModelSpec) providers.ModelConfig {
	return providers.ModelConfig{Providers: []providers.ProviderConfig{{
		ID: "local", BaseURL: localURL, API: providers.APIOpenAICompletions, Models: models,
	}}}
}

func problemPaths(set *providers.ModelSet) []string {
	var out []string
	for _, p := range set.Problems() {
		out = append(out, p.Path)
	}
	return out
}

func TestCustomModelGetsPiDefaults(t *testing.T) {
	t.Parallel()
	set := providers.NewModelSet(nil, nil, nil, localConfig(providers.ModelSpec{ID: "qwen3-coder:30b"}))
	require.Empty(t, set.Problems())
	m, err := set.Find(providers.Ref{Provider: "local", ID: "qwen3-coder:30b"})
	require.NoError(t, err)
	assert.Equal(t, providers.Model{
		ID: "qwen3-coder:30b", Name: "qwen3-coder:30b", Provider: "local",
		API: providers.APIOpenAICompletions, BaseURL: localURL,
		Input: []string{"text"}, ContextWindow: 128_000, MaxTokens: 16_384,
		Cost:   providers.Cost{Tiers: []providers.Tier{{}}},
		Compat: providers.CompletionsCompat{SupportsReasoningEffort: true},
	}, m)
	assert.Nil(t, m.Headers)
	assert.Equal(t, providers.OriginUser, set.Origin(refOf(m)))

	maxTokens, contextWindow := providers.RequestLimits(m)
	assert.Equal(t, 16_384, maxTokens)
	assert.Equal(t, 128_000, contextWindow)
	assert.Equal(t, protocol.ThinkingOff, providers.ClampThinkingLevel(m, protocol.ThinkingHigh))
}

func TestCustomModelThinkingMapTurnsReasoningOn(t *testing.T) {
	t.Parallel()
	levels := providers.ThinkingLevelMap{protocol.ThinkingHigh: ptr("high")}
	set := providers.NewModelSet(nil, nil, nil, localConfig(
		providers.ModelSpec{ID: "thinks", ThinkingLevelMap: levels},
		providers.ModelSpec{ID: "quiet", ThinkingLevelMap: levels, Reasoning: ptr(false)},
		providers.ModelSpec{
			ID: "full", Name: "Full", API: providers.APIAnthropicMessages, Input: []string{"text", "image"},
			ContextWindow: 200_000, MaxTokens: 8_000, Cost: providers.Price{Input: 1, Output: 2},
		},
	))
	require.Empty(t, set.Problems())
	thinks, err := set.Find(providers.Ref{Provider: "local", ID: "thinks"})
	require.NoError(t, err)
	assert.True(t, thinks.Reasoning)
	assert.Equal(t, protocol.ThinkingHigh, providers.ClampThinkingLevel(thinks, protocol.ThinkingHigh))
	quiet, err := set.Find(providers.Ref{Provider: "local", ID: "quiet"})
	require.NoError(t, err)
	assert.False(t, quiet.Reasoning)

	full, err := set.Find(providers.Ref{Provider: "local", ID: "full"})
	require.NoError(t, err)
	assert.Equal(t, "Full", full.Name)
	assert.Equal(t, providers.AnthropicCompat{}, full.Compat)
	assert.Equal(t, []string{"text", "image"}, full.Input)
	assert.Equal(t, 200_000, full.ContextWindow)
	assert.Equal(t, 8_000, full.MaxTokens)
	assert.Equal(t, providers.Price{Input: 1, Output: 2}, full.Cost.Tiers[0].Price)
	assert.False(t, full.CostUnknown)
}

func TestCustomModelNeedsAPI(t *testing.T) {
	t.Parallel()
	cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{{
		ID: "local", BaseURL: localURL,
		Models: []providers.ModelSpec{
			{ID: "no-api"},
			{ID: "bad-api", API: "openai-chat"},
			{ID: "ok", API: providers.APIOpenAICompletions},
			{ID: "bad-compat", API: providers.APIOpenAICompletions, Compat: providers.AnthropicCompat{}},
			{ID: "bad-input", API: providers.APIOpenAICompletions, Input: []string{"audio"}},
			{ID: "bad-level", API: providers.APIOpenAICompletions, ThinkingLevelMap: providers.ThinkingLevelMap{"huge": ptr("x")}},
			{ID: "bad-limit", API: providers.APIOpenAICompletions, MaxTokens: -1},
			{ID: "ok", API: providers.APIOpenAICompletions, Name: "second"},
		},
	}}}
	set := providers.NewModelSet(nil, nil, nil, cfg)
	assert.Equal(t, []string{"ok"}, idsOf(set.All()))
	assert.Equal(t, "ok", set.All()[0].Name, "the first row of a repeated id stays")
	assert.Equal(t, []string{
		"providers[0].models[0].api",
		"providers[0].models[1].api",
		"providers[0].models[3].compat",
		"providers[0].models[4].input",
		"providers[0].models[5].thinkingLevelMap",
		"providers[0].models[6]",
		"providers[0].models[7]",
	}, problemPaths(set))
}

func TestCustomModelOnBuiltInProviderUsesItsBaseURL(t *testing.T) {
	t.Parallel()
	cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{
		{ID: providers.ProviderOpenAI, Models: []providers.ModelSpec{{ID: "gpt-new", API: providers.APIOpenAIResponses}}},
		{ID: "nowhere", Models: []providers.ModelSpec{{ID: "x", API: providers.APIOpenAICompletions}}},
	}}
	set := providers.NewModelSet([]providers.Model{openAIRow("gpt-5.5")}, nil, nil, cfg)
	m, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: "gpt-new"})
	require.NoError(t, err)
	assert.Equal(t, providers.OpenAIURL, m.BaseURL)
	assert.Equal(t, []string{"providers[1].models[0]"}, problemPaths(set))
	assert.Equal(t, providers.OriginBuiltin, set.Origin(refOf(openAIRow("gpt-5.5"))))
}

func TestCustomModelReplacesBaseRowWithSameRef(t *testing.T) {
	t.Parallel()
	completions := customRow(providers.ProviderOpenAI, "chat-row", providers.APIOpenAICompletions)
	base := []providers.Model{providers.OpenAIGPT55(), anthropicRow("claude-opus-4-5"), completions}
	cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{{
		ID: providers.ProviderOpenAI,
		Models: []providers.ModelSpec{
			{ID: providers.ModelGPT55, API: providers.APIOpenAIResponses, MaxTokens: 1_000},
			// Another API adds a second row instead.
			{ID: providers.ModelGPT55, API: providers.APIOpenAICompletions},
		},
	}}}
	set := providers.NewModelSet(base, nil, nil, cfg)
	require.Empty(t, set.Problems())
	rows := set.Models(providers.ProviderOpenAI)
	require.Len(t, rows, 3)
	added, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55, API: providers.APIOpenAICompletions})
	require.NoError(t, err)
	assert.Equal(t, completions.BaseURL, added.BaseURL, "a row with no baseUrl takes the URL of a provider row with its API")
	m, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55, API: providers.APIOpenAIResponses})
	require.NoError(t, err)
	assert.Equal(t, 1_000, m.MaxTokens)
	assert.Equal(t, 128_000, m.ContextWindow, "a replaced row takes the custom defaults")
	assert.Equal(t, providers.ResponsesCompat{}, m.Compat)
	assert.Equal(t, providers.OriginUser, set.Origin(refOf(m)))
	assert.Equal(t, providers.OriginBuiltin, set.Origin(refOf(anthropicRow("claude-opus-4-5"))))
}

func TestOverrideOnBuiltInKeepsFamilyAPIAndWords(t *testing.T) {
	t.Parallel()
	cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{{
		ID: providers.ProviderOpenAI,
		Overrides: map[string]providers.ModelPatch{providers.ModelGPT55: {
			API:              providers.APIOpenAICompletions,
			Compat:           providers.ResponsesCompat{Store: true},
			ThinkingLevelMap: providers.ThinkingLevelMap{protocol.ThinkingMax: ptr("max")},
			ContextWindow:    ptr(200_000),
		}},
	}}}
	set := providers.NewModelSet([]providers.Model{providers.OpenAIGPT55()}, nil, nil, cfg)
	m, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55})
	require.NoError(t, err)
	want := providers.OpenAIGPT55()
	assert.Equal(t, want.API, m.API)
	assert.Equal(t, want.Compat, m.Compat)
	assert.Nil(t, m.ThinkingLevelMap)
	assert.Equal(t, 200_000, m.ContextWindow)
	assert.Equal(t, providers.OriginUser, set.Origin(refOf(m)))
	assert.Equal(t, []string{
		"providers[0].modelOverrides.api",
		"providers[0].modelOverrides.compat",
		"providers[0].modelOverrides.thinkingLevelMap",
	}, problemPaths(set))
}

func TestOverrideOnBuiltInCanTurnLevelsOffAndSetMetadata(t *testing.T) {
	t.Parallel()
	cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{{
		ID: providers.ProviderOpenAI,
		Overrides: map[string]providers.ModelPatch{providers.ModelGPT55: {
			Name:             ptr("Five"),
			MaxTokens:        ptr(4_096),
			Cost:             &providers.PricePatch{Input: ptr(int64(7))},
			Input:            []string{"text"},
			ThinkingLevelMap: providers.ThinkingLevelMap{protocol.ThinkingHigh: nil, protocol.ThinkingLow: ptr("low")},
		}},
	}}}
	set := providers.NewModelSet([]providers.Model{providers.OpenAIGPT55()}, nil, nil, cfg)
	require.Empty(t, set.Problems())
	m, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55})
	require.NoError(t, err)
	assert.Equal(t, "Five", m.Name)
	assert.Equal(t, 4_096, m.MaxTokens)
	want := providers.OpenAIGPT55().Cost.Tiers[0].Price
	want.Input = 7
	assert.Equal(t, providers.Cost{Tiers: []providers.Tier{{Price: want}}}, m.Cost, "the omitted prices come from the base row")
	assert.Equal(t, []string{"text"}, m.Input)
	assert.Equal(t, protocol.ThinkingMedium, providers.ClampThinkingLevel(m, protocol.ThinkingHigh))
}

func TestOverrideCostMergesFieldsAndKeepsTiersWithoutPrice(t *testing.T) {
	t.Parallel()
	base := providers.OpenAIGPT55()
	base.Cost = providers.Cost{Tiers: []providers.Tier{
		{Price: providers.Price{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}},
		{Above: 1000, Price: providers.Price{Input: 10, Output: 20}},
	}}
	build := func(patch providers.ModelPatch) providers.Model {
		cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{{
			ID: providers.ProviderOpenAI, Overrides: map[string]providers.ModelPatch{base.ID: patch},
		}}}
		set := providers.NewModelSet([]providers.Model{base}, nil, nil, cfg)
		require.Empty(t, set.Problems())
		m, err := set.Find(providers.Ref{Provider: base.Provider, ID: base.ID})
		require.NoError(t, err)
		return m
	}
	kept := build(providers.ModelPatch{Name: ptr("n"), Cost: &providers.PricePatch{}})
	assert.Equal(t, base.Cost, kept.Cost, "a patch that sets no price keeps the tiers")
	merged := build(providers.ModelPatch{Cost: &providers.PricePatch{Output: ptr(int64(9))}})
	assert.Equal(t, providers.Cost{Tiers: []providers.Tier{{Price: providers.Price{Input: 1, Output: 9, CacheRead: 3, CacheWrite: 4}}}}, merged.Cost)

	unknown := base
	unknown.Cost, unknown.CostUnknown = providers.Cost{}, true
	base = unknown
	partial := build(providers.ModelPatch{Cost: &providers.PricePatch{Input: ptr(int64(5))}})
	assert.True(t, partial.CostUnknown, "the omitted prices stay unknown")
	zero := int64(0)
	full := build(providers.ModelPatch{Cost: &providers.PricePatch{Input: &zero, Output: &zero, CacheRead: &zero, CacheWrite: &zero}})
	assert.False(t, full.CostUnknown)
}

func TestOverrideRefusesBadValuesAndKeepsTheRow(t *testing.T) {
	t.Parallel()
	cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{{
		ID: providers.ProviderOpenAI,
		Overrides: map[string]providers.ModelPatch{
			providers.ModelGPT55: {ContextWindow: ptr(0), MaxTokens: ptr(-1), Name: ptr(""), Input: []string{"audio"}},
			"not-a-row":          {ContextWindow: ptr(1)},
		},
	}}}
	set := providers.NewModelSet([]providers.Model{providers.OpenAIGPT55()}, nil, nil, cfg)
	m, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55})
	require.NoError(t, err)
	assert.Equal(t, providers.OpenAIGPT55(), m)
	assert.Equal(t, providers.OriginBuiltin, set.Origin(refOf(m)), "a refused override does not change the row")
	assert.Len(t, set.Problems(), 4)
}

func TestOverrideOnCustomRowMayChangeCompatAndWords(t *testing.T) {
	t.Parallel()
	cfg := localConfig(providers.ModelSpec{ID: "m"})
	cfg.Providers[0].Overrides = map[string]providers.ModelPatch{"m": {
		Compat:           providers.CompletionsCompat{SupportsStore: true},
		ThinkingLevelMap: providers.ThinkingLevelMap{protocol.ThinkingMax: ptr("max")},
	}}
	set := providers.NewModelSet(nil, nil, nil, cfg)
	require.Empty(t, set.Problems())
	m, err := set.Find(providers.Ref{Provider: "local", ID: "m"})
	require.NoError(t, err)
	assert.Equal(t, providers.CompletionsCompat{SupportsStore: true}, m.Compat)
	assert.Equal(t, "max", *m.ThinkingLevelMap[protocol.ThinkingMax])
}

func TestModelSetDoesNotShareConfigStorage(t *testing.T) {
	t.Parallel()
	input := []string{"text", "image"}
	levels := providers.ThinkingLevelMap{protocol.ThinkingHigh: ptr("high")}
	set := providers.NewModelSet(nil, nil, nil, localConfig(providers.ModelSpec{ID: "m", Input: input, ThinkingLevelMap: levels}))
	input[0] = "changed"
	*levels[protocol.ThinkingHigh] = "changed"
	m, err := set.Find(providers.Ref{Provider: "local", ID: "m"})
	require.NoError(t, err)
	assert.Equal(t, []string{"text", "image"}, m.Input)
	assert.Equal(t, "high", *m.ThinkingLevelMap[protocol.ThinkingHigh])
}

func TestOverrideWithEmptyInputChangesNothing(t *testing.T) {
	t.Parallel()
	cfg := providers.ModelConfig{Providers: []providers.ProviderConfig{{
		ID:        providers.ProviderOpenAI,
		Overrides: map[string]providers.ModelPatch{providers.ModelGPT55: {Input: []string{}}},
	}}}
	set := providers.NewModelSet([]providers.Model{providers.OpenAIGPT55()}, nil, nil, cfg)
	require.Empty(t, set.Problems())
	m, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55})
	require.NoError(t, err)
	assert.Equal(t, providers.OpenAIGPT55().Input, m.Input)
}

func TestNewCompletionsRowSendsReasoningEffortByDefault(t *testing.T) {
	t.Parallel()
	set := providers.NewModelSet(nil, nil, nil, localConfig(providers.ModelSpec{ID: "m"}))
	require.Empty(t, set.Problems())
	m, err := set.Find(providers.Ref{Provider: "local", ID: "m"})
	require.NoError(t, err)
	compat, ok := m.Completions()
	require.True(t, ok)
	assert.True(t, compat.SupportsReasoningEffort)
}
