package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestStreamThinkingOffSendsDisabled(t *testing.T) {
	body, msg, err := streamOnce(t, streamOnceOpts{
		reasoning: protocol.ThinkingOff,
		sse:       textSSE("end_turn"),
	})
	require.NoError(t, err)
	assert.Equal(t, protocol.StopStop, msg.StopReason)
	th, ok := body["thinking"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"type": "disabled"}, th)
	_, hasEffort := body["output_config"]
	assert.False(t, hasEffort)
}

func TestStreamEmptyReasoningSendsMediumEffort(t *testing.T) {
	body, _, err := streamOnce(t, streamOnceOpts{
		sse: textSSE("end_turn"),
	})
	require.NoError(t, err)
	oc, ok := body["output_config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "medium", oc["effort"])
	_, hasThinking := body["thinking"]
	assert.False(t, hasThinking)
}

func TestStreamReplaysEmptySignature(t *testing.T) {
	empty := ""
	body, _, err := streamOnce(t, streamOnceOpts{
		messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.AssistantMessage{
				API: APIID, Provider: ProviderID, Model: ModelID,
				StopReason: protocol.StopStop,
				Content: []protocol.AssistantBlock{
					protocol.Thinking{Thinking: "prior", ThinkingSignature: &empty},
					protocol.Text{Text: "ok"},
				},
			},
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
		},
		sse: textSSE("end_turn"),
	})
	require.NoError(t, err)
	sig, ok := findThinkingSignature(body)
	require.True(t, ok)
	assert.Equal(t, "", sig)
}

func TestStreamToolSchemaKeepsRequired(t *testing.T) {
	raw, body, _, err := streamRaw(t, streamOnceOpts{
		tools: []protocol.ToolDecl{{
			Name:       "echo",
			Parameters: json.RawMessage(`{"type":"object","required":[],"properties":{}}`),
		}},
		sse: textSSE("end_turn"),
	})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"required":[]`)
	tools, ok := body["tools"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, tools)
	tool := tools[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	req, ok := schema["required"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{}, req)
}

func TestStreamMaxTokensPresent(t *testing.T) {
	body, _, err := streamOnce(t, streamOnceOpts{sse: textSSE("end_turn")})
	require.NoError(t, err)
	mt, ok := body["max_tokens"].(float64)
	require.True(t, ok)
	assert.GreaterOrEqual(t, mt, 1.0)
}

func TestStreamMissingTerminalEventIsIncomplete(t *testing.T) {
	for _, tc := range []struct{ name, sse string }{
		{"empty", ""},
		{"without-message-stop", strings.Replace(textSSE("end_turn"), sse("message_stop", `{"type":"message_stop"}`), "", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, msg, err := streamOnce(t, streamOnceOpts{sse: tc.sse})
			require.ErrorIs(t, err, providers.ErrStreamIncomplete)
			assert.Equal(t, protocol.StopError, msg.StopReason)
		})
	}
}

func TestStreamSecondSystemMessageIsPresent(t *testing.T) {
	raw, _, _, err := streamRaw(t, streamOnceOpts{
		messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.SystemMessage{Content: []protocol.Text{{Text: "later-system-update"}}},
		},
		sse: textSSE("end_turn"),
	})
	require.NoError(t, err)
	assert.Contains(t, string(raw), "later-system-update")
}

func TestStreamStubUserTextAbsent(t *testing.T) {
	body, _, err := streamOnce(t, streamOnceOpts{sse: textSSE("end_turn")})
	require.NoError(t, err)
	assert.False(t, stubUserPresent(body), "the ExtraBody must replace the fantasy stub user text")
}

func TestStreamMissingKeyMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
		t.Error("handler must not be called when the Ask keys are empty")
	}))
	t.Cleanup(srv.Close)

	p := New(
		WithHTTPClient(rewriteClient(srv)),
		WithEnv(func(k string) (string, bool) {
			if k == "ANTHROPIC_API_KEY" {
				return "must-not-be-used", true
			}
			return "", false
		}),
		WithNow(func() int64 { return 1 }),
	)
	s := p.Stream(context.Background(), Model(), providers.TranscriptRequest{}, providers.StreamOptions{})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Contains(t, *msg.ErrorMessage, "alibaba-token-plan")
	assert.Contains(t, *msg.ErrorMessage, "StreamOptions.APIKey")
	assert.Contains(t, *msg.ErrorMessage, askKeyEnv)
	assert.Contains(t, *msg.ErrorMessage, altKeyEnv)
	assert.Equal(t, int32(0), hits.Load())
}

func TestStreamContextWindowExceededIsError(t *testing.T) {
	_, msg, err := streamOnce(t, streamOnceOpts{
		sse: textSSE("model_context_window_exceeded"),
	})
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.NotEqual(t, protocol.StopLength, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Contains(t, *msg.ErrorMessage, "stop reason model_context_window_exceeded")
	require.NotNil(t, msg.RawStopReason)
	assert.Equal(t, "model_context_window_exceeded", *msg.RawStopReason)
}

func TestStream500IsSingleRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"busy","message":"no"}`))
	}))
	t.Cleanup(srv.Close)

	p := testProvider(srv)
	s := p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, int32(1), hits.Load())
}

