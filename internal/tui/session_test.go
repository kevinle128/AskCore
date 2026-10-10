package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// editorBox returns the rows of the editor box at a width, for a text row.
func editorBox(width int, text string) []string {
	inner := width - 4
	return []string{
		"╭" + strings.Repeat("─", width-2) + "╮",
		"│ " + padTo(text, inner) + " │",
		"╰" + strings.Repeat("─", width-2) + "╯",
	}
}

const placeholderRow = "❯ Ask anything  ·  / commands  ·  ctrl+l model"

// footerRow builds a footer row of a width from its two sides. The right side
// keeps its trailing space, as in the sketches.
func footerRow(width int, left, right string) string {
	return left + strings.Repeat(" ", width-displayWidth(left)-displayWidth(right)) + right
}

const (
	leftLeader  = "  ● leader 7c1e   ⌂ ~/src/askcore"
	rightIdle   = "deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 "
	rightNoCtx  = "deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.04 "
	rightCostly = "deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 "
)

// boxRow builds one row of a selector box. The left part and the right part are
// joined with spaces up to the inner width.
func boxRow(width int, left, right string) string {
	inner := width - 4
	return "│ " + padTo(left, inner-displayWidth(right)) + right + " │"
}

func modelRow(width int, selected bool, label, window, tag string) string {
	mark := "  "
	if selected {
		mark = "❯ "
	}
	return boxRow(width, mark+"● "+label, padLeft(window, 4)+"  "+padTo(tag, 9)+" ")
}

func sessionRow(width int, selected bool, id, cwd, state string) string {
	mark := "  "
	if selected {
		mark = "❯ "
	}
	return boxRow(width, mark+id+"  "+cwd, padLeft(state, 18))
}

func boxTop(width int, title string) string {
	head := "╭─ " + title + " "
	return head + strings.Repeat("─", width-displayWidth(head)-1) + "╮"
}

func boxBottom(width int) string { return "╰" + strings.Repeat("─", width-2) + "╯" }

func TestSendClearsDraftOnlyAfterAcceptance(t *testing.T) {
	t.Run("edit while sending keeps the new text", func(t *testing.T) {
		r := startView(t, 80, 24)
		r.typeText("show me the probe code")
		r.key("enter")
		r.h.waitText("┊ ◌ Sending…")
		r.waitCall("prompt:show me the probe code")
		require.Equal(t, 1, r.h.count("❯ show me the probe code"), "the draft stays in the editor while the prompt is not accepted")

		r.typeText(" now")
		r.fc.push(PromptAcceptedMsg{ID: 1})
		r.waitGone("Sending…")
		require.Equal(t, 1, r.h.count("❯ show me the probe code now"), "an edit made while sending is kept")
		require.Zero(t, r.h.count("Ask anything"))
	})

	t.Run("an unchanged draft is cleared and no prompt block is added", func(t *testing.T) {
		r := startView(t, 80, 24)
		r.typeText("show me the probe code")
		r.key("enter")
		r.waitCall("prompt:show me the probe code")
		r.fc.push(PromptAcceptedMsg{ID: 1})
		r.waitGone("Sending…")
		r.h.waitText("Ask anything")
		// The renderer draws on a timer, so the draft can show for one more frame.
		r.waitGone("show me the probe code")

		r.event(userAt("show me the probe code", sketchTime(14, 3))...)
		require.Equal(t, 1, r.h.count("❯ show me the probe code"))
	})

	t.Run("a second enter while sending sends nothing", func(t *testing.T) {
		r := startView(t, 80, 24)
		r.typeText("one")
		r.key("enter")
		r.h.waitText("Sending…")
		r.key("enter")
		time.Sleep(50 * time.Millisecond)
		require.Equal(t, 1, r.fc.count("prompt:"))
	})
}

func TestRefusedPromptKeepsDraft(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("show me the probe code")
	r.key("enter")
	r.waitCall("prompt:show me the probe code")
	r.fc.push(PromptSendFailedMsg{ID: 1, Err: &RequestError{Code: -32000, Kind: "not_driver", Message: "not the driver"}})
	r.h.waitText("✗ Not sent:")
	requireRows(t, r.h.grid(), []string{
		"┊ ✗ Not sent: not_driver · draft kept · /take to drive",
		"╭" + strings.Repeat("─", 78) + "╮",
		"│ ❯ show me the probe code" + strings.Repeat(" ", 53) + "│",
	})
	r.typeText("!")
	r.waitGone("Not sent")
}

func TestSendFailureAfterDisconnectIsOutcomeUnknown(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("hello")
	r.key("enter")
	r.waitCall("prompt:hello")
	r.fc.push(PromptSendFailedMsg{ID: 1, Err: ErrOutcomeUnknown, OutcomeUnknown: true})
	r.h.waitText("✗ Not sent:")
	require.GreaterOrEqual(t, r.h.count("nothing is resent"), 1)
	require.Equal(t, 1, r.fc.count("prompt:"), "the prompt is not sent again")
}

func TestEnterWhileRunningSteers(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.h.waitText("Working… 0s · esc to interrupt")
	r.typeText("use the fake clock from internal/testsupport")
	r.key("enter")
	r.waitCall("steer:use the fake clock from internal/testsupport")
	r.event(&protocol.QueueUpdate{Steering: []string{"use the fake clock from internal/testsupport"}})
	r.h.waitText("┆ ⧗ steer   use the fake clock from internal/testsupport")
	r.h.waitText("┆ alt+↑ edit queue · dashed rail = not sent yet")
	r.h.waitText("Ask anything")
}

func TestAltEnterWhileRunningFollowsUp(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.typeText("then run the leader tests")
	r.key("alt+enter")
	r.waitCall("followup:then run the leader tests")
	r.event(&protocol.QueueUpdate{FollowUp: []string{"then run the leader tests"}})
	r.h.waitText("┆ ⧗ next    then run the leader tests")
}

func TestAltEnterWhileIdleSendsPrompt(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("hi")
	r.key("alt+enter")
	r.waitCall("prompt:hi")
}

func TestQueueRowsBeyondThreeShowMore(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.event(&protocol.QueueUpdate{Steering: []string{"a", "b"}, FollowUp: []string{"c", "d"}})
	r.h.waitText("┆ ⧗ steer   a")
	r.h.waitText("┆ +")
}

