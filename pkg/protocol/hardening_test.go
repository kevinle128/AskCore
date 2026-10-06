package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndentedUnknownEventStaysOneJSONLLine(t *testing.T) {
	ev, err := DecodeEvent([]byte("{\n  \"type\": \"test_unknown_event\",\n  \"steering\": [\"a\"]\n}"))
	require.NoError(t, err)
	require.IsType(t, &RawEvent{}, ev)
	var buf bytes.Buffer
	require.NoError(t, NewJSONLWriter(&buf).Write(ev))
	assert.Equal(t, 1, strings.Count(buf.String(), "\n"), "output: %q", buf.String())
	line, err := NewJSONLReader(&buf).Next()
	require.NoError(t, err)
	back, err := DecodeEvent(line)
	require.NoError(t, err)
	assert.JSONEq(t, string(ev.(*RawEvent).Data), string(back.(*RawEvent).Data))
}

func TestIndentedUnknownRoleStaysOneLine(t *testing.T) {
	m, err := UnmarshalMessage([]byte("{\n  \"role\": \"custom\",\n  \"x\": 1\n}"))
	require.NoError(t, err)
	require.IsType(t, RawMessage{}, m)
	for _, msg := range []Message{m, &RawMessage{RoleName: "custom", Data: m.(RawMessage).Data}} {
		out, err := MarshalMessage(msg)
		require.NoError(t, err)
		assert.NotContains(t, string(out), "\n")
		assert.JSONEq(t, `{"role":"custom","x":1}`, string(out))
	}
	_, err = MarshalMessage(RawMessage{RoleName: "custom", Data: json.RawMessage(`{"role":`)})
	assert.Error(t, err)
}

func TestDiagnosticErrorWithBlankCodeIsAnError(t *testing.T) {
	assert.NotPanics(t, func() {
		_, err := json.Marshal(DiagnosticError{Message: "m", Code: json.RawMessage(" ")})
		assert.Error(t, err)
	})
}

func TestDecodeStartEventsWithoutInitialContent(t *testing.T) {
	ev, err := UnmarshalStreamEvent([]byte(`{"type":"text_start","contentIndex":0}`))
	require.NoError(t, err)
	assert.Equal(t, TextStartEvent{ContentIndex: 0}, ev)
	ev, err = UnmarshalStreamEvent([]byte(`{"type":"thinking_start","contentIndex":2,"content":null}`))
	require.NoError(t, err)
	assert.Equal(t, ThinkingStartEvent{ContentIndex: 2}, ev)
}

func TestBuilderMessageEndBeforeStartAcceptsOnlyFailures(t *testing.T) {
	end := func(r StopReason) *MessageEnd {
		m := seedMessage()
		m.StopReason = r
		return &MessageEnd{Message: m}
	}
	for _, r := range []StopReason{StopStop, StopToolUse, StopLength} {
		assert.Error(t, NewBuilder().ApplyAgentEvent(end(r)), "reason %s", r)
	}
	for _, r := range []StopReason{StopError, StopAborted} {
		b := NewBuilder()
		require.NoError(t, b.ApplyAgentEvent(end(r)), "reason %s", r)
		_, ok := b.Result()
		assert.True(t, ok)
	}
}

func TestBuilderRejectsStartWithTerminalStopReason(t *testing.T) {
	m := seedMessage()
	m.StopReason = StopStop
	b := NewBuilder()
	assert.Error(t, b.Apply(StartEvent{Message: m}))
	assert.False(t, b.Started())
	assert.Error(t, NewBuilder().ApplyAgentEvent(&MessageStart{Message: m}))
	require.NoError(t, NewBuilder().Apply(StartEvent{Message: seedMessage()}))
}

func TestCloneMessageKeepsPointerKind(t *testing.T) {
	msgs := []Message{
		&UserMessage{Content: []UserBlock{Text{Text: "a"}}},
		&SystemMessage{Content: []Text{{Text: "a"}}},
		&AssistantMessage{Content: []AssistantBlock{Text{Text: "a"}}},
		&ToolResultMessage{ToolCallID: "x", Content: []UserBlock{Text{Text: "a"}}},
		&RawMessage{RoleName: "custom", Data: json.RawMessage(`{"role":"custom"}`)},
	}
	for _, m := range msgs {
		c := CloneMessage(m)
		assert.IsType(t, m, c)
		assert.NotSame(t, m, c)
		assert.Equal(t, m, c)
	}
	u := &UserMessage{Content: []UserBlock{Text{Text: "a"}}}
	c := CloneMessage(u).(*UserMessage)
	c.Content[0] = Text{Text: "changed"}
	assert.Equal(t, Text{Text: "a"}, u.Content[0], "the copy is deep")
	assert.IsType(t, UserMessage{}, CloneMessage(UserMessage{}))
}

func TestBuilderSeedWithoutStopReasonIsPending(t *testing.T) {
	b := NewBuilder()
	require.NoError(t, b.Apply(StartEvent{Message: AssistantMessage{API: "a", Provider: "p", Model: "m"}}))
	snap := b.Snapshot()
	require.Equal(t, StopPending, snap.StopReason)
	_, err := MarshalMessage(snap)
	require.NoError(t, err)
}

func TestRawEventEnvelopeChangesAreWrittenBack(t *testing.T) {
	ev, err := DecodeEvent([]byte(`{"seq":1,"ts":5,"sessionId":"s","runId":"r","type":"test_unknown_event","extra":{"k":[1,2]}}`))
	require.NoError(t, err)
	env := ev.Env()
	env.Seq = 42
	env.RunID = "r2"
	out, err := EncodeEvent(ev)
	require.NoError(t, err)
	back, err := DecodeEvent(out)
	require.NoError(t, err)
	raw, ok := back.(*RawEvent)
	require.True(t, ok)
	require.Equal(t, uint64(42), raw.Seq)
	require.Equal(t, "r2", raw.RunID)
	require.Equal(t, "s", raw.SessionID)
	require.Equal(t, "test_unknown_event", raw.Type)
	require.Contains(t, string(out), `"extra":{"k":[1,2]}`)
}
