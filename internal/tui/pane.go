package tui

import (
	"fmt"
	"time"
)

// Block pane.
//
// The pane shows the blocks of a Transcript in a window of fixed height. It is
// virtual: it draws only the blocks that meet the window, and it keeps for each
// block the rows that the block drew, so that a frame draws a block again only
// when its content, its drawing state, its fold mode, the width or the theme
// changed.
//
// The window top is an anchor, not a row number: a block index and a row inside
// that block. A block above the window that grows or folds does not move the
// text at the top, and a resize keeps the same block at the top. The pane
// never needs the row count of the whole session, so a wheel step or a delta
// in the last block touches only the blocks near the window.
//
// Follow mode keeps the window at the end of the transcript. A scroll up turns
// it off and new output then does not move the window. While follow is off the
// last pane row belongs to the bottom marker, and the window shows height-1
// rows of content.

// drawStater is implemented by blocks whose drawing depends on state that is
// not in the block, such as the spinner frame or the thinking form. The value
// changes when the drawing changes.
type drawStater interface{ drawState() uint64 }

// Marker texts of the bottom marker. The short form is used under
// paneShortMarkerWidth columns.
const (
	paneMarkerEnd      = "▼ End"
	paneMarkerNew      = "▼ %d new lines · End"
	paneMarkerNewShort = "▼ %d · End"

	paneShortMarkerWidth = 30

	// paneSettleBudget is the time that one frame may spend to draw blocks
	// below the window only for the count N of the marker. A frame that needs
	// more time counts the rest of the blocks with their last known row counts
	// and sets PaneMarker.Approx; the next frames draw the rest.
	paneSettleBudget = 4 * time.Millisecond
)

// PaneMarker is the data of the bottom marker. The pane draws no marker: the
// caller styles Text and draws it on the last row at column Pad, in the tool
// role when New is false and in the accent role when it is true.
type PaneMarker struct {
	// Show is true while follow is off.
	Show bool
	// New is true when output came while the window was away from the end.
	New bool
	// N is the number of pane rows below the window. It is set only when New
	// is true, because the form "▼ End" does not show it.
	N int
	// Approx is true when N counts some blocks below the window with a row
	// count from before a resize or a fold change. The caller should draw
	// another frame soon: each frame settles more blocks.
	Approx bool
	Text   string
	// Pad is the number of spaces before Text that center it in the pane.
	Pad int
}

// PaneFrame is what the pane shows after one draw.
type PaneFrame struct {
	// Rows has one entry for each pane row and no row wider than the pane. A
	// row that holds no content is empty. The selection mark of a block is part
	// of its rows; the pane adds no other style.
	// When Marker.Show is true the last row is empty: it is the marker slot.
	Rows []string
	// FirstBlock and LastBlock are the indexes of the first and the last block
	// that have a row in the window, or -1 when the transcript has no rows.
	FirstBlock, LastBlock int
	Marker                PaneMarker
}

// paneItem is the state of one block of the transcript. Items are in the order
// of the transcript entries and are never removed.
type paneItem struct {
	id   string
	fold FoldMode
	// userSet is true while the user holds a mode above the lowest mode of the
	// block. The pane then keeps it when the block changes; before that, and
	// after the user folds the block back to its lowest mode, it asks the block
	// for its default.
	userSet bool

	// mark is the selection and hover state of the block. A block draws again
	// when its mark changes, so a move of the selection touches two blocks.
	mark itemMark

	// seen is the content version that the last frame saw. It tells new output.
	seen uint64

	// The drawing and what it was drawn from. h is the row count of the
	// drawing; the rows of the drawing are valid only when valid() is true.
	drawn     bool
	rows      []string
	h         int
	version   uint64
	state     uint64
	width     int
	theme     *Theme
	drawnFold FoldMode
	drawnMark itemMark
}

// itemMark is what the pane tells a block about the user's pointer and
// selection. skipHead and skipTail say that the first or the last block of a
// selected group leaves its outer empty rows without a mark.
type itemMark struct {
	selected, hover    bool
	skipHead, skipTail bool
}

