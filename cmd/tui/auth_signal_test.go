package main

import (
	"AskCore/internal/testsupport"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func waitAuthSignal(t *testing.T, p *oauthProcess, sig os.Signal, want int) {
	t.Helper()
	if err := p.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != want {
			t.Fatalf("signal exit: %v want %d", err, want)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("auth command did not stop")
	}
	<-p.readDone
	_ = p.input.Close()
}
func TestAuthCommandSignalsBlockedPrivateInputAndCallback(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	for _, tc := range []struct {
		name string
		sig  os.Signal
		code int
	}{{"interrupt", os.Interrupt, 130}, {"terminate", syscall.SIGTERM, 143}, {"hangup", syscall.SIGHUP, 129}} {
		for _, method := range []string{"api-key", "anthropic-oauth"} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				home := t.TempDir()
				_, _, err := authCommand(t, home, "test-private-key\n", "auth", "login", "--provider", "anthropic", "--method", "api-key")
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(filepath.Join(home, "auth.json"))
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("blocked login reached external HTTP")
					http.Error(w, "bad", 400)
				}))
				defer server.Close()
				args := []string{"auth", "login", "--provider", "anthropic", "--method", method}
				if method == "anthropic-oauth" {
					args = append(args, "--interaction", "browser")
				}
				p := startOAuthCommand(t, home, server.URL, "", args...)
				prefix := "Enter the API key"
				if method == "anthropic-oauth" {
					prefix = "https://claude.ai/"
				}
				p.notice(t, prefix)
				waitAuthSignal(t, p, tc.sig, tc.code)
				after, err := os.ReadFile(filepath.Join(home, "auth.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatal("signal changed credential")
				}
				if method == "anthropic-oauth" {
					p = startOAuthCommand(t, home, server.URL, "", args...)
					p.notice(t, "https://claude.ai/")
					waitAuthSignal(t, p, tc.sig, tc.code)
				}
			})
		}
	}
}

func TestAuthCommandLoginLogoutFence(t *testing.T) {
	home := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { oauthJSON(w, 200, oauthTokens()) }))
	defer server.Close()
	p := startOAuthCommand(t, home, server.URL, "", "auth", "login", "--provider", "anthropic", "--method", "anthropic-oauth", "--interaction", "copy-code")
	raw := p.notice(t, "https://claude.ai/")
	logout := startOAuthCommand(t, home, server.URL, "", "auth", "logout", "--provider", "anthropic")
	logout.finish(t, true)
	// The pending login still completes its provider exchange but cannot resurrect
	// the credential after a different process advances the store revision.
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("missing state")
	}
	if _, err := fmt.Fprintln(p.input, "private-code-canary#"+state); err != nil {
		t.Fatal(err)
	}
	p.finish(t, false)
	next := startOAuthCommand(t, home, server.URL, "", "--provider", "anthropic", "-p", "hello")
	next.finish(t, false)
}

func TestAuthCommandPrivateInputLimitRetainsCredential(t *testing.T) {
	home := t.TempDir()
	_, _, err := authCommand(t, home, "test-private-key\n", "auth", "login", "--provider", "anthropic", "--method", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("oversize input reached token exchange")
		http.Error(w, "bad", 400)
	}))
	defer server.Close()
	p := startOAuthCommand(t, home, server.URL, "", "auth", "login", "--provider", "anthropic", "--method", "anthropic-oauth", "--interaction", "copy-code")
	p.notice(t, "https://claude.ai/")
	if _, err = io.WriteString(p.input, string(bytes.Repeat([]byte{'x'}, authInputLimit+1))+"\n"); err != nil {
		t.Fatal(err)
	}
	p.finish(t, false)
	after, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("oversize input replaced credential")
	}
}

func TestAuthCommandSignalsDrainRotatingRefreshBeforeExit(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  os.Signal
		code int
	}{{"interrupt", os.Interrupt, 130}, {"terminate", syscall.SIGTERM, 143}, {"hangup", syscall.SIGHUP, 129}} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			started := make(chan struct{})
			release := make(chan struct{})
			released := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Test-Logical-URL") != "https://platform.claude.com/v1/oauth/token" {
					t.Error("unexpected refresh request")
					http.Error(w, "bad", 400)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				v := oauthTokens()
				if bytes.Contains(body, []byte("refresh_token")) {
					close(started)
					<-release
					v["access_token"] = "rotated-access-canary"
					v["refresh_token"] = "rotated-refresh-canary"
				}
				oauthJSON(w, 200, v)
			}))
			defer server.Close()
			defer func() {
				if !released {
					close(release)
				}
			}()
			p := startOAuthCommand(t, home, server.URL, "", "auth", "login", "--provider", "anthropic", "--method", "anthropic-oauth", "--interaction", "copy-code")
			completeOAuth(t, p, false)
			p.finish(t, true)
			t.Setenv("ASK_AUTH_TIME_ADVANCE", "55m")
			t.Setenv("ASK_AUTH_IGNORE_CANCEL", "1")
			p = startOAuthCommand(t, home, server.URL, "", "--provider", "anthropic", "-p", "hello")
			_ = p.input.Close()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("refresh did not start")
			}
			if err := p.cmd.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			p.notice(t, "External request cancellation observed.")
			if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- p.cmd.Wait() }()
			select {
			case err := <-done:
				t.Fatalf("second signal ended pending rotation: %v", err)
			case <-time.After(2200 * time.Millisecond):
			}
			close(release)
			released = true
			select {
			case err := <-done:
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != tc.code {
					t.Fatalf("refresh signal exit: %v want %d", err, tc.code)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("refresh drain did not finish")
			}
			<-p.readDone
			data, err := os.ReadFile(filepath.Join(home, "auth.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("rotated-refresh-canary")) || bytes.Contains(data, []byte(`"status":"pending"`)) {
				t.Fatal("rotation was not durably committed")
			}
			if bytes.Contains(p.output.Bytes(), []byte("rotated")) || bytes.Contains(p.diagnostics.Bytes(), []byte("rotated")) {
				t.Fatal("rotated credential leaked")
			}
		})
	}
}

