package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func plainRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

func numbered(prefix string, n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return rows
}

// Rows go to the slots in the order of the design, and the last slots lose
// rows first.
func TestLayoutSlotPriority(t *testing.T) {
	full := demand{editor: 3, status: 1, footer: 1, pending: 5, menu: 12, tail: 100}

	t.Run("hand checked heights", func(t *testing.T) {
		cases := []struct {
			height int
			want   alloc
		}{
			{24, alloc{editor: 3, status: 1, footer: 1, pending: 3, menu: 8, tail: 8}},
			{10, alloc{editor: 3, status: 1, footer: 1, pending: 3, menu: 2}},
			{8, alloc{editor: 2, status: 1, footer: 1, pending: 3, menu: 1}},
			{7, alloc{editor: 2, status: 1, pending: 3, menu: 1}},
			{4, alloc{editor: 1, status: 1, pending: 2}},
			{3, alloc{editor: 1, status: 1}},
		}
		for _, c := range cases {
			require.Equal(t, c.want, allocate(c.height, full), "height %d", c.height)
		}
	})

	t.Run("invariants for heights 3 to 40", func(t *testing.T) {
		for h := 3; h <= 40; h++ {
			a := allocate(h, full)
			total := a.editor + a.status + a.footer + a.pending + a.menu + a.tail
			require.LessOrEqual(t, total, h, "height %d", h)
			require.GreaterOrEqual(t, a.editor, 1, "editor keeps at least one row at height %d", h)
			require.LessOrEqual(t, a.editor, max(1, h*30/100), "editor cap at height %d", h)
			require.Equal(t, 1, a.status, "height %d", h)
			require.LessOrEqual(t, a.pending, 3)
			require.LessOrEqual(t, a.menu, 8)
			if h < 8 {
				require.Zero(t, a.footer, "footer hidden at height %d", h)
			} else {
				require.Equal(t, 1, a.footer, "height %d", h)
			}
			if h < 4 {
				require.Equal(t, alloc{editor: a.editor, status: a.status}, a, "only editor and status at height %d", h)
			}
			// A lower slot gets rows only when every higher slot has its maximum.
			if a.menu > 0 {
				require.Equal(t, 3, a.pending, "menu rows only after pending is full at height %d", h)
			}
			if a.tail > 0 {
				require.Equal(t, 8, a.menu, "tail rows only after the menu is full at height %d", h)
			}
			// The tail takes the whole rest, so no row is wasted.
			if a.tail > 0 {
				require.Equal(t, h, total, "height %d", h)
			}
		}
	})

	t.Run("empty slots use no rows", func(t *testing.T) {
		a := allocate(24, demand{editor: 3, tail: 5})
		require.Equal(t, alloc{editor: 3, tail: 5}, a)
		a = allocate(24, demand{editor: 3, status: 1, footer: 1})
		require.Equal(t, alloc{editor: 3, status: 1, footer: 1}, a)
		a = allocate(24, demand{})
		require.Equal(t, alloc{}, a)
	})

	t.Run("editor grows to thirty percent of the height", func(t *testing.T) {
		require.Equal(t, 7, allocate(24, demand{editor: 20}).editor)
		require.Equal(t, 12, allocate(40, demand{editor: 20}).editor)
		require.Equal(t, 3, allocate(24, demand{editor: 3}).editor)
	})

	t.Run("a modal slot is capped at twelve rows and keeps three", func(t *testing.T) {
		require.Equal(t, 12, allocate(40, demand{editor: 30, modal: true}).editor)
		require.Equal(t, 10, allocate(24, demand{editor: 10, modal: true}).editor)
		require.Equal(t, 5, allocate(5, demand{editor: 10, modal: true, status: 1}).editor, "a modal slot may use every row")
		require.Equal(t, 3, allocate(3, demand{editor: 10, modal: true}).editor)
	})
}

func TestComposeFrameDrawsEditorBox(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	f := composeFrame(Slots{Editor: EditorSlot{Rows: []string{"❯ hello"}, Cursor: &CursorPos{Row: 0, Col: 7}}}, nil, 20, 24, th)
	require.Equal(t, []string{
		"╭──────────────────╮",
		"│ ❯ hello          │",
		"╰──────────────────╯",
	}, plainRows(f.Rows))
	require.Equal(t, &CursorPos{Row: 1, Col: 2 + 7}, f.Cursor, "cursor is inside the box, after the left border and a space")
}

func TestComposeFrameOrderAndCursor(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	slots := Slots{
		Editor:  EditorSlot{Rows: []string{"❯ x"}, Cursor: &CursorPos{Row: 0, Col: 3}},
		Status:  "status",
		Footer:  "footer",
		Pending: []string{"p1", "p2"},
		Menu:    []string{"m1"},
	}
	f := composeFrame(slots, []string{"t1", "t2"}, 12, 24, th)
	rows := plainRows(f.Rows)
	require.Equal(t, []string{"t1", "t2", "status", "p1", "p2", "m1"}, rows[:6], "tail, status, pending, menu come before the editor")
	require.True(t, strings.HasPrefix(rows[6], "╭"))
	require.Equal(t, "footer", rows[len(rows)-1])
	require.Len(t, rows, 6+3+1)
	require.Equal(t, &CursorPos{Row: 7, Col: 5}, f.Cursor)
}

