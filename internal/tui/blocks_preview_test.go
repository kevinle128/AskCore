package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// --- helpers ---

// numbered returns n lines "<prefix> 1" to "<prefix> n" joined by line ends,
// without a final line end.
func fixtureLines(prefix string, n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}
	return strings.Join(lines, "\n")
}

func resultEnd(id, name, text string, d protocol.ToolResultDetails, isErr bool, ms int64) protocol.Event {
	raw, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	return &protocol.ToolExecutionEnd{
		Envelope: at(ms), ToolCallID: id, ToolName: name, IsError: isErr,
		Result: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Details: raw},
	}
}

func resultUpdate(id, name, text string, ms int64) protocol.Event {
	return &protocol.ToolExecutionUpdate{
		Envelope: at(ms), ToolCallID: id, ToolName: name,
		PartialResult: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: text}}},
	}
}

func numberedRows(prefix string, from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("    %s %d", prefix, i))
	}
	return out
}

// --- truncated form ---

// feedTool plays a finished tool call with a result text and details.
func feedTool(x *tx, name, args, text string, d protocol.ToolResultDetails, failed bool) {
	x.t.Helper()
	x.feed(toolStart("c1", name, args, 0), resultEnd("c1", name, text, d, failed, 100))
}

func TestToolTruncatedShowsFirstThreeHintAndLastThree(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	feedTool(x, "bash", `{"command":"go test"}`, fixtureLines("out", 12), protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false)
	rows := x.rows()
	require.Equal(t, "  ◆ Ran go test", rows[0], "no command row in the truncated form")
	require.Equal(t, numberedRows("out", 1, 3), rows[1:4])
	require.Equal(t, "    ⋮ 6 more lines · e to expand · Enter full screen", rows[4])
	require.Equal(t, numberedRows("out", 10, 12), rows[5:8])
	require.Len(t, rows, 8, "no empty row below a step")
	require.Equal(t, 1, x.tr.Frontier(), "a finished block is final")
}

func TestToolTruncatedAtTheCriteriaFailure(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	out := "--- FAIL: TestCount (0.00s)\n    parser_test.go:12: want 2, got 3\n=== RUN   TestCountEmpty\n--- PASS: TestCountEmpty (0.00s)\n=== RUN   TestCountOne\n--- PASS: TestCountOne (0.00s)\nFAIL\nFAIL\tp\t0.012s\nFAIL"
	feedTool(x, "bash", `{"command":"go test ./..."}`, "exit: 1\n"+out, producerDetails(t, realBashFailDetails), true)
	rows := x.rows()
	require.True(t, strings.HasPrefix(rows[0], "  ◆ Ran go test ./..."), rows[0])
	require.True(t, strings.HasSuffix(rows[0], "✗ exit 1"), rows[0])
	require.Equal(t, []string{
		"    --- FAIL: TestCount (0.00s)",
		"        parser_test.go:12: want 2, got 3",
		"    === RUN   TestCountEmpty",
		"    ⋮ 3 more lines · e to expand · Enter full screen",
		"    FAIL",
		"    FAIL    p   0.012s",
		"    FAIL",
	}, rows[1:])
}

func TestToolTruncatedWithSixLinesOrFewerShowsAllAndNoHint(t *testing.T) {
	for _, n := range []int{1, 5, 6} {
		x := newTx(t)
		x.rc.Fold = FoldTruncated
		feedTool(x, "read", `{"path":"a.go"}`, fixtureLines("l", n), protocol.ToolResultDetails{}, false)
		rows := x.rows()
		require.Equal(t, numberedRows("l", 1, n), rows[1:], "%d lines", n)
	}
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	feedTool(x, "read", `{"path":"a.go"}`, fixtureLines("l", 7), protocol.ToolResultDetails{}, false)
	rows := x.rows()
	require.Equal(t, "    ⋮ 1 more line · e to expand · Enter full screen", rows[4], "seven lines hide one")
}

