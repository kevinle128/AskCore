package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Provider box.
//
// /providers replaces the editor with one row for each way to sign in. The
// search row is always last. The box owns its filter, its selected row and its
// window; a key press gives back an action for the session view, and the box
// never calls the client.

const (
	// providerBoxChrome is the number of rows that the box keeps for itself
	// besides the list rows: title, the "more" row, search row, bottom and hint.
	providerBoxChrome = 5
	// providerWindowMin is the step of a page key. A slot that is too low shows
	// fewer list rows than this.
	providerWindowMin = 3
	providerWindowMax = 8

	providerSearchPlaceholder = "Type to search"
	providerLoadingText       = "Loading providers…"
)

// provItem is one row of the box.
type provItem struct {
	opt  protocol.ACPLoginOption
	role Role
}

// suffix is the status text at the right of the row.
func (it provItem) suffix() string {
	switch it.opt.State {
	case protocol.ACPLoginStateSignedIn:
		return "signed in"
	case protocol.ACPLoginStateAPIKey:
		return "api key"
	case protocol.ACPLoginStateEnv:
		if it.opt.EnvVar != "" {
			return "env " + it.opt.EnvVar
		}
		return "env"
	}
	return ""
}

// saved tells that the row has a credential that the leader stores.
func (it provItem) saved() bool {
	return it.opt.State == protocol.ACPLoginStateSignedIn || it.opt.State == protocol.ACPLoginStateAPIKey
}

func (it provItem) key() string { return it.opt.ProviderID + "/" + it.opt.MethodID }

// rowMenu is the two-row menu of a row that has a saved credential.
type rowMenu struct {
	item provItem
	sel  int // 0 sign in again, 1 remove
}

type providerBox struct {
	items   []provItem
	gen     uint64
	loading bool
	// loadErr is the reason the list could not load, or empty.
	loadErr string
	filter  string
	sel     int // index in the filtered list
	top     int // first visible row of the filtered list
	menu    *rowMenu
	// notes are the rows of an error under the list (nothing to remove).
	notes []string
	// req numbers the list requests; only the answer to the latest applies.
	req uint64
}

// provAction tells the session view what a key did.
type provAction int

const (
	provNone provAction = iota
	provClose
	provStart  // start the flow of the selected row
	provRemove // remove the saved credential of the selected row
	provMenu   // sign in again from the row menu
)

// visible returns the indexes of the rows that match the filter. The filter
// matches the name and the provider id.
func (pb *providerBox) visible() []int {
	var out []int
	q := strings.ToLower(pb.filter)
	for i, it := range pb.items {
		if q == "" || strings.Contains(strings.ToLower(it.opt.Name), q) || strings.Contains(strings.ToLower(it.opt.ProviderID), q) {
			out = append(out, i)
		}
	}
	return out
}

// selected returns the chosen row.
func (pb *providerBox) selected() (provItem, bool) {
	vis := pb.visible()
	if len(vis) == 0 {
		return provItem{}, false
	}
	return pb.items[vis[min(pb.sel, len(vis)-1)]], true
}

// setItems puts a new list in the box. The selection stays on the same
// provider and method; when that row is gone it moves to the row at the same
// position, which is the nearest one. The filter stays.
func (pb *providerBox) setItems(opts []protocol.ACPLoginOption) {
	prev, hadPrev := pb.selected()
	prevPos := pb.sel
	order := map[string]int{}
	pb.items = pb.items[:0]
	for _, o := range opts {
		pb.items = append(pb.items, provItem{opt: o, role: providerRole(order, o.ProviderID)})
	}
	pb.loading, pb.loadErr = false, ""
	vis := pb.visible()
	pb.sel = min(prevPos, max(0, len(vis)-1))
	if hadPrev {
		for pos, idx := range vis {
			if pb.items[idx].key() == prev.key() {
				pb.sel = pos
				break
			}
		}
	}
	pb.notes = nil
}

// key handles a key press.
func (pb *providerBox) key(k tea.KeyPressMsg) provAction {
	key := k.String()
	if pb.menu != nil {
		switch key {
		case "esc":
			pb.menu = nil
		case "up", "ctrl+p", "down", "ctrl+n":
			pb.menu.sel = 1 - pb.menu.sel
		case "enter":
			if pb.menu.sel == 1 {
				return provRemove
			}
			return provMenu
		}
		return provNone
	}
	if key == "esc" {
		return provClose
	}
	pb.notes = nil
	vis := pb.visible()
	switch key {
	case "up", "ctrl+p":
		if len(vis) > 0 {
			pb.sel = (pb.sel + len(vis) - 1) % len(vis)
		}
	case "down", "ctrl+n":
		if len(vis) > 0 {
			pb.sel = (pb.sel + 1) % len(vis)
		}
	case "pgup":
		pb.sel = max(0, pb.sel-providerWindowMin)
	case "pgdown":
		if len(vis) > 0 {
			pb.sel = min(len(vis)-1, pb.sel+providerWindowMin)
		}
	case "enter":
		it, ok := pb.selected()
		if !ok || pb.loading || pb.loadErr != "" {
			return provNone
		}
		if it.saved() {
			pb.menu = &rowMenu{item: it}
			return provNone
		}
		return provStart
	case "delete":
		if _, ok := pb.selected(); ok && !pb.loading && pb.loadErr == "" {
			return provRemove
		}
	case "backspace", "ctrl+h":
		if r := []rune(pb.filter); len(r) > 0 {
			pb.filter = string(r[:len(r)-1])
			pb.sel, pb.top = 0, 0
		}
	case "ctrl+u":
		pb.filter, pb.sel, pb.top = "", 0, 0
	default:
		if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			pb.addFilter(k.Text)
		}
	}
	return provNone
}

