package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func twoFormatFake() *fakeClient {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: tokenPlanFlash, Models: designModels(true), UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: tokenPlanFlash})
	return fc
}

func TestModelLineGoldens(t *testing.T) {
	t.Run("start_80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(providerFake()))
		requireContentGolden(t, r.h)
		rows := r.content()
		require.Equal(t, "  model  alibaba-token-plan/deepseek-v4.1-flash", rows[1])
		require.Equal(t, "", rows[2])
		header := r.headerOf()
		require.Contains(t, header, "deepseek-v4.1-flash")
		require.NotContains(t, header, "alibaba-token-plan")
	})
	t.Run("start_120x40", func(t *testing.T) {
		r := startView(t, 120, 40, withFake(providerFake()))
		requireContentGolden(t, r.h)
		require.Equal(t, "  model  alibaba-token-plan/deepseek-v4.1-flash", r.content()[1])
	})
	t.Run("start_40x14", func(t *testing.T) {
		r := startView(t, 40, 14, withFake(providerFake()))
		requireContentGolden(t, r.h)
		require.Equal(t, "  alibaba-token-plan/deepseek-v4.1-flash", r.content()[1])
	})
	t.Run("two_formats_80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(twoFormatFake()))
		requireContentGolden(t, r.h)
		require.Equal(t, "  model  alibaba-token-plan/deepseek-v4.1-flash (anthropic-messages)", r.content()[1])
	})
	t.Run("two_formats_40x14", func(t *testing.T) {
		r := startView(t, 40, 14, withFake(twoFormatFake()))
		requireContentGolden(t, r.h)
		rows := r.content()
		require.Equal(t, "  alibaba-token-plan/deepseek-v4.1-flash", rows[1])
		require.Equal(t, "  (anthropic-messages)", rows[2])
	})
}

func TestModelChangeLine(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("gpt-5.5")
	r.key("enter")
	r.waitCall("set_model:" + openaiGPT55)
	r.h.waitText("model  openai/gpt-5.5 · was alibaba-token-plan/deepseek-v4.1-flash")
	r.waitGone("Select model")
	requireContentGolden(t, r.h)
	// The header keeps the bare model id.
	header := r.headerOf()
	require.Contains(t, header, "gpt-5.5")
	require.NotContains(t, header, "openai/")
}

func TestChangeLineDropsWasWhenNarrow(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 40, 14, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("gpt-5.5")
	r.key("enter")
	r.waitCall("set_model:" + openaiGPT55)
	r.h.waitText("  openai/gpt-5.5")
	r.waitGone("Select model")
	r.requireNotOnScreen("was ")
}

func TestObserverAttachStartLine(t *testing.T) {
	fc := providerFake()
	fc.live = []protocol.ACPLiveSession{
		{SessionID: testSessionID, Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleDriver, HasDriver: true},
		{SessionID: "sess_8d07aa11bb22cc33", Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleObserver, HasDriver: true},
	}
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.typeText("/sessions")
	r.key("enter")
	r.h.waitText("Live sessions")
	r.key("down", "enter")
	r.waitCall("attach:sess_8d07aa11bb22cc33")
	r.h.waitText("◉ observer 8d07")
	fc.push(SnapshotMsg{SessionID: "sess_8d07aa11bb22cc33", Entries: []SnapshotEntry{{Kind: "message", Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "show me the probe code"}}, Timestamp: sketchTime(14, 3).UnixMilli()}}}})
	r.h.waitText("show me the probe code")
	rows := r.content()
	divider, line, prompt := -1, -1, -1
	for i, row := range rows {
		switch {
		case row == "  ── session 8d07 ──":
			divider = i
		case i > divider && divider >= 0 && row == "  model  alibaba-token-plan/deepseek-v4.1-flash":
			line = i
		case strings.HasPrefix(row, "  ❯") && i > divider && divider >= 0 && strings.Contains(row, "show me the probe code"):
			prompt = i
		}
	}
	require.Positive(t, divider, "the divider is on screen")
	require.Greater(t, line, divider, "the start line follows the divider")
	require.Greater(t, prompt, line, "the followed history comes below the start line")
}

