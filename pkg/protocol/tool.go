package protocol

import "encoding/json"

// ToolDecl declares a tool to the model. Parameters is a JSON Schema document.
// A nil Parameters encodes as the empty object.
type ToolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolRef names a tool without its declaration.
type ToolRef struct {
	Name string `json:"name"`
}

// MarshalJSON encodes the declaration and defaults empty parameters to {}.
func (d ToolDecl) MarshalJSON() ([]byte, error) {
	if len(d.Parameters) == 0 {
		d.Parameters = json.RawMessage(`{}`)
	}
	type plain ToolDecl
	return marshalJSON(plain(d))
}

func cloneToolDecls(in []ToolDecl) []ToolDecl {
	if in == nil {
		return nil
	}
	out := make([]ToolDecl, len(in))
	for i, d := range in {
		d.Parameters = cloneRaw(d.Parameters)
		out[i] = d
	}
	return out
}
