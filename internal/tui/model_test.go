package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

const purgeSequence = "\x1b[2J\x1b[3J\x1b[H"

// fakeSource stands in for the editor, status, pending and footer components.
// It is immutable: a test that needs another draft starts another model.
type fakeSource struct {
	draft       string
	cursorCol   int // columns after the prompt marker
	placeholder bool
	status      string
	footerL     string
	footerR     string
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
		out.Editor = EditorSlot{Rows: []string{truncateTo(text, max(1, BoxInnerWidth(width)))}, Cursor: &CursorPos{Row: 0, Col: 2 + s.cursorCol}}
	}
	if s.footerL != "" {
		gap := max(1, width-displayWidth(s.footerL)-displayWidth(s.footerR))
		out.Footer = s.footerL + strings.Repeat(" ", gap) + s.footerR
	}
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

type headerBlock struct{ text string }

func (b headerBlock) Lines(rc RenderContext) []string {
	return []string{truncateTo(b.text, rc.Width), ""}
}

func newTestModel(src SlotSource) *Model {
	return New(Options{Slots: src, Profile: colorprofile.NoTTY})
}

func appendBlock(h *screenHarness, id string, b Block, stable bool) {
	h.send(AppendBlockMsg{ID: id, Block: b, Stable: stable})
}

func TestModelFirstSizeDoesNotRebuild(t *testing.T) {
	m := newTestModel(fakeSource{})
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24, Generation: 1})
	require.Nil(t, cmd, "the first size only records the size; nothing is on screen to repair")
	w, h := m.Size()
	require.Equal(t, [2]int{80, 24}, [2]int{w, h})

	_, cmd = m.Update(tea.WindowSizeMsg{Width: 60, Height: 20, Generation: 2})
	require.NotNil(t, cmd, "a later size waits for the settle delay")
}

func TestModelStaleSettleIsIgnored(t *testing.T) {
	m := newTestModel(fakeSource{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24, Generation: 1})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20, Generation: 2})
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 20, Generation: 3})

	_, cmd := m.Update(settleMsg{generation: 2})
	require.Nil(t, cmd, "a newer size is pending, so the older settle does nothing")
	_, cmd = m.Update(settleMsg{generation: 3})
	require.NotNil(t, cmd, "the latest size starts the rebuild")
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

// Stable blocks reach the scrollback once and in order, even when many arrive
// in one burst. The test reads the real renderer output.
func TestCommitFrontierOrderedOnceThroughRenderer(t *testing.T) {
	h := startScreen(t, newTestModel(fakeSource{placeholder: true}), 60, 12)

	appendBlock(h, "first", textBlock{"first-mutable"}, false)
	appendBlock(h, "second", textBlock{"second-stable"}, true)
	appendBlock(h, "third", textBlock{"third-stable"}, true)
	h.waitText("third-stable")
	time.Sleep(150 * time.Millisecond)
	for _, row := range h.scrollback() {
		require.Empty(t, strings.TrimSpace(row), "nothing passes the mutable block, but %q is in the scrollback", row)
	}

	for i := 0; i < 30; i++ {
		appendBlock(h, fmt.Sprintf("burst-%02d", i), textBlock{fmt.Sprintf("burst-line-%02d", i)}, true)
	}
	h.send(FinishBlockMsg{ID: "first"})
	tokens := []string{"first-mutable", "second-stable", "third-stable"}
	for i := 0; i < 30; i++ {
		tokens = append(tokens, fmt.Sprintf("burst-line-%02d", i))
	}
	h.requireEachOnce(tokens...)
	require.NotEmpty(t, h.scrollback(), "committed blocks moved to the scrollback")
}

