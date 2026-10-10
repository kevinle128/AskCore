package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/testsupport"
	"AskCore/pkg/protocol"
)

// Keys and paste markers as the terminal sends them.
const (
	keyDelete  = "\x1b[3~"
	keyDown    = "\x1b[B"
	pasteBegin = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

const (
	tokenPlanHost = "token-plan.ap-southeast-1.maas.aliyuncs.com"
	deviceCode    = "WDJB-MJHT"
	deviceAccount = "dev@example.com"
	// chatGPTAccessToken is the access token of the ChatGPT sign-in; the model
	// discovery of that account shares a URL with the key check of the API.
	chatGPTAccessToken = "private-access-canary"
)

// fakeSecret returns a fake key or code that is new for each run, so no text on
// the screen can collide with it.
func fakeSecret(prefix string) string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// routeHit is one request that reached the provider routes.
type routeHit struct {
	Host, Path, Credential string
}

// providerRoutes answers the destinations of the provider setup flows on the
// provider fixture: the key checks of the three providers that have one, the
// device authorization of xAI, the token and key set of ChatGPT, and the
// messages endpoint of the Alibaba Token Plan.
type providerRoutes struct {
	t *testing.T

	mu   sync.Mutex
	hits []routeHit
	// probeStatus is the status of a key check by host. A host that is not
	// there answers 200.
	probeStatus map[string]int
	// probeHold, when set, keeps every key check open until it is closed.
	probeHold chan struct{}

	deviceStarts int
	devicePolls  int
	approved     bool
	// deviceExpiresIn is the lifetime of a device code in seconds.
	deviceExpiresIn int

	chatKey   *rsa.PrivateKey
	chatNonce string
}

func (p *providerRoutes) record(h routeHit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hits = append(p.hits, h)
}

// requests returns the hits of one host.
func (p *providerRoutes) requests(host string) []routeHit {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []routeHit
	for _, h := range p.hits {
		if h.Host == host {
			out = append(out, h)
		}
	}
	return out
}

func (p *providerRoutes) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.hits)
}

func (p *providerRoutes) setProbe(host string, status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probeStatus[host] = status
}

func (p *providerRoutes) approve() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.approved = true
}

func (p *providerRoutes) counters() (starts, polls int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.deviceStarts, p.devicePolls
}

func unsignedJWT(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]any{"alg": "none"}) + "." + enc(claims) + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
}

func (p *providerRoutes) route(r *http.Request, body []byte) (int, string, string, bool) {
	cred := r.Header.Get("Authorization")
	if cred == "" {
		cred = r.Header.Get("X-Api-Key")
	}
	p.record(routeHit{Host: r.Host, Path: r.URL.Path, Credential: cred})
	const jsonType = "application/json"
	if r.Host == tokenPlanHost {
		return http.StatusUnauthorized, jsonType, `{"type":"error","error":{"type":"authentication_error","code":"invalid_api_key","message":"bad key"}}`, true
	}
	switch r.Host + r.URL.Path {
	case "api.openai.com/v1/models", "api.x.ai/v1/models", "api.anthropic.com/v1/models":
		if cred == "Bearer "+chatGPTAccessToken {
			return 200, jsonType, `{"models":[{"slug":"gpt-5.5","visibility":"list"}]}`, true
		}
		p.mu.Lock()
		hold := p.probeHold
		status, ok := p.probeStatus[r.Host]
		p.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-time.After(60 * time.Second):
			}
		}
		if !ok {
			status = 200
		}
		if status == 200 {
			return 200, jsonType, `{"data":[{"id":"fixture-model"}]}`, true
		}
		return status, jsonType, `{"error":{"message":"The key is not valid.","type":"invalid_request_error","code":"invalid_api_key"}}`, true
	case "auth.x.ai/oauth2/device/code":
		p.mu.Lock()
		p.deviceStarts++
		expires := p.deviceExpiresIn
		p.mu.Unlock()
		if expires == 0 {
			expires = 600
		}
		out, _ := json.Marshal(map[string]any{
			"device_code": "private-device-canary", "user_code": deviceCode,
			"verification_uri": "https://auth.x.ai/device", "expires_in": expires, "interval": 1,
		})
		return 200, jsonType, string(out), true
	case "auth.x.ai/oauth2/token":
		p.mu.Lock()
		p.devicePolls++
		approved := p.approved
		p.mu.Unlock()
		if !approved {
			return http.StatusBadRequest, jsonType, `{"error":"authorization_pending"}`, true
		}
		out, _ := json.Marshal(map[string]any{
			"access_token": "private-access-canary", "refresh_token": "private-refresh-canary", "expires_in": 3600,
			"token_type": "Bearer", "scope": "openid profile email offline_access grok-cli:access api:access",
			"id_token": unsignedJWT(map[string]any{"email": deviceAccount, "sub": "xai-account"}),
		})
		return 200, jsonType, string(out), true
	case "auth.openai.com/api/accounts/oauth/token":
		p.mu.Lock()
		key, nonce := p.chatKey, p.chatNonce
		p.mu.Unlock()
		tokens := oauthTokens()
		tokens["earliest_refresh_at"] = time.Now().Add(56 * time.Minute).Unix()
		tokens["id_token"] = signedOAuthID(p.t, key, nonce, "account-one")
		out, _ := json.Marshal(tokens)
		return 200, jsonType, string(out), true
	case "auth.openai.com/.well-known/jwks.json":
		p.mu.Lock()
		key := p.chatKey
		p.mu.Unlock()
		out, _ := json.Marshal(oauthJWKS(key))
		return 200, jsonType, string(out), true
	}
	return 0, "", "", false
}

