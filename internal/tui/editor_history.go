package tui

// Editor history: undo steps, the kill ring and prompt history.
//
// All three are local to the editor. They are not the durable transcript of
// the agent and are lost when the TUI exits.

const (
	maxUndoSteps   = 200
	maxKillEntries = 30
	maxHistory     = 500
)

// editOp names the kind of an edit. Typing runs and backspace runs share one
// undo step; every other edit is a step of its own.
type editOp int

const (
	opNone editOp = iota
	opType
	opBackspace
	opOther
)

func (o editOp) coalesces() bool { return o == opType || o == opBackspace }

// snapshot is the editor state that undo restores.
type snapshot struct {
	text   string
	cursor int
}

type undoStack struct{ steps []snapshot }

func (u *undoStack) push(s snapshot) {
	if len(u.steps) == maxUndoSteps {
		u.steps = u.steps[1:]
	}
	u.steps = append(u.steps, s)
}

func (u *undoStack) pop() (snapshot, bool) {
	if len(u.steps) == 0 {
		return snapshot{}, false
	}
	s := u.steps[len(u.steps)-1]
	u.steps = u.steps[:len(u.steps)-1]
	return s, true
}

// killRing keeps the text that kill keys removed. Consecutive kills join into
// one entry so that one yank brings back everything.
type killRing struct{ entries []string }

// add stores killed text. With merge set, the text joins the latest entry:
// in front of it for a backward kill, behind it for a forward kill.
func (k *killRing) add(text string, backward, merge bool) {
	if text == "" {
		return
	}
	if merge && len(k.entries) > 0 {
		last := &k.entries[len(k.entries)-1]
		if backward {
			*last = text + *last
		} else {
			*last += text
		}
		return
	}
	if len(k.entries) == maxKillEntries {
		k.entries = k.entries[1:]
	}
	k.entries = append(k.entries, text)
}

func (k *killRing) latest() (string, bool) {
	if len(k.entries) == 0 {
		return "", false
	}
	return k.entries[len(k.entries)-1], true
}

// promptHistory is the list of prompts that the leader accepted.
type promptHistory struct {
	entries []string
	pos     int    // index of the shown entry, or -1 when the draft is shown
	stash   string // the draft before the walk started
}

func (h *promptHistory) add(text string) {
	h.pos = -1
	if text == "" {
		return
	}
	if n := len(h.entries); n > 0 && h.entries[n-1] == text {
		return
	}
	if len(h.entries) == maxHistory {
		h.entries = h.entries[1:]
	}
	h.entries = append(h.entries, text)
}

// leave ends a history walk. An edit makes the shown entry a normal draft.
func (h *promptHistory) leave() { h.pos = -1 }

// older returns the next older entry. The draft is the text to bring back
// when the walk returns to it.
func (h *promptHistory) older(draft string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	switch {
	case h.pos == -1:
		h.stash = draft
		h.pos = len(h.entries) - 1
	case h.pos > 0:
		h.pos--
	}
	return h.entries[h.pos], true
}

// newer returns the next newer entry, or the saved draft after the newest.
func (h *promptHistory) newer() (string, bool) {
	if h.pos == -1 {
		return "", false
	}
	if h.pos == len(h.entries)-1 {
		h.pos = -1
		return h.stash, true
	}
	h.pos++
	return h.entries[h.pos], true
}
