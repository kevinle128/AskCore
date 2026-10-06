package main

import (
	"AskCore/internal/settings"
	"AskCore/internal/testsupport"
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Only external requests are replaced. Logical production destinations remain
// intact through the real parser, app, store, verifier, agent, and adapters.
func runOAuthCommandHelper(args []string) int {
	now := time.Now()
	if advance, err := time.ParseDuration(os.Getenv("ASK_AUTH_TIME_ADVANCE")); err == nil {
		now = now.Add(advance)
	}
	transport := func(private bool) authCommandRT {
		return func(r *http.Request) (*http.Response, error) {
			allowed := map[string]bool{"https://platform.claude.com/v1/oauth/token": true, "https://auth.openai.com/api/accounts/oauth/token": true, "https://auth.openai.com/.well-known/jwks.json": true, "https://api.openai.com/v1/models": true, "https://auth.x.ai/oauth2/device/code": true, "https://auth.x.ai/oauth2/token": true}
			if private != allowed[r.URL.String()] {
				return nil, fmt.Errorf("unexpected external destination")
			}
			if !private && r.URL.String() != "https://api.anthropic.com/v1/messages" && r.URL.String() != "https://api.openai.com/v1/responses" && r.URL.String() != "https://api.x.ai/v1/responses" {
				return nil, fmt.Errorf("unexpected inference destination")
			}
			requestContext := r.Context()
			if os.Getenv("ASK_AUTH_IGNORE_CANCEL") == "1" {
				requestContext = context.Background()
			}
			req := r.Clone(requestContext)
			req.Header = r.Header.Clone()
			req.Header.Set("X-Test-Logical-URL", r.URL.String())
			req.Header.Set("X-Test-Time", fmt.Sprint(now.UnixNano()))
			u, err := url.Parse(os.Getenv("ASK_AUTH_EXTERNAL_SERVER"))
			if err != nil {
				return nil, err
			}
			req.URL = u
			req.Host = u.Host
			requestDone := make(chan struct{})
			if os.Getenv("ASK_AUTH_IGNORE_CANCEL") == "1" {
				go func() {
					select {
					case <-r.Context().Done():
						fmt.Fprintln(os.Stderr, "External request cancellation observed.")
					case <-requestDone:
					}
				}()
			}
			defer close(requestDone)
			response, err := http.DefaultTransport.RoundTrip(req)
			if err == nil && os.Getenv("ASK_AUTH_IGNORE_CANCEL") == "1" {
				body, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if readErr != nil {
					return nil, readErr
				}
				response.Body = io.NopCloser(bytes.NewReader(body))
			}
			return response, err
		}
	}
	return runWithDependencies(args, os.Stdin, os.Stdout, os.Stderr, runDependencies{getenv: os.Getenv, authHTTP: &http.Client{Transport: transport(true)}, inferenceHTTP: &http.Client{Transport: transport(false)}, now: func() time.Time { return now }, wait: func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		now = now.Add(d)
		return nil
	}})
}

type oauthProcess struct {
	cmd         *exec.Cmd
	input       io.WriteCloser
	lines       <-chan string
	output      bytes.Buffer
	diagnostics bytes.Buffer
	readDone    chan struct{}
}

