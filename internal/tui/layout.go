package tui

import (
	"fmt"
	"strings"
)

// Screen layout.
//
// The TUI owns the whole alternate screen. Top to bottom the rows are:
//
//	header row
//	rule
//	block pane            all rows that no other part uses
//	queue rows            only when inputs wait
//	status row(s)         only when the rail has text
//	rule
//	prompt pane           menu rows, then the editor rows (or a selector or box)
//	rule
//	shortcut bar
//
// Layout counts terminal columns and rows, never bytes. It gives rows to the
// parts in a fixed priority order, so a small terminal keeps the prompt usable:
//  1. the prompt pane keeps one row, the bar and the header stay, the block
//     pane keeps one row (below 4 rows the pane and then the header give way)
//  2. the three rules, all or none: they exist from rulesMinHeight rows
//  3. the status row
//  4. the prompt pane grows up to half of the screen height
//  5. the queue rows: up to 3, the last one is "+N more" when rows are cut
//  6. the menu rows (completion list): up to 8, inside the half of the prompt
//     pane, the last one is "… N more"
//  7. the block pane takes the rest
//
// A selector or a box replaces the prompt rows and has the same cap.

const (
	statusMaxRows  = 2
	pendingMaxRows = 3
	menuMaxRows    = 8
	// modalMaxRows is the most rows that a selector or dialog takes before the
	// half-height cap of the prompt pane applies. The provider box with an error
	// under its seven rows needs 14.
	modalMaxRows = 14
	// rulesMinHeight is the height from which the rules are drawn. At this
	// height the header, its rule, one pane row, one status row, the two prompt
	// rules, one prompt row and the bar fill the screen.
	rulesMinHeight = 8
	// minPaneHeight is the number of pane rows that a screen of 4 rows or more
	// keeps.
	minPaneHeight = 1
	// promptLeftPad is the number of columns before a row of the editor. The
	// right side keeps one column free for the cursor at the end of a row.
	promptLeftPad = 1
	// boxPadding is the width of "│ " and " │" in the box of a selector or
	// dialog.
	boxPadding = 4
)

// PromptInnerWidth is the width that an editor row may use inside the prompt
// pane, for a terminal width.
func PromptInnerWidth(width int) int { return max(0, width-promptLeftPad-1) }

// promptSlotRows is the most rows that the prompt pane may use for a terminal
// height: half of the screen, rounded down, and at least one row.
func promptSlotRows(height int) int { return max(1, height/2) }

// CursorPos is a position in rows and columns, counted from the top left of
// the rows it refers to.
type CursorPos struct{ Row, Col int }

// EditorSlot is the content of the prompt pane.
type EditorSlot struct {
	// Rows are the rows of the editor. With Modal set, they are the complete
	// rows of a selector or dialog.
	Rows []string
	// Cursor is the cursor in Rows. Nil hides the hardware cursor.
	Cursor *CursorPos
	// Modal marks a selector or dialog: layout adds no left pad and keeps the
	// first rows when it must cut.
	Modal bool
}

// Slots is what the components contribute to the screen. All rows are already
// drawn for the terminal width. Layout still guards the width.
type Slots struct {
	// Header is the row at the top of the screen.
	Header string
	Editor EditorSlot
	// Status is the run status. A line feed splits it into rows; layout gives
	// it at most statusMaxRows.
	Status string
	// Bar is the shortcut bar at the last row.
	Bar     string
	Pending []string
	Menu    []string
}

// SlotSource supplies the slots for a terminal width. The root model calls it
// from View, so it must not wait for I/O.
type SlotSource interface {
	Slots(width int) Slots
}

// Frame is the screen ready for the renderer: exactly one row for each
// terminal row.
type Frame struct {
	Rows []string
	// Cursor is the hardware cursor inside Rows, or nil.
	Cursor *CursorPos
	// Geo tells where the parts are, so that a mouse position maps to a part.
	Geo FrameGeo
}

// FrameGeo is the place of the parts that take mouse input, counted in screen
// rows from the top.
type FrameGeo struct {
	// PaneTop and PaneRows are the rows of the block pane.
	PaneTop, PaneRows int
	// PromptTop and PromptRows are the rows that show the editor, or a selector
	// or box. PromptStart is the index of the first editor row that is shown.
	PromptTop, PromptRows, PromptStart int
}

// demand is the number of rows each part wants. prompt is the number of editor
// rows (or modal rows); menu and queue are the lists that sit above them.
type demand struct {
	prompt, menu, status, queue int
}

// alloc is the number of rows each part gets. headerRule is 0 or 1 and
// promptRules is 0 or 2.
type alloc struct {
	header, headerRule, pane, queue, status, promptRules, prompt, menu, bar int
}

