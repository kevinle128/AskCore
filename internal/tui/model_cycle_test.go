package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The rows of the model cycle sketches. local/llama4-scout is a custom row
// with reasoning off. The windows make the context use of one reply of 120k
// tokens 12% on claude-sonnet-4-6, 4% on gpt-5.5 and 13% on llama4-scout, as
// the sketches show.
const (
	llamaScout = "local/llama4-scout@openai-completions"
	qwenCoder  = "local/qwen3-coder:30b@openai-completions"
	cycleTurn  = 120_000
)

func cycleRows() []protocol.ACPModel {
	row := func(id, provider, name, api string, window int, reasoning bool) protocol.ACPModel {
		return protocol.ACPModel{ModelID: id, Provider: provider, ID: name, API: api, ContextWindow: window, Reasoning: reasoning}
	}
	return []protocol.ACPModel{
		row(opus47, "anthropic", "claude-opus-4-7", "messages", 1_000_000, true),
		row(sonnet46, "anthropic", "claude-sonnet-4-6", "messages", 1_000_000, true),
		row(llamaScout, "local", "llama4-scout", "openai-completions", 920_000, false),
		row(qwenCoder, "local", "qwen3-coder:30b", "openai-completions", 256_000, false),
		row(miniModel, "openai", "gpt-5.4-mini", "responses", 400_000, true),
		row(openaiGPT55, "openai", "gpt-5.5", "responses", 3_000_000, true),
	}
}

// sketchScope is --models 'anthropic/claude-sonnet-4-6:medium,
// openai/gpt-5.5:high,local/llama4-scout:high'.
func sketchScope() []protocol.ACPScopedModel {
	return []protocol.ACPScopedModel{
		{ModelID: sonnet46, ThinkingLevel: protocol.ThinkingMedium},
		{ModelID: openaiGPT55, ThinkingLevel: protocol.ThinkingHigh},
		{ModelID: llamaScout, ThinkingLevel: protocol.ThinkingHigh},
	}
}

// cycleFake is a session on current at the level medium with a scope.
func cycleFake(current string, scope []protocol.ACPScopedModel) *fakeClient {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: current, Models: cycleRows(), UsableOnly: true, Scope: scope})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: current, ThinkingLevel: protocol.ThinkingMedium})
	return fc
}

func cycleSketch(t *testing.T, w, h int, fc *fakeClient) *viewRig {
	r := startView(t, w, h, withFake(fc))
	r.sketchTurn("claude-sonnet-4-6", cycleTurn)
	r.h.waitText("take 1.2 s.")
	return r
}

// railLines returns the rows of the status rail: the rows that start with ┊.
func (r *viewRig) railLines() []string {
	var out []string
	for _, row := range r.content() {
		if strings.HasPrefix(row, GlyphRailStatus+" ") {
			out = append(out, row)
		}
	}
	return out
}

// paneRows returns the rows of the block pane at a settled screen: the rows
// above the status rail or the prompt rule.
func (r *viewRig) paneRows() []string {
	rows := r.content()
	for i, row := range rows {
		if strings.HasPrefix(row, GlyphRailStatus+" ") || (row != "" && strings.Trim(row, "─") == "") {
			return rows[:i]
		}
	}
	return rows
}