func TestAltUpDequeuesIntoDraft(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.typeText("use the fake clock")
	r.key("enter")
	r.waitCall("steer:use the fake clock")
	// The draft clears when the answer arrives. Text typed before that is an
	// edit during sending, and the editor keeps it on purpose.
	r.waitGone("use the fake clock")
	r.typeText("then run the leader tests")
	r.key("alt+enter")
	r.waitCall("followup:then run the leader tests")
	r.event(&protocol.QueueUpdate{Steering: []string{"use the fake clock"}, FollowUp: []string{"then run the leader tests"}})
	r.h.waitText("⧗ next")

	r.typeText("tail")
	r.key("alt+up")
	r.waitCall("remove:in1")
	r.waitCall("remove:in2")
	r.event(&protocol.QueueUpdate{})
	r.waitGone("⧗")
	r.h.waitText("❯ use the fake clock")
	require.Equal(t, 1, r.h.count("  then run the leader tests"))

	m := r.stopped()
	require.Equal(t, "use the fake clock\nthen run the leader tests\ntail", m.sv.editor.Text())
}

func TestAltUpKeepsItemsThatCouldNotBeRemoved(t *testing.T) {
	fc := newFakeClient()
	fc.removeFails["in1"] = true
	r := startView(t, 80, 24, withFake(fc))
	r.event(&protocol.AgentStart{})
	r.typeText("taken already")
	r.key("enter")
	r.waitCall("steer:taken already")
	r.event(&protocol.QueueUpdate{Steering: []string{"taken already"}})
	r.key("alt+up")
	r.waitCall("remove:in1")
	r.h.waitText("already taken by the agent")
	m := r.stopped()
	require.Equal(t, "", m.sv.editor.Text(), "an item that left the queue is not put back")
}

func TestEscCancelsRunAndMarkerWaitsForSettle(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.key("esc")
	r.waitCall("cancel")
	r.h.waitText("Cancelling…")
	require.Zero(t, r.h.count("⊘"), "no marker at the key press")

	partial := "The probe lives in internal/leader/client.go."
	r.event(flat(
		userAt("show me the probe code", sketchTime(14, 3)),
		assistantStart("deepseek-v4.1-flash", 0),
		update(protocol.TextStartEvent{ContentIndex: 0}),
		update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: partial}),
		assistantEndUsage("deepseek-v4.1-flash", protocol.StopAborted, 500, protocol.Usage{}, protocol.Text{Text: partial}),
		&protocol.CycleEnd{Reason: "aborted", Cause: "user"},
	)...)
	require.Zero(t, r.h.count("⊘"), "the marker waits for the settle event")
	r.event(&protocol.AgentEnd{}, &protocol.AgentSettled{})
	r.h.waitText("⊘ Cancelled by you. Partial reply kept above.")
	r.waitGone("Cancelling…")
}

func TestEscWhileIdleDoesNothing(t *testing.T) {
	r := startView(t, 80, 24)
	r.key("esc")
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, r.fc.count("cancel"))
}

// ---- model selector ----

func TestSelectorChangesModel(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("my draft")
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.h.waitText("enter select  ·  esc close  ·  your draft is kept")
	requireRows(t, r.h.grid(), []string{
		boxTop(80, "Select model"),
		boxRow(80, " ⌕ type to filter", ""),
		modelRow(80, true, "alibaba/deepseek-v4.1-flash", "128k", "current"),
		modelRow(80, false, "alibaba/qwen4-coder-plus", "256k", ""),
		modelRow(80, false, "anthropic/claude-opus-5-5", "1M", "reasoning"),
		modelRow(80, false, "anthropic/claude-sonnet-5-5", "1M", "reasoning"),
		modelRow(80, false, "openai/gpt-5.5-codex", "400k", "reasoning"),
		modelRow(80, false, "xai/grok-5-code", "256k", "reasoning"),
		boxBottom(80),
		"   enter select  ·  esc close  ·  your draft is kept",
		footerRow(80, leftLeader, rightNoCtx),
	})
	requireContentGolden(t, r.h)

	r.typeText("opus")
	r.h.waitFor("one row", func(h *screenHarness) bool { return h.count("claude-sonnet") == 0 })
	r.key("enter")
	r.waitCall("set_model:" + opusModel)
	r.waitGone("Select model")
	r.h.waitText("claude-opus-5-5 ◐ medium")
	require.Equal(t, 1, r.h.count("❯ my draft"), "the draft is kept")
}

func TestSelectorEscClosesAndKeepsModel(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("draft")
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.key("esc")
	r.waitGone("Select model")
	require.Equal(t, 1, r.h.count("❯ draft"))
	require.Zero(t, r.fc.count("set_model"))
}

