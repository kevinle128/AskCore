package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// --- helpers ---

func leftAt(x, y int) tea.Mouse { return tea.Mouse{X: x, Y: y, Button: tea.MouseLeft} }
func wheelAt(x, y int, up bool) tea.Mouse {
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	return tea.Mouse{X: x, Y: y, Button: b}
}

// mouse delivers a mouse message and draws a frame, as the program does.
func mouse(m *Model, msg tea.Msg) {
	m.Update(msg)
	m.View()
}

func clickAt(m *Model, x, y int) {
	mouse(m, tea.MouseClickMsg(leftAt(x, y)))
	mouse(m, tea.MouseReleaseMsg(leftAt(x, y)))
}

func hoverAt(m *Model, x, y int) {
	mouse(m, tea.MouseMotionMsg(tea.Mouse{X: x, Y: y}))
}

// rowIndex returns the screen row that holds the text.
func rowIndex(t *testing.T, m *Model, text string) int {
	t.Helper()
	for i, r := range screenOf(m) {
		if strings.Contains(r, text) {
			return i
		}
	}
	t.Fatalf("no row holds %q:\n%s", text, strings.Join(screenOf(m), "\n"))
	return 0
}

// colOf returns the column at which the text starts in a plain row.
func colOf(t *testing.T, row, text string) int {
	t.Helper()
	i := strings.Index(row, text)
	require.GreaterOrEqual(t, i, 0, "row %q has no %q", row, text)
	return displayWidth(row[:i])
}

// withFakeClipboard gives the model a clipboard that runs no process.
func withFakeClipboard(m *Model) *fakeClipboard {
	f := newFakeClipboard("darwin", nil)
	m.sv.clip = f.options(0)
	return f
}

// dragText drags from one cell to another, releases, and returns the text that
// the copy command carries, or "" when the release started no copy.
func dragText(t *testing.T, m *Model, x0, y0, x1, y1 int) string {
	t.Helper()
	mouse(m, tea.MouseClickMsg(leftAt(x0, y0)))
	mouse(m, tea.MouseMotionMsg(leftAt(x1, y1)))
	cmd := m.sv.onMouse(tea.MouseReleaseMsg(leftAt(x1, y1)))
	m.View()
	if cmd == nil {
		return ""
	}
	done, ok := cmd().(clipboardDoneMsg)
	require.True(t, ok, "the release starts a copy")
	return done.text
}

func listModelWithClipboard(t *testing.T, width, height int) (*Model, *fakeClipboard) {
	t.Helper()
	m := listModel(t, width, height)
	return m, withFakeClipboard(m)
}

// --- wheel ---

func TestWheelScrollsThreeRowsPerNotchAndTurnsFollowOff(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	require.True(t, m.pane.Following())
	before := paneScreen(m)

	mouse(m, tea.MouseWheelMsg(wheelAt(10, m.geo.PaneTop+2, true)))
	require.False(t, m.pane.Following())
	after := paneScreen(m)
	// The window moved up by three rows: row 0 is now what was above row 0, and
	// what was on row 0 is on row 3. The marker takes the last row.
	require.Equal(t, before[0], after[3])
	require.Equal(t, before[len(before)-5], after[len(after)-2])

	mouse(m, tea.MouseWheelMsg(wheelAt(10, m.geo.PaneTop, false)))
	mouse(m, tea.MouseWheelMsg(wheelAt(10, m.geo.PaneTop, false)))
	require.True(t, m.pane.Following(), "a scroll that reaches the last row follows the end")
}

func TestWheelOutsideThePaneIsIgnored(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	for _, y := range []int{0, 1, m.geo.PromptTop, 23} {
		mouse(m, tea.MouseWheelMsg(wheelAt(5, y, true)))
	}
	require.True(t, m.pane.Following())
}

func TestWheelIsIgnoredWhileASelectorIsOpen(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	m.sv.openSelector(newSessionSelector(nil, "", ""))
	m.View()
	mouse(m, tea.MouseWheelMsg(wheelAt(5, m.geo.PaneTop+3, true)))
	require.True(t, m.pane.Following())
	clickAt(m, 5, m.geo.PaneTop+3)
	require.Equal(t, FocusSelector, m.Focus())
	require.Empty(t, m.list.selected)
}

