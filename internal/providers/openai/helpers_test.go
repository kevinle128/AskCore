package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

type captureServer struct {
	srv *httptest.Server
	ch  chan capturedReq
}

type capturedReq struct {
	raw     []byte
	req     *http.Request
	headers http.Header
	url     string
}

func newCaptureServer(t *testing.T, sse string) *captureServer {
	t.Helper()
	return newCaptureServerFn(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	})
}

func newCaptureServerFn(t *testing.T, h http.HandlerFunc) *captureServer {
	t.Helper()
	c := &captureServer{ch: make(chan capturedReq, 1)}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.ch <- capturedReq{raw: b, req: r.Clone(r.Context()), headers: r.Header.Clone(), url: r.URL.String()}
		h(w, r)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *captureServer) take(t *testing.T) capturedReq {
	t.Helper()
	select {
	case got := <-c.ch:
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("no request")
		return capturedReq{}
	}
}

func (c capturedReq) body(t *testing.T) map[string]any {
	t.Helper()
	var body map[string]any
	if len(c.raw) > 0 {
		require.NoError(t, json.Unmarshal(c.raw, &body))
	}
	return body
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

func completionsModel(base string) providers.Model {
	m := providers.TokenPlanCompletions()
	m.BaseURL = base
	return m
}

func responsesModel(base string) providers.Model {
	m := providers.OpenAIGPT55()
	m.BaseURL = base
	return m
}

func completionsProvider(srv *httptest.Server, idle time.Duration) providers.Provider {
	return NewCompletions(
		WithHTTPClient(srv.Client()),
		WithEnv(func(k string) (string, bool) {
			if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
				return "tp-key", true
			}
			return "", false
		}),
		WithNow(func() int64 { return 1_700_000_000_000 }),
		WithIdle(idle),
	)
}

func responsesProvider(srv *httptest.Server, idle time.Duration) providers.Provider {
	return NewResponses(
		WithHTTPClient(srv.Client()),
		WithEnv(func(k string) (string, bool) {
			if k == "OPENAI_API_KEY" {
				return "sk-openai", true
			}
			return "", false
		}),
		WithNow(func() int64 { return 1_700_000_000_000 }),
		WithIdle(idle),
	)
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

func completionsSSE() string {
	return completionsSSEWithUsage(10, 2, "stop")
}

func completionsSSEWithUsage(prompt, completion int, finish string) string {
	var b strings.Builder
	b.WriteString(chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`))
	b.WriteString(chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"` + finish + `"}]}`))
	b.WriteString(chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":` + strconv.Itoa(prompt) + `,"completion_tokens":` + strconv.Itoa(completion) + `,"total_tokens":` + strconv.Itoa(prompt+completion) + `}}`))
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func chatChunk(data string) string {
	return "data: " + data + "\n\n"
}

func responsesSSE() string {
	return responsesTextSSE("hello")
}

func responsesTextSSE(text string) string {
	var b strings.Builder
	b.WriteString(responsesEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","phase":"commentary","role":"assistant","content":[]}}`))
	b.WriteString(responsesEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","delta":"`+text+`"}`))
	b.WriteString(responsesEvent("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","phase":"final_answer","role":"assistant","content":[{"type":"output_text","text":"`+text+`"}]}}`))
	b.WriteString(responsesEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`))
	return b.String()
}

func responsesEvent(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

func stubUserPresent(body map[string]any) bool {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		if msg["role"] != "user" {
			continue
		}
		if s, ok := msg["content"].(string); ok && s == "." {
			return true
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

func strPtr(s string) *string { return &s }

// abortHangHandler reads the body, flushes an SSE ping, then waits for the
// client cancel. Writing first puts the client in Read, where cancel unblocks.
func abortHangHandler(t *testing.T, started chan struct{}) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			_, _ = io.WriteString(w, ": ping\n\n")
			f.Flush()
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
			t.Error("abort did not reach the request")
		}
	}
}
