package agent_test

import (
	"AskCore/internal/agent"
	"AskCore/internal/sessions"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

type safeTool struct {
	*funcTool
	safe func(json.RawMessage) bool
}

func (s safeTool) ConcurrencySafe(args json.RawMessage) bool {
	if s.safe != nil {
		return s.safe(args)
	}
	return true
}

func TestUnknownToolRunsBothHooks(t *testing.T) {
	var before, after int
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		before++
		require.JSONEq(t, `{"x":"raw"}`, string(in.Args))
		return nil, nil
	})
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		after++
		require.True(t, in.IsError)
		require.Equal(t, "Tool absent not found", resultText(in.Result.Content))
		return nil, nil
	})
	r := runTools(t, context.Background(), nil, h, nil, faux.Reply(call("absent", "c1", map[string]any{"x": "raw"})), faux.Say("done"))
	require.Equal(t, 1, before)
	require.Equal(t, 1, after)
	require.True(t, toolResults(r.msgs)["c1"].IsError)
}
func TestActiveBodiesNeverExceedLimit(t *testing.T) {
	var active, peak atomic.Int32
	ready := make(chan struct{}, 12)
	release := make(chan struct{})
	tool := safeTool{funcTool: &funcTool{name: "safe", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		n := active.Add(1)
		for p := peak.Load(); n > p; p = peak.Load() {
			if peak.CompareAndSwap(p, n) {
				break
			}
		}
		ready <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		active.Add(-1)
		return textResult("ok"), nil
	}}}
	blocks := make([]faux.Block, 12)
	for i := range blocks {
		blocks[i] = call("safe", string(rune('a'+i)), nil)
	}
	done := make(chan toolRun, 1)
	go func() {
		done <- runTools(t, context.Background(), registry(t, tool), nil, nil, faux.Reply(blocks...), faux.Say("done"))
	}()
	for range 10 {
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("pool did not fill")
		}
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	<-done
	require.Equal(t, int32(10), peak.Load())
}
func TestAfterToolRunsForDeniedAndCancelledCalls(t *testing.T) {
	h := pipeline.NewRegistry()
	var after []string
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if in.Call.ID == "denied" {
			return &pipeline.BeforeToolCallResult{Block: true}, nil
		}
		return &pipeline.BeforeToolCallResult{Cancel: true}, nil
	})
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		after = append(after, in.Call.ID)
		return nil, nil
	})
	runTools(t, context.Background(), registry(t, okTool("t")), h, nil, faux.Reply(call("t", "denied", nil), call("t", "cancelled", nil)), faux.Say("done"))
	require.Equal(t, []string{"denied", "cancelled"}, after)
}

func TestSettledToolFailureWinsOverLateAbort(t *testing.T) {
	for _, stage := range []string{"deny", "before", "execute", "body", "after"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := pipeline.NewRegistry()
			tool := okTool("t")
			switch stage {
			case "deny":
				h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
					cancel()
					return &pipeline.BeforeToolCallResult{Block: true, Reason: "own failure"}, nil
				})
			case "before":
				h.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
					cancel()
					return nil, errors.New("own failure")
				})
			case "execute":
				h.OnExecuteTool(func(context.Context, pipeline.ExecuteToolInput, pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
					cancel()
					return protocol.ToolExecutionResult{}, errors.New("own failure")
				})
			case "body":
				tool.run = func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
					cancel()
					return protocol.ToolExecutionResult{}, errors.New("own failure")
				}
			case "after":
				h.OnAfterTool(func(context.Context, pipeline.ToolResultInfo, nextAfter) (*pipeline.AfterToolCallResult, error) {
					cancel()
					return nil, errors.New("own failure")
				})
			}
			r := runTools(t, ctx, registry(t, tool), h, nil, faux.Reply(call("t", "c", nil)))
			require.Equal(t, "own failure", resultText(toolResults(r.msgs)["c"].Content))
		})
	}
}

