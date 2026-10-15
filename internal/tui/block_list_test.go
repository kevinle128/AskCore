package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// --- focus ---

func TestTabFocusesTheBlockListWithTheReplySelected(t *testing.T) {
	m := listModel(t, 80, 24)
	require.Equal(t, FocusEditor, m.Focus())
	require.NotNil(t, m.View().Cursor, "the prompt has the cursor")
	require.Contains(t, barOf(m), "Tab blocks · ctrl+t thinking")

	press(t, m, "tab")
	require.Equal(t, FocusBlocks, m.Focus())
	require.Contains(t, barOf(m), "j/k select")
	require.Nil(t, m.View().Cursor, "the prompt shows no cursor in the list focus")
	require.Equal(t, "", m.sv.editor.Text())

	reply := blockID(t, m, isReply)
	require.Equal(t, reply, m.selectedBlock())
	// The placeholder row keeps its text; only the cursor is gone.
	require.Contains(t, strings.Join(screenOf(m), "\n"), "❯ Ask anything  ·  / commands  ·  ctrl+l model")
}

func TestTabWithNothingToSelectStaysInThePrompt(t *testing.T) {
	m := listModel(t, 80, 24)
	m.resetTranscript()
	m.View()
	press(t, m, "tab")
	require.Equal(t, FocusEditor, m.Focus())
	require.NotNil(t, m.View().Cursor)
}

func TestTabAndEscReturnToThePrompt(t *testing.T) {
	for _, key := range []string{"tab", "esc"} {
		m := listModel(t, 80, 24)
		press(t, m, "tab", key)
		require.Equal(t, FocusEditor, m.Focus(), key)
		require.Equal(t, "", m.selectedBlock(), key)
		require.NotNil(t, m.View().Cursor, key)
		require.NotContains(t, strings.Join(screenOf(m), "\n"), "┃", "the selection mark ends with the list focus")
	}
}

func TestEscInTheBlockListDoesNotInterruptTheRun(t *testing.T) {
	m := listModel(t, 80, 24)
	m.sv.run.running = true
	press(t, m, "tab", "esc")
	require.False(t, m.sv.run.cancelling, "Esc in the list only moves the focus")
	require.Equal(t, FocusEditor, m.Focus())
	press(t, m, "esc")
	require.True(t, m.sv.run.cancelling, "Esc in the prompt interrupts the run")
}

func TestEnterInTheBlockListNeverSendsTheDraft(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "draft")
	press(t, m, "tab")
	// Enter on a reply opens nothing and sends nothing.
	press(t, m, "enter")
	require.Equal(t, "draft", m.sv.editor.Text())
	require.Zero(t, m.sv.client.(*fakeClient).count("prompt:"))
	require.False(t, m.viewerOpen())
}

// --- select ---

func TestJAndKSelectNextAndPreviousBlock(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	var order []string
	for range 7 {
		order = append(order, m.selectedBlock())
		press(t, m, "k")
	}
	// reply, failed test run, edit, read, seq, thinking, prompt; then it stops.
	require.Equal(t, blockID(t, m, isPrompt), m.selectedBlock())
	require.Len(t, order, 7)
	require.Equal(t, blockID(t, m, isReply), order[0])
	require.Equal(t, toolID(t, m, "bash", 1), order[1])
	require.Equal(t, toolID(t, m, "edit", 0), order[2])
	require.Equal(t, toolID(t, m, "read", 0), order[3])
	require.Equal(t, toolID(t, m, "bash", 0), order[4])
	require.Equal(t, blockID(t, m, isThinking), order[5])
	press(t, m, "j", "j", "down")
	require.Equal(t, toolID(t, m, "read", 0), m.selectedBlock())
	press(t, m, "up")
	require.Equal(t, toolID(t, m, "bash", 0), m.selectedBlock())
}

func TestTheReplyIsOneBlockWithItsUsageLine(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "j")
	require.Equal(t, blockID(t, m, isReply), m.selectedBlock(), "j at the last block stays")
	press(t, m, "k", "j")
	require.Equal(t, blockID(t, m, isReply), m.selectedBlock(), "the usage line is not a block of its own")
}

func TestSelectionStaysInView(t *testing.T) {
	m := listModel(t, 40, 11)
	press(t, m, "tab", "g")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "run seq 1 40")
	press(t, m, "G")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "┃ claude-sonnet-5-5")
	for range 4 {
		press(t, m, "k")
	}
	require.Contains(t, strings.Join(screenOf(m), "\n"), "Ran seq 1 40")
	press(t, m, "k")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "Thought for 0.5s")
	press(t, m, "g")
	require.False(t, m.pane.Following())
	require.Contains(t, strings.Join(screenOf(m), "\n"), "run seq 1 40")
}

// --- fold keys ---

