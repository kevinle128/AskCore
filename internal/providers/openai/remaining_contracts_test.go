package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestCompletionsFullReplayShapeJSON(t *testing.T) {
	m := completionsModel("")
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "look"}, protocol.Image{Data: "YQ==", MimeType: "image/png"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.Text{Text: "calling"}, protocol.ToolCall{ID: "same|id", Name: "echo", Arguments: json.RawMessage(`{"x":1}`)}}},
		protocol.ToolResultMessage{ToolCallID: "same|id", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}}},
	}
	msgs := completionMessages(t, completionsReplayBody(t, history, func(m *providers.Model) { m.Input = []string{"text", "image"} }))
	require.Len(t, msgs, 4)
	assert.Equal(t, "system", msgs[0].(map[string]any)["role"])
	user := msgs[1].(map[string]any)
	assert.Equal(t, "user", user["role"])
	parts := user["content"].([]any)
	assert.Equal(t, "look", parts[0].(map[string]any)["text"])
	assert.Equal(t, "data:image/png;base64,YQ==", parts[1].(map[string]any)["image_url"].(map[string]any)["url"])
	assistant := msgs[2].(map[string]any)
	assert.Equal(t, "assistant", assistant["role"])
	assert.Equal(t, "calling", assistant["content"])
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, "same|id", call["id"])
	fn := call["function"].(map[string]any)
	assert.Equal(t, "echo", fn["name"])
	assert.JSONEq(t, `{"x":1}`, fn["arguments"].(string))
	result := msgs[3].(map[string]any)
	assert.Equal(t, "tool", result["role"])
	assert.Equal(t, "same|id", result["tool_call_id"])
	assert.Equal(t, "done", result["content"])
}

func TestCompletionsMidConversationSystemAndToolChangesJSON(t *testing.T) {
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.SystemMessage{Content: []protocol.Text{{Text: "new rule"}}, ToolsAdded: []protocol.ToolDecl{{Name: "new_tool", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "second"}}},
	}
	body := completionsReplayBody(t, history, nil)
	msgs := completionMessages(t, body)
	assert.Contains(t, mustJSONReplay(t, msgs), "new rule")
	tools := body["tools"].([]any)
	require.Len(t, tools, 1)
	assert.Equal(t, "new_tool", tools[0].(map[string]any)["function"].(map[string]any)["name"])
}

func TestCompletionsReplayHoldsSystemUntilSyntheticResultJSON(t *testing.T) {
	sig := "foreign-signature"
	history := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig}, protocol.ToolCall{ID: "call|missing", Name: "echo", Arguments: json.RawMessage(`{}`)}}},
		protocol.SystemMessage{Content: []protocol.Text{{Text: "later rule"}}},
		protocol.AssistantMessage{StopReason: protocol.StopAborted, Content: []protocol.AssistantBlock{protocol.Text{Text: "aborted"}}},
		protocol.AssistantMessage{StopReason: protocol.StopError, Content: []protocol.AssistantBlock{protocol.Text{Text: "errored"}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}
	msgs := completionMessages(t, completionsReplayBody(t, history, nil))
	require.Len(t, msgs, 6)
	assistant := msgs[2].(map[string]any)
	assert.Equal(t, "plan", assistant["content"])
	assert.NotContains(t, assistant, "reasoning_content")
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, "call_missing", call["id"])
	result := msgs[3].(map[string]any)
	assert.Equal(t, "call_missing", result["tool_call_id"])
	assert.Contains(t, result["content"], "No result provided")
	assert.Contains(t, msgs[4].(map[string]any)["content"], "later rule")
	assert.NotContains(t, mustJSONReplay(t, msgs), "aborted")
	assert.NotContains(t, mustJSONReplay(t, msgs), "errored")
	assert.NotContains(t, mustJSONReplay(t, msgs), sig)
}

func TestCompletionsForeignToolIDCapAndSameModelIdentityJSON(t *testing.T) {
	long := "call_x|" + strings.Repeat("z", 100)
	for _, tc := range []struct{ name, api, provider, model, id, want string }{
		{"foreign-long", string(providers.APIAnthropicMessages), providers.ProviderTokenPlan, providers.ModelDeepSeekFlash, long, ""},
		{"same-model", string(providers.APIOpenAICompletions), providers.ProviderTokenPlan, providers.ModelDeepSeekFlash, "same|id", "same|id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}, protocol.AssistantMessage{API: tc.api, Provider: tc.provider, Model: tc.model, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.ToolCall{ID: tc.id, Name: "echo", Arguments: json.RawMessage(`{}`)}}}, protocol.ToolResultMessage{ToolCallID: tc.id, ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}}}
			msgs := completionMessages(t, completionsReplayBody(t, history, nil))
			id := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"].(string)
			if tc.want != "" {
				assert.Equal(t, tc.want, id)
			} else {
				assert.Len(t, id, 40)
				assert.NotContains(t, id, "|")
			}
			assert.Equal(t, id, msgs[3].(map[string]any)["tool_call_id"])
		})
	}
}