func TestWheelScrollsTheViewerByThreeRows(t *testing.T) {
	m := listModel(t, 80, 24)
	m.openViewer(toolID(t, m, "bash", 0))
	m.View()
	first := screenOf(m)[2]
	require.Contains(t, first, "$ seq 1 40")
	mouse(m, tea.MouseWheelMsg(wheelAt(5, 10, false)))
	require.Equal(t, 3, m.viewer.top)
	mouse(m, tea.MouseWheelMsg(wheelAt(5, 10, true)))
	require.Equal(t, 0, m.viewer.top)
	mouse(m, tea.MouseWheelMsg(wheelAt(5, 10, true)))
	require.Equal(t, 0, m.viewer.top)
}

// --- click ---

func TestClickSelectsTheBlockAndFocusesTheList(t *testing.T) {
	m := listModel(t, 80, 24)
	y := rowIndex(t, m, "value; shall I fix it?")
	clickAt(m, 20, y)
	require.Equal(t, FocusBlocks, m.Focus())
	require.Equal(t, blockID(t, m, isReply), m.selectedBlock())
	require.False(t, m.viewerOpen(), "a reply has no viewer")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "┃ value; shall I fix it?", "the rail marks the selection")
}

func TestClickOnTheFirstRowOfAStepSelectsAndOpensTheViewer(t *testing.T) {
	m := listModel(t, 80, 24)
	y := rowIndex(t, m, "◆ Ran seq 1 40")
	clickAt(m, 6, y)
	require.True(t, m.viewerOpen())
	require.Equal(t, toolID(t, m, "bash", 0), m.list.selected)
	require.Equal(t, FocusBlocks, m.Focus())
	rows := screenOf(m)
	require.Contains(t, rows[0], "← back  Ran seq 1 40")
	require.Contains(t, strings.Join(rows, "\n"), "$ seq 1 40")

	// A click on the way back returns to the list with the same selection.
	clickAt(m, 3, 0)
	require.False(t, m.viewerOpen())
	require.Equal(t, toolID(t, m, "bash", 0), m.selectedBlock())
	require.Contains(t, strings.Join(screenOf(m), "\n"), "› Ran seq 1 40")
}

func TestClickOnAnExpandedStepBelowItsFirstRowOnlySelects(t *testing.T) {
	m := listModel(t, 80, 24)
	id := toolID(t, m, "bash", 0)
	require.NoError(t, m.pane.ExpandBlock(id))
	m.pane.ScrollToTop()
	m.View()
	y := rowIndex(t, m, "$ seq 1 40")
	clickAt(m, 8, y)
	require.False(t, m.viewerOpen(), "a click below the first row does not open")
	require.Equal(t, id, m.selectedBlock())
	require.Equal(t, FocusBlocks, m.Focus())
}

func TestClickOnTheGapRowOfABlockSelectsThatBlock(t *testing.T) {
	m := listModel(t, 80, 24)
	clickAt(m, 10, 3)
	require.Equal(t, FocusBlocks, m.Focus())
	require.Equal(t, blockID(t, m, isPrompt), m.selectedBlock())
}

func TestClickInHeaderBarRulesAndStatusIsIgnored(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "draft")
	for _, y := range []int{0, 1, m.geo.PromptTop - 1, m.geo.PromptTop + 1, 23} {
		clickAt(m, 12, y)
		require.Equal(t, FocusEditor, m.Focus(), "row %d", y)
		require.Empty(t, m.list.selected)
	}
	require.Equal(t, "draft", m.sv.editor.Text())
	require.Equal(t, len("draft"), m.sv.editor.Cursor())
}

func TestClickOnTheBottomMarkerJumpsToTheBottomAndFollowStaysOn(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	m.pane.ScrollBy(-8)
	addNotes(t, m, 2)
	require.False(t, m.pane.Following())
	y := m.geo.PaneTop + m.geo.PaneRows - 1
	require.Contains(t, screenOf(m)[y], "new lines · End")

	clickAt(m, 40, y)
	require.True(t, m.pane.Following())
	pane := paneScreen(m)
	require.Contains(t, pane[len(pane)-2], "new note")
	for _, r := range pane {
		require.NotContains(t, r, "▼")
	}
	addNotes(t, m, 1)
	require.True(t, m.pane.Following(), "a new event keeps the pane at the bottom")
	require.Contains(t, paneScreen(m)[m.geo.PaneRows-2], "new note")
}

