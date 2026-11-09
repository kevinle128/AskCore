package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// defaultFake is a session on current with no chosen thinking level.
func defaultFake(current string, rows []protocol.ACPModel, scope []protocol.ACPScopedModel) *fakeClient {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: current, Models: rows, UsableOnly: true, Scope: scope})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: current, ThinkingLevel: protocol.ThinkingDefault})
	return fc
}

// levelSketch is a session on openai/gpt-5.5 of the cycle rows at level, after
// the reply of the sketches: the context use is 4%.
func levelSketch(t *testing.T, level protocol.ThinkingLevel, edit func(*fakeClient)) *viewRig {
	t.Helper()
	fc := defaultFake(openaiGPT55, cycleRows(), nil)
	fc.state.ThinkingLevel = level
	if edit != nil {
		edit(fc)
	}
	r := startView(t, 80, 24, withFake(fc))
	r.sketchTurn("gpt-5.5", cycleTurn)
	r.h.waitText("◐ " + string(level))
	return r
}

// pressThinking presses shift+tab, waits for the call that asks want and lets
// the answer land.
func (r *viewRig) pressThinking(want protocol.ThinkingLevel, n int) {
	r.t.Helper()
	r.key("shift+tab")
	r.waitCallCount("set_thinking:"+string(want), n)
	r.h.settle()
}

func TestThinkingCycleFromDefault(t *testing.T) {
	r := levelSketch(t, protocol.ThinkingDefault, nil)
	r.pressThinking(protocol.ThinkingOff, 1)
	r.h.waitText("◐ off")
	require.Equal(t, ` ⌂ ~/src/askcore                              openai/gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 4%`, r.footerOf())
	require.Empty(t, r.railLines(), "the status row stays empty")
	require.Equal(t, []string{"set_thinking:off"}, r.fc.callsWith("set_thinking:"))
}

func TestThinkingCycleBackToDefault(t *testing.T) {
	t.Run("from max", func(t *testing.T) {
		r := levelSketch(t, protocol.ThinkingMax, nil)
		r.pressThinking(protocol.ThinkingDefault, 1)
		r.h.waitText("◐ default")
		require.Equal(t, ` ⌂ ~/src/askcore                          openai/gpt-5.5 ◐ default  ▱▱▱▱▱▱▱▱ 4%`, r.footerOf())
		require.Equal(t, []string{"set_thinking:default"}, r.fc.callsWith("set_thinking:"))
	})
	t.Run("clamped", func(t *testing.T) {
		// The model clamps xhigh and max to high, so two presses answer high.
		r := levelSketch(t, protocol.ThinkingDefault, func(fc *fakeClient) { fc.thinkingCap = protocol.ThinkingHigh })
		for _, l := range thinkingLevels[1:] {
			r.pressThinking(l, 1)
		}
		r.pressThinking(protocol.ThinkingDefault, 1)
		r.h.waitText("◐ default")
		require.True(t, strings.HasSuffix(r.footerOf(), "openai/gpt-5.5 ◐ default  ▱▱▱▱▱▱▱▱ 4%"), r.footerOf())
		require.Len(t, r.fc.callsWith("set_thinking:"), 8, "eight presses")
	})
}

func TestThinkingCycleRefusedWhileRunning(t *testing.T) {
	r := levelSketch(t, protocol.ThinkingDefault, nil)
	r.event(flat(
		&protocol.AgentStart{},
		userAt("show me the probe code", sketchTime(14, 3)),
		toolStart("c1", "read", `{"path":"internal/leader/client.go"}`, 0),
	)...)
	r.h.waitText("Working…")
	r.key("shift+tab")
	r.h.waitText("Thinking level can change when the run ends.")
	require.Equal(t, []string{"┊ Thinking level can change when the run ends."}, r.railLines())
	require.True(t, strings.HasSuffix(r.footerOf(), "openai/gpt-5.5 ◐ default  ▱▱▱▱▱▱▱▱ 4%"), r.footerOf())
	require.Zero(t, r.fc.count("set_thinking:"))
}

