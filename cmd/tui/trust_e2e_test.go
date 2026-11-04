package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/leader"
	"AskCore/internal/providers"
	"AskCore/internal/tui"
	"AskCore/internal/tuiclient"
	"AskCore/pkg/protocol"
)

// These tests run the built ask binary with a private Ask home and private
// project folders: ask -p, ask settings show, ask acp, ask connect and the
// leader that a client starts over its Unix socket. The child environment is
// closed, so a credential of the developer cannot reach a child. The home holds
// a fake credential in auth.json, and no output of a child may hold its value.

// trustRig is one Ask home and the processes that the test starts on it.
type trustRig struct {
	t      *testing.T
	bin    string
	home   string
	user   string // HOME of the children
	secret string // the fake credential in auth.json

	mu      sync.Mutex
	outputs []string // every stdout and stderr of a child, for the secret scan
}

func newTrustRig(t *testing.T) *trustRig {
	t.Helper()
	bin := acpBinary(t)
	// A Unix socket path has a short limit, so the home is under /tmp.
	home := leaderHome(t)
	require.NoError(t, os.Mkdir(home, 0o700))
	user := filepath.Join(filepath.Dir(home), "user")
	require.NoError(t, os.Mkdir(user, 0o700))
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	r := &trustRig{t: t, bin: bin, home: home, user: user, secret: "xai-trust-e2e-" + hex.EncodeToString(b)}
	// The credential is for a provider that no test selects, so no child calls it.
	auth := `{"schemaVersion":1,"revision":1,"providers":{"xai":{"method":"api-key","apiKey":"` + r.secret + `"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), []byte(auth), 0o600))
	// The leader stop is registered first, so it runs after every client of
	// the test is closed. Then no leader of this home and no socket remain.
	t.Cleanup(func() {
		cmd := exec.Command(bin, "leader", "stop")
		cmd.Env = r.env()
		out, _ := cmd.CombinedOutput()
		r.record(string(out))
		paths, err := leader.ResolvePaths(home)
		if assert.NoError(t, err) {
			assert.Eventually(t, func() bool {
				_, err := os.Lstat(paths.Socket)
				return errors.Is(err, fs.ErrNotExist)
			}, e2eWait, 20*time.Millisecond, "the leader socket of the test home is gone")
		}
		for _, row := range leaderProcesses(bin) {
			assert.NotContains(t, row, home, "no leader of the test home runs")
		}
		r.scanSecret()
	})
	return r
}

// env is the closed environment of every child: the faux provider, no model
// list refresh, and no variable of the developer except PATH.
func (r *trustRig) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + r.user, "ASK_HOME=" + r.home, "ASK_FAUX=1",
		"TZ=Asia/Saigon", disableModelRefreshEnv + "=1",
	}
}

func (r *trustRig) record(out ...string) {
	r.mu.Lock()
	r.outputs = append(r.outputs, out...)
	r.mu.Unlock()
}

// scanSecret fails when the fake credential is in an output of a child or in
// the leader log.
func (r *trustRig) scanSecret() {
	r.mu.Lock()
	all := strings.Join(r.outputs, "\n")
	r.mu.Unlock()
	if log, err := os.ReadFile(filepath.Join(r.home, "leader.log")); err == nil {
		all += string(log)
	}
	assert.NotContains(r.t, all, r.secret, "a child wrote the credential")
}

// run runs the built ask in dir and returns its output and exit code.
func (r *trustRig) run(dir string, argv ...string) result {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), e2eWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, argv...)
	cmd.Env = r.env()
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		r.t.Fatalf("run %v: %v", argv, err)
	}
	r.record(out.String(), errb.String())
	return result{out.String(), errb.String(), code}
}

// show runs ask settings show --json in dir.
func (r *trustRig) show(dir string, flags ...string) settingsDump {
	r.t.Helper()
	got := r.run(dir, append([]string{"settings", "show", "--json"}, flags...)...)
	require.Equal(r.t, 0, got.code, got.stderr)
	require.Empty(r.t, got.stderr)
	var d settingsDump
	require.NoError(r.t, json.Unmarshal([]byte(got.stdout), &d), got.stdout)
	return d
}

func (r *trustRig) writeUser(content string) {
	r.t.Helper()
	require.NoError(r.t, os.WriteFile(filepath.Join(r.home, "settings.json"), []byte(content), 0o600))
}

func (r *trustRig) writeTrust(content string) {
	r.t.Helper()
	require.NoError(r.t, os.WriteFile(filepath.Join(r.home, "trust.json"), []byte(content), 0o600))
}

// trustStore reads trust.json; nil when it does not exist.
func (r *trustRig) trustStore() map[string]bool {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.home, "trust.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(r.t, err)
	var m map[string]bool
	require.NoError(r.t, json.Unmarshal(b, &m))
	return m
}

func (r *trustRig) requireNoTrustStore() {
	r.t.Helper()
	_, err := os.Lstat(filepath.Join(r.home, "trust.json"))
	require.True(r.t, errors.Is(err, fs.ErrNotExist), "trust.json must not exist: %v", err)
}

// client opens the TUI client implementation on the leader of the home. The
// first client starts the leader from the built binary.
func (r *trustRig) client() *tuiclient.Client {
	r.t.Helper()
	paths, err := leader.ResolvePaths(r.home)
	require.NoError(r.t, err)
	cfg := leader.ConnectConfig{
		Paths:     paths,
		Hello:     protocol.LeaderRegister{ClientKind: "e2e", Build: "e2e"},
		CallerExe: r.bin,
		Env:       r.env(),
		Warn:      func(string) {},
		Deadline:  e2eWait,
	}
	c, err := tuiclient.Open(context.Background(), tuiclient.Config{
		Dial:           tuiclient.LeaderDial(cfg),
		ClientName:     "ask-test",
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
	})
	require.NoError(r.t, err)
	collect(r.t, c)
	r.t.Cleanup(func() { _ = c.Close() })
	return c
}

func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), clientWait)
}

// project makes a project folder with the given files and returns its
// canonical root.
func trustProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeFiles(t, root, files)
	return root
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if strings.HasSuffix(name, "/") {
			require.NoError(t, os.MkdirAll(path, 0o755))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

// treeHash records the name, mode and content of every entry under root, so a
// test can show that a run wrote nothing in a project.
func treeHash(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		v := info.Mode().String()
		if d.Type().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			v += " " + hex.EncodeToString(sum[:])
		}
		out[path] = v
		return nil
	}))
	return out
}

func fileBytes(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func untrustedWarning(root string) string {
	return "Warning: project settings ignored: " + root + " is not trusted. Use --approve, or save a decision in the TUI with /trust.\n"
}

func deniedKeyWarning(path, key string) string {
	return "Warning: settings: " + path + ": " + key + ": a project file cannot set this key; it is ignored\n"
}

func settingOf(t *testing.T, d settingsDump, key string) settingsValue {
	t.Helper()
	for _, s := range d.Settings {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no setting %q in %+v", key, d.Settings)
	return settingsValue{}
}

// anthropicDefault is a model id of a provider with no credential in the test
// home. A run whose default is this model warns and uses the faux model, so the
// warning shows which layer set the default.
func anthropicDefault() string { return acp.ModelID(providers.AnthropicSonnet46()) }

func notUsableWarning(t *testing.T, id string) string {
	return "Warning: default model " + id + " is not usable (no credential); using " + fauxModelID(t) + ".\n"
}

// An untrusted project: ask -p warns once, reads no project file, writes no
// trust.json and nothing in the project. --approve and --no-approve change the
// trust of one run only.
func TestTrustE2EPrintUntrustedProject(t *testing.T) {
	r := newTrustRig(t)
	root := trustProject(t, map[string]string{
		".ask/settings.json": `{"defaultModel":"` + anthropicDefault() + `","thinkingFold":"collapsed","env":{"HIDDEN":"x"}}`,
	})
	projectFile := filepath.Join(root, ".ask", "settings.json")
	r.writeUser(`{"defaultModel":"` + fauxModelID(t) + `"}`)
	before := treeHash(t, root)

	got := r.run(root, "-p", "hello")
	assert.Equal(t, result{"hello\n", untrustedWarning(root), 0}, got, "one warning; the user default applies")
	d := r.show(root)
	assert.Equal(t, settingsProject{Root: root, Layer: projectLayerUntrusted, Source: "no_ui", Gated: []string{".ask/settings.json"}}, d.Project)
	assert.Equal(t, settingsValue{Key: "defaultModel", Value: fauxModelID(t), Source: "user"}, settingOf(t, d, "defaultModel"))
	assert.Equal(t, "default", settingOf(t, d, "thinkingFold").Source)
	assert.Empty(t, d.Diagnostics, "an untrusted project file gives no diagnostic")

	for _, flag := range []string{"--approve", "-a"} {
		got = r.run(root, flag, "-p", "hello")
		assert.Equal(t, 0, got.code, flag)
		assert.Equal(t, "hello\n", got.stdout, flag)
		assert.Equal(t, deniedKeyWarning(projectFile, "env")+notUsableWarning(t, anthropicDefault()), got.stderr,
			"%s: the project default applies and the denied key is dropped once", flag)
		assert.NotContains(t, got.stderr, "HIDDEN")
	}
	d = r.show(root, "--approve")
	assert.Equal(t, projectLayerLoaded, d.Project.Layer)
	assert.Equal(t, "flag", d.Project.Source)
	assert.Equal(t, settingsValue{Key: "defaultModel", Value: anthropicDefault(), Source: "project"}, settingOf(t, d, "defaultModel"))
	assert.Equal(t, settingsValue{Key: "thinkingFold", Value: "collapsed", Source: "project"}, settingOf(t, d, "thinkingFold"))
	require.Len(t, d.Diagnostics, 1)
	assert.Equal(t, settingsDiagnostic{Layer: "project", Path: projectFile, Key: "env", Text: "a project file cannot set this key; it is ignored"}, d.Diagnostics[0])

	// The user default "always" trusts the project; --no-approve denies it for one run.
	r.writeUser(`{"defaultModel":"` + fauxModelID(t) + `","defaultProjectTrust":"always"}`)
	got = r.run(root, "-p", "hello")
	assert.Equal(t, 0, got.code)
	assert.Equal(t, deniedKeyWarning(projectFile, "env")+notUsableWarning(t, anthropicDefault()), got.stderr)
	assert.Equal(t, "default_always", r.show(root).Project.Source)
	got = r.run(root, "--no-approve", "-p", "hello")
	assert.Equal(t, result{"hello\n", untrustedWarning(root), 0}, got)
	assert.Equal(t, "flag", r.show(root, "--no-approve").Project.Source)
	got = r.run(root, "-na", "-a", "-p", "hello")
	assert.Equal(t, result{"", "Error: use only one of --approve and --no-approve\n", 1}, got)

	r.requireNoTrustStore()
	assert.Equal(t, before, treeHash(t, root), "no run wrote in the project")
}

// defaultProjectTrust counts only in the user file. A project file cannot set
// it, also when the project is trusted.
func TestTrustE2EDefaultProjectTrustUserFileOnly(t *testing.T) {
	r := newTrustRig(t)
	root := trustProject(t, map[string]string{".ask/settings.json": `{"defaultProjectTrust":"always","thinkingFold":"collapsed"}`})
	projectFile := filepath.Join(root, ".ask", "settings.json")

	got := r.run(root, "-p", "hello")
	assert.Equal(t, result{"hello\n", untrustedWarning(root), 0}, got, "the project value does not trust the project")
	d := r.show(root)
	assert.Equal(t, "no_ui", d.Project.Source)
	assert.Equal(t, settingsValue{Key: "defaultProjectTrust", Value: "ask", Source: "default"}, settingOf(t, d, "defaultProjectTrust"))

	got = r.run(root, "--approve", "-p", "hello")
	assert.Equal(t, result{"hello\n", deniedKeyWarning(projectFile, "defaultProjectTrust"), 0}, got)
	d = r.show(root, "--approve")
	assert.Equal(t, settingsValue{Key: "defaultProjectTrust", Value: "ask", Source: "default"}, settingOf(t, d, "defaultProjectTrust"))
	assert.Equal(t, settingsValue{Key: "thinkingFold", Value: "collapsed", Source: "project"}, settingOf(t, d, "thinkingFold"))

	r.writeUser(`{"defaultProjectTrust":"never"}`)
	got = r.run(root, "-p", "hello")
	assert.Equal(t, result{"hello\n", untrustedWarning(root), 0}, got)
	assert.Equal(t, "default_never", r.show(root).Project.Source)

	r.writeUser(`{"defaultProjectTrust":"always"}`)
	got = r.run(root, "-p", "hello")
	assert.Equal(t, result{"hello\n", deniedKeyWarning(projectFile, "defaultProjectTrust"), 0}, got)
	assert.Equal(t, "default_always", r.show(root).Project.Source)
	r.requireNoTrustStore()
}

// A saved decision applies through the nearest saved folder, and the folder
// is the canonical path also when the cwd is a symbolic link.
func TestTrustE2ESavedDecisionNearestAncestor(t *testing.T) {
	r := newTrustRig(t)
	parent := trustProject(t, nil)
	child := filepath.Join(parent, "child")
	writeFiles(t, child, map[string]string{".agents/skills/": ""})
	links, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	link := filepath.Join(links, "link")
	require.NoError(t, os.Symlink(child, link))
	before := treeHash(t, parent)

	cases := []struct {
		name    string
		store   map[string]bool
		trusted bool
		source  string
		entry   string
	}{
		{"parent trusted", map[string]bool{parent: true}, true, "saved_ancestor", parent},
		{"child beats parent", map[string]bool{parent: true, child: false}, false, "saved", child},
		{"child trusted under a distrusted parent", map[string]bool{parent: false, child: true}, true, "saved", child},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.store)
		require.NoError(t, err)
		r.writeTrust(string(b))
		for _, cwd := range []string{child, link} {
			d := r.show(cwd)
			assert.Equal(t, child, d.Project.Root, "%s, cwd %s: the root is canonical", tc.name, cwd)
			assert.Equal(t, tc.trusted, d.Project.Trusted, "%s, cwd %s", tc.name, cwd)
			assert.Equal(t, tc.source, d.Project.Source, "%s, cwd %s", tc.name, cwd)
			assert.Equal(t, tc.entry, d.Project.Entry, "%s, cwd %s", tc.name, cwd)
			got := r.run(cwd, "-p", "hello")
			want := ""
			if !tc.trusted {
				want = untrustedWarning(child)
			}
			assert.Equal(t, result{"hello\n", want, 0}, got, "%s, cwd %s", tc.name, cwd)
		}
		assert.Equal(t, string(b), fileBytes(t, filepath.Join(r.home, "trust.json")), "a read does not change trust.json")
	}
	assert.Equal(t, before, treeHash(t, parent))
}

// A plain folder is trusted with no question and saves nothing. A folder whose
// .ask is the Ask home has no project layer.
func TestTrustE2EPlainFolderAndAskHome(t *testing.T) {
	r := newTrustRig(t)
	plain := trustProject(t, map[string]string{"AGENTS.md": "# notes\n", "CLAUDE.md": "# notes\n"})
	got := r.run(plain, "-p", "hello")
	assert.Equal(t, result{"hello\n", "", 0}, got)
	d := r.show(plain)
	assert.True(t, d.Project.Trusted)
	assert.Equal(t, "no_gated_files", d.Project.Source)
	r.requireNoTrustStore()

	// An Ask home named .ask in the cwd is the user layer, not a project. This
	// test starts no leader, so the rig can move to the second home.
	base := trustProject(t, nil)
	r.home = filepath.Join(base, ".ask")
	require.NoError(t, os.Mkdir(r.home, 0o700))
	r.writeUser(`{"thinkingFold":"truncated"}`)
	got = r.run(base, "-p", "hello")
	assert.Equal(t, result{"hello\n", "", 0}, got)
	d = r.show(base)
	assert.Equal(t, projectLayerNone, d.Project.Layer)
	assert.Equal(t, settingsValue{Key: "thinkingFold", Value: "truncated", Source: "user"}, settingOf(t, d, "thinkingFold"))
	assert.Empty(t, d.Diagnostics)
	r.requireNoTrustStore()
}

// A corrupt trust.json makes every project untrusted with one warning, also
// with defaultProjectTrust "always". Nothing crashes and nothing overwrites it.
func TestTrustE2ECorruptTrustStore(t *testing.T) {
	r := newTrustRig(t)
	root := trustProject(t, map[string]string{".ask/settings.json": `{"thinkingFold":"collapsed"}`})
	r.writeUser(`{"defaultProjectTrust":"always"}`)
	const bad = `{"broken`
	r.writeTrust(bad)
	storePath := filepath.Join(r.home, "trust.json")

	got := r.run(root, "-p", "hello")
	assert.Equal(t, 0, got.code)
	assert.Equal(t, "hello\n", got.stdout)
	lines := strings.Split(strings.TrimSuffix(got.stderr, "\n"), "\n")
	require.Len(t, lines, 1, "%q", got.stderr)
	assert.Contains(t, lines[0], storePath)
	assert.NotContains(t, lines[0], "broken")
	assert.Equal(t, "store_error", r.show(root).Project.Source)

	// The leader refuses a grant and keeps the bytes; a session starts untrusted.
	c := r.client()
	ctx, cancel := callCtx()
	defer cancel()
	st, err := c.TrustState(ctx, root)
	require.NoError(t, err)
	assert.False(t, st.Trusted)
	assert.Equal(t, "store_error", st.Source)
	assert.Contains(t, st.Warning, storePath)
	_, err = c.SetTrust(ctx, protocol.ACPTrustSetRequest{Cwd: root, Target: protocol.ACPTrustTargetFolder, Decision: protocol.ACPTrustDecisionTrust})
	require.Error(t, err, "a grant on a corrupt store is refused")
	_, err = c.NewSession(ctx, root, tui.NewSessionOptions{})
	require.NoError(t, err)
	state, err := c.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: "store_error"}, state.Trust)
	assert.Equal(t, bad, fileBytes(t, storePath))
}

