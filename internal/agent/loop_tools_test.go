package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// toolLabels keeps the tool events and the tool result messages.
func toolLabels(labels []string) []string {
	var out []string
	for _, l := range labels {
		if strings.HasPrefix(l, "tool_execution") || strings.Contains(l, "toolResult:") {
			out = append(out, l)
		}
	}
	return out
}

func call(name, id string, args map[string]any) faux.Block {
	if args == nil {
		args = map[string]any{}
	}
	return faux.ToolCall(name, args, faux.ID(id))
}

func okTool(name string) *funcTool {
	return &funcTool{name: name, run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		return textResult(name + " ok"), nil
	}}
}

type toolRun struct {
	msgs []protocol.Message
	rec  *recorder
	p    *faux.Provider
}

func runTools(t *testing.T, ctx context.Context, reg *tools.Registry, handlers *pipeline.Registry, wrap func(agent.Emit) agent.Emit, steps ...faux.Step) toolRun {
	t.Helper()
	p, m := newFaux(t)
	p.Set(steps...)
	rec := &recorder{}
	emit := agent.Emit(rec.emit)
	if wrap != nil {
		emit = wrap(emit)
	}
	var msgs []protocol.Message
	var err error
	within(t, 5*time.Second, func() {
		msgs, err = agent.Run(ctx, []protocol.Message{user("go")}, pipeline.AgentContext{Tools: reg}, config(p, m, handlers), emit)
	})
	require.NoError(t, err)
	return toolRun{msgs: msgs, rec: rec, p: p}
}

func TestToolCompletionOrderAndSourceOrder(t *testing.T) {
	bEnded := make(chan struct{})
	var once sync.Once
	a := &funcTool{name: "a", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if err := waitOrFail(ctx, bEnded, "end of b"); err != nil {
			return protocol.ToolExecutionResult{}, err
		}
		return textResult("a ok"), nil
	}}
	var seen [][]string
	var mu sync.Mutex
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		mu.Lock()
		seen = append(seen, roles(info.Context.Messages))
		mu.Unlock()
		return nil, nil
	})
	wrap := func(next agent.Emit) agent.Emit {
		return func(e protocol.Event) error {
			err := next(e)
			if label(e) == "tool_execution_end(b)" {
				once.Do(func() { close(bEnded) })
			}
			return err
		}
	}
	r := runTools(t, context.Background(), registry(t, safeTool{funcTool: a}, safeTool{funcTool: &funcTool{name: "b", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		once.Do(func() { close(bEnded) })
		return textResult("b ok"), nil
	}}}), hooks, wrap,
		faux.Reply(call("a", "a", nil), call("b", "b", nil)), faux.Say("done"))

	assert.Equal(t, []string{
		"tool_execution_start(a)", "tool_execution_start(b)",
		"tool_execution_end(a)",
		"message_start(toolResult:a)", "message_end(toolResult:a)",
		"tool_execution_end(b)",
		"message_start(toolResult:b)", "message_end(toolResult:b)",
	}, toolLabels(r.rec.eventLabels()))
	assert.Equal(t, [][]string{{"system", "user", "assistant"}, {"system", "user", "assistant"}}, seen,
		"no result joins the context during the batch")
	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:a", "toolResult:b"}, roles(r.p.Requests()[1].Transcript.Messages))
	assert.Equal(t, "a ok", resultText(toolResults(r.msgs)["a"].Content))
	assert.Equal(t, "b ok", resultText(toolResults(r.msgs)["b"].Content))
}

func TestToolParallelOverlap(t *testing.T) {
	var started sync.WaitGroup
	started.Add(2)
	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()
	barrier := func(name string) *funcTool {
		return &funcTool{name: name, run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
			started.Done()
			if err := waitOrFail(ctx, allStarted, "both tools"); err != nil {
				return protocol.ToolExecutionResult{}, err
			}
			return textResult(name + " ok"), nil
		}}
	}
	r := runTools(t, context.Background(), registry(t, safeTool{funcTool: barrier("a")}, safeTool{funcTool: barrier("b")}), nil, nil,
		faux.Reply(call("a", "a", nil), call("b", "b", nil)), faux.Say("done"))
	res := toolResults(r.msgs)
	assert.False(t, res["a"].IsError, resultText(res["a"].Content))
	assert.False(t, res["b"].IsError, resultText(res["b"].Content))
}