func TestClickInThePromptFocusesItAndPlacesTheCursor(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "hello world")
	press(t, m, "tab")
	require.Equal(t, FocusBlocks, m.Focus())

	// "❯ hello world" starts at column 1; the text starts at column 3.
	clickAt(m, 3+6, m.geo.PromptTop)
	require.Equal(t, FocusEditor, m.Focus())
	require.Empty(t, m.list.selected)
	require.Equal(t, 6, m.sv.editor.Cursor())
	typed(t, m, "X")
	require.Equal(t, "hello Xworld", m.sv.editor.Text())
}

func TestClickInTheScrolledPromptUsesTheShownRows(t *testing.T) {
	m := listModel(t, 80, 14)
	for i := 1; i <= 12; i++ {
		typed(t, m, "line")
		if i < 12 {
			press(t, m, "ctrl+j")
		}
	}
	require.Greater(t, m.geo.PromptRows, 1)
	row := m.geo.PromptStart
	require.Greater(t, row, 0, "the draft scrolls inside the prompt pane")
	clickAt(m, 3+2, m.geo.PromptTop)
	want := row*len("line\n") + 2
	require.Equal(t, want, m.sv.editor.Cursor())
}

func TestClickWhileTheCommandListIsOpenDoesNotTakeTheFocus(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "/")
	require.NotEmpty(t, m.sv.editor.MenuRows(80, m.theme))
	y := rowIndex(t, m, "value; shall I fix it?")
	clickAt(m, 20, y)
	require.Equal(t, FocusEditor, m.Focus())
}

// --- hover ---

func TestHoverShowsTheIndicatorWithoutChangingTheSelection(t *testing.T) {
	m := listModel(t, 80, 24)
	y := rowIndex(t, m, "◆ Read parser.go")
	hoverAt(m, 10, y)
	require.Equal(t, toolID(t, m, "read", 0), m.list.hover)
	require.Regexp(t, `^  › Read parser\.go`, screenOf(m)[y])
	require.Empty(t, m.list.selected)
	require.Equal(t, FocusEditor, m.Focus())

	hoverAt(m, 10, 0)
	require.Empty(t, m.list.hover)
	require.Regexp(t, `^  ◆ Read parser\.go`, screenOf(m)[y])
}

func TestHoverOverANonFoldableBlockClearsTheIndicator(t *testing.T) {
	m := listModel(t, 80, 24)
	hoverAt(m, 10, rowIndex(t, m, "◆ Read parser.go"))
	require.NotEmpty(t, m.list.hover)
	hoverAt(m, 10, rowIndex(t, m, "value; shall I fix it?"))
	require.Empty(t, m.list.hover)
	hoverAt(m, 10, rowIndex(t, m, "◆ Ran seq 1 40"))
	require.NotEmpty(t, m.list.hover)
	hoverAt(m, 10, m.geo.PromptTop)
	require.Empty(t, m.list.hover)
}

func TestHoverOverTheBottomMarkerRowIsNoBlock(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	m.pane.ScrollBy(-3)
	m.View()
	hoverAt(m, 40, m.geo.PaneTop+m.geo.PaneRows-1)
	require.Empty(t, m.list.hover)
}

// --- drag and copy ---

func TestDragCopiesThePlainTextOfTheCells(t *testing.T) {
	m, f := listModelWithClipboard(t, 80, 24)
	y := rowIndex(t, m, "Count now skips empty fields.")
	row := screenOf(m)[y]
	x0 := colOf(t, row, "skips empty")
	text := dragText(t, m, x0, y, x0+len("skips empty fields.")-1, y)
	require.Equal(t, "skips empty fields.", text)
	require.Equal(t, []clipRun{{name: "pbcopy", input: "skips empty fields."}}, f.calls())
}