func TestEFoldsTheSelectedBlockThroughThreeModes(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k")
	failed := toolID(t, m, "bash", 1)
	require.Equal(t, failed, m.selectedBlock())
	require.Contains(t, barOf(m), "e fold: collapsed")

	press(t, m, "e")
	rows := screenOf(m)
	text := strings.Join(rows, "\n")
	require.Contains(t, barOf(m), "e fold: truncated")
	require.NotContains(t, text, "$ go test ./...", "no command row in the truncated form")
	at := -1
	for i, r := range rows {
		if strings.Contains(r, "Ran go test") {
			at = i
		}
	}
	require.Greater(t, at, 0)
	want := []string{
		"┃   --- FAIL: TestCount (0.00s)",
		"┃       parser_test.go:12: want 2, got 3",
		"┃   === RUN   TestCountEmpty",
		"┃   ⋮ 3 more lines · e to expand · Enter full screen",
		"┃   FAIL",
		"┃   FAIL    example.com/p/parser    0.012s",
		"┃   FAIL",
	}
	require.Equal(t, want, rows[at+1:at+1+len(want)])

	press(t, m, "e")
	rows = screenOf(m)
	require.Contains(t, barOf(m), "e fold: expanded")
	at = -1
	for i, r := range rows {
		if strings.Contains(r, "Ran go test") {
			at = i
		}
	}
	require.Equal(t, "┃   $ go test ./...", rows[at+1], "the command is the first body row")
	require.Equal(t, "┃   --- FAIL: TestCount (0.00s)", rows[at+2])

	press(t, m, "e")
	require.Contains(t, barOf(m), "e fold: collapsed")
	require.Regexp(t, `(?m)^  › Ran go test \./\.\.\. +✗ exit 1$`, strings.Join(screenOf(m), "\n"))
}

func TestEOnAToolWithFiveLinesShowsAllAndNoHint(t *testing.T) {
	events := flat(
		&protocol.AgentStart{},
		userAt("go", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		toolStart("c1", "bash", `{"command":"seq 1 5"}`, 0),
		resultEnd("c1", "bash", "1\n2\n3\n4\n5", protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, 100),
	)
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, events...)
	press(t, m, "tab", "e")
	text := strings.Join(screenOf(m), "\n")
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		require.Regexp(t, `(?m)^┃   `+n+`$`, text)
	}
	require.NotContains(t, text, "⋮")
}

func TestEOnAnEditShowsTheDiffThenOneRow(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k", "k")
	require.Equal(t, toolID(t, m, "edit", 0), m.selectedBlock())
	press(t, m, "e", "e")
	text := strings.Join(screenOf(m), "\n")
	require.Regexp(t, `(?m)^┃\s+\d+ - `, text)
	require.Regexp(t, `(?m)^┃\s+\d+ \+ `, text)
	press(t, m, "e")
	text = strings.Join(screenOf(m), "\n")
	require.NotRegexp(t, `\d+ - `, text)
	require.Contains(t, text, "› Edited parser.go  +3 -1")
}

func TestRightAndLExpandLeftAndHCollapse(t *testing.T) {
	for _, keys := range [][2]string{{"right", "left"}, {"l", "h"}} {
		m := listModel(t, 80, 24)
		press(t, m, "tab", "k")
		press(t, m, keys[0])
		require.Contains(t, barOf(m), "e fold: expanded", keys[0])
		require.Contains(t, strings.Join(screenOf(m), "\n"), "$ go test ./...", keys[0])
		press(t, m, keys[0])
		require.Contains(t, barOf(m), "e fold: expanded", "no change when it is expanded already")
		press(t, m, keys[1])
		require.Contains(t, barOf(m), "e fold: collapsed", keys[1])
		require.NotContains(t, strings.Join(screenOf(m), "\n"), "$ go test ./...", keys[1])
	}
}

func TestRightOnTheReplyChangesNothing(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	before := screenOf(m)
	press(t, m, "right", "l", "e", "left", "h")
	require.Equal(t, before, screenOf(m))
}

func TestLeftAndRightMoveTheCursorInThePrompt(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "abc")
	before := strings.Join(screenOf(m)[2:12], "\n")
	press(t, m, "left")
	typed(t, m, "X")
	require.Equal(t, "abXc", m.sv.editor.Text())
	require.Equal(t, before, strings.Join(screenOf(m)[2:12], "\n"), "no block changed")
	require.Equal(t, FocusEditor, m.Focus())
}

func TestShortPromptHasNoFoldKey(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "g")
	require.Equal(t, blockID(t, m, isPrompt), m.selectedBlock())
	require.False(t, m.pane.Foldable(m.selectedBlock()))
	require.NotContains(t, barOf(m), "e fold:")
	before := screenOf(m)
	press(t, m, "e", "right")
	require.Equal(t, before, screenOf(m))
}

