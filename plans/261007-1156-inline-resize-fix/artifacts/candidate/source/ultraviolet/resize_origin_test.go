package uv

import (
	"bytes"
	"testing"
)

func TestInlineResizeCursorReflow(t *testing.T) {
	for _, tc := range []struct{ width, x, y int }{{13, 3, 1}, {12, 5, 1}} {
		var output bytes.Buffer
		r := NewTerminalRenderer(&output, []string{"TERM=xterm-256color"})
		r.SetRelativeCursor(true)
		r.SetInlineReflow(true)
		r.Resize(40, 12)
		cells := NewRenderBuffer(40, 3)
		x := 0
		for _, c := range []Cell{{Content: "e", Width: 1}, {Content: "d", Width: 1}, {Content: "i", Width: 1}, {Content: "t", Width: 1}, {Content: "o", Width: 1}, {Content: "r", Width: 1}, {Content: ">", Width: 1}, {Content: " ", Width: 1}, {Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}, {Content: "界", Width: 2}, {Content: "😀", Width: 2}, {Content: "Z", Width: 1}} {
			cells.SetCell(x, 0, &c)
			x += c.Width
		}
		r.Render(cells)
		r.MoveTo(16, 0)
		if err := r.Flush(); err != nil {
			t.Fatal(err)
		}
		r.Resize(tc.width, 6)
		x, y := r.Position()
		if x != tc.x || y != tc.y {
			t.Errorf("resize width %d: cursor %d,%d; native reflow requires %d,%d", tc.width, x, y, tc.x, tc.y)
		}
		r.Resize(13, 6)
		x, y = r.Position()
		if x != 3 || y != 1 {
			t.Errorf("resize burst: cursor %d,%d; expected3,1", x, y)
		}
	}
}