func TestDragShowsTheRangeOnTheSelectedBackgroundAndKeepsItUntilTheNextKey(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24)
	withFakeClipboard(m)
	y := rowIndex(t, m, "Count now skips empty fields.")
	x0 := colOf(t, screenOf(m)[y], "skips")
	dragText(t, m, x0, y, x0+4, y)

	sel := "\x1b[48;2;45;49;80m"
	rows := strings.Split(m.View().Content, "\n")
	require.Contains(t, rows[y], sel+"skips", "the dragged cells have the selected background")
	require.Equal(t, 1, strings.Count(rows[y], sel))
	for i, r := range rows {
		if i != y {
			require.NotContains(t, r, sel, "row %d", i)
		}
	}

	press(t, m, "tab")
	require.NotContains(t, m.View().Content, sel, "a key ends the range")
}

func TestDragWithoutColorReversesTheRange(t *testing.T) {
	m := listModel(t, 80, 24)
	withFakeClipboard(m)
	y := rowIndex(t, m, "Count now skips empty fields.")
	x0 := colOf(t, screenOf(m)[y], "skips")
	dragText(t, m, x0, y, x0+4, y)
	require.Contains(t, strings.Split(m.View().Content, "\n")[y], "\x1b[7mskips\x1b[27m")
}

func TestDragAcrossTheWrapPointCopiesOneLine(t *testing.T) {
	m, _ := listModelWithClipboard(t, 80, 24)
	y := rowIndex(t, m, "Count now skips empty fields.")
	require.Contains(t, screenOf(m)[y+1], "value; shall I fix it?", "the reply wraps after 'old'")
	x0 := colOf(t, screenOf(m)[y], "the old")
	x1 := colOf(t, screenOf(m)[y+1], "shall") + len("shall") - 1
	text := dragText(t, m, x0, y, x1, y+1)
	require.Equal(t, "the old value; shall", text)
}

func TestDragOverRowsThatAreSeparateLinesKeepsTheLineBreaks(t *testing.T) {
	m, _ := listModelWithClipboard(t, 80, 24)
	id := toolID(t, m, "bash", 0)
	require.NoError(t, m.pane.ExpandBlock(id))
	m.pane.ScrollToTop()
	m.View()
	y1 := rowIndex(t, m, "$ seq 1 40")
	rows := screenOf(m)
	require.Equal(t, "    1", rows[y1+1])
	text := dragText(t, m, 4, y1+1, 4, y1+3)
	require.Equal(t, "1\n2\n3", text)
}

func TestDragCopiesNoGutterRailNoIndicatorAndNoTrailingSpaces(t *testing.T) {
	m, _ := listModelWithClipboard(t, 80, 24)
	press(t, m, "tab", "k") // the failed test run: a step at its lowest mode, shown with its indicator
	y := rowIndex(t, m, "Ran go test ./...")
	require.Regexp(t, `^  › Ran go test`, screenOf(m)[y])
	text := dragText(t, m, 0, y, 79, y)
	require.Equal(t, "◆ Ran go test ./...                                                  ✗ exit 1", text, "the glyph is the clean one")
	require.NotContains(t, text, "›")
	require.False(t, strings.HasSuffix(text, " "))
}

func TestDragOverTheRailOfAReplyCopiesNoRail(t *testing.T) {
	m, _ := listModelWithClipboard(t, 80, 24)
	press(t, m, "tab")
	y := rowIndex(t, m, "Count now skips empty fields.")
	require.True(t, strings.HasPrefix(screenOf(m)[y], "┃"))
	text := dragText(t, m, 0, y, 79, y+1)
	require.NotContains(t, text, "┃")
	require.Equal(t, "Count now skips empty fields. TestCount still fails because it expects the old value; shall I fix it?", text)
}

func TestDragCountsWideCharactersAsTwoColumns(t *testing.T) {
	m, _ := listModelWithClipboard(t, 40, 14)
	m.Update(AppendBlockMsg{ID: "cjk", Block: NewReconnectNote("日本語のテキスト"), Stable: true})
	m.View()
	y := rowIndex(t, m, "日本語")
	x0 := colOf(t, screenOf(m)[y], "日本語")
	// Two columns for each character: 日 at x0..x0+1, 本 at +2..+3.
	text := dragText(t, m, x0, y, x0+3, y)
	require.Equal(t, "日本", text)
	text = dragText(t, m, x0+1, y, x0+2, y)
	require.Equal(t, "日本", text, "a character is copied when one of its cells is selected")
}

