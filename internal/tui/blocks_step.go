package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Step blocks: tools and thinking.
//
// A step is one row by default: a glyph, a bold verb, what it did, and a right
// column for the outcome. The same block opens in two more forms, chosen by
// RenderContext.Fold. Truncated shows the first and the last rows of the
// output. Expanded shows everything, with nothing cut.

const (
	// stepRightPad is the number of spaces after the right column.
	stepRightPad = 1
	// stepNarrowWidth is the width under which the right column goes away.
	stepNarrowWidth = 30
	// minRightWidth is the least width that a cut right column keeps.
	minRightWidth = 3
	// hintShortWidth is the width under which the fold hint is the short form.
	hintShortWidth = 60

	// truncatedEdge is the number of output rows that Truncated shows before and
	// after the hint.
	truncatedEdge = 3
	// thinkingTail is the number of last lines that Truncated thinking shows
	// when the block has ended; a block that streams shows streamingTail.
	thinkingTail  = 3
	streamingTail = 2
)

// stepRow is the data of the collapsed row of a step.
type stepRow struct {
	glyph     string
	glyphRole Role
	// indicator, when it is not empty, stands in the place of the glyph: the
	// expand indicator of a step that is selected or under the pointer. It is
	// padded to the width of the glyph.
	indicator string
	verb      string
	what      string
	// added and removed are the line counts of a file change. showStat tells
	// which of them show.
	added, removed int
	showStat       statKind
	// right is the outcome at the right edge.
	right     string
	rightRole Role
	// fitWhat, when set, shortens the description for a room in columns and
	// returns what shows there; an empty result shows nothing. Without it the
	// description is cut at the end with an ellipsis.
	fitWhat func(room int) string
}

type statKind uint8

const (
	statNone statKind = iota
	// statAdded shows "+N" only, as for a file that is written.
	statAdded
	// statBoth shows "+N -M".
	statBoth
)

func (r stepRow) statText() string {
	switch r.showStat {
	case statAdded:
		return fmt.Sprintf("+%d", r.added)
	case statBoth:
		return fmt.Sprintf("+%d -%d", r.added, r.removed)
	}
	return ""
}

// render draws the row for a width. When the row is too narrow, the part that
// the user needs least goes first: the description is cut with an ellipsis,
// then the right column goes below stepNarrowWidth, then the line counts, then
// the verb is cut. Rows hold no more than width columns. cut is true when the
// description did not show whole.
func (r stepRow) render(rc RenderContext) (row string, cut bool) {
	th, width := rc.Theme, rc.Width
	leadW := glyphIndent + displayWidth(r.glyph) + 1
	verb := oneLine(r.verb)
	right := r.right
	if width < stepNarrowWidth {
		right = ""
	}
	rightPart := func() int {
		if right == "" {
			return 0
		}
		return displayWidth(right) + stepRightPad + 1
	}
	stat := r.statText()
	statW := func() int {
		if stat == "" {
			return 0
		}
		return 2 + displayWidth(stat)
	}
	// The right column keeps its place and is cut only when it does not fit
	// beside the verb and the line counts. Without the line counts there may be
	// room for it.
	fitRight := func() {
		room := width - leadW - displayWidth(verb) - statW() - 1 - stepRightPad
		if right != "" && displayWidth(right) > room {
			if room >= minRightWidth {
				right = truncateTo(right, room)
			} else {
				right = ""
			}
		}
	}
	fitRight()
	if right == "" && r.right != "" && width >= stepNarrowWidth {
		stat = ""
		right = r.right
		fitRight()
	}
	if leadW+displayWidth(verb)+statW() > width {
		stat = ""
	}
	verb = truncateTo(verb, max(1, width-leadW-statW()-rightPart()))

	used := leadW + displayWidth(verb)
	var b strings.Builder
	glyph, glyphStyle := r.glyph, th.Style(r.glyphRole)
	if r.indicator != "" {
		glyph, glyphStyle = indicatorGlyph(r.indicator, r.glyph), th.Style(RoleAccent).Bold(true)
	}
	b.WriteString(strings.Repeat(" ", glyphIndent) + glyphStyle.Render(glyph) + " ")
	b.WriteString(th.Style(RoleFg).Bold(true).Render(verb))
	if what := oneLine(r.what); what != "" {
		// One space before the description and one before the right column.
		room := width - used - 1 - statW() - rightPart()
		shown := ""
		if room >= 2 {
			if r.fitWhat != nil {
				shown = r.fitWhat(room)
			} else {
				shown = truncateTo(what, room)
			}
			if shown != "" {
				b.WriteString(" " + th.Render(RoleFg, shown))
				used += 1 + displayWidth(shown)
			}
		}
		cut = shown != what
	}
	if stat != "" {
		b.WriteString("  " + th.Render(RoleSuccess, fmt.Sprintf("+%d", r.added)))
		if r.showStat == statBoth {
			b.WriteString(" " + th.Render(RoleError, fmt.Sprintf("-%d", r.removed)))
		}
		used += statW()
	}
	if right != "" {
		gap := max(1, width-used-rightPart()+1)
		b.WriteString(strings.Repeat(" ", gap) + th.Render(r.rightRole, right) + strings.Repeat(" ", stepRightPad))
	}
	return b.String(), cut
}

