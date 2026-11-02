package providers_test

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func refOf(m providers.Model) providers.Ref {
	return providers.Ref{Provider: m.Provider, ID: m.ID, API: m.API}
}

func idsOf(rows []providers.Model) []string {
	out := make([]string, 0, len(rows))
	for _, m := range rows {
		out = append(out, m.ID)
	}
	return out
}

// openAIRow is a shown-family OpenAI row in the shape of a generated row.
func openAIRow(id string) providers.Model {
	return providers.Model{
		ID: id, Name: id, Provider: providers.ProviderOpenAI, API: providers.APIOpenAIResponses,
		BaseURL: providers.OpenAIURL, Compat: providers.ResponsesCompat{SupportsMaxOutputTokens: true},
		Input: []string{"text"}, ContextWindow: 400_000, MaxTokens: 128_000,
	}
}

func anthropicRow(id string) providers.Model {
	return providers.Model{
		ID: id, Name: id, Provider: providers.ProviderAnthropic, API: providers.APIAnthropicMessages,
		BaseURL: providers.AnthropicURL, Compat: providers.AnthropicCompat{},
		Input: []string{"text"}, ContextWindow: 200_000, MaxTokens: 64_000,
	}
}

func known(ids ...string) providers.AccountView { return providers.AccountView{Known: true, IDs: ids} }

func TestModelSetStartsWithTheBaseRows(t *testing.T) {
	t.Parallel()
	base := providers.GeneratedCatalog()
	set := providers.NewModelSet(base, nil, nil)
	got := set.All()
	require.Len(t, got, len(base))
	want := map[providers.Ref]providers.Model{}
	for _, m := range base {
		want[refOf(m)] = m
	}
	for _, m := range got {
		assert.Equal(t, want[refOf(m)], m)
	}
}

func TestModelSetOverlayAddsOnlyMissingRows(t *testing.T) {
	t.Parallel()
	base := []providers.Model{openAIRow("gpt-5.4")}
	replace := openAIRow("gpt-5.4")
	replace.Name = "overlay name"
	replace.ContextWindow = 1
	added := openAIRow("gpt-5.4-mini")
	set := providers.NewModelSet(base, []providers.Model{replace, added, added}, nil)

	got := set.All()
	require.Len(t, got, 2, "one row for each provider, id and API")
	m, err := set.Find(refOf(base[0]))
	require.NoError(t, err)
	assert.Equal(t, base[0], m, "an overlay row never replaces a base row")
	assert.True(t, set.Contains(refOf(added)), "an overlay row fills a gap in the base rows")
}

func TestModelSetDropsDuplicateBaseRows(t *testing.T) {
	t.Parallel()
	first := openAIRow("gpt-5.4")
	second := openAIRow("gpt-5.4")
	second.Name = "second"
	set := providers.NewModelSet([]providers.Model{first, second}, nil, nil)
	require.Len(t, set.All(), 1)
	m, err := set.Find(refOf(first))
	require.NoError(t, err)
	assert.Equal(t, first, m, "the first row of a key wins")
}

func TestModelSetUnknownAccountKeepsEveryRow(t *testing.T) {
	t.Parallel()
	base := providers.GeneratedCatalog()
	all := len(providers.NewModelSet(base, nil, nil).Models(providers.ProviderOpenAI))
	require.Positive(t, all)
	for name, accounts := range map[string]map[string]providers.AccountView{
		"no entry":     nil,
		"zero value":   {providers.ProviderOpenAI: {}},
		"known false":  {providers.ProviderOpenAI: {Known: false, IDs: []string{"gpt-5.4"}}},
		"other vendor": {providers.ProviderAnthropic: known("claude-opus-5")},
	} {
		set := providers.NewModelSet(base, nil, accounts)
		assert.Len(t, set.Models(providers.ProviderOpenAI), all, name)
	}
}

