package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func twoFormatFake() *fakeClient {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: tokenPlanFlash, Models: designModels(true), UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: tokenPlanFlash})
	return fc
}

// The footer row names the provider and the model; the pane has no model line.
func TestFooterNamesModelWithoutModelLine(t *testing.T) {
	t.Run("start_80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(providerFake()))
		requireContentGolden(t, r.h)
		rows := r.content()
		requireNoModelLine(t, rows)
		require.Equal(t, strings.TrimRight(footerRow(80, leftFooter, "alibaba-token-plan/deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0% "), " "), r.footerOf())
	})
	t.Run("start_120x40", func(t *testing.T) {
		r := startView(t, 120, 40, withFake(providerFake()))
		requireContentGolden(t, r.h)
		requireNoModelLine(t, r.content())
		require.True(t, strings.HasPrefix(r.footerOf(), leftFooter), r.footerOf())
		require.True(t, strings.HasSuffix(r.footerOf(), "alibaba-token-plan/deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%"), r.footerOf())
	})
	t.Run("start_40x14", func(t *testing.T) {
		r := startView(t, 40, 14, withFake(providerFake()))
		requireContentGolden(t, r.h)
		requireNoModelLine(t, r.content())
		require.Equal(t, "  alibaba-token-plan/deepseek-v4.1-… 0%", r.footerOf())
	})
	t.Run("two_formats_80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(twoFormatFake()))
		requireContentGolden(t, r.h)
		requireNoModelLine(t, r.content())
		require.Contains(t, r.footerOf(), "alibaba-token-plan/deepseek-v4.1-flash")
		r.requireNotOnScreen("anthropic-messages")
	})
}

// A model change shows in the footer row and in the status row, never as a
// line in the pane.
func TestModelChangeShowsInFooterOnly(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
		foot string
	}{
		{"80x24", 80, 24, " ⌂ ~/src/askcore                           openai/gpt-5.5 ◐ medium  ▱▱▱▱▱▱▱▱ 2%"},
		{"40x14", 40, 14, "                      openai/gpt-5.5 2%"},
	} {
		t.Run(size.name, func(t *testing.T) {
			fc := providerFake()
			fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
			r := startView(t, size.w, size.h, withFake(fc))
			r.firstTurn()
			r.key("ctrl+l")
			r.h.waitText("Select model")
			r.typeText("gpt-5.5")
			r.key("enter")
			r.waitCall("set_model:" + openaiGPT55)
			r.waitGone("Select model")
			r.h.waitFor("the new model in the footer row", func(*screenHarness) bool { return strings.Contains(r.footerOf(), "openai/gpt-5.5") })
			requireContentGolden(t, r.h)
			requireNoModelLine(t, r.content())
			require.Equal(t, size.foot, r.footerOf())
		})
	}
}

// An observer that attaches gets the divider and then the history: no model
// line, and no fallback notice of a session it did not start.
func TestObserverAttachShowsDividerThenHistory(t *testing.T) {
	fc := providerFake()
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: deepseekChat,
		ModelFallback: &protocol.ACPModelFallback{SavedDefault: "openai/gpt-5.4-mini@responses", Reason: protocol.ACPFallbackNotInAccount}})
	fc.live = []protocol.ACPLiveSession{
		{SessionID: testSessionID, Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleDriver, HasDriver: true},
		{SessionID: "sess_8d07aa11bb22cc33", Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleObserver, HasDriver: true},
	}
	r := startView(t, 80, 24, withFake(fc))
	r.h.waitText("(not in account list).")
	r.firstTurn()
	r.typeText("/sessions")
	r.key("enter")
	r.h.waitText("Live sessions")
	r.key("down", "enter")
	r.waitCall("attach:sess_8d07aa11bb22cc33")
	r.h.waitText("Watching. Type /take")
	fc.push(SnapshotMsg{SessionID: "sess_8d07aa11bb22cc33", Entries: []SnapshotEntry{{Kind: "message", Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "show me the probe code"}}, Timestamp: sketchTime(14, 3).UnixMilli()}}}})
	r.h.waitText("show me the probe code")
	r.h.settle()
	rows := r.content()
	requireNoModelLine(t, rows)
	divider, prompt := -1, -1
	for i, row := range rows {
		switch {
		case row == "  ── session 8d07 ──":
			divider = i
		case strings.HasPrefix(row, "  ❯") && strings.Contains(row, "show me the probe code"):
			prompt = i
		}
	}
	require.GreaterOrEqual(t, divider, 0, "the divider is on screen")
	require.Equal(t, divider+2, prompt, "the followed history comes directly below the divider")
	for _, row := range rows[divider:] {
		require.NotContains(t, row, "Default model", "the observer gets no fallback notice")
	}
}

