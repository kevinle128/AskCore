package providers

import (
	"fmt"

	"AskCore/pkg/protocol"
)

// ThinkingLevelMap is Pi's map.
//
//	missing key   send the level name. xhigh and max are unsupported unless
//	              a key is present.
//	nil pointer   level unsupported. Clamp skips it.
//	non-nil       send that word.
type ThinkingLevelMap map[protocol.ThinkingLevel]*string

// Price is micro-USD per 1_000_000 tokens.
type Price struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Tier applies when input+cacheRead+cacheWrite > Above. The base tier has
// Above 0. Tiers are sorted by Above ascending.
type Tier struct {
	Above int64
	Price Price
}

// Cost is the price schedule on a model. The field name on Model is Cost.
type Cost struct {
	Tiers []Tier
}

// ServiceTier is the Responses service_tier echoed on the terminal response.
// The empty value means the multiplier is 1. Ask does not send a request tier.
type ServiceTier string

const (
	TierFlex     ServiceTier = "flex"
	TierPriority ServiceTier = "priority"
	TierFast     ServiceTier = "fast"
)

// Model is one catalog row and the only policy object a request carries.
// SamplingParams is stored for H7. H4 adapters do not read it.
type Model struct {
	ID               string
	Name             string
	API              API
	Provider         string
	Reasoning        bool
	Input            []string
	ContextWindow    int
	MaxTokens        int
	BaseURL          string
	Headers          map[string]string
	ThinkingLevelMap ThinkingLevelMap
	Cost             Cost
	Compat           Compat
	SamplingParams   map[string]any
}

func (m Model) Anthropic() (AnthropicCompat, bool) {
	c, ok := m.Compat.(AnthropicCompat)
	return c, ok && m.API == APIAnthropicMessages
}

func (m Model) Completions() (CompletionsCompat, bool) {
	c, ok := m.Compat.(CompletionsCompat)
	return c, ok && m.API == APIOpenAICompletions
}

func (m Model) Responses() (ResponsesCompat, bool) {
	c, ok := m.Compat.(ResponsesCompat)
	return c, ok && m.API == APIOpenAIResponses
}

// Validate checks catalog invariants. Adapters do not call it on the hot path.
func Validate(m Model) error {
	if m.ID == "" || m.Provider == "" || m.API == "" {
		return fmt.Errorf("model identity is incomplete: provider %q id %q api %q", m.Provider, m.ID, m.API)
	}
	if !compatMatchesAPI(m.Compat, m.API) {
		return fmt.Errorf("model %s/%s: compat %T does not match API %s", m.Provider, m.ID, m.Compat, m.API)
	}
	return nil
}

func compatMatchesAPI(c Compat, api API) bool {
	switch api {
	case APIAnthropicMessages:
		_, ok := c.(AnthropicCompat)
		return ok
	case APIOpenAICompletions:
		_, ok := c.(CompletionsCompat)
		return ok
	case APIOpenAIResponses:
		_, ok := c.(ResponsesCompat)
		return ok
	default:
		return c == nil
	}
}