// Pane is a virtualized view of a transcript. It is not safe for concurrent
// use: the model owns it and calls it from the update loop.
type Pane struct {
	tr            *Transcript
	items         []*paneItem
	width, height int
	theme         *Theme

	// DefaultFold overrides the default mode of a block, which is what the block
	// itself reports. A nil function uses the block.
	DefaultFold func(id string, b Block) FoldMode

	// follow is true while the window shows the end of the transcript.
	follow bool
	// keepScroll preserves free scroll after a resize until a scroll action or
	// a content shrink makes the end visible.
	keepScroll bool
	// ai and ar are the anchor: the block and the row inside it at the top of
	// the window. They are used while follow is off.
	ai, ar int
	// newOutput is true when the transcript changed below the window top while
	// follow was off.
	newOutput bool

	// selIdx and hoverIdx are the items that carry a mark now.
	selIdx   []int
	hoverIdx int
	hasHover bool

	// draws counts block drawings, for tests and measurements.
	draws int
}

// NewPane returns a pane over a transcript. Set the size and the theme before
// the first frame.
func NewPane(t *Transcript) *Pane { return &Pane{tr: t, follow: true} }

// SetSize sets the pane size. A new width draws the blocks again, lazily: a
// block draws when the window or a scroll meets it. A new height needs no
// drawing.
func (p *Pane) SetSize(width, height int) {
	width, height = max(0, width), max(0, height)
	if !p.follow && (width != p.width || height != p.height) {
		p.keepScroll = true
	}
	p.width, p.height = width, height
}

// SetTheme sets the theme. Blocks draw again with it when they are met.
func (p *Pane) SetTheme(th *Theme) { p.theme = th }

// Following reports whether the window stays at the end of the transcript.
func (p *Pane) Following() bool { return p.follow }

// Draws returns the number of block drawings so far.
func (p *Pane) Draws() int { return p.draws }

// Fold returns the fold mode of a block. An unknown block gives FoldExpanded.
func (p *Pane) Fold(id string) FoldMode {
	p.sync()
	it, err := p.itemByID(id)
	if err != nil {
		return FoldExpanded
	}
	return it.fold
}

func (p *Pane) itemByID(id string) (*paneItem, error) {
	p.sync()
	i, ok := p.tr.index[id]
	if !ok || i >= len(p.items) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBlock, id)
	}
	return p.items[i], nil
}

// sync adds items for new blocks and notes content changes. It costs one
// compare for each block.
func (p *Pane) sync() {
	es := p.tr.entries
	old := len(p.items)
	for i, it := range p.items {
		if v := es[i].version; it.seen != v {
			it.seen = v
			if !it.userSet {
				it.fold = p.defaultFold(es[i])
			}
			if !p.follow && i >= p.ai {
				p.newOutput = true
			}
		}
	}
	for i := old; i < len(es); i++ {
		e := es[i]
		p.items = append(p.items, &paneItem{id: e.id, fold: p.defaultFold(e), seen: e.version, h: 1})
		if !p.follow {
			p.newOutput = true
		}
	}
}

// defaultFold returns the mode that an entry has before the user sets one.
func (p *Pane) defaultFold(e *entry) FoldMode {
	if p.DefaultFold != nil {
		return p.DefaultFold(e.id, e.block)
	}
	return blockDefaultFold(e.block)
}

// current returns the drawing state of block i and whether its drawing is
// still valid for it.
func (p *Pane) current(i int) (state uint64, ok bool) {
	it, e := p.items[i], p.tr.entries[i]
	if ds, isState := e.block.(drawStater); isState {
		state = ds.drawState()
	}
	ok = it.drawn && it.version == e.version && it.state == state &&
		it.width == p.width && it.theme == p.theme && it.drawnFold == it.fold && it.drawnMark == it.mark
	return state, ok
}

// measure draws block i when its drawing is not valid, and returns its row
// count.
func (p *Pane) measure(i int) int {
	it, e := p.items[i], p.tr.entries[i]
	if state, ok := p.current(i); !ok {
		rc := RenderContext{Width: p.width, Theme: p.theme, Fold: it.fold, Pointed: it.mark.selected || it.mark.hover}
		it.rows = paintSelection(rc, e.block, it.mark, renderRows(e.block, rc))
		if it.drawn && it.version != e.version && it.width == p.width && it.drawnFold == it.fold && len(it.rows) < it.h {
			p.keepScroll = false
		}
		it.h = len(it.rows)
		it.drawn, it.version, it.state = true, e.version, state
		it.width, it.theme, it.drawnFold, it.drawnMark = p.width, p.theme, it.fold, it.mark
		p.draws++
	}
	return it.h
}

// viewRows is the number of content rows in the window. While follow is off
// the last pane row is the marker.
func (p *Pane) viewRows(follow bool) int {
	if follow || p.height < 2 {
		return p.height
	}
	return p.height - 1
}

