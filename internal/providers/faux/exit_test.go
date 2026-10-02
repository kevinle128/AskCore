package faux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// exitScript is the reply of the exit sequence: thinking, text and two tool
// calls, one delta each under a chunk size of one token.
func exitScript() Step {
	return Reply(
		Thinking("go"), Text("ok"),
		ToolCall("echo", map[string]any{}, ID("a")),
		ToolCall("echo", map[string]any{}, ID("b")),
	)
}

func exitProvider(t *testing.T) *Provider {
	t.Helper()
	return newProvider(t, WithChunk(1, 1), WithClock(newFakeClock()))
}

// exitWant is the normalized script as the final message of the exit reply.
func exitWant() protocol.AssistantMessage {
	return protocol.AssistantMessage{
		Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "go"},
			protocol.Text{Text: "ok"},
			protocol.ToolCall{ID: "a", Name: "echo", Arguments: []byte(`{}`)},
			protocol.ToolCall{ID: "b", Name: "echo", Arguments: []byte(`{}`)},
		},
		API: "faux", Provider: "faux", Model: defaultModelID,
		// "user:hi" is 2 tokens; "go\nok\necho:{}\necho:{}" is 21 runes, 6 tokens.
		Usage:      protocol.Usage{Input: 2, Output: 6, TotalTokens: 8},
		StopReason: protocol.StopToolUse,
		Timestamp:  fixedNow,
	}
}

// project maps provider items to agent events: start becomes message_start,
// the nine block events become message_update with the captured usage, and the
// result becomes message_end. Terminal events are not mapped on their own.
// A stream with no start event (a setup error) gives message_start with the
// pending seed of the final message, then message_end with the final message.
func project(items []providers.StreamItem, final protocol.AssistantMessage) []protocol.Event {
	var out []protocol.Event
	for _, it := range items {
		switch ev := it.Event.(type) {
		case protocol.StartEvent:
			out = append(out, &protocol.MessageStart{Message: ev.Message})
		case protocol.DoneEvent, protocol.ErrorEvent:
		case protocol.BlockEvent:
			out = append(out, &protocol.MessageUpdate{AssistantMessageEvent: ev, Usage: it.Usage})
		}
	}
	if len(out) == 0 {
		seed := final
		seed.StopReason = protocol.StopPending
		seed.ErrorMessage = nil
		seed.Content = []protocol.AssistantBlock{}
		out = append(out, &protocol.MessageStart{Message: seed})
	}
	out = append(out, &protocol.MessageEnd{Message: final})
	for i, ev := range out {
		*ev.Env() = protocol.Envelope{Seq: uint64(i + 1), TS: fixedNow, SessionID: "session-1", RunID: "run-1"}
	}
	return out
}

