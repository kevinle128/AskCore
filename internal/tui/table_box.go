package tui

import "strings"

// Table box.
//
// textTable draws plain text cells in a box:
//
//	┌──────┬─────┐
//	│ Type │ Job │
//	├──────┼─────┤
//	│ a    │ b   │
//	└──────┴─────┘
//
// It knows nothing of markdown or of the block that shows it. A column is as
// wide as its widest cell. When the box is wider than the width, the widest
// columns become narrower and their cells wrap with wrapLines, so a cell is
// never cut without "…".

// cellAlign is the alignment of the text in a column.
type cellAlign int

const (
	alignLeft cellAlign = iota
	alignCenter
	alignRight
)

// textTable is a table of plain text cells. Every row has one cell for each
// header cell; align has one entry for each column, or none for left.
type textTable struct {
	header []string
	rows   [][]string
	align  []cellAlign
}

// tableStyle names the roles of a table: the box lines, the header cells
// (also bold) and the body cells.
type tableStyle struct {
	border, header, body Role
}

// boxRule is one horizontal line of a table box: the left corner, the joint
// between two columns and the right corner.
type boxRule struct{ left, joint, right string }

// The glyphs of a table box.
const (
	boxLine = "─"
	boxBar  = "│"
)

var (
	tableTop    = boxRule{"┌", "┬", "┐"}
	tableMiddle = boxRule{"├", "┼", "┤"}
	tableBottom = boxRule{"└", "┴", "┘"}
)

// minTableColumn is the narrowest column: a two-column grapheme still fits.
const minTableColumn = 2

// tableBoxWidth is the width that the bars and the cell padding of n columns
// use: one column for each bar and one space at each side of a cell.
func tableBoxWidth(n int) int { return 3*n + 1 }

// draw returns the rows of the box for a width. It returns false when the box
// cannot fit, even with the narrowest columns.
func (t textTable) draw(width int, th *Theme, st tableStyle) ([]string, bool) {
	n := len(t.header)
	avail := width - tableBoxWidth(n)
	if n == 0 || avail < n*minTableColumn {
		return nil, false
	}
	widths := make([]int, n)
	for _, row := range append([][]string{t.header}, t.rows...) {
		for c := range n {
			for _, line := range wrapLines(cellAt(row, c), avail) {
				widths[c] = max(widths[c], displayWidth(line))
			}
		}
	}
	shrinkColumns(widths, avail)

	rule := func(g boxRule) string {
		parts := make([]string, n)
		for c, w := range widths {
			parts[c] = strings.Repeat(boxLine, w+2)
		}
		return th.Render(st.border, g.left+strings.Join(parts, g.joint)+g.right)
	}
	bar := th.Render(st.border, boxBar)
	header := th.Style(st.header).Bold(true)
	line := func(row []string, isHeader bool) []string {
		cells := make([][]string, n)
		height := 1
		for c := range n {
			cells[c] = wrapLines(cellAt(row, c), widths[c])
			height = max(height, len(cells[c]))
		}
		out := make([]string, height)
		for r := range out {
			var b strings.Builder
			b.WriteString(bar)
			for c, w := range widths {
				text := ""
				if r < len(cells[c]) {
					text = cells[c][r]
				}
				gap := max(0, w-displayWidth(text))
				if text != "" {
					if isHeader {
						text = header.Render(text)
					} else {
						text = th.Render(st.body, text)
					}
				}
				b.WriteString(" " + alignText(text, gap, t.alignOf(c)) + " " + bar)
			}
			out[r] = b.String()
		}
		return out
	}

	rows := []string{rule(tableTop)}
	rows = append(rows, line(t.header, true)...)
	rows = append(rows, rule(tableMiddle))
	for _, r := range t.rows {
		rows = append(rows, line(r, false)...)
	}
	return append(rows, rule(tableBottom)), true
}

func (t textTable) alignOf(c int) cellAlign {
	if c < len(t.align) {
		return t.align[c]
	}
	return alignLeft
}

// cellAt returns cell c of a row, or "" when the row is short.
func cellAt(row []string, c int) string {
	if c < len(row) {
		return row[c]
	}
	return ""
}

// shrinkColumns makes the widest column one column narrower until the sum of
// the widths fits avail. A column is at least one column wide and never
// becomes narrower than minTableColumn.
func shrinkColumns(widths []int, avail int) {
	sum := 0
	for i := range widths {
		widths[i] = max(widths[i], 1)
		sum += widths[i]
	}
	for sum > avail {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= minTableColumn {
			return
		}
		widths[widest]--
		sum--
	}
}

// alignText puts gap spaces around styled text for an alignment.
func alignText(text string, gap int, a cellAlign) string {
	switch a {
	case alignRight:
		return strings.Repeat(" ", gap) + text
	case alignCenter:
		return strings.Repeat(" ", gap/2) + text + strings.Repeat(" ", gap-gap/2)
	default:
		return text + strings.Repeat(" ", gap)
	}
}
