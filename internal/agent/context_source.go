package agent

import (
	"context"
	"slices"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// projectContext returns the PrepareRequest handler of the run that projects
// the log into every request: the request context becomes the system message
// plus src.Messages(). The Agent registers it before the handlers of the
// config, so those handlers see the projected context. Its update keeps every
// field of the inner update; the projected context is the default, and an
// inner update that returns a context wins.
func projectContext(src sessions.Writer, system []protocol.Message) pipeline.PrepareRequestHandler {
	return func(ctx context.Context, r pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		r.Context.Messages = append(slices.Clip(system), src.Messages()...)
		r.Context.Messages = append(r.Context.Messages, freezeMessages(r.StagedMessages)...)
		upd, err := next(ctx, r)
		if err != nil {
			return nil, err
		}
		if upd == nil {
			upd = &pipeline.RequestUpdate{}
		}
		if upd.Context == nil {
			upd.Context = &r.Context
		}
		return upd, nil
	}
}
