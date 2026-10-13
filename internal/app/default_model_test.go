package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// choiceOf builds a start choice over the compiled catalog. signedIn lists the
// usable providers, saved is the saved default id.
func choiceOf(initial providers.Model, saved string, signedIn ...string) startChoice {
	usable := map[string]bool{}
	for _, p := range signedIn {
		usable[p] = true
	}
	rows := func() []providers.Model {
		catalog := providers.AvailableModels()
		if initial.ID == "" {
			return catalog
		}
		return append([]providers.Model{initial}, catalog...)
	}
	return startChoice{
		initial: initial,
		rows:    rows,
		find: func(ref providers.Ref) (providers.Model, error) {
			if initial.ID != "" && ref.Provider == initial.Provider && ref.ID == initial.ID {
				return initial, nil
			}
			return providers.Find(ref)
		},
		usable: func(_ context.Context, provider string) bool {
			return usable[provider] || (initial.ID != "" && provider == initial.Provider)
		},
		saved: func(context.Context) (string, error) { return saved, nil },
	}
}

func idOf(m providers.Model) string { return acp.ModelID(m) }

func TestSessionStartUsesSavedDefault(t *testing.T) {
	sol := providers.OpenAIGPT56Sol()
	m, fb := choiceOf(providers.Model{}, idOf(sol), "openai", "anthropic").pick(context.Background())
	require.Equal(t, sol, m, "the saved default wins over the provider default")
	require.Nil(t, fb)
}

func TestSessionStartFallsBackWhenDefaultUnusable(t *testing.T) {
	saved := idOf(providers.OpenAIGPT56Sol())
	m, fb := choiceOf(providers.Model{}, saved, "anthropic").pick(context.Background())
	require.Equal(t, providers.AnthropicSonnet46(), m)
	require.Equal(t, &protocol.ACPModelFallback{SavedDefault: saved, Reason: protocol.ACPFallbackNoCredential}, fb)

	gone := "openai/gpt-0@openai-responses"
	m, fb = choiceOf(providers.Model{}, gone, "anthropic").pick(context.Background())
	require.Equal(t, providers.AnthropicSonnet46(), m)
	require.Equal(t, &protocol.ACPModelFallback{SavedDefault: gone, Reason: protocol.ACPFallbackNotInCatalog}, fb)

	m, fb = choiceOf(providers.Model{}, "not a model id", "anthropic").pick(context.Background())
	require.Equal(t, providers.AnthropicSonnet46(), m)
	require.Equal(t, protocol.ACPFallbackNotInCatalog, fb.Reason)
}

func TestSessionStartUsesRecommendedModel(t *testing.T) {
	c := choiceOf(providers.Model{}, "", "openai")
	// The catalog lists the large row first in this ordering; the recommended row is the other.
	c.rows = func() []providers.Model {
		return []providers.Model{providers.OpenAIGPT56Sol(), providers.OpenAIGPT55()}
	}
	m, fb := c.pick(context.Background())
	require.Equal(t, providers.OpenAIGPT55(), m)
	require.Nil(t, fb)

	// The first usable provider in catalog order decides, not the first signed in.
	m, _ = choiceOf(providers.Model{}, "", "xai", "anthropic").pick(context.Background())
	require.Equal(t, providers.AnthropicSonnet46(), m)
}

func TestStartChoiceFirstUsableRowWhenNoRecommendedModel(t *testing.T) {
	c := choiceOf(providers.Model{}, "", "openai")
	c.find = func(providers.Ref) (providers.Model, error) { return providers.Model{}, errors.New("gone") }
	c.rows = func() []providers.Model {
		return []providers.Model{providers.OpenAIGPT56Sol(), providers.OpenAIGPT55()}
	}
	m, _ := c.pick(context.Background())
	require.Equal(t, providers.OpenAIGPT56Sol(), m, "the first usable row when the provider default is not in the catalog")
}

func TestSessionStartNoModelWhenNothingUsable(t *testing.T) {
	m, fb := choiceOf(providers.Model{}, "").pick(context.Background())
	require.Equal(t, providers.Model{}, m)
	require.Nil(t, fb)
	m, fb = choiceOf(providers.Model{}, idOf(providers.OpenAIGPT55())).pick(context.Background())
	require.Equal(t, providers.Model{}, m, "a saved default of an unusable provider is not used")
	require.Nil(t, fb, "a fallback names a model that replaced the default; none did")
}

func TestStartChoiceDemoModelFirstWhenEnabled(t *testing.T) {
	demo := providers.Model{ID: "faux-1", Name: "faux-1", Provider: "faux", API: "faux"}
	m, fb := choiceOf(demo, "", "openai").pick(context.Background())
	require.Equal(t, demo, m)
	require.Nil(t, fb)
	// A usable saved default still wins over the demo model.
	m, _ = choiceOf(demo, idOf(providers.OpenAIGPT55()), "openai").pick(context.Background())
	require.Equal(t, providers.OpenAIGPT55(), m)
}
