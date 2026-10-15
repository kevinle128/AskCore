package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func addedContext(value string) []protocol.UserMessage {
	return []protocol.UserMessage{{Content: text(value)}}
}

func TestAfterToolAddedContextPassesThroughWithoutChangingResult(t *testing.T) {
	registry := NewRegistry()
	registry.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return next(ctx, info)
	})
	registry.OnAfterTool(func(context.Context, ToolResultInfo, Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return &AfterToolCallResult{AddedContext: addedContext("next turn")}, nil
	})
	base := toolResult("result")
	override, err := registry.AfterTool(context.Background(), ToolResultInfo{Result: base})
	require.NoError(t, err)
	require.Equal(t, addedContext("next turn"), override.AddedContext)
	result, isError := override.Apply(base, false)
	require.Equal(t, base, result, "added context does not replace tool content")
	require.False(t, isError)
}

func TestAfterToolAddedContextKeepsWaterfallOuterPrecedence(t *testing.T) {
	registry := NewRegistry()
	registry.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		inner, err := next(ctx, info)
		require.NoError(t, err)
		require.Equal(t, addedContext("inner"), inner.AddedContext)
		return &AfterToolCallResult{AddedContext: addedContext("outer")}, nil
	})
	registry.OnAfterTool(func(context.Context, ToolResultInfo, Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return &AfterToolCallResult{AddedContext: addedContext("inner")}, nil
	})
	override, err := registry.AfterTool(context.Background(), ToolResultInfo{})
	require.NoError(t, err)
	require.Equal(t, addedContext("outer"), override.AddedContext, "the outer returned result replaces the inner result")
}

func TestAfterToolDispatchCopiesReturnedContextAtEveryBoundary(t *testing.T) {
	innerResult := &AfterToolCallResult{
		Content: text("content"), Details: json.RawMessage(`{"d":1}`), StructuredContent: json.RawMessage(`{"s":1}`),
		IsError: ptrTo(false), Terminate: ptrTo(false), Usage: &protocol.Usage{Input: 1, CacheWrite1h: ptrTo(int64(2))},
		AddedContext: []protocol.UserMessage{{Content: []protocol.UserBlock{protocol.Text{Text: "context", TextSignature: ptrTo("signature")}}}},
	}
	registry := NewRegistry()
	var outerResult *AfterToolCallResult
	registry.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		result, err := next(ctx, info)
		require.NoError(t, err)
		contextText := result.AddedContext[0].Content[0].(protocol.Text)
		*contextText.TextSignature = "outer signature"
		contextText.Text = "outer"
		result.AddedContext[0].Content[0] = contextText
		result.Content[0] = protocol.Text{Text: "outer content"}
		result.Details[5] = '2'
		result.StructuredContent[5] = '2'
		*result.IsError = true
		*result.Terminate = true
		result.Usage.Input = 9
		*result.Usage.CacheWrite1h = 9
		outerResult = result
		return result, nil
	})
	registry.OnAfterTool(func(context.Context, ToolResultInfo, Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return innerResult, nil
	})
	returned, err := registry.AfterTool(context.Background(), ToolResultInfo{})
	require.NoError(t, err)
	require.Equal(t, "outer", returned.AddedContext[0].Content[0].(protocol.Text).Text)
	require.Equal(t, text("content"), innerResult.Content)
	require.Equal(t, "context", innerResult.AddedContext[0].Content[0].(protocol.Text).Text)
	require.JSONEq(t, `{"d":1}`, string(innerResult.Details))
	require.JSONEq(t, `{"s":1}`, string(innerResult.StructuredContent))
	require.False(t, *innerResult.IsError)
	require.False(t, *innerResult.Terminate)
	require.EqualValues(t, 1, innerResult.Usage.Input)
	require.EqualValues(t, 2, *innerResult.Usage.CacheWrite1h)
	returned.AddedContext[0].Content[0] = protocol.Text{Text: "caller"}
	returned.Content[0] = protocol.Text{Text: "caller content"}
	require.Equal(t, "outer", outerResult.AddedContext[0].Content[0].(protocol.Text).Text, "the caller cannot edit the outer handler's result")
	require.Equal(t, text("outer content"), outerResult.Content)
	require.Equal(t, "signature", *innerResult.AddedContext[0].Content[0].(protocol.Text).TextSignature)
}

func TestAfterToolTerminalOverrideIsCopied(t *testing.T) {
	registry := NewRegistry()
	passed := toolResult("passed on")
	registry.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		info.Result = passed
		return next(ctx, info)
	})
	returned, err := registry.AfterTool(context.Background(), ToolResultInfo{Result: toolResult("original")})
	require.NoError(t, err)
	require.Equal(t, text("passed on"), returned.Content)
	returned.Content[0] = protocol.Text{Text: "caller"}
	require.Equal(t, text("passed on"), passed.Content)
}

func TestAfterToolCloneKeepsEmptyOverridesAndNilContext(t *testing.T) {
	var absent *AfterToolCallResult
	require.Nil(t, absent.Clone())
	empty := &AfterToolCallResult{Content: []protocol.UserBlock{}, AddedContext: []protocol.UserMessage{}}
	cloned := empty.Clone()
	require.Equal(t, empty, cloned)
	require.NotNil(t, cloned.Content, "empty content is a deliberate override")
	require.NotNil(t, cloned.AddedContext)
	require.Nil(t, (&AfterToolCallResult{}).Clone().AddedContext)
}

func TestAfterToolPointerBlocksArePrivateAtHandlerAndCallerBoundaries(t *testing.T) {
	signature := "signed"
	block := &protocol.Text{Text: "inner", TextSignature: &signature}
	image := &protocol.Image{Data: "YQ==", MimeType: "image/png"}
	source := &AfterToolCallResult{Content: []protocol.UserBlock{block, image}, AddedContext: []protocol.UserMessage{{Content: []protocol.UserBlock{block, image}}}}
	registry := NewRegistry()
	var retained *AfterToolCallResult
	registry.OnAfterTool(func(ctx context.Context, info ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		result, err := next(ctx, info)
		require.NoError(t, err)
		result.AddedContext[0].Content[0].(*protocol.Text).Text = "outer"
		*result.Content[0].(*protocol.Text).TextSignature = "outer signature"
		result.AddedContext[0].Content[1].(*protocol.Image).Data = "Yg=="
		result.Content[1].(*protocol.Image).MimeType = "image/jpeg"
		retained = result
		return result, nil
	})
	registry.OnAfterTool(func(context.Context, ToolResultInfo, Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
		return source, nil
	})
	result, err := registry.AfterTool(context.Background(), ToolResultInfo{})
	require.NoError(t, err)
	require.Equal(t, "inner", block.Text, "the outer handler cannot edit an inner pointer block")
	require.Equal(t, "signed", signature)
	require.Equal(t, "YQ==", image.Data)
	require.Equal(t, "image/png", image.MimeType)
	result.Content[0].(*protocol.Text).Text = "caller"
	result.AddedContext[0].Content[1].(*protocol.Image).Data = "caller image"
	require.Equal(t, "inner", retained.Content[0].(*protocol.Text).Text, "the caller cannot edit the outer handler's pointer block")
	require.Equal(t, "Yg==", retained.AddedContext[0].Content[1].(*protocol.Image).Data)
}