func TestModelSetKnownAccountKeepsListedRows(t *testing.T) {
	t.Parallel()
	base := []providers.Model{openAIRow("gpt-5.4"), openAIRow("gpt-5.2"), anthropicRow("claude-opus-5")}
	set := providers.NewModelSet(base, nil, map[string]providers.AccountView{
		providers.ProviderOpenAI: known("gpt-5.4", "gpt-5.4", "dall-e-3"),
	})
	assert.Equal(t, []string{"gpt-5.4"}, idsOf(set.Models(providers.ProviderOpenAI)),
		"a listed duplicate gives one row; an id with no rule gives none")
	assert.Equal(t, []string{"claude-opus-5"}, idsOf(set.Models(providers.ProviderAnthropic)),
		"a provider with no account entry keeps its rows")

	empty := providers.NewModelSet(base, nil, map[string]providers.AccountView{providers.ProviderOpenAI: known()})
	assert.Empty(t, empty.Models(providers.ProviderOpenAI), "a known empty list keeps no row")
}

func TestModelSetKnownAccountKeepsAHandWrittenRowWithAHiddenRule(t *testing.T) {
	t.Parallel()
	set := providers.NewModelSet(providers.GeneratedCatalog(), nil, map[string]providers.AccountView{
		providers.ProviderXAI: known(providers.ModelGrok47, "grok-4.3"),
	})
	assert.Equal(t, []string{providers.ModelGrok47}, idsOf(set.Models(providers.ProviderXAI)),
		"a listed id with a row stays; a listed id of a hidden family gets no row")
}

func TestModelSetAliasMatching(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		rows   []string
		listed []string
		want   []string
	}{
		{"listed dated id keeps the alias row", []string{"gpt-5.4-nano"}, []string{"gpt-5.4-nano-2026-03-17"}, []string{"gpt-5.4-nano"}},
		{"listed alias keeps the dated row", []string{"gpt-4o-2024-08-06"}, []string{"gpt-4o"}, []string{"gpt-4o-2024-08-06"}},
		{"alias and dated both listed give one row", []string{"gpt-5.4-nano"}, []string{"gpt-5.4-nano", "gpt-5.4-nano-2026-03-17"}, []string{"gpt-5.4-nano"}},
		{"two dated snapshots do not match", []string{"gpt-4o-2024-08-06"}, []string{"gpt-4o-2024-11-20"}, []string{"gpt-4o-2024-11-20"}},
		{"a longer id is not an alias", []string{"gpt-5.4-mini"}, []string{"gpt-5.4"}, []string{"gpt-5.4"}},
	}
	for _, tc := range cases {
		var base []providers.Model
		for _, id := range tc.rows {
			base = append(base, openAIRow(id))
		}
		set := providers.NewModelSet(base, nil, map[string]providers.AccountView{providers.ProviderOpenAI: known(tc.listed...)})
		assert.Equal(t, tc.want, idsOf(set.Models(providers.ProviderOpenAI)), tc.name)
	}

	// Anthropic dated snapshots have an 8-digit suffix.
	set := providers.NewModelSet([]providers.Model{anthropicRow("claude-haiku-4-5")}, nil,
		map[string]providers.AccountView{providers.ProviderAnthropic: known("claude-haiku-4-5-20251001")})
	assert.Equal(t, []string{"claude-haiku-4-5"}, idsOf(set.Models(providers.ProviderAnthropic)))
	set = providers.NewModelSet([]providers.Model{anthropicRow("claude-haiku-4-5-20251001")}, nil,
		map[string]providers.AccountView{providers.ProviderAnthropic: known("claude-haiku-4-5")})
	assert.Equal(t, []string{"claude-haiku-4-5-20251001"}, idsOf(set.Models(providers.ProviderAnthropic)))
}

