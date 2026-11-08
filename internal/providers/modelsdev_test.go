package providers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// modelsDevFixture has one row for each case of the mapper. The keys of the
// openai provider name the case.
const modelsDevFixture = `{
  "fetchedAt": "2026-10-28T00:00:00Z",
  "openai": {
    "api": "https://evil.example/v1",
    "npm": "@evil/sdk",
    "models": {
      "chat": {"id": "gpt-5.5", "name": "GPT-5.5", "tool_call": true, "reasoning": true,
        "reasoning_options": [{"type": "effort", "values": ["none", "low", "medium", "high", "xhigh", "max"]}],
        "modalities": {"input": ["text", "image", "pdf"], "output": ["text"]},
        "limit": {"context": 1050000, "input": 922000, "output": 128000},
        "cost": {"input": 5, "output": 30, "cache_read": 0.5,
          "tiers": [{"input": 10, "output": 45, "cache_read": 1, "tier": {"type": "context", "size": 272000}}],
          "context_over_200k": {"input": 99, "output": 99}}},
      "realtime": {"id": "gpt-5.4-realtime", "tool_call": true, "type": "realtime",
        "modalities": {"input": ["text", "audio"], "output": ["text", "audio"]}, "limit": {"context": 128000, "output": 32000}},
      "image": {"id": "gpt-5.4-image", "tool_call": false, "type": "image",
        "modalities": {"input": ["text"], "output": ["image"]}, "limit": {"context": 0, "output": 0}},
      "embedding": {"id": "gpt-5.4-embed", "tool_call": false, "type": "embedding",
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 8191, "output": 1536}},
      "deprecated": {"id": "gpt-5.2", "tool_call": true, "status": "deprecated", "reasoning": true,
        "reasoning_options": [{"type": "effort", "values": ["none", "low"]}],
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 400000, "output": 128000}},
      "unknown-option": {"id": "gpt-5.4", "tool_call": true, "reasoning": true,
        "reasoning_options": [{"type": "effort", "values": ["low"]}, {"type": "adaptive_budget"}],
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 400000, "output": 128000}},
      "override-fields": {"id": "gpt-5.1", "name": "GPT-5.1", "tool_call": true, "reasoning": true,
        "api": "https://evil.example/v1", "npm": "@evil/sdk", "provider": {"api": "https://evil.example/v1", "npm": "@evil/sdk"},
        "headers": {"x-evil": "1"},
        "reasoning_options": [{"type": "effort", "values": ["none", "low", "medium", "high"]}],
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 400000, "output": 128000}},
      "escape": {"id": "gpt-5.6-luna\u001b[2J", "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 400000, "output": 128000}},
      "no-rule": {"id": "gpt-7-preview", "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 400000, "output": 128000}},
      "hidden-rule": {"id": "gpt-daybreak-red-latest", "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 400000, "output": 128000}},
      "no-metadata": {"id": "gpt-5.6-terra", "name": "Terra\u0007", "tool_call": true, "reasoning": true,
        "reasoning_options": [{"type": "effort", "values": ["none", "low", "max"]}],
        "modalities": {"input": ["text"], "output": ["text"]}},
      "bad-shape": {"id": "gpt-5.6-sol", "tool_call": true, "limit": {"context": "big"},
        "modalities": {"input": ["text"], "output": ["text"]}}
    }
  },
  "not-an-ask-provider": {"models": {"x": {"id": "x"}}}
}`

func mapFixture(t *testing.T) map[string]Model {
	t.Helper()
	rows, err := MapModelsDev([]byte(modelsDevFixture))
	require.NoError(t, err)
	byID := map[string]Model{}
	for _, m := range rows {
		byID[m.ID] = m
	}
	return byID
}

func TestMapModelsDevKeepsOnlyRowsThatAskCanSend(t *testing.T) {
	t.Parallel()
	byID := mapFixture(t)
	assert.ElementsMatch(t, []string{"gpt-5.5", "gpt-5.1", "gpt-5.6-terra"}, keysOf(byID))
	for _, dropped := range []struct{ id, why string }{
		{"gpt-5.4-realtime", "realtime type"},
		{"gpt-5.4-image", "image output"},
		{"gpt-5.4-embed", "embedding type"},
		{"gpt-5.2", "deprecated"},
		{"gpt-5.4", "unknown reasoning option type"},
		{"gpt-7-preview", "no family rule"},
		{"gpt-daybreak-red-latest", "hidden family rule"},
		{"gpt-5.6-sol", "bad field type"},
	} {
		assert.NotContains(t, byID, dropped.id, dropped.why)
	}
	for id := range byID {
		assert.True(t, validModelText(id), "escape sequence in an id is dropped")
	}
}

