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
