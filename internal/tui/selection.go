package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Text selection.
//
// A drag selects cells of the text that the block pane or the viewer draws.
// The selection is kept as positions in the content, a block, a row inside
// the block and a column, and not as screen rows, so that it stays on the same
// text when the pane scrolls under a drag at its edge.
//
// What a selection copies is the plain text of its cells. The copy draws the
// blocks again without the selection mark and without the expand indicator, so
// the rail, the shade, the indicator and its padding are never part of the
// text. Column 0 of the pane is the gutter and is not copied.

// textSurface names where a selection lives.
type textSurface int

const (
	surfNone textSurface = iota
	surfPane
	surfViewer
)

// textPoint is one cell of the content. In the viewer block is always 0 and row
// is the row of the body.
type textPoint struct{ block, row, col int }

func (p textPoint) before(q textPoint) bool {
	if p.block != q.block {
		return p.block < q.block
	}
	if p.row != q.row {
		return p.row < q.row
	}
	return p.col < q.col
}

// textRange is a selection: from is not after to, and both cells are inside
// the range.
type textRange struct {
	surf     textSurface
	from, to textPoint
}

func orderedRange(surf textSurface, a, b textPoint) textRange {
	if b.before(a) {
		a, b = b, a
	}
	return textRange{surf: surf, from: a, to: b}
}

// colEnd is a column past the end of every row: a point at the end of a row.
const colEnd = 1 << 20

// unwrappedWidth is the width at which a block draws without any soft wrap. It
// is used to find the rows that one logical line was cut into.
const unwrappedWidth = 1 << 12

// paneGutter is the number of columns at the left of the pane that hold no
// text.
const paneGutter = 1

// colSpan returns the columns [c0, c1) that a range takes in a row. first and
// last tell whether the row is the first or the last row of the range.
func (r textRange) colSpan(first, last bool, minCol int) (c0, c1 int) {
	c0, c1 = minCol, colEnd
	if first {
		c0 = max(c0, r.from.col)
	}
	if last {
		c1 = r.to.col + 1
	}
	return c0, c1
}

// cellSlice returns the part of a plain row that covers the columns
// [left, right). A wide character is part of the slice when one of its cells
// is.
func cellSlice(s string, left, right int) string {
	var b strings.Builder
	col := 0
	for rest := s; rest != "" && col < right; {
		g, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		w := displayWidth(g)
		if col+w > left {
			b.WriteString(g)
		}
		col += w
		rest = tail
	}
	return b.String()
}

// shadeCells draws the cells [from, to) of a styled row on the `selected`
// background. A style sequence inside the range would end or replace the
// background, so the background is set again after each one. Without color the
// cells are reversed.
func shadeCells(th *Theme, row string, from, to int) string {
	if to <= from {
		return row
	}
	on, off := bgSequence(th, RoleSelected), "\x1b[49m"
	if on == "" {
		on, off = ansiRev, "\x1b[27m"
	}
	var b strings.Builder
	b.Grow(len(row) + 3*len(on))
	var state byte
	col := 0
	shaded := false
	for rest := row; rest != ""; {
		seq, w, n, next := ansi.DecodeSequence(rest, state, nil)
		state, rest = next, rest[n:]
		if w > 0 {
			inside := col < to && col+w > from
			switch {
			case inside && !shaded:
				b.WriteString(on)
			case !inside && shaded:
				b.WriteString(off)
			}
			shaded = inside
			b.WriteString(seq)
			col += w
			continue
		}
		b.WriteString(seq)
		if shaded && ansi.HasCsiPrefix(seq) && strings.HasSuffix(seq, "m") {
			b.WriteString(on)
		}
	}
	if shaded {
		b.WriteString(off)
	}
	return b.String()
}

// wrapJoin tells how a drawn row continues the row above it.
type wrapJoin uint8

const (
	// joinNone: the row starts a logical line.
	joinNone wrapJoin = iota
	// joinSpace: the row continues the line; the wrap took the space between the
	// two words.
	joinSpace
	// joinDirect: the row continues the line inside a word.
	joinDirect
)

// softWraps finds which rows of a block are the continuation of the row above
// it. narrow holds the plain rows at the width of the pane. wide holds the
// plain rows of the same block drawn without wrapping, so each row of it is one
// logical line. A row that cannot be matched to a logical line starts a line:
// a wrong match would join lines that belong apart.
func softWraps(narrow, wide []string) []wrapJoin {
	out := make([]wrapJoin, len(narrow))
	k := 0
	// rest is the text of the current logical line that no row took yet, with
	// the runs of spaces folded. atWord is true when the text taken so far ended
	// at the end of a word, so a wrap there took a space.
	rest, atWord := "", false
	nextLine := func() {
		rest = ""
		for k < len(wide) && rest == "" {
			rest = strings.Join(strings.Fields(wide[k]), " ")
			k++
		}
	}
	// take removes the start of rest that a row shows. It reports false when the
	// row does not start like rest. A row may end in text that rest does not
	// have at that place, such as a send time at the right edge.
	take := func(t string) bool {
		p := 0
		for p < len(t) && p < len(rest) && t[p] == rest[p] {
			p++
		}
		for p > 0 && p < len(t) && !utf8.RuneStart(t[p]) {
			p--
		}
		if p == 0 || (p < len(t) && t[p-1] != ' ') {
			return false
		}
		atWord = rest[p-1] == ' ' || (p < len(rest) && rest[p] == ' ')
		rest = strings.TrimPrefix(rest[p:], " ")
		return true
	}
	for i, row := range narrow {
		t := strings.Join(strings.Fields(row), " ")
		if t == "" {
			rest = ""
			continue
		}
		if rest != "" {
			if wasWord := atWord; take(t) {
				out[i] = joinDirect
				if wasWord {
					out[i] = joinSpace
				}
				continue
			}
		}
		nextLine()
		if rest != "" && !take(t) {
			rest = ""
		}
	}
	return out
}

