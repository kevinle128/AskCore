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

// paneOf is a pane that shows the last rows of a list, as a pane that follows
// the end does.
func paneOf(rows []string) func(n int) []string {
	return func(n int) []string {
		if n >= len(rows) {
			return rows
		}
		return rows[len(rows)-n:]
	}
}

// rowKinds names the screen rows of a frame by their content, so a test can
// check the order of the parts without caring for their text.
func rowKinds(rows []string) string {
	var out []string
	for _, r := range rows {
		switch {
		case r != "" && strings.Trim(r, "─") == "":
			out = append(out, "rule")
		case r == "":
			out = append(out, "-")
		default:
			out = append(out, r)
		}
	}
	return strings.Join(out, ",")
}

// Rows go to the parts in a fixed order, and the pane takes what is left.
func TestAllocateScreenRows(t *testing.T) {
	full := demand{prompt: 1, status: 1, queue: 5, menu: 12}

	t.Run("hand checked heights", func(t *testing.T) {
		cases := []struct {
			height int
			want   alloc
		}{
			{40, alloc{header: 1, headerRule: 1, pane: 21, queue: 3, status: 1, promptRules: 2, prompt: 1, menu: 8, footer: 1, bar: 1}},
			{24, alloc{header: 1, headerRule: 1, pane: 5, queue: 3, status: 1, promptRules: 2, prompt: 1, menu: 8, footer: 1, bar: 1}},
			{9, alloc{header: 1, headerRule: 1, pane: 1, status: 1, promptRules: 2, prompt: 1, footer: 1, bar: 1}},
			// Under 9 rows the rules are gone; the queue rows come before the
			// pane rows that are left.
			{8, alloc{header: 1, pane: 1, queue: 2, status: 1, prompt: 1, footer: 1, bar: 1}},
			{7, alloc{header: 1, pane: 1, queue: 1, status: 1, prompt: 1, footer: 1, bar: 1}},
			{5, alloc{header: 1, pane: 1, prompt: 1, footer: 1, bar: 1}},
			// Under 5 rows the header row gives way first, then the pane, then
			// the footer row.
			{4, alloc{pane: 1, prompt: 1, footer: 1, bar: 1}},
			{3, alloc{prompt: 1, footer: 1, bar: 1}},
			{2, alloc{prompt: 1, bar: 1}},
			{1, alloc{prompt: 1}},
		}
		for _, c := range cases {
			require.Equal(t, c.want, allocate(c.height, full), "height %d", c.height)
		}
	})

	t.Run("invariants for heights 1 to 60", func(t *testing.T) {
		for h := 1; h <= 60; h++ {
			a := allocate(h, full)
			require.Equal(t, h, a.total(), "height %d: every row has an owner", h)
			require.GreaterOrEqual(t, a.prompt, 1, "the prompt keeps a row at height %d", h)
			if h >= 2 {
				require.Equal(t, 1, a.bar, "the bar stays at height %d", h)
			}
			if h >= 3 {
				require.Equal(t, 1, a.footer, "the footer row stays at height %d", h)
			}
			if h >= 4 {
				require.GreaterOrEqual(t, a.pane, 1, "the pane keeps a row at height %d", h)
			}
			if h >= 5 {
				require.Equal(t, 1, a.header, "the header row stays at height %d", h)
			}
			if h < 9 {
				require.Zero(t, a.headerRule+a.promptRules, "the rules go first under 9 rows, height %d", h)
			} else {
				require.Equal(t, 3, a.headerRule+a.promptRules, "height %d", h)
			}
			require.LessOrEqual(t, a.queue, 3)
			require.LessOrEqual(t, a.menu, 8)
			require.LessOrEqual(t, a.prompt+a.menu, max(1, h/2), "the prompt pane is at most half the screen at height %d", h)
		}
	})

	t.Run("the status row goes after the rules", func(t *testing.T) {
		require.Equal(t, 1, allocate(6, demand{prompt: 1, status: 1}).status)
		require.Zero(t, allocate(5, demand{prompt: 1, status: 1}).status, "five rows hold the header, the pane, the prompt, the footer row and the bar")
	})

	t.Run("empty parts use no rows", func(t *testing.T) {
		a := allocate(24, demand{prompt: 1})
		require.Equal(t, alloc{header: 1, headerRule: 1, pane: 17, promptRules: 2, prompt: 1, footer: 1, bar: 1}, a)
	})

	t.Run("the prompt pane grows to half of the height", func(t *testing.T) {
		require.Equal(t, 12, allocate(24, demand{prompt: 12}).prompt)
		require.Equal(t, 12, allocate(24, demand{prompt: 13}).prompt, "a 13th line scrolls inside the pane")
		require.Equal(t, 6, allocate(24, demand{prompt: 12}).pane)
		require.Equal(t, 20, allocate(40, demand{prompt: 30}).prompt)
		require.Equal(t, 3, allocate(24, demand{prompt: 3}).prompt)
	})

	t.Run("a modal slot has the same cap", func(t *testing.T) {
		require.Equal(t, 12, allocate(24, demand{prompt: 14}).prompt)
		require.Equal(t, 7, allocate(14, demand{prompt: 14}).prompt)
		require.Equal(t, 10, allocate(24, demand{prompt: 10}).prompt)
	})

	t.Run("menu rows share the half with the prompt rows", func(t *testing.T) {
		a := allocate(24, demand{prompt: 10, menu: 8})
		require.Equal(t, 10, a.prompt)
		require.Equal(t, 2, a.menu)
	})
}