// runningText is the elapsed time of a running step: whole seconds, then
// minutes and seconds.
func runningText(secs int) string {
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
}

// railRows draws text lines on the rail of a step body: two spaces, the rail
// mark and the text, all in one role.
func railRows(th *Theme, role Role, lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			out[i] = strings.Repeat(" ", glyphIndent) + th.Render(role, GlyphRail)
			continue
		}
		out[i] = strings.Repeat(" ", glyphIndent) + th.Render(role, GlyphRail+" "+l)
	}
	return out
}

// --- thinking ---

type thinkingBlock struct {
	text string
	view *TranscriptView
	// streaming is true until the thinking ends.
	streaming bool
	// startMs is the time of the first event of the thinking, and durMs is how
	// long it took. durKnown is false when the events carried no time, as in a
	// snapshot of history.
	startMs  int64
	durMs    int64
	durKnown bool
}

// NewThinkingBlock returns the block of a thinking that has ended, without a
// duration.
func NewThinkingBlock(text string, view *TranscriptView) Block {
	return thinkingBlock{text: text, view: view}
}

// newThinkingBlock returns a thinking block. While it streams it counts the
// seconds from startMs; when it has ended, durMs is its duration if durKnown.
func newThinkingBlock(text string, view *TranscriptView, streaming bool, startMs, durMs int64, durKnown bool) Block {
	return thinkingBlock{text: text, view: view, streaming: streaming, startMs: startMs, durMs: durMs, durKnown: durKnown}
}

func (b thinkingBlock) drawState() uint64 {
	if !b.streaming {
		return 0
	}
	return b.view.runningState(b.startMs)
}

func (b thinkingBlock) foldable(int) bool    { return true }
func (b thinkingBlock) foldGroup() foldGroup { return groupThinking }

// defaultFold shows the last lines while the thinking streams and one row
// when it has ended.
func (b thinkingBlock) defaultFold() FoldMode { return b.collapseFold() }

func (b thinkingBlock) collapseFold() FoldMode {
	if b.streaming {
		return FoldTruncated
	}
	return FoldCollapsed
}

func (b thinkingBlock) title(rc RenderContext) string {
	r := stepRow{glyph: GlyphStep, glyphRole: RoleThinking, indicator: indicatorFor(rc, b.collapseFold())}
	if b.streaming {
		r.verb = "Thinking…"
		if secs := b.view.elapsedSeconds(b.startMs); secs >= 0 {
			r.what = runningText(secs)
		}
		r.right, r.rightRole = b.view.frame(), RoleAccent
	} else {
		r.verb = "Thought"
		if b.durKnown {
			r.what = "for " + formatDurationMs(b.durMs)
		}
	}
	row, _ := r.render(rc)
	return row
}

// formatDurationMs writes a duration in milliseconds as formatDuration does.
func formatDurationMs(ms int64) string {
	return formatDuration(time.Duration(ms) * time.Millisecond)
}

