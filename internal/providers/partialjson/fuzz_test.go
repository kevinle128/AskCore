package partialjson

import (
	"encoding/json"
	"math"
	"testing"
)

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		``, `{`, `{"a":1`, `{"a":"x\`, `{"a":"\ud83d\ude`, `{"a":[{"b":1},{"c"`,
		"{\"a\":\"x\ty", `{"a":1e999}`, `{"a":-`, `[1,2]`, `null`, `{"a":NaN`,
		`{"a":tr`, `{"a":"\q`, "\xff\xfe{\"a\":\"\xc3",
	} {
		f.Add(s)
	}
	for _, d := range sweepDocs {
		f.Add(d)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := Parse(s)
		if got == nil {
			t.Fatal("Parse returned nil")
		}
		if !finite(got) {
			t.Fatalf("non-finite value in result for %q", s)
		}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("result does not marshal for %q: %v", s, err)
		}
		if !json.Valid(out) {
			t.Fatalf("marshal output is not valid JSON for %q", s)
		}
		_ = Repair(s)
	})
}

func finite(v any) bool {
	switch x := v.(type) {
	case float64:
		return !math.IsInf(x, 0) && !math.IsNaN(x)
	case map[string]any:
		for _, e := range x {
			if !finite(e) {
				return false
			}
		}
	case []any:
		for _, e := range x {
			if !finite(e) {
				return false
			}
		}
	}
	return true
}
