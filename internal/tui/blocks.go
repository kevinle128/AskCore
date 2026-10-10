package tui

import (
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Transcript blocks.
//
// Every committed format of the style guide is a Block here. A block holds
// content only. It draws itself for a width and a theme when asked, so a resize
// or a color change draws the same block again. Rows start at column 0 of the
// transcript: the two-column indent and the glyph column are part of each
// block.

const (
	// glyphIndent is the indent before a glyph, and textIndent is where the
	// text of a block starts: indent, glyph and one space.
	glyphIndent = 2
	textIndent  = 4
)

// SpinnerFrames are the frames of a running tool mark and of the status
// spinner.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerFrame returns the frame for a counter; the counter wraps.
func SpinnerFrame(i int) string {
	n := len(SpinnerFrames)
	return SpinnerFrames[((i%n)+n)%n]
}

// TranscriptView is display state that blocks read each time they draw. Blocks
// in the scrollback cannot be replaced, so a setting that changes how they
// look lives here and a rebuild draws them again.
//
// The thinking toggle changes only this state. After a toggle, send RebuildMsg
// (or call Model.Rebuild) so that committed thinking blocks are drawn in their
// new form.
type TranscriptView struct {
	thinkingHidden atomic.Bool
	spinner        atomic.Uint32
}

// NewTranscriptView returns a view with thinking shown.
func NewTranscriptView() *TranscriptView { return &TranscriptView{} }

// ThinkingHidden reports whether thinking blocks show the short form.
func (v *TranscriptView) ThinkingHidden() bool { return v != nil && v.thinkingHidden.Load() }

// SetThinkingHidden sets the form of all thinking blocks.
func (v *TranscriptView) SetThinkingHidden(hidden bool) { v.thinkingHidden.Store(hidden) }

// ToggleThinking switches between the full and the short form of thinking
// blocks and returns the new state.
func (v *TranscriptView) ToggleThinking() (hidden bool) {
	for {
		old := v.thinkingHidden.Load()
		if v.thinkingHidden.CompareAndSwap(old, !old) {
			return !old
		}
	}
}

// SetSpinnerFrame sets the frame that running tool lines show. Live tool lines
// are in the tail, which the next frame draws again; no rebuild is needed.
func (v *TranscriptView) SetSpinnerFrame(i int) { v.spinner.Store(uint32(max(i, 0))) }

func (v *TranscriptView) frame() string {
	if v == nil {
		return SpinnerFrame(0)
	}
	return SpinnerFrame(int(v.spinner.Load()))
}

// ThinkingNotice is the text of the status notice after a toggle.
func ThinkingNotice(hidden bool) string {
	if hidden {
		return "Thinking blocks: hidden · ctrl+t to show"
	}
	return "Thinking blocks: visible · ctrl+t to hide"
}

// onBackground returns the style of a role with the background of another
// role, so that text on a band keeps the band color.
func (t *Theme) onBackground(role, band Role) lipgloss.Style {
	return t.Style(role).Background(t.Style(band).GetBackground())
}

// leadRows places body rows after a glyph: the first row gets the glyph, later
// rows are indented to the text column. An empty body still shows the glyph.
func leadRows(glyph string, body []string) []string {
	lead := strings.Repeat(" ", glyphIndent) + glyph + " "
	pad := strings.Repeat(" ", textIndent)
	if len(body) == 0 {
		return []string{lead}
	}
	out := make([]string, len(body))
	for i, row := range body {
		if i == 0 {
			out[i] = lead + row
			continue
		}
		out[i] = pad + row
	}
	return out
}

// wrapUntrusted wraps text that comes from a model, a tool or another client.
// It first removes every escape sequence, style sequences too: such text must
// not set colors or move the cursor. Blocks style the rows after wrapping.
func wrapUntrusted(s string, width int) []string { return wrapLines(ansi.Strip(s), width) }

// bodyWidth is the width for text after the indent.
func bodyWidth(rc RenderContext) int { return max(1, rc.Width-textIndent) }

// styleRows styles each row with a role.
func styleRows(th *Theme, role Role, rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = th.Render(role, r)
	}
	return out
}

// --- startup header ---

type startupHeader struct {
	version, where, cwd string
	// start is the model line and the notes that follow the header row. The
	// empty row of the header comes after them.
	start modelLineBlock
}

// NewStartupHeader returns the one-line header at the top of the scrollback.
// where is "leader <id>" or "in-process agent".
func NewStartupHeader(version, where, cwd string) Block {
	return startupHeader{version: version, where: where, cwd: cwd}
}

