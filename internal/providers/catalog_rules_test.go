package providers

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

var effortWords = []string{offWord, "minimal", "low", "medium", "high", "xhigh", "max"}

func TestFamilyRulesAreComplete(t *testing.T) {
	t.Parallel()
	for _, r := range familyRules {
		name := r.provider + " " + r.pattern.String()
		require.Contains(t, modelsDevProviders, r.provider, name)
		if r.hidden != "" {
			assert.Empty(t, r.api, "%s: a hidden rule holds no request data", name)
			continue
		}
		assert.True(t, compatMatchesAPI(r.compat, r.api), "%s: compat matches the API", name)
		assert.NotEmpty(t, r.baseURL, name)
		assert.Positive(t, r.maxTokens, "%s: request default for the output limit", name)
		assert.Positive(t, r.contextWindow, "%s: request default for the context window", name)
		assert.Less(t, r.maxTokens, r.contextWindow, name)
		for _, w := range r.words {
			assert.Contains(t, effortWords, w, name)
		}
		if r.thinkingOffOnly {
			assert.Empty(t, r.words, "%s: a thinking-off-only rule has no effort words", name)
			assert.NotEqual(t, offRejected, r.off, "%s: thinking off only and off rejected contradict", name)
		}
		if r.off != offByWord {
			assert.NotContains(t, r.words, offWord, "%s: only an offByWord rule sends the off word", name)
		}
	}
}

func TestFamilyRuleMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider, id string
		wantShown    bool
		wantHidden   bool
	}{
		{ProviderAnthropic, "claude-opus-5", true, false},
		{ProviderAnthropic, "claude-opus-5-5", true, false},
		{ProviderAnthropic, "claude-haiku-4-5-20251001", true, false},
		{ProviderAnthropic, "claude-opus-4-1", false, false},
		{ProviderOpenAI, "gpt-5", true, false},
		{ProviderOpenAI, "gpt-5-nano-2025-08-07", true, false},
		{ProviderOpenAI, "gpt-5.4-pro-2026-03-05", true, false},
		{ProviderOpenAI, "gpt-5.1-codex", false, false},
		{ProviderOpenAI, "gpt-5.4-realtime", false, false},
		{ProviderOpenAI, "xgpt-5", false, false},
		{ProviderOpenAI, "gpt-6-astra", true, false},
		{ProviderOpenAI, "gpt-6.1-terra", true, false},
		{ProviderOpenAI, "gpt-6-terra", false, false},
		{ProviderOpenAI, "gpt-daybreak-blue-latest", false, true},
		{ProviderXAI, "grok-4.7", false, true},
		{ProviderXAI, "grok-4.20-0309-reasoning", false, true},
		{ProviderXAI, "grok-4.20-0309-non-reasoning", false, true},
		{ProviderXAI, "grok-4.20-multi-agent-0309", false, false},
		{ProviderTokenPlan, "deepseek-v4.1-flash-1017", false, true},
		{ProviderTokenPlan, "MiniMax-M2.5", false, true},
		{"other", "gpt-5", false, false},
	}
	for _, tc := range cases {
		r, ok := ruleFor(tc.provider, tc.id)
		assert.Equal(t, tc.wantShown, ok && r.hidden == "", "%s/%s shown", tc.provider, tc.id)
		assert.Equal(t, tc.wantHidden, ok && r.hidden != "", "%s/%s hidden", tc.provider, tc.id)
	}
}

func TestThinkingOf(t *testing.T) {
	t.Parallel()
	all := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	levels := func(words map[protocol.ThinkingLevel]string) ThinkingLevelMap {
		out := ThinkingLevelMap{}
		for _, l := range thinkingRank {
			out[l] = nil
			if w, ok := words[l]; ok {
				out[l] = stringPointer(w)
			}
		}
		return out
	}
	cases := []struct {
		name       string
		rule       familyRule
		reasoning  bool
		values     []string
		wantOn     bool
		wantLevels ThinkingLevelMap
		wantOK     bool
	}{
		{
			name: "off by word needs none in both lists", rule: familyRule{off: offByWord, words: []string{"none", "low", "high"}},
			reasoning: true, values: all, wantOn: true, wantOK: true,
			wantLevels: levels(map[protocol.ThinkingLevel]string{protocol.ThinkingOff: "none", protocol.ThinkingLow: "low", protocol.ThinkingHigh: "high"}),
		},
		{
			name: "off by word without none leaves off nil", rule: familyRule{off: offByWord, words: []string{"none", "minimal", "high"}},
			reasoning: true, values: []string{"minimal", "high"}, wantOn: true, wantOK: true,
			wantLevels: levels(map[protocol.ThinkingLevel]string{protocol.ThinkingMinimal: "minimal", protocol.ThinkingHigh: "high"}),
		},
		{
			name: "off by wire supports off without a models.dev word", rule: familyRule{off: offByWire, words: []string{"low", "max"}},
			reasoning: true, values: []string{"low", "medium", "max"}, wantOn: true, wantOK: true,
			wantLevels: levels(map[protocol.ThinkingLevel]string{protocol.ThinkingOff: "none", protocol.ThinkingLow: "low", protocol.ThinkingMax: "max"}),
		},
		{
			name: "off rejected has a nil off", rule: familyRule{off: offRejected, words: []string{"low", "high"}},
			reasoning: true, values: all, wantOn: true, wantOK: true,
			wantLevels: levels(map[protocol.ThinkingLevel]string{protocol.ThinkingLow: "low", protocol.ThinkingHigh: "high"}),
		},
		{
			name: "off rejected with no effort word cannot make a request", rule: familyRule{off: offRejected, words: []string{"low"}},
			reasoning: true, values: nil, wantOK: false,
		},
		{
			name: "thinking off only", rule: familyRule{off: offByWire, thinkingOffOnly: true},
			reasoning: true, values: all, wantOK: true,
		},
		{
			name: "models.dev without reasoning", rule: familyRule{off: offByWord, words: []string{"none", "low"}},
			reasoning: false, values: all, wantOK: true,
		},
		{
			name: "only the off word left", rule: familyRule{off: offByWord, words: []string{"none", "low"}},
			reasoning: true, values: []string{"none"}, wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			on, got, ok := tc.rule.thinkingOf(tc.reasoning, tc.values)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantOn, on)
			assert.Equal(t, tc.wantLevels, got)
			if on {
				assert.Len(t, got, len(thinkingRank), "every level has an explicit entry")
			}
		})
	}
}

