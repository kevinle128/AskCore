package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestBeforeToolErrorSkipsAfterTool(t *testing.T) {
	h := pipeline.NewRegistry()
	after := 0
	h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		return nil, errors.New("before error")
	})
	h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
		after++
		return nil, nil
	})
	r := runTools(t, context.Background(), registry(t, okTool("t")), h, nil, faux.Reply(call("t", "a", nil)), faux.Say("done"))
	require.Zero(t, after)
	require.Equal(t, "before error", resultText(toolResults(r.msgs)["a"].Content))
}
func TestValidationErrorRunsNoHook(t *testing.T) {
	h := pipeline.NewRegistry()
	hooks := 0
	h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		hooks++
		return nil, nil
	})
	h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
		hooks++
		return nil, nil
	})
	r := runTools(t, context.Background(), registry(t, tools.Echo{}), h, nil, faux.Reply(call("echo", "a", nil)), faux.Say("done"))
	require.Zero(t, hooks)
	require.Contains(t, resultText(toolResults(r.msgs)["a"].Content), "Validation failed")
}
func TestExecuteToolShortCircuitSkipsBody(t *testing.T) {
	var bodies, after int
	tool := okTool("t")
	tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies++
		return textResult("body"), nil
	}
	h := pipeline.NewRegistry()
	h.OnExecuteTool(func(context.Context, pipeline.ExecuteToolInput, pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		return textResult("cached"), nil
	})
	h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
		after++
		return nil, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), h, nil, faux.Reply(call("t", "a", nil)), faux.Say("done"))
	require.Zero(t, bodies)
	require.Equal(t, 1, after)
	require.Equal(t, "cached", resultText(toolResults(r.msgs)["a"].Content))
}
func TestExecuteToolErrorSkipsAfterTool(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{true: "panic", false: "error"}[panics], func(t *testing.T) {
			after := 0
			h := pipeline.NewRegistry()
			h.OnExecuteTool(func(context.Context, pipeline.ExecuteToolInput, pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
				if panics {
					panic("dispatch failed")
				}
				return protocol.ToolExecutionResult{}, errors.New("dispatch failed")
			})
			h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
				after++
				return nil, nil
			})
			r := runTools(t, context.Background(), registry(t, okTool("t")), h, nil, faux.Reply(call("t", "a", nil)), faux.Say("done"))
			require.Zero(t, after)
			require.Equal(t, "dispatch failed", resultText(toolResults(r.msgs)["a"].Content))
		})
	}
}
func TestExecuteToolCachedSuccessAfterAbortBecomesAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := pipeline.NewRegistry()
	h.OnExecuteTool(func(context.Context, pipeline.ExecuteToolInput, pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		cancel()
		return textResult("cached"), nil
	})
	r := runTools(t, ctx, registry(t, okTool("t")), h, nil, faux.Reply(call("t", "a", nil)))
	require.Equal(t, "Operation aborted", resultText(toolResults(r.msgs)["a"].Content))
	require.True(t, toolResults(r.msgs)["a"].IsError)
}
func TestExecuteToolCannotDetachBodyFromAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := pipeline.NewRegistry()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	h.OnExecuteTool(func(_ context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		return next(context.Background(), in)
	})
	tool := &funcTool{name: "t", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return textResult("late"), nil
	}}
	done := make(chan toolRun, 1)
	go func() { done <- runTools(t, ctx, registry(t, tool), h, nil, faux.Reply(call("t", "a", nil))) }()
	<-started
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("body escaped abort")
	}
	r := <-done
	require.Equal(t, "Operation aborted", resultText(toolResults(r.msgs)["a"].Content))
}
func TestExecuteToolNextAfterAbortSkipsBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := pipeline.NewRegistry()
	var bodies int
	h.OnExecuteTool(func(_ context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		cancel()
		return next(context.Background(), in)
	})
	tool := okTool("t")
	tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies++
		return textResult("bad"), nil
	}
	r := runTools(t, ctx, registry(t, tool), h, nil, faux.Reply(call("t", "a", nil)))
	require.Zero(t, bodies)
	require.Equal(t, "Tool call aborted before dispatch", resultText(toolResults(r.msgs)["a"].Content))
}
func TestCancelDuringBeforeToolTakesEffectAfterHandlerReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var bodies, hooks atomic.Int32
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		hooks.Add(1)
		close(entered)
		<-release
		return nil, nil
	})
	tool := okTool("t")
	tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies.Add(1)
		return textResult("body"), nil
	}
	done := make(chan toolRun, 1)
	go func() {
		done <- runTools(t, ctx, registry(t, tool), h, nil, faux.Reply(call("t", "a", nil), call("t", "b", nil)))
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Error("settled during control hook")
	default:
	}
	require.Equal(t, int32(1), hooks.Load())
	close(release)
	r := <-done
	require.Zero(t, bodies.Load())
	require.Len(t, toolResults(r.msgs), 2)
	require.Equal(t, "Tool call aborted before dispatch", resultText(toolResults(r.msgs)["a"].Content))
}
func TestAbortDuringBeforeToolCallSkipsExecute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var body, dispatch int
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		cancel()
		return nil, nil
	})
	h.OnExecuteTool(func(context.Context, pipeline.ExecuteToolInput, pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		dispatch++
		return textResult("bad"), nil
	})
	tool := okTool("t")
	tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		body++
		return textResult("bad"), nil
	}
	r := runTools(t, ctx, registry(t, tool), h, nil, faux.Reply(call("t", "a", nil)))
	require.Zero(t, dispatch)
	require.Zero(t, body)
	require.Equal(t, "Tool call aborted before dispatch", resultText(toolResults(r.msgs)["a"].Content))
}
func TestCancelDuringAfterToolTakesEffectAfterHandlerReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	post := make(chan struct{})
	release := make(chan struct{})
	secondStarted := make(chan struct{})
	secondDone := make(chan struct{})
	h := pipeline.NewRegistry()
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		if in.Call.ID == "a" {
			close(post)
			<-release
			return &pipeline.AfterToolCallResult{Content: []protocol.UserBlock{protocol.Text{Text: "late post"}}, IsError: ptr(false), AddedContext: []protocol.UserMessage{user("late context").(protocol.UserMessage)}}, nil
		}
		return nil, nil
	})
	tool := safeTool{funcTool: &funcTool{name: "t", run: func(ctx context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if tc.CallID == "a" {
			<-secondStarted
		} else {
			close(secondStarted)
			<-ctx.Done()
			close(secondDone)
		}
		return textResult("ok"), nil
	}}}
	done := make(chan toolRun, 1)
	go func() {
		done <- runTools(t, ctx, registry(t, tool), h, nil, faux.Reply(call("t", "a", nil), call("t", "b", nil)))
	}()
	<-post
	cancel()
	<-secondDone
	select {
	case <-done:
		t.Error("settled before post returned")
	default:
	}
	close(release)
	r := <-done
	require.Equal(t, "Operation aborted", resultText(toolResults(r.msgs)["a"].Content))
	for _, m := range r.msgs {
		require.NotEqual(t, "late context", messageTextForTest(m))
	}
}
func messageTextForTest(m protocol.Message) string {
	if u, ok := m.(protocol.UserMessage); ok {
		return resultText(u.Content)
	}
	return ""
}
func TestLateToolSuccessAfterAbortBecomesAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		cancel()
		return textResult("late success"), nil
	}}
	r := runTools(t, ctx, registry(t, tool), nil, nil, faux.Reply(call("t", "a", nil)))
	require.Equal(t, "Operation aborted", resultText(toolResults(r.msgs)["a"].Content))
	require.True(t, toolResults(r.msgs)["a"].IsError)
}
func TestCallIsRecordedBeforeBeforeTool(t *testing.T) {
	log := &sessions.MemoryLog{}
	p, m := newFaux(t)
	p.Set(faux.Reply(call("echo", "a", map[string]any{"text": "hi"})), faux.Say("done"))
	h := pipeline.NewRegistry()
	var found bool
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		entries := log.Entries()
		for _, e := range entries {
			if c, ok := e.(sessions.ToolCall); ok && c.CallID == in.Call.ID {
				require.IsType(t, protocol.AssistantMessage{}, entries[c.AssistantEntry].(sessions.MessageEntry).Message)
				found = true
			}
		}
		return &pipeline.BeforeToolCallResult{Block: true}, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = h; c.NewContext = func() sessions.Writer { return log } })
	defer func() { require.NoError(t, a.Dispose()) }()
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	require.True(t, found)
}
func TestToolCallIdReusedFromEarlierTurnIsRejected(t *testing.T) {
	var bodies int
	tool := okTool("t")
	tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies++
		return textResult("ok"), nil
	}
	r := runTools(t, context.Background(), registry(t, tool), nil, nil, faux.Reply(call("t", "same", nil)), faux.Reply(call("t", "same", nil)), faux.Say("done"))
	require.Equal(t, 1, bodies)
	require.True(t, toolResults(r.msgs)["same"].IsError)
	require.Contains(t, resultText(toolResults(r.msgs)["same"].Content), "already used in this session")
}