// A restore note has no model line under it, and a change that the state
// already holds shows only in the footer row.
func TestRestoreNoteHasNoModelLine(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
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
	r.h.waitFor("the new model in the footer row", func(*screenHarness) bool { return strings.Contains(r.footerOf(), "openai/gpt-5.5") })
	rows := r.content()
	requireNoModelLine(t, rows)
	note := -1
	for i, row := range rows {
		if row == "  ↻ Reconnected. Session 3f2a restored." {
			note = i
		}
	}
	require.Positive(t, note)
	require.Equal(t, "", rows[note+1], "the note is one block with its empty row")
}

func TestNoModelHasNoModelLine(t *testing.T) {
	r := startView(t, 80, 24, withFake(noModelFake()))
	requireNoModelLine(t, r.content())
	r.requireOnScreen("no model", "Add a provider with /providers")
}

func TestLiveModelClearKeepsRemovalNoticeWithoutLine(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: openaiGPT55, Models: singleFormatModels()[1:], UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: openaiGPT55})
	r := startView(t, 80, 24, withFake(fc))
	require.Contains(t, r.footerOf(), "openai/gpt-5.5")
	r.event(&protocol.ModelUpdate{})
	r.h.waitText("┊ No model. Choose one with /model")
	requireNoModelLine(t, r.content())
	require.True(t, strings.HasSuffix(r.footerOf(), "no model"))
	r.h.resize(40, 14)
	r.h.settle()
	r.requireOnScreen("┊ No model. Choose one with /model")
	require.True(t, strings.HasSuffix(r.footerOf(), "no model"))
	r.h.resize(80, 24)
	r.typeText("hello")
	r.h.waitText("┊ No model. Choose one with /model")
	requireNoModelLine(t, r.content())
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
	require.Equal(t, before, r.screen(), "an event of an old epoch or at the cut changes neither the pane nor the footer row")
}

// A model event of the leader changes the footer row and adds no row.
func TestLiveModelEventChangesFooterOnly(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	before := r.content()
	r.event(&protocol.ModelUpdate{Provider: "openai", ID: "gpt-5.5", API: "responses"})
	r.h.waitFor("the new model in the footer row", func(*screenHarness) bool { return strings.Contains(r.footerOf(), "openai/gpt-5.5") })
	after := r.content()
	require.Equal(t, before[:len(before)-2], after[:len(after)-2], "the pane and the prompt pane do not change")
}

// An observer did not start the session, so it gets no fallback notice, also
// when the state of the session names one.
func TestObserverStartShowsNoFallbackNotice(t *testing.T) {
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	s.fallback = &protocol.ACPModelFallback{SavedDefault: "openai/gpt-5.4-mini@responses", Reason: protocol.ACPFallbackNotInAccount}

	s.sess = SessionInfo{ID: testSessionID, Role: protocol.ACPRoleDriver}
	_, driverNotice := s.fallbackBlock().(noticeBlock)
	require.True(t, driverNotice, "the driver sees the notice")

	s.sess = SessionInfo{ID: testSessionID, Role: protocol.ACPRoleObserver}
	require.Equal(t, Block(noRows{}), s.fallbackBlock(), "the observer sees none")
}
