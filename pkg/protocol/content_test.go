package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }
func ip(i int64) *int64   { return &i }

func TestContentBlocksGoldenJSON(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"text", Text{Text: "hi"}, `{"type":"text","text":"hi"}`},
		{"text with empty signature", Text{Text: "", TextSignature: sp("")}, `{"type":"text","text":"","textSignature":""}`},
		{"thinking redacted false", Thinking{Thinking: "t", ThinkingSignature: sp("s"), Redacted: bp(false)},
			`{"type":"thinking","thinking":"t","thinkingSignature":"s","redacted":false}`},
		{"thinking redacted", Thinking{ThinkingSignature: sp("enc"), Redacted: bp(true)},
			`{"type":"thinking","thinking":"","thinkingSignature":"enc","redacted":true}`},
		{"image", Image{Data: "AAA=", MimeType: "image/png"}, `{"type":"image","data":"AAA=","mimeType":"image/png"}`},
		{"tool call", ToolCall{ID: "a", Name: "echo", Arguments: json.RawMessage(`{"x":1}`), ThoughtSignature: sp("ts"), Namespace: sp("ns")},
			`{"type":"toolCall","id":"a","name":"echo","arguments":{"x":1},"thoughtSignature":"ts","namespace":"ns"}`},
		{"tool call nil arguments", ToolCall{ID: "a", Name: "echo"}, `{"type":"toolCall","id":"a","name":"echo","arguments":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.v)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestToolCallArgumentsMustBeObject(t *testing.T) {
	_, err := json.Marshal(ToolCall{ID: "a", Name: "n", Arguments: json.RawMessage(`[1]`)})
	require.Error(t, err)

	var c ToolCall
	require.Error(t, json.Unmarshal([]byte(`{"type":"toolCall","id":"a","name":"n","arguments":"x"}`), &c))
	require.NoError(t, json.Unmarshal([]byte(`{"type":"toolCall","id":"a","name":"n","arguments":null}`), &c))
	assert.Nil(t, c.Arguments)
}

func TestBlockDiscriminatorChecked(t *testing.T) {
	var tx Text
	require.Error(t, json.Unmarshal([]byte(`{"type":"image","text":"x"}`), &tx))
	var th Thinking
	require.Error(t, json.Unmarshal([]byte(`{"type":"text","thinking":"x"}`), &th))
	var im Image
	require.Error(t, json.Unmarshal([]byte(`{"text":"x"}`), &im))
}

func TestMarshalDoesNotEscapeHTML(t *testing.T) {
	got, err := marshalJSON(Text{Text: "<a & b>"})
	require.NoError(t, err)
	assert.Equal(t, `{"type":"text","text":"<a & b>"}`, string(got))
}

func TestLoneSurrogateDecodesToReplacementCharacter(t *testing.T) {
	// Go's encoding/json turns a lone UTF-16 surrogate escape into U+FFFD.
	// Pi (JavaScript) keeps the lone surrogate. Ask text is valid UTF-8, so
	// the replacement is the intended behavior.
	var tx Text
	require.NoError(t, json.Unmarshal([]byte(`{"type":"text","text":"a\ud800b"}`), &tx))
	assert.Equal(t, "a�b", tx.Text)
}

func TestLineSeparatorsAreEscaped(t *testing.T) {
	got, err := json.Marshal(Text{Text: "a b c\nd"})
	require.NoError(t, err)
	assert.NotContains(t, string(got), " ")
	assert.NotContains(t, string(got), "\n")
}

func TestToolCallAbsentArgumentsDecodeToNil(t *testing.T) {
	var c ToolCall
	require.NoError(t, json.Unmarshal([]byte(`{"type":"toolCall","id":"a","name":"n"}`), &c))
	assert.Nil(t, c.Arguments)
}

func TestCloneAssistantBlockSharesNothing(t *testing.T) {
	text := Text{Text: "a", TextSignature: sp("s")}
	thinking := Thinking{Thinking: "b", ThinkingSignature: sp("s"), Redacted: bp(true)}
	call := ToolCall{ID: "i", Name: "n", Arguments: json.RawMessage(`{"a":1}`), ThoughtSignature: sp("t"), Namespace: sp("ns")}

	ct := CloneAssistantBlock(text).(Text)
	*ct.TextSignature = "x"
	assert.Equal(t, "s", *text.TextSignature)

	ck := CloneAssistantBlock(thinking).(Thinking)
	*ck.ThinkingSignature = "x"
	*ck.Redacted = false
	assert.Equal(t, "s", *thinking.ThinkingSignature)
	assert.True(t, *thinking.Redacted)

	cc := CloneAssistantBlock(call).(ToolCall)
	cc.Arguments[2] = 'z'
	*cc.ThoughtSignature = "x"
	*cc.Namespace = "x"
	assert.JSONEq(t, `{"a":1}`, string(call.Arguments))
	assert.Equal(t, "t", *call.ThoughtSignature)
	assert.Equal(t, "ns", *call.Namespace)
}
