package tui

import "strings"

// Pending input component.
//
// The rows are the queue projection that the agent reported with its last
// queue update. Removing an input sends a request; the rows leave the list when
// the agent reports the change, not when the key is pressed. The queue is
// hidden while the link is down, because the projection cannot be checked.

const queueHint = "alt+↑ edit queue · dashed rail = not sent yet"

// pendingRows returns the queue rows and the hint row for a terminal width.
// Layout cuts the list to its row limit and writes the "+N more" marker.
func (s *sessionView) pendingRows(width int) []string {
	if s.conn != connUp || (len(s.queue.steer) == 0 && len(s.queue.follow) == 0) {
		return nil
	}
	th := s.m.theme
	rail := th.Render(RoleDim, GlyphRailQueue) + " "
	room := max(1, width-displayWidth("┆ ⧗ steer   "))
	row := func(role Role, label, text string) string {
		return rail + th.Render(role, GlyphQueued+" "+label) + "  " + truncateTo(oneLine(text), room)
	}
	var rows []string
	for _, t := range s.queue.steer {
		rows = append(rows, row(RoleAssistant, "steer ", t))
	}
	for _, t := range s.queue.follow {
		rows = append(rows, row(RoleTool, "next  ", t))
	}
	hint := truncateTo(queueHint, max(1, width-2))
	return append(rows, rail+th.Style(RoleDim).Italic(true).Render(strings.TrimRight(hint, " ")))
}
