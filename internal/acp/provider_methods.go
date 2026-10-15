package acp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"
)

// Errors that a ProviderSetup implementation returns. The adapter maps them
// to fixed kinds and texts, so the text of the implementation never reaches the
// wire. Any other error is an internal error.
var (
	// ErrProviderBusy means another sign-in or key check holds the provider.
	ErrProviderBusy = errors.New("acp: another sign-in for this provider is running")
	// ErrLoginNotFound means the sign-in id is unknown to the calling client.
	ErrLoginNotFound = errors.New("acp: sign-in not found")
	// ErrLoginState means the sign-in cannot take the call in its state.
	ErrLoginState = errors.New("acp: the sign-in cannot take this call now")
	// ErrLoginMethod means the provider or method is not a login option.
	ErrLoginMethod = errors.New("acp: unknown login option")
)

// ProviderSetup is what the host needs to serve the provider methods. The
// composition implements it over the credential broker; the adapter imports no
// credential code. owner is the client id of the route: a sign-in belongs to
// the client that started it.
type ProviderSetup interface {
	// List returns the login options with their state.
	List(ctx context.Context) (protocol.ACPProvidersResult, error)
	// SaveKey checks and saves an API key. A refusal is in the outcome.
	SaveKey(ctx context.Context, owner string, req protocol.ACPAPIKeyRequest) (protocol.ACPLoginOutcome, error)
	// LoginStart opens a browser, address or device sign-in.
	LoginStart(ctx context.Context, owner string, req protocol.ACPLoginStartRequest) (protocol.ACPLoginStartResult, error)
	// LoginInput gives a pasted code or callback address to a sign-in.
	LoginInput(owner string, req protocol.ACPLoginInputRequest) error
	// LoginWait blocks until the sign-in ends or ctx ends. Ending ctx does not
	// stop the sign-in.
	LoginWait(ctx context.Context, owner, loginID string) (protocol.ACPLoginOutcome, error)
	// LoginCancel stops a sign-in. It reports whether nothing was saved.
	LoginCancel(owner, loginID string) (bool, error)
	// Logout removes the saved credential. It changes no session: the change
	// callback of the composition calls Adapter.ProvidersChanged.
	Logout(ctx context.Context, req protocol.ACPLogoutRequest) (protocol.ACPLogoutResult, error)
	// ReleaseClient ends the sign-ins of a client that is gone.
	ReleaseClient(owner string)
	// Usable reports whether a provider can serve models now.
	Usable(ctx context.Context, provider string) bool
}

// providerCallTimeout bounds the offline checks that the host makes itself.
const providerCallTimeout = 5 * time.Second

type providerHandler func(ctx context.Context, owner string, raw json.RawMessage) (any, error)

func (a *Adapter) providerHandlers() map[string]providerHandler {
	return map[string]providerHandler{
		protocol.ACPProvidersList:          a.providersList,
		protocol.ACPProvidersAPIKey:        a.providersAPIKey,
		protocol.ACPLoginStart:             a.loginStart,
		protocol.ACPLoginInput:             a.loginInput,
		protocol.ACPLoginWait:              a.loginWait,
		protocol.ACPLoginCancel:            a.loginCancel,
		protocol.ACPProvidersLogout:        a.providersLogout,
		protocol.ACPProvidersReleaseClient: a.releaseClient,
	}
}

// handleProviderMethod serves a provider method. handled is false when method
// is not one. The methods are served only on the leader link (RequireRoute) and only
// when the composition set the provider setup: an editor link never takes a secret,
// whatever its _meta holds.
func (a *Adapter) handleProviderMethod(ctx context.Context, method string, raw json.RawMessage) (handled bool, res any, err error) {
	handler, ok := a.providerHandlers()[method]
	if !ok {
		return false, nil, nil
	}
	if err := a.gate(); err != nil {
		return true, nil, requestError(err)
	}
	if !a.cfg.RequireRoute || a.cfg.ProviderSetup == nil {
		return true, nil, requestError(unsupported(method))
	}
	route, err := routeFromParams(raw)
	if err == nil {
		err = a.needRoute(route)
	}
	if err != nil {
		return true, nil, requestError(err)
	}
	if route == nil || route.ClientID == "" {
		return true, nil, requestError(errBadRoute)
	}
	out, err := handler(ctx, route.ClientID, raw)
	if err != nil {
		return true, nil, requestError(providerSetupError(err))
	}
	return true, out, nil
}

// providerSetupError maps an error of the provider setup to a kind with a fixed text.
func providerSetupError(err error) error {
	switch {
	case errors.Is(err, ErrProviderBusy):
		return newKindError(protocol.ACPErrProviderBusy, "")
	case errors.Is(err, ErrLoginNotFound):
		return newKindError(protocol.ACPErrInvalidParams, "unknown sign-in")
	case errors.Is(err, ErrLoginState):
		return newKindError(protocol.ACPErrInvalidState, "the sign-in cannot take this call now")
	case errors.Is(err, ErrLoginMethod):
		return newKindError(protocol.ACPErrInvalidParams, "unknown login option")
	case errors.Is(err, ErrHostClosed):
		return ErrHostClosed
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return context.Canceled
	}
	var ke *kindError
	if errors.As(err, &ke) {
		return err
	}
	return newKindError(protocol.ACPErrInternal, "")
}

