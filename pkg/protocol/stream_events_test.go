package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedMessage() AssistantMessage {
	return AssistantMessage{API: "faux", Provider: "faux", Model: "faux-1", StopReason: StopPending, Content: []AssistantBlock{}, Timestamp: 5}
}

func TestStreamEventGoldenJSON(t *testing.T) {
	final := seedMessage()
	final.StopReason = StopToolUse
	aborted := seedMessage()
	aborted.StopReason = StopAborted
	msgJSON := func(m AssistantMessage) string {
		b, err := MarshalMessage(m)
		require.NoError(t, err)
		return string(b)
	}
	cases := []struct {
		ev   AssistantMessageEvent
		want string
	}{
		{StartEvent{Message: seedMessage()}, `{"type":"start","message":` + msgJSON(seedMessage()) + `}`},
		{TextStartEvent{ContentIndex: 1, Content: Text{Text: "a"}}, `{"type":"text_start","contentIndex":1,"content":{"type":"text","text":"a"}}`},
		{TextDeltaEvent{ContentIndex: 0, Delta: ""}, `{"type":"text_delta","contentIndex":0,"delta":""}`},
		{TextEndEvent{ContentIndex: 1, Content: "ok", TextSignature: sp("")}, `{"type":"text_end","contentIndex":1,"content":"ok","textSignature":""}`},
		{ThinkingStartEvent{ContentIndex: 0, Content: Thinking{ThinkingSignature: sp("e"), Redacted: bp(true)}},
			`{"type":"thinking_start","contentIndex":0,"content":{"type":"thinking","thinking":"","thinkingSignature":"e","redacted":true}}`},
		{ThinkingDeltaEvent{ContentIndex: 0, Delta: "go"}, `{"type":"thinking_delta","contentIndex":0,"delta":"go"}`},
		{ThinkingEndEvent{ContentIndex: 0, Content: "go", Redacted: bp(false)}, `{"type":"thinking_end","contentIndex":0,"content":"go","redacted":false}`},
		{ToolCallStartEvent{ContentIndex: 2, ID: "a", ToolName: "echo"}, `{"type":"toolcall_start","contentIndex":2,"id":"a","toolName":"echo"}`},
		{ToolCallStartEvent{ContentIndex: 2, ID: "a", ToolName: "echo", Arguments: json.RawMessage(`{}`), ThoughtSignature: sp("t"), Namespace: sp("n")},
			`{"type":"toolcall_start","contentIndex":2,"id":"a","toolName":"echo","arguments":{},"thoughtSignature":"t","namespace":"n"}`},
		{ToolCallDeltaEvent{ContentIndex: 2, Delta: "{}"}, `{"type":"toolcall_delta","contentIndex":2,"delta":"{}"}`},
		{ToolCallEndEvent{ContentIndex: 2, ToolCall: ToolCall{ID: "a", Name: "echo", Arguments: json.RawMessage(`{}`)}},
			`{"type":"toolcall_end","contentIndex":2,"toolCall":{"type":"toolCall","id":"a","name":"echo","arguments":{}}}`},
		{DoneEvent{Reason: StopToolUse, Message: final}, `{"type":"done","reason":"toolUse","message":` + msgJSON(final) + `}`},
		{ErrorEvent{Reason: StopAborted, Error: aborted}, `{"type":"error","reason":"aborted","error":` + msgJSON(aborted) + `}`},
	}
	for _, tc := range cases {
		t.Run(tc.ev.EventType(), func(t *testing.T) {
			b, err := MarshalStreamEvent(tc.ev)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
			back, err := UnmarshalStreamEvent(b)
			require.NoError(t, err)
			assert.Equal(t, tc.ev, back)
		})
	}
}

