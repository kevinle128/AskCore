package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"AskCore/pkg/protocol"
)

// Slash command completion.
//
// The list opens when the whole draft is a slash and the start of a command
// name, for example "/" or "/se". It closes on esc, on a space, and when no
// command matches. The commands are fixed and the skills of the session come
// from Editor.SetSkills, so the matches come from the draft at once and no
// stale result can arrive. A skill row is "/skill:<name>": it comes after the
// commands, and choosing it puts the name and a space in the draft instead of
// running anything, so the user can add arguments. Tab completes the draft in
// two steps: the common prefix of the rows, then the selected row.

// maxMenuRows is the most rows that the list shows at once: the rows that the
// layout gives to the menu. A longer list scrolls with the selection and ends
// with a row that counts the rest.
const maxMenuRows = menuMaxRows

// CommandSpec is a slash command that the list offers.
type CommandSpec struct {
	Name        string // with the slash, for example "/model"
	Description string
}

// DefaultCommands returns the commands of the local TUI, in list order.
func DefaultCommands() []CommandSpec {
	return []CommandSpec{
		{"/model", "Select a model"},
		{"/providers", "Add or remove providers"},
		{"/trust", "Trust or do not trust this folder"},
		{"/new", "Start a new session in this directory"},
		{"/sessions", "Watch another live session"},
		{"/take", "Take control of this session"},
		{"/copy", "Copy the last reply"},
		{"/quit", "Detach and quit"},
	}
}

// Completion is the state of the command list. The editor owns it.
type Completion struct {
	commands []CommandSpec
	skills   []CommandSpec
	selected int
	query    string // the draft that selected was chosen for

	// The user closed the list at closedRev. A later edit opens it again.
	closed    bool
	closedRev uint64
}

func isSkillRow(name string) bool { return strings.HasPrefix(name, protocol.SkillCommandPrefix) }

func newCompletion(commands []CommandSpec) Completion {
	return Completion{commands: commands}
}

// matches returns the commands for a draft, or nil when the list is closed.
func (c *Completion) matches(draft string, rev uint64) []CommandSpec {
	if len(c.commands)+len(c.skills) == 0 || !strings.HasPrefix(draft, "/") || strings.ContainsAny(draft, " \t\n") {
		return nil
	}
	if c.closed && c.closedRev == rev {
		return nil
	}
	prefix := strings.ToLower(draft)
	var out []CommandSpec
	for _, list := range [][]CommandSpec{c.commands, c.skills} {
		for _, cmd := range list {
			if strings.HasPrefix(strings.ToLower(cmd.Name), prefix) {
				out = append(out, cmd)
			}
		}
	}
	return out
}

// lookup finds the command that a typed name means. Command names are not
// case sensitive; the result has the canonical spelling.
func (c *Completion) lookup(name string) (string, bool) {
	name = strings.ToLower(name)
	for _, cmd := range c.commands {
		if cmd.Name == name {
			return cmd.Name, true
		}
	}
	return "", false
}

// completionState returns the matches and the selected index for the draft. The
// selection returns to the first row when the draft changes.
func (e *Editor) completionState() ([]CommandSpec, int) {
	list := e.comp.matches(e.text, e.revision)
	if len(list) == 0 {
		return nil, 0
	}
	sel := e.comp.selected
	if e.comp.query != e.text || sel >= len(list) {
		sel = 0
	}
	return list, sel
}

// handleCompletionKey handles the keys that the open list owns. It reports
// whether it consumed the key.
func (e *Editor) handleCompletionKey(key string) (Result, bool) {
	list, sel := e.completionState()
	if len(list) == 0 {
		return Result{}, false
	}
	switch key {
	case "up":
		e.selectCompletion((sel + len(list) - 1) % len(list))
	case "down":
		e.selectCompletion((sel + 1) % len(list))
	case "esc":
		e.comp.closed, e.comp.closedRev = true, e.revision
	case "tab":
		if next := e.tabCompletion(list, sel); next != e.text {
			e.edit(opOther, next, len(next))
		}
		e.selectCompletion(0)
	case "enter", "alt+enter":
		name := list[sel].Name
		if isSkillRow(name) {
			e.edit(opOther, name+" ", len(name)+1)
			return Result{Handled: true}, true
		}
		e.Clear()
		return Result{Handled: true, Command: name}, true
	default:
		return Result{}, false
	}
	return Result{Handled: true}, true
}

// tabCompletion returns the draft that Tab makes. With several rows that share
// a prefix longer than the draft it is that prefix, so the list stays open.
// Otherwise it is the selected row; a skill row gets a space for arguments.
func (e *Editor) tabCompletion(list []CommandSpec, sel int) string {
	if len(list) > 1 {
		names := make([]string, len(list))
		for i, row := range list {
			names[i] = row.Name
		}
		if p := commonPrefix(names); utf8.RuneCountInString(p) > utf8.RuneCountInString(e.text) {
			return p
		}
	}
	name := list[sel].Name
	if isSkillRow(name) {
		return name + " "
	}
	return name
}

// commonPrefix returns the longest prefix, in runes, that all names share
// without regard to case. The result has the spelling of the first name.
func commonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	first := []rune(names[0])
	n := len(first)
	for _, name := range names[1:] {
		rs := []rune(name)
		n = min(n, len(rs))
		for i := range n {
			if unicode.ToLower(rs[i]) != unicode.ToLower(first[i]) {
				n = i
				break
			}
		}
	}
	return string(first[:n])
}

func (e *Editor) selectCompletion(i int) {
	e.comp.selected, e.comp.query = i, e.text
}

// SetSkills replaces the skill rows of the list. Each row is a name that
// starts with "/skill:". The built-in commands stay first. The selection goes
// back to the first row.
func (e *Editor) SetSkills(rows []CommandSpec) {
	e.comp.skills = rows
	e.comp.selected, e.comp.query = 0, ""
}

// MenuRows returns the rows of the command list for the layout, or nil when
// the list is closed. Rows fit the terminal width. A list of more than
// maxMenuRows rows shows a window of it that keeps the selected row in view,
// and a last dim row that counts the rows that are out of view.
func (e *Editor) MenuRows(width int, th *Theme) []string {
	list, sel := e.completionState()
	if len(list) == 0 || width < 1 {
		return nil
	}
	nameW := 0
	for _, c := range list {
		nameW = max(nameW, displayWidth(c.Name))
	}
	first, shown := 0, len(list)
	if len(list) > maxMenuRows {
		shown = maxMenuRows - 1
		first = min(max(0, sel-shown/2), len(list)-shown)
	}
	rows := make([]string, 0, shown+1)
	for i := first; i < first+shown; i++ {
		c := list[i]
		mark := "  "
		if i == sel {
			mark = GlyphUser + " "
		}
		plain := "  " + mark + padTo(c.Name, nameW) + "  " + c.Description
		plain = truncateTo(plain, width)
		if i == sel {
			rows = append(rows, th.Render(RoleSelected, padTo(plain, width)))
			continue
		}
		prefix := "  " + mark + padTo(c.Name, nameW)
		if displayWidth(plain) <= displayWidth(prefix) {
			rows = append(rows, plain)
			continue
		}
		rows = append(rows, prefix+th.Render(RoleMuted, plain[len(prefix):]))
	}
	if rest := len(list) - shown; rest > 0 {
		rows = append(rows, th.Render(RoleDim, truncateTo(fmt.Sprintf("  … %d more", rest), width)))
	}
	return rows
}
