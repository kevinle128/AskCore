package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// Texts and details in this file are what the bash and read tools return,
// copied from real runs. They hold the lines that only the model needs: the
// exit header, the spill file line, the line numbers and the continue notice.

// producerDetails decodes the details of a real result.
func producerDetails(t *testing.T, raw string) protocol.ToolResultDetails {
	t.Helper()
	var d protocol.ToolResultDetails
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	return d
}

const (
	realBashOKDetails   = `{"exitCode":0,"jobId":"bash-1","status":"completed","kind":"execute"}`
	realBashFailDetails = `{"exitCode":1,"jobId":"bash-2","status":"completed","kind":"execute"}`
	realBashSpillPath   = "/var/folders/84/5njq4pvs747b3zzntvtgtpl80000gn/T/ask-jobs-4246230116/bash-4.log"
	realBashSpillInfo   = `{"truncation":{"truncatedBy":"lines","totalLines":0,"totalBytes":348894,"outputLines":2000,"outputBytes":10292},"spillPath":"` +
		realBashSpillPath + `","exitCode":0,"jobId":"bash-4","status":"completed","kind":"execute"}`
	realReadFullDetails  = `{"path":"/tmp/small.txt","locations":[{"path":"/tmp/small.txt","line":1}],"kind":"read"}`
	realReadLimitDetails = `{"path":"/tmp/big.txt","locations":[{"path":"/tmp/big.txt","line":1}],"nextOffset":6,"kind":"read"}`
	realReadTruncDetails = `{"path":"/tmp/huge.txt","locations":[{"path":"/tmp/huge.txt","line":1}],` +
		`"truncation":{"truncatedBy":"lines","totalLines":3000,"totalBytes":42785,"outputLines":2000,"outputBytes":27785},"nextOffset":2001,"kind":"read"}`
)

// realBashSpillText is the text of `seq 1 60000`: the first 800 lines, the
// elision marker, the last 1200 lines and the spill file line.
func realBashSpillText() string {
	var b strings.Builder
	b.WriteString("exit: 0\n")
	for i := 1; i <= 800; i++ {
		fmt.Fprintf(&b, "%d\n", i)
	}
	b.WriteString("\n[... 338602 bytes elided ...]\n")
	for i := 58801; i <= 60000; i++ {
		fmt.Fprintf(&b, "%d\n", i)
	}
	b.WriteString("[full output: " + realBashSpillPath + "]")
	return b.String()
}

// realReadNumbered is the numbered text of a file whose line n is "line n"
// and whose first line is "package tui".
func realReadNumbered(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%d|line %d", i+1, i+1)
	}
	lines[0] = "1|package tui"
	return strings.Join(lines, "\n")
}

// feedResult plays a tool call with a result of the given text and details.
// The rows show in the truncated form unless the test chose another one.
func feedResult(x *tx, name, text, details string, failed bool) {
	x.t.Helper()
	if x.rc.Fold == FoldCollapsed {
		x.rc.Fold = FoldTruncated
	}
	x.feed(toolStart("c1", name, `{}`, 0), &protocol.ToolExecutionEnd{
		Envelope: at(100), ToolCallID: "c1", ToolName: name, IsError: failed,
		Result: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Details: json.RawMessage(details)},
	})
}

func TestProducerBashOKShowsOutputWithoutExitHeader(t *testing.T) {
	x := newTx(t)
	feedResult(x, "bash", "exit: 0\none\ntwo\nthree", realBashOKDetails, false)
	require.Equal(t, []string{"    one", "    two", "    three"}, x.rows()[1:])
}

func TestProducerBashWithoutOutputKeepsTheNote(t *testing.T) {
	x := newTx(t)
	feedResult(x, "bash", "exit: 0\n(no output)", realBashOKDetails, false)
	require.Equal(t, []string{"    (no output)"}, x.rows()[1:])
}

func TestProducerBashFailureShowsExitOnceOnTheToolLine(t *testing.T) {
	x := newTx(t)
	feedResult(x, "bash", "exit: 1\nboom", realBashFailDetails, true)
	rows := x.rows()
	require.True(t, strings.HasSuffix(rows[0], "✗ exit 1"), rows[0])
	require.Equal(t, []string{"    boom"}, rows[1:])
	require.Equal(t, 1, strings.Count(strings.Join(rows, "\n"), "exit 1"))
}

