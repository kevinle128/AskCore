package main

import "encoding/json"

// incoming is the envelope of a JSON-RPC body, as the transport fixtures read it.
type incoming struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// rpcBody encodes one JSON-RPC body from its fields.
func rpcBody(fields map[string]any) json.RawMessage {
	fields["jsonrpc"] = "2.0"
	b, _ := json.Marshal(fields)
	return b
}