func TestRegistryChangeDuringTurnDoesNotChangeExecutableTools(t *testing.T) {
	var count atomic.Int32
	all := make(chan struct{})
	tool := safeTool{funcTool: &funcTool{name: "t", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if count.Add(1) == 2 {
			close(all)
		}
		err := waitOrFail(ctx, all, "snapshot siblings")
		return textResult("snapshot"), err
	}}}
	reg := registry(t, tool)
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if in.Call.ID == "a" {
			reg.Unregister("t")
		}
		return nil, nil
	})
	r := runTools(t, context.Background(), reg, h, nil, faux.Reply(call("t", "a", nil), call("t", "b", nil)), faux.Say("done"))
	require.Equal(t, int32(2), count.Load())
	for _, res := range toolResults(r.msgs) {
		require.False(t, res.IsError)
		require.Equal(t, "snapshot", resultText(res.Content))
	}
}
func TestRepairNeverRunsToolAgain(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil), call("t", "b", nil)))
	var bodies int
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies++
		return textResult("body"), nil
	}}
	disk := errors.New("first result write failed")
	failed := false
	log := &commitGate{err: disk, failWhen: func(entries []sessions.Entry) bool {
		for _, e := range entries {
			if m, ok := e.(sessions.MessageEntry); ok {
				if _, ok := m.Message.(protocol.ToolResultMessage); ok && !failed {
					failed = true
					return true
				}
			}
		}
		return false
	}}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tool)
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	require.ErrorIs(t, a.Prompt(context.Background(), user("go")), disk)
	require.Equal(t, 1, bodies)
	res := toolResults(log.Messages())
	require.Len(t, res, 2)
	require.Equal(t, "outcome unknown", resultText(res["a"].Content))
	require.Equal(t, "not started", resultText(res["b"].Content))
}
func TestRepairUsesRecordedCallNotBodyInvocation(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil), call("t", "b", nil), call("t", "c", nil)))
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var third atomic.Int32
	tool := safeTool{funcTool: &funcTool{name: "t", run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if tc.CallID == "c" {
			third.Add(1)
		}
		started <- struct{}{}
		<-release
		return textResult("body"), nil
	}}}
	failed := false
	disk := errors.New("result commit failed")
	log := &commitGate{err: disk, failWhen: func(entries []sessions.Entry) bool {
		for _, e := range entries {
			if m, ok := e.(sessions.MessageEntry); ok {
				if _, ok := m.Message.(protocol.ToolResultMessage); ok && !failed {
					failed = true
					return true
				}
			}
		}
		return false
	}}
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if in.Call.ID == "c" {
			<-started
			<-started
			var recorded bool
			for _, e := range log.Entries() {
				if c, ok := e.(sessions.ToolCall); ok && c.CallID == "c" {
					recorded = true
				}
			}
			require.True(t, recorded)
			close(release)
			return nil, errors.New("pre-control failed")
		}
		return nil, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tool)
		c.Pipeline = h
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	require.ErrorIs(t, a.Prompt(context.Background(), user("go")), disk)
	require.Zero(t, third.Load())
	res := toolResults(log.Messages())
	require.Len(t, res, 3)
	for _, id := range []string{"a", "b", "c"} {
		require.Equal(t, "outcome unknown", resultText(res[id].Content))
	}
}
func TestRepairWriteFailureKeepsOriginalCause(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil)))
	original := errors.New("original writer failure")
	repair := errors.New("repair writer failure")
	calls := 0
	log := &multiFailureLog{first: original, second: repair, count: &calls}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, okTool("t"))
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	err := a.Prompt(context.Background(), user("go"))
	require.ErrorIs(t, err, original)
	require.ErrorIs(t, err, repair)
	require.Contains(t, err.Error(), "original writer failure\nagent: repair tool results: repair writer failure")
}