// copyLine is one line of a copy while it is built.
type copyLine struct {
	text string
	// start is the column of the first cell of text. indent is the column of the
	// first character in the row it came from, or -1 for a blank row. partial is
	// true when the selection began inside the text of the row, so that text
	// holds no indent of the row.
	start, indent int
	partial       bool
}

// lineBuilder collects the lines of a copy.
type lineBuilder struct{ lines []copyLine }

// add appends the cells of one row. start is the first column of the cells. A
// row that continues the line above it joins that line: the space of the wrap
// comes back, and the indent of the row, which was never part of the text, is
// dropped.
func (b *lineBuilder) add(row string, start, end int, join wrapJoin) {
	cells := strings.TrimRight(cellSlice(row, start, end), " ")
	if join != joinNone && len(b.lines) > 0 {
		last := &b.lines[len(b.lines)-1]
		cells = strings.TrimLeft(cells, " ")
		if join == joinSpace && cells != "" && last.text != "" {
			last.text += " "
		}
		last.text += cells
		return
	}
	indent := displayWidth(row) - displayWidth(strings.TrimLeft(row, " "))
	if strings.TrimSpace(row) == "" {
		indent = -1
	}
	b.lines = append(b.lines, copyLine{text: cells, start: start, indent: indent, partial: indent >= 0 && start > indent})
}

// text joins the lines. The columns that every line shares at its left edge are
// the indent that the blocks draw before their text; they are not copied, and
// the indent of one line inside another stays.
func (b *lineBuilder) text() string {
	strip := -1
	for _, l := range b.lines {
		if l.indent >= 0 && !l.partial && l.text != "" && (strip < 0 || l.indent < strip) {
			strip = l.indent
		}
	}
	out := make([]string, len(b.lines))
	for i, l := range b.lines {
		out[i] = l.text
		if strip > l.start && !l.partial {
			n := min(strip-l.start, len(l.text)-len(strings.TrimLeft(l.text, " ")))
			out[i] = l.text[n:]
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// blockPlain returns the plain rows of block i for a width. They are the rows
// that the pane draws, without the selection mark and without the expand
// indicator.
func (m *Model) blockPlain(i, width int) []string {
	e := m.transcript.entries[i]
	rows := renderRows(e.block, RenderContext{Width: width, Theme: m.theme, Fold: m.pane.Fold(e.id)})
	return stripRows(rows)
}

func stripRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

// paneRangeText returns the text of a range of the block pane.
func (m *Model) paneRangeText(r textRange) string {
	var lb lineBuilder
	last := len(m.transcript.entries) - 1
	for b := max(r.from.block, 0); b <= min(r.to.block, last); b++ {
		rows := m.blockPlain(b, m.width)
		var joins []wrapJoin
		if len(rows) > 1 {
			joins = softWraps(rows, m.blockPlain(b, unwrappedWidth))
		}
		r0, r1 := 0, len(rows)-1
		if b == r.from.block {
			r0 = r.from.row
		}
		if b == r.to.block {
			r1 = min(r.to.row, r1)
		}
		for row := r0; row <= r1; row++ {
			c0, c1 := r.colSpan(b == r.from.block && row == r.from.row, b == r.to.block && row == r.to.row, paneGutter)
			join := joinNone
			if row > r0 && row < len(joins) {
				join = joins[row]
			}
			lb.add(rows[row], c0, c1, join)
		}
	}
	return lb.text()
}

// viewerRangeText returns the text of a range of the viewer body.
func (m *Model) viewerRangeText(r textRange) string {
	e := m.viewerEntry()
	if e == nil {
		return ""
	}
	vb, ok := e.block.(viewerBlock)
	if !ok {
		return ""
	}
	rows := stripRows(m.viewerBody())
	rc := m.viewerContext()
	rc.Width = unwrappedWidth
	joins := softWraps(rows, stripRows(wrapBody(vb, rc)))
	var lb lineBuilder
	for row := max(r.from.row, 0); row <= min(r.to.row, len(rows)-1); row++ {
		c0, c1 := r.colSpan(row == r.from.row, row == r.to.row, 0)
		join := joinNone
		if row > r.from.row && row < len(joins) {
			join = joins[row]
		}
		lb.add(rows[row], c0, c1, join)
	}
	return lb.text()
}

// rangeText returns the plain text of a selection.
func (m *Model) rangeText(r textRange) string {
	switch r.surf {
	case surfPane:
		return m.paneRangeText(r)
	case surfViewer:
		return m.viewerRangeText(r)
	}
	return ""
}

// shadePane puts the selection on the rows of the pane. rows are the content
// rows that the pane drew; the row of the bottom marker is not content.
func (m *Model) shadePane(rows []string) {
	r := m.mouse.rng
	if r.surf != surfPane || m.viewerOpen() {
		return
	}
	first := textPoint{block: r.from.block, row: r.from.row}
	last := textPoint{block: r.to.block, row: r.to.row}
	for y := range rows {
		b, off, ok := m.pane.BlockAt(y)
		if !ok {
			continue
		}
		p := textPoint{block: b, row: off}
		if p.before(first) || last.before(p) {
			continue
		}
		c0, c1 := r.colSpan(p == first, p == last, paneGutter)
		rows[y] = shadeCells(m.theme, rows[y], c0, min(c1, displayWidth(rows[y])))
	}
}
