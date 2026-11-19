package fantasykit

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"

	"AskCore/internal/providers"
)

func classifyFlagged(status int, body string, flag bool) *providers.Failure {
	pe := &fantasy.ProviderError{
		Message:            "provider error",
		StatusCode:         status,
		ResponseBody:       []byte(body),
		ContextTooLargeErr: flag,
	}
	return Classify(context.Background(), pe, "", nil)
}

func TestOverflowFlagIsHonoredOnBadRequest(t *testing.T) {
	f := classifyFlagged(400, `{"error":{"message":"bad"}}`, true)
	assert.Equal(t, providers.CodeContextWindow, f.Code)
}

func TestOverflowFlagIsIgnoredOnRateLimitAndQuotaStatus(t *testing.T) {
	body := `{"error":{"message":"prompt is too long: 250000 tokens > 200000 maximum"}}`
	assert.Equal(t, providers.CodeRateLimit, classifyFlagged(429, body, true).Code)
	assert.Equal(t, providers.CodeQuota, classifyFlagged(402, body, true).Code)
}

func TestOverflowFlagIsIgnoredWhenTheTextIsQuotaOrThrottling(t *testing.T) {
	quota := `{"error":{"message":"You exceeded your current quota; too many tokens"}}`
	assert.Equal(t, providers.CodeQuota, classifyFlagged(400, quota, true).Code)
	throttle := `{"error":{"message":"ThrottlingException: Too many tokens, rate limit reached"}}`
	assert.Equal(t, providers.CodeInvalidRequest, classifyFlagged(400, throttle, true).Code)
}

func TestOverflowFlagOnPayload413IsNotOverflowWhenVetoed(t *testing.T) {
	body := `{"error":{"type":"rate_limit_error","message":"too many requests"}}`
	assert.Equal(t, providers.CodeRateLimit, classifyFlagged(413, body, true).Code)
}
