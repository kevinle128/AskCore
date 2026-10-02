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

// projectContext returns the PrepareRequest of a run: the request context
// becomes the system message plus src.Messages(), then the user hook runs on
// that projection and its update wins.
func projectContext(src ContextSource, system []protocol.Message, user func(context.Context, pipeline.Request) (*pipeline.RequestUpdate, error)) func(context.Context, pipeline.Request) (*pipeline.RequestUpdate, error) {
	return func(ctx context.Context, r pipeline.Request) (*pipeline.RequestUpdate, error) {
		r.Context.Messages = append(slices.Clip(system), src.Messages()...)
		projected := &pipeline.RequestUpdate{Context: &r.Context}
		if user == nil {
			return projected, nil
		}
		upd, err := user(ctx, r)
		if err != nil {
			return nil, err
		}
		if upd == nil {
			return projected, nil
		}
		merged := *upd
		if merged.Context == nil {
			merged.Context = projected.Context
		}
		return &merged, nil
	}
}