// anchorFromEnd returns the anchor of the window of rows content rows that ends
// with the last row of the transcript. It draws only the blocks of that
// window. A transcript shorter than the window gives the first row.
func (p *Pane) anchorFromEnd(rows int) (ai, ar int) {
	filled := 0
	for i := len(p.items) - 1; i >= 0; i-- {
		h := p.measure(i)
		if filled+h >= rows {
			return i, filled + h - rows
		}
		filled += h
	}
	return 0, 0
}

// normalize puts the anchor on a row that exists: a row of a block that has
// rows. It returns false when the anchor is past the last row.
func (p *Pane) normalize() bool {
	for p.ai < len(p.items) {
		h := p.measure(p.ai)
		if h == 0 {
			p.ai, p.ar = p.ai+1, 0
			continue
		}
		p.ar = min(max(p.ar, 0), h-1)
		return true
	}
	return false
}

// windowTop returns the anchor of the window as it is now. With follow on it
// is the window of the end.
func (p *Pane) windowTop() (ai, ar int) {
	if p.follow {
		return p.anchorFromEnd(p.height)
	}
	if !p.normalize() {
		return p.anchorFromEnd(p.viewRows(false))
	}
	return p.ai, p.ar
}

// setTop moves the window to an anchor. The window follows the end when it
// reaches the last row: that is the first anchor from which the content rows
// end with the last row of the transcript.
func (p *Pane) setTop(ai, ar int) {
	ei, er := p.anchorFromEnd(p.viewRows(false))
	if ai > ei || (ai == ei && ar >= er) {
		p.follow, p.newOutput = true, false
		return
	}
	p.follow, p.ai, p.ar = false, ai, ar
}

// ScrollBy moves the window by rows: a negative number scrolls up. A move that
// ends at the last row turns follow on; any other move turns it off. The
// window never leaves the transcript.
func (p *Pane) ScrollBy(rows int) {
	p.sync()
	if rows == 0 || p.height < 1 || p.width < 1 {
		return
	}
	p.keepScroll = false
	ai, ar := p.windowTop()
	if rows < 0 {
		r := ar + rows
		for r < 0 && ai > 0 {
			ai--
			r += p.measure(ai)
		}
		p.setTop(ai, max(r, 0))
		return
	}
	// A move down stops at the end anchor, so that a long move draws only the
	// blocks of the window.
	ei, _ := p.anchorFromEnd(p.viewRows(false))
	r := ar + rows
	for ai <= ei {
		h := p.measure(ai)
		if r < h {
			break
		}
		r -= h
		ai++
	}
	p.setTop(ai, r)
}

// RowsOf returns the number of rows that block i draws at the pane width. A
// block that draws no rows, such as a placeholder, gives zero. Before the pane
// has a width every block counts as one row.
func (p *Pane) RowsOf(i int) int {
	p.sync()
	if i < 0 || i >= len(p.items) {
		return 0
	}
	if p.width < 1 {
		return 1
	}
	return p.measure(i)
}

// ScrollPage moves the window by one page: the rows of the window less one, so
// that the row at the edge stays in view. A negative dir scrolls up.
func (p *Pane) ScrollPage(dir int) {
	p.ScrollBy(dir * max(1, p.viewRows(p.follow)-1))
}

// ScrollToTop puts the first row of the transcript at the top of the window.
func (p *Pane) ScrollToTop() {
	p.sync()
	if p.height < 1 || p.width < 1 {
		return
	}
	p.keepScroll = false
	p.setTop(0, 0)
}

// ScrollToBottom shows the end of the transcript and turns follow on.
func (p *Pane) ScrollToBottom() {
	p.sync()
	p.follow, p.newOutput, p.keepScroll = true, false, false
}

// Reveal moves the window by the fewest rows that show block i. A block above
// the window goes to the top row. A block that is taller than the window shows
// its first rows. A block that is already in view does not move the window.
func (p *Pane) Reveal(i int) {
	p.sync()
	if i < 0 || i >= len(p.items) || p.height < 1 || p.width < 1 {
		return
	}
	ai, ar := p.windowTop()
	if i < ai || (i == ai && ar > 0 && p.measure(i) > 0) {
		p.setTop(i, 0)
		return
	}
	// s is the row of the block start, counted from the window top.
	view := p.viewRows(p.follow)
	s := -ar
	for j := ai; j < i; j++ {
		s += p.measure(j)
	}
	h := p.measure(i)
	switch {
	case s < view && s+h <= view:
		// The block is in view.
	case h >= view:
		p.ScrollBy(s)
	default:
		p.ScrollBy(s + h - view)
	}
}

