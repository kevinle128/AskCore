package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestResponsesStoreFalseEncryptedInclude(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	p := responsesProvider(srv.srv, 0)
	s := p.Stream(context.Background(), responsesModel(srv.srv.URL), helloReq(), providers.StreamOptions{SessionID: strings.Repeat("s", 80)})
	msg, err := resultOf(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopStop, msg.StopReason)
	require.NotEmpty(t, msg.Content)
	if text, ok := msg.Content[0].(protocol.Text); ok {
		require.NotNil(t, text.TextSignature)
		replay, ok := providers.DecodeTextSignature(*text.TextSignature)
		require.True(t, ok)
		assert.Equal(t, "msg_1", replay.ID)
	}

	got := srv.take(t)
	body := got.body(t)
	assert.Equal(t, false, body["store"])
	include, _ := body["include"].([]any)
	assert.Contains(t, include, "reasoning.encrypted_content")
	key, _ := body["prompt_cache_key"].(string)
	assert.Len(t, []rune(key), 64)
}

func TestResponsesThinkingMapRequestJSON(t *testing.T) {
	for _, tc := range []struct {
		name, effort string
		level        protocol.ThinkingLevel
		offDisabled  bool
	}{
		{"high", "high-mapped", protocol.ThinkingHigh, false},
		{"off", "none", protocol.ThinkingOff, false},
		{"off-disabled", "", protocol.ThinkingOff, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCaptureServer(t, responsesSSE())
			m := responsesModel(srv.srv.URL)
			m.ThinkingLevelMap = providers.ThinkingLevelMap{
				protocol.ThinkingHigh: strPtr("high-mapped"),
				protocol.ThinkingOff:  strPtr("none"),
			}
			if tc.offDisabled {
				m.ThinkingLevelMap[protocol.ThinkingOff] = nil
			}
			stream := responsesProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Reasoning: tc.level})
			_, err := resultOf(t, stream)
			require.NoError(t, err)
			body := srv.take(t).body(t)
			if tc.offDisabled {
				assert.NotContains(t, body, "reasoning")
				return
			}
			reasoning, ok := body["reasoning"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tc.effort, reasoning["effort"])
		})
	}
}

func TestResponsesCacheAndLimitCompatRequestJSON(t *testing.T) {
	for _, tc := range []struct {
		name, retention string
		maxTokens       int
		wantKey         bool
		wantLong        bool
		wantMax         bool
	}{
		{"none", providers.CacheRetentionNone, 0, false, false, false},
		{"short", providers.CacheRetentionShort, 0, true, false, false},
		{"long-and-small-limit", providers.CacheRetentionLong, 1, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCaptureServer(t, responsesSSE())
			m := responsesModel(srv.srv.URL)
			m.Compat = providers.ResponsesCompat{SupportsLongCacheRetention: true, SupportsMaxOutputTokens: true}
			stream := responsesProvider(srv.srv, 0).Stream(context.Background(), m, helloReq(), providers.StreamOptions{SessionID: "session-1", CacheRetention: tc.retention, MaxTokens: tc.maxTokens})
			_, err := resultOf(t, stream)
			require.NoError(t, err)
			body := srv.take(t).body(t)
			if tc.wantKey {
				assert.Equal(t, "session-1", body["prompt_cache_key"])
			} else {
				assert.NotContains(t, body, "prompt_cache_key")
			}
			if tc.wantLong {
				assert.Equal(t, "24h", body["prompt_cache_retention"])
			} else {
				assert.NotContains(t, body, "prompt_cache_retention")
			}
			if tc.wantMax {
				assert.EqualValues(t, 16, body["max_output_tokens"])
			} else {
				assert.NotContains(t, body, "max_output_tokens")
			}
		})
	}
}

