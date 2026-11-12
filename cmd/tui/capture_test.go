package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/providers"
)

// TestCaptureCreatesPrivateFolderAndCassette checks that a capture folder
// made by ask and the cassette in it are readable by the owner only.
func TestCaptureCreatesPrivateFolderAndCassette(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "made", "by", "ask")
	o := options{provider: providers.ProviderAnthropic, model: providers.AnthropicSonnet46().ID}
	path, err := startCapture(&o, func(k string) string {
		if k == captureEnv {
			return dir
		}
		return ""
	}, os.Stderr, sseResponse(http.StatusOK, sseText("pong")))
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://provider.invalid/v1/messages", nil)
	require.NoError(t, err)
	resp, err := o.transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestCaptureRecordsWhenOnlyModelPicksTheProvider checks that a print run
// whose provider comes from --model alone is captured, not skipped.
func TestCaptureRecordsWhenOnlyModelPicksTheProvider(t *testing.T) {
	capDir := filepath.Join(t.TempDir(), "cap")
	sonnet := providers.AnthropicSonnet46()
	model := &scriptedModel{answers: []string{sseText("pong")}}
	env := map[string]string{"ANTHROPIC_API_KEY": fixtureKey, captureEnv: capDir}

	got := printWithDefault(t, t.TempDir(), privateHome(t), model, env, "-p", "--model", acp.ModelID(sonnet), "ping")

	assert.Equal(t, 0, got.code, got.stderr)
	assert.NotContains(t, got.stderr, "ignored")
	assert.Contains(t, got.stderr, "capturing provider HTTP to")
	files, err := filepath.Glob(filepath.Join(capDir, "*.yaml"))
	require.NoError(t, err)
	assert.Len(t, files, 1)
}

// TestFauxPaceIsReadOnlyForFauxRuns checks that a bad ASK_FAUX_TPS stops a
// faux run and leaves a real-provider run alone.
func TestFauxPaceIsReadOnlyForFauxRuns(t *testing.T) {
	sonnet := providers.AnthropicSonnet46()
	env := map[string]string{"ANTHROPIC_API_KEY": fixtureKey, fauxPaceEnv: "bad"}

	model := &scriptedModel{answers: []string{sseText("pong")}}
	got := printWithDefault(t, t.TempDir(), privateHome(t), model, env, "-p", "--model", acp.ModelID(sonnet), "ping")
	assert.Equal(t, result{"pong\n", "", 0}, got)

	got = printWithDefault(t, t.TempDir(), privateHome(t), &scriptedModel{}, env, "-p", "ping")
	assert.Equal(t, 1, got.code)
	assert.Contains(t, got.stderr, fauxPaceEnv+" must be a non-negative number")
}