func TestCompletionsFinishReasonVariants(t *testing.T) {
	for _, tc := range []struct {
		name, finish string
		supports     bool
		want         protocol.StopReason
		wantError    bool
	}{
		{"stop", "stop", true, protocol.StopStop, false},
		{"length", "length", true, protocol.StopLength, false},
		{"tools", "tool_calls", true, protocol.StopToolUse, false},
		{"unknown", "mystery", true, protocol.StopError, true},
		{"infer-stop", "", false, protocol.StopStop, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finish := tc.finish
			if finish == "" {
				finish = "null"
			} else {
				finish = `"` + finish + `"`
			}
			sse := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":`+finish+`}]}`) + "data: [DONE]\n\n"
			srv := newCaptureServer(t, sse)
			m := completionsModel(srv.srv.URL)
			m.Compat = providers.CompletionsCompat{ThinkingFormat: "qwen", SupportsFinishReason: tc.supports}
			msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{}))
			_ = srv.take(t)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, msg.StopReason)
		})
	}
}

func TestCompletionsFinishInferenceRequiresTerminalAndCompleteTool(t *testing.T) {
	toolChunk := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo","arguments":"{}"}}]},"finish_reason":null}]}`)
	textChunk := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":null}]}`)
	for _, tc := range []struct {
		name, sse string
		want      protocol.StopReason
		fail      bool
	}{
		{"tool-done", toolChunk + "data: [DONE]\n\n", protocol.StopToolUse, false},
		{"text-truncated", textChunk, protocol.StopError, true},
		{"empty-done", "data: [DONE]\n\n", protocol.StopError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCaptureServer(t, tc.sse)
			m := completionsModel(srv.srv.URL)
			m.Compat = providers.CompletionsCompat{ThinkingFormat: "qwen", SupportsFinishReason: false}
			msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{}))
			_ = srv.take(t)
			if tc.fail {
				require.ErrorIs(t, err, providers.ErrStreamIncomplete)
			} else {
				require.NoError(t, err)
				require.Len(t, msg.Content, 1)
				assert.Equal(t, "call_a", msg.Content[0].(protocol.ToolCall).ID)
			}
			assert.Equal(t, tc.want, msg.StopReason)
		})
	}
}

func TestCompletionsAbortKeepsPartialAndErrorBodyIsBounded(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":null}]}`))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := completionsProvider(srv, 0).Stream(ctx, completionsModel(srv.URL), helloReq(), providers.StreamOptions{})
	<-started
	var sawDelta bool
	for ev := range stream.Events() {
		if _, ok := ev.Event.(protocol.TextDeltaEvent); ok {
			sawDelta = true
			cancel()
		}
	}
	msg, err := stream.Result(context.Background())
	require.Error(t, err)
	assert.True(t, sawDelta)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	require.NotEmpty(t, msg.Content)
	assert.Equal(t, "partial", msg.Content[0].(protocol.Text).Text)
	require.NotNil(t, msg.ErrorMessage)
	assert.Equal(t, "Request was aborted", *msg.ErrorMessage)
	encoded, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "rawToolJSON")

	errorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, strings.Repeat("x", 4000))
	}))
	t.Cleanup(errorSrv.Close)
	failed, err := resultOf(t, completionsProvider(errorSrv, 0).Stream(context.Background(), completionsModel(errorSrv.URL), helloReq(), providers.StreamOptions{}))
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, failed.StopReason)
	require.NotNil(t, failed.ErrorMessage)
	assert.Contains(t, *failed.ErrorMessage, "HTTP 502")
	assert.LessOrEqual(t, len(*failed.ErrorMessage), 540)
}

func TestResponsesDropsFailedReasoningOnlyTurnsJSON(t *testing.T) {
	for _, stop := range []protocol.StopReason{protocol.StopAborted, protocol.StopError} {
		for _, signed := range []bool{false, true} {
			sig := "opaque"
			thinking := protocol.Thinking{Thinking: "secret"}
			if signed {
				thinking.ThinkingSignature = &sig
			}
			errText := "failed"
			input := responsesReplayInput(t, []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}}, protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: stop, ErrorMessage: &errText, Content: []protocol.AssistantBlock{thinking, protocol.Text{Text: ""}}}, protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}}})
			assert.Empty(t, responseItems(input, "reasoning"))
			for _, item := range responseItems(input, "message") {
				assert.NotEqual(t, "assistant", item["role"])
			}
			assert.NotContains(t, mustJSONReplay(t, input), "secret")
			assert.NotContains(t, mustJSONReplay(t, input), "opaque")
			assert.NotContains(t, mustJSONReplay(t, input), "failed")
		}
	}
}