func startOAuthCommand(t *testing.T, home, server, capture string, args ...string) *oauthProcess {
	t.Helper()
	p := &oauthProcess{readDone: make(chan struct{})}
	p.cmd = exec.Command(os.Args[0], append([]string{"-test.run=^TestAuthCommandSubprocessHelper$", "--"}, args...)...)
	for _, v := range os.Environ() {
		k := strings.SplitN(v, "=", 2)[0]
		if k != "ASK_HOME" && k != "ASK_AUTH_COMMAND_HELPER" && k != "ASK_AUTH_EXTERNAL_SERVER" && k != "ASK_CAPTURE" && !strings.HasSuffix(k, "_API_KEY") {
			p.cmd.Env = append(p.cmd.Env, v)
		}
	}
	p.cmd.Env = append(p.cmd.Env, "ASK_AUTH_COMMAND_HELPER=1", "ASK_HOME="+home, "ASK_AUTH_EXTERNAL_SERVER="+server, "ASK_CAPTURE="+capture)
	var err error
	p.input, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.cmd.Stdout = &p.output
	ch := make(chan string, 32)
	p.lines = ch
	if err = p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(p.readDone)
		defer close(ch)
		scan := bufio.NewScanner(stderr)
		for scan.Scan() {
			line := scan.Text()
			p.diagnostics.WriteString(line + "\n")
			ch <- line
		}
	}()
	t.Cleanup(func() {
		_ = p.input.Close()
		if p.cmd.ProcessState == nil {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
		}
	})
	return p
}
func (p *oauthProcess) notice(t *testing.T, prefix string) string {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				t.Fatalf("command ended before notice %s", prefix)
			}
			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-timer.C:
			t.Fatalf("no notice %s", prefix)
		}
	}
}
func (p *oauthProcess) finish(t *testing.T, success bool) string {
	t.Helper()
	_ = p.input.Close()
	err := p.cmd.Wait()
	<-p.readDone
	if (err == nil) != success {
		t.Fatalf("command success=%v: %v stdout=%s stderr=%s", success, err, p.output.String(), p.diagnostics.String())
	}
	all := p.output.String() + p.diagnostics.String()
	for _, secret := range []string{"private-access-canary", "private-refresh-canary", "private-code-canary", "private-device-canary"} {
		if strings.Contains(all, secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	return p.output.String()
}
func oauthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func oauthTokens() map[string]any {
	return map[string]any{"access_token": "private-access-canary", "refresh_token": "private-refresh-canary", "expires_in": 3600, "token_type": "Bearer", "scope": "openid resource.invoke chatgpt.tokens.use.direct user:inference api:access grok-cli:access"}
}
func completeOAuth(t *testing.T, p *oauthProcess, oversize bool) *url.URL {
	t.Helper()
	raw := p.notice(t, "https://")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	redirect := q.Get("redirect_uri")
	cb := redirect + "?" + url.Values{"state": {q.Get("state")}, "code": {"private-code-canary"}, "client_id": {"issued-client"}}.Encode()
	if strings.HasPrefix(redirect, "https://") {
		_, err = fmt.Fprintln(p.input, "private-code-canary#"+q.Get("state"))
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	client := &http.Client{Timeout: 5 * time.Second}
	if oversize {
		r, e := client.Get(redirect + "?padding=" + strings.Repeat("x", authInputLimit+1))
		if e != nil {
			t.Fatal(e)
		}
		_ = r.Body.Close()
		if r.StatusCode != 400 {
			t.Fatalf("oversize callback status %d", r.StatusCode)
		}
	}
	r, err := client.Get(cb)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("callback status %d", r.StatusCode)
	}
	return u
}
func anthropicConnectedSSE() string {
	return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"connected\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
}
func TestAuthAnthropicOAuthCommandNextPromptCaptureLogout(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	for _, interaction := range []string{"browser", "copy-code"} {
		t.Run(interaction, func(t *testing.T) {
			home := t.TempDir()
			capture := t.TempDir()
			var mu sync.Mutex
			tokens, inference := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Header.Get("X-Test-Logical-URL") {
				case "https://platform.claude.com/v1/oauth/token":
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["code"] != "private-code-canary" || body["grant_type"] != "authorization_code" || body["code_verifier"] == "" || body["state"] != body["code_verifier"] {
						t.Error("invalid token exchange")
					}
					mu.Lock()
					tokens++
					mu.Unlock()
					oauthJSON(w, 200, oauthTokens())
				case "https://api.anthropic.com/v1/messages":
					if r.Header.Get("Authorization") != "Bearer private-access-canary" || r.Header.Get("X-Api-Key") != "" {
						t.Error("wrong OAuth profile")
					}
					mu.Lock()
					inference++
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					if inference == 1 {
						_, _ = io.WriteString(w, anthropicToolSSE())
					} else {
						if !bytes.Contains(body, []byte("tool-result-canary")) || !bytes.Contains(body, []byte("tool_result")) || bytes.Contains(body, []byte("Validation failed")) {
							t.Errorf("missing real echo result: %s", body)
						}
						_, _ = io.WriteString(w, anthropicConnectedSSE())
					}
				default:
					t.Error("unexpected request")
					http.Error(w, "bad", 400)
				}
			}))
			defer server.Close()
			p := startOAuthCommand(t, home, server.URL, capture, "auth", "login", "--provider", "anthropic", "--method", "anthropic-oauth", "--interaction", interaction)
			u := completeOAuth(t, p, interaction == "browser")
			if u.Host != "claude.ai" {
				t.Fatal("wrong authorization origin")
			}
			p.finish(t, true)
			p = startOAuthCommand(t, home, server.URL, capture, "--provider", "anthropic", "--mode", "json", "-p", "hello")
			if out := p.finish(t, true); !strings.Contains(out, "connected") || !strings.Contains(out, `"type":"tool_execution_end"`) || !strings.Contains(out, `"toolName":"echo"`) || strings.Contains(out, `"isError":true`) {
				t.Fatal("missing reply")
			}
			entries, err := os.ReadDir(capture)
			if err != nil || len(entries) != 1 {
				t.Fatalf("capture: %v files=%d", err, len(entries))
			}
			data, err := os.ReadFile(filepath.Join(capture, entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"private-access-canary", "private-refresh-canary", "private-code-canary", "oauth/token"} {
				if bytes.Contains(data, []byte(secret)) {
					t.Fatal("auth secret or exchange in capture")
				}
			}
			p = startOAuthCommand(t, home, server.URL, "", "auth", "logout", "--provider", "anthropic")
			p.finish(t, true)
			p = startOAuthCommand(t, home, server.URL, "", "--provider", "anthropic", "-p", "hello")
			p.finish(t, false)
			mu.Lock()
			defer mu.Unlock()
			if tokens != 1 || inference != 2 {
				t.Fatalf("requests token=%d inference=%d", tokens, inference)
			}
		})
	}
}