func TestModelCycleKeyMovesThroughScope(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		r := cycleSketch(t, 80, 24, cycleFake(sonnet46, sketchScope()))
		require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
		before := r.paneRows()

		r.key("ctrl+p")
		r.waitCall("set_model:" + openaiGPT55)
		r.h.waitText("2 of 3")
		requireContentGolden(t, r.h)
		require.Equal(t, []string{"┊ ✓ Model: openai/gpt-5.5 · high · 2 of 3"}, r.railLines())
		require.True(t, strings.HasSuffix(r.footerOf(), "openai/gpt-5.5 ◐ high  ▱▱▱▱▱▱▱▱ 4%"), r.footerOf())
		require.Equal(t, sketchBar, r.rows()[23], "the shortcut bar does not change")
		after := r.paneRows()
		require.Equal(t, before[:len(after)], after, "the pane gets no block")
		r.fc.mu.Lock()
		require.Equal(t, []protocol.ThinkingLevel{protocol.ThinkingHigh}, r.fc.askedLevels, "the level of the entry goes in the same call")
		require.False(t, r.fc.savedDefault[openaiGPT55], "a press does not save the default")
		r.fc.mu.Unlock()

		r.key("ctrl+shift+p")
		r.h.waitText("1 of 3")
		require.Equal(t, []string{"┊ ✓ Model: anthropic/claude-sonnet-4-6 · medium · 1 of 3"}, r.railLines())
		require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
	})
	t.Run("the last entry wraps to the first", func(t *testing.T) {
		r := cycleSketch(t, 80, 24, cycleFake(llamaScout, sketchScope()))
		r.key("ctrl+p")
		r.waitCall("set_model:" + sonnet46)
		r.h.waitText("1 of 3")
		require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
	})
	t.Run("40x14", func(t *testing.T) {
		r := cycleSketch(t, 40, 14, cycleFake(sonnet46, sketchScope()))
		r.key("ctrl+p")
		r.waitCall("set_model:" + openaiGPT55)
		r.h.waitText("✓ openai/gpt-5.5")
		requireContentGolden(t, r.h)
		require.Equal(t, []string{"┊ ✓ openai/gpt-5.5 · high"}, r.railLines())
		require.Equal(t, "                      openai/gpt-5.5 4%", r.footerOf())
	})
	t.Run("a resize draws the notice in the form of the new width", func(t *testing.T) {
		r := cycleSketch(t, 80, 24, cycleFake(sonnet46, sketchScope()))
		r.key("ctrl+p")
		r.h.waitText("2 of 3")
		r.h.resize(40, 14)
		r.h.waitText("✓ openai/gpt-5.5 · high")
		r.requireNotOnScreen("Model: ", "2 of 3")
		r.h.resize(80, 24)
		r.h.waitText("┊ ✓ Model: openai/gpt-5.5 · high · 2 of 3")
	})
	t.Run("a model that is not in the list starts at the first or the last entry", func(t *testing.T) {
		r := cycleSketch(t, 80, 24, cycleFake(qwenCoder, sketchScope()))
		r.key("ctrl+shift+p")
		r.waitCall("set_model:" + llamaScout)
		r.h.waitText("3 of 3")
		r.key("ctrl+p")
		r.h.waitText("1 of 3")
	})
}

func TestModelCycleKeyWithoutScope(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		r := cycleSketch(t, 80, 24, cycleFake(miniModel, nil))
		r.key("ctrl+p")
		r.waitCall("set_model:" + openaiGPT55)
		r.h.waitText("6 of 6")
		requireContentGolden(t, r.h)
		require.Equal(t, []string{"┊ ✓ Model: openai/gpt-5.5 · medium · 6 of 6"}, r.railLines())
		require.True(t, strings.HasSuffix(r.footerOf(), "openai/gpt-5.5 ◐ medium  ▱▱▱▱▱▱▱▱ 4%"), r.footerOf())
		r.fc.mu.Lock()
		require.Equal(t, []protocol.ThinkingLevel{""}, r.fc.askedLevels, "with no scope the session keeps its level")
		r.fc.mu.Unlock()
	})
	t.Run("the list order is the order of /model", func(t *testing.T) {
		r := cycleSketch(t, 80, 24, cycleFake(openaiGPT55, nil))
		r.key("ctrl+p")
		r.waitCall("set_model:" + opus47)
		r.h.waitText("1 of 6")
	})
	t.Run("a row that left models.json is not in the list", func(t *testing.T) {
		fc := cycleFake(qwenCoder, nil)
		rows := cycleRows()
		rows[3].NotInConfig = true
		fc.setModels(protocol.ACPModelsResult{Current: qwenCoder, Models: rows, UsableOnly: true})
		r := cycleSketch(t, 80, 24, fc)
		r.key("ctrl+p")
		r.waitCall("set_model:" + opus47)
		r.h.waitText("1 of 5")
	})
}

