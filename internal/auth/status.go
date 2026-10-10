package auth

import (
	"context"
	"os"

	"AskCore/internal/providers"
)

// Credential states of one login option.
const (
	StateSignedIn = "signed_in" // a saved OAuth credential of this method
	StateAPIKey   = "api_key"   // a saved API key
	StateEnv      = "env"       // no saved credential, and an environment key applies
	StateNone     = "none"
)

// LoginOption is one way to sign in: a provider, a method and a display name.
type LoginOption struct{ Provider, Method, Name string }

// OptionState is a login option with the state of its credential.
type OptionState struct {
	LoginOption
	State  string
	EnvVar string // the variable that makes the provider usable; set only when State is StateEnv
}

// LoginOptions returns the login options in display order: by provider id,
// with the sign-in row before the API-key row of each provider.
func LoginOptions() []LoginOption {
	return []LoginOption{
		{providers.ProviderTokenPlan, "api-key", "Alibaba Token Plan API key"},
		{providers.ProviderAnthropic, "anthropic-oauth", "Claude Pro/Max sign-in"},
		{providers.ProviderAnthropic, "api-key", "Anthropic API key"},
		{providers.ProviderOpenAI, "openai-chatgpt", "ChatGPT Plus/Pro sign-in"},
		{providers.ProviderOpenAI, "api-key", "OpenAI API key"},
		{providers.ProviderXAI, "xai-oauth", "xAI Grok sign-in"},
		{providers.ProviderXAI, "api-key", "xAI API key"},
	}
}

// Status reports the credential state of every login option. It reads the
// local store once for each provider and the environment, and it never
// refreshes a token or sends a request, so an expired sign-in still shows as
// signed in. A saved record wins over an environment key, as in Resolve.
func (s *Service) Status(ctx context.Context) ([]OptionState, error) {
	if s == nil || s.Store == nil {
		return nil, ErrNoCredential
	}
	env := s.Env
	if env == nil {
		env = os.LookupEnv
	}
	options := LoginOptions()
	saved := map[string]string{} // provider -> method of the saved record
	for _, o := range options {
		if _, done := saved[o.Provider]; done {
			continue
		}
		c, _, err := s.Store.Read(ctx, o.Provider)
		if err != nil {
			return nil, err
		}
		saved[o.Provider] = c.Method
	}
	rows := make([]OptionState, 0, len(options))
	for _, o := range options {
		row := OptionState{LoginOption: o, State: StateNone}
		switch method := saved[o.Provider]; {
		case method == o.Method && method == "api-key":
			row.State = StateAPIKey
		case method == o.Method:
			row.State = StateSignedIn
		case method == "" && o.Method == "api-key":
			if name := providers.KeyEnvName(o.Provider, env); name != "" {
				row.State, row.EnvVar = StateEnv, name
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