func signedOAuthID(t *testing.T, key *rsa.PrivateKey, nonce, subject string) string {
	t.Helper()
	enc := func(v any) string {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	s := enc(map[string]any{"alg": "RS256", "kid": "cli-key"}) + "." + enc(map[string]any{"iss": "https://auth.openai.com", "aud": "issued-client", "sub": subject, "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()})
	hash := sha256.Sum256([]byte(s))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if e != nil {
		t.Fatal(e)
	}
	return s + "." + base64.RawURLEncoding.EncodeToString(sig)
}
func oauthJWKS(key *rsa.PrivateKey) any {
	return map[string]any{"keys": []any{map[string]any{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "cli-key", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}}
}

func responsesConnectedSSE() string {
	events := []struct{ name, data string }{
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","delta":"connected"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"connected"}]}}`},
		{"response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`},
	}
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e.name, e.data)
	}
	return b.String()
}
func TestAuthChatGPTCommandVerifiedIdentityReturningAndNewAccount(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var mu sync.Mutex
	nonce, subject, scope := "", "account-one", ""
	challenge, stableHost := "", ""
	listedModel := "gpt-5.5"
	discovery, inference, tokens := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Header.Get("X-Test-Logical-URL") {
		case "https://auth.openai.com/api/accounts/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("client_id") != "issued-client" || r.Form.Get("code") != "private-code-canary" || r.Form.Get("code_verifier") == "" {
				t.Error("invalid ChatGPT exchange")
			}
			proof := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(proof[:]) != challenge {
				t.Error("PKCE verifier does not match authorization challenge")
			}
			v := oauthTokens()
			if scope != "" {
				v["scope"] = scope
			}
			v["earliest_refresh_at"] = time.Now().Add(56 * time.Minute).Unix()
			v["id_token"] = signedOAuthID(t, key, nonce, subject)
			tokens++
			oauthJSON(w, 200, v)
		case "https://auth.openai.com/.well-known/jwks.json":
			oauthJSON(w, 200, oauthJWKS(key))
		case "https://api.openai.com/v1/models":
			if r.Header.Get("Authorization") != "Bearer private-access-canary" {
				t.Error("discovery credential")
			}
			discovery++
			models := []any{map[string]any{"slug": listedModel, "visibility": "list"}}
			if listedModel == "gpt-5.6-sol" {
				models = append(models, map[string]any{"slug": "gpt-5.5", "visibility": "hide"})
			}
			oauthJSON(w, 200, map[string]any{"models": models})
		case "https://api.openai.com/v1/responses":
			if r.Header.Get("Authorization") != "Bearer private-access-canary" {
				t.Error("inference credential")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["store"] != false {
				t.Error("ChatGPT store profile")
			}
			if body["model"] != listedModel {
				t.Error("Responses used a model outside the discovered list")
			}
			inference++
			w.Header().Set("Content-Type", "text/event-stream")
			if inference%2 == 1 {
				_, _ = io.WriteString(w, responsesToolSSE())
			} else {
				raw, _ := json.Marshal(body)
				if !bytes.Contains(raw, []byte("tool-result-canary")) || !bytes.Contains(raw, []byte("function_call_output")) || bytes.Contains(raw, []byte("Validation failed")) {
					t.Errorf("missing real echo result: %s", raw)
				}
				_, _ = io.WriteString(w, responsesConnectedSSE())
			}
		default:
			t.Error("unexpected request")
			http.Error(w, "bad", 400)
		}
	}))
	defer server.Close()
	login := func(newAccount, success bool, wantClient string) {
		t.Helper()
		args := []string{"auth", "login", "--provider", "openai", "--method", "openai-chatgpt"}
		if newAccount {
			args = append(args, "--new-account")
		}
		p := startOAuthCommand(t, home, server.URL, "", args...)
		raw := p.notice(t, "https://auth.openai.com/")
		u, e := url.Parse(raw)
		if e != nil {
			t.Fatal(e)
		}
		q := u.Query()
		if q.Get("client_id") != wantClient || !strings.HasPrefix(q.Get("ext_agent_host_id"), "urn:uuid:") || q.Get("nonce") == "" || q.Get("nonce") == q.Get("state") {
			t.Fatal("invalid authorization identity")
		}
		if stableHost == "" {
			stableHost = q.Get("ext_agent_host_id")
		} else if stableHost != q.Get("ext_agent_host_id") {
			t.Fatal("host identity changed between command processes")
		}
		mu.Lock()
		challenge = q.Get("code_challenge")
		nonce = q.Get("nonce")
		mu.Unlock()
		cb := q.Get("redirect_uri") + "?" + url.Values{"state": {q.Get("state")}, "code": {"private-code-canary"}, "client_id": {"issued-client"}}.Encode()
		r, e := http.Get(cb)
		if e != nil {
			t.Fatal(e)
		}
		_ = r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(r.StatusCode)
		}
		p.finish(t, success)
	}
	login(false, true, "dynamic_agent_client")
	login(false, true, "issued-client")
	before, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	subject = "account-two"
	mu.Unlock()
	login(false, false, "issued-client")
	after, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("account rejection replaced record")
	}
	login(true, true, "dynamic_agent_client")
	before, err = os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	scope = "openid profile"
	mu.Unlock()
	login(false, false, "issued-client")
	after, err = os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("scope rejection replaced record")
	}
	t.Setenv("ASK_AUTH_TIME_ADVANCE", "55m")
	p := startOAuthCommand(t, home, server.URL, "", "--provider", "openai", "--mode", "json", "-p", "hello")
	if out := p.finish(t, true); !strings.Contains(out, "connected") || !strings.Contains(out, `"type":"tool_execution_end"`) || !strings.Contains(out, `"toolName":"echo"`) || strings.Contains(out, `"isError":true`) {
		t.Fatal("missing next-process reply")
	}
	mu.Lock()
	listedModel = "gpt-5.6-sol"
	beforeDeniedInference := inference
	mu.Unlock()
	p = startOAuthCommand(t, home, server.URL, "", "--provider", "openai", "--model", "gpt-5.5", "-p", "hello")
	p.finish(t, false)
	if !strings.Contains(p.diagnostics.String(), "model access denied") {
		t.Fatal("hidden explicit model did not report denial")
	}
	mu.Lock()
	if inference != beforeDeniedInference {
		t.Error("hidden explicit model reached inference")
	}
	mu.Unlock()
	p = startOAuthCommand(t, home, server.URL, "", "--provider", "openai", "--model", "gpt-5.6-sol", "--mode", "json", "-p", "hello")
	if out := p.finish(t, true); !strings.Contains(out, "connected") || !strings.Contains(out, `"type":"tool_execution_end"`) || !strings.Contains(out, `"toolName":"echo"`) || strings.Contains(out, `"isError":true`) {
		t.Fatal("listed explicit model did not complete real echo turn")
	}
	t.Setenv("ASK_AUTH_TIME_ADVANCE", "")
	store, err := settings.NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	credential, revision, err := store.Read(context.Background(), "openai")
	if err != nil {
		t.Fatal(err)
	}
	credential.OAuth.Subject = ""
	if _, err = store.Replace(context.Background(), "openai", revision, credential); err != nil {
		t.Fatal(err)
	}
	p = startOAuthCommand(t, home, server.URL, "", "auth", "login", "--provider", "openai", "--method", "openai-chatgpt")
	p.finish(t, false)
	mu.Lock()
	scope = ""
	mu.Unlock()
	login(true, true, "dynamic_agent_client")
	p = startOAuthCommand(t, home, server.URL, "", "auth", "logout", "--provider", "openai")
	p.finish(t, true)
	p = startOAuthCommand(t, home, server.URL, "", "--provider", "openai", "-p", "hello")
	p.finish(t, false)
	mu.Lock()
	defer mu.Unlock()
	if tokens != 6 || discovery != 3 || inference != 4 {
		t.Fatalf("requests tokens=%d discovery=%d inference=%d", tokens, discovery, inference)
	}
}

