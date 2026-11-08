package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// The model options of the command line reach the host in the session
// options. The host resolves them against its model list, echoes them, keeps
// the scope for the model list and reports a dropped level in the session
// state.
func TestSessionModelOptionsOverTheInProcessAgent(t *testing.T) {
	home := leaderHome(t)
	require.NoError(t, os.MkdirAll(home, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, "models.json"), []byte(`{"providers": {"local": {
  "baseUrl": "http://127.0.0.1:9/v1", "api": "openai-completions", "apiKey": "local-key",
  "models": [{"id": "a"}, {"id": "b"}]}}}`), 0o600))
	deps := runDependencies{
		getenv: func(k string) string {
			switch k {
			case "ASK_HOME":
				return home
			case "ANTHROPIC_API_KEY":
				return "fixture-anthropic-key"
			}
			return ""
		},
		inferenceHTTP: &http.Client{Transport: scriptModel(t, say("unused"))},
	}
	c, err := openInProcessClient(context.Background(), deps)
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	sonnet := providers.AnthropicSonnet46()

	o, diags := parseArgs([]string{"--model", "local/a:high", "--models", "local/*,anthropic/" + sonnet.ID + ":low,nope-x/none"})
	require.Empty(t, diags)
	info, err := c.NewSession(ctx, t.TempDir(), tui.NewSessionOptions{Flags: interactiveSessionFlags(o)})
	require.NoError(t, err, "the host echoes the options it applied")
	assert.False(t, info.FlagsIgnored)

	st, err := c.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, "local/a@openai-completions", st.ModelID)
	assert.Equal(t, protocol.ThinkingOff, st.ThinkingLevel)
	require.NotNil(t, st.LevelDropped)
	assert.Equal(t, "local/a has no thinking; level high is not used.", st.LevelDropped.Text())
	require.NotNil(t, st.Trust)
	assert.Contains(t, st.Diagnostics, protocol.ACPDiagnostic{Key: "--models", Text: `unknown model "nope-x/none"`},
		"a scope entry that matches nothing is a start problem, not a dropped level")
	for _, d := range st.Diagnostics {
		assert.NotContains(t, d.Text, "has no thinking", "the dropped level has its own field")
	}

	models, err := c.Models(ctx)
	require.NoError(t, err)
	assert.Equal(t, []protocol.ACPScopedModel{
		{ModelID: "local/a@openai-completions"},
		{ModelID: "local/b@openai-completions"},
		{ModelID: "anthropic/" + sonnet.ID + "@anthropic-messages", ThinkingLevel: protocol.ThinkingLow},
	}, models.Scope)

	// A pattern that does not resolve fails the start with its text.
	o, _ = parseArgs([]string{"--model", "openai/" + providers.ModelGPT55 + ":hgih"})
	_, err = c.NewSession(ctx, t.TempDir(), tui.NewSessionOptions{Flags: interactiveSessionFlags(o)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown model "openai/gpt-5.5:hgih"`)
	assert.Contains(t, err.Error(), "off, minimal, low, medium, high, xhigh, max")
}
