package tui

import (
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
//     finished, because it may already be in the scrollback.
//   - The user prompt comes from the user message event. It is the same for
//     live events, a replayed snapshot and an observer, so the session view
//     must not add a prompt block of its own.
//   - Tool lines come from tool_execution_* events, not from the tool calls in
//     the assistant message.
//   - Errors and cancel markers are written when the run ends. A cancel marker
//     waits for agent_settled, so the partial reply is final before it.
//   - agent_settled finishes everything that is still open, so that no block
//     holds the commit frontier after a run.

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

// ToggleThinking switches all thinking blocks between the full text and the
// short form, and returns the new state. The caller must then send RebuildMsg
// (or call Model.Rebuild) so that the committed blocks are drawn again, and
// should show ThinkingNotice(hidden) in the status row for a short time.
func (t *Translator) ToggleThinking() (hidden bool) { return t.view.ToggleThinking() }

// Reset forgets the state of the open run, message and tools. Blocks that
// were already sent stay in the transcript. Block ids stay unique across a
// reset.
func (t *Translator) Reset() {
	t.reply = nil
	t.tools = map[string]*toolState{}
	t.run = runState{}
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
	startTS int64
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
	case *protocol.ToolExecutionEnd:
		t.onToolEnd(e, &out)
	case *protocol.AutoRetryStart:
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

// segBlock makes the block for a part of a message. last is true for the
// block that is open; a finished block of a split thinking text is not last.
func (t *Translator) segBlock(p *partState, text string, last bool) Block {
	if p.thinking {
		return newThinkingPart(text, t.view, p.segments == 0, last)
	}
	return newAssistantTextBlock(text, p.md, p.segments == 0)
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
		emitAppend(out, p.cur, t.segBlock(p, "", true), false)
	}
	return p
}

// setText sets the text of an open part. Paragraphs that cannot change any
// more leave the open block and become finished blocks.
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
		emitAppend(out, p.cur, t.segBlock(p, "", true), false)
	}
	for {
		cut := closedCut(p.thinking, rest)
		if cut <= 0 {
			break
		}
		emitReplace(out, p.cur, t.segBlock(p, rest[:cut], false))
		emitFinish(out, p.cur)
		skip := cut + leadingBlankBytes(rest[cut:], p.thinking)
		p.frozen += skip
		rest = rest[skip:]
		p.segments++
		p.cur = t.nextID("a")
		if !p.thinking {
			p.md = newMarkdownStream()
		}
		emitAppend(out, p.cur, t.segBlock(p, "", true), false)
	}
	emitReplace(out, p.cur, t.segBlock(p, rest, true))
}

func (t *Translator) finishPart(p *partState, out *[]tea.Msg) {
	if p.done {
		return
	}
	p.done = true
	if p.shown {
		emitFinish(out, p.cur)
	}
}

func (t *Translator) onUpdate(e *protocol.MessageUpdate, out *[]tea.Msg) {
	r := t.ensureReply(e.TS)
	switch ev := e.AssistantMessageEvent.(type) {
	case protocol.TextStartEvent:
		p := t.part(r, ev.ContentIndex, false, out)
		t.setText(p, ev.Content.Text, out)
	case protocol.TextDeltaEvent:
		p := t.part(r, ev.ContentIndex, false, out)
		t.setText(p, p.raw+ev.Delta, out)
	case protocol.TextEndEvent:
		p := t.part(r, ev.ContentIndex, false, out)
		t.setText(p, ev.Content, out)
		t.finishPart(p, out)
	case protocol.ThinkingStartEvent:
		p := t.part(r, ev.ContentIndex, true, out)
		t.setText(p, ev.Content.Thinking, out)
	case protocol.ThinkingDeltaEvent:
		p := t.part(r, ev.ContentIndex, true, out)
		t.setText(p, p.raw+ev.Delta, out)
	case protocol.ThinkingEndEvent:
		p := t.part(r, ev.ContentIndex, true, out)
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
		// keeps the text of its own end event: it may be in the scrollback.
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
			emitAppend(out, t.nextID("s"), NewSpacerBlock(), true)
		}
	case protocol.StopAborted:
		// The cancel marker follows the partial reply without an empty row.
	default:
		if visible {
			if text := metaText(am, e.TS, r.startTS); text != "" {
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
	st := &toolState{id: t.nextID("t"), order: t.seq, name: e.ToolName, args: shortArgs(e.Args), startTS: e.TS}
	if key == "" {
		key = st.id
	}
	t.tools[key] = st
	emitAppend(out, st.id, NewToolLineBlock(st.name, st.args, ToolRunning, "", "", t.view), false)
}

func (t *Translator) onToolEnd(e *protocol.ToolExecutionEnd, out *[]tea.Msg) {
	failed := e.IsError || (e.Result.IsError != nil && *e.Result.IsError)
	reason := ""
	if failed {
		reason = firstLine(blocksText(e.Result.Content))
	}
	st, ok := t.tools[e.ToolCallID]
	if !ok {
		// The start event was not seen: show the outcome as a final line.
		emitAppend(out, t.nextID("t"), toolOutcome(e.ToolName, "", failed, "", reason, t.view), true)
		return
	}
	delete(t.tools, e.ToolCallID)
	duration := ""
	if st.startTS > 0 && e.TS >= st.startTS {
		duration = formatDuration(time.Duration(e.TS-st.startTS) * time.Millisecond)
	}
	name := st.name
	if name == "" {
		name = e.ToolName
	}
	emitReplace(out, st.id, toolOutcome(name, st.args, failed, duration, reason, t.view))
	emitFinish(out, st.id)
}

func toolOutcome(name, args string, failed bool, duration, reason string, view *TranscriptView) Block {
	if failed {
		return NewToolLineBlock(name, args, ToolFailed, "", reason, view)
	}
	return NewToolLineBlock(name, args, ToolSucceeded, duration, "", view)
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
	emitAppend(out, t.nextID("e"), NewErrorBlock(ErrorHeadline(kind, sentence(providerErrorText(msg, provider))), next), true)
}

func (t *Translator) writeNotice(kind, msg, next string, out *[]tea.Msg) {
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
	// A tool that never reported an end would hold the commit frontier.
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
		emitReplace(out, st.id, NewToolLineBlock(st.name, st.args, ToolFailed, "", "interrupted", t.view))
		emitFinish(out, st.id)
	}
	t.tools = map[string]*toolState{}

	if t.run.finalError != "" && !t.run.errorShown {
		t.writeError("", out)
	}
	if t.run.cancelled {
		emitAppend(out, t.nextID("c"), NewCancelMarker(cancelText(t.run.cause, t.run.hadReply)), true)
	}
	t.run = runState{}
}
