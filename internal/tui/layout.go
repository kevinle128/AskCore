package tui

import (
	"fmt"
	"strings"
)

// Live region layout.
//
// The live region is the part of the screen that the program redraws: the
// transcript tail, status, pending input, a completion list, the editor slot
// and the footer. Layout gives rows to the slots in a fixed priority order, so
// a small terminal always keeps the editor usable. It counts terminal columns
// and rows, never bytes.
//
// Priority, first to last (the last slots lose rows first):
//  1. editor slot: at least 1 row; up to 30% of the height, then it scrolls
//  2. status: 1 row when it has content; a disconnected link uses 2 rows
//  3. footer: 1 row; hidden under 8 rows
//  4. pending: up to 3 rows, the last one is "+N more" when rows are cut
//  5. menu (completion list): up to 8 rows, the last one is "… N more"
//  6. transcript tail: the rest, but never the last screen row; when it does
//     not fit, its last rows show
//
// Under 4 rows only the editor and the status remain. A modal slot (a
// selector or a dialog) replaces the editor and may use up to 12 rows.
//
// Top to bottom the rows are drawn: tail, status, pending, menu, editor,
// footer.

const (
	editorHeightPercent = 30
	statusMaxRows       = 2
	pendingMaxRows      = 3
	menuMaxRows         = 8
	modalMaxRows        = 12
	footerMinHeight     = 8
	compactHeight       = 4
	minBoxedEditorRows  = 3
	minBoxedWidth       = 6
	boxPadding          = 4 // "│ " on the left and " │" on the right
)

// BoxInnerWidth is the width that an editor row may use inside the box that
// layout draws around the editor, for a terminal width.
func BoxInnerWidth(width int) int { return max(0, width-boxPadding) }

// CursorPos is a position in rows and columns, counted from the top left of
// the rows it refers to.
type CursorPos struct{ Row, Col int }

// EditorSlot is the content of the editor slot.
type EditorSlot struct {
	// Rows are the inner rows of the editor, without the box. With Modal
	// set, they are the complete rows of a selector or dialog.
	Rows []string
	// Cursor is the cursor in Rows. Nil hides the hardware cursor.
	Cursor *CursorPos
	// Modal marks a selector or dialog: layout draws no box and keeps the
	// first rows when it must cut.
	Modal bool
}

// Slots is what the components contribute to the live region. All rows are
// already drawn for the terminal width. Layout still guards the width.
type Slots struct {
	Editor EditorSlot
	// Status is the run status. A line feed splits it into rows; layout gives
	// it at most statusMaxRows.
	Status  string
	Footer  string
	Pending []string
	Menu    []string
}

// SlotSource supplies the slots for a terminal width. The root model calls it
// from View, so it must not wait for I/O.
type SlotSource interface {
	Slots(width int) Slots
}

// Frame is the live region ready for the renderer.
type Frame struct {
	Rows []string
	// Cursor is the hardware cursor inside Rows, or nil.
	Cursor *CursorPos
}

// demand is the number of rows each slot wants.
type demand struct {
	editor, status, footer, pending, menu, tail int
	modal                                       bool
}

// alloc is the number of rows each slot gets.
type alloc struct {
	editor, status, footer, pending, menu, tail int
}

func editorCap(height int, modal bool) int {
	if modal {
		return min(modalMaxRows, height)
	}
	return max(1, height*editorHeightPercent/100)
}

// allocate gives rows to the slots in priority order.
func allocate(height int, d demand) alloc {
	var a alloc
	if height <= 0 {
		return a
	}
	rem := height
	take := func(want, limit int) int {
		n := max(0, min(want, limit, rem))
		rem -= n
		return n
	}
	a.editor = take(d.editor, editorCap(height, d.modal))
	a.status = take(d.status, statusMaxRows)
	if height < compactHeight {
		return a
	}
	if height >= footerMinHeight {
		a.footer = take(d.footer, 1)
	}
	a.pending = take(d.pending, pendingMaxRows)
	a.menu = take(d.menu, menuMaxRows)
	a.tail = take(d.tail, rem)
	return a
}

