package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// loadWithin runs Load and fails the test when it does not return within d.
func loadWithin(t *testing.T, l *Layered, d time.Duration) Effective {
	t.Helper()
	type result struct {
		eff Effective
		err error
	}
	done := make(chan result, 1)
	go func() {
		eff, err := l.Load(context.Background())
		done <- result{eff, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("load: %v", r.err)
		}
		return r.eff
	case <-time.After(d):
		t.Fatal("load blocked")
	}
	return Effective{}
}

func TestProjectReaderRefusesSpecialFiles(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"fifo": func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0644); err != nil {
				t.Skipf("mkfifo: %v", err)
			}
		},
		"directory": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
		},
		"oversized": func(t *testing.T, path string) {
			b := []byte(`{"pad":"` + strings.Repeat("a", 2<<20) + `"}`)
			if err := os.WriteFile(path, b, 0644); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newLayeredFixture(t)
			if err := os.Mkdir(filepath.Join(f.root, projectSettingsDir), 0755); err != nil {
				t.Fatal(err)
			}
			setup(t, f.projectPath())
			for _, trusted := range []bool{false, true} {
				l, err := NewLayered(f.home, Project{Root: f.root, Trusted: trusted})
				if err != nil {
					t.Fatal(err)
				}
				eff := loadWithin(t, l, 2*time.Second)
				got := diagnosticsFor(eff, LayerProject)
				if !trusted {
					if len(eff.Diagnostics) != 0 {
						t.Fatalf("untrusted diagnostics: %+v", eff.Diagnostics)
					}
					continue
				}
				if len(got) != 1 || got[0].Path != f.projectPath() {
					t.Fatalf("trusted diagnostics: %+v", eff.Diagnostics)
				}
			}
		})
	}
}

func TestProjectReaderAcceptsCommittedMode(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeProject(t, `{"defaultModel":"openai/b@openai-responses"}`, 0644)
	eff := f.load(t, true)
	if eff.DefaultModel != "openai/b@openai-responses" || len(eff.Diagnostics) != 0 {
		t.Fatalf("committed project file: %q %+v", eff.DefaultModel, eff.Diagnostics)
	}
}

func TestProjectReaderFollowsLinkToRegularFile(t *testing.T) {
	f := newLayeredFixture(t)
	target := filepath.Join(f.root, "shared.json")
	if err := os.WriteFile(target, []byte(`{"thinkingFold":"collapsed"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.root, projectSettingsDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, f.projectPath()); err != nil {
		t.Fatal(err)
	}
	eff := f.load(t, true)
	if eff.ThinkingFold != ThinkingFoldCollapsed || len(eff.Diagnostics) != 0 {
		t.Fatalf("linked project file: %q %+v", eff.ThinkingFold, eff.Diagnostics)
	}
}

func TestProjectReaderRefusesNonObject(t *testing.T) {
	for _, content := range []string{`[]`, `null`, `"x"`, ``} {
		f := newLayeredFixture(t)
		f.writeProject(t, content, 0644)
		got := diagnosticsFor(f.load(t, true), LayerProject)
		if len(got) != 1 || got[0].Path != f.projectPath() {
			t.Fatalf("content %q: %+v", content, got)
		}
	}
}