func TestDragAcrossBlocksJoinsRowsWithLineBreaks(t *testing.T) {
	m, _ := listModelWithClipboard(t, 80, 24)
	y0 := rowIndex(t, m, "◆ Ran seq 1 40")
	y1 := rowIndex(t, m, "◆ Read parser.go")
	text := dragText(t, m, 2, y0, 40, y1)
	require.Equal(t, "◆ Ran seq 1 40\n◆ Read parser.go", text)
}

func TestPressAndReleaseOnOneCellIsAClickAndCopiesNothing(t *testing.T) {
	m, f := listModelWithClipboard(t, 80, 24)
	y := rowIndex(t, m, "Count now skips empty fields.")
	mouse(m, tea.MouseClickMsg(leftAt(20, y)))
	cmd := m.sv.onMouse(tea.MouseReleaseMsg(leftAt(20, y)))
	require.Nil(t, cmd)
	require.Empty(t, f.calls())
	require.Equal(t, FocusBlocks, m.Focus(), "it selected the block")
}

func TestDragThatEndsOnItsStartCellIsAClick(t *testing.T) {
	m, f := listModelWithClipboard(t, 80, 24)
	y := rowIndex(t, m, "Count now skips empty fields.")
	mouse(m, tea.MouseClickMsg(leftAt(20, y)))
	mouse(m, tea.MouseMotionMsg(leftAt(30, y)))
	require.NotEqual(t, textRange{}, m.mouse.rng)
	mouse(m, tea.MouseMotionMsg(leftAt(20, y)))
	require.Equal(t, textRange{}, m.mouse.rng, "back on the cell of the press: no range")
	require.Nil(t, m.sv.onMouse(tea.MouseReleaseMsg(leftAt(20, y))))
	require.Empty(t, f.calls())
}

func TestDragStartingInThePromptPaneStartsNoSelection(t *testing.T) {
	m, f := listModelWithClipboard(t, 80, 24)
	typed(t, m, "hello world")
	mouse(m, tea.MouseClickMsg(leftAt(4, m.geo.PromptTop)))
	mouse(m, tea.MouseMotionMsg(leftAt(12, m.geo.PromptTop)))
	require.Nil(t, m.sv.onMouse(tea.MouseReleaseMsg(leftAt(12, m.geo.PromptTop))))
	require.Equal(t, textRange{}, m.mouse.rng)
	require.Empty(t, f.calls())
}

func TestDragStartingOnAStepRowSelectsTextAndDoesNotOpenTheViewer(t *testing.T) {
	m, _ := listModelWithClipboard(t, 80, 24)
	y := rowIndex(t, m, "◆ Ran seq 1 40")
	text := dragText(t, m, 4, y, 6, y)
	require.Equal(t, "Ran", text)
	require.False(t, m.viewerOpen(), "a press that becomes a drag is no click")
}

func TestDragInTheViewerCopiesBodyText(t *testing.T) {
	m, _ := listModelWithClipboard(t, 80, 24)
	m.openViewer(toolID(t, m, "bash", 0))
	m.View()
	text := dragText(t, m, 4, 4, 5, 6)
	require.Equal(t, "1\n2\n3", text)
	require.Contains(t, strings.Split(m.View().Content, "\n")[4], "\x1b[7m", "the viewer shows the range")
}

func TestDragAtThePaneEdgeScrollsOneRowEachTickAndTurnsFollowOff(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	withFakeClipboard(m)
	require.True(t, m.pane.Following())
	top := m.geo.PaneTop
	mouse(m, tea.MouseClickMsg(leftAt(10, top+5)))
	_, cmd := m.Update(tea.MouseMotionMsg(leftAt(12, top)))
	require.NotNil(t, cmd, "the edge arms a scroll tick")
	m.View()
	gen := m.mouse.tickGen

	before := paneScreen(m)
	mouse(m, dragTickMsg{gen: gen})
	require.False(t, m.pane.Following())
	after := paneScreen(m)
	require.Equal(t, before[0], after[1], "one row of scroll")
	require.NotEqual(t, textRange{}, m.mouse.rng)

	// Release ends the drag: an old tick does nothing.
	m.sv.onMouse(tea.MouseReleaseMsg(leftAt(12, top)))
	now := paneScreen(m)
	mouse(m, dragTickMsg{gen: gen})
	require.Equal(t, now, paneScreen(m))
}

