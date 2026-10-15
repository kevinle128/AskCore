package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// A model without a program, with the compact session of the design: a
// prompt, a thinking, four tool steps and a reply. The tests call Update and
// read View, and change folds through the pane.

const (
	foldPrompt = "run seq 1 40, then read parser.go and fix the count of empty fields"
	foldReply  = "Count now skips empty fields. TestCount still fails because it expects the old value; shall I fix it?"
)

var foldTestOutput = []string{
	"--- FAIL: TestCount (0.00s)",
	"    parser_test.go:12: want 2, got 3",
	"=== RUN   TestCountEmpty",
	"--- PASS: TestCountEmpty (0.00s)",
	"=== RUN   TestCountOne",
	"--- PASS: TestCountOne (0.00s)",
	"FAIL",
	"FAIL    example.com/p/parser    0.012s",
	"FAIL",
}

func foldSessionEvents() []protocol.Event {
	seq := make([]string, 40)
	for i := range seq {
		seq[i] = fmt.Sprint(i + 1)
	}
	editDetails, _ := json.Marshal(protocol.ToolResultDetails{Kind: protocol.ToolKindEdit, Diff: &protocol.ToolDiff{
		Path:    "parser.go",
		OldText: "func Count(s string) int {\n\tn := 0\n\tfor _, f := range fields(s) {\n\t\tn++\n\t}\n\treturn n\n}\n",
		NewText: "func Count(s string) int {\n\tn := 0\n\tfor _, f := range fields(s) {\n\t\tif f != \"\" {\n\t\t\tn++\n\t\t}\n\t}\n\treturn n\n}\n",
	}})
	return flat(
		&protocol.AgentStart{},
		userAt(foldPrompt, time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		assistantStart("claude-sonnet-5-5", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(500, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "Count splits on a comma.\nAn empty field must not count."}),
		updateAt(500, protocol.ThinkingEndEvent{ContentIndex: 0, Content: "Count splits on a comma.\nAn empty field must not count."}),
		toolStart("c1", "bash", `{"command":"seq 1 40"}`, 600),
		resultEnd("c1", "bash", strings.Join(seq, "\n"), protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, 700),
		toolStart("c2", "read", `{"path":"parser.go"}`, 800),
		resultEnd("c2", "read", "package p\nfunc Count() {}", protocol.ToolResultDetails{}, false, 900),
		toolStart("c3", "edit", `{"path":"parser.go"}`, 1000),
		&protocol.ToolExecutionEnd{
			Envelope: at(1100), ToolCallID: "c3", ToolName: "edit",
			Result: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "Edited parser.go."}}, Details: editDetails},
		},
		toolStart("c4", "bash", `{"command":"go test ./..."}`, 1200),
		resultEnd("c4", "bash", "exit: 1\n"+strings.Join(foldTestOutput, "\n"), producerDetails0(), true, 1300),
		textParts(1, foldReply),
		assistantEndUsage("claude-sonnet-5-5", protocol.StopStop, 6200, protocol.Usage{Input: 8100, Output: 320, Cost: protocol.Cost{Total: 6000}},
			protocol.Thinking{Thinking: "x"}, protocol.Text{Text: foldReply}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)
}

// producerDetails0 is the detail of a failed shell call.
func producerDetails0() protocol.ToolResultDetails {
	one := 1
	return protocol.ToolResultDetails{Kind: protocol.ToolKindExecute, ExitCode: &one}
}

func foldSource() fakeSource {
	return fakeSource{
		placeholder: true,
		headerL:     "◢◤ ask  ● leader 7c1e  ⌂ ~/src/p",
		headerR:     "claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 ",
	}
}

// foldModel returns the model after the session events, at a size.
func foldModel(t *testing.T, width, height int, parts ...any) *Model {
	t.Helper()
	return foldModelAt(t, width, height, 0, parts...)
}

// foldModelAt is foldModel with the clock of the blocks set to a time in Unix
// milliseconds, so that running steps show their seconds.
func foldModelAt(t *testing.T, width, height int, nowMs int64, parts ...any) *Model {
	t.Helper()
	m := New(Options{Slots: foldSource(), Profile: colorprofile.NoTTY})
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	trl := NewTranslator(TranslatorOptions{Location: time.UTC})
	events := foldSessionEvents()
	if len(parts) > 0 {
		events = flat(parts...)
	}
	for _, ev := range events {
		for _, msg := range trl.Event(ev) {
			m.Update(msg)
		}
	}
	require.NoError(t, m.Err())
	if nowMs > 0 {
		trl.View().SetClock(nowMs)
	}
	return m
}