type multiFailureLog struct {
	sessions.MemoryLog
	first, second error
	count         *int
}

func (l *multiFailureLog) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	for _, e := range entries {
		if m, ok := e.(sessions.MessageEntry); ok {
			if _, ok := m.Message.(protocol.ToolResultMessage); ok {
				*l.count++
				if *l.count == 1 {
					return sessions.CommitRef{}, l.first
				}
				return sessions.CommitRef{}, l.second
			}
		}
	}
	return l.MemoryLog.Append(entries...)
}

func TestAbortWhileExclusiveCallWaitsSkipsDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	waiting := make(chan struct{})
	release := make(chan struct{})
	var dispatch atomic.Int32
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if in.Call.ID == "b" {
			<-started
			close(waiting)
		}
		return nil, nil
	})
	h.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		if in.Call.ID == "b" {
			dispatch.Add(1)
		}
		return next(ctx, in)
	})
	first := safeTool{funcTool: &funcTool{name: "safe", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		close(started)
		<-release
		return textResult("late"), nil
	}}}
	done := make(chan toolRun, 1)
	go func() {
		done <- runTools(t, ctx, registry(t, first, okTool("exclusive")), h, nil, faux.Reply(call("safe", "a", nil), call("exclusive", "b", nil)))
	}()
	<-waiting
	cancel()
	close(release)
	r := <-done
	require.Zero(t, dispatch.Load())
	require.Equal(t, "Tool call aborted before dispatch", resultText(toolResults(r.msgs)["b"].Content))
}

