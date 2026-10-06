package agent

import (
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepairMarksNotStartedAndUnknownForOpenTurnOnly(t *testing.T) {
	log := &sessions.MemoryLog{}
	a := protocol.AssistantMessage{StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.ToolCall{ID: "a", Name: "t"}, protocol.ToolCall{ID: "b", Name: "t"}}}
	_, err := log.Append(sessions.TurnOpened{CycleID: "c"}, sessions.MessageEntry{Message: a}, sessions.ToolCall{AssistantEntry: 1, CallID: "a"})
	require.NoError(t, err)
	require.NoError(t, repairTools(log, 0))
	messages := log.Messages()
	require.Len(t, messages, 3)
	require.Equal(t, "outcome unknown", messages[1].(protocol.ToolResultMessage).Content[0].(protocol.Text).Text)
	require.Equal(t, "not started", messages[2].(protocol.ToolResultMessage).Content[0].(protocol.Text).Text)
}

func repairAssistant(ids ...string) protocol.AssistantMessage {
	a := protocol.AssistantMessage{StopReason: protocol.StopToolUse}
	for _, id := range ids {
		a.Content = append(a.Content, protocol.ToolCall{ID: id, Name: "t"})
	}
	return a
}
func TestRepairNeverOverwritesCommittedResult(t *testing.T) {
	log := &sessions.MemoryLog{}
	_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: repairAssistant("a", "b")}, sessions.ToolCall{AssistantEntry: 1, CallID: "a"}, sessions.MessageEntry{Message: toolResultMessage(toolOutcome{call: protocol.ToolCall{ID: "a", Name: "t"}, result: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "saved"}}}})})
	require.NoError(t, err)
	require.NoError(t, repairTools(log, 0))
	msgs := log.Messages()
	require.Len(t, msgs, 3)
	require.Equal(t, "saved", msgs[1].(protocol.ToolResultMessage).Content[0].(protocol.Text).Text)
	require.NoError(t, repairTools(log, 0))
	require.Equal(t, msgs, log.Messages())
}
func TestRepairKeepsUnansweredCallOfClosedTurn(t *testing.T) {
	log := &sessions.MemoryLog{}
	_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: repairAssistant("old")}, sessions.TurnClosed{}, sessions.TurnOpened{}, sessions.MessageEntry{Message: repairAssistant("new")})
	require.NoError(t, err)
	require.NoError(t, repairTools(log, 0))
	msgs := log.Messages()
	require.Len(t, msgs, 3)
	require.Equal(t, "new", msgs[2].(protocol.ToolResultMessage).ToolCallID)
	transcript := providers.TransformMessages(msgs, providers.Model{}, nil, nil)
	var synthetic bool
	for _, m := range transcript {
		if r, ok := m.(protocol.ToolResultMessage); ok && r.ToolCallID == "old" {
			synthetic = true
			require.Contains(t, r.Content[0].(protocol.Text).Text, "No result provided")
		}
	}
	require.True(t, synthetic)
	require.Len(t, log.Messages(), 3)
}
func TestRepairMatchesByAssistantEntryAndCallId(t *testing.T) {
	log := &sessions.MemoryLog{}
	_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: repairAssistant("same")}, sessions.ToolCall{AssistantEntry: 1, CallID: "same"}, sessions.MessageEntry{Message: repairAssistant("same")})
	require.NoError(t, err)
	require.NoError(t, repairTools(log, 0))
	msgs := log.Messages()
	require.Len(t, msgs, 4)
	require.Equal(t, "outcome unknown", msgs[2].(protocol.ToolResultMessage).Content[0].(protocol.Text).Text)
	require.Equal(t, "not started", msgs[3].(protocol.ToolResultMessage).Content[0].(protocol.Text).Text)
	before := log.Entries()
	require.NoError(t, repairTools(log, 0))
	require.Equal(t, before, log.Entries())
}
func TestRepairMatchesResultsBySameTurnAndCallID(t *testing.T) {
	log := &sessions.MemoryLog{}
	_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: repairAssistant("same")}, sessions.MessageEntry{Message: toolResultMessage(errorOutcome(protocol.ToolCall{ID: "same", Name: "t"}, "old answer"))}, sessions.TurnClosed{}, sessions.TurnOpened{}, sessions.MessageEntry{Message: repairAssistant("same")}, sessions.ToolCall{AssistantEntry: 5, CallID: "same"}, sessions.ToolCall{AssistantEntry: 5, CallID: "unrequested"})
	require.NoError(t, err)
	require.NoError(t, repairTools(log, 0))
	msgs := log.Messages()
	require.Len(t, msgs, 4)
	require.Equal(t, "outcome unknown", msgs[3].(protocol.ToolResultMessage).Content[0].(protocol.Text).Text)
	require.Equal(t, "same", msgs[3].(protocol.ToolResultMessage).ToolCallID)
}
func TestRepairSkipsHistoricalOpenTurnAndDurablyClosedTurn(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{true: "closed", false: "historical"}[closed], func(t *testing.T) {
			log := &sessions.MemoryLog{}
			_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: repairAssistant("a")})
			require.NoError(t, err)
			start := len(log.Entries())
			if closed {
				start = 0
				_, err = log.Append(sessions.TurnClosed{})
				require.NoError(t, err)
			}
			before := log.Entries()
			require.NoError(t, repairTools(log, start))
			require.Equal(t, before, log.Entries())
		})
	}
}