func TestSelectorReadinessFailureKeepsModel(t *testing.T) {
	fc := newFakeClient()
	fc.setModelErr[opusModel] = &RequestError{Code: -32000, Kind: string(protocol.ACPErrNoAPIKey), Message: "not signed in for this model"}
	r := startView(t, 80, 24, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("anth")
	r.h.waitFor("filtered", func(h *screenHarness) bool { return h.count("qwen4") == 0 })
	r.key("enter")
	r.waitCall("set_model:" + opusModel)
	r.h.waitText("✗ anthropic is not ready: no credential. Model not changed.")
	requireRows(t, r.h.grid(), []string{
		boxTop(80, "Select model"),
		boxRow(80, " ⌕ anth", ""),
		modelRow(80, true, "anthropic/claude-opus-5-5", "1M", "reasoning"),
		modelRow(80, false, "anthropic/claude-sonnet-5-5", "1M", "reasoning"),
		boxRow(80, "", ""),
		boxRow(80, " ✗ anthropic is not ready: no credential. Model not changed.", ""),
		boxRow(80, "   Run in a shell: ask auth login --provider anthropic", ""),
		boxBottom(80),
		"   enter select  ·  esc close  ·  your draft is kept",
		footerRow(80, leftLeader, rightNoCtx),
	})
	requireContentGolden(t, r.h)
	r.key("esc")
	r.waitGone("Select model")
	require.GreaterOrEqual(t, r.h.count("deepseek-v4.1-flash"), 1, "the previous model stays in the footer")
}

// A model that the sign-in does not serve is not a sign-in problem: the text
// names the provider, has no login hint, and the model stays as it was.
func TestSelectorModelAccessFailureKeepsModel(t *testing.T) {
	fc := newFakeClient()
	fc.setModelErr[opusModel] = &RequestError{Code: protocol.ACPCodeModelAccess, Kind: string(protocol.ACPErrModelAccess), Message: "not available"}
	r := startView(t, 80, 24, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("anth")
	r.h.waitFor("filtered", func(h *screenHarness) bool { return h.count("qwen4") == 0 })
	r.key("enter")
	r.waitCall("set_model:" + opusModel)
	r.h.waitText("✗ anthropic: this model is not available with your ChatGPT sign-in. Model")
	r.h.waitText("not changed.")
	require.Zero(t, r.h.count("Run in a shell"))
	requireContentGolden(t, r.h)
	r.key("esc")
	r.waitGone("Select model")
	require.GreaterOrEqual(t, r.h.count("deepseek-v4.1-flash"), 1, "the previous model stays in the footer")
}

func TestStaleSetModelIgnored(t *testing.T) {
	fc := newFakeClient()
	gate := make(chan struct{})
	fc.setModelGate[opusModel] = gate
	r := startView(t, 80, 24, withFake(fc))

	// The first request is still in flight when the user picks another model.
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("opus")
	r.key("enter")
	r.waitCall("set_model:" + opusModel)
	r.key("esc")
	r.waitGone("Select model")

	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("qwen")
	r.key("enter")
	r.waitCall("set_model:" + qwenModel)
	r.h.waitText("qwen4-coder-plus")
	r.waitGone("Select model")

	close(gate) // the older answer arrives last
	time.Sleep(100 * time.Millisecond)
	r.h.settle()
	footer := ""
	for _, row := range r.h.settle() {
		if strings.TrimSpace(row) != "" {
			footer = row
		}
	}
	require.Contains(t, footer, "qwen4-coder-plus")
	require.NotContains(t, footer, "claude-opus-5-5", "an older answer changes nothing")
}

func TestModelSelectorOnlyWhileIdle(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.key("ctrl+l")
	r.h.waitText("Model can change when the run ends.")
	require.Zero(t, r.h.count("Select model"))
	r.typeText("/model")
	r.key("enter")
	r.h.waitText("Model can change when the run ends.")
	require.Zero(t, r.h.count("Select model"))
}

func TestStaleResultsKeepCurrentSelector(t *testing.T) {
	for _, kind := range []string{"model", "session"} {
		t.Run(kind, func(t *testing.T) {
			r := startView(t, 80, 24)
			r.fc.push(DisconnectedMsg{}, SessionRestoredMsg{Session: SessionInfo{ID: r.fc.sessionID, Role: protocol.ACPRoleDriver}})
			r.h.waitText("Reconnected.")
			if kind == "model" {
				r.key("ctrl+l")
				r.h.waitText("Select model")
				r.h.send(setModelMsg{gen: 1, req: 1})
			} else {
				r.typeText("/sessions")
				r.key("enter")
				r.h.waitText("Live sessions")
				r.h.send(attachMsg{gen: 1})
			}
			r.h.settle()
			title := "Select model"
			if kind == "session" {
				title = "Live sessions"
			}
			require.Greater(t, r.h.count(title), 0, "an old result must not close the new selector")
		})
	}
}

func TestRefusalAfterUnrelatedAcceptanceRestoresDraft(t *testing.T) {
	for _, edited := range []bool{false, true} {
		t.Run(fmt.Sprintf("edited=%v", edited), func(t *testing.T) {
			r := startView(t, 80, 24)
			r.typeText("keep this draft")
			r.key("enter")
			r.waitCall("prompt:keep this draft")
			r.event(&protocol.AgentStart{})
			r.fc.push(PromptAcceptedMsg{ID: 1})
			r.h.waitText("Ask anything")
			if edited {
				r.typeText("new edits")
			}
			r.fc.push(SnapshotMsg{SessionID: r.fc.sessionID, Resync: true})
			r.fc.push(PromptSendFailedMsg{ID: 1, Err: &RequestError{Kind: string(protocol.ACPErrNotDriver)}})
			r.h.waitText("Not sent: not_driver")
			m := r.stopped()
			want := "keep this draft"
			if edited {
				want += "\nnew edits"
			}
			require.Equal(t, want, m.sv.editor.Text())
		})
	}
}

func TestEarlyPromptDoneDoesNotSettleNextRun(t *testing.T) {
	m := New(Options{Client: newFakeClient()})
	s := m.sv
	s.sess = SessionInfo{ID: "session", Role: protocol.ACPRoleDriver}
	s.followed = "session"
	s.editor.Handle(tea.PasteMsg{Content: "first"})
	sub := Submit{Text: "first", display: "first", Revision: s.editor.Revision()}
	called := s.submit(sub)().(promptCalledMsg)
	event := func(ev protocol.Event) { s.onSessionEvent(SessionEventMsg{SessionID: "session", Event: ev}) }
	event(&protocol.AgentStart{})
	s.onPromptMessage(PromptAcceptedMsg{ID: called.id})
	event(&protocol.AgentSettled{})
	event(&protocol.AgentStart{})
	s.onPromptMessage(PromptDoneMsg{ID: called.id})
	s.onPromptCalled(called)
	require.True(t, s.run.running, "the previous prompt must not settle the queued run")
}

func TestPendingPromptSurvivesConnectionAndSessionUpdates(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("fresh=%v", fresh), func(t *testing.T) {
			fc := newFakeClient()
			m := New(Options{Client: fc})
			s := m.sv
			s.sess = SessionInfo{ID: "old", Role: protocol.ACPRoleDriver}
			s.followed, s.gen, s.needNew = "old", 1, fresh
			s.editor.Handle(tea.PasteMsg{Content: "keep this prompt"})
			sub := Submit{Text: s.editor.Text(), display: s.editor.Text(), Revision: s.editor.Revision()}
			cmd := s.submit(sub)
			called := cmd().(promptCalledMsg)
			if fresh {
				s.onSnapshot(SnapshotMsg{SessionID: called.session.ID})
			} else {
				s.onClient(DisconnectedMsg{})
			}
			s.onPromptCalled(called)
			if fresh {
				s.onPromptMessage(PromptAcceptedMsg{ID: called.id})
				require.Empty(t, s.editor.Text(), "the replacement prompt clears after acceptance")
				require.Equal(t, called.session.ID, s.promptSession)
			} else {
				s.onPromptMessage(PromptSendFailedMsg{ID: called.id, Err: ErrOutcomeUnknown, OutcomeUnknown: true})
				s.onRestored(SessionRestoredMsg{Session: SessionInfo{ID: "old", Role: protocol.ACPRoleDriver}})
				require.Nil(t, s.send, "a stale command must not leave Sending blocked")
				require.Equal(t, "keep this prompt", s.editor.Text())
				require.NotNil(t, s.submit(sub), "the user can send again; nothing is resent automatically")
			}
		})
	}
}

