package sessions_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
)

func TestRetryEntryIsLogOnlyAndAddsNoMessage(t *testing.T) {
	log := &sessions.MemoryLog{}
	scheduled := sessions.RetryScheduled{RetryID: "retry", CycleID: "cycle", Turn: 1, Provider: "provider", PolicyKey: "default", Retry: 1, MaxRetries: 5, DelayMs: 500, Failure: sessions.Failure{Code: "RATE_LIMIT", Text: "slow down"}}
	started := sessions.RetryStarted{RetryID: scheduled.RetryID, Retry: scheduled.Retry}
	_, err := log.Append(scheduled, started)
	require.NoError(t, err)
	require.Empty(t, log.Messages())
	require.Equal(t, []sessions.Entry{scheduled, started}, log.Entries())
	copied := sessions.Clone(scheduled).(sessions.RetryScheduled)
	copied.Failure.Text = "changed"
	require.Equal(t, "slow down", log.Entries()[0].(sessions.RetryScheduled).Failure.Text)
	encoded, err := json.Marshal(scheduled)
	require.NoError(t, err)
	require.JSONEq(t, `{"retryId":"retry","cycleId":"cycle","turn":1,"provider":"provider","policyKey":"default","retry":1,"maxRetries":5,"delayMs":500,"failure":{"code":"RATE_LIMIT","text":"slow down"}}`, string(encoded))
}
