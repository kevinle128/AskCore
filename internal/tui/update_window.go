package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Window size handling.
//
// A terminal reflows rows it already holds in its scrollback, so after a
// resize the committed rows can look wrong. The model waits until the size
// has stayed the same for the settle delay, then asks the renderer to clear
// the screen and scrollback and draw the committed rows again at the new
// width. The draft, cursor, focus and queue are not touched.

func (m *Model) onWindowSize(msg tea.WindowSizeMsg) tea.Cmd {
	changed := msg.Width != m.width || msg.Height != m.height
	m.width, m.height = msg.Width, msg.Height
	m.generation = msg.Generation
	if !m.sized {
		// Nothing was drawn before the first size, so nothing needs repair.
		m.sized = true
		if !m.frameReady {
			return firstFrameAfter(firstFrameDelay)
		}
		return nil
	}
	if !changed {
		return nil
	}
	m.settleGen = msg.Generation
	m.settleWaiting = true
	return settleAfter(m.settleDelay, msg.Generation)
}

func (m *Model) onSettle(msg settleMsg) {
	if msg.generation != m.settleGen {
		// A newer size started its own wait.
		return
	}
	m.settleWaiting = false
	m.rebuildWanted = true
}

func (m *Model) onReplayResult(res tea.InlineReplayResult) {
	m.replaying = false
	m.setErr(res.Err)
	if res.Stale && !m.settleWaiting {
		// The size event that made the request old did not change the size,
		// so no wait is running. Ask again with the current generation.
		m.rebuildWanted = true
	}
}
