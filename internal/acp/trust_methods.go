package acp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"AskCore/pkg/protocol"
)

// TrustSetup is what the host needs to serve the project trust methods. The
// composition implements it over the trust store; the adapter imports no
// settings code. The adapter checks the parameters before it calls.
type TrustSetup interface {
	// State returns the trust decision for a cwd as a client that can ask the
	// user would get it. It never saves a decision.
	State(ctx context.Context, req protocol.ACPTrustStateRequest) (protocol.ACPTrustStateResult, error)
	// Set saves a decision and returns the new state of the cwd. It returns a
	// *TrustStoreError when the store cannot be changed.
	Set(ctx context.Context, req protocol.ACPTrustSetRequest) (protocol.ACPTrustStateResult, error)
}

// TrustStoreError means that the trust store is corrupt or not safe, so a
// decision cannot be saved. The adapter sends Warning, a fixed text with the
// path of the store, as the detail of an invalid_state error.
type TrustStoreError struct {
	Warning string
}

func (e *TrustStoreError) Error() string { return e.Warning }

// SessionTrust is the trust decision and the settings view that a session
// started with.
type SessionTrust struct {
	Trust       protocol.ACPSessionTrust
	Diagnostics []protocol.ACPDiagnostic
	// ThinkingFold is the effective thinkingFold setting of the session.
	ThinkingFold string
}

// SettingsScope names the settings that apply to a session: its cwd and
// whether its project layer applies.
type SettingsScope struct {
	Cwd     string
	Trusted bool
}

// trustFromMeta reads the trust override of a session/new call. No key gives
// the empty override. A value other than "approve" or "deny" is an error.
func trustFromMeta(meta map[string]any) (protocol.ACPTrustOverride, error) {
	raw, ok := meta[protocol.ACPTrustMetaKey]
	if !ok {
		return "", nil
	}
	word, _ := raw.(string)
	switch o := protocol.ACPTrustOverride(word); o {
	case protocol.ACPTrustApprove, protocol.ACPTrustDeny:
		return o, nil
	}
	return "", newKindError(protocol.ACPErrInvalidParams, `the trust override must be "approve" or "deny"`)
}

type trustHandler func(ctx context.Context, raw json.RawMessage) (any, error)

func (a *Adapter) trustHandlers() map[string]trustHandler {
	return map[string]trustHandler{
		protocol.ACPTrustState: a.trustState,
		protocol.ACPTrustSet:   a.trustSet,
	}
}

// handleTrustMethod serves a trust method. handled is false when method is not
// one. Without a TrustSetup both methods answer unsupported. A route is not
// needed on a link without routes; a route that is present must be valid,
// and the leader link needs one. The calls name no session.
func (a *Adapter) handleTrustMethod(ctx context.Context, method string, raw json.RawMessage) (handled bool, res any, err error) {
	handler, ok := a.trustHandlers()[method]
	if !ok {
		return false, nil, nil
	}
	if err := a.gate(); err != nil {
		return true, nil, requestError(err)
	}
	if a.cfg.TrustSetup == nil {
		return true, nil, requestError(unsupported(method))
	}
	route, err := routeFromParams(raw)
	if err == nil {
		err = a.needRoute(route)
	}
	if err != nil {
		return true, nil, requestError(err)
	}
	out, err := handler(ctx, raw)
	if err != nil {
		return true, nil, requestError(trustSetupError(err))
	}
	return true, out, nil
}

// trustSetupError maps an error of the trust setup to a kind with a fixed text.
func trustSetupError(err error) error {
	var store *TrustStoreError
	var ke *kindError
	switch {
	case errors.As(err, &store):
		return newKindError(protocol.ACPErrInvalidState, store.Warning)
	case errors.As(err, &ke):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return context.Canceled
	}
	return newKindError(protocol.ACPErrInternal, "")
}

func checkTrustCwd(cwd string) error {
	if !filepath.IsAbs(cwd) {
		return newKindError(protocol.ACPErrInvalidParams, "cwd must be an absolute path")
	}
	return nil
}

func (a *Adapter) trustState(ctx context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPTrustStateRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if err := checkTrustCwd(req.Cwd); err != nil {
		return nil, err
	}
	return a.cfg.TrustSetup.State(ctx, req)
}

func (a *Adapter) trustSet(ctx context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPTrustSetRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if err := checkTrustCwd(req.Cwd); err != nil {
		return nil, err
	}
	switch req.Target {
	case protocol.ACPTrustTargetFolder, protocol.ACPTrustTargetParent:
	default:
		return nil, newKindError(protocol.ACPErrInvalidParams, `target must be "folder" or "parent"`)
	}
	switch req.Decision {
	case protocol.ACPTrustDecisionTrust, protocol.ACPTrustDecisionDistrust, protocol.ACPTrustDecisionClear:
	default:
		return nil, newKindError(protocol.ACPErrInvalidParams, `decision must be "trust", "distrust" or "clear"`)
	}
	return a.cfg.TrustSetup.Set(ctx, req)
}