func TestThinkingDefaultRefusedByOlderLeader(t *testing.T) {
	r := levelSketch(t, protocol.ThinkingMax, func(fc *fakeClient) { fc.refuseDefault = true })
	r.pressThinking(protocol.ThinkingDefault, 1)
	r.h.waitText("Restart it with this build: ask leader stop")
	requireContentGolden(t, r.h)
	require.Equal(t, []string{
		"┊ Could not change the thinking level: the leader does not know default.",
		"┊ Restart it with this build: ask leader stop",
	}, r.railLines())
	require.Equal(t, ` ⌂ ~/src/askcore                              openai/gpt-5.5 ◐ max  ▱▱▱▱▱▱▱▱ 4%`, r.footerOf())

	r.pressThinking(protocol.ThinkingOff, 1)
	r.h.waitText("◐ off")
	require.Equal(t, []string{"set_thinking:default", "set_thinking:off"}, r.fc.callsWith("set_thinking:"))
}

// defaultScope is --models 'openai/gpt-5.5,anthropic/claude-sonnet-4-6,
// local/llama4-scout': no entry states a level.
func defaultScope(first, second string) []protocol.ACPScopedModel {
	return []protocol.ACPScopedModel{{ModelID: first}, {ModelID: second}, {ModelID: llamaScout}}
}

func TestModelCycleKeyKeepsDefault(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		fc := defaultFake(openaiGPT55, cycleRows(), defaultScope(openaiGPT55, sonnet46))
		r := startView(t, 80, 24, withFake(fc))
		r.sketchTurn("gpt-5.5", cycleTurn)
		r.key("ctrl+p")
		r.waitCall("set_model:" + sonnet46)
		r.h.waitText("2 of 3")
		requireContentGolden(t, r.h)
		require.Equal(t, []string{"┊ ✓ Model: anthropic/claude-sonnet-4-6 · default · 2 of 3"}, r.railLines())
		require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-sonnet-4-6 ◐ default  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
		r.fc.mu.Lock()
		require.Equal(t, []protocol.ThinkingLevel{""}, r.fc.askedLevels, "an entry with no level keeps the level")
		r.fc.mu.Unlock()
	})
	t.Run("40x14", func(t *testing.T) {
		fc := defaultFake(sonnet46, cycleRows(), defaultScope(sonnet46, openaiGPT55))
		r := startView(t, 40, 14, withFake(fc))
		r.sketchTurn("gpt-5.5", cycleTurn)
		r.key("ctrl+p")
		r.waitCall("set_model:" + openaiGPT55)
		r.h.waitText("✓ openai/gpt-5.5")
		requireContentGolden(t, r.h)
		require.Equal(t, []string{"┊ ✓ openai/gpt-5.5 · default"}, r.railLines())
		require.Equal(t, "                      openai/gpt-5.5 4%", r.footerOf())
	})
	t.Run("an entry :default removes a chosen level", func(t *testing.T) {
		scope := defaultScope(openaiGPT55, sonnet46)
		scope[1].ThinkingLevel = protocol.ThinkingDefault
		fc := defaultFake(openaiGPT55, cycleRows(), scope)
		fc.state.ThinkingLevel = protocol.ThinkingHigh
		r := startView(t, 80, 24, withFake(fc))
		r.sketchTurn("gpt-5.5", cycleTurn)
		r.key("ctrl+p")
		r.h.waitText("2 of 3")
		require.Equal(t, []string{"┊ ✓ Model: anthropic/claude-sonnet-4-6 · default · 2 of 3"}, r.railLines())
		r.fc.mu.Lock()
		require.Equal(t, []protocol.ThinkingLevel{protocol.ThinkingDefault}, r.fc.askedLevels)
		r.fc.mu.Unlock()
	})
}