func TestModelCommandOpensSelector(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("/model")
	r.key("enter")
	r.h.waitText("Select model")
}

func TestShiftTabCyclesThinkingForReasoningModels(t *testing.T) {
	fc := newFakeClient()
	fc.state.ModelID, fc.state.ThinkingLevel = opusModel, protocol.ThinkingMedium
	fc.models.Current = opusModel
	r := startView(t, 80, 24, withFake(fc))
	r.h.waitText("claude-opus-5-5 ◐ medium")
	r.key("shift+tab")
	r.waitCall("set_thinking:high")
	r.h.waitText("claude-opus-5-5 ◐ high")

	// A non-reasoning model has no thinking level to cycle.
	fc2 := newFakeClient()
	r2 := startView(t, 80, 24, withFake(fc2))
	r2.key("shift+tab")
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, fc2.count("set_thinking"))
}

func TestThinkingCycleDoesNotStickOnClampedLevel(t *testing.T) {
	fc := newFakeClient()
	fc.state.ModelID, fc.state.ThinkingLevel = opusModel, protocol.ThinkingHigh
	fc.models.Current = opusModel
	fc.thinkingCap = protocol.ThinkingHigh
	r := startView(t, 80, 24, withFake(fc))
	r.h.waitText("◐ high")
	r.key("shift+tab")
	r.waitCall("set_thinking:xhigh")
	r.key("shift+tab")
	r.waitCall("set_thinking:max")
	r.key("shift+tab")
	r.waitCall("set_thinking:off")
	r.h.waitText("◐ off")
}

func TestCtrlTTogglesThinkingBlocks(t *testing.T) {
	r := startView(t, 80, 40)
	r.event(flat(
		&protocol.AgentStart{},
		userAt("show me the probe code", sketchTime(14, 3)),
		assistantStart("deepseek-v4.1-flash", 0),
		thinkingParts(0, thinkingText),
		textParts(1, probeReply),
		assistantEndUsage("deepseek-v4.1-flash", protocol.StopStop, 7900, sketchUsage(),
			protocol.Thinking{Thinking: thinkingText}, protocol.Text{Text: probeReply}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
	r.h.waitText("The stale check runs before the deadline starts")

	r.key("ctrl+t")
	r.h.waitText("┊ Thinking blocks: hidden · ctrl+t to show")
	r.h.waitText("∴ Thinking...")
	require.Zero(t, r.h.count("The stale check runs"))

	r.key("ctrl+t")
	r.h.waitText("Thinking blocks: visible · ctrl+t to hide")
	r.h.waitText("The stale check runs before the deadline starts")
	require.Equal(t, 1, r.h.count("The stale check runs"), "a rebuild draws each committed row once")
}

// ---- connection states ----

func TestReconnectStatesGoldens(t *testing.T) {
	footerLost := footerRow(80, "  ● leader lost   ⌂ ~/src/askcore", rightIdle)
	prep := func(t *testing.T) *viewRig {
		r := startView(t, 80, 24)
		r.firstTurn()
		r.typeText("also check the lock timeout")
		return r
	}

	t.Run("disconnected", func(t *testing.T) {
		r := prep(t)
		r.fc.push(DisconnectedMsg{Err: errors.New("eof")}, ReconnectingMsg{Attempt: 2, Wait: 2 * time.Second, Err: errNoLeader})
		r.h.waitText("Disconnected from the leader")
		r.h.settle()
		requireRows(t, r.h.grid(), append([]string{
			"┊ ✗ Disconnected from the leader · reconnecting (attempt 2, next in 2s)",
			"┊ draft kept · sending waits until the leader is back · nothing is resent",
		}, append(editorBox(80, "❯ also check the lock timeout"), footerLost)...))
		requireContentGolden(t, r.h)

		r.key("enter")
		r.h.waitText("┊ Reconnecting, please wait…")
		time.Sleep(50 * time.Millisecond)
		require.Zero(t, r.fc.count("prompt:"), "nothing is sent while the link is down")
		require.GreaterOrEqual(t, r.h.count("❯ also check the lock timeout"), 1, "the draft is kept")
	})

	t.Run("restored", func(t *testing.T) {
		r := prep(t)
		r.fc.push(DisconnectedMsg{Err: errors.New("eof")}, ConnectedMsg{Info: r.fc.Info()},
			SessionRestoredMsg{Session: SessionInfo{ID: testSessionID, Role: protocol.ACPRoleDriver}, Resumed: true},
			SnapshotMsg{SessionID: testSessionID, Resumed: true})
		r.h.waitText("↻ Reconnected. Session 3f2a restored.")
		r.waitGone("Disconnected")
		r.h.settle()
		requireRows(t, r.h.grid(), append(editorBox(80, "❯ also check the lock timeout"),
			footerRow(80, leftLeader, rightIdle)))
		requireContentGolden(t, r.h)

		// A block that was produced during the outage arrives once.
		r.event(flat(
			userAt("and the timeout?", sketchTime(14, 6)),
			assistantStart("deepseek-v4.1-flash", 0),
			textParts(0, "It is one second."),
			assistantEndUsage("deepseek-v4.1-flash", protocol.StopStop, 900, sketchUsage(), protocol.Text{Text: "It is one second."}),
			&protocol.AgentSettled{},
		)...)
		r.h.waitText("It is one second.")
		require.Equal(t, 1, r.h.count("It is one second."))
		require.Equal(t, 1, r.h.count("and the timeout?"))
	})

	t.Run("session gone", func(t *testing.T) {
		r := prep(t)
		restarted := r.fc.Info()
		restarted.InstanceID = "91b0aa00cc11dd22"
		r.fc.setInfo(restarted)
		r.fc.push(DisconnectedMsg{Err: errors.New("eof")}, ConnectedMsg{Info: restarted}, SessionGoneMsg{SessionID: testSessionID})
		r.h.waitText("✗ Session 3f2a is gone: the leader restarted.")
		r.h.waitText("The transcript above is kept. Your next prompt starts a new session.")
		r.h.waitText("leader 91b0")
		requireRows(t, r.h.grid(), append(editorBox(80, "❯ also check the lock timeout"),
			footerRow(80, "  ● leader 91b0   ⌂ ~/src/askcore", rightCostly)))
		requireContentGolden(t, r.h)

		// The next prompt creates a session first; the earlier prompt is not sent again.
		r.key("enter")
		r.waitCall("prompt:also check the lock timeout")
		calls := r.fc.callList()
		require.Equal(t, 1, r.fc.count("prompt:"))
		var order []string
		for _, c := range calls {
			if c == "new_session" || strings.HasPrefix(c, "prompt:") {
				order = append(order, c)
			}
		}
		require.Equal(t, []string{"new_session", "new_session", "prompt:also check the lock timeout"}, order,
			"the first new_session is the start of the program")
	})
}

func TestStaleEventsOfAnotherSessionAreIgnored(t *testing.T) {
	r := startView(t, 80, 24)
	r.fc.push(SessionEventMsg{SessionID: "sess_other", Event: userAt("from elsewhere", sketchTime(14, 1))[1]})
	r.event(userAt("mine", sketchTime(14, 2))...)
	r.h.waitText("❯ mine")
	require.Zero(t, r.h.count("from elsewhere"))
}

func TestResyncSnapshotReplacesTranscript(t *testing.T) {
	r := startView(t, 80, 40)
	r.firstTurn()
	history := []SnapshotEntry{
		{Kind: "message", Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "fresh question"}}, Timestamp: sketchTime(14, 9).UnixMilli()}},
	}
	r.fc.push(SnapshotMsg{SessionID: testSessionID, Entries: history, Resync: true})
	r.h.waitText("fresh question")
	r.waitGone("why does TestLeaderStale fail on CI?")
	require.Equal(t, 1, r.h.count("ask   v0.1.0"), "the header is drawn again once")
}