func TestResizeRebuildKeepsDraftAndFocus(t *testing.T) {
	src := fakeSource{draft: "draft 日本語", cursorCol: displayWidth("draft 日本"), footerL: "  ● leader 7c1e", footerR: "model  "}
	model := newTestModel(src)
	model.SetFocus(FocusEditor)
	h := startScreen(t, model, 80, 24)
	// The cursor is read before any commit. The renderer does not put the
	// cursor back after a commit when the next frame is identical, so only a
	// frame that was drawn in full tells where the cursor is.
	cursorBefore := h.emu.CursorPosition()
	require.Equal(t, 2+displayWidth("❯ draft 日本"), cursorBefore.X, "cursor after the box border, a space and the typed text")

	para := func(n int) string {
		return fmt.Sprintf("para%d %s end%d", n, strings.Repeat("word ", 30), n)
	}
	for i := 1; i <= 3; i++ {
		appendBlock(h, fmt.Sprintf("p%d", i), textBlock{para(i)}, true)
	}
	h.waitText("end3")
	h.waitText("draft 日本語")

	editorRow := func() int {
		for i, row := range h.grid() {
			if strings.Contains(row, "draft 日本語") {
				return i
			}
		}
		return -1
	}
	h.settle()

	// A burst of sizes is one change: one purge, after the settle delay.
	h.resize(70, 20)
	h.resize(55, 16)
	h.resize(40, 14)
	require.Zero(t, strings.Count(h.out.all(), purgeSequence), "no repair before the size settles")

	h.waitFor("one rebuild", func(h *screenHarness) bool { return strings.Count(h.out.all(), purgeSequence) == 1 })
	h.waitText("end3")
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, strings.Count(h.out.all(), purgeSequence), "one rebuild for the whole burst")

	for _, token := range []string{"para1", "end1", "para2", "end2", "para3", "end3"} {
		require.Equal(t, 1, h.count(token), "%s is drawn once after the rebuild:\n%s", token, strings.Join(h.text(), "\n"))
	}
	for _, row := range h.text() {
		require.LessOrEqual(t, displayWidth(row), 40, "row %q is wider than the new terminal", row)
	}

	h.settle()
	require.Equal(t, 1, h.count("draft 日本語"), "the draft is drawn once")
	cursorAfter := h.emu.CursorPosition()
	require.Equal(t, editorRow(), cursorAfter.Y, "cursor stays on the draft row")
	require.Equal(t, cursorBefore.X, cursorAfter.X, "cursor column is unchanged")

	final := h.stop().(*Model)
	require.Equal(t, FocusEditor, final.Focus())
	require.NoError(t, final.Err())
}

func TestRebuildMessageRedrawsCommittedOnce(t *testing.T) {
	h := startScreen(t, newTestModel(fakeSource{placeholder: true}), 60, 20)
	appendBlock(h, "a", textBlock{"alpha-text"}, true)
	appendBlock(h, "b", textBlock{"beta-text"}, true)
	h.waitText("beta-text")
	require.Zero(t, strings.Count(h.out.all(), purgeSequence))

	h.send(RebuildMsg{})
	h.waitFor("rebuild", func(h *screenHarness) bool { return strings.Count(h.out.all(), purgeSequence) == 1 })
	h.waitText("beta-text")
	require.Equal(t, 1, h.count("alpha-text"))
	require.Equal(t, 1, h.count("beta-text"))

	// New output after a rebuild still commits once, below the old lines.
	appendBlock(h, "c", textBlock{"gamma-text"}, true)
	h.waitText("gamma-text")
	require.Equal(t, 1, h.count("gamma-text"))
	require.Equal(t, 1, strings.Count(h.out.all(), purgeSequence))
}