// screenOf is the plain rows of the screen, without trailing spaces.
func screenOf(m *Model) []string {
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return rows
}

// blockID returns the id of the first block that the test accepts.
func blockID(t *testing.T, m *Model, accept func(Block) bool) string {
	t.Helper()
	for _, e := range m.transcript.entries {
		if accept(e.block) {
			return e.id
		}
	}
	t.Fatalf("no block matches")
	return ""
}

// toolID returns the id of the n-th tool step with a name.
func toolID(t *testing.T, m *Model, name string, nth int) string {
	t.Helper()
	seen := 0
	for _, e := range m.transcript.entries {
		if tb, ok := e.block.(toolLineBlock); ok && tb.call.name == name {
			if seen == nth {
				return e.id
			}
			seen++
		}
	}
	t.Fatalf("no %s step %d", name, nth)
	return ""
}

func isThinking(b Block) bool { _, ok := b.(thinkingBlock); return ok }
func isPrompt(b Block) bool   { _, ok := b.(userPromptBlock); return ok }
func isReply(b Block) bool    { _, ok := b.(assistantTextBlock); return ok }

func requireScreenGolden(t *testing.T, m *Model) {
	t.Helper()
	golden.RequireEqual(t, strings.Join(screenOf(m), "\n")+"\n")
}

// --- default folds and the compact session ---

func TestCompactSessionShowsOneRowForEachStepAt80x24(t *testing.T) {
	m := foldModel(t, 80, 24)
	rows := screenOf(m)
	text := strings.Join(rows, "\n")
	require.Equal(t, 24, len(rows))
	for _, want := range []string{"◆ Thought for 0.5s", "◆ Ran seq 1 40", "◆ Read parser.go", "◆ Edited parser.go  +3 -1"} {
		require.Equal(t, 1, strings.Count(text, want), want)
	}
	require.NotContains(t, text, "\n    40\n", "no output line of seq")
	require.Contains(t, text, "◆ Ran go test ./...")
	require.Regexp(t, `◆ Ran go test \./\.\.\. +✗ exit 1$`, rows[strings.Count(text[:strings.Index(text, "◆ Ran go test")], "\n")], "the failure shows at the right edge")
	requireScreenGolden(t, m)
}

func TestCompactSessionAt40x14(t *testing.T) {
	m := foldModel(t, 40, 14)
	for _, r := range screenOf(m) {
		require.LessOrEqual(t, displayWidth(r), 40, r)
	}
	requireScreenGolden(t, m)
}

func TestFailedToolRowIsExactlyTheWidth(t *testing.T) {
	m := foldModel(t, 80, 40)
	var row string
	for _, r := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(r, "◆ Ran go test") {
			row = r
		}
	}
	require.Equal(t, 80, displayWidth(row), "the right column ends one column before the edge")
	require.True(t, strings.HasSuffix(row, "✗ exit 1 "), row)
}

func TestRunningToolRowHasVerbAndSpinnerAt80x24(t *testing.T) {
	m := foldModelAt(t, 80, 24, baseMs+4_000,
		&protocol.AgentStart{},
		userAt("run all tests and tell me what fails", time.Date(2026, 10, 12, 15, 31, 0, 0, time.UTC)),
		toolStart("c1", "bash", `{"command":"go test ./... -count=1"}`, 0),
	)
	text := strings.Join(screenOf(m), "\n")
	require.Contains(t, text, "◆ Running go test ./... -count=1")
	require.Regexp(t, `(?m)\.\.\. -count=1 +⠋ 4s$`, text)
	requireScreenGolden(t, m)
}

func TestStreamingThinkingRowsAt80x24(t *testing.T) {
	m := foldModelAt(t, 80, 24, baseMs+2_000,
		&protocol.AgentStart{},
		userAt("run all tests and tell me what fails", time.Date(2026, 10, 12, 15, 31, 0, 0, time.UTC)),
		assistantStart("m", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "Split on \",\" gives an empty string for \"a,,b\".\nSo the loop must skip f == \"\"."}),
		updateAt(20, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "\nThen the count is right."}),
	)
	text := strings.Join(screenOf(m), "\n")
	require.Regexp(t, `(?m)^  ◆ Thinking… 2s +⠋$`, text)
	require.Contains(t, text, "  ┃ …\n  ┃ So the loop must skip f == \"\".\n  ┃ Then the count is right.")
	requireScreenGolden(t, m)
}

