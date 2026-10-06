package auth

import (
	"AskCore/internal/settings"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLoginPrivateKeyAndRevision(t *testing.T) {
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: store, Methods: append(NativeMethods(NativeOptions{}), Method{Provider: "openai", ID: "api-key"})}
	if err = s.Login(context.Background(), "openai", "api-key", LoginRequest{Input: func(context.Context) (string, error) { return "private-key", nil }}); err != nil {
		t.Fatal(err)
	}
	c, _, err := store.Read(context.Background(), "openai")
	if err != nil || c.APIKey != "private-key" {
		t.Fatalf("key save: %v", err)
	}
	err = s.Login(context.Background(), "openai", "api-key", LoginRequest{Input: func(ctx context.Context) (string, error) {
		_, rev, e := store.Read(ctx, "openai")
		if e != nil {
			return "", e
		}
		_, e = store.Logout(ctx, "openai", rev)
		return "next-key", e
	}})
	if !errors.Is(err, settings.ErrConflict) {
		t.Fatalf("revision: %v", err)
	}
	err = s.Login(context.Background(), "openai", "api-key", LoginRequest{Input: func(context.Context) (string, error) { return strings.Repeat("x", 16385), nil }})
	if err == nil {
		t.Fatal("oversize input accepted")
	}
}

func TestCallbackInputBinding(t *testing.T) {
	for _, input := range []string{"code", "http://wrong/callback?code=x&state=s", "http://localhost:53692/callback?code=x&state=bad", strings.Repeat("x", 16385)} {
		if _, err := parseCallback(input, "http://localhost:53692/callback", "s", false, false); err == nil {
			t.Fatal("invalid callback accepted")
		}
	}
	if got, err := parseCallback("code#s", "https://platform.claude.com/oauth/code/callback", "s", true, false); err != nil || got.code != "code" {
		t.Fatalf("copy: %v", err)
	}
}