// A rebuild that runs while stable blocks wait for a commit draws those blocks
// itself. They must not stay in the live tail: the frame of the rebuild would
// show them, and the commit after it would write them a second time.
func TestRebuildTakesStableBlocksThatWaitForCommit(t *testing.T) {
	m := newTestModel(fakeSource{placeholder: true})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20, Generation: 1})
	m.Update(firstFrameMsg{})
	m.Update(AppendBlockMsg{ID: "a", Block: textBlock{"alpha-text"}, Stable: true})
	require.True(t, m.committing, "the first commit is running")
	m.Update(AppendBlockMsg{ID: "b", Block: textBlock{"beta-text"}, Stable: true})
	m.Update(RebuildMsg{})
	require.Equal(t, 1, m.transcript.Committed(), "the busy output holds the second block back")

	_, cmd := m.Update(commitDoneMsg{})
	require.NotNil(t, cmd)
	require.True(t, m.replaying, "the rebuild starts when the commit ends")
	require.Equal(t, 2, m.transcript.Committed())
	require.Empty(t, m.transcript.TailLines(m.renderContext()), "the frame of the rebuild holds no row that is written again")
}

// appendSettled adds a block and waits until the screen stops changing. A
// golden compares one exact screen, and the renderer leaves different gaps
// below the frame when the frame size changes between draws, so goldens add
// one block at a time.
func appendSettled(h *screenHarness, id string, b Block, stable bool) {
	appendBlock(h, id, b, stable)
	h.settle()
}

