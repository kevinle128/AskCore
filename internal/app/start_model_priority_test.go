package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"
)

// The start model follows one order: the model flag, then the scope (the
// saved default wins inside the scope), then the saved default, then the
// first usable model.
func TestStartModelPriority(t *testing.T) {
	ctx := context.Background()
	gpt, sol, sonnet := providers.OpenAIGPT55(), providers.OpenAIGPT56Sol(), providers.AnthropicSonnet46()
	eff := func(saved string, enabled ...string) settings.Effective {
		return settings.Effective{DefaultModel: saved, EnabledModels: enabled}
	}

	t.Run("the flag beats the scope and the saved default", func(t *testing.T) {
		got, err := choiceOf(providers.Model{}, idOf(sol), "openai", "anthropic").choose(ctx,
			ModelOptions{Pattern: "openai/" + gpt.ID + ":high"}, eff(idOf(sol), "anthropic/*"))
		require.NoError(t, err)
		assert.Equal(t, gpt, got.Model)
		assert.Equal(t, protocol.ThinkingHigh, got.Level)
		assert.Nil(t, got.Fallback)
		assert.Equal(t, []string{"anthropic/*"}, got.Scope, "the session keeps the scope for its cycle list")
	})
	t.Run("the scope beats the saved default", func(t *testing.T) {
		got, err := choiceOf(providers.Model{}, idOf(sol), "openai", "anthropic").choose(ctx,
			ModelOptions{}, eff(idOf(sol), "anthropic/"+sonnet.ID+":low", "openai/"+gpt.ID))
		require.NoError(t, err)
		assert.Equal(t, sonnet, got.Model, "the first scope entry; the saved default is not in the scope")
		assert.Equal(t, protocol.ThinkingLow, got.Level, "the level of the entry")
	})
	t.Run("the saved default inside the scope wins", func(t *testing.T) {
		got, err := choiceOf(providers.Model{}, idOf(sol), "openai", "anthropic").choose(ctx,
			ModelOptions{}, eff(idOf(sol), "anthropic/"+sonnet.ID, "openai/*"))
		require.NoError(t, err)
		assert.Equal(t, sol, got.Model)
	})
	t.Run("--models replaces enabledModels", func(t *testing.T) {
		got, err := choiceOf(providers.Model{}, "", "openai", "anthropic").choose(ctx,
			ModelOptions{Scope: []string{"openai/" + gpt.ID}}, eff("", "anthropic/*"))
		require.NoError(t, err)
		assert.Equal(t, gpt, got.Model)
		assert.Equal(t, []string{"openai/" + gpt.ID}, got.Scope)
	})
	t.Run("a scope row of an unusable provider is skipped", func(t *testing.T) {
		got, err := choiceOf(providers.Model{}, "", "anthropic").choose(ctx,
			ModelOptions{}, eff("", "openai/"+gpt.ID, "anthropic/"+sonnet.ID))
		require.NoError(t, err)
		assert.Equal(t, sonnet, got.Model)
	})
	t.Run("the saved default without a scope", func(t *testing.T) {
		got, err := choiceOf(providers.Model{}, idOf(sol), "openai", "anthropic").choose(ctx, ModelOptions{}, eff(idOf(sol)))
		require.NoError(t, err)
		assert.Equal(t, sol, got.Model)
		assert.Empty(t, got.Level, "no level was asked")
	})
	t.Run("the first usable model last", func(t *testing.T) {
		got, err := choiceOf(providers.Model{}, idOf(sol), "anthropic").choose(ctx, ModelOptions{}, eff(idOf(sol), "openai/*"))
		require.NoError(t, err)
		assert.Equal(t, sonnet, got.Model, "no usable scope row and an unusable saved default")
		require.NotNil(t, got.Fallback)
		assert.Equal(t, protocol.ACPFallbackNoCredential, got.Fallback.Reason)
		assert.Equal(t, "Warning: default model "+idOf(sol)+" is not usable (no credential); using "+idOf(sonnet)+".", got.FallbackWarning())
	})
}

