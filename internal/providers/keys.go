package providers

import (
	"context"
	"strings"
)

// BoundKey is the --api-key pin. ResolveKey applies Secret only when
// Provider matches, so the CLI key never goes to another host.
type BoundKey struct {
	Provider string
	Secret   string
}

// ResolveKey returns the secret for provider.
//
// The bound secret wins when Provider matches and Secret is non-empty.
// Otherwise get runs; a non-empty result wins and an empty result stays
// empty when BoundKey is pinned to another provider. A nil get and an
// empty BoundKey return fallback so existing Options.APIKey tests pass.
func ResolveKey(ctx context.Context, provider string, bound BoundKey, get func(context.Context, string) (string, error), fallback string) (string, error) {
	if bound.Secret != "" && bound.Provider == provider {
		return bound.Secret, nil
	}
	if get != nil {
		key, err := get(ctx, provider)
		if err != nil {
			return "", err
		}
		if key != "" {
			return key, nil
		}
	}
	if bound.Secret != "" {
		return "", nil
	}
	return fallback, nil
}

// LookupKey is the only env read for secrets.
// alibaba-token-plan: ASK_ALIBABA_TOKEN_PLAN_API_KEY then ALIBABA_TOKEN_PLAN_API_KEY.
// openai: OPENAI_API_KEY.
// Any other provider: empty. Never reads OPENAI_BASE_URL or other OPENAI_*.
func LookupKey(provider string, env func(string) (string, bool)) string {
	if env == nil {
		return ""
	}
	var names []string
	switch provider {
	case ProviderTokenPlan:
		names = []string{"ASK_ALIBABA_TOKEN_PLAN_API_KEY", "ALIBABA_TOKEN_PLAN_API_KEY"}
	case ProviderAnthropic:
		names = []string{"ANTHROPIC_API_KEY"}
	case ProviderXAI:
		names = []string{"XAI_API_KEY"}
	case ProviderOpenAI:
		names = []string{"OPENAI_API_KEY"}
	default:
		return ""
	}
	for _, name := range names {
		v, ok := env(name)
		if !ok {
			continue
		}
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
