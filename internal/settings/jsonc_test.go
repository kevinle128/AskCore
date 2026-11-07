package settings

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestStripJSONCKeepsStringsAndDropsComments(t *testing.T) {
	cases := map[string]struct {
		in   string
		want any
	}{
		"line comment":            {`{"a": 1 // note` + "\n}", map[string]any{"a": 1.0}},
		"block comment":           {`{/* x */"a": /* y */ 2}`, map[string]any{"a": 2.0}},
		"comment marks in string": {`{"a": "http://h/x // not /* a */ comment"}`, map[string]any{"a": "http://h/x // not /* a */ comment"}},
		"escaped quote":           {`{"a": "say \"// hi\" /*"}`, map[string]any{"a": `say "// hi" /*`}},
		"escaped backslash":       {`{"a": "c:\\" // end` + "\n}", map[string]any{"a": `c:\`}},
		"end marker in string":    {`{"a": "*/", /* "b": 1 */ "c": 3}`, map[string]any{"a": "*/", "c": 3.0}},
		"crlf":                    {"{\r\n\"a\": 1, // x\r\n\"b\": 2\r\n}", map[string]any{"a": 1.0, "b": 2.0}},
		"comment at end of file":  {`{"a": 1} // last`, map[string]any{"a": 1.0}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := stripJSONC([]byte(tc.in))
			if err != nil {
				t.Fatalf("strip: %v", err)
			}
			if len(out) != len(tc.in) {
				t.Fatalf("length %d, want %d: offsets must stay", len(out), len(tc.in))
			}
			if strings.Count(string(out), "\n") != strings.Count(tc.in, "\n") {
				t.Fatal("newlines must stay")
			}
			var got any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("decode %q: %v", out, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestStripJSONCTrailingCommas(t *testing.T) {
	cases := map[string]struct {
		in   string
		want any
	}{
		"object":            {`{"a": 1,}`, map[string]any{"a": 1.0}},
		"array":             {`[1, 2,]`, []any{1.0, 2.0}},
		"nested arrays":     {`{"a": [[1,], [2, [3,],],],}`, map[string]any{"a": []any{[]any{1.0}, []any{2.0, []any{3.0}}}}},
		"comment and space": {"{\"a\": 1, // x\n /* y */ }", map[string]any{"a": 1.0}},
		"comma in string":   {`{"a": ",}", "b": ",]",}`, map[string]any{"a": ",}", "b": ",]"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := stripJSONC([]byte(tc.in))
			if err != nil {
				t.Fatalf("strip: %v", err)
			}
			var got any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("decode %q: %v", out, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestStripJSONCUnterminatedCommentGivesLineOnly(t *testing.T) {
	_, err := stripJSONC([]byte("{\n\"a\": 1,\n/* secret-text"))
	if err == nil {
		t.Fatal("an unterminated comment must be an error")
	}
	if err.Error() != "unterminated comment at line 3" {
		t.Fatalf("error %q", err)
	}
}

func TestStripJSONCDoubleCommaStaysInvalid(t *testing.T) {
	out, err := stripJSONC([]byte(`[1,,]`))
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(out) {
		t.Fatal("a double comma must stay invalid JSON")
	}
}
