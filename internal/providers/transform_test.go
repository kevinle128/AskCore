package providers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestTransformMessages(t *testing.T) {
	model := Model{ID: "m", API: "api", Provider: "prov", Input: []string{"text", "image"}}
	plain := Model{ID: "m", API: "api", Provider: "prov", Input: []string{"text"}}
	clock := func() int64 { return 42 }
	emptySig := ""
	sig := func(s string) *string { return &s }

	type tc struct {
		name      string
		model     Model
		in        []protocol.Message
		want      []protocol.Message
		normalize NormalizeToolCallID
	}

	sameAsst := func(reason protocol.StopReason, blocks ...protocol.AssistantBlock) protocol.AssistantMessage {
		return protocol.AssistantMessage{
			API: "api", Provider: "prov", Model: "m",
			StopReason: reason,
			Content:    blocks,
		}
	}
	foreignAsst := func(blocks ...protocol.AssistantBlock) protocol.AssistantMessage {
		return protocol.AssistantMessage{
			API: "api", Provider: "prov", Model: "other",
			StopReason: protocol.StopStop,
			Content:    blocks,
		}
	}

	cases := []tc{
		{
			name:  "error and aborted assistants dropped without synthetic results",
			model: model,
			in: []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
				sameAsst(protocol.StopError, protocol.ToolCall{ID: "c1", Name: "echo"}),
				sameAsst(protocol.StopAborted, protocol.ToolCall{ID: "c2", Name: "echo"}),
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "again"}}},
			},
			want: []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
				protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "again"}}},
			},
		},
		{
			name:  "orphan call at the end gets a synthetic error result",
			model: model,
			in: []protocol.Message{
				sameAsst(protocol.StopToolUse, protocol.ToolCall{ID: "c1", Name: "echo"}),
			},
			want: []protocol.Message{
				sameAsst(protocol.StopToolUse, protocol.ToolCall{ID: "c1", Name: "echo"}),
				protocol.ToolResultMessage{
					ToolCallID: "c1",
					ToolName:   "echo",
					Content:    []protocol.UserBlock{protocol.Text{Text: "No result provided"}},
					IsError:    true,
					Timestamp:  42,
				},
			},
		},
		{
			name:  "foreign thinking becomes plain text with no tags and no signature",
			model: model,
			in: []protocol.Message{
				foreignAsst(protocol.Thinking{Thinking: "secret plan", ThinkingSignature: sig("abc")}),
			},
			want: []protocol.Message{
				foreignAsst(protocol.Text{Text: "secret plan"}),
			},
		},
		{
			name:  "same-model thinking with empty signature pointer and non-empty text is kept",
			model: model,
			in: []protocol.Message{
				sameAsst(protocol.StopStop, protocol.Thinking{Thinking: "kept", ThinkingSignature: &emptySig}),
			},
			want: []protocol.Message{
				sameAsst(protocol.StopStop, protocol.Thinking{Thinking: "kept", ThinkingSignature: &emptySig}),
			},
		},
		{
			name:  "empty thinking text is dropped",
			model: model,
			in: []protocol.Message{
				sameAsst(protocol.StopStop, protocol.Thinking{Thinking: "   ", ThinkingSignature: &emptySig}, protocol.Text{Text: "hi"}),
			},
			want: []protocol.Message{
				sameAsst(protocol.StopStop, protocol.Text{Text: "hi"}),
			},
		},
		{
			name:  "non-empty signature keeps empty thinking text",
			model: model,
			in: []protocol.Message{
				sameAsst(protocol.StopStop, protocol.Thinking{Thinking: "", ThinkingSignature: sig("sig")}),
			},
			want: []protocol.Message{
				sameAsst(protocol.StopStop, protocol.Thinking{Thinking: "", ThinkingSignature: sig("sig")}),
			},
		},
		{
			name:  "signatures dropped on foreign text and tool thought signatures",
			model: model,
			in: []protocol.Message{
				foreignAsst(
					protocol.Text{Text: "hi", TextSignature: sig("tsig")},
					protocol.ToolCall{ID: "c1", Name: "echo", ThoughtSignature: sig("thsig")},
				),
			},
			want: []protocol.Message{
				foreignAsst(
					protocol.Text{Text: "hi"},
					protocol.ToolCall{ID: "c1", Name: "echo"},
				),
				protocol.ToolResultMessage{
					ToolCallID: "c1",
					ToolName:   "echo",
					Content:    []protocol.UserBlock{protocol.Text{Text: "No result provided"}},
					IsError:    true,
					Timestamp:  42,
				},
			},
		},
		{
			name:  "non-vision model replaces images and collapses consecutive placeholders",
			model: plain,
			in: []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{
					protocol.Image{Data: "aaa", MimeType: "image/png"},
					protocol.Image{Data: "bbb", MimeType: "image/png"},
					protocol.Text{Text: "look"},
				}},
				sameAsst(protocol.StopToolUse, protocol.ToolCall{ID: "c1", Name: "see"}),
				protocol.ToolResultMessage{
					ToolCallID: "c1",
					ToolName:   "see",
					Content: []protocol.UserBlock{
						protocol.Text{Text: "(tool image omitted: model does not support images)"},
						protocol.Image{Data: "ccc", MimeType: "image/png"},
					},
				},
			},
			want: []protocol.Message{
				protocol.UserMessage{Content: []protocol.UserBlock{
					protocol.Text{Text: "(image omitted: model does not support images)"},
					protocol.Text{Text: "look"},
				}},
				sameAsst(protocol.StopToolUse, protocol.ToolCall{ID: "c1", Name: "see"}),
				protocol.ToolResultMessage{
					ToolCallID: "c1",
					ToolName:   "see",
					Content: []protocol.UserBlock{
						protocol.Text{Text: "(tool image omitted: model does not support images)"},
					},
				},
			},
		},
		{
			name:  "system message held while a call is pending and flushed after the synthetic result",
			model: model,
			in: []protocol.Message{
				sameAsst(protocol.StopToolUse, protocol.ToolCall{ID: "c1", Name: "echo"}),
				protocol.SystemMessage{Content: []protocol.Text{{Text: "held"}}},
			},
			want: []protocol.Message{
				sameAsst(protocol.StopToolUse, protocol.ToolCall{ID: "c1", Name: "echo"}),
				protocol.ToolResultMessage{
					ToolCallID: "c1",
					ToolName:   "echo",
					Content:    []protocol.UserBlock{protocol.Text{Text: "No result provided"}},
					IsError:    true,
					Timestamp:  42,
				},
				protocol.SystemMessage{Content: []protocol.Text{{Text: "held"}}},
			},
		},
		{
			name:  "orphan tool result dropped",
			model: model,
			in: []protocol.Message{
				protocol.ToolResultMessage{
					ToolCallID: "orphan",
					ToolName:   "echo",
					Content:    []protocol.UserBlock{protocol.Text{Text: "late"}},
				},
			},
			want: []protocol.Message{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TransformMessages(c.in, c.model, clock, c.normalize)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestTransformMessagesDoesNotMutateInput(t *testing.T) {
	model := Model{ID: "m", API: "api", Provider: "prov", Input: []string{"text"}}
	sig := "keep"
	in := make([]protocol.Message, 2, 6)
	in[0] = protocol.UserMessage{Content: []protocol.UserBlock{
		protocol.Image{Data: "aaa", MimeType: "image/png"},
		protocol.Text{Text: "hi"},
	}}
	in[1] = protocol.AssistantMessage{
		API: "api", Provider: "prov", Model: "m",
		StopReason: protocol.StopStop,
		Content:    []protocol.AssistantBlock{protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig}},
	}
	snapshot := []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{
			protocol.Image{Data: "aaa", MimeType: "image/png"},
			protocol.Text{Text: "hi"},
		}},
		protocol.AssistantMessage{
			API: "api", Provider: "prov", Model: "m",
			StopReason: protocol.StopStop,
			Content:    []protocol.AssistantBlock{protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig}},
		},
	}

	out := TransformMessages(in, model, func() int64 { return 1 }, nil)
	require.NotEmpty(t, out)
	out[0] = protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "mutated"}}}
	if um, ok := out[0].(protocol.UserMessage); ok && len(um.Content) > 0 {
		_ = um
	}

	assert.Equal(t, snapshot, in)
	assert.Nil(t, in[:3][2], "the caller's spare capacity is untouched")
}

