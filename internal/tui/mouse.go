package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Mouse handling.
//
// The program asks the terminal for every mouse event (Model.View sets the
// mouse mode), so wheel, click, drag, release and motion without a button reach
// Update. Everything here reads the geometry of the last frame, so a position
// maps to what the user saw.
//
// A press on text can start a drag, so a click on text acts on the release, and
// only when the pointer is still on the cell of the press. A press on a part
// that cannot start a drag acts at once: the bottom marker, the way back of the
// viewer and the prompt pane.
//
// The mouse is ignored while a selector or a box is open: they take the prompt
// slot and own all input.

const (
	// wheelRows is the number of rows that one wheel notch scrolls.
	wheelRows = 3
	// dragScrollEvery is the time between two rows of scroll while a drag holds
	// the pointer on the first or the last row of the pane.
	dragScrollEvery = 50 * time.Millisecond
)

// mouseState is the state of the pointer.
type mouseState struct {
	// down is true between the press and the release of the left button.
	down         bool
	downX, downY int
	// surf is where the press started a drag, or surfNone when the press cannot
	// start one. anchor is the cell of the press and head the cell of the
	// pointer now.
	surf         textSurface
	anchor, head textPoint
	// lastX and lastY are the last position of the pointer in a drag. The
	// scroll tick uses them, because the pointer does not move while it scrolls.
	lastX, lastY int
	// rng is the selection that is shown. It stays until the next click or key.
	rng textRange
	// edge is -1 or 1 while the pointer is on the first or the last row of the
	// surface in a drag, and 0 otherwise. tickGen numbers the scroll ticks, so a
	// tick of an ended drag does nothing.
	edge     int
	tickGen  uint64
	tickLive bool
}

// cancel ends a drag that is in progress. The shown selection stays.
func (ms *mouseState) cancel() {
	ms.down, ms.edge, ms.tickLive = false, 0, false
	ms.tickGen++
}

// dragTickMsg scrolls the surface of a drag one row.
type dragTickMsg struct{ gen uint64 }

// inPane reports whether a screen row is a row of the block pane.
func (m *Model) inPane(y int) bool {
	return y >= m.geo.PaneTop && y < m.geo.PaneTop+m.geo.PaneRows
}

// inPrompt reports whether a screen row shows the editor.
func (m *Model) inPrompt(y int) bool {
	return y >= m.geo.PromptTop && y < m.geo.PromptTop+m.geo.PromptRows
}

// viewerBodyTop is the screen row of the first body row of the viewer.
func (m *Model) viewerBodyTop() int {
	if m.viewerChrome() {
		return 2
	}
	return 0
}

// viewerBodyAt returns the index of the body row that a screen row shows.
func (m *Model) viewerBodyAt(y int) (idx int, ok bool) {
	r := y - m.viewerBodyTop()
	total := len(m.viewerBody())
	idx = m.viewerTop(total) + r
	return idx, r >= 0 && r < m.viewerWindow() && idx < total
}

// onMouse routes a mouse message.
func (s *sessionView) onMouse(msg tea.MouseMsg) tea.Cmd {
	ms := &s.m.mouse
	if s.boxOpen() || s.sel != nil {
		ms.cancel()
		return nil
	}
	mo := msg.Mouse()
	switch msg.(type) {
	case tea.MouseWheelMsg:
		s.onWheel(mo)
	case tea.MouseClickMsg:
		if mo.Button == tea.MouseLeft {
			return s.onPress(mo.X, mo.Y)
		}
	case tea.MouseReleaseMsg:
		// A terminal that cannot tell which button went up reports none.
		if mo.Button == tea.MouseLeft || mo.Button == tea.MouseNone {
			return s.onRelease(mo.X, mo.Y)
		}
	case tea.MouseMotionMsg:
		switch mo.Button {
		case tea.MouseLeft:
			return s.onDrag(mo.X, mo.Y)
		case tea.MouseNone:
			// Motion with no button while a drag is down means that the release
			// was lost. The drag ends, so that its edge scroll stops.
			if ms.down {
				ms.cancel()
			}
			s.onHover(mo.Y)
		}
	}
	return nil
}

