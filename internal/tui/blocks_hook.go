package tui

import (
	"strconv"
	"strings"
	"time"
)

// Hook blocks.
//
// A hook block is the record of one hook run. It is a step like a tool: one
// header row with the event, the hook and what it was asked about, and the
// result at the right edge. A muted second row says what the hook does, or
// shows its command when the hook has no description. The same block opens in
// two more forms, chosen by RenderContext.Fold. Truncated adds the first and
// the last rows of the output. Expanded adds the detail fields and all of the
// output, with nothing cut.
//
// The block is built from a HookRun, a plain struct. The agent has no hook
// runtime yet, so no translator path makes this block; tests build it from
// fixtures. When the runtime sends a hook event, the event carries the id of the
// tool call that it belongs to, and the transcript puts the hook block before
// the block of that tool call: the hook decides whether the call runs.
//
// The glyph is two columns wide. The header row reserves both columns, so the
// expand indicator, which is one column, is padded to the same width and the
// text does not move.

// Hook decisions.
const (
	HookAllowed = "allowed"
	HookBlocked = "blocked"
	HookError   = "error"
	HookTimeout = "timeout"
)

// Hook scopes.
const (
	HookScopeGlobal  = "global"
	HookScopeProject = "project"
)

// HookRun holds what the block shows of one hook run.
type HookRun struct {
	// ID is the hook run id and CallID is the id of the tool call that the run
	// belongs to.
	ID, CallID string
	// Event is the enum name of the hook event, for example "PreToolUse".
	Event string
	// Name and Scope tell which hook ran. Scope is HookScopeGlobal for the
	// config in the home directory and HookScopeProject for the repo config.
	Name, Scope string
	// Description says what the hook does. It may be empty.
	Description string
	// Matcher is the tools and file globs that the hook matches.
	Matcher string
	// Source is the config file that holds the hook.
	Source string
	// Command is what the hook runs, and Timeout is how long it may run.
	Command string
	Timeout time.Duration
	// Target is the tool and its short arguments that the hook was asked about.
	Target string
	// Running is true until the run ends. StartMs is the time of its start in
	// Unix milliseconds, for the seconds of a running hook; zero when unknown.
	Running bool
	StartMs int64
	// Decision is one of the Hook decisions once the run has ended.
	Decision string
	// ExitCode is nil when the run has no exit code, for example a timeout.
	ExitCode *int
	Duration time.Duration
	// Reason is why the hook blocked the call.
	Reason string
	// Output is the stdout and stderr of the hook.
	Output string
}

// hookLabelWidth is the column width of a detail field name and the gap after
// it: the longest name, "Matcher" or "Command", and two spaces.
const hookLabelWidth = 9

// minHookTarget is the least number of columns that a cut target keeps. A
// narrower room drops the target.
const minHookTarget = 3

type hookBlock struct {
	run  HookRun
	out  toolOutput
	view *TranscriptView
}

// NewHookBlock returns the block of a hook run. A running hook shows the
// spinner frame and the seconds of the view.
func NewHookBlock(run HookRun, view *TranscriptView) Block {
	return hookBlock{run: run, out: toolOutput{text: run.Output, lines: outputLines(run.Output)}, view: view}
}

func (b hookBlock) drawState() uint64 {
	if !b.run.Running {
		return 0
	}
	return b.view.runningState(b.run.StartMs)
}

func (b hookBlock) foldable(int) bool      { return true }
func (b hookBlock) defaultFold() FoldMode  { return FoldCollapsed }
func (b hookBlock) collapseFold() FoldMode { return FoldCollapsed }

// foldGroup puts a hook with the tool steps: the fold-all commands move both.
func (b hookBlock) foldGroup() foldGroup { return groupTool }

// verb is the event and the word "hook", the bold part of the header row.
func (b hookBlock) verb() string { return oneLine(b.run.Event) + " hook" }

// owner is "(<scope>/<name>)".
func (b hookBlock) owner() string {
	return "(" + oneLine(b.run.Scope) + "/" + oneLine(b.run.Name) + ")"
}

// what is the description of the header row: the owner and then the target.
func (b hookBlock) what() string {
	if t := oneLine(b.run.Target); t != "" {
		return b.owner() + " · " + t
	}
	return b.owner()
}

// fitWhat cuts the description of the header row for a room. The target is cut
// first and goes when less than minHookTarget columns are left for it. The
// owner is cut only after that.
func (b hookBlock) fitWhat(room int) string {
	if full := b.what(); displayWidth(full) <= room {
		return full
	}
	owner := b.owner()
	if t := oneLine(b.run.Target); t != "" {
		lead := owner + " · "
		if left := room - displayWidth(lead); left >= minHookTarget {
			return lead + truncateTo(t, left)
		}
	}
	return truncateTo(owner, room)
}

