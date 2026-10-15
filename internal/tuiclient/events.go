package tuiclient

import (
	"context"
	"encoding/json"
	"fmt"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

type followMode int

const (
	// followFresh follows a session that the client just created or attached.
	followFresh followMode = iota
	// followRestore follows from the last cursor after a reconnect.
	followRestore
	// followResync follows again after the leader dropped a slow subscription.
	followResync
)

// follow starts a subscription. The answer is committed on the reader, before
// the reader takes the next frame: the subscription, the cursor and the snapshot
// are in place when the first live event arrives.
func (c *Client) follow(ctx context.Context, conn rpcCaller, sid string, cursor *protocol.ACPCursor, mode followMode) error {
	params := protocol.ACPFollowRequest{SessionID: sid, Cursor: cursor}
	_, err := conn.CallCommit(ctx, protocol.ACPFollow, params, func(res json.RawMessage) { c.commitFollow(sid, res, mode) })
	return translate(err)
}

func (c *Client) commitFollow(sid string, raw json.RawMessage, mode followMode) {
	var res protocol.ACPFollowResult
	if json.Unmarshal(raw, &res) != nil || res.SubscriptionID == "" {
		c.events.push(tui.ClientErrorMsg{Err: fmt.Errorf("tuiclient: the follow answer is not valid")})
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.sess.has || c.sess.id != sid {
		return // the UI moved to another session while the call was in flight
	}
	c.sess.sub, c.sess.epoch, c.sess.seq = res.SubscriptionID, res.Cursor.Epoch, res.Cursor.Seq
	if mode == followRestore {
		c.events.push(tui.SessionRestoredMsg{
			Session: tui.SessionInfo{ID: sid, Role: c.sess.role, Cwd: c.sess.cwd},
			Resumed: res.Resumed,
		})
	}
	snap := tui.SnapshotMsg{
		SessionID: sid, Cursor: res.Cursor, Entries: decodeEntries(res.Entries), Stream: res.Stream,
		Resumed: res.Resumed, Resync: res.Resync || mode == followResync,
	}
	if res.Head != nil {
		snap.Head = *res.Head
	}
	c.events.push(snap)
}

// decodeEntries reads the rows of a snapshot. A row that cannot be read keeps
// its raw form and has no Message.
func decodeEntries(rows []json.RawMessage) []tui.SnapshotEntry {
	out := make([]tui.SnapshotEntry, 0, len(rows))
	for _, raw := range rows {
		e := tui.SnapshotEntry{Raw: raw}
		var head struct {
			Kind    string          `json:"kind"`
			Message json.RawMessage `json:"message"`
			InputID string          `json:"inputId"`
		}
		if json.Unmarshal(raw, &head) == nil {
			e.Kind, e.InputID = head.Kind, head.InputID
			if head.Kind == "message" && len(head.Message) > 0 {
				if m, err := protocol.UnmarshalMessage(head.Message); err == nil {
					e.Message = m
				}
			}
		}
		out = append(out, e)
	}
	return out
}

// Notification handles a notification of the agent on the reader. It never
// waits for an answer of its own connection.
func (h *handler) Notification(method string, params json.RawMessage) {
	c := h.c
	switch method {
	case protocol.ACPEvent:
		var p protocol.ACPEventNotification
		if json.Unmarshal(params, &p) != nil {
			c.events.push(tui.ClientErrorMsg{Err: fmt.Errorf("tuiclient: an event notification is not valid")})
			return
		}
		c.onEvent(p)
	case protocol.ACPResync:
		var p protocol.ACPResyncNotification
		if json.Unmarshal(params, &p) != nil {
			return
		}
		c.onResync(h.conn, p)
	case protocol.ACPProvidersChanged:
		var p protocol.ACPProvidersChangedNotification
		if json.Unmarshal(params, &p) != nil {
			return
		}
		c.onProvidersChanged(h.instance, p)
	}
	// session/update repeats what the events say, and $/cancel_request closes a
	// question that this client already refused.
}

// Request handles a call of the agent. The client offers no file or terminal.
// A question about a permission is answered as cancelled, so the agent never
// waits for a person who is not there.
func (h *handler) Request(id json.RawMessage, method string, _ json.RawMessage) {
	if method == "session/request_permission" {
		_ = h.conn.Respond(id, map[string]any{"outcome": map[string]any{"outcome": "cancelled"}})
		return
	}
	_ = h.conn.RespondError(id, -32601, "not supported")
}

// onProvidersChanged delivers a providers-changed notice unless the generation
// is not newer than the last one of the same leader instance. A restarted
// leader counts from zero again, so a new instance resets the last value.
func (c *Client) onProvidersChanged(instance string, p protocol.ACPProvidersChangedNotification) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.providersInstance != instance {
		c.providersInstance, c.providersGen = instance, 0
	}
	if p.Generation <= c.providersGen {
		return
	}
	c.providersGen = p.Generation
	c.events.push(tui.ProvidersChangedMsg{Generation: p.Generation, Provider: p.ProviderID, Change: p.Change})
}

func (c *Client) onEvent(p protocol.ACPEventNotification) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.sess.has || c.sess.sub == "" || p.SubscriptionID != c.sess.sub {
		return // an ended subscription has no reader
	}
	if p.Epoch == c.sess.epoch && p.Seq <= c.sess.seq {
		return // delivered before; the UI sees each event once
	}
	c.sess.epoch, c.sess.seq = p.Epoch, p.Seq
	ev, err := protocol.DecodeEvent(p.Event)
	if err != nil {
		c.events.push(tui.ClientErrorMsg{Err: fmt.Errorf("tuiclient: event %d of %s: %w", p.Seq, p.SessionID, err)})
		return
	}
	// A run start gives an early acceptance signal without request identity.
	// The correlated admission error can still refuse this prompt.
	if _, ok := ev.(*protocol.AgentStart); ok && c.prompt != nil && !c.prompt.accepted {
		c.prompt.accepted = true
		c.events.push(tui.PromptAcceptedMsg{ID: c.prompt.id})
	}
	c.events.push(tui.SessionEventMsg{SessionID: p.SessionID, Epoch: p.Epoch, Seq: p.Seq, Event: ev})
}

// onResync starts a new follow. The old subscription is dropped at once, so no
// late event of it reaches the UI.
func (c *Client) onResync(conn rpcCaller, p protocol.ACPResyncNotification) {
	c.mu.Lock()
	if !c.sess.has || p.SubscriptionID != c.sess.sub {
		c.mu.Unlock()
		return
	}
	c.sess.sub = ""
	sid := c.sess.id
	c.mu.Unlock()
	// The follow call waits for its answer, which the reader delivers: it runs
	// on its own goroutine.
	c.goSafe("resync", false, func() {
		ctx, cancel := context.WithTimeout(c.ctx, requestTimeout)
		defer cancel()
		if err := c.follow(ctx, conn, sid, nil, followResync); err != nil {
			c.reportRestoreError(err)
		}
	})
}
