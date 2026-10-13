package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

// failTransport fails the test on any request, so a test can prove that a code
// path stays offline.
func failTransport(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected HTTP request to %s", r.URL.Host)
		return nil, http.ErrHandlerTimeout
	})}
}

// testService builds the same method set as the runtime composition: the native
// OAuth methods and one API-key method for each provider API.
func testService(t *testing.T, client *http.Client, env map[string]string) (*Service, *settings.AuthStore) {
	t.Helper()
	return testServiceWith(t, NativeOptions{HTTPClient: client}, env)
}

func testServiceWith(t *testing.T, o NativeOptions, env map[string]string) (*Service, *settings.AuthStore) {
	t.Helper()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	methods := NativeMethods(o)
	for _, row := range []struct {
		provider string
		api      providers.API
		endpoint string
	}{
		{providers.ProviderTokenPlan, providers.APIAnthropicMessages, providers.TokenPlanMessagesURL},
		{providers.ProviderTokenPlan, providers.APIOpenAICompletions, providers.TokenPlanCompletionsURL},
		{providers.ProviderAnthropic, providers.APIAnthropicMessages, providers.AnthropicURL},
		{providers.ProviderOpenAI, providers.APIOpenAIResponses, providers.OpenAIURL},
		{providers.ProviderXAI, providers.APIOpenAIResponses, providers.XAIURL},
	} {
		methods = append(methods, Method{Provider: row.provider, ID: "api-key", API: row.api, Profile: "api-key", Endpoint: row.endpoint})
	}
	return &Service{Store: store, Methods: methods, Env: func(name string) (string, bool) { v, ok := env[name]; return v, ok }}, store
}

func stateOf(t *testing.T, rows []OptionState, provider, method string) OptionState {
	t.Helper()
	for _, r := range rows {
		if r.Provider == provider && r.Method == method {
			return r
		}
	}
	t.Fatalf("no row for %s/%s", provider, method)
	return OptionState{}
}

func TestStatusRowsOneForEachLoginOption(t *testing.T) {
	s, _ := testService(t, failTransport(t), nil)
	rows, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{
		{"alibaba-token-plan", "api-key"},
		{"anthropic", "anthropic-oauth"},
		{"anthropic", "api-key"},
		{"openai", "openai-chatgpt"},
		{"openai", "api-key"},
		{"xai", "xai-oauth"},
		{"xai", "api-key"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows: %d", len(rows))
	}
	for i, w := range want {
		if rows[i].Provider != w[0] || rows[i].Method != w[1] || rows[i].Name == "" || rows[i].State != StateNone {
			t.Fatalf("row %d: %+v", i, rows[i])
		}
	}
	if rows[0].Name != "Alibaba Token Plan API key" || rows[1].Name != "Claude Pro/Max sign-in" || rows[3].Name != "ChatGPT Plus/Pro sign-in" || rows[5].Name != "xAI Grok sign-in" {
		t.Fatalf("names: %+v", rows)
	}
	if got := LoginOptions(); len(got) != 7 {
		t.Fatalf("LoginOptions: %d", len(got))
	}
}

func TestStatusEnvOnlyWhenNoSavedRecord(t *testing.T) {
	ctx := context.Background()
	s, store := testService(t, failTransport(t), map[string]string{"ANTHROPIC_API_KEY": "env-key"})
	rows, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	api := stateOf(t, rows, "anthropic", "api-key")
	if api.State != StateEnv || api.EnvVar != "ANTHROPIC_API_KEY" {
		t.Fatalf("env row: %+v", api)
	}
	if oauth := stateOf(t, rows, "anthropic", "anthropic-oauth"); oauth.State != StateNone {
		t.Fatalf("env key marked the account row: %+v", oauth)
	}
	_, rev, _ := store.Read(ctx, "anthropic")
	if _, err = store.Replace(ctx, "anthropic", rev, settings.Credential{Method: "api-key", APIKey: "saved-key"}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	api = stateOf(t, rows, "anthropic", "api-key")
	if api.State != StateAPIKey || api.EnvVar != "" {
		t.Fatalf("saved key must win over env: %+v", api)
	}
	if got := stateOf(t, rows, "openai", "api-key"); got.State != StateNone {
		t.Fatalf("other provider: %+v", got)
	}
}

func TestStatusNeverRefreshes(t *testing.T) {
	ctx := context.Background()
	s, store := testService(t, failTransport(t), nil)
	_, rev, _ := store.Read(ctx, "xai")
	expired := settings.Credential{Method: "xai-oauth", OAuth: &settings.OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Hour)}}
	if _, err := store.Replace(ctx, "xai", rev, expired); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, rows, "xai", "xai-oauth"); got.State != StateSignedIn {
		t.Fatalf("expired token: %+v", got)
	}
	if got := stateOf(t, rows, "xai", "api-key"); got.State != StateNone {
		t.Fatalf("api-key row of an OAuth provider: %+v", got)
	}
}
