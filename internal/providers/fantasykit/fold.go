package fantasykit

import (
	"cmp"
	"context"
	"encoding/json"
	"strings"

	"charm.land/fantasy"

	"AskCore/internal/providers"
	"AskCore/internal/providers/partialjson"
	"AskCore/pkg/protocol"
)

// ReasoningMeta is the signature data a wire adapter pulls from
// ProviderMetadata. Chat has none; Anthropic fills both fields.
type ReasoningMeta struct {
	Signature    string
	RedactedData string
}

// Options are the per-API hooks Fold needs. MapStop is the finish-reason
// table; Idle is the cancel cause (usually ErrIdleTimeout).
type Options struct {
	ReqCtx          context.Context
	RunCtx          context.Context
	Idle            error
	Witness         interface{ StopReason() string }
	InferStopOnDone bool
	MapStop         func(raw string) (protocol.StopReason, error)
	Reasoning       func(fantasy.ProviderMetadata) *ReasoningMeta
	// Text fills TextSignature from part metadata. Nil leaves it unset.
	Text func(fantasy.ProviderMetadata) *string
	// Tool rewrites the tool-call id from part metadata. Nil keeps part.ID.
	Tool func(id string, md fantasy.ProviderMetadata) string
	// Usage replaces the default fantasy token copy. Nil uses Input/Output/cache.
	Usage func(fantasy.StreamPart) protocol.Usage
	// Meta merges extra finish metadata. Nil keeps response id and raw stop.
	Meta func(fantasy.StreamPart) providers.Metadata
	// RawStop overrides the witness string passed to MapStop.
	RawStop func(part fantasy.StreamPart, witnessed string) string
}

type textBlock struct {
	index int
	text  string
}

type thinkBlock struct {
	index    int
	text     string
	sig      *string
	redacted *bool
}

type toolBlock struct {
	index int
	id    string
	input string
}

// Fold maps fantasy StreamParts onto Assembler. Tool-argument text comes
// from Delta when it is set, otherwise ToolCallInput.
func Fold(a *providers.Assembler, parts fantasy.StreamResponse, opt Options) {
	texts := map[string]*textBlock{}
	thinks := map[string]*thinkBlock{}
	tools := map[string]*toolBlock{}
	sawContent, sawTool := false, false

	for part := range parts {
		if a.Settled() {
			return
		}
		switch part.Type {
		case fantasy.StreamPartTypeWarnings:
			diags := make([]protocol.Diagnostic, 0, len(part.Warnings))
			for _, w := range part.Warnings {
				text := w.Message
				if text == "" {
					text = w.Details
				}
				if text == "" {
					text = w.Setting
				}
				diags = append(diags, protocol.Diagnostic{
					Type:  "provider_warning",
					Error: &protocol.DiagnosticError{Message: text},
				})
			}
			if len(diags) > 0 {
				a.SetMetadata(providers.Metadata{Diagnostics: diags})
			}
		case fantasy.StreamPartTypeKeepalive:
		case fantasy.StreamPartTypeTextStart:
			i := a.TextStart("")
			texts[part.ID] = &textBlock{index: i}
		case fantasy.StreamPartTypeTextDelta:
			if b := texts[part.ID]; b != nil {
				if part.Delta != "" {
					sawContent = true
				}
				b.text += part.Delta
				a.TextDelta(b.index, part.Delta)
			}
		case fantasy.StreamPartTypeTextEnd:
			if b := texts[part.ID]; b != nil {
				a.TextEnd(b.index, b.text, textSig(opt.Text, part.ProviderMetadata))
			}
		case fantasy.StreamPartTypeReasoningStart:
			// Delay ThinkingStart so an empty disabled-mode block never opens.
			tb := &thinkBlock{index: -1}
			if meta := reasoningOf(opt.Reasoning, part.ProviderMetadata); meta != nil && meta.RedactedData != "" {
				red := true
				sig := meta.RedactedData
				tb.redacted = &red
				tb.sig = &sig
				tb.index = a.ThinkingStart("", tb.sig, tb.redacted)
			}
			thinks[part.ID] = tb
		case fantasy.StreamPartTypeReasoningDelta:
			tb := thinks[part.ID]
			if tb == nil {
				continue
			}
			if part.Delta != "" {
				sawContent = true
				tb.text += part.Delta
				if tb.index < 0 {
					tb.index = a.ThinkingStart("", tb.sig, tb.redacted)
				}
				a.ThinkingDelta(tb.index, part.Delta)
			}
			if meta := reasoningOf(opt.Reasoning, part.ProviderMetadata); meta != nil {
				sig := meta.Signature
				tb.sig = &sig
			}
		case fantasy.StreamPartTypeReasoningEnd:
			tb := thinks[part.ID]
			if tb == nil {
				continue
			}
			if meta := reasoningOf(opt.Reasoning, part.ProviderMetadata); meta != nil {
				if meta.RedactedData != "" {
					red := true
					tb.redacted = &red
					sig := meta.RedactedData
					tb.sig = &sig
				} else {
					sig := meta.Signature
					tb.sig = &sig
				}
			}
			if strings.TrimSpace(tb.text) == "" && (tb.sig == nil || *tb.sig == "") {
				continue
			}
			if tb.index < 0 {
				tb.index = a.ThinkingStart(tb.text, tb.sig, tb.redacted)
			}
			a.ThinkingEnd(tb.index, tb.text, tb.sig, tb.redacted)
		case fantasy.StreamPartTypeToolInputStart:
			id := toolID(opt.Tool, part.ID, part.ProviderMetadata)
			i := a.ToolStart(id, part.ToolCallName, nil, nil, nil)
			tools[part.ID] = &toolBlock{index: i, id: id}
		case fantasy.StreamPartTypeToolInputDelta:
			if b := tools[part.ID]; b != nil {
				delta := cmp.Or(part.Delta, part.ToolCallInput)
				b.input += delta
				a.ToolDelta(b.index, delta)
			}
		case fantasy.StreamPartTypeToolInputEnd:
		case fantasy.StreamPartTypeToolCall:
			if b := tools[part.ID]; b != nil {
				sawTool = true
				id := toolID(opt.Tool, part.ID, part.ProviderMetadata)
				if id != b.id {
					// A final block replaces arguments. Keep them when only the item ID changes.
					args, err := json.Marshal(partialjson.Parse(cmp.Or(part.ToolCallInput, b.input)))
					if err != nil {
						Fail(a, opt.ReqCtx, opt.RunCtx, opt.Idle, err)
						return
					}
					a.ToolEnd(b.index, &protocol.ToolCall{ID: id, Arguments: args})
				} else {
					a.ToolEnd(b.index, nil)
				}
			}
		case fantasy.StreamPartTypeSource, fantasy.StreamPartTypeToolResult:
		case fantasy.StreamPartTypeError:
			if inferStop(a, opt, sawContent, sawTool, part.Error) {
				return
			}
			Fail(a, opt.ReqCtx, opt.RunCtx, opt.Idle, part.Error)
			return
		case fantasy.StreamPartTypeFinish:
			raw := rawStop(opt.Witness)
			if opt.RawStop != nil {
				raw = opt.RawStop(part, raw)
			}
			if raw == "" && canInferStop(opt, sawContent, sawTool, nil) {
				if sawTool {
					raw = "tool_calls"
				} else {
					raw = "stop"
				}
			}
			blocks := len(texts) + len(tools)
			for _, tb := range thinks {
				if tb.index >= 0 {
					blocks++
				}
			}
			finish(a, part, raw, blocks, opt)
			return
		}
	}
	inferStop(a, opt, sawContent, sawTool, nil)
}

