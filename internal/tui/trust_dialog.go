package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Trust boxes.
//
// The startup trust box asks for the trust of a project folder before a
// session starts. The /trust box shows the saved decision of the folder and
// the trust of the current session, and changes the saved decision for the
// next session. Both are boxes in the prompt pane slot. A box owns its rows,
// its highlight and its lock. A key gives back an action for the session view;
// the box never calls the client.
//
// The box acts only on the arrow keys, Enter, Esc and ctrl+d. It drops every
// printable key and every paste, so that a typed-ahead letter cannot choose a
// row.

// trustBoxKind tells the two boxes apart.
type trustBoxKind int

const (
	// trustStartBox is the startup question before a session starts.
	trustStartBox trustBoxKind = iota
	// trustCommandBox is the box of /trust.
	trustCommandBox
)

const (
	trustStartTitle   = "Trust this project folder?"
	trustCommandTitle = "Project trust"
)

// trustChoice is one option row of a trust box.
type trustChoice struct {
	label, short string
	// desc and descShort are the description with full paths and with paths
	// in the form "~/…/<last element>".
	desc, descShort string
	// notNow is the startup choice that saves nothing.
	notNow   bool
	target   protocol.ACPTrustTarget
	decision protocol.ACPTrustDecision
	// current marks the row of the saved decision.
	current bool
}

// trustBox is an open trust box.
type trustBox struct {
	kind    trustBoxKind
	choices []trustChoice
	sel     int
	// locked is true while a save is in flight.
	locked bool
	// err is the text of a failed save, shown under the options until the
	// next key.
	err string
	// req is the number of the trust request that the box waits for.
	req uint64
	// detach is true when the session start leaves the current session first
	// (/new).
	detach bool

	// The rows of the /trust box above the options. rootPath is the canonical
	// root that the rows name.
	root, saved, session string
	rootPath             string
	// storeErr is the warning of a trust store that cannot be read. The box
	// then shows the warning and no options.
	storeErr string
}

// trustAction tells the session view what a key did.
type trustAction int

const (
	trustNone trustAction = iota
	// trustCancel is Esc: Not now in the startup box, close in /trust.
	trustCancel
	// trustChoose confirms the highlighted row.
	trustChoose
	// trustQuit ends the program.
	trustQuit
)

// key handles a key press.
func (tb *trustBox) key(k tea.KeyPressMsg) trustAction {
	key := k.String()
	if key == "ctrl+d" {
		return trustQuit
	}
	if tb.locked {
		return trustNone
	}
	if tb.storeErr != "" {
		if key == "esc" {
			return trustCancel
		}
		return trustNone
	}
	switch key {
	case "esc":
		tb.err = ""
		return trustCancel
	case "up":
		tb.err = ""
		tb.sel = max(0, tb.sel-1)
	case "down":
		tb.err = ""
		tb.sel = min(len(tb.choices)-1, tb.sel+1)
	case "enter":
		tb.err = ""
		return trustChoose
	}
	return trustNone
}

// selected returns the highlighted row.
func (tb *trustBox) selected() (trustChoice, bool) {
	if tb.sel < 0 || tb.sel >= len(tb.choices) {
		return trustChoice{}, false
	}
	return tb.choices[tb.sel], true
}

// hint is the hint row for a width.
func (tb *trustBox) hint(width int) string {
	switch {
	case tb.storeErr != "":
		return "esc close"
	case tb.kind == trustStartBox && width < shortWidth:
		return "↑↓ · enter · esc not now"
	case tb.kind == trustStartBox:
		return hintText(width, []hintPart{{"↑↓ choose", 0}, {"enter confirm", 0}, {"esc not now", 0}, {"your draft is kept", 1}})
	case width < shortWidth:
		return "enter save · esc close"
	}
	return hintText(width, []hintPart{{"↑↓ choose", 0}, {"enter save", 0}, {"esc close", 0}, {"applies to the next session", 1}})
}

