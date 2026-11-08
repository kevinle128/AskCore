package app

import (
	"context"
	"net/http"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// AuthRunner composes the same resolver for streaming and idle model readiness.
// It reads no user models.json; BindModelAuth adds the model view.
func AuthRunner(service *auth.Service, registry *providers.Registry) (providers.StreamFn, func(context.Context, providers.Model, string) error) {
	return authRunner(service, registry, nil)
}

// authRunner is AuthRunner over the model view. Each request reads the stored
// snapshot of view once, with no file access. When the user file configures
// the endpoint of the request row, the request uses its key and a
// request-local copy of the row with the expanded headers. A row that the file
// made and no longer has fails with errModelConfigChanged and sends nothing.
func authRunner(service *auth.Service, registry *providers.Registry, view *ModelView) (providers.StreamFn, func(context.Context, providers.Model, string) error) {
	ready := func(ctx context.Context, model providers.Model, key string) error {
		entry, state := view.snapshot().lookup(model)
		if state == lookupChanged {
			return errModelConfigChanged
		}
		_, err := service.ResolveWith(ctx, auth.ResolveRequest{Model: model, Override: key, Config: entry.configKey()})
		return err
	}
	stream := func(ctx context.Context, model providers.Model, request providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
		if opts.Auth != (providers.AuthSnapshot{}) && opts.APIKey != "" {
			return authFailure(ctx, model, auth.ErrCompetingOverride)
		}
		entry, state := view.snapshot().lookup(model)
		if state == lookupChanged {
			return failedStream(ctx, model, providers.NewFailure(providers.CodeModelConfig, 0, 0, errModelConfigChanged.Error(), errModelConfigChanged))
		}
		sent, commands, err := requestModel(ctx, service.Values(), entry, model)
		if err != nil {
			return authFailure(ctx, model, err)
		}
		if opts.Auth != (providers.AuthSnapshot{}) {
			if err := service.ValidateOverride(model, opts.Auth); err != nil {
				return authFailure(ctx, model, err)
			}
		} else {
			binding, err := service.ResolveWith(ctx, auth.ResolveRequest{Model: model, Override: opts.APIKey, Config: entry.configKey()})
			if err != nil {
				return authFailure(ctx, model, err)
			}
			opts.Auth = binding
			if command := keyCommand(entry.apiKey); command != "" && binding.Source == string(auth.SourceConfig) {
				commands = append(commands, command)
			}
		}
		if pin := opts.RequireBinding; pin != nil {
			bound := opts.Auth.Binding()
			if bound.Method != pin.Method || bound.Profile != pin.Profile || bound.BillingHint != pin.BillingHint {
				return authFailure(ctx, model, auth.ErrBindingChanged)
			}
		}
		if opts.Auth.Method == "api-key" {
			opts.APIKey = opts.Auth.AccessToken
		} else {
			opts.APIKey = ""
		}
		// The stream reports the binding without token or account, so the Agent can
		// log which credential the attempt used.
		out := registry.Stream(ctx, sent, request, opts).WithBinding(opts.Auth.Binding())
		if len(commands) == 0 {
			return out
		}
		// A 401 means that a command value may be stale. The relay forgets the
		// cached output before the end reaches the consumer, so the next request
		// runs the command again.
		values := service.Values()
		return providers.Relay(ctx, out, func(err error) {
			if f, ok := providers.AsFailure(err); ok && f.Status == http.StatusUnauthorized {
				for _, command := range commands {
					values.Forget(command)
				}
			}
		})
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

// authFailure is a stream that ends at once with a failure of code AUTH. The
// failure wraps err, so errors.Is still finds the cause.
func authFailure(ctx context.Context, model providers.Model, err error) *providers.Stream {
	return failedStream(ctx, model, providers.NewFailure(providers.CodeAuth, 0, 0, err.Error(), err))
}

func failedStream(ctx context.Context, model providers.Model, failure *providers.Failure) *providers.Stream {
	seed := protocol.AssistantMessage{API: string(model.API), Provider: model.Provider, Model: model.ID}
	return providers.NewStream(ctx, 0, seed, func(a *providers.Assembler) { a.Fail(protocol.StopError, failure.Message, failure) })
}

// BindAuth installs the composed request and readiness functions together.
func BindAuth(cfg agent.Config, service *auth.Service, registry *providers.Registry) agent.Config {
	return BindModelAuth(cfg, service, registry, nil)
}

// BindModelAuth is BindAuth over the model view of the process, so the
// requests use the key and the headers of the user models.json. A nil view
// reads no user file.
func BindModelAuth(cfg agent.Config, service *auth.Service, registry *providers.Registry, view *ModelView) agent.Config {
	cfg.Stream, cfg.Ready = authRunner(service, registry, view)
	if cfg.Prepare == nil {
		cfg.Prepare = registry.Prepare
	}
	return cfg
}