func TestToolTruncatedHintIsShortUnderSixtyColumns(t *testing.T) {
	x := newTx(t)
	x.rc = RenderContext{Width: 59, Theme: NewTheme(0), Fold: FoldTruncated}
	feedTool(x, "ls", `{"path":"."}`, fixtureLines("f", 20), protocol.ToolResultDetails{}, false)
	require.Equal(t, "    ⋮ 14 more lines · e", x.rows()[4])
	x.rc.Width = 60
	require.Equal(t, "    ⋮ 14 more lines · e to expand · Enter full screen", x.rows()[4])
}

func TestToolTruncatedHintDoesNotCountTheFinalLineEnd(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	feedTool(x, "bash", `{}`, fixtureLines("out", 7)+"\n", protocol.ToolResultDetails{}, false)
	require.Equal(t, "    ⋮ 1 more line · e to expand · Enter full screen", x.rows()[4])
}

func TestToolTruncatedKeepsInnerEmptyLines(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	feedTool(x, "read", `{}`, "a\n\nb", protocol.ToolResultDetails{}, false)
	require.Equal(t, []string{"    a", "", "    b"}, x.rows()[1:])
}

func TestToolWithoutOutputHasNoBodyRows(t *testing.T) {
	for _, text := range []string{"", "\n", "  \n \n"} {
		for _, mode := range []FoldMode{FoldTruncated, FoldExpanded} {
			x := newTx(t)
			x.rc.Fold = mode
			feedTool(x, "read", `{"path":"a"}`, text, protocol.ToolResultDetails{}, false)
			require.Len(t, x.rows(), 1, "%q %v", text, mode)
		}
	}
}

func TestToolFailureKeepsReasonOnTheRow(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	feedTool(x, "bash", `{"command":"go test"}`, "exit: 1\n"+fixtureLines("out", 8), producerDetails(t, realBashFailDetails), true)
	rows := x.rows()
	require.True(t, strings.HasSuffix(rows[0], "✗ exit 1"), rows[0])
	require.Equal(t, numberedRows("out", 1, 3), rows[1:4])
	require.Equal(t, "    ⋮ 2 more lines · e to expand · Enter full screen", rows[4])
	require.Equal(t, numberedRows("out", 6, 8), rows[5:8])
}

// --- expanded form ---

func TestToolExpandedShowsCommandRowThenAllOutput(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	feedTool(x, "bash", `{"command":"go test ./..."}`, "exit: 1\n"+fixtureLines("out", 9), producerDetails(t, realBashFailDetails), true)
	rows := x.rows()
	require.Equal(t, "    $ go test ./...", rows[1], "the first body row is the command")
	require.Equal(t, numberedRows("out", 1, 9), rows[2:])
}

func TestToolExpandedWrapsLongLinesAndCutsNothing(t *testing.T) {
	x := newTx(t)
	x.rc = RenderContext{Width: 30, Theme: NewTheme(0), Fold: FoldExpanded}
	long := strings.Repeat("0123456789", 6)
	feedTool(x, "read", `{}`, long, protocol.ToolResultDetails{}, false)
	rows := x.rows()
	require.Equal(t, []string{"    " + long[:26], "    " + long[26:52], "    " + long[52:]}, rows[1:])
}

func TestToolExpandedCommandWrapsAndKeepsEveryLine(t *testing.T) {
	x := newTx(t)
	x.rc = RenderContext{Width: 30, Theme: NewTheme(0), Fold: FoldExpanded}
	feedTool(x, "bash", `{"command":"echo one\necho two three four five six seven"}`, "", protocol.ToolResultDetails{}, false)
	rows := x.rows()
	require.Equal(t, "  ◆ Ran echo one …", rows[0], "the row shows the first line and the ellipsis")
	require.Equal(t, []string{
		"    $ echo one",
		"      echo two three four five",
		"      six seven",
	}, rows[1:])
}

func TestToolExpandedEditShowsTheUnifiedDiff(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	d := protocol.ToolResultDetails{Kind: protocol.ToolKindEdit, Diff: &protocol.ToolDiff{
		Path: "status.go", OldText: "a\nb\nc\nd\n", NewText: "a\nB\nc\nd\n",
	}}
	feedTool(x, "edit", `{"path":"status.go"}`, "Edited status.go.", d, false)
	require.Equal(t, []string{
		"  ◆ Edited status.go  +1 -1",
		"       1   a",
		"       2 - b",
		"       2 + B",
		"       3   c",
		"       4   d",
	}, x.rows())
}

