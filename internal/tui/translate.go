package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Event translation.
//
// A Translator turns the agent events of one followed session, and the
// snapshot of its history, into block messages for the transcript. It is a
// state machine with no I/O: the session view calls it from Update and applies
// the returned messages in order. One Translator serves the whole TUI; call
// Reset when the view moves to another session or when a resync replaces the
// history.
//
// Rules that keep the transcript consistent:
//
//   - A block that is still changing is added unstable and replaced; it is
//     finished at its end event. A block is never replaced after it is
//     finished, because a finished block is final.
//   - The user prompt comes from the user message event. It is the same for
//     live events, a replayed snapshot and an observer, so the session view
//     must not add a prompt block of its own.
//   - Tool lines come from tool_execution_* events, not from the tool calls in
//     the assistant message.
//   - Errors and cancel markers are written when the run ends. A cancel marker
//     waits for agent_settled, so the partial reply is final before it.
//   - agent_settled finishes everything that is still open, so that no block
//     holds the frontier after a run.

// TranslatorOptions configure a Translator.
type TranslatorOptions struct {
	// Location is the time zone of the send times. Nil means local time.
	Location *time.Location
	// View is the display state that blocks share. Nil makes a new one.
	View *TranscriptView
}

// Translator maps agent events to block messages. It is not safe for
// concurrent use; call it only from Update.
type Translator struct {
	loc  *time.Location
	view *TranscriptView
	seq  uint64

	reply *replyState
	tools map[string]*toolState
	run   runState
	// jobs are the background jobs that run, in the order they started. A
	// reset keeps them: a resync replaces the history, not the jobs.
	jobs []RunningJob
	// lastReply is the text of the last finished assistant message that had
	// text, for /copy.
	lastReply string
	// afterStep is true while the last block that the translator added is a
	// tool or thinking step. A reply that follows a step starts with an empty
	// row.
	afterStep bool
}

// RunningJob is a background job that the session view knows to run.
type RunningJob struct {
	ID    string
	Label string
}

// NewTranslator returns a Translator with thinking shown.
func NewTranslator(o TranslatorOptions) *Translator {
	t := &Translator{loc: o.Location, view: o.View}
	if t.loc == nil {
		t.loc = time.Local
	}
	if t.view == nil {
		t.view = NewTranscriptView()
	}
	t.tools = map[string]*toolState{}
	return t
}

// View returns the display state of the blocks. Use it to set the spinner
// frame of running tool lines.
func (t *Translator) View() *TranscriptView { return t.view }

// Reset forgets the state of the open run, message and tools. Blocks that
// were already sent stay in the transcript. Block ids stay unique across a
// reset.
func (t *Translator) Reset() {
	t.lastReply = ""
	t.afterStep = false
	t.reply = nil
	t.tools = map[string]*toolState{}
	t.run = runState{}
}

// RunningJobs returns the background jobs that run, in the order they started.
func (t *Translator) RunningJobs() []RunningJob { return append([]RunningJob(nil), t.jobs...) }

// ForgetJobs forgets the running jobs. Call it when the view follows another session.
func (t *Translator) ForgetJobs() { t.jobs = nil }

// Detach freezes the last observed content before the view follows another
// session. It does not report a tool outcome, cancellation, or a settled run.
func (t *Translator) Detach() []tea.Msg {
	var out []tea.Msg
	t.closeReply(&out)
	for _, st := range t.tools {
		emitFinish(&out, st.id)
	}
	t.Reset()
	return out
}

// partState is one text or thinking block of an assistant message. A long
// part is shown by several transcript blocks: the closed paragraphs are
// finished one by one while the rest streams. frozen is the number of bytes of
// the cleaned text that finished blocks show, and cur is the block that is
// still open.
type partState struct {
	thinking bool
	raw      string
	frozen   int
	segments int
	cur      string
	md       *markdownStream
	done     bool
	// shown is true when the open block is in the transcript. A thinking part
	// is added with its first text, so an empty reasoning summary draws nothing.
	shown bool
	// lead is true when the part follows a step and needs an empty row above.
	lead bool
	// startTS and lastTS are the times of the first and the last event of the
	// part, in milliseconds. They give the duration of a thinking.
	startTS, lastTS int64
	// curText is the text that the open block holds.
	curText string
}

// touch notes the time of an event of the part.
func (p *partState) touch(ts int64) {
	if ts <= 0 {
		return
	}
	if p.startTS == 0 {
		p.startTS = ts
	}
	p.lastTS = max(p.lastTS, ts)
}

type replyState struct {
	startTS int64
	model   string
	parts   map[int]*partState
	order   []int
}