func (a *Adapter) providersList(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
	return a.cfg.ProviderSetup.List(ctx)
}

func (a *Adapter) providersAPIKey(ctx context.Context, owner string, raw json.RawMessage) (any, error) {
	var req protocol.ACPAPIKeyRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if req.LoginID == "" || req.ProviderID == "" {
		return nil, newKindError(protocol.ACPErrInvalidParams, "login id and provider id are required")
	}
	return a.cfg.ProviderSetup.SaveKey(ctx, owner, req)
}

func (a *Adapter) loginStart(ctx context.Context, owner string, raw json.RawMessage) (any, error) {
	var req protocol.ACPLoginStartRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if req.ProviderID == "" || req.MethodID == "" {
		return nil, newKindError(protocol.ACPErrInvalidParams, "provider id and method id are required")
	}
	return a.cfg.ProviderSetup.LoginStart(ctx, owner, req)
}

func (a *Adapter) loginInput(_ context.Context, owner string, raw json.RawMessage) (any, error) {
	var req protocol.ACPLoginInputRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if req.LoginID == "" {
		return nil, newKindError(protocol.ACPErrInvalidParams, "login id is required")
	}
	if err := a.cfg.ProviderSetup.LoginInput(owner, req); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (a *Adapter) loginWait(ctx context.Context, owner string, raw json.RawMessage) (any, error) {
	var req protocol.ACPLoginIDRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if req.LoginID == "" {
		return nil, newKindError(protocol.ACPErrInvalidParams, "login id is required")
	}
	return a.cfg.ProviderSetup.LoginWait(ctx, owner, req.LoginID)
}

func (a *Adapter) loginCancel(_ context.Context, owner string, raw json.RawMessage) (any, error) {
	var req protocol.ACPLoginIDRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if req.LoginID == "" {
		return nil, newKindError(protocol.ACPErrInvalidParams, "login id is required")
	}
	before, err := a.cfg.ProviderSetup.LoginCancel(owner, req.LoginID)
	if err != nil {
		return nil, err
	}
	return protocol.ACPLoginCancelResult{Cancelled: before}, nil
}

// providersLogout removes a credential. When the request names the caller's
// session, the result says whether that session lost its model, so the caller's
// note does not depend on the order of this answer and the model_update event.
func (a *Adapter) providersLogout(ctx context.Context, _ string, raw json.RawMessage) (any, error) {
	var req protocol.ACPLogoutRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if req.ProviderID == "" || req.MethodID == "" {
		return nil, newKindError(protocol.ACPErrInvalidParams, "provider id and method id are required")
	}
	var caller *Session
	var before string
	var beforeProvider string
	if req.SessionID != "" {
		s, err := a.session(req.SessionID)
		if err != nil {
			return nil, err
		}
		caller = s
		m := s.Agent().State().Model
		before, beforeProvider = modelID(m), m.Provider
		if m.ID == "" {
			before, beforeProvider = "", ""
		}
	}
	res, err := a.cfg.ProviderSetup.Logout(ctx, req)
	if err != nil {
		return nil, err
	}
	if caller != nil && res.Removed && before != "" && beforeProvider == req.ProviderID {
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerCallTimeout)
		usable := a.cfg.ProviderSetup.Usable(uctx, req.ProviderID)
		cancel()
		if !usable {
			st := caller.Agent().State()
			switch {
			case st.Model.ID == "":
				res.ClearedModel = before
			case st.Status == agent.Running && modelID(st.Model) == before:
				res.ClearedModel, res.ClearPending = before, true
			}
		}
	}
	return res, nil
}

func (a *Adapter) releaseClient(_ context.Context, owner string, _ json.RawMessage) (any, error) {
	a.cfg.ProviderSetup.ReleaseClient(owner)
	return struct{}{}, nil
}

// ProvidersChanged tells the host that a credential was saved or removed. It
// clears the model of every live session whose provider is no longer usable
// (a busy session clears when its run ends) and sends _ask/providers/changed
// to the link. The composition calls it from the change callback of the
// credential broker, in the order of the generation numbers.
func (a *Adapter) ProvidersChanged(generation uint64, provider string, change protocol.ACPProviderChange) {
	if change == protocol.ACPProviderChangeRemoved && a.cfg.ProviderSetup != nil {
		ctx, cancel := context.WithTimeout(context.Background(), providerCallTimeout)
		usable := a.cfg.ProviderSetup.Usable(ctx, provider)
		cancel()
		if !usable {
			for _, s := range a.host.Sessions() {
				s.commit.Lock()
				if s.Agent().State().Model.Provider == provider {
					s.Agent().ClearModel()
				}
				s.commit.Unlock()
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-a.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	out, err := a.waitOut(ctx)
	if err != nil {
		return
	}
	// A failed write also fails the host through the checked writer, so the
	// error needs no second report.
	_ = out.n.NotifyExtension(ctx, protocol.ACPProvidersChanged, protocol.ACPProvidersChangedNotification{
		Generation: generation, ProviderID: provider, Change: change,
	})
}
