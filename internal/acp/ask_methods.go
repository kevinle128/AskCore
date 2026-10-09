package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
)

// HandleExtensionMethod serves the Ask methods. The SDK calls it for every
// method whose name starts with an underscore.
func (a *Adapter) HandleExtensionMethod(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	handler, ok := a.askHandlers()[method]
	if !ok {
		switch method {
		case protocol.ACPCompact, protocol.ACPFork, protocol.ACPTree:
			if err := a.gate(); err != nil {
				return nil, requestError(err)
			}
			return nil, requestError(unsupported(method))
		}
		return nil, sdk.NewMethodNotFound(method)
	}
	if err := a.gate(); err != nil {
		return nil, requestError(err)
	}
	// The route is checked once for every Ask method. A route that is present
	// but not valid is refused here, so no handler can take it for an editor call.
	route, err := routeFromParams(raw)
	if err == nil {
		err = a.needRoute(route)
	}
	if err == nil && route != nil {
		// Decode the session the way the handlers do, so both read the same key.
		var named protocol.ACPSessionRequest
		_ = json.Unmarshal(raw, &named)
		err = a.checkRouteSession(route, named.SessionID)
	}
	if err != nil {
		return nil, requestError(err)
	}
	res, err := handler(withRouteCtx(ctx, route), raw)
	if err != nil {
		return nil, requestError(err)
	}
	return res, nil
}

type askHandler func(ctx context.Context, raw json.RawMessage) (any, error)

func (a *Adapter) askHandlers() map[string]askHandler {
	return map[string]askHandler{
		protocol.ACPState:       a.state,
		protocol.ACPModels:      a.models,
		protocol.ACPSetModel:    a.setModel,
		protocol.ACPSetThinking: a.setThinking,
		protocol.ACPContinue:    a.continueRun,
		protocol.ACPReset:       a.reset,
		protocol.ACPSteer:       func(ctx context.Context, raw json.RawMessage) (any, error) { return a.enqueue(ctx, raw, false) },
		protocol.ACPFollowUp:    func(ctx context.Context, raw json.RawMessage) (any, error) { return a.enqueue(ctx, raw, true) },
		protocol.ACPRemove:      a.remove,
		protocol.ACPFollow:      a.follow,
		protocol.ACPUnfollow:    a.unfollow,
		protocol.ACPUsage:       a.usage,
		protocol.ACPTake:        a.take,
	}
}

// sessionOf decodes a request that names a session and returns the session.
func (a *Adapter) sessionOf(raw json.RawMessage) (*Session, error) {
	var req protocol.ACPSessionRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	return a.session(req.SessionID)
}

// modelID names a model row as "provider/id@api". Two rows can share provider
// and id, so the API makes the name reversible.
func modelID(m providers.Model) string {
	return fmt.Sprintf("%s/%s@%s", m.Provider, m.ID, m.API)
}

// parseModelID is the reverse of modelID. A name without "@api" gives an empty
// API, which the catalog refuses when two rows match.
func parseModelID(s string) (providers.Ref, error) {
	provider, rest, ok := strings.Cut(s, "/")
	if !ok || provider == "" || rest == "" {
		return providers.Ref{}, newKindError(protocol.ACPErrInvalidModel, "model id is not provider/id@api")
	}
	id, api := rest, ""
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		id, api = rest[:i], rest[i+1:]
		if id == "" || api == "" {
			return providers.Ref{}, newKindError(protocol.ACPErrInvalidModel, "model id is not provider/id@api")
		}
	}
	return providers.Ref{Provider: provider, ID: id, API: providers.API(api)}, nil
}

func (a *Adapter) state(_ context.Context, raw json.RawMessage) (any, error) {
	s, err := a.sessionOf(raw)
	if err != nil {
		return nil, err
	}
	st := s.Agent().State()
	steering, followUp := s.Queues()
	return protocol.ACPStateResult{
		SessionID:     s.ID,
		Epoch:         s.Epoch(),
		Running:       st.Status == agent.Running,
		ModelID:       modelID(st.Model),
		ThinkingLevel: st.ThinkingLevel,
		MessageCount:  len(st.Messages),
		Steering:      steering,
		FollowUp:      followUp,
	}, nil
}

