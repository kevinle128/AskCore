package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// These tests drive whole alternate-screen scenarios as a user does: keys,
// the mouse wheel and pointer, and agent events. Each one reads the plain
// screen rows. Narrower checks of one part live in the tests of that part.

// plainScreen returns the rows of the frame without styles and without any
// change to their width.
func plainScreen(m *Model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

// requireReplyRail checks that every row of the selected reply, with its usage
// row, has the rail and that no row of the screen is shaded.
func requireReplyRail(t *testing.T, m *Model) {
	t.Helper()
	require.Equal(t, blockID(t, m, isReply), m.selectedBlock())
	raw, plain := rawRows(m), screenOf(m)
	var marked []string
	for i, r := range raw {
		require.False(t, hasShade(m.theme, r), "no row is shaded: %q", plain[i])
		if strings.HasPrefix(plain[i], "┃") {
			marked = append(marked, plain[i])
		}
	}
	require.Equal(t, []string{
		"┃ Count now skips empty fields. TestCount still fails because it expects the old",
		"┃ value; shall I fix it?",
		"┃ claude-sonnet-5-5 · 6.2s · ↑8.1k ↓320 · $0.006",
	}, marked)
}

func TestRunningToolShowsTheSpinnerAndTheWorkingStatusAt80x24(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(flat(
		&protocol.AgentStart{},
		userAt("run all tests and tell me what fails", sketchTime(15, 31)),
		toolStart("c1", "bash", `{"command":"go test ./... -count=1"}`, 0),
	)...)
	r.clock.advance(4 * time.Second)
	r.event(&protocol.QueueUpdate{})
	r.h.waitText("Working… 4s")
	grid := r.h.settle()
	require.Len(t, grid, 24)

	row := ""
	status := -1
	for i, g := range grid {
		if strings.Contains(g, "◆ Running go test ./... -count=1") {
			row = g
		}
		if strings.HasPrefix(g, "┊ ◌ Working…") {
			status = i
		}
	}
	require.Regexp(t, `^  ◆ Running go test \./\.\.\. -count=1 +[\x{2800}-\x{28FF}]( 4s)?$`, row)
	require.Equal(t, 19, status, "the status row sits above the prompt pane: %v", grid)
	require.Equal(t, ruleRow(80), grid[20])
}

func TestStreamingThinkingFoldsToOneThoughtRowWhenTheTurnEndsAt80x24(t *testing.T) {
	m, trl := listModelWithTranslator(t, colorprofile.NoTTY, 80, 24,
		flat(
			&protocol.AgentStart{},
			userAt("run all tests and tell me what fails", time.Date(2026, 10, 12, 15, 31, 0, 0, time.UTC)),
			assistantStart("m", 0),
			updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
			updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "Split on \",\" gives an empty string.\nSo the loop must skip f == \"\".\nThen the count is right."}),
		)...)
	text := strings.Join(screenOf(m), "\n")
	require.Regexp(t, `(?m)^  ◆ Thinking…`, text)
	require.Contains(t, text, "  ┃ …\n  ┃ So the loop must skip f == \"\".\n  ┃ Then the count is right.")

	playEvents(m, trl, flat(
		updateAt(1200, protocol.ThinkingEndEvent{ContentIndex: 0, Content: "Split on \",\" gives an empty string.\nSo the loop must skip f == \"\".\nThen the count is right."}),
		assistantEndUsage("m", protocol.StopStop, 1300, protocol.Usage{}, protocol.Thinking{Thinking: "x"}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
	text = strings.Join(screenOf(m), "\n")
	require.Equal(t, 1, strings.Count(text, "◆ Thought for 1.2s"))
	require.NotContains(t, text, "┃", "the folded thinking has no body rows")
	require.NotContains(t, text, "Thinking…")
	require.NotContains(t, text, "skip f ==")
}

func TestSecondFoldKeyOnTheFailedToolShowsTheCommandRowThenEveryOutputLine(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k", "e", "e")
	require.Contains(t, barOf(m), "e fold: expanded")
	rows := screenOf(m)
	at := rowIndexOf(t, rows, "Ran go test ./...")
	want := []string{"┃   $ go test ./..."}
	for _, line := range foldTestOutput {
		want = append(want, "┃   "+line)
	}
	require.Equal(t, want, rows[at+1:at+1+len(want)])
}

func TestFoldKeyOnAnEditOfALaterLineShowsTheDiffRowsThenOneRow(t *testing.T) {
	var oldLines, newLines []string
	for i := 1; i <= 8; i++ {
		oldLines = append(oldLines, fmt.Sprintf("// line %d", i))
	}
	newLines = append(newLines, oldLines...)
	oldLines = append(oldLines, "\tfor _, f := range fields {", "\t\tn++", "\t}", "\treturn n", "}")
	newLines = append(newLines, "\tfor _, f := range fields {", "\t\tif f != \"\" {", "\t\t\tn++", "\t\t}", "\t}", "\treturn n", "}")
	details, err := json.Marshal(protocol.ToolResultDetails{Kind: protocol.ToolKindEdit, Diff: &protocol.ToolDiff{
		Path:    "parser.go",
		OldText: strings.Join(oldLines, "\n") + "\n",
		NewText: strings.Join(newLines, "\n") + "\n",
	}})
	require.NoError(t, err)
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, flat(
		&protocol.AgentStart{},
		userAt("fix the count", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		toolStart("c3", "edit", `{"path":"parser.go"}`, 100),
		&protocol.ToolExecutionEnd{
			Envelope: at(200), ToolCallID: "c3", ToolName: "edit",
			Result: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "Edited parser.go."}}, Details: details},
		},
	)...)
	press(t, m, "tab")
	require.Equal(t, toolID(t, m, "edit", 0), m.selectedBlock())
	press(t, m, "e", "e")
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "Edited parser.go  +3 -1")
	require.Contains(t, rows[i], "⌄ Edited parser.go")
	body := strings.Join(rows[i+1:], "\n")
	require.Regexp(t, `(?m)^┃\s+10 - +n\+\+$`, body)
	require.Regexp(t, `(?m)^┃\s+10 \+ +if f != ""`, body)
	require.Contains(t, barOf(m), "e fold: expanded")

	press(t, m, "e")
	text := strings.Join(screenOf(m), "\n")
	require.NotRegexp(t, `\d+ [-+] `, text)
	require.Contains(t, text, "› Edited parser.go  +3 -1")
	require.Contains(t, barOf(m), "e fold: collapsed")
}