// providerRig is one isolated home with its fixture. Every `ask` of a test
// shares the leader that the first one starts.
type providerRig struct {
	t         *testing.T
	bin, home string
	f         *providerFixture
	p         *providerRoutes
}

func newProviderRig(t *testing.T) *providerRig {
	t.Helper()
	bin, home := newE2EHome(t)
	f := newProviderFixture(t)
	p := &providerRoutes{t: t, probeStatus: map[string]int{}}
	f.route = p.route
	// A run that reaches the model service of the fixture is a failure of the
	// test unless the test sets its own answer.
	f.inference = func(int, *http.Request, []byte) (int, string, string) {
		t.Errorf("an unexpected request reached the model service")
		return 500, "application/json", `{"type":"error","error":{"type":"api_error","message":"unexpected"}}`
	}
	f.token = func(map[string]string) (int, any) {
		t.Errorf("an unexpected request reached the token service")
		return 400, map[string]any{"error": "invalid_request"}
	}
	return &providerRig{t: t, bin: bin, home: home, f: f, p: p}
}

// env is the environment of an `ask` that reaches the fixture. The faux demo
// provider stays on, as in the other interactive tests.
func (g *providerRig) env(extra ...string) []string {
	return append(envPairs(g.f.env()), extra...)
}

// firstRunEnv turns the demo provider off, so a home without a credential has
// no model.
func (g *providerRig) firstRunEnv(extra ...string) []string {
	return append(g.env("ASK_FAUX=0"), extra...)
}

// start runs `ask` on a terminal and waits for the leader footer.
func (g *providerRig) start(env []string) *askTerm {
	g.t.Helper()
	a := startAskTerm(g.t, g.bin, g.home, askTermOptions{}, env...)
	a.waitText("Ask anything")
	requireLeaderFooter(g.t, a, g.bin, g.home)
	return a
}

// saveKey stores an API key through the supported host login. The leader does
// not check it.
func (g *providerRig) saveKey(provider, key string) {
	g.t.Helper()
	loginKey(g.t, g.home, provider, key)
}

// chatGPTLogin signs in to ChatGPT with the production `ask auth login`
// command against the fixture, as a user would do before the TUI runs.
func (g *providerRig) chatGPTLogin() {
	t := g.t
	t.Helper()
	testsupport.LockOAuthPorts(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	g.p.mu.Lock()
	g.p.chatKey = key
	g.p.mu.Unlock()

	cmd := exec.Command(acpBinary(t), "auth", "login", "--provider", "openai", "--method", "openai-chatgpt")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "ASK_HOME=" + g.home}
	for k, v := range g.f.env() {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	proc := startOAuthProcess(t, cmd)
	raw := proc.notice(t, "https://auth.openai.com/")
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	g.p.mu.Lock()
	g.p.chatNonce = q.Get("nonce")
	g.p.mu.Unlock()
	cb := q.Get("redirect_uri") + "?" + url.Values{"state": {q.Get("state")}, "code": {"private-code-canary"}, "client_id": {"issued-client"}}.Encode()
	resp, err := http.Get(cb)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	proc.finish(t, true)
}

// ---- terminal helpers ----

func (a *askTerm) openProviders() {
	a.t.Helper()
	a.write("/providers\r")
	a.waitRow("╭─ Providers")
	a.waitText("Type to search")
}

// pickProvider filters the provider box and presses enter on the first match.
func (a *askTerm) pickProvider(filter string) {
	a.t.Helper()
	a.write(filter)
	a.waitRow("⌕ " + filter)
	a.write("\r")
}

func (a *askTerm) pasteSecret(v string) { a.write(pasteBegin + v + pasteEnd) }

// requireNoSecret fails when a secret is on the screen, in the scrollback, in
// any byte the process wrote, or on its stderr.
func (a *askTerm) requireNoSecret(secrets ...string) {
	a.t.Helper()
	screen, raw, errOut := a.screen(), a.rawOutput(), a.stderr.String()
	for _, s := range secrets {
		require.NotContains(a.t, screen, s, "the secret is on the screen or in the scrollback")
		require.NotContains(a.t, raw, s, "the secret was written to the terminal")
		require.NotContains(a.t, errOut, s, "the secret is on stderr")
	}
}

func (a *askTerm) waitNoModelFooter() {
	a.t.Helper()
	a.waitFor("the no model footer", func(rows []string) bool { return hasRow(rows, "leader ", "no model") })
}

// resize changes the terminal size. The process has no controlling terminal,
// so it learns the size from the in-band report of the terminal and from the
// window change signal.
func (a *askTerm) resize(cols, rows int) {
	a.t.Helper()
	require.NoError(a.t, pty.Setsize(a.master, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}))
	a.mu.Lock()
	a.emu.Resize(cols, rows)
	a.mu.Unlock()
	require.NoError(a.t, a.cmd.Process.Signal(syscall.SIGWINCH))
}

