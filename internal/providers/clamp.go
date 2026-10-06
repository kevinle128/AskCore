package providers

import "AskCore/pkg/protocol"

var thinkingRank = []protocol.ThinkingLevel{
	protocol.ThinkingOff,
	protocol.ThinkingMinimal,
	protocol.ThinkingLow,
	protocol.ThinkingMedium,
	protocol.ThinkingHigh,
	protocol.ThinkingXHigh,
	protocol.ThinkingMax,
}

// ClampThinkingLevel walks from level down to off and returns the first
// supported level. A model with Reasoning false returns off.
// xhigh and max are unsupported unless the map has a non-nil entry.
// The second call is idempotent.
func ClampThinkingLevel(m Model, level protocol.ThinkingLevel) protocol.ThinkingLevel {
	if !m.Reasoning {
		return protocol.ThinkingOff
	}
	if level == "" {
		level = protocol.ThinkingOff
	}
	start := -1
	for i, l := range thinkingRank {
		if l == level {
			start = i
			break
		}
	}
	if start < 0 {
		if thinkingSupported(m, level) {
			return level
		}
		start = len(thinkingRank) - 1
	}
	for i := start; i >= 0; i-- {
		if thinkingSupported(m, thinkingRank[i]) {
			return thinkingRank[i]
		}
	}
	return protocol.ThinkingOff
}

func thinkingSupported(m Model, level protocol.ThinkingLevel) bool {
	word, ok := m.ThinkingLevelMap[level]
	if !ok {
		return level != protocol.ThinkingXHigh && level != protocol.ThinkingMax
	}
	return word != nil
}