// --- truncated and expanded forms on the screen ---

func TestTruncatedFailedToolOnTheScreen(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	id := toolID(t, m, "bash", 1)
	require.NoError(t, m.pane.CycleFold(id))
	rows := screenOf(m)
	text := strings.Join(rows, "\n")
	at := strings.Index(text, "◆ Ran go test ./...")
	body := strings.Split(text[at:], "\n")
	require.Equal(t, "    --- FAIL: TestCount (0.00s)", body[1], "the first body row is the first output line")
	require.Equal(t, "        parser_test.go:12: want 2, got 3", body[2])
	require.Equal(t, "    === RUN   TestCountEmpty", body[3])
	require.Equal(t, "    ⋮ 3 more lines · e to expand · Enter full screen", body[4])
	require.Equal(t, "    FAIL", body[5])
	require.Equal(t, "    FAIL    example.com/p/parser    0.012s", body[6])
	require.Equal(t, "    FAIL", body[7])
	require.NotContains(t, text, "$ go test ./...", "no command row in the truncated form")
	requireScreenGolden(t, m)
}

func TestToolWithFiveLinesShowsAllAndNoHintAfterOneCycle(t *testing.T) {
	m := foldModel(t, 80, 24,
		&protocol.AgentStart{},
		userAt("go", time.Date(2026, 10, 12, 15, 31, 0, 0, time.UTC)),
		toolStart("c1", "bash", `{"command":"printf five"}`, 0),
		resultEnd("c1", "bash", fixtureLines("l", 5), protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, 10),
	)
	m.View()
	require.NoError(t, m.pane.CycleFold(toolID(t, m, "bash", 0)))
	text := strings.Join(screenOf(m), "\n")
	for i := 1; i <= 5; i++ {
		require.Contains(t, text, fmt.Sprintf("\n    l %d\n", i))
	}
	require.NotContains(t, text, "⋮")
}

func TestExpandedFailedToolShowsCommandRowThenAllOutput(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	id := toolID(t, m, "bash", 1)
	require.NoError(t, m.pane.CycleFold(id))
	require.NoError(t, m.pane.CycleFold(id))
	text := strings.Join(screenOf(m), "\n")
	at := strings.Index(text, "◆ Ran go test ./...")
	body := strings.Split(text[at:], "\n")
	require.Equal(t, "    $ go test ./...", body[1])
	for i, line := range foldTestOutput {
		require.Equal(t, "    "+line, body[2+i])
	}
	require.NoError(t, m.pane.CycleFold(id))
	require.Equal(t, FoldCollapsed, m.pane.Fold(id), "a third cycle returns to one row")
	require.NotContains(t, strings.Join(screenOf(m), "\n"), "$ go test ./...")
}

func TestExpandedEditShowsTheDiffOnTheScreen(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	id := toolID(t, m, "edit", 0)
	require.NoError(t, m.pane.ExpandBlock(id))
	rows := screenOf(m)
	text := strings.Join(rows, "\n")
	require.Contains(t, text, "◆ Edited parser.go  +3 -1")
	require.Regexp(t, `(?m)^ +4 - +n\+\+$`, text)
	require.Regexp(t, `(?m)^ +4 \+ +if f != ""`, text)
	require.NotContains(t, text, "$ ", "an edit has no command row")
	requireScreenGolden(t, m)
	require.NoError(t, m.pane.CollapseBlock(id))
	require.NotRegexp(t, `(?m)^ +4 - `, strings.Join(screenOf(m), "\n"))
}

