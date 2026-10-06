package sessions_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
)

func TestToolCallEntryIsLogOnlyAndKeepsAssistantScope(t *testing.T) {
	log := &sessions.MemoryLog{}
	first := sessions.ToolCall{AssistantEntry: 2, CallID: "same-call"}
	second := sessions.ToolCall{AssistantEntry: 7, CallID: "same-call"}
	ref, err := log.Append(first, second)
	require.NoError(t, err)
	require.Equal(t, sessions.CommitRef{Start: 0, End: 2}, ref)
	require.Empty(t, log.Messages(), "recording intent adds no model message")
	require.Equal(t, []sessions.Entry{first, second}, log.Entries(), "call identity includes the assistant entry")
	clone := sessions.Clone(first).(sessions.ToolCall)
	clone.CallID = "changed"
	require.Equal(t, "same-call", log.Entries()[0].(sessions.ToolCall).CallID)
	encoded, err := json.Marshal(first)
	require.NoError(t, err)
	require.JSONEq(t, `{"assistantEntry":2,"callId":"same-call"}`, string(encoded))
}
