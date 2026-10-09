package leader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"AskCore/pkg/protocol"
)

// rpcMessage is one decoded JSON-RPC message. The fields keep their raw bytes.
type rpcMessage struct {
	fields    map[string]json.RawMessage
	raw       json.RawMessage
	method    string
	hasMethod bool
	id        json.RawMessage
	hasID     bool
}

// parseMessage checks the envelope: one object, jsonrpc "2.0", a string method
// when there is one, and an id that is a number or a string when there is one.
func parseMessage(raw json.RawMessage) (*rpcMessage, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	var version string
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" {
		return nil, fmt.Errorf("%w: jsonrpc must be \"2.0\"", ErrInvalidMessage)
	}
	m := &rpcMessage{fields: fields, raw: raw}
	if m.method, m.hasMethod, err = methodOf(fields); err != nil {
		return nil, err
	}
	if id, ok := fields["id"]; ok {
		if _, err := canonicalID(id); err != nil {
			return nil, err
		}
		m.id, m.hasID = id, true
	}
	return m, nil
}

// params returns the params object. A missing params is an empty object.
func (m *rpcMessage) params() (map[string]json.RawMessage, error) {
	raw, ok := m.fields["params"]
	if !ok {
		return map[string]json.RawMessage{}, nil
	}
	p, err := decodeObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: params must be an object", ErrInvalidMessage)
	}
	if hasFoldDuplicate(p) {
		return nil, fmt.Errorf("%w: params have two keys that differ only in case", ErrInvalidMessage)
	}
	return p, nil
}

// foldKey returns one key for all spellings of a name that a JSON decoder takes
// for the same field. Go matches object keys without regard to case, and when two
// keys match one field the last one wins, so the router must not read one key
// while the host reads another.
func foldKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		low := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < low {
				low = f
			}
		}
		b.WriteRune(low)
	}
	return b.String()
}

// hasFoldDuplicate tells whether two keys of an object differ only in case.
func hasFoldDuplicate(obj map[string]json.RawMessage) bool {
	seen := make(map[string]struct{}, len(obj))
	for k := range obj {
		fk := foldKey(k)
		if _, dup := seen[fk]; dup {
			return true
		}
		seen[fk] = struct{}{}
	}
	return false
}

// stringParam reads one string param. It is empty when the param is missing or is not a string.
func stringParam(p map[string]json.RawMessage, name string) string {
	var s string
	if raw, ok := p[name]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// marshalPlain encodes without HTML escaping, so raw ids and params keep their bytes.
func marshalPlain(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return json.RawMessage(`null`)
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage(`null`)
	}
	return id
}

// errorResponse answers a request with an Ask error. It carries only the fixed
// text and kind of the protocol package.
func errorResponse(id json.RawMessage, kind protocol.ACPErrorKind) json.RawMessage {
	return marshalPlain(map[string]any{
		"jsonrpc": "2.0",
		"id":      idOrNull(id),
		"error": map[string]any{
			"code":    protocol.ACPErrorCode(kind),
			"message": protocol.ACPErrorMessage(kind),
			"data":    protocol.ACPErrorData{Kind: kind},
		},
	})
}

// invalidRequest answers a message that is not a valid JSON-RPC message.
func invalidRequest(id json.RawMessage) json.RawMessage {
	return marshalPlain(map[string]any{
		"jsonrpc": "2.0",
		"id":      idOrNull(id),
		"error":   map[string]any{"code": -32600, "message": "invalid request"},
	})
}

func resultResponse(id json.RawMessage, result any) json.RawMessage {
	return marshalPlain(map[string]any{"jsonrpc": "2.0", "id": idOrNull(id), "result": result})
}

func notification(method string, params any) json.RawMessage {
	return marshalPlain(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func requestMessage(id, method string, params any) json.RawMessage {
	return marshalPlain(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

// injectRoute puts the route context into params._meta. It replaces any value
// that the client sent under the route key and keeps the other meta keys.
func injectRoute(params map[string]json.RawMessage, meta protocol.ACPRouteMeta) json.RawMessage {
	out := make(map[string]json.RawMessage, len(params)+1)
	metaObj := map[string]json.RawMessage{}
	for k, v := range params {
		if foldKey(k) == foldKey("_meta") {
			// A key that a decoder takes for _meta never reaches the host as it is.
			// Its content is kept, under the exact key, without the route key.
			if existing, err := decodeObject(v); err == nil {
				metaObj = existing
			}
			continue
		}
		out[k] = v
	}
	metaObj[protocol.ACPRouteMetaKey] = marshalPlain(meta)
	out["_meta"] = marshalPlain(metaObj)
	return marshalPlain(out)
}

// stripRouteMeta removes the route context from the _meta of a result. The host
// adds it for the router; the client has no use for it.
func stripRouteMeta(m *rpcMessage) json.RawMessage {
	result, err := decodeObject(m.fields["result"])
	if err != nil {
		return m.raw
	}
	metaRaw, ok := result["_meta"]
	if !ok {
		return m.raw
	}
	meta, err := decodeObject(metaRaw)
	if err != nil {
		return m.raw
	}
	delete(meta, protocol.ACPRouteMetaKey)
	if len(meta) == 0 {
		delete(result, "_meta")
	} else {
		result["_meta"] = marshalPlain(meta)
	}
	fields := make(map[string]json.RawMessage, len(m.fields))
	for k, v := range m.fields {
		fields[k] = v
	}
	fields["result"] = marshalPlain(result)
	return marshalPlain(fields)
}

// routeGen reads the driver generation that the host put in a result.
func routeGen(m *rpcMessage) (uint64, bool) {
	result, err := decodeObject(m.fields["result"])
	if err != nil {
		return 0, false
	}
	meta, err := decodeObject(result["_meta"])
	if err != nil {
		return 0, false
	}
	var route protocol.ACPRouteMeta
	if json.Unmarshal(meta[protocol.ACPRouteMetaKey], &route) != nil || route.DriverGen == 0 {
		return 0, false
	}
	return route.DriverGen, true
}

// clientAllows tells whether the client capabilities allow a reverse method.
func clientAllows(caps json.RawMessage, method string) bool {
	var c struct {
		FS struct {
			ReadTextFile  bool `json:"readTextFile"`
			WriteTextFile bool `json:"writeTextFile"`
		} `json:"fs"`
		Terminal bool `json:"terminal"`
	}
	if len(caps) == 0 || json.Unmarshal(caps, &c) != nil {
		return false
	}
	switch method {
	case "fs/read_text_file":
		return c.FS.ReadTextFile
	case "fs/write_text_file":
		return c.FS.WriteTextFile
	}
	return len(method) > len("terminal/") && method[:len("terminal/")] == "terminal/" && c.Terminal
}
