package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Connection states.
//
// The client reconnects by itself and reports each step as a message. The view
// shows the state and never resends anything: a prompt whose answer was lost
// stays in the draft, and the resync after the reconnect shows whether it
// arrived.

// onRestored shows that the same leader and the same session are back. The
// client follows the session from the last cursor, so the events that were
// missed arrive after this message and appear once.
func (s *sessionView) onRestored(v SessionRestoredMsg) tea.Cmd {
	s.conn = connUp
	s.reconnect = reconnectView{}
	s.useSession(v.Session)
	s.addStartSlot(startRestore, "Reconnected. Session "+ShortID(v.Session.ID)+" restored.")
	return tea.Batch(s.refreshCmd(false), s.onBoxReconnected())
}

// onGone handles a session that the leader no longer has, for example after a
// restart. The transcript stays as history; the next prompt starts a session.
func (s *sessionView) onGone(v SessionGoneMsg) {
	s.promptID, s.promptSession = 0, ""
	s.accepted = nil
	s.gen++
	s.needNew = true
	// Blocks of the lost run would hold the frontier for ever.
	s.apply(s.trl.Event(&protocol.AgentSettled{}))
	s.resetSessionState()
	s.settleStarts()
	s.sess = SessionInfo{Cwd: s.sess.Cwd}
	s.send = nil
	s.appendBlock(NewErrorBlock(
		"Session "+ShortID(v.SessionID)+" is gone: the leader restarted.",
		"The transcript above is kept. Your next prompt starts a new session."))
}

// disconnectedRows are the two status rows of a lost link.
func (s *sessionView) disconnectedRows(width int) string {
	th := s.m.theme
	rail := th.Render(RoleDim, GlyphRailStatus) + " "
	head := "Disconnected from the leader"
	if s.info.Mode == ModeInProcess {
		head = "Disconnected from the agent"
	}
	tail := " · reconnecting"
	if s.reconnect.attempt > 0 {
		tail = fmt.Sprintf(" · reconnecting (attempt %d, next in %s)", s.reconnect.attempt, s.countdown(s.reconnect.at.Add(s.reconnect.wait)))
	}
	if width < shortWidth {
		tail = ""
		if s.reconnect.attempt > 0 {
			tail = fmt.Sprintf(" · attempt %d", s.reconnect.attempt)
		}
	}
	first := rail + th.Render(RoleError, GlyphFail+" "+head) + th.Render(RoleMuted, tail)
	hint := "draft kept · sending waits until the leader is back · nothing is resent"
	if s.now().Before(s.reconnect.hintUntil) {
		hint = "Reconnecting, please wait…"
	}
	return first + "\n" + rail + th.Render(RoleDim, hint)
}

// countdown writes the time until a moment in whole seconds, rounded up.
func (s *sessionView) countdown(due time.Time) string {
	left := due.Sub(s.now())
	if left < 0 {
		left = 0
	}
	return fmt.Sprintf("%ds", int((left+time.Second-1)/time.Second))
}
