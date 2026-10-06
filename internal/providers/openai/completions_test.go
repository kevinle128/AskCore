package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestCompletionsQwenOffNoDeveloperNoStub(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	p := completionsProvider(srv.srv, 0)
	s := p.Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
	msg, err := resultOf(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopStop, msg.StopReason)

	got := srv.take(t)
	body := got.body(t)
	assert.Equal(t, false, body["enable_thinking"])
	assert.NotContains(t, body, "store")
	assert.NotContains(t, body, "stream_options")
	assert.False(t, stubUserPresent(body), "the ExtraBody must replace the fantasy stub user text")
	if stubUserPresent(body) {
		t.Fatalf("stub prompt leaked; captured request JSON: %s", got.raw)
	}
	for _, m := range body["messages"].([]any) {
		assert.NotEqual(t, "developer", m.(map[string]any)["role"])
	}
	assert.NotContains(t, string(got.raw), `"role":"developer"`)
}

func TestCompletionsQwenThinkingOnRequest(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	p := completionsProvider(srv.srv, 0)
	s := p.Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingHigh})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	body := srv.take(t).body(t)
	assert.Equal(t, true, body["enable_thinking"])
	assert.NotContains(t, body, "reasoning_effort")
	assert.NotContains(t, body, "store")
}

func TestCompletionsCompatRoleLimitAndThinkingMapJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	m.ThinkingLevelMap = providers.ThinkingLevelMap{protocol.ThinkingHigh: strPtr("high-mapped")}
	m.Compat = providers.CompletionsCompat{
		ThinkingFormat:          "openai",
		SupportsDeveloperRole:   true,
		SupportsReasoningEffort: true,
		SupportsFinishReason:    true,
		MaxTokensField:          "max_completion_tokens",
	}
	stream := completionsProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingHigh, MaxTokens: 123})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	body := srv.take(t).body(t)
	assert.Equal(t, "high-mapped", body["reasoning_effort"])
	assert.EqualValues(t, 123, body["max_completion_tokens"])
	assert.NotContains(t, body, "max_tokens")
	msgs := body["messages"].([]any)
	assert.Equal(t, "developer", msgs[0].(map[string]any)["role"])
}

func TestCompletionsSystemRoleCompatJSON(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reasoning bool
		developer bool
		firstRole string
	}{
		{"reasoning-developer", true, true, "developer"},
		{"reasoning-system", true, false, "system"},
		{"plain-system", false, true, "system"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCaptureServer(t, completionsSSE())
			m := completionsModel(srv.srv.URL)
			m.Reasoning = tc.reasoning
			m.Compat = providers.CompletionsCompat{ThinkingFormat: "qwen", SupportsDeveloperRole: tc.developer, SupportsFinishReason: true}
			req := providers.NormalizeRequest(providers.Request{SystemPrompt: "first-system", Messages: []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
				protocol.SystemMessage{Content: []protocol.Text{{Text: "later-system"}}},
			}})
			stream := completionsProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{Reasoning: protocol.ThinkingOff})
			_, err := resultOf(t, stream)
			require.NoError(t, err)
			msgs := srv.take(t).body(t)["messages"].([]any)
			require.Len(t, msgs, 3)
			assert.Equal(t, tc.firstRole, msgs[0].(map[string]any)["role"])
			assert.Equal(t, "user", msgs[2].(map[string]any)["role"])
			assert.Contains(t, msgs[2].(map[string]any)["content"], "later-system")
		})
	}
}

func TestCompletionsStoreAndStreamingUsageCompatJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	m.Compat = providers.CompletionsCompat{
		ThinkingFormat:        "qwen",
		SupportsStore:         true,
		SupportsStreamOptions: true,
		SupportsFinishReason:  true,
	}
	stream := completionsProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	body := srv.take(t).body(t)
	assert.Equal(t, false, body["store"])
	opts, ok := body["stream_options"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, opts["include_usage"])
}