// composeFrame lays out the live region for a terminal of w columns and h rows.
func composeFrame(s Slots, tail []string, w, h int, th *Theme) Frame {
	if w < 1 || h < 1 {
		return Frame{}
	}
	ed := s.Editor
	boxed := !ed.Modal && len(ed.Rows) > 0 && w >= minBoxedWidth && editorCap(h, false) >= minBoxedEditorRows

	d := demand{
		editor:  len(ed.Rows),
		footer:  boolInt(s.Footer != ""),
		status:  statusRowCount(s.Status),
		pending: len(s.Pending),
		menu:    len(s.Menu),
		tail:    len(tail),
		modal:   ed.Modal,
	}
	if boxed {
		d.editor += 2
	}
	a := allocate(h, d)
	if h >= compactHeight {
		// The frame never fills the whole screen. A commit scrolls the screen
		// by the rows it writes; with one free row above the frame, the row
		// that leaves the screen is always history and never a stale copy of
		// the frame.
		others := a.editor + a.status + a.footer + a.pending + a.menu
		a.tail = max(0, min(a.tail, h-1-others))
	}

	var rows []string
	add := func(r string) { rows = append(rows, truncateTo(r, w)) }

	for _, r := range tail[max(0, len(tail)-a.tail):] {
		add(r)
	}
	if a.status > 0 {
		for _, r := range strings.Split(s.Status, "\n")[:a.status] {
			add(r)
		}
	}
	for _, r := range limitRows(s.Pending, a.pending, func(n int) string {
		return th.Render(RoleDim, fmt.Sprintf("%s +%d more", GlyphRailQueue, n))
	}) {
		add(r)
	}
	for _, r := range limitRows(s.Menu, a.menu, func(n int) string {
		return th.Render(RoleDim, fmt.Sprintf("… %d more", n))
	}) {
		add(r)
	}

	editorTop := len(rows)
	cursor := layoutEditor(&rows, ed, a.editor, boxed, w, th)
	if cursor != nil {
		cursor.Row += editorTop
	}

	if a.footer > 0 {
		add(s.Footer)
	}
	return Frame{Rows: rows, Cursor: cursor}
}

// layoutEditor appends the editor rows and returns the cursor relative to the
// first appended row, or nil when the cursor is not visible.
func layoutEditor(out *[]string, ed EditorSlot, n int, boxed bool, w int, th *Theme) *CursorPos {
	if n <= 0 {
		return nil
	}
	inner := n
	if boxed {
		inner = n - 2
	}
	start := 0
	if !ed.Modal && ed.Cursor != nil {
		start = min(max(0, ed.Cursor.Row-inner+1), max(0, len(ed.Rows)-inner))
	}
	visible := ed.Rows[start:min(len(ed.Rows), start+inner)]

	var cursor *CursorPos
	if ed.Cursor != nil && ed.Cursor.Row >= start && ed.Cursor.Row < start+len(visible) {
		cursor = &CursorPos{Row: ed.Cursor.Row - start, Col: ed.Cursor.Col}
	}

	if !boxed {
		for _, r := range visible {
			*out = append(*out, truncateTo(r, w))
		}
		if cursor != nil {
			cursor.Col = min(cursor.Col, w-1)
		}
		return cursor
	}

	innerWidth := BoxInnerWidth(w)
	bar := th.Render(RoleBorder, "│")
	*out = append(*out, th.Render(RoleBorder, "╭"+strings.Repeat("─", w-2)+"╮"))
	for _, r := range visible {
		*out = append(*out, bar+" "+padTo(truncateTo(r, innerWidth), innerWidth)+" "+bar)
	}
	*out = append(*out, th.Render(RoleBorder, "╰"+strings.Repeat("─", w-2)+"╯"))
	if cursor != nil {
		cursor.Row++
		cursor.Col = min(cursor.Col+2, w-3)
	}
	return cursor
}

// limitRows cuts a list to n rows. When rows are cut, the last row is a
// marker that counts the rows it hides.
func limitRows(rows []string, n int, marker func(hidden int) string) []string {
	if n <= 0 {
		return nil
	}
	if len(rows) <= n {
		return rows
	}
	out := append([]string(nil), rows[:n-1]...)
	return append(out, marker(len(rows)-(n-1)))
}

// statusRowCount is the number of rows that a status wants.
func statusRowCount(status string) int {
	if status == "" {
		return 0
	}
	return min(statusMaxRows, strings.Count(status, "\n")+1)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
