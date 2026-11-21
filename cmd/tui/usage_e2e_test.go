package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usageOf returns the usage object of the last agent_settled event.
func usageOf(t *testing.T, stdout string) map[string]any {
	t.Helper()
	settled := eventsOfType(t, stdout, "agent_settled")
	require.NotEmpty(t, settled, stdout)
	usage, ok := settled[len(settled)-1]["usage"].(map[string]any)
	require.True(t, ok, "agent_settled has no usage: %v", settled[len(settled)-1])
	return usage
}

// number reads a JSON number at a path of keys.
func number(t *testing.T, v map[string]any, path ...string) float64 {
	t.Helper()
	var cur any = v
	for _, k := range path {
		m, ok := cur.(map[string]any)
		require.True(t, ok, "no object before %q", k)
		cur = m[k]
	}
	n, ok := cur.(float64)
	require.True(t, ok, "%v is not a number", path)
	return n
}

func TestJSONTotalsAddRetriedAttempts(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f,
		answer(http.StatusInternalServerError, "application/json", `{"type":"error","error":{"type":"api_error","message":"busy"}}`),
		answer(http.StatusOK, "text/event-stream", sseText("pong")),
	)

	got := printSonnet(t, f, "go", "--mode", "json")

	require.Equal(t, 0, got.code, got.stderr)
	usage := usageOf(t, got.stdout)
	assert.EqualValues(t, 10, number(t, usage, "total", "input"))
	assert.EqualValues(t, 3, number(t, usage, "total", "output"))
	// The refused request streamed nothing, so it cost nothing and the total
	// stays complete.
	assert.Equal(t, true, usage["complete"])
}

func TestJSONTotalsAddTwoPrompts(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f,
		answer(http.StatusOK, "text/event-stream", sseText("one")),
		answer(http.StatusOK, "text/event-stream", sseText("two")),
	)
	dir, home := canonicalTempDir(t), privateHome(t)

	got := runBuilt(t, f, dir, home, "-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "--mode", "json", "first", "second")

	require.Equal(t, 0, got.code, got.stderr)
	usage := usageOf(t, got.stdout)
	assert.EqualValues(t, 20, number(t, usage, "total", "input"))
	assert.EqualValues(t, 6, number(t, usage, "total", "output"))
	assert.Equal(t, true, usage["complete"])
	assert.EqualValues(t, 1_000_000, number(t, usage, "contextWindow"))
	assert.Positive(t, number(t, usage, "contextTokens"))
}

func TestResumeKeepsUsageTotals(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f,
		answer(http.StatusOK, "text/event-stream", sseText("one")),
		answer(http.StatusOK, "text/event-stream", sseText("two")),
	)
	dir, home := canonicalTempDir(t), privateHome(t)
	base := []string{"-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6"}

	require.Equal(t, 0, runBuilt(t, f, dir, home, append(base, "first")...).code)
	got := runBuilt(t, f, dir, home, append(base, "-c", "--mode", "json", "second")...)

	require.Equal(t, 0, got.code, got.stderr)
	usage := usageOf(t, got.stdout)
	assert.EqualValues(t, 20, number(t, usage, "total", "input"), "the totals of both runs")
	assert.EqualValues(t, 6, number(t, usage, "total", "output"))
}

func TestJSONUnpricedModelIsCostIncomplete(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f, answer(http.StatusOK, "text/event-stream", sseText("pong")))

	got := printSonnet(t, f, "go", "--mode", "json")

	require.Equal(t, 0, got.code, got.stderr)
	usage := usageOf(t, got.stdout)
	assert.Equal(t, true, usage["complete"])
	assert.Equal(t, false, usage["costComplete"], "the row has no price")
	assert.EqualValues(t, 0, number(t, usage, "total", "cost", "total"))
}

func TestJSONPricedModelShowsCumulativeCost(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f,
		answer(http.StatusOK, "text/event-stream", sseText("one")),
		answer(http.StatusOK, "text/event-stream", sseText("two")),
	)
	dir, home := canonicalTempDir(t), privateHome(t)
	// All four prices make the row priced. Prices are USD for each million
	// tokens, so a run of 10 input and 3 output tokens costs 10*3 + 3*15 micro-USD.
	models := `{"providers":{"anthropic":{"modelOverrides":{"claude-sonnet-4-6":{"cost":{"input":3,"output":15,"cacheRead":1,"cacheWrite":4}}}}}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, "models.json"), []byte(models), 0o600))
	base := []string{"-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6"}

	require.Equal(t, 0, runBuilt(t, f, dir, home, append(base, "first")...).code)
	got := runBuilt(t, f, dir, home, append(base, "-c", "--mode", "json", "second")...)

	require.Equal(t, 0, got.code, got.stderr)
	usage := usageOf(t, got.stdout)
	assert.Equal(t, true, usage["costComplete"])
	assert.EqualValues(t, 2*(10*3+3*15), number(t, usage, "total", "cost", "total"))
}