func TestStartModelLevelRules(t *testing.T) {
	ctx := context.Background()
	gpt := providers.OpenAIGPT55()
	demo := providers.Model{Provider: "faux", ID: "faux-1", API: "faux", Name: "faux-1", ContextWindow: 1000, MaxTokens: 100}

	got, err := choiceOf(providers.Model{}, "", "openai").choose(ctx, ModelOptions{Pattern: "openai/" + gpt.ID + ":high", Thinking: protocol.ThinkingLow}, settings.Effective{})
	require.NoError(t, err)
	assert.Equal(t, protocol.ThinkingLow, got.Level, "--thinking wins over the level of the pattern")

	got, err = choiceOf(demo, "").choose(ctx, ModelOptions{Pattern: "faux/faux-1:high"}, settings.Effective{})
	require.NoError(t, err)
	assert.Equal(t, protocol.ThinkingOff, got.Level, "a model with no thinking drops the level")
	assert.Equal(t, &protocol.ACPLevelDropped{ModelID: "faux/faux-1@faux", Level: protocol.ThinkingHigh}, got.Dropped)

	got, err = choiceOf(demo, "").choose(ctx, ModelOptions{Pattern: "faux/faux-1:off"}, settings.Effective{})
	require.NoError(t, err)
	assert.Nil(t, got.Dropped, "off is never dropped")
}

func TestStartModelPatternErrors(t *testing.T) {
	ctx := context.Background()
	_, err := choiceOf(providers.Model{}, "", "openai").choose(ctx, ModelOptions{Pattern: "openai/gpt-5.5:hgih"}, settings.Effective{})
	require.ErrorIs(t, err, providers.ErrUnknownModel, "an unknown level is an unknown model on the command line")
	assert.Contains(t, err.Error(), "off, minimal, low, medium, high, xhigh, max")

	_, err = choiceOf(providers.Model{}, "", "openai").choose(ctx, ModelOptions{Pattern: "nope-x/none"}, settings.Effective{})
	require.ErrorIs(t, err, providers.ErrUnknownModel)

	// A Token Plan id is on two wires. Without a preferred wire it is
	// ambiguous; with one, the pattern resolves and keeps its level.
	pattern := providers.ProviderTokenPlan + "/" + providers.ModelDeepSeekFlash + ":high"
	_, err = choiceOf(providers.Model{}, "", providers.ProviderTokenPlan).choose(ctx, ModelOptions{Pattern: pattern}, settings.Effective{})
	require.True(t, errors.Is(err, providers.ErrAmbiguousModel), "%v", err)
	got, err := choiceOf(providers.Model{}, "", providers.ProviderTokenPlan).choose(ctx, ModelOptions{Pattern: pattern, PreferAPI: providers.APIAnthropicMessages}, settings.Effective{})
	require.NoError(t, err)
	assert.Equal(t, providers.APIAnthropicMessages, got.Model.API)
	assert.Equal(t, protocol.ThinkingHigh, got.Level)
}

// A problem of an enabledModels entry names its position and a fixed text,
// because the entry is file content; a --models entry is the text of the
// user and is shown.
func TestStartModelScopeDiagnostics(t *testing.T) {
	ctx := context.Background()
	got, err := choiceOf(providers.Model{}, "", "openai").choose(ctx, ModelOptions{}, settings.Effective{
		EnabledModels: []string{"openai/*", "private-x/none", "-", "zzz*"},
		Sources:       map[string]settings.Layer{settings.EnabledModelsKey: settings.LayerUser},
	})
	require.NoError(t, err)
	require.Len(t, got.Diagnostics, 3)
	assert.Equal(t, settings.Diagnostic{Layer: settings.LayerUser, Key: "enabledModels[1]", Text: "matches no model; it is ignored"}, got.Diagnostics[0])
	assert.Equal(t, "enabledModels[2]", got.Diagnostics[1].Key)
	assert.Equal(t, "names more than one model; it is ignored", got.Diagnostics[1].Text)
	assert.Equal(t, settings.Diagnostic{Layer: settings.LayerUser, Key: "enabledModels[3]", Text: "the glob matches no model"}, got.Diagnostics[2])
	for _, d := range got.Diagnostics {
		assert.NotContains(t, d.Text, "private-x")
	}

	got, err = choiceOf(providers.Model{}, "", "openai").choose(ctx, ModelOptions{Scope: []string{"private-x/none"}}, settings.Effective{})
	require.NoError(t, err)
	require.Len(t, got.Diagnostics, 1)
	assert.Equal(t, "--models", got.Diagnostics[0].Key)
	assert.Contains(t, got.Diagnostics[0].Text, "private-x/none")
}
