package providers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestConvertToLLMKeepsFourRolesAndDropsTheRest(t *testing.T) {
	user := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}, Timestamp: 1}
	assistant := protocol.AssistantMessage{StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{protocol.Text{Text: "yo"}}}
	result := protocol.ToolResultMessage{ToolCallID: "a", ToolName: "echo"}
	system := protocol.SystemMessage{Content: []protocol.Text{{Text: "sys"}}}
	custom := protocol.RawMessage{RoleName: "bashExecution", Data: json.RawMessage(`{"role":"bashExecution"}`)}
	in := []protocol.Message{custom, system, user, &assistant, custom, result}

	before := append([]protocol.Message(nil), in...)
	out := ConvertToLLM(in)

	assert.Equal(t, []protocol.Message{system, user, &assistant, result}, out)
	assert.Equal(t, before, in, "the input is not changed")
	assert.Len(t, in, 6)
}

func TestConvertToLLMDropsARawMessageEvenWithABuiltInRoleName(t *testing.T) {
	raw := protocol.RawMessage{RoleName: "user", Data: json.RawMessage(`{"role":"user"}`)}
	assert.Empty(t, ConvertToLLM([]protocol.Message{raw}))
}

func TestConvertToLLMEmptyAndNilGiveEmptySlice(t *testing.T) {
	assert.Equal(t, []protocol.Message{}, ConvertToLLM(nil))
	assert.Equal(t, []protocol.Message{}, ConvertToLLM([]protocol.Message{}))
}

func TestNormalizeRequestFoldsPromptAndToolsIntoOneSystemMessage(t *testing.T) {
	tool := protocol.ToolDecl{Name: "echo", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}
	user := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}, Timestamp: 9}
	req := Request{SystemPrompt: "be brief", Messages: []protocol.Message{user}, Tools: []protocol.ToolDecl{tool}}

	got := NormalizeRequest(req)

	require.Len(t, got.Messages, 2)
	sys, ok := got.Messages[0].(protocol.SystemMessage)
	require.True(t, ok)
	assert.Equal(t, int64(0), sys.Timestamp)
	assert.Equal(t, []protocol.Text{{Text: "be brief"}}, sys.Content)
	require.Len(t, sys.ToolsAdded, 1)
	assert.Equal(t, "echo", sys.ToolsAdded[0].Name)
	assert.Equal(t, user, got.Messages[1])
}

func TestNormalizeRequestPromptOnlyAndToolsOnly(t *testing.T) {
	promptOnly := NormalizeRequest(Request{SystemPrompt: "p"})
	require.Len(t, promptOnly.Messages, 1)
	assert.Empty(t, promptOnly.Messages[0].(protocol.SystemMessage).ToolsAdded)

	toolsOnly := NormalizeRequest(Request{Tools: []protocol.ToolDecl{{Name: "t"}}})
	require.Len(t, toolsOnly.Messages, 1)
	sys := toolsOnly.Messages[0].(protocol.SystemMessage)
	assert.NotNil(t, sys.Content)
	assert.Empty(t, sys.Content)
	assert.Len(t, sys.ToolsAdded, 1)
}

func TestNormalizeRequestAddsNothingWhenEmpty(t *testing.T) {
	user := protocol.UserMessage{Timestamp: 1}
	got := NormalizeRequest(Request{Messages: []protocol.Message{user}})
	assert.Equal(t, []protocol.Message{user}, got.Messages)

	empty := NormalizeRequest(Request{})
	assert.NotNil(t, empty.Messages)
	assert.Empty(t, empty.Messages)

	zeroTools := NormalizeRequest(Request{Tools: []protocol.ToolDecl{}})
	assert.Empty(t, zeroTools.Messages)
}

func TestNormalizeRequestDoesNotShareOrMutateCallerData(t *testing.T) {
	params := json.RawMessage(`{"type":"object"}`)
	tools := []protocol.ToolDecl{{Name: "echo", Parameters: params}}
	msgs := make([]protocol.Message, 1, 4) // spare capacity must not be used
	msgs[0] = protocol.UserMessage{Timestamp: 1}
	req := Request{SystemPrompt: "p", Messages: msgs, Tools: tools}

	got := NormalizeRequest(req)
	got.Messages[1] = protocol.UserMessage{Timestamp: 2}
	got.Messages[0].(protocol.SystemMessage).ToolsAdded[0].Parameters[2] = 'X'

	assert.Equal(t, protocol.UserMessage{Timestamp: 1}, msgs[0])
	assert.Nil(t, msgs[:2][1], "the caller's spare capacity is untouched")
	assert.JSONEq(t, `{"type":"object"}`, string(params))
	assert.Len(t, req.Messages, 1)
	assert.Equal(t, "echo", tools[0].Name)
}
