package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// Every step kind draws through one helper: the mark of its first row and the
// layout of its open form are the same for thinking, tool and hook steps.

// sixThoughts are the six lines of thinking of the example turn.
var sixThoughts = []string{
	"The test fails on an empty field.",
	"parser.go splits on commas and counts every part.",
	"An empty part still adds one to the count.",
	`So the loop must skip f == "".`,
	"Then TestCount gets 2, not 3.",
	"I will edit Count and run the tests again.",
}

// thinkingTurnEvents is a turn with six lines of thinking and a one-line
// reply. With ended false the thinking still streams and there is no reply.
func thinkingTurnEvents(ended bool) []protocol.Event {
	text := strings.Join(sixThoughts, "\n")
	events := []any{
		&protocol.AgentStart{},
		userAt("run the tests and fix what fails", time.Date(2026, 10, 12, 15, 31, 0, 0, time.UTC)),
		assistantStart("claude-sonnet-5-5", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(100, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: text}),
	}
	if ended {
		reply := "Count now skips empty fields, and go test ./... passes."
		events = append(events,
			updateAt(4200, protocol.ThinkingEndEvent{ContentIndex: 0, Content: text}),
			textParts(1, reply),
			assistantEndUsage("claude-sonnet-5-5", protocol.StopStop, 9400, protocol.Usage{Input: 8100, Output: 410},
				protocol.Thinking{Thinking: text}, protocol.Text{Text: reply}),
			&protocol.AgentEnd{}, &protocol.AgentSettled{},
		)
	}
	return flat(events...)
}

// requireOpenStep checks the open form of a step on the screen: the header
// row, one blank row, then the body rows at column 4. It returns the row of
// the header.
func requireOpenStep(t *testing.T, rows []string, header string, body []string) int {
	t.Helper()
	h := rowIndexOf(t, rows, header)
	require.True(t, strings.HasPrefix(rows[h], "  › "), "an open step shows the open mark: %q", rows[h])
	require.Empty(t, rows[h+1], "one blank row follows the header")
	for i, want := range body {
		require.Equal(t, "    "+want, rows[h+2+i], "body row %d is at column 4", i)
	}
	return h
}

func requireNoRail(t *testing.T, rows []string) {
	t.Helper()
	for _, r := range rows {
		require.NotContains(t, r, "┃", "no row shows the rail")
	}
}

func TestExpandedThinkingShowsHeaderBlankRowAndAllLinesAtColumnFour(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, thinkingTurnEvents(true)...)
	require.NoError(t, m.pane.ExpandBlock(blockID(t, m, isThinking)))
	rows := screenOf(m)
	h := requireOpenStep(t, rows, "Thought for", sixThoughts)
	require.Regexp(t, `^  › Thought for 4\.2s$`, rows[h])
	requireNoRail(t, rows)
}

func TestCollapsedEndedThinkingIsOneRowWithItsOwnMark(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, thinkingTurnEvents(true)...)
	require.Equal(t, FoldCollapsed, m.pane.Fold(blockID(t, m, isThinking)))
	rows := screenOf(m)
	h := rowIndexOf(t, rows, "Thought for")
	require.Regexp(t, `^  ◆ Thought for`, rows[h])
	require.Empty(t, rows[h+1], "the gap row follows the one row")
	require.Contains(t, rows[h+2], "Count now skips empty fields", "the reply follows the gap row")
}

func TestTruncatedEndedThinkingShowsEllipsisAndTheLastThreeLines(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, thinkingTurnEvents(true)...)
	require.NoError(t, m.pane.CycleFold(blockID(t, m, isThinking)))
	rows := screenOf(m)
	requireOpenStep(t, rows, "Thought for", append([]string{"…"}, sixThoughts[3:]...))
	requireNoRail(t, rows)
}

func TestStreamingThinkingShowsHeaderBlankRowAndTheLastLines(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, thinkingTurnEvents(false)...)
	rows := screenOf(m)
	requireOpenStep(t, rows, "Thinking…", append([]string{"…"}, sixThoughts[4:]...))
	requireNoRail(t, rows)
}

func TestSelectedCollapsedThinkingShowsTheOpenMarkInABox(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	for m.selectedBlock() != blockID(t, m, isThinking) {
		press(t, m, "k")
	}
	row := rowIndex(t, m, "Thought for")
	require.Regexp(t, `^│ › Thought for 0\.5s`, screenOf(m)[row])
	requireBox(t, m, row, row)
	press(t, m, "esc")
	require.Regexp(t, `^  ◆ Thought for 0\.5s`, rowWith(t, m, "Thought for"), "without the selection the step shows its own mark")
}

func TestSelectedCollapsedToolShowsTheOpenMarkInABox(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k")
	row := rowIndex(t, m, "Ran go test ./...")
	require.Regexp(t, `^│ › Ran go test \./\.\.\.`, screenOf(m)[row])
	requireBox(t, m, row, row)
	require.Regexp(t, `^  ◆ Read parser\.go$`, rowWith(t, m, "Read parser.go"), "other steps keep their own mark")
}

func TestExpandedToolShowsHeaderBlankRowThenBodyAtColumnFour(t *testing.T) {
	m := foldModel(t, 80, 40)
	m.View()
	require.NoError(t, m.pane.ExpandBlock(toolID(t, m, "bash", 1)))
	rows := screenOf(m)
	requireOpenStep(t, rows, "Ran go test ./...", append([]string{"$ go test ./..."}, foldTestOutput...))
}