func (b thinkingBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	rows := []string{b.title(rc)}
	text := strings.TrimSpace(cleanText(ansi.Strip(b.text)))
	if text == "" || rc.Fold == FoldCollapsed && !b.streaming {
		return rows
	}
	if rc.Fold == FoldExpanded {
		return append(rows, railRows(th, RoleThinking, wrapLines(text, max(1, rc.Width-textIndent)))...)
	}
	// Truncated: the last lines, each cut to one row.
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	tail := thinkingTail
	if b.streaming {
		tail = streamingTail
	}
	var body []string
	if len(lines) > tail {
		body = append(body, "…")
		lines = lines[len(lines)-tail:]
	}
	for _, l := range lines {
		body = append(body, truncateTo(l, max(1, rc.Width-textIndent)))
	}
	return append(rows, railRows(th, RoleThinking, body)...)
}

// --- tool step ---

// ToolState is the stage of a tool step.
type ToolState int

// Tool step stages.
const (
	ToolRunning ToolState = iota
	ToolSucceeded
	ToolFailed
	// ToolNoted is a finished line whose status is a plain muted note, such
	// as the event count of a monitor.
	ToolNoted
)

// toolCall is what the block knows about the call.
type toolCall struct {
	name string
	// args is the one-line summary of the arguments.
	args string
	// raw holds the arguments as the model sent them. It may be empty, for
	// example for a call that the block draws from a summary only.
	raw json.RawMessage
	// startMs is the time of the start event, for the seconds of a running
	// step. Zero when it is not known.
	startMs int64
}

type toolLineBlock struct {
	call     toolCall
	state    ToolState
	duration string // the note of ToolNoted
	reason   string // set when the tool failed
	out      toolOutput
	view     *TranscriptView
	// notice is true for a one-row line that reports an event, such as a
	// finished background job. It has no output and does not fold.
	notice bool
}

// NewToolLineBlock returns a tool step. A running step shows the spinner frame
// of the view; a failed step shows the reason at the right.
func NewToolLineBlock(name, args string, state ToolState, duration, reason string, view *TranscriptView) Block {
	return newToolCallBlock(toolCall{name: name, args: args}, state, duration, reason, toolOutput{}, view)
}

// newToolBlock returns a tool step that holds the output of the tool.
func newToolBlock(name, args string, state ToolState, duration, reason string, out toolOutput, view *TranscriptView) Block {
	return newToolCallBlock(toolCall{name: name, args: args}, state, duration, reason, out, view)
}

func newToolCallBlock(c toolCall, state ToolState, duration, reason string, out toolOutput, view *TranscriptView) Block {
	return toolLineBlock{call: c, state: state, duration: duration, reason: reason, out: out, view: view}
}

// newJobNotice returns the one-row line of a background job event.
func newJobNotice(name, label string, state ToolState, status, reason string, view *TranscriptView) Block {
	return toolLineBlock{call: toolCall{name: name, args: label}, state: state, duration: status, reason: reason, view: view, notice: true}
}

// drawState is the spinner frame and the seconds of a running step, which
// change the rows. A finished step does not depend on the view.
func (b toolLineBlock) drawState() uint64 {
	if b.state != ToolRunning {
		return 0
	}
	return b.view.runningState(b.call.startMs)
}

func (b toolLineBlock) foldable(int) bool      { return !b.notice }
func (b toolLineBlock) defaultFold() FoldMode  { return FoldCollapsed }
func (b toolLineBlock) collapseFold() FoldMode { return FoldCollapsed }
func (b toolLineBlock) foldGroup() foldGroup   { return groupTool }

// diffStat tells which line counts the row shows: a written file shows the
// added lines only.
func (b toolLineBlock) diffStat() statKind {
	switch {
	case b.out.diff == nil:
		return statNone
	case b.call.name == "write":
		return statAdded
	}
	return statBoth
}

var exitReason = regexp.MustCompile(`^exit: (-?\d+)\b`)