func TestPromptRunErrorDoesNotDuplicateTheErrorBlock(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("go")
	r.key("enter")
	r.waitCall("prompt:go")
	r.fc.push(PromptAcceptedMsg{ID: 1})
	r.event(flat(&protocol.AgentStart{}, userAt("go", sketchTime(14, 3)),
		assistantStart("deepseek-v4.1-flash", 0),
		assistantEndUsage("deepseek-v4.1-flash", protocol.StopError, 500, protocol.Usage{}),
		&protocol.CycleEnd{Reason: "error", Code: "AUTH"}, &protocol.AgentEnd{}, &protocol.AgentSettled{})...)
	r.fc.push(PromptRunErrorMsg{ID: 1, Err: &RequestError{Kind: "internal", Message: "failed"}})
	r.h.waitText("✗ Error (auth)")
	r.h.settle()
	require.Equal(t, 1, r.h.count("✗ Error (auth)"))
}

// ---- sessions, observer, take, new ---

func TestSessionsSelectorAttachesAsObserver(t *testing.T) {
	fc := newFakeClient()
	fc.live = []protocol.ACPLiveSession{
		{SessionID: testSessionID, Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleDriver, HasDriver: true, Subscribers: 1},
		{SessionID: "sess_8d07aa11bb22cc33", Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleObserver, HasDriver: true},
		{SessionID: "sess_c1e9aa11bb22cc33", Cwd: "/home/u/src/pi-notes"},
	}
	fc.attachEvents = []Message{SnapshotMsg{SessionID: "sess_8d07aa11bb22cc33", Entries: []SnapshotEntry{
		{Kind: "message", Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "show me the probe code"}}, Timestamp: sketchTime(14, 3).UnixMilli()}},
	}}}
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.typeText("/sessions")
	r.key("enter")
	r.h.waitText("Live sessions")
	requireRows(t, r.h.grid(), []string{
		boxTop(80, "Live sessions"),
		boxRow(80, " ⌕ type to filter", ""),
		sessionRow(80, true, "3f2a", "~/src/askcore", "this client drives"),
		sessionRow(80, false, "8d07", "~/src/askcore", "driver busy"),
		sessionRow(80, false, "c1e9", "~/src/pi-notes", "no driver"),
		boxBottom(80),
		"   enter watch  ·  esc close",
	})
	requireContentGolden(t, r.h)

	r.key("down", "enter")
	r.waitCall("attach:sess_8d07aa11bb22cc33")
	r.h.waitText("── session 8d07 ──")
	r.h.waitText("◉ observer 8d07")
	r.h.waitText("◉ Watching. Type /take to take control when the run ends.")
	require.True(t, fc.has("detach"))
	require.Zero(t, r.h.count("Ask anything"))

	// The followed run shows its status for the observer.
	fc.sessionID = "sess_8d07aa11bb22cc33"
	r.clock.advance(6 * time.Second)
	r.event(&protocol.AgentStart{})
	r.clock.advance(6 * time.Second)
	r.event(&protocol.QueueUpdate{})
	r.h.waitText("Working… 6s · another client drives this session")

	// /take makes this client the driver again, once the run has ended.
	r.event(&protocol.AgentEnd{}, &protocol.AgentSettled{})
	r.waitGone("Working…")
	r.typeText("/take")
	r.key("enter")
	r.waitCall("take")
	r.h.waitText("● leader 7c1e")
	r.waitGone("◉ observer")
}

func TestNewCommandStartsSessionWhenIdle(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("/new")
	r.key("enter")
	r.h.waitFor("second session", func(*screenHarness) bool { return r.fc.count("new_session") == 2 })
	require.True(t, r.fc.has("detach"))

	r.event(&protocol.AgentStart{})
	r.typeText("/new")
	r.key("enter")
	r.h.waitText("Session can change when the run ends.")
	require.Equal(t, 2, r.fc.count("new_session"))
}

func TestInProcessSessionNeedsNoLeaderCalls(t *testing.T) {
	fc := newFakeClient()
	fc.info = ConnInfo{Mode: ModeInProcess}
	fc.listErr = ErrNeedsLeader
	r := startView(t, 80, 24, withFake(fc))
	r.h.waitText("○ in-process")
	r.h.waitText("◢◤ ask   v0.1.0 · in-process agent")

	r.typeText("/sessions")
	r.key("enter")
	r.h.waitText("Needs the leader; this session runs in process.")
	r.typeText("/take")
	r.key("enter")
	r.h.waitText("Needs the leader; this session runs in process.")
}

func TestFailedAttachLeavesNoSessionUntilNextPrompt(t *testing.T) {
	// A client without a session reports ErrNoSession on the next prompt; the
	// view starts a new session instead of failing.
	fc := newFakeClient()
	r := startView(t, 80, 24, withFake(fc))
	r.fc.push(SessionGoneMsg{SessionID: testSessionID})
	r.h.waitText("is gone")
	r.typeText("again")
	r.key("enter")
	r.waitCall("prompt:again")
	require.Equal(t, 2, fc.count("new_session"))
}