func TestProducerBashTwelveLinesShowThreeSixMoreAndThree(t *testing.T) {
	x := newTx(t)
	feedResult(x, "bash", "exit: 0\n"+fixtureLines("out", 12), realBashOKDetails, false)
	rows := x.rows()
	require.Equal(t, numberedRows("out", 1, 3), rows[1:4])
	require.Equal(t, "    ⋮ 6 more lines · e to expand · Enter full screen", rows[4])
	require.Equal(t, numberedRows("out", 10, 12), rows[5:8])
}

func TestProducerBashSpilledOutputShowsNoSpillLineInTheTruncatedForm(t *testing.T) {
	x := newTx(t)
	feedResult(x, "bash", realBashSpillText(), realBashSpillInfo, false)
	rows := x.rows()
	require.Equal(t, "    1", rows[1], "the first output line is the first body row")
	require.Equal(t, "    ⋮ 1996 more lines · e to expand · Enter full screen", rows[4], "the output holds 800 + 1 + 1 + 1200 lines; the spill line is not one")
	require.Equal(t, "    60000", rows[len(rows)-1], "the last output line is the last row")
	require.NotContains(t, strings.Join(rows, "\n"), "model saw", "the truncation line shows only in the expanded form")
	require.Equal(t, 0, strings.Count(strings.Join(rows, ""), realBashSpillPath))
}

func TestProducerBashSpillCapLineIsRemovedWhenTheDetailsSayCapped(t *testing.T) {
	x := newTx(t)
	d := producerDetails(t, realBashSpillInfo)
	d.SpillCapped = true
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	text := realBashSpillText() + "\n[spill file capped at 64 MB]"
	feedResult(x, "bash", text, string(raw), false)
	x.rc.Fold = FoldExpanded
	all := strings.Join(x.rows(), "\n")
	require.NotContains(t, all, "[spill file capped at 64 MB]")
	require.NotContains(t, all, "[full output:")
	require.Contains(t, all, "(file stopped at 64 MB)")
}

func TestProducerBashFullViewHasNoModelDecoration(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	feedResult(x, "bash", realBashSpillText(), realBashSpillInfo, false)
	rows := x.rows()
	all := strings.Join(rows, "\n")
	require.NotContains(t, all, "exit: 0")
	require.NotContains(t, all, "[full output:")
	require.Equal(t, "    1", rows[1])
	require.Contains(t, all, "[... 338602 bytes elided ...]", "the cut of the model view stays")
}

func TestProducerReadShowsTextWithoutLineNumbers(t *testing.T) {
	x := newTx(t)
	feedResult(x, "read", "1|package tui\n2|import x\n3|\n4|func a() {}", realReadFullDetails, false)
	require.Equal(t, []string{"    package tui", "    import x", "", "    func a() {}"}, x.rows()[1:])
}

func TestProducerReadOf172LinesShowsThreeHintAndThree(t *testing.T) {
	x := newTx(t)
	feedResult(x, "read", realReadNumbered(172), realReadFullDetails, false)
	rows := x.rows()
	require.Equal(t, "    package tui", rows[1])
	require.Equal(t, "    line 3", rows[3])
	require.Equal(t, "    ⋮ 166 more lines · e to expand · Enter full screen", rows[4])
	require.Equal(t, "    line 172", rows[7])
}

func TestProducerReadWithLimitDropsTheContinueNotice(t *testing.T) {
	x := newTx(t)
	text := "1|line 1\n2|line 2\n3|line 3\n4|line 4\n5|line 5\n\n[167 more lines in file. Use offset=6 to continue.]"
	feedResult(x, "read", text, realReadLimitDetails, false)
	rows := x.rows()
	require.Equal(t, "    line 1", rows[1])
	require.Equal(t, "    line 5", rows[5])
	require.NotContains(t, strings.Join(rows, "\n"), "more lines in file")
	require.Len(t, rows, 6)
}

func TestProducerReadTruncatedDropsTheNoticeAndShowsNextOffset(t *testing.T) {
	x := newTx(t)
	var b strings.Builder
	for i := 1; i <= 2000; i++ {
		fmt.Fprintf(&b, "%d|line %d\n", i, i)
	}
	text := b.String() + "\n[Showing lines 1-2000 of 3000. Use offset=2001 to continue.]"
	feedResult(x, "read", text, realReadTruncDetails, false)
	rows := x.rows()
	require.Equal(t, "    line 1", rows[1])
	require.Equal(t, "    ⋮ 1994 more lines · e to expand · Enter full screen", rows[4])
	x.rc.Fold = FoldExpanded
	rows = x.rows()
	require.Equal(t, "    model saw 2000 of 3000 lines · next offset 2001", rows[len(rows)-1])
	require.NotContains(t, strings.Join(rows, "\n"), "Showing lines")
}

