package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// fakeSource stands in for the header, editor, status, pending and bar
// components.
// It is immutable: a test that needs another draft starts another model.
type fakeSource struct {
	draft       string
	cursorCol   int // columns after the prompt marker
	placeholder bool
	status      string
	headerL     string
	headerR     string
	pending     []string
	menu        []string
	modal       []string
	modalCur    *CursorPos
}

func (s fakeSource) Slots(width int) Slots {
	out := Slots{Status: s.status, Pending: s.pending, Menu: s.menu}
	if s.modal != nil {
		out.Editor = EditorSlot{Rows: s.modal, Modal: true, Cursor: s.modalCur}
	} else {
		text := "❯ " + s.draft
		if s.placeholder {
			text = "❯ Ask anything  ·  / commands  ·  ctrl+l model"
		}
		out.Editor = EditorSlot{Rows: []string{truncateTo(text, max(1, PromptInnerWidth(width)))}, Cursor: &CursorPos{Row: 0, Col: 2 + s.cursorCol}}
	}
	if s.headerL != "" {
		gap := max(1, width-displayWidth(s.headerL)-displayWidth(s.headerR))
		out.Header = s.headerL + strings.Repeat(" ", gap) + s.headerR
	}
	out.Bar = " " + pick(width < shortWidth, barTextShort, barText)
	return out
}

// promptBlock and replyBlock follow the block patterns of the style guide,
// without color, so goldens show the plain grid.
type promptBlock struct{ text, at string }

func (b promptBlock) Lines(rc RenderContext) []string {
	rows := wrapLines(b.text, max(1, rc.Width-4))
	out := make([]string, 0, len(rows)+1)
	for i, r := range rows {
		if i == 0 {
			line := "  ❯ " + r
			if gap := rc.Width - displayWidth(line) - displayWidth(b.at) - 2; gap > 0 {
				line += strings.Repeat(" ", gap) + b.at
			}
			out = append(out, line)
			continue
		}
		out = append(out, "    "+r)
	}
	return append(out, "")
}

type replyBlock struct{ text string }

func (b replyBlock) Lines(rc RenderContext) []string {
	rows := wrapLines(b.text, max(1, rc.Width-4))
	out := make([]string, 0, len(rows)+1)
	for i, r := range rows {
		if i == 0 {
			out = append(out, "  ◆ "+r)
			continue
		}
		out = append(out, "    "+r)
	}
	return append(out, "")
}

func newTestModel(src SlotSource) *Model {
	return New(Options{Slots: src, Profile: colorprofile.NoTTY})
}

func appendBlock(h *screenHarness, id string, b Block, stable bool) {
	h.send(AppendBlockMsg{ID: id, Block: b, Stable: stable})
}

// appendSettled adds a block and waits until the screen stops changing. A
// golden compares one exact screen, so goldens add one block at a time.
func appendSettled(h *screenHarness, id string, b Block, stable bool) {
	appendBlock(h, id, b, stable)
	h.settle()
}

func TestModelRecordsTheFirstSize(t *testing.T) {
	m := newTestModel(fakeSource{})
	require.Empty(t, m.View().Content, "no frame before the size is known")
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	require.Nil(t, cmd, "a size starts no command")
	w, h := m.Size()
	require.Equal(t, [2]int{80, 24}, [2]int{w, h})

	_, cmd = m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	require.Nil(t, cmd)
	w, h = m.Size()
	require.Equal(t, [2]int{60, 20}, [2]int{w, h})
}

func TestModelColorProfileMessageChangesTheme(t *testing.T) {
	m := New(Options{Slots: fakeSource{}, Profile: colorprofile.TrueColor})
	rev := m.Theme().Revision()
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.NoTTY})
	require.Equal(t, colorprofile.NoTTY, m.Theme().Profile())
	require.Greater(t, m.Theme().Revision(), rev)
}

func TestModelReportsInvalidBlockMessages(t *testing.T) {
	m := newTestModel(fakeSource{})
	m.Update(AppendBlockMsg{ID: "a", Block: textBlock{"x"}, Stable: true})
	require.NoError(t, m.Err())
	m.Update(AppendBlockMsg{ID: "a", Block: textBlock{"y"}, Stable: true})
	require.ErrorIs(t, m.Err(), ErrDuplicateBlock, "a failed change is visible, not dropped")
}