func inferStop(a *providers.Assembler, opt Options, sawContent, sawTool bool, err error) bool {
	if a.Settled() || !canInferStop(opt, sawContent, sawTool, err) {
		return false
	}
	if sawTool {
		a.Done(protocol.StopToolUse)
	} else {
		a.Done(protocol.StopStop)
	}
	return true
}

func canInferStop(opt Options, sawContent, sawTool bool, err error) bool {
	if !opt.InferStopOnDone || (!sawContent && !sawTool) || (err != nil && !isIncompleteStream(err)) {
		return false
	}
	witnessed, ok := opt.Witness.(interface{ SawDone() bool })
	if !ok || !witnessed.SawDone() {
		return false
	}
	return true
}

func reasoningOf(fn func(fantasy.ProviderMetadata) *ReasoningMeta, md fantasy.ProviderMetadata) *ReasoningMeta {
	if fn == nil {
		return nil
	}
	return fn(md)
}

func rawStop(w interface{ StopReason() string }) string {
	if w == nil {
		return ""
	}
	return w.StopReason()
}

// finish settles the stream at the finish part. blocks counts the content
// blocks of the message; a completed stop with none of them is an empty
// response and not a success.
func finish(a *providers.Assembler, part fantasy.StreamPart, raw string, blocks int, opt Options) {
	u := protocol.Usage{
		Input:      part.Usage.InputTokens,
		Output:     part.Usage.OutputTokens,
		CacheRead:  part.Usage.CacheReadTokens,
		CacheWrite: part.Usage.CacheCreationTokens,
	}
	u.TotalTokens = u.Input + u.Output + u.CacheRead + u.CacheWrite
	if opt.Usage != nil {
		u = opt.Usage(part)
	}
	// A finish part that carries no usage keeps the last sample. One that does
	// replaces it: the final count is the total, never an addition.
	if u != (protocol.Usage{}) {
		a.SetUsage(u)
	}
	meta := providers.Metadata{}
	if opt.Meta != nil {
		meta = opt.Meta(part)
	}
	if part.ID != "" && meta.ResponseID == nil {
		id := part.ID
		meta.ResponseID = &id
	}
	if raw != "" && meta.RawStopReason == nil {
		r := raw
		meta.RawStopReason = &r
	}
	a.SetMetadata(meta)
	if opt.MapStop == nil {
		a.Fail(protocol.StopError, "stop reason unavailable", errStopUnavailable)
		return
	}
	reason, err := opt.MapStop(raw)
	if err != nil {
		a.Fail(protocol.StopError, err.Error(), err)
		return
	}
	if reason == protocol.StopStop && blocks == 0 {
		const text = "provider returned no content"
		a.Fail(protocol.StopError, text, providers.NewFailure(providers.CodeEmptyResponse, 0, 0, text, nil))
		return
	}
	a.Done(reason)
}

func textSig(fn func(fantasy.ProviderMetadata) *string, md fantasy.ProviderMetadata) *string {
	if fn == nil {
		return nil
	}
	return fn(md)
}

func toolID(fn func(string, fantasy.ProviderMetadata) string, id string, md fantasy.ProviderMetadata) string {
	if fn == nil {
		return id
	}
	return fn(id, md)
}