// onWheel scrolls the viewer, or the block pane when the pointer is over it.
func (s *sessionView) onWheel(mo tea.Mouse) {
	var dir int
	switch mo.Button {
	case tea.MouseWheelUp:
		dir = -1
	case tea.MouseWheelDown:
		dir = 1
	default:
		return
	}
	m := s.m
	switch {
	case m.viewerOpen():
		m.scrollViewer(dir * wheelRows)
	case m.inPane(mo.Y):
		m.pane.ScrollBy(dir * wheelRows)
	}
}

// onHover marks the block under the pointer. Only a block that can fold shows
// the indicator. A hover is a repaint: it changes no selection.
func (s *sessionView) onHover(y int) {
	m := s.m
	id := ""
	if !m.viewerOpen() && m.inPane(y) {
		if b, _, ok := m.pane.BlockAt(y - m.geo.PaneTop); ok {
			if e := m.transcript.entries[b]; m.pane.Foldable(e.id) {
				id = e.id
			}
		}
	}
	m.list.hover = id
}

// onPress starts a click or a drag.
func (s *sessionView) onPress(x, y int) tea.Cmd {
	m := s.m
	ms := &m.mouse
	ms.cancel()
	ms.down, ms.downX, ms.downY, ms.lastX, ms.lastY = true, x, y, x, y
	ms.surf, ms.rng = surfNone, textRange{}

	if m.viewerOpen() {
		switch idx, ok := m.viewerBodyAt(y); {
		case y == 0 && m.viewerChrome() && x <= 1+displayWidth(viewerBack):
			m.closeViewer()
			ms.down = false
		case ok:
			ms.surf, ms.anchor = surfViewer, textPoint{row: idx, col: x}
			ms.head = ms.anchor
		}
		return nil
	}
	switch {
	case m.inPane(y):
		if row, ok := m.pane.MarkerRow(); ok && y-m.geo.PaneTop == row {
			m.pane.ScrollToBottom()
			ms.down = false
		} else if b, off, found := m.pane.BlockAt(y - m.geo.PaneTop); found {
			ms.surf, ms.anchor = surfPane, textPoint{block: b, row: off, col: x}
			ms.head = ms.anchor
		}
	case m.inPrompt(y):
		s.clickPrompt(x, y)
		ms.down = false
	}
	return nil
}

// clickPrompt gives the prompt the focus and puts the cursor at the cell.
func (s *sessionView) clickPrompt(x, y int) {
	m := s.m
	if m.focus == FocusBlocks {
		m.leaveBlockList()
	}
	s.editor.MoveCursorTo(m.geo.PromptStart+y-m.geo.PromptTop, x-promptLeftPad-promptWidth)
}

// onDrag extends the selection to the pointer, and arms the scroll at the edge
// of the surface.
func (s *sessionView) onDrag(x, y int) tea.Cmd {
	m := s.m
	ms := &m.mouse
	if !ms.down || ms.surf == surfNone {
		return nil
	}
	ms.lastX, ms.lastY = x, y
	m.dragTo(x, y)
	ms.edge = m.edgeAt(y)
	if ms.edge == 0 || ms.tickLive {
		return nil
	}
	ms.tickLive = true
	return s.dragTick()
}

func (s *sessionView) dragTick() tea.Cmd {
	gen := s.m.mouse.tickGen
	return tea.Tick(dragScrollEvery, func(time.Time) tea.Msg { return dragTickMsg{gen: gen} })
}

// onDragTick scrolls the surface of a drag one row toward the edge that the
// pointer holds.
func (s *sessionView) onDragTick(msg dragTickMsg) tea.Cmd {
	m := s.m
	ms := &m.mouse
	if s.boxOpen() || s.sel != nil {
		// A box or a selector owns the input: no release can reach the drag.
		ms.cancel()
		return nil
	}
	if msg.gen != ms.tickGen || !ms.down || ms.edge == 0 {
		if msg.gen == ms.tickGen {
			ms.tickLive = false
		}
		return nil
	}
	switch ms.surf {
	case surfPane:
		m.pane.ScrollBy(ms.edge)
	case surfViewer:
		m.scrollViewer(ms.edge)
	}
	m.dragTo(ms.lastX, ms.lastY)
	return s.dragTick()
}