// ---- retry, starting ---

func TestRetryStatusCountsDown(t *testing.T) {
	r := startView(t, 80, 24, withOptions(func(o *Options) { o.TickInterval = 10 * time.Millisecond }))
	r.event(&protocol.AgentStart{})
	r.event(&protocol.AutoRetryStart{Attempt: 1, MaxAttempts: 5, DelayMs: 4000, ErrorMessage: "rate limit (HTTP 429)"})
	r.h.waitText("┊ ◌ Retrying (1/5) in 4s · rate limit · esc to cancel")
	r.h.waitText("↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 5 scheduled.")
	r.clock.advance(1500 * time.Millisecond)
	r.h.waitText("Retrying (1/5) in 3s")
	r.event(&protocol.AutoRetryEnd{Success: true, Attempt: 1})
	r.h.waitText("Working…")
}

func TestStartingStatusThenSession(t *testing.T) {
	fc := newFakeClient()
	release := make(chan struct{})
	r := startView(t, 80, 24, withOptions(func(o *Options) {
		o.Client = nil
		o.Open = func(context.Context) (Client, error) {
			<-release
			return fc, nil
		}
	}))
	r.h.waitText("┊ ◌ Starting leader…  first run can take a few seconds")
	r.h.waitText("◌ leader starting   ⌂ ~/src/askcore")
	r.typeText("early input")
	r.key("enter")
	r.h.waitText("Starting, please wait…")
	require.Zero(t, fc.count("prompt:"))
	close(release)
	r.h.waitText("◢◤ ask   v0.1.0 · leader 7c1e")
	r.waitGone("Starting leader…")
	r.h.waitText("● leader 7c1e")
	require.Equal(t, 1, r.h.count("❯ early input"), "keys typed during startup reach the editor")
}

func TestStartErrorEndsTheProgram(t *testing.T) {
	m := New(Options{Profile: colorprofile.NoTTY, TickInterval: -1, Open: func(context.Context) (Client, error) {
		return nil, errors.New("leader: boom; in-process agent: boom")
	}})
	// The program ends before a frame may show, so the screen harness, which
	// waits for the first frame, does not fit here.
	prog := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(&syncBuffer{}), tea.WithWindowSize(80, 24),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := prog.Run(); done <- err }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		prog.Kill()
		t.Fatal("the program did not end after a failed open")
	}
	require.ErrorContains(t, m.StartError(), "boom")
	require.Empty(t, m.ExitNote(), "a failed start is not a quit")
}

func TestClientErrorFatalEndsTheProgram(t *testing.T) {
	r := startView(t, 80, 24)
	r.fc.push(ClientErrorMsg{Err: errors.New("link panic"), Fatal: true})
	res := r.exited()
	require.NoError(t, res.err)
	require.ErrorContains(t, res.model.(*Model).ClientError(), "link panic")
}

// ---- footer ---

func footerModel(t *testing.T, profile colorprofile.Profile) *Model {
	t.Helper()
	fc := newFakeClient()
	m := New(Options{Client: fc, Profile: profile, Cwd: "/home/u/src/askcore", Home: "/home/u", Version: "0.1.0", TickInterval: -1})
	s := m.sv
	s.conn, s.info = connUp, fc.Info()
	s.models = fc.models.Models
	s.modelID = deepseekModel
	return m
}

func TestFooterUsageThresholds(t *testing.T) {
	cases := []struct {
		pct  int64
		bar  string
		role Role
	}{
		{18, "▰▱▱▱▱▱▱▱ 18%", RoleAccent},
		{69, "▰▰▰▰▰▰▱▱ 69%", RoleAccent},
		{70, "▰▰▰▰▰▰▱▱ 70%", RoleWarning},
		{89, "▰▰▰▰▰▰▰▱ 89%", RoleWarning},
		{90, "▰▰▰▰▰▰▰▱ 90%", RoleError},
		{100, "▰▰▰▰▰▰▰▰ 100%", RoleError},
	}
	for _, c := range cases {
		m := footerModel(t, colorprofile.TrueColor)
		m.sv.ctxTokens = 128_000 * c.pct / 100
		row := m.sv.footerText(80)
		require.Contains(t, row, m.theme.Render(c.role, c.bar), "%d%%", c.pct)
	}

	t.Run("short form under 60 columns", func(t *testing.T) {
		m := footerModel(t, colorprofile.NoTTY)
		m.sv.ctxTokens = 23_040
		require.Equal(t, "  ● 7c1e        deepseek-v4.1-flash 18% ", ansi.Strip(m.sv.footerText(40)))
		require.Equal(t, 40, displayWidth(m.sv.footerText(40)))
	})

	t.Run("wide characters at 40x14", func(t *testing.T) {
		r := startView(t, 40, 14)
		r.event(flat(
			&protocol.AgentStart{},
			userAt("giải thích lỗi 日本語のテスト", sketchTime(14, 5)),
			assistantStart("deepseek-v4.1-flash", 0),
			update(protocol.TextStartEvent{ContentIndex: 0}),
			update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "The 日本語 test fails because each wide character uses two columns."}),
			assistantEndUsage("deepseek-v4.1-flash", protocol.StopStop, 500, sketchUsage(), protocol.Text{Text: "The 日本語 test fails because each wide character uses two columns."}),
		)...)
		r.clock.advance(3 * time.Second)
		r.event(&protocol.QueueUpdate{})
		r.h.waitText("┊ ◌ Working… 3s · esc")
		requireRows(t, r.h.grid(), []string{
			"┊ ◌ Working… 3s · esc",
			"╭" + strings.Repeat("─", 38) + "╮",
			"│ ❯ Ask anything" + strings.Repeat(" ", 23) + "│",
			"╰" + strings.Repeat("─", 38) + "╯",
			"  ● 7c1e        deepseek-v4.1-flash 18%",
		})
		for _, row := range r.h.text() {
			require.LessOrEqual(t, displayWidth(row), 40, row)
		}
		require.GreaterOrEqual(t, r.h.count("日本語のテスト"), 1)
		requireContentGolden(t, r.h)
	})
}

