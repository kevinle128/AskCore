package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func replayAnthropicBody(t *testing.T, history []protocol.Message) map[string]any {
	t.Helper()
	body, _, err := streamOnce(t, streamOnceOpts{messages: history, sse: textSSE("end_turn")})
	require.NoError(t, err)
	return body
}

func anthropicMessages(t *testing.T, body map[string]any) []any {
	t.Helper()
	messages, ok := body["messages"].([]any)
	require.True(t, ok)
	return messages
}

func anthropicContent(t *testing.T, message any) []any {
	t.Helper()
	blocks, ok := message.(map[string]any)["content"].([]any)
	require.True(t, ok)
	return blocks
}

func TestReplayResponsesReasoningAndToolIDsToAnthropicJSON(t *testing.T) {
	for _, tc := range []struct {
		name, summary string
	}{
		{"empty-summary", ""},
		{"nonempty-summary", "prior summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig := providers.EncodeThinkingSignature(providers.ReasoningReplay{Type: "reasoning", ID: "rs_1", EncryptedContent: "opaque"})
			history := []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
				protocol.AssistantMessage{API: string(providers.APIOpenAIResponses), Provider: providers.ProviderOpenAI, Model: "gpt-5.5", StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
					protocol.Thinking{Thinking: tc.summary, ThinkingSignature: &sig},
					protocol.ToolCall{ID: "call_x|fc_y", Name: "echo", Arguments: json.RawMessage(`{"x":1}`)},
				}},
				protocol.ToolResultMessage{ToolCallID: "call_x|fc_y", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}}},
			}
			msgs := anthropicMessages(t, replayAnthropicBody(t, history))
			require.Len(t, msgs, 3)
			assistant := anthropicContent(t, msgs[1])
			if tc.summary == "" {
				require.Len(t, assistant, 1)
			} else {
				require.Len(t, assistant, 2)
				assert.Equal(t, map[string]any{"type": "text", "text": tc.summary}, assistant[0])
			}
			call := assistant[len(assistant)-1].(map[string]any)
			assert.Equal(t, "tool_use", call["type"])
			assert.Equal(t, "call_x_fc_y", call["id"])
			assert.NotContains(t, call, "signature")
			result := anthropicContent(t, msgs[2])[0].(map[string]any)
			assert.Equal(t, "call_x_fc_y", result["tool_use_id"])
		})
	}
}

func TestReplayForeignCompletionsReasoningToAnthropicJSON(t *testing.T) {
	field := "reasoning_content"
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIOpenAICompletions), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "reasoning text", ThinkingSignature: &field},
			protocol.Text{Text: "answer"},
		}},
	}
	msgs := anthropicMessages(t, replayAnthropicBody(t, history))
	require.Len(t, msgs, 2)
	blocks := anthropicContent(t, msgs[1])
	require.Len(t, blocks, 2)
	assert.Equal(t, map[string]any{"type": "text", "text": "reasoning text"}, blocks[0])
	assert.Equal(t, map[string]any{"type": "text", "text": "answer"}, blocks[1])
}

func TestReplayAnthropicThinkingAfterSystemAndToolChangesJSON(t *testing.T) {
	sig := "signed-original"
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: APIID, Provider: ProviderID, Model: ModelID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig},
			protocol.Text{Text: "answer"},
		}},
		protocol.SystemMessage{Content: []protocol.Text{{Text: "new rule"}}, ToolsAdded: []protocol.ToolDecl{{Name: "new_tool", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}
	body := replayAnthropicBody(t, history)
	msgs := anthropicMessages(t, body)
	blocks := anthropicContent(t, msgs[1])
	assert.Equal(t, "signed-original", blocks[0].(map[string]any)["signature"])
	assert.Contains(t, body["tools"], map[string]any{"name": "new_tool", "input_schema": map[string]any{"type": "object"}})
	assert.Contains(t, stringMustJSON(t, msgs), "new rule")
}

func TestReplayProxyResponseModelKeepsAnthropicSignatureJSON(t *testing.T) {
	sig, responseModel := "signed-original", "backend-variant"
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: APIID, Provider: ProviderID, Model: ModelID, ResponseModel: &responseModel, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig},
			protocol.Text{Text: "answer"},
		}},
	}
	msgs := anthropicMessages(t, replayAnthropicBody(t, history))
	require.Len(t, msgs, 2)
	blocks := anthropicContent(t, msgs[1])
	require.Len(t, blocks, 2)
	assert.Equal(t, "thinking", blocks[0].(map[string]any)["type"])
	assert.Equal(t, sig, blocks[0].(map[string]any)["signature"])
	assert.NotContains(t, stringMustJSON(t, msgs), responseModel)
}