func TestViewUsesTheAlternateScreenAndTheMouse(t *testing.T) {
	m := newTestModel(fakeSource{placeholder: true})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	require.True(t, v.AltScreen)
	require.Equal(t, tea.MouseModeAllMotion, v.MouseMode)
	require.Len(t, strings.Split(v.Content, "\n"), 24, "one row for each terminal row")

	h := startScreen(t, newTestModel(fakeSource{placeholder: true}), 80, 24)
	require.Contains(t, h.out.all(), "\x1b[?1049h", "the program enters the alternate screen")
	h.stop()
	require.Contains(t, h.out.all(), "\x1b[?1049l", "the program leaves the alternate screen")
}

// A resize draws the screen again at the new size. The draft, the cursor, the
// focus and the follow state do not change, and no row is wider than the new
// terminal.
func TestResizeKeepsDraftFocusAndFollow(t *testing.T) {
	src := fakeSource{draft: "draft 日本語", cursorCol: displayWidth("draft 日本"), headerL: "◢◤ ask  ● leader 7c1e", headerR: "model  "}
	model := newTestModel(src)
	model.SetFocus(FocusEditor)
	h := startScreen(t, model, 80, 24)
	cursorBefore := h.emu.CursorPosition()
	require.Equal(t, 1+displayWidth("❯ draft 日本"), cursorBefore.X, "cursor after the left pad and the typed text")

	para := func(n int) string {
		return fmt.Sprintf("para%d %s end%d", n, strings.Repeat("word ", 30), n)
	}
	for i := 1; i <= 8; i++ {
		appendBlock(h, fmt.Sprintf("p%d", i), textBlock{para(i)}, true)
	}
	h.waitText("end8")
	h.settle()
	require.True(t, model.pane.Following())

	h.resize(40, 14)
	h.waitText("end8")
	h.settle()
	for _, row := range h.grid() {
		require.LessOrEqual(t, displayWidth(row), 40, "row %q is wider than the new terminal", row)
	}
	require.Equal(t, 1, h.count("draft 日本語"), "the draft is drawn once")
	editorRow := -1
	for i, row := range h.grid() {
		if strings.Contains(row, "draft 日本語") {
			editorRow = i
		}
	}
	cursorAfter := h.emu.CursorPosition()
	require.Equal(t, editorRow, cursorAfter.Y, "cursor stays on the draft row")
	require.Equal(t, cursorBefore.X, cursorAfter.X, "cursor column is unchanged")

	h.resize(120, 40)
	h.waitText("end8")
	h.settle()
	grid := h.grid()
	require.Contains(t, grid[len(grid)-1], "Tab blocks", "the bar is on the last row")

	final := h.stop().(*Model)
	require.Equal(t, FocusEditor, final.Focus())
	require.True(t, final.pane.Following(), "follow is on after the resizes")
	require.NoError(t, final.Err())
}

// A block that is taller than the pane shows its last rows while the pane
// follows the end, and a streaming block keeps showing its newest words.
func TestTallStreamingBlockShowsItsEnd(t *testing.T) {
	h := startScreen(t, newTestModel(sketchSource("")), 60, 14)
	appendBlock(h, "p", textBlock{"prompt"}, true)
	var words []string
	appendBlock(h, "tall", textBlock{"start"}, false)
	for i := 0; i < 150; i++ {
		words = append(words, fmt.Sprintf("w%03d", i))
		h.send(ReplaceBlockMsg{ID: "tall", Block: textBlock{strings.Join(words, " ")}})
	}
	h.send(FinishBlockMsg{ID: "tall"})
	h.send(AppendBlockMsg{ID: "meta", Block: textBlock{"meta-row"}, Stable: true})
	h.waitText("meta-row")
	h.waitText("w149")
	require.Zero(t, h.count("w000"), "the first words are above the window")
	require.Zero(t, h.count("prompt"), "the first block is above the window")
}

