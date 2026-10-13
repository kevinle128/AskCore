package app

import (
	"context"
	"errors"
	"time"

	"AskCore/internal/acp"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// providerSetup serves the provider methods of the leader over the
// credential broker. It keeps no secret: a key or a code lives only in the
// call that carries it.
type providerSetup struct {
	broker  *auth.Broker
	service *auth.Service
	usable  func(ctx context.Context, provider string) bool
	rows    func() []providers.Model
}

var _ acp.ProviderSetup = (*providerSetup)(nil)

// newProviderSetup builds the provider setup of a runtime. onChange is called
// after every saved credential and every removal, in generation order, and
// must not call back into the provider setup.
func newProviderSetup(rt *ACPRuntime, onChange func(generation uint64, provider string, change protocol.ACPProviderChange)) *providerSetup {
	broker := auth.NewBroker(rt.Service, auth.BrokerOptions{
		CheckKey: auth.NativeKeyCheck(rt.Native),
		HostID:   rt.Service.Store.HostID,
		OnChange: func(generation uint64, provider string, change auth.Change) {
			kind := protocol.ACPProviderChangeSaved
			if change == auth.ChangeRemoved {
				kind = protocol.ACPProviderChangeRemoved
			}
			onChange(generation, provider, kind)
		},
	})
	return &providerSetup{broker: broker, service: rt.Service, usable: rt.Usable, rows: rt.Rows}
}

// Close cancels every sign-in and waits until each has ended. It must run
// before the credential refresh drains, because a sign-in holds a refresh slot.
func (p *providerSetup) Close() { p.broker.Close() }

func (p *providerSetup) List(ctx context.Context) (protocol.ACPProvidersResult, error) {
	var states []auth.OptionState
	var generation uint64
	for {
		if err := ctx.Err(); err != nil {
			return protocol.ACPProvidersResult{}, err
		}
		generation = p.broker.Generation()
		var err error
		states, err = p.service.Status(ctx)
		if err != nil {
			return protocol.ACPProvidersResult{}, err
		}
		if generation == p.broker.Generation() {
			break
		}
	}
	res := protocol.ACPProvidersResult{
		Generation: generation,
		Options:    make([]protocol.ACPLoginOption, 0, len(states)),
		Providers:  []protocol.ACPProviderStatus{},
	}
	usable := map[string]bool{}
	for _, s := range states {
		usable[s.Provider] = usable[s.Provider] || s.State != auth.StateNone
		res.Options = append(res.Options, protocol.ACPLoginOption{
			ProviderID: s.Provider, MethodID: s.Method, Name: s.Name,
			State: protocol.ACPLoginState(s.State), EnvVar: s.EnvVar,
		})
	}
	// One status for each provider of the catalog, in catalog order.
	index := map[string]int{}
	for _, m := range p.rows() {
		i, ok := index[m.Provider]
		if !ok {
			i = len(res.Providers)
			index[m.Provider] = i
			ready, known := usable[m.Provider]
			if !known {
				ready = p.usable(ctx, m.Provider)
			}
			res.Providers = append(res.Providers, protocol.ACPProviderStatus{ProviderID: m.Provider, Usable: ready})
		}
		res.Providers[i].ModelCount++
	}
	return res, nil
}

func (p *providerSetup) SaveKey(ctx context.Context, owner string, req protocol.ACPAPIKeyRequest) (protocol.ACPLoginOutcome, error) {
	out, err := p.broker.SaveKey(ctx, owner, req.LoginID, req.ProviderID, req.Key)
	if err != nil {
		return protocol.ACPLoginOutcome{}, mapAuthError(err)
	}
	return outcomeOf(req.LoginID, out), nil
}

func (p *providerSetup) LoginStart(ctx context.Context, owner string, req protocol.ACPLoginStartRequest) (protocol.ACPLoginStartResult, error) {
	info, err := p.broker.Start(ctx, owner, req.ProviderID, req.MethodID)
	if err != nil {
		return protocol.ACPLoginStartResult{}, mapAuthError(err)
	}
	res := protocol.ACPLoginStartResult{LoginID: info.LoginID, Kind: protocol.ACPLoginKind(info.Kind), URL: info.URL, UserCode: info.UserCode}
	if !info.ExpiresAt.IsZero() {
		res.ExpiresAt = info.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return res, nil
}

func (p *providerSetup) LoginInput(owner string, req protocol.ACPLoginInputRequest) error {
	return mapAuthError(p.broker.Input(owner, req.LoginID, req.Code))
}

func (p *providerSetup) LoginWait(ctx context.Context, owner, loginID string) (protocol.ACPLoginOutcome, error) {
	out, err := p.broker.Wait(ctx, owner, loginID)
	if err != nil {
		return protocol.ACPLoginOutcome{}, mapAuthError(err)
	}
	return outcomeOf(loginID, out), nil
}

func (p *providerSetup) LoginCancel(owner, loginID string) (bool, error) {
	before, err := p.broker.Cancel(owner, loginID)
	return before, mapAuthError(err)
}

func (p *providerSetup) Logout(ctx context.Context, req protocol.ACPLogoutRequest) (protocol.ACPLogoutResult, error) {
	info, err := p.broker.Logout(ctx, req.ProviderID, req.MethodID)
	if err != nil {
		return protocol.ACPLogoutResult{}, mapAuthError(err)
	}
	return protocol.ACPLogoutResult{Removed: true, EnvVar: info.EnvVar, Generation: p.broker.Generation()}, nil
}

func (p *providerSetup) ReleaseClient(owner string) { p.broker.ReleaseOwner(owner) }

func (p *providerSetup) Usable(ctx context.Context, provider string) bool {
	return p.usable(ctx, provider)
}

// outcomeOf maps a broker outcome to the wire shape. No text of the provider
// crosses it: the reason is a fixed token and the code was checked by the broker.
func outcomeOf(loginID string, o auth.Outcome) protocol.ACPLoginOutcome {
	return protocol.ACPLoginOutcome{
		LoginID: loginID, Status: protocol.ACPLoginStatus(o.Status), Reason: protocol.ACPLoginReason(o.Reason),
		HTTPStatus: o.HTTPStatus, ProviderCode: o.Code, NotChecked: o.NotChecked, Account: o.Account, Generation: o.Generation,
	}
}

// mapAuthError turns a broker error into an error that the adapter maps to a
// fixed kind. An error that has no kind stays as it is: the adapter then sends
// the internal kind and never its text.
func mapAuthError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, auth.ErrProviderBusy):
		return acp.ErrProviderBusy
	case errors.Is(err, auth.ErrLoginNotFound):
		return acp.ErrLoginNotFound
	case errors.Is(err, auth.ErrInvalidState):
		return acp.ErrLoginState
	case errors.Is(err, auth.ErrMethod):
		return acp.ErrLoginMethod
	case errors.Is(err, auth.ErrBrokerClosed):
		return acp.ErrHostClosed
	}
	return err
}