func TestModelCycleKeyNothingToCycle(t *testing.T) {
	fc := cycleFake(sonnet46, sketchScope()[:1])
	r := cycleSketch(t, 80, 24, fc)
	r.key("ctrl+p")
	r.h.waitText(noOtherModelText)
	requireContentGolden(t, r.h)
	require.Equal(t, []string{"┊ No other model to cycle to."}, r.railLines())
	require.Zero(t, r.fc.count("set_model:"))
	require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
}

func TestModelCycleKeyRefusedWhileRunning(t *testing.T) {
	r := cycleSketch(t, 80, 24, cycleFake(sonnet46, sketchScope()))
	r.event(flat(
		&protocol.AgentStart{},
		userAt("show me the probe code", sketchTime(14, 3)),
		toolStart("c1", "read", `{"path":"internal/leader/client.go"}`, 0),
	)...)
	r.h.waitText("Working…")
	r.key("ctrl+p")
	r.h.waitText(modelBusyText)
	requireContentGolden(t, r.h)
	require.Equal(t, []string{"┊ Model can change when the run ends."}, r.railLines())
	require.Zero(t, r.fc.count("set_model:"))
	require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
}

func TestModelCycleKeyIgnoredWhileSwitching(t *testing.T) {
	fc := cycleFake(sonnet46, sketchScope())
	gate := make(chan struct{})
	fc.setModelGate[openaiGPT55] = gate
	r := cycleSketch(t, 80, 24, fc)
	r.key("ctrl+p")
	r.waitCall("set_model:" + openaiGPT55)
	r.key("ctrl+p", "ctrl+shift+p")
	r.h.settle()
	require.Equal(t, 1, r.fc.count("set_model:"), "a press while a change waits is ignored")
	r.requireNotOnScreen("of 3", modelBusyText)
	close(gate)
	r.h.waitText("┊ ✓ Model: openai/gpt-5.5 · high · 2 of 3")
	require.Equal(t, 1, r.fc.count("set_model:"))
	// The next press starts from the new current model.
	r.key("ctrl+p")
	r.waitCall("set_model:" + llamaScout)
}

func TestModelCycleKeyDroppedLevel(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		r := cycleSketch(t, 80, 24, cycleFake(openaiGPT55, sketchScope()))
		r.key("ctrl+p")
		r.waitCall("set_model:" + llamaScout)
		r.h.waitText("3 of 3")
		requireContentGolden(t, r.h)
		require.Equal(t, []string{
			"┊ ✓ Model: local/llama4-scout · 3 of 3",
			"┊ local/llama4-scout has no thinking; level high is not used.",
		}, r.railLines())
		require.NotContains(t, r.footerOf(), GlyphLevel)
		require.True(t, strings.HasSuffix(r.footerOf(), "local/llama4-scout  ▰▱▱▱▱▱▱▱ 13%"), r.footerOf())
	})
	t.Run("40x14", func(t *testing.T) {
		r := cycleSketch(t, 40, 14, cycleFake(openaiGPT55, sketchScope()))
		r.key("ctrl+p")
		r.h.waitText("✓ local/llama4-scout")
		requireContentGolden(t, r.h)
		require.Equal(t, []string{
			"┊ ✓ local/llama4-scout",
			"┊ local/llama4-scout has no thinking;",
			"┊ level high is not used.",
		}, r.railLines())
	})
	t.Run("an entry with no level has no dropped row", func(t *testing.T) {
		scope := sketchScope()
		scope[2].ThinkingLevel = ""
		r := cycleSketch(t, 80, 24, cycleFake(openaiGPT55, scope))
		r.key("ctrl+p")
		r.h.waitText("3 of 3")
		require.Equal(t, []string{"┊ ✓ Model: local/llama4-scout · 3 of 3"}, r.railLines())
	})
}

