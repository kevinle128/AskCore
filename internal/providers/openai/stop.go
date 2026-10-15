package openai

import (
	"errors"
	"fmt"

	"charm.land/fantasy"
	fopenai "charm.land/fantasy/providers/openai"

	"AskCore/internal/providers"
	"AskCore/internal/providers/fantasykit"
	"AskCore/pkg/protocol"
)

func mapCompletionsStop(raw string) (protocol.StopReason, error) {
	switch raw {
	case "stop", "end":
		return protocol.StopStop, nil
	case "length":
		return protocol.StopLength, nil
	case "function_call", "tool_calls":
		return protocol.StopToolUse, nil
	case "":
		return "", errors.New("stop reason unavailable")
	default:
		return "", fmt.Errorf("stop reason %s", raw)
	}
}

func mapResponsesStop(raw string) (protocol.StopReason, error) {
	switch raw {
	case "stop", "completed", "":
		return protocol.StopStop, nil
	case "length", "max_tokens", "max_output_tokens":
		return protocol.StopLength, nil
	case "tool-calls", "tool_calls", "function_call":
		return protocol.StopToolUse, nil
	case "content-filter", "content_filter":
		return "", errors.New("stop reason content_filter")
	default:
		if raw == string(fantasy.FinishReasonOther) || raw == string(fantasy.FinishReasonError) {
			return "", fmt.Errorf("stop reason %s", raw)
		}
		return "", fmt.Errorf("stop reason %s", raw)
	}
}

func completionsReasoning(md fantasy.ProviderMetadata) *fantasykit.ReasoningMeta {
	sig := "reasoning_content"
	if v, ok := md[fopenai.Name].(*fopenai.ProviderMetadata); ok {
		_ = v.ExtraField(reasoningFieldKey, &sig)
	}
	return &fantasykit.ReasoningMeta{Signature: sig}
}

func responsesReasoning(md fantasy.ProviderMetadata) *fantasykit.ReasoningMeta {
	meta := fopenai.GetReasoningMetadata(fantasy.ProviderOptions(md))
	if meta == nil {
		return nil
	}
	r := providers.ReasoningReplay{Type: "reasoning", ID: meta.ItemID, Status: "completed"}
	if meta.EncryptedContent != nil {
		r.EncryptedContent = *meta.EncryptedContent
	}
	for _, s := range meta.Summary {
		r.Summary = append(r.Summary, providers.ReasoningSummary{Type: "summary_text", Text: s})
	}
	sig := providers.EncodeThinkingSignature(r)
	return &fantasykit.ReasoningMeta{Signature: sig}
}

func responsesText(md fantasy.ProviderMetadata) *string {
	if md == nil {
		return nil
	}
	v, ok := md[fopenai.Name]
	if !ok {
		return nil
	}
	meta, _ := v.(*fopenai.ResponsesTextMetadata)
	if meta == nil || meta.ItemID == "" {
		return nil
	}
	sig := providers.EncodeTextSignature(meta.ItemID, meta.Phase)
	if sig == "" {
		return nil
	}
	return &sig
}

func responsesToolID(id string, md fantasy.ProviderMetadata) string {
	if md == nil {
		return id
	}
	v, ok := md[fopenai.Name]
	if !ok {
		return id
	}
	meta, _ := v.(*fopenai.ResponsesToolCallMetadata)
	if meta == nil || meta.ItemID == "" {
		return id
	}
	return providers.JoinToolCallID(id, meta.ItemID)
}

func responsesRawStop(part fantasy.StreamPart, witnessed string) string {
	if md := responsesProviderMeta(part.ProviderMetadata); md != nil {
		if md.RawFinishReason != "" {
			return md.RawFinishReason
		}
		if md.ResponseStatus != "" && md.ResponseStatus != "completed" {
			return md.ResponseStatus
		}
	}
	if part.FinishReason != "" && part.FinishReason != fantasy.FinishReasonUnknown {
		return string(part.FinishReason)
	}
	return witnessed
}

func responsesProviderMeta(md fantasy.ProviderMetadata) *fopenai.ResponsesProviderMetadata {
	if md == nil {
		return nil
	}
	v, ok := md[fopenai.Name]
	if !ok {
		return nil
	}
	meta, _ := v.(*fopenai.ResponsesProviderMetadata)
	return meta
}

func responsesFinishMeta(part fantasy.StreamPart) providers.Metadata {
	meta := providers.Metadata{}
	if rm := responsesProviderMeta(part.ProviderMetadata); rm != nil {
		if rm.ResponseID != "" {
			id := rm.ResponseID
			meta.ResponseID = &id
		}
		if rm.RawFinishReason != "" {
			r := rm.RawFinishReason
			meta.RawStopReason = &r
		} else if rm.ResponseStatus != "" {
			r := rm.ResponseStatus
			meta.RawStopReason = &r
		}
	}
	return meta
}
