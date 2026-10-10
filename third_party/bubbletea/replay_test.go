package tea

import (
	"bytes"
	"testing"
)

func TestInlineRepairPurgesAndReconstructs(t *testing.T) {
	var output bytes.Buffer
	r := newCursedRenderer(&output, []string{"TERM=xterm-256color"}, 13, 6)
	v := NewView("editor> abc界\n😀Z")
	v.Cursor = NewCursor(3, 1)
	r.setSyncdUpdates(true)
	r.render(v)
	r.resize(13, 6)
	if err := r.replayInline([]string{"T0 startup", "G1[0000]", "G1[0001]"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("\x1b[2J\x1b[3J\x1b[H")) {
		t.Fatalf("resize has no atomic purge/reconstruction: %q", output.String())
	}
	if bytes.Count(output.Bytes(), []byte("\x1b[?2026h")) != 1 || bytes.Count(output.Bytes(), []byte("\x1b[?2026l")) != 1 {
		t.Fatalf("repair synchronization is not one pair: %q", output.String())
	}
	for _, token := range []string{"T0 startup", "G1[0000]", "G1[0001]", "editor> abc界", "😀Z"} {
		if bytes.Count(output.Bytes(), []byte(token)) != 1 {
			t.Fatalf("missing/duplicate %q: %q", token, output.String())
		}
	}
}

// A committed row that fills every column keeps its last character. The
// renderer used to erase to the end of the line after each row. After a row
// that fills the line the cursor still sits on the last column, so the erase
// removed that column.
func TestInsertAboveKeepsFullWidthRow(t *testing.T) {
	var output bytes.Buffer
	r := newCursedRenderer(&output, []string{"TERM=xterm-256color"}, 10, 4)
	r.render(NewView("live"))
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	output.Reset()

	if err := r.insertAbove("0123456789\nabc\n" + "日本語日本語日本語日本語"); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !bytes.Contains(output.Bytes(), []byte("0123456789\r\n")) {
		t.Fatalf("full-width row must be followed by a line break, not an erase: %q", got)
	}
	if bytes.Contains(output.Bytes(), []byte("0123456789\x1b[K")) {
		t.Fatalf("erase after a full-width row removes its last column: %q", got)
	}
	if !bytes.Contains(output.Bytes(), []byte("abc\x1b[K\r\n")) {
		t.Fatalf("a short row is still erased to the end of the line: %q", got)
	}
	// 24 columns of wide characters fill two and a half lines of 10; the
	// last line is not full, so it is erased; the first two are full.
	if bytes.Contains(output.Bytes(), []byte("日本語日本語日本語日本語\x1b[K")) == false {
		t.Fatalf("a row that ends in a part line is erased to the end of the line: %q", got)
	}
}

// After a commit the hardware cursor is back in the frame at the next flush,
// even when the frame did not change. A commit leaves the cursor at the top
// of the frame, and an unchanged frame used to write nothing.
func TestFlushAfterInsertAbovePlacesCursor(t *testing.T) {
	var output bytes.Buffer
	r := newCursedRenderer(&output, []string{"TERM=xterm-256color"}, 10, 6)
	v := NewView("first\nsecond\nthird")
	v.Cursor = NewCursor(3, 1)
	r.render(v)
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	output.Reset()

	if err := r.insertAbove("committed"); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	r.render(v)
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	t.Logf("cursor placement after a commit: %d bytes %q", len(got), got)
	if got == "" {
		t.Fatal("no cursor placement after a commit")
	}
	for _, text := range []string{"first", "second", "third"} {
		if bytes.Contains(output.Bytes(), []byte(text)) {
			t.Fatalf("an unchanged frame must not be drawn again after a commit: %q", got)
		}
	}
	if len(got) > 32 {
		t.Fatalf("cursor placement should be a short move, got %d bytes: %q", len(got), got)
	}

	output.Reset()
	r.render(v)
	if err := r.flush(false); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("a second flush without a commit writes nothing: %q", output.String())
	}
}