func TestLiveOpenAIResponsesReplay(t *testing.T) {
	if os.Getenv("ASK_LIVE_OPENAI") != "1" {
		t.Skip("set ASK_LIVE_OPENAI=1 for the OpenAI smoke test")
	}
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY is unavailable")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	p := NewResponses(WithHTTPClient(&http.Client{Transport: transport}))
	m := providers.OpenAIGPT55()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	opts := providers.StreamOptions{APIKey: key, Reasoning: protocol.ThinkingLow, SessionID: "h4-live-smoke"}
	first := p.Stream(ctx, m, providers.NormalizeRequest(providers.Request{
		SystemPrompt: "Reply briefly.",
		Messages:     []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "Reply with only OK."}}}},
	}), opts)
	answer, err := resultOf(t, first)
	require.NoError(t, err)
	require.Equal(t, protocol.StopStop, answer.StopReason)
	require.NotEmpty(t, answer.Content)
	var signedReasoning bool
	for _, block := range answer.Content {
		thinking, ok := block.(protocol.Thinking)
		if ok && thinking.ThinkingSignature != nil && *thinking.ThinkingSignature != "" {
			signedReasoning = true
		}
	}
	require.True(t, signedReasoning, "the first turn must supply encrypted reasoning for replay")

	second := p.Stream(ctx, m, providers.NormalizeRequest(providers.Request{
		SystemPrompt: "Reply briefly.",
		Messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "Reply with only OK."}}},
			answer,
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "Reply with only DONE."}}},
		},
	}), opts)
	replayed, err := resultOf(t, second)
	require.NoError(t, err)
	require.Equal(t, protocol.StopStop, replayed.StopReason)
	require.NotEmpty(t, replayed.Content)
}

func TestResponsesSameModelReasoningReplayed(t *testing.T) {
	sig := providers.EncodeThinkingSignature(providers.ReasoningReplay{
		Type:             "reasoning",
		ID:               "rs_1",
		EncryptedContent: "enc-final",
		Summary:          []providers.ReasoningSummary{{Type: "summary_text", Text: "prior"}},
	})
	srv := newCaptureServer(t, responsesSSE())
	p := responsesProvider(srv.srv, 0)
	m := responsesModel(srv.srv.URL)
	req := providers.NormalizeRequest(providers.Request{
		SystemPrompt: "leading",
		Messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.AssistantMessage{
				API: string(m.API), Provider: m.Provider, Model: m.ID,
				StopReason: protocol.StopStop,
				Content: []protocol.AssistantBlock{
					protocol.Thinking{Thinking: "prior", ThinkingSignature: &sig},
					protocol.Text{Text: "ok", TextSignature: strPtr(providers.EncodeTextSignature("msg_prev", "final_answer"))},
				},
			},
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
		},
	})
	s := p.Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	raw := string(srv.take(t).raw)
	assert.Contains(t, raw, `"type":"reasoning"`)
	assert.Contains(t, raw, "rs_1")
	assert.Contains(t, raw, "enc-final")
}

func TestResponsesEmptySignedThinkingAndOrphanToolResultJSON(t *testing.T) {
	sig := providers.EncodeThinkingSignature(providers.ReasoningReplay{Type: "reasoning", ID: "rs_empty", EncryptedContent: "opaque"})
	srv := newCaptureServer(t, responsesSSE())
	m := responsesModel(srv.srv.URL)
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "leading", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "", ThinkingSignature: &sig},
			protocol.ToolCall{ID: "call_orphan|fc_orphan", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	stream := responsesProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	input := srv.take(t).body(t)["input"].([]any)
	var foundReasoning, foundCall, foundSynthetic bool
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch item["type"] {
		case "reasoning":
			foundReasoning = true
			assert.Equal(t, "rs_empty", item["id"])
			assert.Equal(t, "opaque", item["encrypted_content"])
		case "function_call":
			foundCall = true
			assert.Equal(t, "call_orphan", item["call_id"])
		case "function_call_output":
			foundSynthetic = true
			assert.Equal(t, "call_orphan", item["call_id"])
			assert.Contains(t, item["output"], "No result provided")
		}
	}
	assert.True(t, foundReasoning)
	assert.True(t, foundCall)
	assert.True(t, foundSynthetic)
}

func TestResponsesSameProviderDifferentModelDropsItemID(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	m := responsesModel(srv.srv.URL)
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "leading", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: "other-model", StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{
			protocol.ToolCall{ID: "call_1|fc_prior", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}},
		protocol.ToolResultMessage{ToolCallID: "call_1|fc_prior", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	stream := responsesProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	input := srv.take(t).body(t)["input"].([]any)
	var foundCall bool
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || item["type"] != "function_call" {
			continue
		}
		foundCall = true
		assert.Equal(t, "call_1", item["call_id"])
		assert.NotContains(t, item, "id")
	}
	assert.True(t, foundCall)
}

