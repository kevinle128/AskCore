package tea

import (
	"bytes"
	"testing"
)

func TestResizeWaitsForSizedView(t *testing.T) {
	var output bytes.Buffer
	r := newCursedRenderer(&output, []string{"TERM=xterm-256color"}, 40, 12)
	v := NewView("editor> abc界😀Z")
	v.Cursor = NewCursor(16, 0)
	r.render(v)
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	r.resize(13, 6)
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("resize emitted stale-sized view before model render: %q", output.String())
	}
	v = NewView("editor> abc界\n😀Z")
	v.Cursor = NewCursor(3, 1)
	r.render(v)
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("sized view did not redraw")
	}
}