func TestUndeclaredToolRunsAloneBetweenSafeCalls(t *testing.T) {
	var active, peak atomic.Int32
	counting := func(name string) *funcTool {
		return &funcTool{name: name, run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			n := active.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			active.Add(-1)
			return textResult(name + " ok"), nil
		}}
	}
	reg := registry(t, safeTool{funcTool: counting("a")}, counting("s"))
	r := runTools(t, context.Background(), reg, nil, nil,
		faux.Reply(call("a", "a1", nil), call("s", "s1", nil), call("a", "a2", nil)), faux.Say("done"))

	assert.Equal(t, int32(1), peak.Load())
	assert.Equal(t, []string{
		"tool_execution_start(a1)", "tool_execution_start(s1)", "tool_execution_end(a1)", "message_start(toolResult:a1)", "message_end(toolResult:a1)",
		"tool_execution_end(s1)", "message_start(toolResult:s1)", "message_end(toolResult:s1)",
		"tool_execution_start(a2)", "tool_execution_end(a2)", "message_start(toolResult:a2)", "message_end(toolResult:a2)",
	}, toolLabels(r.rec.eventLabels()))
}

func TestToolPreflightFailures(t *testing.T) {
	var before, after []string
	control := &recorder{}
	observed := map[string]pipeline.ToolResultInfo{}
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		control.note("before:" + info.Call.ID)
		defer control.note("before_return:" + info.Call.ID)
		before = append(before, info.Call.ID)
		if info.Call.ID == "blocked" {
			return &pipeline.BeforeToolCallResult{Block: true, Reason: "not allowed"}, nil
		}
		if info.Call.ID == "silent" {
			return &pipeline.BeforeToolCallResult{Block: true}, nil
		}
		return nil, nil
	})
	hooks.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		control.note("execute:" + in.Call.ID)
		return next(ctx, in)
	})
	hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		control.note("after:" + info.Call.ID)
		observed[info.Call.ID] = info
		after = append(after, info.Call.ID)
		return nil, nil
	})
	r := runTools(t, context.Background(), registry(t, tools.Echo{}), hooks, nil,
		faux.Reply(
			call("nope", "unknown", nil),
			call("echo", "invalid", map[string]any{}),
			call("echo", "blocked", map[string]any{"text": "x"}),
			call("echo", "silent", map[string]any{"text": "x"}),
			call("echo", "ok", map[string]any{"text": "fine"}),
		), faux.Say("done"))

	res := toolResults(r.msgs)
	want := map[string]string{
		"unknown": "Tool nope not found",
		"invalid": "Validation failed for tool \"echo\":\n  - text: must have required properties text\n\nReceived arguments:\n{}",
		"blocked": "not allowed",
		"silent":  "Tool execution was blocked",
	}
	for id, text := range want {
		assert.True(t, res[id].IsError, id)
		assert.Equal(t, text, resultText(res[id].Content), id)
		assert.JSONEq(t, `{}`, string(res[id].Details), id)
		wire, err := json.Marshal(res[id])
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(wire, &fields))
		assert.JSONEq(t, `{}`, string(fields["details"]), id)
		for _, key := range []string{"error", "name", "code", "errorName", "errorCode"} {
			assert.NotContains(t, fields, key, id)
		}
	}
	assert.False(t, res["ok"].IsError)
	assert.Equal(t, "fine", resultText(res["ok"].Content))
	assert.Equal(t, []string{"unknown", "blocked", "silent", "ok"}, before, "unknown names reach policy; invalid known arguments do not")
	assert.Equal(t, []string{"unknown", "blocked", "silent", "ok"}, after, "post-control sees body failures, denied calls and success")
	assert.Equal(t, []string{
		"before:unknown", "before_return:unknown", "execute:unknown", "after:unknown",
		"before:blocked", "before_return:blocked", "after:blocked",
		"before:silent", "before_return:silent", "after:silent",
		"before:ok", "before_return:ok", "execute:ok", "after:ok",
	}, control.labels(), "validation and denial skip execution; execution follows completed pre-control")
	for id, info := range observed {
		assert.Equal(t, res[id].IsError, info.IsError, id)
		assert.Equal(t, resultText(res[id].Content), resultText(info.Result.Content), id)
	}
	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:unknown", "toolResult:invalid", "toolResult:blocked", "toolResult:silent", "toolResult:ok", "assistant"}, roles(r.msgs))
}