// window returns the anchor of the window that a frame draws now and the number
// of content rows in it. Content that grew or folded may have put the end
// inside the window: the window then follows the end.
func (p *Pane) window() (ai, ar, view int) {
	ai, ar = p.windowTop()
	if !p.follow && !p.keepScroll {
		p.setTop(ai, ar)
		if p.follow {
			ai, ar = p.anchorFromEnd(p.height)
		}
	}
	return ai, ar, p.viewRows(p.follow)
}

// BlockAt returns the block that a pane row shows and the row inside that
// block, counted from the first row of the block. The row counts from the top
// of the pane. ok is false for a row that holds no content: a row below the
// last block, or the row of the bottom marker. It answers for the window that
// the next frame draws, so a click that follows a scroll finds what the user
// sees.
func (p *Pane) BlockAt(row int) (block, offset int, ok bool) {
	p.sync()
	if row < 0 || p.height < 1 || p.width < 1 || len(p.items) == 0 {
		return 0, 0, false
	}
	ai, ar, view := p.window()
	if row >= view {
		return 0, 0, false
	}
	r := row + ar
	for i := ai; i < len(p.items); i++ {
		h := p.measure(i)
		if r < h {
			return i, r, true
		}
		r -= h
	}
	return 0, 0, false
}

// MarkerRow returns the pane row of the bottom marker. ok is false while
// follow is on, and when the pane has no room for a marker row.
func (p *Pane) MarkerRow() (row int, ok bool) {
	p.sync()
	if p.height < 2 || p.width < 1 || len(p.items) == 0 {
		return 0, false
	}
	p.window()
	return p.height - 1, !p.follow
}

// NewOutput reports whether output came below the window while follow is off.
func (p *Pane) NewOutput() bool {
	p.sync()
	return !p.follow && p.newOutput
}

// Frame draws the pane. It returns exactly height rows.
func (p *Pane) Frame() PaneFrame {
	p.sync()
	f := PaneFrame{Rows: make([]string, p.height), FirstBlock: -1, LastBlock: -1}
	if p.height < 1 || p.width < 1 || len(p.items) == 0 {
		return f
	}
	ai, ar, view := p.window()
	out := f.Rows[:0]
	i, row := ai, ar
	for ; i < len(p.items) && len(out) < view; i++ {
		p.measure(i)
		it := p.items[i]
		if f.FirstBlock < 0 && row < len(it.rows) {
			f.FirstBlock = i
		}
		take := min(view-len(out), len(it.rows)-row)
		if take > 0 {
			out = append(out, it.rows[row:row+take]...)
			f.LastBlock = i
		}
		if row+take < len(it.rows) {
			// The window ends inside this block.
			row += take
			break
		}
		row = 0
	}
	if p.follow {
		return f
	}
	f.Marker = p.marker(i, row)
	return f
}

// marker returns the bottom marker for a window that ends before row row of
// block i.
func (p *Pane) marker(i, row int) PaneMarker {
	m := PaneMarker{Show: true, New: p.newOutput}
	switch {
	case !m.New:
		m.Text = paneMarkerEnd
	default:
		// Block i may not have drawn at the current size: the window can end
		// exactly at the end of the block before it. It counts like the blocks
		// after it, under the same time budget.
		deadline := time.Now().Add(paneSettleBudget)
		for j := i; j < len(p.items); j++ {
			if _, ok := p.current(j); !ok && time.Now().After(deadline) {
				m.N += p.items[j].h
				m.Approx = true
				continue
			}
			m.N += p.measure(j)
		}
		m.N -= row
		if p.width < paneShortMarkerWidth {
			m.Text = fmt.Sprintf(paneMarkerNewShort, m.N)
		} else {
			m.Text = fmt.Sprintf(paneMarkerNew, m.N)
		}
	}
	m.Pad = max(0, (p.width-displayWidth(m.Text))/2)
	return m
}

// setFold sets a mode as the user's choice. A block that goes back to its
// lowest mode is no longer held by the user, so that it folds again by itself
// when its content ends, as a thinking block does.
func (it *paneItem) setFold(m FoldMode, fb foldBlock) {
	it.fold = m
	it.userSet = m != fb.collapseFold()
}

// --- selection marks ---