func TestResponsesParallelMissingResultOrderJSON(t *testing.T) {
	const api = string(providers.APIAnthropicMessages)
	history := []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}}, protocol.AssistantMessage{API: api, Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.ToolCall{ID: "one|a", Name: "echo", Arguments: json.RawMessage(`{}`)}, protocol.ToolCall{ID: "two|b", Name: "echo", Arguments: json.RawMessage(`{}`)}, protocol.ToolCall{ID: "three|c", Name: "echo", Arguments: json.RawMessage(`{}`)}}}, protocol.ToolResultMessage{ToolCallID: "one|a", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "one done"}}}, protocol.ToolResultMessage{ToolCallID: "three|c", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "three done"}}}, protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}}}
	input := responsesReplayInput(t, history)
	transformed := providers.TransformMessages(history, responsesModel(""), func() int64 { return 42 }, providers.NormalizeResponsesToolCallID)
	var synthetic *protocol.ToolResultMessage
	for _, raw := range transformed {
		if result, ok := raw.(protocol.ToolResultMessage); ok && result.ToolCallID == "two_b" {
			synthetic = &result
		}
	}
	require.NotNil(t, synthetic)
	assert.True(t, synthetic.IsError)
	assert.Equal(t, "No result provided", synthetic.Content[0].(protocol.Text).Text)
	var calls, results []string
	var next int
	for i, raw := range input {
		item := raw.(map[string]any)
		switch item["type"] {
		case "function_call":
			calls = append(calls, item["call_id"].(string))
		case "function_call_output":
			results = append(results, item["call_id"].(string))
			if item["call_id"] == "two_b" {
				assert.Contains(t, item["output"], "No result provided")
			}
		}
		if item["role"] == "user" && strings.Contains(mustJSONReplay(t, item), "next") {
			next = i
		}
	}
	assert.Equal(t, []string{"one_a", "two_b", "three_c"}, calls)
	assert.Equal(t, []string{"one_a", "three_c", "two_b"}, results)
	assert.Equal(t, len(input)-1, next)
}

func TestResponsesThinkingIncludeAndOpaqueReasoningRoundTripJSON(t *testing.T) {
	for _, tc := range []struct {
		level   protocol.ThinkingLevel
		include bool
	}{{protocol.ThinkingOff, false}, {protocol.ThinkingHigh, true}} {
		srv := newCaptureServer(t, responsesSSE())
		_, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), responsesModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: tc.level}))
		require.NoError(t, err)
		body := srv.take(t).body(t)
		assert.Equal(t, false, body["store"])
		include, has := body["include"]
		if tc.include {
			require.True(t, has)
			assert.Contains(t, include, "reasoning.encrypted_content")
		}
	}
	sse := responsesEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_7","type":"reasoning","encrypted_content":"opaque-start","summary":[]}}`) +
		responsesEvent("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_7","type":"reasoning","encrypted_content":"opaque-final","summary":[{"type":"summary_text","text":"plan"}]}}`) +
		responsesEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_7","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	srv := newCaptureServer(t, sse)
	m := responsesModel(srv.srv.URL)
	msg, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingHigh}))
	require.NoError(t, err)
	_ = srv.take(t)
	var thought protocol.Thinking
	for _, block := range msg.Content {
		if v, ok := block.(protocol.Thinking); ok {
			thought = v
		}
	}
	require.NotNil(t, thought.ThinkingSignature)
	replay, ok := providers.DecodeThinkingSignature(*thought.ThinkingSignature)
	require.True(t, ok)
	assert.Equal(t, "rs_7", replay.ID)
	assert.Equal(t, "opaque-final", replay.EncryptedContent)
	require.Len(t, replay.Summary, 1)
	assert.Equal(t, "plan", replay.Summary[0].Text)
	input := responsesReplayInput(t, []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}}, msg, protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}}})
	reasoning := responseItems(input, "reasoning")
	require.Len(t, reasoning, 1)
	assert.Equal(t, "rs_7", reasoning[0]["id"])
	assert.Equal(t, "opaque-final", reasoning[0]["encrypted_content"])
	assert.Equal(t, "plan", reasoning[0]["summary"].([]any)[0].(map[string]any)["text"])
}

func TestResponsesForeignTextSignatureAndThinkingBecomePlainTextJSON(t *testing.T) {
	sig := "anthropic-secret"
	textSig := providers.EncodeTextSignature("msg_foreign", "final_answer")
	input := responsesReplayInput(t, []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}}, protocol.AssistantMessage{API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{protocol.Thinking{Thinking: "foreign plan", ThinkingSignature: &sig}, protocol.Text{Text: "foreign answer", TextSignature: &textSig}}}})
	assert.Empty(t, responseItems(input, "reasoning"))
	raw := mustJSONReplay(t, input)
	assert.Contains(t, raw, "foreign plan")
	assert.Contains(t, raw, "foreign answer")
	assert.NotContains(t, raw, sig)
	assert.NotContains(t, raw, "msg_foreign")
}
