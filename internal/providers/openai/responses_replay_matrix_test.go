package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func responsesReplayInput(t *testing.T, history []protocol.Message) []any {
	t.Helper()
	srv := newCaptureServer(t, responsesSSE())
	model := responsesModel(srv.srv.URL)
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "base", Messages: history})
	_, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), model, req, providers.StreamOptions{}))
	require.NoError(t, err)
	input, ok := srv.take(t).body(t)["input"].([]any)
	require.True(t, ok)
	return input
}

func responseItems(input []any, kind string) []map[string]any {
	var out []map[string]any
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if ok && item["type"] == kind {
			out = append(out, item)
		}
	}
	return out
}

func TestReplayForeignCompletionsThinkingToResponsesJSON(t *testing.T) {
	field := "reasoning_content"
	input := responsesReplayInput(t, []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIOpenAICompletions), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "plan", ThinkingSignature: &field},
			protocol.Text{Text: "answer"},
		}},
	})
	assert.Empty(t, responseItems(input, "reasoning"))
	assistant := responseItems(input, "message")
	var text string
	for _, item := range assistant {
		if item["role"] == "assistant" {
			text += mustJSONReplay(t, item["content"])
		}
	}
	assert.Contains(t, text, "plan")
	assert.Contains(t, text, "answer")
	assert.NotContains(t, mustJSONReplay(t, input), "reasoning_content")
}

func TestReplayRedactedThinkingToResponsesJSON(t *testing.T) {
	redacted, sig := true, "opaque-redacted"
	input := responsesReplayInput(t, []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Thinking{ThinkingSignature: &sig, Redacted: &redacted}, protocol.Text{Text: "answer"},
		}},
	})
	assert.Empty(t, responseItems(input, "reasoning"))
	assert.NotContains(t, mustJSONReplay(t, input), sig)
	assert.Contains(t, mustJSONReplay(t, input), "answer")
}

func TestReplayOtherResponsesModelReasoningAndCallJSON(t *testing.T) {
	sig := providers.EncodeThinkingSignature(providers.ReasoningReplay{Type: "reasoning", ID: "rs_prior", EncryptedContent: "opaque"})
	input := responsesReplayInput(t, []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIOpenAIResponses), Provider: providers.ProviderOpenAI, Model: "gpt-5-mini", StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "prior summary", ThinkingSignature: &sig},
			protocol.ToolCall{ID: "call_1|fc_old", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "call_1|fc_old", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}}},
	})
	assert.Empty(t, responseItems(input, "reasoning"))
	assert.Contains(t, mustJSONReplay(t, input), "prior summary")
	calls := responseItems(input, "function_call")
	require.Len(t, calls, 1)
	assert.Equal(t, "call_1", calls[0]["call_id"])
	assert.NotContains(t, calls[0], "id")
	results := responseItems(input, "function_call_output")
	require.Len(t, results, 1)
	assert.Equal(t, "call_1", results[0]["call_id"])
}

func TestReplayToolNamespaceOnSameModelOnlyJSON(t *testing.T) {
	namespace := "workspace"
	for _, tc := range []struct {
		name, sourceModel string
		wantNamespace     bool
	}{
		{"same-model", providers.ModelGPT55, true},
		{"other-model", "gpt-5-mini", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := responsesReplayInput(t, []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
				protocol.AssistantMessage{API: string(providers.APIOpenAIResponses), Provider: providers.ProviderOpenAI, Model: tc.sourceModel, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
					protocol.ToolCall{ID: "call_1|fc_1", Name: "echo", Namespace: &namespace, Arguments: json.RawMessage(`{}`)},
				}},
				protocol.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}}},
			})
			calls := responseItems(input, "function_call")
			require.Len(t, calls, 1)
			if tc.wantNamespace {
				assert.Equal(t, namespace, calls[0]["namespace"])
			} else {
				assert.NotContains(t, calls[0], "namespace")
			}
		})
	}
}

func TestReplayEmptyAssistantToResponsesJSON(t *testing.T) {
	input := responsesReplayInput(t, []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIOpenAIResponses), Provider: providers.ProviderOpenAI, Model: providers.ModelGPT55, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{protocol.Text{Text: " "}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	})
	for _, item := range responseItems(input, "message") {
		assert.NotEqual(t, "assistant", item["role"])
	}
	assert.Contains(t, mustJSONReplay(t, input), "next")
}