func TestToolEditRowShowsPathAndDiffStat(t *testing.T) {
	x := newTx(t)
	d := protocol.ToolResultDetails{Kind: protocol.ToolKindEdit, Diff: &protocol.ToolDiff{
		Path: "status.go", OldText: "a\nb\nc\nd\n", NewText: "a\nB\nc\nd\n",
	}}
	feedTool(x, "edit", `{"path":"internal/tui/status.go"}`, "Edited internal/tui/status.go.\nmore text", d, false)
	require.Equal(t, []string{"  ◆ Edited internal/tui/status.go  +1 -1"}, x.rows())
}

func TestToolWriteRowShowsAddedLinesOnly(t *testing.T) {
	x := newTx(t)
	d := protocol.ToolResultDetails{Diff: &protocol.ToolDiff{Path: "n.go", NewText: "a\nb\nc\n"}}
	feedTool(x, "write", `{"path":"n.go"}`, "Wrote 6 bytes", d, false)
	require.Equal(t, []string{"  ◆ Wrote n.go  +3"}, x.rows())
}

func TestToolEditFailureHasNoDiffStat(t *testing.T) {
	x := newTx(t)
	feedTool(x, "edit", `{"path":"a.go"}`, "no match for old text", protocol.ToolResultDetails{}, true)
	rows := x.rows()
	require.Len(t, rows, 1)
	require.NotContains(t, rows[0], "+0")
	require.True(t, strings.HasSuffix(rows[0], "✗ no match for old text"), rows[0])
}

func TestToolDiffStatStaysWhenThePathIsCut(t *testing.T) {
	rc := RenderContext{Width: 40, Theme: NewTheme(0)}
	d := protocol.ToolDiff{OldText: "a\n", NewText: "b\n"}
	b := newToolBlock("edit", "internal/tui/a/very/long/path/status.go", ToolSucceeded, "0.1s", "",
		newToolOutput("", protocol.ToolResultDetails{Diff: &d}), nil)
	row := ansi.Strip(b.Lines(rc)[0])
	require.Contains(t, row, "…  +1 -1")
	require.True(t, strings.HasPrefix(row, "  ◆ Edited internal/tui"), row)
	require.LessOrEqual(t, displayWidth(row), 40)
}

func TestLineDiffStats(t *testing.T) {
	for _, tc := range []struct {
		name, old, new string
		add, del       int
	}{
		{"same", "a\nb\n", "a\nb\n", 0, 0},
		{"one line changed", "a\nb\nc\n", "a\nB\nc\n", 1, 1},
		{"insert", "a\nc\n", "a\nb\nc\n", 1, 0},
		{"delete", "a\nb\nc\n", "a\nc\n", 0, 1},
		{"new file", "", "a\nb\n", 2, 0},
		{"emptied file", "a\nb\n", "", 0, 2},
		{"moved block", "a\nb\nc\nd\n", "c\nd\na\nb\n", 2, 2},
		{"no final line end", "a\nb", "a\nb\nc", 1, 0},
		{"windows line ends", "a\r\nb\r\n", "a\nb\n", 0, 0},
		{"only a final line end added", "a", "a\n", 0, 0},
	} {
		add, del := lineDiffStats(tc.old, tc.new)
		require.Equal(t, [2]int{tc.add, tc.del}, [2]int{add, del}, tc.name)
	}
}

// --- width, tabs, controls ---

func TestToolTruncatedLongLineIsCutToOneRow(t *testing.T) {
	x := newTx(t)
	x.rc = RenderContext{Width: 40, Theme: NewTheme(0), Fold: FoldTruncated}
	long := strings.Repeat("0123456789", 6)
	feedTool(x, "read", `{}`, long+"\nshort", protocol.ToolResultDetails{}, false)
	rows := x.rows()
	require.Len(t, rows, 3)
	require.Equal(t, "    "+long[:35]+"…", rows[1])
	require.Equal(t, 40, displayWidth(rows[1]))
	require.Equal(t, "    short", rows[2])
}