// ---- first run ----

func TestInteractiveFirstRunNoModel(t *testing.T) {
	g := newProviderRig(t)
	a := g.start(g.firstRunEnv())
	a.waitText("Add a provider with /providers")
	a.waitNoModelFooter()
	rows := a.rows()
	require.False(t, containsRow(rows, "▱"), "no context bar:\n%s", a.screen())
	require.False(t, hasRowPrefix(rows, "  model  "), "no model line:\n%s", a.screen())
	require.Len(t, leaderProcesses(g.bin), 1)
	require.Zero(t, g.f.calls())
	a.quitDetached()
}

func TestInteractiveFirstPromptNotSent(t *testing.T) {
	g := newProviderRig(t)
	a := g.start(g.firstRunEnv())
	a.waitNoModelFooter()
	a.write("hello there")
	a.waitRow("│ ❯ hello there")
	a.write("\r")
	a.waitRow("✗ Not sent: no model")
	a.waitText("Add a provider with /providers")
	rows := a.rows()
	require.True(t, hasRowPrefix(rows, "│ ❯ hello there"), "the draft stays in the editor:\n%s", a.screen())
	require.False(t, hasRowPrefix(rows, "  ❯ hello there"), "nothing went to the scrollback:\n%s", a.screen())

	// Nothing reached the leader or the model service.
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	st, err := leader.Status(ctx, e2eConnect(g.home, g.bin))
	require.NoError(t, err)
	require.Zero(t, st.ActiveRuns)
	ids := liveSessionIDs(t, g.bin, g.home)
	require.Len(t, ids, 1)
	c, _ := openLeaderClient(t, g.bin, g.home)
	_, err = c.Attach(ctx, ids[0])
	require.NoError(t, err)
	state, err := c.State(ctx)
	require.NoError(t, err)
	require.Zero(t, state.MessageCount, "no prompt reached the session")
	require.Empty(t, state.Steering)
	require.Empty(t, state.FollowUp)
	require.Zero(t, g.f.calls())
	a.write(keyCtrlC) // clears the draft
	a.quitDetached()
}

// ---- provider list ----

func TestInteractiveProvidersList(t *testing.T) {
	g := newProviderRig(t)
	g.saveKey("alibaba-token-plan", fakeSecret("tp-test-"))
	g.chatGPTLogin()
	a := g.start(g.firstRunEnv("ANTHROPIC_API_KEY=" + fakeSecret("sk-ant-test-")))
	a.openProviders()

	names := []string{
		"Alibaba Token Plan API key", "Claude Pro/Max sign-in", "Anthropic API key",
		"ChatGPT Plus/Pro sign-in", "OpenAI API key", "xAI Grok sign-in", "xAI API key",
	}
	rows := a.rows()
	last := -1
	for _, name := range names {
		idx := -1
		for i, r := range rows {
			if strings.HasPrefix(r, "│") && strings.Contains(r, name) {
				idx = i
			}
		}
		require.Greater(t, idx, last, "row %q in order:\n%s", name, a.screen())
		last = idx
	}
	require.True(t, hasRow(rows, "Alibaba Token Plan API key", "api key"), a.screen())
	require.True(t, hasRow(rows, "Anthropic API key", "env ANTHROPIC_API_KEY"), a.screen())
	require.True(t, hasRow(rows, "ChatGPT Plus/Pro sign-in", "signed in"), a.screen())
	for _, bare := range []string{"Claude Pro/Max sign-in", "OpenAI API key", "xAI Grok sign-in", "xAI API key"} {
		for _, r := range rows {
			if strings.Contains(r, bare) {
				require.NotContains(t, r, "signed in")
				require.NotContains(t, r, "env ")
				require.NotContains(t, r, "api key")
			}
		}
	}
	search := -1
	for i, r := range rows {
		if strings.Contains(r, "⌕ Type to search") {
			search = i
		}
	}
	require.Greater(t, search, last, "the search row is the last row of the box:\n%s", a.screen())
	require.True(t, hasRow(rows, "esc close", "your draft is kept"))
	a.write(keyEsc)
	a.waitFor("the editor back", func(rows []string) bool {
		return containsRow(rows, "Ask anything") && !containsRow(rows, "╭─ Providers")
	})
	a.quitDetached()
}

// ---- API key ----

