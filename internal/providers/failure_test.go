package providers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestRetryAfterSecondsAndHTTPDateAreParsed(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"whole seconds", "30", 30 * time.Second},
		{"fractional seconds", "1.5", 1500 * time.Millisecond},
		{"http date", now.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second},
		{"http date in the past", now.Add(-time.Minute).UTC().Format(http.TimeFormat), 0},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"empty", "", 0},
		{"text", "soon", 0},
		{"not finite", "Inf", 0},
		{"not a number", "NaN", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, providers.ParseRetryAfter(tc.value, now))
		})
	}
}

func TestFailureUnwrapsToSentinel(t *testing.T) {
	cause := errors.New("raw provider text")
	tests := []struct {
		code     string
		sentinel error
	}{
		{providers.CodeRateLimit, providers.ErrRateLimited},
		{providers.CodeAuth, providers.ErrAuthentication},
		{providers.CodeQuota, providers.ErrAllowanceExhausted},
		{providers.CodeInvalidRequest, providers.ErrUnsupportedRequest},
		{providers.CodeTransport, providers.ErrTransport},
		{providers.CodeStreamClosed, providers.ErrStreamIncomplete},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			err := fmt.Errorf("stream: %w", providers.NewFailure(tc.code, 0, 0, "text", cause))
			assert.ErrorIs(t, err, tc.sentinel)
			assert.ErrorIs(t, err, cause, "the cause stays reachable")
			f, ok := providers.AsFailure(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, f.Code)
		})
	}
	// A code without a sentinel still unwraps to its cause.
	f := providers.NewFailure(providers.CodeServer, 503, 0, "text", cause)
	assert.ErrorIs(t, f, cause)
	assert.NotErrorIs(t, f, providers.ErrRateLimited)
}

func TestFailureTextIsCleaned(t *testing.T) {
	f := providers.NewFailure(providers.CodeServer, 500, 0,
		"POST https://api.example.test/v1?key=SECRETQ failed: Authorization: Bearer SECRETB", nil)
	assert.NotContains(t, f.Error(), "SECRETQ")
	assert.NotContains(t, f.Error(), "SECRETB")
	assert.Contains(t, f.Error(), "https://api.example.test/v1")
}

func TestCodeOfFallsBackToUnknown(t *testing.T) {
	assert.Equal(t, providers.CodeUnknown, providers.CodeOf(errors.New("no provider facts")))
	assert.Equal(t, providers.CodeUnknown, providers.CodeOf(nil))
	assert.Equal(t, providers.CodeAuth, providers.CodeOf(fmt.Errorf("w: %w", providers.NewFailure(providers.CodeAuth, 401, 0, "x", nil))))
}

func TestClassifyProviderErrorFollowsTransportRules(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		typ     string
		code    string
		message string
		want    string
	}{
		{"401", 401, "", "", "bad key", "AUTH"},
		{"403", 403, "", "", "denied", "AUTH"},
		{"auth type without status", 0, "authentication_error", "", "bad key", "AUTH"},
		{"permission type", 0, "permission_error", "", "no", "AUTH"},
		{"402", 402, "", "", "pay up", "QUOTA"},
		{"quota wording on 429", 429, "rate_limit_error", "", "You have exceeded your current quota", "QUOTA"},
		{"usage limit reached", 429, "", "", "usage limit reached for this plan", "QUOTA"},
		{"insufficient balance", 400, "", "", "insufficient balance", "QUOTA"},
		{"subscription quota code", 429, "", "subscription_sharing_usage_limit_exceeded", "limit", "QUOTA"},
		{"429", 429, "", "", "slow down", "RATE_LIMIT"},
		{"rate limit type", 0, "rate_limit_error", "", "slow down", "RATE_LIMIT"},
		{"context window", 400, "invalid_request_error", "", "prompt is too long for this model's context window", "CONTEXT_WINDOW_EXCEEDED"},
		{"400", 400, "", "", "bad", "INVALID_REQUEST"},
		{"413", 413, "", "", "big", "INVALID_REQUEST"},
		{"invalid request type", 0, "invalid_request_error", "", "bad", "INVALID_REQUEST"},
		{"500", 500, "", "", "boom", "SERVER"},
		{"529", 529, "overloaded_error", "", "busy", "SERVER"},
		{"api error in band", 0, "api_error", "", "boom", "SERVER"},
		{"overloaded in band", 0, "overloaded_error", "", "busy", "SERVER"},
		{"unknown in band", 0, "mystery_error", "", "odd", "SERVER"},
		{"418", 418, "", "", "teapot", "HTTP_418"},
		{"404", 404, "", "", "gone", "HTTP_404"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, providers.ClassifyProvider(tc.status, tc.typ, tc.code, tc.message))
		})
	}
}

func TestCodeOfMapsSentinelsAndAssemblerFailTypesThem(t *testing.T) {
	assert.Equal(t, providers.CodeAuth, providers.CodeOf(fmt.Errorf("%w: no key", providers.ErrAuthentication)))
	assert.Equal(t, providers.CodeQuota, providers.CodeOf(fmt.Errorf("x: %w", providers.ErrAllowanceExhausted)))
	assert.Equal(t, providers.CodeInvalidRequest, providers.CodeOf(fmt.Errorf("%w: bad tool choice", providers.ErrUnsupportedRequest)))
	assert.Equal(t, providers.CodeUnknown, providers.CodeOf(errors.New("no sentinel, no provider fact")))

	// A failure before the request, such as a missing key, is a typed failure.
	s := providers.NewStream(context.Background(), 4, protocol.AssistantMessage{}, func(a *providers.Assembler) {
		a.Fail(protocol.StopError, "no API key", fmt.Errorf("%w: no API key", providers.ErrAuthentication))
	})
	for range s.Events() {
	}
	_, err := s.Result(context.Background())
	f, ok := providers.AsFailure(err)
	require.True(t, ok)
	assert.Equal(t, providers.CodeAuth, f.Code)
	assert.ErrorIs(t, err, providers.ErrAuthentication)

	// An error with no sentinel stays a plain error.
	s = providers.NewStream(context.Background(), 4, protocol.AssistantMessage{}, func(a *providers.Assembler) {
		a.Fail(protocol.StopError, "odd", errors.New("odd"))
	})
	for range s.Events() {
	}
	_, err = s.Result(context.Background())
	_, typed := providers.AsFailure(err)
	assert.False(t, typed)
}
