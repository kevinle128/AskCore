package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

// trustFixture is a real leader with trust requests on, two usable providers
// and a log that collects the session warnings.
type trustFixture struct {
	t    *testing.T
	home string
	c    *routingClient

	mu   sync.Mutex
	warn []string
}

func newTrustFixture(t *testing.T, edit func(*LeaderParams)) *trustFixture {
	t.Helper()
	f := &trustFixture{t: t}
	l, _, p := providerSetupLeader(t, func(p *LeaderParams) {
		p.Home = privateHome(t)
		p.Initial = providers.Model{}
		p.TrustRequests = true
		p.Warn = func(line string) {
			f.mu.Lock()
			f.warn = append(f.warn, line)
			f.mu.Unlock()
		}
		if edit != nil {
			edit(p)
		}
	})
	f.home = p.Home
	f.c = l.client(nil)
	f.c.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k1", "providerId": "openai", "key": "sk-fake-openai-key"})
	f.c.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k2", "providerId": "anthropic", "key": "sk-fake-anthropic-key"})
	return f
}

func (f *trustFixture) warnings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.warn...)
}

// session opens a session in cwd with an optional trust override and returns
// its state.
func (f *trustFixture) session(cwd string, override string) (string, protocol.ACPStateResult) {
	f.t.Helper()
	params := map[string]any{"cwd": cwd, "mcpServers": []any{}}
	if override != "" {
		params["_meta"] = map[string]any{protocol.ACPTrustMetaKey: override}
	}
	out := decodeInto[struct {
		SessionID string `json:"sessionId"`
	}](f.t, f.c.must("session/new", params))
	st := decodeInto[protocol.ACPStateResult](f.t, f.c.must(protocol.ACPState, map[string]any{"sessionId": out.SessionID}))
	return out.SessionID, st
}

func (f *trustFixture) writeUser(content string) {
	f.t.Helper()
	require.NoError(f.t, os.WriteFile(filepath.Join(f.home, "settings.json"), []byte(content), 0o600))
}

func (f *trustFixture) trustPath() string { return filepath.Join(f.home, "trust.json") }

// gatedProject makes a project folder whose .ask/settings.json has content.
func gatedProject(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".ask"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".ask", "settings.json"), []byte(content), 0o644))
	return root
}

func canonical(t *testing.T, p string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(p)
	require.NoError(t, err)
	return out
}

func requireNoFile(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	require.True(t, os.IsNotExist(err), "%s exists: %v", path, err)
}

var (
	userModel    = acp2ID(providers.OpenAIGPT55())
	projectModel = acp2ID(providers.AnthropicSonnet46())
)

// An old client sends no override: the session decides with no UI. The
// override of a new client trusts the project, and the project default wins.
func TestDecideThroughFactory(t *testing.T) {
	f := newTrustFixture(t, nil)
	f.writeUser(`{"defaultModel":"` + userModel + `","thinkingFold":"truncated"}`)
	root := gatedProject(t, `{"defaultModel":"`+projectModel+`","thinkingFold":"collapsed"}`)

	sid, st := f.session(root, "")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: string(workspace.SourceNoUI)}, st.Trust)
	require.Equal(t, userModel, st.ModelID)
	require.Equal(t, "truncated", st.ThinkingFold)
	models := decodeInto[protocol.ACPModelsResult](t, f.c.must(protocol.ACPModels, map[string]any{"sessionId": sid}))
	require.Equal(t, userModel, models.Default)

	sid, st = f.session(root, "approve")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: string(workspace.SourceFlag)}, st.Trust)
	require.Equal(t, projectModel, st.ModelID)
	require.Equal(t, "collapsed", st.ThinkingFold, "a trusted project overrides the fold")
	models = decodeInto[protocol.ACPModelsResult](t, f.c.must(protocol.ACPModels, map[string]any{"sessionId": sid}))
	require.Equal(t, projectModel, models.Default)

	_, st = f.session(root, "deny")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: string(workspace.SourceFlag)}, st.Trust)
	require.Equal(t, userModel, st.ModelID)
	requireNoFile(t, f.trustPath())
}

