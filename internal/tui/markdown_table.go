package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/colorprofile"
	xansi "github.com/charmbracelet/x/ansi"
)

// Markdown tables.
//
// Glamour draws a table without the outer box, stretches it to the full width
// and gives the header the style of the body. A reply therefore finds the GFM
// tables in its prose and draws them with textTable (table_box.go). The text
// before and after a table still goes to glamour. When the box cannot fit,
// glamour draws the whole chunk.

// markdownTableStyle is the style of a table in a reply.
var markdownTableStyle = tableStyle{border: RoleDim, header: RoleFg, body: RoleFg}

var (
	tableDelimiterCell = regexp.MustCompile(`^:?-+:?$`)
	// tableBreak is a line that starts a new block and so ends a table.
	tableBreak = regexp.MustCompile("^ {0,3}(?:#{1,6}(?: |$)|>|```|~~~)")
	quoteLine  = regexp.MustCompile(`^ {0,3}>`)
)

// proseOrTableRows draws a prose chunk. A table in the chunk gets the table
// box; the text before and after it goes to glamour.
func proseOrTableRows(body string, width int, th *Theme) []mdRow {
	before, table, after, ok := findTable(body)
	if !ok {
		return proseRows(body, width, th)
	}
	box, fits := table.draw(width, th, markdownTableStyle)
	if !fits {
		rows := proseRows(body, width, th)
		if before == "" && len(rows) > 0 {
			// Glamour draws the table, so the first row is a row of it.
			rows[0].boxed = true
		}
		return rows
	}
	var rows []mdRow
	if before != "" {
		if rows = proseRows(before, width, th); len(rows) > 0 {
			rows = append(rows, mdRow{})
		}
	}
	for _, r := range box {
		rows = append(rows, mdRow{text: r, boxed: true})
	}
	if after != "" {
		if rest := proseOrTableRows(after, width, th); len(rest) > 0 {
			rows = append(rows, mdRow{})
			rows = append(rows, rest...)
		}
	}
	return rows
}

// startsWithTable reports whether a prose chunk begins with a table, so that
// its first row is a box row and not a text row. It reads the source only, so
// a caller can choose the width before it draws the chunk.
func startsWithTable(body string) bool {
	before, _, _, ok := findTable(body)
	return ok && strings.TrimSpace(before) == ""
}

// findTable finds the first table in the lines of a prose chunk. It returns
// the text before the table, the table with plain cell text, and the text
// after it. A table in a list or a block quote stays with glamour, which
// draws it in its context.
func findTable(body string) (before string, t textTable, after string, ok bool) {
	lines := strings.Split(body, "\n")
	for i := 1; i < len(lines); i++ {
		prev := lines[i-1]
		if listMarker.MatchString(prev) || quoteLine.MatchString(prev) {
			// The lines after a list item or a quote line belong to it.
			return "", textTable{}, "", false
		}
		align, isDelim := delimiterRow(lines[i])
		if !isDelim || !hasPipe(prev) || indentOf(prev) > 3 {
			continue
		}
		header := splitCells(prev)
		if len(header) != len(align) {
			continue
		}
		end := i + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" && !tableBreak.MatchString(lines[end]) {
			end++
		}
		t = textTable{header: inlineCells(header, len(header)), align: align}
		for _, line := range lines[i+1 : end] {
			t.rows = append(t.rows, inlineCells(splitCells(line), len(header)))
		}
		before = strings.TrimRight(strings.Join(lines[:i-1], "\n"), "\n")
		after = strings.TrimLeft(strings.Join(lines[end:], "\n"), "\n")
		return before, t, after, true
	}
	return "", textTable{}, "", false
}

// delimiterRow reads the row under a table header, such as "|---|:--:|". It
// returns the alignment of each column.
func delimiterRow(line string) ([]cellAlign, bool) {
	if !hasPipe(line) || indentOf(line) > 3 {
		return nil, false
	}
	cells := splitCells(line)
	align := make([]cellAlign, len(cells))
	for i, c := range cells {
		if !tableDelimiterCell.MatchString(c) {
			return nil, false
		}
		switch left, right := strings.HasPrefix(c, ":"), strings.HasSuffix(c, ":"); {
		case left && right:
			align[i] = alignCenter
		case right:
			align[i] = alignRight
		}
	}
	return align, len(cells) > 0
}

// splitCells splits a table row at each pipe that has no backslash before it.
// The pipes at the start and at the end of the row are optional. An escaped
// pipe becomes a plain pipe in the cell.
func splitCells(line string) []string {
	line = strings.TrimPrefix(strings.TrimSpace(line), "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func hasPipe(line string) bool { return strings.Contains(strings.ReplaceAll(line, `\|`, ""), "|") }

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// plainTheme draws markdown without any style. inlineCells uses it to read
// the text of a cell.
var plainTheme = NewTheme(colorprofile.NoTTY)

// inlineWidth is wide enough that glamour does not wrap the text of a cell.
const inlineWidth = 1 << 12

// inlineCells returns n cells as plain text: glamour reads the inline markdown
// (emphasis markers, backticks, escapes and links go), and the style
// sequences are removed. Extra cells go and missing cells are empty.
func inlineCells(cells []string, n int) []string {
	out := make([]string, n)
	for i := range min(n, len(cells)) {
		var parts []string
		for _, r := range proseRows(cells[i], inlineWidth, plainTheme) {
			if s := strings.TrimSpace(xansi.Strip(r.text)); s != "" {
				parts = append(parts, s)
			}
		}
		out[i] = strings.Join(parts, " ")
	}
	return out
}
