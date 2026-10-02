package tools

import (
	"context"
	"encoding/json"

	"AskCore/pkg/protocol"
)

// Tool is something the model can call. Execute receives the arguments that
// Registry.Prepare returned. ctx is the abort signal, so a tool returns
// promptly once ctx is done.
type Tool interface {
	Decl() protocol.ToolDecl
	Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error)
}

// Sequential is implemented by a tool that must not run alongside other
// tools. When one call in a batch names such a tool, the whole batch runs one
// call at a time.
type Sequential interface{ Sequential() bool }

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
