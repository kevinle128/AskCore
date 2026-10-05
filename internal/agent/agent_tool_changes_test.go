package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// These tests run the real Agent, registry, snapshot and loop. Only the
// model is the scripted faux provider.

// toolDeltas returns the tool changes that the system messages in msgs
// declare, as "+name" and "-name", in order.
func toolDeltas(msgs []protocol.Message) []string {
	var out []string
	for _, m := range msgs {
		sys, ok := m.(protocol.SystemMessage)
		if !ok {
			continue
		}
		for _, r := range sys.ToolsRemoved {
			out = append(out, "-"+r.Name)
		}
		for _, d := range sys.ToolsAdded {
			out = append(out, "+"+d.Name)
		}
	}
	return out
}

func lateTool(ran *bool) *funcTool {
	return &funcTool{name: "late", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		*ran = true
		return textResult("late ran"), nil
	}}
}

// installer registers the late tool when it runs, the way an extension or
// an MCP connection adds a tool while a run is going on.
func installer(reg *tools.Registry, late tools.Tool) sequentialTool {
	return sequentialTool{&funcTool{name: "install", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		if err := reg.Register(late, tools.SourceInfo{Kind: tools.SourceExtension}); err != nil {
			return protocol.ToolExecutionResult{}, err
		}
		return textResult("installed"), nil
	}}}
}

func TestToolRegisteredMidRunIsDeclaredThenRuns(t *testing.T) {
	p, m := newFaux(t)
	p.Set(
		faux.Reply(faux.ToolCall("install", nil, faux.ID("c1"))),
		faux.Reply(faux.ToolCall("late", nil, faux.ID("c2"))),
		faux.Say("done"),
	)
	reg := &tools.Registry{}
	ran := false
	require.NoError(t, reg.Register(installer(reg, lateTool(&ran)), tools.SourceInfo{Kind: tools.SourceBuiltin}))
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = reg })

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.True(t, ran, "the late tool runs once it is declared")
	reqs := p.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, []string{"+install"}, toolDeltas(reqs[0].Transcript.Messages), "request 1 knows only the first tool")
	assert.Equal(t, []string{"+install", "+late"}, toolDeltas(reqs[1].Transcript.Messages), "the new tool is declared before request 2")
	assert.Equal(t, []string{"+install", "+late"}, toolDeltas(reqs[2].Transcript.Messages), "the delta is declared once, not again on each turn")
	assert.Equal(t, []string{"user", "assistant", "toolResult:c1", "system", "assistant", "toolResult:c2", "assistant"}, roles(a.State().Messages),
		"the declaration is kept in the log, after the result of the turn that added the tool")
}

func TestToolRegisteredDuringTurnDoesNotRunInThatTurn(t *testing.T) {
	p, m := newFaux(t)
	// The model calls both tools in one message. install runs first (it is
	// sequential) and registers late, but the turn runs against the
	// snapshot taken before the request, where late does not exist.
	p.Set(
		faux.Reply(faux.ToolCall("install", nil, faux.ID("c1")), faux.ToolCall("late", nil, faux.ID("c2"))),
		faux.Say("done"),
	)
	reg := &tools.Registry{}
	ran := false
	require.NoError(t, reg.Register(installer(reg, lateTool(&ran)), tools.SourceInfo{Kind: tools.SourceBuiltin}))
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = reg })

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.False(t, ran, "a tool the model was not told about must not run")
	res := toolResults(a.State().Messages)
	assert.True(t, res["c2"].IsError)
	assert.Equal(t, "Tool late not found", resultText(res["c2"].Content))
	assert.Equal(t, []string{"+install", "+late"}, toolDeltas(p.Requests()[1].Transcript.Messages), "the next request declares it")
}

func TestToolUnregisteredMidRunRunsThisTurnThenIsRemoved(t *testing.T) {
	p, m := newFaux(t)
	p.Set(
		faux.Reply(faux.ToolCall("gone", nil, faux.ID("c1"))),
		faux.Reply(faux.ToolCall("gone", nil, faux.ID("c2"))),
		faux.Say("done"),
	)
	runs := 0
	gone := &funcTool{name: "gone", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		runs++
		return textResult("ok"), nil
	}}
	reg := registry(t, tools.Echo{}, gone)
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = reg
		c.Hooks.FinishTurn = func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			reg.Unregister("gone")
			return pipeline.Proceed, nil
		}
	})

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, 1, runs, "the first call runs; the second comes after the tool was removed")
	res := toolResults(a.State().Messages)
	assert.False(t, res["c1"].IsError)
	assert.True(t, res["c2"].IsError)
	assert.Equal(t, "Tool gone not found", resultText(res["c2"].Content))
	assert.Equal(t, []string{"+echo", "+gone", "-gone"}, toolDeltas(p.Requests()[1].Transcript.Messages), "the removal is declared before request 2")
}

func TestUnchangedToolsAddNoSystemMessage(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	a := newAgent(t, p, m, nil)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	for _, msg := range a.State().Messages {
		assert.NotEqual(t, protocol.RoleSystem, msg.Role(), "no tool change, so the log gets no system message")
	}
}
