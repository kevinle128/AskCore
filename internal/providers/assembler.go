package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"AskCore/internal/providers/partialjson"
	"AskCore/pkg/protocol"
)

// Metadata holds response metadata that can arrive at any time of a stream.
// SetMetadata merges it: a nil field leaves the earlier value alone, and the
// diagnostics are appended. The final message carries the merged values.
type Metadata struct {
	ResponseID            *string
	ResponseModel         *string
	ThinkingLevel         *protocol.ThinkingLevel
	ProviderThinkingLevel *string
	Diagnostics           []protocol.Diagnostic
	RawStopReason         *string
	EndTurn               *bool
}

// Assembler builds the assistant message of one stream from the calls of one
// producer. It is the shared producer side for the faux provider and the real
// adapters. Every call checks the request context, applies the event to a
// protocol.Builder (which validates the order) and queues an immutable
// StreamItem. The terminal methods (Done, Fail) only settle the result; the
// stream sends the terminal event and closes the channel after the producer
// body returns.
//
// An Assembler belongs to the producer goroutine and is not safe for
// concurrent use. After the stream is settled, every method is a no-op and
// the index-returning methods return -1. A call that the builder rejects is a
// producer bug: it settles the stream with stop reason error and the builder
// error text.
type Assembler struct {
	ctx      context.Context
	out      chan<- StreamItem
	settleFn func(protocol.AssistantMessage, error)

	seed    protocol.AssistantMessage
	b       *protocol.Builder
	usage   protocol.Usage
	meta    Metadata
	next    int
	tools   map[int]protocol.ToolCall
	usedIDs map[string]struct{}
	idSeq   int

	settled       bool
	terminal      protocol.AssistantMessageEvent
	terminalUsage protocol.Usage
}

func newAssembler(ctx context.Context, out chan<- StreamItem, seed protocol.AssistantMessage, settle func(protocol.AssistantMessage, error)) *Assembler {
	seed = seed.Clone()
	seed.Content = []protocol.AssistantBlock{}
	seed.StopReason = protocol.StopPending
	seed.ErrorMessage = nil
	return &Assembler{
		ctx:      ctx,
		out:      out,
		settleFn: settle,
		seed:     seed,
		b:        protocol.NewBuilder(),
		usage:    seed.Usage.Clone(),
		tools:    map[int]protocol.ToolCall{},
		usedIDs:  map[string]struct{}{},
	}
}

// Context returns the request context of the stream.
func (a *Assembler) Context() context.Context { return a.ctx }

// Settled reports that the stream has its result: a terminal method ran, the
// request was cancelled and observed, or a producer bug failed the stream. A
// producer loop can stop when it returns true.
func (a *Assembler) Settled() bool { return a.settled }

// Start emits the start event with the seed message.
func (a *Assembler) Start() {
	a.emit(protocol.StartEvent{Message: a.seed})
}

// TextStart opens a text block that already holds initial and returns its
// content index.
func (a *Assembler) TextStart(initial string) int {
	i := a.next
	if !a.emit(protocol.TextStartEvent{ContentIndex: i, Content: protocol.Text{Text: initial}}) {
		return -1
	}
	a.next++
	return i
}

// TextDelta appends text to the open text block i.
func (a *Assembler) TextDelta(i int, s string) {
	a.emit(protocol.TextDeltaEvent{ContentIndex: i, Delta: s})
}

// TextEnd closes the text block i. content and sig are the complete final
// values; a nil sig removes an earlier signature.
func (a *Assembler) TextEnd(i int, content string, sig *string) {
	a.emit(protocol.TextEndEvent{ContentIndex: i, Content: content, TextSignature: sig})
}

// ThinkingStart opens a thinking block and returns its content index. initial,
// sig and redacted are the values known at the start.
func (a *Assembler) ThinkingStart(initial string, sig *string, redacted *bool) int {
	i := a.next
	blk := protocol.Thinking{Thinking: initial, ThinkingSignature: sig, Redacted: redacted}
	if !a.emit(protocol.ThinkingStartEvent{ContentIndex: i, Content: blk}) {
		return -1
	}
	a.next++
	return i
}

// ThinkingDelta appends text to the open thinking block i.
func (a *Assembler) ThinkingDelta(i int, s string) {
	a.emit(protocol.ThinkingDeltaEvent{ContentIndex: i, Delta: s})
}

// ThinkingEnd closes the thinking block i. The values are the complete final
// ones; nil metadata removes an earlier value.
func (a *Assembler) ThinkingEnd(i int, content string, sig *string, redacted *bool) {
	a.emit(protocol.ThinkingEndEvent{ContentIndex: i, Content: content, ThinkingSignature: sig, Redacted: redacted})
}

