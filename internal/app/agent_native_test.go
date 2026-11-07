package app

import (
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/settings"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNativeAgentExplicitIdentity(t *testing.T) {
	cfg := NativeAgentConfig{Config: agent.Config{SessionID: "session-explicit", LoopConfig: agent.LoopConfig{Cwd: t.TempDir()}}}
	// The initial stream serves the demo model only; a catalog model or no
	// model needs none.
	ag, err := NewNativeAgent(cfg)
	if err != nil {
		t.Fatalf("an agent with no initial stream: %v", err)
	}
	if ag.State().Model.ID != "" {
		t.Fatal("an agent with no model reports one")
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

func TestNativeAgentSweepsOnce(t *testing.T) {
	sweepTasksOnce = sync.Once{}
	home := t.TempDir()
	// A private directory is required at every level; TempDir uses the mask of the process.
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "tasks")
	old := time.Now().Add(-30 * 24 * time.Hour)
	aged := func(name string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "sh-1.log")
		if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{file, dir} {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	firstOld := aged("old-one")

	p, err := faux.New()
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.Model("faux-1")
	newAgent := func(session string) *agent.Agent {
		a, err := NewNativeAgent(NativeAgentConfig{
			Home:          home,
			InitialStream: p.Stream,
			Config:        agent.Config{SessionID: session, LoopConfig: agent.LoopConfig{Cwd: t.TempDir(), Model: model}},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = a.Dispose() })
		return a
	}

	newAgent("session-a")
	if _, err := os.Stat(firstOld); !os.IsNotExist(err) {
		t.Fatalf("old session directory survived the first sweep: %v", err)
	}
	// A session without task output makes no directory.
	if _, err := os.Stat(filepath.Join(root, "session-a")); !os.IsNotExist(err) {
		t.Fatalf("a session without output made a directory: %v", err)
	}

	secondOld := aged("old-two")
	newAgent("session-b")
	if _, err := os.Stat(secondOld); err != nil {
		t.Fatalf("the second Agent swept again: %v", err)
	}
}

func TestNativeAgentMakesTaskDirOnlyOnPrepare(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	dir := taskDir(NativeAgentConfig{Home: home}, "s1")
	if dir != filepath.Join(home, "tasks", "s1") {
		t.Fatalf("task directory %q", dir)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("taskDir touched the file system: %v", err)
	}
	if err := ensureTaskDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, level := range []string{home, filepath.Dir(dir), dir} {
		info, err := os.Lstat(level)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("%s: %v %v", level, info, err)
		}
	}
}

func TestNativeAgentTaskDirFailureIsNotFatal(t *testing.T) {
	cases := map[string]NativeAgentConfig{
		"relative home": {Home: "relative"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if dir := taskDir(in, "s1"); dir != "" {
				t.Fatalf("task directory %q for a bad home", dir)
			}
		})
	}
	if dir := taskDir(NativeAgentConfig{Home: t.TempDir()}, "../escape"); dir != "" {
		t.Fatalf("task directory %q for a session that is not a plain name", dir)
	}
}

// oldSession makes a session directory with one log, both older than the
// retention, and returns its path.
func oldSession(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "sh-1.log")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, p := range []string{file, dir} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTaskDirSweepSkipsUnsafeLocation(t *testing.T) {
	private := func(t *testing.T) string {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	cases := map[string]func(t *testing.T) (home, survivor string){
		"tasks directory is a symbolic link": func(t *testing.T) (string, string) {
			home, elsewhere := private(t), private(t)
			survivor := oldSession(t, elsewhere, "important-work")
			if err := os.Symlink(elsewhere, filepath.Join(home, "tasks")); err != nil {
				t.Fatal(err)
			}
			return home, survivor
		},
		"home has an open mode": func(t *testing.T) (string, string) {
			home := private(t)
			survivor := oldSession(t, filepath.Join(home, "tasks"), "old")
			if err := os.Chmod(home, 0755); err != nil {
				t.Fatal(err)
			}
			return home, survivor
		},
		"tasks directory has an open mode": func(t *testing.T) (string, string) {
			home := private(t)
			survivor := oldSession(t, filepath.Join(home, "tasks"), "old")
			if err := os.Chmod(filepath.Join(home, "tasks"), 0755); err != nil {
				t.Fatal(err)
			}
			return home, survivor
		},
		"home is a symbolic link": func(t *testing.T) (string, string) {
			real := private(t)
			survivor := oldSession(t, filepath.Join(real, "tasks"), "old")
			link := filepath.Join(private(t), "home")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			return link, survivor
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			sweepTasksOnce = sync.Once{}
			home, survivor := setup(t)
			taskDir(NativeAgentConfig{Home: home}, "current")
			if _, err := os.Stat(survivor); err != nil {
				t.Fatalf("the sweep removed %s: %v", survivor, err)
			}
		})
	}

	t.Run("a private location is still swept", func(t *testing.T) {
		sweepTasksOnce = sync.Once{}
		home := private(t)
		old := oldSession(t, filepath.Join(home, "tasks"), "old")
		taskDir(NativeAgentConfig{Home: home}, "current")
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatalf("old session directory survived: %v", err)
		}
	})
}
