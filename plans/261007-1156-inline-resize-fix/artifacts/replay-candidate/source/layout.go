package main

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// liveFrame measures the active slot at the final width before allocating optional rows.
func (m model) liveFrame() (string, int, int) {
	width, height := m.width, m.height
	if width <= 0 {
		width = 40
	}
	if height <= 0 {
		height = 3
	}
	var content string
	var x, y int
	if m.selector.open {
		content, x, y = m.selector.frame(width, height)
	} else {
		// First measure wrapping at the final width, then reconcile the bounded slot.
		content, x, y = editorFrame(m.text, m.cursor, width, 3)
		measuredHeight := strings.Count(content, "\n") + 1
		if measuredHeight > height {
			content, x, y = editorFrame(m.text, m.cursor, width, height)
		}
	}
	remaining := height - (strings.Count(content, "\n") + 1)
	if m.g1 && m.transcript.tailRows > 0 && remaining > 0 {
		tails := []string{fmt.Sprintf("tail %04d %s", m.transcript.confirmed, strings.Repeat(".", m.transcript.confirmed%20))}
		if m.transcript.tailRows > 1 && m.transcript.confirmed == 0 {
			tails = append(tails, "tail extra", "tail extra")
		}
		for _, tail := range tails[:min(remaining, len(tails))] {
			content += "\n" + ansi.Truncate(tail, width, "")
			remaining--
		}
	}
	if m.g6 && remaining > 0 {
		help := "Alt+Enter: newline; Enter: submit"
		if m.negotiated {
			help = "Shift/Ctrl/Alt+Enter: newline"
		}
		content += "\n" + ansi.Truncate(help, width, "")
	}
	return content, x, y
}