func TestModelCycleKeyInBlockList(t *testing.T) {
	r := cycleSketch(t, 80, 24, cycleFake(sonnet46, sketchScope()))
	r.key("tab")
	r.h.settle()
	before := r.paneRows()
	r.key("ctrl+p")
	r.h.waitText("2 of 3")
	require.True(t, strings.HasSuffix(r.footerOf(), "openai/gpt-5.5 ◐ high  ▱▱▱▱▱▱▱▱ 4%"), r.footerOf())
	after := r.paneRows()
	// The status row takes one row of the pane; the rows that stay are the
	// same, so the selection did not move.
	require.Equal(t, before[:len(after)], after)
}

func TestModelCycleKeyInSelectorMovesUp(t *testing.T) {
	r := cycleSketch(t, 80, 24, cycleFake(sonnet46, sketchScope()))
	r.key("ctrl+l")
	r.h.waitText("❯   claude-sonnet-4-6")
	r.key("ctrl+p")
	r.h.waitText("❯   claude-opus-4-7")
	r.key("ctrl+shift+p")
	r.h.settle()
	r.requireOnScreen("❯   claude-opus-4-7", "Select model")
	require.Zero(t, r.fc.count("set_model:"))
	require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
}

// A press answer that arrives while /model is open changes the footer row and
// leaves the list open.
func TestModelCycleAnswerKeepsSelectorOpen(t *testing.T) {
	fc := cycleFake(sonnet46, sketchScope())
	gate := make(chan struct{})
	fc.setModelGate[openaiGPT55] = gate
	r := cycleSketch(t, 80, 24, fc)
	r.key("ctrl+p")
	r.waitCall("set_model:" + openaiGPT55)
	r.key("ctrl+l")
	r.h.waitText("Select model")
	close(gate)
	r.h.waitFor("the new model in the footer row", func(*screenHarness) bool {
		return strings.Contains(r.footerOf(), "openai/gpt-5.5 ◐ high")
	})
	r.h.settle()
	r.requireOnScreen("Select model")
}

func TestModelCycleKeyReadsListWhenNone(t *testing.T) {
	t.Run("the press reads the list first", func(t *testing.T) {
		fc := cycleFake(sonnet46, sketchScope())
		fc.modelsErrUntil = 1
		r := cycleSketch(t, 80, 24, fc)
		r.key("ctrl+p")
		r.waitCall("set_model:" + openaiGPT55)
		r.h.waitText("2 of 3")
		require.Equal(t, 2, r.fc.count("models"))
	})
	t.Run("a list read that fails", func(t *testing.T) {
		fc := cycleFake(sonnet46, sketchScope())
		fc.modelsErrUntil = 100
		r := cycleSketch(t, 80, 24, fc)
		r.key("ctrl+p")
		r.h.waitText("Could not read the model list: models are not available.")
		require.Zero(t, r.fc.count("set_model:"))
	})
	t.Run("the problems of that list go to the pane", func(t *testing.T) {
		fc := cycleFake(sonnet46, sketchScope())
		fc.modelsErrUntil = 1
		fc.models.Diagnostics = reloadProblems()
		r := cycleSketch(t, 80, 24, fc)
		r.key("ctrl+p")
		r.h.waitText("2 of 3")
		r.requireOnScreen(modelProblemsHead)
		r.requireNotOnScreen("Select model")
	})
}

