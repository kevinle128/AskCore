package openai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func generatedRow(t *testing.T, provider, id string) providers.Model {
	t.Helper()
	for _, m := range providers.GeneratedCatalog() {
		if m.Provider == provider && m.ID == id && m.API == providers.APIOpenAIResponses {
			return m
		}
	}
	t.Fatalf("no generated row for %s/%s", provider, id)
	return providers.Model{}
}

// sentEffortFor streams one request through the real Responses provider and
// returns the reasoning.effort value in the request body. The bool is false
// when the body has no effort.
func sentEffortFor(t *testing.T, m providers.Model, level protocol.ThinkingLevel) (string, bool) {
	t.Helper()
	srv := newCaptureServer(t, responsesSSE())
	m.BaseURL = srv.srv.URL
	stream := responsesProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Reasoning: level, APIKey: "test-key"})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	reasoning, ok := srv.take(t).body(t)["reasoning"].(map[string]any)
	if !ok {
		return "", false
	}
	effort, ok := reasoning["effort"].(string)
	return effort, ok
}

// TestGPT6SendsItsEffort checks that every supported level of a gpt-6 row
// reaches the wire, and that a family that refuses "none" never receives it.
func TestGPT6SendsItsEffort(t *testing.T) {
	t.Parallel()
	full := map[protocol.ThinkingLevel]string{
		protocol.ThinkingLow: "low", protocol.ThinkingMedium: "medium", protocol.ThinkingHigh: "high",
		protocol.ThinkingXHigh: "xhigh", protocol.ThinkingMax: "max",
	}
	cases := []struct {
		id      string
		offWord string // the word sent for thinking off
	}{
		{"gpt-6-astra", "low"},
		{"gpt-6.1-sol", "low"},
		{"gpt-6-sol", "none"},
		{"gpt-6-luna", "none"},
		{"gpt-6.1-terra", "none"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			m := generatedRow(t, providers.ProviderOpenAI, tc.id)
			require.True(t, m.Reasoning)
			for level, want := range full {
				got, ok := sentEffortFor(t, m, providers.ClampThinkingLevel(m, level))
				require.True(t, ok, "%s %s sends an effort", tc.id, level)
				assert.Equal(t, want, got, "%s %s", tc.id, level)
			}
			got, ok := sentEffortFor(t, m, providers.ClampThinkingLevel(m, protocol.ThinkingOff))
			require.True(t, ok, "%s off sends an effort", tc.id)
			assert.Equal(t, tc.offWord, got, "%s off", tc.id)
		})
	}
}

// TestGrok47SendsItsEffort checks that the built-in grok-4.7 row puts the
// level on the wire. The model cannot turn thinking off, so off clamps to low.
func TestGrok47SendsItsEffort(t *testing.T) {
	t.Parallel()
	m := providers.XAIGrok47()
	for level, want := range map[protocol.ThinkingLevel]string{
		protocol.ThinkingOff:    "low",
		protocol.ThinkingLow:    "low",
		protocol.ThinkingMedium: "medium",
		protocol.ThinkingHigh:   "high",
		protocol.ThinkingXHigh:  "xhigh",
	} {
		got, ok := sentEffortFor(t, m, providers.ClampThinkingLevel(m, level))
		require.True(t, ok, "%s sends an effort", level)
		assert.Equal(t, want, got, "grok-4.7 %s", level)
	}
}

// TestGPT6RefusingNoneNeverSendsIt checks that an unclamped off request to a
// family that refuses "none" does not put that word on the wire.
func TestGPT6RefusingNoneNeverSendsIt(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		got, ok := sentEffortFor(t, generatedRow(t, providers.ProviderOpenAI, id), protocol.ThinkingOff)
		assert.NotEqual(t, "none", got, id)
		assert.False(t, ok, "%s: off sends no effort", id)
	}
}

// TestGrok420SendsNoEffort checks that a request to the grok-4.20 ids has no
// reasoning.effort at any thinking level, with and without the clamp. xAI
// answers HTTP 400 for these ids when the request has an effort. The row has
// the thinking data that the family rule gives: no thinking and no levels.
func TestGrok420SendsNoEffort(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"grok-4.20-0309-reasoning", "grok-4.20-0309-non-reasoning"} {
		m := providers.Model{ID: id, Name: id, Provider: providers.ProviderXAI, API: providers.APIOpenAIResponses, BaseURL: providers.XAIURL,
			Input: []string{"text"}, Compat: providers.ResponsesCompat{Store: false, SupportsMaxOutputTokens: true}}
		for _, level := range []protocol.ThinkingLevel{
			protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
			protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax,
		} {
			for _, sent := range []protocol.ThinkingLevel{level, providers.ClampThinkingLevel(m, level)} {
				got, ok := sentEffortFor(t, m, sent)
				assert.False(t, ok, "%s %s sends no effort, got %q", id, sent, got)
			}
		}
	}
}