func TestLongPromptCyclesBetweenTwoFormsWithoutADeadKey(t *testing.T) {
	long := strings.Repeat("alpha beta gamma delta ", 12)
	events := flat(&protocol.AgentStart{}, userAt(long, time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)))
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, events...)
	press(t, m, "tab")
	require.True(t, m.pane.Foldable(m.selectedBlock()))
	count := func() int { return strings.Count(strings.Join(screenOf(m), "\n"), "alpha") }
	folded := count()
	require.Contains(t, barOf(m), "e fold: truncated")
	press(t, m, "e")
	require.Greater(t, count(), folded)
	require.Contains(t, barOf(m), "e fold: expanded")
	press(t, m, "e")
	require.Equal(t, folded, count())
	require.Contains(t, barOf(m), "e fold: truncated")
}

func TestShiftEFoldsAndUnfoldsEveryStep(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "E")
	for _, e := range m.transcript.entries {
		if fb, ok := e.block.(foldBlock); ok && fb.foldGroup().isStep() {
			require.Equal(t, FoldExpanded, m.pane.Fold(e.id), e.id)
		}
	}
	require.Contains(t, strings.Join(screenOf(m), "\n"), "$ go test ./...")
	press(t, m, "E")
	for _, e := range m.transcript.entries {
		if fb, ok := e.block.(foldBlock); ok && fb.foldGroup().isStep() {
			require.Equal(t, FoldCollapsed, m.pane.Fold(e.id), e.id)
		}
	}
	press(t, m, "g")
	rows := screenOf(m)
	text := strings.Join(rows, "\n")
	require.NotContains(t, text, "$ seq 1 40")
	require.Contains(t, text, "◆ Ran seq 1 40")
	require.Contains(t, text, "◆ Thought for 0.5s")
	require.Equal(t, 24, len(rows))
}

func TestCtrlTTogglesThinkingInEveryFocusAndCtrlEOnlyInTheList(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "draft")
	press(t, m, "ctrl+t")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "┃ Count splits on a comma.", "thinking is expanded")
	require.Equal(t, "draft", m.sv.editor.Text())
	press(t, m, "ctrl+t")
	require.NotContains(t, strings.Join(screenOf(m), "\n"), "Count splits on a comma.")

	// ctrl+e in the prompt is the end of the line.
	press(t, m, "home", "ctrl+e")
	typed(t, m, "X")
	require.Equal(t, "draftX", m.sv.editor.Text())
	require.NotContains(t, strings.Join(screenOf(m), "\n"), "Count splits on a comma.")

	// In the list it toggles the thinking.
	press(t, m, "tab", "ctrl+e")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "Count splits on a comma.")
	require.Equal(t, "draftX", m.sv.editor.Text())
	press(t, m, "ctrl+t")
	require.NotContains(t, strings.Join(screenOf(m), "\n"), "Count splits on a comma.")
	require.Equal(t, FocusBlocks, m.Focus(), "a shortcut does not move the focus")
}

// --- printable keys ---

func TestPrintableKeyInTheListFocusesThePromptAndTypesIt(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	typed(t, m, "x")
	require.Equal(t, FocusEditor, m.Focus())
	require.Equal(t, "x", m.sv.editor.Text())
	require.NotNil(t, m.View().Cursor)

	// A slash starts a command, as in the prompt.
	press(t, m, "tab")
	typed(t, m, "/")
	require.Equal(t, "x/", m.sv.editor.Text())
}

func TestListKeysAreNotTyped(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	for _, k := range []string{"j", "k", "e", "E", "g", "G", "b", "h", "l", "H", "L", "space"} {
		press(t, m, k)
		require.Equal(t, "", m.sv.editor.Text(), k)
		require.Equal(t, FocusBlocks, m.Focus(), k)
	}
}

func TestPasteInTheListGoesToThePrompt(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	m.Update(tea.PasteMsg{Content: "pasted"})
	require.Equal(t, "pasted", m.sv.editor.Text())
	require.Equal(t, FocusEditor, m.Focus())
}

func TestCtrlDInTheListKeepsADraft(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "draft")
	press(t, m, "tab", "ctrl+d")
	require.False(t, m.quitting)
	require.Equal(t, "draft", m.sv.editor.Text())
	m2 := listModel(t, 80, 24)
	press(t, m2, "tab")
	cmd := m2.sv.update(keyNamed(t, "ctrl+d"))
	require.NotNil(t, cmd, "an empty draft quits")
	require.True(t, m2.quitting)
}

// --- scroll keys ---

// longSession is a session of many one-row steps, so that the pane scrolls.
func longSession(n int) []protocol.Event {
	var parts []any
	parts = append(parts, &protocol.AgentStart{}, userAt("first turn", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)))
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c%d", i)
		parts = append(parts,
			toolStart(id, "bash", fmt.Sprintf(`{"command":"step %d"}`, i), int64(i*10)),
			resultEnd(id, "bash", "ok", protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, int64(i*10+5)))
	}
	parts = append(parts, assistantStart("m", 5000), textParts(0, "all done"),
		assistantEndUsage("m", protocol.StopStop, 5100, protocol.Usage{}, protocol.Text{Text: "all done"}))
	return flat(parts...)
}