func TestPromptSlotRows(t *testing.T) {
	require.Equal(t, 12, promptSlotRows(24))
	require.Equal(t, 7, promptSlotRows(14))
	require.Equal(t, 1, promptSlotRows(1))
	require.Equal(t, 1, promptSlotRows(0))
}

func (a alloc) total() int {
	return a.header + a.headerRule + a.pane + a.queue + a.status + a.promptRules + a.prompt + a.menu + a.footer + a.bar
}

func testSlots() Slots {
	return Slots{
		Header: "header",
		Footer: "footer",
		Editor: EditorSlot{Rows: []string{"❯ x"}, Cursor: &CursorPos{Row: 0, Col: 3}},
		Bar:    "bar",
	}
}

func TestComposeFrameScreenParts(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)

	t.Run("80x24 with every part", func(t *testing.T) {
		s := testSlots()
		s.Status = "status"
		s.Pending = []string{"q1", "q2"}
		s.Menu = []string{"m1"}
		f := composeFrame(s, paneOf(numbered("b", 100)), 80, 24, th)
		rows := plainRows(f.Rows)
		require.Len(t, rows, 24)
		require.Equal(t, "header", rows[0])
		require.Equal(t, strings.Repeat("─", 80), rows[1], "the rule under the header")
		require.Equal(t, "b88", rows[2], "the pane shows its last 13 rows")
		require.Equal(t, "b100", rows[14])
		require.Equal(t, FrameGeo{PaneTop: 2, PaneRows: 13, PromptTop: 20, PromptRows: 1}, f.Geo)
		require.Equal(t, []string{"q1", "q2"}, rows[15:17], "the queue is above the status")
		require.Equal(t, "status", rows[17], "the status row is right above the rule")
		require.Equal(t, strings.Repeat("─", 80), rows[18], "the rule above the prompt pane")
		require.Equal(t, "m1", rows[19], "the menu is inside the prompt pane, above the draft")
		require.Equal(t, " ❯ x", rows[20])
		require.Equal(t, strings.Repeat("─", 80), rows[21], "the rule under the prompt pane")
		require.Equal(t, "footer", rows[22], "the footer row is under the lower rule")
		require.Equal(t, "bar", rows[23])
		require.Equal(t, &CursorPos{Row: 20, Col: 4}, f.Cursor, "the cursor is in the prompt pane, after the left pad")
	})

	t.Run("80x24 with a 12 line draft", func(t *testing.T) {
		s := testSlots()
		s.Editor = EditorSlot{Rows: numbered("line ", 12), Cursor: &CursorPos{Row: 11, Col: 1}}
		f := composeFrame(s, paneOf(numbered("b", 100)), 80, 24, th)
		rows := plainRows(f.Rows)
		require.Len(t, rows, 24)
		require.Equal(t, "b95", rows[2], "the pane has 6 rows")
		require.Equal(t, "b100", rows[7])
		require.Equal(t, strings.Repeat("─", 80), rows[8])
		require.Equal(t, " line 1", rows[9])
		require.Equal(t, " line 12", rows[20])
		require.Equal(t, strings.Repeat("─", 80), rows[21])
		require.Equal(t, &CursorPos{Row: 20, Col: 2}, f.Cursor)
	})

	t.Run("80x24 with a 13 line draft scrolls inside the pane", func(t *testing.T) {
		s := testSlots()
		s.Editor = EditorSlot{Rows: numbered("line ", 13), Cursor: &CursorPos{Row: 12, Col: 1}}
		rows := plainRows(composeFrame(s, paneOf(numbered("b", 100)), 80, 24, th).Rows)
		require.Equal(t, " line 2", rows[9])
		require.Equal(t, " line 13", rows[20])
	})

	t.Run("40x14", func(t *testing.T) {
		f := composeFrame(testSlots(), paneOf(numbered("b", 3)), 40, 14, th)
		rows := plainRows(f.Rows)
		require.Len(t, rows, 14)
		require.Equal(t, "header", rows[0])
		require.Equal(t, strings.Repeat("─", 40), rows[1])
		require.Equal(t, "b3", rows[4])
		require.Equal(t, strings.Repeat("─", 40), rows[9])
		require.Equal(t, " ❯ x", rows[10])
		require.Equal(t, strings.Repeat("─", 40), rows[11])
		require.Equal(t, "footer", rows[12])
		require.Equal(t, "bar", rows[13])
	})

	t.Run("28x14", func(t *testing.T) {
		rows := plainRows(composeFrame(testSlots(), paneOf(numbered("b", 3)), 28, 14, th).Rows)
		require.Len(t, rows, 14)
		for _, r := range rows {
			require.LessOrEqual(t, displayWidth(r), 28)
		}
		require.Equal(t, strings.Repeat("─", 28), rows[1])
		require.Equal(t, strings.Repeat("─", 28), rows[9])
	})

	t.Run("8 rows have no rules", func(t *testing.T) {
		s := testSlots()
		s.Status = "status"
		rows := plainRows(composeFrame(s, paneOf(numbered("b", 100)), 40, 8, th).Rows)
		require.Equal(t, []string{"header", "b98", "b99", "b100", "status", " ❯ x", "footer", "bar"}, rows)
	})

	t.Run("9 rows with a status show the three rules", func(t *testing.T) {
		s := testSlots()
		s.Status = "status"
		rows := plainRows(composeFrame(s, paneOf(numbered("b", 100)), 80, 9, th).Rows)
		rule := strings.Repeat("─", 80)
		require.Equal(t, []string{"header", rule, "b100", "status", rule, " ❯ x", rule, "footer", "bar"}, rows)
	})

	t.Run("4 rows lose the header row first", func(t *testing.T) {
		rows := plainRows(composeFrame(testSlots(), paneOf(numbered("b", 100)), 80, 4, th).Rows)
		require.Equal(t, []string{"b100", " ❯ x", "footer", "bar"}, rows)
	})

	t.Run("3 rows keep the prompt, the footer row and the bar", func(t *testing.T) {
		rows := plainRows(composeFrame(testSlots(), paneOf(numbered("b", 100)), 80, 3, th).Rows)
		require.Equal(t, []string{" ❯ x", "footer", "bar"}, rows)
	})

	t.Run("a short pane callback is padded to its rows", func(t *testing.T) {
		rows := composeFrame(testSlots(), func(int) []string { return nil }, 40, 12, th).Rows
		require.Len(t, rows, 12)
	})

	t.Run("a screen without a pane asks for no rows", func(t *testing.T) {
		var asked int
		composeFrame(testSlots(), func(n int) []string { asked = n; return nil }, 40, 3, th)
		require.Zero(t, asked)
	})
}

