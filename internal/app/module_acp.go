package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/fx"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/internal/tools"
	"AskCore/internal/workspace"
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
	// Now is the clock of the credential refresh and of the session date. Nil
	// uses time.Now. Wait is the wait of the credential refresh; nil uses the
	// production one.
	Now  func() time.Time
	Wait func(context.Context, time.Duration) error
	// Initial is the demo model. It is listed first in the catalog and is always
	// usable. It is the zero model unless the demo provider is enabled.
	Initial providers.Model
	// Info names the agent in the initialize result.
	Info sdk.Implementation
	// ModelRefresh configures the network refresh of the model list. The zero
	// value reads the cache only; the product compositions turn it on.
	ModelRefresh ModelRefresh
	// NewAgent builds the Agent of one session on the start model that the
	// runtime chose, which is the zero model when no provider is usable. It must
	// use NewNativeAgent with the given service, so that sessions share one
	// credential resolver.
	NewAgent func(service *auth.Service, spec AgentSpec) (*agent.Agent, error)
	// UserHome is the home folder of the user. Its .agents/skills folder is
	// not a project resource. Empty uses os.UserHomeDir.
	UserHome string
	// TrustOverride trusts ("approve") or does not trust ("deny") the project
	// of every session of the process. The override of one session wins over
	// it. Only ask acp sets it, from its command line; the leader is shared and
	// never takes one. Empty means no override.
	TrustOverride protocol.ACPTrustOverride
	// TrustRequests serves _ask/trust/state and _ask/trust/set. The leader and
	// the in-process TUI set it; an editor link has no prompt and leaves it off.
	TrustRequests bool
	// Warn receives the warning lines of each session start once: trust
	// warnings and settings diagnostics, with paths and fixed texts only. Nil
	// discards them.
	Warn func(line string)
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
	// Rows lists the rows that the model list shows, in display order.
	Rows func() []providers.Model

	// models owns the model list and its refresh. Cleanup stops it.
	models *modelRefresher
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
	home, err := settings.ResolveHome(p.Home)
	if err != nil {
		return nil, err
	}
	switch p.TrustOverride {
	case "", protocol.ACPTrustApprove, protocol.ACPTrustDeny:
	default:
		return nil, fmt.Errorf("acp runtime: unknown trust override %q", p.TrustOverride)
	}
	userHome := p.UserHome
	if userHome == "" {
		// Without a user home, every .agents/skills folder counts as a project
		// resource, which fails closed.
		userHome, _ = os.UserHomeDir()
	}
	warn := p.Warn
	if warn == nil {
		warn = func(string) {}
	}
	models, err := newModelRefresher(p.Home, service, p.ModelRefresh)
	if err != nil {
		return nil, err
	}
	choice := newStartChoice(p.Initial, models.Current, service)
	rows, find, usable := choice.rows, choice.find, choice.usable
	// A session that starts on a fallback model hands its note and its trust
	// decision to the adapter once, when the adapter publishes the session.
	var startMu sync.Mutex
	fallbacks := map[string]*protocol.ACPModelFallback{}
	trusts := map[string]*acp.SessionTrust{}
	cfg := acp.Config{
		Factory: func(ctx context.Context, req acp.SessionRequest) (*agent.Agent, error) {
			// The trust decision comes before any read of a project file. The
			// flags and the trust override come from this request only.
			override := p.TrustOverride
			if req.Trust != "" {
				override = req.Trust
			}
			session, err := PrepareSession(ctx, SessionInput{
				Home: home, UserHome: userHome, Cwd: req.Cwd,
				Trust: workspace.Override(override), Flags: req.Flags, Now: p.Now,
			})
			if err != nil {
				return nil, startError(err)
			}
			c := choice
			c.saved = func(context.Context) (string, error) { return session.Settings.DefaultModel, nil }
			start, fb := c.pick(ctx)
			a, err := p.NewAgent(service, AgentSpec{SessionID: req.ID, Cwd: req.Cwd, Start: start, Session: session})
			if err != nil {
				return nil, startError(err)
			}
			startMu.Lock()
			if fb != nil {
				fallbacks[req.ID] = fb
			}
			trusts[req.ID] = sessionTrust(session)
			startMu.Unlock()
			for _, line := range session.Warnings() {
				warn(line)
			}
			return a, nil
		},
		StartFallback: func(id string) *protocol.ACPModelFallback {
			startMu.Lock()
			defer startMu.Unlock()
			fb := fallbacks[id]
			delete(fallbacks, id)
			return fb
		},
		StartTrust: func(id string) *acp.SessionTrust {
			startMu.Lock()
			defer startMu.Unlock()
			t := trusts[id]
			delete(trusts, id)
			return t
		},
		DefaultModel: func(ctx context.Context, scope acp.SettingsScope) (string, error) {
			layered, err := scopeSettings(home, scope)
			if err != nil {
				return "", err
			}
			eff, err := layered.Load(ctx)
			return eff.DefaultModel, err
		},
		SaveDefault: func(ctx context.Context, scope acp.SettingsScope, modelID string) protocol.ACPDefaultNotSaved {
			layered, err := scopeSettings(home, scope)
			if err != nil {
				return protocol.ACPDefaultFailed
			}
			return defaultNotSaved(layered.SetDefaultModel(ctx, modelID))
		},
		Info:        p.Info,
		AuthMethods: acpAuthMethods(service),
		// A method is ready when some row accepts it with the saved or
		// environment credential. The check never starts a sign-in. Readiness
		// depends on the provider, the API and the endpoint of a row (they
		// select the sign-in method), so one row of each is enough: the list
		// can have many rows of one provider.
		Authenticate: func(ctx context.Context, methodID string) error {
			seen := map[[3]string]bool{}
			for _, m := range models.Current().set.All() {
				k := [3]string{m.Provider, string(m.API), m.BaseURL}
				if seen[k] {
					continue
				}
				seen[k] = true
				if NativeAuthReady(ctx, service, m, "", methodID) == nil {
					return nil
				}
			}
			return auth.ErrNoCredential
		},
		Models:        rows,
		RefreshModels: models.RefreshIfStale,
		FindModel:     find,
		ModelAuth: func(ctx context.Context, m providers.Model, methodID string) error {
			return NativeAuthReady(ctx, service, m, "", methodID)
		},
	}
	if p.TrustRequests {
		cfg.TrustSetup = trustSetup{home: home, userHome: userHome}
	}
	// The model refresh stops first: an account list fetch can hold a
	// credential refresh slot, which the drain waits for.
	cleanup := func(ctx context.Context) error {
		models.Stop()
		service.StopRefresh()
		return AuthWait(service)(ctx)
	}
	// The start path reads the cache only; a stale cache starts one refresh
	// in the background.
	models.RefreshIfStale()
	return &ACPRuntime{Config: cfg, Cleanup: cleanup, Service: service, Native: native, Usable: usable, Rows: rows, models: models}, nil
}