// A corrupt project or user settings file is reported with its path and is
// never overwritten, also by a default model save.
func TestTrustE2ECorruptSettingsNeverOverwritten(t *testing.T) {
	r := newTrustRig(t)
	const badProject = `{"defaultModel":`
	root := trustProject(t, map[string]string{".ask/settings.json": badProject})
	projectFile := filepath.Join(root, ".ask", "settings.json")
	r.writeTrust(`{"` + root + `":true}`)
	before := treeHash(t, root)

	got := r.run(root, "-p", "hello")
	assert.Equal(t, 0, got.code)
	assert.Equal(t, "hello\n", got.stdout)
	lines := strings.Split(strings.TrimSuffix(got.stderr, "\n"), "\n")
	require.Len(t, lines, 1, "%q", got.stderr)
	assert.True(t, strings.HasPrefix(lines[0], "Warning: settings: "+projectFile), lines[0])
	d := r.show(root)
	require.Len(t, d.Diagnostics, 1)
	assert.Equal(t, projectFile, d.Diagnostics[0].Path)

	c := r.client()
	ctx, cancel := callCtx()
	defer cancel()
	_, err := c.NewSession(ctx, root, tui.NewSessionOptions{})
	require.NoError(t, err)
	state, err := c.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: "saved"}, state.Trust)
	require.Len(t, state.Diagnostics, 1)
	assert.Equal(t, projectFile, state.Diagnostics[0].Path)
	res, err := c.SetModel(ctx, fauxModelID(t), true)
	require.NoError(t, err)
	assert.True(t, res.DefaultSaved, "a corrupt project file sets no default, so the user save happens: %+v", res)
	assert.Equal(t, before, treeHash(t, root), "the corrupt project file keeps its bytes")

	// A corrupt user file refuses the save and keeps its bytes.
	const badUser = `{"defaultModel":`
	r.writeUser(badUser)
	res, err = c.SetModel(ctx, fauxModelID(t), true)
	require.NoError(t, err)
	assert.Equal(t, protocol.ACPDefaultUnsafeFile, res.DefaultNotSaved)
	assert.False(t, res.DefaultSaved)
	assert.Equal(t, badUser, fileBytes(t, filepath.Join(r.home, "settings.json")))
	assert.Equal(t, before, treeHash(t, root))
}