func TestInteractiveAPIKeyRefused(t *testing.T) {
	g := newProviderRig(t)
	g.p.setProbe("api.openai.com", http.StatusUnauthorized)
	key := fakeSecret("sk-test-")
	a := g.start(g.firstRunEnv())
	a.openProviders()
	a.pickProvider("openai api")
	a.waitText("Paste your API key.")
	a.pasteSecret(key)
	a.waitRow("key", "(hidden)")
	a.write("\r")
	a.waitRow("openai refused the key: invalid_api_key (HTTP 401)")
	a.waitText("Nothing was saved.")
	rows := a.rows()
	require.False(t, containsRow(rows, "Checking the key"), "the check ended:\n%s", a.screen())
	require.True(t, hasRow(rows, "key", "paste a new key"), "the input is empty again:\n%s", a.screen())
	a.requireNoSecret(key)
	require.Len(t, g.p.requests("api.openai.com"), 1)
	require.Equal(t, "Bearer "+key, g.p.requests("api.openai.com")[0].Credential)

	// Nothing was saved: the row has no status and the store holds no key.
	a.write(keyEsc)
	a.waitRow("⊘ Sign-in to openai cancelled. Nothing was saved.")
	a.openProviders()
	require.True(t, hasRow(a.rows(), "OpenAI API key"), a.screen())
	require.False(t, hasRow(a.rows(), "OpenAI API key", "api key"))
	if raw, err := os.ReadFile(filepath.Join(g.home, "auth.json")); err == nil {
		require.NotContains(t, string(raw), key)
	}
	a.write(keyEsc)
	a.quitDetached()
}

