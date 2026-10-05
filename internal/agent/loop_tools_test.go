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

func runTools(t *testing.T, ctx context.Context, reg *tools.Registry, hooks pipeline.Hooks, wrap func(agent.Emit) agent.Emit, steps ...faux.Step) toolRun {
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
		msgs, err = agent.Run(ctx, []protocol.Message{user("go")}, pipeline.AgentContext{Tools: reg}, config(p, m, hooks), emit)
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
	hooks := pipeline.Hooks{BeforeToolCall: func(_ context.Context, info pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
		mu.Lock()
		seen = append(seen, roles(info.Context.Messages))
		mu.Unlock()
		return nil, nil
	}}
	wrap := func(next agent.Emit) agent.Emit {
		return func(e protocol.Event) error {
			err := next(e)
			if label(e) == "tool_execution_end(b)" {
				once.Do(func() { close(bEnded) })
			}
			return err
		}
	}
	r := runTools(t, context.Background(), registry(t, a, okTool("b")), hooks, wrap,
		faux.Reply(call("a", "a", nil), call("b", "b", nil)), faux.Say("done"))

	assert.Equal(t, []string{
		"tool_execution_start(a)", "tool_execution_start(b)",
		"tool_execution_end(b)", "tool_execution_end(a)",
		"message_start(toolResult:a)", "message_end(toolResult:a)",
		"message_start(toolResult:b)", "message_end(toolResult:b)",
	}, toolLabels(r.rec.eventLabels()))
	assert.Equal(t, [][]string{{"system", "user", "assistant"}, {"system", "user", "assistant"}}, seen,
		"no result joins the context during the batch")
	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:a", "toolResult:b"}, roles(r.p.Requests()[1].Transcript.Messages))
	assert.Equal(t, "a ok", resultText(toolResults(r.msgs)["a"].Content))
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
	r := runTools(t, context.Background(), registry(t, barrier("a"), barrier("b")), pipeline.Hooks{}, nil,
		faux.Reply(call("a", "a", nil), call("b", "b", nil)), faux.Say("done"))
	res := toolResults(r.msgs)
	assert.False(t, res["a"].IsError, resultText(res["a"].Content))
	assert.False(t, res["b"].IsError, resultText(res["b"].Content))
}

func TestToolSequentialSwitch(t *testing.T) {
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
	reg := registry(t, counting("a"), sequentialTool{counting("s")})
	r := runTools(t, context.Background(), reg, pipeline.Hooks{}, nil,
		faux.Reply(call("a", "a1", nil), call("s", "s1", nil), call("a", "a2", nil)), faux.Say("done"))

	assert.Equal(t, int32(1), peak.Load())
	assert.Equal(t, []string{
		"tool_execution_start(a1)", "tool_execution_end(a1)", "message_start(toolResult:a1)", "message_end(toolResult:a1)",
		"tool_execution_start(s1)", "tool_execution_end(s1)", "message_start(toolResult:s1)", "message_end(toolResult:s1)",
		"tool_execution_start(a2)", "tool_execution_end(a2)", "message_start(toolResult:a2)", "message_end(toolResult:a2)",
	}, toolLabels(r.rec.eventLabels()))
}

func TestToolPreflightFailures(t *testing.T) {
	var before, after []string
	hooks := pipeline.Hooks{
		BeforeToolCall: func(_ context.Context, info pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
			before = append(before, info.Call.ID)
			if info.Call.ID == "blocked" {
				return &pipeline.BeforeToolCallResult{Block: true, Reason: "not allowed"}, nil
			}
			if info.Call.ID == "silent" {
				return &pipeline.BeforeToolCallResult{Block: true}, nil
			}
			return nil, nil
		},
		AfterToolCall: func(_ context.Context, info pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
			after = append(after, info.Call.ID)
			return nil, nil
		},
	}
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
	}
	assert.False(t, res["ok"].IsError)
	assert.Equal(t, "fine", resultText(res["ok"].Content))
	assert.Equal(t, []string{"blocked", "silent", "ok"}, before, "BeforeToolCall runs only for valid calls")
	assert.Equal(t, []string{"ok"}, after, "AfterToolCall runs only for executed calls")
	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:unknown", "toolResult:invalid", "toolResult:blocked", "toolResult:silent", "toolResult:ok", "assistant"}, roles(r.msgs))
}

