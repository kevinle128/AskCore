package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Editor input. Handle is the one entry point for key presses and pastes.
//
// Order inside the editor follows the input order of the TUI: the completion
// list first, then the editor keys. Keys that belong to the host or to the
// application (ctrl+c, esc without a list, ctrl+d on an empty draft, alt+up,
// ctrl+l, ctrl+t, shift+tab) come back with Handled false.

// Handle processes a tea.KeyPressMsg or a tea.PasteMsg. Other messages are
// not handled.
func (e *Editor) Handle(msg tea.Msg) Result {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		e.endChains()
		e.paste(msg.Content)
		return Result{Handled: true}
	case tea.KeyPressMsg:
		return e.handleKey(msg)
	}
	return Result{}
}

// endChains ends the runs that join several keys: kill merging and the sticky
// column of vertical moves. Callers keep a chain by setting the flag again.
func (e *Editor) endChains() {
	e.lastKill = false
	e.goalSet = false
}

func (e *Editor) handleKey(k tea.KeyPressMsg) Result {
	key := k.String()
	if res, ok := e.handleCompletionKey(key); ok {
		e.endChains()
		return res
	}

	wasKill, wasGoal := e.lastKill, e.goalSet
	e.endChains()
	switch key {
	case "enter":
		return e.submit(false)
	case "alt+enter":
		return e.submit(true)
	case "shift+enter", "ctrl+j":
		e.insert(opOther, "\n")

	case "left":
		e.moveTo(e.prevBoundary(e.cursor))
	case "right":
		e.moveTo(e.nextBoundary(e.cursor))
	case "alt+b", "alt+left", "ctrl+left":
		e.moveTo(e.wordLeft(e.cursor))
	case "alt+f", "alt+right", "ctrl+right":
		e.moveTo(e.wordRight(e.cursor))
	case "home", "ctrl+a":
		e.moveTo(e.lineStart(e.cursor))
	case "end", "ctrl+e":
		e.moveTo(e.lineEnd(e.cursor))
	case "up":
		e.goalSet = wasGoal
		if !e.moveVertical(-1) {
			e.walkHistory(true)
		}
	case "down":
		e.goalSet = wasGoal
		if !e.moveVertical(1) {
			e.walkHistory(false)
		}

	case "backspace", "ctrl+h":
		e.deleteRange(opBackspace, e.prevBoundary(e.cursor), e.cursor)
	case "delete":
		e.deleteRange(opOther, e.cursor, e.nextBoundary(e.cursor))
	case "ctrl+d":
		if e.text == "" {
			return Result{} // the host detaches
		}
		e.deleteRange(opOther, e.cursor, e.nextBoundary(e.cursor))
	case "ctrl+w", "alt+backspace":
		e.killBackward(e.wordLeft(e.cursor), wasKill)
	case "ctrl+u":
		e.killBackward(e.lineStartOrJoin(), wasKill)
	case "ctrl+k":
		e.killForward(e.lineEndOrJoin(), wasKill)
	case "ctrl+y":
		if text, ok := e.kill.latest(); ok {
			e.insert(opOther, text)
		}
	case "ctrl+-", "ctrl+_":
		e.undoLast()

	default:
		if k.Text == "" || k.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			return Result{}
		}
		e.insert(opType, cleanInput(k.Text))
	}
	return Result{Handled: true}
}

// killBackward removes text before the cursor into the kill ring.
func (e *Editor) killBackward(from int, merge bool) {
	e.kill.add(e.deleteRange(opOther, from, e.cursor), true, merge)
	e.lastKill = true
}

// killForward removes text after the cursor into the kill ring.
func (e *Editor) killForward(to int, merge bool) {
	e.kill.add(e.deleteRange(opOther, e.cursor, to), false, merge)
	e.lastKill = true
}

// lineStartOrJoin is the start of the line, or the end of the previous line
// when the cursor already is at the start, so that the kill joins the lines.
func (e *Editor) lineStartOrJoin() int {
	start := e.lineStart(e.cursor)
	if start == e.cursor && start > 0 {
		return start - 1
	}
	return start
}

// lineEndOrJoin is the end of the line, or the start of the next line when the
// cursor already is at the end.
func (e *Editor) lineEndOrJoin() int {
	end := e.lineEnd(e.cursor)
	if end == e.cursor && end < len(e.text) {
		return end + 1
	}
	return end
}

// submit builds the result of enter or alt+enter. A draft that is exactly a
// slash command runs the command; a blank draft does nothing.
func (e *Editor) submit(alt bool) Result {
	if strings.TrimSpace(e.text) == "" {
		return Result{Handled: true}
	}
	if name, ok := e.comp.lookup(strings.TrimSpace(e.text)); ok {
		e.Clear()
		return Result{Handled: true, Command: name}
	}
	return Result{Handled: true, Submit: &Submit{
		Text:     strings.TrimRight(e.expand(e.text), " \t\n"),
		Revision: e.revision,
		Alt:      alt,
		display:  e.text,
	}}
}

// walkHistory shows an older or newer prompt. The cursor goes to the end so
// that a following arrow key moves through the recalled text.
func (e *Editor) walkHistory(older bool) {
	var text string
	var ok bool
	if older {
		text, ok = e.history.older(e.text)
	} else {
		text, ok = e.history.newer()
	}
	if !ok {
		return
	}
	pos := e.history.pos
	e.edit(opOther, text, len(text))
	e.history.pos = pos
}