func TestLongPromptFoldsToThreeRowsAndACount(t *testing.T) {
	long := "the parser counts empty fields. Read parser.go and parser_test.go, find why Count(\"a,,b\") returns 3, fix it so empty fields are skipped, keep the public API the same, run the tests, and then tell me what changed in the end."
	m := foldModel(t, 80, 24,
		&protocol.AgentStart{},
		userAt(long, time.Date(2026, 10, 12, 15, 44, 0, 0, time.UTC)),
	)
	rows := screenOf(m)
	require.True(t, strings.HasPrefix(rows[2], "  ❯ the parser counts empty fields."), rows[2])
	require.True(t, strings.HasSuffix(rows[2], "15:44"), rows[2])
	require.Equal(t, "    … +1 line · e to expand", rows[5])
	id := blockID(t, m, isPrompt)
	require.True(t, m.pane.Foldable(id))
	require.NoError(t, m.pane.ExpandBlock(id))
	rows = screenOf(m)
	require.NotContains(t, strings.Join(rows, "\n"), "· e to expand")
	require.Contains(t, strings.Join(rows, "\n"), "the end.")
}

func TestPromptOfSixRowsShowsThreeAndPlusThreeLines(t *testing.T) {
	var words []string
	for i := 0; i < 6; i++ {
		words = append(words, strings.TrimSpace(strings.Repeat(fmt.Sprintf("w%d ", i), 20)))
	}
	m := foldModel(t, 80, 24,
		&protocol.AgentStart{},
		userAt(strings.Join(words, "\n"), time.Date(2026, 10, 12, 15, 44, 0, 0, time.UTC)),
	)
	rows := screenOf(m)
	require.Equal(t, "    … +3 lines · e to expand", rows[5], strings.Join(rows[:8], "\n"))
	requireScreenGolden(t, m)
}

func TestLongPromptAt40x14(t *testing.T) {
	long := "the parser counts empty fields. Read parser.go and parser_test.go, find why Count returns 3, fix it so empty fields are skipped, keep the public API the same, run the tests, and then tell me what changed."
	m := foldModel(t, 40, 14,
		&protocol.AgentStart{},
		userAt(long, time.Date(2026, 10, 12, 15, 44, 0, 0, time.UTC)),
		assistantStart("m", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "x"}),
		updateAt(1200, protocol.ThinkingEndEvent{ContentIndex: 0, Content: "x"}),
		assistantEndUsage("m", protocol.StopStop, 1300, protocol.Usage{}, protocol.Thinking{Thinking: "x"}),
		&protocol.AgentSettled{},
	)
	for _, r := range screenOf(m) {
		require.LessOrEqual(t, displayWidth(r), 40, r)
	}
	text := strings.Join(screenOf(m), "\n")
	require.Contains(t, text, "… +")
	require.Contains(t, text, "◆ Thought for 1.2s")
	requireScreenGolden(t, m)
}

// --- the fold commands of the pane ---

func TestPaneFoldCyclesCollapsedTruncatedExpandedCollapsed(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	id := toolID(t, m, "bash", 1)
	require.True(t, m.pane.Foldable(id))
	require.Equal(t, FoldCollapsed, m.pane.Fold(id), "a tool is collapsed by default")
	for _, want := range []FoldMode{FoldTruncated, FoldExpanded, FoldCollapsed, FoldTruncated} {
		require.NoError(t, m.pane.CycleFold(id))
		require.Equal(t, want, m.pane.Fold(id))
	}
}

func TestPaneExpandSetsExpandedAndCollapseGoesToTheLowestMode(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	id := toolID(t, m, "bash", 1)
	require.NoError(t, m.pane.ExpandBlock(id))
	require.Equal(t, FoldExpanded, m.pane.Fold(id))
	require.NoError(t, m.pane.ExpandBlock(id))
	require.Equal(t, FoldExpanded, m.pane.Fold(id), "no change when it is expanded already")
	require.Equal(t, FoldCollapsed, m.pane.CollapseFold(id))
	require.NoError(t, m.pane.CollapseBlock(id))
	require.Equal(t, FoldCollapsed, m.pane.Fold(id))
}

func TestPaneFoldCommandsLeaveANonFoldableBlockAlone(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	reply := blockID(t, m, isReply)
	require.False(t, m.pane.Foldable(reply))
	before := m.pane.Fold(reply)
	require.NoError(t, m.pane.CycleFold(reply))
	require.NoError(t, m.pane.ExpandBlock(reply))
	require.NoError(t, m.pane.CollapseBlock(reply))
	require.Equal(t, before, m.pane.Fold(reply))
	require.Equal(t, FoldExpanded, before, "a reply is always expanded")

	short := blockID(t, m, isPrompt)
	require.False(t, m.pane.Foldable(short), "a prompt of one row has no other form")
	require.ErrorIs(t, m.pane.CycleFold("nope"), ErrUnknownBlock)
}

