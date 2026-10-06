package tools

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// shape is the part of a parameters schema that coercion reads, built once at
// Register next to the compiled schema. It keeps "type" in declaration order,
// which the compiled schema loses, because the first listed type that changes
// a value wins a multi-type coercion.
type shape struct {
	sch         *jsonschema.Schema
	types       []string
	ref         bool
	acceptsNull bool
	props       map[string]*shape
	additional  *shape
	items       *shape
	tuple       []*shape
	allOf       []*shape
	anyOf       []*shape
	oneOf       []*shape
}

func buildShape(raw any, sch *jsonschema.Schema) *shape {
	s := &shape{sch: sch, acceptsNull: sch.Validate(nil) == nil}
	m, ok := raw.(map[string]any)
	if !ok {
		return s
	}
	switch t := m["type"].(type) {
	case string:
		s.types = []string{t}
	case []any:
		for _, v := range t {
			if name, ok := v.(string); ok {
				s.types = append(s.types, name)
			}
		}
	}
	_, s.ref = m["$ref"].(string)
	if props, ok := m["properties"].(map[string]any); ok && sch.Properties != nil {
		s.props = make(map[string]*shape, len(props))
		for k, v := range props {
			if p := sch.Properties[k]; p != nil {
				s.props[k] = buildShape(v, p)
			}
		}
	}
	if ap, ok := sch.AdditionalProperties.(*jsonschema.Schema); ok {
		s.additional = buildShape(m["additionalProperties"], ap)
	}
	switch items := m["items"].(type) {
	case map[string]any:
		if sch.Items2020 != nil {
			s.items = buildShape(items, sch.Items2020)
		} else if one, ok := sch.Items.(*jsonschema.Schema); ok {
			s.items = buildShape(items, one)
		}
	case []any:
		if list, ok := sch.Items.([]*jsonschema.Schema); ok && len(list) == len(items) {
			s.tuple = buildShapes(items, list)
		}
	}
	s.allOf = buildShapes(m["allOf"], sch.AllOf)
	s.anyOf = buildShapes(m["anyOf"], sch.AnyOf)
	s.oneOf = buildShapes(m["oneOf"], sch.OneOf)
	return s
}

func buildShapes(raw any, schs []*jsonschema.Schema) []*shape {
	list, ok := raw.([]any)
	if !ok || len(list) != len(schs) {
		return nil
	}
	out := make([]*shape, len(list))
	for i, v := range list {
		out[i] = buildShape(v, schs[i])
	}
	return out
}

// dropOptionalNulls deletes a null property that is not required, is not a
// $ref, and whose schema rejects null.
func dropOptionalNulls(v any, s *shape) {
	switch v := v.(type) {
	case []any:
		if s.tuple != nil {
			for i := range min(len(v), len(s.tuple)) {
				dropOptionalNulls(v[i], s.tuple[i])
			}
		} else if s.items != nil {
			for _, item := range v {
				dropOptionalNulls(item, s.items)
			}
		}
	case map[string]any:
		for k, p := range s.props {
			pv, ok := v[k]
			if !ok {
				continue
			}
			if pv == nil && !p.ref && !p.acceptsNull && !slices.Contains(s.sch.Required, k) {
				delete(v, k)
				continue
			}
			dropOptionalNulls(pv, p)
		}
	}
}

// coerce converts v toward the schema types. Maps and slices are changed in
// place; the returned value replaces v.
func coerce(v any, s *shape) any {
	for _, arm := range s.allOf {
		v = coerce(v, arm)
	}
	if s.anyOf != nil {
		v = coerceUnion(v, s.anyOf)
	}
	if s.oneOf != nil {
		v = coerceUnion(v, s.oneOf)
	}
	matchesOne := len(s.types) > 1 && slices.ContainsFunc(s.types, func(t string) bool { return isType(v, t) })
	if !matchesOne {
		for _, t := range s.types {
			if c, changed := coercePrimitive(v, t); changed {
				v = c
				break
			}
		}
	}
	if m, ok := v.(map[string]any); ok && slices.Contains(s.types, "object") {
		for k, p := range s.props {
			if pv, ok := m[k]; ok {
				m[k] = coerce(pv, p)
			}
		}
		if s.additional != nil {
			for k, pv := range m {
				if _, declared := s.props[k]; !declared {
					m[k] = coerce(pv, s.additional)
				}
			}
		}
	}
	if a, ok := v.([]any); ok && slices.Contains(s.types, "array") {
		if s.tuple != nil {
			for i := range min(len(a), len(s.tuple)) {
				a[i] = coerce(a[i], s.tuple[i])
			}
		} else if s.items != nil {
			for i := range a {
				a[i] = coerce(a[i], s.items)
			}
		}
	}
	return v
}

// coerceUnion keeps v when an arm already accepts it. Otherwise the first arm
// that accepts its own coercion of v wins.
func coerceUnion(v any, arms []*shape) any {
	for _, arm := range arms {
		if arm.sch.Validate(v) == nil {
			return v
		}
	}
	for _, arm := range arms {
		if c := coerce(cloneValue(v), arm); arm.sch.Validate(c) == nil {
			return c
		}
	}
	return v
}

// coercePrimitive is the D21 table. It reports whether it changed v.
func coercePrimitive(v any, typ string) (any, bool) {
	switch typ {
	case "number", "integer":
		switch x := v.(type) {
		case nil:
			return json.Number("0"), true
		case bool:
			if x {
				return json.Number("1"), true
			}
			return json.Number("0"), true
		case string:
			f, ok := parseNumber(x)
			if ok && (typ == "number" || f == math.Trunc(f)) {
				return formatNumber(f), true
			}
		}
	case "boolean":
		switch x := v.(type) {
		case nil:
			return false, true
		case string:
			if x == "true" || x == "false" {
				return x == "true", true
			}
		case json.Number:
			if f, err := x.Float64(); err == nil && (f == 0 || f == 1) {
				return f == 1, true
			}
		}
	case "string":
		switch x := v.(type) {
		case nil:
			return "", true
		case json.Number:
			return string(x), true
		case bool:
			return strconv.FormatBool(x), true
		}
	case "null":
		switch x := v.(type) {
		case string:
			if x == "" {
				return nil, true
			}
		case bool:
			if !x {
				return nil, true
			}
		case json.Number:
			if f, err := x.Float64(); err == nil && f == 0 {
				return nil, true
			}
		}
	}
	return v, false
}

func isType(v any, typ string) bool {
	switch x := v.(type) {
	case nil:
		return typ == "null"
	case bool:
		return typ == "boolean"
	case string:
		return typ == "string"
	case []any:
		return typ == "array"
	case map[string]any:
		return typ == "object"
	case json.Number:
		if typ == "number" {
			return true
		}
		f, err := x.Float64()
		return typ == "integer" && err == nil && f == math.Trunc(f)
	}
	return false
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// formatNumber writes f the way encoding/json does, which matches JavaScript
// and keeps an integer like 1e6 as "1000000" so it decodes into an int.
func formatNumber(f float64) json.Number {
	b, _ := json.Marshal(f)
	return json.Number(b)
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = cloneValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	}
	return v
}