func TestToolTruncatedCutNeverSplitsAWideCharacter(t *testing.T) {
	x := newTx(t)
	x.rc = RenderContext{Width: 40, Theme: NewTheme(0), Fold: FoldTruncated}
	text := strings.Repeat("a", 34) + "日本語の文字"
	feedTool(x, "read", `{}`, text, protocol.ToolResultDetails{}, false)
	row := x.rows()[1]
	require.Equal(t, "    "+strings.Repeat("a", 34)+"…", row)
	require.LessOrEqual(t, displayWidth(row), 40)
}

func TestToolExpandsTabsAndMarksControls(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	text := "a\tb\n\x1b[31mred\x1b[0m\nbell\x07x\n"
	feedTool(x, "read", `{}`, text, protocol.ToolResultDetails{}, false)
	rows := x.rows()
	require.Equal(t, "    a   b", rows[1])
	require.Equal(t, "    red", rows[2])
	require.Equal(t, "    bell?x", rows[3])
}

func TestToolTreatsWindowsLineEndsAsLineEnds(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	feedTool(x, "read", `{}`, "a\r\nb\r\n", protocol.ToolResultDetails{}, false)
	require.Equal(t, []string{"    a", "    b"}, x.rows()[1:])
}

// --- truncation rows: the expanded form and the viewer only ---

func TestToolTruncationLineWithSpillMovesPathToItsOwnRow(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	d := protocol.ToolResultDetails{
		Truncation: &protocol.ToolTruncation{TruncatedBy: "lines", OutputLines: 2000, OutputBytes: 50 * 1024, TotalBytes: 2_516_582},
		SpillPath:  "/tmp/ask-jobs-81f2/bash-4.log",
	}
	feedTool(x, "bash", `{"command":"go test -v"}`, fixtureLines("ok", 2000), d, false)
	rows := x.rows()
	n := len(rows)
	require.Equal(t, "    model saw 2000 lines (50 KB) of 2.4 MB", rows[n-2])
	require.Equal(t, "    full output: /tmp/ask-jobs-81f2/bash-4.log", rows[n-1])
}

func TestToolTruncationLineIsNotInTheTruncatedForm(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	d := protocol.ToolResultDetails{
		Truncation: &protocol.ToolTruncation{TruncatedBy: "bytes", OutputLines: 30, OutputBytes: 900, TotalBytes: 2048},
		SpillPath:  "/tmp/b.log",
	}
	feedTool(x, "bash", `{}`, "x", d, false)
	require.NotContains(t, strings.Join(x.rows(), "\n"), "model saw")
	require.NotContains(t, strings.Join(x.rows(), "\n"), "full output")
}

func TestToolTruncationLineKeepsPathOnTheRowWhenItFits(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	d := protocol.ToolResultDetails{
		Truncation: &protocol.ToolTruncation{TruncatedBy: "bytes", OutputLines: 30, OutputBytes: 900, TotalBytes: 2048},
		SpillPath:  "/tmp/b.log",
	}
	feedTool(x, "bash", `{}`, "x", d, false)
	rows := x.rows()
	require.Equal(t, "    model saw 30 lines (900 B) of 2.0 KB · full output: /tmp/b.log", rows[len(rows)-1])
}

func TestToolTruncationReadShowsNextOffset(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	d := protocol.ToolResultDetails{
		Truncation: &protocol.ToolTruncation{TruncatedBy: "lines", TotalLines: 5120, OutputLines: 2000},
		NextOffset: 2001,
	}
	feedTool(x, "read", `{"path":"events.jsonl"}`, fixtureLines("ev", 2000), d, false)
	rows := x.rows()
	require.Equal(t, "    model saw 2000 of 5120 lines · next offset 2001", rows[len(rows)-1])
}

func TestToolTruncationLineSaysWhenTheSpillFileStopped(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	d := protocol.ToolResultDetails{SpillPath: "/tmp/b.log", SpillCapped: true}
	feedTool(x, "bash", `{}`, "x", d, false)
	rows := x.rows()
	require.Equal(t, "    full output: /tmp/b.log (file stopped at 64 MB)", rows[len(rows)-1])
}