func TestHomeAndEndSelectFirstAndLastAndEndTurnsFollowOn(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	press(t, m, "tab")
	require.True(t, m.pane.Following())
	press(t, m, "home")
	require.Equal(t, blockID(t, m, isPrompt), m.selectedBlock())
	require.False(t, m.pane.Following())
	require.Contains(t, strings.Join(screenOf(m), "\n"), "first turn")
	press(t, m, "end")
	require.Equal(t, blockID(t, m, isReply), m.selectedBlock())
	require.True(t, m.pane.Following())
	require.Contains(t, strings.Join(screenOf(m), "\n"), "all done")
	press(t, m, "g", "G")
	require.True(t, m.pane.Following())
	require.Equal(t, blockID(t, m, isReply), m.selectedBlock())
}

func TestPageKeysScrollAndKeepTheSelectionInView(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	press(t, m, "tab", "pgup")
	require.False(t, m.pane.Following())
	sel := m.selectedBlock()
	require.NotEmpty(t, sel)
	f := m.pane.Frame()
	idx := m.transcript.index[sel]
	require.GreaterOrEqual(t, idx, f.FirstBlock, "the selection is in the window")
	require.LessOrEqual(t, idx, f.LastBlock)
	top := screenOf(m)[2]
	press(t, m, "pgdown")
	require.NotEqual(t, top, screenOf(m)[2])
	f = m.pane.Frame()
	idx = m.transcript.index[m.selectedBlock()]
	require.GreaterOrEqual(t, idx, f.FirstBlock)
	require.LessOrEqual(t, idx, f.LastBlock)
	press(t, m, "pgup", "pgup", "pgup")
	f = m.pane.Frame()
	idx = m.transcript.index[m.selectedBlock()]
	require.GreaterOrEqual(t, idx, f.FirstBlock)
	require.LessOrEqual(t, idx, f.LastBlock)
}

func TestCtrlEndJumpsToTheBottomFromThePrompt(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	m.pane.ScrollToTop()
	m.View()
	require.False(t, m.pane.Following())
	typed(t, m, "keep")
	press(t, m, "ctrl+end")
	require.True(t, m.pane.Following())
	require.Contains(t, strings.Join(screenOf(m), "\n"), "all done")
	require.Equal(t, "keep", m.sv.editor.Text())
	require.Equal(t, FocusEditor, m.Focus())
}

func TestShiftArrowsAndHLJumpBetweenTurns(t *testing.T) {
	var parts []any
	parts = append(parts, &protocol.AgentStart{})
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("c%d", i)
		parts = append(parts,
			userAt(fmt.Sprintf("question %d", i), time.Date(2026, 10, 12, 15, 20+i, 0, 0, time.UTC)),
			toolStart(id, "bash", `{"command":"x"}`, int64(i*10)),
			resultEnd(id, "bash", "ok", protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, int64(i*10+5)))
	}
	for _, keys := range [][2]string{{"shift+left", "shift+right"}, {"H", "L"}} {
		m := listModelAt(t, colorprofile.NoTTY, 80, 24, flat(parts...)...)
		press(t, m, "tab")
		prompts := func() (ids []string) {
			for _, e := range m.transcript.entries {
				if isPrompt(e.block) {
					ids = append(ids, e.id)
				}
			}
			return ids
		}()
		require.Len(t, prompts, 3)
		press(t, m, keys[0])
		require.Equal(t, prompts[2], m.selectedBlock(), keys[0])
		press(t, m, keys[0])
		require.Equal(t, prompts[1], m.selectedBlock(), keys[0])
		press(t, m, keys[1])
		require.Equal(t, prompts[2], m.selectedBlock(), keys[1])
		press(t, m, keys[1])
		require.Equal(t, prompts[2], m.selectedBlock(), "no later turn: stays")
		press(t, m, keys[0], keys[0], keys[0])
		require.Equal(t, prompts[0], m.selectedBlock(), "no earlier turn: stays")
	}
}

// --- bar ---

func TestBarForEachFocusAndBlock(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k")
	require.Equal(t, " j/k select · e fold: collapsed · E all · Enter open · Tab prompt", barOf(m))
	press(t, m, "e")
	require.Equal(t, " j/k select · e fold: truncated · E all · Enter open · Tab prompt", barOf(m))
	press(t, m, "e")
	require.Equal(t, " j/k select · e fold: expanded · E all · Enter open · Tab prompt", barOf(m))
	press(t, m, "j")
	require.Equal(t, " j/k select · e fold · E all · Enter open · Tab prompt", barOf(m), "the mode shows only for a foldable block")
	press(t, m, "esc")
	require.Equal(t, " Tab blocks · ctrl+t thinking · ctrl+l model · / commands", barOf(m))
}