// ToolStart opens a tool call block and returns its content index. An empty id
// is allowed: the id is generated when the block ends. initialArgs, when set,
// must be a JSON object; it is the fallback arguments if no delta arrives.
func (a *Assembler) ToolStart(id, name string, initialArgs json.RawMessage, thoughtSig, ns *string) int {
	if a.settled {
		return -1
	}
	if a.ctx.Err() != nil {
		a.abort()
		return -1
	}
	if len(initialArgs) > 0 && !isJSONObject(initialArgs) {
		a.fail(errors.New("toolcall start arguments are not a JSON object"))
		return -1
	}
	i := a.next
	ev := protocol.ToolCallStartEvent{
		ContentIndex: i, ID: id, ToolName: name, Arguments: initialArgs, ThoughtSignature: thoughtSig, Namespace: ns,
	}
	if !a.emit(ev) {
		return -1
	}
	a.next++
	a.tools[i] = protocol.CloneAssistantBlock(protocol.ToolCall{
		ID: id, Name: name, Arguments: initialArgs, ThoughtSignature: thoughtSig, Namespace: ns,
	}).(protocol.ToolCall)
	if id != "" {
		a.usedIDs[id] = struct{}{}
	}
	return i
}

// ToolDelta appends raw argument JSON text to the open tool call block i.
func (a *Assembler) ToolDelta(i int, raw string) {
	a.emit(protocol.ToolCallDeltaEvent{ContentIndex: i, Delta: raw})
}

// ToolEnd closes the tool call block i. A non-nil final is authoritative: it
// replaces the whole block and its arguments (a JSON object, or empty for {})
// are never parsed again. An empty id or name in final falls back to the
// value given at the start. With a nil final, the raw delta bytes are
// repaired and parsed once; with no delta the start arguments are used, or {}.
// A tool call that still has no id gets a generated one that no other call of
// the stream uses.
func (a *Assembler) ToolEnd(i int, final *protocol.ToolCall) {
	if a.settled {
		return
	}
	start, ok := a.tools[i]
	if !ok {
		a.fail(fmt.Errorf("toolcall end for unknown tool block %d", i))
		return
	}
	var tc protocol.ToolCall
	if final != nil {
		tc = protocol.CloneAssistantBlock(*final).(protocol.ToolCall)
		if tc.ID == "" {
			tc.ID = start.ID
		}
		if tc.Name == "" {
			tc.Name = start.Name
		}
		switch {
		case len(tc.Arguments) == 0:
			tc.Arguments = json.RawMessage(`{}`)
		case !isJSONObject(tc.Arguments):
			a.fail(fmt.Errorf("toolcall end %d has arguments that are not a JSON object", i))
			return
		}
	} else {
		tc = start
		tc.Arguments = toolArguments(a.b.RawToolJSON(i), start.Arguments)
	}
	if tc.ID == "" {
		tc.ID = a.newID()
	}
	a.usedIDs[tc.ID] = struct{}{}
	a.emit(protocol.ToolCallEndEvent{ContentIndex: i, ToolCall: tc})
}

// SetUsage records the latest usage. It emits no event: the next item and the
// final message carry the value.
func (a *Assembler) SetUsage(u protocol.Usage) {
	if a.settled {
		return
	}
	a.usage = u.Clone()
}

// SetMetadata merges response metadata into the final message (see Metadata).
func (a *Assembler) SetMetadata(m Metadata) {
	if a.settled {
		return
	}
	setIfPresent(&a.meta.ResponseID, m.ResponseID)
	setIfPresent(&a.meta.ResponseModel, m.ResponseModel)
	setIfPresent(&a.meta.ThinkingLevel, m.ThinkingLevel)
	setIfPresent(&a.meta.ProviderThinkingLevel, m.ProviderThinkingLevel)
	setIfPresent(&a.meta.RawStopReason, m.RawStopReason)
	setIfPresent(&a.meta.EndTurn, m.EndTurn)
	if len(m.Diagnostics) > 0 {
		a.meta.Diagnostics = append(a.meta.Diagnostics, cloneDiagnostics(m.Diagnostics)...)
	}
}

