package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Selection box.
//
// The user's selection never changes the row count of a block, so nothing
// reflows when the selection moves. The pane draws a thin box around the rows
// of the selected unit, the same for every kind of block: a side mark in
// column 0 and in the last column of each row, the top corners on the row
// above the unit and the bottom corners on its gap row. Column 0 and the last
// column are blank in every block, so the box covers no text. When the window
// clips the box, its edge row in the window shows the clip mark.

// Box glyphs. They draw in the `border` role.
const (
	boxSide        = "│"
	boxTopLeft     = "┌"
	boxTopRight    = "┐"
	boxBottomLeft  = "└"
	boxBottomRight = "┘"
	boxClipped     = "┆"
)

// ANSI sequences that reset all styles and that turn on reverse video.
const (
	ansiReset = "\x1b[m"
	ansiRev   = "\x1b[7m"
)

// paintBox draws the selection box on the content rows of a window. top and
// bottom are the rows of the top and the bottom edge; an edge outside the
// rows is clipped. The rows keep their count and their width.
func paintBox(th *Theme, rows []string, top, bottom, width int) {
	if width < 2 || len(rows) == 0 {
		return
	}
	edge := func(g string) string {
		if th == nil {
			return g
		}
		return th.Render(RoleBorder, g)
	}
	last := len(rows) - 1
	for r := max(top, 0); r <= min(bottom, last); r++ {
		left, right := boxSide, boxSide
		switch r {
		case top:
			left, right = boxTopLeft, boxTopRight
		case bottom:
			left, right = boxBottomLeft, boxBottomRight
		}
		if (r == 0 && top < 0) || (r == last && bottom > last) {
			left, right = boxClipped, boxClipped
		}
		rows[r] = boxEdgeRow(rows[r], edge(left), edge(right), width)
	}
}

// boxEdgeRow puts a mark in column 0 and another in the last column of a row
// that is width columns wide. The style of the row ends before the right mark,
// so that a band does not go on behind it.
func boxEdgeRow(row, left, right string, width int) string {
	row = withGutterMark(row, left)
	if displayWidth(row) > width-1 {
		row = ansi.Truncate(row, width-1, "")
	}
	return row + ansiReset + strings.Repeat(" ", max(0, width-1-displayWidth(row))) + right
}

// withGutterMark puts a mark in column 0 of a row in place of its first cell.
// The style sequences of the row stay where they are, so a band behind the
// first cell goes on behind the other cells. An empty row becomes the mark
// alone.
func withGutterMark(row, mark string) string {
	var b strings.Builder
	b.Grow(len(row) + len(mark))
	b.WriteString(mark)
	var state byte
	dropped := false
	for rest := row; rest != ""; {
		seq, width, n, next := ansi.DecodeSequence(rest, state, nil)
		state, rest = next, rest[n:]
		if !dropped && width > 0 {
			dropped = true
			// A wide first cell leaves the columns after the mark empty.
			b.WriteString(strings.Repeat(" ", width-1))
			continue
		}
		b.WriteString(seq)
	}
	return b.String()
}
