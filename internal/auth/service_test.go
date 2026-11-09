package auth

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

func TestResolvePrecedenceAndNoOAuthFallback(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	model := providers.OpenAIGPT55()
	s := &Service{Store: store, Env: func(name string) (string, bool) {
		if name == "OPENAI_API_KEY" {
			return "ambient", true
		}
		return "", false
	}, Methods: []Method{{Provider: model.Provider, ID: "oauth", API: model.API, Endpoint: model.BaseURL, Profile: "subscription", Lead: 8 * time.Minute}}}
	got, err := s.Resolve(ctx, model, "")
	if err != nil || got.AccessToken != "ambient" {
		t.Fatalf("ambient: %v, %v", got, err)
	}
	_, rev, err := store.Read(ctx, model.Provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Replace(ctx, model.Provider, rev, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "saved", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.Resolve(ctx, model, "")
	if err != nil || got.AccessToken != "saved" || got.Source != "saved" {
		t.Fatalf("saved: %v, %v", got, err)
	}
	got, err = s.Resolve(ctx, model, "explicit")
	if err != nil || got.AccessToken != "explicit" {
		t.Fatalf("explicit: %v, %v", got, err)
	}
	_, rev, err = store.Read(ctx, model.Provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Replace(ctx, model.Provider, rev, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "expired", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Resolve(ctx, model, "")
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("want recovery, got %v", err)
	}
}

const localURL = "http://127.0.0.1:11434/v1"

func localModel() providers.Model {
	return providers.Model{Provider: "local", ID: "qwen3-coder:30b", API: providers.APIOpenAICompletions, BaseURL: localURL, Compat: providers.CompletionsCompat{}}
}

func newOrderService(t *testing.T, vars map[string]string) *Service {
	t.Helper()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: store, Env: mapEnv(vars)}
}

// orderCase is one input of the source order: each field set adds a source.
type orderCase struct {
	name                  string
	override, saved, conf string
	ambient               string
	wantKey, wantSource   string
	wantName              string
	wantErr               error
}

func orderCases() []orderCase {
	return []orderCase{
		{name: "all four", override: "flag-key", saved: "saved-key", conf: "conf-key", ambient: "env-key", wantKey: "flag-key", wantSource: "override", wantName: "override"},
		{name: "no override", saved: "saved-key", conf: "conf-key", ambient: "env-key", wantKey: "saved-key", wantSource: "saved", wantName: "saved:api-key"},
		{name: "config and env", conf: "conf-key", ambient: "env-key", wantKey: "conf-key", wantSource: "config", wantName: "config:literal"},
		{name: "config from a variable", conf: "$CONF_VAR", ambient: "env-key", wantKey: "from-var", wantSource: "config", wantName: "config:$CONF_VAR"},
		{name: "env only", ambient: "env-key", wantKey: "env-key", wantSource: "env", wantName: "env:OPENAI_API_KEY"},
		{name: "blank config key", conf: "  ", ambient: "env-key", wantErr: ErrNoCredential},
		{name: "none", wantErr: ErrNoCredential},
	}
}

func (tc orderCase) setup(t *testing.T) (*Service, ResolveRequest) {
	t.Helper()
	vars := map[string]string{"CONF_VAR": "from-var"}
	if tc.ambient != "" {
		vars["OPENAI_API_KEY"] = tc.ambient
	}
	s := newOrderService(t, vars)
	m := providers.OpenAIGPT55()
	if tc.saved != "" {
		saveCredential(t, s, m.Provider, settings.Credential{Method: "api-key", APIKey: tc.saved})
	}
	req := ResolveRequest{Model: m, Override: tc.override}
	if tc.conf != "" {
		req.Config = &ConfigKey{Value: tc.conf, Endpoint: m.BaseURL}
	}
	return s, req
}

func TestResolveOrderOverrideSavedConfigEnv(t *testing.T) {
	for _, tc := range orderCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, req := tc.setup(t)
			got, err := s.ResolveWith(context.Background(), req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil || got.AccessToken != tc.wantKey || got.Source != tc.wantSource || got.Endpoint != req.Model.BaseURL {
				t.Fatalf("got %v (key match %v), %v", got, got.AccessToken == tc.wantKey, err)
			}
		})
	}
}