// A scope entry of a provider with no credential is refused by the host; the
// model does not change.
func TestModelCycleKeyNoCredential(t *testing.T) {
	const grok = "xai/grok-5-code@chat"
	scope := append(sketchScope()[:1], protocol.ACPScopedModel{ModelID: grok})
	fc := cycleFake(sonnet46, scope)
	fc.setModelErr[grok] = &RequestError{Kind: string(protocol.ACPErrNoAPIKey), Message: "not signed in"}
	r := cycleSketch(t, 80, 24, fc)
	r.key("ctrl+p")
	r.waitCall("set_model:" + grok)
	r.h.waitText("Model not changed.")
	require.Equal(t, []string{"┊ xai is not ready: no credential. Model not changed. Run in a shell: ask auth …"}, r.railLines()[:1])
	require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
}

func TestModelCycleKeyNames(t *testing.T) {
	for _, tc := range []struct {
		key      string
		back, ok bool
	}{
		{"ctrl+p", false, true},
		{"ctrl+shift+p", true, true},
		{"ctrl+P", true, true},
		{"ctrl+alt+p", false, false},
		{"p", false, false},
		{"ctrl+n", false, false},
	} {
		back, ok := modelCycleKey(keyNamed(t, tc.key))
		require.Equal(t, tc.ok, ok, tc.key)
		require.Equal(t, tc.back, back, tc.key)
	}
}

// ---- models.json problems, changed rows, dropped levels ----

func reloadProblems() []protocol.ACPDiagnostic {
	const path = "/home/u/.ask/models.json"
	return []protocol.ACPDiagnostic{
		{Layer: "user", Path: path, Key: "providers.local.models[2].contextWindow", Text: "must be a number; the model is skipped"},
		{Layer: "user", Path: path, Key: "providers.gateway", Text: "unknown key"},
	}
}

func reloadRows() []protocol.ACPModel {
	rows := cycleRows()
	rows[2].ContextWindow = 128_000
	rows[5].ContextWindow = 1_000_000
	return []protocol.ACPModel{rows[2], rows[3], rows[5]}
}

func reloadFake(diags []protocol.ACPDiagnostic) *fakeClient {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: qwenCoder, Models: reloadRows(), UsableOnly: true, Diagnostics: diags})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: qwenCoder, ThinkingLevel: protocol.ThinkingOff})
	return fc
}

func reloadSketch(t *testing.T, w, h int, fc *fakeClient) *viewRig {
	r := startView(t, w, h, withFake(fc))
	r.sketchTurn("qwen3-coder:30b", 15_360)
	r.h.waitText("6%")
	return r
}