// addFilter appends search text. Line breaks are removed.
func (pb *providerBox) addFilter(text string) {
	text = strings.NewReplacer("\r", "", "\n", "").Replace(cleanInput(text))
	if text == "" {
		return
	}
	pb.notes = nil
	pb.filter += text
	pb.sel, pb.top = 0, 0
}

// providerWindow is the number of rows the list shows in a slot of height rows.
func providerWindow(height int) int {
	return min(providerWindowMax, max(1, height-providerBoxChrome))
}

// hint is the key hints of the box for a width.
func (pb *providerBox) hint(width int) string {
	if pb.menu != nil {
		return hintText(width, []hintPart{{"enter choose", 0}, {"esc back", 0}, {"your draft is kept", 2}})
	}
	parts := []hintPart{}
	if it, ok := pb.selected(); ok && it.saved() {
		parts = append(parts, hintPart{"enter manage", 0}, hintPart{"delete remove", 1})
	} else {
		parts = append(parts, hintPart{"enter sign in", 0})
	}
	parts = append(parts, hintPart{"esc close", 0}, hintPart{"your draft is kept", 2})
	return hintText(width, parts)
}

// rowText draws one provider row in the inner width.
func (pb *providerBox) rowText(it provItem, selected bool, inner int, th *Theme) string {
	mark := "  "
	if selected {
		mark = GlyphUser + " "
	}
	dot := GlyphToolDone + " "
	suffix := it.suffix()
	rightW := displayWidth(suffix)
	room := max(1, inner-displayWidth(mark)-displayWidth(dot))
	nameRoom := room
	if rightW > 0 {
		nameRoom = max(1, room-rightW-1)
	}
	name := truncateTo(oneLine(it.opt.Name), nameRoom)
	left := mark + dot + name
	gap := max(1, inner-displayWidth(left)-rightW)
	if selected {
		return th.Render(RoleSelected, padTo(left+strings.Repeat(" ", gap)+suffix, inner))
	}
	styled := mark + th.Render(it.role, GlyphToolDone) + " " + name + strings.Repeat(" ", gap)
	if suffix != "" {
		styled += th.Render(RoleSuccess, suffix)
	}
	return styled
}

// slot draws the box for the editor slot.
func (pb *providerBox) slot(width, height int, th *Theme) EditorSlot {
	b := newBox(th, width)
	if pb.menu != nil {
		return pb.menuSlot(b)
	}
	var c boxContent
	vis := pb.visible()
	sel := min(pb.sel, max(0, len(vis)-1))
	window := providerWindow(height - pb.noteRows(b.inner))
	switch {
	case pb.loading:
		c.add(" " + th.Style(RoleDim).Render(providerLoadingText))
	case pb.loadErr != "":
		c.addWrapped(th, b.inner, "Could not load providers: "+pb.loadErr, RoleError, GlyphFail+" ")
	case len(vis) == 0:
		c.add("   " + th.Render(RoleDim, "No match"))
	default:
		shown := min(len(vis), window)
		if sel < pb.top {
			pb.top = sel
		}
		if sel >= pb.top+shown {
			pb.top = sel - shown + 1
		}
		pb.top = max(0, min(pb.top, len(vis)-shown))
		for i := pb.top; i < pb.top+shown; i++ {
			c.add(pb.rowText(pb.items[vis[i]], i == sel, b.inner, th))
		}
		if below := len(vis) - (pb.top + shown); below > 0 {
			c.add(th.Render(RoleDim, fmt.Sprintf("  … %d more", below)))
		}
	}
	if len(pb.notes) > 0 {
		c.add("")
		for i, n := range pb.notes {
			lead := "   "
			if i == 0 {
				lead = " " + GlyphFail + " "
			}
			for j, r := range wrapLines(n, max(1, b.inner-3)) {
				l := lead
				if j > 0 {
					l = "   "
				}
				c.add(th.Render(RoleError, l+r))
			}
		}
	}
	search := " " + GlyphFilter + " " + pb.filter
	if pb.filter == "" {
		c.add(th.Render(RoleMuted, " "+GlyphFilter+" ") + th.Style(RoleDim).Italic(true).Render(providerSearchPlaceholder))
	} else {
		c.add(th.Render(RoleMuted, " "+GlyphFilter+" ") + pb.filter)
	}
	c.cursor = &CursorPos{Row: len(c.rows) - 1, Col: displayWidth(search)}
	return b.slot("Providers", c, pb.hint(width))
}

// noteRows is the number of rows that the notes take: an empty row and the
// wrapped lines of each note.
func (pb *providerBox) noteRows(inner int) int {
	if len(pb.notes) == 0 {
		return 0
	}
	n := 1
	for _, note := range pb.notes {
		n += len(wrapLines(note, max(1, inner-3)))
	}
	return n
}

// menuSlot draws the row menu of a row that has a saved credential.
func (pb *providerBox) menuSlot(b boxDraw) EditorSlot {
	th := b.th
	var c boxContent
	c.add(" " + th.Render(RoleMuted, "Signed in. What do you want to do?"))
	c.add("")
	items := []struct{ name, text string }{
		{"Sign in again", "Use another account or refresh this one"},
		{"Remove", "Delete the saved sign-in for " + pb.menu.item.opt.ProviderID},
	}
	for i, it := range items {
		mark := "  "
		if i == pb.menu.sel {
			mark = GlyphUser + " "
		}
		plain := mark + padTo(it.name, 15) + it.text
		if i == pb.menu.sel {
			c.add(th.Render(RoleSelected, padTo(truncateTo(plain, b.inner), b.inner)))
			continue
		}
		c.add(mark + padTo(it.name, 15) + th.Render(RoleMuted, it.text))
	}
	return b.slot(oneLine(pb.menu.item.opt.Name), c, pb.hint(b.width))
}