func TestSelectedReplyHasTheRailOnEveryRowWhenReachedFromBelowAndWithG(t *testing.T) {
	t.Run("k from the block below", func(t *testing.T) {
		events := flat(
			foldSessionEvents(),
			&protocol.AgentStart{},
			toolStart("c9", "bash", `{"command":"echo tail"}`, 7000),
			resultEnd("c9", "bash", "tail", protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, 7100),
		)
		m := listModelAt(t, colorprofile.TrueColor, 80, 24, events...)
		press(t, m, "tab")
		require.Equal(t, toolID(t, m, "bash", 2), m.selectedBlock(), "the step below the reply is selected first")
		press(t, m, "k")
		requireReplyRail(t, m)
	})
	t.Run("Tab then G", func(t *testing.T) {
		m := listModelAt(t, colorprofile.TrueColor, 80, 24)
		press(t, m, "tab", "g", "G")
		requireReplyRail(t, m)
	})
}

func TestPointerOverACollapsedHookShowsTheIndicatorUntilItLeaves(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, lintGuardHook(), noRmHook())
	y := rowIndexOf(t, screenOf(m), "(project/lint-guard)")
	require.Regexp(t, `^  ⚓ PreToolUse hook \(project/lint-guard\)`, screenOf(m)[y])

	hoverAt(m, 10, y)
	require.Regexp(t, `^  ›  PreToolUse hook \(project/lint-guard\)`, screenOf(m)[y])
	require.Empty(t, m.list.selected, "a pointer is no selection")

	hoverAt(m, 10, 0)
	require.Regexp(t, `^  ⚓ PreToolUse hook \(project/lint-guard\)`, screenOf(m)[y])
}

