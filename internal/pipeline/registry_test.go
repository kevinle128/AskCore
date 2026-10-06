package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func userMsg(s string) protocol.Message {
	return protocol.UserMessage{Content: text(s)}
}

func ptrTo[T any](v T) *T { return &v }

func TestNilRegistryGivesDefaults(t *testing.T) {
	var r *Registry
	ctx := context.Background()

	upd, err := r.PrepareRequest(ctx, Request{})
	require.NoError(t, err)
	assert.Nil(t, upd)
	before, err := r.BeforeTool(ctx, ToolCallInfo{})
	require.NoError(t, err)
	assert.Nil(t, before)
	after, err := r.AfterTool(ctx, ToolResultInfo{})
	require.NoError(t, err)
	assert.Nil(t, after)
	d, err := r.CompleteStep(ctx, Turn{})
	require.NoError(t, err)
	assert.Equal(t, Proceed, d)
	d, err = r.StopTurn(ctx, StopInput{})
	require.NoError(t, err)
	assert.Equal(t, Proceed, d)
	stream := &ModelOutcome{Message: protocol.AssistantMessage{StopReason: protocol.StopStop}}
	got, err := r.ExecuteModel(ctx, ModelCall{}, func(context.Context, ModelCall) (*ModelOutcome, error) { return stream, nil })
	require.NoError(t, err)
	assert.Same(t, stream, got)
	admit, err := r.AdmitStep(ctx, AdmitInput{Messages: []protocol.Message{userMsg("a")}})
	require.NoError(t, err)
	assert.Equal(t, AdmitDecision{Messages: []protocol.Message{userMsg("a")}}, admit)
	rec, err := r.RecoverModel(ctx, RecoverInput{Failure: errHandler})
	require.NoError(t, err)
	assert.Equal(t, RecoverAction{}, rec)
	res, err := r.ExecuteTool(ctx, ExecuteToolInput{}, func(context.Context, ExecuteToolInput) (protocol.ToolExecutionResult, error) {
		return toolResult("t"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, toolResult("t"), res)
}

func TestAdmitStepAndRecoverModelDefaultsPassThrough(t *testing.T) {
	r := NewRegistry()
	msgs := []protocol.Message{userMsg("a")}
	admit, err := r.AdmitStep(context.Background(), AdmitInput{Messages: msgs})
	require.NoError(t, err)
	assert.Equal(t, AdmitDecision{Messages: msgs}, admit, "admission enters with the reserved messages")
	recover, err := r.RecoverModel(context.Background(), RecoverInput{Failure: errHandler})
	require.NoError(t, err)
	assert.Equal(t, RecoverAction{}, recover, "recovery stops with the original failure")
}

func TestPrepareRequestInnerHandlerSeesOuterModelSwitch(t *testing.T) {
	r := NewRegistry()
	r.OnPrepareRequest(func(ctx context.Context, req Request, next Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		req.Model = providers.Model{ID: "switched"}
		upd, err := next(ctx, req)
		if upd == nil {
			upd = &RequestUpdate{}
		}
		if upd.Model == nil {
			upd.Model = &req.Model
		}
		return upd, err
	})
	var seen string
	r.OnPrepareRequest(func(_ context.Context, req Request, _ Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		seen = req.Model.ID
		return nil, nil
	})
	got, err := r.PrepareRequest(context.Background(), Request{Model: providers.Model{ID: "orig"}})
	require.NoError(t, err)
	assert.Equal(t, "switched", seen)
	assert.Equal(t, "switched", got.Model.ID)
}

func TestAfterToolCallResultApply(t *testing.T) {
	base := protocol.ToolExecutionResult{Content: text("orig"), StructuredContent: json.RawMessage(`{"s":0}`), Details: json.RawMessage(`{"d":0}`)}

	tests := []struct {
		name    string
		r       *AfterToolCallResult
		want    protocol.ToolExecutionResult
		wantErr bool
	}{
		{"nil receiver", nil, base, false},
		{"empty override", &AfterToolCallResult{}, base, false},
		{"empty non-nil content replaces", &AfterToolCallResult{Content: []protocol.UserBlock{}},
			protocol.ToolExecutionResult{Content: []protocol.UserBlock{}, Details: base.Details}, false},
		{"content drops structured content", &AfterToolCallResult{Content: text("new")},
			protocol.ToolExecutionResult{Content: text("new"), Details: base.Details}, false},
		{"content with structured content keeps both", &AfterToolCallResult{Content: text("new"), StructuredContent: json.RawMessage(`{"s":1}`)},
			protocol.ToolExecutionResult{Content: text("new"), StructuredContent: json.RawMessage(`{"s":1}`), Details: base.Details}, false},
		{"error flag", &AfterToolCallResult{IsError: ptrTo(true)}, base, true},
		{"terminate", &AfterToolCallResult{Terminate: ptrTo(true)},
			protocol.ToolExecutionResult{Content: base.Content, StructuredContent: base.StructuredContent, Details: base.Details, Terminate: ptrTo(true)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, isErr := tt.r.Apply(base, false)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantErr, isErr)
		})
	}
}

// The rules below are the waterfall rules of every around point: handlers wrap
// in registration order, the outermost has the final word, a handler without
// next short-circuits the inner ones, and the terminal honors what was passed on.

func TestPrepareRequestPassedOnInputReachesTerminal(t *testing.T) {
	r := NewRegistry()
	r.OnPrepareRequest(func(ctx context.Context, req Request, next Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		req.Model = providers.Model{ID: "outer"}
		req.ThinkingLevel = "high"
		req.Context.Messages = []protocol.Message{userMsg("outer")}
		return next(ctx, req)
	})
	r.OnPrepareRequest(func(ctx context.Context, req Request, next Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		return next(ctx, req)
	})
	got, err := r.PrepareRequest(context.Background(), Request{Model: providers.Model{ID: "orig"}, ThinkingLevel: protocol.ThinkingOff})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "outer", got.Model.ID)
	assert.Equal(t, protocol.ThinkingLevel("high"), got.ThinkingLevel)
	assert.Equal(t, []protocol.Message{userMsg("outer")}, got.Context.Messages)
}

