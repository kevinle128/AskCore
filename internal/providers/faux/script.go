package faux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// Call is what a Func factory sees: the call number (counted from 1, the
// factory's own call included) and copies of the selected model, the options
// and the normalized request.
type Call struct {
	Number  int
	Model   providers.Model
	Options providers.StreamOptions
	Request providers.TranscriptRequest
}

// Factory builds the step for one call. It runs inside the stream producer, so
// a panic or an error becomes a terminal error of that stream.
type Factory func(ctx context.Context, c Call) (Step, error)

type stepKind int

const (
	kindReply stepKind = iota
	kindFunc
	kindRaw
)

// Step is one scripted model call. A Step is a value: every modifier returns a
// changed copy and never changes the original or the blocks it holds.
type Step struct {
	kind   stepKind
	blocks []Block
	fn     Factory
	raw    []protocol.AssistantMessageEvent

	stop     *protocol.StopReason
	usage    *protocol.Usage
	errMsg   *string
	respID   *string
	ts       *int64
	delay    time.Duration
	tps      *float64
	truncate *int
}

// Reply is a step that streams blocks in order. The stop reason is toolUse when
// a ToolCall block exists and stop otherwise (see Step.Stop).
func Reply(blocks ...Block) Step {
	return Step{kind: kindReply, blocks: append([]Block(nil), blocks...)}
}

// Say is Reply(Text(text)).
func Say(text string) Step { return Reply(Text(text)) }

// Func is a step that is built at call time. The modifiers of a Func step are
// defaults for the step that f returns. f must not return a Func step or set a
// Timestamp, because the message identity is fixed before f runs.
func Func(f Factory) Step { return Step{kind: kindFunc, fn: f} }

// Fail is a step that streams start and then a terminal error with msg.
func Fail(msg string) Step {
	return Reply().Stop(protocol.StopError).Error(msg)
}

// Raw is a step that gives the events to the stream as they are. The identity
// of the script (api, provider and model in its messages) passes through
// unchanged; the provider does not rewrite it as it does for Reply. The stream
// still validates them with the message builder. Delay, Truncate and Timestamp
// apply to a Raw step. WithUsage sets the usage that the items carry, and
// ResponseID reaches the result only when the events hold no terminal event.
// Stop and Error do not apply. When the events hold no terminal event, the
// stream ends as incomplete.
func Raw(events ...protocol.AssistantMessageEvent) Step {
	return Step{kind: kindRaw, raw: append([]protocol.AssistantMessageEvent(nil), events...)}
}

// Stop sets the stop reason. Error and aborted end with a terminal error event,
// pending ends with an error because a scripted reply must have a reason, and
// length is the only success reason that can follow an unfinished block.
func (s Step) Stop(r protocol.StopReason) Step { s.stop = &r; return s }

// WithUsage sets the exact usage of the result, cost included. It replaces the
// estimate but not the cache bookkeeping of the provider.
func (s Step) WithUsage(u protocol.Usage) Step { c := u.Clone(); s.usage = &c; return s }

// Error sets the error message of an error or aborted reply.
func (s Step) Error(msg string) Step { s.errMsg = &msg; return s }

// ResponseID sets the response id of the result.
func (s Step) ResponseID(id string) Step { s.respID = &id; return s }

// Timestamp sets the message timestamp in unix milliseconds. Zero is a valid
// value; without this modifier the provider clock gives the timestamp.
func (s Step) Timestamp(ms int64) Step { s.ts = &ms; return s }

// Delay waits on the provider clock before the first event.
func (s Step) Delay(d time.Duration) Step { s.delay = d; return s }

// Pace sets the tokens per second of this step. Zero turns pacing off.
func (s Step) Pace(tokensPerSecond float64) Step { s.tps = &tokensPerSecond; return s }

// Truncate ends the stream without a terminal event after n provider events
// (start, block start, delta and block end count; usage and metadata do not).
// The stream then settles as incomplete. Truncate(0) sends no start event.
func (s Step) Truncate(n int) Step {
	if n < 0 {
		n = 0
	}
	s.truncate = &n
	return s
}

// overlay merges the step that a factory returned with the modifiers of the
// Func step that held it. Fields that the inner step sets win.
func (s Step) overlay(inner Step) (Step, error) {
	if inner.kind == kindFunc {
		return Step{}, errors.New("faux: a factory cannot return a Func step")
	}
	if inner.ts != nil {
		return Step{}, errors.New("faux: a factory cannot set Timestamp; set it on the Func step")
	}
	inner.ts = s.ts
	if inner.stop == nil {
		inner.stop = s.stop
	}
	if inner.usage == nil {
		inner.usage = s.usage
	}
	if inner.errMsg == nil {
		inner.errMsg = s.errMsg
	}
	if inner.respID == nil {
		inner.respID = s.respID
	}
	if inner.tps == nil {
		inner.tps = s.tps
	}
	if inner.truncate == nil {
		inner.truncate = s.truncate
	}
	if inner.delay == 0 {
		inner.delay = s.delay
	}
	return inner, nil
}

// Block is one scripted content block of a Reply.
type Block interface{ scriptBlock() }

type textBlock struct{ text string }

type thinkingBlock struct{ text string }

type toolBlock struct {
	name string
	id   string
	raw  string // exact argument bytes
	auth bool   // true: the arguments are the authoritative final value
	err  error
}

func (textBlock) scriptBlock()     {}
func (thinkingBlock) scriptBlock() {}
func (toolBlock) scriptBlock()     {}

// Text is a text block. An empty text still gives one empty delta.
func Text(s string) Block { return textBlock{text: s} }

// Thinking is a thinking block.
func Thinking(s string) Block { return thinkingBlock{text: s} }

// ToolOpt changes a ToolCall block.
type ToolOpt func(*toolBlock)

// ID sets the id of a tool call. Without it the provider gives tool:CALL:N.
func ID(id string) ToolOpt { return func(b *toolBlock) { b.id = id } }

// ToolCall is a tool call whose arguments are args encoded with encoding/json
// (nil means {}). The encoded text is sent as deltas, and the end event holds
// the exact object, so the result equals the script. Arguments that are not a
// JSON object make the stream fail before it starts.
func ToolCall(name string, args any, opts ...ToolOpt) Block {
	b := toolBlock{name: name, auth: true}
	for _, o := range opts {
		o(&b)
	}
	if args == nil {
		b.raw = "{}"
		return b
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(args); err != nil {
		b.err = err
		return b
	}
	raw := bytes.TrimRight(buf.Bytes(), "\n")
	if len(raw) == 0 || raw[0] != '{' {
		b.err = errors.New("tool call arguments must be a JSON object")
		return b
	}
	b.raw = string(raw)
	return b
}

// ToolCallRaw is a tool call that sends raw as the argument deltas, byte for
// byte, and lets the assembler parse it at the end. The text can be broken
// JSON. An empty id means the provider gives tool:CALL:N.
func ToolCallRaw(name, id, raw string) Block {
	return toolBlock{name: name, id: id, raw: raw}
}
