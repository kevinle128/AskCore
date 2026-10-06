package protocol

import (
	"errors"
	"fmt"
)

type blockKind uint8

const (
	kindText blockKind = iota + 1
	kindThinking
	kindToolCall
)

func (k blockKind) String() string {
	switch k {
	case kindText:
		return "text"
	case kindThinking:
		return "thinking"
	case kindToolCall:
		return "toolcall"
	}
	return "unknown"
}

// blockState is the scratch state of one content block. The block itself sits
// in Builder.msg.Content; an open block keeps its growing text or raw tool
// JSON in buf, so a delta costs only an append.
type blockState struct {
	kind blockKind
	open bool
	buf  []byte
}

// Builder rebuilds an assistant message from a stream of AssistantMessageEvent
// values. A client or an assembler feeds it the events in order and reads the
// partial message with Snapshot. It is not safe for concurrent use.
//
// The builder accepts the sequence: StartEvent once, then blocks in contiguous
// content-index order (updates to different open blocks can interleave), then
// one terminal DoneEvent or ErrorEvent. An ErrorEvent is also valid before
// StartEvent, for a failure during request setup. Every other sequence returns
// an error and leaves the builder state unchanged by that event.
//
// The builder never parses tool argument JSON: it keeps the raw delta bytes
// (RawToolJSON) and takes the final arguments from ToolCallEndEvent.
type Builder struct {
	started bool
	msg     AssistantMessage
	blocks  []blockState
	result  AssistantMessage
	done    bool
}

// NewBuilder returns an empty builder.
func NewBuilder() *Builder { return &Builder{} }

// Started reports whether the stream has started (or ended with a setup error).
func (b *Builder) Started() bool { return b.started }

// Apply applies one event. See Builder for the accepted sequence.
func (b *Builder) Apply(ev AssistantMessageEvent) error {
	if ev == nil {
		return errors.New("protocol: builder got a nil event")
	}
	if b.done {
		return fmt.Errorf("protocol: %s event after the terminal event", ev.EventType())
	}
	switch e := ev.(type) {
	case StartEvent:
		return b.start(e)
	case ErrorEvent:
		if err := checkError(e.Reason, &e.Error); err != nil {
			return err
		}
		b.finish(e.Error)
		return nil
	}
	if !b.started {
		return fmt.Errorf("protocol: %s event before start", ev.EventType())
	}
	switch e := ev.(type) {
	case DoneEvent:
		if err := checkDone(e.Reason, &e.Message); err != nil {
			return err
		}
		b.finish(e.Message)
	case TextStartEvent:
		return b.openBlock(e.ContentIndex, kindText, cloneText(e.Content), []byte(e.Content.Text))
	case ThinkingStartEvent:
		return b.openBlock(e.ContentIndex, kindThinking, cloneThinking(e.Content), []byte(e.Content.Thinking))
	case ToolCallStartEvent:
		return b.openBlock(e.ContentIndex, kindToolCall, ToolCall{
			ID: e.ID, Name: e.ToolName, Arguments: cloneRaw(e.Arguments),
			ThoughtSignature: clonePtr(e.ThoughtSignature), Namespace: clonePtr(e.Namespace),
		}, nil)
	case TextDeltaEvent:
		return b.appendDelta(e.ContentIndex, kindText, e.Delta)
	case ThinkingDeltaEvent:
		return b.appendDelta(e.ContentIndex, kindThinking, e.Delta)
	case ToolCallDeltaEvent:
		return b.appendDelta(e.ContentIndex, kindToolCall, e.Delta)
	case TextEndEvent:
		return b.closeBlock(e.ContentIndex, kindText, Text{Text: e.Content, TextSignature: clonePtr(e.TextSignature)})
	case ThinkingEndEvent:
		return b.closeBlock(e.ContentIndex, kindThinking, Thinking{
			Thinking: e.Content, ThinkingSignature: clonePtr(e.ThinkingSignature), Redacted: clonePtr(e.Redacted),
		})
	case ToolCallEndEvent:
		return b.closeBlock(e.ContentIndex, kindToolCall, cloneToolCall(e.ToolCall))
	default:
		return fmt.Errorf("protocol: builder cannot apply %T", ev)
	}
	return nil
}

