package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"AskCore/pkg/protocol"
)

// Echo returns its text argument as one text block.
type Echo struct{}

// ConcurrencySafe returns true because Echo has no shared mutable state.
func (Echo) ConcurrencySafe(json.RawMessage) bool { return true }

func (Echo) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name:        "echo",
		Description: "Return the given text unchanged.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"The text to return."}},"required":["text"]}`),
	}
}

func (Echo) Execute(ctx context.Context, _ Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("echo: %w", err)
	}
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: in.Text}}}, nil
}