func (b startupHeader) Lines(rc RenderContext) []string {
	rest := "   v" + b.version
	if b.where != "" {
		rest += " · " + b.where
	}
	if b.cwd != "" {
		rest += " · " + b.cwd
	}
	logo := "◢◤ ask"
	room := rc.Width - displayWidth(logo)
	rows := []string{rc.Theme.RenderLogo(logo) + rc.Theme.Render(RoleMuted, truncateTo(rest, max(0, room)))}
	rows = append(rows, b.start.rows(rc)...)
	return append(rows, "")
}

// --- user prompt ---

type userPromptBlock struct{ text, at string }

// NewUserPromptBlock returns the prompt band: a full-width panel with the
// prompt in bold and the send time at the right.
func NewUserPromptBlock(text, at string) Block { return userPromptBlock{text: text, at: at} }

// wrapHanging wraps text so that the first row is first columns wide and later
// rows are rest columns wide.
func wrapHanging(text string, first, rest int) []string {
	first, rest = max(1, first), max(1, rest)
	lines := strings.Split(cleanText(ansi.Strip(text)), "\n")
	var rows []string
	for i, line := range lines {
		if i > 0 || first == rest {
			rows = append(rows, wrapLine(line, rest)...)
			continue
		}
		head := wrapLine(line, first)
		if len(head) == 1 || !strings.HasPrefix(line, head[0]) {
			rows = append(rows, wrapLine(line, rest)...)
			continue
		}
		rows = append(rows, head[0])
		rows = append(rows, wrapLine(strings.TrimLeft(line[len(head[0]):], " "), rest)...)
	}
	return rows
}

func (b userPromptBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	// Columns: indent, glyph and space on the left; two spaces at the right.
	const rightPad = 2
	avail := max(1, rc.Width-textIndent-rightPad)
	first := avail
	if b.at != "" {
		first = max(1, avail-displayWidth(b.at)-1)
	}
	rows := wrapHanging(b.text, first, avail)
	if len(rows) == 0 {
		rows = []string{""}
	}

	band := th.Style(RolePanel)
	text := th.onBackground(RoleFg, RolePanel).Bold(true)
	glyph := th.onBackground(RoleAccent, RolePanel).Bold(true)
	dim := th.onBackground(RoleDim, RolePanel)
	fill := func(n int) string { return band.Render(strings.Repeat(" ", max(0, n))) }

	out := make([]string, 0, len(rows)+1)
	for i, row := range rows {
		var line string
		used := textIndent + displayWidth(row)
		if i == 0 {
			line = fill(glyphIndent) + glyph.Render(GlyphUser) + fill(1) + text.Render(row)
			if b.at != "" && rc.Width-rightPad-used-displayWidth(b.at) >= 1 {
				gap := rc.Width - rightPad - used - displayWidth(b.at)
				line += fill(gap) + dim.Render(b.at)
				used += gap + displayWidth(b.at)
			}
		} else {
			line = fill(textIndent) + text.Render(row)
		}
		out = append(out, line+fill(rc.Width-used))
	}
	return append(out, "")
}

// --- assistant text ---

// A long reply is shown by several blocks, one after the other, so that the
// parts that are final can leave the live tail while the rest still streams.
// The first block has the reply mark. A later block continues the reply: it
// starts with an empty row and has no mark. Together the rows are the rows of
// one block that holds the whole text.

type assistantTextBlock struct {
	text  string
	md    *markdownStream
	first bool
}

// NewAssistantTextBlock returns a reply block that draws its text as markdown.
func NewAssistantTextBlock(text string) Block {
	return newAssistantTextBlock(text, newMarkdownStream(), true)
}

// newAssistantTextBlock returns a block for a part of a reply. Blocks that
// stand for the same part while it streams share one stream, which caches the
// parts of the text that are drawn.
func newAssistantTextBlock(text string, stream *markdownStream, first bool) Block {
	return assistantTextBlock{text: text, md: stream, first: first}
}

func (b assistantTextBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	glyph := th.Render(RoleAssistant, GlyphAssistant)
	rows, _ := b.md.render(b.text, bodyWidth(rc), th)
	if len(rows) == 0 {
		if b.first {
			return leadRows(glyph, nil)
		}
		return nil
	}
	var out []string
	pad := strings.Repeat(" ", textIndent)
	if !b.first {
		out = append(out, "")
	} else if rows[0].panel {
		// A reply that starts with code keeps its mark on a row of its own.
		out = append(out, leadRows(glyph, nil)...)
	}
	for i, row := range rows {
		switch {
		case row.panel:
			out = append(out, panelPad(th, textIndent)+row.text)
		case i == 0 && b.first:
			out = append(out, leadRows(glyph, []string{row.text})...)
		case row.text == "":
			out = append(out, "")
		default:
			out = append(out, pad+row.text)
		}
	}
	return out
}