func TestRestoreDrawsOneModelLine(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	// The follow of the first session had its own cut.
	fc.push(SnapshotMsg{SessionID: testSessionID, Resumed: true, Cursor: protocol.ACPCursor{Epoch: "e1", Seq: 10}})
	r.h.settle()
	// While the client was away the model changed; the state read after the
	// restore shows the new model.
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: openaiGPT55})
	fc.push(DisconnectedMsg{Err: errNoLeader}, ConnectedMsg{Info: fc.Info()},
		SessionRestoredMsg{Session: SessionInfo{ID: testSessionID, Role: protocol.ACPRoleDriver}, Resumed: true},
		SnapshotMsg{SessionID: testSessionID, Resumed: true, Cursor: protocol.ACPCursor{Epoch: "e1", Seq: 10}},
		// A model event at or below the cut is part of the state already.
		SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 9, Event: &protocol.ModelUpdate{Provider: "openai", ID: "gpt-5.5", API: "responses"}},
	)
	r.h.waitText("↻ Reconnected. Session 3f2a restored.")
	r.h.waitText("model  openai/gpt-5.5")
	r.h.settle()
	require.Equal(t, 1, r.h.count("model  openai/gpt-5.5"), "one start line follows the restore note")
	require.Zero(t, r.h.count("· was"), "no change line for a change that the state already holds")
	rows := r.rows()
	note := -1
	for i, row := range rows {
		if row == "  ↻ Reconnected. Session 3f2a restored." {
			note = i
		}
	}
	require.Positive(t, note)
	require.Equal(t, "  model  openai/gpt-5.5", rows[note+1])
}

func TestNoModelHasNoModelLine(t *testing.T) {
	r := startView(t, 80, 24, withFake(noModelFake()))
	for _, row := range r.content() {
		require.False(t, strings.HasPrefix(row, "  model  "), "no model line: %q", row)
	}
	r.requireOnScreen("no model", "Add a provider with /providers")
}

func TestLiveModelClearKeepsRemovalNoticeWithoutLine(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: openaiGPT55, Models: singleFormatModels()[1:], UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: openaiGPT55})
	r := startView(t, 80, 24, withFake(fc))
	before := r.h.count("  model  ")
	require.Equal(t, 1, before)
	r.event(&protocol.ModelUpdate{})
	r.h.waitText("┊ No model. Choose one with /model")
	require.Equal(t, before, r.h.count("  model  "))
	require.Zero(t, r.h.count("· was"))
	require.True(t, strings.HasSuffix(r.headerOf(), "no model"))
	r.h.resize(40, 14)
	r.h.settle()
	r.requireOnScreen("┊ No model. Choose one with /model")
	require.True(t, strings.HasSuffix(r.headerOf(), "no model"))
	r.h.resize(80, 24)
	r.typeText("hello")
	r.h.waitText("┊ No model. Choose one with /model")
	require.LessOrEqual(t, r.h.count("  model  "), before)
	require.Zero(t, r.h.count("· was"))
}

func TestStaleModelEventAddsNoRow(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.fc.push(SnapshotMsg{SessionID: testSessionID, Resumed: true, Cursor: protocol.ACPCursor{Epoch: "e2", Seq: 40}})
	r.h.settle()
	before := r.screen()
	r.fc.push(
		SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 50, Event: &protocol.ModelUpdate{Provider: "openai", ID: "gpt-5.5", API: "responses"}},
		SessionEventMsg{SessionID: testSessionID, Epoch: "e2", Seq: 40, Event: &protocol.ModelUpdate{Provider: "openai", ID: "gpt-5.5", API: "responses"}},
	)
	r.h.settle()
	require.Equal(t, before, r.screen(), "an event of an old epoch or at the cut adds no row")
}
