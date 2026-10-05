package agent

import (
	"context"
	"slices"

	"AskCore/internal/pipeline"
	"AskCore/pkg/protocol"
)

// ContextSource is the message log that an Agent runs on. The Agent appends
// each message on its message_end, so a partial message never enters it, and
// projects Messages into every request. State reads Messages from another
// goroutine than the one that appends.
type ContextSource interface {
	Append(m protocol.Message) error
	Messages() []protocol.Message
}

// projectContext returns the hooks of the run that project the log into every
// request: the request context becomes the system message plus src.Messages().
// The Agent composes them in front of the user hooks, so a user hook sees the
// projected context.
func projectContext(src ContextSource, system []protocol.Message) pipeline.Hooks {
	return pipeline.Hooks{
		PrepareRequest: func(_ context.Context, r pipeline.Request) (*pipeline.RequestUpdate, error) {
			r.Context.Messages = append(slices.Clip(system), src.Messages()...)
			return &pipeline.RequestUpdate{Context: &r.Context}, nil
		},
	}
}
