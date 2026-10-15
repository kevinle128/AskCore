package main

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestEditorCellCursor(t *testing.T) {
	for _, c := range []struct {
		text                     string
		pos, width, height, x, y int
	}{
		{"界😀", len("界😀"), 40, 3, 12, 0}, {"é", len("é"), 40, 3, 9, 0}, {"a\nb", 3, 40, 3, 1, 1}, {strings.Repeat("x\n", 2000) + "tail", 4004, 40, 3, 4, 2}, {"界", 3, 1, 1, 0, 0},
	} {
		view, x, y := editorFrame(c.text, c.pos, c.width, c.height)
		if x != c.x || y != c.y {
			t.Fatalf("%q cursor (%d,%d) want (%d,%d)", c.text, x, y, c.x, c.y)
		}
		if strings.Count(view, "\n")+1 > max(c.height, 1) {
			t.Fatal("viewport exceeds height")
		}
	}
	if previousBoundary("é😀", len("é😀")) != len("é") {
		t.Fatal("backspace splits grapheme")
	}
}

func TestEditorViewBounds(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 1}, {10, 2}, {40, 4}} {
		m := model{g6: true, text: "界😀\ttext\nlast", cursor: len("界😀\ttext\nlast"), width: size[0], height: size[1]}
		v := m.View()
		if strings.Count(v.Content, "\n")+1 > size[1] {
			t.Fatalf("size=%v viewport=%q", size, v.Content)
		}
		for _, line := range strings.Split(v.Content, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("size=%v row overflows %q", size, line)
			}
		}
		if v.Cursor.X < 0 || v.Cursor.X >= size[0] || v.Cursor.Y < 0 || v.Cursor.Y >= size[1] {
			t.Fatalf("size=%v cursor=%v", size, v.Cursor)
		}
	}
}