// result is the right column and its role: the outcome of the run, or the
// spinner and the seconds while it runs.
func (b hookBlock) result() (text string, role Role) {
	if b.run.Running {
		text = b.view.frame()
		if secs := b.view.elapsedSeconds(b.run.StartMs); secs >= 0 {
			text += " " + runningText(secs)
		}
		return text, RoleWarning
	}
	text = oneLine(b.run.Decision) + " " + formatDuration(b.run.Duration)
	if b.run.Decision == HookAllowed {
		return GlyphOK + " " + text, RoleSuccess
	}
	return GlyphFail + " " + text, RoleError
}

func (b hookBlock) row(rc RenderContext) stepRow {
	right, role := b.result()
	return stepRow{
		glyph: GlyphHook, glyphRole: RoleHook,
		indicator: indicatorFor(rc, FoldCollapsed),
		verb:      b.verb(), what: b.what(), fitWhat: b.fitWhat,
		right: right, rightRole: role,
	}
}

// aboutRow is the muted row under the header: what the hook does, or its
// command when it has no description. It is one row, cut with an ellipsis.
// It is nil when the hook has neither.
func (b hookBlock) aboutRow(rc RenderContext) []string {
	text := oneLine(b.run.Description)
	if text == "" {
		text = oneLine(b.run.Command)
	}
	if text == "" {
		return nil
	}
	return styledPad(rc.Theme, RoleMuted, []string{truncateTo(text, bodyWidth(rc))}, "")
}

// field is one name and value pair of the detail rows.
type hookField struct {
	name, value string
	role        Role
}

// fields lists the detail fields in order. A field without a value is left
// out, except Event, Command and Result, which every run has.
func (b hookBlock) fields() []hookField {
	r := b.run
	f := []hookField{{name: "Event", value: r.Event}}
	add := func(name, value string, role Role) {
		if strings.TrimSpace(value) != "" {
			f = append(f, hookField{name: name, value: value, role: role})
		}
	}
	add("Matcher", r.Matcher, RoleFg)
	add("Does", r.Description, RoleFg)
	source := r.Source
	if source != "" && r.Scope != "" {
		source += " (" + r.Scope + ")"
	}
	add("Source", source, RoleFg)
	command := r.Command
	if r.Timeout > 0 {
		command += " · timeout " + formatTimeout(r.Timeout)
	}
	f = append(f, hookField{name: "Command", value: command, role: RoleFg})
	f = append(f, hookField{name: "Result", value: b.resultField(), role: RoleFg})
	if r.Decision == HookBlocked {
		add("Reason", r.Reason, RoleError)
	}
	return f
}

// resultField is "<decision> · exit <n> · <duration>"; parts that the run does
// not have are left out.
func (b hookBlock) resultField() string {
	if b.run.Running {
		return "running"
	}
	parts := []string{oneLine(b.run.Decision)}
	if b.run.ExitCode != nil {
		parts = append(parts, "exit "+strconv.Itoa(*b.run.ExitCode))
	}
	parts = append(parts, formatDuration(b.run.Duration))
	return strings.Join(parts, " · ")
}

// formatTimeout writes a timeout in whole seconds when it is one, so that
// "5s" does not show as "5.0s".
func formatTimeout(d time.Duration) string {
	if d%time.Second == 0 {
		return d.String()
	}
	return formatDuration(d)
}

// detailRows draws the detail fields. A value wraps under itself and nothing
// is cut.
func (b hookBlock) detailRows(rc RenderContext) []string {
	th := rc.Theme
	pad := strings.Repeat(" ", textIndent)
	valueW := max(1, rc.Width-textIndent-hookLabelWidth)
	var rows []string
	for _, f := range b.fields() {
		label := th.Render(RoleMuted, padTo(f.name, hookLabelWidth))
		for i, part := range wrapUntrusted(f.value, valueW) {
			lead := strings.Repeat(" ", hookLabelWidth)
			if i == 0 {
				lead = label
			}
			rows = append(rows, pad+lead+th.Render(f.role, part))
		}
	}
	return rows
}

// outputRows is the output rule and all output rows. A run with no output has
// neither.
func (b hookBlock) outputRows(rc RenderContext) []string {
	rows := b.out.fullRows(rc)
	if len(rows) == 0 {
		return nil
	}
	rule := strings.Repeat(" ", textIndent) + rc.Theme.Render(RoleDim, "── output ──")
	return append([]string{rule}, rows...)
}

func (b hookBlock) Lines(rc RenderContext) []string {
	head, _ := b.row(rc).render(rc)
	rows := append([]string{head}, b.aboutRow(rc)...)
	switch rc.Fold {
	case FoldCollapsed:
		return rows
	case FoldTruncated:
		return append(rows, b.out.truncatedRows(rc)...)
	}
	rows = append(rows, b.detailRows(rc)...)
	return append(rows, b.outputRows(rc)...)
}

// viewerHead is the title of the viewer: the header row without its glyph, and
// the result.
func (b hookBlock) viewerHead(RenderContext) (title, outcome string, role Role) {
	outcome, role = b.result()
	return strings.TrimSpace(b.verb() + " " + b.what()), outcome, role
}

// viewerBody is the detail fields and the output. The description row is not
// repeated: the Does field holds it.
func (b hookBlock) viewerBody(rc RenderContext) []string {
	return append(b.detailRows(rc), b.outputRows(rc)...)
}
