package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

func TestModelDenialDoesNotDiscardValidatedRotation(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	model := providers.OpenAIGPT55()
	_, rev, err := store.Read(ctx, model.Provider)
	if err != nil {
		t.Fatal(err)
	}
	old := settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "old-grant", ExpiresAt: time.Now().Add(-time.Minute)}}
	if _, err := store.Replace(ctx, model.Provider, rev, old); err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Methods: []Method{{Provider: model.Provider, ID: "oauth", API: model.API, Endpoint: model.BaseURL, Refresh: func(_ context.Context, c settings.Credential) (settings.Credential, error) {
		c.OAuth.AccessToken = "new"
		c.OAuth.RefreshToken = "new-grant"
		c.OAuth.ExpiresAt = time.Now().Add(time.Hour)
		return c, nil
	}, CheckAccess: func(_ context.Context, _ providers.Model, c settings.Credential) error {
		if c.Generation != 2 {
			t.Errorf("discovery did not use committed generation: %d", c.Generation)
		}
		return errors.New("model denied")
	}}}}
	if _, err := service.Resolve(ctx, model, ""); !errors.Is(err, ErrAccess) {
		t.Fatalf("expected access denial: %v", err)
	}
	saved, _, err := store.Read(ctx, model.Provider)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RefreshState != nil || saved.Generation != 2 || saved.OAuth.RefreshToken != "new-grant" {
		t.Fatal("model denial discarded validated rotation")
	}
}