func TestExplicitToolCancelWinsOverBlockAndTerminate(t *testing.T) {
	h := pipeline.NewRegistry()
	var bodies, after int
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		bodies++
		return textResult("body"), nil
	}}
	h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		return &pipeline.BeforeToolCallResult{Cancel: true, Block: true, Reason: "denied", Terminate: true}, nil
	})
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		after++
		require.True(t, in.IsError)
		require.Equal(t, "Tool call aborted before dispatch", resultText(in.Result.Content))
		require.Nil(t, in.Result.Terminate)
		return nil, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), h, nil, faux.Reply(call("t", "a", nil)), faux.Say("done"))
	require.Zero(t, bodies)
	require.Equal(t, 1, after)
	require.Equal(t, 2, r.p.Calls())
	require.Equal(t, "Tool call aborted before dispatch", resultText(toolResults(r.msgs)["a"].Content))
}

func TestToolHookCallSignaturesCopied(t *testing.T) {
	h := pipeline.NewRegistry()
	h.OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		out, err := next(ctx, in)
		if len(out.Message.Content) > 0 {
			if c, ok := out.Message.Content[0].(protocol.ToolCall); ok {
				sig, ns := "signed", "namespace"
				c.ThoughtSignature = &sig
				c.Namespace = &ns
				out.Message.Content[0] = c
			}
		}
		return out, err
	})
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		*in.Call.ThoughtSignature = "changed"
		*in.Call.Namespace = "changed"
		return nil, nil
	})
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		require.Equal(t, "signed", *in.Call.ThoughtSignature)
		require.Equal(t, "namespace", *in.Call.Namespace)
		return nil, nil
	})
	runTools(t, context.Background(), registry(t, okTool("t")), h, nil, faux.Reply(call("t", "a", nil)), faux.Say("done"))
}
func TestPersistentWriterPanicIsContainedDuringRepair(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil)))
	log := &persistentToolResultPanicLog{}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, okTool("t"))
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	settled := 0
	a.Subscribe(func(e protocol.Event) error {
		if _, ok := e.(*protocol.AgentSettled); ok {
			settled++
		}
		return nil
	})
	var err error
	require.NotPanics(t, func() { err = a.Prompt(context.Background(), user("go")) })
	require.Equal(t, "panic: original writer panic\npanic: repair writer panic", err.Error())
	require.Equal(t, 1, settled)
}

type persistentToolResultPanicLog struct {
	sessions.MemoryLog
	failures int
}

func (l *persistentToolResultPanicLog) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	for _, e := range entries {
		if m, ok := e.(sessions.MessageEntry); ok {
			if _, ok := m.Message.(protocol.ToolResultMessage); ok {
				l.failures++
				if l.failures == 1 {
					panic("original writer panic")
				}
				panic("repair writer panic")
			}
		}
	}
	return l.MemoryLog.Append(entries...)
}