func TestResponsesDropsItemIDWhenSourceDiffers(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	p := responsesProvider(srv.srv, 0)
	m := responsesModel(srv.srv.URL)
	req := providers.NormalizeRequest(providers.Request{
		SystemPrompt: "leading",
		Messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.AssistantMessage{
				API: string(providers.APIAnthropicMessages), Provider: providers.ProviderTokenPlan, Model: providers.ModelDeepSeekFlash,
				StopReason: protocol.StopToolUse,
				Content: []protocol.AssistantBlock{
					protocol.ToolCall{ID: "call_1|fc_olditem", Name: "echo", Arguments: json.RawMessage(`{}`)},
				},
			},
			protocol.ToolResultMessage{ToolCallID: "call_1|fc_olditem", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}},
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
		},
	})
	s := p.Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	raw := string(srv.take(t).raw)
	assert.NotContains(t, raw, `"id":"fc_olditem"`)
	assert.NotContains(t, raw, "call_1|fc_olditem")
}

func TestResponsesReplayItemIDsAndPhaseInRequest(t *testing.T) {
	for _, tc := range []struct {
		name, itemID string
		wantItemID   bool
	}{
		{"function-call", "fc_item", true},
		{"custom-tool-prefix", "ctc_item", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCaptureServer(t, responsesSSE())
			m := responsesModel(srv.srv.URL)
			callID := "call_1"
			req := providers.NormalizeRequest(providers.Request{
				SystemPrompt: "leading",
				Messages: []protocol.Message{
					protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
					protocol.AssistantMessage{
						API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopToolUse,
						Content: []protocol.AssistantBlock{
							protocol.Text{Text: "thinking done", TextSignature: strPtr(providers.EncodeTextSignature("msg_prior", "final_answer"))},
							protocol.ToolCall{ID: callID + "|" + tc.itemID, Name: "echo", Arguments: json.RawMessage(`{}`)},
						},
					},
					protocol.ToolResultMessage{ToolCallID: callID + "|" + tc.itemID, ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}},
					protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
				},
			})
			s := responsesProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{})
			_, err := resultOf(t, s)
			require.NoError(t, err)
			body := srv.take(t).body(t)
			input, ok := body["input"].([]any)
			require.True(t, ok)
			var foundText, foundCall, foundResult bool
			for _, item := range input {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				switch entry["type"] {
				case "message":
					if entry["id"] == "msg_prior" {
						foundText = true
						assert.Equal(t, "final_answer", entry["phase"])
					}
				case "function_call":
					foundCall = true
					assert.Equal(t, callID, entry["call_id"])
					if tc.wantItemID {
						assert.Equal(t, tc.itemID, entry["id"])
					} else {
						assert.NotContains(t, entry, "id")
					}
				case "function_call_output":
					foundResult = true
					assert.Equal(t, callID, entry["call_id"])
				}
			}
			assert.True(t, foundText)
			assert.True(t, foundCall)
			assert.True(t, foundResult)
		})
	}
}

func TestResponsesTextReplayFallbackAndLongIDJSON(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	m := responsesModel(srv.srv.URL)
	long := strings.Repeat("x", 90)
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "leading", Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopStop, Content: []protocol.AssistantBlock{
			protocol.Text{Text: "unsigned"},
			protocol.Text{Text: "long-signed", TextSignature: strPtr(providers.EncodeTextSignature(long, "final_answer"))},
		}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	stream := responsesProvider(srv.srv, 0).Stream(context.Background(), m, req, providers.StreamOptions{})
	_, err := resultOf(t, stream)
	require.NoError(t, err)
	input := srv.take(t).body(t)["input"].([]any)
	var sawFallback, sawLong bool
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || item["type"] != "message" || item["role"] != "assistant" {
			continue
		}
		id, _ := item["id"].(string)
		if strings.HasPrefix(id, "msg_pi_") {
			sawFallback = true
		}
		if id != "" && id != long && len([]rune(id)) <= 64 && item["phase"] == "final_answer" {
			sawLong = true
		}
	}
	assert.True(t, sawFallback)
	assert.True(t, sawLong)
}

func TestResponsesSecondSystemMessageIsPresent(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	p := responsesProvider(srv.srv, 0)
	req := providers.NormalizeRequest(providers.Request{
		SystemPrompt: "leading",
		Messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.SystemMessage{Content: []protocol.Text{{Text: "later-system-update"}}},
		},
	})
	s := p.Stream(context.Background(), responsesModel(srv.srv.URL), req, providers.StreamOptions{})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	assert.Contains(t, string(srv.take(t).raw), "later-system-update")
}

