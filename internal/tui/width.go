package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Width helpers. Every size in the TUI is a count of terminal columns, never
// bytes or runes. A grapheme is never split: CJK and most emoji use two
// columns, and a joined emoji sequence is one unit.

const (
	tabStop        = 4
	truncateMarker = "…"
	controlMarker  = "?"
)

// displayWidth returns the columns that a string uses. SGR sequences count as
// zero columns.
func displayWidth(s string) int { return ansi.StringWidth(s) }

// cleanText makes untrusted text safe to draw. Tabs expand to the next
// multiple of four columns. Control characters become "?" so that text from a
// model or a file cannot move the cursor or change the terminal. Line breaks
// stay. SGR (style) sequences stay because callers style text before they
// wrap it; every other escape sequence is dropped.
func cleanText(s string) string {
	if s == "" {
		return s
	}
	// C1 controls are two bytes in UTF-8; the decoder could read them as
	// 8-bit sequence introducers, so they are replaced first.
	s = strings.Map(func(r rune) rune {
		if r >= 0x80 && r <= 0x9f {
			return '?'
		}
		return r
	}, s)

	var b strings.Builder
	b.Grow(len(s))
	var state byte
	col := 0
	for len(s) > 0 {
		seq, width, n, next := ansi.DecodeSequence(s, state, nil)
		state = next
		s = s[n:]
		switch {
		case seq == "\n":
			b.WriteString(seq)
			col = 0
		case seq == "\t":
			pad := tabStop - col%tabStop
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		case len(seq) == 1 && (seq[0] < 0x20 || seq[0] == 0x7f):
			b.WriteString(controlMarker)
			col++
		case strings.HasPrefix(seq, "\x1b"):
			if ansi.HasCsiPrefix(seq) && strings.HasSuffix(seq, "m") {
				b.WriteString(seq)
			}
		default:
			b.WriteString(seq)
			col += width
		}
	}
	return b.String()
}

// wrapLines cleans text and breaks it into rows of at most width columns.
// Words move to the next row when they do not fit; a word longer than a row
// breaks at the column limit. Empty lines stay as empty rows, and spaces at
// the start of a line stay as indentation. Style sequences do not carry from
// one row to the next, so style each row after wrapping.
func wrapLines(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var rows []string
	for _, line := range strings.Split(cleanText(s), "\n") {
		rows = append(rows, wrapLine(line, width)...)
	}
	return rows
}

// token is a printable grapheme or a zero-width style sequence.
type token struct {
	text  string
	width int
	space bool
}

func tokenize(line string) []token {
	var toks []token
	var state byte
	for len(line) > 0 {
		seq, width, n, next := ansi.DecodeSequence(line, state, nil)
		state = next
		line = line[n:]
		toks = append(toks, token{text: seq, width: width, space: seq == " "})
	}
	return toks
}

// rowBuilder collects rows for wrapLine.
type rowBuilder struct {
	limit   int
	rows    []string
	cur     strings.Builder
	curW    int
	wrapped bool // the current row started because the previous row was full
}

func (r *rowBuilder) newRow() {
	r.rows = append(r.rows, r.cur.String())
	r.cur.Reset()
	r.curW = 0
	r.wrapped = true
}

func (r *rowBuilder) put(t token) {
	r.cur.WriteString(t.text)
	r.curW += t.width
}

// putWord adds a word, which holds no space, to the rows.
func (r *rowBuilder) putWord(word []token, wordW int) {
	if len(word) == 0 {
		return
	}
	switch {
	case r.curW+wordW <= r.limit:
	case wordW <= r.limit:
		r.newRow()
	default:
		// The word is longer than a row: break it at the column limit.
		if r.curW > 0 {
			r.newRow()
		}
		for _, t := range word {
			if t.width > r.limit {
				// A wide grapheme in a one-column space cannot be drawn.
				t = token{text: truncateMarker, width: 1}
			}
			if r.curW+t.width > r.limit {
				r.newRow()
			}
			r.put(t)
		}
		return
	}
	for _, t := range word {
		r.put(t)
	}
}

func wrapLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	b := &rowBuilder{limit: width}
	var word []token
	wordW := 0
	flush := func() {
		b.putWord(word, wordW)
		word, wordW = word[:0], 0
	}
	for _, t := range tokenize(line) {
		if !t.space {
			word = append(word, t)
			wordW += t.width
			continue
		}
		flush()
		switch {
		case b.curW == 0 && b.wrapped:
			// A space at the start of a wrapped row is dropped.
		case b.curW+1 > width:
			// The row is full: the space is the break. The next word starts
			// a new row because it cannot fit.
		default:
			b.put(t)
		}
	}
	flush()
	return append(b.rows, b.cur.String())
}

// hardWrapRows splits a row that is wider than the terminal into rows that
// fit. It is the last guard for block output: no content is cut, so the
// number of terminal rows stays equal to the number of rows counted.
func hardWrapRows(row string, width int) []string {
	if width < 1 || displayWidth(row) <= width {
		return []string{row}
	}
	var rows []string
	for _, part := range strings.Split(ansi.Hardwrap(row, width, true), "\n") {
		if displayWidth(part) > width {
			part = ansi.Truncate(part, width, "")
		}
		rows = append(rows, part)
	}
	return rows
}

// truncateTo shortens a row to width columns and marks the cut with "…".
func truncateTo(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, truncateMarker)
}

// padTo adds spaces to the right until the row is width columns wide. It never
// cuts a row that is already wider.
func padTo(s string, width int) string {
	if gap := width - displayWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