// allocate gives rows to the parts in priority order. The rows add up to
// height.
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
	a.prompt = take(1, 1)
	a.bar = take(1, 1)
	a.header = take(1, 1)
	a.pane = take(minPaneHeight, minPaneHeight)
	if height >= rulesMinHeight {
		a.headerRule = take(1, 1)
		a.promptRules = take(2, 2)
	}
	a.status = take(d.status, statusMaxRows)
	slot := promptSlotRows(height)
	a.prompt += take(d.prompt-1, slot-1)
	a.queue = take(d.queue, pendingMaxRows)
	a.menu = take(d.menu, min(menuMaxRows, slot-a.prompt))
	a.pane += rem
	return a
}

// composeFrame lays out the screen for a terminal of w columns and h rows.
// pane draws the block pane for a number of rows; layout asks for it once the
// other parts have their rows. The frame has h rows.
func composeFrame(s Slots, pane func(rows int) []string, w, h int, th *Theme) Frame {
	if w < 1 || h < 1 {
		return Frame{}
	}
	ed := s.Editor
	d := demand{
		prompt: max(1, len(ed.Rows)),
		menu:   len(s.Menu),
		status: statusRowCount(s.Status),
		queue:  len(s.Pending),
	}
	a := allocate(h, d)

	rows := make([]string, 0, h)
	add := func(r string) { rows = append(rows, truncateTo(r, w)) }
	rule := func() { rows = append(rows, th.Render(RoleBorder, strings.Repeat("─", w))) }

	if a.header > 0 {
		add(s.Header)
	}
	if a.headerRule > 0 {
		rule()
	}
	geo := FrameGeo{PaneTop: len(rows), PaneRows: a.pane}
	if a.pane > 0 && pane != nil {
		got := pane(a.pane)
		got = got[:min(len(got), a.pane)]
		for _, r := range got {
			add(r)
		}
		for i := len(got); i < a.pane; i++ {
			rows = append(rows, "")
		}
	} else {
		for i := 0; i < a.pane; i++ {
			rows = append(rows, "")
		}
	}
	for _, r := range limitRows(s.Pending, a.queue, func(n int) string {
		return th.Render(RoleDim, fmt.Sprintf("%s +%d more", GlyphRailQueue, n))
	}) {
		add(r)
	}
	if a.status > 0 {
		for _, r := range strings.Split(s.Status, "\n")[:a.status] {
			add(r)
		}
	}
	if a.promptRules > 0 {
		rule()
	}
	for _, r := range limitRows(s.Menu, a.menu, func(n int) string {
		return th.Render(RoleDim, fmt.Sprintf("… %d more", n))
	}) {
		add(r)
	}

	editorTop := len(rows)
	geo.PromptTop, geo.PromptRows, geo.PromptStart = editorTop, a.prompt, promptStart(ed, a.prompt)
	cursor := layoutPrompt(&rows, ed, a.prompt, w)
	if cursor != nil {
		cursor.Row += editorTop
	}
	if a.promptRules > 0 {
		rule()
	}
	if a.bar > 0 {
		add(s.Bar)
	}
	return Frame{Rows: rows, Cursor: cursor, Geo: geo}
}

// promptStart is the index of the first editor row that n prompt rows show. An
// editor scrolls to keep its cursor in view; a selector or box keeps its first
// rows.
func promptStart(ed EditorSlot, n int) int {
	if ed.Modal || ed.Cursor == nil {
		return 0
	}
	return min(max(0, ed.Cursor.Row-n+1), max(0, len(ed.Rows)-n))
}

// layoutPrompt appends n prompt rows and returns the cursor relative to the
// first appended row, or nil when the cursor is not visible.
func layoutPrompt(out *[]string, ed EditorSlot, n, w int) *CursorPos {
	if n <= 0 {
		return nil
	}
	start := promptStart(ed, n)
	visible := ed.Rows[min(start, len(ed.Rows)):min(len(ed.Rows), start+n)]

	var cursor *CursorPos
	if ed.Cursor != nil && ed.Cursor.Row >= start && ed.Cursor.Row < start+len(visible) {
		cursor = &CursorPos{Row: ed.Cursor.Row - start, Col: ed.Cursor.Col}
	}
	pad := promptLeftPad
	if ed.Modal {
		pad = 0
	}
	lead := strings.Repeat(" ", pad)
	for _, r := range visible {
		*out = append(*out, truncateTo(lead+r, w))
	}
	// An empty editor still owns its row.
	for i := len(visible); i < n; i++ {
		*out = append(*out, "")
	}
	if cursor != nil {
		cursor.Col = min(cursor.Col+pad, w-1)
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
