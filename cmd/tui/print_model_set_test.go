package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
)

var tokenPlanKey = map[string]string{"ALIBABA_TOKEN_PLAN_API_KEY": fixtureKey}

// Print mode on Token Plan resolves the row through the model set, so the user
// models.json applies to it like to every other provider.
func TestPrintTokenPlanUsesModelSet(t *testing.T) {
	home := privateHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, "models.json"), []byte(`{"providers": {
  "`+anthropic.ProviderID+`": {"modelOverrides": {"`+providers.ModelDeepSeekFlash+`": {"maxTokens": 1234}}},
}}`), 0o600))
	model := &scriptedModel{answers: []string{sseText("from the plan")}}

	got := printWithDefault(t, t.TempDir(), home, model, tokenPlanKey, "-p", "--provider", anthropic.ProviderID, "hello")

	assert.Equal(t, result{"from the plan\n", "", 0}, got)
	body := model.body(t, 0)
	assert.Equal(t, providers.ModelDeepSeekFlash, body["model"])
	assert.EqualValues(t, 1234, body["max_tokens"], "the override of the user file reaches the request")
	assert.NotContains(t, got.stdout+got.stderr, fixtureKey)
}

// A bad user models.json gives a warning line; the run still uses the catalog.
func TestPrintBadModelsFileWarnsAndRuns(t *testing.T) {
	home := privateHome(t)
	path := filepath.Join(home, "models.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"providers": {`), 0o600))
	model := &scriptedModel{answers: []string{sseText("still runs")}}

	got := printWithDefault(t, t.TempDir(), home, model, tokenPlanKey, "-p", "--provider", anthropic.ProviderID, "hello")

	require.Equal(t, 0, got.code, got.stderr)
	assert.Equal(t, "still runs\n", got.stdout)
	assert.Contains(t, got.stderr, "Warning: settings: "+path)
	assert.Equal(t, 1, model.requests())
}
