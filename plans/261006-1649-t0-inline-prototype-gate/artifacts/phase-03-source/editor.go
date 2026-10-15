package main

import (
	"github.com/rivo/uniseg"
	"strings"
	"unicode"
)

// editorFrame retains the full draft and shows at most three rows around its cursor.
func editorFrame(text string, cursor, width, height int) (string, int, int) {
	width = max(width, 1)
	height = max(min(height, 3), 1)
	cursor = min(max(cursor, 0), len(text))
	rows := []string{""}
	x, y, cx, cy := 0, 0, 0, 0
	appendCell := func(s string, w int) {
		if x+w > width && x > 0 {
			rows = append(rows, "")
			y++
			x = 0
		}
		if w > width {
			s = "?"
			w = 1
		}
		rows[y] += s
		x += w
		if x >= width {
			rows = append(rows, "")
			y++
			x = 0
		}
	}
	// The prefix is part of the same cell layout as the draft.
	for _, r := range "editor> " {
		appendCell(string(r), 1)
	}
	cx, cy = x, y
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		start, end := g.Positions()
		if start == cursor {
			cx, cy = x, y
		}
		s := g.Str()
		switch s {
		case "\n":
			rows = append(rows, "")
			y++
			x = 0
		case "\t":
			for n := 4 - x%4; n > 0; n-- {
				appendCell(" ", 1)
			}
		default:
			if strings.ContainsFunc(s, unicode.IsControl) {
				s = "?"
			}
			appendCell(s, uniseg.StringWidth(s))
		}
		if end == cursor {
			cx, cy = x, y
		}
	}
	top := max(0, cy-height+1)
	bottom := min(len(rows), top+height)
	return strings.Join(rows[top:bottom], "\n"), min(cx, width-1), cy - top
}
func previousBoundary(s string, pos int) int {
	last := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		start, end := g.Positions()
		if end >= pos {
			return start
		}
		last = end
	}
	return last
}
func nextBoundary(s string, pos int) int {
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		_, end := g.Positions()
		if end > pos {
			return end
		}
	}
	return len(s)
}