// The decision comes before any read of the project file: a file that cannot
// be opened gives no diagnostic while the project is untrusted, and a
// diagnostic with its path once it is trusted.
func TestUntrustedProjectFileNotOpenedBySession(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a file with mode 0000")
	}
	f := newTrustFixture(t, nil)
	root := gatedProject(t, `{}`)
	path := filepath.Join(root, ".ask", "settings.json")
	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, st := f.session(root, "")
	require.False(t, st.Trust.Trusted)
	for _, d := range st.Diagnostics {
		require.NotEqual(t, "project", d.Layer, "untrusted project file was read: %+v", d)
	}
	_, st = f.session(root, "approve")
	require.True(t, st.Trust.Trusted)
	require.Len(t, st.Diagnostics, 1)
	require.Equal(t, "project", st.Diagnostics[0].Layer)
	require.Equal(t, filepath.Join(canonical(t, root), ".ask", "settings.json"), st.Diagnostics[0].Path)
}

func TestImplicitTrustIsNeverSaved(t *testing.T) {
	f := newTrustFixture(t, nil)
	root := t.TempDir()
	_, st := f.session(root, "")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: string(workspace.SourceNoGatedFiles)}, st.Trust)
	requireNoFile(t, f.trustPath())

	// A model can make the folder gated; the next session decides again.
	require.NoError(t, os.Mkdir(filepath.Join(root, ".ask"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".ask", "settings.json"), []byte(`{}`), 0o644))
	_, st = f.session(root, "")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: string(workspace.SourceNoUI)}, st.Trust)
	requireNoFile(t, f.trustPath())
}

func TestSessionOverrideIsNotSaved(t *testing.T) {
	f := newTrustFixture(t, nil)
	root := gatedProject(t, `{}`)
	_, st := f.session(root, "approve")
	require.True(t, st.Trust.Trusted)
	requireNoFile(t, f.trustPath())
	_, st = f.session(root, "")
	require.False(t, st.Trust.Trusted, "the override applied to one session only")
}

func TestTrustReadAtEachSession(t *testing.T) {
	f := newTrustFixture(t, nil)
	root := gatedProject(t, `{"defaultModel":"`+projectModel+`"}`)

	state := decodeInto[protocol.ACPTrustStateResult](t, f.c.must(protocol.ACPTrustState, map[string]any{"cwd": root}))
	require.Equal(t, canonical(t, root), state.Root)
	require.False(t, state.Trusted)
	require.Equal(t, string(workspace.SourcePrompt), state.Source)
	require.True(t, state.NeedsDecision, "the client can ask, so the state asks for a decision")
	require.Equal(t, []string{filepath.Join(".ask", "settings.json")}, state.Gated)

	set := decodeInto[protocol.ACPTrustStateResult](t, f.c.must(protocol.ACPTrustSet, map[string]any{"cwd": root, "target": "folder", "decision": "trust"}))
	require.True(t, set.Trusted)
	require.Equal(t, string(workspace.SourceSaved), set.Source)
	require.Equal(t, canonical(t, root), set.EntryPath)
	require.False(t, set.NeedsDecision)

	_, st := f.session(root, "")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: string(workspace.SourceSaved)}, st.Trust)
	require.Equal(t, projectModel, st.ModelID)
	info, err := os.Stat(f.trustPath())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	f.c.must(protocol.ACPTrustSet, map[string]any{"cwd": root, "target": "folder", "decision": "clear"})
	_, st = f.session(root, "")
	require.Equal(t, string(workspace.SourceNoUI), st.Trust.Source)

	// A plain folder needs no decision.
	plain := decodeInto[protocol.ACPTrustStateResult](t, f.c.must(protocol.ACPTrustState, map[string]any{"cwd": t.TempDir()}))
	require.True(t, plain.Trusted)
	require.False(t, plain.NeedsDecision)
	require.Equal(t, string(workspace.SourceNoGatedFiles), plain.Source)
}