func TestPaneRunningToolCollapsesToOneRow(t *testing.T) {
	m := foldModel(t, 80, 24,
		&protocol.AgentStart{},
		userAt("go", time.Date(2026, 10, 12, 15, 31, 0, 0, time.UTC)),
		toolStart("c1", "bash", `{"command":"sleep 9"}`, 0),
	)
	m.View()
	id := toolID(t, m, "bash", 0)
	require.Equal(t, FoldCollapsed, m.pane.CollapseFold(id), "a running tool is one row")
	require.NoError(t, m.pane.ExpandBlock(id))
	require.NoError(t, m.pane.CollapseBlock(id))
	require.Equal(t, FoldCollapsed, m.pane.Fold(id))
}

func TestPaneStreamingThinkingCollapseModeIsTruncated(t *testing.T) {
	m := foldModel(t, 80, 24,
		&protocol.AgentStart{},
		assistantStart("m", 0),
		updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "a\nb\nc"}),
	)
	m.View()
	id := blockID(t, m, isThinking)
	require.Equal(t, FoldTruncated, m.pane.Fold(id))
	require.Equal(t, FoldTruncated, m.pane.CollapseFold(id))
	require.NoError(t, m.pane.ExpandBlock(id))
	require.NoError(t, m.pane.CollapseBlock(id))
	require.Equal(t, FoldTruncated, m.pane.Fold(id), "a streaming thinking never goes below truncated")
	require.NoError(t, m.pane.CycleFold(id))
	require.Equal(t, FoldExpanded, m.pane.Fold(id))
	require.NoError(t, m.pane.CycleFold(id))
	require.Equal(t, FoldTruncated, m.pane.Fold(id))
}

func TestThinkingFoldsWhenItEndsUnlessTheUserSetAMode(t *testing.T) {
	stream := func(m *Model, trl *Translator, evs ...protocol.Event) {
		for _, ev := range evs {
			for _, msg := range trl.Event(ev) {
				m.Update(msg)
			}
		}
	}
	for _, tc := range []struct {
		name   string
		user   bool
		before FoldMode
		after  FoldMode
	}{
		{"automatic", false, FoldTruncated, FoldCollapsed},
		{"set by the user", true, FoldExpanded, FoldExpanded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Options{Slots: foldSource(), Profile: colorprofile.NoTTY})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			trl := NewTranslator(TranslatorOptions{Location: time.UTC})
			stream(m, trl, assistantStart("m", 0),
				updateAt(0, protocol.ThinkingStartEvent{ContentIndex: 0}),
				updateAt(10, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "a\nb\nc"}))
			m.View()
			id := blockID(t, m, isThinking)
			if tc.user {
				require.NoError(t, m.pane.ExpandBlock(id))
			}
			require.Equal(t, tc.before, m.pane.Fold(id))
			stream(m, trl, updateAt(900, protocol.ThinkingEndEvent{ContentIndex: 0, Content: "a\nb\nc"}),
				assistantEndUsage("m", protocol.StopStop, 1000, protocol.Usage{}, protocol.Thinking{Thinking: "a\nb\nc"}))
			m.View()
			require.Equal(t, tc.after, m.pane.Fold(id))
		})
	}
}

func TestPaneToggleAllStepsExpandsThenCollapses(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	steps := []string{blockID(t, m, isThinking), toolID(t, m, "bash", 0), toolID(t, m, "read", 0), toolID(t, m, "edit", 0), toolID(t, m, "bash", 1)}

	require.True(t, m.pane.ToggleAllSteps(), "some step is collapsed, so all expand")
	for _, id := range steps {
		require.Equal(t, FoldExpanded, m.pane.Fold(id), id)
	}
	require.Equal(t, FoldTruncated, m.pane.Fold(blockID(t, m, isPrompt)), "a prompt is not a step")

	require.False(t, m.pane.ToggleAllSteps(), "none is collapsed, so all collapse")
	for _, id := range steps {
		require.Equal(t, FoldCollapsed, m.pane.Fold(id), id)
	}
}

