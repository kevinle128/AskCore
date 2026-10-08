package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/fx"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"

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
	// Initial is the model of a new session. It is listed first in the catalog.
	Initial providers.Model
	// Info names the agent in the initialize result.
	Info sdk.Implementation
	// NewAgent builds the Agent of one session. It must use NewNativeAgent with
	// the given service, so that sessions share one credential resolver.
	NewAgent func(service *auth.Service, sessionID, cwd string) (*agent.Agent, error)
}

// ACPRuntime is the composed editor connection.
type ACPRuntime struct {
	// Config is the adapter configuration.
	Config acp.Config
	// Cleanup stops the credential refresh and waits for its drain. It is
	// independent of the drain of inference and tools.
	Cleanup func(ctx context.Context) error
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
	service, err := NewNativeAuth(p.Home, p.Env, auth.NativeOptions{HTTPClient: p.AuthHTTP, Now: p.Now, Wait: p.Wait})
	if err != nil {
		return nil, err
	}
	rows := func() []providers.Model {
		return append([]providers.Model{p.Initial}, providers.AvailableModels()...)
	}
	cfg := acp.Config{
		Factory: func(_ context.Context, id, cwd string) (*agent.Agent, error) {
			return p.NewAgent(service, id, cwd)
		},
		Info:        p.Info,
		AuthMethods: acpAuthMethods(service),
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
		Models: rows,
		FindModel: func(ref providers.Ref) (providers.Model, error) {
			if ref.Provider == p.Initial.Provider && ref.ID == p.Initial.ID && (ref.API == "" || ref.API == p.Initial.API) {
				return p.Initial, nil
			}
			return providers.Find(ref)
		},
		ModelAuth: func(ctx context.Context, m providers.Model, methodID string) error {
			return NativeAuthReady(ctx, service, m, "", methodID)
		},
	}
	cleanup := func(ctx context.Context) error {
		service.StopRefresh()
		return AuthWait(service)(ctx)
	}
	return &ACPRuntime{Config: cfg, Cleanup: cleanup}, nil
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
