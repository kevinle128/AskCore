package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Selector component.
//
// A selector is a filtered list in a rounded box. It replaces the editor in
// the prompt pane slot and keeps the draft. It owns its filter, its selected row and
// the window of rows it shows. A key press gives back an action for the root;
// the selector never calls the client.
//
// The model selector and the session selector differ only in their rows.

type selectorKind int

const (
	selectModel selectorKind = iota
	selectSession
)

// Column widths of the rows. The model rows end with the context window and a
// tag; the session rows end with the driver state and the watcher count.
const (
	windowColWidth  = 4
	tagColWidth     = 9
	modelRightWidth = windowColWidth + 2 + tagColWidth + 1
	stateColWidth   = 18
	selectorMaxRows = 8
	selectorMinRows = 3
	// selectorRows is the height that the window of a selector is sized for.
	selectorRows = 12
)

// selItem is one row of a selector.
type selItem struct {
	id     string // the model id or the session id
	label  string // left text, without the mark
	dot    string // provider dot of a model row, else empty
	role   Role   // role of the dot
	right  string // right text, already padded to its column width
	filter string // text that the filter matches
	// header marks a group header row: the cursor skips it, and it shows only
	// while a row of its group matches the filter.
	header bool
	// group is the index of the header of a row in a grouped list, or -1.
	group int
}

// selAction tells the root what a key did.
type selAction int

const (
	selNone selAction = iota
	selClose
	selChoose
)

type selector struct {
	kind   selectorKind
	title  string
	hint   string
	items  []selItem
	filter string
	sel    int // index in the filtered list
	top    int // first visible row of the filtered list
	// err and next are shown inside the box after a failed choice.
	err, next string
	// busy is true while the choice is being applied; keys other than esc wait.
	busy bool
	// gen is the generation of the view when the selector opened.
	gen uint64
	// empty is true for a model list with no row: the box shows a hint and has
	// no filter row.
	empty bool
}

// visible returns the indexes of the rows that match the filter. Group headers
// are never in it, so the cursor skips them.
func (sl *selector) visible() []int {
	var out []int
	q := strings.ToLower(sl.filter)
	for i, it := range sl.items {
		if it.header {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(it.filter), q) {
			out = append(out, i)
		}
	}
	return out
}

// dispRow is one drawn row: an item and its place among the selectable rows,
// or -1 for a group header.
type dispRow struct{ item, pos int }

// display lists the drawn rows for the matching rows. A group header comes
// before the first matching row of its group and not at all without one.
func (sl *selector) display(vis []int) []dispRow {
	out := make([]dispRow, 0, len(vis)+4)
	emitted := map[int]bool{}
	for k, idx := range vis {
		if h := sl.items[idx].group; h >= 0 && !emitted[h] {
			emitted[h] = true
			out = append(out, dispRow{item: h, pos: -1})
		}
		out = append(out, dispRow{item: idx, pos: k})
	}
	return out
}

// selected returns the chosen item.
func (sl *selector) selected() (selItem, bool) {
	vis := sl.visible()
	if len(vis) == 0 {
		return selItem{}, false
	}
	return sl.items[vis[min(sl.sel, len(vis)-1)]], true
}

// key handles a key press.
func (sl *selector) key(k tea.KeyPressMsg) selAction {
	key := k.String()
	if key == "esc" {
		return selClose
	}
	if sl.busy {
		return selNone
	}
	sl.err, sl.next = "", ""
	vis := sl.visible()
	switch key {
	case "up", "ctrl+p":
		if len(vis) > 0 {
			sl.sel = (sl.sel + len(vis) - 1) % len(vis)
		}
	case "down", "ctrl+n":
		if len(vis) > 0 {
			sl.sel = (sl.sel + 1) % len(vis)
		}
	case "enter":
		if _, ok := sl.selected(); ok {
			return selChoose
		}
	case "backspace", "ctrl+h":
		if r := []rune(sl.filter); len(r) > 0 {
			sl.filter = string(r[:len(r)-1])
			sl.sel, sl.top = 0, 0
		}
	case "ctrl+u":
		sl.filter, sl.sel, sl.top = "", 0, 0
	default:
		if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			sl.filter += cleanInput(k.Text)
			sl.sel, sl.top = 0, 0
		}
	}
	return selNone
}

// rowBudget is the number of item rows the selector can draw in a terminal
// height, with the rows of an error taken out.
func (sl *selector) rowBudget(height, errRows int) int {
	total := min(selectorRows, height)
	return min(selectorMaxRows, max(selectorMinRows, total-4-errRows))
}