func TestTrustSetParentReplacesChild(t *testing.T) {
	f := newTrustFixture(t, nil)
	parent := t.TempDir()
	root := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".ask"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".ask", "settings.json"), []byte(`{}`), 0o644))

	f.c.must(protocol.ACPTrustSet, map[string]any{"cwd": root, "target": "folder", "decision": "distrust"})
	got := decodeInto[protocol.ACPTrustStateResult](t, f.c.must(protocol.ACPTrustSet, map[string]any{"cwd": root, "target": "parent", "decision": "trust"}))
	require.True(t, got.Trusted)
	require.Equal(t, string(workspace.SourceSavedAncestor), got.Source)
	require.Equal(t, canonical(t, parent), got.EntryPath)

	store, err := settings.NewTrustStore(f.home)
	require.NoError(t, err)
	entries, err := store.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]bool{canonical(t, parent): true}, entries)
}

func TestTrustSetRefusedWhenStoreCorrupt(t *testing.T) {
	f := newTrustFixture(t, nil)
	f.writeUser(`{"defaultProjectTrust":"always"}`)
	const bad = `{"broken`
	require.NoError(t, os.WriteFile(f.trustPath(), []byte(bad), 0o600))
	root := gatedProject(t, `{}`)

	_, e := f.c.call(protocol.ACPTrustSet, map[string]any{"cwd": root, "target": "folder", "decision": "trust"})
	require.NotNil(t, e)
	require.Equal(t, string(protocol.ACPErrInvalidState), e.Data.Kind)
	b, err := os.ReadFile(f.trustPath())
	require.NoError(t, err)
	require.Equal(t, bad, string(b), "a corrupt store keeps its bytes")

	state := decodeInto[protocol.ACPTrustStateResult](t, f.c.must(protocol.ACPTrustState, map[string]any{"cwd": root}))
	require.False(t, state.Trusted)
	require.Equal(t, string(workspace.SourceStoreError), state.Source)
	require.False(t, state.NeedsDecision)
	require.Contains(t, state.Warning, f.trustPath())
	require.NotContains(t, state.Warning, "broken", "the warning never quotes the file")

	// The session starts untrusted: "always" does not override a damaged store.
	_, st := f.session(root, "")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: string(workspace.SourceStoreError)}, st.Trust)
	logged := strings.Join(f.warnings(), "\n")
	require.Contains(t, logged, f.trustPath())
}

