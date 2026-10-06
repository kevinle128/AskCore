//go:build fsfault

package main

import (
	"AskCore/internal/settings"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// This target requires the external Linux FUSE fixture; missing setup fails.
// Ordinary go test does not select the fsfault build tag.
type authSyncGate struct{ home, backing, socket string }

func (g authSyncGate) command(command string) (string, error) {
	c, err := net.DialTimeout("unix", g.socket, time.Second)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	if err = c.SetDeadline(time.Now().Add(22 * time.Second)); err != nil {
		return "", err
	}
	if _, err = fmt.Fprintln(c, command); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(line), err
}

func (g authSyncGate) expect(t *testing.T, command, want string) {
	t.Helper()
	got, err := g.command(command)
	if err != nil || got != want {
		t.Fatalf("filesystem gate %s: reply=%q error=%v want=%q", command, got, err, want)
	}
}

func startAuthSyncGate(t *testing.T) authSyncGate {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Fatal("fsfault requires Linux with /dev/fuse and mount permission")
	}
	g := authSyncGate{home: os.Getenv("ASK_TEST_FSYNC_HOME"), backing: os.Getenv("ASK_TEST_FSYNC_BACKING"), socket: os.Getenv("ASK_TEST_FSYNC_CONTROL")}
	if g.home != "" || g.backing != "" || g.socket != "" {
		if g.home == "" || g.backing == "" || g.socket == "" {
			t.Fatal("set ASK_TEST_FSYNC_HOME, ASK_TEST_FSYNC_BACKING, and ASK_TEST_FSYNC_CONTROL together")
		}
		status, err := g.command("STATUS")
		if err != nil || (status != "IDLE" && status != "RELEASED" && status != "SYNCED") {
			t.Fatalf("external filesystem fixture is not ready: status=%q error=%v", status, err)
		}
		var stat syscall.Statfs_t
		if err := syscall.Statfs(g.home, &stat); err != nil || stat.Type != 0x65735546 {
			t.Fatalf("external credential home is not a FUSE mount: %v", err)
		}
		g.home, err = os.MkdirTemp(g.home, "actor-")
		if err != nil {
			t.Fatal(err)
		}
		g.backing = filepath.Join(g.backing, filepath.Base(g.home))
		t.Cleanup(func() {
			_, _ = g.command("RELEASE")
			_, _ = g.command("RELEASE_EXIT")
			if err := os.RemoveAll(g.home); err != nil {
				t.Error(err)
			}
		})
		return g
	}
	binary := os.Getenv("ASK_FSYNC_GATE_BINARY")
	if binary == "" {
		t.Fatal("set ASK_FSYNC_GATE_BINARY to the external FUSE fixture executable")
	}
	root, err := os.MkdirTemp("", "ask-fsync-")
	if err != nil {
		t.Fatal(err)
	}
	g = authSyncGate{home: filepath.Join(root, "home"), backing: filepath.Join(root, "backing"), socket: filepath.Join(root, "gate.sock")}
	for _, path := range []string{g.backing, g.home} {
		if err = os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(binary, g.backing, g.home, g.socket)
	var diagnostics bytes.Buffer
	cmd.Stdout, cmd.Stderr = &diagnostics, &diagnostics
	if err = cmd.Start(); err != nil {
		_ = os.RemoveAll(root)
		t.Fatal(err)
	}
	t.Logf("owned filesystem fixture: command=%s pid=%d mount=%s socket=%s", binary, cmd.Process.Pid, g.home, g.socket)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_, _ = g.command("RELEASE")
		_, _ = g.command("RELEASE_EXIT")
		unmount := exec.Command("fusermount3", "-u", g.home)
		if err := unmount.Run(); err != nil {
			t.Errorf("unmount owned test filesystem: %v", err)
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := g.command("STATUS"); err == nil && status == "IDLE" {
			var stat syscall.Statfs_t
			if err = syscall.Statfs(g.home, &stat); err == nil && stat.Type == 0x65735546 {
				return g
			}
		}
		select {
		case err := <-done:
			done <- err
			t.Fatalf("filesystem fixture stopped: %v %s", err, diagnostics.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("filesystem fixture did not become ready")
	return g
}

// Read backing bytes: Linux can serialize mounted inode reads behind FUSE fsync.
// The production transaction still owns its lock and uses the mounted path.
func TestAuthSyncReadSubprocessHelper(t *testing.T) {
	path := os.Getenv("ASK_TEST_FSYNC_READ")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		os.Exit(2)
	}
	if _, err = os.Stdout.Write(data); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func readAuthSyncCredential(t *testing.T, path string) ([]byte, settings.Credential) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAuthSyncReadSubprocessHelper$")
	cmd.Env = append(os.Environ(), "ASK_TEST_FSYNC_READ="+path, "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("separate credential observer: %v", err)
	}
	var doc struct {
		Providers map[string]settings.Credential `json:"providers"`
	}
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return data, doc.Providers["anthropic"]
}

func TestAuthCommandSignalsDuringLocalDurableCommit(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  os.Signal
		code int
	}{{"interrupt", os.Interrupt, 130}, {"terminate", syscall.SIGTERM, 143}, {"hangup", syscall.SIGHUP, 129}, {"budget-expired", os.Interrupt, 130}, {"forced-death", syscall.SIGKILL, -1}} {
		t.Run(tc.name, func(t *testing.T) {
			g := startAuthSyncGate(t)
			t.Setenv("ASK_AUTH_TIME_ADVANCE", "")
			t.Setenv("ASK_AUTH_IGNORE_CANCEL", "")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-Test-Logical-URL") != "https://platform.claude.com/v1/oauth/token" {
					t.Error("commit actor reached unexpected HTTP destination")
					http.Error(w, "bad", 400)
					return
				}
				var grant map[string]string
				if err := json.NewDecoder(r.Body).Decode(&grant); err != nil {
					t.Error(err)
					return
				}
				v := oauthTokens()
				switch grant["grant_type"] {
				case "authorization_code":
					if grant["code"] != "private-code-canary" || grant["code_verifier"] == "" || grant["state"] != grant["code_verifier"] {
						t.Error("invalid native login exchange")
					}
				case "refresh_token":
					if grant["refresh_token"] != "private-refresh-canary" {
						t.Error("wrong rotating grant")
					}
					v["access_token"], v["refresh_token"] = "rotated-access-canary", "rotated-refresh-canary"
				default:
					t.Error("invalid grant type")
					http.Error(w, "bad", 400)
					return
				}
				oauthJSON(w, 200, v)
			}))
			defer server.Close()
			login := startOAuthCommand(t, g.home, server.URL, "", "auth", "login", "--provider", "anthropic", "--method", "anthropic-oauth", "--interaction", "copy-code")
			completeOAuth(t, login, false)
			login.finish(t, true)
			path := filepath.Join(g.backing, "auth.json")
			_, initial := readAuthSyncCredential(t, path)
			t.Setenv("ASK_AUTH_TIME_ADVANCE", "55m")
			g.expect(t, "ARM", "OK")
			p := startOAuthCommand(t, g.home, server.URL, "", "--provider", "anthropic", "-p", "hello")
			_ = p.input.Close()
			g.expect(t, "WAIT", "BLOCKED")
			blockedAt := time.Now()
			fenced, pending := readAuthSyncCredential(t, path)
			if pending.Generation != initial.Generation || pending.RefreshState == nil || pending.RefreshState.Status != "pending" || pending.RefreshState.Generation != pending.Generation || pending.RefreshState.AttemptID == "" {
				t.Fatal("replacement sync entered without durable pending fence")
			}
			files, err := filepath.Glob(filepath.Join(g.backing, ".auth-*"))
			if err != nil || len(files) != 1 {
				t.Fatalf("replacement file count=%d error=%v", len(files), err)
			}
			_, replacement := readAuthSyncCredential(t, files[0])
			if replacement.Generation != initial.Generation+1 || replacement.RefreshState != nil || replacement.OAuth == nil || replacement.OAuth.AccessToken != "rotated-access-canary" || replacement.OAuth.RefreshToken != "rotated-refresh-canary" {
				t.Fatal("blocked sync does not contain validated replacement bytes")
			}
			if calls.Load() != 2 {
				t.Fatal("expected only native login and completed rotating exchange")
			}
			if err = p.cmd.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			waited := make(chan struct{})
			go func() {
				done <- p.cmd.Wait()
				close(waited)
			}()
			t.Cleanup(func() {
				_ = p.cmd.Process.Kill()
				select {
				case <-waited:
				case <-time.After(3 * time.Second):
					t.Error("owned CLI actor did not stop")
				}
			})
			if tc.name != "forced-death" {
				// Distinct delivery times prevent ordinary signal coalescing.
				select {
				case err = <-done:
					t.Fatalf("first signal ended blocked commit: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				if err = p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
				hold := 2200 * time.Millisecond
				if tc.name == "budget-expired" {
					hold = 5200*time.Millisecond - time.Since(blockedAt)
				}
				select {
				case err = <-done:
					t.Fatalf("second signal ended blocked commit: %v", err)
				case <-time.After(hold):
				}
			}
			t.Logf("replacement sync held for %s before release", time.Since(blockedAt))
			g.expect(t, "RELEASE", "OK")
			if tc.name != "budget-expired" && tc.name != "forced-death" {
				g.expect(t, "WAIT_SYNC", "SYNCED")
				_, saved := readAuthSyncCredential(t, path)
				if !reflect.DeepEqual(saved, replacement) {
					t.Fatal("separate process did not observe durable replacement before exit")
				}
				select {
				case err = <-done:
					t.Fatalf("CLI exited before independent durable-byte observation: %v", err)
				default:
				}
			}
			g.expect(t, "RELEASE_EXIT", "OK")
			select {
			case err = <-done:
			case <-time.After(8 * time.Second):
				t.Fatal("commit actor did not exit after release")
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != tc.code {
				t.Fatalf("commit actor exit=%v want=%d", err, tc.code)
			}
			if tc.name == "forced-death" {
				status, ok := exit.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatal("forced-death actor was not killed by SIGKILL")
				}
			}
			<-p.readDone
			for _, secret := range []string{"private-access-canary", "private-refresh-canary", "rotated-access-canary", "rotated-refresh-canary"} {
				if strings.Contains(p.output.String()+p.diagnostics.String(), secret) {
					t.Fatal("signal actor leaked credential")
				}
			}
			if tc.name == "budget-expired" || tc.name == "forced-death" {
				after, _ := readAuthSyncCredential(t, path)
				if !bytes.Equal(fenced, after) {
					t.Fatal("uncertain local commit changed durable fence")
				}
				restart := startOAuthCommand(t, g.home, server.URL, "", "--provider", "anthropic", "-p", "hello")
				restart.finish(t, false)
				if !strings.Contains(restart.diagnostics.String(), "refresh result uncertain; sign in again") || calls.Load() != 2 {
					t.Fatal("next process did not refuse fenced grant without HTTP")
				}
				after, _ = readAuthSyncCredential(t, path)
				if !bytes.Equal(fenced, after) {
					t.Fatal("next command changed pending fence")
				}
			}
		})
	}
}