func TestComposeFrameEditorScrollsWithTheCursor(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	rows := numbered("line ", 30)
	s := testSlots()
	s.Editor = EditorSlot{Rows: rows, Cursor: &CursorPos{Row: 0, Col: 1}}
	got := plainRows(composeFrame(s, paneOf(nil), 20, 24, th).Rows)
	require.Equal(t, " line 1", got[9])
	require.Equal(t, " line 12", got[20])

	s.Editor.Cursor = &CursorPos{Row: 29, Col: 1}
	f := composeFrame(s, paneOf(nil), 20, 24, th)
	got = plainRows(f.Rows)
	require.Equal(t, " line 19", got[9])
	require.Equal(t, " line 30", got[20])
	require.Equal(t, &CursorPos{Row: 20, Col: 2}, f.Cursor)
}

func TestComposeFrameOverflowMarkers(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	s := testSlots()
	s.Pending = numbered("p", 5)
	s.Menu = numbered("m", 12)
	rows := plainRows(composeFrame(s, paneOf(nil), 30, 40, th).Rows)
	i := indexOf(rows, "p1")
	require.Equal(t, []string{"p1", "p2", "┆ +3 more"}, rows[i:i+3])
	j := indexOf(rows, "m1")
	require.Equal(t, "m7", rows[j+6])
	require.Equal(t, "… 5 more", rows[j+7])
}

