package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/settings"
	"AskCore/internal/testsupport"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
)

// testACPParams gives the parameters of a runtime that stays offline: the
// model refresh is off, and the sign-in and model refresh HTTP refuse every
// host but loopback, so a test that turns the refresh on by mistake fails.
func testACPParams(t *testing.T) ACPParams {
	t.Helper()
	guard := testsupport.LoopbackOnly(t)
	return ACPParams{
		Home:         t.TempDir(),
		UserHome:     t.TempDir(),
		AuthHTTP:     guard,
		ModelRefresh: ModelRefresh{Enabled: false, HTTP: guard},
		Initial:      providers.Model{ID: "faux-1", Name: "faux-1", Provider: "faux", API: "faux"},
		Info:         sdk.Implementation{Name: "ask", Version: "test"},
		NewAgent: func(*auth.Service, AgentSpec) (*agent.Agent, error) {
			return nil, errors.New("not used")
		},
	}
}

// The editor composition needs no configuration, database, gateway or leader:
// the graph validates with the parameters alone.
func TestACPModuleValidatesWithoutServers(t *testing.T) {
	p := testACPParams(t)
	require.NoError(t, fx.ValidateApp(ACPModule, fx.Supply(p), fx.Invoke(func(*ACPRuntime) {})))
}

func TestACPModuleRequiresAgentConstructor(t *testing.T) {
	p := testACPParams(t)
	p.NewAgent = nil
	_, err := NewACPRuntime(p)
	require.Error(t, err)
}

func TestACPRuntimeAuthMethodsAndReadiness(t *testing.T) {
	p := testACPParams(t)
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, m := range rt.Config.AuthMethods {
		require.NotNil(t, m.Agent)
		require.NotNil(t, m.Agent.Description)
		require.Contains(t, *m.Agent.Description, "ask auth login")
		ids[m.Agent.Id] = true
	}
	for _, want := range []string{"api-key", "anthropic-oauth", "openai-chatgpt", "xai-oauth"} {
		require.True(t, ids[want], want)
	}
	ctx := context.Background()
	require.Error(t, rt.Config.Authenticate(ctx, "api-key"), "no credential is saved yet")

	store, err := settings.NewAuthStore(p.Home)
	require.NoError(t, err)
	_, err = store.Replace(ctx, providers.ProviderOpenAI, 0, settings.Credential{Method: "api-key", APIKey: "saved-secret"})
	require.NoError(t, err)
	require.NoError(t, rt.Config.Authenticate(ctx, "api-key"))
	require.Error(t, rt.Config.Authenticate(ctx, "xai-oauth"), "only the configured method is ready")
	require.NoError(t, rt.Cleanup(ctx))
}

func TestACPRuntimeCatalogListsInitialModelFirst(t *testing.T) {
	p := testACPParams(t)
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	rows := rt.Config.Models()
	require.Equal(t, "faux", rows[0].Provider)
	require.Greater(t, len(rows), 1)
	m, err := rt.Config.FindModel(providers.Ref{Provider: "faux", ID: "faux-1"})
	require.NoError(t, err)
	require.Equal(t, p.Initial, m)
	_, err = rt.Config.FindModel(providers.Ref{Provider: "openai", ID: providers.ModelGPT55})
	require.NoError(t, err)

	err = rt.Config.ModelAuth(context.Background(), providers.OpenAIGPT55(), "subscription")
	require.Error(t, err)
}

// resumeRuntime composes the runtime over a faux model whose sessions save
// their log in the home of the test.
func resumeRuntime(t *testing.T) (*ACPRuntime, ACPParams) {
	t.Helper()
	p := testACPParams(t)
	p.NewAgent = func(_ *auth.Service, spec AgentSpec) (*agent.Agent, error) {
		fp, err := faux.New(faux.WithChunk(1000, 1000))
		if err != nil {
			return nil, err
		}
		m, _ := fp.Model("faux-1")
		fp.Set(faux.Say("reply"))
		cfg := agent.Config{SessionID: spec.SessionID, LoopConfig: agent.LoopConfig{
			Model: m, Stream: fp.Stream, Cwd: spec.Cwd,
			Wait: func(context.Context, time.Duration) error { return nil },
		}}
		return NewNativeAgent(NativeAgentConfig{Config: cfg, InitialStream: fp.Stream, InitialAPI: "faux", Home: p.Home, Log: spec.Log})
	}
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	return rt, p
}

func promptOnce(t *testing.T, a *agent.Agent, text string) {
	t.Helper()
	require.NoError(t, a.Prompt(context.Background(), protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}}))
}