func TestModelSetFallbackRow(t *testing.T) {
	t.Parallel()
	set := providers.NewModelSet(nil, nil, map[string]providers.AccountView{
		providers.ProviderOpenAI:    known("gpt-5.4-nano-2026-03-17", "o3"),
		providers.ProviderAnthropic: known("claude-opus-5", "claude-sonnet-4-5-20250929"),
	})
	assert.Equal(t, []string{"gpt-5.4-nano-2026-03-17", "o3"}, idsOf(set.Models(providers.ProviderOpenAI)))
	assert.Equal(t, []string{"claude-sonnet-4-5-20250929", "claude-opus-5"}, idsOf(set.Models(providers.ProviderAnthropic)),
		"fallback rows follow the display order")
	for _, m := range set.All() {
		name := m.Provider + "/" + m.ID
		require.NoError(t, providers.Validate(m), name)
		assert.Equal(t, m.ID, m.Name, name)
		assert.Zero(t, m.ContextWindow, "%s: unknown for display", name)
		assert.Zero(t, m.MaxTokens, "%s: unknown for display", name)
		assert.True(t, m.CostUnknown, name)
		assert.Empty(t, m.Cost.Tiers, name)
		assert.Equal(t, []string{"text"}, m.Input, name)
		maxTokens, window := providers.RequestLimits(m)
		assert.Greater(t, maxTokens, 1, "%s: the request sends a real output limit", name)
		assert.Greater(t, window, 1, name)
	}

	opus, err := set.Find(providers.Ref{Provider: providers.ProviderAnthropic, ID: "claude-opus-5"})
	require.NoError(t, err)
	assert.Equal(t, providers.AnthropicURL, opus.BaseURL)
	assert.True(t, opus.Reasoning, "the rule words give the thinking levels")
	assert.Equal(t, protocol.ThinkingXHigh, providers.ClampThinkingLevel(opus, protocol.ThinkingXHigh))
	sonnet, err := set.Find(providers.Ref{Provider: providers.ProviderAnthropic, ID: "claude-sonnet-4-5-20250929"})
	require.NoError(t, err)
	assert.False(t, sonnet.Reasoning, "a thinking-off-only family gives Reasoning false")
	o3, err := set.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: "o3"})
	require.NoError(t, err)
	assert.Equal(t, providers.OpenAIURL, o3.BaseURL)
	assert.Equal(t, protocol.ThinkingLow, providers.ClampThinkingLevel(o3, protocol.ThinkingLow))
}

func TestModelSetDropsListedIDsWithNoShownRule(t *testing.T) {
	t.Parallel()
	set := providers.NewModelSet(nil, nil, map[string]providers.AccountView{
		providers.ProviderOpenAI:    known("gpt-daybreak-blue-latest", "whisper-1", "gpt-5.1-codex", "gpt-5.4\x1b[31m", "gpt-5.4@x"),
		providers.ProviderXAI:       known("grok-4.3"),
		providers.ProviderTokenPlan: known("qwen3.6-plus"),
	})
	assert.Empty(t, set.All(), "hidden families, ids with no rule and unsafe ids give no row")
}

func TestModelSetTokenPlanTwoAPIs(t *testing.T) {
	t.Parallel()
	for name, accounts := range map[string]map[string]providers.AccountView{
		"unknown": nil,
		"listed":  {providers.ProviderTokenPlan: known(providers.ModelDeepSeekFlash)},
	} {
		set := providers.NewModelSet(providers.GeneratedCatalog(), nil, accounts)
		rows := set.Models(providers.ProviderTokenPlan)
		require.Len(t, rows, 2, name)
		assert.Equal(t, []providers.API{providers.APIAnthropicMessages, providers.APIOpenAICompletions},
			[]providers.API{rows[0].API, rows[1].API}, "%s: both APIs, in a fixed order", name)

		ref := providers.Ref{Provider: providers.ProviderTokenPlan, ID: providers.ModelDeepSeekFlash}
		_, err := set.Find(ref)
		_, wantErr := providers.Find(ref)
		require.Error(t, err, name)
		assert.EqualError(t, err, wantErr.Error(), "%s: the same ambiguity error as Find", name)
		assert.True(t, set.Contains(ref), "%s: the set has the id on some API", name)

		ref.API = providers.APIOpenAICompletions
		m, err := set.Find(ref)
		require.NoError(t, err, name)
		assert.Equal(t, providers.TokenPlanCompletions(), m, name)
	}
}

func TestModelSetFindErrors(t *testing.T) {
	t.Parallel()
	set := providers.NewModelSet(providers.GeneratedCatalog(), nil, nil)
	for _, ref := range []providers.Ref{
		{Provider: providers.ProviderOpenAI, ID: "no-such-model"},
		{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55, API: providers.APIAnthropicMessages},
	} {
		_, err := set.Find(ref)
		_, wantErr := providers.Find(ref)
		require.Error(t, err)
		assert.EqualError(t, err, wantErr.Error())
		assert.False(t, set.Contains(ref))
	}

	var none *providers.ModelSet
	assert.Empty(t, none.All())
	assert.False(t, none.Contains(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55}))
	_, err := none.Find(providers.Ref{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55})
	assert.Error(t, err)
}

