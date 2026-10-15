package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestSIGTERMDisposesAndExits143(t *testing.T) {
	ag := newAgent(t, "20")
	streaming := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var seen []string
	var causes []string
	ag.Subscribe(func(ev protocol.Event) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, ev.EventType())
		if e, ok := ev.(*protocol.CycleEnd); ok {
			causes = append(causes, e.Cause)
		}
		if _, ok := ev.(*protocol.MessageUpdate); ok {
			once.Do(func() { close(streaming) })
		}
		return nil
	})
	sigs := make(chan os.Signal, 1)
	var out, errb bytes.Buffer
	codes := make(chan int, 1)

	go func() { codes <- runHeadless(ag, []string{longPrompt, "never runs"}, modeJSON, &out, &errb, sigs) }()
	<-streaming
	sigs <- syscall.SIGTERM

	select {
	case code := <-codes:
		assert.Equal(t, 143, code)
	case <-time.After(5 * time.Second):
		t.Fatal("runHeadless did not return after the signal")
	}

	evs := decodeJSONL(t, out.String())
	assert.Equal(t, "agent_settled", label(evs[len(evs)-1]), "the JSON stream ends with agent_settled")
	for _, ev := range evs {
		assert.NotEqual(t, protocol.TypeAgentDisposed, ev.EventType(), "the JSON writer leaves agent_disposed out")
	}
	assert.Equal(t, "", errb.String())
	assert.Equal(t, agent.Idle, ag.State().Status)

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(seen), 2)
	assert.Equal(t, []string{"agent_settled", "agent_disposed"}, seen[len(seen)-2:], "a Go listener gets agent_disposed after agent_settled")
	assert.Equal(t, []string{"disposed"}, causes, "the cycle ended because the agent was disposed")
	assert.Equal(t, 2, len(ag.State().Messages), "only the first prompt and its aborted reply")
}

// stuckTool does not look at its context: it returns when the test releases it.
type stuckTool struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*stuckTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "stuck", Description: "waits for the test", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (s *stuckTool) Execute(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "released"}}}, nil
}

// sseToolUse is an Anthropic Messages stream that calls the tool name with no
// arguments and ends the turn.
func sseToolUse(name string) string {
	s := sseText("x")
	start := strings.Index(s, "event:content_block_start")
	end := strings.Index(s, "event:message_delta")
	block := `event:content_block_start
data:{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"` + name + `","input":{}}}

event:content_block_delta
data:{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}

event:content_block_stop
data:{"type":"content_block_stop","index":0}

`
	return strings.Replace(s[:start]+block+s[end:], "end_turn", "tool_use", 1)
}

func TestSignalExitIsBoundedWhileDisposeWaits(t *testing.T) {
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: sseResponse(200, sseToolUse("stuck"))}
	tool := &stuckTool{started: make(chan struct{}), release: make(chan struct{})}
	ag, err := newHeadlessAgent(o, func(k string) string {
		if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "test-key"
		}
		return ""
	}, tool)
	require.NoError(t, err)
	disposed := make(chan struct{})
	ag.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.AgentDisposed); ok {
			close(disposed)
		}
		return nil
	})
	sigs := make(chan os.Signal, 1)
	var out, errb bytes.Buffer
	codes := make(chan int, 1)

	go func() { codes <- runHeadless(ag, []string{"go"}, modeJSON, &out, &errb, sigs) }()
	select {
	case <-tool.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool did not start")
	}
	start := time.Now()
	sigs <- os.Interrupt

	select {
	case code := <-codes:
		assert.Equal(t, 130, code)
	case <-time.After(abortGrace + 2*time.Second):
		t.Fatal("runHeadless did not return within the grace")
	}
	assert.GreaterOrEqual(t, time.Since(start), abortGrace-100*time.Millisecond, "it waited for the grace")
	assert.Equal(t, agent.Running, ag.State().Status, "Dispose still waits for the tool body")

	close(tool.release)
	select {
	case <-disposed:
	case <-time.After(5 * time.Second):
		t.Fatal("Dispose did not finish after the tool returned")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, ag.WaitForIdle(ctx))
}