// slot draws the box for the editor slot. height is the number of rows of the
// slot. The cursor is hidden while a box is open.
func (tb *trustBox) slot(width, height int, th *Theme) EditorSlot {
	b := newBox(th, width)
	wide := width >= shortWidth
	var c boxContent
	title := trustStartTitle
	if tb.kind == trustCommandBox {
		title = trustCommandTitle
	}
	if tb.storeErr != "" {
		c.addWrapped(th, b.inner, tb.storeErr+".", RoleError, GlyphFail+" ")
		c.addWrapped(th, b.inner, "Ask does not change it. Fix or remove it; the next session reads it again.", RoleMuted, "")
		return b.slot(title, c, tb.hint(width))
	}
	if tb.kind == trustCommandBox && wide {
		c.add(" " + th.Style(RoleFg).Bold(true).Render(tb.root))
		c.add(" " + th.Render(RoleMuted, "Saved: ") + tb.saved)
		c.add(" " + th.Render(RoleMuted, "This session: ") + tb.session)
		c.add("")
	}
	// The box keeps its title, its bottom row and the hint row.
	room := height - 3 - len(c.rows)
	if tb.err != "" {
		room--
	}
	for _, r := range tb.optionRows(b.inner, wide, room, th) {
		c.add(r)
	}
	if tb.err != "" {
		c.add(" " + th.Render(RoleError, GlyphFail+" Not saved: "+tb.err))
	}
	return b.slot(title, c, tb.hint(width))
}

// optionRows draws the option rows in the inner width. When room is lower
// than the number of options, the rows are a window that keeps the
// highlighted row, and the last row is "… <n> more", where n counts every row
// that does not show.
func (tb *trustBox) optionRows(inner int, wide bool, room int, th *Theme) []string {
	labw := 0
	for _, ch := range tb.choices {
		labw = max(labw, displayWidth(ch.label))
	}
	labw += 2
	var rows []string
	for i, ch := range tb.choices {
		rows = append(rows, tb.optionRow(ch, i == tb.sel, inner, labw, wide, th))
	}
	n := len(rows)
	if room >= n || room < 1 {
		return rows
	}
	keep := max(1, room-1)
	start := min(max(0, tb.sel-keep+1), n-keep)
	out := append([]string(nil), rows[start:start+keep]...)
	if room >= 2 {
		out = append(out, th.Render(RoleMuted, fmt.Sprintf("  … %d more", n-keep)))
	}
	return out
}

// optionRow draws one option row: "❯ label  description" at shortWidth
// columns and more, "❯ label" under it, and the current tag at the right.
func (tb *trustBox) optionRow(ch trustChoice, selected bool, inner, labw int, wide bool, th *Theme) string {
	mark := "  "
	if selected {
		mark = GlyphUser + " "
	}
	tag := ""
	if ch.current {
		tag = "current "
	}
	label, desc := ch.short, ""
	if wide {
		label = padTo(ch.label, labw)
		room := max(0, inner-displayWidth(mark)-labw-displayWidth(tag))
		if tag != "" {
			// Keep one blank column between the description and the tag.
			room = max(0, room-1)
		}
		desc = fitDescription(ch.desc, ch.descShort, room)
	}
	gap := max(0, inner-displayWidth(mark)-displayWidth(label)-displayWidth(desc)-displayWidth(tag))
	if selected {
		// The whole row is on the selected background, as in the row menu of
		// the provider box. Without color the theme draws it in reverse.
		return th.Render(RoleSelected, mark+label+desc+strings.Repeat(" ", gap)+tag)
	}
	return mark + label + th.Render(RoleMuted, desc) + strings.Repeat(" ", gap) + th.Render(RoleMuted, tag)
}

// fitDescription returns the full description when it fits room columns,
// else the description with short paths, cut with "…" when it still does not
// fit.
func fitDescription(full, short string, room int) string {
	if displayWidth(full) <= room {
		return full
	}
	if short == "" {
		short = full
	}
	return truncateTo(short, room)
}
