package providers

import (
	"AskCore/pkg/protocol"
	"fmt"
	"strings"
)

func catalog() []Model {
	return []Model{TokenPlanMessages(), TokenPlanCompletions(), OpenAIGPT55(), OpenAIGPT56Sol(), AnthropicSonnet46(), XAIGrok47()}
}

// AvailableModels returns independent rows from the compiled model catalog.
func AvailableModels() []Model { return catalog() }

// TokenPlanMessages is the H3 Token Plan row on the Anthropic messages wire.
func TokenPlanMessages() Model {
	return Model{
		ID:            ModelDeepSeekFlash,
		Name:          ModelDeepSeekFlash,
		API:           APIAnthropicMessages,
		Provider:      ProviderTokenPlan,
		Reasoning:     true,
		Input:         []string{"text", "image"},
		ContextWindow: 1_000_000,
		MaxTokens:     384_000,
		BaseURL:       TokenPlanMessagesURL,
		Compat:        AnthropicCompat{},
	}
}

// TokenPlanCompletions is deepseek-v4.1-flash on compatible-mode/v1.
func TokenPlanCompletions() Model {
	return Model{
		ID:            ModelDeepSeekFlash,
		Name:          ModelDeepSeekFlash,
		API:           APIOpenAICompletions,
		Provider:      ProviderTokenPlan,
		Reasoning:     true,
		Input:         []string{"text", "image"},
		ContextWindow: 1_000_000,
		MaxTokens:     384_000,
		BaseURL:       TokenPlanCompletionsURL,
		Compat: CompletionsCompat{
			ThinkingFormat:        "qwen",
			SupportsDeveloperRole: false,
			SupportsStore:         false,
			SupportsFinishReason:  true,
		},
	}
}

// OpenAIGPT55 is gpt-5.5 on openai-responses.
//
// Prices are micro-USD per 1_000_000 tokens from the public GPT-5.5 model
// page (https://developers.openai.com/api/docs/models/gpt-5.5), fetched
// 2026-10-05: $5 input, $0.50 cached input, $30 output; prompts above 272k
// input tokens are 2x input, 2x cached input, and 1.5x output. Cache write
// is not listed on that page or the pricing table, so it stays 0.
func OpenAIGPT55() Model {
	const (
		input     int64 = 5_000_000
		output    int64 = 30_000_000
		cacheRead int64 = 500_000
	)
	return Model{
		ID:            ModelGPT55,
		Name:          ModelGPT55,
		API:           APIOpenAIResponses,
		Provider:      ProviderOpenAI,
		Reasoning:     true,
		Input:         []string{"text", "image"},
		ContextWindow: 1_050_000,
		MaxTokens:     128_000,
		BaseURL:       OpenAIURL,
		Cost: Cost{Tiers: []Tier{
			{Above: 0, Price: Price{Input: input, Output: output, CacheRead: cacheRead}},
			{Above: LongContextAbove, Price: Price{Input: input * 2, Output: output * 3 / 2, CacheRead: cacheRead * 2}},
		}},
		Compat: ResponsesCompat{Store: false, SupportsLongCacheRetention: true, SupportsMaxOutputTokens: true},
	}
}

// OpenAIGPT56Sol uses the native Responses endpoint.
// Limits, reasoning levels and prices: https://developers.openai.com/api/docs/models/gpt-5.6-sol (2026-10-06).
func OpenAIGPT56Sol() Model {
	return Model{
		ID: ModelGPT56Sol, Name: "GPT-5.6 Sol", API: APIOpenAIResponses, Provider: ProviderOpenAI,
		Reasoning: true, Input: []string{"text", "image"}, ContextWindow: 1_050_000, MaxTokens: 128_000, BaseURL: OpenAIURL,
		ThinkingLevelMap: ThinkingLevelMap{protocol.ThinkingOff: stringPointer("none"), protocol.ThinkingMinimal: nil, protocol.ThinkingXHigh: stringPointer("xhigh"), protocol.ThinkingMax: stringPointer("max")},
		Cost: Cost{Tiers: []Tier{
			{Above: 0, Price: Price{Input: 4_000_000, Output: 20_000_000, CacheRead: 400_000, CacheWrite: 5_000_000}},
			{Above: LongContextAbove, Price: Price{Input: 8_000_000, Output: 30_000_000, CacheRead: 800_000, CacheWrite: 10_000_000}},
		}},
		Compat: ResponsesCompat{Store: false, SupportsLongCacheRetention: true, SupportsMaxOutputTokens: true},
	}
}

// Find returns the one row for ref.
// API set: exact triple. API empty: the unique provider+id row.
// Two rows (Token Plan) with API empty return an error that names both APIs.
func Find(ref Ref) (Model, error) {
	var matches []Model
	for _, m := range catalog() {
		if m.Provider != ref.Provider || m.ID != ref.ID {
			continue
		}
		if ref.API == "" || m.API == ref.API {
			matches = append(matches, m)
		}
	}
	switch len(matches) {
	case 0:
		if ref.API != "" {
			return Model{}, fmt.Errorf("unknown model %s/%s API %s", ref.Provider, ref.ID, ref.API)
		}
		return Model{}, fmt.Errorf("unknown model %s/%s", ref.Provider, ref.ID)
	case 1:
		return matches[0], nil
	default:
		apis := make([]string, 0, len(matches))
		for _, m := range matches {
			apis = append(apis, string(m.API))
		}
		return Model{}, fmt.Errorf("ambiguous model %s/%s: %s", ref.Provider, ref.ID, strings.Join(apis, " and "))
	}
}

// AnthropicSonnet46 uses the native Messages endpoint.
// Limits and inputs: https://platform.claude.com/docs/en/models/sonnet-4-6/overview (2026-10-06).
func AnthropicSonnet46() Model {
	return Model{ID: ModelClaudeSonnet46, Name: "Claude Sonnet 4.6", Provider: ProviderAnthropic, API: APIAnthropicMessages, BaseURL: AnthropicURL,
		Reasoning: true, Input: []string{"text", "image"}, ContextWindow: 1_000_000, MaxTokens: 128_000, Compat: AnthropicCompat{}}
}

// XAIGrok47 uses the native Responses endpoint.
// Metadata: https://docs.x.ai/developers/models/grok-4.7 and https://pi.dev/models/xai/grok-4-7 (2026-10-06).
func XAIGrok47() Model {
	return Model{ID: ModelGrok47, Name: "Grok 4.7", Provider: ProviderXAI, API: APIOpenAIResponses, BaseURL: XAIURL,
		Reasoning: true, Input: []string{"text", "image"}, ContextWindow: 500_000, MaxTokens: 500_000,
		ThinkingLevelMap: ThinkingLevelMap{protocol.ThinkingOff: nil, protocol.ThinkingMinimal: nil, protocol.ThinkingXHigh: stringPointer("xhigh"), protocol.ThinkingMax: nil},
		Compat:           ResponsesCompat{Store: false, SupportsMaxOutputTokens: true}}
}

func stringPointer(s string) *string { return &s }
