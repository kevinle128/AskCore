package settings

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHostIDPersistsWithoutCredential(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	store, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.HostID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.HostID(context.Background())
	if err != nil || first != again || len(first) != 36 {
		t.Fatalf("host identity changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("host identity must not create credentials: %v", err)
	}
}