func TestKeySourceMatchesResolveWith(t *testing.T) {
	for _, tc := range orderCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, req := tc.setup(t)
			snap, resolveErr := s.ResolveWith(context.Background(), req)
			src, err := s.KeySource(context.Background(), req)
			if !errors.Is(err, tc.wantErr) || (resolveErr == nil) != (err == nil) {
				t.Fatalf("key source error %v, resolve error %v", err, resolveErr)
			}
			if err != nil {
				return
			}
			if string(src.Kind) != snap.Source || src.Name != tc.wantName {
				t.Fatalf("key source %+v, resolve source %q", src, snap.Source)
			}
			if strings.Contains(src.Name, tc.wantKey) {
				t.Fatalf("source name holds the key: %q", src.Name)
			}
		})
	}
}

func TestKeySourceNeverRunsCommand(t *testing.T) {
	s := newOrderService(t, nil)
	var calls atomic.Int32
	s.RunCommand = func(context.Context, string) (string, error) {
		calls.Add(1)
		return "cmd-key", nil
	}
	req := ResolveRequest{Model: localModel(), Config: &ConfigKey{Value: "!op read key", Endpoint: localURL}}
	src, err := s.KeySource(context.Background(), req)
	if err != nil || src != (Source{Kind: SourceConfig, Name: "config:command"}) || calls.Load() != 0 {
		t.Fatalf("dry run: %+v, %v, runs %d", src, err, calls.Load())
	}
	got, err := s.ResolveWith(context.Background(), req)
	if err != nil || got.AccessToken != "cmd-key" || calls.Load() != 1 {
		t.Fatalf("resolve: %v, %v, runs %d", got, err, calls.Load())
	}
	unset := ResolveRequest{Model: localModel(), Config: &ConfigKey{Value: "$NOT_SET_KEY", Endpoint: localURL}}
	_, dryErr := s.KeySource(context.Background(), unset)
	_, err = s.ResolveWith(context.Background(), unset)
	if !errors.Is(dryErr, ErrValue) || !errors.Is(err, ErrValue) || !strings.Contains(dryErr.Error(), "NOT_SET_KEY") {
		t.Fatalf("unset variable: dry %v, resolve %v", dryErr, err)
	}
}

func TestKeySourceDoesNotRefreshSavedOAuth(t *testing.T) {
	s := newOrderService(t, nil)
	m := providers.OpenAIGPT55()
	var refreshes atomic.Int32
	s.Methods = []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Profile: "subscription", Lead: time.Hour,
		Refresh: func(context.Context, settings.Credential) (settings.Credential, error) {
			refreshes.Add(1)
			return settings.Credential{}, errors.New("no refresh in this test")
		}}}
	saveCredential(t, s, m.Provider, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "saved", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Minute)}})
	src, err := s.KeySource(context.Background(), ResolveRequest{Model: m})
	if err != nil || src.Name != "saved:oauth" || refreshes.Load() != 0 {
		t.Fatalf("dry run: %+v, %v, refreshes %d", src, err, refreshes.Load())
	}
}

// A saved record that waits for recovery blocks the environment key. The dry
// run names the record with ErrRecovery, and the local check tells that the
// record waits, with no refresh and no change to the record.
func TestKeySourceNamesSavedRecordThatWaitsForRecovery(t *testing.T) {
	ctx := context.Background()
	s := newOrderService(t, map[string]string{"OPENAI_API_KEY": "env-key"})
	m := providers.OpenAIGPT55()
	var refreshes atomic.Int32
	s.Methods = []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Profile: "subscription", Lead: time.Hour,
		Refresh: func(context.Context, settings.Credential) (settings.Credential, error) {
			refreshes.Add(1)
			return settings.Credential{}, errors.New("no refresh in this test")
		}}}
	if s.WaitsForRecovery(ctx, m.Provider) {
		t.Fatal("no record: nothing waits")
	}
	saveCredential(t, s, m.Provider, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "saved", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}})
	if s.WaitsForRecovery(ctx, m.Provider) {
		t.Fatal("a record with no fence does not wait")
	}
	saved, _, err := s.Store.Read(ctx, m.Provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.FenceRefresh(ctx, m.Provider, saved.Generation, "attempt"); err != nil {
		t.Fatal(err)
	}
	if !s.WaitsForRecovery(ctx, m.Provider) {
		t.Fatal("a fenced record waits for recovery")
	}
	src, err := s.KeySource(ctx, ResolveRequest{Model: m})
	if !errors.Is(err, ErrRecovery) || src.Name != "saved:oauth" || src.Kind != SourceSaved {
		t.Fatalf("dry run: %+v, %v", src, err)
	}
	if _, err := s.ResolveWith(ctx, ResolveRequest{Model: m}); !errors.Is(err, ErrRecovery) {
		t.Fatalf("resolve: %v", err)
	}
	if refreshes.Load() != 0 {
		t.Fatalf("refreshes %d", refreshes.Load())
	}
	after, _, err := s.Store.Read(ctx, m.Provider)
	if err != nil || after.RefreshState == nil {
		t.Fatalf("the fence stays: %+v, %v", after.RefreshState, err)
	}
}