// edgeAt returns -1 for the first row of the surface that a drag runs in, 1 for
// the last row, and 0 for the others. A pointer outside the surface counts as
// on the edge that it is beyond.
func (m *Model) edgeAt(y int) int {
	top, rows := m.geo.PaneTop, m.geo.PaneRows
	if m.mouse.surf == surfViewer {
		top, rows = m.viewerBodyTop(), m.viewerWindow()
	}
	switch {
	case y <= top:
		return -1
	case y >= top+rows-1:
		return 1
	}
	return 0
}

// dragTo moves the head of the selection to the cell under a screen position.
// A head on the anchor cell is no selection.
func (m *Model) dragTo(x, y int) {
	ms := &m.mouse
	ms.head = m.headAt(ms.surf, x, y)
	ms.rng = textRange{}
	if ms.head != ms.anchor {
		ms.rng = orderedRange(ms.surf, ms.anchor, ms.head)
	}
}

// headAt returns the cell of a drag for a screen position. A position above
// the surface gives the start of its first row; a position below its content
// gives the end of its last row.
func (m *Model) headAt(surf textSurface, x, y int) textPoint {
	x = max(x, 0)
	switch surf {
	case surfPane:
		row := y - m.geo.PaneTop
		if row < 0 {
			if b, off, ok := m.pane.BlockAt(0); ok {
				return textPoint{block: b, row: off}
			}
			return m.mouse.anchor
		}
		for r := min(row, m.geo.PaneRows-1); r >= 0; r-- {
			if b, off, ok := m.pane.BlockAt(r); ok {
				col := x
				if r != row {
					col = colEnd
				}
				return textPoint{block: b, row: off, col: col}
			}
		}
	case surfViewer:
		total := len(m.viewerBody())
		if total == 0 {
			return m.mouse.anchor
		}
		r := y - m.viewerBodyTop()
		col := x
		switch {
		case r < 0:
			r, col = 0, 0
		case r >= m.viewerWindow():
			r, col = m.viewerWindow()-1, colEnd
		}
		idx := m.viewerTop(total) + r
		if idx >= total {
			idx, col = total-1, colEnd
		}
		return textPoint{row: idx, col: col}
	}
	return m.mouse.anchor
}

// onRelease ends a click or a drag. A release on the cell of the press is a
// click; a release elsewhere copies the selected text.
func (s *sessionView) onRelease(x, y int) tea.Cmd {
	m := s.m
	ms := &m.mouse
	if !ms.down {
		return nil
	}
	surf := ms.surf
	if surf != surfNone && (x != ms.lastX || y != ms.lastY) {
		m.dragTo(x, y)
	}
	ms.cancel()
	if surf == surfNone {
		return nil
	}
	if ms.head == ms.anchor {
		ms.rng = textRange{}
		s.clickContent()
		return nil
	}
	return s.copySelection(m.rangeText(ms.rng))
}

// copySelection sends selected text to the clipboard with the path and the
// notices of /copy.
func (s *sessionView) copySelection(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		s.setNotice("Nothing to copy", false)
		return nil
	}
	return s.copyTextCmd(text)
}

// clickContent selects the block that a click on the pane landed on and gives
// the block list the focus. A click on the first row of a step opens the
// viewer in the same action, as Enter does.
func (s *sessionView) clickContent() {
	m := s.m
	anchor := m.mouse.anchor
	if m.mouse.surf != surfPane || anchor.block >= len(m.transcript.entries) || !m.selectable(anchor.block) {
		return
	}
	if len(s.editor.MenuRows(max(1, m.width), m.theme)) != 0 {
		// The command list is drawn above the prompt and would stay while the keys
		// drove the blocks.
		return
	}
	first, _ := m.unitBounds(anchor.block)
	m.list.selected = m.transcript.entries[first].id
	m.SetFocus(FocusBlocks)
	if id := m.transcript.entries[anchor.block].id; anchor.row == 0 && m.openable(id) {
		m.openViewer(id)
	}
}