func TestSaveDefaultReportsShadowed(t *testing.T) {
	f := newTrustFixture(t, nil)
	f.writeUser(`{"theme":"dark"}`)
	root := gatedProject(t, `{"defaultModel":"`+projectModel+`"}`)
	sid, st := f.session(root, "approve")
	require.Equal(t, projectModel, st.ModelID)

	res := decodeInto[protocol.ACPSetModelResult](t, f.c.must(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": userModel, "saveDefault": true}))
	require.False(t, res.DefaultSaved)
	require.Equal(t, protocol.ACPDefaultShadowed, res.DefaultNotSaved)
	require.Equal(t, userModel, res.ModelID, "the switch is kept")
	b, err := os.ReadFile(filepath.Join(f.home, "settings.json"))
	require.NoError(t, err)
	require.Equal(t, `{"theme":"dark"}`, string(b))

	// The same id as the project value is saved.
	res = decodeInto[protocol.ACPSetModelResult](t, f.c.must(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": projectModel, "saveDefault": true}))
	require.True(t, res.DefaultSaved)
	require.Empty(t, res.DefaultNotSaved)

	// An untrusted session of the same folder is not shadowed.
	other, _ := f.session(root, "")
	res = decodeInto[protocol.ACPSetModelResult](t, f.c.must(protocol.ACPSetModel, map[string]any{"sessionId": other, "modelId": userModel, "saveDefault": true}))
	require.True(t, res.DefaultSaved)

	// A user file that is not safe gives its own reason.
	require.NoError(t, os.Chmod(filepath.Join(f.home, "settings.json"), 0o644))
	res = decodeInto[protocol.ACPSetModelResult](t, f.c.must(protocol.ACPSetModel, map[string]any{"sessionId": other, "modelId": projectModel, "saveDefault": true}))
	require.False(t, res.DefaultSaved)
	require.Equal(t, protocol.ACPDefaultUnsafeFile, res.DefaultNotSaved)
}

// Each session writes its diagnostics to the leader log once, with paths and
// fixed texts only.
func TestSessionDiagnosticsLoggedOnce(t *testing.T) {
	f := newTrustFixture(t, nil)
	root := gatedProject(t, `{"env":{"SECRET_VALUE":"x"},"thinkingFold":"collapsed"}`)
	sid, st := f.session(root, "approve")
	require.Len(t, st.Diagnostics, 1)
	require.Equal(t, "env", st.Diagnostics[0].Key)
	f.c.must(protocol.ACPState, map[string]any{"sessionId": sid})
	lines := f.warnings()
	require.Len(t, lines, 1, "%q", lines)
	require.Contains(t, lines[0], filepath.Join(canonical(t, root), ".ask", "settings.json"))
	require.Contains(t, lines[0], "env")
	require.NotContains(t, lines[0], "SECRET_VALUE")

	// A plain session has nothing to log.
	f.session(t.TempDir(), "")
	require.Len(t, f.warnings(), 1)
}

// The process-wide override of ask acp applies to each session; the override
// of one session wins over it.
func TestProcessOverrideAndSessionOverride(t *testing.T) {
	f := newTrustFixture(t, func(p *LeaderParams) { p.TrustOverride = "approve" })
	root := gatedProject(t, `{}`)
	_, st := f.session(root, "")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: string(workspace.SourceFlag)}, st.Trust)
	_, st = f.session(root, "deny")
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: false, Source: string(workspace.SourceFlag)}, st.Trust)
	requireNoFile(t, f.trustPath())
}

// Without trust requests (ask acp) the trust methods are not served.
func TestTrustMethodsOffWithoutTrustRequests(t *testing.T) {
	l, _, _ := providerSetupLeader(t, nil)
	c := l.client(nil)
	_, e := c.call(protocol.ACPTrustState, map[string]any{"cwd": t.TempDir()})
	require.NotNil(t, e)
	require.Equal(t, string(protocol.ACPErrUnsupported), e.Data.Kind)
	_, e = c.call(protocol.ACPTrustSet, map[string]any{"cwd": t.TempDir(), "target": "folder", "decision": "trust"})
	require.NotNil(t, e)
	require.Equal(t, string(protocol.ACPErrUnsupported), e.Data.Kind)
}

func TestResolveTrustOutcome(t *testing.T) {
	home := privateHome(t)
	root := gatedProject(t, `{}`)
	out, err := ResolveTrust(context.Background(), home, "", root, workspace.OverrideNone, true)
	require.NoError(t, err)
	require.Equal(t, canonical(t, root), out.Root)
	require.True(t, out.HasLayer)
	require.Equal(t, workspace.Decision{Trusted: false, Source: workspace.SourcePrompt, NeedsPrompt: true}, out.Decision)
	require.Empty(t, out.Warning)

	// The Ask home is not a project layer.
	askHome := filepath.Join(t.TempDir(), ".ask")
	require.NoError(t, os.Mkdir(askHome, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(askHome, "settings.json"), []byte(`{}`), 0o600))
	out, err = ResolveTrust(context.Background(), askHome, "", filepath.Dir(askHome), workspace.OverrideNone, false)
	require.NoError(t, err)
	require.False(t, out.HasLayer)
	require.Equal(t, workspace.SourceNoGatedFiles, out.Decision.Source)

	_, err = ResolveTrust(context.Background(), home, "", "", workspace.OverrideNone, false)
	require.Error(t, err)
}