func TestToolBlockTerminate(t *testing.T) {
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		return &pipeline.BeforeToolCallResult{Block: true, Reason: "stop here", Terminate: true}, nil
	})
	r := runTools(t, context.Background(), registry(t, tools.Echo{}), hooks, nil,
		faux.Reply(call("echo", "c1", map[string]any{"text": "x"})), faux.Say("unused"))
	assert.Equal(t, 1, r.p.Calls(), "a batch where every result terminates ends the run")
	assert.Equal(t, "agent_end", r.rec.eventLabels()[len(r.rec.eventLabels())-1])
}

func TestToolArgs(t *testing.T) {
	schema := `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`

	t.Run("raw for events and hooks, prepared for Execute", func(t *testing.T) {
		var executed, hookRaw, hookPrepared, afterArgs json.RawMessage
		tool := &funcTool{name: "num", schema: schema, run: func(_ context.Context, tc tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
			executed = args
			tc.Update(textResult("half"))
			return textResult("ok"), nil
		}}
		hooks := pipeline.NewRegistry()
		hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
			hookRaw, hookPrepared = info.Call.Arguments, info.Args
			return nil, nil
		})
		hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
			afterArgs = info.Args
			return nil, nil
		})
		r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
			faux.Reply(call("num", "c1", map[string]any{"n": "5"})), faux.Say("done"))

		assert.JSONEq(t, `{"n":5}`, string(executed))
		assert.JSONEq(t, `{"n":"5"}`, string(hookRaw))
		assert.JSONEq(t, `{"n":5}`, string(hookPrepared))
		assert.JSONEq(t, `{"n":5}`, string(afterArgs))
		for _, e := range r.rec.events {
			switch v := e.(type) {
			case *protocol.ToolExecutionStart:
				assert.JSONEq(t, `{"n":"5"}`, string(v.Args))
			case *protocol.ToolExecutionUpdate:
				assert.JSONEq(t, `{"n":"5"}`, string(v.Args))
			}
		}
	})

	t.Run("PrepareArguments runs before validation", func(t *testing.T) {
		var executed json.RawMessage
		tool := preparingTool{&funcTool{name: "num", schema: schema,
			run: func(_ context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
				executed = args
				return textResult("ok"), nil
			},
			prepare: func(raw json.RawMessage) (json.RawMessage, error) {
				var legacy struct{ Count int }
				if err := json.Unmarshal(raw, &legacy); err != nil {
					return nil, err
				}
				return json.Marshal(map[string]int{"n": legacy.Count})
			},
		}}
		r := runTools(t, context.Background(), registry(t, tool), nil, nil,
			faux.Reply(call("num", "c1", map[string]any{"count": 3})), faux.Say("done"))
		assert.JSONEq(t, `{"n":3}`, string(executed))
		assert.False(t, toolResults(r.msgs)["c1"].IsError)
	})
}

func TestToolLateUpdateDropped(t *testing.T) {
	var saved func(protocol.ToolExecutionResult)
	tool := &funcTool{name: "u", run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		saved = tc.Update
		tc.Update(textResult("partial"))
		return textResult("ok"), nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
		saved(textResult("late"))
		return nil, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("u", "c1", nil)), faux.Say("done"))
	assert.Equal(t, []string{
		"tool_execution_start(c1)", "tool_execution_update(c1)", "tool_execution_end(c1)",
		"message_start(toolResult:c1)", "message_end(toolResult:c1)",
	}, toolLabels(r.rec.eventLabels()))
	for _, e := range r.rec.events {
		if u, ok := e.(*protocol.ToolExecutionUpdate); ok {
			assert.Equal(t, "partial", resultText(u.PartialResult.Content))
		}
	}
}

func TestAfterToolCallOverride(t *testing.T) {
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		return protocol.ToolExecutionResult{
			Content:           []protocol.UserBlock{protocol.Text{Text: "orig"}},
			Details:           json.RawMessage(`{"keep":true}`),
			StructuredContent: json.RawMessage(`{"s":1}`),
		}, nil
	}}
	var seenResult string
	var seenErr bool
	hooks := pipeline.NewRegistry()
	hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		seenResult, seenErr = resultText(info.Result.Content), info.IsError
		isErr := true
		return &pipeline.AfterToolCallResult{Content: []protocol.UserBlock{protocol.Text{Text: "over"}}, IsError: &isErr}, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("t", "c1", nil)), faux.Say("done"))

	assert.Equal(t, "orig", seenResult)
	assert.False(t, seenErr)
	res := toolResults(r.msgs)["c1"]
	assert.Equal(t, "over", resultText(res.Content))
	assert.True(t, res.IsError)
	assert.JSONEq(t, `{"keep":true}`, string(res.Details), "fields the hook leaves nil are kept")
	end := r.rec.toolEnds()["c1"]
	assert.Equal(t, "over", resultText(end.Result.Content))
	assert.True(t, end.IsError)
	assert.Nil(t, end.Result.StructuredContent, "replaced content drops the old structured content")
}