func TestStreamEventContentIndexZeroIsKept(t *testing.T) {
	b, err := MarshalStreamEvent(TextDeltaEvent{ContentIndex: 0, Delta: "x"})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"contentIndex":0`)
}

func TestStreamEventTerminalReasonRules(t *testing.T) {
	m := seedMessage()
	m.StopReason = StopStop
	for _, r := range []StopReason{StopStop, StopLength, StopToolUse, StopDeferred} {
		mr := seedMessage()
		mr.StopReason = r
		_, err := MarshalStreamEvent(DoneEvent{Reason: r, Message: mr})
		require.NoError(t, err, r)
	}
	for _, r := range []StopReason{StopPending, StopError, StopAborted, "", "bogus"} {
		_, err := MarshalStreamEvent(DoneEvent{Reason: r, Message: m})
		require.Error(t, err, "done %q", r)
	}
	for _, r := range []StopReason{StopError, StopAborted} {
		mr := seedMessage()
		mr.StopReason = r
		_, err := MarshalStreamEvent(ErrorEvent{Reason: r, Error: mr})
		require.NoError(t, err, r)
	}
	// A terminal reason that differs from the message stop reason is invalid
	// data, so encode rejects it the same way decode does.
	_, err := MarshalStreamEvent(DoneEvent{Reason: StopToolUse, Message: m})
	require.Error(t, err)
	_, err = MarshalStreamEvent(ErrorEvent{Reason: StopError, Error: m})
	require.Error(t, err)
	for _, r := range []StopReason{StopPending, StopStop, StopLength, StopToolUse, StopDeferred, ""} {
		_, err := MarshalStreamEvent(ErrorEvent{Reason: r, Error: m})
		require.Error(t, err, "error %q", r)
	}

	mj, _ := MarshalMessage(m)
	for _, src := range []string{
		`{"type":"done","reason":"error","message":` + string(mj) + `}`,
		`{"type":"done","reason":"pending","message":` + string(mj) + `}`,
		`{"type":"error","reason":"stop","error":` + string(mj) + `}`,
		`{"type":"error","reason":"toolUse","error":` + string(mj) + `}`,
	} {
		_, err := UnmarshalStreamEvent([]byte(src))
		require.Error(t, err, src)
	}
}

func TestUnmarshalStreamEventRejectsMalformed(t *testing.T) {
	cases := []string{
		`{}`,
		`{"type":"nope"}`,
		`{"type":"text_delta","delta":"x"}`,
		`{"type":"text_delta","contentIndex":-1,"delta":"x"}`,
		`{"type":"text_delta","contentIndex":0}`,
		`{"type":"text_start","contentIndex":0,"content":{"type":"thinking","thinking":"x"}}`,
		`{"type":"text_start","contentIndex":0,"content":{"type":"thinking","thinking":"x"}}`,
		`{"type":"text_end","contentIndex":0,"content":5}`,
		`{"type":"thinking_end","contentIndex":0}`,
		`{"type":"toolcall_start","contentIndex":0,"id":"a"}`,
		`{"type":"toolcall_start","contentIndex":0,"id":"a","toolName":"n","arguments":[1]}`,
		`{"type":"toolcall_end","contentIndex":0,"toolCall":{"type":"text","text":"x"}}`,
		`{"type":"start"}`,
		`{"type":"done","reason":"stop"}`,
		`{"type":"error","reason":"error","message":{"role":"assistant","stopReason":"error"}}`,
	}
	for _, src := range cases {
		_, err := UnmarshalStreamEvent([]byte(src))
		require.Error(t, err, src)
	}
}

func TestMarshalStreamEventRejectsBadInput(t *testing.T) {
	_, err := MarshalStreamEvent(nil)
	require.Error(t, err)
	_, err = MarshalStreamEvent(&TextDeltaEvent{})
	require.Error(t, err, "pointer events are not supported")
	_, err = MarshalStreamEvent(ToolCallStartEvent{Arguments: json.RawMessage(`[]`)})
	require.Error(t, err)
}

func TestOnlyNineEventsAreBlockEvents(t *testing.T) {
	blocks := []AssistantMessageEvent{
		TextStartEvent{}, TextDeltaEvent{}, TextEndEvent{}, ThinkingStartEvent{}, ThinkingDeltaEvent{},
		ThinkingEndEvent{}, ToolCallStartEvent{}, ToolCallDeltaEvent{}, ToolCallEndEvent{},
	}
	for _, ev := range blocks {
		_, ok := ev.(BlockEvent)
		assert.True(t, ok, ev.EventType())
	}
	for _, ev := range []AssistantMessageEvent{StartEvent{}, DoneEvent{}, ErrorEvent{}} {
		_, ok := ev.(BlockEvent)
		assert.False(t, ok, ev.EventType())
	}
}

func TestStreamEventDecodeRejectsReasonThatDiffersFromMessage(t *testing.T) {
	stopped := seedMessage()
	stopped.StopReason = StopStop
	msg, err := MarshalMessage(stopped)
	require.NoError(t, err)
	_, err = UnmarshalStreamEvent([]byte(`{"type":"done","reason":"length","message":` + string(msg) + `}`))
	require.Error(t, err)
	_, err = UnmarshalStreamEvent([]byte(`{"type":"error","reason":"error","error":` + string(msg) + `}`))
	require.Error(t, err)

	failed := seedMessage()
	failed.StopReason = StopError
	msg, err = MarshalMessage(failed)
	require.NoError(t, err)
	_, err = UnmarshalStreamEvent([]byte(`{"type":"error","reason":"aborted","error":` + string(msg) + `}`))
	require.Error(t, err)
	_, err = UnmarshalStreamEvent([]byte(`{"type":"error","reason":"error","error":` + string(msg) + `}`))
	require.NoError(t, err)
}