// Done settles the stream with a success reason (stop, length, toolUse or
// deferred). Only length can have unfinished blocks: they are kept with
// repaired tool arguments. Any other reason with an open block is a producer
// bug and fails the stream. A second terminal call is a no-op.
func (a *Assembler) Done(reason protocol.StopReason) {
	if a.settled {
		return
	}
	if a.ctx.Err() != nil {
		a.abort()
		return
	}
	if reason == protocol.StopError || reason == protocol.StopAborted {
		// A failure must come with a Go error, so callers that retry on
		// errors see it. Fail is the only path for these reasons.
		a.fail(fmt.Errorf("done with failure reason %q; use Fail", reason))
		return
	}
	if reason != protocol.StopLength {
		if open := a.b.OpenBlocks(); len(open) > 0 {
			a.fail(fmt.Errorf("done %q with unfinished content block %d", reason, open[0]))
			return
		}
	}
	a.finish(reason, "", nil)
}

// Fail settles the stream with reason error or aborted (any other value
// becomes error). The final message keeps the content so far, with repaired
// tool arguments for open blocks, the latest usage and msg as errorMessage.
// err is the Go error of Result; nil means an error made from msg, and an
// empty msg is taken from err. A second
// terminal call is a no-op.
func (a *Assembler) Fail(reason protocol.StopReason, msg string, err error) {
	if a.settled {
		return
	}
	if a.ctx.Err() != nil {
		a.abort()
		return
	}
	if reason != protocol.StopError && reason != protocol.StopAborted {
		reason = protocol.StopError
	}
	if msg == "" {
		msg = string(reason)
		if err != nil {
			msg = err.Error()
		}
	}
	if err == nil {
		err = errors.New(msg)
	}
	if reason == protocol.StopError {
		err = typedFailure(msg, err)
	}
	a.finish(reason, msg, err)
}

// Emit is the raw seam: it validates ev with the builder and queues a copy.
// It returns the builder error for an invalid event and leaves the stream
// open. A DoneEvent or ErrorEvent settles the stream with its own message as
// the result. It returns ErrStreamClosed when the stream is settled.
func (a *Assembler) Emit(ev protocol.AssistantMessageEvent) error {
	if a.settled {
		return ErrStreamClosed
	}
	if a.ctx.Err() != nil {
		a.abort()
		return ErrStreamClosed
	}
	ev = cloneEvent(ev)
	if err := a.checkRaw(ev); err != nil {
		a.fail(err)
		return err
	}
	if err := a.b.Apply(ev); err != nil {
		return err
	}
	switch e := ev.(type) {
	case protocol.DoneEvent:
		a.settleRaw(ev, e.Message, nil)
		return nil
	case protocol.ErrorEvent:
		text := string(e.Reason)
		if e.Error.ErrorMessage != nil && *e.Error.ErrorMessage != "" {
			text = *e.Error.ErrorMessage
		}
		a.settleRaw(ev, e.Error, errors.New(text))
		return nil
	case protocol.TextStartEvent:
		a.next = e.ContentIndex + 1
	case protocol.ThinkingStartEvent:
		a.next = e.ContentIndex + 1
	case protocol.ToolCallStartEvent:
		a.next = e.ContentIndex + 1
		a.tools[e.ContentIndex] = protocol.ToolCall{
			ID: e.ID, Name: e.ToolName, Arguments: e.Arguments, ThoughtSignature: e.ThoughtSignature, Namespace: e.Namespace,
		}
		if e.ID != "" {
			a.usedIDs[e.ID] = struct{}{}
		}
	case protocol.ToolCallEndEvent:
		if e.ToolCall.ID != "" {
			a.usedIDs[e.ToolCall.ID] = struct{}{}
		}
	}
	if !a.send(ev) {
		return ErrStreamClosed
	}
	return nil
}

// checkRaw finds producer bugs that the builder accepts for clients: a done
// event with an unfinished block (unless the reason is length), and a tool call
// end whose arguments are not a JSON object. The result could not be encoded.
func (a *Assembler) checkRaw(ev protocol.AssistantMessageEvent) error {
	switch e := ev.(type) {
	case protocol.DoneEvent:
		if e.Reason != protocol.StopLength {
			if open := a.b.OpenBlocks(); len(open) > 0 {
				return fmt.Errorf("done %q with unfinished content block %d", e.Reason, open[0])
			}
		}
	case protocol.ToolCallEndEvent:
		if len(e.ToolCall.Arguments) > 0 && !isJSONObject(e.ToolCall.Arguments) {
			return fmt.Errorf("toolcall end %d has arguments that are not a JSON object", e.ContentIndex)
		}
	}
	return nil
}

// emit validates one event with the builder and queues it. It returns false
// when the event was not queued because the stream is settled, the request was
// cancelled or the builder rejected the event.
func (a *Assembler) emit(ev protocol.AssistantMessageEvent) bool {
	if a.settled {
		return false
	}
	if a.ctx.Err() != nil {
		a.abort()
		return false
	}
	ev = cloneEvent(ev)
	if err := a.b.Apply(ev); err != nil {
		a.fail(err)
		return false
	}
	return a.send(ev)
}

