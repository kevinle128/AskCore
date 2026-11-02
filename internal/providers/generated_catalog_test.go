package providers_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/internal/providers/openai"
	"AskCore/pkg/protocol"
)

var allLevels = []protocol.ThinkingLevel{
	protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
	protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax,
}

func helloRequest() providers.TranscriptRequest {
	return providers.NormalizeRequest(providers.Request{
		SystemPrompt: "you are a tool",
		Messages:     []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}},
	})
}

func TestGeneratedCatalogKeepsTheHandWrittenRows(t *testing.T) {
	t.Parallel()
	rows := providers.GeneratedCatalog()
	key := func(m providers.Model) providers.Ref {
		return providers.Ref{Provider: m.Provider, ID: m.ID, API: m.API}
	}
	byKey := map[providers.Ref]providers.Model{}
	for _, m := range rows {
		_, dup := byKey[key(m)]
		require.False(t, dup, "one row for %v", key(m))
		byKey[key(m)] = m
		require.NoError(t, providers.Validate(m), "%v", key(m))
	}
	for _, hand := range providers.AvailableModels() {
		got, ok := byKey[key(hand)]
		require.True(t, ok, "hand-written row %v is in the generated catalog", key(hand))
		if hand.ThinkingLevelMap == nil {
			// A hand-written row with no map takes the map of the generated
			// row; every other field stays hand-written.
			hand.ThinkingLevelMap = got.ThinkingLevelMap
		}
		assert.Equal(t, hand, got, "the hand-written row wins over the generated row")
	}
	for _, ref := range []providers.Ref{
		{Provider: providers.ProviderOpenAI, ID: providers.ModelGPT55, API: providers.APIOpenAIResponses},
		{Provider: providers.ProviderAnthropic, ID: providers.ModelClaudeSonnet46, API: providers.APIAnthropicMessages},
	} {
		assert.NotNil(t, byKey[ref].ThinkingLevelMap, "%v takes the map of its generated row", ref)
	}
	assert.Len(t, providers.AvailableModels(), 6, "the compiled catalog does not use the generated rows")
	assert.Greater(t, len(rows), len(providers.AvailableModels()))

	rows[0].ID = "changed"
	assert.NotEqual(t, "changed", providers.GeneratedCatalog()[0].ID, "rows share no storage")
}

// TestGeneratedRowsSendTheirEffort walks every generated Responses and
// Completions row (not the hand-written rows) through the real Prepare and
// Encode. For each level that ClampThinkingLevel keeps, a row with Reasoning
// true must put an effort word on the wire, and a row with Reasoning false
// must send none.
func TestGeneratedRowsSendTheirEffort(t *testing.T) {
	t.Parallel()
	preparers := map[providers.API]providers.Preparer{
		providers.APIOpenAIResponses:   openai.NewResponses().(providers.Preparer),
		providers.APIOpenAICompletions: openai.NewCompletions().(providers.Preparer),
	}
	// The hand-written rows keep their own maps and are not part of this walk.
	hand := map[providers.Ref]bool{}
	for _, m := range providers.AvailableModels() {
		hand[providers.Ref{Provider: m.Provider, ID: m.ID, API: m.API}] = true
	}
	walked := map[providers.API]int{}
	for _, m := range providers.GeneratedCatalog() {
		p, ok := preparers[m.API]
		if !ok || hand[providers.Ref{Provider: m.Provider, ID: m.ID, API: m.API}] {
			continue
		}
		walked[m.API]++
		for _, level := range allLevels {
			if providers.ClampThinkingLevel(m, level) != level {
				continue
			}
			pr, err := p.Prepare(m, helloRequest(), providers.StreamOptions{Reasoning: level})
			require.NoError(t, err, "%s/%s %s", m.Provider, m.ID, level)
			enc, err := p.Encode(context.Background(), m, pr, helloRequest(), providers.AuthBinding{})
			require.NoError(t, err, "%s/%s %s", m.Provider, m.ID, level)
			if !m.Reasoning {
				assert.Empty(t, pr.Effort, "%s/%s %s", m.Provider, m.ID, level)
				continue
			}
			require.NotEmpty(t, pr.Effort, "%s/%s %s has an effort word", m.Provider, m.ID, level)
			assert.Equal(t, pr.Effort, sentEffort(t, m.API, enc.Body), "%s/%s %s effort on the wire", m.Provider, m.ID, level)
		}
	}
	assert.Positive(t, walked[providers.APIOpenAIResponses], "the walk covers Responses rows")
	// Completions has only the hand-written Token Plan row today: the generated
	// Token Plan families are hidden. The walk covers Completions rows when a
	// family is shown.
	t.Logf("rows walked per API: %v", walked)
}