type toolState struct {
	id      string
	order   uint64
	name    string
	args    string
	raw     json.RawMessage
	startTS int64
	// out is the latest partial output of the tool.
	out toolOutput
}

type errorInfo struct{ text, provider string }

type runState struct {
	cancelled  bool
	cause      string
	hadReply   bool
	lastErr    *errorInfo
	finalError string
	errorShown bool
}

func (t *Translator) nextID(prefix string) string {
	t.seq++
	return fmt.Sprintf("%s%d", prefix, t.seq)
}

func emitAppend(out *[]tea.Msg, id string, b Block, stable bool) {
	*out = append(*out, AppendBlockMsg{ID: id, Block: b, Stable: stable})
}

func emitReplace(out *[]tea.Msg, id string, b Block) {
	*out = append(*out, ReplaceBlockMsg{ID: id, Block: b})
}

func emitFinish(out *[]tea.Msg, id string) { *out = append(*out, FinishBlockMsg{ID: id}) }

// Event translates one agent event. It returns the block messages to apply in
// order, or nil when the event does not change the transcript.
func (t *Translator) Event(ev protocol.Event) []tea.Msg {
	var out []tea.Msg
	switch e := ev.(type) {
	case *protocol.MessageStart:
		if m, ok := assistantOf(e.Message); ok {
			t.closeReply(&out)
			t.reply = &replyState{startTS: e.TS, model: m.Model, parts: map[int]*partState{}}
		}
	case *protocol.MessageUpdate:
		t.onUpdate(e, &out)
	case *protocol.MessageEnd:
		switch m := e.Message.(type) {
		case protocol.UserMessage:
			t.userPrompt(m, &out)
		case *protocol.UserMessage:
			t.userPrompt(*m, &out)
		default:
			if am, ok := assistantOf(e.Message); ok {
				t.onAssistantEnd(e, am, &out)
			}
		}
	case *protocol.ToolExecutionStart:
		t.onToolStart(e, &out)
	case *protocol.ToolExecutionUpdate:
		t.onToolUpdate(e, &out)
	case *protocol.ToolExecutionEnd:
		t.onToolEnd(e, &out)
	case *protocol.AutoRetryStart:
		t.afterStep = false
		emitAppend(&out, t.nextID("n"), NewRetryNote(retryText(e)), true)
	case *protocol.AutoRetryEnd:
		if !e.Success {
			t.run.finalError = oneLine(e.FinalError)
		}
	case *protocol.CycleEnd:
		t.onCycleEnd(e, &out)
	case *protocol.AgentSettled:
		t.onSettled(&out)
	}
	return out
}

func assistantOf(m protocol.Message) (protocol.AssistantMessage, bool) {
	switch v := m.(type) {
	case protocol.AssistantMessage:
		return v, true
	case *protocol.AssistantMessage:
		if v != nil {
			return *v, true
		}
	}
	return protocol.AssistantMessage{}, false
}

func userOf(m protocol.Message) (protocol.UserMessage, bool) {
	switch v := m.(type) {
	case protocol.UserMessage:
		return v, true
	case *protocol.UserMessage:
		if v != nil {
			return *v, true
		}
	}
	return protocol.UserMessage{}, false
}

func toolResultOf(m protocol.Message) (protocol.ToolResultMessage, bool) {
	switch v := m.(type) {
	case protocol.ToolResultMessage:
		return v, true
	case *protocol.ToolResultMessage:
		if v != nil {
			return *v, true
		}
	}
	return protocol.ToolResultMessage{}, false
}

// --- user prompt ---

func (t *Translator) userPrompt(m protocol.UserMessage, out *[]tea.Msg) {
	if body, ok := protocol.SystemReminderBody(m); ok {
		t.reminder(body, out)
		return
	}
	t.afterStep = false
	emitAppend(out, t.nextID("u"), NewUserPromptBlock(userText(m), clockTime(m.Timestamp, t.loc)), true)
}

// userText joins the blocks of a user message. An image shows as a marker.
func userText(m protocol.UserMessage) string {
	var parts []string
	for _, blk := range m.Content {
		switch b := blk.(type) {
		case protocol.Text:
			parts = append(parts, b.Text)
		case protocol.Image:
			parts = append(parts, "[image]")
		}
	}
	return strings.Join(parts, "\n")
}

// --- assistant message ---

func (t *Translator) ensureReply(ts int64) *replyState {
	if t.reply == nil {
		t.reply = &replyState{startTS: ts, parts: map[int]*partState{}}
	}
	return t.reply
}

