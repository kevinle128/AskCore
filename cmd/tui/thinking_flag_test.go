package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/anthropic"
	"AskCore/pkg/protocol"
)

// TestThinkingFlagDefault runs --thinking default through the real entry
// points. The word is valid, an unknown word lists it first, and the start
// level rule of the provider that headless.go gives medium (the token plan)
// does not apply: Ask sends no level, so the adapter rule applies.
func TestThinkingFlagDefault(t *testing.T) {
	t.Run("an unknown word lists default", func(t *testing.T) {
		got := runCLI("", "--thinking", "ultra", "-p", "hi")
		assert.Equal(t, "Warning: Invalid thinking level \"ultra\". Valid values: default, off, minimal, low, medium, high, xhigh, max\n", got.stderr)
	})
	t.Run("print mode stamps default", func(t *testing.T) {
		got := runCLI("", "--thinking", "default", "--mode", "json", "-p", "hi")
		require.Equal(t, 0, got.code, got.stderr)
		assert.Empty(t, got.stderr)
		assert.Contains(t, got.stdout, `"thinkingLevel":"default"`)
		assert.NotContains(t, got.stdout, `"thinkingLevel":"off"`)
	})
	t.Run("the command line sends no level for the token plan", func(t *testing.T) {
		getenv := func(string) string { return "" }
		ag, err := newHeadlessAgent(options{provider: anthropic.ProviderID, model: anthropic.ModelID, thinking: protocol.ThinkingDefault}, getenv)
		require.NoError(t, err)
		t.Cleanup(func() { _ = ag.Dispose() })
		assert.Equal(t, protocol.ThinkingDefault, ag.State().ThinkingLevel, "no level of the command line")

		plain, err := newHeadlessAgent(options{provider: anthropic.ProviderID, model: anthropic.ModelID}, getenv)
		require.NoError(t, err)
		t.Cleanup(func() { _ = plain.Dispose() })
		assert.Equal(t, protocol.ThinkingMedium, plain.State().ThinkingLevel, "with no --thinking the start rule of the token plan stays")
	})
	t.Run("a prompt with default reaches the adapter with no level", func(t *testing.T) {
		ag, err := newHeadlessAgent(options{provider: anthropic.ProviderID, model: anthropic.ModelID, thinking: protocol.ThinkingDefault}, func(string) string { return "" })
		require.NoError(t, err)
		t.Cleanup(func() { _ = ag.Dispose() })
		require.NoError(t, ag.Prompt(context.Background(), protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}))
		msgs := ag.State().Messages
		last, ok := msgs[len(msgs)-1].(protocol.AssistantMessage)
		require.True(t, ok)
		require.NotNil(t, last.ThinkingLevel)
		assert.Equal(t, protocol.ThinkingDefault, *last.ThinkingLevel)
	})
}