// send queues one item. A full channel blocks the producer, but never past the
// cancellation of the request.
func (a *Assembler) send(ev protocol.AssistantMessageEvent) bool {
	item := StreamItem{Event: ev, Usage: a.usage.Clone()}
	select {
	case a.out <- item:
		return true
	case <-a.ctx.Done():
		a.abort()
		return false
	}
}

// fail settles the stream for a producer bug.
func (a *Assembler) fail(err error) {
	a.finish(protocol.StopError, err.Error(), fmt.Errorf("providers: invalid stream production: %w", err))
}

func (a *Assembler) abort() {
	a.finish(protocol.StopAborted, abortedMessage, a.abortErr())
}

// abortErr is the Go error of a cancelled request. It wraps the context error.
func (a *Assembler) abortErr() error {
	return fmt.Errorf("%s: %w", abortedMessage, a.ctx.Err())
}

// finish builds the final message, applies the terminal event to the builder
// and settles. It reads the builder state before the terminal event, because
// the terminal event clears the open blocks.
func (a *Assembler) finish(reason protocol.StopReason, errMsg string, err error) {
	if a.settled {
		return
	}
	msg := a.compose(reason, errMsg)
	ev := terminalEvent(reason, msg)
	if aerr := a.b.Apply(ev); aerr != nil {
		msg = a.compose(protocol.StopError, aerr.Error())
		err = fmt.Errorf("providers: invalid stream production: %w", aerr)
		ev = terminalEvent(protocol.StopError, msg)
		// An error event is valid in every builder state, so this cannot fail.
		_ = a.b.Apply(ev)
	}
	a.settleRaw(ev, msg, err)
}

// settleRaw records ev as the terminal event and settles with a copy of msg.
func (a *Assembler) settleRaw(ev protocol.AssistantMessageEvent, msg protocol.AssistantMessage, err error) {
	a.terminal = ev
	a.terminalUsage = msg.Usage.Clone()
	a.settled = true
	a.settleFn(msg.Clone(), err)
}

// seal settles a stream whose producer returned without a terminal call.
func (a *Assembler) seal() {
	if a.settled {
		return
	}
	if a.ctx.Err() != nil {
		a.safeFinish(protocol.StopAborted, abortedMessage, a.abortErr())
		return
	}
	a.safeFinish(protocol.StopError, incompleteMessage, NewFailure(CodeStreamClosed, 0, 0, incompleteMessage, nil))
}

// safeFinish runs finish and, if that panics, settles with a plain error
// message built from the seed, so the result and the close path always work.
func (a *Assembler) safeFinish(reason protocol.StopReason, errMsg string, err error) {
	defer func() {
		if r := recover(); r != nil && !a.settled {
			msg := a.seed.Clone()
			msg.StopReason = protocol.StopError
			text := fmt.Sprintf("stream assembler panicked: %v", r)
			msg.ErrorMessage = &text
			a.settleRaw(protocol.ErrorEvent{Reason: protocol.StopError, Error: msg.Clone()}, msg, errors.New(text))
		}
	}()
	a.finish(reason, errMsg, err)
}

// runBody runs the producer body. A panic in the body is a producer bug: it
// becomes a settled error and never ends the process.
func (a *Assembler) runBody(body func(*Assembler)) {
	defer func() {
		if r := recover(); r != nil {
			a.safeFinish(protocol.StopError, fmt.Sprintf("stream producer panicked: %v", r),
				fmt.Errorf("providers: stream producer panicked: %v", r))
		}
	}()
	body(a)
}

// terminalItem returns the item that the stream sends last.
func (a *Assembler) terminalItem() StreamItem {
	return StreamItem{Event: a.terminal, Usage: a.terminalUsage.Clone()}
}

// compose builds the final message for reason from the builder state (or from
// the seed before a start event), the latest usage and the merged metadata.
// Open blocks are kept; open tool calls get repaired arguments and an id.
func (a *Assembler) compose(reason protocol.StopReason, errMsg string) protocol.AssistantMessage {
	var msg protocol.AssistantMessage
	if a.b.Started() {
		msg = a.b.Snapshot()
		for _, i := range a.b.OpenBlocks() {
			tc, ok := msg.Content[i].(protocol.ToolCall)
			if !ok {
				continue
			}
			tc.Arguments = toolArguments(a.b.RawToolJSON(i), tc.Arguments)
			if tc.ID == "" {
				tc.ID = a.newID()
				a.usedIDs[tc.ID] = struct{}{}
			}
			msg.Content[i] = tc
		}
	} else {
		msg = a.seed.Clone()
	}
	if msg.Content == nil {
		msg.Content = []protocol.AssistantBlock{}
	}
	msg.Usage = a.usage.Clone()
	a.applyMetadata(&msg)
	msg.StopReason = reason
	msg.ErrorMessage = nil
	if errMsg != "" {
		msg.ErrorMessage = &errMsg
	}
	return msg
}

