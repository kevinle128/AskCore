package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(seq uint64) Envelope {
	return Envelope{Seq: seq, TS: 1700000000000 + int64(seq), SessionID: "s1", RunID: "r1"}
}

func finalAssistant() AssistantMessage {
	m := fullAssistant()
	m.StopReason = StopToolUse
	return m
}

func allEvents() []Event {
	upd := &MessageUpdate{Envelope: env(5), AssistantMessageEvent: TextDeltaEvent{ContentIndex: 1, Delta: "ok"}, Usage: Usage{Input: 3, TotalTokens: 3}}
	return []Event{
		&AgentStart{Envelope: env(1)},
		&CycleStart{Envelope: env(2), CycleID: "c1"},
		&TurnStart{Envelope: env(2), CycleID: "c1"},
		&AttemptStart{Envelope: env(2), AttemptID: "a1", CycleID: "c1", Number: 1},
		&AttemptEnd{Envelope: env(2), AttemptID: "a1", Outcome: "completed"},
		&CycleEnd{Envelope: env(2), CycleID: "c1", Reason: "aborted", Cause: "user"},
		&MessageStart{Envelope: env(3), Message: UserMessage{Content: []UserBlock{Text{Text: "hi"}}, Timestamp: 1}},
		&MessageStart{Envelope: env(4), Message: seedMessage()},
		upd,
		&MessageEnd{Envelope: env(6), Message: finalAssistant()},
		&ToolExecutionStart{Envelope: env(7), ToolCallID: "a", ToolName: "echo", Args: json.RawMessage(`{"x":1}`)},
		&ToolExecutionUpdate{Envelope: env(8), ToolCallID: "a", ToolName: "echo", Args: json.RawMessage(`{"x":1}`),
			PartialResult: ToolExecutionResult{Content: []UserBlock{Text{Text: "p"}}}},
		&ToolExecutionEnd{Envelope: env(9), ToolCallID: "a", ToolName: "echo",
			Result: ToolExecutionResult{Content: []UserBlock{Text{Text: "done"}}, Details: json.RawMessage(`{"d":1}`)}, IsError: true},
		&TurnEnd{Envelope: env(10), Message: finalAssistant(),
			ToolResults: []ToolResultMessage{{ToolCallID: "a", ToolName: "echo", Content: []UserBlock{Text{Text: "done"}}}}},
		&AgentEnd{Envelope: env(11), Messages: []Message{UserMessage{Content: []UserBlock{}}, finalAssistant(),
			RawMessage{RoleName: "custom", Data: json.RawMessage(`{"role":"custom"}`)}}},
		&AgentSettled{Envelope: env(12)},
		&AutoRetryStart{Envelope: env(13), Attempt: 1, MaxAttempts: 5, DelayMs: 500, ErrorMessage: "slow down"},
		&AutoRetryEnd{Envelope: env(14), Success: false, Attempt: 5, FinalError: "failed"},
	}
}

func TestAgentSettledJSONLRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONLWriter(&buf)
	require.NoError(t, w.Write(&AgentEnd{Envelope: env(1)}))
	require.NoError(t, w.Write(&AgentSettled{Envelope: env(2)}))
	assert.Equal(t, `{"seq":1,"ts":1700000000001,"sessionId":"s1","runId":"r1","type":"agent_end","messages":[],"willRetry":false}`+"\n"+
		`{"seq":2,"ts":1700000000002,"sessionId":"s1","runId":"r1","type":"agent_settled"}`+"\n", buf.String())

	r := NewJSONLReader(&buf)
	_, err := r.Next()
	require.NoError(t, err)
	line, err := r.Next()
	require.NoError(t, err)
	ev, err := DecodeEvent(line)
	require.NoError(t, err)
	assert.Equal(t, &AgentSettled{Envelope: env(2)}, ev)
	_, err = r.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestEventRoundTripAllTypes(t *testing.T) {
	for _, ev := range allEvents() {
		t.Run(ev.EventType(), func(t *testing.T) {
			b, err := EncodeEvent(ev)
			require.NoError(t, err)
			back, err := DecodeEvent(b)
			require.NoError(t, err)
			assert.Equal(t, ev, back)
		})
	}
}