func TestMapModelsDevChatRow(t *testing.T) {
	t.Parallel()
	m := mapFixture(t)["gpt-5.5"]
	require.NoError(t, Validate(m))
	assert.Equal(t, "GPT-5.5", m.Name)
	assert.Equal(t, ProviderOpenAI, m.Provider)
	assert.Equal(t, []string{"text", "image"}, m.Input, "pdf is not an Ask input kind")
	assert.Equal(t, 1_050_000, m.ContextWindow)
	assert.Equal(t, 128_000, m.MaxTokens)
	assert.False(t, m.CostUnknown)
	assert.Equal(t, []Tier{
		{Above: 0, Price: Price{Input: 5_000_000, Output: 30_000_000, CacheRead: 500_000}},
		{Above: 272_000, Price: Price{Input: 10_000_000, Output: 45_000_000, CacheRead: 1_000_000}},
	}, m.Cost.Tiers, "context_over_200k is not read")
	assert.True(t, m.Reasoning)
	assert.Equal(t, ThinkingLevelMap{
		protocol.ThinkingOff:     stringPointer("none"),
		protocol.ThinkingMinimal: nil,
		protocol.ThinkingLow:     stringPointer("low"),
		protocol.ThinkingMedium:  stringPointer("medium"),
		protocol.ThinkingHigh:    stringPointer("high"),
		protocol.ThinkingXHigh:   stringPointer("xhigh"),
		protocol.ThinkingMax:     nil,
	}, m.ThinkingLevelMap, "the rule has no max word, so models.dev cannot add it")
}

func TestMapModelsDevIgnoresEndpointFields(t *testing.T) {
	t.Parallel()
	m := mapFixture(t)["gpt-5.1"]
	assert.Equal(t, OpenAIURL, m.BaseURL)
	assert.Equal(t, APIOpenAIResponses, m.API)
	assert.Equal(t, ResponsesCompat{Store: false, SupportsMaxOutputTokens: true}, m.Compat)
	assert.Empty(t, m.Headers)
}

func TestMapModelsDevRowWithoutMetadata(t *testing.T) {
	t.Parallel()
	m := mapFixture(t)["gpt-5.6-terra"]
	assert.Equal(t, "gpt-5.6-terra", m.Name, "a name with a control character falls back to the id")
	assert.Zero(t, m.ContextWindow, "unknown for display")
	assert.Zero(t, m.MaxTokens, "unknown for display")
	assert.True(t, m.CostUnknown)
	assert.Empty(t, m.Cost.Tiers)
	maxTokens, window := RequestLimits(m)
	assert.Equal(t, 128_000, maxTokens, "the request uses the family default")
	assert.Equal(t, 1_050_000, window)
	assert.Equal(t, ThinkingLevelMap{
		protocol.ThinkingOff:     stringPointer("none"),
		protocol.ThinkingMinimal: nil,
		protocol.ThinkingLow:     stringPointer("low"),
		protocol.ThinkingMedium:  nil,
		protocol.ThinkingHigh:    nil,
		protocol.ThinkingXHigh:   nil,
		protocol.ThinkingMax:     stringPointer("max"),
	}, m.ThinkingLevelMap, "models.dev narrows the rule words")
}

func TestMapModelsDevRejectsABadPayload(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{``, `[]`, `null`, `{"openai": []}`, `{"openai": {"models": []}}`} {
		_, err := MapModelsDev([]byte(payload))
		assert.Error(t, err, "payload %q", payload)
	}
	rows, err := MapModelsDev([]byte(`{"other": {}}`))
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestCostOfModelsDev(t *testing.T) {
	t.Parallel()
	price := modelsDevPrice{Input: 0.175, Output: 14, CacheRead: 0.0175, CacheWrite: 3.75}
	tier := func(typ string, size int64) modelsDevTier {
		t := modelsDevTier{modelsDevPrice: modelsDevPrice{Input: 2}}
		t.Tier.Type, t.Tier.Size = typ, size
		return t
	}
	cases := []struct {
		name        string
		in          *modelsDevCost
		wantTiers   []Tier
		wantUnknown bool
	}{
		{name: "missing cost", in: nil, wantUnknown: true},
		{name: "free is known", in: &modelsDevCost{}, wantTiers: []Tier{{}}},
		{
			name:      "rounds to micro-USD",
			in:        &modelsDevCost{modelsDevPrice: price},
			wantTiers: []Tier{{Price: Price{Input: 175_000, Output: 14_000_000, CacheRead: 17_500, CacheWrite: 3_750_000}}},
		},
		{
			name: "tiers are sorted by threshold",
			in:   &modelsDevCost{Tiers: []modelsDevTier{tier("context", 500_000), tier("context", 200_000)}},
			wantTiers: []Tier{{}, {Above: 200_000, Price: Price{Input: 2_000_000}},
				{Above: 500_000, Price: Price{Input: 2_000_000}}},
		},
		{name: "unknown tier type", in: &modelsDevCost{Tiers: []modelsDevTier{tier("time_of_day", 1)}}, wantUnknown: true},
		{name: "tier without size", in: &modelsDevCost{Tiers: []modelsDevTier{tier("context", 0)}}, wantUnknown: true},
		{name: "negative price", in: &modelsDevCost{modelsDevPrice: modelsDevPrice{Input: -1}}, wantUnknown: true},
		{name: "absurd price", in: &modelsDevCost{modelsDevPrice: modelsDevPrice{Output: 2e9}}, wantUnknown: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, unknown := costOfModelsDev(tc.in)
			assert.Equal(t, tc.wantUnknown, unknown)
			assert.Equal(t, tc.wantTiers, got.Tiers)
		})
	}
}

