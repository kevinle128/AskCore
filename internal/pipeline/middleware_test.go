package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

var errHandler = errors.New("handler failed")

func text(s string) []protocol.UserBlock { return []protocol.UserBlock{protocol.Text{Text: s}} }

func toolResult(s string) protocol.ToolExecutionResult {
	return protocol.ToolExecutionResult{Content: text(s)}
}

// runTool dispatches ExecuteTool with a terminal that counts its runs.
func runTool(t *testing.T, r *Registry, runs *int) (protocol.ToolExecutionResult, error) {
	t.Helper()
	return r.ExecuteTool(context.Background(), ExecuteToolInput{}, func(context.Context, ExecuteToolInput) (protocol.ToolExecutionResult, error) {
		*runs++
		return toolResult("terminal"), nil
	})
}

func TestNextCalledTwiceRunsTerminalOnce(t *testing.T) {
	r := NewRegistry()
	var second error
	r.OnExecuteTool(func(ctx context.Context, in ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		first, err := next(ctx, in)
		require.NoError(t, err)
		_, second = next(ctx, in)
		return first, nil
	})
	var runs int
	got, err := runTool(t, r, &runs)
	require.NoError(t, err)
	assert.Equal(t, toolResult("terminal"), got)
	assert.Equal(t, 1, runs, "the terminal runs once")
	assert.ErrorIs(t, second, ErrNextReused)
}

func TestNextCalledAfterHandlerReturnsIsRejected(t *testing.T) {
	r := NewRegistry()
	var late Next[ExecuteToolInput, protocol.ToolExecutionResult]
	r.OnExecuteTool(func(_ context.Context, _ ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		late = next
		return toolResult("kept"), nil
	})
	var runs int
	got, err := runTool(t, r, &runs)
	require.NoError(t, err)
	assert.Equal(t, toolResult("kept"), got)
	require.NotNil(t, late)

	_, err = late(context.Background(), ExecuteToolInput{})
	assert.ErrorIs(t, err, ErrNextReused, "a retained next is dead once its handler returned")
	assert.Zero(t, runs, "the terminal never ran")
}

func TestZeroNextNeedsValidTerminalResult(t *testing.T) {
	call := ModelCall{Model: providers.Model{ID: "m"}}
	terminal := func(context.Context, ModelCall) (*ModelOutcome, error) {
		t.Fatal("the terminal must not run")
		return nil, nil
	}

	r := NewRegistry()
	r.OnExecuteModel(func(context.Context, ModelCall, Next[ModelCall, *ModelOutcome]) (*ModelOutcome, error) {
		return nil, nil
	})
	stream, err := r.ExecuteModel(context.Background(), call, terminal)
	assert.ErrorIs(t, err, ErrInvalidResult, "no stream and no next is not a result")
	assert.Nil(t, stream)

	want := &ModelOutcome{Message: protocol.AssistantMessage{StopReason: protocol.StopStop}}
	r = NewRegistry()
	r.OnExecuteModel(func(context.Context, ModelCall, Next[ModelCall, *ModelOutcome]) (*ModelOutcome, error) {
		return want, nil
	})
	stream, err = r.ExecuteModel(context.Background(), call, terminal)
	require.NoError(t, err)
	assert.Equal(t, want, stream, "a handler may answer with a settled outcome copy")

	// A zero result is valid at a point whose default is "no change".
	r = NewRegistry()
	r.OnBeforeTool(func(context.Context, ToolCallInfo, Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		return nil, nil
	})
	res, err := r.BeforeTool(context.Background(), ToolCallInfo{})
	require.NoError(t, err)
	assert.Nil(t, res)

	// A handler that calls next may return a zero value afterwards.
	r = NewRegistry()
	r.OnExecuteModel(func(ctx context.Context, in ModelCall, next Next[ModelCall, *ModelOutcome]) (*ModelOutcome, error) {
		_, _ = next(ctx, in)
		return nil, nil
	})
	_, err = r.ExecuteModel(context.Background(), call, func(context.Context, ModelCall) (*ModelOutcome, error) { return want, nil })
	assert.ErrorIs(t, err, ErrInvalidResult, "a handler cannot drop the stream that next returned")
}