func TestAuthXAICommandPollTimingDenialRetainsCredential(t *testing.T) {
	for _, denial := range []string{"access_denied", "authorization_denied"} {
		t.Run(denial, func(t *testing.T) {
			home := t.TempDir()
			var mu sync.Mutex
			polls := 0
			reject := false
			var timestamps []int64
			inference := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.Header.Get("X-Test-Logical-URL") {
				case "https://auth.x.ai/oauth2/device/code":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("client_id") != "b1a00492-073a-47ea-816f-4c329264a828" || !strings.Contains(r.Form.Get("scope"), "api:access") {
						t.Error("invalid device request")
					}
					var ts int64
					_, _ = fmt.Sscan(r.Header.Get("X-Test-Time"), &ts)
					timestamps = append(timestamps, ts)
					oauthJSON(w, 200, map[string]any{"device_code": "private-device-canary", "user_code": "ABCD", "verification_uri": "https://auth.x.ai/device", "expires_in": 100, "interval": 2})
				case "https://auth.x.ai/oauth2/token":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("device_code") != "private-device-canary" {
						t.Error("wrong device code")
					}
					var ts int64
					_, _ = fmt.Sscan(r.Header.Get("X-Test-Time"), &ts)
					timestamps = append(timestamps, ts)
					polls++
					if reject {
						oauthJSON(w, 400, map[string]any{"error": denial})
					} else if polls == 1 {
						oauthJSON(w, 400, map[string]any{"error": "authorization_pending"})
					} else if polls == 2 {
						oauthJSON(w, 400, map[string]any{"error": "slow_down"})
					} else {
						oauthJSON(w, 200, oauthTokens())
					}
				case "https://api.x.ai/v1/responses":
					if r.Header.Get("Authorization") != "Bearer private-access-canary" {
						t.Error("wrong xAI credential")
					}
					inference++
					w.Header().Set("Content-Type", "text/event-stream")
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					if inference == 1 {
						_, _ = io.WriteString(w, responsesToolSSE())
					} else {
						if !bytes.Contains(body, []byte("tool-result-canary")) || !bytes.Contains(body, []byte("function_call_output")) || bytes.Contains(body, []byte("Validation failed")) {
							t.Errorf("missing real echo result: %s", body)
						}
						_, _ = io.WriteString(w, responsesConnectedSSE())
					}
				default:
					t.Error("unexpected request")
					http.Error(w, "bad", 400)
				}
			}))
			defer server.Close()
			p := startOAuthCommand(t, home, server.URL, "", "auth", "login", "--provider", "xai", "--method", "xai-oauth")
			p.finish(t, true)
			mu.Lock()
			if len(timestamps) != 4 || timestamps[1]-timestamps[0] != int64(2*time.Second) || timestamps[2]-timestamps[1] != int64(2*time.Second) || timestamps[3]-timestamps[2] != int64(7*time.Second) {
				t.Error("wrong pending/slow_down timing")
			}
			reject = true
			mu.Unlock()
			before, err := os.ReadFile(filepath.Join(home, "auth.json"))
			if err != nil {
				t.Fatal(err)
			}
			p = startOAuthCommand(t, home, server.URL, "", "auth", "login", "--provider", "xai", "--method", "xai-oauth")
			p.finish(t, false)
			after, err := os.ReadFile(filepath.Join(home, "auth.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("denial replaced credential")
			}
			p = startOAuthCommand(t, home, server.URL, "", "--provider", "xai", "--mode", "json", "-p", "hello")
			if out := p.finish(t, true); !strings.Contains(out, "connected") || !strings.Contains(out, `"type":"tool_execution_end"`) || !strings.Contains(out, `"toolName":"echo"`) || strings.Contains(out, `"isError":true`) {
				t.Fatal("missing next prompt")
			}
			p = startOAuthCommand(t, home, server.URL, "", "auth", "logout", "--provider", "xai")
			p.finish(t, true)
			p = startOAuthCommand(t, home, server.URL, "", "--provider", "xai", "-p", "hello")
			p.finish(t, false)
			mu.Lock()
			defer mu.Unlock()
			if inference != 2 {
				t.Fatalf("inference requests=%d", inference)
			}
		})
	}
}

func responsesToolSSE() string {
	return `event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","arguments":""}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"text\":\"tool-result-canary\"}"}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","arguments":"{\"text\":\"tool-result-canary\"}"}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

`
}
func anthropicToolSSE() string {
	s := anthropicConnectedSSE()
	start := strings.Index(s, "event: content_block_start")
	end := strings.Index(s, "event: message_delta")
	s = s[:start] + `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"echo","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"text\":\"tool-result-canary\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

` + s[end:]
	return strings.Replace(s, "end_turn", "tool_use", 1)
}