func TestTransformMessagesNilNormalizeKeepsIDs(t *testing.T) {
	model := Model{ID: "m", API: "other-api", Provider: "prov"}
	in := []protocol.Message{
		protocol.AssistantMessage{
			API: "api", Provider: "prov", Model: "m",
			StopReason: protocol.StopToolUse,
			Content:    []protocol.AssistantBlock{protocol.ToolCall{ID: "toolu_1", Name: "echo"}},
		},
	}
	got := TransformMessages(in, model, func() int64 { return 7 }, nil)
	require.Len(t, got, 2)
	asst := got[0].(protocol.AssistantMessage)
	tc := asst.Content[0].(protocol.ToolCall)
	assert.Equal(t, "toolu_1", tc.ID)
	res := got[1].(protocol.ToolResultMessage)
	assert.Equal(t, "toolu_1", res.ToolCallID)
	assert.Equal(t, int64(7), res.Timestamp)
}

func TestTransformMessagesRekeysForeignToolIDs(t *testing.T) {
	model := Model{ID: "m", API: "api", Provider: "prov"}
	in := []protocol.Message{
		protocol.AssistantMessage{
			API: "other", Provider: "prov", Model: "m",
			StopReason: protocol.StopToolUse,
			Content:    []protocol.AssistantBlock{protocol.ToolCall{ID: "bad id", Name: "echo"}},
		},
		protocol.ToolResultMessage{ToolCallID: "bad id", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}},
	}
	got := TransformMessages(in, model, func() int64 { return 1 }, func(id string, _ Model, _ protocol.AssistantMessage) string {
		return "bad_id"
	})
	asst := got[0].(protocol.AssistantMessage)
	assert.Equal(t, "bad_id", asst.Content[0].(protocol.ToolCall).ID)
	assert.Equal(t, "bad_id", got[1].(protocol.ToolResultMessage).ToolCallID)
}

