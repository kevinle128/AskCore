package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Selection marks.
//
// The user's selection never changes the row count of a block, so nothing
// reflows when the selection moves. A selected block that is a step at its
// lowest mode is shaded: the `selected` background covers every column of its
// rows. Any other selected block keeps its colors, because a background would
// clobber styles such as diff colors and the prompt band. It gets a rail in
// the gutter instead: column 0, which every block leaves blank.

// Expand indicators. They stand in the place of the first-row glyph of a step
// that is selected or under the pointer.
const (
	// IndicatorExpand shows that the block can open to more rows.
	IndicatorExpand = "›"
	// IndicatorOpen shows that the block is open and can close.
	IndicatorOpen = "⌄"

	ansiReset = "\x1b[m"
	ansiSGR0  = "\x1b[0m"
	ansiRev   = "\x1b[7m"
)

// indicatorFor returns the glyph that replaces the step glyph of a block that
// is drawn in a mode, or "" when the glyph stays. low is the lowest mode of the
// block. A block that is Truncated above its lowest mode keeps its glyph.
func indicatorFor(rc RenderContext, low FoldMode) string {
	if !rc.Pointed {
		return ""
	}
	switch rc.Fold {
	case low:
		return IndicatorExpand
	case FoldExpanded:
		return IndicatorOpen
	}
	return ""
}

// indicatorGlyph pads an indicator to the width of the glyph it replaces, so
// that a two-column glyph does not move the text of its row.
func indicatorGlyph(indicator, glyph string) string {
	if pad := displayWidth(glyph) - displayWidth(indicator); pad > 0 {
		return indicator + strings.Repeat(" ", pad)
	}
	return indicator
}

// paintSelection applies the selection mark to the rows of a block. Rows of a
// block that is not selected are returned as they are.
func paintSelection(rc RenderContext, b Block, m itemMark, rows []string) []string {
	if !m.selected || len(rows) == 0 || rc.Theme == nil {
		return rows
	}
	if fb, ok := b.(foldBlock); ok && fb.foldGroup().isStep() && fb.foldable(rc.Width) && rc.Fold == FoldCollapsed {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = shadeRow(rc.Theme, r, rc.Width)
		}
		return out
	}
	// The prompt band has its own background; the rail keeps it.
	_, band := b.(userPromptBlock)
	mark := rc.Theme.Style(RoleAccent).Bold(true)
	if band {
		mark = rc.Theme.onBackground(RoleAccent, RolePanel).Bold(true)
	}
	rail := mark.Render(GlyphRail)

	first, last := 0, len(rows)
	if m.skipHead {
		for first < last && rows[first] == "" {
			first++
		}
	}
	if m.skipTail {
		for last > first && rows[last-1] == "" {
			last--
		}
	}
	out := make([]string, len(rows))
	copy(out, rows)
	for i := first; i < last; i++ {
		out[i] = withGutterMark(rows[i], rail)
	}
	return out
}

// shadeRow draws a row on the `selected` background over the whole width. The
// style sequences inside the row end with a reset, which would end the
// background too, so the background starts again after each reset. Without
// color the row is reversed.
func shadeRow(th *Theme, row string, width int) string {
	on := bgSequence(th, RoleSelected)
	if on == "" {
		on = ansiRev
	}
	row = strings.ReplaceAll(row, ansiReset, ansiReset+on)
	row = strings.ReplaceAll(row, ansiSGR0, ansiSGR0+on)
	pad := max(0, width-displayWidth(row))
	return on + row + strings.Repeat(" ", pad) + ansiReset
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
