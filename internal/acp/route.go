package acp

import (
	"context"
	"encoding/json"

	"AskCore/pkg/protocol"
)

// routeFromMeta reads the route context that the leader router put in the _meta
// of a call. It returns nil when there is none, which is the editor link. A
// context that is present but not valid is an error: it must never fall back to
// the editor path, where no driver generation is checked.
func routeFromMeta(meta map[string]any) (*protocol.ACPRouteMeta, error) {
	raw, ok := meta[protocol.ACPRouteMetaKey]
	if !ok {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, errBadRoute
	}
	return decodeRoute(data)
}

// routeFromParams reads the route context from the raw params of an Ask method.
func routeFromParams(raw json.RawMessage) (*protocol.ACPRouteMeta, error) {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil, errBadRoute
	}
	data, ok := p.Meta[protocol.ACPRouteMetaKey]
	if !ok {
		return nil, nil
	}
	return decodeRoute(data)
}

func decodeRoute(data []byte) (*protocol.ACPRouteMeta, error) {
	var r protocol.ACPRouteMeta
	// A generation must be decimal text. A number would lose bits above 2^53.
	if err := json.Unmarshal(data, &r); err != nil || r.ClientID == "" {
		return nil, errBadRoute
	}
	return &r, nil
}

var errBadRoute = newKindError(protocol.ACPErrInvalidParams, "malformed route context")

// needRoute checks that a call has the route context that the link requires.
func (a *Adapter) needRoute(route *protocol.ACPRouteMeta) error {
	if route == nil && a.cfg.RequireRoute {
		return newKindError(protocol.ACPErrInvalidParams, "this connection needs a route context")
	}
	return nil
}

// checkRouteSession refuses a call whose own session id differs from the one
// that the router checked the caller against. On the leader link the route must
// name the session. This is the host's own check, so a call that reaches the
// host with keys the router did not read cannot act on another session.
func (a *Adapter) checkRouteSession(route *protocol.ACPRouteMeta, sessionID string) error {
	if route == nil {
		return nil
	}
	if route.SessionID == "" {
		if a.cfg.RequireRoute {
			return errBadRoute
		}
		return nil
	}
	if route.SessionID != sessionID {
		return errBadRoute
	}
	return nil
}

// metaRoute reads the route from the _meta of a standard call and checks that
// the link has one when it must.
func (a *Adapter) metaRoute(meta map[string]any) (*protocol.ACPRouteMeta, error) {
	route, err := routeFromMeta(meta)
	if err != nil {
		return nil, err
	}
	return route, a.needRoute(route)
}

type routeCtxKey struct{}

func withRouteCtx(ctx context.Context, route *protocol.ACPRouteMeta) context.Context {
	return context.WithValue(ctx, routeCtxKey{}, route)
}

// routeOf returns the route that HandleExtensionMethod checked, or nil.
func routeOf(ctx context.Context) *protocol.ACPRouteMeta {
	route, _ := ctx.Value(routeCtxKey{}).(*protocol.ACPRouteMeta)
	return route
}