func TestValidModelText(t *testing.T) {
	t.Parallel()
	long := make([]byte, maxModelText+1)
	for i := range long {
		long[i] = 'a'
	}
	assert.True(t, validModelText("gpt-5.5"))
	assert.True(t, validModelText("Claude Sonnet 4.5 (latest)"))
	assert.True(t, validModelText(string(long[:maxModelText])))
	for _, bad := range []string{"", "a@b", "a\x1b[2J", "a\nb", "a\x7f", "gpt-5é", string(long)} {
		assert.False(t, validModelText(bad), "%q", bad)
	}
}

// TestModelsDevParityWithHandWrittenRows maps the snapshot rows of the
// hand-written models and compares the data that models.dev gives. Each known
// difference is listed with its reason.
func TestModelsDevParityWithHandWrittenRows(t *testing.T) {
	t.Parallel()
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(catalogSnapshot, &top))
	snapshotRow := func(t *testing.T, provider, id string) modelsDevModel {
		t.Helper()
		var p modelsDevProvider
		require.NoError(t, json.Unmarshal(top[provider], &p), provider)
		var row modelsDevModel
		require.NoError(t, json.Unmarshal(p.Models[id], &row), "%s/%s", provider, id)
		return row
	}
	cases := []struct {
		hand Model
		// wantTiers is the price that the generated row has. Nil means the
		// same tiers as the hand-written row.
		wantTiers []Tier
		reason    string
	}{
		{hand: OpenAIGPT55()},
		{hand: OpenAIGPT56Sol()},
		{
			hand:      AnthropicSonnet46(),
			wantTiers: []Tier{{Price: Price{Input: 3_000_000, Output: 15_000_000, CacheRead: 300_000, CacheWrite: 3_750_000}}},
			reason:    "the hand-written row has no price, because the Anthropic adapter computes no cost",
		},
		{
			hand: XAIGrok47(),
			wantTiers: []Tier{
				{Price: Price{Input: 2_000_000, Output: 6_000_000, CacheRead: 500_000}},
				{Above: 200_000, Price: Price{Input: 4_000_000, Output: 12_000_000, CacheRead: 1_000_000}},
			},
			reason: "the hand-written row has no price; the xAI family rule is hidden, so only the metadata is compared",
		},
	}
	for _, tc := range cases {
		t.Run(tc.hand.ID, func(t *testing.T) {
			t.Parallel()
			var got Model
			applyModelsDevMetadata(&got, snapshotRow(t, tc.hand.Provider, tc.hand.ID))
			assert.Equal(t, tc.hand.ContextWindow, got.ContextWindow)
			assert.Equal(t, tc.hand.MaxTokens, got.MaxTokens)
			assert.Equal(t, tc.hand.Input, got.Input)
			assert.False(t, got.CostUnknown)
			want := tc.hand.Cost.Tiers
			if tc.wantTiers != nil {
				want = tc.wantTiers
			}
			assert.Equal(t, want, got.Cost.Tiers, tc.reason)
		})
	}

	// Request data differs on purpose. The generated gpt-5.5 row has an
	// explicit thinking map and no long cache retention, because no live
	// request proved that retention for the generated families; the
	// hand-written row keeps both of its choices and wins in GeneratedCatalog.
	rows, err := MapModelsDev(catalogSnapshot)
	require.NoError(t, err)
	for _, m := range rows {
		if m.ID != ModelGPT55 {
			continue
		}
		c, _ := m.Responses()
		assert.False(t, c.SupportsLongCacheRetention)
		assert.NotNil(t, m.ThinkingLevelMap)
		assert.Equal(t, "GPT-5.5", m.Name, "the hand-written name is the id")
	}
}

func keysOf(m map[string]Model) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
