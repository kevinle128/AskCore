package protocol

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func roundTrip(t *testing.T, m Message) Message {
	t.Helper()
	b, err := MarshalMessage(m)
	require.NoError(t, err)
	back, err := UnmarshalMessage(b)
	require.NoError(t, err)
	b2, err := MarshalMessage(back)
	require.NoError(t, err)
	assert.JSONEq(t, string(b), string(b2))
	return back
}

func fullAssistant() AssistantMessage {
	lvl := ThinkingHigh
	return AssistantMessage{
		Content: []AssistantBlock{
			Thinking{Thinking: "go", ThinkingSignature: sp("sig"), Redacted: bp(false)},
			Text{Text: "ok", TextSignature: sp("")},
			ToolCall{ID: "a", Name: "echo", Arguments: json.RawMessage(`{"k":"v"}`), ThoughtSignature: sp("t"), Namespace: sp("n")},
		},
		API: "faux", Provider: "faux", Model: "faux-1",
		ResponseModel: sp("m2"), ResponseID: sp("r1"), ProviderThinkingLevel: sp("native"), ThinkingLevel: &lvl,
		Diagnostics: []Diagnostic{{Type: "retry", Timestamp: 5, Details: json.RawMessage(`{"a":1}`),
			Error: &DiagnosticError{Name: sp("E"), Message: "boom", Stack: sp("st"), Code: json.RawMessage(`"ECONN"`)}}},
		Usage:      Usage{Input: 1, Output: 2, CacheWrite1h: ip(0), TotalTokens: 3, Cost: Cost{Total: 9}},
		StopReason: StopToolUse, ErrorMessage: sp(""), RawStopReason: sp("tool_use"), EndTurn: bp(false), Timestamp: 1700,
	}
}

func TestAssistantMessageGoldenJSON(t *testing.T) {
	b, err := MarshalMessage(AssistantMessage{
		Content: []AssistantBlock{Text{Text: "hi"}}, API: "faux", Provider: "faux", Model: "faux-1",
		StopReason: StopStop, Timestamp: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, `{"role":"assistant","content":[{"type":"text","text":"hi"}],"api":"faux","provider":"faux","model":"faux-1",`+
		`"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},`+
		`"stopReason":"stop","timestamp":0}`, string(b))
}

func TestAssistantMessageRoundTripKeepsOptionalValues(t *testing.T) {
	in := fullAssistant()
	back := roundTrip(t, in).(AssistantMessage)
	assert.Equal(t, in, back)
	b, _ := MarshalMessage(in)
	for _, key := range []string{`"responseModel":"m2"`, `"responseId":"r1"`, `"providerThinkingLevel":"native"`,
		`"thinkingLevel":"high"`, `"errorMessage":""`, `"rawStopReason":"tool_use"`, `"endTurn":false`,
		`"code":"ECONN"`, `"redacted":false`, `"textSignature":""`, `"cacheWrite1h":0`} {
		assert.Contains(t, string(b), key)
	}
}

func TestAssistantMessageEmptyContentEncodesAsArray(t *testing.T) {
	b, err := MarshalMessage(AssistantMessage{StopReason: StopPending})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"content":[]`)
	assert.NotContains(t, string(b), `"diagnostics"`)
}

func TestAssistantMessageNullOrAbsentContentIsEmptySlice(t *testing.T) {
	for _, src := range []string{
		`{"role":"assistant","content":null,"stopReason":"stop"}`,
		`{"role":"assistant","stopReason":"stop"}`,
	} {
		m, err := UnmarshalMessage([]byte(src))
		require.NoError(t, err)
		c := m.(AssistantMessage).Content
		assert.NotNil(t, c)
		assert.Empty(t, c)
	}
}

func TestAssistantMessageRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"image block":         `{"role":"assistant","content":[{"type":"image","data":"x","mimeType":"image/png"}],"stopReason":"stop"}`,
		"unknown block":       `{"role":"assistant","content":[{"type":"audio"}],"stopReason":"stop"}`,
		"block without type":  `{"role":"assistant","content":[{"text":"x"}],"stopReason":"stop"}`,
		"string content":      `{"role":"assistant","content":"hi","stopReason":"stop"}`,
		"unknown stop reason": `{"role":"assistant","content":[],"stopReason":"weird"}`,
		"missing stop reason": `{"role":"assistant","content":[]}`,
		"bad tool arguments":  `{"role":"assistant","content":[{"type":"toolCall","id":"a","name":"n","arguments":[]}],"stopReason":"stop"}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := UnmarshalMessage([]byte(src))
			require.Error(t, err)
		})
	}
}