func TestAuthCommandRestartDoesNotReuseUncertainRefreshGrant(t *testing.T) {
	for _, failure := range []string{"process-killed", "response-lost", "replacement-write-failed"} {
		t.Run(failure, func(t *testing.T) {
			home := t.TempDir()
			started := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-Test-Logical-URL") != "https://platform.claude.com/v1/oauth/token" {
					t.Error("unexpected external request")
					http.Error(w, "bad", 400)
					return
				}
				var grant map[string]string
				if err := json.NewDecoder(r.Body).Decode(&grant); err != nil {
					t.Error(err)
					http.Error(w, "bad", 400)
					return
				}
				if grant["grant_type"] == "authorization_code" {
					oauthJSON(w, 200, oauthTokens())
					return
				}
				if grant["grant_type"] != "refresh_token" || grant["refresh_token"] != "private-refresh-canary" {
					t.Error("wrong rotating grant")
					http.Error(w, "bad", 400)
					return
				}
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if failure == "replacement-write-failed" {
					rotated := oauthTokens()
					rotated["access_token"] = "rotated-access-canary"
					rotated["refresh_token"] = "rotated-refresh-canary"
					oauthJSON(w, 200, rotated)
					return
				}
				// The provider has accepted this rotating grant, but the client receives
				// no token response. Only the external connection is faulted here.
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
			}))
			defer server.Close()
			defer close(release)
			p := startOAuthCommand(t, home, server.URL, "", "auth", "login", "--provider", "anthropic", "--method", "anthropic-oauth", "--interaction", "copy-code")
			completeOAuth(t, p, false)
			p.finish(t, true)
			t.Setenv("ASK_AUTH_TIME_ADVANCE", "55m")
			p = startOAuthCommand(t, home, server.URL, "", "--provider", "anthropic", "-p", "hello")
			_ = p.input.Close()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("rotating exchange did not start")
			}
			path := filepath.Join(home, "auth.json")
			fenced, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Providers map[string]struct {
					Generation   uint64 `json:"generation"`
					RefreshState *struct {
						Status     string `json:"status"`
						Generation uint64 `json:"generation"`
						AttemptID  string `json:"attemptId"`
					} `json:"refreshState"`
				} `json:"providers"`
			}
			if err = json.Unmarshal(fenced, &document); err != nil {
				t.Fatal(err)
			}
			credential := document.Providers["anthropic"]
			if credential.RefreshState == nil || credential.RefreshState.Status != "pending" || credential.RefreshState.AttemptID == "" || credential.RefreshState.Generation != credential.Generation {
				t.Fatal("exchange started without durable generation fence")
			}
			if failure == "process-killed" {
				if err = p.cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				err = p.cmd.Wait()
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("process death: %v", err)
				}
				status, ok := exit.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatalf("process did not die by SIGKILL: %v", err)
				}
				<-p.readDone
			} else {
				if failure == "replacement-write-failed" {
					if err = os.Chmod(home, 0500); err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := os.Chmod(home, 0700); err != nil {
							t.Error(err)
						}
					}()
					probe, probeErr := os.CreateTemp(home, ".commit-permission-probe-")
					if probeErr == nil {
						name := probe.Name()
						_ = probe.Close()
						_ = os.Remove(name)
						t.Fatal("owned home still allows replacement writes")
					}
					if !os.IsPermission(probeErr) {
						t.Fatalf("replacement write probe: %v", probeErr)
					}
				}
				release <- struct{}{}
				p.finish(t, false)
				if failure == "replacement-write-failed" {
					if err = os.Chmod(home, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if !strings.Contains(p.diagnostics.String(), "refresh result uncertain; sign in again") {
					t.Fatal("uncertain refresh did not request recovery")
				}
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("before restart: external requests=%d want login and one refresh", got)
			}
			restart := startOAuthCommand(t, home, server.URL, "", "--provider", "anthropic", "-p", "hello")
			restart.finish(t, false)
			if !strings.Contains(restart.diagnostics.String(), "refresh result uncertain; sign in again") {
				t.Fatal("restart did not request recovery")
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("restart reused fenced grant or sent inference: external requests=%d", got)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(fenced, after) {
				t.Fatal("restart changed the uncertain credential")
			}
		})
	}
}
