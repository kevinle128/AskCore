package acp

import (
	"context"
	"encoding/json"
	"path/filepath"

	"AskCore/internal/agent"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
)

// LoadSession opens a stored session, replays its history as session/update
// frames and then answers. The Factory takes the write lock of the session, so
// a session that another process holds fails with session_in_use.
//
// On the leader link a session that the host runs already is joined, not
// opened again: the router names it with a driver generation in the route
// context, the history goes to the caller only, and no second writer exists.
// Any other call for a live session is refused.
func (a *Adapter) LoadSession(ctx context.Context, req sdk.LoadSessionRequest) (sdk.LoadSessionResponse, error) {
	if err := a.gate(); err != nil {
		return sdk.LoadSessionResponse{}, requestError(err)
	}
	route, trust, flags, err := a.startRequest(req.Cwd, req.McpServers, req.AdditionalDirectories, req.Meta)
	if err != nil {
		return sdk.LoadSessionResponse{}, requestError(err)
	}
	id := string(req.SessionId)
	if id == "" {
		return sdk.LoadSessionResponse{}, requestError(newKindError(protocol.ACPErrInvalidParams, "session id is required"))
	}
	if flags.NoSession {
		return sdk.LoadSessionResponse{}, requestError(newKindError(protocol.ACPErrInvalidParams, "a stored session cannot be loaded without saving"))
	}
	if err := a.checkRouteSession(route, id); err != nil {
		return sdk.LoadSessionResponse{}, requestError(err)
	}
	// The router sends a driver generation only for a session that it routes
	// already.
	routed := route != nil && route.DriverGen != 0
	live, isLive := a.host.Session(id)
	if !isLive && route != nil && !routed {
		// The log that a live session writes after a reset has an ID of its own.
		// The router does not know it, so the host names the session it joins.
		if byLog, ok := a.host.sessionByLogID(id); ok && sameDir(byLog.CWD, req.Cwd) {
			return a.joinLoaded(ctx, byLog, id, route, flags)
		}
	}
	switch {
	case isLive && !sameDir(live.CWD, req.Cwd):
		// A session belongs to its folder, live or stored.
		return sdk.LoadSessionResponse{}, requestError(errUnknownSession)
	case isLive && routed:
		return a.joinLoaded(ctx, live, id, route, flags)
	case isLive:
		return sdk.LoadSessionResponse{}, requestError(ErrSessionInUse)
	case routed:
		// The router believes that the host runs the session; the host does not.
		return sdk.LoadSessionResponse{}, requestError(errUnknownSession)
	}
	s, err := a.host.LoadSessionWith(ctx, SessionRequest{ID: id, Cwd: req.Cwd, Trust: trust, Flags: flags})
	if err != nil {
		return sdk.LoadSessionResponse{}, requestError(err)
	}
	a.publish(ctx, s)
	if err := a.replay(ctx, s, s.ID, route); err != nil {
		// The client got no answer, so it cannot use the session. Close it, so
		// its lock is free for the next try.
		_ = s.Close()
		return sdk.LoadSessionResponse{}, requestError(err)
	}
	return sdk.LoadSessionResponse{Meta: startMeta(s, route, flags, true)}, nil
}

// joinLoaded answers a load of a session that this host runs. It writes the
// history for the caller and leaves the driver, the model and the run alone, so
// the flags of the call do not apply.
//
// asked is the ID that the client sent. It is the ID of the session, or the ID of
// the log that the session writes now; the answer names the session then.
func (a *Adapter) joinLoaded(ctx context.Context, s *Session, asked string, route *protocol.ACPRouteMeta, flags protocol.ACPSessionOptions) (sdk.LoadSessionResponse, error) {
	if err := a.replay(ctx, s, asked, route); err != nil {
		return sdk.LoadSessionResponse{}, requestError(err)
	}
	meta := startMeta(s, route, flags, false)
	if asked != s.ID {
		if meta == nil {
			meta = map[string]any{}
		}
		meta[protocol.ACPSessionIDMetaKey] = s.ID
	}
	return sdk.LoadSessionResponse{Meta: meta}, nil
}

