package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"AskCore/pkg/protocol"
)

// Tool output of a tool block.
//
// A tool block keeps what the tool result carries: the full text, the diff of
// a file change, and the facts about a cut of the model view. The truncated
// form of the block shows the first and the last rows of it; the expanded form
// shows all of it.

// toolOutput is the output of one tool call. The zero value is no output.
type toolOutput struct {
	// text is the text of the result. lines are its lines, without the final
	// line end; a text that holds only white space has no lines.
	text  string
	lines []string
	// diff is the file change of an edit or write. added and removed count the
	// lines of its line diff.
	diff           *protocol.ToolDiff
	added, removed int
	// diffRows are the rows of the unified diff of the file change.
	diffRows []diffRow
	// truncation, nextOffset, spillPath and spillCapped tell how much of the
	// output the model saw and where the rest is.
	truncation  *protocol.ToolTruncation
	nextOffset  int
	spillPath   string
	spillCapped bool
}

// newToolOutput returns the output of a result text and its details.
func newToolOutput(text string, d protocol.ToolResultDetails) toolOutput {
	o := toolOutput{
		text:        text,
		lines:       outputLines(text),
		diff:        d.Diff,
		truncation:  d.Truncation,
		nextOffset:  d.NextOffset,
		spillPath:   d.SpillPath,
		spillCapped: d.SpillCapped,
	}
	if d.Diff != nil {
		o.diffRows, o.added, o.removed = unifiedDiff(d.Diff.OldText, d.Diff.NewText)
	}
	return o
}

// toolOutputOf returns the output of a tool result. Details that cannot be
// read are ignored: they only add to the display.
func toolOutputOf(content []protocol.UserBlock, details json.RawMessage) toolOutput {
	var d protocol.ToolResultDetails
	if len(details) > 0 && json.Unmarshal(details, &d) != nil {
		d = protocol.ToolResultDetails{}
	}
	return newToolOutput(withoutModelNotes(blocksText(content), d), d)
}

// Decoration that the bash and read tools add for the model. The result text
// carries it, and the details carry the same facts, which the tool line and the
// truncation rows show. Text is removed only when it has the exact form of the
// tool and the details confirm the fact; anything else is user data.
const (
	shellSpillCapText = "[spill file capped at 64 MB]"
	readContinueText  = `\. Use offset=(\d+) to continue\.\]$`
)

var (
	// readTruncatedNotice ends a read that the tool cut at its limits.
	readTruncatedNotice = regexp.MustCompile(`\n\n\[Showing lines \d+-\d+ of \d+(?: \([^)]*\))?` + readContinueText)
	// readLimitNotice ends a read that stopped at the limit argument.
	readLimitNotice = regexp.MustCompile(`\n\n\[\d+ more lines in file` + readContinueText)
	// readLineNumber starts each line of a read result.
	readLineNumber = regexp.MustCompile(`^(\d+)\|`)
)

// withoutModelNotes returns the text of a tool result without the lines that only tell
// the model about the call.
func withoutModelNotes(text string, d protocol.ToolResultDetails) string {
	switch d.Kind {
	case protocol.ToolKindExecute:
		return shellUserText(text, d)
	case protocol.ToolKindRead:
		return readUserText(text, d)
	}
	return text
}

// shellUserText drops the exit header, which the tool line shows as status,
// and the lines about the spill file, which the truncation rows show.
func shellUserText(text string, d protocol.ToolResultDetails) string {
	lines := strings.Split(text, "\n")
	var header string
	switch {
	case d.ExitCode != nil:
		header = fmt.Sprintf("exit: %d", *d.ExitCode)
	case d.Signal != "":
		header = "signal: " + d.Signal
	}
	if first := lines[0]; header != "" && (first == header || (strings.HasPrefix(first, header+" (") && strings.HasSuffix(first, ")"))) {
		lines = lines[1:]
	}
	if d.SpillPath != "" {
		marker := "[full output: " + d.SpillPath + "]"
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i] != marker {
				continue
			}
			drop := 1
			if d.SpillCapped && i+1 < len(lines) && lines[i+1] == shellSpillCapText {
				drop = 2
			}
			lines = append(lines[:i:i], lines[i+drop:]...)
			break
		}
	}
	return strings.Join(lines, "\n")
}

