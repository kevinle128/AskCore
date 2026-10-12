package tea

import (
	"bytes"
	"testing"
)

// A resize must not draw the old view at the new size. The old view has its
// rows laid out for the old size, and the program draws the right view in the
// next frame. The renderer writes nothing until a view arrives after the
// resize.
func TestFlushAfterResizeWaitsForSizedView(t *testing.T) {
	var output bytes.Buffer
	r := newCursedRenderer(&output, []string{"TERM=xterm-256color"}, 20, 5)
	r.render(NewView("old view"))
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	output.Reset()

	r.resize(30, 8)
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("a flush between a resize and the next view must write nothing: %q", output.String())
	}

	r.render(NewView("new view"))
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("new view")) {
		t.Fatalf("the view that follows a resize must be drawn: %q", output.String())
	}
}

// The closing flush runs even when no view followed the resize, so the
// terminal is restored on every end path.
func TestClosingFlushAfterResizeStillWrites(t *testing.T) {
	var output bytes.Buffer
	r := newCursedRenderer(&output, []string{"TERM=xterm-256color"}, 20, 5)
	r.render(NewView("old view"))
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	r.resize(30, 8)
	output.Reset()
	if err := r.flush(true); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("the closing flush must write after a resize")
	}
}
