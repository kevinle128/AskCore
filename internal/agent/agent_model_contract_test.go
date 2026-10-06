package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/openai"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestModelSwitchClampsStoredAndWireThinking(t *testing.T) {
	anthGot := make(chan capturedReq, 2)
	compGot := make(chan capturedReq, 1)
	anthSrv := newSSEServer(t, anthGot, anthropicTextSSE())
	compSrv := newSSEServer(t, compGot, completionsChatSSE())
	anthModel := providers.TokenPlanMessages()
	anthModel.BaseURL = anthSrv.URL
	plain := providers.TokenPlanCompletions()
	plain.BaseURL = compSrv.URL
	plain.Reasoning = false
	env := func(string) (string, bool) { return "", false }
	wires := providers.NewRegistry()
	wires.Register(providers.APIAnthropicMessages, anthropic.New(anthropic.WithHTTPClient(anthSrv.Client()), anthropic.WithEnv(env)).Stream)
	wires.Register(providers.APIOpenAICompletions, openai.NewCompletions(openai.WithHTTPClient(compSrv.Client()), openai.WithEnv(env)).Stream)
	a, err := agent.New(agent.Config{Registry: wires, LoopConfig: agent.LoopConfig{Model: anthModel, BoundKey: providers.BoundKey{Provider: anthModel.Provider, Secret: anthKey}, Options: providers.StreamOptions{APIKey: anthKey}}})
	require.NoError(t, err)
	require.NoError(t, a.SetThinkingLevel(protocol.ThinkingHigh))
	require.NoError(t, a.Prompt(context.Background(), user("first")))
	var first map[string]any
	require.NoError(t, json.Unmarshal(takeReq(t, anthGot).body, &first))
	assert.Equal(t, "high", first["output_config"].(map[string]any)["effort"])
	require.NoError(t, a.SetModel(context.Background(), plain))
	assert.Equal(t, protocol.ThinkingOff, a.State().ThinkingLevel)
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	var second map[string]any
	require.NoError(t, json.Unmarshal(takeReq(t, compGot).body, &second))
	assert.NotContains(t, second, "enable_thinking")
	assert.NotContains(t, second, "reasoning_effort")
	require.NoError(t, a.SetModel(context.Background(), anthModel))
	assert.Equal(t, protocol.ThinkingOff, a.State().ThinkingLevel)
	require.NoError(t, a.Prompt(context.Background(), user("third")))
	var third map[string]any
	require.NoError(t, json.Unmarshal(takeReq(t, anthGot).body, &third))
	assert.Equal(t, "disabled", third["thinking"].(map[string]any)["type"])
}

