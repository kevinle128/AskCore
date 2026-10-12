package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Keys of the block list.
//
// The block list has focus after the user pressed Tab in the prompt. Its keys
// select and fold blocks; the editor sees none of them. A key that the list
// does not own goes to the application shortcuts, and a printable key moves
// focus to the prompt and is typed there, so the user never loses a keystroke
// to a list that has no use for it.

// onListKey handles a key while the block list has focus.
func (s *sessionView) onListKey(k tea.KeyPressMsg) tea.Cmd {
	m := s.m
	switch k.String() {
	case "tab", "esc":
		// Esc here never interrupts the run: that needs the prompt.
		m.leaveBlockList()
	case "j", "down":
		m.moveSelection(1)
	case "k", "up":
		m.moveSelection(-1)
	case "e":
		s.foldSelected(m.pane.CycleFold)
	case "right", "l":
		s.foldSelected(m.pane.ExpandBlock)
	case "left", "h":
		s.foldSelected(m.pane.CollapseBlock)
	case "E", "shift+e":
		m.pane.ToggleAllSteps()
		m.revealSelected()
	case "enter":
		if id := m.list.selected; m.openable(id) {
			m.openViewer(id)
		}
	case "pgup", "b":
		m.pageSelection(-1)
	case "pgdown", "space", " ":
		m.pageSelection(1)
	case "home", "g":
		m.selectEdge(false)
	case "end", "G", "shift+g":
		m.selectEdge(true)
	case "shift+left", "H", "shift+h":
		m.selectTurn(-1)
	case "shift+right", "L", "shift+l":
		m.selectTurn(1)
	default:
		return s.onListOtherKey(k)
	}
	return nil
}

// onListOtherKey handles a key that the list does not own in the list focus.
func (s *sessionView) onListOtherKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+e":
		// The editor keeps ctrl+e for the end of the line in the prompt focus;
		// here it does what ctrl+t does.
		return s.toggleThinking()
	case "ctrl+d":
		// A draft that is not empty is not lost to a key press that the editor
		// would have read as "delete".
		if s.editor.Text() != "" {
			return nil
		}
	}
	if cmd, ok := s.onShortcut(k); ok {
		return cmd
	}
	if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		s.m.leaveBlockList()
		return s.onEditorResult(s.editor.Handle(k))
	}
	return nil
}

// foldSelected applies a fold command to the selected block and keeps the
// block in view: its row count changes with its fold mode.
func (s *sessionView) foldSelected(fold func(id string) error) {
	m := s.m
	if id := m.list.selected; id != "" {
		m.setErr(fold(id))
	}
	m.revealSelected()
}

// onShortcut handles the application shortcuts that work in every focus. ok is
// false for a key that is not one of them.
func (s *sessionView) onShortcut(k tea.KeyPressMsg) (cmd tea.Cmd, ok bool) {
	switch k.String() {
	case "ctrl+d":
		return s.requestQuit(), true
	case "alt+up":
		return s.dequeueCmd(), true
	case "ctrl+l":
		return s.openModelSelector(), true
	case "ctrl+t":
		return s.toggleThinking(), true
	case "shift+tab":
		return s.cycleThinking(), true
	case "ctrl+end":
		s.m.pane.ScrollToBottom()
		return nil, true
	}
	return nil, false
}

// openable reports whether Enter opens the viewer for a block: a step that can
// fold. A note or a reply has no more to show.
func (m *Model) openable(id string) bool {
	return id != "" && m.pane.Foldable(id) && m.canOpenViewer(id)
}