func TestFoldAllKeyMovesEveryToolThinkingAndHookBlockThereAndBack(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, lintGuardHook(), noRmHook())
	steps := 0
	press(t, m, "tab", "E")
	for _, e := range m.transcript.entries {
		fb, ok := e.block.(foldBlock)
		if !ok || !fb.foldGroup().isStep() {
			continue
		}
		steps++
		require.Equal(t, FoldExpanded, m.pane.Fold(e.id), e.id)
	}
	require.Equal(t, 2+4+1, steps, "two hooks, four tools and the thinking")
	require.Equal(t, FoldExpanded, m.pane.Fold(blockID(t, m, isThinking)))
	text := strings.Join(screenOf(m), "\n")
	require.Contains(t, text, "Event    PreToolUse")

	press(t, m, "E")
	for _, e := range m.transcript.entries {
		if fb, ok := e.block.(foldBlock); ok && fb.foldGroup().isStep() {
			require.Equal(t, FoldCollapsed, m.pane.Fold(e.id), e.id)
		}
	}
	text = strings.Join(screenOf(m), "\n")
	require.NotContains(t, text, "Event    PreToolUse")
	require.NotContains(t, text, "$ ")
}

func TestEnterOnTheSelectedToolRowOpensTheViewerAndEscReturnsToTheSameList(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k", "k", "k", "k")
	require.Equal(t, toolID(t, m, "bash", 0), m.selectedBlock())
	before := screenOf(m)
	require.Regexp(t, `(?m)^  › Ran seq 1 40$`, strings.Join(before, "\n"))

	press(t, m, "enter")
	require.True(t, m.viewerOpen())
	rows := screenOf(m)
	require.Contains(t, rows[0], "← back  Ran seq 1 40")
	require.Equal(t, "    $ seq 1 40", rows[2])
	for i := 1; i <= 18; i++ {
		require.Equal(t, fmt.Sprintf("    %d", i), rows[3+i])
	}

	press(t, m, "esc")
	require.False(t, m.viewerOpen())
	require.Equal(t, before, screenOf(m))
	require.Equal(t, toolID(t, m, "bash", 0), m.selectedBlock())
}

