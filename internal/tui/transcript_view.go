package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Block viewer.
//
// The viewer shows one block on the whole screen: a title row with the way
// back and the outcome of the block, a rule, the full body of the block with
// every line wrapped, a rule and a key row. It holds no state of the session
// and it never changes the transcript, the editor or the block pane: Esc
// returns to the same list position and selection. The body is the block drawn
// in its Expanded form, so what the viewer shows and what the pane shows when
// the block is expanded come from the same drawing.

// viewerBlock is implemented by the blocks that the viewer opens.
type viewerBlock interface {
	// viewerHead returns the title and the outcome that the title row shows at
	// its right edge, with the role of the outcome. The outcome may be empty.
	viewerHead(rc RenderContext) (title, outcome string, role Role)
	// viewerBody returns the rows below the title: the whole content of the
	// block, wrapped for the width, with nothing cut.
	viewerBody(rc RenderContext) []string
}

// viewerDrawing is the head and the body of the viewed block and what they were
// drawn from, so that a frame or a key draws the block again only when its
// content, its drawing state, the width or the theme changed.
type viewerDrawing struct {
	valid   bool
	entry   *entry
	version uint64
	state   uint64
	width   int
	theme   *Theme

	title, outcome string
	outcomeRole    Role
	body           []string
}

// viewerState is the state of the viewer.
type viewerState struct {
	open bool
	// id is the block that the viewer shows.
	id string
	// top is the first body row of the window while the viewer does not follow
	// the end.
	top int
	// follow is true while the window shows the end of the body: content that
	// grows then keeps the window at the end.
	follow bool
	// drawing is the cache of the drawing of the block. It ends with the viewer.
	drawing viewerDrawing
}

// Texts of the key row. The short form is used under shortViewerWidth columns,
// where the position of the window does not show either.
const (
	viewerKeys      = "↑↓ scroll · PgUp PgDn page · Home End · Esc back"
	viewerKeysShort = "↑↓ PgUp PgDn · Esc back"
	viewerBack      = "← back"

	// shortViewerWidth is the width under which the short key row is used.
	shortViewerWidth = 60
	// viewerChromeRows is the number of rows around the body: the title row, two
	// rules and the key row. Under viewerChromeRows+1 rows only the body shows.
	viewerChromeRows = 4
)

// viewerOpen reports whether the viewer is on the screen.
func (m *Model) viewerOpen() bool { return m.viewer.open }

// viewerContext is the render context of the viewer.
func (m *Model) viewerContext() RenderContext {
	rc := m.renderContext()
	rc.Fold = FoldExpanded
	return rc
}

// canOpenViewer reports whether the viewer can show a block.
func (m *Model) canOpenViewer(id string) bool {
	i, ok := m.transcript.index[id]
	if !ok {
		return false
	}
	_, ok = m.transcript.entries[i].block.(viewerBlock)
	return ok
}

// openViewer shows a block on the whole screen, at its first row. It does
// nothing for a block that the viewer cannot show.
func (m *Model) openViewer(id string) {
	if !m.canOpenViewer(id) {
		return
	}
	m.viewer = viewerState{open: true, id: id}
	// A selection and a drag belong to the surface that they began on.
	m.mouse.cancel()
	m.mouse.rng = textRange{}
	m.viewer.follow = len(m.viewerBody()) <= m.viewerWindow()
}

// closeViewer leaves the viewer.
func (m *Model) closeViewer() {
	if m.viewer.open {
		m.mouse.cancel()
		m.mouse.rng = textRange{}
	}
	m.viewer = viewerState{}
}

// scrollViewer moves the window of the viewer by rows: a negative number scrolls
// up. A move that ends at the last row makes the window follow the end.
func (m *Model) scrollViewer(rows int) {
	total, win := len(m.viewerBody()), m.viewerWindow()
	maxTop := max(0, total-win)
	top := min(max(m.viewerTop(total)+rows, 0), maxTop)
	m.viewer.top, m.viewer.follow = top, top >= maxTop
}

// viewerEntry returns the entry that the viewer shows, or nil when it is gone.
func (m *Model) viewerEntry() *entry {
	i, ok := m.transcript.index[m.viewer.id]
	if !ok {
		return nil
	}
	return m.transcript.entries[i]
}

// viewerDraw returns the drawing of the viewed block. It draws the block again
// only when something it depends on changed. A block that is gone gives an
// empty drawing.
func (m *Model) viewerDraw() *viewerDrawing {
	d := &m.viewer.drawing
	e := m.viewerEntry()
	if e == nil {
		*d = viewerDrawing{}
		return d
	}
	vb, ok := e.block.(viewerBlock)
	if !ok {
		*d = viewerDrawing{}
		return d
	}
	var state uint64
	if ds, ok := e.block.(drawStater); ok {
		state = ds.drawState()
	}
	rc := m.viewerContext()
	if d.valid && d.entry == e && d.version == e.version && d.state == state && d.width == rc.Width && d.theme == rc.Theme {
		return d
	}
	*d = viewerDrawing{valid: true, entry: e, version: e.version, state: state, width: rc.Width, theme: rc.Theme}
	d.title, d.outcome, d.outcomeRole = vb.viewerHead(rc)
	d.body = wrapBody(vb, rc)
	return d
}