func TestFooterShapes(t *testing.T) {
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	plain := func(w int) string { return strings.TrimRight(ansi.Strip(s.footerText(w)), " ") }

	s.sess = SessionInfo{ID: testSessionID, Role: protocol.ACPRoleObserver}
	require.Equal(t, strings.TrimRight(footerRow(80, "  ◉ observer 3f2a   ⌂ ~/src/askcore", rightCostly), " "), plain(80))
	s.sess.Role = protocol.ACPRoleDriver

	s.conn = connDown
	require.Contains(t, plain(80), "● leader lost")
	s.conn = connUp

	s.info = ConnInfo{Mode: ModeInProcess}
	require.Contains(t, plain(80), "○ in-process")
	s.info = ConnInfo{Mode: ModeLeader, InstanceID: testLeaderID}

	s.cwd = "/home/u/projects/deeply/nested/repository"
	narrow := plain(80)
	require.Contains(t, narrow, "⌂ ~/…/repository")
	require.LessOrEqual(t, displayWidth(narrow), 80)
}

func TestFooterReasoningModelShowsLevel(t *testing.T) {
	m := footerModel(t, colorprofile.NoTTY)
	s := m.sv
	s.modelID, s.thinking = opusModel, protocol.ThinkingMedium
	s.ctxTokens = 210_000
	s.costMicro = 110_000
	require.Equal(t, "  ● leader 7c1e   ⌂ ~/src/askcore claude-opus-5-5 ◐ medium  ▰▰▱▱▱▱▱▱ 21%  $0.11 ", ansi.Strip(m.sv.footerText(80)))
}

// ---- 120 column sketches ---

func TestScreens120Goldens(t *testing.T) {
	const w = 120
	footer := footerRow(w, leftLeader, rightIdle)
	placeholder := editorBox(w, placeholderRow)

	t.Run("idle", func(t *testing.T) {
		r := startView(t, w, 40)
		r.firstTurn()
		requireRows(t, r.h.grid(), append(placeholder, footer))
		requireContentGolden(t, r.h)
	})

	t.Run("streaming with a tool call", func(t *testing.T) {
		r := startView(t, w, 40)
		r.firstTurn()
		r.event(flat(
			&protocol.AgentStart{},
			userAt("show me the probe code", sketchTime(14, 3)),
			toolStart("c1", "echo", `{"text":"probe code request"}`, 0),
			assistantStart("deepseek-v4.1-flash", 10),
			update(protocol.TextStartEvent{ContentIndex: 0}),
			update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"}),
		)...)
		r.clock.advance(4 * time.Second)
		r.event(&protocol.QueueUpdate{})
		r.h.waitText("Working… 4s")
		running := "  ◇ echo  probe code request" + strings.Repeat(" ", w-2-displayWidth("◇ echo  probe code request")-displayWidth("running ⠋")-1) + "running ⠋"
		requireRows(t, r.h.grid(), append([]string{
			running,
			"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits",
			"",
			"┊ ◌ Working… 4s · esc to interrupt",
		}, append(placeholder, footer)...))
		requireContentGolden(t, r.h)
	})

	t.Run("running with queued input", func(t *testing.T) {
		r := startView(t, w, 40)
		r.firstTurn()
		r.event(flat(
			&protocol.AgentStart{},
			userAt("show me the probe code", sketchTime(14, 3)),
			toolStart("c1", "echo", `{"text":"probe code request"}`, 0),
			toolEnd("c1", "echo", false, "ok", 100),
			assistantStart("deepseek-v4.1-flash", 200),
			update(protocol.TextStartEvent{ContentIndex: 0}),
			update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"}),
		)...)
		r.clock.advance(9 * time.Second)
		r.event(&protocol.QueueUpdate{
			Steering: []string{"use the fake clock from internal/testsupport"},
			FollowUp: []string{"then run the leader tests"},
		})
		r.h.waitText("Working… 9s")
		requireRows(t, r.h.grid(), append([]string{
			"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits",
			"",
			"┊ ◌ Working… 9s · esc to interrupt",
			"┆ ⧗ steer   use the fake clock from internal/testsupport",
			"┆ ⧗ next    then run the leader tests",
			"┆ alt+↑ edit queue · dashed rail = not sent yet",
		}, append(placeholder, footer)...))
		requireContentGolden(t, r.h)
	})

	t.Run("model selector", func(t *testing.T) {
		r := startView(t, w, 40)
		r.firstTurn()
		r.key("ctrl+l")
		r.h.waitText("Select model")
		requireRows(t, r.h.grid(), []string{
			boxTop(w, "Select model"),
			boxRow(w, " ⌕ type to filter", ""),
			modelRow(w, true, "alibaba/deepseek-v4.1-flash", "128k", "current"),
			modelRow(w, false, "alibaba/qwen4-coder-plus", "256k", ""),
			modelRow(w, false, "anthropic/claude-opus-5-5", "1M", "reasoning"),
			modelRow(w, false, "anthropic/claude-sonnet-5-5", "1M", "reasoning"),
			modelRow(w, false, "openai/gpt-5.5-codex", "400k", "reasoning"),
			modelRow(w, false, "xai/grok-5-code", "256k", "reasoning"),
			boxBottom(w),
			"   enter select  ·  esc close  ·  your draft is kept",
			footer,
		})
		requireContentGolden(t, r.h)
	})
}

// ---- quit keys, notices, animation, selector window ---

// plainTrimmed removes the styling and the trailing spaces of rows.
func plainTrimmed(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.TrimRight(ansi.Strip(r), " ")
	}
	return out
}

// finalModel returns the model of a program that ended by itself.
func (r *viewRig) finalModel() *Model {
	r.t.Helper()
	res := r.exited()
	require.NoError(r.t, res.err)
	return res.model.(*Model)
}

func TestCtrlCClearsDraftThenConfirmsQuit(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("draft")
	r.key("ctrl+c")
	r.waitGone("❯ draft")
	r.h.waitText("Ask anything")
	// The draft is gone, so the next press asks for confirmation. It does not
	// quit.
	r.key("ctrl+c")
	r.h.waitText("Press ctrl+c again to quit")
	require.Zero(t, r.h.count("the run continues"), "no run is active")
	r.key("ctrl+c")
	note := strings.Join(plainTrimmed(r.finalModel().ExitNote()), "\n")
	require.Contains(t, note, "Detached from session 3f2a")
}

func TestCtrlCConfirmationNamesTheRunInTheLeader(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.key("ctrl+c")
	r.h.waitText("Press ctrl+c again to quit · the run continues in the leader")
}