func TestEventFlatJSONGolden(t *testing.T) {
	b, err := EncodeEvent(&AgentStart{Envelope: env(1)})
	require.NoError(t, err)
	assert.Equal(t, `{"seq":1,"ts":1700000000001,"sessionId":"s1","runId":"r1","type":"agent_start"}`, string(b))

	b, err = EncodeEvent(&MessageUpdate{Envelope: env(2), AssistantMessageEvent: ToolCallStartEvent{ContentIndex: 2, ID: "a", ToolName: "echo"}})
	require.NoError(t, err)
	assert.Equal(t, `{"seq":2,"ts":1700000000002,"sessionId":"s1","runId":"r1","type":"message_update",`+
		`"assistantMessageEvent":{"type":"toolcall_start","contentIndex":2,"id":"a","toolName":"echo"},`+
		`"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}`, string(b))

	b, err = EncodeEvent(&ToolExecutionEnd{Envelope: env(3), ToolCallID: "a", ToolName: "echo", IsError: false})
	require.NoError(t, err)
	assert.Equal(t, `{"seq":3,"ts":1700000000003,"sessionId":"s1","runId":"r1","type":"tool_execution_end",`+
		`"toolCallId":"a","toolName":"echo","result":{"content":[]},"isError":false}`, string(b))

	b, err = EncodeEvent(&TurnEnd{Envelope: env(4), Message: UserMessage{}})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"toolResults":[]`)
	b, err = EncodeEvent(&AgentEnd{Envelope: env(4)})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"messages":[]`)
}

func TestEventEnvelopeFieldsAreDecoded(t *testing.T) {
	ev, err := DecodeEvent([]byte(`{"seq":18446744073709551615,"ts":0,"sessionId":"","runId":"","type":"turn_start"}`))
	require.NoError(t, err)
	assert.Equal(t, uint64(18446744073709551615), ev.Env().Seq)
	assert.Equal(t, int64(0), ev.Env().TS)
}

func TestEnvelopeIsFilledThroughEnv(t *testing.T) {
	var ev Event = &TurnStart{}
	*ev.Env() = env(9)
	b, err := EncodeEvent(ev)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"seq":9`)
}

func TestMessageUpdateRejectsNonBlockEvents(t *testing.T) {
	start, err := MarshalStreamEvent(StartEvent{Message: seedMessage()})
	require.NoError(t, err)
	for _, inner := range []string{string(start), `{"type":"done","reason":"stop"}`, `{"type":"zzz"}`} {
		_, err := DecodeEvent([]byte(`{"seq":1,"type":"message_update","assistantMessageEvent":` + inner + `}`))
		require.Error(t, err, inner)
	}
	_, err = EncodeEvent(&MessageUpdate{})
	require.Error(t, err)
}

func TestUnknownAgentEventKeptRaw(t *testing.T) {
	src := `{"seq":4,"ts":9,"sessionId":"s","runId":"r","type":"test_unknown_event","queued":["a","b"],"n":{"x":1}}`
	ev, err := DecodeEvent([]byte(src))
	require.NoError(t, err)
	raw, ok := ev.(*RawEvent)
	require.True(t, ok)
	assert.Equal(t, "test_unknown_event", raw.EventType())
	assert.Equal(t, uint64(4), raw.Seq)
	b, err := EncodeEvent(raw)
	require.NoError(t, err)
	assert.Equal(t, src, string(b))

	b, err = EncodeEvent(&RawEvent{Envelope: env(1), Type: "x"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"seq":1,"ts":1700000000001,"sessionId":"s1","runId":"r1","type":"x"}`, string(b))
	_, err = EncodeEvent(&RawEvent{})
	require.Error(t, err)
	_, err = EncodeEvent(&RawEvent{Type: "x", Data: json.RawMessage(`[1]`)})
	require.Error(t, err)
}

func TestDecodeEventRejectsMalformed(t *testing.T) {
	for _, src := range []string{
		``, `null`, `[]`, `{}`, `{"type":""}`, `{"type":5}`,
		`{"type":"message_start"}`,
		`{"type":"message_end","message":{"role":"assistant","stopReason":"bogus"}}`,
		`{"type":"turn_end","toolResults":[]}`,
		`{"type":"turn_end","message":{"role":"user"},"toolResults":[{"role":"user"}]}`,
		`{"type":"agent_end","messages":[null]}`,
		`{"type":"agent_end","messages":[{"role":"user","content":[{"type":"zzz"}]}]}`,
		`{"type":"tool_execution_end","result":{"content":"x"}}`,
		`{"type":"agent_start"} trailing`,
	} {
		_, err := DecodeEvent([]byte(src))
		require.Error(t, err, src)
	}
}

func TestEncodeEventRejectsBadInput(t *testing.T) {
	_, err := EncodeEvent(nil)
	require.Error(t, err)
	_, err = EncodeEvent(&MessageEnd{})
	require.Error(t, err, "nil message")
	_, err = EncodeEvent(&AgentEnd{Messages: []Message{nil}})
	require.Error(t, err)
}

func TestUnknownRoleInsideEventStaysRaw(t *testing.T) {
	ev, err := DecodeEvent([]byte(`{"type":"message_end","message":{"role":"branchSummary","summary":"s"}}`))
	require.NoError(t, err)
	assert.Equal(t, "branchSummary", ev.(*MessageEnd).Message.Role())
}