func TestCompletionsForeignToolIDNormalized(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	p := completionsProvider(srv.srv, 0)
	req := providers.NormalizeRequest(providers.Request{
		SystemPrompt: "you are a tool",
		Messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.AssistantMessage{
				API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash,
				StopReason: protocol.StopToolUse,
				Content: []protocol.AssistantBlock{
					protocol.ToolCall{ID: "call|item-foreign", Name: "echo", Arguments: json.RawMessage(`{}`)},
				},
			},
			protocol.ToolResultMessage{ToolCallID: "call|item-foreign", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}},
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
		},
	})
	s := p.Stream(context.Background(), completionsModel(srv.srv.URL), req, providers.StreamOptions{Reasoning: protocol.ThinkingOff})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	raw := string(srv.take(t).raw)
	assert.NotContains(t, raw, "call|item-foreign")
	assert.Contains(t, raw, "call_item-foreign")
}

func TestCompletionsSameModelReasoningFieldReplayJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	sig := "reasoning_content"
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "leading", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "step one", ThinkingSignature: &sig},
			protocol.Thinking{Thinking: "step two", ThinkingSignature: &sig},
			protocol.Text{Text: "answer"},
		}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	stream := completionsProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	msgs := srv.take(t).body(t)["messages"].([]any)
	var found bool
	for _, raw := range msgs {
		msg := raw.(map[string]any)
		if msg["role"] != "assistant" {
			continue
		}
		found = true
		assert.Equal(t, "step one\nstep two", msg["reasoning_content"])
		assert.Equal(t, "answer", msg["content"])
	}
	assert.True(t, found)
}

func TestCompletionsThinkingAsTextReplayJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	m.Compat = providers.CompletionsCompat{ThinkingFormat: "qwen", RequiresThinkingAsText: true, SupportsFinishReason: true}
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "leading", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "step one"},
			protocol.Thinking{Thinking: "step two"},
			protocol.Text{Text: "answer"},
		}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	stream := completionsProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	msgs := srv.take(t).body(t)["messages"].([]any)
	var found bool
	for _, raw := range msgs {
		msg := raw.(map[string]any)
		if msg["role"] != "assistant" {
			continue
		}
		found = true
		content, ok := msg["content"].([]any)
		require.True(t, ok)
		require.Len(t, content, 2)
		assert.Equal(t, "step one\n\nstep two", content[0].(map[string]any)["text"])
		assert.Equal(t, "answer", content[1].(map[string]any)["text"])
		assert.NotContains(t, msg, "reasoning_content")
	}
	assert.True(t, found)
}

func TestCompletionsToolResultImagesFollowRunJSON(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "leading", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
			protocol.ToolCall{ID: "call_a", Name: "echo", Arguments: json.RawMessage(`{}`)},
			protocol.ToolCall{ID: "call_b", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "call_a", ToolName: "echo", Content: []protocol.UserBlock{protocol.Image{MimeType: "image/png", Data: "aGk="}}},
		protocol.ToolResultMessage{ToolCallID: "call_b", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "done"}, protocol.Image{MimeType: "image/png", Data: "aGk="}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	stream := completionsProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	msgs := srv.take(t).body(t)["messages"].([]any)
	var toolsSeen int
	var imageMessages int
	for _, raw := range msgs {
		msg := raw.(map[string]any)
		switch msg["role"] {
		case "tool":
			toolsSeen++
			switch msg["tool_call_id"] {
			case "call_a":
				assert.Equal(t, "(see attached image)", msg["content"])
			case "call_b":
				assert.Equal(t, "done", msg["content"])
			}
		case "user":
			content, ok := msg["content"].([]any)
			if !ok {
				continue
			}
			imageMessages++
			assert.Len(t, content, 3)
		}
	}
	assert.Equal(t, 2, toolsSeen)
	assert.Equal(t, 1, imageMessages)
}

func TestCompletionsSecondSystemMessageIsPresent(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	p := completionsProvider(srv.srv, 0)
	req := providers.NormalizeRequest(providers.Request{
		SystemPrompt: "leading",
		Messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.SystemMessage{Content: []protocol.Text{{Text: "later-system-update"}}},
		},
	})
	s := p.Stream(context.Background(), completionsModel(srv.srv.URL), req, providers.StreamOptions{Reasoning: protocol.ThinkingOff})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	got := srv.take(t)
	assert.Contains(t, string(got.raw), "later-system-update")
	var userLater bool
	for _, m := range got.body(t)["messages"].([]any) {
		msg := m.(map[string]any)
		if msg["role"] == "developer" {
			t.Fatalf("developer role must not appear: %s", got.raw)
		}
		if msg["role"] == "user" {
			if s, ok := msg["content"].(string); ok && strings.Contains(s, "later-system-update") {
				userLater = true
			}
		}
	}
	assert.True(t, userLater)
}

