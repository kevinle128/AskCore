package tools

import (
	"context"
	"encoding/json"

	"AskCore/pkg/protocol"
)

// Tool is something the model can call. Execute receives the arguments that
// Registry.Prepare returned. ctx is the abort signal, so a tool returns
// promptly once ctx is done. Started bodies drain without a time bound, and
// the Agent stays busy until each body returns. A tool that starts a process
// must stop its entire process group on cancellation.
type Tool interface {
	Decl() protocol.ToolDecl
	Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error)
}

// ConcurrencySafe is implemented by a tool that can run with other safe calls.
// Args are validated and frozen for the call. A missing interface, false answer,
// or panic means the call runs alone. The coordinator checks before each start.
type ConcurrencySafe interface {
	ConcurrencySafe(args json.RawMessage) bool
}

// ArgumentPreparer rewrites the raw arguments before validation, for example
// to accept an older argument shape.
type ArgumentPreparer interface {
	PrepareArguments(raw json.RawMessage) (json.RawMessage, error)
}

// Context holds the per-call data a tool needs besides its arguments.
type Context struct {
	CallID string
	Cwd    string
	Update func(partial protocol.ToolExecutionResult)
}
