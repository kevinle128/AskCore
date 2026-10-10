package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Dialog composition.
//
// A dialog is a rounded box in the editor slot. It replaces the editor and
// keeps the draft. The provider box, its row menu, the key and login boxes and
// the removal box share the box drawing below. A box holds text rows, at most
// one input row, and a hint row under it. The row helpers measure visible
// columns, so a row may carry style and link sequences.

// boxDraw draws the lines of one box for a terminal width.
type boxDraw struct {
	th    *Theme
	width int
	inner int
}

func newBox(th *Theme, width int) boxDraw {
	return boxDraw{th: th, width: width, inner: max(1, width-boxPadding)}
}

// top is the title row: "╭─ Title ───╮".
func (b boxDraw) top(title string) string {
	room := max(0, b.width-5)
	title = truncateTo(oneLine(title), room)
	head := "╭─"
	text := " " + title + " "
	fill := max(0, b.width-displayWidth(head)-displayWidth(text)-1)
	return b.th.Render(RoleBorder, head) + b.th.Style(RoleAccent).Bold(true).Render(text) +
		b.th.Render(RoleBorder, strings.Repeat("─", fill)+"╮")
}

// row is one content row. content may hold style and link sequences.
func (b boxDraw) row(content string) string {
	bar := b.th.Render(RoleBorder, "│")
	if displayWidth(content) > b.inner {
		content = ansi.Truncate(content, b.inner, truncateMarker)
	}
	gap := max(0, b.inner-displayWidth(content))
	return bar + " " + content + strings.Repeat(" ", gap) + " " + bar
}

// bottom is the closing row.
func (b boxDraw) bottom() string {
	return b.th.Render(RoleBorder, "╰"+strings.Repeat("─", max(0, b.width-2))+"╯")
}

// hintRow is the dim italic row under the box.
func (b boxDraw) hintRow(text string) string {
	return "   " + b.th.Style(RoleDim).Italic(true).Render(truncateTo(text, max(1, b.width-3)))
}

// hintPart is one key hint. Parts with a drop rank above zero leave the row
// when it is too wide, the highest rank first; rank zero parts stay.
type hintPart struct {
	text string
	drop int
}

// hintText joins the key hints for a width. Under shortWidth columns the
// separator is one space on each side.
func hintText(width int, parts []hintPart) string {
	sep := "  ·  "
	if width < shortWidth {
		sep = " · "
	}
	room := max(1, width-3)
	keep := append([]hintPart(nil), parts...)
	join := func() string {
		texts := make([]string, len(keep))
		for i, p := range keep {
			texts[i] = p.text
		}
		return strings.Join(texts, sep)
	}
	for displayWidth(join()) > room {
		worst := -1
		for i, p := range keep {
			if p.drop > 0 && (worst < 0 || p.drop > keep[worst].drop) {
				worst = i
			}
		}
		if worst < 0 {
			break
		}
		keep = append(keep[:worst], keep[worst+1:]...)
	}
	return join()
}

// boxContent is a column of rows with an input cursor position.
type boxContent struct {
	rows   []string
	cursor *CursorPos
}

// add appends a content row with the one-column indent of dialog text.
func (c *boxContent) add(row string) { c.rows = append(c.rows, row) }

// addWrapped appends text wrapped to the box with a role. The first row may
// start with lead (for example a glyph); later rows are indented by the width
// of lead.
func (c *boxContent) addWrapped(th *Theme, inner int, text string, role Role, lead string) {
	indent := strings.Repeat(" ", displayWidth(lead))
	for i, r := range wrapLines(text, max(1, inner-1-displayWidth(lead))) {
		prefix := indent
		if i == 0 {
			prefix = lead
		}
		c.add(" " + th.Render(role, prefix+r))
	}
}

// slot finishes the rows of a box into an editor slot.
func (b boxDraw) slot(title string, c boxContent, hint string) EditorSlot {
	rows := make([]string, 0, len(c.rows)+3)
	rows = append(rows, b.top(title))
	for _, r := range c.rows {
		rows = append(rows, b.row(r))
	}
	rows = append(rows, b.bottom(), b.hintRow(hint))
	var cur *CursorPos
	if c.cursor != nil {
		// The cursor row counts from the first content row; the top row is one
		// above it.
		cur = &CursorPos{Row: c.cursor.Row + 1, Col: min(c.cursor.Col+2, max(0, b.width-3))}
	}
	return EditorSlot{Rows: rows, Cursor: cur, Modal: true}
}