func TestBarShortFormAt40Columns(t *testing.T) {
	m := listModel(t, 40, 14)
	require.Equal(t, " Tab blocks · / commands", barOf(m))
	press(t, m, "tab", "k")
	require.Equal(t, " j/k · e fold · Enter open", barOf(m))
	press(t, m, "j")
	require.Equal(t, " j/k · e fold · Enter open", barOf(m))
	for _, r := range screenOf(m) {
		require.LessOrEqual(t, displayWidth(r), 40)
	}
}

func TestSelectorOpenedFromTheListTakesThePrompt(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	m.sv.openSelector(newModelSelectorFor(protocol.ACPModelsResult{Current: deepseekModel, Models: testCatalog()}, deepseekModel))
	m.View()
	require.Equal(t, FocusSelector, m.Focus())
	require.NotContains(t, barOf(m), "j/k select")
	press(t, m, "j")
	require.Equal(t, "", m.sv.editor.Text(), "the selector took the key")
	press(t, m, "esc")
	require.Equal(t, FocusEditor, m.Focus())
	require.Equal(t, "", m.selectedBlock())
}

func TestNewTranscriptEndsTheListFocusAndTheViewer(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k", "enter")
	require.True(t, m.viewerOpen())
	m.resetTranscript()
	require.False(t, m.viewerOpen())
	require.Equal(t, FocusEditor, m.Focus())
	require.Equal(t, "", m.selectedBlock())
}

// --- selection marks ---

func rawRows(m *Model) []string { return strings.Split(m.View().Content, "\n") }

func hasShade(th *Theme, row string) bool {
	on := bgSequence(th, RoleSelected)
	return on != "" && strings.Contains(row, on)
}

func TestSelectedReplyHasARailOnEveryRowAndNoShade(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24)
	press(t, m, "tab")
	rows := rawRows(m)
	plain := screenOf(m)
	var marked []string
	for i, r := range rows {
		require.False(t, hasShade(m.theme, r), "no row is shaded: %q", plain[i])
		if strings.HasPrefix(plain[i], "┃") {
			marked = append(marked, plain[i])
		}
	}
	require.Len(t, marked, 3, "two text rows and the usage row")
	require.Contains(t, marked[2], "claude-sonnet-5-5 · 6.2s")
	// The rail is in the accent role.
	var first string
	for i, r := range rows {
		if strings.HasPrefix(plain[i], "┃") {
			first = r
			break
		}
	}
	require.Contains(t, first, m.theme.Style(RoleAccent).Bold(true).Render("┃"))
}

func TestSelectedReplyRailCoversTheEmptyRowBetweenParagraphs(t *testing.T) {
	text := "para one\n\npara two"
	events := flat(
		&protocol.AgentStart{},
		userAt("go", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		assistantStart("m", 0),
		textParts(0, text),
		assistantEndUsage("m", protocol.StopStop, 100, protocol.Usage{Input: 1, Output: 1}, protocol.Text{Text: text}),
	)
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, events...)
	press(t, m, "tab")
	var rail []string
	for _, r := range screenOf(m) {
		if strings.HasPrefix(r, "┃") {
			rail = append(rail, r)
		}
	}
	require.Len(t, rail, 4, "paragraph, gap, paragraph and the usage row")
	require.Equal(t, "┃ para one", rail[0])
	require.Equal(t, "┃", rail[1], "the gap between paragraphs carries the rail")
	require.Equal(t, "┃ para two", rail[2])
	rows := screenOf(m)
	for i, r := range rows {
		if r == "┃ para one" {
			require.Equal(t, "", rows[i-1], "the lead row above the reply has no rail")
		}
	}
}

func TestSelectedThinkingAtItsLowestModeIsShaded(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24)
	press(t, m, "tab", "k", "k", "k", "k", "k")
	require.Equal(t, blockID(t, m, isThinking), m.selectedBlock())
	rows, plain := rawRows(m), screenOf(m)
	n := 0
	for i, r := range rows {
		if hasShade(m.theme, r) {
			n++
			require.Contains(t, plain[i], "› Thought for 0.5s")
		}
	}
	require.Equal(t, 1, n)
}

func TestTabIsIgnoredWhileTheCommandListIsOpen(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "/mo")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "/model")
	press(t, m, "tab")
	require.Equal(t, FocusEditor, m.Focus())
	require.Equal(t, "/mo", m.sv.editor.Text())
	require.Contains(t, strings.Join(screenOf(m), "\n"), "/model", "the list stays with the prompt")
}

