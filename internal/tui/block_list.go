package tui

// Block list.
//
// The block list is the selection layer over the block pane. The user selects
// one unit at a time. A unit is what the user sees as one block: a prompt, a
// step, a note, or a whole reply. A reply is drawn by several transcript blocks
// (the paragraphs that a stream closed one after the other, and the usage
// line), and they are selected, marked and revealed together.

// blockList is the selection state of the block pane.
type blockList struct {
	// selected is the id of the first block of the selected unit, or empty.
	selected string
	// hover is the id of the block under the mouse pointer, or empty. The mouse
	// handling sets it; the pane shows the expand indicator for it.
	hover string
}

// joinsPrevious reports whether a block belongs to the unit of the block before
// it: a continuation paragraph of a reply, or the usage line of a reply.
func joinsPrevious(prev, cur Block) bool {
	if _, ok := prev.(assistantTextBlock); !ok {
		return false
	}
	switch c := cur.(type) {
	case assistantTextBlock:
		return !c.first
	case metaLineBlock:
		return true
	}
	return false
}

// selectable reports whether the user can select the unit that starts at block
// i. A block that draws no rows, and the empty spacer between blocks, have no
// content to select.
func (m *Model) selectable(i int) bool {
	_, spacer := m.transcript.entries[i].block.(spacerBlock)
	return !spacer && m.pane.RowsOf(i) > 0
}

// unitBounds returns the first and the last block of the unit that holds block
// i.
func (m *Model) unitBounds(i int) (first, last int) {
	es := m.transcript.entries
	first, last = i, i
	for first > 0 && joinsPrevious(es[first-1].block, es[first].block) {
		first--
	}
	for last+1 < len(es) && joinsPrevious(es[last].block, es[last+1].block) {
		last++
	}
	return first, last
}

// unitFrom returns the first block of the nearest selectable unit that holds or
// follows block i (dir > 0), or that holds or precedes it (dir < 0). ok is
// false when there is none.
func (m *Model) unitFrom(i, dir int) (first int, ok bool) {
	n := len(m.transcript.entries)
	for ; i >= 0 && i < n; i += dir {
		if m.selectable(i) {
			first, _ = m.unitBounds(i)
			return first, true
		}
	}
	return 0, false
}

// selectedUnit returns the blocks of the selected unit. ok is false when
// nothing is selected.
func (m *Model) selectedUnit() (first, last int, ok bool) {
	i, found := m.transcript.index[m.list.selected]
	if !found {
		return 0, 0, false
	}
	first, last = m.unitBounds(i)
	return first, last, true
}

// listActive reports whether the block list has focus and a selection.
func (m *Model) listActive() bool {
	if m.focus != FocusBlocks {
		return false
	}
	_, _, ok := m.selectedUnit()
	return ok
}

// enterBlockList moves focus to the block list with the last unit selected. It
// does nothing when the transcript has nothing to select.
func (m *Model) enterBlockList() {
	first, ok := m.unitFrom(len(m.transcript.entries)-1, -1)
	if !ok {
		return
	}
	m.list.selected = m.transcript.entries[first].id
	m.SetFocus(FocusBlocks)
	m.revealSelected()
}

// leaveBlockList gives focus back to the prompt and drops the selection.
func (m *Model) leaveBlockList() {
	m.list.selected = ""
	m.SetFocus(FocusEditor)
}

// selectUnit selects the unit that starts at block first and keeps it in view.
func (m *Model) selectUnit(first int) {
	m.list.selected = m.transcript.entries[first].id
	m.revealSelected()
}

// revealSelected moves the pane by the fewest rows that show the selected unit:
// all of it when it fits, its first rows when it is taller than the pane. It
// does nothing while the block list has no focus.
func (m *Model) revealSelected() {
	if m.focus != FocusBlocks {
		return
	}
	first, last, ok := m.selectedUnit()
	if !ok {
		return
	}
	m.pane.Reveal(last)
	m.pane.Reveal(first)
}

// moveSelection selects the next unit (dir > 0) or the previous one.
func (m *Model) moveSelection(dir int) {
	first, last, ok := m.selectedUnit()
	if !ok {
		return
	}
	from := first - 1
	if dir > 0 {
		from = last + 1
	}
	if next, found := m.unitFrom(from, dir); found {
		m.selectUnit(next)
	}
}

// selectEdge selects the first or the last unit. The last one also turns
// follow on: the pane shows the end of the transcript.
func (m *Model) selectEdge(last bool) {
	if last {
		if first, ok := m.unitFrom(len(m.transcript.entries)-1, -1); ok {
			m.list.selected = m.transcript.entries[first].id
		}
		m.pane.ScrollToBottom()
		return
	}
	if first, ok := m.unitFrom(0, 1); ok {
		m.list.selected = m.transcript.entries[first].id
	}
	m.pane.ScrollToTop()
}

// selectTurn selects the previous or the next unit that is a user prompt.
func (m *Model) selectTurn(dir int) {
	first, last, ok := m.selectedUnit()
	if !ok {
		return
	}
	es := m.transcript.entries
	i := first - 1
	if dir > 0 {
		i = last + 1
	}
	for ; i >= 0 && i < len(es); i += dir {
		if _, prompt := es[i].block.(userPromptBlock); prompt {
			m.selectUnit(i)
			return
		}
	}
}

// pageSelection scrolls the pane by one page. A selection that the page moved
// out of view moves to the unit at the edge that the page came from.
func (m *Model) pageSelection(dir int) {
	m.pane.ScrollPage(dir)
	f := m.pane.Frame()
	first, last, ok := m.selectedUnit()
	if !ok || f.FirstBlock < 0 || (last >= f.FirstBlock && first <= f.LastBlock) {
		return
	}
	edge := f.FirstBlock
	if dir < 0 {
		edge = f.LastBlock
	}
	if next, found := m.unitFrom(edge, dir); found {
		m.list.selected = m.transcript.entries[next].id
	}
}

// syncMarks tells the pane which blocks are selected and hovered. The marks
// show only while the block list has focus; the selection of the list is the
// one the user moved there with.
func (m *Model) syncMarks() {
	if !m.listActive() {
		m.pane.SetSelected()
	} else {
		first, last, _ := m.selectedUnit()
		ids := make([]string, 0, last-first+1)
		for i := first; i <= last; i++ {
			ids = append(ids, m.transcript.entries[i].id)
		}
		m.pane.SetSelected(ids...)
	}
	m.pane.SetHover(m.list.hover)
}

// selectedBlock returns the id of the selected block, or empty when the block
// list has no selection.
func (m *Model) selectedBlock() string {
	if !m.listActive() {
		return ""
	}
	return m.list.selected
}