func TestAssistantMessageMarshalRejectsInvalidStopReason(t *testing.T) {
	_, err := MarshalMessage(AssistantMessage{})
	require.Error(t, err)
}

func TestUserMessageStringBecomesTextBlock(t *testing.T) {
	m, err := UnmarshalMessage([]byte(`{"role":"user","content":"hello","timestamp":7}`))
	require.NoError(t, err)
	assert.Equal(t, UserMessage{Content: []UserBlock{Text{Text: "hello"}}, Timestamp: 7}, m)
	b, err := MarshalMessage(m)
	require.NoError(t, err)
	assert.Equal(t, `{"role":"user","content":[{"type":"text","text":"hello"}],"timestamp":7}`, string(b))
}

func TestUserMessageImageOnlyHasNoEmptyText(t *testing.T) {
	in := UserMessage{Content: []UserBlock{Image{Data: "AAA=", MimeType: "image/png"}}}
	b, err := MarshalMessage(in)
	require.NoError(t, err)
	assert.Equal(t, `{"role":"user","content":[{"type":"image","data":"AAA=","mimeType":"image/png"}],"timestamp":0}`, string(b))
	assert.Equal(t, in, roundTrip(t, in))
}

func TestUserMessageNullAndEmptyContent(t *testing.T) {
	m, err := UnmarshalMessage([]byte(`{"role":"user","content":null}`))
	require.NoError(t, err)
	assert.Equal(t, []UserBlock{}, m.(UserMessage).Content)
	b, err := MarshalMessage(UserMessage{})
	require.NoError(t, err)
	assert.Equal(t, `{"role":"user","content":[],"timestamp":0}`, string(b))
}

func TestUserMessageRejectsAssistantBlocks(t *testing.T) {
	for _, blk := range []string{`{"type":"thinking","thinking":"x"}`, `{"type":"toolCall","id":"a","name":"n","arguments":{}}`, `{"type":"zzz"}`} {
		_, err := UnmarshalMessage([]byte(`{"role":"user","content":[` + blk + `]}`))
		require.Error(t, err, blk)
	}
}

func TestSystemMessageStringContentAndOrderedSections(t *testing.T) {
	src := `{"role":"system","content":"base","sections":{"z":"last-name-first","a":null,"m":"v"},` +
		`"toolsAdded":[{"name":"t","description":"d","parameters":{"type":"object"}}],"toolsRemoved":[{"name":"old"}],"timestamp":0}`
	m, err := UnmarshalMessage([]byte(src))
	require.NoError(t, err)
	sys := m.(SystemMessage)
	assert.Equal(t, []Text{{Text: "base"}}, sys.Content)
	assert.Equal(t, Sections{{"z", sp("last-name-first")}, {"a", nil}, {"m", sp("v")}}, sys.Sections)

	b, err := MarshalMessage(sys)
	require.NoError(t, err)
	assert.Equal(t, `{"role":"system","content":[{"type":"text","text":"base"}],"sections":{"z":"last-name-first","a":null,"m":"v"},`+
		`"toolsAdded":[{"name":"t","description":"d","parameters":{"type":"object"}}],"toolsRemoved":[{"name":"old"}],"timestamp":0}`, string(b))
}

func TestSystemMessageEmptyContentAndNoOptionalFields(t *testing.T) {
	b, err := MarshalMessage(SystemMessage{})
	require.NoError(t, err)
	assert.Equal(t, `{"role":"system","content":[],"timestamp":0}`, string(b))
}

func TestSystemMessageRejectsNonTextBlocks(t *testing.T) {
	_, err := UnmarshalMessage([]byte(`{"role":"system","content":[{"type":"image","data":"x","mimeType":"image/png"}]}`))
	require.Error(t, err)
}

func TestSectionsDuplicateAndErrors(t *testing.T) {
	var s Sections
	require.NoError(t, json.Unmarshal([]byte(`{"a":"1","b":"2","a":null}`), &s))
	assert.Equal(t, Sections{{"a", nil}, {"b", sp("2")}}, s)
	require.Error(t, json.Unmarshal([]byte(`{"a":1}`), &s))
	require.Error(t, json.Unmarshal([]byte(`["a"]`), &s))
	_, err := json.Marshal(Sections{{"a", nil}, {"a", sp("x")}})
	require.Error(t, err)
}

