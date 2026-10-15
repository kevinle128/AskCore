package anthropic

import (
	"errors"
	"fmt"

	"charm.land/fantasy"
	fanthropic "charm.land/fantasy/providers/anthropic"

	"AskCore/internal/providers/fantasykit"
	"AskCore/pkg/protocol"
)

func mapStop(raw string) (protocol.StopReason, error) {
	switch raw {
	case "end_turn", "pause_turn", "stop_sequence":
		return protocol.StopStop, nil
	case "max_tokens":
		return protocol.StopLength, nil
	case "tool_use":
		return protocol.StopToolUse, nil
	case "":
		return "", errors.New("stop reason unavailable")
	default:
		return "", fmt.Errorf("stop reason %s", raw)
	}
}

func reasoningMeta(md fantasy.ProviderMetadata) *fantasykit.ReasoningMeta {
	if md == nil {
		return nil
	}
	v, ok := md[fanthropic.Name]
	if !ok {
		return nil
	}
	meta, _ := v.(*fanthropic.ReasoningOptionMetadata)
	if meta == nil {
		return nil
	}
	return &fantasykit.ReasoningMeta{Signature: meta.Signature, RedactedData: meta.RedactedData}
}