func TestPaneToggleAllThinkingMovesOnlyThinkingBlocks(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	think, tool := blockID(t, m, isThinking), toolID(t, m, "bash", 1)
	require.True(t, m.pane.ToggleAllThinking())
	require.Equal(t, FoldExpanded, m.pane.Fold(think))
	require.Equal(t, FoldCollapsed, m.pane.Fold(tool))
	require.False(t, m.pane.ToggleAllThinking())
	require.Equal(t, FoldCollapsed, m.pane.Fold(think))

	empty := foldModel(t, 80, 24, &protocol.AgentStart{})
	require.False(t, empty.pane.ToggleAllSteps(), "no step: nothing is expanded")
}

func TestPaneRedrawsOnlyTheBlockThatFolded(t *testing.T) {
	m := foldModel(t, 80, 24)
	m.View()
	before := m.pane.Draws()
	m.View()
	require.Equal(t, before, m.pane.Draws(), "an unchanged screen draws no block")
	require.NoError(t, m.pane.CycleFold(toolID(t, m, "bash", 1)))
	m.View()
	require.Equal(t, before+1, m.pane.Draws())
}

// --- row text ---

func TestToolVerbsAndWhat(t *testing.T) {
	for _, tc := range []struct {
		name, args, wantDone, wantRun string
	}{
		{"bash", `{"command":"go test ./..."}`, "Ran go test ./...", "Running go test ./..."},
		{"bash", `{"command":"echo one\necho two"}`, "Ran echo one …", "Running echo one …"},
		{"bash", `{"command":"\n  ls -la\n"}`, "Ran ls -la", "Running ls -la"},
		{"read", `{"path":"internal/tui/pane.go"}`, "Read internal/tui/pane.go", "Reading internal/tui/pane.go"},
		{"edit", `{"path":"a.go","old_text":"x","new_text":"y"}`, "Edited a.go", "Editing a.go"},
		{"write", `{"path":"b.go","content":"x"}`, "Wrote b.go", "Writing b.go"},
		{"grep", `{"pattern":"TODO","path":"internal"}`, `Searched "TODO" internal`, `Searching "TODO" internal`},
		{"grep", `{"pattern":"TODO"}`, `Searched "TODO"`, `Searching "TODO"`},
		{"find", `{"pattern":"*.go"}`, "Found *.go", "Finding *.go"},
		{"ls", `{"path":"internal"}`, "Listed internal", "Listing internal"},
		{"job_output", `{"job_id":"bash-1"}`, "Checked job bash-1", "Checking job bash-1"},
		{"job_kill", `{"job_id":"bash-1"}`, "Stopped job bash-1", "Stopping job bash-1"},
		{"job_list", `{}`, "Listed jobs", "Listing jobs"},
		{"monitor", `{"command":"tail -f log"}`, "Watched tail -f log", "Watching tail -f log"},
		{"fetch_page", `{"url":"https://x.dev"}`, "fetch_page https://x.dev", "fetch_page https://x.dev"},
	} {
		for _, running := range []bool{false, true} {
			state, want := ToolSucceeded, tc.wantDone
			if running {
				state, want = ToolRunning, tc.wantRun
			}
			call := toolCall{name: tc.name, args: shortArgs(json.RawMessage(tc.args)), raw: json.RawMessage(tc.args)}
			rows := plain(renderRows(newToolCallBlock(call, state, "", "", toolOutput{}, NewTranscriptView()),
				RenderContext{Width: 100, Theme: noColor()}))
			require.True(t, strings.HasPrefix(rows[0], "  ◆ "+want), "%s: %q", tc.name, rows[0])
		}
	}
}

func TestToolRowGlyphRoles(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	row := func(state ToolState) string {
		return renderRows(NewToolLineBlock("bash", "x", state, "", "boom", NewTranscriptView()), RenderContext{Width: 60, Theme: th})[0]
	}
	require.Contains(t, row(ToolSucceeded), "\x1b[38;2;45;212;191m◆", "finished: tool role")
	require.Contains(t, row(ToolRunning), "38;2;250;204;21m◆", "running: warning role")
	require.Contains(t, row(ToolFailed), "38;2;248;113;113m◆", "failed: error role")
	require.Contains(t, row(ToolSucceeded), "\x1b[1;38;2;217;219;232mRan", "the verb is bold")
}

func TestCancelledToolStepKeepsItsGlyphAndReason(t *testing.T) {
	rows := plain(renderRows(NewToolLineBlock("bash", "sleep 9", ToolFailed, "", "interrupted", nil), RenderContext{Width: 60, Theme: noColor()}))
	require.True(t, strings.HasSuffix(rows[0], "✗ interrupted"), rows[0])
}