func (a *Adapter) models(_ context.Context, raw json.RawMessage) (any, error) {
	s, err := a.sessionOf(raw)
	if err != nil {
		return nil, err
	}
	rows := a.cfg.Models()
	out := protocol.ACPModelsResult{Current: modelID(s.Agent().State().Model), Models: make([]protocol.ACPModel, 0, len(rows))}
	for _, m := range rows {
		out.Models = append(out.Models, protocol.ACPModel{
			ModelID: modelID(m), Provider: m.Provider, ID: m.ID, Name: m.Name, API: string(m.API),
			Reasoning: m.Reasoning, Input: append([]string{}, m.Input...),
			ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
		})
	}
	return out, nil
}

// setModel switches the model of an idle session. The sign-in method that the
// client names must be the configured one. A refusal changes nothing and starts
// no inference.
func (a *Adapter) setModel(ctx context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPModelRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	s, err := a.session(req.SessionID)
	if err != nil {
		return nil, err
	}
	release, err := s.guard(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer release()
	ref, err := parseModelID(req.ModelID)
	if err != nil {
		return nil, err
	}
	m, err := a.cfg.FindModel(ref)
	if err != nil {
		return nil, newKindError(protocol.ACPErrInvalidModel, "unknown or ambiguous model")
	}
	if req.AuthMethodID != "" {
		if a.cfg.ModelAuth == nil {
			return nil, newKindError(protocol.ACPErrInvalidParams, "authMethodId is not supported")
		}
		if err := a.cfg.ModelAuth(ctx, m, req.AuthMethodID); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAuth, err)
		}
	}
	if err := s.Agent().SetModel(s.mutationContext(ctx, routeOf(ctx)), m); err != nil {
		if classify(err) == protocol.ACPErrInternal {
			err = fmt.Errorf("%w: %w", ErrAuth, err)
		}
		return nil, err
	}
	st := s.Agent().State()
	return protocol.ACPSetModelResult{ModelID: modelID(st.Model), ThinkingLevel: st.ThinkingLevel}, nil
}

func validThinking(l protocol.ThinkingLevel) bool {
	switch l {
	case protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
		protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax:
		return true
	}
	return false
}

