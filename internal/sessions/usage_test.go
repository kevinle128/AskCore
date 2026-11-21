package sessions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func tokens(in, out, cost int64) *protocol.Usage {
	return &protocol.Usage{Input: in, Output: out, TotalTokens: in + out, Cost: protocol.Cost{Total: cost}}
}

func TestFoldUsageAddsRetriedAttempts(t *testing.T) {
	entries := []sessions.Entry{
		sessions.CycleOpened{CycleID: "c1"},
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "failed", Usage: tokens(10, 2, 5), Failure: &sessions.Failure{Code: "SERVER"}},
		sessions.RetryScheduled{RetryID: "r1"},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "completed", Usage: tokens(20, 4, 7)},
		sessions.CycleClosed{CycleID: "c1", Reason: "completed"},
	}
	rows, totals := sessions.FoldUsage(entries)
	require.Len(t, rows, 2)
	assert.Equal(t, "a1", rows[0].AttemptID)
	assert.Equal(t, "failed", rows[0].Outcome)
	assert.Equal(t, "a2", rows[1].AttemptID)
	assert.Equal(t, int64(30), totals.Total.Input)
	assert.Equal(t, int64(6), totals.Total.Output)
	assert.Equal(t, int64(36), totals.Total.TotalTokens)
	assert.Equal(t, int64(12), totals.Total.Cost.Total)
	assert.True(t, totals.Complete)
	assert.True(t, totals.CostComplete)
}

func TestFoldUsageMissingUsageIsIncomplete(t *testing.T) {
	entries := []sessions.Entry{
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "completed", Usage: tokens(10, 2, 5)},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "completed"},
	}
	rows, totals := sessions.FoldUsage(entries)
	require.Len(t, rows, 2)
	assert.Nil(t, rows[1].Usage)
	assert.False(t, totals.Complete)
	assert.False(t, totals.CostComplete)
	assert.Equal(t, int64(10), totals.Total.Input)
}

func TestFoldUsageRefusedAttemptCountsAsZero(t *testing.T) {
	entries := []sessions.Entry{
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "failed", NoOutput: true, Failure: &sessions.Failure{Code: "RATE_LIMIT"}},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "completed", Usage: tokens(10, 2, 5)},
	}
	rows, totals := sessions.FoldUsage(entries)
	require.Len(t, rows, 2)
	assert.True(t, totals.Complete)
	assert.True(t, totals.CostComplete)
	assert.Equal(t, int64(10), totals.Total.Input)
	assert.Equal(t, int64(5), totals.Total.Cost.Total)
}

func TestFoldUsageNoOutputOnlyExcusesFailedAttempts(t *testing.T) {
	cases := map[string]sessions.AttemptSettled{
		"failed after output":  {AttemptID: "a", Outcome: "failed"},
		"aborted":              {AttemptID: "a", Outcome: "aborted", NoOutput: false},
		"completed no usage":   {AttemptID: "a", Outcome: "completed", NoOutput: true},
		"aborted with no flag": {AttemptID: "a", Outcome: "aborted", NoOutput: true},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			_, totals := sessions.FoldUsage([]sessions.Entry{e})
			assert.False(t, totals.Complete)
		})
	}
}

func TestFoldUsageUnknownPriceIsCostIncomplete(t *testing.T) {
	entries := []sessions.Entry{
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "completed", Usage: tokens(10, 2, 0), CostUnknown: true},
	}
	_, totals := sessions.FoldUsage(entries)
	assert.True(t, totals.Complete)
	assert.False(t, totals.CostComplete)
	assert.Equal(t, int64(10), totals.Total.Input)
}

func TestFoldUsageIgnoresMessageUsage(t *testing.T) {
	entries := []sessions.Entry{
		sessions.MessageEntry{Message: protocol.AssistantMessage{Usage: *tokens(99, 99, 99), StopReason: protocol.StopStop}},
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "completed", Usage: tokens(1, 1, 1)},
	}
	_, totals := sessions.FoldUsage(entries)
	assert.Equal(t, int64(1), totals.Total.Input)
	assert.Equal(t, int64(1), totals.Total.Cost.Total)
}

func TestFoldUsageAcrossCycles(t *testing.T) {
	entries := []sessions.Entry{
		sessions.CycleOpened{CycleID: "c1"},
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "completed", Usage: tokens(1, 1, 1)},
		sessions.CycleClosed{CycleID: "c1", Reason: "completed"},
		sessions.CycleOpened{CycleID: "c2"},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "completed", Usage: tokens(2, 2, 2)},
		sessions.AttemptSettled{AttemptID: "a3", Outcome: "completed", Usage: tokens(3, 3, 3)},
	}
	rows, totals := sessions.FoldUsage(entries)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"c1", "c2", "c2"}, []string{rows[0].CycleID, rows[1].CycleID, rows[2].CycleID})
	assert.Equal(t, int64(6), totals.Total.Input)
}

func TestFoldUsageEmptyLogIsCompleteAndZero(t *testing.T) {
	rows, totals := sessions.FoldUsage(nil)
	assert.Empty(t, rows)
	assert.NotNil(t, rows)
	assert.True(t, totals.Complete)
	assert.True(t, totals.CostComplete)
	assert.Zero(t, totals.Total.TotalTokens)
}

func TestFoldUsageAddsOptionalCountersAndDoesNotAliasEntries(t *testing.T) {
	one, two := int64(1), int64(2)
	first := &protocol.Usage{Input: 1, Reasoning: &one}
	entries := []sessions.Entry{
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "completed", Usage: first},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "completed", Usage: &protocol.Usage{Input: 1, Reasoning: &two}},
		sessions.AttemptSettled{AttemptID: "a3", Outcome: "completed", Usage: &protocol.Usage{Input: 1}},
	}
	rows, totals := sessions.FoldUsage(entries)
	require.NotNil(t, totals.Total.Reasoning)
	assert.Equal(t, int64(3), *totals.Total.Reasoning)
	assert.Nil(t, totals.Total.CacheWrite1h)
	assert.Equal(t, int64(1), *first.Reasoning, "the log entry is not changed")
	require.NotNil(t, rows[0].Usage)
	assert.NotSame(t, first, rows[0].Usage)
}