// SetSelected marks the blocks of the user's selection, in transcript order:
// one block, or the blocks of one reply. An empty list clears the selection.
// Only the blocks whose mark changed draw again. An unknown id is ignored.
func (p *Pane) SetSelected(ids ...string) {
	p.sync()
	for _, i := range p.selIdx {
		p.items[i].mark.selected, p.items[i].mark.skipHead, p.items[i].mark.skipTail = false, false, false
	}
	p.selIdx = p.selIdx[:0]
	for _, id := range ids {
		if i, ok := p.tr.index[id]; ok && i < len(p.items) {
			p.selIdx = append(p.selIdx, i)
		}
	}
	for k, i := range p.selIdx {
		m := &p.items[i].mark
		m.selected, m.skipHead, m.skipTail = true, k == 0, k == len(p.selIdx)-1
	}
}

// SetHover marks the block under the mouse pointer. An empty or unknown id
// clears it.
func (p *Pane) SetHover(id string) {
	p.sync()
	if p.hasHover {
		p.items[p.hoverIdx].mark.hover = false
		p.hasHover = false
	}
	if i, ok := p.tr.index[id]; ok && i < len(p.items) {
		p.items[i].mark.hover = true
		p.hoverIdx, p.hasHover = i, true
	}
}

// --- fold commands ---
//
// The commands below change the mode of blocks as the user's choice. A block
// that cannot fold at the current width is left as it is.

// foldBlockOf returns the item and the fold interface of a foldable block. ok
// is false for a block that cannot fold.
func (p *Pane) foldBlockOf(id string) (it *paneItem, fb foldBlock, ok bool, err error) {
	it, err = p.itemByID(id)
	if err != nil {
		return nil, nil, false, err
	}
	fb, isFold := p.tr.entries[p.tr.index[id]].block.(foldBlock)
	return it, fb, isFold && fb.foldable(max(1, p.width)), nil
}

// Foldable reports whether a block has more than one form at the pane width.
// An unknown block is not foldable.
func (p *Pane) Foldable(id string) bool {
	_, _, ok, err := p.foldBlockOf(id)
	return err == nil && ok
}

// CollapseFold returns the lowest mode of a block now: FoldCollapsed, or
// FoldTruncated for a block that stays open while it streams. A block that
// cannot fold gives FoldExpanded.
func (p *Pane) CollapseFold(id string) FoldMode {
	_, fb, ok, err := p.foldBlockOf(id)
	if err != nil || !ok {
		return FoldExpanded
	}
	return fb.collapseFold()
}

// CycleFold moves a block to its next mode: collapsed, truncated, expanded,
// and then its lowest mode again.
func (p *Pane) CycleFold(id string) error {
	it, fb, ok, err := p.foldBlockOf(id)
	if err != nil || !ok {
		return err
	}
	it.setFold(nextFold(it.fold, fb.collapseFold()), fb)
	return nil
}

// ExpandBlock sets a block straight to FoldExpanded.
func (p *Pane) ExpandBlock(id string) error {
	it, fb, ok, err := p.foldBlockOf(id)
	if err != nil || !ok {
		return err
	}
	it.setFold(FoldExpanded, fb)
	return nil
}

// CollapseBlock sets a block to its lowest mode.
func (p *Pane) CollapseBlock(id string) error {
	it, fb, ok, err := p.foldBlockOf(id)
	if err != nil || !ok {
		return err
	}
	it.setFold(fb.collapseFold(), fb)
	return nil
}

// toggleAll expands every foldable block of the group set when one of them is
// at its lowest mode, and collapses them all otherwise. It returns true when
// the blocks are expanded now, and false when they are collapsed or when no
// block matches.
func (p *Pane) toggleAll(match func(foldGroup) bool) bool {
	p.sync()
	type target struct {
		it *paneItem
		fb foldBlock
	}
	var targets []target
	anyLow := false
	for i, it := range p.items {
		fb, ok := p.tr.entries[i].block.(foldBlock)
		if !ok || !match(fb.foldGroup()) || !fb.foldable(max(1, p.width)) {
			continue
		}
		targets = append(targets, target{it, fb})
		anyLow = anyLow || it.fold == fb.collapseFold()
	}
	for _, t := range targets {
		if anyLow {
			t.it.setFold(FoldExpanded, t.fb)
		} else {
			t.it.setFold(t.fb.collapseFold(), t.fb)
		}
	}
	return anyLow && len(targets) > 0
}

// ToggleAllSteps expands all tool and thinking steps, or collapses them all
// when none is at its lowest mode. It returns true when they are expanded.
func (p *Pane) ToggleAllSteps() bool { return p.toggleAll(foldGroup.isStep) }

// ToggleAllThinking does the same for the thinking steps only.
func (p *Pane) ToggleAllThinking() bool {
	return p.toggleAll(func(g foldGroup) bool { return g == groupThinking })
}
