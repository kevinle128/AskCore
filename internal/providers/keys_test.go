package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bound := BoundKey{Provider: ProviderTokenPlan, Secret: "pinned"}
	get := func(_ context.Context, provider string) (string, error) {
		if provider == ProviderOpenAI {
			return "oa", nil
		}
		return "", nil
	}
	t.Run("bound secret wins for matching provider", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveKey(ctx, ProviderTokenPlan, bound, get, "fallback")
		require.NoError(t, err)
		assert.Equal(t, "pinned", got)
	})
	t.Run("bound secret does not leak to another provider", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveKey(ctx, ProviderOpenAI, bound, get, "pinned")
		require.NoError(t, err)
		assert.Equal(t, "oa", got)
	})
	t.Run("empty get for another provider is empty", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveKey(ctx, ProviderOpenAI, bound, func(context.Context, string) (string, error) {
			return "", nil
		}, "pinned")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("nil get and empty bound keep fallback", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveKey(ctx, "faux", BoundKey{}, nil, "static")
		require.NoError(t, err)
		assert.Equal(t, "static", got)
	})
	t.Run("empty get and empty bound keep fallback", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveKey(ctx, "faux", BoundKey{}, func(context.Context, string) (string, error) {
			return "", nil
		}, "static")
		require.NoError(t, err)
		assert.Equal(t, "static", got)
	})
	t.Run("get error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		_, err := ResolveKey(ctx, ProviderOpenAI, bound, func(context.Context, string) (string, error) {
			return "", boom
		}, "fallback")
		require.ErrorIs(t, err, boom)
	})
}

func TestKeyEnvNames(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"ASK_ALIBABA_TOKEN_PLAN_API_KEY", "ALIBABA_TOKEN_PLAN_API_KEY"}, KeyEnvNames(ProviderTokenPlan))
	assert.Equal(t, []string{"ANTHROPIC_API_KEY"}, KeyEnvNames(ProviderAnthropic))
	assert.Equal(t, []string{"XAI_API_KEY"}, KeyEnvNames(ProviderXAI))
	assert.Equal(t, []string{"OPENAI_API_KEY"}, KeyEnvNames(ProviderOpenAI))
	assert.Empty(t, KeyEnvNames("other"))
	// A caller cannot change the shared table through the result.
	names := KeyEnvNames(ProviderOpenAI)
	names[0] = "CHANGED"
	assert.Equal(t, []string{"OPENAI_API_KEY"}, KeyEnvNames(ProviderOpenAI))
}

func TestKeyEnvNameReturnsSetVariable(t *testing.T) {
	t.Parallel()
	env := func(values map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := values[k]; return v, ok }
	}
	assert.Equal(t, "ALIBABA_TOKEN_PLAN_API_KEY", KeyEnvName(ProviderTokenPlan, env(map[string]string{"ALIBABA_TOKEN_PLAN_API_KEY": "k"})))
	assert.Equal(t, "ASK_ALIBABA_TOKEN_PLAN_API_KEY", KeyEnvName(ProviderTokenPlan, env(map[string]string{"ASK_ALIBABA_TOKEN_PLAN_API_KEY": "k", "ALIBABA_TOKEN_PLAN_API_KEY": "k"})))
	assert.Empty(t, KeyEnvName(ProviderOpenAI, env(map[string]string{"OPENAI_API_KEY": "   "})), "a blank value is not a key")
	assert.Empty(t, KeyEnvName(ProviderOpenAI, nil))
	assert.Empty(t, KeyEnvName("other", env(map[string]string{"OPENAI_API_KEY": "k"})))
}