func TestToolResultMessageRoundTripAndOmissions(t *testing.T) {
	in := ToolResultMessage{
		ToolCallID: "a", ToolName: "echo",
		Content: []UserBlock{Text{Text: "out"}, Image{Data: "x", MimeType: "image/png"}},
		Details: json.RawMessage(`null`),
		Usage:   &Usage{Input: 1},
		NestedCalls: &NestedToolCalls{Complete: false, Calls: []NestedToolCallRecord{
			{ID: "n1", Name: "x", Arguments: json.RawMessage(`{}`), Status: NestedStatusOK, DurationMs: func() *float64 { f := 1.5; return &f }()},
			{ID: "n2", Name: "y", ArgumentsBytes: ip(99999), Status: NestedStatusUnfinished, Error: sp("cut")},
		}},
		IsError: true, Timestamp: 3,
	}
	back := roundTrip(t, in).(ToolResultMessage)
	assert.Equal(t, in, back)

	b, _ := MarshalMessage(in)
	assert.Contains(t, string(b), `"details":null`)
	assert.Contains(t, string(b), `{"id":"n1","name":"x","arguments":{},"status":"ok","durationMs":1.5}`)
	assert.Contains(t, string(b), `{"id":"n2","name":"y","argumentsBytes":99999,"status":"unfinished","error":"cut"}`)
	assert.Nil(t, back.NestedCalls.Calls[1].Arguments, "omitted arguments stay omitted")
}

