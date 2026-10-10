package main

import (
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/testsupport"
)

// holdProbes keeps every key check open until the returned function runs, so a
// test can look at the screen while the leader checks the key.
func (p *providerRoutes) holdProbes(t *testing.T) (release func()) {
	t.Helper()
	ch := make(chan struct{})
	p.mu.Lock()
	p.probeHold = ch
	p.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			close(ch)
			p.mu.Lock()
			p.probeHold = nil
			p.mu.Unlock()
		})
	}
	t.Cleanup(release)
	return release
}

// watchScreen reads the screen and the scrollback every few milliseconds and
// notes any time a secret shows. stop ends the watch and reports whether it saw
// one.
func (a *askTerm) watchScreen(secret string) (stop func() (seen bool)) {
	var hit atomic.Bool
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-quit:
				return
			case <-time.After(5 * time.Millisecond):
				if strings.Contains(a.screen(), secret) {
					hit.Store(true)
				}
			}
		}
	}()
	var once sync.Once
	return func() bool {
		once.Do(func() { close(quit); <-done })
		return hit.Load()
	}
}

// requireNoFileHolds fails when a file under dir other than the allowed ones
// holds the secret.
func requireNoFileHolds(t *testing.T, dir, secret string, allowed ...string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&fs.ModeSocket != 0 {
			return nil
		}
		for _, name := range allowed {
			if filepath.Base(path) == name {
				return nil
			}
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		require.NotContains(t, string(raw), secret, "the secret is in %s", path)
		return nil
	}))
}

// The key is on no row of the screen grid at any time: not while it is pasted,
// not while it is checked, not after the refusal and not after the save. The
// watch reads the grid and the scrollback continuously, and the raw byte
// stream of the process is searched at the end.
func TestInteractiveSecretNeverOnScreen(t *testing.T) {
	g := newProviderRig(t)
	key := fakeSecret("sk-test-")
	g.p.setProbe("api.openai.com", http.StatusUnauthorized)
	release := g.p.holdProbes(t)

	a := g.start(g.firstRunEnv())
	stop := a.watchScreen(key)
	a.openProviders()
	a.pickProvider("openai api")
	a.waitText("Paste your API key.")
	a.pasteSecret(key)
	a.waitRow("key", "(hidden)")
	a.write("\r")
	a.waitText("Checking the key with openai…")
	a.waitRow("key", "(hidden)")
	// Typing is ignored while the key is checked.
	a.write("more")
	a.waitRow("key", "(hidden)")
	a.requireNoSecret(key)

	release()
	a.waitText("openai refused the key: invalid_api_key (HTTP 401)")
	a.requireNoSecret(key)

	g.p.setProbe("api.openai.com", http.StatusOK)
	a.pasteSecret(key)
	a.waitRow("key", "(hidden)")
	a.write("\r")
	a.waitRow("✓ openai: API key saved.")
	a.waitFor("the editor back", func(rows []string) bool { return !containsRow(rows, "╭─ OpenAI API key") })
	a.requireNoSecret(key)
	require.False(t, stop(), "the key showed on the screen while the test watched it")
	a.quitDetached()
	a.requireNoSecret(key)
}

// After a sign-in with a fake key the key is not in the scrollback, not after a
// new session, and not after the transcript is drawn again at another width.
func TestInteractiveSecretNotInTranscript(t *testing.T) {
	g := newProviderRig(t)
	key := fakeSecret("sk-test-")
	a := g.start(g.firstRunEnv())
	a.openProviders()
	a.pickProvider("openai api")
	a.waitText("Paste your API key.")
	a.pasteSecret(key)
	a.write("\r")
	a.waitRow("✓ openai: API key saved.")
	a.requireNoSecret(key)

	a.write("/new\r")
	a.waitFor("the new session", func(rows []string) bool {
		return countRows(rows, "◢◤ ask") >= 1 && countRows(rows, "✓ openai: API key saved.") >= 1
	})
	a.resize(100, 30)
	a.waitFor("the screen drawn again", func(rows []string) bool { return hasRow(rows, "╰", "╯") })
	a.resize(termCols, termRows)
	time.Sleep(400 * time.Millisecond) // the renderer rebuilds 120 ms after the size settles
	a.requireNoSecret(key)

	// The history that the editor keeps does not hold it: up recalls nothing.
	a.write("\x1b[A")
	time.Sleep(100 * time.Millisecond)
	a.requireNoSecret(key)
	a.quitDetached()
	a.requireNoSecret(key)
}

