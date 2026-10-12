package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Window size handling.
//
// The model draws the whole screen again at the new size. The block pane keeps
// the same block at its top and its follow state, and the draft, cursor, focus
// and queue are not touched.

func (m *Model) onWindowSize(msg tea.WindowSizeMsg) {
	m.width, m.height = msg.Width, msg.Height
	m.sized = true
	m.resized = true
	// Columns and rows of a selection are only valid at the size it was made.
	m.mouse.cancel()
	m.mouse.rng = textRange{}
}