// roundTrip writes the events as JSONL, reads them back, checks that they are
// equal and rebuilds the message. The second message is the builder state
// after the updates only, before message_end.
func roundTrip(t *testing.T, events []protocol.Event) (final, beforeEnd protocol.AssistantMessage, lines [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	w := protocol.NewJSONLWriter(&buf)
	for _, ev := range events {
		require.NoError(t, w.Write(ev))
	}
	r := protocol.NewJSONLReader(&buf)
	var decoded []protocol.Event
	for {
		line, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		lines = append(lines, line)
		ev, err := protocol.DecodeEvent(line)
		require.NoError(t, err)
		decoded = append(decoded, ev)
	}
	require.Equal(t, events, decoded, "JSONL decode gives equal events")

	b := protocol.NewBuilder()
	for i, ev := range decoded {
		if i == len(decoded)-1 {
			beforeEnd = b.Snapshot()
		}
		require.NoError(t, b.ApplyAgentEvent(ev))
	}
	final = b.Snapshot()
	return final, beforeEnd, lines
}

func assertNoTerminalInUpdates(t *testing.T, lines [][]byte) {
	t.Helper()
	nine := map[string]bool{
		"text_start": true, "text_delta": true, "text_end": true,
		"thinking_start": true, "thinking_delta": true, "thinking_end": true,
		"toolcall_start": true, "toolcall_delta": true, "toolcall_end": true,
	}
	for _, line := range lines {
		var env struct {
			Type string `json:"type"`
			Evt  *struct {
				Type string `json:"type"`
			} `json:"assistantMessageEvent"`
		}
		require.NoError(t, json.Unmarshal(line, &env))
		if env.Type == protocol.TypeMessageUpdate {
			require.NotNil(t, env.Evt)
			assert.True(t, nine[env.Evt.Type], "message_update holds %q", env.Evt.Type)
		} else {
			assert.Nil(t, env.Evt)
		}
	}
}

func TestExitSequenceEventOrder(t *testing.T) {
	p := exitProvider(t)
	p.Set(exitScript())
	items, msg, err := play(t, p)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"start",
		"thinking_start", "thinking_delta", "thinking_end",
		"text_start", "text_delta", "text_end",
		"toolcall_start", "toolcall_delta", "toolcall_end",
		"toolcall_start", "toolcall_delta", "toolcall_end",
		"done",
	}, kinds(items))
	assert.Equal(t, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "go"}, items[2].Event)
	assert.Equal(t, protocol.TextDeltaEvent{ContentIndex: 1, Delta: "ok"}, items[5].Event)
	assert.Equal(t, protocol.ToolCallStartEvent{ContentIndex: 2, ID: "a", ToolName: "echo"}, items[7].Event)
	assert.Equal(t, protocol.ToolCallDeltaEvent{ContentIndex: 2, Delta: "{}"}, items[8].Event)
	assert.Equal(t, protocol.ToolCallStartEvent{ContentIndex: 3, ID: "b", ToolName: "echo"}, items[10].Event)
	assert.Equal(t, exitWant(), msg)
	done := items[len(items)-1].Event.(protocol.DoneEvent)
	assert.Equal(t, protocol.StopToolUse, done.Reason)
	assert.Equal(t, exitWant(), done.Message)
	for _, it := range items {
		assert.Equal(t, msg.Usage, it.Usage)
	}
}

func TestExitProjectionJSONLRoundTrip(t *testing.T) {
	p := exitProvider(t)
	p.Set(exitScript())
	items, msg, err := play(t, p)
	require.NoError(t, err)
	want := exitWant()

	events := project(items, msg)
	require.Len(t, events, 14, "message_start, twelve updates, message_end")
	start, ok := events[0].(*protocol.MessageStart)
	require.True(t, ok)
	seed := start.Message.(protocol.AssistantMessage)
	assert.Equal(t, "faux", seed.API)
	assert.Equal(t, "faux", seed.Provider)
	assert.Equal(t, defaultModelID, seed.Model)
	assert.Equal(t, fixedNow, seed.Timestamp)
	assert.Equal(t, protocol.StopPending, seed.StopReason)
	assert.Empty(t, seed.Content)
	for _, ev := range events[1:13] {
		up, ok := ev.(*protocol.MessageUpdate)
		require.True(t, ok)
		assert.Equal(t, want.Usage, up.Usage, "each update carries the usage captured with its item")
	}
	end, ok := events[13].(*protocol.MessageEnd)
	require.True(t, ok)
	assert.Equal(t, want, end.Message)

	final, beforeEnd, lines := roundTrip(t, events)
	assertNoTerminalInUpdates(t, lines)
	assert.Equal(t, want, final)
	assert.Equal(t, msg, final)

	// The updates alone rebuild the content, identity and usage.
	assert.Equal(t, want.Content, beforeEnd.Content)
	assert.Equal(t, want.API, beforeEnd.API)
	assert.Equal(t, want.Model, beforeEnd.Model)
	assert.Equal(t, want.Timestamp, beforeEnd.Timestamp)
	assert.Equal(t, want.Usage, beforeEnd.Usage)
}