func (a *Adapter) setThinking(ctx context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPThinkingRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	s, err := a.session(req.SessionID)
	if err != nil {
		return nil, err
	}
	release, err := s.guard(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer release()
	if !validThinking(req.Level) {
		return nil, newKindError(protocol.ACPErrInvalidParams, "unknown thinking level")
	}
	commit, err := s.mutationCommit(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer commit()
	if err := s.Agent().SetThinkingLevel(req.Level); err != nil {
		return nil, err
	}
	return protocol.ACPThinkingResult{Level: s.Agent().State().ThinkingLevel}, nil
}

func (a *Adapter) continueRun(ctx context.Context, raw json.RawMessage) (any, error) {
	s, err := a.sessionOf(raw)
	if err != nil {
		return nil, err
	}
	release, err := s.guard(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer release()
	res, err := s.Continue(s.mutationContext(ctx, routeOf(ctx)))
	if err != nil {
		return nil, err
	}
	if err := runError(res); err != nil {
		return nil, err
	}
	return protocol.ACPContinueResult{StopReason: string(stopReason(res.Reason))}, nil
}

func (a *Adapter) reset(ctx context.Context, raw json.RawMessage) (any, error) {
	s, err := a.sessionOf(raw)
	if err != nil {
		return nil, err
	}
	release, err := s.guard(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer release()
	commit, err := s.mutationCommit(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer commit()
	cut, err := s.Reset(ctx)
	if err != nil {
		return nil, err
	}
	return protocol.ACPResetResult{SessionID: s.ID, Epoch: cut.Cursor.Epoch}, nil
}

// enqueue admits a steering or follow-up message. The ID that it returns names
// the queued input; it is not a run ID.
func (a *Adapter) enqueue(ctx context.Context, raw json.RawMessage, followUp bool) (any, error) {
	var req protocol.ACPInputRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	s, err := a.session(req.SessionID)
	if err != nil {
		return nil, err
	}
	msg, err := inputMessage(req)
	if err != nil {
		return nil, err
	}
	release, err := s.guard(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer release()
	commit, err := s.mutationCommit(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer commit()
	var id string
	if followUp {
		id, err = s.FollowUp(msg)
	} else {
		id, err = s.Steer(msg)
	}
	if err != nil {
		return nil, err
	}
	return protocol.ACPInputResult{InputID: id}, nil
}

func (a *Adapter) remove(ctx context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPRemoveRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	s, err := a.session(req.SessionID)
	if err != nil {
		return nil, err
	}
	release, err := s.guard(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer release()
	commit, err := s.mutationCommit(routeOf(ctx))
	if err != nil {
		return nil, err
	}
	defer commit()
	return protocol.ACPRemoveResult{Removed: s.Agent().Remove(req.InputID)}, nil
}

// usage reports the exact usage of each model attempt from the session log.
// It is a read of committed rows, not a billing store.
func (a *Adapter) usage(_ context.Context, raw json.RawMessage) (any, error) {
	s, err := a.sessionOf(raw)
	if err != nil {
		return nil, err
	}
	f := s.Agent().Follow(agent.Cursor{})
	f.Events.Close()
	return usageResult(f.Entries), nil
}

// usageResult projects the attempt rows of a session log. A row holds the
// outcome and the usage only: the failure of an attempt is not part of it.
func usageResult(entries []sessions.Entry) protocol.ACPUsageResult {
	res := protocol.ACPUsageResult{Attempts: []protocol.ACPUsageRow{}, Complete: true}
	cycle := ""
	for _, e := range entries {
		switch v := e.(type) {
		case sessions.CycleOpened:
			cycle = v.CycleID
		case sessions.CycleClosed:
			cycle = ""
		case sessions.AttemptSettled:
			row := protocol.ACPUsageRow{AttemptID: v.AttemptID, CycleID: cycle, Outcome: v.Outcome}
			if v.Usage != nil {
				u := v.Usage.Clone()
				row.Usage = &u
				addUsage(&res.Total, u)
			} else {
				res.Complete = false
			}
			res.Attempts = append(res.Attempts, row)
		}
	}
	return res
}

// addUsage adds u to dst. An optional counter stays unset until one attempt
// reports it.
func addUsage(dst *protocol.Usage, u protocol.Usage) {
	dst.Input += u.Input
	dst.Output += u.Output
	dst.CacheRead += u.CacheRead
	dst.CacheWrite += u.CacheWrite
	dst.TotalTokens += u.TotalTokens
	dst.Cost.Input += u.Cost.Input
	dst.Cost.Output += u.Cost.Output
	dst.Cost.CacheRead += u.Cost.CacheRead
	dst.Cost.CacheWrite += u.Cost.CacheWrite
	dst.Cost.Total += u.Cost.Total
	dst.CacheWrite1h = addOptional(dst.CacheWrite1h, u.CacheWrite1h)
	dst.Reasoning = addOptional(dst.Reasoning, u.Reasoning)
}

func addOptional(dst, v *int64) *int64 {
	if v == nil {
		return dst
	}
	sum := *v
	if dst != nil {
		sum += *dst
	}
	return &sum
}

// take makes the calling client the driver of a session. The leader router
// sends it with the route context. The host commits it and returns the new
// generation, which the router copies.
func (a *Adapter) take(ctx context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPSessionRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	route := routeOf(ctx)
	if route == nil {
		return nil, newKindError(protocol.ACPErrInvalidParams, "take needs a leader route")
	}
	s, err := a.session(req.SessionID)
	if err != nil {
		return nil, err
	}
	gen, err := s.Take(*route)
	if err != nil {
		return nil, err
	}
	return protocol.ACPTakeResult{SessionID: s.ID, DriverGen: gen}, nil
}