// ApplyAgentEvent feeds an agent event to the builder for an assistant
// message: message_start seeds it, message_update applies the block event and
// the usage, and message_end replaces the whole message with the final one.
// Events about other message roles and all other event types are ignored.
func (b *Builder) ApplyAgentEvent(ev Event) error {
	switch e := ev.(type) {
	case *MessageStart:
		if m, ok := asAssistant(e.Message); ok {
			return b.start(StartEvent{Message: m})
		}
	case *MessageUpdate:
		if e.AssistantMessageEvent == nil {
			return errors.New("protocol: message_update has no assistant message event")
		}
		if err := b.Apply(e.AssistantMessageEvent); err != nil {
			return err
		}
		b.msg.Usage = e.Usage.Clone()
	case *MessageEnd:
		m, ok := asAssistant(e.Message)
		if !ok {
			return nil
		}
		if b.done {
			return errors.New("protocol: message_end after the terminal event")
		}
		if !b.started && m.StopReason != StopError && m.StopReason != StopAborted {
			return fmt.Errorf("protocol: message_end with stop reason %q before start", m.StopReason)
		}
		if m.StopReason == StopPending || !m.StopReason.valid() {
			return fmt.Errorf("protocol: message_end has non-terminal stop reason %q", m.StopReason)
		}
		b.finish(m)
	}
	return nil
}

func asAssistant(m Message) (AssistantMessage, bool) {
	switch v := m.(type) {
	case AssistantMessage:
		return v, true
	case *AssistantMessage:
		return *v, true
	}
	return AssistantMessage{}, false
}

func (b *Builder) start(e StartEvent) error {
	if b.started {
		return errors.New("protocol: duplicate start event")
	}
	if e.Message.StopReason != StopPending && e.Message.StopReason != "" {
		return fmt.Errorf("protocol: start message has stop reason %q, want pending", e.Message.StopReason)
	}
	b.started = true
	b.msg = e.Message.Clone()
	// A zero-value seed has no stop reason; a partial message is always pending.
	b.msg.StopReason = StopPending
	if b.msg.Content == nil {
		b.msg.Content = []AssistantBlock{}
	}
	// A seed that already holds blocks gets them as closed blocks.
	b.blocks = make([]blockState, len(b.msg.Content))
	for i, blk := range b.msg.Content {
		switch blk.(type) {
		case Text:
			b.blocks[i].kind = kindText
		case Thinking:
			b.blocks[i].kind = kindThinking
		case ToolCall:
			b.blocks[i].kind = kindToolCall
		default:
			b.started = false
			b.msg, b.blocks = AssistantMessage{}, nil
			return fmt.Errorf("protocol: start message content[%d] has an unsupported block %T", i, blk)
		}
	}
	return nil
}

func (b *Builder) finish(final AssistantMessage) {
	b.started = true
	b.done = true
	b.msg = final.Clone()
	b.result = final.Clone()
	b.blocks = nil
}

func (b *Builder) openBlock(index int, kind blockKind, blk AssistantBlock, initial []byte) error {
	if index < 0 {
		return fmt.Errorf("protocol: negative content index %d", index)
	}
	switch {
	case index < len(b.blocks):
		return fmt.Errorf("protocol: duplicate %s start at content index %d", kind, index)
	case index > len(b.blocks):
		return fmt.Errorf("protocol: %s start at content index %d leaves a gap (next index is %d)", kind, index, len(b.blocks))
	}
	b.msg.Content = append(b.msg.Content, blk)
	b.blocks = append(b.blocks, blockState{kind: kind, open: true, buf: initial})
	return nil
}

// openState returns the state of an open block of the wanted kind.
func (b *Builder) openState(index int, kind blockKind, op string) (*blockState, error) {
	if index < 0 || index >= len(b.blocks) {
		return nil, fmt.Errorf("protocol: %s %s for unknown content index %d", kind, op, index)
	}
	st := &b.blocks[index]
	if st.kind != kind {
		return nil, fmt.Errorf("protocol: %s %s for content index %d, which is a %s block", kind, op, index, st.kind)
	}
	if !st.open {
		return nil, fmt.Errorf("protocol: %s %s for content index %d, which already ended", kind, op, index)
	}
	return st, nil
}

func (b *Builder) appendDelta(index int, kind blockKind, delta string) error {
	st, err := b.openState(index, kind, "delta")
	if err != nil {
		return err
	}
	st.buf = append(st.buf, delta...)
	return nil
}

