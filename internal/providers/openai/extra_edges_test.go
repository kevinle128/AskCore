package openai

import (
	"context"
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestCompletionsBlankAssistantAndContextClampJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	m.ContextWindow = 4100
	m.MaxTokens = 100
	sig := "reasoning_content"
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "base", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Text{Text: " "}, protocol.Thinking{Thinking: " ", ThinkingSignature: &sig},
		}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	_, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{}))
	require.NoError(t, err)
	body := srv.take(t).body(t)
	msgs := completionMessages(t, body)
	require.Len(t, msgs, 3)
	assert.Equal(t, "next", msgs[2].(map[string]any)["content"])
	limit := body["max_tokens"].(float64)
	assert.GreaterOrEqual(t, limit, 1.0)
	assert.LessOrEqual(t, limit, 4.0)
}

func orphanReplayHistory() []protocol.Message {
	assistant := func(reason protocol.StopReason, id string) protocol.AssistantMessage {
		return protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: reason, Content: []protocol.AssistantBlock{
			protocol.ToolCall{ID: id, Name: "echo", Arguments: json.RawMessage(`{}`)},
		}}
	}
	result := func(id string) protocol.ToolResultMessage {
		return protocol.ToolResultMessage{ToolCallID: id, ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: id + " result"}}}
	}
	return []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		assistant(protocol.StopAborted, "aborted_call"), result("aborted_call"),
		assistant(protocol.StopToolUse, "valid_call"), result("valid_call"),
		assistant(protocol.StopError, "errored_call"), result("errored_call"),
	}
}

func TestReplayDropsOrphanResultsAndKeepsValidPairCompletionsJSON(t *testing.T) {
	msgs := completionMessages(t, completionsReplayBody(t, orphanReplayHistory(), nil))
	var calls, results int
	for _, raw := range msgs {
		msg := raw.(map[string]any)
		switch msg["role"] {
		case "assistant":
			calls++
			toolCalls := msg["tool_calls"].([]any)
			require.Len(t, toolCalls, 1)
			assert.Equal(t, "valid_call", toolCalls[0].(map[string]any)["id"])
		case "tool":
			results++
			assert.Equal(t, "valid_call", msg["tool_call_id"])
		}
	}
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, results)
	assert.NotContains(t, mustJSONReplay(t, msgs), "aborted_call")
	assert.NotContains(t, mustJSONReplay(t, msgs), "errored_call")
}

func TestReplayDropsOrphanResultsAndKeepsValidPairResponsesJSON(t *testing.T) {
	input := responsesReplayInput(t, orphanReplayHistory())
	calls := responseItems(input, "function_call")
	results := responseItems(input, "function_call_output")
	require.Len(t, calls, 1)
	require.Len(t, results, 1)
	assert.Equal(t, "valid_call", calls[0]["call_id"])
	assert.Equal(t, "valid_call", results[0]["call_id"])
	assert.NotContains(t, mustJSONReplay(t, input), "aborted_call")
	assert.NotContains(t, mustJSONReplay(t, input), "errored_call")
}

func TestCompletionsReasoningStubCompatJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	m.Reasoning = true
	m.Compat = providers.CompletionsCompat{ThinkingFormat: "qwen", RequiresReasoningContentOnAssistantMessages: true, SupportsFinishReason: true}
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "base", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{protocol.Text{Text: "answer"}}},
	}})
	_, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{}))
	require.NoError(t, err)
	msgs := completionMessages(t, srv.take(t).body(t))
	assert.Equal(t, "", msgs[2].(map[string]any)["reasoning_content"])
}

func TestResponsesOmitToolsWhenNoneJSON(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	m := responsesModel(srv.srv.URL)
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "base", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
	}})
	_, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{}))
	require.NoError(t, err)
	body := srv.take(t).body(t)
	assert.NotContains(t, body, "tools")
}

func TestResponsesToolResultTextPlaceholdersJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []protocol.UserBlock
		want    string
	}{
		{"empty", nil, "(no tool output)"},
		{"image", []protocol.UserBlock{protocol.Image{Data: "YQ==", MimeType: "image/png"}}, "(see attached image)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := responsesReplayInput(t, []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
				protocol.AssistantMessage{API: string(providers.APIOpenAIResponses), Provider: providers.ProviderOpenAI, Model: providers.ModelGPT55, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.ToolCall{ID: "call_1|fc_1", Name: "echo"}}},
				protocol.ToolResultMessage{ToolCallID: "call_1|fc_1", ToolName: "echo", Content: tc.content},
			})
			results := responseItems(input, "function_call_output")
			require.Len(t, results, 1)
			assert.Equal(t, tc.want, results[0]["output"])
		})
	}
}

func TestResponsesFirstSystemRoleByModelJSON(t *testing.T) {
	for _, tc := range []struct {
		name, modelID, wantRole string
		reasoning               bool
	}{
		{"reasoning", providers.ModelGPT55, "developer", true},
		{"plain", "gpt-4.1", "system", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCaptureServer(t, responsesSSE())
			m := responsesModel(srv.srv.URL)
			m.ID, m.Reasoning = tc.modelID, tc.reasoning
			req := providers.NormalizeRequest(providers.Request{SystemPrompt: "base-system", Messages: []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}}})
			_, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{}))
			require.NoError(t, err)
			body := srv.take(t).body(t)
			input := body["input"].([]any)
			require.NotEmpty(t, input)
			first := input[0].(map[string]any)
			assert.Equal(t, tc.wantRole, first["role"])
			assert.Contains(t, mustJSONReplay(t, first), "base-system")
		})
	}
}

func TestOpenAIWiresReplaceInvalidUTF8InRequestJSON(t *testing.T) {
	bad := string([]byte{'x', 0xff, 'y'})
	want := "x\ufffdy"
	for _, api := range []string{"completions", "responses"} {
		t.Run(api, func(t *testing.T) {
			sse := responsesSSE()
			if api == "completions" {
				sse = completionsSSE()
			}
			srv := newCaptureServer(t, sse)
			req := providers.NormalizeRequest(providers.Request{SystemPrompt: "base", Messages: []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: bad}}}}})
			var p providers.Provider
			var model providers.Model
			if api == "completions" {
				p, model = completionsProvider(srv.srv, 0), completionsModel(srv.srv.URL)
			} else {
				p, model = responsesProvider(srv.srv, 0), responsesModel(srv.srv.URL)
			}
			_, err := resultOf(t, p.Stream(context.Background(), model, req, providers.StreamOptions{}))
			require.NoError(t, err)
			got := srv.take(t)
			assert.True(t, utf8.Valid(got.raw))
			body := got.body(t)
			if api == "completions" {
				msgs := completionMessages(t, body)
				assert.Equal(t, want, msgs[1].(map[string]any)["content"])
			} else {
				input := body["input"].([]any)
				user := input[1].(map[string]any)
				parts := user["content"].([]any)
				assert.Equal(t, want, parts[0].(map[string]any)["text"])
			}
		})
	}
}

func TestTokenPlanCompletionsOmitsUnsupportedCacheFieldsJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	_, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{
		SessionID: "session-1", CacheRetention: providers.CacheRetentionLong,
	}))
	require.NoError(t, err)
	got := srv.take(t)
	body := got.body(t)
	assert.NotContains(t, body, "prompt_cache_key")
	assert.NotContains(t, body, "prompt_cache_retention")
	assert.Empty(t, got.headers.Get("X-Session-Id"))
	assert.Empty(t, got.headers.Get("X-Client-Request-Id"))
	assert.Empty(t, got.headers.Get("X-Session-Affinity"))
}

func TestCompletionsCapturesResponseIDAndRawStopReason(t *testing.T) {
	sse := chatChunk(`{"id":"chatcmpl-1","model":"served-model","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","model":"served-model","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n"
	srv := newCaptureServer(t, sse)
	msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{}))
	require.NoError(t, err)
	require.NotNil(t, msg.ResponseID)
	assert.Equal(t, "chatcmpl-1", *msg.ResponseID)
	require.NotNil(t, msg.ResponseModel)
	assert.Equal(t, "served-model", *msg.ResponseModel)
	require.NotNil(t, msg.RawStopReason)
	assert.Equal(t, "stop", *msg.RawStopReason)
	_ = srv.take(t)
}