func TestToolBlockTerminate(t *testing.T) {
	hooks := pipeline.Hooks{BeforeToolCall: func(context.Context, pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
		return &pipeline.BeforeToolCallResult{Block: true, Reason: "stop here", Terminate: true}, nil
	}}
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
		hooks := pipeline.Hooks{
			BeforeToolCall: func(_ context.Context, info pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
				hookRaw, hookPrepared = info.Call.Arguments, info.Args
				return nil, nil
			},
			AfterToolCall: func(_ context.Context, info pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
				afterArgs = info.Args
				return nil, nil
			},
		}
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

	t.Run("BeforeToolCall replaces the args", func(t *testing.T) {
		var executed, afterArgs json.RawMessage
		tool := &funcTool{name: "num", schema: schema, run: func(_ context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
			executed = args
			return textResult("ok"), nil
		}}
		hooks := pipeline.Hooks{
			BeforeToolCall: func(context.Context, pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
				return &pipeline.BeforeToolCallResult{Args: json.RawMessage(`{"n":7}`)}, nil
			},
			AfterToolCall: func(_ context.Context, info pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
				afterArgs = info.Args
				return nil, nil
			},
		}
		runTools(t, context.Background(), registry(t, tool), hooks, nil,
			faux.Reply(call("num", "c1", map[string]any{"n": 5})), faux.Say("done"))
		assert.JSONEq(t, `{"n":7}`, string(executed))
		assert.JSONEq(t, `{"n":7}`, string(afterArgs))
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
		r := runTools(t, context.Background(), registry(t, tool), pipeline.Hooks{}, nil,
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
	hooks := pipeline.Hooks{AfterToolCall: func(context.Context, pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
		saved(textResult("late"))
		return nil, nil
	}}
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
	hooks := pipeline.Hooks{AfterToolCall: func(_ context.Context, info pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
		seenResult, seenErr = resultText(info.Result.Content), info.IsError
		isErr := true
		return &pipeline.AfterToolCallResult{Content: []protocol.UserBlock{protocol.Text{Text: "over"}}, IsError: &isErr}, nil
	}}
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
	r := runTools(t, context.Background(), registry(t, tools.Echo{}), pipeline.Hooks{}, nil,
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
		hooks pipeline.Hooks
		text  string
	}{
		{"Execute panic", failing(func() (protocol.ToolExecutionResult, error) { panic("kaboom") }), pipeline.Hooks{}, "kaboom"},
		{"Execute error panic value", failing(func() (protocol.ToolExecutionResult, error) { panic(errors.New("err value")) }), pipeline.Hooks{}, "err value"},
		{"Execute error", failing(func() (protocol.ToolExecutionResult, error) {
			return protocol.ToolExecutionResult{}, errors.New("disk full")
		}), pipeline.Hooks{}, "disk full"},
		{"BeforeToolCall panic", failing(ok), pipeline.Hooks{BeforeToolCall: func(context.Context, pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
			panic("before broke")
		}}, "before broke"},
		{"BeforeToolCall error", failing(ok), pipeline.Hooks{BeforeToolCall: func(context.Context, pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
			return nil, errors.New("before failed")
		}}, "before failed"},
		{"AfterToolCall panic", failing(ok), pipeline.Hooks{AfterToolCall: func(context.Context, pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
			panic("after broke")
		}}, "after broke"},
		{"AfterToolCall error", failing(ok), pipeline.Hooks{AfterToolCall: func(context.Context, pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
			return nil, errors.New("after failed")
		}}, "after failed"},
		{"PrepareArguments panic", preparingTool{&funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			return ok()
		}, prepare: func(json.RawMessage) (json.RawMessage, error) { panic("prepare broke") }}}, pipeline.Hooks{}, "prepare broke"},
		{"PrepareArguments error", preparingTool{&funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			return ok()
		}, prepare: func(json.RawMessage) (json.RawMessage, error) { return nil, errors.New("bad shape") }}}, pipeline.Hooks{}, "bad shape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := runTools(t, context.Background(), registry(t, tc.tool), tc.hooks, nil,
				faux.Reply(call("t", "c1", nil)), faux.Say("done"))
			res := toolResults(r.msgs)["c1"]
			assert.True(t, res.IsError)
			assert.Equal(t, tc.text, resultText(res.Content))
			assert.True(t, r.rec.toolEnds()["c1"].IsError)
			assert.Equal(t, 2, r.p.Calls(), "the loop goes on")
		})
	}
}

