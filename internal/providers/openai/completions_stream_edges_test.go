package openai

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestCompletionsReasoningDeltaFieldsJSON(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
		t.Run(field, func(t *testing.T) {
			sse := chatChunk(fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","%s":"plan"},"finish_reason":null}]}`, field)) +
				chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n"
			srv := newCaptureServer(t, sse)
			msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingHigh}))
			require.NoError(t, err)
			_ = srv.take(t)
			var thoughts []protocol.Thinking
			for _, block := range msg.Content {
				if v, ok := block.(protocol.Thinking); ok {
					thoughts = append(thoughts, v)
				}
			}
			require.Len(t, thoughts, 1)
			assert.Equal(t, "plan", thoughts[0].Thinking)
			require.NotNil(t, thoughts[0].ThinkingSignature)
			assert.Equal(t, field, *thoughts[0].ThinkingSignature)
		})
	}
}

func TestCompletionsReasoningDeltaPrecedenceJSON(t *testing.T) {
	for _, tc := range []struct {
		name, delta, wantText, wantField string
	}{
		{"first", `"reasoning_content":"first","reasoning":"second","reasoning_text":"third"`, "first", "reasoning_content"},
		{"second", `"reasoning_content":"","reasoning":"second","reasoning_text":"third"`, "second", "reasoning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sse := chatChunk(fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant",%s},"finish_reason":null}]}`, tc.delta)) +
				chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n"
			srv := newCaptureServer(t, sse)
			msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{}))
			require.NoError(t, err)
			require.Len(t, msg.Content, 1)
			thought := msg.Content[0].(protocol.Thinking)
			assert.Equal(t, tc.wantText, thought.Thinking)
			require.NotNil(t, thought.ThinkingSignature)
			assert.Equal(t, tc.wantField, *thought.ThinkingSignature)
			_ = srv.take(t)
		})
	}
}

func TestCompletionsOneTextAndThinkingBlockEndsJSON(t *testing.T) {
	sse := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"plan","content":"one"},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":" more","content":" two"},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n"
	srv := newCaptureServer(t, sse)
	stream := completionsProvider(srv.srv, 0).Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingHigh})
	var starts, ends int
	for event := range stream.Events() {
		switch event.Event.(type) {
		case protocol.ThinkingStartEvent, protocol.TextStartEvent:
			starts++
		case protocol.ThinkingEndEvent, protocol.TextEndEvent:
			ends++
		}
	}
	msg, err := stream.Result(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, starts)
	assert.Equal(t, 2, ends)
	assert.Len(t, msg.Content, 2)
	assert.Equal(t, "plan more", msg.Content[0].(protocol.Thinking).Thinking)
	assert.Equal(t, "one two", msg.Content[1].(protocol.Text).Text)
	_ = srv.take(t)
}

func TestCompletionsMissingToolIDGetsStableIndexIDJSON(t *testing.T) {
	sse := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":3,"type":"function","function":{"name":"echo","arguments":"{}"}}]},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`) + "data: [DONE]\n\n"
	srv := newCaptureServer(t, sse)
	msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{}))
	require.NoError(t, err)
	_ = srv.take(t)
	var ids []string
	for _, block := range msg.Content {
		if call, ok := block.(protocol.ToolCall); ok {
			ids = append(ids, call.ID)
		}
	}
	require.Len(t, ids, 1)
	assert.True(t, strings.HasPrefix(ids[0], "tool-call-"))
	assert.NotEmpty(t, ids[0])
}

func TestCompletionsInterleavedToolDeltasKeepIndexIDsJSON(t *testing.T) {
	sse := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo","arguments":"{\"a\":"}}]},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"echo","arguments":"{\"b\":"}}]},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"2}"}}]},"finish_reason":"tool_calls"}]}`) + "data: [DONE]\n\n"
	srv := newCaptureServer(t, sse)
	msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{}))
	require.NoError(t, err)
	assert.Equal(t, protocol.StopToolUse, msg.StopReason)
	var calls []protocol.ToolCall
	for _, block := range msg.Content {
		if call, ok := block.(protocol.ToolCall); ok {
			calls = append(calls, call)
		}
	}
	require.Len(t, calls, 2)
	assert.Equal(t, "call_a", calls[0].ID)
	assert.JSONEq(t, `{"a":1}`, string(calls[0].Arguments))
	assert.Equal(t, "call_b", calls[1].ID)
	assert.JSONEq(t, `{"b":2}`, string(calls[1].Arguments))
	_ = srv.take(t)
}