func TestCompletionsUsageAfterEmptyChoicesKeepsEarlier(t *testing.T) {
	var b strings.Builder
	b.WriteString(chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	b.WriteString(chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[]}`))
	b.WriteString(chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`))
	b.WriteString("data: [DONE]\n\n")
	srv := newCaptureServer(t, b.String())
	p := completionsProvider(srv.srv, 0)
	s := p.Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
	msg, err := resultOf(t, s)
	require.NoError(t, err)
	assert.Equal(t, int64(10), msg.Usage.Input)
	assert.Equal(t, int64(2), msg.Usage.Output)
	assert.Equal(t, protocol.Cost{}, msg.Usage.Cost)
	_ = srv.take(t)
}

func TestCompletionsUsageCacheAndReasoningSubset(t *testing.T) {
	sse := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`) +
		chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":100000,"completion_tokens":2000,"total_tokens":102000,"prompt_tokens_details":{"cached_tokens":5000},"completion_tokens_details":{"reasoning_tokens":500}}}`) +
		"data: [DONE]\n\n"
	srv := newCaptureServer(t, sse)
	msg, err := resultOf(t, completionsProvider(srv.srv, 0).Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{}))
	require.NoError(t, err)
	assert.Equal(t, int64(95000), msg.Usage.Input)
	assert.Equal(t, int64(5000), msg.Usage.CacheRead)
	assert.Equal(t, int64(2000), msg.Usage.Output)
	require.NotNil(t, msg.Usage.Reasoning)
	assert.Equal(t, int64(500), *msg.Usage.Reasoning)
	assert.Equal(t, int64(102000), msg.Usage.TotalTokens)
	assert.Equal(t, protocol.Cost{}, msg.Usage.Cost)
	_ = srv.take(t)
}

func TestCompletionsMissingFinishReasonIsIncomplete(t *testing.T) {
	var b strings.Builder
	b.WriteString(chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`))
	b.WriteString("data: [DONE]\n\n")
	srv := newCaptureServer(t, b.String())
	p := completionsProvider(srv.srv, 0)
	s := p.Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.ErrorIs(t, err, providers.ErrStreamIncomplete)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	_ = srv.take(t)
}

func TestCompletions500IsSingleRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"no","type":"busy"}}`))
	}))
	t.Cleanup(srv.Close)
	p := completionsProvider(srv, 0)
	s := p.Stream(context.Background(), completionsModel(srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, int32(1), hits.Load())
}

func TestCompletionsGoleakNormalAbortIdle(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		srv := newCaptureServer(t, completionsSSE())
		p := completionsProvider(srv.srv, 0)
		s := p.Stream(context.Background(), completionsModel(srv.srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
		_, err := resultOf(t, s)
		require.NoError(t, err)
		_ = srv.take(t)
	})
	t.Run("abort", func(t *testing.T) {
		started := make(chan struct{})
		srv := httptest.NewServer(abortHangHandler(t, started))
		t.Cleanup(srv.Close)
		p := completionsProvider(srv, 0)
		ctx, cancel := context.WithCancel(context.Background())
		s := p.Stream(ctx, completionsModel(srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
		<-started
		cancel()
		msg, err := resultOf(t, s)
		require.Error(t, err)
		assert.Equal(t, protocol.StopAborted, msg.StopReason)
	})
	t.Run("idle", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
				t.Error("idle cancel did not reach the request")
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		p := completionsProvider(srv, 80*time.Millisecond)
		start := time.Now()
		s := p.Stream(context.Background(), completionsModel(srv.URL), helloReq(), providers.StreamOptions{Reasoning: protocol.ThinkingOff})
		msg, err := resultOf(t, s)
		require.Error(t, err)
		assert.Equal(t, protocol.StopError, msg.StopReason)
		require.NotNil(t, msg.ErrorMessage)
		assert.Equal(t, "idle timeout", *msg.ErrorMessage)
		assert.Less(t, time.Since(start), 2*time.Second)
	})
}
