package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Time at the right of a first row.
//
// The prompt band and a reply show the 24h time (clockTime) at the right end
// of their first row, in the dim role. Both use the same room rule: the time
// shows only when the row keeps rowTimeGap blank columns before it. A reply
// shows the time only when its first row is a row of prose (mdRow.textRow):
// a reply that starts with a table or a code block shows no time.

// rowTimeGap is the least number of blank columns between the text of a row
// and the time at its right.
const rowTimeGap = 2

// rowTimeRoom is the width that is left for the text of a first row when the
// time takes the right end of a row that is width columns wide. The time and
// its gap never take more than half of the row: on a narrower row, and
// without a time, the whole width is left and the row shows no time.
func rowTimeRoom(width int, at string) int {
	room := width - displayWidth(at) - rowTimeGap
	if at == "" || room < width-width/2 {
		return width
	}
	return room
}

// withRowTime puts the styled time at the right end of a row, so that the row
// becomes width columns wide. fill draws n blank columns. It returns false and
// the row as it is when the time has no room.
func withRowTime(row string, width int, at string, style lipgloss.Style, fill func(n int) string) (string, bool) {
	used := displayWidth(row)
	if at == "" || used+rowTimeGap+displayWidth(at) > width {
		return row, false
	}
	return row + fill(width-used-displayWidth(at)) + style.Render(at), true
}

// spaces is a fill of plain blank columns.
func spaces(n int) string { return strings.Repeat(" ", max(0, n)) }