func TestWheelUpShowsTheEndMarkerThenANewToolEventCountsTheRowsBelow(t *testing.T) {
	m, trl := listModelWithTranslator(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	require.True(t, m.pane.Following())
	mouse(m, tea.MouseWheelMsg(wheelAt(10, m.geo.PaneTop+2, true)))
	require.False(t, m.pane.Following())
	pane := paneScreen(m)
	pad, text := centered(pane[len(pane)-1])
	require.Equal(t, "▼ End", text)
	require.Equal(t, (80-displayWidth("▼ End"))/2, pad)
	top := pane[0]

	playEvents(m, trl, flat(
		&protocol.AgentStart{},
		toolStart("n1", "bash", `{"command":"go vet ./..."}`, 9000),
		resultEnd("n1", "bash", "ok", protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, 9100),
		toolStart("n2", "bash", `{"command":"go build ./..."}`, 9200),
	)...)
	pane = paneScreen(m)
	require.Equal(t, top, pane[0], "new output does not move the window")
	want := rowsBelowWindow(t, m)
	require.Positive(t, want)
	pad, text = centered(pane[len(pane)-1])
	require.Equal(t, fmt.Sprintf("▼ %d new lines · End", want), text)
	require.Equal(t, (80-displayWidth(text))/2, pad)
}

// sketchReplyOfTheHookTurn is a reply that wraps at 80 columns so that the
// second row starts with "directory.".
const sketchReplyOfTheHookTurn = "Count now skips empty fields. The hook no-rm blocked the removal of the build directory. TestCount still fails because it expects the old value; shall I fix it?"

func TestDragOverWrappedReplyTextCopiesItAndTheStatusRowSaysCopied(t *testing.T) {
	events := flat(
		&protocol.AgentStart{},
		userAt("fix the count", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		assistantStart("claude-sonnet-5-5", 0),
		textParts(0, sketchReplyOfTheHookTurn),
		assistantEndUsage("claude-sonnet-5-5", protocol.StopStop, 6200, protocol.Usage{}, protocol.Text{Text: sketchReplyOfTheHookTurn}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, events...)
	f := withFakeClipboard(m)

	const picked = "directory. TestCount still fails"
	y := rowIndexOf(t, screenOf(m), picked)
	x0 := colOf(t, screenOf(m)[y], picked)
	x1 := x0 + displayWidth(picked) - 1
	mouse(m, tea.MouseClickMsg(leftAt(x0, y)))
	mouse(m, tea.MouseMotionMsg(leftAt(x1, y)))
	cmd := m.sv.onMouse(tea.MouseReleaseMsg(leftAt(x1, y)))
	require.NotNil(t, cmd)
	done, ok := cmd().(clipboardDoneMsg)
	require.True(t, ok)
	require.Equal(t, picked, done.text)
	mouse(m, done)
	require.Equal(t, []clipRun{{name: "pbcopy", input: picked}}, f.calls())

	rows := screenOf(m)
	require.Equal(t, "┊ ✓ Copied", rows[m.geo.PromptTop-2], "the status row is above the rule of the prompt pane")
}

// sketchScenarios40x14 builds the screens of the narrow terminal.
func sketchScenarios40x14(t *testing.T) []struct {
	name   string
	model  *Model
	header bool
} {
	t.Helper()
	compact := listModel(t, 40, 14)

	truncated := listModel(t, 40, 14)
	press(t, truncated, "tab", "k", "e")

	streaming := listModelAt(t, colorprofile.NoTTY, 40, 14, flat(
		&protocol.AgentStart{},
		userAt("run all tests and tell me what fails", time.Date(2026, 10, 12, 15, 31, 0, 0, time.UTC)),
		assistantStart("m", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "Split on \",\" gives an empty string for \"a,,b\".\nSo the loop must skip f == \"\"."}),
		toolStart("c1", "bash", `{"command":"go test ./... -count=1"}`, 100),
	)...)

	long := "the parser counts empty fields. Read parser.go and parser_test.go, find why Count returns 3, fix it so empty fields are skipped, keep the public API the same, run the tests, and then tell me what changed."
	longPrompt := listModelAt(t, colorprofile.NoTTY, 40, 14, flat(
		&protocol.AgentStart{},
		userAt(long, time.Date(2026, 10, 12, 15, 44, 0, 0, time.UTC)),
		assistantStart("m", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "x"}),
		updateAt(1200, protocol.ThinkingEndEvent{ContentIndex: 0, Content: "x"}),
		toolStart("c2", "read", `{"path":"parser.go"}`, 1300),
		resultEnd("c2", "read", "package p", protocol.ToolResultDetails{}, false, 1400),
		&protocol.AgentSettled{},
	)...)

	toolViewer := listModel(t, 40, 14)
	openTool(t, toolViewer, 0)

	hookViewer := hookModel(t, colorprofile.NoTTY, 40, 14, noRmHook())
	press(t, hookViewer, "tab", "enter")

	scrolled := listModel(t, 40, 14)
	mouse(scrolled, tea.MouseWheelMsg(wheelAt(10, scrolled.geo.PaneTop+1, true)))
	addNotes(t, scrolled, 2)

	return []struct {
		name   string
		model  *Model
		header bool
	}{
		{"compact session in the prompt focus", compact, true},
		{"truncated failed tool", truncated, true},
		{"turn that streams", streaming, true},
		{"long prompt above three rows", longPrompt, true},
		{"viewer of a tool", toolViewer, false},
		{"viewer of a hook", hookViewer, false},
		{"scrolled up while new output comes", scrolled, true},
	}
}

// The renderer does not pad a row with trailing blanks: a blank cell and a
// missing cell look the same. A row of the narrow screens is never wider than
// 40 columns, and the rows that run to the edge (the header with its right
// side and the rules) are exactly 40 columns.
func TestEveryRowOfTheNarrowScreensFitsFortyColumns(t *testing.T) {
	for _, sc := range sketchScenarios40x14(t) {
		t.Run(sc.name, func(t *testing.T) {
			rows := plainScreen(sc.model)
			require.Len(t, rows, 14)
			for i, r := range rows {
				require.LessOrEqual(t, displayWidth(r), 40, "row %d: %q", i+1, r)
				if strings.Trim(r, "─") == "" && r != "" {
					require.Equal(t, 40, displayWidth(r), "a rule runs to the edge: row %d", i+1)
				}
			}
			if sc.header {
				require.Equal(t, 40, displayWidth(rows[0]), "the header runs to the edge: %q", rows[0])
				require.True(t, strings.HasPrefix(rows[0], "◢◤ ask ● 7c1e"), rows[0])
				require.Equal(t, strings.Repeat("─", 40), rows[1])
			} else {
				require.True(t, strings.HasPrefix(rows[0], " ← back"), "a viewer replaces the header: %q", rows[0])
				require.Equal(t, 40, displayWidth(rows[0]), "the outcome ends at the edge: %q", rows[0])
			}
		})
	}
}

func TestResizeTo120x40KeepsTheSelectedBlockInViewAndTheBarOnTheLastRow(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	press(t, m, "tab")
	for range 15 {
		press(t, m, "k")
	}
	sel := m.selectedBlock()
	require.False(t, m.pane.Following())
	selectedRow := regexp.MustCompile(`(?m)^  › Ran step \d+`)
	require.Regexp(t, selectedRow, strings.Join(screenOf(m), "\n"))

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	rows := screenOf(m)
	require.Len(t, rows, 40)
	require.Equal(t, sel, m.selectedBlock())
	f := m.pane.Frame()
	idx := m.transcript.index[sel]
	require.GreaterOrEqual(t, idx, f.FirstBlock, "the selection is in the window")
	require.LessOrEqual(t, idx, f.LastBlock)
	require.Regexp(t, selectedRow, strings.Join(rows, "\n"), "the selected row shows")
	require.Contains(t, rows[39], "j/k select", "the bar is on row 40")
}

func TestResizeTo40x14KeepsTheSelectedBlockOnTheTerminal(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	h := startScreen(t, m, 80, 24)
	h.send(keyNamed(t, "tab"))
	h.send(keyNamed(t, "home"))
	for range 9 {
		h.send(keyNamed(t, "j"))
	}
	h.waitText("› Ran step 7")
	h.resize(40, 14)
	require.Contains(t, strings.Join(h.settle(), "\n"), "› Ran step 7")
	final := h.stop().(*Model)
	require.Equal(t, FocusBlocks, final.Focus())
	require.False(t, final.pane.Following(), "resize keeps free scroll")
}

func TestResizeTo120x40KeepsFreeScrollWhenTheEndFits(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(20)...)
	h := startScreen(t, m, 80, 24)
	h.send(tea.MouseWheelMsg(wheelAt(10, 5, true)))
	h.waitText("▼ End")
	h.resize(120, 40)
	require.Contains(t, strings.Join(h.settle(), "\n"), "▼ End")
	final := h.stop().(*Model)
	require.False(t, final.pane.Following())
}

func TestResizeKeepsFreeScrollWhenTheSelectedReplyWasBelowTheWindow(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	h := startScreen(t, m, 80, 24)
	h.send(keyNamed(t, "tab"))
	h.waitText("┃ all done")
	h.send(tea.MouseWheelMsg(wheelAt(10, 5, true)))
	h.waitText("▼ End")
	h.resize(40, 14)
	rows := strings.Join(h.settle(), "\n")
	require.Contains(t, rows, "┃ all done")
	require.Contains(t, rows, "▼ End")
	final := h.stop().(*Model)
	require.False(t, final.pane.Following())
}