func (b *Builder) closeBlock(index int, kind blockKind, final AssistantBlock) error {
	st, err := b.openState(index, kind, "end")
	if err != nil {
		return err
	}
	b.msg.Content[index] = final
	st.open = false
	st.buf = nil
	return nil
}

// Snapshot returns a deep copy of the message so far. Open text and thinking
// blocks show the text received up to now. Open tool call blocks show the
// arguments given at their start (use RawToolJSON for the delta bytes). The
// copy costs time in proportion to the message size, so call it when a view is
// needed, not for every delta.
func (b *Builder) Snapshot() AssistantMessage {
	if !b.started && !b.done {
		return AssistantMessage{Content: []AssistantBlock{}, StopReason: StopPending}
	}
	snap := b.msg.Clone()
	for i := range b.blocks {
		st := &b.blocks[i]
		if !st.open {
			continue
		}
		switch blk := snap.Content[i].(type) {
		case Text:
			blk.Text = string(st.buf)
			snap.Content[i] = blk
		case Thinking:
			blk.Thinking = string(st.buf)
			snap.Content[i] = blk
		}
	}
	return snap
}

// Partial is the state of an unfinished builder, for a client that joins in the
// middle of a stream. Message is Snapshot. Open maps each open block to the raw
// bytes received for it: the text of a text or thinking block, and the argument
// delta bytes of a tool call block, which Message does not show. Blocks that are
// not in Open have ended.
type Partial struct {
	Message AssistantMessage
	Open    map[int][]byte
}

// Partial returns a deep copy of the state of the builder.
func (b *Builder) Partial() Partial {
	p := Partial{Message: b.Snapshot()}
	for i := range b.blocks {
		if !b.blocks[i].open {
			continue
		}
		if p.Open == nil {
			p.Open = map[int][]byte{}
		}
		var raw []byte
		if len(b.blocks[i].buf) > 0 {
			raw = append([]byte{}, b.blocks[i].buf...)
		}
		p.Open[i] = raw
	}
	return p
}

// ResumeFrom seeds an empty builder from p and keeps the blocks of p.Open open,
// so the events that follow the cut apply as they would have to the builder
// that p came from. The message must be pending, and every open index must name
// a block of it.
func (b *Builder) ResumeFrom(p Partial) error {
	if b.started || b.done {
		return errors.New("protocol: builder is not empty")
	}
	m := p.Message.Clone()
	if m.StopReason != StopPending {
		return fmt.Errorf("protocol: resume message has stop reason %q, want pending", m.StopReason)
	}
	if m.Content == nil {
		m.Content = []AssistantBlock{}
	}
	blocks := make([]blockState, len(m.Content))
	for i, blk := range m.Content {
		switch blk.(type) {
		case Text:
			blocks[i].kind = kindText
		case Thinking:
			blocks[i].kind = kindThinking
		case ToolCall:
			blocks[i].kind = kindToolCall
		default:
			return fmt.Errorf("protocol: resume content[%d] has an unsupported block %T", i, blk)
		}
	}
	for i, raw := range p.Open {
		if i < 0 || i >= len(blocks) {
			return fmt.Errorf("protocol: resume names open block %d of %d", i, len(blocks))
		}
		blocks[i].open = true
		if len(raw) > 0 {
			blocks[i].buf = append([]byte{}, raw...)
		}
	}
	b.started, b.msg, b.blocks = true, m, blocks
	return nil
}

// Result returns a deep copy of the final message once a terminal event was
// applied. The second value is false before that.
func (b *Builder) Result() (AssistantMessage, bool) {
	if !b.done {
		return AssistantMessage{}, false
	}
	return b.result.Clone(), true
}

// RawToolJSON returns a copy of the argument delta bytes of an open tool call
// block. It returns nil for an unknown, ended or non-tool block.
func (b *Builder) RawToolJSON(contentIndex int) []byte {
	if contentIndex < 0 || contentIndex >= len(b.blocks) {
		return nil
	}
	st := b.blocks[contentIndex]
	if st.kind != kindToolCall || !st.open || st.buf == nil {
		return nil
	}
	return append([]byte{}, st.buf...)
}

// OpenBlocks returns the content indexes that started and did not end, in
// ascending order.
func (b *Builder) OpenBlocks() []int {
	var out []int
	for i := range b.blocks {
		if b.blocks[i].open {
			out = append(out, i)
		}
	}
	return out
}