func TestScreenGoldens(t *testing.T) {
	logo := "◢◤ ask  "
	header := func(left string) (string, string) {
		return logo + left, "deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 "
	}
	history := func(h *screenHarness) {
		appendSettled(h, "p1", promptBlock{"why does TestLeaderStale fail on CI?", "14:02"}, true)
		appendSettled(h, "r1", replyBlock{"The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket after the lock probe, and the probe can take 1.2 s."}, true)
	}

	t.Run("idle_80x24", func(t *testing.T) {
		l, r := header("● leader 7c1e  ⌂ ~/src/askcore")
		h := startScreen(t, newTestModel(fakeSource{placeholder: true, cursorCol: 0, headerL: l, headerR: r}), 80, 24)
		history(h)
		h.waitText("probe can take")
		h.requireGolden()
	})

	t.Run("idle_120x40", func(t *testing.T) {
		l, r := header("● leader 7c1e  ⌂ ~/src/askcore")
		h := startScreen(t, newTestModel(fakeSource{placeholder: true, headerL: l, headerR: r}), 120, 40)
		history(h)
		h.waitText("probe can take")
		h.requireGolden()
	})

	t.Run("streaming_80x24", func(t *testing.T) {
		l, r := header("● leader 7c1e  ⌂ ~/src/askcore")
		src := fakeSource{placeholder: true, status: "┊ ◌ Working… 4s · esc to interrupt", headerL: l, headerR: r}
		h := startScreen(t, newTestModel(src), 80, 24)
		history(h)
		appendSettled(h, "p2", promptBlock{"show me the probe code", "14:03"}, true)
		appendSettled(h, "tool", textBlock{"  ◇ echo  probe code request                                          running ⠋"}, false)
		appendSettled(h, "r2", replyBlock{"The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"}, false)
		h.waitText("initialize frame")
		h.requireGolden()
	})

	t.Run("queue_80x24", func(t *testing.T) {
		l, r := header("● leader 7c1e  ⌂ ~/src/askcore")
		src := fakeSource{
			placeholder: true, status: "┊ ◌ Working… 9s · esc to interrupt", headerL: l, headerR: r,
			pending: []string{
				"┆ ⧗ steer   use the fake clock from internal/testsupport",
				"┆ ⧗ next    then run the leader tests",
				"┆ alt+↑ edit queue · dashed rail = not sent yet",
			},
		}
		h := startScreen(t, newTestModel(src), 80, 24)
		history(h)
		appendSettled(h, "r2", replyBlock{"The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"}, false)
		h.waitText("initialize frame")
		h.requireGolden()
	})

	t.Run("selector_80x24", func(t *testing.T) {
		l, r := header("● leader 7c1e  ⌂ ~/src/askcore")
		inner := 78
		row := func(s string) string { return "│" + padTo(s, inner) + "│" }
		modal := []string{
			"╭─ Select model " + strings.Repeat("─", inner-displayWidth("─ Select model ")) + "╮",
			row("  ⌕ type to filter"),
			row("   ● alibaba/deepseek-v4.1-flash                             128k  current"),
			row("   ● alibaba/qwen4-coder-plus                                256k"),
			row(" ❯ ● anthropic/claude-opus-5-5                                 1M  reasoning"),
			row("   ● anthropic/claude-sonnet-5-5                               1M  reasoning"),
			"╰" + strings.Repeat("─", inner) + "╯",
			"   enter select  ·  esc close  ·  your draft is kept",
		}
		h := startScreen(t, newTestModel(fakeSource{modal: modal, modalCur: &CursorPos{Row: 1, Col: 6}, headerL: l, headerR: r}), 80, 24)
		history(h)
		h.waitText("Select model")
		h.requireGolden()
	})

	t.Run("narrow_40x14", func(t *testing.T) {
		src := fakeSource{placeholder: true, status: "┊ ◌ Working… 3s · esc", headerL: "◢◤ ask ● 7c1e", headerR: "deepseek-v4.1-flash 18% "}
		h := startScreen(t, newTestModel(src), 40, 14)
		appendSettled(h, "p1", promptBlock{"giải thích lỗi\n日本語のテスト", "14:05"}, true)
		appendSettled(h, "r1", replyBlock{"The 日本語 test fails because each wide character uses two columns."}, true)
		h.waitText("two columns.")
		h.requireGolden()
	})
}