func TestInflightNextBodyCannotOutliveRun(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached result", true: "handler panic"}[panics], func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(call("t", "a", nil)))
			entered, release, returned, handlerRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			h := pipeline.NewRegistry()
			h.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
				go func() { _, _ = next(ctx, in); close(returned) }()
				<-entered
				<-handlerRelease
				if panics {
					panic("handler panic")
				}
				return textResult("cached"), nil
			})
			tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
				close(entered)
				<-release
				return textResult("body"), nil
			}}
			a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool); c.Pipeline = h })
			defer func() { require.NoError(t, a.Dispose()) }()
			done := make(chan error, 1)
			go func() { done <- a.Prompt(context.Background(), user("go")) }()
			<-entered
			a.Abort()
			close(handlerRelease)
			settled := false
			select {
			case <-done:
				settled = true
				t.Error("aborted run settled while an invoked body was still running")
			case <-time.After(20 * time.Millisecond):
			}
			if !settled {
				if a.State().Status != agent.Running {
					t.Error("agent did not stay busy during drain")
				}
				if err := a.Prompt(context.Background(), user("second")); !errors.Is(err, agent.ErrBusy) {
					t.Errorf("prompt during drain returned %v", err)
				}
			}
			close(release)
			<-returned
			if !settled {
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Error("run did not settle after body return")
				}
			}
		})
	}
}

func TestAsyncNextBodyPanicBecomesErrorAndRunsAfterTool(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil)))
	entered, release := make(chan struct{}), make(chan struct{})
	h := pipeline.NewRegistry()
	h.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		result := make(chan protocol.ToolExecutionResult, 1)
		failure := make(chan error, 1)
		go func() { r, err := next(ctx, in); result <- r; failure <- err }()
		r := <-result
		return r, <-failure
	})
	var after int
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		after++
		require.True(t, in.IsError)
		require.Equal(t, "body panic", resultText(in.Result.Content))
		return nil, nil
	})
	tool := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		close(entered)
		<-release
		panic("body panic")
	}}
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool); c.Pipeline = h })
	defer func() { require.NoError(t, a.Dispose()) }()
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-entered
	a.Abort()
	close(release)
	require.NoError(t, <-done)
	require.Equal(t, 1, after)
	require.Equal(t, "body panic", resultText(toolResults(a.State().Messages)["a"].Content))
	require.Equal(t, 1, p.Calls())
}

func TestRunFailureReturnsOriginalHandlerError(t *testing.T) {
	for _, point := range []string{"admission", "prepare", "complete"} {
		t.Run(point, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Say("done"))
			cause := errors.New("handler sentinel")
			h := pipeline.NewRegistry()
			switch point {
			case "admission":
				h.OnAdmitStep(func(context.Context, pipeline.AdmitInput, pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
					return pipeline.AdmitDecision{}, cause
				})
			case "prepare":
				h.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
					return nil, cause
				})
			case "complete":
				h.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) { return pipeline.Proceed, cause })
			}
			a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = h })
			defer func() { require.NoError(t, a.Dispose()) }()
			err := a.Prompt(context.Background(), user("go"))
			require.True(t, err == cause, "original handler error must retain identity; got %T", err)
		})
	}
}

func TestExecuteToolFailureCancelsAndJoinsInflightNextBody(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler error", true: "handler panic"}[panics], func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(call("t", "a", nil)), faux.Say("done"))
			entered, returned := make(chan struct{}), make(chan struct{})
			h := pipeline.NewRegistry()
			var after int
			h.OnExecuteTool(func(_ context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
				go func() { _, _ = next(context.Background(), in) }()
				<-entered
				if panics {
					panic("dispatch failed")
				}
				return protocol.ToolExecutionResult{}, errors.New("dispatch failed")
			})
			h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
				after++
				return nil, nil
			})
			tool := &funcTool{name: "t", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
				close(entered)
				<-ctx.Done()
				close(returned)
				return textResult("late"), nil
			}}
			a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool); c.Pipeline = h })
			defer func() { require.NoError(t, a.Dispose()) }()
			a.Subscribe(func(e protocol.Event) error {
				if _, ok := e.(*protocol.ToolExecutionEnd); ok {
					select {
					case <-returned:
					default:
						t.Error("tool result published before in-flight body returned")
					}
				}
				return nil
			})
			done := make(chan error, 1)
			go func() { done <- a.Prompt(context.Background(), user("go")) }()
			stalled := false
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				stalled = true
				t.Error("handler failure did not cancel accepted body")
				a.Abort()
				require.NoError(t, <-done)
			}
			<-returned
			require.Zero(t, after)
			require.Equal(t, "dispatch failed", resultText(toolResults(a.State().Messages)["a"].Content))
			if !stalled {
				require.Equal(t, 2, p.Calls())
				require.Equal(t, protocol.StopStop, lastAssistant(t, a.State().Messages).StopReason)
			}
		})
	}
}
