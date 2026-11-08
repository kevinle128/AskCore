package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
)

// completionsText is an OpenAI chat completions stream that says text and
// stops.
func completionsText(text string) string {
	return "data: " + `{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"},"finish_reason":null}]}` + "\n\n" +
		"data: " + `{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

// localModelsHome is a private Ask home whose user models.json has one
// OpenAI-compatible provider "local" with the given model ids. The rows have
// no thinking, as every custom row by default.
func localModelsHome(t *testing.T, ids ...string) string {
	t.Helper()
	home := privateHome(t)
	rows := ""
	for i, id := range ids {
		if i > 0 {
			rows += ","
		}
		rows += fmt.Sprintf(`{"id": %q}`, id)
	}
	body := `{"providers": {"local": {"baseUrl": "http://127.0.0.1:9/v1", "api": "openai-completions", "apiKey": "local-key", "models": [` + rows + `]}}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, "models.json"), []byte(body), 0o600))
	return home
}

// An unknown level word on the command line is an unknown model, not a
// silent default level. The text lists the level words, because the text
// before the colon names a model.
func TestPrintModelPatternUnknownLevel(t *testing.T) {
	model := &scriptedModel{}

	got := printWithDefault(t, t.TempDir(), privateHome(t), model, nil, "-p", "--model", "openai/"+providers.ModelGPT55+":hgih", "hello")

	assert.Equal(t, 1, got.code)
	assert.Empty(t, got.stdout)
	assert.Contains(t, got.stderr, `Error: unknown model "openai/gpt-5.5:hgih"`)
	assert.Contains(t, got.stderr, "off, minimal, low, medium, high, xhigh, max")
	assert.Zero(t, model.requests(), "no request starts")
}

// A model id with a colon is matched as a whole first, so the tag of a local
// model is not read as a thinking level.
func TestPrintColonTaggedModelID(t *testing.T) {
	model := &scriptedModel{answers: []string{completionsText("from local")}}

	got := printWithDefault(t, t.TempDir(), localModelsHome(t, "qwen3-coder:30b"), model, nil, "-p", "--model", "local/qwen3-coder:30b", "hello")

	assert.Equal(t, result{"from local\n", "", 0}, got)
	body := model.body(t, 0)
	assert.Equal(t, "qwen3-coder:30b", body["model"])
	assert.NotContains(t, body, "reasoning_effort")
}

// A requested level on a row with no thinking is dropped with one warning
// line; the run goes on with thinking off.
func TestPrintDroppedLevelNotice(t *testing.T) {
	model := &scriptedModel{answers: []string{completionsText("ok")}}

	got := printWithDefault(t, t.TempDir(), localModelsHome(t, "x"), model, nil, "-p", "--model", "local/x:high", "hello")

	assert.Equal(t, result{"ok\n", "Warning: local/x has no thinking; level high is not used.\n", 0}, got)
	assert.NotContains(t, model.body(t, 0), "reasoning_effort")

	// --thinking is a request too.
	model = &scriptedModel{answers: []string{completionsText("ok")}}
	got = printWithDefault(t, t.TempDir(), localModelsHome(t, "x"), model, nil, "-p", "--model", "local/x", "--thinking", "low", "hello")
	assert.Equal(t, result{"ok\n", "Warning: local/x has no thinking; level low is not used.\n", 0}, got)
}

// The Token Plan wire in the pattern and the level suffix both reach the
// request through the model list.
func TestPrintTokenPlanPatternWithLevel(t *testing.T) {
	pattern := func(level string) string {
		return anthropic.ProviderID + "/" + providers.ModelDeepSeekFlash + "@" + string(providers.APIOpenAICompletions) + ":" + level
	}
	high := &scriptedModel{answers: []string{completionsText("thinking")}}
	got := printWithDefault(t, t.TempDir(), privateHome(t), high, tokenPlanKey, "-p", "--model", pattern("high"), "hello")
	require.Equal(t, result{"thinking\n", "", 0}, got)
	off := &scriptedModel{answers: []string{completionsText("plain")}}
	got = printWithDefault(t, t.TempDir(), privateHome(t), off, tokenPlanKey, "-p", "--model", pattern("off"), "hello")
	require.Equal(t, result{"plain\n", "", 0}, got)

	assert.Equal(t, providers.ModelDeepSeekFlash, high.body(t, 0)["model"])
	assert.Equal(t, true, high.body(t, 0)["enable_thinking"], "the level of the pattern turns thinking on")
	assert.Equal(t, false, off.body(t, 0)["enable_thinking"])
}