// scopeSettings returns the settings layers of a session scope. The project
// layer applies only when the session is trusted.
func scopeSettings(home string, scope acp.SettingsScope) (*settings.Layered, error) {
	root, hasLayer, err := workspace.ProjectRoot(scope.Cwd, home)
	if err != nil {
		return nil, err
	}
	project := settings.Project{}
	if hasLayer {
		project = settings.Project{Root: root, Trusted: scope.Trusted}
	}
	return settings.NewLayered(home, project)
}

// defaultNotSaved maps the error of a default save to its wire reason.
func defaultNotSaved(err error) protocol.ACPDefaultNotSaved {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, settings.ErrShadowed):
		return protocol.ACPDefaultShadowed
	case errors.Is(err, settings.ErrUnsafeStore):
		return protocol.ACPDefaultUnsafeFile
	}
	return protocol.ACPDefaultFailed
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

// startError maps a session start failure that the client or its settings
// caused to invalid_params. The adapter never sends the text of another error.
// The detail holds fixed text, the origin constants, and tool names that the
// flag and settings parsers checked.
func startError(err error) error {
	var sel *tools.SelectionError
	switch {
	case errors.As(err, &sel):
		return acp.InvalidParams(fmt.Sprintf("%s: unknown tool %s; known tools: %s", sel.Origin, sel.Name, strings.Join(sel.Known, ", ")))
	case errors.Is(err, ErrSessionFlags):
		return acp.InvalidParams("the tool flags are not valid: use --tools or --no-tools, not both")
	default:
		return err
	}
}