// replay writes the history of s as session/update frames. The frames come
// before the answer of the load, in log order. With a route they name the
// calling client, so that the router sends them to it and to no other client
// that follows the session. sessionID is the ID that the client asked for. A
// write error fails the connection; a cancelled request only stops the replay.
//
// Live events of a run that is active during a join are written by the session
// writer on its own; they can interleave with the history, which a client
// that joins a busy session has to expect.
func (a *Adapter) replay(ctx context.Context, s *Session, sessionID string, route *protocol.ACPRouteMeta) error {
	f := s.Agent().Follow(agent.Cursor{})
	f.Events.Close()
	updates := historyUpdates(f.Entries)
	if len(updates) == 0 {
		return nil
	}
	out, err := a.waitOut(ctx)
	if err != nil {
		return err
	}
	var meta map[string]any
	if route != nil {
		meta = map[string]any{protocol.ACPRouteMetaKey: protocol.ACPRouteMeta{ClientID: route.ClientID}}
	}
	for _, u := range updates {
		if err := ctx.Err(); err != nil {
			return err
		}
		// The frames go out on the host context: the SDK fails a write on a
		// cancelled context, and the end of one request is no output failure.
		if err := out.n.SessionUpdate(a.host.ctx, sdk.SessionNotification{SessionId: sdk.SessionId(sessionID), Update: u, Meta: meta}); err != nil {
			if a.host.ctx.Err() == nil {
				a.host.latch(err)
				if f := a.host.Failure(); f != nil {
					return f
				}
			}
			return err
		}
	}
	return nil
}

// sameDir tells whether two absolute paths name one directory.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// historyUpdates maps the log of a session to the standard updates that rebuild
// the conversation: user text and images, assistant text and thinking, and each
// tool call with its final status followed by its result. Seed and notice
// messages, system reminders, and every entry that is not a message, are not
// part of what the user saw, so they are not sent. A call with no result in the
// log is still running.
//
// A call ID can repeat in a log, so each result settles the oldest call with
// its ID that has no result yet, in log order.
func historyUpdates(entries []sessions.Entry) []sdk.SessionUpdate {
	var results []*protocol.ToolResultMessage
	pending := map[string][]int{}
	for _, e := range entries {
		m, ok := e.(sessions.MessageEntry)
		if !ok {
			continue
		}
		switch msg := m.Message.(type) {
		case protocol.AssistantMessage:
			for _, b := range msg.Content {
				if c, ok := b.(protocol.ToolCall); ok {
					pending[c.ID] = append(pending[c.ID], len(results))
					results = append(results, nil)
				}
			}
		case protocol.ToolResultMessage:
			if slots := pending[msg.ToolCallID]; len(slots) > 0 {
				results[slots[0]] = &msg
				pending[msg.ToolCallID] = slots[1:]
			}
		}
	}
	var out []sdk.SessionUpdate
	call := 0
	for _, e := range entries {
		m, ok := e.(sessions.MessageEntry)
		if !ok {
			continue
		}
		switch msg := m.Message.(type) {
		case protocol.UserMessage:
			if !protocol.IsSystemReminder(msg) {
				out = append(out, userUpdates(msg.Content)...)
			}
		case protocol.AssistantMessage:
			for _, b := range msg.Content {
				switch v := b.(type) {
				case protocol.Text:
					out = append(out, textUpdate(v.Text, false)...)
				case protocol.Thinking:
					out = append(out, textUpdate(v.Thinking, true)...)
				case protocol.ToolCall:
					out = append(out, toolCallUpdates(v, results[call])...)
					call++
				}
			}
		}
	}
	return out
}

func userUpdates(blocks []protocol.UserBlock) []sdk.SessionUpdate {
	var out []sdk.SessionUpdate
	for _, b := range blocks {
		switch v := b.(type) {
		case protocol.Text:
			// A prompt that the agent expanded from "/skill:<name>" shows as the
			// user typed it, not as the skill body and its path.
			text := v.Text
			if cmd, ok := protocol.SkillInputCommand(text); ok {
				text = cmd
			}
			if text != "" {
				out = append(out, sdk.UpdateUserMessageText(text))
			}
		case protocol.Image:
			out = append(out, sdk.UpdateUserMessage(sdk.ImageBlock(v.Data, v.MimeType)))
		}
	}
	return out
}

// toolCallUpdates gives the tool_call of a call with the status that its result
// settled on, then the tool_call_update with the result content.
// A nil result means the call is still running.
func toolCallUpdates(c protocol.ToolCall, r *protocol.ToolResultMessage) []sdk.SessionUpdate {
	done := r != nil
	status := sdk.ToolCallStatusInProgress
	if done {
		status = sdk.ToolCallStatusCompleted
		if r.IsError {
			status = sdk.ToolCallStatusFailed
		}
	}
	opts := []sdk.ToolCallStartOpt{sdk.WithStartKind(kindOf(c.Name)), sdk.WithStartStatus(status)}
	if json.Valid(c.Arguments) {
		opts = append(opts, sdk.WithStartRawInput(json.RawMessage(c.Arguments)))
	}
	out := []sdk.SessionUpdate{sdk.StartToolCall(sdk.ToolCallId(c.ID), c.Name, opts...)}
	if !done {
		return out
	}
	// The result goes through the mapping of a live result, so a replayed call
	// shows the same kind, locations and diff.
	return append(out, standardUpdates(&protocol.ToolExecutionEnd{
		ToolCallID: c.ID, ToolName: c.Name, IsError: r.IsError,
		Result: protocol.ToolExecutionResult{Content: r.Content, Details: r.Details},
	})...)
}