// No key reaches the log of the leader, the stderr of the client, the event
// stream that a second client follows, or any file of the homes except the
// store, which holds it once. The leader receives the key once, and nothing
// that it sends back repeats it. The fixture sees one check with the key.
func TestInteractiveSecretNotInLogsOrEvents(t *testing.T) {
	g := newProviderRig(t)
	key := fakeSecret("sk-test-")

	// The client reaches the leader through a proxy home, so every frame it
	// sends and receives can be read.
	env := append(withoutCredentialEnv(leaderEnv(g.home)), "ASK_FAUX=0")
	for k, v := range g.f.env() {
		env = append(env, k+"="+v)
	}
	_ = openRawLeader(t, g.bin, g.home, env)
	paths, err := leader.ResolvePaths(g.home)
	require.NoError(t, err)
	proxyHome := leaderHome(t)
	require.NoError(t, os.Mkdir(proxyHome, 0o700))
	lp := newLinkProxy(t, filepath.Join(proxyHome, "leader.sock"), paths.Socket)

	a := startAskTerm(t, g.bin, proxyHome, askTermOptions{})
	a.waitText("Ask anything")
	a.waitNoModelFooter()
	ids := liveSessionIDs(t, g.bin, g.home)
	require.Len(t, ids, 1)

	watcher := startConnect(t, g.bin, g.home, noProviderEnv(), "--attach", ids[0])
	watcher.waitLine("connected", hasPrefix("connected "))
	watcher.send("/follow")
	watcher.waitLine("follow", hasPrefix("follow "))

	a.openProviders()
	a.pickProvider("openai api")
	a.waitText("Paste your API key.")
	a.pasteSecret(key)
	a.write("\r")
	a.waitRow("✓ openai: API key saved.")
	// Give the follower time to see anything that the save sent it.
	a.write("a prompt that has no model\r")
	a.waitRow("✗ Not sent: no model")
	a.write(keyCtrlC)
	time.Sleep(300 * time.Millisecond)

	require.NotContains(t, strings.Join(watcher.snapshot(), "\n"), key, "the event stream of a second client")
	require.NotContains(t, a.stderr.String(), key)
	a.requireNoSecret(key)
	toLeader, fromLeader := lp.frames()
	require.Equal(t, 1, strings.Count(toLeader, key), "the leader receives the key once")
	require.NotContains(t, fromLeader, key, "the leader never sends it back")

	// The store holds the key once. No other file of either home does.
	store, err := os.ReadFile(filepath.Join(g.home, "auth.json"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(store), key), "the store holds the key once")
	requireNoFileHolds(t, g.home, key, "auth.json")
	requireNoFileHolds(t, proxyHome, key)
	if log, err := os.ReadFile(filepath.Join(g.home, "leader.log")); err == nil {
		require.NotContains(t, string(log), key, "the leader log")
	}
	reqs := g.p.requests("api.openai.com")
	require.Len(t, reqs, 1, "the leader checked the key once")
	require.Equal(t, "Bearer "+key, reqs[0].Credential)
	a.quitDetached()
	watcher.send("/quit")
}

