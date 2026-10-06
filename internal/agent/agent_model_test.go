package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/openai"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

const (
	anthKey = "anth-secret"
	compKey = "comp-secret"
)

type capturedReq struct {
	headers http.Header
	body    []byte
}

func TestSetModelSwitchesWireAndPinsKey(t *testing.T) {
	anthGot := make(chan capturedReq, 1)
	compGot := make(chan capturedReq, 1)
	anthSrv := newSSEServer(t, anthGot, anthropicTextSSE())
	compSrv := newSSEServer(t, compGot, completionsChatSSE())

	anthModel := providers.TokenPlanMessages()
	anthModel.BaseURL = anthSrv.URL
	compModel := providers.TokenPlanCompletions()
	compModel.BaseURL = compSrv.URL
	compModel.Provider = providers.ProviderOpenAI

	env := func(string) (string, bool) { return "", false }
	reg := providers.NewRegistry()
	reg.Register(providers.APIAnthropicMessages, anthropic.New(
		anthropic.WithHTTPClient(anthSrv.Client()),
		anthropic.WithEnv(env),
	).Stream)
	reg.Register(providers.APIOpenAICompletions, openai.NewCompletions(
		openai.WithHTTPClient(compSrv.Client()),
		openai.WithEnv(env),
	).Stream)

	a, err := agent.New(agent.Config{
		Registry: reg,
		LoopConfig: agent.LoopConfig{
			Model:    anthModel,
			BoundKey: providers.BoundKey{Provider: anthModel.Provider, Secret: anthKey},
			Options:  providers.StreamOptions{APIKey: anthKey},
			GetAPIKey: func(_ context.Context, provider string) (string, error) {
				if provider == providers.ProviderOpenAI {
					return compKey, nil
				}
				return "", nil
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, a.Prompt(context.Background(), user("first")))
	first := takeReq(t, anthGot)
	assert.Contains(t, headerBlob(first.headers), anthKey)
	assert.Equal(t, 0, len(compGot), "the completions host must stay quiet on the first prompt")

	require.NoError(t, a.SetModel(context.Background(), compModel))
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	second := takeReq(t, compGot)
	blob := headerBlob(second.headers) + string(second.body)
	assert.NotContains(t, blob, anthKey)
	assert.Contains(t, headerBlob(second.headers), compKey)
	assert.Contains(t, string(second.body), "first")
	assert.Contains(t, string(second.body), "hello")
	assert.Contains(t, string(second.body), "second")
	assert.Contains(t, string(second.body), "secret-thought")
	assert.NotContains(t, string(second.body), "anth-sig-keep")

	var body map[string]any
	require.NoError(t, json.Unmarshal(second.body, &body))
	msgs, _ := body["messages"].([]any)
	require.GreaterOrEqual(t, len(msgs), 3, "the second body is the existing transcript, not a rewritten history")
}

func TestSetModelSwitchesAnthropicToResponsesRequestJSON(t *testing.T) {
	anthGot := make(chan capturedReq, 1)
	respGot := make(chan capturedReq, 1)
	anthSrv := newSSEServer(t, anthGot, anthropicTextSSE())
	respSrv := newSSEServer(t, respGot, responsesTextSSEForAgent())
	anthModel := providers.TokenPlanMessages()
	anthModel.BaseURL = anthSrv.URL
	respModel := providers.OpenAIGPT55()
	respModel.BaseURL = respSrv.URL

	env := func(string) (string, bool) { return "", false }
	reg := providers.NewRegistry()
	reg.Register(providers.APIAnthropicMessages, anthropic.New(anthropic.WithHTTPClient(anthSrv.Client()), anthropic.WithEnv(env)).Stream)
	reg.Register(providers.APIOpenAIResponses, openai.NewResponses(openai.WithHTTPClient(respSrv.Client()), openai.WithEnv(env)).Stream)
	a, err := agent.New(agent.Config{Registry: reg, LoopConfig: agent.LoopConfig{
		Model: anthModel, BoundKey: providers.BoundKey{Provider: anthModel.Provider, Secret: anthKey},
		Options: providers.StreamOptions{APIKey: anthKey},
		GetAPIKey: func(_ context.Context, provider string) (string, error) {
			if provider == providers.ProviderOpenAI {
				return compKey, nil
			}
			return "", nil
		},
	}})
	require.NoError(t, err)
	require.NoError(t, a.Prompt(context.Background(), user("first")))
	_ = takeReq(t, anthGot)
	require.NoError(t, a.SetModel(context.Background(), respModel))
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	got := takeReq(t, respGot)
	assert.NotContains(t, headerBlob(got.headers)+string(got.body), anthKey)
	assert.Contains(t, headerBlob(got.headers), compKey)
	var body map[string]any
	require.NoError(t, json.Unmarshal(got.body, &body))
	input, ok := body["input"].([]any)
	require.True(t, ok)
	var text []string
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		assert.NotEqual(t, "reasoning", item["type"])
		bytes, _ := json.Marshal(item)
		text = append(text, string(bytes))
	}
	joined := strings.Join(text, " ")
	assert.Contains(t, joined, "first")
	assert.Contains(t, joined, "secret-thought")
	assert.Contains(t, joined, "hello")
	assert.Contains(t, joined, "second")
	assert.NotContains(t, joined, "anth-sig-keep")
}

func TestAnthropicToolHistoryReplaysToResponses(t *testing.T) {
	anthGot := make(chan capturedReq, 2)
	respGot := make(chan capturedReq, 1)
	var anthHits atomic.Int32
	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read Anthropic request: %v", err)
			return
		}
		anthGot <- capturedReq{headers: r.Header.Clone(), body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		if anthHits.Add(1) == 1 {
			_, _ = io.WriteString(w, anthropicSignedToolSSE())
		} else {
			_, _ = io.WriteString(w, anthropicTextSSE())
		}
	}))
	t.Cleanup(anthSrv.Close)
	respSrv := newSSEServer(t, respGot, responsesTextSSEForAgent())
	anthModel := providers.TokenPlanMessages()
	anthModel.BaseURL = anthSrv.URL
	respModel := providers.OpenAIGPT55()
	respModel.BaseURL = respSrv.URL
	env := func(string) (string, bool) { return "", false }
	wires := providers.NewRegistry()
	wires.Register(providers.APIAnthropicMessages, anthropic.New(anthropic.WithHTTPClient(anthSrv.Client()), anthropic.WithEnv(env)).Stream)
	wires.Register(providers.APIOpenAIResponses, openai.NewResponses(openai.WithHTTPClient(respSrv.Client()), openai.WithEnv(env)).Stream)
	reg := &tools.Registry{}
	var toolRuns atomic.Int32
	require.NoError(t, reg.Register(&funcTool{name: "echo", run: func(_ context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
		toolRuns.Add(1)
		assert.JSONEq(t, `{"value":"ping"}`, string(args))
		return textResult("echo:ping"), nil
	}}, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	a, err := agent.New(agent.Config{Registry: wires, Tools: reg, LoopConfig: agent.LoopConfig{
		Model: anthModel, BoundKey: providers.BoundKey{Provider: anthModel.Provider, Secret: anthKey},
		Options: providers.StreamOptions{APIKey: anthKey, Reasoning: protocol.ThinkingMedium},
		GetAPIKey: func(_ context.Context, provider string) (string, error) {
			if provider == providers.ProviderOpenAI {
				return compKey, nil
			}
			return "", nil
		},
	}})
	require.NoError(t, err)
	require.NoError(t, a.Prompt(context.Background(), user("first")))
	assert.Equal(t, int32(1), toolRuns.Load())
	assert.Equal(t, int32(2), anthHits.Load())
	first := takeReq(t, anthGot)
	second := takeReq(t, anthGot)
	assert.Contains(t, headerBlob(first.headers), anthKey)
	assert.Contains(t, headerBlob(second.headers), anthKey)
	before := a.State().Messages
	beforeBytes, err := json.Marshal(before)
	require.NoError(t, err)
	var signed, call, result bool
	for _, m := range before {
		switch v := m.(type) {
		case protocol.AssistantMessage:
			for _, b := range v.Content {
				switch block := b.(type) {
				case protocol.Thinking:
					signed = signed || block.ThinkingSignature != nil && *block.ThinkingSignature == "anth-tool-sig"
				case protocol.ToolCall:
					call = block.ID == "toolu_replay"
				}
			}
		case protocol.ToolResultMessage:
			result = v.ToolCallID == "toolu_replay"
		}
	}
	assert.True(t, signed)
	assert.True(t, call)
	assert.True(t, result)

	require.NoError(t, a.SetModel(context.Background(), respModel))
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	got := takeReq(t, respGot)
	assert.NotContains(t, headerBlob(got.headers)+string(got.body), anthKey)
	assert.Contains(t, headerBlob(got.headers), compKey)
	var payload struct {
		Input []map[string]any `json:"input"`
	}
	require.NoError(t, json.Unmarshal(got.body, &payload))
	var functionCall, functionOutput bool
	for _, item := range payload.Input {
		switch item["type"] {
		case "function_call":
			functionCall = true
			assert.Equal(t, "toolu_replay", item["call_id"])
			assert.Equal(t, "echo", item["name"])
			assert.NotContains(t, item, "id")
		case "function_call_output":
			functionOutput = true
			assert.Equal(t, "toolu_replay", item["call_id"])
			assert.Contains(t, item["output"], "echo:ping")
		case "reasoning":
			t.Fatal("foreign Anthropic thinking must not be a Responses reasoning item")
		}
	}
	assert.True(t, functionCall)
	assert.True(t, functionOutput)
	assert.NotContains(t, string(got.body), "anth-tool-sig")
	assert.Contains(t, string(got.body), "plan to echo")
	afterBytes, err := json.Marshal(a.State().Messages[:len(before)])
	require.NoError(t, err)
	assert.JSONEq(t, string(beforeBytes), string(afterBytes), "stored Anthropic turn must not mutate on replay")
}

func TestPrepareRequestSwitchesWireWithinActiveToolRun(t *testing.T) {
	anthGot := make(chan capturedReq, 1)
	respGot := make(chan capturedReq, 1)
	anthSrv := newSSEServer(t, anthGot, anthropicSignedToolSSE())
	respSrv := newSSEServer(t, respGot, responsesTextSSEForAgent())
	anthModel := providers.TokenPlanMessages()
	anthModel.BaseURL = anthSrv.URL
	respModel := providers.OpenAIGPT55()
	respModel.BaseURL = respSrv.URL
	env := func(string) (string, bool) { return "", false }
	wires := providers.NewRegistry()
	wires.Register(providers.APIAnthropicMessages, anthropic.New(anthropic.WithHTTPClient(anthSrv.Client()), anthropic.WithEnv(env)).Stream)
	wires.Register(providers.APIOpenAIResponses, openai.NewResponses(openai.WithHTTPClient(respSrv.Client()), openai.WithEnv(env)).Stream)
	reg := registry(t, &funcTool{name: "echo", run: func(_ context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
		assert.JSONEq(t, `{"value":"ping"}`, string(args))
		return textResult("echo:ping"), nil
	}})
	prepares := 0
	handlers := pipeline.NewRegistry()
	handlers.OnPrepareRequest(func(_ context.Context, r pipeline.Request, _ pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		prepares++
		if prepares == 1 {
			assert.Equal(t, providers.APIAnthropicMessages, r.Model.API)
			return nil, nil
		}
		assert.Equal(t, providers.APIAnthropicMessages, r.Model.API)
		return &pipeline.RequestUpdate{Model: &respModel}, nil
	})
	msgs, err := agent.Run(context.Background(), []protocol.Message{user("first")}, pipeline.AgentContext{Tools: reg}, agent.LoopConfig{
		Model: anthModel, Stream: wires.Stream, BoundKey: providers.BoundKey{Provider: anthModel.Provider, Secret: anthKey},
		Options: providers.StreamOptions{APIKey: anthKey, Reasoning: protocol.ThinkingMedium}, Pipeline: handlers,
		GetAPIKey: func(_ context.Context, provider string) (string, error) {
			if provider == providers.ProviderOpenAI {
				return compKey, nil
			}
			return "", nil
		},
	}, func(protocol.Event) error { return nil })
	require.NoError(t, err)
	assert.Equal(t, 2, prepares)
	assert.GreaterOrEqual(t, len(msgs), 4, "the run includes the tool declaration and both wire turns")
	anthRequest := takeReq(t, anthGot)
	respRequest := takeReq(t, respGot)
	assert.Contains(t, headerBlob(anthRequest.headers), anthKey)
	assert.NotContains(t, headerBlob(respRequest.headers)+string(respRequest.body), anthKey)
	assert.Contains(t, headerBlob(respRequest.headers), compKey)
	var payload struct {
		Input []map[string]any `json:"input"`
	}
	require.NoError(t, json.Unmarshal(respRequest.body, &payload))
	var foundCall, foundResult bool
	for _, item := range payload.Input {
		switch item["type"] {
		case "function_call":
			foundCall = true
			assert.Equal(t, "toolu_replay", item["call_id"])
		case "function_call_output":
			foundResult = true
			assert.Equal(t, "toolu_replay", item["call_id"])
			assert.Contains(t, item["output"], "echo:ping")
		case "reasoning":
			t.Fatal("foreign thinking must be text on the Responses wire")
		}
	}
	assert.True(t, foundCall)
	assert.True(t, foundResult)
}

func anthropicSignedToolSSE() string {
	return "" +
		"event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":"anth-tool-sig"}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan to echo"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_replay","name":"echo","input":{}}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"value\":\"ping\"}"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":1}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":3,"output_tokens":1}}` + "\n\n" +
		"event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"
}

func responsesTextSSEForAgent() string {
	return "" +
		"event: response.output_item.added\n" +
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}` + "\n\n" +
		"event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","item_id":"msg_1","delta":"ok"}` + "\n\n" +
		"event: response.output_item.done\n" +
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}` + "\n\n" +
		"event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}` + "\n\n"
}

