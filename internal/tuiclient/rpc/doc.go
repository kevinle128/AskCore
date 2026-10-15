// Package rpc is the JSON-RPC request tracker that every Ask client of the
// agent shares. It sends requests, matches the answers to them and hands
// notifications and calls of the agent to a handler.
//
// A Transport carries one JSON-RPC body at a time. The leader socket and the
// in-process pipe are two transports. The package knows nothing about
// sessions, prompts or the terminal UI.
package rpc