func TestModelsReloadProblemShownOnce(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		r := reloadSketch(t, 80, 24, reloadFake(reloadProblems()))
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.h.waitText(modelProblemsHead)
		requireContentGolden(t, r.h)
		requireRows(t, r.rows(), []string{
			"  " + modelProblemsHead,
			"    ~/.ask/models.json",
			"      providers.local.models[2].contextWindow: must be a number; the model is",
			"        skipped",
			"      providers.gateway: unknown key",
		})
		requireRows(t, r.rows(), []string{
			"│                                                                              │",
			"│  models.json has 2 problems; the list is in the transcript.                  │",
			"╰──────────────────────────────────────────────────────────────────────────────╯",
		})

		r.key("esc")
		r.waitGone("Select model")
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.h.settle()
		r.requireOnScreen("models.json has 2 problems; the list is in the transcript.")
		require.Equal(t, 1, strings.Count(strings.Join(r.h.settleText(), "\n"), modelProblemsHead), "the same set shows once")
	})
	t.Run("the start block already showed the set", func(t *testing.T) {
		fc := reloadFake(reloadProblems())
		fc.state.Diagnostics = append([]protocol.ACPDiagnostic{
			{Layer: "user", Path: "/home/u/.ask/settings.json", Key: "DefaultModel", Text: "unknown key; did you mean defaultModel?"},
		}, reloadProblems()...)
		r := reloadSketch(t, 80, 30, fc)
		r.h.waitText("Settings problems")
		r.key("ctrl+l")
		r.h.waitText("models.json has 2 problems")
		r.h.settle()
		r.requireNotOnScreen(modelProblemsHead)
	})
	t.Run("a changed set adds a block and a fixed file shows nothing", func(t *testing.T) {
		r := reloadSketch(t, 80, 30, reloadFake(reloadProblems()))
		r.key("ctrl+l")
		r.h.waitText(modelProblemsHead)
		r.key("esc")
		r.fc.setModels(protocol.ACPModelsResult{Current: qwenCoder, Models: reloadRows(), UsableOnly: true, Diagnostics: reloadProblems()[1:]})
		r.key("ctrl+l")
		r.h.waitText("models.json has 1 problem;")
		r.h.waitFor("a second block", func(h *screenHarness) bool {
			return strings.Count(strings.Join(h.settleText(), "\n"), modelProblemsHead) == 2
		})
		r.key("esc")
		r.fc.setModels(protocol.ACPModelsResult{Current: qwenCoder, Models: reloadRows(), UsableOnly: true})
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.h.settle()
		r.requireNotOnScreen("models.json has", "models.json:")
		require.Equal(t, 2, strings.Count(strings.Join(r.h.settleText(), "\n"), modelProblemsHead))
	})
	t.Run("a flag problem has no path row", func(t *testing.T) {
		r := reloadSketch(t, 80, 30, reloadFake([]protocol.ACPDiagnostic{{Layer: "user", Key: "--models", Text: "nope/* matches no model"}}))
		r.key("ctrl+l")
		r.h.waitText(modelProblemsHead)
		requireRows(t, r.rows(), []string{
			"  " + modelProblemsHead,
			"      --models: nope/* matches no model",
		})
	})
}

func TestModelsReloadProblemNarrow(t *testing.T) {
	r := reloadSketch(t, 40, 14, reloadFake(reloadProblems()))
	r.key("ctrl+l")
	r.h.waitText("models.json: 2 problems")
	requireContentGolden(t, r.h)
	requireRows(t, r.rows(), []string{
		"╭─ Select model ───────────────────────╮",
		"│  ⌕ type to filter                    │",
		"│ ❯   qwen3-coder:30b 256k  current    │",
		"│    … 4 more                          │",
		"│  models.json: 2 problems             │",
		"╰──────────────────────────────────────╯",
	})
	r.key("esc")
	r.waitGone("Select model")
	t.Run("after esc", func(t *testing.T) {
		requireContentGolden(t, r.h)
		requireRows(t, r.rows(), []string{
			"    ~/.ask/models.json",
			"      providers.local.models[2].contex",
			"        tWindow: must be a number; the",
			"        model is skipped",
			"      providers.gateway: unknown key",
		})
	})
}

func TestModelSelectorNotInConfig(t *testing.T) {
	changed := func(t *testing.T, w, h int) *viewRig {
		rows := reloadRows()
		rows[1].NotInConfig = true
		fc := reloadFake(nil)
		fc.setModels(protocol.ACPModelsResult{Current: qwenCoder, Models: rows, UsableOnly: true})
		r := reloadSketch(t, w, h, fc)
		r.key("ctrl+l")
		r.h.waitText("Select model")
		return r
	}
	t.Run("80x24", func(t *testing.T) {
		r := changed(t, 80, 24)
		r.h.waitText("not in models.json")
		requireContentGolden(t, r.h)
		require.Equal(t, "│ ❯   qwen3-coder:30b · not in models.json                    256k  current    │", screenRowWith(r.rows(), "qwen3-coder"))
	})
	t.Run("40x14", func(t *testing.T) {
		r := changed(t, 40, 14)
		r.h.waitText("changed")
		requireContentGolden(t, r.h)
		require.Equal(t, "│ ❯   qwen3-coder:30b 256k  changed    │", screenRowWith(r.rows(), "qwen3-coder"))
		r.requireNotOnScreen("not in models.json", "current")
	})
	t.Run("choosing the changed row again changes the model", func(t *testing.T) {
		r := changed(t, 80, 24)
		r.key("enter")
		r.waitCall("set_model:" + qwenCoder)
		r.waitGone("Select model")
	})
	t.Run("choosing an unchanged current row only closes the list", func(t *testing.T) {
		r := reloadSketch(t, 80, 24, reloadFake(nil))
		r.key("ctrl+l")
		r.h.waitText("❯   qwen3-coder:30b")
		r.key("enter")
		r.waitGone("Select model")
		require.Zero(t, r.fc.count("set_model:"))
	})
}