func TestInteractiveCopyCodeNotInLogsOrEvents(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	busy, err := net.Listen("tcp", "localhost:53692")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })
	g := newProviderRig(t)
	code := fakeSecret("fake-code-")
	access, refresh := fakeSecret("fake-access-"), fakeSecret("fake-refresh-")
	var exchanges atomic.Int32
	g.f.token = func(in map[string]string) (int, any) {
		require.Equal(t, code, in["code"])
		exchanges.Add(1)
		return http.StatusOK, map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 3600}
	}
	base := g.f.route
	g.f.route = func(r *http.Request, body []byte) (int, string, string, bool) {
		switch r.Host + r.URL.Path {
		case "api.anthropic.com/api/oauth/profile":
			return http.StatusOK, "application/json", `{"account":{"email":"fake@example.test"}}`, true
		}
		return base(r, body)
	}
	env := append(withoutCredentialEnv(leaderEnv(g.home)), g.firstRunEnv()...)
	_ = openRawLeader(t, g.bin, g.home, env)
	paths, err := leader.ResolvePaths(g.home)
	require.NoError(t, err)
	proxyHome := leaderHome(t)
	require.NoError(t, os.Mkdir(proxyHome, 0o700))
	lp := newLinkProxy(t, filepath.Join(proxyHome, "leader.sock"), paths.Socket)
	a := startAskTerm(t, g.bin, proxyHome, askTermOptions{})
	a.waitNoModelFooter()
	ids := liveSessionIDs(t, g.bin, g.home)
	require.Len(t, ids, 1)
	watcher := startConnect(t, g.bin, g.home, noProviderEnv(), "--attach", ids[0])
	watcher.waitLine("connected", hasPrefix("connected "))
	watcher.send("/follow")
	watcher.waitLine("follow", hasPrefix("follow "))
	a.openProviders()
	a.pickProvider("claude")
	a.waitRow("code", "paste the code; it stays hidden")
	link := regexp.MustCompile(`https://claude\.ai/oauth/authorize\?[^\x1b\x07\s]+`).FindString(a.rawOutput())
	u, err := url.Parse(link)
	require.NoError(t, err)
	require.NotEmpty(t, u.Query().Get("state"))
	input := code + "#" + u.Query().Get("state")
	a.pasteSecret(input)
	a.write("\r")
	a.waitRow("✓ anthropic: signed in as fake@example.test.")
	a.quitDetached()
	watcher.send("/quit")
	toLeader, fromLeader := lp.frames()
	require.Equal(t, 1, strings.Count(toLeader, input), "the leader receives the code once")
	require.Equal(t, int32(1), exchanges.Load(), "the leader exchanges the code once")
	for _, secret := range []string{code, input, access, refresh} {
		a.requireNoSecret(secret)
		require.NotContains(t, fromLeader, secret)
		require.NotContains(t, strings.Join(watcher.snapshot(), "\n"), secret)
		requireNoFileHolds(t, g.home, secret, "auth.json")
		requireNoFileHolds(t, proxyHome, secret)
	}
}

// ---- TEA_TRACE ----

func traceFile(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("/tmp", "ask-trace-")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

func TestInteractiveTraceWarningShown(t *testing.T) {
	g := newProviderRig(t)
	trace := traceFile(t)
	a := g.start(g.firstRunEnv("TEA_TRACE=" + trace))
	const warning = "TEA_TRACE is on: typed and pasted text, keys included, is written to"
	a.waitRow(warning)
	a.waitRow(trace + ".")
	require.Equal(t, 1, countRows(a.rows(), warning), "the warning shows once at the start:\n%s", a.screen())

	// The key box shows it again above the key input. No key is pasted: the
	// trace file is the stated exception and the test keeps it empty of secrets.
	a.openProviders()
	a.pickProvider("openai api")
	a.waitText("Paste your API key.")
	a.waitFor("the warning in the box", func(rows []string) bool { return countRows(rows, warning) == 2 })
	rows := a.rows()
	input, last := -1, -1
	for i, r := range rows {
		if strings.Contains(r, "key  paste or type the key; it stays hidden") {
			input = i
		}
		if strings.Contains(r, warning) {
			last = i
		}
	}
	require.Greater(t, input, last, "the warning is above the key input:\n%s", a.screen())
	a.write(keyEsc)
	a.quitDetached()
}

func TestInteractiveNoTraceWarningWhenUnset(t *testing.T) {
	g := newProviderRig(t)
	// An empty value is no value: Bubble Tea writes no trace.
	a := g.start(g.firstRunEnv("TEA_TRACE="))
	a.openProviders()
	a.pickProvider("openai api")
	a.waitText("Paste your API key.")
	a.waitRow("key", "paste or type the key")
	require.False(t, containsRow(a.rows(), "TEA_TRACE"), "no row mentions the trace:\n%s", a.screen())
	require.NotContains(t, a.rawOutput(), "TEA_TRACE")
	a.write(keyEsc)
	a.quitDetached()
}
