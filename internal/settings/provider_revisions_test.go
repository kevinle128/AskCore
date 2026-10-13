package settings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderRevisionAllowsUnrelatedWrite(t *testing.T) {
	s, err := NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, pending, err := s.ReadProvider(ctx, "xai")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Replace(ctx, "openai", 0, Credential{Method: "api-key", APIKey: "fake-openai-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceProvider(ctx, "xai", pending, Credential{Method: "api-key", APIKey: "fake-xai-key"}); err != nil {
		t.Fatalf("unrelated write stopped the replacement: %v", err)
	}
	if _, err = s.Replace(ctx, "xai", pending, Credential{Method: "api-key", APIKey: "stale-key"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("the global revision contract changed: %v", err)
	}
}

func TestProviderRevisionFencesAbsentLogout(t *testing.T) {
	s, err := NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, pending, err := s.ReadProvider(ctx, "xai")
	if err != nil {
		t.Fatal(err)
	}
	global, err := s.LogoutProvider(ctx, "xai", pending)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Replace(ctx, "openai", global, Credential{Method: "api-key", APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceProvider(ctx, "xai", pending, Credential{Method: "api-key", APIKey: "stale-key"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("absent-provider logout lost its fence: %v", err)
	}
	c, _, err := s.Read(ctx, "xai")
	if err != nil || c.Method != "" {
		t.Fatalf("removed credential returned: %v", err)
	}
}

func TestProviderRevisionFencesOlderWriter(t *testing.T) {
	home := t.TempDir()
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = s.Replace(ctx, "openai", 0, Credential{Method: "api-key", APIKey: "fake-key"}); err != nil {
		t.Fatal(err)
	}
	_, pending, err := s.ReadProvider(ctx, "xai")
	if err != nil {
		t.Fatal(err)
	}
	// An older writer keeps unknown fields and advances only the global revision.
	path := filepath.Join(home, "auth.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["revision"] = json.RawMessage("2")
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceProvider(ctx, "xai", pending, Credential{Method: "api-key", APIKey: "stale-key"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("older writer did not fence the pending sign-in: %v", err)
	}
	_, fresh, err := s.ReadProvider(ctx, "xai")
	if err != nil || fresh != 2 {
		t.Fatalf("older writer fallback: %d %v", fresh, err)
	}
	if _, err = s.ReplaceProvider(ctx, "xai", fresh, Credential{Method: "api-key", APIKey: "fresh-key"}); err != nil {
		t.Fatal(err)
	}
	_, openai, err := s.ReadProvider(ctx, "openai")
	if err != nil || openai != 2 {
		t.Fatalf("older metadata was not reset: %d %v", openai, err)
	}
}

func TestProviderRevisionLoadsOlderStore(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"schemaVersion":1,"revision":4,"providers":{"openai":{"method":"api-key","apiKey":"fake-key"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, pending, err := s.ReadProvider(ctx, "xai")
	if err != nil || pending != 4 {
		t.Fatalf("legacy baseline: %d %v", pending, err)
	}
	if _, err = s.Replace(ctx, "openai", 4, Credential{Method: "api-key", APIKey: "changed-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceProvider(ctx, "xai", pending, Credential{Method: "api-key", APIKey: "fake-xai-key"}); err != nil {
		t.Fatalf("legacy baseline did not survive an unrelated write: %v", err)
	}
}

func TestProviderRevisionRefusesInvalidMetadata(t *testing.T) {
	for _, metadata := range []string{
		`null`,
		`{"storeRevision":2,"baseline":0,"providers":{}}`,
		`{"storeRevision":1,"baseline":2,"providers":{}}`,
		`{"storeRevision":1,"baseline":0,"providers":{"xai":2}}`,
		`{"storeRevision":1,"baseline":0,"providers":{"bad/provider":1}}`,
	} {
		t.Run(metadata, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "auth.json")
			if err := os.WriteFile(path, []byte(`{"revision":1,"providers":{},"providerRevisions":`+metadata+`}`), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := NewAuthStore(home)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.ReadProvider(context.Background(), "xai"); !errors.Is(err, ErrUnsafeStore) {
				t.Fatalf("invalid metadata accepted: %v", err)
			}
		})
	}
}
