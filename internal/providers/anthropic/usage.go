package anthropic

import (
	"charm.land/fantasy"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// foldUsage copies the token counters and prices them with the model row.
// Anthropic reports input without the cached tokens, so the counters map to the
// protocol split without a subtraction. A model with no price gets cost 0;
// Model.PriceKnown tells the caller that the zero is not a real price.
func foldUsage(m providers.Model) func(fantasy.StreamPart) protocol.Usage {
	return func(part fantasy.StreamPart) protocol.Usage {
		u := protocol.Usage{
			Input:      part.Usage.InputTokens,
			Output:     part.Usage.OutputTokens,
			CacheRead:  part.Usage.CacheReadTokens,
			CacheWrite: part.Usage.CacheCreationTokens,
		}
		u.TotalTokens = u.Input + u.Output + u.CacheRead + u.CacheWrite
		u.Cost = providers.CalculateCost(m, u, "")
		return u
	}
}
