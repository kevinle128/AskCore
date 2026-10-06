package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// These tests run the real agent setup and run loop of ask -p in JSON mode on
// the real Token Plan adapter, with hand-written HTTP answers. They check what
// a JSON reader sees of a failed request.

// cycleEndOf runs one prompt in JSON mode on rt and returns the cycle_end
// event of its only cycle, with the exit code.
func cycleEndOf(t *testing.T, rt http.RoundTripper, delays *[]time.Duration) (map[string]any, int) {
	t.Helper()
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rt, wait: func(ctx context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return ctx.Err()
	}}
	ag, err := newHeadlessAgent(o, func(k string) string {
		if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "test-key"
		}
		return ""
	})
	require.NoError(t, err)
	var out, errb bytes.Buffer
	code := runHeadless(ag, []string{"ping"}, modeJSON, &out, &errb, nil)
	var found []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var ev map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &ev), line)
		if ev["type"] == "cycle_end" {
			found = append(found, ev)
		}
	}
	require.Len(t, found, 1, "one cycle_end in:\n%s", out.String())
	return found[0], code
}

func TestHeadlessFailureCodeReachesCycleEndAfterRetryPolicy(t *testing.T) {
	reply := func(status int, header http.Header, body string) http.RoundTripper {
		return rtFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
	}
	jsonH := func(kv ...string) http.Header {
		h := http.Header{"Content-Type": {"application/json"}}
		for i := 0; i+1 < len(kv); i += 2 {
			h.Set(kv[i], kv[i+1])
		}
		return h
	}
	full := sseText("pong")
	cut := full[:strings.Index(full, "event:content_block_stop")]
	for _, tc := range []struct {
		name string
		rt   http.RoundTripper
		want string
	}{
		{"rate limit", reply(429, jsonH("Retry-After", "5"), `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`), "RATE_LIMIT"},
		{"server error", reply(500, jsonH(), `{"type":"error","error":{"type":"api_error","message":"boom"}}`), "SERVER"},
		{"auth", reply(401, jsonH(), `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`), "AUTH"},
		{"unmapped status", reply(418, jsonH(), `{"error":{"message":"short and stout"}}`), "HTTP_418"},
		{"clean end without terminal event", sseResponse(http.StatusOK, cut), "STREAM_CLOSED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var n atomic.Int32
			var delays []time.Duration
			end, code := cycleEndOf(t, counted(tc.rt, &n), &delays)
			assert.Equal(t, "error", end["reason"])
			assert.Equal(t, tc.want, end["code"])
			assert.Equal(t, 0, code, "json mode exits 0 on an assistant error")
			switch tc.want {
			case "RATE_LIMIT":
				assert.EqualValues(t, 6, n.Load())
				assert.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second}, delays)
			case "SERVER":
				assert.EqualValues(t, 6, n.Load())
				assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}, delays)
			default:
				assert.EqualValues(t, 1, n.Load(), "a nonretryable failure stays one request")
				assert.Empty(t, delays)
			}
		})
	}
}

func TestJSONPrepareFailureHasNoUserMessageBeforeError(t *testing.T) {
	var requests atomic.Int32
	adapter := anthropic.New(anthropic.WithHTTPClient(&http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected HTTP request")
	})}))
	registry := providers.NewRegistry()
	registry.RegisterProvider(adapter)
	log := &sessions.MemoryLog{}
	ag, err := agent.New(agent.Config{Registry: registry, NewContext: func() sessions.Writer { return log }, LoopConfig: agent.LoopConfig{
		Model: anthropic.Model(), Prepare: func(providers.Model, providers.TranscriptRequest, providers.StreamOptions) (*providers.Prepared, error) {
			return nil, errors.New("request preparation failed")
		},
	}})
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	code := runHeadless(ag, []string{"ping"}, modeJSON, &stdout, &stderr, nil)
	require.Equal(t, 1, code)
	require.EqualValues(t, 0, requests.Load())
	require.Empty(t, log.Messages(), "neither the input nor its error wrapper was committed")
	var wrapper []string
	for _, event := range decodeJSONL(t, stdout.String()) {
		switch event := event.(type) {
		case *protocol.AttemptStart, *protocol.AttemptEnd:
			t.Fatalf("preparation failure opened an attempt: %T", event)
		case *protocol.MessageStart:
			require.Equal(t, protocol.RoleAssistant, event.Message.Role(), "no user message is published before preparation succeeds")
			wrapper = append(wrapper, "message_start")
		case *protocol.MessageEnd:
			final, ok := event.Message.(protocol.AssistantMessage)
			require.True(t, ok, "only the error assistant is published")
			require.Equal(t, protocol.StopError, final.StopReason)
			require.NotNil(t, final.ErrorMessage)
			require.Contains(t, *final.ErrorMessage, "request preparation failed")
			wrapper = append(wrapper, "message_end")
		case *protocol.TurnEnd:
			wrapper = append(wrapper, "turn_end")
		case *protocol.AgentEnd:
			wrapper = append(wrapper, "agent_end")
		}
	}
	require.Equal(t, []string{"message_start", "message_end", "turn_end", "agent_end"}, wrapper)
	for _, entry := range log.Entries() {
		_, isRequest := entry.(sessions.RequestDelta)
		require.False(t, isRequest, "preparation failure has no request record")
	}
}