func TestSelectedReplyRailCoversWrappedRows(t *testing.T) {
	m := listModel(t, 40, 14)
	press(t, m, "tab")
	rows := screenOf(m)
	n := 0
	for _, r := range rows {
		if strings.HasPrefix(r, "┃") {
			n++
		}
	}
	require.Equal(t, 4, n, "three wrapped rows and the usage row")
	for i, r := range rows {
		if strings.Contains(r, "claude-sonnet-5-5 ·") {
			require.True(t, strings.HasPrefix(r, "┃"), "usage row %d", i)
		}
	}
}

func TestSelectedCollapsedStepIsShadedAndHasNoRail(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24)
	press(t, m, "tab", "k")
	rows, plain := rawRows(m), screenOf(m)
	shaded := 0
	for i, r := range rows {
		if hasShade(m.theme, r) {
			shaded++
			require.Contains(t, plain[i], "Ran go test ./...")
			require.False(t, strings.Contains(plain[i], "┃"))
			require.Equal(t, 80, displayWidth(ansi.Strip(r)), "the shade covers the whole row")
		}
	}
	require.Equal(t, 1, shaded)
	for _, p := range plain {
		require.False(t, strings.HasPrefix(p, "┃"))
	}

	// Expanded: the shade goes and the rail covers every row of the block.
	press(t, m, "right")
	rows, plain = rawRows(m), screenOf(m)
	for i, r := range rows {
		require.False(t, hasShade(m.theme, r), plain[i])
	}
	start := -1
	for i, p := range plain {
		if strings.Contains(p, "Ran go test") {
			start = i
		}
	}
	require.Equal(t, "┃ ⌄ Ran go test ./...                                                  ✗ exit 1", strings.TrimRight(plain[start], " "))
	for i := start; i < start+11; i++ {
		require.True(t, strings.HasPrefix(plain[i], "┃"), "row %d: %q", i, plain[i])
	}
	require.False(t, strings.HasPrefix(plain[start+11], "┃"), "the empty row below the block has no rail")

	press(t, m, "left")
	rows = rawRows(m)
	shaded = 0
	for _, r := range rows {
		if hasShade(m.theme, r) {
			shaded++
		}
	}
	require.Equal(t, 1, shaded, "the shade is back when the block is one row again")
}

func TestSelectedStepWithoutColorIsReversed(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k")
	var row string
	for _, r := range rawRows(m) {
		if strings.Contains(ansi.Strip(r), "Ran go test") {
			row = r
		}
	}
	require.True(t, strings.HasPrefix(row, ansiRev), "reverse starts the row: %q", row)
	require.Equal(t, 80, displayWidth(ansi.Strip(row)))
	// Every style reset inside the row starts the reverse again.
	require.Equal(t, strings.Count(row, ansiReset), strings.Count(row, ansiReset+ansiRev)+1)
}

// twoRowStep is a step of two rows at its lowest mode, as a hook step is.
type twoRowStep struct{}

func (b twoRowStep) Lines(rc RenderContext) []string {
	r := stepRow{glyph: "⚓", glyphRole: RoleTool, verb: "PreToolUse hook", what: "(project/lint-guard)", indicator: indicatorFor(rc, FoldCollapsed)}
	row, _ := r.render(rc)
	rows := []string{row, "    " + rc.Theme.Render(RoleMuted, "Runs gofmt on the file")}
	if rc.Fold != FoldCollapsed {
		rows = append(rows, "    "+rc.Theme.Render(RoleFg, "more detail"))
	}
	return rows
}
func (twoRowStep) foldable(int) bool      { return true }
func (twoRowStep) defaultFold() FoldMode  { return FoldCollapsed }
func (twoRowStep) collapseFold() FoldMode { return FoldCollapsed }
func (twoRowStep) foldGroup() foldGroup   { return groupTool }

func TestSelectedCollapsedStepOfTwoRowsIsShadedOnBothRows(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24)
	require.NoError(t, m.transcript.Append("hook", twoRowStep{}, true))
	press(t, m, "tab")
	require.Equal(t, "hook", m.selectedBlock(), "the last block is selected")
	plain := screenOf(m)
	rows := rawRows(m)
	var shaded []string
	for i, r := range rows {
		if hasShade(m.theme, r) {
			shaded = append(shaded, plain[i])
		}
	}
	require.Equal(t, []string{"  ›  PreToolUse hook (project/lint-guard)", "    Runs gofmt on the file"}, shaded)

	// One cycle step: both rows leave the shade and take the rail.
	press(t, m, "e")
	for _, r := range rawRows(m) {
		require.False(t, hasShade(m.theme, r))
	}
	got := 0
	for _, p := range screenOf(m) {
		if strings.HasPrefix(p, "┃") {
			got++
		}
	}
	require.Equal(t, 3, got)
}