func TestCtrlCConfirmationWindowEndsAfterTwoSeconds(t *testing.T) {
	r := startView(t, 80, 24, withOptions(func(o *Options) { o.TickInterval = 10 * time.Millisecond }))
	r.key("ctrl+c")
	r.h.waitText("Press ctrl+c again to quit")
	r.clock.advance(1900 * time.Millisecond)
	r.h.waitText("Press ctrl+c again to quit")
	r.clock.advance(200 * time.Millisecond)
	r.waitGone("Press ctrl+c again to quit")
	// A press after the window asks again and does not quit.
	r.key("ctrl+c")
	r.h.waitText("Press ctrl+c again to quit")
	select {
	case res := <-r.h.done:
		r.h.done <- res
		t.Fatal("the program ended on a press after the window")
	case <-time.After(200 * time.Millisecond):
	}
	r.key("ctrl+c")
	r.finalModel()
}

func TestCtrlCWithDraftClearsInsteadOfQuitting(t *testing.T) {
	r := startView(t, 80, 24)
	r.key("ctrl+c")
	r.h.waitText("Press ctrl+c again to quit")
	r.typeText("x")
	// With a draft the press clears it, so it cannot quit by accident.
	r.key("ctrl+c")
	r.waitGone("❯ x")
	select {
	case res := <-r.h.done:
		r.h.done <- res
		t.Fatal("the program ended on a press that cleared a draft")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestQuitCommandAndCtrlDDetachWithTheLeaderNote(t *testing.T) {
	for name, send := range map[string]func(*viewRig){
		"ctrl+d": func(r *viewRig) { r.key("ctrl+d") },
		"/quit":  func(r *viewRig) { r.typeText("/quit"); r.key("enter") },
	} {
		t.Run(name, func(t *testing.T) {
			r := startView(t, 80, 24)
			r.event(&protocol.AgentStart{})
			send(r)
			note := plainTrimmed(r.finalModel().ExitNote())
			require.Equal(t, []string{
				"  ⏏ Detached from session 3f2a. The run continues in leader 7c1e.",
				"    Run ask in this directory to see it again.",
			}, note)
			// The live region is gone: the note takes its place.
			require.Zero(t, r.h.count("Ask anything"))
			require.Zero(t, r.h.count("leader 7c1e   "))
		})
	}
}

func TestQuitWhenIdleDoesNotClaimARun(t *testing.T) {
	r := startView(t, 80, 24)
	r.key("ctrl+d")
	note := strings.Join(plainTrimmed(r.finalModel().ExitNote()), "\n")
	require.Contains(t, note, "Detached from session 3f2a.")
	require.NotContains(t, note, "The run continues")
}

func TestInProcessQuitWording(t *testing.T) {
	fc := newFakeClient()
	fc.info = ConnInfo{Mode: ModeInProcess}
	r := startView(t, 80, 24, withFake(fc))
	r.key("ctrl+c")
	r.h.waitText("Press ctrl+c again to quit. The run stops.")
	r.key("ctrl+c")
	require.Equal(t, []string{"  ⏏ Run stopped. Session ended."}, plainTrimmed(r.finalModel().ExitNote()))
}

func TestStopMsgEndsTheProgramWithoutANote(t *testing.T) {
	r := startView(t, 80, 24)
	r.h.send(StopMsg{})
	m := r.finalModel()
	require.Empty(t, m.ExitNote())
	require.Zero(t, r.h.count("Ask anything"), "the live region is cleared")
}

func TestExitNoteIsEmptyBeforeAnyQuit(t *testing.T) {
	r := startView(t, 80, 24)
	require.Empty(t, r.stopped().ExitNote())
}

func TestCtrlDWithDraftDeletesACharacter(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("ab")
	r.key("left", "ctrl+d")
	r.h.waitText("❯ a")
	m := r.stopped()
	require.Equal(t, "a", m.sv.editor.Text())
}

func TestNonFatalClientErrorShowsBlock(t *testing.T) {
	r := startView(t, 80, 24)
	r.fc.push(ClientErrorMsg{Err: errors.New("event 4 is not valid")})
	r.h.waitText("✗ Error (client): event 4 is not valid.")
	r.typeText("still alive")
	r.h.waitText("❯ still alive")
}

func TestRunningToolSpinnerAdvances(t *testing.T) {
	r := startView(t, 80, 24, withOptions(func(o *Options) { o.TickInterval = 10 * time.Millisecond }))
	r.event(flat(
		&protocol.AgentStart{},
		userAt("go", sketchTime(14, 3)),
		toolStart("c1", "echo", `{"text":"probe code request"}`, 0),
	)...)
	r.h.waitText("running ⠋")
	r.clock.advance(80 * time.Millisecond)
	r.h.waitText("running ⠙")
}

func TestNoticeExpires(t *testing.T) {
	r := startView(t, 80, 24, withOptions(func(o *Options) { o.TickInterval = 10 * time.Millisecond }))
	r.event(&protocol.AgentStart{})
	r.key("ctrl+l")
	r.h.waitText("Model can change when the run ends.")
	r.clock.advance(noticeFor + time.Second)
	r.waitGone("Model can change")
	r.h.waitText("Working…")
}

func TestSelectorWindowKeepsSelectionVisible(t *testing.T) {
	fc := newFakeClient()
	fc.models.Models = nil
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("prov/model-%02d@x", i)
		fc.models.Models = append(fc.models.Models, protocol.ACPModel{ModelID: id, Provider: "prov", ID: fmt.Sprintf("model-%02d", i), ContextWindow: 100_000})
	}
	fc.models.Current = fc.models.Models[0].ModelID
	fc.state.ModelID = fc.models.Current
	r := startView(t, 80, 24, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.h.waitText("… 7 more")
	for i := 0; i < 10; i++ {
		r.key("down")
	}
	r.h.waitText("❯ ● prov/model-10")
	require.Equal(t, 1, r.h.count("❯ ●"), "one row is selected")
	for _, row := range r.h.grid() {
		require.LessOrEqual(t, displayWidth(row), 80)
	}
	r.h.waitText("enter select")
}

func TestSelectorFilterWithNoMatchSelectsNothing(t *testing.T) {
	r := startView(t, 80, 24)
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("zzz")
	r.h.waitText("no match")
	r.key("enter")
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, r.fc.count("set_model"))
	r.key("backspace", "backspace", "backspace")
	r.h.waitText("alibaba/deepseek-v4.1-flash")
}
