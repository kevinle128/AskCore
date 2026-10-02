// Package partialjson parses JSON that can be incomplete or slightly wrong.
// Model providers stream tool-call arguments as text fragments. This package
// turns such text into a Go value without failing.
package partialjson

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// maxDepth is the nesting limit of the tolerant parser. It is the same limit
// as encoding/json and it protects the call stack.
const maxDepth = 10000

// jsonBlank holds the JSON whitespace characters. It is not the same set as
// the one that strings.TrimSpace uses.
const jsonBlank = " \t\r\n"

// errInvalid means the tolerant parser cannot read the text at this point.
var errInvalid = errors.New("partialjson: invalid or incomplete value")

// Parse reads s as a JSON object and never panics. It returns a non-nil map.
// When s cannot be read as an object, the map is empty.
//
// Order of work:
//  1. Blank text gives an empty map.
//  2. A strict decode of s, then of Repair(s) if Repair changed s.
//  3. A tolerant decode of s.
//  4. A tolerant decode of Repair(s), when step 3 failed or gave an empty object.
//
// Numbers are float64. A number that is not finite, for example 1e999, is
// treated as a malformed number. A repeated key keeps the last value.
// A partial result can hold a number that is shorter than the final number, so
// a caller must not run a tool with a result from incomplete text.
func Parse(s string) map[string]any {
	if strings.Trim(s, jsonBlank) == "" {
		return map[string]any{}
	}
	if m, ok := strictObject(s); ok {
		return m
	}
	repaired := Repair(s)
	changed := repaired != s
	if changed {
		if m, ok := strictObject(repaired); ok {
			return m
		}
	}

	v, err := tolerant(s)
	if m, ok := v.(map[string]any); err == nil && ok && len(m) > 0 {
		return m
	}
	if changed {
		v2, err2 := tolerant(repaired)
		if m, ok := v2.(map[string]any); err2 == nil && ok {
			return m
		}
	}
	return map[string]any{}
}

// StrictWithRepair decodes complete JSON from data into v. When the first
// decode fails, it decodes Repair(data) once. It never accepts incomplete
// text. If both decodes fail, it returns the error of the first decode. A
// failed decode can leave v partly filled, so callers must discard v on error.
func StrictWithRepair(data []byte, v any) error {
	err := json.Unmarshal(data, v)
	if err == nil {
		return nil
	}
	repaired := Repair(string(data))
	if repaired == string(data) {
		return err
	}
	if json.Unmarshal([]byte(repaired), v) != nil {
		return err
	}
	return nil
}

func strictObject(s string) (map[string]any, bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// tolerant parses the first JSON value in s and ignores text after it.
func tolerant(s string) (any, error) {
	p := parser{s: strings.Trim(s, jsonBlank)}
	if p.s == "" {
		return nil, errInvalid
	}
	return p.value(0)
}

type parser struct {
	s string
	i int
}

func (p *parser) skipBlank() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\r', '\n':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errInvalid
	}
	p.skipBlank()
	if p.i >= len(p.s) {
		return nil, errInvalid
	}
	switch p.s[p.i] {
	case '"':
		return p.str()
	case '{':
		return p.object(depth)
	case '[':
		return p.array(depth)
	}
	if v, ok := p.literal(); ok {
		return v, nil
	}
	return p.number()
}

// literal reads null, true or false. It also accepts a prefix of a literal
// when the text ends inside the word.
func (p *parser) literal() (any, bool) {
	rest := p.s[p.i:]
	for _, l := range [...]struct {
		text string
		val  any
	}{{"null", nil}, {"true", true}, {"false", false}} {
		if strings.HasPrefix(rest, l.text) {
			p.i += len(l.text)
			return l.val, true
		}
		if len(rest) < len(l.text) && strings.HasPrefix(l.text, rest) {
			p.i = len(p.s)
			return l.val, true
		}
	}
	return nil, false
}

// str reads a string literal. An open string is closed. A dangling backslash
// or an incomplete escape at the end is cut.
func (p *parser) str() (string, error) {
	start := p.i
	j := start + 1
	closed := false
	for j < len(p.s) {
		c := p.s[j]
		if c == '"' {
			closed = true
			j++
			break
		}
		if c == '\\' {
			j += 2
			continue
		}
		j++
	}
	end := min(j, len(p.s))
	raw := p.s[start:end]
	p.i = end

	if closed {
		return unquote(raw)
	}
	if v, err := unquote(raw + `"`); err == nil {
		return v, nil
	}
	k := strings.LastIndexByte(raw, '\\')
	if k < 0 {
		return "", errInvalid
	}
	return unquote(raw[:k] + `"`)
}

func unquote(quoted string) (string, error) {
	var out string
	if err := json.Unmarshal([]byte(quoted), &out); err != nil {
		return "", errInvalid
	}
	return out, nil
}

// number reads text up to the next comma, bracket or brace. If that text is
// not a number, it tries the text before the last "e".
func (p *parser) number() (any, error) {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != ',' && p.s[p.i] != ']' && p.s[p.i] != '}' {
		p.i++
	}
	tok := p.s[start:p.i]
	f, wellFormed, ok := jsonNumber(tok)
	if ok {
		return f, nil
	}
	if wellFormed {
		// A number such as 1e999 is complete but not finite.
		return nil, errInvalid
	}
	if k := strings.LastIndexByte(tok, 'e'); k >= 0 {
		if f, _, ok := jsonNumber(tok[:k]); ok {
			return f, nil
		}
	}
	return nil, errInvalid
}

// jsonNumber reads a complete JSON number. wellFormed is true when the text
// has the syntax of a number. ok is true when it is also finite.
func jsonNumber(tok string) (f float64, wellFormed, ok bool) {
	tok = strings.Trim(tok, jsonBlank)
	if tok == "" || (tok[0] != '-' && (tok[0] < '0' || tok[0] > '9')) {
		return 0, false, false
	}
	if !json.Valid([]byte(tok)) {
		return 0, false, false
	}
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, true, false
	}
	return f, true, true
}

// object returns the members read so far when it meets an error or the end of
// the text.
func (p *parser) object(depth int) (any, error) {
	obj := map[string]any{}
	p.i++
	p.skipBlank()
	for p.i < len(p.s) && p.s[p.i] != '}' {
		if p.s[p.i] != '"' {
			return obj, nil
		}
		key, err := p.str()
		if err != nil {
			return obj, nil
		}
		p.skipBlank()
		// The byte at the colon position is skipped without a check.
		p.i = min(p.i+1, len(p.s))
		val, err := p.value(depth + 1)
		if err != nil {
			return obj, nil
		}
		obj[key] = val
		p.skipBlank()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		}
		p.skipBlank()
	}
	if p.i < len(p.s) {
		p.i++
	}
	return obj, nil
}

// array returns the elements read so far when it meets an error or the end of
// the text.
func (p *parser) array(depth int) (any, error) {
	arr := []any{}
	p.i++
	p.skipBlank()
	for p.i < len(p.s) && p.s[p.i] != ']' {
		val, err := p.value(depth + 1)
		if err != nil {
			return arr, nil
		}
		arr = append(arr, val)
		p.skipBlank()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		}
		p.skipBlank()
	}
	if p.i < len(p.s) {
		p.i++
	}
	return arr, nil
}
