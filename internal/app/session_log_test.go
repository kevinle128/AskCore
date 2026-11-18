package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/settings"
	"AskCore/internal/store"
	"AskCore/pkg/protocol"
)

// TestMain gives the tests an Ask home of their own. An agent without an
// explicit home reads ASK_HOME and then ~/.ask, and agents now save their
// session there, so without this the tests would write into the home of the
// developer.
func TestMain(m *testing.M) { os.Exit(runIsolated(m)) }

func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "ask-app-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "app tests: make home: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(home) }()
	if err := os.Chmod(home, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "app tests: private home: %v\n", err)
		return 1
	}
	prev, had := os.LookupEnv("ASK_HOME")
	if err := os.Setenv("ASK_HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "app tests: set ASK_HOME: %v\n", err)
		return 1
	}
	defer func() {
		if had {
			_ = os.Setenv("ASK_HOME", prev)
		} else {
			_ = os.Unsetenv("ASK_HOME")
		}
	}()
	return m.Run()
}

func newTestLog(t *testing.T) (*SessionLog, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(home, 0o700))
	log, err := NewSessionLog(home)
	require.NoError(t, err)
	return log, home
}

func canonicalDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func assistantEntry(text string) sessions.MessageEntry {
	return sessions.MessageEntry{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: text}}, StopReason: protocol.StopStop}}
}

func TestSessionLogMakesNoFileUntilFirstReply(t *testing.T) {
	log, home := newTestLog(t)
	cwd := canonicalDir(t)

	w, err := log.NewContext(SessionLogOptions{}, "1001", cwd)()
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	assert.NoDirExists(t, filepath.Join(home, "sessions"), "a built log and a lazy writer make no folder")
	_, err = w.Append(sessions.MessageEntry{Message: protocol.UserMessage{}})
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(home, "sessions"))
	_, err = w.Append(assistantEntry("hi"))
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(home, "sessions", "--"+encodedForTest(cwd)+"--", "1001.jsonl"))
}

// encodedForTest is the folder name of a cwd: separators become dashes.
func encodedForTest(cwd string) string {
	out := []byte(cwd[1:])
	for i, c := range out {
		if c == '/' {
			out[i] = '-'
		}
	}
	return string(out)
}

func TestSessionLogCreateNowStoresAtOnceAndResetMakesNewSession(t *testing.T) {
	log, _ := newTestLog(t)
	cwd := canonicalDir(t)
	next := log.NewContext(SessionLogOptions{CreateNow: true}, "2002", cwd)

	first, err := next()
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := next()
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	ids, err := log.store.List(context.Background(), cwd)
	require.NoError(t, err)
	require.Len(t, ids, 2)
	assert.Equal(t, "2002", first.(store.SessionWriter).ID())
	assert.Regexp(t, `^\d{19}$`, second.(store.SessionWriter).ID(), "a reset takes a new snowflake id")
}

func TestSessionLogResumeAndLatest(t *testing.T) {
	ctx := context.Background()
	log, _ := newTestLog(t)
	cwd := canonicalDir(t)

	_, err := log.Latest(ctx, cwd)
	var se *StoreError
	require.ErrorAs(t, err, &se)
	assert.ErrorIs(t, err, store.ErrSessionNotFound)
	assert.Equal(t, "no session to continue in "+cwd, err.Error())
	_, err = log.Resume(ctx, cwd, "42")
	assert.Equal(t, fmt.Sprintf("no session 42 in %s", cwd), err.Error())

	for _, id := range []string{"1000", "2000"} {
		w, err := log.NewContext(SessionLogOptions{CreateNow: true}, id, cwd)()
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}
	_, err = log.Latest(ctx, cwd)
	require.ErrorIs(t, err, store.ErrSessionNotFound, "sessions with no reply are not continued")

	// A named id opens a session with no reply.
	r, err := log.Resume(ctx, cwd, "2000")
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Writer.Close() })
	assert.Equal(t, "2000", r.Header.ID)
	assert.Nil(t, r.Selection, "a session that never ran names no model")

	_, err = log.Resume(ctx, cwd, "2000")
	require.ErrorIs(t, err, store.ErrSessionOwned)
	assert.Equal(t, "session 2000 is open in another ask process (a leader keeps it after the TUI quits); open it in the TUI with `ask -c`, or stop the leader with `ask leader stop`", err.Error())
}

func TestSessionLogResumeReadsSelectionFromLog(t *testing.T) {
	ctx := context.Background()
	log, _ := newTestLog(t)
	cwd := canonicalDir(t)
	w, err := log.NewContext(SessionLogOptions{CreateNow: true}, "3003", cwd)()
	require.NoError(t, err)
	ref := sessions.ModelRef{Provider: "anthropic", API: "anthropic-messages", ID: "claude-sonnet-4-6"}
	_, err = w.Append(
		sessions.ModelChange{Model: ref},
		sessions.ThinkingChange{Level: protocol.ThinkingHigh},
		assistantEntry("hi"),
		sessions.AttemptSettled{AttemptID: "a1", Binding: &sessions.AuthBinding{Provider: "anthropic", Method: "api-key"}},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := log.Resume(ctx, cwd, "3003")
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Writer.Close() })

	require.NotNil(t, r.Selection)
	assert.Equal(t, RestoredSelection{Model: ref, Method: "api-key", Level: protocol.ThinkingHigh}, *r.Selection)
}

