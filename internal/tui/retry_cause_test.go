package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// A provider failure text starts with the HTTP status and holds the body of the
// answer. The note and the status name the cause in words and never show the body.
func TestRetryCauseHidesProviderBody(t *testing.T) {
	const body = `HTTP 429: {"type":"error","error":{"message":"slow down"}}`
	note := retryText(&protocol.AutoRetryStart{Attempt: 1, MaxAttempts: 5, ErrorMessage: body})
	require.Equal(t, "Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 5 scheduled.", note)
	require.Equal(t, "rate limit", retryReason(body))
	require.Equal(t, "server error", retryReason("HTTP 503 overloaded: try later"))
	require.Equal(t, "rate limit", retryReason("rate limit (HTTP 429)"), "a text without a body stays as it is")
}
