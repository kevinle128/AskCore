package sessions_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func userText(n int) sessions.MessageEntry {
	return sessions.MessageEntry{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: strings.Repeat("x", n)}}}}
}

func assistantText(n int) sessions.MessageEntry {
	return sessions.MessageEntry{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: strings.Repeat("y", n)}}, StopReason: protocol.StopStop}}
}

func settled(outcome string, u *protocol.Usage) sessions.AttemptSettled {
	return sessions.AttemptSettled{AttemptID: "a", Outcome: outcome, Usage: u}
}

func TestEstimateUsesLastCompletedAttemptPlusTail(t *testing.T) {
	entries := []sessions.Entry{
		sessions.SystemSnapshot{SystemPrompt: strings.Repeat("s", 4000)},
		userText(400),
		assistantText(40),
		settled("completed", &protocol.Usage{Input: 1000, Output: 10, TotalTokens: 1010}),
		userText(401), // ceil(401/4) = 101
	}
	got, ok := sessions.EstimateContext(entries)
	require.True(t, ok)
	assert.Equal(t, int64(1010+101), got)
}

func TestEstimateSumsPartsWhenTotalTokensIsZero(t *testing.T) {
	entries := []sessions.Entry{
		userText(4),
		settled("completed", &protocol.Usage{Input: 100, Output: 10, CacheRead: 5, CacheWrite: 1}),
	}
	got, ok := sessions.EstimateContext(entries)
	require.True(t, ok)
	assert.Equal(t, int64(116), got)
}

func TestEstimateSkipsFailedAndAbortedAttempts(t *testing.T) {
	entries := []sessions.Entry{
		userText(4),
		settled("completed", &protocol.Usage{TotalTokens: 500}),
		userText(8), // 2
		settled("failed", &protocol.Usage{TotalTokens: 9000}),
		settled("aborted", &protocol.Usage{TotalTokens: 9000}),
		settled("completed", nil),
		settled("completed", &protocol.Usage{}),
	}
	got, ok := sessions.EstimateContext(entries)
	require.True(t, ok)
	assert.Equal(t, int64(502), got)
}

func TestEstimateDropsAnchorBeforeContextEdit(t *testing.T) {
	entries := []sessions.Entry{
		sessions.SystemSnapshot{SystemPrompt: strings.Repeat("s", 40)}, // 10
		userText(400), // 100, entry 1
		settled("completed", &protocol.Usage{TotalTokens: 5000}),
		sessions.ContextEdit{Target: 1, Replacement: &sessions.ContextReplacement{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "tiny"}}}}}, // 1
	}
	got, ok := sessions.EstimateContext(entries)
	require.True(t, ok)
	assert.Equal(t, int64(10+1), got, "the anchor is gone, so the estimate is the snapshot plus the edited history")
}

func TestEstimateKeepsAnchorAfterContextEdit(t *testing.T) {
	entries := []sessions.Entry{
		userText(400),
		sessions.ContextEdit{Target: 0},
		userText(40),
		settled("completed", &protocol.Usage{TotalTokens: 700}),
	}
	got, ok := sessions.EstimateContext(entries)
	require.True(t, ok)
	assert.Equal(t, int64(700), got)
}

func TestEstimateWithoutAnchorUsesSnapshotAndMessages(t *testing.T) {
	entries := []sessions.Entry{
		sessions.SystemSnapshot{SystemPrompt: strings.Repeat("s", 40)}, // 10
		userText(400),    // 100
		assistantText(4), // 1
		settled("failed", nil),
	}
	got, ok := sessions.EstimateContext(entries)
	require.True(t, ok)
	assert.Equal(t, int64(111), got)
}

func TestEstimateCountsToolDeclarationsOfSnapshot(t *testing.T) {
	plain := []sessions.Entry{sessions.SystemSnapshot{SystemPrompt: "p"}, userText(4)}
	withTool := []sessions.Entry{sessions.SystemSnapshot{SystemPrompt: "p", Tools: []protocol.ToolDecl{{Name: "read", Description: strings.Repeat("d", 400)}}}, userText(4)}
	a, _ := sessions.EstimateContext(plain)
	b, _ := sessions.EstimateContext(withTool)
	assert.GreaterOrEqual(t, b-a, int64(100))
}

func TestEstimateCountsImageAsFixedSize(t *testing.T) {
	img := sessions.MessageEntry{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Image{Data: strings.Repeat("A", 100000), MimeType: "image/png"}}}}
	got, ok := sessions.EstimateContext([]sessions.Entry{img})
	require.True(t, ok)
	assert.Equal(t, int64(1200), got) // 4800 / 4
}

func TestEstimateCountsAssistantBlocksAndToolResults(t *testing.T) {
	asst := sessions.MessageEntry{Message: protocol.AssistantMessage{
		Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: strings.Repeat("t", 8)},                                // 8
			protocol.ToolCall{ID: "1", Name: "read", Arguments: []byte(`{"path":"abcdefgh"}`)}, // 4 + 19
		},
		StopReason: protocol.StopToolUse,
	}}
	result := sessions.MessageEntry{Message: protocol.ToolResultMessage{ToolCallID: "1", Content: []protocol.UserBlock{protocol.Text{Text: strings.Repeat("r", 10)}}}} // 10
	got, ok := sessions.EstimateContext([]sessions.Entry{asst, result})
	require.True(t, ok)
	// Each message rounds up on its own: ceil(31/4) + ceil(10/4).
	assert.Equal(t, int64(8+3), got)
}

func TestEstimateErrorReplyCountsZero(t *testing.T) {
	msg := "boom"
	errReply := sessions.MessageEntry{Message: protocol.AssistantMessage{
		Content:      []protocol.AssistantBlock{protocol.Text{Text: strings.Repeat("z", 400)}},
		StopReason:   protocol.StopError,
		ErrorMessage: &msg,
	}}
	got, ok := sessions.EstimateContext([]sessions.Entry{userText(4), errReply})
	require.True(t, ok)
	assert.Equal(t, int64(1), got)
}

func TestEstimateEmptyLog(t *testing.T) {
	got, ok := sessions.EstimateContext(nil)
	assert.False(t, ok)
	assert.Zero(t, got)
	got, ok = sessions.EstimateContext([]sessions.Entry{sessions.CycleOpened{CycleID: "c"}})
	assert.False(t, ok)
	assert.Zero(t, got)
}