func TestScreenGoldens(t *testing.T) {
	footer := func(left string) (string, string) {
		return left, "deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 "
	}
	history := func(h *screenHarness) {
		appendSettled(h, "header", headerBlock{"◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore"}, true)
		appendSettled(h, "p1", promptBlock{"why does TestLeaderStale fail on CI?", "14:02"}, true)
		appendSettled(h, "r1", replyBlock{"The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket after the lock probe, and the probe can take 1.2 s."}, true)
	}

	t.Run("idle_80x24", func(t *testing.T) {
		l, r := footer("  ● leader 7c1e   ⌂ ~/src/askcore")
		h := startScreen(t, newTestModel(fakeSource{placeholder: true, cursorCol: 0, footerL: l, footerR: r}), 80, 24)
		history(h)
		h.waitText("probe can take")
		h.requireGolden()
	})

	t.Run("idle_120x40", func(t *testing.T) {
		l, r := footer("  ● leader 7c1e   ⌂ ~/src/askcore")
		h := startScreen(t, newTestModel(fakeSource{placeholder: true, footerL: l, footerR: r}), 120, 40)
		history(h)
		h.waitText("probe can take")
		h.requireGolden()
	})

	t.Run("streaming_80x24", func(t *testing.T) {
		l, r := footer("  ● leader 7c1e   ⌂ ~/src/askcore")
		src := fakeSource{placeholder: true, status: "┊ ◌ Working… 4s · esc to interrupt", footerL: l, footerR: r}
		h := startScreen(t, newTestModel(src), 80, 24)
		history(h)
		appendSettled(h, "p2", promptBlock{"show me the probe code", "14:03"}, true)
		appendSettled(h, "tool", textBlock{"  ◇ echo  probe code request                                          running ⠋"}, false)
		appendSettled(h, "r2", replyBlock{"The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"}, false)
		h.waitText("initialize frame")
		h.requireGolden()
	})

	t.Run("queue_80x24", func(t *testing.T) {
		l, r := footer("  ● leader 7c1e   ⌂ ~/src/askcore")
		src := fakeSource{
			placeholder: true, status: "┊ ◌ Working… 9s · esc to interrupt", footerL: l, footerR: r,
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
		l, r := footer("  ● leader 7c1e   ⌂ ~/src/askcore")
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
		h := startScreen(t, newTestModel(fakeSource{modal: modal, modalCur: &CursorPos{Row: 1, Col: 6}, footerL: l, footerR: r}), 80, 24)
		history(h)
		h.waitText("Select model")
		h.requireGolden()
	})

	t.Run("narrow_40x14", func(t *testing.T) {
		src := fakeSource{placeholder: true, status: "┊ ◌ Working… 3s · esc", footerL: "  ● 7c1e", footerR: "deepseek-v4.1-flash 18% "}
		h := startScreen(t, newTestModel(src), 40, 14)
		appendSettled(h, "p1", promptBlock{"giải thích lỗi\n日本語のテスト", "14:05"}, true)
		appendSettled(h, "r1", replyBlock{"The 日本語 test fails because each wide character uses two columns."}, true)
		h.waitText("two columns.")
		h.requireGolden()
	})
}

// A committed row that fills every column keeps its last character. The
// renderer does not erase to the end of the line after such a row.
func TestCommittedRowKeepsLastCharacter(t *testing.T) {
	h := startScreen(t, newTestModel(fakeSource{placeholder: true}), 40, 12)
	row := strings.Repeat("ab", 19) + "XZ"
	require.Equal(t, 40, displayWidth(row))
	appendBlock(h, "wide", rawBlock{row}, true)
	appendBlock(h, "after", textBlock{"after"}, true)
	h.waitText("after")
	h.requireEachOnce(row, "after")
}

// After a commit the cursor is back on the draft, also when the frame did not
// change. The renderer leaves it at the top of the frame otherwise.
func TestCursorReturnsToDraftAfterCommit(t *testing.T) {
	src := fakeSource{draft: "draft", cursorCol: 3}
	h := startScreen(t, newTestModel(src), 40, 12)
	appendBlock(h, "one", textBlock{"first committed row"}, true)
	appendBlock(h, "two", textBlock{"second committed row"}, true)
	h.requireEachOnce("first committed row", "second committed row")
	editor := -1
	for i, row := range h.grid() {
		if strings.Contains(row, "draft") {
			editor = i
		}
	}
	require.GreaterOrEqual(t, editor, 0)
	cur := h.emu.CursorPosition()
	require.Equal(t, editor, cur.Y, "cursor is on the draft row")
	require.Equal(t, 2+displayWidth("❯ dra"), cur.X)
}

// A row from a block that is wider than the terminal is wrapped, not cut and
// not left to the terminal, so the row count of the live area stays right.
func TestWideBlockRowsAreWrapped(t *testing.T) {
	h := startScreen(t, newTestModel(fakeSource{placeholder: true}), 30, 12)
	appendBlock(h, "long", rawBlock{strings.Repeat("x", 70)}, true)
	appendBlock(h, "after", textBlock{"after"}, true)
	h.waitText("after")
	for _, row := range h.text() {
		require.LessOrEqual(t, displayWidth(row), 30)
	}
	var total int
	for _, row := range h.text() {
		total += strings.Count(row, "x")
	}
	require.Equal(t, 70, total, "no character is lost")
}

// rawBlock returns its text as one row without wrapping.
type rawBlock struct{ row string }

func (b rawBlock) Lines(RenderContext) []string { return []string{b.row} }

// Stable blocks that wait behind a mutable block fill the live tail. When the
// mutable block finishes, they all commit at once. No row may appear twice or
// out of order, whatever the screen height, because a commit that is larger
// than the room above the live frame would scroll stale frame rows into the
// scrollback.
func TestCommitAfterBlockedTailKeepsEveryRowOnce(t *testing.T) {
	for _, height := range []int{6, 8, 12, 24} {
		for _, queued := range []int{2, height, height * 3} {
			t.Run(fmt.Sprintf("h%d_queued%d", height, queued), func(t *testing.T) {
				h := startScreen(t, newTestModel(fakeSource{placeholder: true, footerL: "foot"}), 50, height)
				appendBlock(h, "head", textBlock{"row-head"}, false)
				var tokens []string
				for i := 0; i < queued; i++ {
					tok := fmt.Sprintf("row-%03d", i)
					tokens = append(tokens, tok)
					appendBlock(h, tok, textBlock{tok}, true)
				}
				h.waitText(tokens[len(tokens)-1])
				h.send(FinishBlockMsg{ID: "head"})
				h.requireEachOnce(append([]string{"row-head"}, tokens...)...)
			})
		}
	}
}
