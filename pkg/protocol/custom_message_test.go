package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleCustom() CustomMessage {
	return CustomMessage{
		CustomType: "task-notification",
		Content:    []UserBlock{Text{Text: "task done"}, Image{Data: "AAAA", MimeType: "image/png"}},
		Display:    true,
		Details:    json.RawMessage(`{"taskId":"t-1","status":"completed"}`),
		Timestamp:  42,
	}
}

func TestCustomMessageRoundTrip(t *testing.T) {
	m := sampleCustom()
	for name, in := range map[string]Message{"value": m, "pointer": &m} {
		t.Run(name, func(t *testing.T) {
			b, err := MarshalMessage(in)
			require.NoError(t, err)
			assert.Contains(t, string(b), `"role":"custom"`)
			assert.Contains(t, string(b), `"customType":"task-notification"`)
			assert.NotContains(t, string(b), "\n")

			back, err := UnmarshalMessage(b)
			require.NoError(t, err)
			assert.Equal(t, m, back)
			assert.Equal(t, RoleCustom, back.Role())
		})
	}

	t.Run("string content is one text block", func(t *testing.T) {
		back, err := UnmarshalMessage([]byte(`{"role":"custom","customType":"note","content":"hello","display":false,"timestamp":7}`))
		require.NoError(t, err)
		assert.Equal(t, CustomMessage{CustomType: "note", Content: []UserBlock{Text{Text: "hello"}}, Timestamp: 7}, back)
	})
	t.Run("absent and null details are nil", func(t *testing.T) {
		for _, src := range []string{
			`{"role":"custom","customType":"note","content":[],"display":true,"timestamp":1}`,
			`{"role":"custom","customType":"note","content":[],"display":true,"details":null,"timestamp":1}`,
		} {
			back, err := UnmarshalMessage([]byte(src))
			require.NoError(t, err)
			assert.Nil(t, back.(CustomMessage).Details)
		}
	})
	t.Run("details that is not an object is rejected", func(t *testing.T) {
		for _, details := range []string{`"text"`, `[1]`, `7`, `true`} {
			_, err := UnmarshalMessage([]byte(`{"role":"custom","customType":"note","content":[],"details":` + details + `}`))
			require.Error(t, err, details)
		}
		_, err := MarshalMessage(CustomMessage{CustomType: "note", Details: json.RawMessage(`[1]`)})
		require.Error(t, err)
	})
	t.Run("nil content encodes as an empty array", func(t *testing.T) {
		b, err := MarshalMessage(CustomMessage{CustomType: "note"})
		require.NoError(t, err)
		assert.JSONEq(t, `{"role":"custom","customType":"note","content":[],"display":false,"timestamp":0}`, string(b))
	})
	t.Run("marshal without a type is rejected", func(t *testing.T) {
		_, err := MarshalMessage(CustomMessage{})
		require.Error(t, err)
	})
	t.Run("another role is rejected by the typed decoder", func(t *testing.T) {
		var c CustomMessage
		require.Error(t, c.UnmarshalJSON([]byte(`{"role":"user","customType":"x","content":[]}`)))
	})
}

func TestCustomRoleWithoutTypeIsAnError(t *testing.T) {
	for _, src := range []string{
		`{"role":"custom"}`,
		`{"role":"custom","customType":"","content":[]}`,
		`{"role":"custom","customType":null,"content":[]}`,
	} {
		m, err := UnmarshalMessage([]byte(src))
		require.Error(t, err, src)
		assert.Nil(t, m)
	}
}

func TestRegisterRoleRejectsCustom(t *testing.T) {
	err := RegisterRole("custom", func([]byte) (Message, error) { return nil, nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestCloneCustomMessageIsDeep(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		orig := sampleCustom()
		c := CloneMessage(orig).(CustomMessage)
		assert.Equal(t, orig, c)

		c.Content[0] = Text{Text: "changed"}
		c.Details[2] = 'X'
		assert.Equal(t, Text{Text: "task done"}, orig.Content[0])
		assert.JSONEq(t, `{"taskId":"t-1","status":"completed"}`, string(orig.Details))
	})
	t.Run("pointer", func(t *testing.T) {
		orig := sampleCustom()
		c := CloneMessage(&orig).(*CustomMessage)
		assert.NotSame(t, &orig, c)
		assert.Equal(t, orig, *c)

		c.Content[0] = Text{Text: "changed"}
		c.Details[2] = 'X'
		assert.Equal(t, Text{Text: "task done"}, orig.Content[0])
		assert.JSONEq(t, `{"taskId":"t-1","status":"completed"}`, string(orig.Details))
	})
	t.Run("nil pointer and nil fields stay as they are", func(t *testing.T) {
		var p *CustomMessage
		assert.Equal(t, Message(p), CloneMessage(p))
		c := CloneMessage(CustomMessage{CustomType: "x"}).(CustomMessage)
		assert.Nil(t, c.Content)
		assert.Nil(t, c.Details)
	})
}