func TestPanickingHandlerLeavesRetainedNextDead(t *testing.T) {
	r := NewRegistry()
	var retained Next[ExecuteToolInput, protocol.ToolExecutionResult]
	r.OnExecuteTool(func(_ context.Context, _ ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		retained = next
		panic("handler broke")
	})
	var runs int
	assert.PanicsWithValue(t, "handler broke", func() { _, _ = runTool(t, r, &runs) })
	require.NotNil(t, retained)
	_, err := retained(context.Background(), ExecuteToolInput{})
	assert.ErrorIs(t, err, ErrNextReused)
	assert.Zero(t, runs)
}

func TestOuterHandlerSeesDownstreamRejection(t *testing.T) {
	r := NewRegistry()
	var outerSaw *BeforeToolCallResult
	r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		info.Args = json.RawMessage(`{"added":"context"}`)
		res, err := next(ctx, info)
		outerSaw = res
		return res, err
	})
	reject := &BeforeToolCallResult{Block: true, Reason: "denied downstream"}
	var innerArgs json.RawMessage
	r.OnBeforeTool(func(_ context.Context, info ToolCallInfo, _ Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		innerArgs = info.Args
		return reject, nil
	})

	got, err := r.BeforeTool(context.Background(), ToolCallInfo{Args: json.RawMessage(`{}`)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"added":"context"}`, string(innerArgs), "the inner handler sees what the outer added")
	assert.Same(t, reject, outerSaw, "the outer handler gets the inner rejection from next")
	assert.Same(t, reject, got, "the outer handler returns it unchanged")
}

func TestRegistrationDuringChainDoesNotChangeChain(t *testing.T) {
	r := NewRegistry()
	var ran []string
	late := func(context.Context, ToolCallInfo, Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		ran = append(ran, "late")
		return nil, nil
	}
	r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		ran = append(ran, "first")
		r.OnBeforeTool(late)
		return next(ctx, info)
	})

	_, err := r.BeforeTool(context.Background(), ToolCallInfo{})
	require.NoError(t, err)
	assert.Equal(t, []string{"first"}, ran, "the chain in flight keeps its snapshot")

	ran = nil
	r = NewRegistry()
	r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		ran = append(ran, "first")
		return next(ctx, info)
	})
	r.OnBeforeTool(late)
	_, err = r.BeforeTool(context.Background(), ToolCallInfo{})
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "late"}, ran, "a later dispatch sees the new handler")
}

func TestAroundHandlersRunOuterToInnerAndUnwind(t *testing.T) {
	var trace []string
	around := func(name string) ExecuteToolHandler {
		return func(ctx context.Context, in ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
			trace = append(trace, name+" in")
			res, err := next(ctx, in)
			trace = append(trace, name+" out")
			return res, err
		}
	}
	r := NewRegistry()
	r.OnExecuteTool(around("A"))
	r.OnExecuteTool(around("B"))
	_, err := r.ExecuteTool(context.Background(), ExecuteToolInput{}, func(context.Context, ExecuteToolInput) (protocol.ToolExecutionResult, error) {
		trace = append(trace, "terminal")
		return toolResult("t"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"A in", "B in", "terminal", "B out", "A out"}, trace)

	trace = nil
	r = NewRegistry()
	r.OnExecuteTool(around("A"))
	r.OnExecuteTool(func(context.Context, ExecuteToolInput, Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		trace = append(trace, "B stops")
		return toolResult("b"), nil
	})
	var runs int
	got, err := runTool(t, r, &runs)
	require.NoError(t, err)
	assert.Equal(t, toolResult("b"), got)
	assert.Equal(t, []string{"A in", "B stops", "A out"}, trace, "a handler without next stops the chain")
	assert.Zero(t, runs, "the terminal does not run")
}

func TestHandlerErrorStopsChainAndUnwinds(t *testing.T) {
	r := NewRegistry()
	var outerErr error
	r.OnExecuteTool(func(ctx context.Context, in ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		res, err := next(ctx, in)
		outerErr = err
		return res, err
	})
	r.OnExecuteTool(func(context.Context, ExecuteToolInput, Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		return protocol.ToolExecutionResult{}, errHandler
	})
	var runs int
	_, err := runTool(t, r, &runs)
	assert.ErrorIs(t, err, errHandler)
	assert.ErrorIs(t, outerErr, errHandler)
	assert.Zero(t, runs)
}