func TestInteractiveAPIKeySaved(t *testing.T) {
	g := newProviderRig(t)
	key := fakeSecret("sk-test-")
	a := g.start(g.firstRunEnv())
	a.waitNoModelFooter()
	a.openProviders()
	a.pickProvider("openai api")
	a.waitText("Paste your API key.")
	a.pasteSecret(key)
	a.write("\r")
	a.waitRow("✓ openai: API key saved.")
	a.waitText("Choose a model with /model.")
	require.Regexp(t, regexp.MustCompile(`openai: API key saved\. \d+ models? ready\.`), a.screen())
	a.waitFor("the editor back", func(rows []string) bool {
		return containsRow(rows, "Ask anything") && !containsRow(rows, "╭─ Providers")
	})
	// A saved key does not choose a model by itself.
	a.waitNoModelFooter()
	a.requireNoSecret(key)

	reqs := g.p.requests("api.openai.com")
	require.Len(t, reqs, 1, "one check")
	require.Equal(t, "Bearer "+key, reqs[0].Credential)
	info, err := os.Stat(filepath.Join(g.home, "auth.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	a.openProviders()
	a.waitFor("the saved row", func(rows []string) bool { return hasRow(rows, "OpenAI API key", "api key") })
	a.write(keyEsc)
	a.quitDetached()
}

func TestInteractiveTokenPlanKeySavedUnchecked(t *testing.T) {
	g := newProviderRig(t)
	key := fakeSecret("tp-test-")
	a := g.start(g.firstRunEnv())
	a.waitNoModelFooter()
	a.openProviders()
	a.pickProvider("alibaba")
	a.waitText("Paste your API key.")
	a.waitText("This provider cannot be checked before use; the key is saved as it is.")
	a.pasteSecret(key)
	a.waitRow("key", "(hidden)")
	a.write("\r")
	a.waitRow("✓ alibaba-token-plan: Saved. This provider cannot be checked before use.")
	a.waitText("Choose a model with /model.")
	require.NotContains(t, a.rawOutput(), "Checking the key", "no check row at any time")
	a.waitFor("the rail line", func([]string) bool {
		return strings.Contains(a.rawOutput(), "alibaba-token-plan key saved")
	})
	require.Zero(t, g.p.total(), "no request left the leader")
	require.Zero(t, g.f.calls())
	a.requireNoSecret(key)

	a.openProviders()
	a.waitFor("the saved row", func(rows []string) bool { return hasRow(rows, "Alibaba Token Plan API key", "api key") })
	a.write(keyEsc)
	a.waitFor("the editor back", func(rows []string) bool { return !containsRow(rows, "╭─ Providers") })

	a.write(keyCtrlL)
	a.waitText("Select model")
	a.waitRow("● alibaba-token-plan")
	a.waitRow("deepseek")
	a.write(keyEsc)
	a.quitDetached()
}

func TestInteractiveTokenPlanBadKeyAtFirstPrompt(t *testing.T) {
	g := newProviderRig(t)
	key := fakeSecret("tp-test-")
	a := g.start(g.firstRunEnv())
	a.openProviders()
	a.pickProvider("alibaba")
	a.waitText("This provider cannot be checked before use;")
	a.pasteSecret(key)
	a.write("\r")
	a.waitRow("✓ alibaba-token-plan: Saved. This provider cannot be checked before use.")
	a.waitNoModelFooter()
	require.Zero(t, g.p.total(), "the key save makes no provider request")
	a.write(keyCtrlL)
	a.waitText("Select model")
	a.write("deepseek")
	a.waitRow("│", "deepseek")
	a.write("\r")
	a.waitFor("the model in the footer", func(rows []string) bool { return hasRow(rows, "leader ", "deepseek") })

	a.write("say hello\r")
	a.waitRow("✗ Error (auth): alibaba-token-plan refused the key (HTTP 401).")
	a.waitRow("Replace the key with /providers. Then send again.")
	a.waitFor("the run ended", func(rows []string) bool { return !containsRow(rows, "Working…") })
	require.NotEmpty(t, g.p.requests(tokenPlanHost), "the leader asked the Token Plan service")
	a.requireNoSecret(key)
	a.quitDetached()
}

// ---- start line ----

func TestInteractiveStartLineShowsPair(t *testing.T) {
	g := newProviderRig(t)
	a := g.start(g.firstRunEnv("ALIBABA_TOKEN_PLAN_API_KEY=" + fakeSecret("tp-test-")))
	pair := regexp.MustCompile(`^  model  (alibaba-token-plan/\S+)`)
	var line string
	var head int
	a.waitFor("the start line", func(rows []string) bool {
		for i, r := range rows {
			if strings.HasPrefix(r, "◢◤ ask") && i+1 < len(rows) {
				if m := pair.FindStringSubmatch(rows[i+1]); m != nil {
					line, head = m[1], i
					return true
				}
			}
		}
		return false
	})
	rows := a.rows()
	require.Empty(t, rows[head+2], "one empty row follows the line:\n%s", a.screen())
	require.True(t, strings.HasPrefix(rows[head+3], "╭"), "the editor follows:\n%s", a.screen())
	bare := strings.TrimPrefix(line, "alibaba-token-plan/")
	a.waitFor("the bare model in the footer", func(rows []string) bool {
		return hasRow(rows, "leader ", bare) && !hasRow(rows, "leader ", "alibaba-token-plan/")
	})
	require.Equal(t, "deepseek-v4.1-flash", bare)
	a.quitDetached()
}

// ---- device sign-in ----

// startGrokLogin opens the provider box and starts the device sign-in of xAI.
func (a *askTerm) startGrokLogin() {
	a.t.Helper()
	a.openProviders()
	a.pickProvider("grok")
	a.waitRow("1. Open this page on any device:")
	a.waitRow(deviceCode)
}

func TestInteractiveOAuthSignedInNamesAccount(t *testing.T) {
	g := newProviderRig(t)
	a := g.start(g.firstRunEnv())
	a.startGrokLogin()
	a.waitText("The code expires in")
	a.waitText("https://auth.x.ai/device")
	starts, _ := g.p.counters()
	require.Equal(t, 1, starts)
	g.p.approve()
	a.waitRow("✓ xai: signed in as " + deviceAccount + ".")
	a.waitFor("the box closed", func(rows []string) bool { return !containsRow(rows, "1. Open this page") })
	require.Regexp(t, regexp.MustCompile(`signed in as dev@example\.com\. \d+ models? ready\.`), a.screen())
	for _, secret := range []string{"private-access-canary", "private-refresh-canary", "private-device-canary"} {
		a.requireNoSecret(secret)
	}
	a.quitDetached()
}

func TestInteractiveModelListAfterSignIn(t *testing.T) {
	g := newProviderRig(t)
	a := g.start(g.firstRunEnv())
	a.write(keyCtrlL)
	a.waitText("Use /providers to add providers.")
	a.write(keyEsc)
	a.waitFor("the selector closed", func(rows []string) bool { return !containsRow(rows, "Select model") })

	a.startGrokLogin()
	g.p.approve()
	a.waitRow("✓ xai: signed in as " + deviceAccount + ".")
	a.write(keyCtrlL)
	a.waitText("Select model")
	a.waitRow("● xai")
	rows := a.rows()
	require.False(t, hasRow(rows, "● openai"), "a provider that is not signed in has no group:\n%s", a.screen())
	require.False(t, hasRow(rows, "● anthropic"), a.screen())
	require.False(t, hasRow(rows, "● alibaba-token-plan"), a.screen())
	a.write(keyEsc)
	a.quitDetached()
}

func TestInteractiveProviderBusyTwoClients(t *testing.T) {
	g := newProviderRig(t)
	first := g.start(g.firstRunEnv())
	first.startGrokLogin()

	second := startAskTerm(t, g.bin, g.home, askTermOptions{}, g.firstRunEnv()...)
	second.waitText("Ask anything")
	requireLeaderFooter(t, second, g.bin, g.home)
	second.openProviders()
	second.pickProvider("grok")
	second.waitRow("Could not start: another client is signing in to xai.")
	starts, _ := g.p.counters()
	require.Equal(t, 1, starts, "the second client started nothing")
	second.write(keyEsc)

	first.write(keyEsc)
	first.waitRow("⊘ Sign-in to xai cancelled. Nothing was saved.")
	first.quitDetached()
	second.waitFor("the editor", func(rows []string) bool { return !containsRow(rows, "Could not start") })
	second.quitDetached()
}

func TestInteractiveLoginLeaderLost(t *testing.T) {
	g := newProviderRig(t)
	a := g.start(g.firstRunEnv())
	a.startGrokLogin()
	oldLeader := requireLeaderFooter(t, a, g.bin, g.home)

	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	res, err := leader.Stop(ctx, e2eConnect(g.home, g.bin), 20*time.Second)
	require.NoError(t, err)
	require.True(t, res.WasRunning)

	a.waitText("The sign-in stopped because the leader is gone.")
	// The client starts a new leader while it reconnects. The sign-in is not
	// resumed on it.
	a.waitFor("the new leader", func(rows []string) bool {
		st, err := leader.Status(ctx, e2eConnect(g.home, g.bin))
		return err == nil && st.InstanceID != oldLeader && containsRow(rows, "is gone: the leader restarted.")
	})
	require.False(t, containsRow(a.rows(), "Waiting for approval"), a.screen())
	starts, _ := g.p.counters()
	require.Equal(t, 1, starts, "no second device authorization")
	a.write(keyEsc)
	a.waitFor("the box closed", func(rows []string) bool { return !containsRow(rows, "The sign-in stopped") })
	a.waitFor("the editor", func(rows []string) bool { return containsRow(rows, "Ask anything") })
	a.quitDetached()
}

func TestInteractiveLoginResizeKeepsLogin(t *testing.T) {
	g := newProviderRig(t)
	a := g.start(g.firstRunEnv())
	a.resize(120, 40)
	a.write("/providers\r")
	a.waitText("Type to search")
	a.pickProvider("grok")
	a.waitRow("1. Open this page on any device:")
	a.waitRow(deviceCode)

	a.resize(80, 24)
	a.waitFor("the box drawn at 80 columns", func(rows []string) bool {
		for _, r := range rows {
			if strings.HasPrefix(r, "╭─ xAI Grok sign-in") && strings.HasSuffix(r, "╮") && utf8.RuneCountInString(r) == 80 {
				return true
			}
		}
		return false
	})
	require.True(t, containsRow(a.rows(), deviceCode), a.screen())
	require.True(t, containsRow(a.rows(), "Waiting for approval"), "the wait stays:\n%s", a.screen())
	time.Sleep(1500 * time.Millisecond)
	starts, _ := g.p.counters()
	require.Equal(t, 1, starts, "the resize sent no new request")

	g.p.approve()
	a.waitRow("✓ xai: signed in as " + deviceAccount + ".")
	a.quitDetached()
}

// ---- removal ----

func TestInteractiveRemoveProvider(t *testing.T) {
	g := newProviderRig(t)
	key := fakeSecret("tp-test-")
	g.saveKey("alibaba-token-plan", key)
	a := g.start(g.firstRunEnv())
	a.openProviders()
	a.write("alibaba")
	a.waitRow("⌕ alibaba")
	a.write(keyDelete)
	a.waitRow("Remove Alibaba Token Plan API key?")
	a.waitText("This deletes the saved sign-in for alibaba-token-plan from this leader.")
	a.write(keyEsc)
	a.waitFor("the boxes closed", func(rows []string) bool {
		return containsRow(rows, "Ask anything") && !containsRow(rows, "Remove Alibaba")
	})
	a.openProviders()
	require.True(t, hasRow(a.rows(), "Alibaba Token Plan API key", "api key"), "esc kept the credential:\n%s", a.screen())

	a.write("alibaba")
	a.waitRow("⌕ alibaba")
	a.write(keyDelete)
	a.waitRow("Remove Alibaba Token Plan API key?")
	a.write("\r")
	a.waitRow("✓ Removed the saved credential for alibaba-token-plan.")
	raw, err := os.ReadFile(filepath.Join(g.home, "auth.json"))
	if err == nil {
		require.NotContains(t, string(raw), key)
	}
	a.write(keyEsc)
	a.openProviders()
	a.waitFor("the row without status", func(rows []string) bool {
		return hasRow(rows, "Alibaba Token Plan API key") && !hasRow(rows, "Alibaba Token Plan API key", "api key")
	})
	a.write(keyEsc)
	a.quitDetached()
}

func TestInteractiveRemoveCurrentProvider(t *testing.T) {
	g := newProviderRig(t)
	g.saveKey("alibaba-token-plan", fakeSecret("tp-test-"))
	// A second provider stays usable, so the rail asks for a model and does not
	// ask for a provider.
	env := g.firstRunEnv("ANTHROPIC_API_KEY=" + fakeSecret("sk-ant-test-"))
	first := g.start(env)
	first.waitFor("a model in the footer", func(rows []string) bool {
		return hasRow(rows, "leader ", "deepseek") && !containsRow(rows, "no model")
	})
	second := startAskTerm(t, g.bin, g.home, askTermOptions{}, env...)
	second.waitText("Ask anything")
	requireLeaderFooter(t, second, g.bin, g.home)
	second.waitFor("a model in the footer", func(rows []string) bool { return hasRow(rows, "leader ", "deepseek") })

	first.openProviders()
	first.write("alibaba")
	first.waitRow("⌕ alibaba")
	first.write(keyDelete)
	first.waitRow("Remove Alibaba Token Plan API key?")
	first.write("\r")
	first.waitRow("✓ Removed the saved credential for alibaba-token-plan.")
	first.waitRow("is no longer available.")
	first.waitText("with /model.")
	first.waitNoModelFooter()
	second.waitNoModelFooter()
	first.waitText("No model. Choose one with /model")

	first.write("hello after removal\r")
	first.waitRow("✗ Not sent: no model")
	first.waitText("Choose one with /model")
	second.write("hello after removal\r")
	second.waitRow("✗ Not sent: no model")
	require.Zero(t, g.f.calls())
	require.Empty(t, g.p.requests(tokenPlanHost), "no prompt reached the provider")
	first.write(keyCtrlC)
	second.write(keyCtrlC)
	first.quitDetached()
	second.quitDetached()
}

func TestInteractiveRemovalCancelsOtherClientSignIn(t *testing.T) {
	g := newProviderRig(t)
	remover := g.start(g.firstRunEnv())
	// The remover signs in first, so a credential exists to remove.
	remover.startGrokLogin()
	g.p.approve()
	remover.waitRow("✓ xai: signed in as " + deviceAccount + ".")
	g.p.mu.Lock()
	g.p.approved = false
	g.p.mu.Unlock()

	signer := startAskTerm(t, g.bin, g.home, askTermOptions{}, g.firstRunEnv()...)
	signer.waitText("Ask anything")
	requireLeaderFooter(t, signer, g.bin, g.home)
	signer.openProviders()
	signer.waitFor("the signed in row", func(rows []string) bool { return hasRow(rows, "xAI Grok sign-in", "signed in") })
	signer.write("grok")
	signer.waitRow("⌕ grok")
	signer.write("\r")
	signer.waitText("Sign in again")
	signer.write("\r")
	signer.waitRow("1. Open this page on any device:")
	startsBefore, _ := g.p.counters()
	require.Equal(t, 2, startsBefore)

	remover.openProviders()
	remover.write("grok")
	remover.waitRow("⌕ grok")
	remover.write(keyDelete)
	remover.waitRow("Remove xAI Grok sign-in?")
	remover.write("\r")
	remover.waitRow("✓ Removed the saved credential for xai.")

	signer.waitRow("✗ Sign-in cancelled: another client removed xai.")
	signer.write(keyEsc)
	signer.waitRow("⊘ Sign-in to xai cancelled: another client removed xai.")
	// The sign-in of the signer asks the token service no more.
	_, polls := g.p.counters()
	time.Sleep(2500 * time.Millisecond)
	_, later := g.p.counters()
	require.Equal(t, polls, later, "no token request after the removal")
	remover.write(keyEsc)
	remover.quitDetached()
	signer.quitDetached()
}

// ---- default model ----

func TestInteractiveDefaultModelSurvivesNewSession(t *testing.T) {
	g := newProviderRig(t)
	// The Token Plan model leads the catalog, so the session starts on it and
	// the choice of the OpenAI model is a change.
	env := g.firstRunEnv("OPENAI_API_KEY="+fakeSecret("sk-test-"), "ALIBABA_TOKEN_PLAN_API_KEY="+fakeSecret("tp-test-"))
	a := g.start(env)
	a.selectModel("gpt-5.5", "gpt-5.5", "gpt-5.5")
	a.waitText("Default model: openai/gpt-5.5")
	before := liveSessionIDs(t, g.bin, g.home)
	a.write("/new\r")
	a.waitFor("the new session starts on the saved default", func(rows []string) bool {
		return countRows(rows, "  model  openai/gpt-5.5") == 2
	})
	after := liveSessionIDs(t, g.bin, g.home)
	require.Len(t, after, len(before)+1, "the command made a new leader session")
	a.quitDetached()

	b := g.start(env)
	b.waitFor("the start line with the saved default", func(rows []string) bool {
		return hasRow(rows, "  model  openai/gpt-5.5")
	})
	b.waitFor("the footer", func(rows []string) bool { return hasRow(rows, "leader ", "gpt-5.5") })
	info, err := os.Stat(filepath.Join(g.home, "settings.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	b.quitDetached()
}

func TestInteractiveDemoWorksAfterNativeDefault(t *testing.T) {
	g := newProviderRig(t)
	g.saveKey("alibaba-token-plan", fakeSecret("tp-test-"))
	a := g.start(g.env())
	a.selectModel("deepseek", "deepseek", "deepseek")
	a.waitText("Default model: alibaba-token-plan/deepseek")
	a.quitDetached()

	b := g.start(g.env())
	b.waitRow("  model  alibaba-token-plan/deepseek")
	b.selectModel("faux-1", "faux-1", "faux-1")
	b.write("say hello\r")
	b.waitReplyMeta("faux-1")
	require.NotContains(t, b.screen(), "unknown model API")
	require.Zero(t, g.p.total(), "the demo must not send a provider request")
	b.quitDetached()
}

func TestInteractiveFallbackNoticeWhenDefaultRemoved(t *testing.T) {
	g := newProviderRig(t)
	g.saveKey("alibaba-token-plan", fakeSecret("tp-test-"))
	g.saveKey("openai", fakeSecret("sk-test-"))
	a := g.start(g.firstRunEnv())
	a.selectModel("gpt-5.5", "gpt-5.5", "gpt-5.5")
	a.waitText("Default model: openai/gpt-5.5")

	a.openProviders()
	a.write("openai api")
	a.waitRow("⌕ openai api")
	a.write(keyDelete)
	a.waitRow("Remove OpenAI API key?")
	a.write("\r")
	a.waitRow("✓ Removed the saved credential for openai.")
	a.write(keyEsc)
	a.waitNoModelFooter()
	a.quitDetached()

	b := g.start(g.firstRunEnv())
	b.waitFor("the start line of the next choice", func(rows []string) bool {
		return hasRow(rows, "  model  alibaba-token-plan/")
	})
	b.waitRow("Default model openai/gpt-5.5 is not usable (no credential).")
	b.waitFor("the footer", func(rows []string) bool { return hasRow(rows, "leader ", "deepseek") })
	b.quitDetached()
}

func TestInteractiveProvidersChangedSecondClient(t *testing.T) {
	g := newProviderRig(t)
	driver := g.start(g.firstRunEnv())
	observer := startAskTerm(t, g.bin, g.home, askTermOptions{}, g.firstRunEnv()...)
	observer.waitText("Ask anything")
	requireLeaderFooter(t, observer, g.bin, g.home)
	observer.openProviders()
	observer.write(strings.Repeat(keyDown, 5)) // the xAI Grok row
	observer.waitFor("the selection on the Grok row", func(rows []string) bool {
		return hasRowPrefix(rows, "│ ❯ ● xAI Grok sign-in")
	})

	driver.startGrokLogin()
	g.p.approve()
	driver.waitRow("✓ xai: signed in as " + deviceAccount + ".")

	observer.waitRow("Providers changed in the leader: xai credential saved")
	observer.waitFor("the row reads signed in", func(rows []string) bool {
		return hasRowPrefix(rows, "│ ❯ ● xAI Grok sign-in") && hasRow(rows, "xAI Grok sign-in", "signed in")
	})
	require.False(t, containsRow(observer.rows(), "✓ xai: signed in"), "no note in the scrollback of the other client:\n%s", observer.screen())
	observer.write(keyEsc)
	observer.quitDetached()
	driver.quitDetached()
}

// ---- ask connect: a follow-up with no model ----

func TestConnectFollowUpWithoutModelRefused(t *testing.T) {
	bin, home := newE2EHome(t)
	env := leaderEnvWithoutFaux(home)

	// The line client of `ask connect` has no follow-up verb, so the request
	// goes over the same leader link with the raw call helper.
	clean := startConnect(t, bin, home, noProviderEnv())
	clean.waitLine("connected", hasPrefix("connected "))
	sid := clean.sessionID()

	// The raw client takes the idle session of the connect client and sends the
	// two kinds of queued input.
	c := openRawLeader(t, bin, home, env)
	rawCall[protocol.ACPAttachResult](t, c, protocol.ACPAttach, map[string]any{"sessionId": sid})
	rawCall[map[string]any](t, c, protocol.ACPTake, map[string]any{"sessionId": sid})
	_, f := c.call(protocol.ACPFollowUp, e2eInput(sid, "later"))
	require.NotNil(t, f, "the follow-up is refused")
	require.Equal(t, "no_model", f.Kind)
	require.Equal(t, protocol.ACPCodeNoModel, f.Code)
	_, f = c.call(protocol.ACPSteer, e2eInput(sid, "now"))
	require.NotNil(t, f)
	require.Equal(t, "no_model", f.Kind)

	st := rawCall[protocol.ACPStateResult](t, c, protocol.ACPState, map[string]any{"sessionId": sid})
	require.Empty(t, st.FollowUp, "nothing is queued")
	require.Empty(t, st.Steering)
	require.False(t, st.Running)
	require.Zero(t, clean.count("update "))
}

// ---- fixture ----

// The routes answer through the proxy with the temporary CA, as the leader
// reaches them, and a host that is not listed is refused.
func TestProviderFixtureAnswersProviderRoutes(t *testing.T) {
	g := newProviderRig(t)
	pem, err := os.ReadFile(g.f.certFile)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(pem))
	proxy, err := url.Parse(g.f.server.URL)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		Proxy: http.ProxyURL(proxy), TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	do := func(method, target, body string, header map[string]string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, target, strings.NewReader(body))
		require.NoError(t, err)
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(out)
	}

	status, _ := do("GET", "https://api.openai.com/v1/models", "", map[string]string{"Authorization": "Bearer fake-one"})
	require.Equal(t, 200, status)
	g.p.setProbe("api.x.ai", 401)
	status, body := do("GET", "https://api.x.ai/v1/models", "", map[string]string{"Authorization": "Bearer fake-two"})
	require.Equal(t, 401, status)
	require.Contains(t, body, `"code":"invalid_api_key"`)
	status, _ = do("GET", "https://api.anthropic.com/v1/models", "", map[string]string{"X-Api-Key": "fake-three"})
	require.Equal(t, 200, status)
	require.Equal(t, "fake-three", g.p.requests("api.anthropic.com")[0].Credential)

	status, body = do("POST", "https://auth.x.ai/oauth2/device/code", "client_id=x", nil)
	require.Equal(t, 200, status)
	require.Contains(t, body, deviceCode)
	status, body = do("POST", "https://auth.x.ai/oauth2/token", "grant_type=x", nil)
	require.Equal(t, 400, status)
	require.Contains(t, body, "authorization_pending")
	g.p.approve()
	status, body = do("POST", "https://auth.x.ai/oauth2/token", "grant_type=x", nil)
	require.Equal(t, 200, status)
	require.Contains(t, body, "id_token")
	starts, polls := g.p.counters()
	require.Equal(t, 1, starts)
	require.Equal(t, 2, polls)

	status, _ = do("POST", "https://"+tokenPlanHost+"/apps/anthropic/v1/messages", "{}", nil)
	require.Equal(t, 401, status)
	require.Len(t, g.p.requests(tokenPlanHost), 1)

	// A destination that the fixture does not list never gets a connection.
	_, err = client.Get("https://example.org/")
	require.Error(t, err)
}