func TestStreamUsageComesFromTheFinalCounters(t *testing.T) {
	_, msg, err := streamOnce(t, streamOnceOpts{
		sse: textSSEWithUsage("end_turn", 3, 40, 7, 2, 1),
	})
	require.NoError(t, err)
	assert.Equal(t, protocol.Usage{
		Input:       40,
		Output:      7,
		CacheRead:   2,
		CacheWrite:  1,
		TotalTokens: 50,
	}, msg.Usage)
}

func TestStreamDropsErroredAssistantAndSynthesizesResult(t *testing.T) {
	raw, _, _, err := streamRaw(t, streamOnceOpts{
		messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.AssistantMessage{
				API: APIID, Provider: ProviderID, Model: ModelID,
				StopReason:   protocol.StopError,
				ErrorMessage: strPtr("boom-should-not-appear"),
				Content: []protocol.AssistantBlock{
					protocol.ToolCall{ID: "gone", Name: "echo"},
				},
			},
			protocol.AssistantMessage{
				API: APIID, Provider: ProviderID, Model: ModelID,
				StopReason: protocol.StopToolUse,
				Content: []protocol.AssistantBlock{
					protocol.ToolCall{ID: "call_1", Name: "echo"},
				},
			},
		},
		sse: textSSE("end_turn"),
	})
	require.NoError(t, err)
	body := string(raw)
	assert.NotContains(t, body, "boom-should-not-appear")
	assert.NotContains(t, body, "gone")
	assert.Contains(t, body, "No result provided")
	assert.Contains(t, body, "call_1")
}

func TestStreamRekeysResponsesToolCallAndResultJSON(t *testing.T) {
	model := providers.OpenAIGPT55()
	raw, body, _, err := streamRaw(t, streamOnceOpts{
		messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
			protocol.AssistantMessage{API: string(model.API), Provider: model.Provider, Model: model.ID, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
				protocol.ToolCall{ID: "call_x|fc_y", Name: "echo", Arguments: json.RawMessage(`{}`)},
			}},
			protocol.ToolResultMessage{ToolCallID: "call_x|fc_y", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}},
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
		},
		sse: textSSE("end_turn"),
	})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "call_x|fc_y")
	msgs := body["messages"].([]any)
	var call, result bool
	for _, raw := range msgs {
		msg := raw.(map[string]any)
		if calls, ok := msg["content"].([]any); ok {
			for _, c := range calls {
				block := c.(map[string]any)
				switch block["type"] {
				case "tool_use":
					call = true
					assert.Equal(t, "call_x_fc_y", block["id"])
				case "tool_result":
					result = true
					assert.Equal(t, "call_x_fc_y", block["tool_use_id"])
				}
			}
		}
	}
	assert.True(t, call)
	assert.True(t, result)
}

