package providers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestOverflowPatternsMatchProviderTexts(t *testing.T) {
	tests := []struct{ provider, text string }{
		{"anthropic", "prompt is too long: 250000 tokens > 200000 maximum"},
		{"z.ai", "Prompt too long"},
		{"z.ai cn", "Prompt exceeds max length"},
		{"bedrock", "Input is too long for requested model"},
		{"openai", "Your input exceeds the context window of this model"},
		{"litellm", "This model's maximum context length is 8192 tokens. exceeds the maximum context length of 8,192 tokens"},
		{"litellm paren", "exceeds model's maximum context length (8192)"},
		{"gemini", "The input token count (1196265) exceeds the maximum number of tokens allowed (1048575)"},
		{"xai", "maximum prompt length is 131072 but the request contains 200000"},
		{"groq", "Please reduce the length of the messages or completion"},
		{"openrouter", "This endpoint's maximum context length is 128000 tokens"},
		{"poolside", "exceeds the maximum allowed input length of 65,536 tokens"},
		{"together", "input (9000 tokens) is longer than the model's context length (8192 tokens)"},
		{"copilot", "prompt token count of 140000 exceeds the limit of 128000"},
		{"llama.cpp", "the request exceeds the available context size"},
		{"lm studio", "tokens to keep from the initial prompt is greater than the context length"},
		{"minimax", "invalid params, context window exceeds limit"},
		{"kimi", "Your request exceeded model token limit: 262144 (requested: 300000)"},
		{"mistral", "Prompt contains 40000 tokens ... too large for model with 32768 maximum context length"},
		{"ds4", "Prompt has 5000 tokens, but the configured context size is 4096 tokens"},
		{"z.ai finish", "model_context_window_exceeded"},
		{"ollama", "prompt too long; exceeded max context length by 100 tokens"},
		{"dashscope", "Range of input length should be [1, 30720]"},
		{"generic code", "context_length_exceeded"},
		{"generic tokens", "too many tokens in the prompt"},
		{"generic limit", "token limit exceeded"},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			assert.Equal(t, providers.CodeContextWindow, providers.ClassifyProvider(400, "", "", tc.text))
		})
	}
}

func TestOverflowVetoKeepsRateLimit(t *testing.T) {
	assert.Equal(t, providers.CodeInvalidRequest,
		providers.ClassifyProvider(400, "", "", "ThrottlingException: Too many tokens, rate limit reached"))
	assert.Equal(t, providers.CodeInvalidRequest,
		providers.ClassifyProvider(400, "", "", "too many requests, too many tokens"))
	assert.NotEqual(t, providers.CodeContextWindow,
		providers.ClassifyProvider(400, "", "", "Throttling error: Too many tokens, please wait"))
	assert.NotEqual(t, providers.CodeContextWindow,
		providers.ClassifyProvider(400, "", "", "Service unavailable: prompt is too long"))
}

func TestOverflowFlagVetoedByStatusTypeAndText(t *testing.T) {
	assert.True(t, providers.OverflowFlagVetoed(429, "", "", "anything"))
	assert.True(t, providers.OverflowFlagVetoed(402, "", "", "anything"))
	assert.True(t, providers.OverflowFlagVetoed(400, "", "", "usage limit reached"))
	assert.True(t, providers.OverflowFlagVetoed(400, "", "", "Too many requests"))
	assert.False(t, providers.OverflowFlagVetoed(400, "invalid_request_error", "", "prompt is too long"))
	assert.False(t, providers.OverflowFlagVetoed(413, "", "", ""))
}

func TestUsageOverflowThresholds(t *testing.T) {
	m := providers.Model{ContextWindow: 1000}
	msg := func(stop protocol.StopReason, in, out, cr, cw int64) protocol.AssistantMessage {
		return protocol.AssistantMessage{
			StopReason: stop,
			Usage:      protocol.Usage{Input: in, Output: out, CacheRead: cr, CacheWrite: cw},
		}
	}
	tests := []struct {
		name  string
		model providers.Model
		msg   protocol.AssistantMessage
		want  bool
	}{
		{"stop at window", m, msg(protocol.StopStop, 1000, 5, 0, 0), false},
		{"stop above window", m, msg(protocol.StopStop, 1001, 5, 0, 0), true},
		{"stop above window by cache read", m, msg(protocol.StopStop, 600, 5, 401, 0), true},
		{"stop above window by cache write", m, msg(protocol.StopStop, 600, 5, 0, 401), true},
		{"length no output at 99 percent", m, msg(protocol.StopLength, 990, 0, 0, 0), true},
		{"length no output at 98 percent", m, msg(protocol.StopLength, 980, 0, 0, 0), false},
		{"length with output", m, msg(protocol.StopLength, 1000, 1, 0, 0), false},
		{"tool use above window", m, msg(protocol.StopToolUse, 2000, 5, 0, 0), false},
		{"error above window", m, msg(protocol.StopError, 2000, 0, 0, 0), false},
		{"unknown window", providers.Model{}, msg(protocol.StopStop, 2000, 5, 0, 0), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, providers.UsageOverflow(tc.model, tc.msg))
		})
	}
}

func TestOverflowTextRuleHonoursStatusAndTokenLimits(t *testing.T) {
	tests := []struct {
		name   string
		status int
		text   string
		want   string
	}{
		{"image count is not a context limit", 400, "Your request exceeds the limit of 20 images", providers.CodeInvalidRequest},
		{"item count is not a context limit", 400, "Request exceeds the limit of 128 items", providers.CodeInvalidRequest},
		{"output token parameter is not a context limit", 400, "Too many tokens requested for output", providers.CodeInvalidRequest},
		{"overloaded 503 stays a server error", 503, "Overloaded: too many tokens in flight", providers.CodeServer},
		{"500 never overflows", 500, "Prompt exceeds max length", providers.CodeServer},
		{"500 with a token limit text never overflows", 500, "prompt is too long: 250000 tokens > 200000 maximum", providers.CodeServer},
		{"copilot token limit at 400", 400, "prompt token count of 140000 exceeds the limit of 128000", providers.CodeContextWindow},
		{"413 with token text", 413, "prompt is too long", providers.CodeContextWindow},
		{"422 with token text", 422, "prompt is too long", providers.CodeContextWindow},
		{"in-band error", 0, "prompt is too long", providers.CodeContextWindow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, providers.ClassifyProvider(tc.status, "", "", tc.text))
		})
	}
}

func TestInBandThrottlingIsNotOverflow(t *testing.T) {
	const text = "Too many tokens, please wait before trying again."
	assert.NotEqual(t, providers.CodeContextWindow, providers.ClassifyProvider(0, "ThrottlingException", "", text))
	assert.NotEqual(t, providers.CodeContextWindow, providers.ClassifyProvider(0, "", "Throttling", text))
	assert.True(t, providers.OverflowFlagVetoed(0, "ThrottlingException", "", text))
	assert.True(t, providers.OverflowFlagVetoed(0, "", "Throttling", text))
}

func TestOverflowFlagVetoedForServerStatus(t *testing.T) {
	assert.True(t, providers.OverflowFlagVetoed(503, "", "", "prompt is too long"))
	assert.True(t, providers.OverflowFlagVetoed(500, "", "", "prompt is too long"))
}
