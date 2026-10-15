package providers

import (
	"encoding/json"
	"strings"
)

// TextReplay is TextSignature for Responses: {"v":1,"id":"...","phase":"..."}.
// Phase is omitted when empty. A legacy plain string is an id only.
type TextReplay struct {
	V     int    `json:"v"`
	ID    string `json:"id"`
	Phase string `json:"phase,omitempty"`
}

func EncodeTextSignature(id, phase string) string {
	b, err := json.Marshal(TextReplay{V: 1, ID: id, Phase: phase})
	if err != nil {
		return ""
	}
	return string(b)
}

func DecodeTextSignature(sig string) (TextReplay, bool) {
	var r TextReplay
	if err := json.Unmarshal([]byte(sig), &r); err != nil {
		if sig == "" {
			return TextReplay{}, false
		}
		return TextReplay{ID: sig}, true
	}
	return r, true
}

// ReasoningReplay is ThinkingSignature for Responses: the reasoning item JSON.
type ReasoningReplay struct {
	Type             string             `json:"type"`
	ID               string             `json:"id"`
	EncryptedContent string             `json:"encrypted_content,omitempty"`
	Summary          []ReasoningSummary `json:"summary,omitempty"`
	Status           string             `json:"status,omitempty"`
}

type ReasoningSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func EncodeThinkingSignature(r ReasoningReplay) string {
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return string(b)
}

func DecodeThinkingSignature(sig string) (ReasoningReplay, bool) {
	var r ReasoningReplay
	if err := json.Unmarshal([]byte(sig), &r); err != nil {
		return ReasoningReplay{}, false
	}
	return r, true
}

func SplitToolCallID(id string) (callID, itemID string) {
	callID, itemID, _ = strings.Cut(id, "|")
	return callID, itemID
}

func JoinToolCallID(callID, itemID string) string {
	if itemID == "" {
		return callID
	}
	return callID + "|" + itemID
}

// DropResponsesItemID drops the item id when the source model differs from
// the target or the prefix is not fc_ / ctc_ for that item type.
func DropResponsesItemID(itemID, itemType string, source, target Model) bool {
	if source.ID != target.ID || source.Provider != target.Provider || source.API != target.API {
		return true
	}
	switch itemType {
	case "custom_tool_call":
		return !strings.HasPrefix(itemID, "ctc_")
	case "function_call":
		return !strings.HasPrefix(itemID, "fc_")
	default:
		return !strings.HasPrefix(itemID, "fc_") && !strings.HasPrefix(itemID, "ctc_")
	}
}