func TestNativeAgentSavesSessionByDefault(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(home, 0o700))
	cwd := canonicalDir(t)
	p, err := faux.New()
	require.NoError(t, err)
	model, _ := p.Model("faux-1")
	p.Set(faux.Say("ok"))
	newAgent := func(log SessionLogOptions, cfg agent.Config) *agent.Agent {
		cfg.Cwd, cfg.Model = cwd, model
		a, err := NewNativeAgent(NativeAgentConfig{Home: home, InitialStream: p.Stream, Config: cfg, Log: log})
		require.NoError(t, err)
		t.Cleanup(func() { _ = a.Dispose() })
		return a
	}
	prompt := func(a *agent.Agent) {
		require.NoError(t, a.Prompt(context.Background(), protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}))
	}

	prompt(newAgent(SessionLogOptions{}, agent.Config{SessionID: "4001"}))
	assert.FileExists(t, filepath.Join(home, "sessions", "--"+encodedForTest(cwd)+"--", "4001.jsonl"))

	p.Set(faux.Say("ok"))
	prompt(newAgent(SessionLogOptions{NoSession: true}, agent.Config{SessionID: "4002"}))
	assert.NoFileExists(t, filepath.Join(home, "sessions", "--"+encodedForTest(cwd)+"--", "4002.jsonl"))

	p.Set(faux.Say("ok"))
	own := &sessions.MemoryLog{}
	prompt(newAgent(SessionLogOptions{}, agent.Config{SessionID: "4003", NewContext: func() (sessions.Writer, error) { return own, nil }}))
	assert.NoFileExists(t, filepath.Join(home, "sessions", "--"+encodedForTest(cwd)+"--", "4003.jsonl"), "a log that the caller supplies wins")
	assert.NotEmpty(t, own.Entries())
}

func TestChooseRestoredModel(t *testing.T) {
	ctx := context.Background()
	p, err := faux.New()
	require.NoError(t, err)
	demo, _ := p.Model("faux-1")
	snap := &modelSnapshot{}
	c := newStartChoice(demo, func() modelSnapshot { return *snap }, func(context.Context, string) bool { return true })
	ref := sessions.ModelRef{Provider: demo.Provider, API: string(demo.API), ID: demo.ID}

	t.Run("the logged model and level come back", func(t *testing.T) {
		got, err := c.choose(ctx, ModelOptions{Restored: &RestoredSelection{Model: ref, Level: protocol.ThinkingDefault}}, settings.Effective{})
		require.NoError(t, err)
		assert.Equal(t, demo.ID, got.Model.ID)
	})
	t.Run("an explicit pattern wins", func(t *testing.T) {
		_, err := c.choose(ctx, ModelOptions{Pattern: "no-such-provider/no-such-model", Restored: &RestoredSelection{Model: ref}}, settings.Effective{})
		require.Error(t, err, "the pattern was resolved and the restored model was not used")
		assert.NotContains(t, err.Error(), "the model of the session")
	})
	t.Run("a model that the list lacks refuses the resume", func(t *testing.T) {
		_, err := c.choose(ctx, ModelOptions{Restored: &RestoredSelection{Model: sessions.ModelRef{Provider: "gone", API: "x", ID: "y"}}}, settings.Effective{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--model")
	})
	t.Run("a refused login method names the old method", func(t *testing.T) {
		c := c
		c.ready = func(context.Context, providers.Model, string) error { return fmt.Errorf("%w: gone", auth.ErrMethod) }
		_, err := c.choose(ctx, ModelOptions{Restored: &RestoredSelection{Model: ref, Method: "old-login"}}, settings.Effective{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "old-login")
		assert.Contains(t, err.Error(), "--model")
	})
	t.Run("a missing credential refuses the same way", func(t *testing.T) {
		c := c
		c.ready = func(context.Context, providers.Model, string) error { return auth.ErrNoCredential }
		_, err := c.choose(ctx, ModelOptions{Restored: &RestoredSelection{Model: ref, Method: "api-key"}}, settings.Effective{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"api-key"`)
	})
	t.Run("a log with no method skips the check", func(t *testing.T) {
		c := c
		c.ready = func(context.Context, providers.Model, string) error { return auth.ErrMethod }
		_, err := c.choose(ctx, ModelOptions{Restored: &RestoredSelection{Model: ref}}, settings.Effective{})
		require.NoError(t, err)
	})
}

func TestSessionLogLatestSkipsSessionsWithoutAssistantReply(t *testing.T) {
	ctx := context.Background()
	log, _ := newTestLog(t)
	cwd := canonicalDir(t)
	put := func(id string, entries ...sessions.Entry) {
		w, err := log.NewContext(SessionLogOptions{CreateNow: true}, id, cwd)()
		require.NoError(t, err)
		if len(entries) > 0 {
			_, err = w.Append(entries...)
			require.NoError(t, err)
		}
		require.NoError(t, w.Close())
	}
	put("1000", assistantEntry("old reply"))
	put("2000", sessions.MessageEntry{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "only a prompt"}}}})
	put("3000")

	latest, err := log.Latest(ctx, cwd)

	require.NoError(t, err)
	assert.Equal(t, "1000", latest, "the newer sessions have no assistant message")

	other := canonicalDir(t)
	w, err := log.NewContext(SessionLogOptions{CreateNow: true}, "4000", other)()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, err = log.Latest(ctx, other)
	require.ErrorIs(t, err, store.ErrSessionNotFound)
	assert.Equal(t, "no session to continue in "+other, err.Error())
}