func TestSelectedPromptKeepsItsBandUnderTheRail(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24)
	press(t, m, "tab", "g")
	rows, plain := rawRows(m), screenOf(m)
	i := 2
	require.True(t, strings.HasPrefix(plain[i], "┃ ❯ run seq 1 40"), plain[i])
	band := bgSequence(m.theme, RolePanel)
	require.Contains(t, rows[i], band)
	require.Equal(t, 80, displayWidth(ansi.Strip(rows[i])))
	require.False(t, strings.HasPrefix(plain[i+1], "┃"), "the empty row after the band has no rail")
}

func TestMovingTheSelectionNeverReflows(t *testing.T) {
	m := listModel(t, 80, 24)
	plain := screenOf(m)
	press(t, m, "tab")
	base := len(screenOf(m))
	for _, k := range []string{"k", "k", "k", "j", "g", "G"} {
		press(t, m, k)
		require.Len(t, screenOf(m), base)
	}
	// The text after the gutter and the glyph column is the same with and
	// without a selection.
	tail := func(r string) string {
		runes := []rune(r)
		if len(runes) < 4 {
			return ""
		}
		return string(runes[4:])
	}
	press(t, m, "k", "k")
	marked := screenOf(m)
	for i := 2; i <= 12; i++ {
		require.Equal(t, tail(plain[i]), tail(marked[i]), "row %d", i)
	}
}

// --- expand indicator ---

func TestSelectedCollapsedStepShowsTheExpandIndicator(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k")
	text := strings.Join(screenOf(m), "\n")
	require.Regexp(t, `(?m)^  › Ran go test \./\.\.\.`, text)
	require.Regexp(t, `(?m)^  ◆ Ran seq 1 40$`, text, "other steps keep their glyph")
	require.Contains(t, text, "  ❯ run seq 1 40, then read parser.go", "the prompt band keeps its text")
	require.Contains(t, text, "  Count now skips empty fields.", "the reply keeps its text")
	press(t, m, "esc")
	require.Regexp(t, `(?m)^  ◆ Ran go test \./\.\.\.`, strings.Join(screenOf(m), "\n"), "the prompt focus shows the glyph")
}

func TestHoverShowsTheExpandIndicatorWithoutSelection(t *testing.T) {
	m := listModel(t, 80, 24)
	read := toolID(t, m, "read", 0)
	require.Contains(t, rowWith(t, m, "Read parser.go"), "◆ Read parser.go")
	m.list.hover = read
	m.View()
	require.Regexp(t, `^  › Read parser\.go`, rowWith(t, m, "Read parser.go"))
	require.NotContains(t, strings.Join(screenOf(m), "\n"), "┃", "a hover is no selection")
	m.list.hover = ""
	m.View()
	require.Regexp(t, `^  ◆ Read parser\.go`, rowWith(t, m, "Read parser.go"))
}

func TestIndicatorForExpandedAndTruncatedSteps(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k", "e")
	require.Regexp(t, `(?m)^┃ ◆ Ran go test`, strings.Join(screenOf(m), "\n"), "truncated keeps its glyph")
	press(t, m, "e")
	require.Regexp(t, `(?m)^┃ ⌄ Ran go test`, strings.Join(screenOf(m), "\n"))
	press(t, m, "esc")
	require.Regexp(t, `(?m)^  ◆ Ran go test`, strings.Join(screenOf(m), "\n"), "an open step that is not pointed at keeps its glyph")
	m.list.hover = toolID(t, m, "bash", 1)
	m.View()
	require.Regexp(t, `(?m)^  ⌄ Ran go test`, strings.Join(screenOf(m), "\n"), "hover shows it on an open step")
}

func TestNonFoldableBlocksNeverShowAnIndicator(t *testing.T) {
	m := listModel(t, 80, 24)
	prompt := blockID(t, m, isPrompt)
	reply := blockID(t, m, isReply)
	before := screenOf(m)
	for _, id := range []string{prompt, reply} {
		m.list.hover = id
		m.View()
		require.Equal(t, before, screenOf(m), "hover on a block that cannot fold changes nothing")
	}
	m.list.hover = ""
	// A job notice is one row and has no more to show.
	require.NoError(t, m.transcript.Append("job", newJobNotice("bash", "sleep 9", ToolSucceeded, "done", "", NewTranscriptView()), true))
	m.list.hover = "job"
	m.View()
	require.Regexp(t, `(?m)^  ◆ `, rowWith(t, m, "sleep 9"))
	require.NotContains(t, rowWith(t, m, "sleep 9"), "›")
}