// A Token Plan pattern with no wire keeps the Messages wire, as print mode
// always did, and the level reaches that wire.
func TestPrintTokenPlanPatternPrefersMessagesWire(t *testing.T) {
	model := &scriptedModel{answers: []string{sseText("messages")}}

	got := printWithDefault(t, t.TempDir(), privateHome(t), model, tokenPlanKey, "-p", "--model", anthropic.ProviderID+"/"+providers.ModelDeepSeekFlash+":high", "hello")

	assert.Equal(t, result{"messages\n", "", 0}, got)
	body := model.body(t, 0)
	assert.Equal(t, providers.ModelDeepSeekFlash, body["model"])
	cfg, _ := body["output_config"].(map[string]any)
	assert.Equal(t, "high", cfg["effort"], "the Messages body has the level of the pattern")
}

// The level of the pattern reaches the request of a catalog model.
func TestPrintModelPatternSelectsCatalogModel(t *testing.T) {
	model := &scriptedModel{answers: []string{responsesConnectedSSE()}}
	env := map[string]string{"OPENAI_API_KEY": fixtureKey}

	got := printWithDefault(t, t.TempDir(), privateHome(t), model, env, "-p", "--model", "openai/"+providers.ModelGPT55+":low", "hello")

	assert.Equal(t, result{"connected\n", "", 0}, got)
	body := model.body(t, 0)
	assert.Equal(t, providers.ModelGPT55, body["model"])
	reasoning, _ := body["reasoning"].(map[string]any)
	assert.Equal(t, "low", reasoning["effort"])

	// --thinking wins over the level of the pattern.
	model = &scriptedModel{answers: []string{responsesConnectedSSE()}}
	got = printWithDefault(t, t.TempDir(), privateHome(t), model, env, "-p", "--thinking", "high", "--model", "openai/"+providers.ModelGPT55+":low", "hello")
	require.Equal(t, 0, got.code, got.stderr)
	reasoning, _ = model.body(t, 0)["reasoning"].(map[string]any)
	assert.Equal(t, "high", reasoning["effort"])
}

// Without --model, the scope chooses the start model: the saved default when
// it is in the scope, else the first scope entry of a usable provider.
func TestPrintStartsOnScope(t *testing.T) {
	home := localModelsHome(t, "a", "b")
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"enabledModels": ["local/b", "local/a"]}`), 0o600))
	model := &scriptedModel{answers: []string{completionsText("ok")}}

	got := printWithDefault(t, t.TempDir(), home, model, nil, "-p", "hello")
	require.Equal(t, result{"ok\n", "", 0}, got)
	assert.Equal(t, "b", model.body(t, 0)["model"], "the first entry of enabledModels")

	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"defaultModel": "local/a@openai-completions", "enabledModels": ["local/b", "local/a"]}`), 0o600))
	model = &scriptedModel{answers: []string{completionsText("ok")}}
	got = printWithDefault(t, t.TempDir(), home, model, nil, "-p", "hello")
	require.Equal(t, result{"ok\n", "", 0}, got)
	assert.Equal(t, "a", model.body(t, 0)["model"], "the saved default inside the scope wins")

	// --models replaces enabledModels, and an entry that matches nothing is a
	// warning line.
	model = &scriptedModel{answers: []string{completionsText("ok")}}
	got = printWithDefault(t, t.TempDir(), home, model, nil, "-p", "--models", "nope-x/none,local/b", "hello")
	require.Equal(t, 0, got.code, got.stderr)
	assert.Equal(t, "b", model.body(t, 0)["model"])
	assert.Contains(t, got.stderr, `Warning: --models: unknown model "nope-x/none"`)
}

// An enabledModels entry that matches nothing is a settings warning with its
// position, never the text of the file.
func TestPrintEnabledModelsProblemNamesPosition(t *testing.T) {
	home := localModelsHome(t, "a")
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"enabledModels": ["secret-looking-x/none", "local/a"]}`), 0o600))
	model := &scriptedModel{answers: []string{completionsText("ok")}}

	got := printWithDefault(t, t.TempDir(), home, model, nil, "-p", "hello")

	require.Equal(t, 0, got.code, got.stderr)
	assert.Contains(t, got.stderr, "Warning: settings: "+filepath.Join(home, "settings.json")+": enabledModels[0]: matches no model; it is ignored")
	assert.NotContains(t, got.stderr, "secret-looking-x")
}
