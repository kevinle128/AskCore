package app

import (
	"context"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// AuthRunner composes the same resolver for streaming and idle model readiness.
func AuthRunner(service *auth.Service, registry *providers.Registry) (providers.StreamFn, func(context.Context, providers.Model, string) error) {
	ready := func(ctx context.Context, model providers.Model, key string) error {
		_, err := service.Resolve(ctx, model, key)
		return err
	}
	stream := func(ctx context.Context, model providers.Model, request providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
		if opts.Auth != (providers.AuthSnapshot{}) && opts.APIKey != "" {
			return authFailure(ctx, model, auth.ErrCompetingOverride)
		}
		if opts.Auth != (providers.AuthSnapshot{}) {
			if err := service.ValidateOverride(model, opts.Auth); err != nil {
				return authFailure(ctx, model, err)
			}
		} else {
			binding, err := service.Resolve(ctx, model, opts.APIKey)
			if err != nil {
				return authFailure(ctx, model, err)
			}
			opts.Auth = binding
		}
		if opts.Auth.Method == "api-key" {
			opts.APIKey = opts.Auth.AccessToken
		} else {
			opts.APIKey = ""
		}
		return registry.Stream(ctx, model, request, opts)
	}
	return stream, ready
}

// AuthWait gives shutdown a bound independent of inference cancellation.
func AuthWait(service *auth.Service) func(context.Context) error {
	return func(ctx context.Context) error {
		waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return service.DrainRefresh(waitCtx)
	}
}

func authFailure(ctx context.Context, model providers.Model, err error) *providers.Stream {
	seed := protocol.AssistantMessage{API: string(model.API), Provider: model.Provider, Model: model.ID}
	return providers.NewStream(ctx, 0, seed, func(a *providers.Assembler) { a.Fail(protocol.StopError, err.Error(), err) })
}

// BindAuth installs the composed request and readiness functions together.
func BindAuth(cfg agent.Config, service *auth.Service, registry *providers.Registry) agent.Config {
	cfg.Stream, cfg.Ready = AuthRunner(service, registry)
	return cfg
}