// failureText is the reason of a failed step as the right column shows it:
// the exit header of the shell tool becomes "exit <n>".
func failureText(reason string) string {
	reason = oneLine(reason)
	if reason == "" {
		return "failed"
	}
	if m := exitReason.FindStringSubmatch(reason); m != nil {
		return "exit " + m[1]
	}
	return reason
}

// toolVerbs gives the verb of each built-in tool for a finished and a running
// step. A tool that is not listed uses its own name.
var toolVerbs = map[string][2]string{
	"bash":       {"Ran", "Running"},
	"read":       {"Read", "Reading"},
	"edit":       {"Edited", "Editing"},
	"write":      {"Wrote", "Writing"},
	"grep":       {"Searched", "Searching"},
	"find":       {"Found", "Finding"},
	"ls":         {"Listed", "Listing"},
	"job_output": {"Checked job", "Checking job"},
	"job_kill":   {"Stopped job", "Stopping job"},
	"job_list":   {"Listed jobs", "Listing jobs"},
	"monitor":    {"Watched", "Watching"},
}

// toolVerb returns the verb of a tool. known is false when the tool has no
// entry and the verb is its name.
func toolVerb(name string, running bool) (verb string, known bool) {
	if v, ok := toolVerbs[name]; ok {
		if running {
			return v[1], true
		}
		return v[0], true
	}
	if name = oneLine(name); name == "" {
		name = "tool"
	}
	return name, false
}

// argString returns a string argument of a call, or "" when it is missing or
// not a string.
func argString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	var s string
	if v, ok := obj[key]; !ok || json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

// commandSummary writes the first line of a command; a command of more lines
// ends with " …".
func commandSummary(cmd string) string {
	var first string
	more := false
	for _, l := range strings.Split(strings.ReplaceAll(cmd, "\r\n", "\n"), "\n") {
		if oneLine(l) == "" {
			continue
		}
		if first == "" {
			first = oneLine(l)
			continue
		}
		more = true
		break
	}
	if more {
		return first + " …"
	}
	return first
}

// whatOf is the description of a call on the collapsed row. The fallback is the
// one-line summary of the arguments.
func (c toolCall) whatOf() string {
	var what string
	switch c.name {
	case "bash", "monitor":
		what = commandSummary(argString(c.raw, "command"))
	case "read", "edit", "write", "ls":
		what = oneLine(argString(c.raw, "path"))
	case "find":
		what = oneLine(argString(c.raw, "pattern"))
	case "grep":
		if p := oneLine(argString(c.raw, "pattern")); p != "" {
			what = `"` + p + `"`
			if path := oneLine(argString(c.raw, "path")); path != "" {
				what += " " + path
			}
		}
	case "job_output", "job_kill":
		what = oneLine(argString(c.raw, "job_id"))
	}
	if what == "" {
		return oneLine(c.args)
	}
	return what
}

// commandOf is the command that the expanded form shows on its "$" row: the
// full command of a shell call, line breaks kept. It is empty for a tool that
// has no command.
func (c toolCall) commandOf() string {
	if c.name != "bash" && c.name != "monitor" {
		return ""
	}
	if cmd := argString(c.raw, "command"); strings.TrimSpace(cmd) != "" {
		return cmd
	}
	return c.args
}

// row builds the collapsed row of the step.
func (b toolLineBlock) row() stepRow {
	verb, _ := toolVerb(b.call.name, b.state == ToolRunning)
	r := stepRow{glyph: GlyphStep, glyphRole: RoleTool, verb: verb, what: b.call.whatOf(), added: b.out.added, removed: b.out.removed, showStat: b.diffStat()}
	switch b.state {
	case ToolRunning:
		r.glyphRole, r.rightRole = RoleWarning, RoleWarning
		r.right = b.view.frame()
		if secs := b.view.elapsedSeconds(b.call.startMs); secs >= 0 {
			r.right += " " + runningText(secs)
		}
	case ToolNoted:
		r.glyphRole, r.rightRole = RoleMuted, RoleMuted
		r.right = oneLine(b.duration)
	case ToolFailed:
		r.glyphRole, r.rightRole = RoleError, RoleError
		r.right = GlyphFail + " " + failureText(b.reason)
	}
	return r
}