func TestExpandIndicatorOfAWideGlyphKeepsTheRowWidth(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	base := stepRow{glyph: "⚓", glyphRole: RoleTool, verb: "PreToolUse hook", what: "(project/lint-guard) · edit parser.go", right: "✓ allowed 0.1s", rightRole: RoleSuccess}
	rc := RenderContext{Width: 80, Theme: th}
	plain, _ := base.render(rc)
	base.indicator = IndicatorExpand
	marked, _ := base.render(rc)
	require.Equal(t, 80, displayWidth(ansi.Strip(plain)), "the glyph takes two columns")
	require.Equal(t, 80, displayWidth(ansi.Strip(marked)))
	require.True(t, strings.HasPrefix(ansi.Strip(marked), "  ›  PreToolUse hook"), ansi.Strip(marked))

	one := stepRow{glyph: "◆", glyphRole: RoleTool, verb: "Ran", what: "x", indicator: IndicatorOpen}
	row, _ := one.render(rc)
	require.True(t, strings.HasPrefix(ansi.Strip(row), "  ⌄ Ran x"))
}

func TestIndicatorIsAccentBold(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24)
	press(t, m, "esc")
	m.list.hover = toolID(t, m, "read", 0)
	m.View()
	var row string
	for _, r := range rawRows(m) {
		if strings.Contains(ansi.Strip(r), "Read parser.go") {
			row = r
		}
	}
	require.Contains(t, row, m.theme.Style(RoleAccent).Bold(true).Render("›"))
}

func TestStreamingThinkingAtItsLowestModeShowsTheIndicatorAndARail(t *testing.T) {
	events := flat(
		&protocol.AgentStart{},
		assistantStart("m", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "a\nb\nc"}),
	)
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, events...)
	press(t, m, "tab")
	require.Equal(t, blockID(t, m, isThinking), m.selectedBlock())
	text := strings.Join(screenOf(m), "\n")
	require.Regexp(t, `(?m)^┃ › Thinking…`, text)
}

// --- user-set mode ---

func TestCollapsingAStreamingThinkingFoldsItAgainWhenItEnds(t *testing.T) {
	m := New(Options{Slots: foldSource(), Profile: colorprofile.NoTTY})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	trl := NewTranslator(TranslatorOptions{Location: time.UTC})
	play := func(evs ...protocol.Event) {
		for _, ev := range evs {
			for _, msg := range trl.Event(ev) {
				m.Update(msg)
			}
		}
	}
	play(assistantStart("m", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "a\nb\nc"}))
	m.View()
	id := blockID(t, m, isThinking)
	require.NoError(t, m.pane.ExpandBlock(id))
	require.NoError(t, m.pane.CollapseBlock(id))
	play(updateAt(900, protocol.ThinkingEndEvent{ContentIndex: 0, Content: "a\nb\nc"}),
		assistantEndUsage("m", protocol.StopStop, 1000, protocol.Usage{}, protocol.Thinking{Thinking: "a\nb\nc"}))
	m.View()
	require.Equal(t, FoldCollapsed, m.pane.Fold(id), "folding back cleared the user's choice")
}

func TestCyclingBackToTheLowestModeClearsTheUsersChoice(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	id := blockID(t, m, isThinking)
	require.NoError(t, m.pane.CycleFold(id))
	require.NoError(t, m.pane.CycleFold(id))
	require.True(t, m.pane.items[m.transcript.index[id]].userSet)
	require.NoError(t, m.pane.CycleFold(id))
	require.False(t, m.pane.items[m.transcript.index[id]].userSet)
}

// --- screens ---

func TestBlockListScreens(t *testing.T) {
	t.Run("reply_selected_80x24", func(t *testing.T) {
		m := listModel(t, 80, 24)
		press(t, m, "tab")
		requireScreenGolden(t, m)
	})
	t.Run("step_selected_80x24", func(t *testing.T) {
		m := listModel(t, 80, 24)
		press(t, m, "tab", "k")
		requireScreenGolden(t, m)
	})
	t.Run("step_truncated_80x24", func(t *testing.T) {
		m := listModel(t, 80, 24)
		press(t, m, "tab", "k", "e")
		requireScreenGolden(t, m)
	})
	t.Run("edit_expanded_80x24", func(t *testing.T) {
		m := listModel(t, 80, 24)
		press(t, m, "tab", "k", "k", "right")
		requireScreenGolden(t, m)
	})
	t.Run("reply_selected_40x14", func(t *testing.T) {
		m := listModel(t, 40, 14)
		press(t, m, "tab")
		requireScreenGolden(t, m)
	})
	t.Run("step_selected_40x14", func(t *testing.T) {
		m := listModel(t, 40, 14)
		press(t, m, "tab", "k")
		requireScreenGolden(t, m)
	})
	t.Run("viewer_tool_80x24", func(t *testing.T) {
		m := listModel(t, 80, 24)
		openTool(t, m, 0)
		requireScreenGolden(t, m)
	})
	t.Run("viewer_failed_tool_80x24", func(t *testing.T) {
		m := listModel(t, 80, 24)
		openTool(t, m, 1)
		requireScreenGolden(t, m)
	})
	t.Run("viewer_tool_40x14", func(t *testing.T) {
		m := listModel(t, 40, 14)
		openTool(t, m, 0)
		requireScreenGolden(t, m)
	})
}
