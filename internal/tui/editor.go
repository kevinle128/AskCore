package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Editor component.
//
// The editor owns the draft text, the cursor, undo, the kill ring, prompt
// history, paste markers and the draft revision. It never calls the agent. A
// key that asks for a send gives the root a Submit intent; the root decides
// between send, steer and follow-up from the run state and later calls Accept.
//
// Only the root update loop may call methods that change the editor. The
// render methods (Slot, MenuRows) only read it, so View can call them.

// Submit is the intent to send the draft.
type Submit struct {
	// Text is the full text to send. Paste markers are already expanded.
	Text string
	// Revision is the draft revision when the user pressed the key. Accept
	// clears the draft only if the revision is still the same.
	Revision uint64
	// Alt is true when the user pressed alt+enter. While a run is active
	// that queues a follow-up; otherwise it is the same as enter.
	Alt bool

	// display is the draft with paste markers, as the user saw it. History
	// keeps this form so that a recalled prompt does not fill the draft.
	display string
}

// Result tells the root what a key or paste message did.
type Result struct {
	// Handled is true when the editor used the message. When it is false the
	// root continues with its own shortcuts.
	Handled bool
	// Submit is set when the user asked to send the draft.
	Submit *Submit
	// Command is the slash command to run, for example "/model". The editor
	// has already cleared the draft in that case.
	Command string
}

// defaultTextWidth is the text width before the root reports a size.
const defaultTextWidth = 76

// promptWidth is the width of the marker and of the continuation indent.
const promptWidth = 2

// Editor holds the draft.
type Editor struct {
	text     string
	cursor   int // byte offset at a grapheme boundary
	revision uint64
	textW    int // columns for text in a row, used for vertical movement

	goalCol int
	goalSet bool

	undo     undoStack
	lastOp   editOp
	lastKill bool
	kill     killRing
	history  promptHistory
	pastes   pasteStore
	comp     Completion
}

// NewEditor returns an empty editor. The commands feed the slash completion
// list; nil turns the list off.
func NewEditor(commands []CommandSpec) *Editor {
	return &Editor{
		textW: defaultTextWidth,
		comp:  newCompletion(commands),
	}
}

// Text returns the draft as the user sees it. Paste markers stay collapsed.
func (e *Editor) Text() string { return e.text }

// Cursor returns the cursor as a byte offset in Text.
func (e *Editor) Cursor() int { return e.cursor }

// Revision returns the draft revision. It grows by at least one with every
// change of the text and stays the same when only the cursor moves.
func (e *Editor) Revision() uint64 { return e.revision }

// SetWidth tells the editor the terminal width. Vertical cursor movement
// needs it to know where rows wrap. Call it when the size changes.
func (e *Editor) SetWidth(terminalWidth int) {
	e.textW = max(1, BoxInnerWidth(terminalWidth)-promptWidth)
}

// Accept tells the editor that the leader accepted a submitted prompt. It
// stores the prompt in the history and clears the draft, but only when the
// user has not edited the draft since the key press. The return value says
// whether the draft was cleared. A later undo brings the text back.
func (e *Editor) Accept(s Submit) bool {
	e.history.add(s.display)
	if s.Revision != e.revision {
		return false
	}
	e.Clear()
	return true
}

// Clear empties the draft. Undo can bring it back.
func (e *Editor) Clear() {
	if e.text == "" {
		return
	}
	e.edit(opOther, "", 0)
}

// RestoreQueued puts queued inputs in front of the current draft, one per
// line, and moves the cursor to the end.
func (e *Editor) RestoreQueued(items []string) {
	if len(items) == 0 {
		return
	}
	parts := append([]string(nil), items...)
	if e.text != "" {
		parts = append(parts, e.text)
	}
	text := strings.Join(parts, "\n")
	e.edit(opOther, text, len(text))
}

// ---- text operations ----

// edit replaces the draft and records an undo step. Edits of the same typing
// kind in a row share one undo step.
func (e *Editor) edit(op editOp, text string, cursor int) {
	if !op.coalesces() || op != e.lastOp {
		e.undo.push(snapshot{text: e.text, cursor: e.cursor})
	}
	e.text, e.cursor = text, cursor
	e.revision++
	e.lastOp = op
	e.history.leave()
}

// insert adds text at the cursor.
func (e *Editor) insert(op editOp, s string) {
	if s == "" {
		return
	}
	e.edit(op, e.text[:e.cursor]+s+e.text[e.cursor:], e.cursor+len(s))
}

// deleteRange removes text[from:to] and puts the cursor at from.
func (e *Editor) deleteRange(op editOp, from, to int) string {
	if from >= to {
		return ""
	}
	cut := e.text[from:to]
	e.edit(op, e.text[:from]+e.text[to:], from)
	return cut
}

// moveTo moves the cursor without changing the text. A move ends the current
// typing run for undo.
func (e *Editor) moveTo(pos int) {
	e.cursor = pos
	e.lastOp = opNone
}

func (e *Editor) undoLast() {
	s, ok := e.undo.pop()
	if !ok {
		return
	}
	e.text, e.cursor = s.text, s.cursor
	e.revision++
	e.lastOp = opNone
	e.history.leave()
}

// ---- boundaries ----

// prevBoundary returns the start of the grapheme before pos. A paste marker
// counts as one grapheme.
func (e *Editor) prevBoundary(pos int) int {
	if pos <= 0 {
		return 0
	}
	for _, sp := range e.markerSpans() {
		if pos > sp.start && pos <= sp.end {
			return sp.start
		}
	}
	last := 0
	rest := e.text[:pos]
	off := 0
	for rest != "" {
		g, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		last = off
		off += len(g)
		rest = tail
	}
	return last
}

// nextBoundary returns the end of the grapheme at pos.
func (e *Editor) nextBoundary(pos int) int {
	if pos >= len(e.text) {
		return len(e.text)
	}
	for _, sp := range e.markerSpans() {
		if pos >= sp.start && pos < sp.end {
			return sp.end
		}
	}
	g, _, _, _ := uniseg.FirstGraphemeClusterInString(e.text[pos:], -1)
	return pos + len(g)
}

func isSpaceRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n'
}

// wordLeft returns the start of the word before pos: it skips spaces and then
// the characters of the word.
func (e *Editor) wordLeft(pos int) int {
	for pos > 0 {
		r, n := utf8.DecodeLastRuneInString(e.text[:pos])
		if !isSpaceRune(r) {
			break
		}
		pos -= n
	}
	for pos > 0 {
		r, n := utf8.DecodeLastRuneInString(e.text[:pos])
		if isSpaceRune(r) {
			break
		}
		pos -= n
	}
	return e.snapToMarkerStart(pos)
}

// wordRight returns the end of the word after pos.
func (e *Editor) wordRight(pos int) int {
	for pos < len(e.text) {
		r, n := utf8.DecodeRuneInString(e.text[pos:])
		if !isSpaceRune(r) {
			break
		}
		pos += n
	}
	for pos < len(e.text) {
		r, n := utf8.DecodeRuneInString(e.text[pos:])
		if isSpaceRune(r) {
			break
		}
		pos += n
	}
	return e.snapToMarkerEnd(pos)
}

// lineStart and lineEnd bound the logical line (between line feeds) of pos.
func (e *Editor) lineStart(pos int) int {
	return strings.LastIndexByte(e.text[:pos], '\n') + 1
}

func (e *Editor) lineEnd(pos int) int {
	if i := strings.IndexByte(e.text[pos:], '\n'); i >= 0 {
		return pos + i
	}
	return len(e.text)
}
