package auth

import (
	"context"
	"errors"
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
