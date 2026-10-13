package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/fx"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
)

// ACPParams supplies the process facts of the editor connection. The module
// reads no configuration file, opens no database and starts no listener: the
// only network use is the outbound HTTP of the model request and of the native
// sign-in methods.
type ACPParams struct {
	// Home is the directory of the saved credentials. Empty selects ASK_HOME or
	// the default home.
	Home string
	// Env reads one environment variable. It reports false when it is not set.
	Env func(string) (string, bool)
	// AuthHTTP carries the token exchange of the sign-in methods. It is separate
	// from the HTTP of the model request.
	AuthHTTP *http.Client
	// Now and Wait are the clocks of the credential refresh. Nil uses the
	// production ones.
	Now  func() time.Time
	Wait func(context.Context, time.Duration) error
	// Initial is the demo model. It is listed first in the catalog and is always
	// usable. It is the zero model unless the demo provider is enabled.
	Initial providers.Model
	// Info names the agent in the initialize result.
	Info sdk.Implementation
	// NewAgent builds the Agent of one session on the start model that the
	// runtime chose, which is the zero model when no provider is usable. It must
	// use NewNativeAgent with the given service, so that sessions share one
	// credential resolver.
	NewAgent func(service *auth.Service, sessionID, cwd string, start providers.Model) (*agent.Agent, error)
}

// ACPRuntime is the composed editor connection.
type ACPRuntime struct {
	// Config is the adapter configuration.
	Config acp.Config
	// Cleanup stops the credential refresh and waits for its drain. It is
	// independent of the drain of inference and tools.
	Cleanup func(ctx context.Context) error
	// Service is the credential service that every session shares. The leader
	// composition builds its sign-in broker over it.
	Service *auth.Service
	// Native holds the options of the native sign-in and key-check HTTP.
	Native auth.NativeOptions
	// Usable tells whether a provider can serve models now. It is offline.
	Usable func(ctx context.Context, provider string) bool
	// Rows lists the catalog rows that the model list shows.
	Rows func() []providers.Model
}

// ACPModule provides the ACPRuntime. Supply ACPParams to use it.
var ACPModule = fx.Options(fx.Provide(NewACPRuntime))

// NewACPRuntime composes the native credential service and the adapter
// callbacks. The adapter gets function fields only, never the service.
func NewACPRuntime(p ACPParams) (*ACPRuntime, error) {
	if p.NewAgent == nil {
		return nil, errors.New("acp runtime: agent constructor is required")
	}
	if p.Env == nil {
		p.Env = func(string) (string, bool) { return "", false }
	}
	native := auth.NativeOptions{HTTPClient: p.AuthHTTP, Now: p.Now, Wait: p.Wait}
	service, err := NewNativeAuth(p.Home, p.Env, native)
	if err != nil {
		return nil, err
	}
	prefs, err := settings.NewSettingsStore(p.Home)
	if err != nil {
		return nil, err
	}
	rows := func() []providers.Model {
		catalog := providers.AvailableModels()
		if p.Initial.ID == "" {
			return catalog
		}
		return append([]providers.Model{p.Initial}, catalog...)
	}
	find := func(ref providers.Ref) (providers.Model, error) {
		in := p.Initial
		if in.ID != "" && ref.Provider == in.Provider && ref.ID == in.ID && (ref.API == "" || ref.API == in.API) {
			return in, nil
		}
		return providers.Find(ref)
	}
	usable := func(ctx context.Context, provider string) bool {
		if p.Initial.ID != "" && provider == p.Initial.Provider {
			return true
		}
		return providerUsable(ctx, service, provider)
	}
	choice := startChoice{initial: p.Initial, rows: rows, find: find, usable: usable, saved: prefs.DefaultModel}
	// A session that starts on a fallback model hands its note to the adapter
	// once, when the adapter publishes the session.
	var fbMu sync.Mutex
	fallbacks := map[string]*protocol.ACPModelFallback{}
	cfg := acp.Config{
		Factory: func(ctx context.Context, id, cwd string) (*agent.Agent, error) {
			start, fb := choice.pick(ctx)
			a, err := p.NewAgent(service, id, cwd, start)
			if err != nil {
				return nil, err
			}
			if fb != nil {
				fbMu.Lock()
				fallbacks[id] = fb
				fbMu.Unlock()
			}
			return a, nil
		},
		StartFallback: func(id string) *protocol.ACPModelFallback {
			fbMu.Lock()
			defer fbMu.Unlock()
			fb := fallbacks[id]
			delete(fallbacks, id)
			return fb
		},
		DefaultModel: prefs.DefaultModel,
		SaveDefault:  prefs.SetDefaultModel,
		Info:         p.Info,
		AuthMethods:  acpAuthMethods(service),
		// A method is ready when some catalog row accepts it with the saved or
		// environment credential. The check never starts a sign-in.
		Authenticate: func(ctx context.Context, methodID string) error {
			for _, m := range providers.AvailableModels() {
				if NativeAuthReady(ctx, service, m, "", methodID) == nil {
					return nil
				}
			}
			return auth.ErrNoCredential
		},
		Models:    rows,
		FindModel: find,
		ModelAuth: func(ctx context.Context, m providers.Model, methodID string) error {
			return NativeAuthReady(ctx, service, m, "", methodID)
		},
	}
	cleanup := func(ctx context.Context) error {
		service.StopRefresh()
		return AuthWait(service)(ctx)
	}
	return &ACPRuntime{Config: cfg, Cleanup: cleanup, Service: service, Native: native, Usable: usable, Rows: rows}, nil
}

// acpAuthMethods lists each native method once. The description names the host
// command that signs in, because the connection never asks for a secret.
func acpAuthMethods(service *auth.Service) []sdk.AuthMethod {
	seen := map[string]bool{}
	var out []sdk.AuthMethod
	for _, m := range service.Methods {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		desc := fmt.Sprintf("Sign in on the host with `ask auth login --provider <provider> --method %s`, then authenticate.", m.ID)
		out = append(out, sdk.AuthMethod{Agent: &sdk.AuthMethodAgent{Id: m.ID, Name: m.ID, Description: &desc}})
	}
	return out
}

// providerUsable tells whether a provider has a saved credential or an
// environment key. It reads the local store only and never refreshes a token.
func providerUsable(ctx context.Context, service *auth.Service, provider string) bool {
	rows, err := service.Status(ctx)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.Provider == provider && r.State != auth.StateNone {
			return true
		}
	}
	return false
}