func TestStreamIdleTimeoutAndActiveStream(t *testing.T) {
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

		p := keyedProvider(srv, 80*time.Millisecond)
		start := time.Now()
		s := p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{})
		msg, err := resultOf(t, s)
		require.Error(t, err)
		assert.Equal(t, protocol.StopError, msg.StopReason)
		require.NotNil(t, msg.ErrorMessage)
		assert.Equal(t, "idle timeout", *msg.ErrorMessage)
		assert.Less(t, time.Since(start), 2*time.Second)
	})

	t.Run("active", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			flusher, _ := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			writeFlush := func(s string) {
				_, _ = io.WriteString(w, s)
				if flusher != nil {
					flusher.Flush()
				}
			}
			writeFlush(sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`))
			for range 4 {
				time.Sleep(40 * time.Millisecond)
				writeFlush(": ping\n\n")
			}
			writeFlush(sse("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`))
			writeFlush(sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`))
			writeFlush(sse("content_block_stop", `{"type":"content_block_stop","index":0}`))
			writeFlush(sse("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`))
			writeFlush(sse("message_stop", `{"type":"message_stop"}`))
		}))
		t.Cleanup(srv.Close)

		p := keyedProvider(srv, 100*time.Millisecond)
		s := p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{})
		msg, err := resultOf(t, s)
		require.NoError(t, err)
		assert.Equal(t, protocol.StopStop, msg.StopReason)
		require.NotEmpty(t, msg.Content)
	})
}

func TestModelUsesTokenPlanMessagesCatalog(t *testing.T) {
	t.Parallel()
	m := Model()
	row := providers.TokenPlanMessages()
	assert.Equal(t, row.ID, m.ID)
	assert.Equal(t, row.API, m.API)
	assert.Equal(t, row.Provider, m.Provider)
	assert.Equal(t, row.BaseURL, m.BaseURL)
	assert.Equal(t, ModelID, m.ID)
	assert.Equal(t, ProviderID, m.Provider)
	assert.Equal(t, APIID, string(m.API))
	assert.Equal(t, BaseURL, m.BaseURL)
}

func TestStreamKeepsAnthropicMessagesAPI(t *testing.T) {
	_, msg, err := streamOnce(t, streamOnceOpts{sse: textSSE("end_turn")})
	require.NoError(t, err)
	assert.Equal(t, APIID, msg.API)
	assert.Equal(t, string(providers.APIAnthropicMessages), msg.API)
}

func TestStreamRejectsNonMessagesAPI(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
		t.Error("handler must not be called for a non-messages API")
	}))
	t.Cleanup(srv.Close)

	p := testProvider(srv)
	m := Model()
	m.API = providers.APIOpenAICompletions
	s := p.Stream(context.Background(), m, helloReq(), providers.StreamOptions{})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Contains(t, *msg.ErrorMessage, string(providers.APIOpenAICompletions))
	assert.Contains(t, *msg.ErrorMessage, string(providers.APIAnthropicMessages))
	assert.Equal(t, int32(0), hits.Load())
}

func TestStreamToolArgumentsFromPartialJSON(t *testing.T) {
	_, msg, err := streamOnce(t, streamOnceOpts{sse: toolSSE()})
	require.NoError(t, err)
	assert.Equal(t, protocol.StopToolUse, msg.StopReason)
	require.NotEmpty(t, msg.Content)
	call, ok := msg.Content[0].(protocol.ToolCall)
	require.True(t, ok)
	assert.Equal(t, "call_1", call.ID)
	assert.Equal(t, "echo", call.Name)
	assert.JSONEq(t, `{"n":1}`, string(call.Arguments))
}

func TestStreamPathIsNotDoubled(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeSSE(w, textSSE("end_turn"))
	}))
	t.Cleanup(srv.Close)
	p := testProvider(srv)
	s := p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	assert.Equal(t, "/apps/anthropic/v1/messages", path)
	assert.NotContains(t, path, "/v1/messages/v1/messages")
}

type streamOnceOpts struct {
	reasoning protocol.ThinkingLevel
	tools     []protocol.ToolDecl
	messages  []protocol.Message
	sse       string
}

func streamOnce(t *testing.T, opts streamOnceOpts) (map[string]any, protocol.AssistantMessage, error) {
	t.Helper()
	_, body, msg, err := streamRaw(t, opts)
	return body, msg, err
}

