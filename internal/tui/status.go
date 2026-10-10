package tui

import (
	"fmt"
	"strings"
	"time"
)

// Run status component.
//
// The status shows one state at a time on the dashed live rail. The order of
// the states below is the order of priority: a lost link hides everything
// else, and an idle session with no notice shows nothing. A cancel request
// stays on screen as "Cancelling…" until the agent reports that the run
// settled; a stopped spinner is never taken as the end of a run.

// shortWidth is the terminal width under which status, footer and placeholder
// use their short forms.
const shortWidth = 60

// statusRow draws "┊ ◌ Word tail": the rail, the glyph and word in a role, and
// a muted tail.
func (s *sessionView) statusRow(role Role, glyph, word, tail string) string {
	th := s.m.theme
	return th.Render(RoleDim, GlyphRailStatus) + " " + th.Render(role, glyph+" "+word) + th.Render(RoleMuted, tail)
}

// noticeRow draws a notice on the rail. A text with line breaks takes one rail
// row for each line.
func (s *sessionView) noticeRow(n noticeView) string {
	th := s.m.theme
	role := RoleDim
	if n.err {
		role = RoleError
	}
	rail := th.Render(RoleDim, GlyphRailStatus) + " "
	lines := strings.Split(n.text, "\n")
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = rail
		if n.ok && i == 0 {
			rows[i] += th.Render(RoleSuccess, GlyphOK) + " "
		}
		rows[i] += th.Render(role, line)
	}
	return strings.Join(rows, "\n")
}

// noModel tells that the state of the session was read and the session has no
// model. Before the first state answer an empty model id means "not known".
func (s *sessionView) noModel() bool {
	return s.haveState && s.modelID == "" && !s.opening
}

// noModelNotice is the persistent rail notice while there is no model.
// A session whose model was removed keeps model selection as its next step.
func (s *sessionView) noModelNotice(short bool) string {
	switch {
	case s.hadModel || len(s.models) > 0:
		return "No model. Choose one with /model"
	case short:
		return "Add a provider with /providers"
	}
	return "Add a provider with /providers · then choose a model with /model"
}

// elapsed writes the time since a start in whole seconds.
func (s *sessionView) elapsed(since time.Time) string {
	d := s.now().Sub(since)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

// statusText returns the status rows for a terminal width, or "".
func (s *sessionView) statusText(width int) string {
	th := s.m.theme
	short := width < shortWidth
	switch {
	case s.opening && s.noticeActive():
		return s.noticeRow(s.notice)
	case s.opening:
		return s.statusRow(RoleAccent, GlyphWorking, "Starting leader…", "  first run can take a few seconds")
	case s.conn == connDown:
		return s.disconnectedRows(width)
	case s.send != nil:
		return s.statusRow(RoleAccent, GlyphWorking, "Sending…", "")
	case s.failure != nil && s.failure.rev == s.editor.Revision():
		return th.Render(RoleDim, GlyphRailStatus) + " " + th.Render(RoleError, GlyphFail+" "+s.failure.text)
	case s.now().Before(s.copiedUntil):
		return s.noticeRow(noticeView{text: "Copied", ok: true})
	case s.waitRow() != "":
		return s.waitRow()
	case s.noticeActive():
		return s.noticeRow(s.notice)
	case s.run.running && s.run.retry != nil:
		return s.retryRow(short)
	case s.run.running && s.run.cancelling:
		return s.statusRow(RoleAccent, GlyphWorking, "Cancelling…", " · waiting for the run to stop")
	case s.run.running:
		tail := " " + s.elapsed(s.run.started)
		switch {
		case s.isObserver():
			tail += " · another client drives this session"
		case short:
			tail += " · esc"
		default:
			tail += " · esc to interrupt"
		}
		return s.statusRow(RoleAccent, GlyphWorking, "Working…", tail)
	case s.noModel() && s.sel == nil:
		// The model list says the same thing, so the notice waits while it is open.
		return s.noticeRow(noticeView{text: s.noModelNotice(short)})
	}
	return ""
}

func (s *sessionView) retryRow(short bool) string {
	r := s.run.retry
	word := "Retrying"
	if r.limit > 0 {
		word += fmt.Sprintf(" (%d/%d)", r.attempt, r.limit)
	} else {
		word += fmt.Sprintf(" (%d)", r.attempt)
	}
	tail := " in " + s.countdown(r.due)
	if !short {
		if r.reason != "" {
			tail += " · " + r.reason
		}
		tail += " · esc to cancel"
	}
	return s.statusRow(RoleWarning, GlyphWorking, word, strings.TrimRight(tail, " "))
}