func TestPrepareRequestOuterOverrideWinsAndShortCircuitSkipsInner(t *testing.T) {
	r := NewRegistry()
	innerRan := false
	r.OnPrepareRequest(func(ctx context.Context, req Request, next Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		upd, err := next(ctx, req)
		if err != nil {
			return nil, err
		}
		upd.ThinkingLevel = "low"
		return upd, nil
	})
	r.OnPrepareRequest(func(context.Context, Request, Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		innerRan = true
		return &RequestUpdate{ThinkingLevel: "high", Model: &providers.Model{ID: "inner"}}, nil
	})
	got, err := r.PrepareRequest(context.Background(), Request{})
	require.NoError(t, err)
	assert.Equal(t, protocol.ThinkingLevel("low"), got.ThinkingLevel, "the outer handler has the final word")
	assert.Equal(t, "inner", got.Model.ID, "fields it leaves alone keep the inner answer")
	assert.True(t, innerRan)

	innerRan = false
	r = NewRegistry()
	r.OnPrepareRequest(func(context.Context, Request, Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		return &RequestUpdate{ThinkingLevel: "medium"}, nil
	})
	r.OnPrepareRequest(func(context.Context, Request, Next[Request, *RequestUpdate]) (*RequestUpdate, error) {
		innerRan = true
		return nil, nil
	})
	got, err = r.PrepareRequest(context.Background(), Request{})
	require.NoError(t, err)
	assert.Equal(t, protocol.ThinkingLevel("medium"), got.ThinkingLevel)
	assert.False(t, innerRan, "a handler without next short-circuits")
}

func TestBeforeToolPassedOnArgsGiveNoAnswerAndOuterKeepsInnerBlock(t *testing.T) {
	orig := json.RawMessage(`{"a":1}`)
	r := NewRegistry()
	r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		info.Args = json.RawMessage(`{"a":2}`)
		return next(ctx, info)
	})
	r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		return next(ctx, info)
	})
	got, err := r.BeforeTool(context.Background(), ToolCallInfo{Args: orig})
	require.NoError(t, err)
	assert.Nil(t, got, "arguments passed to next never become an answer: the call runs with the validated ones")

	r = NewRegistry()
	r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		return next(ctx, info)
	})
	r.OnBeforeTool(func(context.Context, ToolCallInfo, Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		return &BeforeToolCallResult{Block: true, Reason: "inner"}, nil
	})
	got, err = r.BeforeTool(context.Background(), ToolCallInfo{Args: orig})
	require.NoError(t, err)
	assert.Equal(t, &BeforeToolCallResult{Block: true, Reason: "inner"}, got, "an outer handler that calls next keeps the inner Block")

	r = NewRegistry()
	r.OnBeforeTool(func(context.Context, ToolCallInfo, Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		return &BeforeToolCallResult{Reason: "outer"}, nil
	})
	innerRan := false
	r.OnBeforeTool(func(context.Context, ToolCallInfo, Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
		innerRan = true
		return &BeforeToolCallResult{Block: true}, nil
	})
	got, err = r.BeforeTool(context.Background(), ToolCallInfo{Args: orig})
	require.NoError(t, err)
	assert.False(t, got.Block, "the outer answer wins when it does not call next")
	assert.False(t, innerRan, "and the inner handler never runs")
}

func TestAfterToolPassedOnResultReachesTerminalAndOuterOverrideWins(t *testing.T) {
	base := protocol.ToolExecutionResult{Content: text("orig"), StructuredContent: json.RawMessage(`{"s":0}`)}
	r := NewRegistry()
	r.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		info.Result, info.IsError = (&AfterToolCallResult{Content: text("outer"), IsError: ptrTo(true)}).Apply(info.Result, info.IsError)
		return next(ctx, info)
	})
	r.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return next(ctx, info)
	})
	got, err := r.AfterTool(context.Background(), ToolResultInfo{Result: base})
	require.NoError(t, err)
	require.NotNil(t, got)
	res, isErr := got.Apply(base, false)
	assert.Equal(t, text("outer"), res.Content, "the passed-on result is not dropped")
	assert.Nil(t, res.StructuredContent)
	assert.True(t, isErr)

	r = NewRegistry()
	r.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		inner, err := next(ctx, info)
		if err != nil {
			return nil, err
		}
		inner.Content = text("outer wins")
		return inner, nil
	})
	r.OnAfterTool(func(context.Context, ToolResultInfo, Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return &AfterToolCallResult{Content: text("inner"), Details: json.RawMessage(`{"d":1}`)}, nil
	})
	got, err = r.AfterTool(context.Background(), ToolResultInfo{Result: base})
	require.NoError(t, err)
	assert.Equal(t, text("outer wins"), got.Content)
	assert.JSONEq(t, `{"d":1}`, string(got.Details), "inner fields the outer leaves alone stay")

	r = NewRegistry()
	r.OnAfterTool(func(context.Context, ToolResultInfo, Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return nil, nil
	})
	innerRan := false
	r.OnAfterTool(func(context.Context, ToolResultInfo, Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		innerRan = true
		return nil, nil
	})
	_, err = r.AfterTool(context.Background(), ToolResultInfo{Result: base})
	require.NoError(t, err)
	assert.False(t, innerRan, "a handler without next short-circuits")
}