func TestDragSelectionStaysOnTheSameTextWhenThePaneScrolls(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	withFakeClipboard(m)
	top := m.geo.PaneTop
	y := rowIndex(t, m, "◆ Ran step 38")
	var topStep int
	_, err := fmt.Sscanf(strings.TrimSpace(paneScreen(m)[0]), "◆ Ran step %d", &topStep)
	require.NoError(t, err)
	mouse(m, tea.MouseClickMsg(leftAt(10, y)))
	m.Update(tea.MouseMotionMsg(leftAt(10, top)))
	m.View()
	for range 3 {
		mouse(m, dragTickMsg{gen: m.mouse.tickGen})
	}
	cmd := m.sv.onMouse(tea.MouseReleaseMsg(leftAt(10, top)))
	require.NotNil(t, cmd)
	lines := strings.Split(cmd().(clipboardDoneMsg).text, "\n")
	// The head went up with the pane: three rows above the first row that the
	// pane showed, and the anchor row is still the last line.
	require.Equal(t, 38-(topStep-3)+1, len(lines), strings.Join(lines, "|"))
	require.Equal(t, fmt.Sprintf("ep %d", topStep-3), lines[0], "the first line starts at the cell of the head")
	require.Equal(t, "◆ Ran ste", lines[len(lines)-1])
}

func TestMouseIsIgnoredWhileABoxIsOpen(t *testing.T) {
	m := listModel(t, 80, 24)
	m.sv.pbox = &providerBox{}
	mouse(m, tea.MouseClickMsg(leftAt(20, rowIndex(t, m, "◆ Read parser.go"))))
	require.Empty(t, m.list.selected)
	require.Equal(t, FocusEditor, m.Focus())
	require.False(t, m.mouse.down)
}

// The mouse messages of a terminal reach Update through the real program.
func TestMouseMessagesReachUpdateThroughTheProgram(t *testing.T) {
	f := newFakeClipboard("darwin", nil)
	r := startCopyRig(t, 80, 24, f)
	r.firstTurn()

	grid := r.h.grid()
	row := -1
	for i, g := range grid {
		if strings.Contains(g, "stale socket check") {
			row = i
		}
	}
	require.GreaterOrEqual(t, row, 0, strings.Join(grid, "\n"))
	x0 := colOf(t, grid[row], "stale socket")
	r.h.send(tea.MouseClickMsg(leftAt(x0, row)))
	r.h.send(tea.MouseMotionMsg(leftAt(x0+11, row)))
	r.h.send(tea.MouseReleaseMsg(leftAt(x0+11, row)))
	r.h.waitText("┊ ✓ Copied")
	require.Equal(t, []clipRun{{name: "pbcopy", input: "stale socket"}}, f.calls())

	// A wheel step moves the pane: the first turn is short, so only the
	// notice and the program staying alive are checked here.
	r.h.send(tea.MouseWheelMsg(wheelAt(5, row, true)))
	r.h.send(tea.MouseMotionMsg(tea.Mouse{X: 5, Y: row}))
	r.h.settle()
	m := r.stopped()
	require.NotNil(t, m)
}

// --- jump to the bottom, in every way ---

func TestEveryWayToTheBottomRemovesTheMarkerAndKeepsFollowOn(t *testing.T) {
	jump := map[string]func(t *testing.T, m *Model){
		"End in the list focus": func(t *testing.T, m *Model) {
			press(t, m, "tab", "end")
		},
		"ctrl+End in the prompt focus": func(t *testing.T, m *Model) {
			press(t, m, "ctrl+end")
		},
		"ctrl+End in the list focus": func(t *testing.T, m *Model) {
			press(t, m, "tab", "ctrl+end")
		},
		"click on the marker": func(t *testing.T, m *Model) {
			clickAt(m, 40, m.geo.PaneTop+m.geo.PaneRows-1)
		},
	}
	for name, do := range jump {
		t.Run(name, func(t *testing.T) {
			m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
			mouse(m, tea.MouseWheelMsg(wheelAt(5, m.geo.PaneTop, true)))
			addNotes(t, m, 2)
			require.False(t, m.pane.Following())
			require.Contains(t, paneScreen(m)[m.geo.PaneRows-1], "new lines · End")

			do(t, m)
			require.True(t, m.pane.Following())
			for _, r := range paneScreen(m) {
				require.NotContains(t, r, "▼")
			}
			addNotes(t, m, 1)
			require.True(t, m.pane.Following())
			require.Contains(t, paneScreen(m)[m.geo.PaneRows-2], "new note")
		})
	}
}