// commandRows draws the "$ <command>" rows of the expanded form: the whole
// command, wrapped. A tool that has no command shows the whole description
// instead when the row had to cut it (whatCut).
func (b toolLineBlock) commandRows(rc RenderContext, whatCut bool) []string {
	th := rc.Theme
	width := max(1, rc.Width-textIndent)
	cmd := b.call.commandOf()
	if cmd == "" {
		if !whatCut {
			return nil
		}
		return styledPad(th, RoleDim, wrapLine(b.call.whatOf(), width), "")
	}
	var rows []string
	for i, line := range strings.Split(cleanText(ansi.Strip(strings.ReplaceAll(cmd, "\r\n", "\n"))), "\n") {
		prefix := "$ "
		if i > 0 {
			prefix = "  "
		}
		for j, part := range wrapLine(line, max(1, width-2)) {
			if j > 0 {
				prefix = "  "
			}
			rows = append(rows, strings.Repeat(" ", textIndent)+th.Render(RoleDim, prefix+part))
		}
	}
	return rows
}

// styledPad indents rows to the text column in one role.
func styledPad(th *Theme, role Role, rows []string, prefix string) []string {
	pad := strings.Repeat(" ", textIndent)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = pad + th.Render(role, prefix+r)
	}
	return out
}

func (b toolLineBlock) Lines(rc RenderContext) []string {
	fold := rc.Fold
	if b.notice {
		fold = FoldCollapsed
	}
	head := b.row()
	if !b.notice {
		head.indicator = indicatorFor(rc, FoldCollapsed)
	}
	row, cut := head.render(rc)
	rows := []string{row}
	switch fold {
	case FoldCollapsed:
		return rows
	case FoldTruncated:
		return append(rows, b.out.truncatedRows(rc)...)
	}
	rows = append(rows, b.commandRows(rc, cut)...)
	rows = append(rows, b.out.fullRows(rc)...)
	if b.state != ToolRunning {
		rows = append(rows, b.out.truncationRows(rc)...)
	}
	return rows
}

// --- viewer ---

// viewerHead is the title of the viewer for a thinking: the collapsed row
// without its glyph, and the spinner while it streams.
func (b thinkingBlock) viewerHead(rc RenderContext) (title, outcome string, role Role) {
	if b.streaming {
		title = "Thinking…"
		if secs := b.view.elapsedSeconds(b.startMs); secs >= 0 {
			title += " " + runningText(secs)
		}
		return title, b.view.frame(), RoleAccent
	}
	title = "Thought"
	if b.durKnown {
		title += " for " + formatDurationMs(b.durMs)
	}
	return title, "", RoleFg
}

// viewerBody shows every line of the thinking, wrapped.
func (b thinkingBlock) viewerBody(rc RenderContext) []string {
	text := strings.TrimSpace(cleanText(ansi.Strip(b.text)))
	if text == "" {
		return nil
	}
	return styledPad(rc.Theme, RoleThinking, wrapLines(text, bodyWidth(rc)), "")
}

// viewerHead is the title of the viewer for a tool step: the verb and what the
// step did, and the outcome of the step.
func (b toolLineBlock) viewerHead(RenderContext) (title, outcome string, role Role) {
	r := b.row()
	title = strings.TrimSpace(r.verb + " " + oneLine(r.what))
	if stat := r.statText(); stat != "" {
		title += "  " + stat
	}
	switch b.state {
	case ToolSucceeded:
		return title, oneLine(b.duration), RoleSuccess
	case ToolFailed, ToolRunning, ToolNoted:
		return title, r.right, r.rightRole
	}
	return title, "", RoleFg
}

// viewerBody is the command of the step, an empty row, and the whole output.
func (b toolLineBlock) viewerBody(rc RenderContext) []string {
	rows := b.commandRows(rc, true)
	output := b.out.fullRows(rc)
	if b.state != ToolRunning {
		output = append(output, b.out.truncationRows(rc)...)
	}
	if len(rows) > 0 && len(output) > 0 {
		rows = append(rows, "")
	}
	return append(rows, output...)
}
