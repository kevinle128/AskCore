package openai

import (
	"charm.land/fantasy"
	fopenai "charm.land/fantasy/providers/openai"
	openaisdk "github.com/charmbracelet/openai-go"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func foldUsage(m providers.Model, tier providers.ServiceTier) func(fantasy.StreamPart) protocol.Usage {
	return func(part fantasy.StreamPart) protocol.Usage {
		if t := serviceTierOf(part); t != "" {
			tier = t
		}
		raw := providers.RawUsage{
			InputTokens:      part.Usage.InputTokens + part.Usage.CacheReadTokens + part.Usage.CacheCreationTokens,
			OutputTokens:     part.Usage.OutputTokens,
			CachedTokens:     part.Usage.CacheReadTokens,
			CacheWriteTokens: part.Usage.CacheCreationTokens,
			TotalTokens:      part.Usage.TotalTokens,
		}
		if part.Usage.ReasoningTokens > 0 {
			r := part.Usage.ReasoningTokens
			raw.ReasoningTokens = &r
		}
		u := providers.NormalizeUsage(raw)
		u.Cost = providers.CalculateCost(m, u, tier)
		return u
	}
}

func serviceTierOf(part fantasy.StreamPart) providers.ServiceTier {
	md := responsesProviderMeta(part.ProviderMetadata)
	if md == nil || md.ServiceTier == "" {
		return ""
	}
	return providers.ServiceTier(md.ServiceTier)
}

const usageCtxKey = "ask_usage"

// keepStreamUsage ignores a usage-less chunk so an empty-choices heartbeat
// cannot zero tokens already seen. The host often sends usage after that.
func keepStreamUsage(chunk openaisdk.ChatCompletionChunk, ctx map[string]any, metadata fantasy.ProviderMetadata) (fantasy.Usage, fantasy.ProviderMetadata) {
	if chunk.Usage.PromptTokens == 0 && chunk.Usage.CompletionTokens == 0 && chunk.Usage.TotalTokens == 0 {
		if prev, ok := ctx[usageCtxKey].(fantasy.Usage); ok {
			return prev, metadata
		}
		return fantasy.Usage{}, metadata
	}
	u, md := fopenai.DefaultStreamUsageFunc(chunk, ctx, metadata)
	if ctx != nil {
		ctx[usageCtxKey] = u
	}
	return u, md
}
