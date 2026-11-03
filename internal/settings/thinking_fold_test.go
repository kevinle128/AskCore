package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// thinkingFoldOf writes body as settings.json with mode perm in a new home and
// reads the thinkingFold value back. A nil body writes no file.
func thinkingFoldOf(t *testing.T, body []byte, perm os.FileMode) (ThinkingFold, error) {
	t.Helper()
	home := t.TempDir()
	if body != nil {
		if err := os.WriteFile(filepath.Join(home, settingsFile), body, perm); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(home, settingsFile), perm); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewSettingsStore(home)
	if err != nil {
		t.Fatal(err)
	}
	return s.ThinkingFold(context.Background())
}

func TestThinkingFoldGivesTheSavedWord(t *testing.T) {
	for _, want := range []ThinkingFold{ThinkingFoldExpanded, ThinkingFoldTruncated, ThinkingFoldCollapsed} {
		got, err := thinkingFoldOf(t, []byte(`{"thinkingFold":"`+string(want)+`"}`), 0600)
		if err != nil || got != want {
			t.Fatalf("saved %q: got %q %v", want, got, err)
		}
	}
}

func TestThinkingFoldIsExpandedWithoutAValidValue(t *testing.T) {
	for name, body := range map[string][]byte{
		"no file":       nil,
		"no key":        []byte(`{"defaultModel":"a/b@c"}`),
		"unknown word":  []byte(`{"thinkingFold":"sideways"}`),
		"other case":    []byte(`{"thinkingFold":"Collapsed"}`),
		"not a string":  []byte(`{"thinkingFold":2}`),
		"null":          []byte(`{"thinkingFold":null}`),
		"empty string":  []byte(`{"thinkingFold":""}`),
		"space padding": []byte(`{"thinkingFold":" collapsed"}`),
	} {
		got, err := thinkingFoldOf(t, body, 0600)
		if err != nil || got != ThinkingFoldExpanded {
			t.Fatalf("%s: got %q %v", name, got, err)
		}
	}
}

func TestThinkingFoldIsExpandedWhenTheFileCannotBeRead(t *testing.T) {
	for name, tc := range map[string]struct {
		body []byte
		perm os.FileMode
	}{
		"invalid JSON":      {[]byte(`{"thinkingFold":`), 0600},
		"not an object":     {[]byte(`["collapsed"]`), 0600},
		"group-readable":    {[]byte(`{"thinkingFold":"collapsed"}`), 0644},
		"owner cannot read": {[]byte(`{"thinkingFold":"collapsed"}`), 0200},
	} {
		got, err := thinkingFoldOf(t, tc.body, tc.perm)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if got != ThinkingFoldExpanded {
			t.Fatalf("%s: got %q, want the default", name, got)
		}
	}
}

func TestThinkingFoldReadKeepsTheFileAndSetDefaultModelKeepsIt(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, settingsFile)
	const body = `{"theme":"dark","thinkingFold":"truncated"}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSettingsStore(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got, err := s.ThinkingFold(ctx); err != nil || got != ThinkingFoldTruncated {
		t.Fatalf("read: %q %v", got, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != body {
		t.Fatalf("a read changed the file: %q %v", b, err)
	}
	if err = s.SetDefaultModel(ctx, "a/b@c"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ThinkingFold(ctx); err != nil || got != ThinkingFoldTruncated {
		t.Fatalf("after a write of another field: %q %v", got, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"theme":"dark"`, `"thinkingFold":"truncated"`, `"defaultModel":"a/b@c"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("field lost, want %s in %s", want, b)
		}
	}
}

func TestThinkingFoldStopsWithTheContext(t *testing.T) {
	home := t.TempDir()
	s, err := NewSettingsStore(home)
	if err != nil {
		t.Fatal(err)
	}
	// Another holder keeps the lock, so the read waits until the context ends.
	unlock, err := lockSidecar(context.Background(), filepath.Join(home, settingsFile+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := s.ThinkingFold(ctx)
	if !errors.Is(err, context.Canceled) || got != ThinkingFoldExpanded {
		t.Fatalf("got %q %v", got, err)
	}
}
