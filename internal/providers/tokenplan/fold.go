package tokenplan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/fantasy"
	fanthropic "charm.land/fantasy/providers/anthropic"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

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
}

func fold(a *providers.Assembler, parts fantasy.StreamResponse, reqCtx, runCtx context.Context, idle error, witness *stopWitness) {
	texts := map[string]*textBlock{}
	thinks := map[string]*thinkBlock{}
	tools := map[string]*toolBlock{}

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
				b.text += part.Delta
				a.TextDelta(b.index, part.Delta)
			}
		case fantasy.StreamPartTypeTextEnd:
			if b := texts[part.ID]; b != nil {
				a.TextEnd(b.index, b.text, nil)
			}
		case fantasy.StreamPartTypeReasoningStart:
			// Delay ThinkingStart so an empty disabled-mode block never opens.
			tb := &thinkBlock{index: -1}
			if meta := reasoningMeta(part.ProviderMetadata); meta != nil && meta.RedactedData != "" {
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
				tb.text += part.Delta
				if tb.index < 0 {
					tb.index = a.ThinkingStart("", tb.sig, tb.redacted)
				}
				a.ThinkingDelta(tb.index, part.Delta)
			}
			if meta := reasoningMeta(part.ProviderMetadata); meta != nil {
				sig := meta.Signature
				tb.sig = &sig
			}
		case fantasy.StreamPartTypeReasoningEnd:
			tb := thinks[part.ID]
			if tb == nil {
				continue
			}
			if meta := reasoningMeta(part.ProviderMetadata); meta != nil {
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
			i := a.ToolStart(part.ID, part.ToolCallName, nil, nil, nil)
			tools[part.ID] = &toolBlock{index: i}
		case fantasy.StreamPartTypeToolInputDelta:
			if b := tools[part.ID]; b != nil {
				a.ToolDelta(b.index, part.ToolCallInput)
			}
		case fantasy.StreamPartTypeToolInputEnd:
		case fantasy.StreamPartTypeToolCall:
			if b := tools[part.ID]; b != nil {
				a.ToolEnd(b.index, nil)
			}
		case fantasy.StreamPartTypeSource, fantasy.StreamPartTypeToolResult:
		case fantasy.StreamPartTypeError:
			failStream(a, reqCtx, runCtx, idle, part.Error)
			return
		case fantasy.StreamPartTypeFinish:
			finishStream(a, part, witness)
			return
		}
	}
}

func finishStream(a *providers.Assembler, part fantasy.StreamPart, witness *stopWitness) {
	u := protocol.Usage{
		Input:      part.Usage.InputTokens,
		Output:     part.Usage.OutputTokens,
		CacheRead:  part.Usage.CacheReadTokens,
		CacheWrite: part.Usage.CacheCreationTokens,
	}
	u.TotalTokens = u.Input + u.Output + u.CacheRead + u.CacheWrite
	a.SetUsage(u)
	meta := providers.Metadata{}
	if part.ID != "" {
		id := part.ID
		meta.ResponseID = &id
	}
	raw := ""
	if witness != nil {
		raw = witness.StopReason()
	}
	if raw != "" {
		r := raw
		meta.RawStopReason = &r
	}
	a.SetMetadata(meta)
	switch raw {
	case "end_turn", "pause_turn", "stop_sequence":
		a.Done(protocol.StopStop)
	case "max_tokens":
		a.Done(protocol.StopLength)
	case "tool_use":
		a.Done(protocol.StopToolUse)
	case "":
		a.Fail(protocol.StopError, "stop reason unavailable", errors.New("stop reason unavailable"))
	default:
		a.Fail(protocol.StopError, "stop reason "+raw, fmt.Errorf("stop reason %s", raw))
	}
}

func failStream(a *providers.Assembler, reqCtx, runCtx context.Context, idle error, err error) {
	if reqCtx != nil && reqCtx.Err() != nil {
		a.Fail(protocol.StopAborted, "Request was aborted", reqCtx.Err())
		return
	}
	if idle != nil && (errors.Is(err, idle) || (runCtx != nil && errors.Is(context.Cause(runCtx), idle))) {
		a.Fail(protocol.StopError, "idle timeout", idle)
		return
	}
	if err != nil && (errors.Is(err, io.ErrUnexpectedEOF) || isIncompleteStream(err)) {
		a.Fail(protocol.StopError, providers.ErrStreamIncomplete.Error(), providers.ErrStreamIncomplete)
		return
	}
	if err == nil {
		err = errors.New("stream error")
	}
	a.Fail(protocol.StopError, httpMessage(err), err)
}

func isIncompleteStream(err error) bool {
	var pe *fantasy.ProviderError
	if errors.As(err, &pe) && errors.Is(pe.Cause, io.ErrUnexpectedEOF) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

func reasoningMeta(md fantasy.ProviderMetadata) *fanthropic.ReasoningOptionMetadata {
	if md == nil {
		return nil
	}
	v, ok := md[fanthropic.Name]
	if !ok {
		return nil
	}
	meta, _ := v.(*fanthropic.ReasoningOptionMetadata)
	return meta
}

func httpMessage(err error) string {
	var pe *fantasy.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode == 0 {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	body := extractJSONBody(pe.ResponseBody)
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil && (payload.Code != "" || payload.Message != "") {
		return fmt.Sprintf("HTTP %d %s: %s", pe.StatusCode, payload.Code, payload.Message)
	}
	if len(body) > 512 {
		body = body[:512]
	}
	return fmt.Sprintf("HTTP %d: %s", pe.StatusCode, body)
}

func extractJSONBody(raw []byte) []byte {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return bytes.TrimSpace(raw[i+4:])
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return bytes.TrimSpace(raw[i+2:])
	}
	return bytes.TrimSpace(raw)
}