// --- frame budget ---

// longModel is a model with 2000 blocks of the real block types.
func longModel(tb testing.TB, blocks int) *Model {
	tb.Helper()
	m := New(Options{Client: newFakeClient(), Profile: colorprofile.TrueColor, TickInterval: -1})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := range blocks {
		var b Block
		if i%3 == 2 {
			b = NewAssistantTextBlock(fmt.Sprintf("Step %d. ", i) + reply)
		} else {
			b = NewToolLineBlock("bash", fmt.Sprintf("go test ./pkg%d", i), ToolSucceeded, "0.1s", "", NewTranscriptView())
		}
		m.Update(AppendBlockMsg{ID: fmt.Sprintf("b%04d", i), Block: b, Stable: true})
	}
	m.View()
	return m
}

func TestMarkerHoverAndSelectionDrawingStayUnderTheFrameBudget(t *testing.T) {
	m := longModel(t, 2000)
	withFakeClipboard(m)
	top := m.geo.PaneTop
	mouse(m, tea.MouseWheelMsg(wheelAt(5, top+3, true)))
	m.pane.ScrollBy(-3000)
	m.View()
	addNotes(t, m, 1)
	require.False(t, m.pane.Following())
	require.Contains(t, paneScreen(m)[m.geo.PaneRows-1], "▼")

	checkBudget(t, "wheel with the marker", median(101, func(i int) {
		m.Update(tea.MouseWheelMsg(wheelAt(5, top+2, i%2 == 0)))
		m.View()
	}))

	draws := m.pane.Draws()
	checkBudget(t, "hover over many rows", median(101, func(i int) {
		m.Update(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: top + i%m.geo.PaneRows}))
		m.View()
	}))
	t.Logf("hover moves drew %d blocks", m.pane.Draws()-draws)

	mouse(m, tea.MouseClickMsg(leftAt(4, top+1)))
	m.Update(tea.MouseMotionMsg(leftAt(30, top+m.geo.PaneRows-3)))
	require.NotEqual(t, textRange{}, m.mouse.rng)
	checkBudget(t, "frame with a drag range", median(101, func(int) { m.View() }))
}

func TestDragEndsWhenThePointerMovesWithNoButtonDown(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	withFakeClipboard(m)
	top := m.geo.PaneTop
	mouse(m, tea.MouseClickMsg(leftAt(10, top+5)))
	_, cmd := m.Update(tea.MouseMotionMsg(leftAt(12, top)))
	require.NotNil(t, cmd, "the edge arms a scroll tick")
	m.View()
	gen := m.mouse.tickGen

	// The release never came: the next motion has no button.
	mouse(m, tea.MouseMotionMsg(tea.Mouse{X: 12, Y: top}))
	require.False(t, m.mouse.down)
	require.Zero(t, m.mouse.edge)

	before := paneScreen(m)
	_, cmd = m.Update(dragTickMsg{gen: gen})
	require.Nil(t, cmd, "the tick chain ends")
	require.Equal(t, before, paneScreen(m), "no further scroll")
	_, cmd = m.Update(dragTickMsg{gen: m.mouse.tickGen})
	require.Nil(t, cmd)
}

func TestDragTickStopsWhenASelectorOpensMidDrag(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	withFakeClipboard(m)
	top := m.geo.PaneTop
	mouse(m, tea.MouseClickMsg(leftAt(10, top+5)))
	_, cmd := m.Update(tea.MouseMotionMsg(leftAt(12, top)))
	require.NotNil(t, cmd)
	m.View()
	gen := m.mouse.tickGen

	m.sv.openSelector(newModelSelectorFor(protocol.ACPModelsResult{Current: deepseekModel, Models: testCatalog()}, deepseekModel))
	require.NotNil(t, m.sv.sel, "the selector is open")
	m.View()
	before := paneScreen(m)
	_, cmd = m.Update(dragTickMsg{gen: gen})
	require.Nil(t, cmd, "the tick chain ends")
	require.Equal(t, before, paneScreen(m), "no scroll under a selector")
	require.False(t, m.mouse.down)
}