func TestSetModelNoAPIKeyLeavesState(t *testing.T) {
	p, _ := newFaux(t)
	start := providers.TokenPlanMessages()
	a, err := agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{
			Model:    start,
			Stream:   p.Stream,
			BoundKey: providers.BoundKey{Provider: start.Provider, Secret: anthKey},
			Options:  providers.StreamOptions{Reasoning: protocol.ThinkingHigh, APIKey: anthKey},
		},
	})
	require.NoError(t, err)
	require.NoError(t, a.SetThinkingLevel(protocol.ThinkingHigh))
	before := a.State()

	err = a.SetModel(context.Background(), providers.OpenAIGPT55())
	require.ErrorIs(t, err, agent.ErrNoAPIKey)
	after := a.State()
	assert.Equal(t, before.Model, after.Model)
	assert.Equal(t, before.ThinkingLevel, after.ThinkingLevel)
	assert.Equal(t, before.Messages, after.Messages)
	assert.Equal(t, protocol.ThinkingHigh, after.ThinkingLevel)
}

func TestSetThinkingLevelStoresClampedOffAfterNonReasoning(t *testing.T) {
	p, _ := newFaux(t)
	reasoning := providers.TokenPlanMessages()
	a, err := agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{
			Model:    reasoning,
			Stream:   p.Stream,
			BoundKey: providers.BoundKey{Provider: reasoning.Provider, Secret: anthKey},
		},
	})
	require.NoError(t, err)
	require.NoError(t, a.SetThinkingLevel(protocol.ThinkingHigh))
	assert.Equal(t, protocol.ThinkingHigh, a.State().ThinkingLevel)

	plain := reasoning
	plain.ID = "plain"
	plain.Name = "plain"
	plain.Reasoning = false
	require.NoError(t, a.SetModel(context.Background(), plain))
	assert.Equal(t, protocol.ThinkingOff, a.State().ThinkingLevel)

	require.NoError(t, a.SetModel(context.Background(), reasoning))
	assert.Equal(t, protocol.ThinkingOff, a.State().ThinkingLevel)
	assert.Equal(t, reasoning.ID, a.State().Model.ID)
}

func newSSEServer(t *testing.T, got chan capturedReq, sse string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got <- capturedReq{headers: r.Header.Clone(), body: b}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	t.Cleanup(func() {
		srv.Close()
		srv.Client().CloseIdleConnections()
	})
	return srv
}

func takeReq(t *testing.T, ch <-chan capturedReq) capturedReq {
	t.Helper()
	select {
	case got := <-ch:
		return got
	default:
		t.Fatal("no request")
		return capturedReq{}
	}
}

func headerBlob(h http.Header) string {
	var b strings.Builder
	for k, vs := range h {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(strings.Join(vs, ","))
		b.WriteByte('\n')
	}
	return b.String()
}

func anthropicTextSSE() string {
	return "" +
		"event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":"anth-sig-keep"}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"secret-thought"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":1}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":3,"output_tokens":1}}` + "\n\n" +
		"event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"
}

func completionsChatSSE() string {
	return "" +
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n" +
		"data: [DONE]\n\n"
}