func TestToolDuplicateID(t *testing.T) {
	r := runTools(t, context.Background(), registry(t, tools.Echo{}), nil, nil,
		faux.Reply(call("echo", "d", map[string]any{"text": "one"}), call("echo", "d", map[string]any{"text": "two"})), faux.Say("done"))
	var results []protocol.ToolResultMessage
	for _, m := range r.msgs {
		if res, ok := m.(protocol.ToolResultMessage); ok {
			results = append(results, res)
		}
	}
	require.Len(t, results, 2)
	assert.False(t, results[0].IsError)
	assert.Equal(t, "one", resultText(results[0].Content))
	assert.True(t, results[1].IsError)
	assert.Equal(t, `Tool call id "d" is used more than once in this message`, resultText(results[1].Content))
}

func TestToolFailuresBecomeResults(t *testing.T) {
	failing := func(run func() (protocol.ToolExecutionResult, error)) *funcTool {
		return &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			return run()
		}}
	}
	ok := func() (protocol.ToolExecutionResult, error) { return textResult("ok"), nil }
	cases := []struct {
		name  string
		tool  tools.Tool
		setup func(reg *pipeline.Registry)
		text  string
	}{
		{"Execute panic", failing(func() (protocol.ToolExecutionResult, error) { panic("kaboom") }), nil, "kaboom"},
		{"Execute error panic value", failing(func() (protocol.ToolExecutionResult, error) { panic(errors.New("err value")) }), nil, "err value"},
		{"Execute error", failing(func() (protocol.ToolExecutionResult, error) {
			return protocol.ToolExecutionResult{}, errors.New("disk full")
		}), nil, "disk full"},
		{"BeforeTool panic", failing(ok), func(reg *pipeline.Registry) {
			reg.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
				panic("before broke")
			})
		}, "before broke"},
		{"BeforeTool error", failing(ok), func(reg *pipeline.Registry) {
			reg.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
				return nil, errors.New("before failed")
			})
		}, "before failed"},
		{"AfterTool panic", failing(ok), func(reg *pipeline.Registry) {
			reg.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
				panic("after broke")
			})
		}, "after broke"},
		{"AfterTool error", failing(ok), func(reg *pipeline.Registry) {
			reg.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
				return nil, errors.New("after failed")
			})
		}, "after failed"},
		{"PrepareArguments panic", preparingTool{&funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			return ok()
		}, prepare: func(json.RawMessage) (json.RawMessage, error) { panic("prepare broke") }}}, nil, "prepare broke"},
		{"PrepareArguments error", preparingTool{&funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			return ok()
		}, prepare: func(json.RawMessage) (json.RawMessage, error) { return nil, errors.New("bad shape") }}}, nil, "bad shape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reg *pipeline.Registry
			if tc.setup != nil {
				reg = pipeline.NewRegistry()
				tc.setup(reg)
			}
			r := runTools(t, context.Background(), registry(t, tc.tool), reg, nil,
				faux.Reply(call("t", "c1", nil)), faux.Say("done"))
			res := toolResults(r.msgs)["c1"]
			assert.True(t, res.IsError)
			assert.Equal(t, tc.text, resultText(res.Content))
			assert.True(t, r.rec.toolEnds()["c1"].IsError)
			assert.Equal(t, 2, r.p.Calls(), "the loop goes on")
		})
	}
}

func TestAbortParallelBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var executed atomic.Int32
	started := make(chan struct{})
	counted := func(name string) *funcTool {
		return &funcTool{name: name, run: func(ctx context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
			executed.Add(1)
			if tc.CallID == "a" {
				close(started)
				<-ctx.Done()
			}
			return textResult(name + " ok"), nil
		}}
	}
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if info.Call.ID == "b" {
			<-started
			cancel()
		}
		return nil, nil
	})
	r := runTools(t, ctx, registry(t, safeTool{funcTool: counted("t")}), hooks, nil,
		faux.Reply(call("t", "a", nil), call("t", "b", nil), call("t", "c", nil)), faux.Say("unused"))

	res := toolResults(r.msgs)
	require.Len(t, res, 3)
	require.Equal(t, "Operation aborted", resultText(res["a"].Content))
	for _, id := range []string{"b", "c"} {
		require.True(t, res[id].IsError)
		require.Equal(t, "Tool call aborted before dispatch", resultText(res[id].Content))
	}
	require.Equal(t, int32(1), executed.Load())
	require.Equal(t, 1, r.p.Calls())
	require.Equal(t, protocol.StopAborted, lastAssistant(t, r.msgs).StopReason)
	require.Equal(t, 1, strings.Count(strings.Join(r.rec.eventLabels(), ","), "turn_start"))
}

func TestAbortSequentialBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := exclusiveTool{&funcTool{name: "s", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		cancel()
		return textResult("s ok"), nil
	}}}
	r := runTools(t, ctx, registry(t, first, okTool("t")), nil, nil,
		faux.Reply(call("s", "a", nil), call("t", "b", nil), call("t", "c", nil)), faux.Say("unused"))

	res := toolResults(r.msgs)
	require.Len(t, res, 3)
	require.Equal(t, "Operation aborted", resultText(res["a"].Content))
	require.Equal(t, "Tool call aborted before dispatch", resultText(res["b"].Content))
	require.Equal(t, "Tool call aborted before dispatch", resultText(res["c"].Content))
	require.Equal(t, 1, r.p.Calls())
	require.Equal(t, protocol.StopAborted, lastAssistant(t, r.msgs).StopReason)
}

func BenchmarkLoopTurn(b *testing.B) {
	p, m := newFaux(b)
	reg := registry(b, tools.Echo{})
	handlers := pipeline.NewRegistry()
	handlers.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		return pipeline.End, nil
	})
	cfg := config(p, m, handlers)
	prompts := []protocol.Message{user("go")}
	emit := func(protocol.Event) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		p.Set(faux.Reply(faux.Text("calling"), faux.ToolCall("echo", map[string]any{"text": "hi"})))
		if _, err := agent.Run(context.Background(), prompts, pipeline.AgentContext{Tools: reg}, cfg, emit); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBeforeToolBlockWithReason(t *testing.T) {
	executed := 0
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		executed++
		return textResult("ran"), nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		return &pipeline.BeforeToolCallResult{Block: true, Reason: "policy says no"}, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("t", "c1", nil)), faux.Say("done"))

	res := toolResults(r.msgs)["c1"]
	assert.True(t, res.IsError, "a deny is an error result")
	assert.Equal(t, "policy says no", resultText(res.Content), "the model gets the reason")
	assert.Equal(t, "c1", res.ToolCallID)
	assert.Zero(t, executed, "the body never runs")
	assert.Equal(t, 2, r.p.Calls(), "the loop goes on")
}

func TestAfterToolReplacedContentDropsStructuredContent(t *testing.T) {
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		return protocol.ToolExecutionResult{
			Content:           []protocol.UserBlock{protocol.Text{Text: "orig"}},
			StructuredContent: json.RawMessage(`{"s":1}`),
		}, nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
		return &pipeline.AfterToolCallResult{Content: []protocol.UserBlock{protocol.Text{Text: "replaced"}}}, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("t", "c1", nil)), faux.Say("done"))

	res := toolResults(r.msgs)["c1"]
	assert.Equal(t, "c1", res.ToolCallID, "the replacement keeps the call ID")
	assert.Equal(t, "replaced", resultText(res.Content))
	assert.Nil(t, r.rec.toolEnds()["c1"].Result.StructuredContent, "structured content may not match the new content")
	assert.False(t, res.IsError)
}

func TestExecuteToolHandlerWrapsBody(t *testing.T) {
	runs := 0
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		runs++
		return textResult("body"), nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		assert.Equal(t, "c1", in.Call.ID)
		res, err := next(ctx, in)
		res.Content = []protocol.UserBlock{protocol.Text{Text: resultText(res.Content) + "+wrapped"}}
		return res, err
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("t", "c1", nil)), faux.Say("done"))

	assert.Equal(t, 1, runs)
	assert.Equal(t, "body+wrapped", resultText(toolResults(r.msgs)["c1"].Content))
}