func TestStepRowNarrowWidthsDropPartsInOrder(t *testing.T) {
	d := protocol.ToolDiff{OldText: "a\n", NewText: "b\n"}
	b := newToolBlock("edit", "internal/tui/a/very/long/path/status.go", ToolFailed, "", "no match",
		newToolOutput("", protocol.ToolResultDetails{Diff: &d}), nil)
	row := func(w int) string {
		return plain(renderRows(b, RenderContext{Width: w, Theme: noColor()}))[0]
	}
	// Wide: path, line counts and failure.
	require.Equal(t, "  ◆ Edited internal/tui/a/very/long/path/status.go  +1 -1"+strings.Repeat(" ", 80-displayWidth("  ◆ Edited internal/tui/a/very/long/path/status.go  +1 -1")-displayWidth("✗ no match")-1)+"✗ no match", row(80))
	// Narrower: the path is cut with an ellipsis; the verb, the counts and the right column stay.
	r40 := row(40)
	require.Contains(t, r40, "…  +1 -1")
	require.True(t, strings.HasSuffix(r40, "✗ no match"), r40)
	require.True(t, strings.HasPrefix(r40, "  ◆ Edited "), r40)
	// Under 30 columns the right column goes first.
	r29 := row(29)
	require.NotContains(t, r29, "✗")
	require.Contains(t, r29, "+1 -1")
	// Then the line counts.
	r12 := row(12)
	require.NotContains(t, r12, "+1")
	require.True(t, strings.HasPrefix(r12, "  ◆ Edited"), r12)
	for _, w := range []int{80, 40, 29, 12, 5, 1} {
		require.LessOrEqual(t, displayWidth(row(w)), w)
	}
}

func TestStepRowNeverSplitsAWideCharacter(t *testing.T) {
	b := NewToolLineBlock("read", "日本語のファイル名日本語のファイル名.go", ToolSucceeded, "", "", nil)
	for w := 8; w < 40; w++ {
		r := plain(renderRows(b, RenderContext{Width: w, Theme: noColor()}))[0]
		require.LessOrEqual(t, displayWidth(r), w)
		require.NotContains(t, r, "�")
	}
}

func TestJobNoticeIsOneRowAndCannotFold(t *testing.T) {
	b := newJobNotice("job bash-1", "npm run build", ToolNoted, "completed · exit 0", "", NewTranscriptView())
	require.False(t, b.(foldBlock).foldable(80))
	for _, mode := range []FoldMode{FoldCollapsed, FoldTruncated, FoldExpanded} {
		rows := plain(renderRows(b, RenderContext{Width: 80, Theme: noColor(), Fold: mode}))
		require.Len(t, rows, 1)
		require.True(t, strings.HasSuffix(rows[0], "completed · exit 0"), rows[0])
	}
}

func TestExpandedToolWithACutPathShowsThePathInFull(t *testing.T) {
	path := "internal/tui/a/very/long/directory/name/for/a/file/status_test_helpers.go"
	b := NewToolLineBlock("read", path, ToolSucceeded, "", "", nil)
	rows := plain(renderRows(b, RenderContext{Width: 40, Theme: noColor(), Fold: FoldExpanded}))
	require.Contains(t, rows[0], "…", "the row cuts the path")
	var rest strings.Builder
	for _, r := range rows[1:] {
		rest.WriteString(strings.TrimSpace(r))
	}
	require.Equal(t, strings.ReplaceAll(path, " ", ""), rest.String(), "the expanded form shows all of it")

	// A path that the row cuts although it is shorter than the row text width.
	mid := "internal/tui/a/very/long/path/x.go"
	rows = plain(renderRows(NewToolLineBlock("read", mid, ToolSucceeded, "", "", nil), RenderContext{Width: 40, Theme: noColor(), Fold: FoldExpanded}))
	require.Contains(t, rows[0], "…")
	require.Equal(t, []string{"    " + mid}, rows[1:], "the full path shows under the row")

	short := plain(renderRows(NewToolLineBlock("read", "a.go", ToolSucceeded, "", "", nil), RenderContext{Width: 40, Theme: noColor(), Fold: FoldExpanded}))
	require.Equal(t, []string{"  ◆ Read a.go"}, short, "no extra row when nothing was cut")
}