func TestReplayRedactedAndUnsignedThinkingToAnthropicJSON(t *testing.T) {
	redacted, sig := true, "opaque-redacted"
	for _, tc := range []struct {
		name      string
		thinking  protocol.Thinking
		wantType  string
		wantField string
		wantValue string
	}{
		{"redacted", protocol.Thinking{ThinkingSignature: &sig, Redacted: &redacted}, "redacted_thinking", "data", sig},
		{"unsigned", protocol.Thinking{Thinking: "unsigned plan"}, "thinking", "signature", ""},
		{"signed-empty", protocol.Thinking{Thinking: "", ThinkingSignature: &sig}, "thinking", "signature", sig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
				protocol.AssistantMessage{API: APIID, Provider: ProviderID, Model: ModelID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{tc.thinking, protocol.Text{Text: "answer"}}},
			}
			msgs := anthropicMessages(t, replayAnthropicBody(t, history))
			blocks := anthropicContent(t, msgs[1])
			require.Len(t, blocks, 2)
			first := blocks[0].(map[string]any)
			assert.Equal(t, tc.wantType, first["type"])
			assert.Equal(t, tc.wantValue, first[tc.wantField])
		})
	}
}

func TestReplayParallelResultsAndEmptyAssistantToAnthropicJSON(t *testing.T) {
	call := func(id string) protocol.ToolCall {
		return protocol.ToolCall{ID: id, Name: "echo", Arguments: json.RawMessage(`{}`)}
	}
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIOpenAICompletions), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{call("a"), call("b"), call("c")}},
		protocol.ToolResultMessage{ToolCallID: "c", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "C"}}},
		protocol.ToolResultMessage{ToolCallID: "a", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "A"}}},
		protocol.ToolResultMessage{ToolCallID: "b", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "B"}}},
		protocol.AssistantMessage{API: APIID, Provider: ProviderID, Model: ModelID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{protocol.Text{Text: " "}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}
	msgs := anthropicMessages(t, replayAnthropicBody(t, history))
	require.Len(t, msgs, 4)
	results := anthropicContent(t, msgs[2])
	require.Len(t, results, 3)
	for i, id := range []string{"c", "a", "b"} {
		assert.Equal(t, id, results[i].(map[string]any)["tool_use_id"])
	}
	assert.NotContains(t, stringMustJSON(t, msgs), `"content":[]`)
}

func TestReplayDropsOrphanResultsAndKeepsValidPairAnthropicJSON(t *testing.T) {
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: APIID, Provider: ProviderID, Model: ModelID, StopReason: protocol.StopAborted, Content: []protocol.AssistantBlock{
			protocol.ToolCall{ID: "aborted_call", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "aborted_call", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "late"}}},
		protocol.AssistantMessage{API: APIID, Provider: ProviderID, Model: ModelID, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
			protocol.ToolCall{ID: "valid_call", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "valid_call", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "valid"}}},
		protocol.AssistantMessage{API: APIID, Provider: ProviderID, Model: ModelID, StopReason: protocol.StopError, Content: []protocol.AssistantBlock{
			protocol.ToolCall{ID: "errored_call", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "errored_call", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "late-error"}}},
	}
	msgs := anthropicMessages(t, replayAnthropicBody(t, history))
	require.Len(t, msgs, 3)
	call := anthropicContent(t, msgs[1])[0].(map[string]any)
	assert.Equal(t, "valid_call", call["id"])
	result := anthropicContent(t, msgs[2])[0].(map[string]any)
	assert.Equal(t, "valid_call", result["tool_use_id"])
	assert.NotContains(t, stringMustJSON(t, msgs), "aborted_call")
	assert.NotContains(t, stringMustJSON(t, msgs), "errored_call")
}

func TestReplayLongAndAstralResponsesIDsToAnthropicJSON(t *testing.T) {
	for _, id := range []string{"call_x|" + strings.Repeat("a", 400), "call|a😀b"} {
		history := []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.AssistantMessage{API: string(providers.APIOpenAIResponses), Provider: providers.ProviderOpenAI, Model: "gpt-5.5", StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.ToolCall{ID: id, Name: "echo", Arguments: json.RawMessage(`{}`)}}},
			protocol.ToolResultMessage{ToolCallID: id, ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}}},
		}
		msgs := anthropicMessages(t, replayAnthropicBody(t, history))
		call := anthropicContent(t, msgs[1])[0].(map[string]any)
		want := "call_x_" + strings.Repeat("a", 57)
		if strings.Contains(id, "😀") {
			want = "call_a_b"
		}
		assert.Equal(t, want, call["id"])
		assert.LessOrEqual(t, len([]rune(want)), 64)
		result := anthropicContent(t, msgs[2])[0].(map[string]any)
		assert.Equal(t, want, result["tool_use_id"])
	}
}

func stringMustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
