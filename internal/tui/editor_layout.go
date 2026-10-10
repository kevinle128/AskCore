package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Editor layout: where the draft wraps, where the cursor is, and the rows that
// the layout draws inside the editor box.
//
// Rows wrap at word boundaries; a word longer than a row breaks at the column
// limit. Width is counted in terminal columns by grapheme, so a wide
// character is never split and never passes the right edge.

const (
	placeholderText  = "Ask anything  ·  / commands  ·  ctrl+l model"
	placeholderShort = "Ask anything"
	// placeholderMinWidth is the terminal width under which the short
	// placeholder shows.
	placeholderMinWidth = 60
)

// visualRow is a range of the draft that one screen row shows.
type visualRow struct {
	start, end int
	// soft is true when the row ends because the text wraps, not because of
	// a line feed. The next row starts at end.
	soft bool
}

// graphemeCell returns how a grapheme is drawn at a column: the text to draw
// and its width in columns.
func graphemeCell(g string, col, rowWidth int) (string, int) {
	if g == "\t" {
		n := min(tabStop-col%tabStop, rowWidth)
		return strings.Repeat(" ", n), n
	}
	r, _ := utf8.DecodeRuneInString(g)
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return controlMarker, 1
	}
	w := uniseg.StringWidth(g)
	if w > rowWidth {
		return controlMarker, 1
	}
	return g, w
}

// layoutRows breaks the draft into visual rows of at most width columns.
func layoutRows(text string, width int) []visualRow {
	width = max(1, width)
	var rows []visualRow
	lineStart := 0
	for {
		lineEnd := len(text)
		if i := strings.IndexByte(text[lineStart:], '\n'); i >= 0 {
			lineEnd = lineStart + i
		}
		rows = append(rows, wrapLineRows(text, lineStart, lineEnd, width)...)
		if lineEnd == len(text) {
			return rows
		}
		lineStart = lineEnd + 1
	}
}

type graphemeAt struct {
	off int
	str string
}

func wrapLineRows(text string, start, end, width int) []visualRow {
	var gs []graphemeAt
	for rest, off := text[start:end], start; rest != ""; {
		g, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		gs = append(gs, graphemeAt{off, g})
		off += len(g)
		rest = tail
	}
	var rows []visualRow
	rowStart := start
	col := 0
	breakIdx := -1 // index of the first grapheme after the last space of the row
	for i := 0; i < len(gs); {
		g := gs[i]
		_, w := graphemeCell(g.str, col, width)
		if col+w > width && col > 0 {
			if breakIdx >= 0 && gs[breakIdx].off > rowStart {
				rows = append(rows, visualRow{start: rowStart, end: gs[breakIdx].off, soft: true})
				rowStart, i = gs[breakIdx].off, breakIdx
			} else {
				rows = append(rows, visualRow{start: rowStart, end: g.off, soft: true})
				rowStart = g.off
			}
			col, breakIdx = 0, -1
			continue
		}
		col += w
		i++
		if g.str == " " || g.str == "\t" {
			breakIdx = i
		}
	}
	return append(rows, visualRow{start: rowStart, end: end})
}

// columnAt returns the column of the byte offset pos in a row that starts at
// rowStart.
func columnAt(text string, rowStart, pos, width int) int {
	col := 0
	for rest := text[rowStart:pos]; rest != ""; {
		g, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		_, w := graphemeCell(g, col, width)
		col += w
		rest = tail
	}
	return col
}

// cursorRow returns the row index of the cursor. A cursor at the break of a
// wrapped row belongs to the row below.
func cursorRow(rows []visualRow, cursor int) int {
	for i, r := range rows {
		if cursor < r.start || cursor > r.end {
			continue
		}
		if cursor == r.end && r.soft && i+1 < len(rows) {
			continue
		}
		return i
	}
	return len(rows) - 1
}

// offsetAt returns the offset in a row nearest to the column, never right of
// it. In a wrapped row the offset stays before the last grapheme, because the
// end of the row is the start of the next one.
func offsetAt(text string, r visualRow, goal, width int) int {
	col := 0
	last := r.start
	for rest, off := text[r.start:r.end], r.start; rest != ""; {
		g, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		_, w := graphemeCell(g, col, width)
		if col+w > goal {
			return off
		}
		last = off
		col += w
		off += len(g)
		rest = tail
	}
	if r.soft {
		return last
	}
	return r.end
}

// moveVertical moves the cursor one visual row. It keeps the column the user
// started from across several moves. It returns false at the first or last row.
func (e *Editor) moveVertical(dir int) bool {
	rows := layoutRows(e.text, e.textW)
	cur := cursorRow(rows, e.cursor)
	next := cur + dir
	if next < 0 || next >= len(rows) {
		return false
	}
	if !e.goalSet {
		e.goalCol = columnAt(e.text, rows[cur].start, e.cursor, e.textW)
		e.goalSet = true
	}
	e.moveTo(e.snapToMarkerStart(offsetAt(e.text, rows[next], e.goalCol, e.textW)))
	return true
}

// Slot returns the editor rows and the cursor for the layout. Rows fit
// BoxInnerWidth(width); the layout draws the box.
func (e *Editor) Slot(width int, th *Theme) EditorSlot {
	textW := max(1, BoxInnerWidth(width)-promptWidth)
	prompt := th.Render(RoleAccent, GlyphUser) + " "
	indent := strings.Repeat(" ", promptWidth)

	if e.text == "" {
		hint := placeholderText
		if width < placeholderMinWidth {
			hint = placeholderShort
		}
		hint = truncateTo(hint, textW)
		return EditorSlot{
			Rows:   []string{prompt + th.Style(RoleDim).Italic(true).Render(hint)},
			Cursor: &CursorPos{Row: 0, Col: promptWidth},
		}
	}

	rows := layoutRows(e.text, textW)
	out := make([]string, len(rows))
	for i, r := range rows {
		lead := indent
		if i == 0 {
			lead = prompt
		}
		out[i] = lead + e.rowText(r, textW)
	}
	cur := cursorRow(rows, e.cursor)
	col := columnAt(e.text, rows[cur].start, e.cursor, textW)
	return EditorSlot{Rows: out, Cursor: &CursorPos{Row: cur, Col: promptWidth + col}}
}

// rowText draws the graphemes of a row.
func (e *Editor) rowText(r visualRow, width int) string {
	var b strings.Builder
	col := 0
	for rest := e.text[r.start:r.end]; rest != ""; {
		g, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		s, w := graphemeCell(g, col, width)
		b.WriteString(s)
		col += w
		rest = tail
	}
	return b.String()
}
