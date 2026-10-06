package app

import (
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

// NewNativeAuth composes local credentials and native provider protocols.
// HTTP boundaries remain separate from inference and request capture.
func NewNativeAuth(home string, env func(string) (string, bool), options auth.NativeOptions) (*auth.Service, error) {
	store, err := settings.NewAuthStore(home)
	if err != nil {
		return nil, err
	}
	methods := auth.NativeMethods(options)
	for _, row := range []struct {
		provider string
		api      providers.API
		endpoint string
	}{
		{providers.ProviderTokenPlan, providers.APIAnthropicMessages, providers.TokenPlanMessagesURL},
		{providers.ProviderTokenPlan, providers.APIOpenAICompletions, providers.TokenPlanCompletionsURL},
		{"anthropic", providers.APIAnthropicMessages, "https://api.anthropic.com"},
		{providers.ProviderOpenAI, providers.APIOpenAIResponses, providers.OpenAIURL},
		{"xai", providers.APIOpenAIResponses, "https://api.x.ai/v1"},
	} {
		methods = append(methods, auth.Method{Provider: row.provider, ID: "api-key", API: row.api, Profile: "api-key", Endpoint: row.endpoint})
	}
	return &auth.Service{Store: store, Methods: methods, Env: env, Now: options.Now}, nil
}
