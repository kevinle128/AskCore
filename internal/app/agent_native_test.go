package app

import (
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"context"
	"errors"
	"testing"
)

func TestNativeAgentExplicitIdentity(t *testing.T) {
	cfg := NativeAgentConfig{Config: agent.Config{SessionID: "session-explicit", LoopConfig: agent.LoopConfig{Cwd: t.TempDir()}}}
	if _, err := NewNativeAgent(cfg); err == nil {
		t.Fatal("missing initial stream accepted")
	}
	cfg.Config.SessionID = ""
	if _, err := NewNativeAgent(cfg); err == nil {
		t.Fatal("missing session accepted")
	}
}
func TestNativeAuthMethodConstraintDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rev, err := store.Replace(ctx, providers.ProviderOpenAI, 0, settings.Credential{Method: "api-key", APIKey: "saved"})
	if err != nil {
		t.Fatal(err)
	}
	service := &auth.Service{Store: store}
	err = NativeAuthReady(ctx, service, providers.OpenAIGPT55(), "", "subscription")
	if !errors.Is(err, auth.ErrMethod) {
		t.Fatalf("mismatch: %v", err)
	}
	_, after, err := store.Read(ctx, providers.ProviderOpenAI)
	if err != nil || after != rev {
		t.Fatalf("changed credential: %v %d", err, after)
	}
	if err := NativeAuthReady(ctx, nil, providers.Model{Provider: "faux"}, "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestNativeAuthReadyEnvOnlyAPIKey(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := func(k string) (string, bool) {
		if k == "OPENAI_API_KEY" {
			return "env-secret", true
		}
		return "", false
	}
	service := &auth.Service{Store: store, Env: env}
	if err := NativeAuthReady(ctx, service, providers.OpenAIGPT55(), "", "api-key"); err != nil {
		t.Fatalf("env-only api-key rejected: %v", err)
	}
	if err := NativeAuthReady(ctx, service, providers.OpenAIGPT55(), "", "subscription"); !errors.Is(err, auth.ErrMethod) {
		t.Fatalf("subscription accepted for env credential: %v", err)
	}
}

func TestNativeAgentRejectsBadCwd(t *testing.T) {
	stream := func(context.Context, providers.Model, providers.TranscriptRequest, providers.StreamOptions) *providers.Stream {
		return nil
	}
	for _, cwd := range []string{"relative/dir", t.TempDir() + "/missing"} {
		cfg := NativeAgentConfig{InitialStream: stream, Config: agent.Config{SessionID: "s", LoopConfig: agent.LoopConfig{Cwd: cwd}}}
		if _, err := NewNativeAgent(cfg); err == nil {
			t.Fatalf("cwd %q accepted", cwd)
		}
	}
}
