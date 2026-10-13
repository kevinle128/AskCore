package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDefaultModelStoreMissingFileMeansNoDefault(t *testing.T) {
	s, err := NewSettingsStore(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.DefaultModel(context.Background())
	if err != nil || got != "" {
		t.Fatalf("default of a missing file: %q %v", got, err)
	}
}

func TestDefaultModelStorePrivateAndLocked(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	ctx := context.Background()
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// One store for each writer: the file lock, not a process mutex, must serialize them.
			s, err := NewSettingsStore(home)
			if err != nil {
				errs <- err
				return
			}
			errs <- s.SetDefaultModel(ctx, "openai/gpt-5.5@openai-responses")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}
	for _, name := range []string{"settings.json", "settings.json.lock"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %v", name, info.Mode().Perm())
		}
	}
	info, err := os.Stat(home)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("home mode: %v %v", info, err)
	}
	s, _ := NewSettingsStore(home)
	got, err := s.DefaultModel(ctx)
	if err != nil || got != "openai/gpt-5.5@openai-responses" {
		t.Fatalf("read back: %q %v", got, err)
	}
	left, _ := filepath.Glob(filepath.Join(home, ".auth-*"))
	if len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestDefaultModelStoreKeepsUnknownFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte(`{"theme":"dark","nested":{"a":[1,2]},"defaultModel":"old/model@x"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSettingsStore(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got, err := s.DefaultModel(ctx); err != nil || got != "old/model@x" {
		t.Fatalf("read: %q %v", got, err)
	}
	if err = s.SetDefaultModel(ctx, "new/model@y"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{`"theme":"dark"`, `"nested":{"a":[1,2]}`, `"defaultModel":"new/model@y"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("field lost, want %s in %s", want, text)
		}
	}
}

func TestDefaultModelStoreRefusesUnsafeFileAndBadIDs(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := NewSettingsStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DefaultModel(context.Background()); !errors.Is(err, ErrUnsafeStore) {
		t.Fatalf("group-readable file accepted: %v", err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDefaultModel(context.Background(), "a/b@c"); !errors.Is(err, ErrUnsafeStore) {
		t.Fatalf("invalid JSON overwritten: %v", err)
	}
	for _, id := range []string{"", " ", "a\nb", strings.Repeat("x", 300)} {
		if err = s.SetDefaultModel(context.Background(), id); err == nil {
			t.Fatalf("model id %q accepted", id)
		}
	}
}