// slot draws the selector for the editor slot.
func (sl *selector) slot(width, height int, th *Theme) EditorSlot {
	inner := max(1, width-boxPadding)
	bar := th.Render(RoleBorder, "│")
	boxed := func(content string) string {
		return bar + " " + padTo(truncateTo(content, inner), inner) + " " + bar
	}
	styledBoxed := func(plain, styled string) string {
		return bar + " " + styled + strings.Repeat(" ", max(0, inner-displayWidth(plain))) + " " + bar
	}

	title := " " + sl.title + " "
	head := "╭─"
	fill := max(0, width-displayWidth(head)-displayWidth(title)-1)
	rows := []string{th.Render(RoleBorder, head) + th.Style(RoleAccent).Bold(true).Render(title) +
		th.Render(RoleBorder, strings.Repeat("─", fill)+"╮")}

	if sl.empty {
		rows = append(rows, boxed(""), boxed(" No usable models yet."), boxed(" Use /providers to add providers."), boxed(""))
		rows = append(rows, th.Render(RoleBorder, "╰"+strings.Repeat("─", max(0, width-2))+"╯"))
		rows = append(rows, "   "+th.Style(RoleDim).Italic(true).Render(truncateTo(sl.hint, max(1, width-3))))
		return EditorSlot{Rows: rows, Modal: true}
	}

	filterPlain := " " + GlyphFilter + " " + sl.filter
	filterStyled := " " + GlyphFilter + " " + sl.filter
	if sl.filter == "" {
		filterPlain += "type to filter"
		filterStyled += th.Style(RoleDim).Italic(true).Render("type to filter")
	}
	rows = append(rows, styledBoxed(filterPlain, filterStyled))
	cursor := &CursorPos{Row: 1, Col: 2 + displayWidth(" "+GlyphFilter+" "+sl.filter)}

	type errLine struct {
		text string
		role Role
		lead string
	}
	var errLines []errLine
	if sl.err != "" {
		for i, line := range wrapLines(sl.err, max(1, inner-3)) {
			l := errLine{text: line, role: RoleError, lead: "   "}
			if i == 0 {
				l.lead = " " + GlyphFail + " "
			}
			errLines = append(errLines, l)
		}
		for _, line := range wrapLines(sl.next, max(1, inner-3)) {
			if line != "" {
				errLines = append(errLines, errLine{text: line, role: RoleMuted, lead: "   "})
			}
		}
	}
	errRows := 0
	if len(errLines) > 0 {
		errRows = len(errLines) + 1
	}

	vis := sl.visible()
	disp := sl.display(vis)
	budget := sl.rowBudget(height, errRows)
	sel := min(sl.sel, max(0, len(vis)-1))
	selPos := 0
	for i, d := range disp {
		if d.pos == sel {
			selPos = i
		}
	}
	shown, hidden := len(disp), 0
	if shown > budget {
		shown = budget - 1
		hidden = len(disp) - shown
	}
	if selPos < sl.top {
		sl.top = selPos
	}
	if selPos >= sl.top+shown {
		sl.top = selPos - shown + 1
	}
	sl.top = max(0, min(sl.top, len(disp)-shown))
	if len(vis) == 0 {
		rows = append(rows, styledBoxed("   no match", "   "+th.Render(RoleDim, "no match")))
	}
	for i := sl.top; i < sl.top+shown && i < len(disp); i++ {
		d := disp[i]
		rows = append(rows, sl.itemRow(sl.items[d.item], d.pos == sel && d.pos >= 0, inner, bar, th))
	}
	if hidden > 0 {
		more := fmt.Sprintf("   … %d more", hidden)
		rows = append(rows, styledBoxed(more, th.Render(RoleDim, more)))
	}
	if len(errLines) > 0 {
		rows = append(rows, boxed(""))
		for _, l := range errLines {
			rows = append(rows, styledBoxed(l.lead+l.text, th.Render(l.role, l.lead+l.text)))
		}
	}
	rows = append(rows, th.Render(RoleBorder, "╰"+strings.Repeat("─", max(0, width-2))+"╯"))
	rows = append(rows, "   "+th.Style(RoleDim).Italic(true).Render(truncateTo(sl.hint, max(1, width-3))))
	return EditorSlot{Rows: rows, Cursor: cursor, Modal: true}
}

// itemRow draws one row of the list.
func (sl *selector) itemRow(it selItem, selected bool, inner int, bar string, th *Theme) string {
	if it.header {
		plain := "  " + it.dot + " " + it.label
		styled := "  " + th.Render(it.role, it.dot) + " " + th.Style(RoleMuted).Bold(true).Render(it.label)
		return bar + " " + styled + strings.Repeat(" ", max(0, inner-displayWidth(plain))) + " " + bar
	}
	mark := "  "
	if selected {
		mark = GlyphUser + " "
	}
	dot := ""
	if it.dot != "" {
		dot = it.dot + " "
	}
	rightW := displayWidth(it.right)
	leftRoom := max(1, inner-rightW-1)
	label := truncateTo(it.label, max(1, leftRoom-displayWidth(mark)-displayWidth(dot)))
	plainLeft := mark + dot + label
	gap := max(1, inner-displayWidth(plainLeft)-rightW)
	if selected {
		plain := plainLeft + strings.Repeat(" ", gap) + it.right
		return bar + " " + th.Render(RoleSelected, padTo(plain, inner)) + " " + bar
	}
	styled := mark + th.Render(it.role, it.dot)
	if it.dot != "" {
		styled += " "
	}
	styled += label + strings.Repeat(" ", gap) + th.Render(RoleMuted, it.right)
	return bar + " " + styled + strings.Repeat(" ", max(0, inner-displayWidth(plainLeft)-gap-rightW)) + " " + bar
}

