package tui

import (
	"strings"
)

// Slash command completion.
//
// The list opens when the whole draft is a slash and the start of a command
// name, for example "/" or "/se". It closes on esc, on a space, and when no
// command matches. The command set is fixed, so the matches come from the
// draft at once and no stale result can arrive.

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
		{"/new", "Start a new session in this directory"},
		{"/sessions", "Watch another live session"},
		{"/take", "Take control of this session"},
		{"/quit", "Detach and quit"},
	}
}

// Completion is the state of the command list. The editor owns it.
type Completion struct {
	commands []CommandSpec
	selected int
	query    string // the draft that selected was chosen for

	// The user closed the list at closedRev. A later edit opens it again.
	closed    bool
	closedRev uint64
}

func newCompletion(commands []CommandSpec) Completion {
	return Completion{commands: commands}
}

// matches returns the commands for a draft, or nil when the list is closed.
func (c *Completion) matches(draft string, rev uint64) []CommandSpec {
	if len(c.commands) == 0 || !strings.HasPrefix(draft, "/") || strings.ContainsAny(draft, " \t\n") {
		return nil
	}
	if c.closed && c.closedRev == rev {
		return nil
	}
	var out []CommandSpec
	for _, cmd := range c.commands {
		if strings.HasPrefix(cmd.Name, strings.ToLower(draft)) {
			out = append(out, cmd)
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
	case "enter", "alt+enter":
		name := list[sel].Name
		e.Clear()
		return Result{Handled: true, Command: name}, true
	default:
		return Result{}, false
	}
	return Result{Handled: true}, true
}

func (e *Editor) selectCompletion(i int) {
	e.comp.selected, e.comp.query = i, e.text
}

// MenuRows returns the rows of the command list for the layout, or nil when
// the list is closed. Rows fit the terminal width.
func (e *Editor) MenuRows(width int, th *Theme) []string {
	list, sel := e.completionState()
	if len(list) == 0 || width < 1 {
		return nil
	}
	nameW := 0
	for _, c := range list {
		nameW = max(nameW, displayWidth(c.Name))
	}
	rows := make([]string, len(list))
	for i, c := range list {
		mark := "  "
		if i == sel {
			mark = GlyphUser + " "
		}
		plain := "  " + mark + padTo(c.Name, nameW) + "  " + c.Description
		plain = truncateTo(plain, width)
		if i == sel {
			rows[i] = th.Render(RoleSelected, padTo(plain, width))
			continue
		}
		prefix := "  " + mark + padTo(c.Name, nameW)
		if displayWidth(plain) <= displayWidth(prefix) {
			rows[i] = plain
			continue
		}
		rows[i] = prefix + th.Render(RoleMuted, plain[len(prefix):])
	}
	return rows
}
