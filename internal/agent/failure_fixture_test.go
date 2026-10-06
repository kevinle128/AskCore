package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestWriteFailureWithUncertainToolOutcomeRunsNoToolAgain(t *testing.T) {
	p, model := newFaux(t)
	p.Set(faux.Reply(call("count", "uncertain", nil)))
	var bodies atomic.Int32
	tool := &funcTool{name: "count", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies.Add(1)
		return textResult("body completed"), nil
	}}
	writeErr := errors.New("first tool result append failed")
	failed := false
	log := &commitGate{err: writeErr, failWhen: func(entries []sessions.Entry) bool {
		for _, entry := range entries {
			if message, ok := entry.(sessions.MessageEntry); ok {
				if _, ok := message.Message.(protocol.ToolResultMessage); ok && !failed {
					failed = true
					return true
				}
			}
		}
		return false
	}}
	a := newAgent(t, p, model, func(config *agent.Config) {
		config.Tools = registry(t, tool)
		config.NewContext = func() sessions.Writer { return log }
	})
	t.Cleanup(func() { require.NoError(t, a.Dispose()) })

	require.ErrorIs(t, a.Prompt(context.Background(), user("run the counting tool")), writeErr)
	require.True(t, failed)
	require.Equal(t, int32(1), bodies.Load())
	require.Equal(t, 1, p.Calls(), "the failed write stops model requests")
	entries := log.Entries()
	var intent *sessions.ToolCall
	for _, entry := range entries {
		if call, ok := entry.(sessions.ToolCall); ok {
			require.Nil(t, intent, "only one tool dispatch was recorded")
			intent = &call
		}
	}
	require.NotNil(t, intent, "the dispatch intent was committed before the failed result")
	require.Equal(t, "uncertain", intent.CallID)
	request, ok := entries[intent.AssistantEntry].(sessions.MessageEntry)
	require.True(t, ok)
	require.Equal(t, protocol.RoleAssistant, request.Message.Role())
	result := toolResults(log.Messages())[intent.CallID]
	require.True(t, result.IsError)
	require.Equal(t, "outcome unknown", resultText(result.Content))

	p.Set(faux.Reply(call("count", "uncertain", nil)), faux.Say("done"))
	require.NoError(t, a.Prompt(context.Background(), user("continue")))
	require.Equal(t, int32(1), bodies.Load(), "a later request cannot execute the uncertain call again")
	require.Equal(t, 3, p.Calls())
	unknown := 0
	for _, message := range log.Messages() {
		if result, ok := message.(protocol.ToolResultMessage); ok && result.ToolCallID == "uncertain" {
			require.True(t, result.IsError)
			if resultText(result.Content) == "outcome unknown" {
				unknown++
			} else {
				require.Equal(t, `Tool call id "uncertain" is already used in this session`, resultText(result.Content))
			}
		}
	}
	require.Equal(t, 1, unknown, "the committed uncertainty result remains in history")
	require.Equal(t, "done", assistantText(lastAssistant(t, a.State().Messages)))
}