func TestProducerReadFullViewHasNoNumbersAndNoNotice(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldExpanded
	feedResult(x, "read", "1|a\n2|b\n\n[Showing lines 1-2 of 9. Use offset=3 to continue.]",
		`{"kind":"read","truncation":{"truncatedBy":"lines","totalLines":9,"outputLines":2},"nextOffset":3}`, false)
	require.Equal(t, []string{"    a", "    b", "    model saw 2 of 9 lines · next offset 3"}, x.rows()[1:])
}

// --- text that only looks like model decoration stays ---

func TestProducerKeepsTextWhenTheDetailsDoNotConfirmIt(t *testing.T) {
	for _, tc := range []struct {
		name, tool, text, details string
		want                      []string
	}{
		{"exit line without exit code", "bash", "exit: 1\nboom", `{"kind":"execute"}`, []string{"    exit: 1", "    boom"}},
		{"exit line of another code", "bash", "exit: 2\nboom", realBashOKDetails, []string{"    exit: 2", "    boom"}},
		{"spill line without spill path", "bash", "exit: 0\n[full output: /tmp/x.log]", realBashOKDetails, []string{"    [full output: /tmp/x.log]"}},
		{"spill line of another path", "bash", "exit: 0\n[full output: /tmp/x.log]", `{"kind":"execute","exitCode":0,"spillPath":"/tmp/y.log"}`, []string{"    [full output: /tmp/x.log]"}},
		{"numbers without read kind", "read", "1|a\n2|b", `{}`, []string{"    1|a", "    2|b"}},
		{"one line without number", "read", "1|a\nplain\n3|c", realReadFullDetails, []string{"    1|a", "    plain", "    3|c"}},
		{"numbers that skip", "read", "1|a\n3|c", realReadFullDetails, []string{"    1|a", "    3|c"}},
		{"numbers from another line", "read", "5|a\n6|b", realReadFullDetails, []string{"    5|a", "    6|b"}},
		{"notice without next offset", "read", "1|a\n\n[3 more lines in file. Use offset=2 to continue.]", realReadFullDetails,
			[]string{"    1|a", "", "    [3 more lines in file. Use offset=2 to continue.]"}},
		{"notice of another offset", "read", "1|a\n\n[3 more lines in file. Use offset=9 to continue.]", realReadLimitDetails,
			[]string{"    1|a", "", "    [3 more lines in file. Use offset=9 to continue.]"}},
	} {
		x := newTx(t)
		feedResult(x, tc.tool, tc.text, tc.details, false)
		rows := x.rows()
		got := rows[1 : 1+len(tc.want)]
		require.Equal(t, tc.want, got, tc.name)
	}
}

func TestProducerStripsOnlyTheFirstLineNumberOfEachLine(t *testing.T) {
	// A file whose own lines read "1|x": the producer numbers them again.
	x := newTx(t)
	feedResult(x, "read", "1|1|x\n2|2|y", realReadFullDetails, false)
	require.Equal(t, []string{"    1|x", "    2|y"}, x.rows()[1:])
}

func TestProducerStreamingBashShowsNoSpillLine(t *testing.T) {
	x := newTx(t)
	x.rc.Fold = FoldTruncated
	x.feed(toolStart("c1", "bash", `{}`, 0), &protocol.ToolExecutionUpdate{
		Envelope: at(50), ToolCallID: "c1", ToolName: "bash",
		PartialResult: protocol.ToolExecutionResult{
			Content: []protocol.UserBlock{protocol.Text{Text: "one\ntwo\n[full output: /tmp/b.log]"}},
			Details: json.RawMessage(`{"kind":"execute","spillPath":"/tmp/b.log"}`),
		},
	})
	require.Equal(t, []string{"    one", "    two"}, x.rows()[1:])
}

func TestToolTruncationCleansTheSpillPathWhenHeadAndPathShareARow(t *testing.T) {
	x := newTx(t)
	d := protocol.ToolResultDetails{
		Truncation: &protocol.ToolTruncation{TruncatedBy: "bytes", OutputLines: 3, OutputBytes: 90, TotalBytes: 2048},
		SpillPath:  "/tmp/a\rb.log",
	}
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	x.rc.Fold = FoldExpanded
	feedResult(x, "bash", "x", string(raw), false)
	for _, r := range x.rows() {
		require.NotContains(t, r, "\r")
	}
	require.NotContains(t, strings.Join(x.rows(), "\n"), "/tmp/a\rb.log")
}