func TestOpenStepWithoutBodyRowsIsItsHeaderAlone(t *testing.T) {
	th := noColor()
	tool := newToolBlock("bash", "true", ToolSucceeded, "", "", toolOutput{}, nil)
	rows := plain(renderRows(tool, RenderContext{Width: 80, Theme: th, Fold: FoldTruncated}))
	require.Equal(t, []string{"  › Ran true"}, rows, "no blank row when there is no body")
	thinking := newThinkingBlock("", nil, false, 0, 0, false)
	require.Equal(t, []string{"  › Thought"}, plain(renderRows(thinking, RenderContext{Width: 80, Theme: th, Fold: FoldExpanded})))
}

// stepKinds are one block of each step kind, each with a body in every open
// form.
func stepKinds() map[string]Block {
	view := NewTranscriptView()
	return map[string]Block{
		"thinking": newThinkingBlock(fixtureLines("thought", 9), view, false, 0, 0, false),
		"tool":     newToolBlock("bash", "seq 9", ToolSucceeded, "", "", toolOutput{text: fixtureLines("l", 9), lines: strings.Split(fixtureLines("l", 9), "\n")}, view),
		"hook":     NewHookBlock(noRmHook(), view),
	}
}

func TestEveryStepKindHasTheSameMarkAndOpenLayout(t *testing.T) {
	th := noColor()
	const width = 60
	for kind, b := range stepKinds() {
		// The hook keeps its description row under the header in every form.
		lead := 0
		if kind == "hook" {
			lead = 1
		}
		glyph := GlyphStep
		if kind == "hook" {
			glyph = GlyphHook
		}
		collapsed := plain(renderRows(b, RenderContext{Width: width, Theme: th, Fold: FoldCollapsed}))
		require.Len(t, collapsed, 1+lead, kind)
		for _, mode := range []FoldMode{FoldCollapsed, FoldTruncated, FoldExpanded} {
			for _, pointed := range []bool{false, true} {
				rows := plain(renderRows(b, RenderContext{Width: width, Theme: th, Fold: mode, Pointed: pointed}))
				open := mode != FoldCollapsed
				mark := glyph
				if open || pointed {
					mark = GlyphStepOpen
				}
				require.True(t, strings.HasPrefix(rows[0], "  "+mark+" "), "%s %v pointed=%v: %q", kind, mode, pointed, rows[0])
				require.Equal(t, displayWidth(collapsed[0]), displayWidth(rows[0]), "%s: the mark does not move the text", kind)
				if !open {
					require.Len(t, rows, 1+lead, "%s %v: the folded form has no body", kind, mode)
					continue
				}
				require.Greater(t, len(rows), 2+lead, "%s %v has body rows", kind, mode)
				require.Empty(t, rows[1+lead], "%s %v: one blank row after the header", kind, mode)
				for _, r := range rows[2+lead:] {
					if r == "" {
						continue
					}
					require.True(t, strings.HasPrefix(r, "    "), "%s %v: body row at column 4: %q", kind, mode, r)
				}
			}
		}
	}
}

// The text of a step ends textRightPad columns before the right edge, as the
// text of every block does, so that the selection box covers none of it.
func TestStepRowsAndBodiesKeepTheRightMargin(t *testing.T) {
	th := noColor()
	for _, width := range []int{40, 60, 80} {
		for kind, b := range stepKinds() {
			for _, mode := range []FoldMode{FoldCollapsed, FoldTruncated, FoldExpanded} {
				for _, r := range renderRows(b, RenderContext{Width: width, Theme: th, Fold: mode}) {
					require.LessOrEqual(t, displayWidth(strings.TrimRight(ansi.Strip(r), " ")), width-textRightPad,
						"%s %v at %d: %q", kind, mode, width, ansi.Strip(r))
				}
			}
		}
	}
}

func TestFailedToolResultEndsTwoColumnsBeforeTheEdge(t *testing.T) {
	m := foldModel(t, 80, 40)
	row := rowWith(t, m, "◆ Ran go test")
	require.True(t, strings.HasSuffix(row, "✗ exit 1"), row)
	require.Equal(t, 80-textRightPad, displayWidth(row))
}

func TestStreamingThinkingSpinnerEndsTwoColumnsBeforeTheEdge(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, thinkingTurnEvents(false)...)
	row := rowWith(t, m, "Thinking…")
	require.True(t, strings.HasSuffix(row, SpinnerFrame(0)), row)
	require.Equal(t, 80-textRightPad, displayWidth(row))
}

func TestStepMarkKeepsTheRoleColor(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	for kind, b := range stepKinds() {
		role := RoleThinking
		switch kind {
		case "tool":
			role = RoleTool
		case "hook":
			role = RoleHook
		}
		row := renderRows(b, RenderContext{Width: 80, Theme: th, Fold: FoldExpanded})[0]
		require.Contains(t, row, th.Render(role, GlyphStepOpen), kind)
		require.NotContains(t, row, th.Style(RoleAccent).Bold(true).Render(GlyphStepOpen), kind)
	}
}

func TestThinkingBodyIsInTheThinkingRoleAndNotItalic(t *testing.T) {
	for _, p := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
		th := NewTheme(p)
		rows := renderRows(NewThinkingBlock("pondering", NewTranscriptView()), RenderContext{Width: 60, Theme: th, Fold: FoldExpanded})
		require.Len(t, rows, 3)
		require.Equal(t, "    "+th.Render(RoleThinking, "pondering"), rows[2], "%v", p)
		require.False(t, hasParam(sgrParams(rows[2]), "3"), "%v: the thinking body is not italic", p)
	}
}