func TestResolveConfigKeyForFileDefinedProvider(t *testing.T) {
	s := newOrderService(t, nil)
	got, err := s.ResolveWith(context.Background(), ResolveRequest{Model: localModel(), Config: &ConfigKey{Value: "ollama", Endpoint: localURL}})
	if err != nil || got.AccessToken != "ollama" || got.Source != "config" || got.Method != "api-key" || got.Profile != "api-key" || !got.ValidFor("local", localURL) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := s.Resolve(context.Background(), localModel(), ""); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("no config key: %v", err)
	}
}

func TestResolveOverrideKeyOnlyForUserFileEndpoint(t *testing.T) {
	s := newOrderService(t, nil)
	got, err := s.ResolveWith(context.Background(), ResolveRequest{Model: localModel(), Override: "flag-key", Config: &ConfigKey{Value: "ollama", Endpoint: localURL}})
	if err != nil || got.AccessToken != "flag-key" || got.Source != "override" {
		t.Fatalf("file provider: %v, %v", got, err)
	}
	if src, err := s.KeySource(context.Background(), ResolveRequest{Model: localModel(), Override: "flag-key", Config: &ConfigKey{Value: "ollama", Endpoint: localURL}}); err != nil || src != (Source{Kind: SourceOverride, Name: "override"}) {
		t.Fatalf("key source: %+v, %v", src, err)
	}
	if _, err := s.ResolveWith(context.Background(), ResolveRequest{Model: localModel(), Override: "flag-key"}); !errors.Is(err, ErrMethod) {
		t.Fatalf("no config key: %v", err)
	}
	other := ResolveRequest{Model: localModel(), Override: "flag-key", Config: &ConfigKey{Value: "ollama", Endpoint: "http://other.example/v1"}}
	if _, err := s.ResolveWith(context.Background(), other); !errors.Is(err, ErrMethod) {
		t.Fatalf("config key for another endpoint: %v", err)
	}
}

func TestResolveSavedCredentialSkippedOnOtherEndpoint(t *testing.T) {
	s := newOrderService(t, nil)
	m := providers.OpenAIGPT55()
	m.BaseURL = "https://proxy.example/v1"
	saveCredential(t, s, m.Provider, settings.Credential{Method: "api-key", APIKey: "saved-openai"})
	withConfig := ResolveRequest{Model: m, Config: &ConfigKey{Value: "proxy-key", Endpoint: m.BaseURL}}
	got, err := s.ResolveWith(context.Background(), withConfig)
	if err != nil || got.AccessToken != "proxy-key" || got.Source != "config" {
		t.Fatalf("config key: %v, %v", got, err)
	}
	if src, err := s.KeySource(context.Background(), withConfig); err != nil || src.Name != "config:literal" {
		t.Fatalf("key source: %+v, %v", src, err)
	}
	if _, err := s.Resolve(context.Background(), m, ""); !errors.Is(err, ErrMethod) {
		t.Fatalf("no config key: %v", err)
	}
	if _, err := s.KeySource(context.Background(), ResolveRequest{Model: m}); !errors.Is(err, ErrMethod) {
		t.Fatalf("key source with no config key: %v", err)
	}
}

func TestResolveConfigKeyNeedsSameEndpoint(t *testing.T) {
	s := newOrderService(t, map[string]string{"OPENAI_API_KEY": "env-key"})
	other := ResolveRequest{Model: localModel(), Config: &ConfigKey{Value: "ollama", Endpoint: "http://127.0.0.1:9999/v1"}}
	if _, err := s.ResolveWith(context.Background(), other); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("file provider, other endpoint: %v", err)
	}
	m := providers.OpenAIGPT55()
	got, err := s.ResolveWith(context.Background(), ResolveRequest{Model: m, Config: &ConfigKey{Value: "conf", Endpoint: "https://proxy.example/v1"}})
	if err != nil || got.AccessToken != "env-key" || got.Source != "env" {
		t.Fatalf("built-in row keeps the env key: %v, %v", got, err)
	}
}
