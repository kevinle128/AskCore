package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/openai"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// These tests run the real Agent on the real provider adapters. Only the HTTP
// transport is injected: it records what the adapter sends and answers with a
// valid stream. The request log of each attempt must rebuild the body that the
// transport got.

type wireCall struct {
	URL    *url.URL
	Header http.Header
	Body   []byte
}

// wire is the injected transport.
type wire struct {
	mu    sync.Mutex
	calls []wireCall
}

func (w *wire) RoundTrip(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	u := *r.URL
	w.calls = append(w.calls, wireCall{URL: &u, Header: r.Header.Clone(), Body: body})
	w.mu.Unlock()
	var payload string
	switch {
	case strings.HasSuffix(r.URL.Path, "/messages"):
		payload = messagesStream
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		payload = completionsStream
	default:
		payload = responsesStream
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "OK",
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    r,
	}, nil
}

func (w *wire) taken() []wireCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.calls)
}

func sseEvent(event, data string) string { return "event: " + event + "\ndata: " + data + "\n\n" }

var messagesStream = sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}`) +
	sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
	sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`) +
	sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
	sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":3,"output_tokens":1}}`) +
	sseEvent("message_stop", `{"type":"message_stop"}`)

var completionsStream = "data: " + `{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}` + "\n\n" +
	"data: " + `{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
	"data: " + `{"id":"c1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}` + "\n\n" +
	"data: [DONE]\n\n"

var responsesStream = sseEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`) +
	sseEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","delta":"hello"}`) +
	sseEvent("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`) +
	sseEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`)

// realWires registers the three real adapters on a registry. Every request goes
// to rt, and every environment key resolves to a test key.
func realWires(rt http.RoundTripper) *providers.Registry {
	env := func(string) (string, bool) { return "test-key", true }
	client := &http.Client{Transport: rt}
	reg := providers.NewRegistry()
	reg.RegisterProvider(anthropic.New(anthropic.WithEnv(env), anthropic.WithHTTPClient(client)))
	reg.RegisterProvider(openai.NewCompletions(openai.WithEnv(env), openai.WithHTTPClient(client)))
	reg.RegisterProvider(openai.NewResponses(openai.WithEnv(env), openai.WithHTTPClient(client)))
	return reg
}

// realRun is one Agent on the real adapters.
type realRun struct {
	t      *testing.T
	agent  *agent.Agent
	log    *sessions.MemoryLog
	reg    *providers.Registry
	wire   *wire
	events []protocol.Event
}

// newRealRun builds an Agent on the real adapters. Its stream function reports
// the credential binding of the request, as the composed AuthRunner does.
func newRealRun(t *testing.T, m providers.Model, edit func(*agent.Config)) *realRun {
	t.Helper()
	w := &wire{}
	reg := realWires(w)
	log := &sessions.MemoryLog{}
	cfg := agent.Config{
		LoopConfig: agent.LoopConfig{
			Model: m,
			Stream: func(ctx context.Context, model providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
				return reg.Stream(ctx, model, req, opts).WithBinding(opts.Auth.Binding())
			},
		},
		Registry:   reg,
		Tools:      registry(t, tools.Echo{}),
		SessionID:  "s1",
		Clock:      fixedClock,
		NewContext: func() sessions.Writer { return log },
	}
	if edit != nil {
		edit(&cfg)
	}
	a, err := agent.New(cfg)
	require.NoError(t, err)
	r := &realRun{t: t, agent: a, log: log, reg: reg, wire: w}
	a.Subscribe(func(ev protocol.Event) error { r.events = append(r.events, ev); return nil })
	return r
}

func (r *realRun) prompt(text string) {
	r.t.Helper()
	require.NoError(r.t, r.agent.Prompt(context.Background(), user(text)))
}

// rebuilt returns the request that the log of attempt i rebuilds.
func (r *realRun) rebuilt(i int) agent.LoggedRequest {
	r.t.Helper()
	ids := attemptIDs(r.events)
	require.Greater(r.t, len(ids), i)
	got, err := agent.RebuildRequest(r.log.Entries(), ids[i])
	require.NoError(r.t, err)
	return got
}

// encode runs the registry encoder on a rebuilt request.
func (r *realRun) encode(m providers.Model, got agent.LoggedRequest) *providers.Encoded {
	r.t.Helper()
	enc, err := r.reg.Encode(context.Background(), m, got.Prepared, providers.TranscriptRequest{Messages: got.Messages}, got.Binding)
	require.NoError(r.t, err)
	return enc
}

var credentialHeaders = map[string]bool{"x-api-key": true, "authorization": true, "cookie": true}

// assertSameRequest compares the rebuilt request with what the transport got:
// the body byte for byte, the URL path, every header name, and the value of
// every header that is not a credential.
func assertSameRequest(t *testing.T, label string, sent wireCall, enc *providers.Encoded) {
	t.Helper()
	assert.Equal(t, string(sent.Body), string(enc.Body), "%s: body", label)
	assert.Equal(t, sent.URL.Path, enc.URL.Path, "%s: path", label)
	sentNames := map[string]string{}
	for name, values := range sent.Header {
		sentNames[strings.ToLower(name)] = strings.Join(values, ",")
	}
	encNames := map[string]string{}
	for name, values := range enc.Header {
		encNames[strings.ToLower(name)] = strings.Join(values, ",")
	}
	for name, value := range sentNames {
		got, ok := encNames[name]
		if !assert.True(t, ok, "%s: header %q is missing in the rebuilt request", label, name) {
			continue
		}
		if !credentialHeaders[name] {
			assert.Equal(t, value, got, "%s: header %q", label, name)
		}
	}
	for name := range encNames {
		assert.Contains(t, sentNames, name, "%s: the rebuilt request has an extra header", label)
	}
}

// bodyField reads a number from a JSON body.
func bodyField(t *testing.T, body []byte, field string) (float64, bool) {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m))
	v, ok := m[field].(float64)
	return v, ok
}

// realModels are one model of each adapter, with the field that holds the
// output limit in its body.
func realModels(t *testing.T) []struct {
	name       string
	model      providers.Model
	limitField string
} {
	t.Helper()
	completions := providers.TokenPlanCompletions()
	compat, ok := completions.Completions()
	require.True(t, ok)
	field := compat.MaxTokensField
	if field == "" {
		field = "max_tokens"
	}
	return []struct {
		name       string
		model      providers.Model
		limitField string
	}{
		{"anthropic messages", anthropic.Model(), "max_tokens"},
		{"openai completions", completions, field},
		{"openai responses", providers.OpenAIGPT55(), ""},
	}
}

func TestRebuiltRequestEqualsAdapterBodyWithDefaults(t *testing.T) {
	for _, tc := range realModels(t) {
		t.Run(tc.name, func(t *testing.T) {
			r := newRealRun(t, tc.model, nil)
			r.prompt("hello")

			calls := r.wire.taken()
			require.Len(t, calls, 1)
			got := r.rebuilt(0)
			require.NotNil(t, got.Prepared, "the log holds the prepared values")

			assertSameRequest(t, tc.name, calls[0], r.encode(tc.model, got))

			// Options.MaxTokens is zero. The log holds the limit that the adapter
			// sent, and that limit is what the body carries.
			assert.Zero(t, got.Options.MaxTokens)
			if tc.limitField != "" {
				sent, ok := bodyField(t, calls[0].Body, tc.limitField)
				require.True(t, ok, "the body has %s", tc.limitField)
				assert.Positive(t, got.Prepared.MaxTokens, "a logged limit of the options would be zero here")
				assert.EqualValues(t, sent, got.Prepared.MaxTokens)
			}

			// The prepared headers and endpoint are those of the request.
			var names []string
			for name := range calls[0].Header {
				names = append(names, strings.ToLower(name))
			}
			slices.Sort(names)
			assert.Equal(t, names, got.Prepared.HeaderNames)
			assert.Equal(t, providers.EndpointOf(calls[0].URL.String()), got.Prepared.Endpoint)
			for name, value := range got.Prepared.Headers {
				assert.Equal(t, value, calls[0].Header.Get(name), "allowlisted header %q", name)
			}
		})
	}
}

func TestRebuiltRequestEqualsAdapterBodyAfterModelSwitch(t *testing.T) {
	start, next := anthropic.Model(), providers.TokenPlanCompletions()
	prepares := 0
	r := newRealRun(t, start, func(c *agent.Config) {
		c.Options.MaxTokens = 77
		c.Options.Reasoning = protocol.ThinkingHigh
		// The second request switches from Anthropic Messages to OpenAI Completions.
		registryOf(c).OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			prepares++
			if prepares == 2 {
				return &pipeline.RequestUpdate{Model: &next}, nil
			}
			return nil, nil
		})
	})
	r.prompt("one")
	r.prompt("two")

	calls := r.wire.taken()
	require.Len(t, calls, 2)
	assert.True(t, strings.HasSuffix(calls[0].URL.Path, "/messages"))
	assert.True(t, strings.HasSuffix(calls[1].URL.Path, "/chat/completions"))
	for i, m := range []providers.Model{start, next} {
		got := r.rebuilt(i)
		require.NotNil(t, got.Prepared)
		assertSameRequest(t, m.Provider+" "+string(m.API), calls[i], r.encode(m, got))
		assert.Equal(t, m.ID, got.Prepared.Model)
		assert.Equal(t, string(m.API), got.Prepared.API)
		// An explicit limit below the model limit stays, on both adapters.
		assert.Equal(t, 77, got.Prepared.MaxTokens, "attempt %d", i)
		assert.Equal(t, protocol.ThinkingHigh, got.Prepared.Thinking, "attempt %d", i)
	}
}

func TestRequestLogRecordsPreparedMaxTokensAndThinking(t *testing.T) {
	t.Run("after a PrepareRequest model switch", recordsPreparedValuesAfterSwitch)
	for _, tc := range realModels(t)[:2] {
		for _, level := range []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingHigh} {
			t.Run(tc.name+" "+string(level), func(t *testing.T) {
				r := newRealRun(t, tc.model, func(c *agent.Config) { c.Options.Reasoning = level })
				r.prompt("hello")
				calls := r.wire.taken()
				require.Len(t, calls, 1)
				got := r.rebuilt(0)
				require.NotNil(t, got.Prepared)

				sent, ok := bodyField(t, calls[0].Body, tc.limitField)
				require.True(t, ok)
				assert.Positive(t, got.Prepared.MaxTokens, "an entry with no limit while the body has one is wrong")
				assert.EqualValues(t, sent, got.Prepared.MaxTokens)
				assert.Equal(t, level, got.Prepared.Thinking)

				var body map[string]any
				require.NoError(t, json.Unmarshal(calls[0].Body, &body))
				switch {
				case level == protocol.ThinkingOff && tc.model.API == providers.APIAnthropicMessages:
					assert.Equal(t, map[string]any{"type": "disabled"}, body["thinking"])
					assert.Empty(t, got.Prepared.Effort)
				case tc.model.API == providers.APIAnthropicMessages:
					assert.Equal(t, got.Prepared.Effort, body["output_config"].(map[string]any)["effort"])
				case level != protocol.ThinkingOff && got.Prepared.Effort != "":
					assert.Equal(t, got.Prepared.Effort, body["reasoning_effort"])
				}
			})
		}
	}
}

func recordsPreparedValuesAfterSwitch(t *testing.T) {
	next := providers.TokenPlanCompletions()
	compat, ok := next.Completions()
	require.True(t, ok)
	field := compat.MaxTokensField
	if field == "" {
		field = "max_tokens"
	}
	prepares := 0
	r := newRealRun(t, anthropic.Model(), func(c *agent.Config) {
		c.Options.MaxTokens = 77
		c.Options.Reasoning = protocol.ThinkingLow
		registryOf(c).OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			prepares++
			if prepares == 2 {
				return &pipeline.RequestUpdate{Model: &next}, nil
			}
			return nil, nil
		})
	})
	r.prompt("one")
	r.prompt("two")
	calls := r.wire.taken()
	require.Len(t, calls, 2)
	second := r.rebuilt(1)
	require.NotNil(t, second.Prepared)
	assert.Equal(t, next.ID, second.Prepared.Model, "the switch re-materializes the values of the new adapter")
	sent, ok := bodyField(t, calls[1].Body, field)
	require.True(t, ok)
	assert.EqualValues(t, sent, second.Prepared.MaxTokens)
	assert.Equal(t, 77, second.Options.MaxTokens, "the explicit cap stays in the options")
	assert.Equal(t, 77, second.Prepared.MaxTokens, "and below the model limit it is the limit that was sent")
	assert.Equal(t, protocol.ThinkingLow, second.Prepared.Thinking)
}

func TestRebuiltRequestEqualsAdapterBodyForOAuthBinding(t *testing.T) {
	const token = "CANARY-oauth-access-token-91d2"
	m, err := providers.Find(providers.Ref{Provider: providers.ProviderAnthropic, ID: providers.ModelClaudeSonnet46})
	require.NoError(t, err)
	r := newRealRun(t, m, func(c *agent.Config) {
		c.Tools = registry(t, namedTool("read"))
		c.Options.Auth = providers.AuthSnapshot{
			Provider:    providers.ProviderAnthropic,
			Method:      "anthropic-oauth",
			Profile:     "claude-code",
			Endpoint:    m.BaseURL,
			AccessToken: token,
			BillingHint: "subscription",
		}
	})
	r.prompt("hello")

	calls := r.wire.taken()
	require.Len(t, calls, 1)
	var body struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(calls[0].Body, &body))
	require.NotEmpty(t, body.System)
	assert.Equal(t, "You are Claude Code, Anthropic's official CLI for Claude.", body.System[0].Text)
	require.NotEmpty(t, body.Tools)
	assert.Equal(t, "Read", body.Tools[0].Name, "the subscription profile maps tool names")

	got := r.rebuilt(0)
	assert.Equal(t, providers.AuthBinding{Provider: providers.ProviderAnthropic, Method: "anthropic-oauth", Profile: "claude-code", BillingHint: "subscription"}, got.Binding)
	enc := r.encode(m, got)
	assertSameRequest(t, "oauth", calls[0], enc)

	// The prepared header facts describe the request before any profile shapes
	// it. The headers of the subscription profile come from the encoder and the
	// logged binding, and they are the ones on the wire.
	assert.NotContains(t, got.Prepared.HeaderNames, "x-app")
	assert.Equal(t, "cli", enc.Header.Get("X-App"))
	assert.Equal(t, "cli", calls[0].Header.Get("X-App"))
	assert.Contains(t, enc.Header.Get("Anthropic-Beta"), "oauth-2025-04-20")
	assert.Equal(t, calls[0].Header.Get("Anthropic-Beta"), enc.Header.Get("Anthropic-Beta"))
	assert.Equal(t, calls[0].Header.Get("User-Agent"), enc.Header.Get("User-Agent"))

	// Without the logged binding the rebuilt body has no prefix and no mapped
	// names, so the binding in the log is what makes the rebuild exact.
	plain, err := r.reg.Encode(context.Background(), m, got.Prepared, providers.TranscriptRequest{Messages: got.Messages}, providers.AuthBinding{})
	require.NoError(t, err)
	assert.NotEqual(t, string(calls[0].Body), string(plain.Body))

	// The access token reaches no entry, event or rebuild.
	var out bytes.Buffer
	for _, e := range r.log.Entries() {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		out.Write(b)
	}
	for _, ev := range r.events {
		b, err := protocol.EncodeEvent(ev)
		require.NoError(t, err)
		out.Write(b)
	}
	out.WriteString(toJSON(t, got))
	assert.NotContains(t, out.String(), token)
}

func TestCredentialCanariesNeverReachPreparedHeadersOrBinding(t *testing.T) {
	const headerCanary = "CANARY-model-header-value-5b1e"
	const keyCanary = "CANARY-api-key-value-2c7d"
	m := providers.TokenPlanCompletions()
	m.Headers = map[string]string{"X-Canary": headerCanary}
	r := newRealRun(t, m, func(c *agent.Config) {
		c.Options.Auth = providers.AuthSnapshot{
			Provider: m.Provider, Method: "api-key", Profile: "api-key", Endpoint: m.BaseURL, AccessToken: keyCanary,
		}
	})
	r.prompt("hello")

	calls := r.wire.taken()
	require.Len(t, calls, 1)
	assert.Equal(t, headerCanary, calls[0].Header.Get("X-Canary"), "the adapter sends the model header")

	got := r.rebuilt(0)
	require.NotNil(t, got.Prepared)
	assert.Contains(t, got.Prepared.HeaderNames, "x-canary", "the name of the header is kept")
	assert.NotContains(t, got.Prepared.Headers, "x-canary", "the value of a header outside the allowlist is not kept")
	assert.Contains(t, got.Prepared.Headers, "content-type")

	var out bytes.Buffer
	for _, e := range r.log.Entries() {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		out.Write(b)
	}
	out.WriteString(toJSON(t, got))
	assert.NotContains(t, out.String(), headerCanary)
	assert.NotContains(t, out.String(), keyCanary)
}
