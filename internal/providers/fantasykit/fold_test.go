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

func TestFinalUsageReplacesLastSampleAndMissingFinalKeepsSample(t *testing.T) {
	sample := func(n int64) protocol.Usage {
		return protocol.Usage{Input: n, TotalTokens: n}
	}
	cases := []struct {
		name  string
		final fantasy.Usage
		want  protocol.Usage
	}{
		{"final usage replaces the last sample", fantasy.Usage{InputTokens: 25}, sample(25)},
		{"missing final usage keeps the last sample", fantasy.Usage{}, sample(20)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts := func(yield func(fantasy.StreamPart) bool) {
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "t"})
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "t", Delta: "hi"})
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "t"})
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: tc.final})
			}
			s := providers.NewStream(context.Background(), 16, protocol.AssistantMessage{}, func(a *providers.Assembler) {
				a.Start()
				a.SetUsage(sample(10))
				a.SetUsage(sample(20))
				Fold(a, parts, Options{MapStop: func(string) (protocol.StopReason, error) { return protocol.StopStop, nil }})
			})
			for range s.Events() {
			}
			msg, err := s.Result(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, msg.Usage, "the samples are not summed")
		})
	}
}

func TestFoldEmptyStopIsEmptyResponseButEmptyLengthIsNot(t *testing.T) {
	run := func(reason protocol.StopReason) (protocol.AssistantMessage, error) {
		parts := func(yield func(fantasy.StreamPart) bool) {
			_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish})
		}
		s := providers.NewStream(context.Background(), 16, protocol.AssistantMessage{}, func(a *providers.Assembler) {
			a.Start()
			Fold(a, parts, Options{MapStop: func(string) (protocol.StopReason, error) { return reason, nil }})
		})
		for range s.Events() {
		}
		return s.Result(context.Background())
	}
	_, err := run(protocol.StopStop)
	require.Error(t, err)
	assert.Equal(t, providers.CodeEmptyResponse, providers.CodeOf(err))

	msg, err := run(protocol.StopLength)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopLength, msg.StopReason)
}

func TestFoldUsageUsesZeroBucketsAndDerivedTotal(t *testing.T) {
	cases := []struct {
		name  string
		usage fantasy.Usage
		want  protocol.Usage
	}{
		{
			name:  "absent cache and reasoning",
			usage: fantasy.Usage{InputTokens: 10, OutputTokens: 12, TotalTokens: 99},
			want:  protocol.Usage{Input: 10, Output: 12, TotalTokens: 22},
		},
		{
			name:  "both cache buckets contribute",
			usage: fantasy.Usage{InputTokens: 10, OutputTokens: 12, CacheReadTokens: 3, CacheCreationTokens: 5, TotalTokens: 99},
			want:  protocol.Usage{Input: 10, Output: 12, CacheRead: 3, CacheWrite: 5, TotalTokens: 30},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts := func(yield func(fantasy.StreamPart) bool) {
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "text"}) {
					return
				}
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "done"}) {
					return
				}
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "text"}) {
					return
				}
				_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: tc.usage})
			}
			stream := providers.NewStream(context.Background(), 16, protocol.AssistantMessage{}, func(assembler *providers.Assembler) {
				assembler.Start()
				Fold(assembler, parts, Options{MapStop: func(string) (protocol.StopReason, error) {
					return protocol.StopStop, nil
				}})
			})
			for range stream.Events() {
			}
			message, err := stream.Result(context.Background())
			require.NoError(t, err)
			require.Equal(t, protocol.StopStop, message.StopReason)
			require.Equal(t, tc.want, message.Usage, "Fold derives the total instead of copying the reported total")
			require.Nil(t, message.Usage.Reasoning, "default Fold has no separate reasoning count")
		})
	}
}