// panelPad is n columns of the code panel color, or plain spaces without color.
func panelPad(th *Theme, n int) string {
	bg := bgSequence(th, RoleCodePanel)
	if bg == "" {
		return strings.Repeat(" ", n)
	}
	return bg + strings.Repeat(" ", n) + "\x1b[0m"
}

// --- thinking ---

type thinkingBlock struct {
	text        string
	view        *TranscriptView
	first, last bool
}

// NewThinkingBlock returns a thinking block. It reads the form (full text or
// "Thinking...") from the view each time it draws.
func NewThinkingBlock(text string, view *TranscriptView) Block {
	return thinkingBlock{text: text, view: view, first: true, last: true}
}

// newThinkingPart returns a block for a part of a thinking text. The first
// part has the mark and the last part has the empty row below it; a part
// between them has neither. In the short form only the first part shows a row,
// so the short form of a split text is the same two rows as for one block.
func newThinkingPart(text string, view *TranscriptView, first, last bool) Block {
	return thinkingBlock{text: text, view: view, first: first, last: last}
}

const thinkingHiddenText = "Thinking..."

func (b thinkingBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	glyph := th.Render(RoleThinking, GlyphThinking)
	text := strings.TrimSpace(b.text)
	var out []string
	switch {
	case b.view.ThinkingHidden() || (text == "" && b.first && b.last):
		if b.first {
			out = leadRows(glyph, []string{th.Render(RoleThinking, thinkingHiddenText)})
		}
	case text == "":
		// An empty part of a split text has nothing to show.
	default:
		rows := styleRows(th, RoleThinking, wrapUntrusted(text, bodyWidth(rc)))
		if b.first {
			out = leadRows(glyph, rows)
		} else {
			pad := strings.Repeat(" ", textIndent)
			out = append(out, "")
			for _, r := range rows {
				out = append(out, pad+r)
			}
		}
	}
	if b.last {
		out = append(out, "")
	}
	return out
}

// --- tool line ---

// ToolState is the stage of a tool line.
type ToolState int

// Tool line stages.
const (
	ToolRunning ToolState = iota
	ToolSucceeded
	ToolFailed
)

type toolLineBlock struct {
	name, args string
	state      ToolState
	duration   string // "0.1s", set when the tool finished
	reason     string // set when the tool failed
	view       *TranscriptView
}

// NewToolLineBlock returns a tool line. A running line shows "running" and the
// spinner frame of the view; a finished line shows the duration or the reason
// of the failure at the right.
func NewToolLineBlock(name, args string, state ToolState, duration, reason string, view *TranscriptView) Block {
	return toolLineBlock{name: name, args: args, state: state, duration: duration, reason: reason, view: view}
}

func (b toolLineBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	var mark, status string
	var statusRole Role
	switch b.state {
	case ToolRunning:
		mark, statusRole = GlyphToolRun, RoleWarning
		status = "running " + b.view.frame()
	case ToolSucceeded:
		mark, statusRole = GlyphToolDone, RoleSuccess
		status = GlyphOK
		if b.duration != "" {
			status += " " + b.duration
		}
	default:
		mark, statusRole = GlyphToolDone, RoleError
		reason := oneLine(b.reason)
		if reason == "" {
			reason = "failed"
		}
		status = GlyphFail + " " + reason
	}
	const rightPad = 1
	left := strings.Repeat(" ", glyphIndent) + th.Render(RoleTool, mark) + " "
	leftW := glyphIndent + 2

	// Keep the status whole when it fits. A long failure reason is cut first,
	// then the arguments, then the name.
	statusW := displayWidth(status)
	if limit := rc.Width - leftW - 2 - rightPad - displayWidth(b.name); statusW > limit && b.state == ToolFailed {
		status = truncateTo(status, max(3, limit))
		statusW = displayWidth(status)
	}
	room := rc.Width - leftW - statusW - rightPad - 2
	name := truncateTo(oneLine(b.name), max(1, room))
	body := th.Style(RoleTool).Bold(true).Render(name)
	used := leftW + displayWidth(name)
	if args := oneLine(b.args); args != "" {
		if avail := rc.Width - used - 2 - statusW - rightPad - 2; avail >= 2 {
			args = truncateTo(args, avail)
			body += "  " + th.Render(RoleMuted, args)
			used += 2 + displayWidth(args)
		}
	}
	gap := rc.Width - used - statusW - rightPad
	line := left + body
	if gap >= 1 {
		line += strings.Repeat(" ", gap)
	} else {
		line += " "
	}
	line += th.Render(statusRole, status) + strings.Repeat(" ", rightPad)
	if b.state == ToolRunning {
		return []string{line}
	}
	return []string{line, ""}
}

