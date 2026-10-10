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

// noticeRow draws a plain notice on the rail.
func (s *sessionView) noticeRow(text string, isErr bool) string {
	th := s.m.theme
	role := RoleDim
	if isErr {
		role = RoleError
	}
	return th.Render(RoleDim, GlyphRailStatus) + " " + th.Render(role, text)
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
		return s.noticeRow(s.notice.text, s.notice.err)
	case s.opening:
		return s.statusRow(RoleAccent, GlyphWorking, "Starting leader…", "  first run can take a few seconds")
	case s.conn == connDown:
		return s.disconnectedRows(width)
	case s.send != nil:
		return s.statusRow(RoleAccent, GlyphWorking, "Sending…", "")
	case s.failure != nil && s.failure.rev == s.editor.Revision():
		return th.Render(RoleDim, GlyphRailStatus) + " " + th.Render(RoleError, GlyphFail+" "+s.failure.text)
	case s.noticeActive():
		return s.noticeRow(s.notice.text, s.notice.err)
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
