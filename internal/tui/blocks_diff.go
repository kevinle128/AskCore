package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Line diff of a file change.
//
// The row of an edit shows how many lines the change adds and removes. The
// expanded form shows the change itself as a unified diff with three lines of
// context. Both come from one edit script, so the counts on the line
// are always the counts of the rows below it.

const (
	// maxDiffEdits bounds the work of the edit script. A change that needs more
	// edits than this is shown as a full replace of the lines in between.
	maxDiffEdits = 1000
	// diffContext is the number of unchanged lines around each change.
	diffContext = 3
	// diffMinNumberWidth is the least width of the line number column.
	diffMinNumberWidth = 4
)

// Kinds of a diff row.
const (
	diffSame    byte = ' '
	diffAdded   byte = '+'
	diffRemoved byte = '-'
)

// diffRow is one row of a unified diff. num is the line number in the new
// text. A removed row has the number that the next new line has, so a changed
// line shows the same number on its removed and its added row.
type diffRow struct {
	kind byte
	num  int
	text string
}

// diffLines splits a text for a line diff. Unlike outputLines, a text of
// white space keeps its lines.
func diffLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// lineDiffStats counts the lines that a line diff of two texts adds and
// removes. A final line end is not a line.
func lineDiffStats(oldText, newText string) (added, removed int) {
	_, added, removed = unifiedDiff(oldText, newText)
	return added, removed
}

// unifiedDiff returns the rows of the changed hunks with their context, and
// the number of added and removed lines.
func unifiedDiff(oldText, newText string) (rows []diffRow, added, removed int) {
	a, b := diffLines(oldText), diffLines(newText)
	ops := diffOps(a, b)
	// Rows of the whole file first; the hunks are cut from them after.
	all := make([]diffRow, 0, len(ops))
	var ai, bi int
	for _, op := range ops {
		switch op {
		case diffSame:
			all = append(all, diffRow{kind: diffSame, num: bi + 1, text: b[bi]})
			ai++
			bi++
		case diffRemoved:
			all = append(all, diffRow{kind: diffRemoved, num: bi + 1, text: a[ai]})
			ai++
			removed++
		default:
			all = append(all, diffRow{kind: diffAdded, num: bi + 1, text: b[bi]})
			bi++
			added++
		}
	}
	// A row stays when a change is within diffContext rows of it.
	keep := make([]bool, len(all))
	for i, r := range all {
		if r.kind == diffSame {
			continue
		}
		for j := max(0, i-diffContext); j <= min(len(all)-1, i+diffContext); j++ {
			keep[j] = true
		}
	}
	for i, r := range all {
		if keep[i] {
			rows = append(rows, r)
		}
	}
	return rows, added, removed
}

// diffOps returns the edit script from a to b, one op for each line: same,
// removed or added. It is a shortest script (Myers). When more than
// maxDiffEdits edits are needed, the lines between the common start and the
// common end are all removed and all added.
func diffOps(a, b []string) []byte {
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	ops := make([]byte, 0, len(a)+len(b))
	for i := 0; i < start; i++ {
		ops = append(ops, diffSame)
	}
	mid, ok := myersOps(a[start:endA], b[start:endB], maxDiffEdits)
	if !ok {
		mid = mid[:0]
		for i := start; i < endA; i++ {
			mid = append(mid, diffRemoved)
		}
		for i := start; i < endB; i++ {
			mid = append(mid, diffAdded)
		}
	}
	ops = append(ops, mid...)
	for i := endA; i < len(a); i++ {
		ops = append(ops, diffSame)
	}
	return ops
}

// myersOps returns a shortest edit script of two line lists. It gives up when
// more than limit edits are needed.
func myersOps(a, b []string, limit int) ([]byte, bool) {
	n, m := len(a), len(b)
	switch {
	case n == 0 && m == 0:
		return nil, true
	case n == 0:
		return repeatOp(diffAdded, m), true
	case m == 0:
		return repeatOp(diffRemoved, n), true
	}
	limit = min(limit, n+m)
	off := limit + 1
	v := make([]int, 2*limit+3)
	// trace[d] holds v for the diagonals -d to d after round d.
	trace := make([][]int, 0, 16)
	done := false
	for d := 0; d <= limit && !done; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				done = true
				break
			}
		}
		trace = append(trace, append([]int(nil), v[off-d:off+d+1]...))
	}
	if !done {
		return nil, false
	}
	// Walk back from the end. ops come out in reverse order.
	var rev []byte
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		prev := trace[d-1] // index k+(d-1)
		k := x - y
		var prevK int
		if k == -d || (k != d && prev[k-1+d-1] < prev[k+1+d-1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := prev[prevK+d-1]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, diffSame)
			x--
			y--
		}
		if x == prevX {
			rev = append(rev, diffAdded)
			y--
		} else {
			rev = append(rev, diffRemoved)
			x--
		}
	}
	for x > 0 && y > 0 {
		rev = append(rev, diffSame)
		x--
		y--
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, true
}

func repeatOp(op byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = op
	}
	return out
}

// diffRowsFor draws the unified diff of a file change for a width. With wrap, a
// long line wraps under its text column and nothing is cut; without it each
// diff row is one row and a long line ends with an ellipsis.
func (o toolOutput) diffRowsFor(rc RenderContext, wrap bool) []string {
	if len(o.diffRows) == 0 {
		return nil
	}
	th := rc.Theme
	numW := diffMinNumberWidth
	for _, r := range o.diffRows {
		numW = max(numW, len(fmt.Sprint(r.num)))
	}
	prefixW := textIndent + numW + 1 + 2
	textW := max(1, rc.Width-prefixW)
	pad := strings.Repeat(" ", textIndent)
	cont := strings.Repeat(" ", prefixW)
	var rows []string
	for _, r := range o.diffRows {
		signRole, textRole := RoleMuted, RoleFg
		switch r.kind {
		case diffAdded:
			signRole, textRole = RoleSuccess, RoleSuccess
		case diffRemoved:
			signRole, textRole = RoleError, RoleError
		}
		text := cleanText(ansi.Strip(r.text))
		parts := []string{truncateTo(text, textW)}
		if wrap {
			parts = columnCut(text, textW)
		}
		for i, p := range parts {
			if i > 0 {
				rows = append(rows, cont+th.Render(textRole, p))
				continue
			}
			rows = append(rows, pad+th.Render(RoleDim, fmt.Sprintf("%*d ", numW, r.num))+
				th.Render(signRole, string(r.kind)+" ")+th.Render(textRole, p))
		}
	}
	return rows
}

// columnCut breaks a line into pieces of at most width columns. Nothing is
// dropped and no wide character is split. An empty line is one empty piece.
func columnCut(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	return hardWrapRows(line, max(1, width))
}