// segBlock makes the block for a part of a message. A thinking part is always
// one block, so that the user folds it as a whole.
func (t *Translator) segBlock(p *partState, text string) Block {
	if p.thinking {
		known := p.startTS > 0 && p.lastTS >= p.startTS
		return newThinkingBlock(text, t.view, !p.done, p.startTS, p.lastTS-p.startTS, known)
	}
	return newAssistantTextBlock(text, p.md, p.segments == 0, p.lead)
}

// part returns the part for a content index. A missing part is added as an
// unstable block, so that a follower that joined late can still show the rest
// of the message.
func (t *Translator) part(r *replyState, index int, thinking bool, out *[]tea.Msg) *partState {
	if p, ok := r.parts[index]; ok {
		return p
	}
	p := &partState{thinking: thinking, cur: t.nextID("a")}
	if !thinking {
		p.md = newMarkdownStream()
	}
	r.parts[index] = p
	r.order = append(r.order, index)
	if !thinking {
		p.shown = true
		p.lead, t.afterStep = t.afterStep, false
		emitAppend(out, p.cur, t.segBlock(p, ""), false)
	}
	return p
}

// setText sets the text of an open part. For a reply, paragraphs that cannot
// change any more leave the open block and become finished blocks. A thinking
// part stays one block.
func (t *Translator) setText(p *partState, raw string, out *[]tea.Msg) {
	if p.done || raw == p.raw {
		return
	}
	p.raw = raw
	text := sanitizeMarkdown(raw)
	p.frozen = min(p.frozen, len(text))
	rest := text[p.frozen:]
	if !p.shown {
		if strings.TrimSpace(rest) == "" {
			return
		}
		p.shown = true
		t.afterStep = true
		p.curText = rest
		emitAppend(out, p.cur, t.segBlock(p, rest), false)
		return
	}
	for !p.thinking {
		cut := closedCut(rest)
		if cut <= 0 {
			break
		}
		emitReplace(out, p.cur, t.segBlock(p, rest[:cut]))
		emitFinish(out, p.cur)
		skip := cut + leadingBlankBytes(rest[cut:])
		p.frozen += skip
		rest = rest[skip:]
		p.segments++
		p.cur = t.nextID("a")
		p.md = newMarkdownStream()
		emitAppend(out, p.cur, t.segBlock(p, ""), false)
	}
	p.curText = rest
	emitReplace(out, p.cur, t.segBlock(p, rest))
}

// finishPart ends a part. A thinking block is drawn once more, because it
// changes from its streaming form to its final form with its duration.
func (t *Translator) finishPart(p *partState, out *[]tea.Msg) {
	if p.done {
		return
	}
	p.done = true
	if p.shown {
		if p.thinking {
			emitReplace(out, p.cur, t.segBlock(p, p.curText))
		}
		emitFinish(out, p.cur)
	}
}

func (t *Translator) onUpdate(e *protocol.MessageUpdate, out *[]tea.Msg) {
	r := t.ensureReply(e.TS)
	// partAt returns the part of an event and notes the time of the event.
	partAt := func(index int, thinking bool) *partState {
		p := t.part(r, index, thinking, out)
		p.touch(e.TS)
		return p
	}
	switch ev := e.AssistantMessageEvent.(type) {
	case protocol.TextStartEvent:
		p := partAt(ev.ContentIndex, false)
		t.setText(p, ev.Content.Text, out)
	case protocol.TextDeltaEvent:
		p := partAt(ev.ContentIndex, false)
		t.setText(p, p.raw+ev.Delta, out)
	case protocol.TextEndEvent:
		p := partAt(ev.ContentIndex, false)
		t.setText(p, ev.Content, out)
		t.finishPart(p, out)
	case protocol.ThinkingStartEvent:
		p := partAt(ev.ContentIndex, true)
		t.setText(p, ev.Content.Thinking, out)
	case protocol.ThinkingDeltaEvent:
		p := partAt(ev.ContentIndex, true)
		t.setText(p, p.raw+ev.Delta, out)
	case protocol.ThinkingEndEvent:
		p := partAt(ev.ContentIndex, true)
		t.setText(p, ev.Content, out)
		t.finishPart(p, out)
	}
}

// closeReply finishes the parts of the open message that did not get an end
// event, and forgets the message.
func (t *Translator) closeReply(out *[]tea.Msg) {
	if r := t.reply; r != nil {
		for _, i := range r.order {
			t.finishPart(r.parts[i], out)
		}
	}
	t.reply = nil
}

