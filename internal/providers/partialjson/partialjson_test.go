package partialjson

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type obj = map[string]any

func TestParseVectors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want obj
	}{
		{"V1 empty", ``, obj{}},
		{"V1 blank", "  \n\t ", obj{}},
		{"V2 brace", `{`, obj{}},
		{"V2 quote", `{"`, obj{}},
		{"V2 open key", `{"a`, obj{}},
		{"V2 key", `{"a"`, obj{}},
		{"V2 colon", `{"a":`, obj{}},
		{"V3 number", `{"a":1`, obj{"a": 1.0}},
		{"V4 comma", `{"a":1,`, obj{"a": 1.0}},
		{"V5 open string", `{"a":"x`, obj{"a": "x"}},
		{"V6 dangling backslash", `{"a":"x\`, obj{"a": "x"}},
		{"V7 only backslash", `{"a":"\`, obj{"a": ""}},
		{"V8 escaped quote", `{"a":"he said \"hi`, obj{"a": `he said "hi`}},
		{"V9 true prefix", `{"a":tr`, obj{"a": true}},
		{"V9 false prefix", `{"a":fals`, obj{"a": false}},
		{"V10 null prefix", `{"a":nul`, obj{"a": nil}},
		{"V11 minus", `{"a":-`, obj{}},
		{"V11 dot", `{"a":1.`, obj{}},
		{"V11 stops object", `{"b":1,"a":-,"c":2}`, obj{"b": 1.0}},
		{"V12 incomplete unicode", `{"a":"x\u00`, obj{"a": "x"}},
		{"V13 complete unicode", `{"a":"xé`, obj{"a": "xé"}},
		{"V14 lone surrogate", `{"a":"\ud83d\ude`, obj{"a": "�"}},
		{"V15 exponent cut", `{"a":1e`, obj{"a": 1.0}},
		{"V16 exponent", `{"a":1e5`, obj{"a": 100000.0}},
		{"V17 array", `{"a":[1,2`, obj{"a": []any{1.0, 2.0}}},
		{"V17 array comma", `{"a":[1,2,`, obj{"a": []any{1.0, 2.0}}},
		{"V18 nested string", `{"a":{"b":"c`, obj{"a": obj{"b": "c"}}},
		{"V19 half element", `{"a":[{"b":1},{"c"`, obj{"a": []any{obj{"b": 1.0}, obj{}}}},
		{"V20 edit tool", `{"edits":[{"old":"a","new":"b"}`, obj{"edits": []any{obj{"old": "a", "new": "b"}}}},
		{"V21 repair", "{\"path\":\"A\\H\",\"text\":\"col1\tcol2\"}", obj{"path": `A\H`, "text": "col1\tcol2"}},
		{"V22 garbage", `{"a":1}garbage`, obj{"a": 1.0}},
		{"V22 extra brace", `{"a":1}}`, obj{"a": 1.0}},
		{"V22 second object", `{"a":"x"} {"b":1}`, obj{"a": "x"}},
		{"V23 raw tab open string", "{\"a\":\"x\ty", obj{"a": "x\ty"}},
		{"V24 raw newline open string", "{\"a\":\"line1\nline2", obj{"a": "line1\nline2"}},
		{"V25 bad escape open", `{"a":"\q`, obj{"a": ""}},
		{"V26 bad escape closed", `{"a":"\q"}`, obj{"a": `\q`}},
		{"V27 bare key", `{a:1}`, obj{}},
		{"V27 no colon", `{"a" 1}`, obj{}},
		{"V27 no comma", `{"a":1 "b":2}`, obj{}},
		{"V28 array no comma", `{"a":[1 2]}`, obj{"a": []any{}}},
		{"V29 exponent garbage", `{"a":1e5x`, obj{"a": 1.0}},
		{"V30 NaN", `{"a":NaN`, obj{}},
		{"V30 Infinity", `{"a":Infinity`, obj{}},
		{"V30 negative Infinity", `{"a":-Infinity}`, obj{}},
		{"V31 null", `null`, obj{}},
		{"V31 number", `42`, obj{}},
		{"V31 string", `"abc"`, obj{}},
		{"V31 array", `[1,2]`, obj{}},
		{"V31 open array", `[{"a":1}`, obj{}},
		{"V32 literal prefix", `nul`, obj{}},
		{"V33 duplicate keys", `{"a":"x","a":"y"}`, obj{"a": "y"}},
		{"duplicate keys open", `{"a":"x","a":"y`, obj{"a": "y"}},
		{"exponent overflow strict path", `{"a":1e999}`, obj{}},
		{"exponent overflow tolerant path", `{"b":1,"a":1e999`, obj{"b": 1.0}},
		{"leading whitespace", "  \n{\"a\":1", obj{"a": 1.0}},
		{"complete document", `{"a":[1,{"b":null}],"c":true}`, obj{"a": []any{1.0, obj{"b": nil}}, "c": true}},
		{"raw control in closed string", "{\"a\":\"x\u0001y\"}", obj{"a": "x\u0001y"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in)
			require.NotNil(t, got)
			assert.Equal(t, tc.want, map[string]any(got))
		})
	}
}