func TestFactoryResumeContinuesStoredSession(t *testing.T) {
	ctx := context.Background()
	rt, _ := resumeRuntime(t)
	cwd := canonicalDir(t)
	first, err := rt.Config.Factory(ctx, acp.SessionRequest{ID: "7001", Cwd: cwd})
	require.NoError(t, err)
	promptOnce(t, first, "hello")
	n := len(first.State().Messages)
	require.NoError(t, first.Dispose())

	// The load of a session that a live agent holds fails at once.
	held, err := rt.Config.Factory(ctx, acp.SessionRequest{ID: "7001", Cwd: cwd, Resume: true})
	require.NoError(t, err)
	_, err = rt.Config.Factory(ctx, acp.SessionRequest{ID: "7001", Cwd: cwd, Resume: true})
	require.ErrorIs(t, err, acp.ErrSessionInUse)

	require.Len(t, held.State().Messages, n, "the stored conversation is the context")
	require.Equal(t, "faux-1", held.State().Model.ID, "the model comes from the log")
	promptOnce(t, held, "again")
	require.NoError(t, held.Dispose())

	again, err := rt.Config.Factory(ctx, acp.SessionRequest{ID: "7001", Cwd: cwd, Resume: true})
	require.NoError(t, err)
	require.Len(t, again.State().Messages, n+2, "the second turn was saved")
	require.NoError(t, again.Dispose())
}

func TestFactoryResumeRefusesOtherCwdAndUnknownID(t *testing.T) {
	ctx := context.Background()
	rt, _ := resumeRuntime(t)
	cwd := canonicalDir(t)
	first, err := rt.Config.Factory(ctx, acp.SessionRequest{ID: "7002", Cwd: cwd})
	require.NoError(t, err)
	require.NoError(t, first.Dispose())

	_, err = rt.Config.Factory(ctx, acp.SessionRequest{ID: "7002", Cwd: canonicalDir(t), Resume: true})
	require.ErrorIs(t, err, acp.ErrUnknownSession, "a session of another directory is not found there")
	_, err = rt.Config.Factory(ctx, acp.SessionRequest{ID: "7999", Cwd: cwd, Resume: true})
	require.ErrorIs(t, err, acp.ErrUnknownSession)
}

func TestFactoryResumeErrorsHoldFixedTextOnly(t *testing.T) {
	ctx := context.Background()
	rt, p := resumeRuntime(t)
	cwd := canonicalDir(t)
	first, err := rt.Config.Factory(ctx, acp.SessionRequest{ID: "7003", Cwd: cwd})
	require.NoError(t, err)
	require.NoError(t, first.Dispose())
	files, err := filepath.Glob(filepath.Join(p.Home, "sessions", "*", "7003.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.NoError(t, os.WriteFile(files[0], []byte("not a session\n"), 0o600))

	_, err = rt.Config.Factory(ctx, acp.SessionRequest{ID: "7003", Cwd: cwd, Resume: true})
	require.Error(t, err)
	require.Equal(t, "invalid_state: the session file cannot be read", err.Error(), "the text names no path")
}

func TestFactoryResumeRefusesUnavailableLoginMethodAndReleasesLock(t *testing.T) {
	ctx := context.Background()
	rt, p := resumeRuntime(t)
	cwd := canonicalDir(t)
	sl, err := NewSessionLog(p.Home)
	require.NoError(t, err)
	w, err := sl.NewContext(SessionLogOptions{CreateNow: true}, "7004", cwd)()
	require.NoError(t, err)
	_, err = w.Append(
		sessions.ModelChange{Model: sessions.ModelRef{Provider: "faux", API: "faux", ID: "faux-1"}},
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "completed", Binding: &sessions.AuthBinding{Provider: "faux", Method: "oauth"}},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = rt.Config.Factory(ctx, acp.SessionRequest{ID: "7004", Cwd: cwd, Resume: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), `login method "oauth"`, "a method that is gone needs an explicit model choice")

	// The failed start released the lock, and an explicit model resumes.
	a, err := rt.Config.Factory(ctx, acp.SessionRequest{ID: "7004", Cwd: cwd, Resume: true, Flags: protocol.ACPSessionOptions{Model: "faux/faux-1"}})
	require.NoError(t, err)
	require.NoError(t, a.Dispose())
}

func TestFactoryNoSessionSavesNothing(t *testing.T) {
	ctx := context.Background()
	rt, p := resumeRuntime(t)
	cwd := canonicalDir(t)
	a, err := rt.Config.Factory(ctx, acp.SessionRequest{ID: "7005", Cwd: cwd, Flags: protocol.ACPSessionOptions{NoSession: true}})
	require.NoError(t, err)
	promptOnce(t, a, "hello")
	require.NoError(t, a.Dispose())
	require.NoDirExists(t, filepath.Join(p.Home, "sessions"), "an unsaved session makes no file")
}