// The leader owns the decision: two clients on one leader see the same state,
// a grant from one applies to the next session of the other, an old client
// (ask connect, no trust calls) gets an untrusted session, and an implicit
// trust is never saved.
func TestTrustE2ELeaderOwnsDecision(t *testing.T) {
	r := newTrustRig(t)
	root := trustProject(t, map[string]string{
		".ask/settings.json": `{"defaultModel":"` + anthropicDefault() + `","env":{"HIDDEN":"x"}}`,
	})
	projectFile := filepath.Join(root, ".ask", "settings.json")
	// The user file sets no default model, so a save of the faux model would
	// change the user file if the project did not shadow the key.
	userSettings := `{"thinkingFold":"truncated"}`
	r.writeUser(userSettings)
	before := treeHash(t, root)

	a := r.client()
	b := r.client()
	require.Equal(t, a.Info().InstanceID, b.Info().InstanceID, "one leader for both clients")
	ctx, cancel := callCtx()
	defer cancel()
	stA, err := a.TrustState(ctx, root)
	require.NoError(t, err)
	stB, err := b.TrustState(ctx, root)
	require.NoError(t, err)
	assert.Equal(t, stA, stB)
	assert.Equal(t, protocol.ACPTrustStateResult{Root: root, Source: "prompt", NeedsDecision: true, Gated: []string{".ask/settings.json"}}, stA)

	// The old client: ask connect sends no trust call and no override.
	conn := r.run(root, "connect")
	require.Equal(t, 0, conn.code, conn.stderr)
	var sid string
	for _, line := range strings.Split(conn.stdout, "\n") {
		if strings.HasPrefix(line, "session id=") {
			sid = field(line, "id")
		}
	}
	require.NotEmpty(t, sid, conn.stdout)
	_, err = b.Attach(ctx, sid)
	require.NoError(t, err)
	state, err := b.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: "no_ui"}, state.Trust)
	assert.Empty(t, state.Diagnostics, "the untrusted project file was not read")
	assert.Nil(t, state.ModelFallback, "the project default does not apply")
	assert.Equal(t, "truncated", state.ThinkingFold, "the user layer applies")
	r.requireNoTrustStore()

	// A grant from one client is the decision of the next session of the other.
	set, err := a.SetTrust(ctx, protocol.ACPTrustSetRequest{Cwd: root, Target: protocol.ACPTrustTargetFolder, Decision: protocol.ACPTrustDecisionTrust})
	require.NoError(t, err)
	assert.True(t, set.Trusted)
	assert.Equal(t, "saved", set.Source)
	assert.Equal(t, map[string]bool{root: true}, r.trustStore())
	stB, err = b.TrustState(ctx, root)
	require.NoError(t, err)
	assert.Equal(t, protocol.ACPTrustStateResult{Root: root, Trusted: true, Source: "saved", Gated: []string{".ask/settings.json"}, EntryPath: root}, stB)
	_, err = b.NewSession(ctx, root, tui.NewSessionOptions{})
	require.NoError(t, err)
	state, err = b.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: "saved"}, state.Trust)
	assert.Equal(t, []protocol.ACPDiagnostic{{Layer: "project", Path: projectFile, Key: "env", Text: "a project file cannot set this key; it is ignored"}},
		state.Diagnostics, "the denied key is dropped once and the session starts")
	require.NotNil(t, state.ModelFallback, "the project default applies")
	assert.Equal(t, anthropicDefault(), state.ModelFallback.SavedDefault)

	// A user default save that the trusted project shadows is refused.
	res, err := b.SetModel(ctx, fauxModelID(t), true)
	require.NoError(t, err)
	assert.Equal(t, protocol.ACPDefaultShadowed, res.DefaultNotSaved, "%+v", res)
	assert.Equal(t, userSettings, fileBytes(t, filepath.Join(r.home, "settings.json")))

	// The leader re-reads trust.json at each session: a decision that another
	// process saved applies to the next session.
	r.writeTrust(`{"` + root + `":false}`)
	_, err = a.NewSession(ctx, root, tui.NewSessionOptions{})
	require.NoError(t, err)
	state, err = a.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: "saved"}, state.Trust)
	_, err = a.NewSession(ctx, root, tui.NewSessionOptions{Trust: protocol.ACPTrustApprove})
	require.NoError(t, err)
	state, err = a.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: "flag"}, state.Trust, "a session override saves nothing")
	assert.Equal(t, map[string]bool{root: false}, r.trustStore())

	// An implicit trust is never saved: a plain folder that gets a gated file
	// is untrusted at the next session.
	plain := trustProject(t, nil)
	_, err = a.NewSession(ctx, plain, tui.NewSessionOptions{})
	require.NoError(t, err)
	state, err = a.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: "no_gated_files"}, state.Trust)
	writeFiles(t, plain, map[string]string{".ask/settings.json": `{}`})
	_, err = a.NewSession(ctx, plain, tui.NewSessionOptions{})
	require.NoError(t, err)
	state, err = a.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: "no_ui"}, state.Trust)
	assert.Equal(t, map[string]bool{root: false}, r.trustStore())
	assert.Equal(t, before, treeHash(t, root), "the leader wrote nothing in the project")
}