func (a *Assembler) applyMetadata(msg *protocol.AssistantMessage) {
	m := a.meta
	setIfPresent(&msg.ResponseID, m.ResponseID)
	setIfPresent(&msg.ResponseModel, m.ResponseModel)
	setIfPresent(&msg.ThinkingLevel, m.ThinkingLevel)
	setIfPresent(&msg.ProviderThinkingLevel, m.ProviderThinkingLevel)
	setIfPresent(&msg.RawStopReason, m.RawStopReason)
	setIfPresent(&msg.EndTurn, m.EndTurn)
	if len(m.Diagnostics) > 0 {
		msg.Diagnostics = append(msg.Diagnostics, cloneDiagnostics(m.Diagnostics)...)
	}
}

// newID returns a generated tool call id that no call of the stream uses.
func (a *Assembler) newID() string {
	for {
		a.idSeq++
		id := fmt.Sprintf("call_%d", a.idSeq)
		if _, taken := a.usedIDs[id]; !taken {
			return id
		}
	}
}

func terminalEvent(reason protocol.StopReason, msg protocol.AssistantMessage) protocol.AssistantMessageEvent {
	if reason == protocol.StopError || reason == protocol.StopAborted {
		return protocol.ErrorEvent{Reason: reason, Error: msg.Clone()}
	}
	return protocol.DoneEvent{Reason: reason, Message: msg.Clone()}
}

// toolArguments returns the argument object of a tool call that has no final
// value. Order: the raw delta bytes (kept as they are when they are a complete
// JSON object, else repaired and parsed once), then the start arguments, then
// the empty object.
func toolArguments(raw []byte, start json.RawMessage) json.RawMessage {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 {
		if isJSONObject(trimmed) {
			var buf bytes.Buffer
			if json.Compact(&buf, trimmed) == nil {
				return buf.Bytes()
			}
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if enc.Encode(partialjson.Parse(string(trimmed))) == nil {
			return bytes.TrimRight(buf.Bytes(), "\n")
		}
	}
	if isJSONObject(start) {
		return append(json.RawMessage{}, start...)
	}
	return json.RawMessage(`{}`)
}

func isJSONObject(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// setIfPresent sets *dst to a copy of src when src is not nil. A nil src keeps
// the earlier value.
func setIfPresent[T any](dst **T, src *T) {
	if src != nil {
		*dst = clonePtr(src)
	}
}

func cloneDiagnostics(in []protocol.Diagnostic) []protocol.Diagnostic {
	return protocol.AssistantMessage{Diagnostics: in}.Clone().Diagnostics
}

// cloneEvent returns a copy of a provider event that shares no pointer, slice
// or raw JSON with the original.
func cloneEvent(ev protocol.AssistantMessageEvent) protocol.AssistantMessageEvent {
	switch e := ev.(type) {
	case protocol.StartEvent:
		e.Message = e.Message.Clone()
		return e
	case protocol.TextStartEvent:
		e.Content = protocol.CloneAssistantBlock(e.Content).(protocol.Text)
		return e
	case protocol.TextEndEvent:
		e.TextSignature = clonePtr(e.TextSignature)
		return e
	case protocol.ThinkingStartEvent:
		e.Content = protocol.CloneAssistantBlock(e.Content).(protocol.Thinking)
		return e
	case protocol.ThinkingEndEvent:
		e.ThinkingSignature = clonePtr(e.ThinkingSignature)
		e.Redacted = clonePtr(e.Redacted)
		return e
	case protocol.ToolCallStartEvent:
		e.Arguments = append(json.RawMessage(nil), e.Arguments...)
		e.ThoughtSignature = clonePtr(e.ThoughtSignature)
		e.Namespace = clonePtr(e.Namespace)
		return e
	case protocol.ToolCallEndEvent:
		e.ToolCall = protocol.CloneAssistantBlock(e.ToolCall).(protocol.ToolCall)
		return e
	case protocol.DoneEvent:
		e.Message = e.Message.Clone()
		return e
	case protocol.ErrorEvent:
		e.Error = e.Error.Clone()
		return e
	}
	return ev
}