func TestComposeFrameEditorScrollsInside(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	rows := numbered("line ", 10)
	// Height 24 allows 7 rows: 5 inside the box.
	f := composeFrame(Slots{Editor: EditorSlot{Rows: rows, Cursor: &CursorPos{Row: 9, Col: 1}}}, nil, 20, 24, th)
	got := plainRows(f.Rows)
	require.Len(t, got, 7)
	require.Contains(t, got[1], "line 6")
	require.Contains(t, got[5], "line 10")
	require.Equal(t, &CursorPos{Row: 5, Col: 3}, f.Cursor)

	f = composeFrame(Slots{Editor: EditorSlot{Rows: rows, Cursor: &CursorPos{Row: 0, Col: 1}}}, nil, 20, 24, th)
	got = plainRows(f.Rows)
	require.Contains(t, got[1], "line 1 ")
	require.Contains(t, got[5], "line 5")
	require.Equal(t, &CursorPos{Row: 1, Col: 3}, f.Cursor)
}

func TestComposeFrameSmallHeights(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	slots := Slots{
		Editor: EditorSlot{Rows: []string{"❯ x"}, Cursor: &CursorPos{Row: 0, Col: 3}},
		Status: "status", Footer: "footer", Pending: []string{"p"}, Menu: []string{"m"},
	}
	tail := []string{"t1", "t2", "t3"}

	rows := plainRows(composeFrame(slots, tail, 20, 3, th).Rows)
	require.Equal(t, []string{"status", "❯ x"}, rows, "under 4 rows only status and the editor stay")

	rows = plainRows(composeFrame(slots, tail, 20, 7, th).Rows)
	require.NotContains(t, rows, "footer", "footer hidden under 8 rows")
	require.LessOrEqual(t, len(rows), 7)

	rows = plainRows(composeFrame(slots, tail, 20, 12, th).Rows)
	require.Equal(t, "footer", rows[len(rows)-1])
	require.LessOrEqual(t, len(rows), 12)
}

func TestComposeFrameTailShowsLastRows(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	slots := Slots{Editor: EditorSlot{Rows: []string{"x"}}, Footer: "footer"}
	f := composeFrame(slots, numbered("t", 40), 20, 12, th)
	rows := plainRows(f.Rows)
	require.Len(t, rows, 11, "the frame leaves the first screen row free")
	require.Equal(t, "t34", rows[0], "the tail keeps its last rows")
	require.Equal(t, "t40", rows[6])
}

func TestComposeFrameOverflowMarkers(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	slots := Slots{
		Editor:  EditorSlot{Rows: []string{"x"}},
		Pending: numbered("p", 5),
		Menu:    numbered("m", 12),
	}
	rows := plainRows(composeFrame(slots, nil, 30, 40, th).Rows)
	require.Equal(t, []string{"p1", "p2", "┆ +3 more"}, rows[:3])
	require.Equal(t, "m1", rows[3])
	require.Equal(t, "m7", rows[9])
	require.Equal(t, "… 5 more", rows[10])
}

func TestComposeFrameModalReplacesEditor(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	sel := []string{"╭─ Select ─╮", "│ ⌕ ▌       │", "╰──────────╯", " enter select"}
	slots := Slots{
		Editor: EditorSlot{Rows: sel, Modal: true, Cursor: &CursorPos{Row: 1, Col: 4}},
		Footer: "footer",
	}
	f := composeFrame(slots, nil, 20, 24, th)
	require.Equal(t, append(append([]string{}, sel...), "footer"), plainRows(f.Rows), "modal rows are not boxed again")
	require.Equal(t, &CursorPos{Row: 1, Col: 4}, f.Cursor)
}

func TestComposeFrameNeverExceedsWidth(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	long := strings.Repeat("日本語", 30)
	slots := Slots{
		Editor: EditorSlot{Rows: []string{long}},
		Status: long, Footer: long,
		Pending: []string{long}, Menu: []string{long},
	}
	for _, w := range []int{1, 2, 5, 17, 40, 80} {
		f := composeFrame(slots, []string{long}, w, 24, th)
		for _, r := range f.Rows {
			require.LessOrEqual(t, displayWidth(r), w, "width %d row %q", w, ansi.Strip(r))
		}
	}
}

func TestComposeFrameHidesCursorOutsideVisibleRows(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	sel := numbered("row", 30)
	f := composeFrame(Slots{Editor: EditorSlot{Rows: sel, Modal: true, Cursor: &CursorPos{Row: 25, Col: 1}}}, nil, 20, 24, th)
	require.Nil(t, f.Cursor)
}

func TestComposeFrameLeavesOneScreenRowFree(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	slots := Slots{Editor: EditorSlot{Rows: []string{"x"}}, Status: "s", Footer: "f", Pending: numbered("p", 9), Menu: numbered("m", 20)}
	for h := 4; h <= 40; h++ {
		f := composeFrame(slots, numbered("t", 100), 30, h, th)
		require.LessOrEqual(t, len(f.Rows), h, "height %d", h)
		a := allocate(h, demand{editor: 3, status: 1, footer: 1, pending: 9, menu: 20, tail: 100})
		if a.tail > 0 {
			require.Less(t, len(f.Rows), h, "height %d: a tail that overflows still leaves a free row", h)
		}
	}
}