func TestExitProjectionOfSetupError(t *testing.T) {
	p := exitProvider(t)
	items, msg, err := play(t, p) // empty queue
	require.Error(t, err)
	events := project(items, msg)
	require.Len(t, events, 2, "message_start and message_end, no update")
	assert.IsType(t, &protocol.MessageStart{}, events[0])
	assert.IsType(t, &protocol.MessageEnd{}, events[1])
	assert.Equal(t, protocol.StopPending, events[0].(*protocol.MessageStart).Message.(protocol.AssistantMessage).StopReason)
	assert.Equal(t, msg, events[1].(*protocol.MessageEnd).Message)
	final, _, lines := roundTrip(t, events)
	assertNoTerminalInUpdates(t, lines)
	assert.Equal(t, msg, final)
	assert.Equal(t, protocol.StopError, final.StopReason)
}

func TestExitTruncatedReplyKeepsPartialContent(t *testing.T) {
	long := Reply(
		Thinking("go"), Text("ok"),
		ToolCall("echo", map[string]any{"text": "hello world"}, ID("a")),
	)
	tool := func(id string, args string) protocol.AssistantBlock {
		return protocol.ToolCall{ID: id, Name: "echo", Arguments: []byte(args)}
	}
	cases := []struct {
		name    string
		step    Step
		cut     int
		content []protocol.AssistantBlock
	}{
		{"before start", exitScript(), 0, nil},
		{"after start", exitScript(), 1, nil},
		{"inside thinking", exitScript(), 3, []protocol.AssistantBlock{protocol.Thinking{Thinking: "go"}}},
		{"after text", exitScript(), 7, []protocol.AssistantBlock{protocol.Thinking{Thinking: "go"}, protocol.Text{Text: "ok"}}},
		{"mid tool, after its delta", exitScript(), 9, []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "go"}, protocol.Text{Text: "ok"}, tool("a", `{}`),
		}},
		{"mid tool, second call just started", exitScript(), 11, []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "go"}, protocol.Text{Text: "ok"}, tool("a", `{}`), tool("b", `{}`),
		}},
		{"mid tool, long arguments cut after two deltas", long, 10, []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "go"}, protocol.Text{Text: "ok"}, tool("a", `{}`),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := exitProvider(t)
			p.Set(tc.step.Truncate(tc.cut))
			s := start(t, p, context.Background())
			// The result settles after producer close without a channel reader.
			msg, err := result(t, s)
			items := drain(s)

			require.Len(t, items, tc.cut+1, "n events and the error item for the missing terminal event")
			last, ok := items[len(items)-1].Event.(protocol.ErrorEvent)
			require.True(t, ok)
			assert.Equal(t, protocol.StopError, last.Reason)
			assert.ErrorIs(t, err, providers.ErrStreamIncomplete)
			assert.Equal(t, protocol.StopError, msg.StopReason)
			assert.Equal(t, "stream ended without a terminal event", *msg.ErrorMessage)
			assert.Equal(t, "faux", msg.API)
			assert.Equal(t, "faux", msg.Provider)
			assert.Equal(t, defaultModelID, msg.Model)
			assert.Equal(t, fixedNow, msg.Timestamp)
			assert.Equal(t, exitWant().Usage.Input, msg.Usage.Input)
			if tc.step.blocks != nil && tc.cut > 0 {
				for _, it := range items {
					assert.Equal(t, msg.Usage, it.Usage)
				}
			}
			assert.Equal(t, last.Error, msg)

			want := tc.content
			if want == nil {
				want = []protocol.AssistantBlock{}
			}
			got := msg.Content
			if got == nil {
				got = []protocol.AssistantBlock{}
			}
			if tc.name == "mid tool, long arguments cut after two deltas" {
				require.Len(t, got, 3)
				c := got[2].(protocol.ToolCall)
				assert.Equal(t, "a", c.ID)
				assert.True(t, json.Valid(c.Arguments), "the salvaged arguments are a valid object: %s", c.Arguments)
				got = got[:2]
				want = want[:2]
			}
			assert.Equal(t, want, got)

			// The cut stream still projects, encodes and rebuilds to the result.
			final, _, lines := roundTrip(t, project(items, msg))
			assertNoTerminalInUpdates(t, lines)
			assert.Equal(t, msg, final)
		})
	}
}
