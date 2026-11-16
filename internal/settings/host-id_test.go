package settings

import (
	"context"
	"errors"
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

func TestHostIDReaderStoreWritesNothing(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ask")
	r, err := NewAuthStoreReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.HostID(context.Background()); !errors.Is(err, ErrReadOnlyStore) {
		t.Fatalf("host id on a reader: %v", err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the reader created the home: %v", err)
	}
}