// readUserText drops the notice that tells the model where to continue and the
// line numbers. The numbers go only when every line has one, they count up by
// one, and the first is the line that the details name.
func readUserText(text string, d protocol.ToolResultDetails) string {
	body := text
	if d.NextOffset > 0 {
		re := readLimitNotice
		if d.Truncation != nil {
			re = readTruncatedNotice
		}
		if m := re.FindStringSubmatchIndex(text); m != nil && text[m[2]:m[3]] == strconv.Itoa(d.NextOffset) {
			body = text[:m[0]]
		}
	}
	if body == "" {
		return text
	}
	lines := strings.Split(body, "\n")
	want := 0
	if len(d.Locations) > 0 {
		want = d.Locations[0].Line
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		m := readLineNumber.FindStringSubmatch(line)
		if m == nil {
			return body
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || (want > 0 && n != want) {
			return body
		}
		want = n + 1
		out[i] = line[len(m[0]):]
	}
	return strings.Join(out, "\n")
}

// outputLines splits a text into lines. A final line end does not start
// another line, and a line end of two characters (CR LF) is one line end.
func outputLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// truncatedRows returns the rows of the truncated form: the first three rows
// of the output, a hint that counts the rows between, and the last three rows.
// An output of six rows or fewer shows all rows and no hint. Each output line
// is one row: it is cut, never wrapped. The rows of a file change are the rows
// of its diff.
func (o toolOutput) truncatedRows(rc RenderContext) []string {
	th, width := rc.Theme, bodyWidth(rc)
	pad := strings.Repeat(" ", textIndent)
	var rows []string
	if o.diff != nil {
		rows = o.diffRowsFor(rc, false)
	} else {
		for _, line := range o.lines {
			line = truncateTo(cleanText(ansi.Strip(line)), width)
			if line == "" {
				rows = append(rows, "")
				continue
			}
			rows = append(rows, pad+th.Render(RoleFg, line))
		}
	}
	if len(rows) <= 2*truncatedEdge {
		return rows
	}
	hidden := len(rows) - 2*truncatedEdge
	word := "lines"
	if hidden == 1 {
		word = "line"
	}
	hint := fmt.Sprintf("%s %d more %s · e to expand · Enter full screen", GlyphMore, hidden, word)
	if rc.Width < hintShortWidth {
		hint = fmt.Sprintf("%s %d more %s · e", GlyphMore, hidden, word)
	}
	out := append([]string(nil), rows[:truncatedEdge]...)
	out = append(out, pad+th.Render(RoleMuted, truncateTo(hint, width)))
	return append(out, rows[len(rows)-truncatedEdge:]...)
}

// fullRows returns the rows of the whole output for the expanded form and the
// viewer: the diff of a file change, or every line of the text. A long line
// wraps by columns; nothing is cut and no hint is shown.
func (o toolOutput) fullRows(rc RenderContext) []string {
	if o.diff != nil {
		return o.diffRowsFor(rc, true)
	}
	th, width := rc.Theme, bodyWidth(rc)
	pad := strings.Repeat(" ", textIndent)
	rows := make([]string, 0, len(o.lines))
	for _, line := range o.lines {
		for _, part := range columnCut(cleanText(ansi.Strip(line)), width) {
			if part == "" {
				rows = append(rows, "")
				continue
			}
			rows = append(rows, pad+th.Render(RoleFg, part))
		}
	}
	return rows
}

// spillCapText is the note of a spill file that stopped at its size cap.
const spillCapText = "(file stopped at 64 MB)"

// truncationRows returns the muted rows that say how much of the output the
// model saw and where the full output is. The path is never cut: when the row
// does not hold it, the path goes to rows of its own and wraps by columns.
func (o toolOutput) truncationRows(rc RenderContext) []string {
	var head string
	if tr := o.truncation; tr != nil {
		if tr.TotalLines > 0 {
			head = fmt.Sprintf("model saw %d of %d lines", tr.OutputLines, tr.TotalLines)
			if o.nextOffset > 0 {
				head += fmt.Sprintf(" · next offset %d", o.nextOffset)
			}
		} else {
			head = fmt.Sprintf("model saw %d lines (%s) of %s", tr.OutputLines, formatSize(int64(tr.OutputBytes)), formatSize(int64(tr.TotalBytes)))
		}
	}
	var spill string
	if o.spillPath != "" {
		spill = "full output: " + ansi.Strip(o.spillPath)
		if o.spillCapped {
			spill += " " + spillCapText
		}
		spill = cleanText(spill)
	}
	if head == "" && spill == "" {
		return nil
	}
	th, width := rc.Theme, bodyWidth(rc)
	pad := strings.Repeat(" ", textIndent)
	var text []string
	switch {
	case spill == "":
		text = []string{truncateTo(head, width)}
	case head == "":
		text = wrapLine(spill, width)
	case displayWidth(head)+3+displayWidth(spill) <= width:
		text = []string{head + " · " + spill}
	default:
		text = append([]string{truncateTo(head, width)}, wrapLine(spill, width)...)
	}
	rows := make([]string, len(text))
	for i, r := range text {
		rows[i] = pad + th.Render(RoleMuted, r)
	}
	return rows
}

// formatSize writes a size in bytes: one decimal place below 10 of a unit and
// none from 10, for example "900 B", "2.4 MB", "50 KB".
func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", max(n, 0))
	}
	units := []string{"KB", "MB", "GB"}
	v := float64(n) / 1024
	u := 0
	// A value that rounds to 1024 of a unit is shown in the next unit.
	for v >= 1023.95 && u < len(units)-1 {
		v /= 1024
		u++
	}
	if v < 9.95 {
		return fmt.Sprintf("%.1f %s", v, units[u])
	}
	return fmt.Sprintf("%.0f %s", v, units[u])
}