func TestConcurrencySafeFalseForTheseArgumentsRunsAlone(t *testing.T) {
	testModeBarrier(t, func(json.RawMessage) bool { return false })
}
func TestPanickingClassifierRunsAlone(t *testing.T) {
	testModeBarrier(t, func(json.RawMessage) bool { panic("classifier") })
}
func testModeBarrier(t *testing.T, classify func(json.RawMessage) bool) {
	var active, peak atomic.Int32
	f := &funcTool{name: "t", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		n := active.Add(1)
		if n > peak.Load() {
			peak.Store(n)
		}
		time.Sleep(time.Millisecond)
		active.Add(-1)
		return textResult("ok"), nil
	}}
	r := runTools(t, context.Background(), registry(t, safeTool{funcTool: f, safe: classify}), nil, nil, faux.Reply(call("t", "a", nil), call("t", "b", nil)), faux.Say("done"))
	require.Equal(t, int32(1), peak.Load())
	require.Len(t, toolResults(r.msgs), 2)
}
func TestBodiesResumeAfterExclusiveBarrier(t *testing.T) {
	var active atomic.Int32
	firstDone := make(chan struct{})
	exclusiveDone := make(chan struct{})
	both := make(chan struct{})
	var count atomic.Int32
	safe := safeTool{funcTool: &funcTool{name: "safe", run: func(ctx context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if tc.CallID == "first" {
			active.Add(1)
			time.Sleep(time.Millisecond)
			active.Add(-1)
			close(firstDone)
			return textResult("first"), nil
		}
		select {
		case <-exclusiveDone:
		default:
			t.Error("safe resumed before exclusive finished")
		}
		active.Add(1)
		if count.Add(1) == 2 {
			close(both)
		}
		err := waitOrFail(ctx, both, "safe refill")
		active.Add(-1)
		return textResult("safe"), err
	}}}
	exclusive := &funcTool{name: "exclusive", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		select {
		case <-firstDone:
		default:
			t.Error("exclusive started early")
		}
		require.Zero(t, active.Load())
		close(exclusiveDone)
		return textResult("exclusive"), nil
	}}
	r := runTools(t, context.Background(), registry(t, safe, exclusive), nil, nil, faux.Reply(call("safe", "first", nil), call("exclusive", "barrier", nil), call("safe", "a", nil), call("safe", "b", nil)), faux.Say("done"))
	require.Len(t, toolResults(r.msgs), 4)
	require.Equal(t, int32(2), count.Load())
}
func TestSafeCallsAllStartBeforeAnyFinishes(t *testing.T) {
	var count atomic.Int32
	all := make(chan struct{})
	tool := safeTool{funcTool: &funcTool{name: "safe", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if count.Add(1) == 3 {
			close(all)
		}
		err := waitOrFail(ctx, all, "three bodies")
		return textResult("ok"), err
	}}}
	r := runTools(t, context.Background(), registry(t, tool), nil, nil, faux.Reply(call("safe", "a", nil), call("safe", "b", nil), call("safe", "c", nil)), faux.Say("done"))
	for _, res := range toolResults(r.msgs) {
		require.False(t, res.IsError)
	}
	require.Equal(t, int32(3), count.Load())
}
func TestFreedSlotStartsNextBodyBeforeEarlierFinishes(t *testing.T) {
	started := make(chan string, 12)
	release := make(chan struct{})
	second := make(chan struct{})
	tool := safeTool{funcTool: &funcTool{name: "safe", run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		started <- tc.CallID
		if tc.CallID == "b" {
			<-second
		} else {
			<-release
		}
		return textResult("ok"), nil
	}}}
	p, m := newFaux(t)
	blocks := make([]faux.Block, 12)
	for i := range blocks {
		blocks[i] = call("safe", string(rune('a'+i)), nil)
	}
	p.Set(faux.Reply(blocks...), faux.Say("done"))
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })
	defer func() { require.NoError(t, a.Dispose()) }()
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	for range 10 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(second)
			close(release)
			t.Fatal("pool did not fill")
		}
	}
	close(second)
	select {
	case id := <-started:
		require.Equal(t, "k", id)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("free slot did not refill")
	}
	close(release)
	require.NoError(t, <-done)
}
func TestPreAndPostControlAndCommitsFollowSourceOrder(t *testing.T) {
	cDone := make(chan struct{})
	bDone := make(chan struct{})
	var before, after []string
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		before = append(before, in.Call.ID)
		return nil, nil
	})
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		after = append(after, in.Call.ID)
		return nil, nil
	})
	tool := safeTool{funcTool: &funcTool{name: "safe", run: func(ctx context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		switch tc.CallID {
		case "a":
			if err := waitOrFail(ctx, bDone, "b"); err != nil {
				return protocol.ToolExecutionResult{}, err
			}
		case "b":
			if err := waitOrFail(ctx, cDone, "c"); err != nil {
				return protocol.ToolExecutionResult{}, err
			}
			close(bDone)
		case "c":
			close(cDone)
		}
		return textResult(tc.CallID), nil
	}}}
	r := runTools(t, context.Background(), registry(t, tool), h, nil, faux.Reply(call("safe", "a", nil), call("safe", "b", nil), call("safe", "c", nil)), faux.Say("done"))
	require.Equal(t, []string{"a", "b", "c"}, before)
	require.Equal(t, before, after)
	var ends, results []string
	for _, e := range r.rec.events {
		switch e := e.(type) {
		case *protocol.ToolExecutionEnd:
			ends = append(ends, e.ToolCallID)
		case *protocol.MessageEnd:
			if r, ok := e.Message.(protocol.ToolResultMessage); ok {
				results = append(results, r.ToolCallID)
			}
		}
	}
	require.Equal(t, before, ends)
	require.Equal(t, before, results)
}
func TestSlowPostHookDelaysLaterCommitsAndStarts(t *testing.T) {
	firstReady := make(chan struct{})
	secondBodyDone := make(chan struct{})
	post := make(chan struct{})
	releasePost := make(chan struct{})
	third := make(chan struct{})
	h := pipeline.NewRegistry()
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if in.Call.ID == "b" {
			close(firstReady)
		}
		return nil, nil
	})
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		if in.Call.ID == "a" {
			close(post)
			<-releasePost
		}
		return nil, nil
	})
	tool := safeTool{funcTool: &funcTool{name: "safe", run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		switch tc.CallID {
		case "a":
			<-firstReady
		case "b":
			<-post
			close(secondBodyDone)
		case "c":
			close(third)
		}
		return textResult("ok"), nil
	}}}
	p, m := newFaux(t)
	p.Set(faux.Reply(call("safe", "a", nil), call("safe", "b", nil), call("safe", "c", nil)), faux.Say("done"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tool)
		c.Pipeline = h
		c.MaxParallelTools = 2
		c.NewContext = func() sessions.Writer { return log }
	})
	defer func() { require.NoError(t, a.Dispose()) }()
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-post
	select {
	case <-secondBodyDone:
	case <-time.After(time.Second):
		close(releasePost)
		t.Fatal("post hook blocked worker")
	}
	select {
	case <-third:
		t.Error("later body started during post hook")
	default:
	}
	require.Empty(t, toolResults(log.Messages()))
	close(releasePost)
	require.NoError(t, <-done)
	<-third
}
func TestProgressNeverBlocksWorker(t *testing.T) {
	h := pipeline.NewRegistry()
	started := make(chan struct{})
	workerDone := make(chan struct{})
	h.OnBeforeTool(func(_ context.Context, in pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		if in.Call.ID == "b" {
			<-started
			select {
			case <-workerDone:
			case <-time.After(time.Second):
				t.Error("worker updates blocked on control")
			}
		}
		return nil, nil
	})
	tool := safeTool{funcTool: &funcTool{name: "safe", run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		if tc.CallID == "a" {
			close(started)
			for i := range 1000 {
				tc.Update(textResult(fmt.Sprint(i)))
			}
			close(workerDone)
		}
		return textResult("ok"), nil
	}}}
	r := runTools(t, context.Background(), registry(t, tool), h, nil, faux.Reply(call("safe", "a", nil), call("safe", "b", nil)), faux.Say("done"))
	require.False(t, toolResults(r.msgs)["a"].IsError)
}
func TestBatchTerminatesOnlyWhenAllResultsAsk(t *testing.T) {
	h := pipeline.NewRegistry()
	h.OnAfterTool(func(_ context.Context, in pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		yes := in.Call.ID == "a"
		return &pipeline.AfterToolCallResult{Terminate: &yes}, nil
	})
	r := runTools(t, context.Background(), registry(t, okTool("t")), h, nil, faux.Reply(call("t", "a", nil), call("t", "b", nil)), faux.Say("continued"))
	require.Equal(t, 2, r.p.Calls())
	require.Equal(t, "continued", assistantText(lastAssistant(t, r.msgs)))
}