func (t *Translator) onAssistantEnd(e *protocol.MessageEnd, am protocol.AssistantMessage, out *[]tea.Msg) {
	t.noteReply(am)
	r := t.ensureReply(e.TS)
	visible, lastIsText := false, false
	for i, blk := range am.Content {
		var (
			text     string
			thinking bool
		)
		switch b := blk.(type) {
		case protocol.Text:
			text = b.Text
		case protocol.Thinking:
			text, thinking = b.Thinking, true
		default:
			continue
		}
		p, exists := r.parts[i]
		if !exists {
			if strings.TrimSpace(text) == "" && !thinking {
				continue
			}
			p = t.part(r, i, thinking, out)
		}
		// The end event holds the final text. A part that is already final
		// keeps the text of its own end event: it is final.
		t.setText(p, text, out)
		t.finishPart(p, out)
		if p.shown {
			visible = true
			lastIsText = !thinking
		}
	}
	t.closeReply(out)
	if visible {
		t.run.hadReply = true
	}

	switch am.StopReason {
	case protocol.StopError:
		msg := ""
		if am.ErrorMessage != nil {
			msg = *am.ErrorMessage
		}
		t.run.lastErr = &errorInfo{text: msg, provider: am.Provider}
		if lastIsText {
			t.afterStep = false
			emitAppend(out, t.nextID("s"), NewSpacerBlock(), true)
		}
	case protocol.StopAborted:
		// The cancel marker follows the partial reply without an empty row.
	default:
		if visible {
			if text := metaText(am, e.TS, r.startTS); text != "" {
				t.afterStep = false
				emitAppend(out, t.nextID("m"), NewMetaLineBlock(text), true)
			}
		}
	}
}

// metaText writes the line after a reply: the model, the time the reply took,
// the tokens and the cost. It is empty when nothing but the model is known.
func metaText(am protocol.AssistantMessage, endTS, startTS int64) string {
	var parts []string
	if am.Model != "" {
		parts = append(parts, am.Model)
	}
	known := false
	if startTS > 0 && endTS > startTS {
		parts = append(parts, formatDuration(time.Duration(endTS-startTS)*time.Millisecond))
		known = true
	}
	if am.Usage.Input+am.Usage.Output > 0 {
		parts = append(parts, "↑"+formatTokens(am.Usage.Input)+" ↓"+formatTokens(am.Usage.Output))
		known = true
	}
	if am.Usage.Cost.Total > 0 {
		parts = append(parts, formatCost(am.Usage.Cost.Total))
		known = true
	}
	if !known {
		return ""
	}
	return strings.Join(parts, " · ")
}

// --- tools ---

func (t *Translator) onToolStart(e *protocol.ToolExecutionStart, out *[]tea.Msg) {
	key := e.ToolCallID
	if _, dup := t.tools[key]; dup && key != "" {
		return
	}
	st := &toolState{id: t.nextID("t"), order: t.seq, name: e.ToolName, args: shortArgs(e.Args), raw: e.Args, startTS: e.TS}
	if key == "" {
		key = st.id
	}
	t.tools[key] = st
	t.afterStep = true
	emitAppend(out, st.id, newToolCallBlock(st.call(), ToolRunning, "", "", toolOutput{}, t.view), false)
}

// onToolUpdate replaces the output of a running tool with its partial result.
// The partial result holds the output so far, not only the new part.
func (t *Translator) onToolUpdate(e *protocol.ToolExecutionUpdate, out *[]tea.Msg) {
	st, ok := t.tools[e.ToolCallID]
	if !ok {
		return
	}
	o := toolOutputOf(e.PartialResult.Content, e.PartialResult.Details)
	if o.text == st.out.text {
		return
	}
	st.out = o
	if st.name == "" {
		st.name = e.ToolName
	}
	emitReplace(out, st.id, newToolCallBlock(st.call(), ToolRunning, "", "", o, t.view))
}

func (t *Translator) onToolEnd(e *protocol.ToolExecutionEnd, out *[]tea.Msg) {
	output := toolOutputOf(e.Result.Content, e.Result.Details)
	failed := e.IsError || (e.Result.IsError != nil && *e.Result.IsError)
	reason := ""
	if failed {
		reason = firstLine(blocksText(e.Result.Content))
	}
	st, ok := t.tools[e.ToolCallID]
	if !ok {
		// The start event was not seen: show the outcome as a final line.
		t.afterStep = true
		emitAppend(out, t.nextID("t"), toolOutcome(toolCall{name: e.ToolName}, failed, "", reason, output, t.view), true)
		return
	}
	delete(t.tools, e.ToolCallID)
	t.noteJob(e, st.name, st.args)
	duration := ""
	if st.startTS > 0 && e.TS >= st.startTS {
		duration = formatDuration(time.Duration(e.TS-st.startTS) * time.Millisecond)
	}
	if st.name == "" {
		st.name = e.ToolName
	}
	emitReplace(out, st.id, toolOutcome(st.call(), failed, duration, reason, output, t.view))
	emitFinish(out, st.id)
}

