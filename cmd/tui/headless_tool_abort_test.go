package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/anthropic"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

type cooperativeAbortTool struct {
	started  chan struct{}
	returned chan struct{}
	bodies   atomic.Int32
}

func (*cooperativeAbortTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "wait_for_abort", Description: "Wait for cancellation", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (tool *cooperativeAbortTool) Execute(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
	if tool.returned != nil {
		defer close(tool.returned)
	}
	if tool.bodies.Add(1) == 1 {
		close(tool.started)
	}
	<-ctx.Done()
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "cancelled"}}}, nil
}

func TestPrintExitsOneAfterAbortDuringToolBatch(t *testing.T) {
	tool := &cooperativeAbortTool{started: make(chan struct{})}
	stream := sseToolUse(tool.Decl().Name)
	blockStart := strings.Index(stream, "event:content_block_start")
	blockEnd := strings.Index(stream, "event:message_delta")
	require.Positive(t, blockStart)
	require.Greater(t, blockEnd, blockStart)
	second := strings.ReplaceAll(stream[blockStart:blockEnd], `"index":0`, `"index":1`)
	second = strings.ReplaceAll(second, `"call_1"`, `"call_2"`)
	stream = stream[:blockEnd] + second + stream[blockEnd:]
	var requests atomic.Int32
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: counted(sseResponse(200, stream), &requests)}
	ag, err := newHeadlessAgent(o, func(key string) string {
		if key == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "test-key"
		}
		return ""
	}, tool)
	require.NoError(t, err)
	events := countEvents(ag)
	cycleEnd := make(chan string, 1)
	ag.Subscribe(func(event protocol.Event) error {
		if end, ok := event.(*protocol.CycleEnd); ok {
			cycleEnd <- end.Reason
		}
		return nil
	})
	var stdout, stderr bytes.Buffer
	exit := make(chan int, 1)
	go func() { exit <- runHeadless(ag, []string{"run both tools"}, modePrint, &stdout, &stderr, nil) }()
	select {
	case <-tool.started:
	case <-time.After(5 * time.Second):
		ag.Abort()
		t.Fatal("the first tool body did not start")
	}
	ag.Abort()
	select {
	case code := <-exit:
		assert.Equal(t, 1, code, stderr.String())
	case <-time.After(5 * time.Second):
		t.Fatal("print mode did not return after the cooperative tool stopped")
	}
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, stderr.String())
	assert.EqualValues(t, 1, requests.Load(), "abort must not start another model request")
	assert.EqualValues(t, 1, tool.bodies.Load(), "the second exclusive call must not start")
	assert.Equal(t, 2, events.get(protocol.TypeToolExecutionEnd), "each requested call has an outcome")
	require.Len(t, cycleEnd, 1)
	assert.Equal(t, "aborted", <-cycleEnd)
	messages := ag.State().Messages
	require.Len(t, messages, 5, "input, tool request, two results, and aborted assistant")
	for i, id := range []string{"call_1", "call_2"} {
		result, ok := messages[i+2].(protocol.ToolResultMessage)
		require.True(t, ok)
		assert.Equal(t, id, result.ToolCallID)
		assert.True(t, result.IsError)
		assert.Equal(t, []string{"Operation aborted", "Tool call aborted before dispatch"}[i], blockText(result.Content))
	}
	last, ok := messages[len(messages)-1].(protocol.AssistantMessage)
	require.True(t, ok)
	assert.Equal(t, protocol.StopAborted, last.StopReason)
}