func TestToolResultMessageDefaults(t *testing.T) {
	b, err := MarshalMessage(ToolResultMessage{ToolCallID: "a", ToolName: "t"})
	require.NoError(t, err)
	assert.Equal(t, `{"role":"toolResult","toolCallId":"a","toolName":"t","content":[],"isError":false,"timestamp":0}`, string(b))

	b, err = MarshalMessage(ToolResultMessage{NestedCalls: &NestedToolCalls{Complete: true}})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"nestedCalls":{"calls":[],"complete":true}`)

	m, err := UnmarshalMessage([]byte(`{"role":"toolResult","toolCallId":"a","toolName":"t","content":null,"isError":false}`))
	require.NoError(t, err)
	assert.Equal(t, []UserBlock{}, m.(ToolResultMessage).Content)
}

func TestToolResultMessageRejectsBadContentAndStatus(t *testing.T) {
	for _, src := range []string{
		`{"role":"toolResult","content":"text"}`,
		`{"role":"toolResult","content":[{"type":"toolCall","id":"a","name":"n","arguments":{}}]}`,
		`{"role":"toolResult","content":[],"nestedCalls":{"calls":[{"id":"a","name":"n","status":"weird"}],"complete":true}}`,
		`{"role":"toolResult","content":[],"nestedCalls":{"calls":[{"id":"a","name":"n","status":"ok","arguments":[1]}],"complete":true}}`,
	} {
		_, err := UnmarshalMessage([]byte(src))
		require.Error(t, err, src)
	}
}

func TestDiagnosticCodeForms(t *testing.T) {
	for _, tc := range []struct{ src, code string }{
		{`{"type":"t","timestamp":1,"error":{"message":"m","code":"E1"}}`, `"E1"`},
		{`{"type":"t","timestamp":1,"error":{"message":"m","code":404}}`, `404`},
		{`{"type":"t","timestamp":1,"error":{"message":"m","code":1e3}}`, `1e3`},
		{`{"type":"t","timestamp":1,"error":{"message":"m"}}`, ``},
		{`{"type":"t","timestamp":1,"error":{"message":"m","code":null}}`, ``},
	} {
		var d Diagnostic
		require.NoError(t, json.Unmarshal([]byte(tc.src), &d), tc.src)
		assert.Equal(t, tc.code, string(d.Error.Code))
		out, err := json.Marshal(d)
		require.NoError(t, err)
		if tc.code == "" {
			assert.NotContains(t, string(out), `"code"`)
		} else {
			assert.Contains(t, string(out), `"code":`+tc.code)
		}
	}
	var d Diagnostic
	require.Error(t, json.Unmarshal([]byte(`{"type":"t","error":{"message":"m","code":true}}`), &d))
	require.Error(t, json.Unmarshal([]byte(`{"type":"t","error":{"message":"m","code":{}}}`), &d))
	require.Error(t, json.Unmarshal([]byte(`{"type":"t","error":{"message":"m","code":1e999}}`), &d))
	require.Error(t, json.Unmarshal([]byte(`{"type":"t","details":[1]}`), &d))
	_, err := json.Marshal(DiagnosticError{Message: "m", Code: json.RawMessage(`1e999`)})
	require.Error(t, err)
}

func TestUnknownRoleKeptRawAndReencodes(t *testing.T) {
	src := `{"role":"bashExecution","command":"ls","timestamp":1,"extra":{"a":[1,2]}}`
	m, err := UnmarshalMessage([]byte(src))
	require.NoError(t, err)
	raw, ok := m.(RawMessage)
	require.True(t, ok)
	assert.Equal(t, "bashExecution", raw.Role())
	b, err := MarshalMessage(m)
	require.NoError(t, err)
	assert.Equal(t, src, string(b))
}

func TestMessageWithoutRoleIsError(t *testing.T) {
	for _, src := range []string{`{}`, `{"role":""}`, `{"role":5}`, `[]`, `null`} {
		_, err := UnmarshalMessage([]byte(src))
		require.Error(t, err, src)
	}
}

func TestRegisterRole(t *testing.T) {
	name := fmt.Sprintf("customNoteForTest%d", registerSeq.Add(1))
	require.NoError(t, RegisterRole(name, func(b []byte) (Message, error) {
		var c customMsg
		return c, json.Unmarshal(b, &c)
	}))
	assert.Error(t, RegisterRole(name, func([]byte) (Message, error) { return nil, nil }), "duplicate")
	for _, reserved := range []string{RoleSystem, RoleUser, RoleAssistant, RoleToolResult, ""} {
		assert.Error(t, RegisterRole(reserved, func([]byte) (Message, error) { return nil, nil }), reserved)
	}
	assert.Error(t, RegisterRole("nilDecoder", nil))

	m, err := UnmarshalMessage([]byte(`{"role":"` + name + `","note":"n"}`))
	require.NoError(t, err)
	assert.Equal(t, customMsg{RoleName: name, Note: "n"}, m)
	b, err := MarshalMessage(m)
	require.NoError(t, err)
	assert.JSONEq(t, `{"role":"`+name+`","note":"n"}`, string(b))
}

var registerSeq atomic.Int64

type customMsg struct {
	RoleName string `json:"role"`
	Note     string `json:"note"`
}

func (c customMsg) Role() string { return c.RoleName }

func TestRegisterRoleIsRaceSafe(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = RegisterRole(fmt.Sprintf("raceRole%d-%d", registerSeq.Add(1), i), func([]byte) (Message, error) { return nil, nil })
		}()
		go func() {
			defer wg.Done()
			_, _ = UnmarshalMessage([]byte(`{"role":"raceRole3"}`))
		}()
	}
	wg.Wait()
}

func TestCloneAssistantMessageIsDeep(t *testing.T) {
	orig := fullAssistant()
	c := orig.Clone()
	assert.Equal(t, orig, c)

	c.Content[2].(ToolCall).Arguments[2] = 'Z'
	*c.Content[0].(Thinking).ThinkingSignature = "changed"
	*c.Content[1].(Text).TextSignature = "changed"
	*c.ResponseID = "changed"
	*c.EndTurn = true
	*c.Usage.CacheWrite1h = 5
	c.Diagnostics[0].Details[2] = 'Z'
	c.Diagnostics[0].Error.Code[1] = 'Z'
	*c.Diagnostics[0].Error.Name = "changed"
	c.Content = append(c.Content[:1], c.Content[2:]...)

	assert.Equal(t, fullAssistant(), orig)
}

func TestCloneMessageDeepCopiesAllKinds(t *testing.T) {
	sys := SystemMessage{
		Content: []Text{{Text: "a", TextSignature: sp("s")}}, Sections: Sections{{"k", sp("v")}},
		ToolsAdded:   []ToolDecl{{Name: "t", Parameters: json.RawMessage(`{"a":1}`)}},
		ToolsRemoved: []ToolRef{{Name: "r"}},
	}
	cs := CloneMessage(sys).(SystemMessage)
	*cs.Content[0].TextSignature = "x"
	*cs.Sections[0].Value = "x"
	cs.ToolsAdded[0].Parameters[2] = 'Z'
	cs.ToolsRemoved[0].Name = "x"
	assert.Equal(t, "s", *sys.Content[0].TextSignature)
	assert.Equal(t, "v", *sys.Sections[0].Value)
	assert.JSONEq(t, `{"a":1}`, string(sys.ToolsAdded[0].Parameters))
	assert.Equal(t, "r", sys.ToolsRemoved[0].Name)

	usr := UserMessage{Content: []UserBlock{Text{Text: "a", TextSignature: sp("s")}}}
	cu := CloneMessage(usr).(UserMessage)
	*cu.Content[0].(Text).TextSignature = "x"
	assert.Equal(t, "s", *usr.Content[0].(Text).TextSignature)

	tr := ToolResultMessage{
		Content: []UserBlock{Text{Text: "a"}}, Details: json.RawMessage(`{"d":1}`), Usage: &Usage{Reasoning: ip(1)},
		NestedCalls: &NestedToolCalls{Calls: []NestedToolCallRecord{{ID: "n", Arguments: json.RawMessage(`{"a":1}`), Status: "ok", ArgumentsBytes: ip(1)}}},
	}
	ct := CloneMessage(tr).(ToolResultMessage)
	ct.Details[2] = 'Z'
	*ct.Usage.Reasoning = 9
	ct.NestedCalls.Calls[0].Arguments[2] = 'Z'
	*ct.NestedCalls.Calls[0].ArgumentsBytes = 9
	assert.JSONEq(t, `{"d":1}`, string(tr.Details))
	assert.Equal(t, int64(1), *tr.Usage.Reasoning)
	assert.JSONEq(t, `{"a":1}`, string(tr.NestedCalls.Calls[0].Arguments))
	assert.Equal(t, int64(1), *tr.NestedCalls.Calls[0].ArgumentsBytes)

	raw := RawMessage{RoleName: "x", Data: json.RawMessage(`{"role":"x"}`)}
	cr := CloneMessage(raw).(RawMessage)
	cr.Data[2] = 'Z'
	assert.Equal(t, `{"role":"x"}`, string(raw.Data))

	assert.Nil(t, CloneMessage(nil))
}

func TestMarshalMessageAcceptsPointers(t *testing.T) {
	a := fullAssistant()
	b1, err := MarshalMessage(a)
	require.NoError(t, err)
	b2, err := MarshalMessage(&a)
	require.NoError(t, err)
	assert.Equal(t, string(b1), string(b2))
	_, err = MarshalMessage(nil)
	require.Error(t, err)
}

func TestToolExecutionResultJSON(t *testing.T) {
	in := ToolExecutionResult{
		Content: []UserBlock{Text{Text: "x"}}, Details: json.RawMessage(`[1,2]`), StructuredContent: json.RawMessage(`"s"`),
		Usage: &Usage{}, IsError: bp(false), Terminate: bp(true),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":[{"type":"text","text":"x"}],"details":[1,2],"structuredContent":"s","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"isError":false,"terminate":true}`, string(b))
	var back ToolExecutionResult
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, in, back)

	b, err = json.Marshal(ToolExecutionResult{})
	require.NoError(t, err)
	assert.Equal(t, `{"content":[]}`, string(b))
}

