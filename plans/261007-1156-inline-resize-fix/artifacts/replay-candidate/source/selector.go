package main

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type selector struct {
	open     bool
	selected int
}

func (s selector) frame(width, height int) (string, int, int) {
	height = max(1, min(height, 3))
	top := min(max(0, s.selected-height+1), 12-height)
	rows := make([]string, height)
	for i := range rows {
		marker := " "
		if top+i == s.selected {
			marker = ">"
		}
		rows[i] = ansi.Truncate(fmt.Sprintf("%s item %02d", marker, top+i+1), width, "")
	}
	return strings.Join(rows, "\n"), 0, s.selected - top
}