// TestGPT55SendsItsEffort proves that the hand-written gpt-5.5 row, which has
// no thinking map of its own, puts an effort word on the Responses wire once
// the generated catalog gives it the map of its family.
func TestGPT55SendsItsEffort(t *testing.T) {
	t.Parallel()
	var m providers.Model
	for _, row := range providers.GeneratedCatalog() {
		if row.Provider == providers.ProviderOpenAI && row.ID == providers.ModelGPT55 && row.API == providers.APIOpenAIResponses {
			m = row
		}
	}
	require.Equal(t, providers.ModelGPT55, m.ID)
	p := openai.NewResponses().(providers.Preparer)
	require.Equal(t, protocol.ThinkingHigh, providers.ClampThinkingLevel(m, protocol.ThinkingHigh))
	pr, err := p.Prepare(m, helloRequest(), providers.StreamOptions{Reasoning: protocol.ThinkingHigh})
	require.NoError(t, err)
	assert.Equal(t, "high", pr.Effort)
	enc, err := p.Encode(context.Background(), m, pr, helloRequest(), providers.AuthBinding{})
	require.NoError(t, err)
	assert.Equal(t, "high", sentEffort(t, m.API, enc.Body))

	// The compiled row alone sends no effort: this is the defect that the map
	// of the generated row fixes.
	pr, err = p.Prepare(providers.OpenAIGPT55(), helloRequest(), providers.StreamOptions{Reasoning: protocol.ThinkingHigh})
	require.NoError(t, err)
	assert.Empty(t, pr.Effort)
}

func sentEffort(t *testing.T, api providers.API, body []byte) string {
	t.Helper()
	var doc struct {
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	require.NoError(t, json.Unmarshal(body, &doc))
	if api == providers.APIOpenAICompletions {
		return doc.ReasoningEffort
	}
	return doc.Reasoning.Effort
}

// TestGPT6RowsReason checks the effort words of each gpt-6 variant. astra and
// 6.1-sol refuse "none", so off clamps to the lowest effort; the other
// variants take "none" for off.
func TestGPT6RowsReason(t *testing.T) {
	t.Parallel()
	byID := map[string]providers.Model{}
	for _, m := range providers.GeneratedCatalog() {
		if m.Provider == providers.ProviderOpenAI && m.API == providers.APIOpenAIResponses {
			byID[m.ID] = m
		}
	}
	cases := []struct {
		id      string
		off     protocol.ThinkingLevel // clamp result for off
		offWord string                 // "" when the map holds no word for off
	}{
		{"gpt-6-astra", protocol.ThinkingLow, ""},
		{"gpt-6.1-sol", protocol.ThinkingLow, ""},
		{"gpt-6-sol", protocol.ThinkingOff, "none"},
		{"gpt-6-luna", protocol.ThinkingOff, "none"},
		{"gpt-6.1-terra", protocol.ThinkingOff, "none"},
	}
	for _, tc := range cases {
		m, ok := byID[tc.id]
		require.True(t, ok, tc.id)
		assert.True(t, m.Reasoning, tc.id)
		assert.Equal(t, tc.off, providers.ClampThinkingLevel(m, protocol.ThinkingOff), tc.id)
		word, has := m.ThinkingLevelMap[protocol.ThinkingOff]
		if tc.offWord == "" {
			assert.True(t, has && word == nil, "%s: no word for off", tc.id)
		} else {
			require.True(t, has && word != nil, tc.id)
			assert.Equal(t, tc.offWord, *word, tc.id)
		}
		for _, l := range []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax} {
			assert.Equal(t, l, providers.ClampThinkingLevel(m, l), "%s %s", tc.id, l)
		}
	}
}
