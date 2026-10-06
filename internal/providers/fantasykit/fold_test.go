package fantasykit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestFoldToolDeltaPrefersDelta(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		delta string
		input string
		want  string
	}{
		{name: "delta only is not dropped", delta: `{"x":1}`, want: `{"x":1}`},
		{name: "anthropic tool call input", input: `{"y":2}`, want: `{"y":2}`},
		{name: "delta wins when both set", delta: `{"x":1}`, input: `{"y":2}`, want: `{"x":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parts := func(yield func(fantasy.StreamPart) bool) {
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: "call_1", ToolCallName: "echo"})
				_ = yield(fantasy.StreamPart{
					Type:          fantasy.StreamPartTypeToolInputDelta,
					ID:            "call_1",
					Delta:         tc.delta,
					ToolCallInput: tc.input,
				})
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: "call_1"})
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "call_1", ToolCallName: "echo"})
				_ = yield(fantasy.StreamPart{
					Type:  fantasy.StreamPartTypeFinish,
					ID:    "msg_1",
					Usage: fantasy.Usage{InputTokens: 1, OutputTokens: 1},
				})
			}
			s := providers.NewStream(context.Background(), 16, protocol.AssistantMessage{}, func(a *providers.Assembler) {
				a.Start()
				Fold(a, parts, Options{
					MapStop: func(string) (protocol.StopReason, error) {
						return protocol.StopToolUse, nil
					},
				})
			})
			for range s.Events() {
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			msg, err := s.Result(ctx)
			require.NoError(t, err)
			require.NoError(t, ctx.Err())
			require.NotEmpty(t, msg.Content)
			call, ok := msg.Content[0].(protocol.ToolCall)
			require.True(t, ok)
			assert.JSONEq(t, tc.want, string(call.Arguments))
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(call.Arguments, &parsed))
		})
	}
}

func TestFoldToolIDRewritePreservesArguments(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late-metadata/%v", late), func(t *testing.T) {
			parts := func(yield func(fantasy.StreamPart) bool) {
				start := fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: "call_1", ToolCallName: "echo"}
				if !late {
					start.ProviderMetadata = fantasy.ProviderMetadata{"item": nil}
				}
				_ = yield(start)
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: "call_1", Delta: `{"text":"hello"}`})
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "call_1", ToolCallName: "echo", ProviderMetadata: fantasy.ProviderMetadata{"item": nil}})
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish})
			}
			stream := providers.NewStream(context.Background(), 16, protocol.AssistantMessage{}, func(a *providers.Assembler) {
				a.Start()
				Fold(a, parts, Options{Tool: func(id string, md fantasy.ProviderMetadata) string {
					if _, ok := md["item"]; ok {
						return id + "|fc_1"
					}
					return id
				}, MapStop: func(string) (protocol.StopReason, error) { return protocol.StopToolUse, nil }})
			})
			for range stream.Events() {
			}
			msg, err := stream.Result(context.Background())
			require.NoError(t, err)
			call := msg.Content[0].(protocol.ToolCall)
			require.Equal(t, "call_1|fc_1", call.ID)
			require.Equal(t, "echo", call.Name)
			require.JSONEq(t, `{"text":"hello"}`, string(call.Arguments))
		})
	}
}