// ask acp serves no trust methods and takes the trust flags instead.
func TestTrustE2EACP(t *testing.T) {
	r := newTrustRig(t)
	root := trustProject(t, map[string]string{".ask/settings.json": `{"thinkingFold":"collapsed"}`})
	newSession := func(p *e2ePeer) protocol.ACPStateResult {
		var res struct {
			SessionID string `json:"sessionId"`
		}
		p.ok("session/new", map[string]any{"cwd": root, "mcpServers": []any{}}, &res)
		require.NotEmpty(t, res.SessionID)
		var st protocol.ACPStateResult
		p.ok(protocol.ACPState, e2eSession(res.SessionID), &st)
		return st
	}

	p := startE2E(t, e2eOptions{home: r.home})
	p.initialize()
	for _, method := range []string{protocol.ACPTrustState, protocol.ACPTrustSet} {
		f := p.call(method, map[string]any{"cwd": root, "target": "folder", "decision": "trust"})
		assert.Equal(t, string(protocol.ACPErrUnsupported), f.errKind(t), method)
	}
	st := newSession(p)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: "no_ui"}, st.Trust)
	assert.NotEqual(t, "collapsed", st.ThinkingFold)
	p.noSecret(r.secret)

	p = startE2E(t, e2eOptions{home: r.home, args: []string{"--approve"}})
	p.initialize()
	st = newSession(p)
	assert.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: "flag"}, st.Trust)
	assert.Equal(t, "collapsed", st.ThinkingFold, "the project layer is loaded")
	p.noSecret(r.secret)
	r.requireNoTrustStore()
}