func TestModelSetDisplayOrder(t *testing.T) {
	t.Parallel()
	ids := []string{
		"gpt-5.2", "gpt-5.4-nano-2026-03-17", "gpt-4o", "gpt-5.4", "gpt-5.4-nano", "gpt-5.5",
		"gpt-4o-2024-08-06", "gpt-4o-2024-11-20", "gpt-5.4-mini", "o3", "gpt-x-9", "gpt-x-10",
	}
	var base []providers.Model
	for _, id := range ids {
		base = append(base, openAIRow(id))
	}
	base = append(base,
		anthropicRow("claude-opus-4-7"), anthropicRow("claude-haiku-5-5"), anthropicRow("claude-opus-5"),
		anthropicRow("claude-sonnet-5"), anthropicRow("claude-opus-4-8"), anthropicRow("claude-sonnet-4-6"),
		providers.TokenPlanCompletions(), providers.TokenPlanMessages(), providers.XAIGrok47(),
	)
	want := []string{
		// Token Plan, then OpenAI, Anthropic and xAI: the order of the compiled catalog.
		"alibaba-token-plan/deepseek-v4.1-flash/anthropic-messages",
		"alibaba-token-plan/deepseek-v4.1-flash/openai-completions",
		// The gpt-4o rule comes first; the alias comes before its snapshots,
		// and the newest snapshot comes first.
		"openai/gpt-4o/openai-responses",
		"openai/gpt-4o-2024-11-20/openai-responses",
		"openai/gpt-4o-2024-08-06/openai-responses",
		// One rule: the newest version first.
		"openai/gpt-5.5/openai-responses",
		"openai/gpt-5.4/openai-responses",
		"openai/gpt-5.4-mini/openai-responses",
		"openai/gpt-5.4-nano/openai-responses",
		"openai/gpt-5.4-nano-2026-03-17/openai-responses",
		"openai/gpt-5.2/openai-responses",
		"openai/o3/openai-responses",
		// Rows with no rule come last; versions compare as numbers.
		"openai/gpt-x-10/openai-responses",
		"openai/gpt-x-9/openai-responses",
		"anthropic/claude-sonnet-4-6/anthropic-messages",
		"anthropic/claude-haiku-5-5/anthropic-messages",
		"anthropic/claude-opus-5/anthropic-messages",
		"anthropic/claude-sonnet-5/anthropic-messages",
		"anthropic/claude-opus-4-8/anthropic-messages",
		"anthropic/claude-opus-4-7/anthropic-messages",
		"xai/grok-4.7/openai-responses",
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		shuffled := slices.Clone(base)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		var got []string
		for _, m := range providers.NewModelSet(shuffled, nil, nil).All() {
			got = append(got, m.Provider+"/"+m.ID+"/"+string(m.API))
		}
		require.Equal(t, want, got)
	}
}

func TestModelSetRowsShareNoStorage(t *testing.T) {
	t.Parallel()
	base := []providers.Model{providers.OpenAIGPT56Sol()}
	base[0].Headers = map[string]string{"x": "1"}
	base[0].SamplingParams = map[string]any{"top_p": 0.5}
	set := providers.NewModelSet(base, nil, nil)
	want := set.All()

	base[0].Input[0] = "changed"
	base[0].Headers["x"] = "changed"
	*base[0].ThinkingLevelMap[protocol.ThinkingMax] = "changed"
	base[0].Cost.Tiers[0].Price.Input = 1

	for _, read := range []func() providers.Model{
		func() providers.Model { return set.All()[0] },
		func() providers.Model { return set.Models(providers.ProviderOpenAI)[0] },
		func() providers.Model { m, _ := set.Find(refOf(want[0])); return m },
	} {
		m := read()
		assert.Equal(t, want[0], m, "a change to the input rows does not reach the set")
		m.Input[0] = "changed"
		m.Headers["x"] = "changed"
		*m.ThinkingLevelMap[protocol.ThinkingMax] = "changed"
		m.Cost.Tiers[0].Price.Input = 1
		m.SamplingParams["top_p"] = 1.0
		assert.Equal(t, want[0], set.All()[0], "a change to a returned row does not reach the set")
	}
}
