package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Inline presentation.
//
// One path writes to the terminal outside the live frame: the model hands
// stable rows to the renderer with commitCmd, or asks it to draw all
// committed rows again with tea.ReplayInlineTranscript. The model allows only
// one of these at a time (see pump), so scrollback output stays in order and
// a rebuild never overlaps a commit.
//
// The renderer has no acknowledgement for Println. A block counts as
// committed when its rows are scheduled; the sequence in commitCmd keeps the
// order. Output that was committed is never taken again: a rebuild draws the
// rows from the transcript and does not move the commit frontier.

// commitRows is the number of rows that one commit writes.
//
// A write of n rows makes the renderer scroll the screen by n rows, then
// insert the rows at the top of the frame it last drew. The rows that leave
// the screen at the top must be history. Layout never lets the frame use the
// last screen row (see composeFrame), so with one row per write the row that
// leaves is never a row of the frame.
//
// A larger write would be safe only up to (screen height - rows of the frame
// that is on screen when the write runs). The model cannot know that frame:
// View hands frames to the renderer, which draws them on its own timer, and
// the update loop can take many Update calls between two draws. Rows that
// leave the tail in those calls are still on screen. A cap computed from the
// model's own frame was too high in that case and scrolled stale frame rows
// into the scrollback. One row per write does not depend on the frame.
const commitRows = 1

// renderContext returns the context for blocks at the current size.
func (m *Model) renderContext() RenderContext {
	return RenderContext{Width: m.width, Theme: m.theme}
}

// outputBusy reports whether a scheduled output has not finished.
func (m *Model) outputBusy() bool { return m.committing || m.replaying }

// pump starts the next output when the previous one is done. It runs after
// every message, so a change that arrives while output is busy is picked up
// when the busy output finishes.
func (m *Model) pump() tea.Cmd {
	if !m.sized || !m.frameReady || m.outputBusy() {
		return nil
	}
	if m.rebuildWanted {
		m.rebuildWanted = false
		m.replaying = true
		// The rebuild draws every committed row, including the ones that
		// were taken from the transcript but not written yet.
		m.outbox = nil
		entries := m.transcript.CommittedLines(m.renderContext())
		return tea.ReplayInlineTranscript(m.width, m.height, m.generation, entries)
	}
	if len(m.outbox) == 0 {
		m.outbox = m.transcript.TakeStable(m.renderContext())
		if len(m.outbox) == 0 {
			return nil
		}
	}
	n := min(len(m.outbox), commitRows)
	rows := m.outbox[:n:n]
	m.outbox = m.outbox[n:]
	m.committing = true
	return commitCmd(rows)
}

// Rebuild asks for the committed rows to be drawn again at the current size,
// for example after a change that alters how committed blocks look. It
// returns the command to run when the output path is free, or nil when the
// request waits for the running output. Call it from Update.
func (m *Model) Rebuild() tea.Cmd {
	m.rebuildWanted = true
	return m.pump()
}