func TestLengthGuard(t *testing.T) {
	var hooked []string
	hooks := pipeline.Hooks{
		BeforeToolCall: func(_ context.Context, info pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
			hooked = append(hooked, "before:"+info.Call.ID)
			return nil, nil
		},
		AfterToolCall: func(_ context.Context, info pipeline.ToolResultInfo) (*pipeline.AfterToolCallResult, error) {
			hooked = append(hooked, "after:"+info.Call.ID)
			return nil, nil
		},
	}
	executed := false
	tool := &funcTool{name: "write", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		executed = true
		return textResult("ok"), nil
	}}
	r := runTools(t, context.Background(), registry(t, tool, tools.Echo{}), hooks, nil,
		faux.Reply(call("write", "a", nil), call("echo", "b", map[string]any{"text": "x"})).Stop(protocol.StopLength),
		faux.Say("done"))

	assert.False(t, executed)
	assert.Empty(t, hooked)
	assert.Equal(t, 2, r.p.Calls())
	assert.Equal(t, []string{
		"tool_execution_start(a)", "tool_execution_end(a)", "message_start(toolResult:a)", "message_end(toolResult:a)",
		"tool_execution_start(b)", "tool_execution_end(b)", "message_start(toolResult:b)", "message_end(toolResult:b)",
	}, toolLabels(r.rec.eventLabels()))
	res := toolResults(r.msgs)
	assert.Equal(t, `Tool call "write" was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments.`, resultText(res["a"].Content))
	assert.Equal(t, `Tool call "echo" was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments.`, resultText(res["b"].Content))
	assert.True(t, res["a"].IsError)
	assert.True(t, res["b"].IsError)
}

func TestAbortParallelBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var executed atomic.Int32
	counted := func(name string) *funcTool {
		return &funcTool{name: name, run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			executed.Add(1)
			return textResult(name + " ok"), nil
		}}
	}
	hooks := pipeline.Hooks{BeforeToolCall: func(_ context.Context, info pipeline.ToolCallInfo) (*pipeline.BeforeToolCallResult, error) {
		if info.Call.ID == "b" {
			cancel()
		}
		return nil, nil
	}}
	r := runTools(t, ctx, registry(t, counted("t")), hooks, nil,
		faux.Reply(call("t", "a", nil), call("t", "b", nil), call("t", "c", nil)), faux.Say("unused"))

	labels := r.rec.eventLabels()
	i := indexOf(labels, "tool_execution_start(a)")
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, []string{
		"tool_execution_start(a)", "tool_execution_start(b)",
		"tool_execution_end(b)", "tool_execution_end(a)",
		"message_start(toolResult:a)", "message_end(toolResult:a)",
		"message_start(toolResult:b)", "message_end(toolResult:b)",
		"turn_end",
		"turn_start",
		"message_start(assistant)", "message_end(assistant)",
		"turn_end",
		"agent_end",
	}, labels[i:])
	res := toolResults(r.msgs)
	assert.Equal(t, "Operation aborted", resultText(res["a"].Content))
	assert.Equal(t, "Operation aborted", resultText(res["b"].Content))
	assert.True(t, res["a"].IsError)
	assert.True(t, res["b"].IsError)
	assert.NotContains(t, res, "c")
	assert.Equal(t, int32(0), executed.Load())
	assert.Equal(t, 2, r.p.Calls(), "the loop makes one more request with the cancelled context")
	assert.Equal(t, protocol.StopAborted, lastAssistant(t, r.msgs).StopReason)
}

func TestAbortSequentialBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := sequentialTool{&funcTool{name: "s", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		cancel()
		return textResult("s ok"), nil
	}}}
	r := runTools(t, ctx, registry(t, first, okTool("t")), pipeline.Hooks{}, nil,
		faux.Reply(call("s", "a", nil), call("t", "b", nil), call("t", "c", nil)), faux.Say("unused"))

	labels := r.rec.eventLabels()
	i := indexOf(labels, "tool_execution_start(a)")
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, []string{
		"tool_execution_start(a)", "tool_execution_end(a)",
		"message_start(toolResult:a)", "message_end(toolResult:a)",
		"turn_end",
		"turn_start",
		"message_start(assistant)", "message_end(assistant)",
		"turn_end",
		"agent_end",
	}, labels[i:])
	assert.Equal(t, "s ok", resultText(toolResults(r.msgs)["a"].Content))
	assert.Equal(t, 2, r.p.Calls())
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

func BenchmarkLoopTurn(b *testing.B) {
	p, m := newFaux(b)
	reg := registry(b, tools.Echo{})
	cfg := config(p, m, pipeline.Hooks{FinishTurn: func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		return pipeline.End, nil
	}})
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