func indexOf(rows []string, s string) int {
	for i, r := range rows {
		if r == s {
			return i
		}
	}
	return -1
}

func TestComposeFrameModalTakesThePromptSlot(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	sel := []string{"╭─ Select ─╮", "│ ⌕ ▌       │", "╰──────────╯", " enter select"}
	s := testSlots()
	s.Editor = EditorSlot{Rows: sel, Modal: true, Cursor: &CursorPos{Row: 1, Col: 4}}
	f := composeFrame(s, paneOf(numbered("b", 100)), 20, 24, th)
	rows := plainRows(f.Rows)
	require.Len(t, rows, 24)
	require.Equal(t, strings.Repeat("─", 20), rows[16])
	require.Equal(t, sel, rows[17:21], "modal rows are between the rules and are not padded or boxed again")
	require.Equal(t, strings.Repeat("─", 20), rows[21])
	require.Equal(t, "footer", rows[22], "the footer row stays under the selector")
	require.Equal(t, "bar", rows[23])
	require.Equal(t, &CursorPos{Row: 18, Col: 4}, f.Cursor)
}

func TestComposeFrameModalKeepsFirstRowsAtTheCap(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	s := testSlots()
	s.Editor = EditorSlot{Rows: numbered("row", 30), Modal: true, Cursor: &CursorPos{Row: 25, Col: 1}}
	f := composeFrame(s, paneOf(nil), 20, 24, th)
	require.Nil(t, f.Cursor, "a cursor outside the visible rows is hidden")
	rows := plainRows(f.Rows)
	require.Equal(t, "row1", rows[9])
	require.Equal(t, "row12", rows[20])
}

func TestComposeFrameNeverExceedsWidthOrHeight(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	long := strings.Repeat("日本語", 30)
	s := Slots{
		Header: long, Footer: long, Bar: long, Status: long,
		Editor:  EditorSlot{Rows: []string{long}},
		Pending: []string{long}, Menu: []string{long},
	}
	for _, w := range []int{1, 2, 5, 17, 40, 80} {
		for _, h := range []int{1, 3, 4, 5, 8, 9, 14, 24} {
			f := composeFrame(s, paneOf([]string{long, long}), w, h, th)
			require.Len(t, f.Rows, h, "width %d height %d", w, h)
			for _, r := range f.Rows {
				require.LessOrEqual(t, displayWidth(r), w, "width %d row %q", w, ansi.Strip(r))
			}
		}
	}
}

func TestComposeFrameRowOrder(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	s := testSlots()
	s.Status = "status"
	f := composeFrame(s, paneOf([]string{"b1", "b2"}), 12, 12, th)
	require.Equal(t, "header,rule,b1,b2,-,-,status,rule, ❯ x,rule,footer,bar", rowKinds(plainRows(f.Rows)))
}
