package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"AskCore/pkg/protocol"
)

// ShortHash is the first 8 hex chars of SHA-256. Deterministic. No map.
func ShortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// NormalizeAnthropicToolCallID keeps the current rune rule: a rune <= 0x7f
// that is a letter, digit, '_' or '-' stays; every other rune becomes '_';
// the result is truncated to 64 runes.
func NormalizeAnthropicToolCallID(id string, _ Model, _ protocol.AssistantMessage) string {
	runes := []rune(id)
	for i, r := range runes {
		if r <= 0x7f && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			continue
		}
		runes[i] = '_'
	}
	if len(runes) > 64 {
		runes = runes[:64]
	}
	return string(runes)
}

// NormalizeCompletionsToolCallID splits at the first "|", joins as call_item,
// and if the result is over 40 uses 31 runes plus "_" plus ShortHash.
// Provider "openai" also caps a plain id at 40.
func NormalizeCompletionsToolCallID(id string, model Model, _ protocol.AssistantMessage) string {
	call, item, hadPipe := strings.Cut(id, "|")
	out := id
	if hadPipe {
		out = call + "_" + item
	}
	if utf8.RuneCountInString(out) <= 40 {
		return out
	}
	if !hadPipe && model.Provider != ProviderOpenAI {
		return out
	}
	return string([]rune(out)[:31]) + "_" + ShortHash(out)
}

var responsesNativeProviders = map[string]struct{}{
	"openai":       {},
	"openai-codex": {},
	"opencode":     {},
}

// NormalizeResponsesToolCallID splits call_id|item_id.
// Foreign item ids in the native set become fc_<shortHash>.
// Other providers turn "|" into "_". There is no collision map.
func NormalizeResponsesToolCallID(id string, model Model, source protocol.AssistantMessage) string {
	call, item, hadPipe := strings.Cut(id, "|")
	if !hadPipe {
		if !responsesCallIDOK(id) {
			return "fc_" + ShortHash(id)
		}
		return id
	}
	if _, native := responsesNativeProviders[source.Provider]; !native {
		return strings.ReplaceAll(id, "|", "_")
	}
	if source.Model != model.ID {
		if item == "" {
			return call
		}
		return call + "|" + "fc_" + ShortHash(item)
	}
	return JoinToolCallID(call, item)
}

func responsesCallIDOK(id string) bool {
	if id == "" {
		return true
	}
	for _, r := range id {
		if r > 0x7f || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-') {
			return false
		}
	}
	return true
}