func TestTransformMessagesLeavesRawMessages(t *testing.T) {
	raw := protocol.RawMessage{RoleName: "custom", Data: json.RawMessage(`{"role":"custom"}`)}
	got := TransformMessages([]protocol.Message{raw}, Model{}, func() int64 { return 1 }, nil)
	assert.Equal(t, []protocol.Message{raw}, got)
}

func TestTruncatedToolOnlyMessageIsNotReplayed(t *testing.T) {
	model := Model{ID: "m", API: "api", Provider: "prov", Input: []string{"text"}}
	user := func(s string) protocol.Message {
		return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: s}}}
	}
	truncated := protocol.AssistantMessage{
		API: "api", Provider: "prov", Model: "m",
		StopReason: protocol.StopLength,
		Content:    []protocol.AssistantBlock{},
	}
	kept := protocol.AssistantMessage{
		API: "api", Provider: "prov", Model: "m",
		StopReason: protocol.StopStop,
		Content:    []protocol.AssistantBlock{protocol.Text{Text: "kept"}},
	}
	emptyStop := truncated
	emptyStop.StopReason = protocol.StopStop
	in := []protocol.Message{user("one"), kept, user("two"), truncated, user("three"), emptyStop, user("four")}

	out := TransformMessages(in, model, func() int64 { return 1 }, nil)

	assert.Equal(t, []protocol.Message{user("one"), kept, user("two"), user("three"), user("four")}, out,
		"an empty assistant message is left out whatever its stop reason, and its neighbours stay")
}

func TestInterruptedMessageReplayRetainsOnlyNonblankContent(t *testing.T) {
	model := Model{ID: "m", API: "api", Provider: "prov"}
	signature := "signed"
	for _, tc := range []struct {
		name        string
		input, want []protocol.AssistantBlock
	}{
		{"unsigned thinking is plain text", []protocol.AssistantBlock{protocol.Text{Text: "partial"}, protocol.Thinking{Thinking: "plan"}, protocol.ToolCall{ID: "unfinished", Name: "tool"}, protocol.Text{Text: " \n"}}, []protocol.AssistantBlock{protocol.Text{Text: "partial"}, protocol.Text{Text: "plan"}}},
		{"signed thinking stays thinking", []protocol.AssistantBlock{protocol.Thinking{Thinking: "plan", ThinkingSignature: &signature}}, []protocol.AssistantBlock{protocol.Thinking{Thinking: "plan", ThinkingSignature: &signature}}},
		{"blank wrapper stays out", []protocol.AssistantBlock{protocol.Text{Text: " \n"}, protocol.Thinking{Thinking: "\t"}}, nil},
		{"tool only stays out", []protocol.AssistantBlock{protocol.ToolCall{ID: "unfinished", Name: "tool"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			interrupted := protocol.AssistantMessage{API: string(model.API), Provider: model.Provider, Model: model.ID, StopReason: protocol.StopAborted, Content: tc.input}
			for _, source := range []protocol.Message{interrupted, &interrupted} {
				got := TransformMessages([]protocol.Message{source}, model, func() int64 { return 1 }, nil)
				if tc.want == nil {
					require.Empty(t, got)
					continue
				}
				require.Len(t, got, 1)
				message, ok := assistantValue(got[0])
				require.True(t, ok)
				require.Equal(t, tc.want, message.Content)
			}
			require.Equal(t, tc.input, interrupted.Content, "replay does not mutate history")
		})
	}
}

func TestInterruptedUnsignedThinkingIsReplayedAsText(t *testing.T) {
	model := Model{ID: "m", API: "api", Provider: "prov"}
	interrupted := protocol.AssistantMessage{API: string(model.API), Provider: model.Provider, Model: model.ID, StopReason: protocol.StopAborted, Content: []protocol.AssistantBlock{protocol.Thinking{Thinking: "unfinished reasoning"}}}
	got := TransformMessages([]protocol.Message{interrupted}, model, nil, nil)
	require.Len(t, got, 1)
	message, ok := assistantValue(got[0])
	require.True(t, ok)
	require.Equal(t, []protocol.AssistantBlock{protocol.Text{Text: "unfinished reasoning"}}, message.Content)
	require.IsType(t, protocol.Thinking{}, interrupted.Content[0], "replay keeps the original thinking block in history")
}