// call returns what the block shows of the call.
func (st *toolState) call() toolCall {
	return toolCall{name: st.name, args: st.args, raw: st.raw, startMs: st.startTS}
}

func toolOutcome(c toolCall, failed bool, duration, reason string, out toolOutput, view *TranscriptView) Block {
	if failed {
		return newToolCallBlock(c, ToolFailed, "", reason, out, view)
	}
	return newToolCallBlock(c, ToolSucceeded, duration, "", out, view)
}

// blocksText joins the text blocks of a tool result.
func blocksText(blocks []protocol.UserBlock) string {
	var parts []string
	for _, blk := range blocks {
		if b, ok := blk.(protocol.Text); ok {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// --- notes, errors and the end of a run ---

func retryText(e *protocol.AutoRetryStart) string {
	msg := strings.TrimRight(retryCause(e.ErrorMessage), ". ")
	if msg == "" {
		msg = "the model request failed"
	}
	if e.MaxAttempts > 0 {
		return fmt.Sprintf("Attempt %d failed: %s. Retry %d of %d scheduled.", e.Attempt, msg, e.Attempt, e.MaxAttempts)
	}
	return fmt.Sprintf("Attempt %d failed: %s. Retry %d scheduled.", e.Attempt, msg, e.Attempt)
}

func (t *Translator) onCycleEnd(e *protocol.CycleEnd, out *[]tea.Msg) {
	switch e.Reason {
	case "error":
		if !t.run.errorShown {
			t.writeError(e.Code, out)
		}
	case "aborted":
		t.run.cancelled, t.run.cause = true, e.Cause
	case "blocked":
		t.writeNotice("blocked", "The model provider blocked the reply.", "Change the prompt, then send again.", out)
	case "max-tokens":
		t.writeNotice("max-tokens", "The reply reached the output limit.", "Send a follow-up to continue.", out)
	case "continuation-limit":
		t.writeNotice("continuation-limit", "The run reached its limit of automatic continuations.", "Send a prompt to go on.", out)
	}
}

// writeError writes the error block of a failed run. The message is the final
// error of a retry series, or the error of the last failed reply.
func (t *Translator) writeError(code string, out *[]tea.Msg) {
	t.run.errorShown = true
	provider, msg := "", t.run.finalError
	if t.run.lastErr != nil {
		provider = t.run.lastErr.provider
		if msg == "" {
			msg = t.run.lastErr.text
		}
	}
	if oneLine(msg) == "" {
		msg = "The request failed."
	}
	kind, next := classifyError(code, provider)
	t.afterStep = false
	emitAppend(out, t.nextID("e"), NewErrorBlock(ErrorHeadline(kind, sentence(providerErrorText(msg, provider))), next), true)
}

func (t *Translator) writeNotice(kind, msg, next string, out *[]tea.Msg) {
	t.afterStep = false
	emitAppend(out, t.nextID("e"), NewErrorBlock(ErrorHeadline(kind, msg), next), true)
}

func cancelText(cause string, partial bool) string {
	text := "Cancelled."
	switch cause {
	case "user":
		text = "Cancelled by you."
	case "deadline":
		text = "Stopped: the run reached its time limit."
	case "output":
		text = "Stopped: the output limit was reached."
	case "disposed":
		text = "Stopped: the session was closed."
	}
	if partial {
		text += " Partial reply kept above."
	}
	return text
}

func (t *Translator) onSettled(out *[]tea.Msg) {
	t.closeReply(out)
	// A tool that never reported an end would hold the frontier.
	var open []*toolState
	for _, st := range t.tools {
		open = append(open, st)
	}
	for i := 1; i < len(open); i++ {
		for j := i; j > 0 && open[j].order < open[j-1].order; j-- {
			open[j], open[j-1] = open[j-1], open[j]
		}
	}
	for _, st := range open {
		emitReplace(out, st.id, newToolCallBlock(st.call(), ToolFailed, "", "interrupted", st.out, t.view))
		emitFinish(out, st.id)
	}
	t.tools = map[string]*toolState{}

	if t.run.finalError != "" && !t.run.errorShown {
		t.writeError("", out)
	}
	if t.run.cancelled {
		t.afterStep = false
		emitAppend(out, t.nextID("c"), NewCancelMarker(cancelText(t.run.cause, t.run.hadReply)), true)
	}
	t.run = runState{}
}
