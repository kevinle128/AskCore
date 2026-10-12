package tui

import (
	"fmt"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Transcript blocks.
//
// Every block format of the style guide is a Block here. A block holds
// content only. It draws itself for a width and a theme when asked, so a resize
// or a color change draws the same block again. Rows start at column 0 of the
// transcript: the two-column indent and the glyph column are part of each
// block.

const (
	// glyphIndent is the indent before a glyph, and textIndent is where the
	// text of a block starts: indent, glyph and one space. Column 0 of every
	// row is the gutter: it is blank here, and the pane may draw a selection
	// mark in it without any row changing its width.
	glyphIndent = 2
	textIndent  = 4
	// replyIndent is where the text of an assistant reply starts. A reply has
	// no glyph: the step glyph marks tools and thinking.
	replyIndent = 2
)

// SpinnerFrames are the frames of a running tool mark and of the status
// spinner.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerFrame returns the frame for a counter; the counter wraps.
func SpinnerFrame(i int) string {
	n := len(SpinnerFrames)
	return SpinnerFrames[((i%n)+n)%n]
}

// TranscriptView is display state that blocks read each time they draw. A
// block that depends on it reports the value in drawState, so the block pane
// draws the block again when the value changes.
type TranscriptView struct {
	spinner atomic.Uint32
	// nowMs is the time in Unix milliseconds that running blocks measure their
	// elapsed time against. Zero means the clock is not set.
	nowMs atomic.Int64
}

// NewTranscriptView returns a view with no clock.
func NewTranscriptView() *TranscriptView { return &TranscriptView{} }

// SetSpinnerFrame sets the frame that running blocks show. A running block
// reports the frame in drawState, so the pane draws it again at the next
// frame.
func (v *TranscriptView) SetSpinnerFrame(i int) { v.spinner.Store(uint32(max(i, 0))) }

// SetClock sets the time in Unix milliseconds. Running blocks show the whole
// seconds since their start event.
func (v *TranscriptView) SetClock(unixMs int64) { v.nowMs.Store(unixMs) }

func (v *TranscriptView) frame() string {
	if v == nil {
		return SpinnerFrame(0)
	}
	return SpinnerFrame(int(v.spinner.Load()))
}

// elapsedSeconds returns the whole seconds from startMs to the clock, or -1
// when the start or the clock is not known.
func (v *TranscriptView) elapsedSeconds(startMs int64) int {
	if v == nil || startMs <= 0 {
		return -1
	}
	now := v.nowMs.Load()
	if now <= 0 {
		return -1
	}
	return int(max(0, now-startMs) / 1000)
}

// runningState is the draw state of a running block: the spinner frame and the
// whole seconds. It changes when the rows change.
func (v *TranscriptView) runningState(startMs int64) uint64 {
	if v == nil {
		return 1
	}
	frame := uint64(v.spinner.Load())%uint64(len(SpinnerFrames)) + 1
	return frame | uint64(max(v.elapsedSeconds(startMs), -1)+1)<<8
}

// ThinkingNotice is the text of the status notice after the thinking steps are
// expanded or collapsed all at once.
func ThinkingNotice(expanded bool) string {
	if expanded {
		return "Thinking blocks: expanded · ctrl+t to collapse"
	}
	return "Thinking blocks: collapsed · ctrl+t to expand"
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

// promptEdge is the number of rows of a prompt that show before it folds.
const promptEdge = 3

// wrapped returns the rows of the text for a width. The first row is narrower
// by the send time.
func (b userPromptBlock) wrapped(width int) []string {
	// Columns: indent, glyph and space on the left; two spaces at the right.
	avail := max(1, width-textIndent-promptRightPad)
	first := avail
	if b.at != "" {
		first = max(1, avail-displayWidth(b.at)-1)
	}
	rows := wrapHanging(b.text, first, avail)
	if len(rows) == 0 {
		rows = []string{""}
	}
	return rows
}

const promptRightPad = 2

// A prompt of more than promptEdge rows folds. It has two forms, so its lowest
// mode is Truncated: a Collapsed prompt would draw the same rows and a fold key
// would seem to do nothing.
func (b userPromptBlock) foldable(width int) bool {
	return len(b.wrapped(width)) > promptEdge
}
func (b userPromptBlock) defaultFold() FoldMode  { return FoldTruncated }
func (b userPromptBlock) collapseFold() FoldMode { return FoldTruncated }
func (b userPromptBlock) foldGroup() foldGroup   { return groupNone }

func (b userPromptBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	rows := b.wrapped(rc.Width)
	hidden := 0
	if rc.Fold != FoldExpanded && len(rows) > promptEdge {
		hidden = len(rows) - promptEdge
		rows = rows[:promptEdge]
	}

	band := th.Style(RolePanel)
	text := th.onBackground(RoleFg, RolePanel).Bold(true)
	glyph := th.onBackground(RoleAccent, RolePanel).Bold(true)
	dim := th.onBackground(RoleDim, RolePanel)
	muted := th.onBackground(RoleMuted, RolePanel)
	fill := func(n int) string { return band.Render(strings.Repeat(" ", max(0, n))) }

	out := make([]string, 0, len(rows)+2)
	for i, row := range rows {
		var line string
		used := textIndent + displayWidth(row)
		if i == 0 {
			line = fill(glyphIndent) + glyph.Render(GlyphUser) + fill(1) + text.Render(row)
			if b.at != "" && rc.Width-promptRightPad-used-displayWidth(b.at) >= 1 {
				gap := rc.Width - promptRightPad - used - displayWidth(b.at)
				line += fill(gap) + dim.Render(b.at)
				used += gap + displayWidth(b.at)
			}
		} else {
			line = fill(textIndent) + text.Render(row)
		}
		out = append(out, line+fill(rc.Width-used))
	}
	if hidden > 0 {
		word := "lines"
		if hidden == 1 {
			word = "line"
		}
		hint := truncateTo(fmt.Sprintf("… +%d %s · e to expand", hidden, word), max(1, rc.Width-textIndent))
		out = append(out, fill(textIndent)+muted.Render(hint)+fill(rc.Width-textIndent-displayWidth(hint)))
	}
	return append(out, "")
}

// --- assistant text ---

// A long reply is shown by several blocks, one after the other, so that a block
// that is final stops changing while the rest still streams. A block that
// continues a reply starts with an empty row. The first block of a reply starts
// with an empty row only when a step comes right before it. Together the rows
// are the rows of one block that holds the whole text.

type assistantTextBlock struct {
	text  string
	md    *markdownStream
	first bool
	// lead is true when the first block follows a step and needs an empty row
	// above it.
	lead bool
}

// NewAssistantTextBlock returns a reply block that draws its text as markdown.
func NewAssistantTextBlock(text string) Block {
	return newAssistantTextBlock(text, newMarkdownStream(), true, false)
}

// newAssistantTextBlock returns a block for a part of a reply. Blocks that
// stand for the same part while it streams share one stream, which caches the
// parts of the text that are drawn.
func newAssistantTextBlock(text string, stream *markdownStream, first, lead bool) Block {
	return assistantTextBlock{text: text, md: stream, first: first, lead: lead}
}

func (b assistantTextBlock) Lines(rc RenderContext) []string {
	th := rc.Theme
	rows, _ := b.md.render(b.text, max(1, rc.Width-replyIndent), th)
	if len(rows) == 0 {
		return nil
	}
	var out []string
	if !b.first || b.lead {
		out = append(out, "")
	}
	pad := strings.Repeat(" ", replyIndent)
	for _, row := range rows {
		switch {
		case row.panel:
			out = append(out, " "+panelPad(th, replyIndent-1)+row.text)
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
	pad := strings.Repeat(" ", replyIndent)
	return []string{pad + rc.Theme.Render(RoleDim, truncateTo(oneLine(b.text), max(1, rc.Width-replyIndent))), ""}
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