// A pane row that fills every column keeps its last character.
func TestPaneRowKeepsLastCharacter(t *testing.T) {
	h := startScreen(t, newTestModel(fakeSource{placeholder: true}), 40, 12)
	row := strings.Repeat("ab", 19) + "XZ"
	require.Equal(t, 40, displayWidth(row))
	appendBlock(h, "wide", rawBlock{row}, true)
	appendBlock(h, "after", textBlock{"after"}, true)
	h.waitText("after")
	require.Equal(t, 1, h.count(row))
}

// The cursor is on the draft row after the pane changed.
func TestCursorStaysOnTheDraftWhileBlocksArrive(t *testing.T) {
	src := fakeSource{draft: "draft", cursorCol: 3}
	h := startScreen(t, newTestModel(src), 40, 12)
	appendBlock(h, "one", textBlock{"first row"}, true)
	appendBlock(h, "two", textBlock{"second row"}, true)
	h.waitText("second row")
	h.settle()
	editor := -1
	for i, row := range h.grid() {
		if strings.Contains(row, "draft") {
			editor = i
		}
	}
	require.GreaterOrEqual(t, editor, 0)
	cur := h.emu.CursorPosition()
	require.Equal(t, editor, cur.Y, "cursor is on the draft row")
	require.Equal(t, 1+displayWidth("❯ dra"), cur.X)
}

// A row from a block that is wider than the terminal is wrapped, not cut and
// not left to the terminal, so the row count of the pane stays right.
func TestWideBlockRowsAreWrapped(t *testing.T) {
	h := startScreen(t, newTestModel(fakeSource{placeholder: true}), 30, 12)
	appendBlock(h, "long", rawBlock{strings.Repeat("x", 70)}, true)
	appendBlock(h, "after", textBlock{"after"}, true)
	h.waitText("after")
	for _, row := range h.grid() {
		require.LessOrEqual(t, displayWidth(row), 30)
	}
	var total int
	for _, row := range h.grid() {
		total += strings.Count(row, "x")
	}
	require.Equal(t, 70, total, "no character is lost")
}

// rawBlock returns its text as one row without wrapping.
type rawBlock struct{ row string }

func (b rawBlock) Lines(RenderContext) []string { return []string{b.row} }

// Mouse messages that no part handles change nothing.
func TestUnhandledMouseMessagesAreIgnored(t *testing.T) {
	m := newTestModel(fakeSource{draft: "abc", cursorCol: 3})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(AppendBlockMsg{ID: "a", Block: textBlock{"alpha"}, Stable: true})
	before := m.View().Content
	m.Update(tea.MouseClickMsg{X: 3, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 4, Y: 5})
	m.Update(tea.MouseWheelMsg{X: 4, Y: 5, Button: tea.MouseWheelUp})
	m.Update(tea.MouseReleaseMsg{X: 4, Y: 5, Button: tea.MouseLeft})
	require.Equal(t, before, m.View().Content)
	require.True(t, m.pane.Following())
	require.NoError(t, m.Err())
}

// A resize while the pane is scrolled up keeps follow off and keeps the same
// block row at the top of the window.
func TestResizeKeepsAScrolledWindow(t *testing.T) {
	m := newTestModel(fakeSource{placeholder: true})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 0; i < 40; i++ {
		m.Update(AppendBlockMsg{ID: fmt.Sprintf("b%02d", i), Block: textBlock{fmt.Sprintf("block-%02d", i)}, Stable: true})
	}
	m.View()
	require.True(t, m.pane.Following())
	m.pane.ScrollBy(-30)
	require.False(t, m.pane.Following())
	top := func() string {
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		return rows[2]
	}
	before := top()
	require.NotEmpty(t, before)

	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	require.Equal(t, before, top(), "the same block is at the top of the window")
	require.False(t, m.pane.Following(), "a resize does not turn follow on")
	m.pane.ScrollToBottom()
	require.True(t, m.pane.Following())
}
