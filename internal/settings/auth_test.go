package settings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuthStoreRevisionAndFence(t *testing.T) {
	s, err := NewAuthStore(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, rev, err := s.Read(ctx, "openai")
	if err != nil || rev != 0 {
		t.Fatalf("initial: %d %v", rev, err)
	}
	rev, err = s.Logout(ctx, "openai", rev)
	if err != nil || rev != 1 {
		t.Fatalf("logout: %d %v", rev, err)
	}
	if _, err = s.Replace(ctx, "openai", 0, Credential{Method: "api-key", APIKey: "key"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("late login: %v", err)
	}
	_, err = s.Replace(ctx, "openai", rev, Credential{Method: "openai-chatgpt", OAuth: &OAuthCredential{AccessToken: "access", RefreshToken: "refresh"}})
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := s.Read(ctx, "openai")
	if err != nil || c.Generation != 1 {
		t.Fatalf("read: %#v %v", c, err)
	}
	_, err = s.FenceRefresh(ctx, "openai", c.Generation, "attempt")
	if err != nil {
		t.Fatal(err)
	}
	c, _, err = s.Read(ctx, "openai")
	if err != nil || c.RefreshState == nil {
		t.Fatalf("fence: %#v %v", c, err)
	}
	if _, err = s.FenceRefresh(ctx, "openai", c.Generation, "again"); !errors.Is(err, ErrPendingRefresh) {
		t.Fatalf("repeat: %v", err)
	}
	if _, err = s.CommitRefresh(ctx, "openai", 1, "wrong", Credential{Method: "openai-chatgpt", OAuth: &OAuthCredential{AccessToken: "new", RefreshToken: "new-refresh"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong attempt: %v", err)
	}
	rev, err = s.CommitRefresh(ctx, "openai", 1, "attempt", Credential{Method: "openai-chatgpt", OAuth: &OAuthCredential{AccessToken: "new", RefreshToken: "new-refresh"}})
	if err != nil {
		t.Fatal(err)
	}
	c, got, err := s.Read(ctx, "openai")
	if err != nil || got != rev || c.Generation != 2 || c.RefreshState != nil {
		t.Fatalf("commit: %#v %d %v", c, got, err)
	}
}

func TestAuthStoreFailures(t *testing.T) {
	ctx := context.Background()
	s, err := NewAuthStore(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	rev, err := s.Replace(ctx, "openai", 0, Credential{Method: "api-key", APIKey: "old"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.home, "auth.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := s.ops.rename
	s.ops.rename = func(string, string) error { return errors.New("rename failed") }
	if _, err = s.Replace(ctx, "openai", rev, Credential{Method: "api-key", APIKey: "new"}); err == nil {
		t.Fatal("expected failure")
	}
	s.ops.rename = original
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("prior bytes changed: %v", err)
	}
	originalSync := s.ops.syncDir
	s.ops.syncDir = func(*os.File) error { return errors.New("directory sync failed") }
	if _, err = s.Replace(ctx, "openai", rev, Credential{Method: "api-key", APIKey: "new"}); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("post-rename error: %v", err)
	}
	s.ops.syncDir = originalSync
	c, _, err := s.Read(ctx, "openai")
	if err != nil || c.APIKey != "new" {
		t.Fatalf("visible replacement: %#v %v", c, err)
	}
}

func TestAuthStorePreRenameFailures(t *testing.T) {
	for _, stage := range []string{"write", "short-write", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			s, err := NewAuthStore(filepath.Join(t.TempDir(), "home"))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			rev, err := s.Replace(ctx, "openai", 0, Credential{Method: "api-key", APIKey: "old"})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.home, "auth.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New(stage + " failure")
			switch stage {
			case "write":
				s.ops.write = func(*os.File, []byte) (int, error) { return 0, failure }
			case "short-write":
				s.ops.write = func(*os.File, []byte) (int, error) { return 0, nil }
			case "sync":
				s.ops.syncFile = func(*os.File) error { return failure }
			case "close":
				s.ops.closeFile = func(f *os.File) error { _ = f.Close(); return failure }
			case "rename":
				s.ops.rename = func(string, string) error { return failure }
			}
			if _, err = s.Replace(ctx, "openai", rev, Credential{Method: "api-key", APIKey: "new"}); err == nil || errors.Is(err, ErrIndeterminate) {
				t.Fatalf("%s: %v", stage, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatalf("%s changed prior bytes: %v", stage, err)
			}
		})
	}
}

func TestAuthStoreProcessHelper(t *testing.T) {
	mode := os.Getenv("ASK_STORE_PROCESS_MODE")
	if mode == "" {
		return
	}
	home := os.Getenv("ASK_STORE_PROCESS_HOME")
	s, err := NewAuthStore(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	switch mode {
	case "hold":
		release, e := lockSidecar(context.Background(), filepath.Join(home, "auth.json.lock"))
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		_ = release
		fmt.Println("LOCKED")
		time.Sleep(30 * time.Second)
	case "write":
		p := os.Getenv("ASK_STORE_PROCESS_PROVIDER")
		for i := 0; i < 12; i++ {
			for {
				_, rev, e := s.Read(context.Background(), p)
				if e != nil {
					fmt.Fprintln(os.Stderr, e)
					os.Exit(2)
				}
				_, e = s.Replace(context.Background(), p, rev, Credential{Method: "api-key", APIKey: fmt.Sprintf("%s-%d", p, i)})
				if errors.Is(e, ErrConflict) {
					continue
				}
				if e != nil {
					fmt.Fprintln(os.Stderr, e)
					os.Exit(2)
				}
				break
			}
		}
	case "fence-exit":
		if _, e := s.FenceRefresh(context.Background(), "xai", 1, "process-attempt"); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func storeChild(home, mode, provider string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAuthStoreProcessHelper$")
	cmd.Env = append(os.Environ(), "ASK_STORE_PROCESS_MODE="+mode, "ASK_STORE_PROCESS_HOME="+home, "ASK_STORE_PROCESS_PROVIDER="+provider)
	return cmd
}

func TestAuthStoreLockSurvivesProcessDeath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureHome(home); err != nil {
		t.Fatal(err)
	}
	cmd := storeChild(home, "hold", "")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	buf := make([]byte, 7)
	if _, err = io.ReadFull(out, buf); err != nil || string(buf) != "LOCKED\n" {
		t.Fatalf("child lock signal: %q %v", buf, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, _, err = s.Read(ctx, "openai"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock must block: %v", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if _, _, err = s.Read(context.Background(), "openai"); err != nil {
		t.Fatalf("lock retained after death: %v", err)
	}
}

func TestAuthStoreTwoProcessesAndReader(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	first := storeChild(home, "write", "anthropic")
	second := storeChild(home, "write", "xai")
	if err = first.Start(); err != nil {
		t.Fatal(err)
	}
	if err = second.Start(); err != nil {
		_ = first.Process.Kill()
		_ = first.Wait()
		t.Fatal(err)
	}
	defer func() {
		if first.ProcessState == nil {
			_ = first.Process.Kill()
			_ = first.Wait()
		}
		if second.ProcessState == nil {
			_ = second.Process.Kill()
			_ = second.Wait()
		}
	}()
	for i := 0; i < 100; i++ {
		_, _, err = s.Read(context.Background(), "anthropic")
		if err != nil {
			t.Fatalf("partial JSON: %v", err)
		}
	}
	if err = first.Wait(); err != nil {
		t.Fatal(err)
	}
	if err = second.Wait(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"anthropic", "xai"} {
		c, _, e := s.Read(context.Background(), p)
		if e != nil || c.APIKey != p+"-11" {
			t.Fatalf("%s lost: %#v %v", p, c, e)
		}
	}
}

func TestAuthStoreUnsafeAndUnknown(t *testing.T) {
	ctx := context.Background()
	home := filepath.Join(t.TempDir(), "home")
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "auth.json")
	data := []byte(`{"schemaVersion":1,"revision":4,"extra":{"keep":true},"providers":{"other":{"method":"api-key","apiKey":"other","extension":1}}}`)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Replace(ctx, "openai", 4, Credential{Method: "api-key", APIKey: "new"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(got), `"extra"`, `"extension"`, `"other"`) {
		t.Fatalf("unknown data lost: %s", got)
	}
	if err = os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Logout(ctx, "openai", 5); !errors.Is(err, ErrUnsafeStore) {
		t.Fatalf("corrupt file: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "broken" {
		t.Fatal("corrupt bytes changed")
	}
}

func TestAuthStoreLegacyAndConcurrentWriters(t *testing.T) {
	ctx := context.Background()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"openai":{"type":"api_key","key":"legacy","extra":"keep"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := s.Read(ctx, "openai")
	if err != nil || c.Method != "api-key" || c.APIKey != "legacy" {
		t.Fatalf("legacy: %#v %v", c, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, provider := range []string{"anthropic", "xai"} {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			for {
				_, r, e := s.Read(ctx, p)
				if e != nil {
					errs <- e
					return
				}
				_, e = s.Replace(ctx, p, r, Credential{Method: "api-key", APIKey: p})
				if errors.Is(e, ErrConflict) {
					continue
				}
				errs <- e
				return
			}
		}(provider)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, provider := range []string{"openai", "anthropic", "xai"} {
		c, _, err = s.Read(ctx, provider)
		if err != nil || c.APIKey == "" {
			t.Fatalf("%s lost: %#v %v", provider, c, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"extra":"keep"`) {
		t.Fatalf("legacy unknown field lost: %s", got)
	}
}

func TestAuthStoreCanceledLockWait(t *testing.T) {
	s, err := NewAuthStore(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureHome(s.home); err != nil {
		t.Fatal(err)
	}
	release, err := lockSidecar(context.Background(), filepath.Join(s.home, "auth.json.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = s.Read(ctx, "openai")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter: %v", err)
	}
}

func TestAuthStoreRefreshKeepsLockAndFence(t *testing.T) {
	s, err := NewAuthStore(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = s.Replace(ctx, "openai", 0, Credential{Method: "openai-chatgpt", OAuth: &OAuthCredential{AccessToken: "old", RefreshToken: "refresh", Scopes: []string{"a"}}})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	continueExchange := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := s.Refresh(ctx, "openai", 1, "attempt", func(_ context.Context, c Credential) (Credential, error) {
			close(entered)
			<-continueExchange
			c.OAuth.AccessToken = "new"
			c.OAuth.Scopes[0] = "changed"
			return c, nil
		})
		done <- e
	}()
	<-entered
	lockCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, _, err = s.Read(lockCtx, "openai"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exchange did not hold lock: %v", err)
	}
	close(continueExchange)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	c, _, err := s.Read(ctx, "openai")
	if err != nil || c.OAuth.AccessToken != "new" || c.Generation != 2 || c.RefreshState != nil {
		t.Fatalf("replacement: %#v %v", c, err)
	}
	called := false
	again, err := s.Refresh(ctx, "openai", 1, "later", func(context.Context, Credential) (Credential, error) { called = true; return Credential{}, nil })
	if err != nil || called || again.Generation != 2 {
		t.Fatalf("stale generation: %#v %v called=%v", again, err, called)
	}
}

func TestAuthStoreRefreshErrorFenceSurvivesRestart(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = s.Replace(ctx, "xai", 0, Credential{Method: "xai-oauth", OAuth: &OAuthCredential{AccessToken: "old", RefreshToken: "rotating"}})
	if err != nil {
		t.Fatal(err)
	}
	serverError := errors.New("lost response")
	_, err = s.Refresh(ctx, "xai", 1, "attempt", func(context.Context, Credential) (Credential, error) { return Credential{}, serverError })
	if !errors.Is(err, serverError) {
		t.Fatal(err)
	}
	restarted, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := restarted.Read(ctx, "xai")
	if err != nil || c.RefreshState == nil || c.RefreshState.AttemptID != "attempt" {
		t.Fatalf("fence after restart: %#v %v", c, err)
	}
	called := false
	_, err = restarted.Refresh(ctx, "xai", 1, "retry", func(context.Context, Credential) (Credential, error) { called = true; return Credential{}, nil })
	if !errors.Is(err, ErrPendingRefresh) || called {
		t.Fatalf("grant reused: %v called=%v", err, called)
	}
}

func TestAuthStoreFenceSurvivesProcessExit(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Replace(context.Background(), "xai", 0, Credential{Method: "xai-oauth", OAuth: &OAuthCredential{AccessToken: "old", RefreshToken: "rotating"}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := storeChild(home, "fence-exit", "")
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("child: %v %s", e, output)
	}
	c, _, err := s.Read(context.Background(), "xai")
	if err != nil || c.RefreshState == nil {
		t.Fatalf("fence after process exit: %#v %v", c, err)
	}
	called := false
	_, err = s.Refresh(context.Background(), "xai", 1, "retry", func(context.Context, Credential) (Credential, error) { called = true; return Credential{}, nil })
	if !errors.Is(err, ErrPendingRefresh) || called {
		t.Fatalf("grant reused: %v called=%v", err, called)
	}
}

func TestAuthStoreRefreshCommitFailureLeavesFence(t *testing.T) {
	s, err := NewAuthStore(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Replace(context.Background(), "xai", 0, Credential{Method: "xai-oauth", OAuth: &OAuthCredential{AccessToken: "old", RefreshToken: "rotating"}})
	if err != nil {
		t.Fatal(err)
	}
	write := s.ops.write
	calls := 0
	s.ops.write = func(f *os.File, b []byte) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("replacement write failed")
		}
		return write(f, b)
	}
	_, err = s.Refresh(context.Background(), "xai", 1, "attempt", func(context.Context, Credential) (Credential, error) {
		return Credential{Method: "xai-oauth", OAuth: &OAuthCredential{AccessToken: "new", RefreshToken: "new-refresh"}}, nil
	})
	if err == nil {
		t.Fatal("replacement failure reported success")
	}
	c, _, err := s.Read(context.Background(), "xai")
	if err != nil || c.RefreshState == nil || c.OAuth.AccessToken != "old" {
		t.Fatalf("unsafe state: %#v %v", c, err)
	}
}

func TestAuthStoreRejectsUnsafeFileAndSchema(t *testing.T) {
	for _, variant := range []string{"symlink", "directory", "mode", "schema", "ambiguous-oauth"} {
		t.Run(variant, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "auth.json")
			switch variant {
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
					t.Fatal(err)
				}
			case "schema":
				if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"providers":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "ambiguous-oauth":
				if err := os.WriteFile(path, []byte(`{"xai":{"type":"oauth","access":"a","refresh":"r"}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := NewAuthStore(home)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "ambiguous-oauth" {
				_, _, err = s.Read(context.Background(), "xai")
			} else {
				_, err = s.Logout(context.Background(), "xai", 0)
			}
			if !errors.Is(err, ErrUnsafeStore) {
				t.Fatalf("%s accepted: %v", variant, err)
			}
		})
	}
}

func TestAuthStoreMergesRootLegacyWithEnvelope(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "auth.json")
	data := []byte(`{"schemaVersion":1,"revision":7,"providers":{"xai":{"method":"api-key","apiKey":"x"}},"anthropic":{"type":"api_key","key":"a","extension":5},"unknown":{"keep":true}}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Replace(context.Background(), "openai", 7, Credential{Method: "api-key", APIKey: "o"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"anthropic", "xai", "openai"} {
		c, _, e := s.Read(context.Background(), p)
		if e != nil || c.APIKey == "" {
			t.Fatalf("%s lost: %#v %v", p, c, e)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil || !containsAll(string(b), `"extension":5`, `"unknown":{"keep":true}`) {
		t.Fatalf("unknown fields lost: %s %v", b, err)
	}
}

func containsAll(s string, terms ...string) bool {
	for _, term := range terms {
		if !strings.Contains(s, term) {
			return false
		}
	}
	return true
}

func TestAuthStoreRefreshCommitHasIndependentBound(t *testing.T) {
	for _, delay := range []bool{false, true} {
		t.Run(fmt.Sprint("delay=", delay), func(t *testing.T) {
			s, err := NewAuthStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Replace(context.Background(), "xai", 0, Credential{Method: "xai-oauth", OAuth: &OAuthCredential{AccessToken: "old", RefreshToken: "rotating"}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			syncFile := s.ops.syncFile
			syncs := 0
			s.ops.syncFile = func(f *os.File) error {
				syncs++
				if syncs == 2 && delay {
					time.Sleep(5100 * time.Millisecond)
				}
				return syncFile(f)
			}
			_, err = s.Refresh(ctx, "xai", 1, "attempt", func(context.Context, Credential) (Credential, error) {
				cancel()
				return Credential{Method: "xai-oauth", OAuth: &OAuthCredential{AccessToken: "new", RefreshToken: "new-refresh"}}, nil
			})
			if delay && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("unbounded commit: %v", err)
			}
			if !delay && err != nil {
				t.Fatalf("caller cancellation discarded validated rotation: %v", err)
			}
			c, _, readErr := s.Read(context.Background(), "xai")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if delay && (c.RefreshState == nil || c.OAuth.AccessToken != "old") {
				t.Fatal("commit past deadline cleared fence")
			}
			if !delay && (c.RefreshState != nil || c.OAuth.AccessToken != "new") {
				t.Fatal("validated rotation not committed")
			}
		})
	}
}

func TestAuthStoreRefreshPreservesSelectedUnknownFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "auth.json")
	data := []byte(`{"providers":{"xai":{"method":"xai-oauth","generation":1,"extension":{"keep":true},"oauth":{"accessToken":"old","refreshToken":"refresh","providerExtension":{"keep":true}}}}}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Refresh(context.Background(), "xai", 1, "attempt", func(_ context.Context, c Credential) (Credential, error) {
		b, e := os.ReadFile(path)
		if e != nil || !containsAll(string(b), `"extension":{"keep":true}`, `"providerExtension":{"keep":true}`, `"status":"pending"`) {
			t.Fatalf("fence dropped unknown fields: %v", e)
		}
		c.OAuth.AccessToken = "new"
		return c, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !containsAll(string(b), `"extension":{"keep":true}`, `"providerExtension":{"keep":true}`, `"accessToken":"new"`) {
		t.Fatalf("refresh dropped unknown fields: %v", err)
	}
}
