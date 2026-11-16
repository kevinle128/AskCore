package acp

import (
	"strings"
	"testing"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestSessionOptionsBounds(t *testing.T) {
	t.Parallel()
	key := protocol.ACPSessionOptionsMetaKey
	long := strings.Repeat("m", protocol.ACPSessionModelMaxBytes)
	entries := func(n int) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = "openai/*"
		}
		return out
	}

	got, err := flagsFromMeta(map[string]any{key: map[string]any{"model": long, "thinking": "high", "models": entries(protocol.ACPSessionModelsMax)}})
	require.NoError(t, err, "values at the bounds are valid")
	require.Equal(t, long, got.Model)
	require.Equal(t, protocol.ThinkingHigh, got.Thinking)
	require.Len(t, got.Models, protocol.ACPSessionModelsMax)

	got, err = flagsFromMeta(map[string]any{key: map[string]any{"thinking": "default"}})
	require.NoError(t, err, "default is a thinking level")
	require.Equal(t, protocol.ThinkingDefault, got.Thinking)

	for name, tc := range map[string]struct {
		opts map[string]any
		text string
	}{
		"long model":        {map[string]any{"model": long + "x"}, "the model option is too long"},
		"too many models":   {map[string]any{"models": entries(protocol.ACPSessionModelsMax + 1)}, "the models option has too many entries"},
		"long models entry": {map[string]any{"models": []any{long + "x"}}, "a models entry is too long"},
		"bad thinking":      {map[string]any{"thinking": "ultra"}, "the thinking option is not a thinking level"},
		"model not text":    {map[string]any{"model": 3}, "the session options are malformed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := flagsFromMeta(map[string]any{key: tc.opts})
			require.Equal(t, protocol.ACPErrInvalidParams, classify(err))
			require.Contains(t, err.Error(), tc.text)
			require.NotContains(t, err.Error(), "mmmm", "the text never quotes the input")
		})
	}
}

func TestNewSessionEchoesAppliedModelOptions(t *testing.T) {
	t.Parallel()
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	opts := map[string]any{"model": "openai/gpt-5.5:low", "thinking": "high", "models": []any{"openai/*"}}
	var res struct {
		Meta map[string]any `json:"_meta"`
	}
	p.ok("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}, "_meta": map[string]any{protocol.ACPSessionOptionsMetaKey: opts}}, &res)
	require.Equal(t, true, res.Meta[protocol.ACPSessionOptionsMetaKey])
	require.Equal(t, opts, res.Meta[protocol.ACPAppliedOptionsMetaKey], "the echo holds the applied model options")
}

// The model and the level change in one call, under one guard: the client
// needs no second call, and the answer has the level that the model allows.
func TestSetModelAppliesLevelUnderOneGuard(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	p := newAdapterPeer(t, testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "x")} }, nil), nil)
	sid := p.start()
	gpt, sonnet := providers.OpenAIGPT55(), providers.AnthropicSonnet46()

	var res protocol.ACPSetModelResult
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": modelID(sonnet), "thinkingLevel": "high"}, &res)
	require.Equal(t, modelID(sonnet), res.ModelID)
	require.Equal(t, providers.ClampThinkingLevel(sonnet, protocol.ThinkingHigh), res.ThinkingLevel)
	require.Equal(t, res.ThinkingLevel, p.state(sid).ThinkingLevel)

	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": modelID(gpt), "thinkingLevel": "low"}, &res)
	require.Equal(t, protocol.ThinkingLow, res.ThinkingLevel)
	// No level in the request keeps the level of the session, clamped to the
	// new model.
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": modelID(sonnet)}, &res)
	require.Equal(t, providers.ClampThinkingLevel(sonnet, protocol.ThinkingLow), res.ThinkingLevel)

	before := p.state(sid)
	f := p.call(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": modelID(gpt), "thinkingLevel": "ultra"})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	require.Equal(t, before, p.state(sid), "a bad level changes neither the model nor the level")

	// A running session refuses the call and keeps both. The session stays on
	// the faux model, whose script holds the run.
	busy := p.newSession()
	before = p.state(busy)
	id := p.send("session/prompt", promptParams(busy, "go"))
	p.running(busy)
	f = p.call(protocol.ACPSetModel, map[string]any{"sessionId": busy, "modelId": modelID(gpt), "thinkingLevel": "high"})
	require.Equal(t, protocol.ACPErrBusy, f.errKind(t))
	during := p.state(busy)
	require.Equal(t, before.ModelID, during.ModelID)
	require.Equal(t, before.ThinkingLevel, during.ThinkingLevel)
	rel()
	p.await(id)
}

func TestModelsResultScopeOrder(t *testing.T) {
	t.Parallel()
	gpt, sonnet := providers.OpenAIGPT55(), providers.AnthropicSonnet46()
	dropped := &protocol.ACPLevelDropped{ModelID: "local/x@openai-completions", Level: protocol.ThinkingHigh}
	problems := []protocol.ACPDiagnostic{{Layer: "user", Path: "/h/models.json", Key: "providers.local", Text: "bad"}}
	cfg := testConfig(says("x"), nil)
	cfg.StartModel = func(string) *ModelStart {
		return &ModelStart{Scope: []string{"anthropic/" + sonnet.ID + ":high", "openai/" + gpt.ID, "anthropic/*", "nope/none"}, LevelDropped: dropped}
	}
	cfg.ModelProblems = func() []protocol.ACPDiagnostic { return problems }
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()

	var res protocol.ACPModelsResult
	p.ok(protocol.ACPModels, map[string]any{"sessionId": sid}, &res)
	require.GreaterOrEqual(t, len(res.Scope), 2)
	require.Equal(t, protocol.ACPScopedModel{ModelID: modelID(sonnet), ThinkingLevel: protocol.ThinkingHigh}, res.Scope[0], "entry order, with the level of the entry")
	require.Equal(t, protocol.ACPScopedModel{ModelID: modelID(gpt)}, res.Scope[1], "an entry with no level has none")
	for _, sm := range res.Scope[2:] {
		require.True(t, strings.HasPrefix(sm.ModelID, "anthropic/"), "the glob adds the other rows of its provider: %v", sm)
		require.NotEqual(t, modelID(sonnet), sm.ModelID, "a row is in the list once")
	}
	require.Equal(t, problems, res.Diagnostics)
	require.Equal(t, dropped, p.state(sid).LevelDropped)

	// A session with no scope has no cycle list.
	cfg2 := testConfig(says("x"), nil)
	p2 := newAdapterPeer(t, cfg2, nil)
	sid2 := p2.start()
	var none protocol.ACPModelsResult
	p2.ok(protocol.ACPModels, map[string]any{"sessionId": sid2}, &none)
	require.Empty(t, none.Scope)
	require.Nil(t, p2.state(sid2).LevelDropped)
}

// The dropped level of the start holds until the model changes, as the start
// fallback does.
func TestLevelDroppedClearsOnModelChange(t *testing.T) {
	t.Parallel()
	cfg := testConfig(says("x"), nil)
	cfg.StartModel = func(string) *ModelStart {
		return &ModelStart{LevelDropped: &protocol.ACPLevelDropped{ModelID: "faux/faux-1@faux", Level: protocol.ThinkingHigh}}
	}
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	require.NotNil(t, p.state(sid).LevelDropped)
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": modelID(providers.OpenAIGPT55())}, nil)
	require.Eventually(t, func() bool { return p.state(sid).LevelDropped == nil }, 2*time.Second, 5*time.Millisecond)
}