func TestCloneMessageCopiesPointerUserAndToolResultBlocks(t *testing.T) {
	for _, kind := range []string{"user value", "user pointer", "tool result value", "tool result pointer"} {
		t.Run(kind, func(t *testing.T) {
			text := &Text{Text: "original", TextSignature: sp("signature")}
			image := &Image{Data: "YQ==", MimeType: "image/png"}
			content := []UserBlock{text, image}
			var message Message
			switch kind {
			case "user value":
				message = UserMessage{Content: content}
			case "user pointer":
				message = &UserMessage{Content: content}
			case "tool result value":
				message = ToolResultMessage{Content: content}
			case "tool result pointer":
				message = &ToolResultMessage{Content: content}
			}
			clone := CloneMessage(message)
			var copied []UserBlock
			switch value := clone.(type) {
			case UserMessage:
				copied = value.Content
			case *UserMessage:
				copied = value.Content
			case ToolResultMessage:
				copied = value.Content
			case *ToolResultMessage:
				copied = value.Content
			}
			copied[0].(*Text).Text = "changed"
			*copied[0].(*Text).TextSignature = "changed"
			copied[1].(*Image).Data = "Yg=="
			copied[1].(*Image).MimeType = "image/jpeg"
			require.Equal(t, "original", text.Text)
			require.Equal(t, "signature", *text.TextSignature)
			require.Equal(t, "YQ==", image.Data)
			require.Equal(t, "image/png", image.MimeType)
		})
	}
}
