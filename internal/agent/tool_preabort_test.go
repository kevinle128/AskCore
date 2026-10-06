package agent

import (
	"context"
	"encoding/json"
	"testing"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

type neverPrepareTool struct{ t *testing.T }

func (n neverPrepareTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "t", Parameters: json.RawMessage(`{"type":"object","required":["required"]}`)}
}
func (n neverPrepareTool) PrepareArguments(json.RawMessage) (json.RawMessage, error) {
	n.t.Error("pre-aborted call prepared")
	return nil, nil
}
func (n neverPrepareTool) Execute(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
	n.t.Error("pre-aborted body ran")
	return protocol.ToolExecutionResult{}, nil
}
func TestPreAbortedBatchRunsNoHookAndNoValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reg := &tools.Registry{}
	require.NoError(t, reg.Register(neverPrepareTool{t}, tools.SourceInfo{}))
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, pipeline.Next[pipeline.ToolCallInfo, *pipeline.BeforeToolCallResult]) (*pipeline.BeforeToolCallResult, error) {
		t.Error("pre hook ran")
		return nil, nil
	})
	h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, pipeline.Next[pipeline.ToolResultInfo, *pipeline.AfterToolCallResult]) (*pipeline.AfterToolCallResult, error) {
		t.Error("post hook ran")
		return nil, nil
	})
	log := &sessions.MemoryLog{}
	a := repairAssistant("a", "b")
	_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: a})
	require.NoError(t, err)
	l := newLoop(ctx, pipeline.AgentContext{Tools: reg}, LoopConfig{Pipeline: h}, driver{log: log}, func(protocol.Event) error { return nil })
	l.ts.tools = reg.Snapshot()
	batch, err := l.runToolBatch(a, toolCalls(a))
	require.NoError(t, err)
	require.False(t, batch.ran)
	require.Len(t, batch.messages, 2)
	var intents int
	for _, e := range log.Entries() {
		if c, ok := e.(sessions.ToolCall); ok {
			intents++
			require.Equal(t, 1, c.AssistantEntry)
		}
	}
	require.Equal(t, 2, intents)
	for _, m := range batch.messages {
		require.Equal(t, textBeforeDispatch, m.Content[0].(protocol.Text).Text)
	}
}

func TestUnparsableArgumentsGiveValidationErrorResult(t *testing.T) {
	reg := &tools.Registry{}
	require.NoError(t, reg.Register(tools.Echo{}, tools.SourceInfo{}))
	l := newLoop(context.Background(), pipeline.AgentContext{Tools: reg}, LoopConfig{}, driver{log: &sessions.MemoryLog{}}, func(protocol.Event) error { return nil })
	l.ts.tools = reg.Snapshot()
	b := &toolCoordinator{loop: l, assistantEntry: -1, seen: map[string]bool{}}
	slot, err := b.prepare(protocol.ToolCall{ID: "a", Name: "echo", Arguments: json.RawMessage(`{"n":`)})
	require.NoError(t, err)
	require.True(t, slot.ready)
	require.True(t, slot.outcome.isError)
	require.Contains(t, slot.outcome.result.Content[0].(protocol.Text).Text, `{"n":`)
}

func TestToolCallPointersAreFrozenAndDispatchable(t *testing.T) {
	original := protocol.AssistantMessage{StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{&protocol.ToolCall{ID: "a", Name: "echo", Arguments: json.RawMessage(`{"text":"safe"}`)}}}
	copy := cloneAssistant(original)
	copy.Content[0].(*protocol.ToolCall).Arguments[2] = 'X'
	require.Equal(t, `{"text":"safe"}`, string(original.Content[0].(*protocol.ToolCall).Arguments))
	calls := toolCalls(original)
	require.Len(t, calls, 1)
	require.Equal(t, "a", calls[0].ID)
}

func TestPreparedCallSignaturesStayPrivate(t *testing.T) {
	signature, namespace := "original-signature", "original-namespace"
	p := preparedCall{call: protocol.ToolCall{ID: "a", ThoughtSignature: &signature, Namespace: &namespace}}
	first := p.handlerCall()
	*first.ThoughtSignature = "changed"
	*first.Namespace = "changed"
	second := p.handlerCall()
	require.Equal(t, "original-signature", *second.ThoughtSignature)
	require.Equal(t, "original-namespace", *second.Namespace)
}
func TestPointerAssistantHistoryReservesCallID(t *testing.T) {
	historical := repairAssistant("same")
	l := newLoop(context.Background(), pipeline.AgentContext{Messages: []protocol.Message{&historical}}, LoopConfig{}, driver{log: &sessions.MemoryLog{}}, func(protocol.Event) error { return nil })
	require.True(t, l.usedToolIDs["same"], "public pointer assistant history must reserve its call IDs")
}
func TestRepairPointerAssistantRequest(t *testing.T) {
	log := &sessions.MemoryLog{}
	a := repairAssistant("same")
	_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: &a}, sessions.ToolCall{AssistantEntry: 1, CallID: "same"})
	require.NoError(t, err)
	require.NoError(t, repairTools(log, 0))
	require.Len(t, log.Messages(), 2, "pointer assistant request also needs a repair outcome")
	require.Equal(t, "outcome unknown", log.Messages()[1].(protocol.ToolResultMessage).Content[0].(protocol.Text).Text)
}

func TestRepairKeepsPointerToolResult(t *testing.T) {
	log := &sessions.MemoryLog{}
	a := repairAssistant("same")
	r := toolResultMessage(errorOutcome(protocol.ToolCall{ID: "same", Name: "t"}, "saved"))
	_, err := log.Append(sessions.TurnOpened{}, sessions.MessageEntry{Message: a}, sessions.MessageEntry{Message: &r})
	require.NoError(t, err)
	before := log.Entries()
	require.NoError(t, repairTools(log, 0))
	require.Equal(t, before, log.Entries())
	require.Len(t, log.Messages(), 2, "existing public pointer result must not get a duplicate repair result")
}

func TestToolRecordReadsSkipTypedNilMessages(t *testing.T) {
	var assistant *protocol.AssistantMessage
	var result *protocol.ToolResultMessage
	l := newLoop(context.Background(), pipeline.AgentContext{Messages: []protocol.Message{assistant, result}}, LoopConfig{}, driver{log: &sessions.MemoryLog{}}, func(protocol.Event) error { return nil })
	require.Empty(t, l.usedToolIDs)
	require.Nil(t, messageValue(assistant))
	require.Nil(t, messageValue(result))
}
