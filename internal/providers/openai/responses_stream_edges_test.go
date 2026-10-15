package openai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func responsesFromSSE(t *testing.T, sse string) (protocol.AssistantMessage, error) {
	t.Helper()
	srv := newCaptureServer(t, sse)
	msg, err := resultOf(t, responsesProvider(srv.srv, 0).Stream(context.Background(), responsesModel(srv.srv.URL), helloReq(), providers.StreamOptions{}))
	_ = srv.take(t)
	return msg, err
}

func TestResponsesOutputIndexesFinalizeReasoningAndToolJSON(t *testing.T) {
	sse := responsesEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":2,"item":{"id":"fc_2","type":"function_call","call_id":"call_2","name":"echo","arguments":""}}`) +
		responsesEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_0","type":"reasoning","encrypted_content":"opaque-start","summary":[]}}`) +
		responsesEvent("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":2,"item_id":"fc_2","delta":"{}"}`) +
		responsesEvent("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_0","type":"reasoning","encrypted_content":"opaque-final","summary":[{"type":"summary_text","text":"plan"}]}}`) +
		responsesEvent("response.output_item.done", `{"type":"response.output_item.done","output_index":2,"item":{"id":"fc_2","type":"function_call","call_id":"call_2","name":"echo","arguments":"{}"}}`) +
		responsesEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	msg, err := responsesFromSSE(t, sse)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopToolUse, msg.StopReason)
	var sawReasoning, sawTool bool
	for _, block := range msg.Content {
		switch v := block.(type) {
		case protocol.Thinking:
			sawReasoning = true
			require.NotNil(t, v.ThinkingSignature)
			replay, ok := providers.DecodeThinkingSignature(*v.ThinkingSignature)
			require.True(t, ok)
			assert.Equal(t, "rs_0", replay.ID)
			assert.Equal(t, "opaque-final", replay.EncryptedContent)
		case protocol.ToolCall:
			sawTool = true
			assert.Equal(t, "call_2|fc_2", v.ID)
			assert.JSONEq(t, `{}`, string(v.Arguments))
		}
	}
	assert.True(t, sawReasoning)
	assert.True(t, sawTool)
}

func TestResponsesRejectsUnfinishedToolCallJSON(t *testing.T) {
	sse := responsesEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","arguments":""}}`) +
		responsesEvent("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"x\":"}`) +
		responsesEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	msg, err := responsesFromSSE(t, sse)
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
}

func TestResponsesIncompleteAndToolStopMapJSON(t *testing.T) {
	for _, tc := range []struct {
		name, sse string
		want      protocol.StopReason
		wantError bool
	}{
		{"max-output", responsesEvent("response.incomplete", `{"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","output":[],"incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`), protocol.StopLength, false},
		{"content-filter", responsesEvent("response.incomplete", `{"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","output":[],"incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`), protocol.StopError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := responsesFromSSE(t, tc.sse)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, msg.StopReason)
		})
	}
}
