package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func completionsReplayBody(t *testing.T, history []protocol.Message, configure func(*providers.Model)) map[string]any {
	t.Helper()
	srv := newCaptureServer(t, completionsSSE())
	model := completionsModel(srv.srv.URL)
	if configure != nil {
		configure(&model)
	}
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "base", Messages: history})
	_, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), model, req, providers.StreamOptions{Reasoning: protocol.ThinkingOff}))
	require.NoError(t, err)
	return srv.take(t).body(t)
}

func completionMessages(t *testing.T, body map[string]any) []any {
	t.Helper()
	messages, ok := body["messages"].([]any)
	require.True(t, ok)
	return messages
}

func TestReplayAnthropicSignedToolTurnToCompletionsJSON(t *testing.T) {
	sig := "opaque-anthropic"
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig},
			protocol.Text{Text: "answer"},
			protocol.ToolCall{ID: "toolu_123", Name: "echo", Arguments: json.RawMessage(`{"x":1}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "toolu_123", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}}},
	}
	msgs := completionMessages(t, completionsReplayBody(t, history, nil))
	require.Len(t, msgs, 4)
	assistant := msgs[2].(map[string]any)
	assert.Equal(t, "plananswer", assistant["content"])
	assert.NotContains(t, assistant, "reasoning_content")
	calls := assistant["tool_calls"].([]any)
	require.Len(t, calls, 1)
	assert.Equal(t, "toolu_123", calls[0].(map[string]any)["id"])
	assert.Equal(t, "toolu_123", msgs[3].(map[string]any)["tool_call_id"])
}

func TestReplayLongAndParallelResponsesIDsToCompletionsJSON(t *testing.T) {
	long := "call_x|" + strings.Repeat("a", 400)
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"long", []string{long}},
		{"parallel-same-call", []string{"call_x|fc_one", "call_x|fc_two"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := make([]protocol.AssistantBlock, 0, len(tc.ids))
			history := []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}}
			for _, id := range tc.ids {
				calls = append(calls, protocol.ToolCall{ID: id, Name: "echo", Arguments: json.RawMessage(`{}`)})
			}
			history = append(history, protocol.AssistantMessage{API: string(providers.APIOpenAIResponses), Provider: providers.ProviderOpenAI, Model: "gpt-5.5", StopReason: protocol.StopToolUse, Content: calls})
			for _, id := range tc.ids {
				history = append(history, protocol.ToolResultMessage{ToolCallID: id, ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}}})
			}
			msgs := completionMessages(t, completionsReplayBody(t, history, nil))
			assistant := msgs[2].(map[string]any)
			outCalls := assistant["tool_calls"].([]any)
			require.Len(t, outCalls, len(tc.ids))
			got := make([]string, 0, len(tc.ids))
			for i, raw := range outCalls {
				id := raw.(map[string]any)["id"].(string)
				got = append(got, id)
				assert.Equal(t, id, msgs[3+i].(map[string]any)["tool_call_id"])
			}
			if tc.name == "long" {
				joined := "call_x_" + strings.Repeat("a", 400)
				assert.Equal(t, joined[:31]+"_"+providers.ShortHash(joined), got[0])
			} else {
				assert.Equal(t, []string{"call_x_fc_one", "call_x_fc_two"}, got)
			}
		})
	}
}

func TestReplayRedactedThinkingAndNonVisionImagesToCompletionsJSON(t *testing.T) {
	redacted, sig := true, "redacted-data"
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Image{Data: "YQ==", MimeType: "image/png"}, protocol.Image{Data: "Yg==", MimeType: "image/png"}}},
		protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
			protocol.Thinking{ThinkingSignature: &sig, Redacted: &redacted},
			protocol.ToolCall{ID: "toolu_image", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "toolu_image", ToolName: "echo", Content: []protocol.UserBlock{protocol.Image{Data: "Yw==", MimeType: "image/png"}}},
	}
	msgs := completionMessages(t, completionsReplayBody(t, history, func(m *providers.Model) { m.Input = []string{"text"} }))
	require.Len(t, msgs, 4)
	assert.Equal(t, "(image omitted: model does not support images)", msgs[1].(map[string]any)["content"])
	assistant := msgs[2].(map[string]any)
	assert.NotContains(t, assistant, "reasoning_content")
	assert.Equal(t, "(tool image omitted: model does not support images)", msgs[3].(map[string]any)["content"])
	for _, msg := range msgs {
		assert.NotContains(t, mustJSONReplay(t, msg), "image_url")
		assert.NotContains(t, mustJSONReplay(t, msg), "redacted-data")
	}
}

func TestReplayVisionUserAndToolImagesToCompletionsJSON(t *testing.T) {
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Image{Data: "YQ==", MimeType: "image/png"}}},
		protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.ToolCall{ID: "toolu_image", Name: "echo", Arguments: json.RawMessage(`{}`)}}},
		protocol.ToolResultMessage{ToolCallID: "toolu_image", ToolName: "echo", Content: []protocol.UserBlock{protocol.Image{Data: "Yg==", MimeType: "image/png"}}},
	}
	msgs := completionMessages(t, completionsReplayBody(t, history, func(m *providers.Model) { m.Input = []string{"text", "image"} }))
	require.Len(t, msgs, 5)
	user := msgs[1].(map[string]any)["content"].([]any)
	assert.Equal(t, "data:image/png;base64,YQ==", user[0].(map[string]any)["image_url"].(map[string]any)["url"])
	assert.Equal(t, "(see attached image)", msgs[3].(map[string]any)["content"])
	attached := msgs[4].(map[string]any)["content"].([]any)
	assert.Equal(t, "Attached image(s) from tool result:", attached[0].(map[string]any)["text"])
	assert.Equal(t, "data:image/png;base64,Yg==", attached[1].(map[string]any)["image_url"].(map[string]any)["url"])
}

func TestReplayEmptyAssistantToCompletionsJSON(t *testing.T) {
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(providers.APIOpenAICompletions), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{protocol.Text{Text: " "}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}
	msgs := completionMessages(t, completionsReplayBody(t, history, nil))
	require.Len(t, msgs, 3)
	assert.Equal(t, "next", msgs[2].(map[string]any)["content"])
	assert.NotContains(t, mustJSONReplay(t, msgs), `"role":"assistant"`)
}

func mustJSONReplay(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