func TestResponses500IsSingleRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"no","type":"busy","code":"server_error"}}`))
	}))
	t.Cleanup(srv.Close)
	p := responsesProvider(srv, 0)
	s := p.Stream(context.Background(), responsesModel(srv.URL), helloReq(), providers.StreamOptions{})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, int32(1), hits.Load())
	require.NotNil(t, msg.ErrorMessage)
	assert.Contains(t, *msg.ErrorMessage, "500")
	assert.Contains(t, *msg.ErrorMessage, "server_error")
	assert.Contains(t, *msg.ErrorMessage, "no")
}

func TestResponsesFailedEventKeepsStatusAndBody(t *testing.T) {
	var b strings.Builder
	b.WriteString(responsesEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"rate_limit","message":"slow down"}}}`))
	srv := newCaptureServer(t, b.String())
	p := responsesProvider(srv.srv, 0)
	s := p.Stream(context.Background(), responsesModel(srv.srv.URL), helloReq(), providers.StreamOptions{})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.True(t, strings.Contains(*msg.ErrorMessage, "200") || strings.Contains(*msg.ErrorMessage, "slow down") || strings.Contains(*msg.ErrorMessage, "rate_limit"), *msg.ErrorMessage)
	_ = srv.take(t)
}

func TestResponsesMissingTerminalEventIsIncomplete(t *testing.T) {
	srv := newCaptureServer(t, responsesEvent("response.output_text.delta", `{"type":"response.output_text.delta","output_index":0,"delta":"partial"}`))
	p := responsesProvider(srv.srv, 0)
	s := p.Stream(context.Background(), responsesModel(srv.srv.URL), helloReq(), providers.StreamOptions{})
	msg, err := resultOf(t, s)
	require.Error(t, err)
	assert.ErrorIs(t, err, providers.ErrStreamIncomplete)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	_ = srv.take(t)
}

func TestResponsesEchoedTierControlsUsageCost(t *testing.T) {
	sse := responsesEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`) +
		responsesEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","delta":"ok"}`) +
		responsesEvent("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`) +
		responsesEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","service_tier":"priority","output":[],"usage":{"input_tokens":101000,"output_tokens":10000,"total_tokens":111000,"input_tokens_details":{"cached_tokens":1000},"output_tokens_details":{"reasoning_tokens":4000}}}}`)
	srv := newCaptureServer(t, sse)
	msg, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), responsesModel(srv.srv.URL), helloReq(), providers.StreamOptions{}))
	require.NoError(t, err)
	assert.Equal(t, int64(100000), msg.Usage.Input)
	assert.Equal(t, int64(1000), msg.Usage.CacheRead)
	assert.Equal(t, int64(10000), msg.Usage.Output)
	require.NotNil(t, msg.Usage.Reasoning)
	assert.Equal(t, int64(4000), *msg.Usage.Reasoning)
	assert.Equal(t, int64(111000), msg.Usage.TotalTokens)
	assert.Equal(t, int64(2001250), msg.Usage.Cost.Total)
	_ = srv.take(t)
}

func TestResponsesGoleakNormalAbortIdle(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		srv := newCaptureServer(t, responsesSSE())
		p := responsesProvider(srv.srv, 0)
		s := p.Stream(context.Background(), responsesModel(srv.srv.URL), helloReq(), providers.StreamOptions{})
		_, err := resultOf(t, s)
		require.NoError(t, err)
		_ = srv.take(t)
	})
	t.Run("abort", func(t *testing.T) {
		started := make(chan struct{})
		srv := httptest.NewServer(abortHangHandler(t, started))
		t.Cleanup(srv.Close)
		p := responsesProvider(srv, 0)
		ctx, cancel := context.WithCancel(context.Background())
		s := p.Stream(ctx, responsesModel(srv.URL), helloReq(), providers.StreamOptions{})
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
		p := responsesProvider(srv, 80*time.Millisecond)
		start := time.Now()
		s := p.Stream(context.Background(), responsesModel(srv.URL), helloReq(), providers.StreamOptions{})
		msg, err := resultOf(t, s)
		require.Error(t, err)
		assert.Equal(t, protocol.StopError, msg.StopReason)
		require.NotNil(t, msg.ErrorMessage)
		assert.Equal(t, "idle timeout", *msg.ErrorMessage)
		assert.Less(t, time.Since(start), 2*time.Second)
	})
}