func TestRequestLimits(t *testing.T) {
	t.Parallel()
	opus := Model{Provider: ProviderAnthropic, ID: "claude-opus-4-7", API: APIAnthropicMessages}
	cases := []struct {
		name       string
		m          Model
		wantMax    int
		wantWindow int
	}{
		{name: "row values win", m: AnthropicSonnet46(), wantMax: 128_000, wantWindow: 1_000_000},
		{name: "both unknown use the rule", m: opus, wantMax: 128_000, wantWindow: 1_000_000},
		{name: "one unknown", m: withLimits(opus, 9_000, 0), wantMax: 9_000, wantWindow: 1_000_000},
		{name: "negative is unknown", m: withLimits(opus, -1, -1), wantMax: 128_000, wantWindow: 1_000_000},
		{name: "rule of another API does not apply", m: Model{Provider: ProviderAnthropic, ID: "claude-opus-4-7", API: APIOpenAIResponses}},
		{name: "hidden rule gives no default", m: Model{Provider: ProviderXAI, ID: "grok-4.7", API: APIOpenAIResponses}},
		{name: "no rule", m: Model{Provider: "other", ID: "x", API: APIOpenAIResponses}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotMax, gotWindow := RequestLimits(tc.m)
			assert.Equal(t, tc.wantMax, gotMax)
			assert.Equal(t, tc.wantWindow, gotWindow)
		})
	}
}

func withLimits(m Model, maxTokens, window int) Model {
	m.MaxTokens, m.ContextWindow = maxTokens, window
	return m
}

// TestHiddenFamiliesStayHidden checks that the snapshot has rows of the
// hidden families and that none of them is mapped.
func TestHiddenFamiliesStayHidden(t *testing.T) {
	t.Parallel()
	rows, err := MapModelsDev(catalogSnapshot)
	require.NoError(t, err)
	for _, m := range rows {
		r, ok := ruleFor(m.Provider, m.ID)
		require.True(t, ok, "%s/%s has a rule", m.Provider, m.ID)
		assert.Empty(t, r.hidden, "%s/%s is hidden", m.Provider, m.ID)
		assert.NotEqual(t, ProviderXAI, m.Provider)
		assert.NotEqual(t, ProviderTokenPlan, m.Provider)
	}
	assert.False(t, slices.ContainsFunc(rows, func(m Model) bool { return m.ID == "gpt-daybreak-blue-latest" }))

	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(catalogSnapshot, &top))
	hiddenInSnapshot := 0
	for _, provider := range modelsDevProviders {
		var p modelsDevProvider
		require.NoError(t, json.Unmarshal(top[provider], &p), provider)
		for id := range p.Models {
			if r, ok := ruleFor(provider, id); ok && r.hidden != "" {
				hiddenInSnapshot++
			}
		}
	}
	assert.Positive(t, hiddenInSnapshot, "the snapshot has rows of hidden families")
}

// TestGrok420FamiliesHaveNoThinkingLevels checks that the grok-4.20 rows can
// never get an effort level. xAI answers HTTP 400 when a request to these ids
// has reasoning.effort. The rules stay hidden; the check also holds for a
// rule that a later change shows and gives effort words.
func TestGrok420FamiliesHaveNoThinkingLevels(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"grok-4.20-0309-reasoning", "grok-4.20-0309-non-reasoning"} {
		r, ok := ruleFor(ProviderXAI, id)
		require.True(t, ok, id)
		assert.NotEmpty(t, r.hidden, "%s stays hidden", id)

		shown := r.request(APIOpenAIResponses, XAIURL, ResponsesCompat{Store: false, SupportsMaxOutputTokens: true}, 30_000, 1_000_000).
			thinking(offByWord, effortWords...)
		shown.hidden = ""
		for _, reasoning := range []bool{true, false} {
			on, levels, ok := shown.thinkingOf(reasoning, effortWords)
			assert.True(t, ok, "%s reasoning=%v makes a request", id, reasoning)
			assert.False(t, on, "%s reasoning=%v has no thinking", id, reasoning)
			assert.Nil(t, levels, "%s reasoning=%v has no thinking levels", id, reasoning)
		}
	}
}