func TestModelConfigChangedErrorBlock(t *testing.T) {
	run := func(t *testing.T, w, h int) *viewRig {
		r := reloadSketch(t, w, h, reloadFake(nil))
		text := protocol.ModelConfigChanged
		m := protocol.AssistantMessage{Provider: "local", Model: "qwen3-coder:30b", StopReason: protocol.StopError, ErrorMessage: &text}
		r.event(flat(
			&protocol.AgentStart{},
			userAt("and on macOS?", sketchTime(14, 6)),
			assistantStart("qwen3-coder:30b", 0),
			&protocol.MessageEnd{Envelope: at(0), Message: m},
			&protocol.CycleEnd{Reason: "error", Code: "MODEL_CONFIG"}, &protocol.AgentEnd{}, &protocol.AgentSettled{},
		)...)
		r.h.waitText("Then send again.")
		return r
	}
	t.Run("80x24", func(t *testing.T) {
		r := run(t, 80, 24)
		requireContentGolden(t, r.h)
		requireRows(t, r.rows(), []string{
			"  ✗ Error (config): model configuration changed; choose the model again.",
			"    Choose a model with ctrl+l or /model. Then send again.",
		})
	})
	t.Run("40x14", func(t *testing.T) {
		r := run(t, 40, 14)
		requireContentGolden(t, r.h)
		requireRows(t, r.rows(), []string{
			"  ✗ Error (config): model",
			"    configuration changed; choose the",
			"    model again.",
			"    Choose a model with ctrl+l or",
			"    /model. Then send again.",
		})
	})
}

func TestDroppedLevelStartNotice(t *testing.T) {
	start := func(t *testing.T, w, h int) *viewRig {
		fc := reloadFake(nil)
		fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: llamaScout, ThinkingLevel: protocol.ThinkingOff,
			LevelDropped: &protocol.ACPLevelDropped{ModelID: llamaScout, Level: protocol.ThinkingHigh}})
		r := startView(t, w, h, withFake(fc))
		r.h.waitText("level high is not used.")
		return r
	}
	t.Run("80x24", func(t *testing.T) {
		r := start(t, 80, 24)
		requireContentGolden(t, r.h)
		require.Equal(t, "  local/llama4-scout has no thinking; level high is not used.", r.content()[0])
		require.Equal(t, 1, strings.Count(r.screen(), "level high is not used."), "the notice shows once")
	})
	t.Run("40x14", func(t *testing.T) {
		r := start(t, 40, 14)
		requireContentGolden(t, r.h)
		require.Equal(t, []string{"  local/llama4-scout has no thinking;", "  level high is not used."}, r.content()[:2])
	})
}

// The command list takes only the list keys, so a cycle key reaches the
// shortcuts and the list stays open.
func TestModelCycleKeyWithCommandListOpen(t *testing.T) {
	r := cycleSketch(t, 80, 24, cycleFake(sonnet46, sketchScope()))
	r.typeText("/")
	r.h.waitText("/model")
	r.key("ctrl+p")
	r.h.waitText("2 of 3")
	r.requireOnScreen("/model", "/providers")
	require.True(t, strings.HasSuffix(r.footerOf(), "openai/gpt-5.5 ◐ high  ▱▱▱▱▱▱▱▱ 4%"), r.footerOf())
}