func TestToolTruncationPathIsWrappedAndNeverCut(t *testing.T) {
	x := newTx(t)
	x.rc = RenderContext{Width: 40, Theme: NewTheme(0), Fold: FoldExpanded}
	path := "/tmp/ask-jobs-81f2/a-very-long-directory-name/bash-4.log"
	d := protocol.ToolResultDetails{
		Truncation: &protocol.ToolTruncation{TruncatedBy: "lines", OutputLines: 2000, OutputBytes: 50 * 1024, TotalBytes: 2_516_582},
		SpillPath:  path,
	}
	feedTool(x, "read", `{}`, "x", d, false)
	rows := x.rows()
	require.Equal(t, "    model saw 2000 lines (50 KB) of 2.4…", rows[2])
	var joined strings.Builder
	for _, r := range rows[3:] {
		require.LessOrEqual(t, displayWidth(r), 40, r)
		joined.WriteString(strings.TrimSpace(r))
	}
	require.Contains(t, strings.ReplaceAll(joined.String(), " ", ""), path)
	require.NotContains(t, joined.String(), "…", "the path is never cut")
}

func TestToolTruncationRowsWithoutOutputSitUnderTheRow(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	d := protocol.ToolResultDetails{Truncation: &protocol.ToolTruncation{TruncatedBy: "lines", TotalLines: 9, OutputLines: 3}}
	feedTool(x, "ls", `{}`, "", d, false)
	require.Equal(t, []string{"    model saw 3 of 9 lines"}, x.rows()[1:])
}

func TestToolSizeText(t *testing.T) {
	require.Equal(t, "900 B", formatSize(900))
	require.Equal(t, "1.0 KB", formatSize(1024))
	require.Equal(t, "50 KB", formatSize(51200))
	require.Equal(t, "2.4 MB", formatSize(2_516_582))
	require.Equal(t, "10 MB", formatSize(10*1024*1024))
	require.Equal(t, "1.0 MB", formatSize(1024*1024-1))
	require.Equal(t, "10 KB", formatSize(10239))
}

// --- streaming bash ---

func TestToolStreamingBashShowsLiveOutputInTheOpenForms(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("c1", "bash", `{"command":"go test ./..."}`, 0))
	x.feed(resultUpdate("c1", "bash", fixtureLines("ok", 3), 50))
	require.Len(t, x.rows(), 1, "a running step is one row by default")

	x.rc.Fold = FoldTruncated
	rows := x.rows()
	require.Equal(t, numberedRows("ok", 1, 3), rows[1:], "a short output shows all lines and no hint")

	x.feed(resultUpdate("c1", "bash", fixtureLines("ok", 42), 100))
	rows = x.rows()
	require.True(t, strings.HasPrefix(rows[0], "  ◆ Running go test ./..."), rows[0])
	require.True(t, strings.HasSuffix(rows[0], "⠋"), rows[0])
	require.Equal(t, numberedRows("ok", 1, 3), rows[1:4])
	require.Equal(t, "    ⋮ 36 more lines · e to expand · Enter full screen", rows[4])
	require.Equal(t, numberedRows("ok", 40, 42), rows[5:8])
	require.Zero(t, x.tr.Frontier(), "a running block is not final")

	x.feed(resultEnd("c1", "bash", fixtureLines("ok", 43), protocol.ToolResultDetails{}, false, 200))
	rows = x.rows()
	require.Equal(t, numberedRows("ok", 41, 43), rows[5:8])
	require.Equal(t, 1, x.tr.Frontier())
}

func TestToolUpdateWithoutStartIsIgnored(t *testing.T) {
	x := newTx(t)
	require.Empty(t, x.trl.Event(resultUpdate("zz", "bash", "text", 0)))
}

func TestToolSettleKeepsTheLastPartialOutputOfAnInterruptedTool(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	x.feed(toolStart("c1", "bash", `{}`, 0), resultUpdate("c1", "bash", "partial", 10), &protocol.AgentSettled{})
	rows := x.rows()
	require.True(t, strings.HasSuffix(rows[0], "✗ interrupted"), rows[0])
	require.Equal(t, "    partial", rows[1])
}