func streamRaw(t *testing.T, opts streamOnceOpts) ([]byte, map[string]any, protocol.AssistantMessage, error) {
	t.Helper()
	got := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got <- b
		writeSSE(w, opts.sse)
	}))
	t.Cleanup(srv.Close)

	req := helloReq()
	if len(opts.tools) > 0 || len(opts.messages) > 0 {
		in := providers.Request{SystemPrompt: "you are a tool", Tools: opts.tools, Messages: opts.messages}
		if len(opts.messages) == 0 {
			in.Messages = []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			}
		}
		req = providers.NormalizeRequest(in)
	}
	p := testProvider(srv)
	s := p.Stream(context.Background(), Model(), req, providers.StreamOptions{Reasoning: opts.reasoning})
	msg, err := resultOf(t, s)
	var raw []byte
	select {
	case raw = <-got:
	default:
	}
	var body map[string]any
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &body))
	}
	return raw, body, msg, err
}

func helloReq() providers.TranscriptRequest {
	return providers.NormalizeRequest(providers.Request{
		SystemPrompt: "you are a tool",
		Tools: []protocol.ToolDecl{{
			Name:       "echo",
			Parameters: json.RawMessage(`{"type":"object","required":[],"properties":{}}`),
		}},
		Messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		},
	})
}

func testProvider(srv *httptest.Server) providers.Provider {
	return keyedProvider(srv, 0)
}

func keyedProvider(srv *httptest.Server, idle time.Duration) providers.Provider {
	return New(
		WithHTTPClient(rewriteClient(srv)),
		WithEnv(func(k string) (string, bool) {
			if k == askKeyEnv {
				return "test-key", true
			}
			return "", false
		}),
		WithNow(func() int64 { return 1_700_000_000_000 }),
		WithIdle(idle),
	)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func rewriteClient(srv *httptest.Server) *http.Client {
	base := srv.Client()
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			u, err := url.Parse(srv.URL)
			if err != nil {
				return nil, err
			}
			clone := req.Clone(req.Context())
			clone.URL.Scheme = u.Scheme
			clone.URL.Host = u.Host
			clone.Host = u.Host
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				clone.Body = body
			}
			return base.Transport.RoundTrip(clone)
		}),
		Timeout: base.Timeout,
	}
}

func resultOf(t *testing.T, s *providers.Stream) (protocol.AssistantMessage, error) {
	t.Helper()
	for range s.Events() {
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, err := s.Result(ctx)
	require.NoError(t, ctx.Err())
	return msg, err
}

func writeSSE(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, body)
}

func toolSSE() string {
	var b strings.Builder
	b.WriteString(sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}`))
	b.WriteString(sse("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"echo","input":{}}}`))
	b.WriteString(sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"n\":1}"}}`))
	b.WriteString(sse("content_block_stop", `{"type":"content_block_stop","index":0}`))
	b.WriteString(sse("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":3,"output_tokens":1}}`))
	b.WriteString(sse("message_stop", `{"type":"message_stop"}`))
	return b.String()
}

func textSSE(stop string) string {
	return textSSEWithUsage(stop, 3, 3, 1, 0, 0)
}

func textSSEWithUsage(stop string, startInput, finalInput, output, cacheRead, cacheWrite int) string {
	var b strings.Builder
	b.WriteString(sse("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"stop_reason":null,"usage":{"input_tokens":%d,"output_tokens":0}}}`, startInput)))
	b.WriteString(sse("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`))
	b.WriteString(sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`))
	b.WriteString(sse("content_block_stop", `{"type":"content_block_stop","index":0}`))
	b.WriteString(sse("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s","stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}`, stop, finalInput, output, cacheRead, cacheWrite)))
	b.WriteString(sse("message_stop", `{"type":"message_stop"}`))
	return b.String()
}

func sse(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

func strPtr(s string) *string { return &s }

func findThinkingSignature(body map[string]any) (string, bool) {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		content, _ := msg["content"].([]any)
		for _, c := range content {
			block, _ := c.(map[string]any)
			if block["type"] == "thinking" {
				sig, _ := block["signature"].(string)
				return sig, true
			}
		}
	}
	return "", false
}

func stubUserPresent(body map[string]any) bool {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		if msg["role"] != "user" {
			continue
		}
		content, _ := msg["content"].([]any)
		if len(content) != 1 {
			continue
		}
		block, _ := content[0].(map[string]any)
		if block["type"] == "text" && block["text"] == "." {
			return true
		}
	}
	return false
}