// providerRole gives each provider a dot color, in the order the providers
// first appear in the list.
func providerRole(order map[string]int, provider string) Role {
	n, ok := order[provider]
	if !ok {
		n = len(order)
		order[provider] = n
	}
	return Role(int(RoleProv1) + n%4)
}

// formatWindow writes a context window: "128k", "1M".
func formatWindow(n int) string {
	switch {
	case n <= 0:
		return ""
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1_000_000:
		return trimTenth(float64(n)/1_000_000) + "M"
	}
	return fmt.Sprintf("%dk", n/1000)
}

func padLeft(s string, width int) string {
	if gap := width - displayWidth(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// newModelSelector builds a flat model list. The current model starts selected.
func newModelSelector(models []protocol.ACPModel, current string) *selector {
	return newModelSelectorFor(protocol.ACPModelsResult{Models: models}, current)
}

// newModelSelectorFor builds the model list of an answer. When the leader
// filtered the list (UsableOnly), the rows are grouped by provider, with a
// header for each. An old leader sends every row, and the list stays flat. The
// current model starts selected; a saved default that is not current gets a
// tag of its own.
func newModelSelectorFor(res protocol.ACPModelsResult, current string) *selector {
	sl := &selector{kind: selectModel, title: "Select model", hint: "enter select  ·  esc close  ·  your draft is kept"}
	grouped := res.UsableOnly
	order := map[string]int{}
	// One model can be listed on two wire formats. The label then names the
	// format, so that two rows never look the same.
	seen := map[string]bool{}
	shown := map[string]int{}
	for _, m := range res.Models {
		if !seen[m.ModelID] {
			seen[m.ModelID] = true
			shown[m.Provider+"/"+m.ID]++
		}
	}
	seen = map[string]bool{}
	headers := map[string]int{}
	for _, m := range res.Models {
		if seen[m.ModelID] {
			continue
		}
		seen[m.ModelID] = true
		role := providerRole(order, m.Provider)
		group := -1
		if grouped {
			h, ok := headers[m.Provider]
			if !ok {
				h = len(sl.items)
				headers[m.Provider] = h
				sl.items = append(sl.items, selItem{
					label: m.Provider, dot: GlyphToolDone, role: role, header: true, group: -1,
				})
			}
			group = h
		}
		tag := ""
		switch {
		case m.ModelID == current:
			tag = "current"
			sl.sel = countRows(sl.items)
		case res.Default != "" && m.ModelID == res.Default:
			tag = "default"
		case m.Reasoning:
			tag = "reasoning"
		}
		name := m.Provider + "/" + m.ID
		label := name
		if grouped {
			label = m.ID
		}
		if shown[name] > 1 && m.API != "" {
			label += " (" + m.API + ")"
		}
		it := selItem{
			id: m.ModelID, label: label, dot: GlyphToolDone, role: role, group: group,
			right:  padLeft(formatWindow(m.ContextWindow), windowColWidth) + "  " + padTo(tag, tagColWidth) + " ",
			filter: name + " " + label,
		}
		if grouped {
			it.dot = ""
			it.label = "  " + label
		}
		sl.items = append(sl.items, it)
	}
	if countRows(sl.items) == 0 {
		sl.empty = true
		sl.hint = "esc close  ·  your draft is kept"
	}
	return sl
}

// countRows counts the rows that are not group headers.
func countRows(items []selItem) int {
	n := 0
	for _, it := range items {
		if !it.header {
			n++
		}
	}
	return n
}

// newSessionSelector builds the list of live sessions.
func newSessionSelector(list []protocol.ACPLiveSession, current, home string) *selector {
	sl := &selector{kind: selectSession, title: "Live sessions", hint: "enter watch  ·  esc close"}
	for i, ls := range list {
		state := "no driver"
		switch {
		case ls.Role == protocol.ACPRoleDriver:
			state = "this client drives"
		case ls.HasDriver:
			state = "driver busy"
		}
		if ls.SessionID == current {
			sl.sel = i
		}
		cwd := tildePath(ls.Cwd, home)
		sl.items = append(sl.items, selItem{
			id:     ls.SessionID,
			label:  ShortID(ls.SessionID) + "  " + cwd,
			right:  padLeft(state, stateColWidth),
			filter: ShortID(ls.SessionID) + " " + cwd,
			group:  -1,
		})
	}
	return sl
}