// wrapBody returns the rows of the body of a block for a width, with every row
// cut to fit.
func wrapBody(vb viewerBlock, rc RenderContext) []string {
	var body []string
	for _, row := range vb.viewerBody(rc) {
		for _, part := range strings.Split(row, "\n") {
			body = append(body, hardWrapRows(part, rc.Width)...)
		}
	}
	return body
}

// viewerBody returns the rows of the body.
func (m *Model) viewerBody() []string { return m.viewerDraw().body }

// viewerChrome reports whether the screen has room for the rows around the
// body.
func (m *Model) viewerChrome() bool { return m.height > viewerChromeRows }

// viewerWindow is the number of body rows that the viewer shows.
func (m *Model) viewerWindow() int {
	if !m.viewerChrome() {
		return max(1, m.height)
	}
	return m.height - viewerChromeRows
}

// viewerTop returns the first body row of the window for a body of total rows.
func (m *Model) viewerTop(total int) int {
	maxTop := max(0, total-m.viewerWindow())
	if m.viewer.follow {
		return maxTop
	}
	return min(max(m.viewer.top, 0), maxTop)
}

// onViewerKey handles a key while the viewer is open. Every key is taken here:
// a key that has no meaning in the viewer is dropped and never reaches the
// editor or a shortcut.
func (m *Model) onViewerKey(k tea.KeyPressMsg) {
	total, win := len(m.viewerBody()), m.viewerWindow()
	maxTop := max(0, total-win)
	top := m.viewerTop(total)
	page := max(1, win-1)
	switch k.String() {
	case "esc":
		m.closeViewer()
		return
	case "up", "k":
		top--
	case "down", "j":
		top++
	case "pgup", "b":
		top -= page
	case "pgdown", "space", " ":
		top += page
	case "home", "g":
		top = 0
	case "end", "G", "shift+g":
		top = maxTop
	default:
		return
	}
	top = min(max(top, 0), maxTop)
	m.viewer.top = top
	m.viewer.follow = top >= maxTop
}

// viewerFrame builds the screen of the viewer: title row, rule, window, rule,
// key row. Under viewerChromeRows+1 rows only the window is drawn.
func (m *Model) viewerFrame() tea.View {
	d := m.viewerDraw()
	total, win := len(d.body), m.viewerWindow()
	top := m.viewerTop(total)
	end := min(top+win, total)
	out := make([]string, 0, m.height)
	chrome := m.viewerChrome()
	rule := m.theme.Render(RoleBorder, strings.Repeat("─", m.width))
	if chrome {
		out = append(out, viewerTitleRow(m.theme, m.width, d.title, d.outcome, d.outcomeRole), rule)
	}
	for i, r := range d.body[top:end] {
		out = append(out, truncateTo(m.shadeViewerRow(r, top+i), m.width))
	}
	want := m.height
	if chrome {
		want -= 2
	}
	for len(out) < want {
		out = append(out, "")
	}
	if chrome {
		out = append(out, rule, viewerKeyRow(m.theme, m.width, top+1, end, total))
	}
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

// shadeViewerRow puts the selection on a body row.
func (m *Model) shadeViewerRow(row string, idx int) string {
	r := m.mouse.rng
	if r.surf != surfViewer || idx < r.from.row || idx > r.to.row {
		return row
	}
	c0, c1 := r.colSpan(idx == r.from.row, idx == r.to.row, 0)
	return shadeCells(m.theme, row, c0, min(c1, displayWidth(row)))
}

// viewerTitleRow is the first row of the viewer: the way back, the title and,
// at the right edge, the outcome of the block. The title is cut with an
// ellipsis when it does not fit beside the outcome.
func viewerTitleRow(th *Theme, width int, title, outcome string, role Role) string {
	outcome = oneLine(outcome)
	lead := 1 + displayWidth(viewerBack) + 2
	right := 0
	if outcome != "" {
		// The outcome ends one column before the edge and keeps one space of gap
		// to the title.
		right = displayWidth(outcome) + 1
		if lead+1+right > width {
			// A narrow row keeps the title and drops the outcome.
			outcome, right = "", 0
		}
	}
	room := width - lead
	if right > 0 {
		room -= right + 1
	}
	title = truncateTo(title, max(0, room))
	row := " " + th.Style(RoleAccent).Bold(true).Render(viewerBack) + "  " + th.Style(RoleFg).Bold(true).Render(title)
	if outcome != "" {
		gap := max(1, width-lead-displayWidth(title)-right)
		row += strings.Repeat(" ", gap) + th.Render(role, outcome) + " "
	}
	return truncateTo(row, width)
}

// viewerKeyRow is the last row of the viewer: the keys at the left and, when
// the width allows, the rows of the window at the right.
func viewerKeyRow(th *Theme, width, first, last, total int) string {
	if width < shortViewerWidth {
		return " " + th.Render(RoleDim, truncateTo(viewerKeysShort, max(0, width-1)))
	}
	pos := ""
	if total > 0 {
		pos = fmt.Sprintf("%d–%d of %d", first, last, total)
	}
	keys := viewerKeys
	room := width - 1 - displayWidth(pos) - 1
	if pos == "" || displayWidth(keys)+1 > room {
		return " " + th.Render(RoleDim, truncateTo(keys, max(0, width-1)))
	}
	gap := width - 1 - displayWidth(keys) - displayWidth(pos) - 1
	return " " + th.Render(RoleDim, keys) + strings.Repeat(" ", gap) + th.Render(RoleDim, pos) + " "
}