func TestSignedAnthropicToolRoundTripsThroughCompletions(t *testing.T) {
	anthGot := make(chan capturedReq, 3)
	compGot := make(chan capturedReq, 1)
	var hits atomic.Int32
	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		anthGot <- capturedReq{headers: r.Header.Clone(), body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		if hits.Add(1) == 1 {
			_, _ = io.WriteString(w, anthropicSignedToolSSE())
		} else {
			_, _ = io.WriteString(w, anthropicTextSSE())
		}
	}))
	t.Cleanup(anthSrv.Close)
	compSrv := newSSEServer(t, compGot, completionsChatSSE())
	anthModel := providers.TokenPlanMessages()
	anthModel.BaseURL = anthSrv.URL
	compModel := providers.TokenPlanCompletions()
	compModel.BaseURL = compSrv.URL
	env := func(string) (string, bool) { return "", false }
	wires := providers.NewRegistry()
	wires.Register(providers.APIAnthropicMessages, anthropic.New(anthropic.WithHTTPClient(anthSrv.Client()), anthropic.WithEnv(env)).Stream)
	wires.Register(providers.APIOpenAICompletions, openai.NewCompletions(openai.WithHTTPClient(compSrv.Client()), openai.WithEnv(env)).Stream)
	reg := &tools.Registry{}
	var runs atomic.Int32
	require.NoError(t, reg.Register(&funcTool{name: "echo", run: func(_ context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
		runs.Add(1)
		assert.JSONEq(t, `{"value":"ping"}`, string(args))
		return textResult("echo:ping"), nil
	}}, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	a, err := agent.New(agent.Config{Registry: wires, Tools: reg, LoopConfig: agent.LoopConfig{Model: anthModel, BoundKey: providers.BoundKey{Provider: anthModel.Provider, Secret: anthKey}, Options: providers.StreamOptions{APIKey: anthKey, Reasoning: protocol.ThinkingMedium}}})
	require.NoError(t, err)
	require.NoError(t, a.Prompt(context.Background(), user("first")))
	assert.Equal(t, int32(1), runs.Load())
	_ = takeReq(t, anthGot)
	_ = takeReq(t, anthGot)
	original := a.State().Messages
	before, err := json.Marshal(original)
	require.NoError(t, err)
	require.NoError(t, a.SetModel(context.Background(), compModel))
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	comp := takeReq(t, compGot)
	assert.Contains(t, headerBlob(comp.headers), anthKey)
	assert.Contains(t, string(comp.body), "plan to echo")
	assert.Contains(t, string(comp.body), "echo:ping")
	assert.NotContains(t, string(comp.body), "anth-tool-sig")
	var compBody map[string]any
	require.NoError(t, json.Unmarshal(comp.body, &compBody))
	var callID string
	for _, raw := range compBody["messages"].([]any) {
		item := raw.(map[string]any)
		if item["role"] == "assistant" && item["tool_calls"] != nil {
			callID = item["tool_calls"].([]any)[0].(map[string]any)["id"].(string)
		}
		if item["role"] == "tool" {
			assert.Equal(t, callID, item["tool_call_id"])
		}
	}
	assert.Equal(t, "toolu_replay", callID)
	require.NoError(t, a.SetModel(context.Background(), anthModel))
	require.NoError(t, a.Prompt(context.Background(), user("third")))
	last := takeReq(t, anthGot)
	assert.Contains(t, string(last.body), "anth-tool-sig")
	assert.Contains(t, string(last.body), "toolu_replay")
	assert.Contains(t, string(last.body), "ok")
	after, err := json.Marshal(a.State().Messages[:len(original)])
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}

func TestMissingTargetKeyKeepsSourceWireAndTranscript(t *testing.T) {
	anthGot := make(chan capturedReq, 2)
	anthSrv := newSSEServer(t, anthGot, anthropicTextSSE())
	var targetHits atomic.Int32
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(targetSrv.Close)
	source := providers.TokenPlanMessages()
	source.BaseURL = anthSrv.URL
	target := providers.OpenAIGPT55()
	target.BaseURL = targetSrv.URL
	env := func(string) (string, bool) { return "", false }
	wires := providers.NewRegistry()
	wires.Register(providers.APIAnthropicMessages, anthropic.New(anthropic.WithHTTPClient(anthSrv.Client()), anthropic.WithEnv(env)).Stream)
	wires.Register(providers.APIOpenAIResponses, openai.NewResponses(openai.WithHTTPClient(targetSrv.Client()), openai.WithEnv(env)).Stream)
	a, err := agent.New(agent.Config{Registry: wires, LoopConfig: agent.LoopConfig{Model: source, BoundKey: providers.BoundKey{Provider: source.Provider, Secret: anthKey}, Options: providers.StreamOptions{APIKey: anthKey, Reasoning: protocol.ThinkingHigh}}})
	require.NoError(t, err)
	require.NoError(t, a.Prompt(context.Background(), user("first")))
	_ = takeReq(t, anthGot)
	before, err := json.Marshal(a.State().Messages)
	require.NoError(t, err)
	require.ErrorIs(t, a.SetModel(context.Background(), target), agent.ErrNoAPIKey)
	assert.Equal(t, int32(0), targetHits.Load())
	assert.Equal(t, source.ID, a.State().Model.ID)
	assert.Equal(t, protocol.ThinkingHigh, a.State().ThinkingLevel)
	after, err := json.Marshal(a.State().Messages)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	got := takeReq(t, anthGot)
	assert.Contains(t, headerBlob(got.headers), anthKey)
	assert.Equal(t, int32(0), targetHits.Load())
}