// --- snapshot ---

func TestToolSnapshotResultShowsRowAndTruncation(t *testing.T) {
	x := newTx(t)
	raw, err := json.Marshal(protocol.ToolResultDetails{
		Truncation: &protocol.ToolTruncation{TruncatedBy: "lines", TotalLines: 5120, OutputLines: 2000},
		NextOffset: 2001,
	})
	require.NoError(t, err)
	am := protocol.AssistantMessage{Content: []protocol.AssistantBlock{
		protocol.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"events.jsonl"}`)},
	}, StopReason: protocol.StopToolUse}
	res := protocol.ToolResultMessage{
		ToolCallID: "c1", ToolName: "read", Details: raw,
		Content: []protocol.UserBlock{protocol.Text{Text: fixtureLines("ev", 2000)}},
	}
	x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{
		{Kind: "message", Message: am}, {Kind: "message", Message: res},
	}}))
	require.Equal(t, []string{"  ◆ Read events.jsonl"}, x.rows())
	x.rc.Fold = FoldExpanded
	rows := x.rows()
	require.Equal(t, "    model saw 2000 of 5120 lines · next offset 2001", rows[len(rows)-1])
}

func TestToolSnapshotEditShowsDiffStat(t *testing.T) {
	x := newTx(t)
	raw, err := json.Marshal(protocol.ToolResultDetails{Diff: &protocol.ToolDiff{OldText: "a\n", NewText: "b\nc\n"}})
	require.NoError(t, err)
	am := protocol.AssistantMessage{Content: []protocol.AssistantBlock{
		protocol.ToolCall{ID: "c1", Name: "edit", Arguments: json.RawMessage(`{"path":"a.go"}`)},
	}, StopReason: protocol.StopToolUse}
	res := protocol.ToolResultMessage{ToolCallID: "c1", ToolName: "edit", Details: raw, Content: []protocol.UserBlock{protocol.Text{Text: "Edited a.go"}}}
	x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{{Kind: "message", Message: am}, {Kind: "message", Message: res}}}))
	require.Equal(t, []string{"  ◆ Edited a.go  +2 -1"}, x.rows())
}

// --- screens ---

func TestToolStepRowsOnTheScreenAtEightyColumns(t *testing.T) {
	r := startSession(t, 80, 24, sketchSource(""))
	r.play(flat(
		&protocol.AgentStart{},
		userAt("run the tests", sketchTime(15, 31)),
		toolStart("c1", "bash", `{"command":"go test ./internal/tui"}`, 0),
		resultEnd("c1", "bash", "exit: 1\n"+fixtureLines("FAIL", 12), protocol.ToolResultDetails{}, true, 100),
		&protocol.AgentSettled{},
	)...)
	rows := r.h.text()
	at := -1
	for i, row := range rows {
		if strings.Contains(row, "◆ Ran go test ./internal/tui") {
			at = i
		}
	}
	require.GreaterOrEqual(t, at, 0, strings.Join(rows, "\n"))
	require.True(t, strings.HasSuffix(strings.TrimRight(rows[at], " "), "✗ exit 1"), rows[at])
	require.Zero(t, r.h.count("FAIL 12"), "a collapsed step shows no output")
}

func TestToolStreamingBashScreenShowsRunningRowAndSpinner(t *testing.T) {
	r := startSession(t, 80, 24, sketchSource("┊ ◌ Working… 12s · esc to interrupt"))
	r.play(flat(
		&protocol.AgentStart{},
		userAt("run all tests", sketchTime(15, 31)),
		toolStart("c1", "bash", `{"command":"go test ./... -count=1"}`, 0),
		resultUpdate("c1", "bash", fixtureLines("ok", 42), 100),
	)...)
	joined := strings.Join(r.h.text(), "\n")
	require.Contains(t, joined, "◆ Running go test ./... -count=1")
	require.Contains(t, joined, "Working…")
	require.Zero(t, r.h.count("ok 42"), "the running step is one row")
}