func TestParseDeepNestingDoesNotCrash(t *testing.T) {
	in := `{"a":` + strings.Repeat("[", 200000)
	assert.NotNil(t, Parse(in))
	assert.NotNil(t, Parse(strings.Repeat(`{"a":`, 200000)))
}

var sweepDocs = []string{
	`{"path":"a/b.txt","edits":[{"old":"x","new":"y"},{"old":"é","new":"日本語"}],"n":-12.5e3,"ok":true,"none":null}`,
	"{\"s\":\"line1\\nline2\\t\\\"q\\\" \\\\ \\u00e9 \\ud83d\\ude00 emoji \U0001F600 end\",\"k\":[[1,2],[3,[4,{\"z\":false}]]]}",
	`{"a":{"b":{"c":{"d":["x",1,2,3,{"e":"f"}]}}},"g":"h"}`,
	`{"a":"x","a":"y","b":123456789}`,
}

func TestParsePrefixSweep(t *testing.T) {
	for _, doc := range sweepDocs {
		var want map[string]any
		require.NoError(t, json.Unmarshal([]byte(doc), &want))
		for n := 0; n <= len(doc); n++ {
			got := Parse(doc[:n])
			require.NotNil(t, got, "prefix %d of %q", n, doc)
			_, err := json.Marshal(got)
			require.NoError(t, err, "prefix %d", n)
		}
		assert.Equal(t, want, Parse(doc))
	}
}

func TestRepair(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"unchanged", `{"a":"b"}`, `{"a":"b"}`},
		{"tab", "{\"a\":\"x\ty\"}", `{"a":"x\ty"}`},
		{"newline and return", "\"a\nb\rc\"", `"a\nb\rc"`},
		{"backspace and form feed", "\"\b\f\"", `"\b\f"`},
		{"other control", "\"\x01\x1f\"", `"\u0001\u001f"`},
		{"control outside string", "{\n\"a\":1}", "{\n\"a\":1}"},
		{"invalid escape", `"A\H"`, `"A\\H"`},
		{"dangling backslash", `"x\`, `"x\\`},
		{"unicode escape", `"é"`, `"é"`},
		{"short unicode", `"\u00`, `"\u00`},
		{"valid escapes", `"\" \\ \/ \b \f \n \r \t"`, `"\" \\ \/ \b \f \n \r \t"`},
		{"escaped quote keeps string open", `"a\"b\q"`, `"a\"b\\q"`},
		{"invalid escape then control", "\"\\\n\"", `"\\\n"`},
		{"delete is kept", "\"\x7f\"", "\"\x7f\""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Repair(tc.in))
		})
	}
}

func TestStrictWithRepair(t *testing.T) {
	type args struct {
		Path string  `json:"path"`
		N    float64 `json:"n"`
	}
	t.Run("valid", func(t *testing.T) {
		var a args
		require.NoError(t, StrictWithRepair([]byte(`{"path":"p","n":2}`), &a))
		assert.Equal(t, args{"p", 2}, a)
	})
	t.Run("repairable", func(t *testing.T) {
		var a args
		require.NoError(t, StrictWithRepair([]byte("{\"path\":\"A\\H\tz\",\"n\":1}"), &a))
		assert.Equal(t, args{"A\\H\tz", 1}, a)
	})
	t.Run("incomplete is rejected", func(t *testing.T) {
		var a args
		assert.Error(t, StrictWithRepair([]byte(`{"path":"p`), &a))
	})
	t.Run("unrepairable", func(t *testing.T) {
		var a args
		assert.Error(t, StrictWithRepair([]byte("{path:1}\t"), &a))
	})
	t.Run("repair does not fix structure", func(t *testing.T) {
		var a args
		assert.Error(t, StrictWithRepair([]byte("{\"path\":\"x\t\""), &a))
	})
	t.Run("empty", func(t *testing.T) {
		var a args
		assert.Error(t, StrictWithRepair(nil, &a))
	})
}