// --- notes ---

type noteBlock struct {
	glyph     string
	glyphRole Role
	textRole  Role
	text      string
	next      string
}

func (b noteBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	body := styleRows(th, b.textRole, wrapUntrusted(b.text, bodyWidth(rc)))
	out := leadRows(th.Render(b.glyphRole, b.glyph), body)
	if b.next != "" {
		pad := strings.Repeat(" ", textIndent)
		for _, row := range styleRows(th, RoleMuted, wrapUntrusted(b.next, bodyWidth(rc))) {
			out = append(out, pad+row)
		}
	}
	return append(out, "")
}

// NewRetryNote returns the note of a retry: "↻ <text>".
func NewRetryNote(text string) Block {
	return noteBlock{glyph: GlyphRetry, glyphRole: RoleWarning, textRole: RoleFg, text: text}
}

// NewReconnectNote returns the note after a reconnect: "↻ <text>".
func NewReconnectNote(text string) Block {
	return noteBlock{glyph: GlyphRetry, glyphRole: RoleSuccess, textRole: RoleFg, text: text}
}

// NewErrorBlock returns an error block: "✗ <headline>" and, when next is not
// empty, a muted row that tells the user what to do.
func NewErrorBlock(headline, next string) Block {
	return noteBlock{glyph: GlyphFail, glyphRole: RoleError, textRole: RoleError, text: headline, next: next}
}

// NewSuccessNote returns a result note: "✓ <text>" in the success role and,
// for each row of more, a muted row at the text column.
func NewSuccessNote(text string, more ...string) Block {
	return successNote{text: text, more: more}
}

type successNote struct {
	text string
	more []string
}

func (b successNote) Lines(rc RenderContext) []string {
	th := rc.Theme
	out := leadRows(th.Render(RoleSuccess, GlyphOK), styleRows(th, RoleSuccess, wrapUntrusted(b.text, bodyWidth(rc))))
	pad := strings.Repeat(" ", textIndent)
	for _, m := range b.more {
		for _, row := range styleRows(th, RoleMuted, wrapUntrusted(m, bodyWidth(rc))) {
			out = append(out, pad+row)
		}
	}
	return append(out, "")
}

// NewCancelMarker returns the marker for a cancelled run: "⊘ <text>".
func NewCancelMarker(text string) Block {
	return noteBlock{glyph: GlyphCancelled, glyphRole: RoleMuted, textRole: RoleMuted, text: text}
}

// NewDetachNote returns the note after a detach: "⏏ <text>" and, when next is
// not empty, a muted row below it.
func NewDetachNote(text, next string) Block {
	return noteBlock{glyph: GlyphDetached, glyphRole: RoleMuted, textRole: RoleMuted, text: text, next: next}
}

// ErrorHeadline writes "Error (<kind>): <message>".
func ErrorHeadline(kind, message string) string {
	if kind == "" {
		kind = "error"
	}
	return "Error (" + kind + "): " + message
}

// --- meta line, divider, spacer ---

type metaLineBlock struct{ text string }

// NewMetaLineBlock returns the dim line after a finished reply. text is
// already joined (see MetaLineText).
func NewMetaLineBlock(text string) Block { return metaLineBlock{text: text} }

func (b metaLineBlock) Lines(rc RenderContext) []string {
	pad := strings.Repeat(" ", textIndent)
	return []string{pad + rc.Theme.Render(RoleDim, truncateTo(oneLine(b.text), bodyWidth(rc))), ""}
}

type dividerBlock struct{ text string }

// NewDividerBlock returns a divider row such as "── session 8d07 ──".
func NewDividerBlock(text string) Block { return dividerBlock{text: text} }

func (b dividerBlock) Lines(rc RenderContext) []string {
	label := "── " + oneLine(b.text) + " ──"
	return []string{strings.Repeat(" ", glyphIndent) + rc.Theme.Render(RoleDim, truncateTo(label, max(1, rc.Width-glyphIndent))), ""}
}

type spacerBlock struct{}

// NewSpacerBlock returns one empty row.
func NewSpacerBlock() Block { return spacerBlock{} }

func (spacerBlock) Lines(RenderContext) []string { return []string{""} }
