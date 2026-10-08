package acp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"

	"AskCore/internal/agent"
	"AskCore/internal/bus"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
)

// standardUpdates maps one Agent event to the standard session/update frames.
// Events that have no standard form give none; a follower gets them whole.
func standardUpdates(ev protocol.Event) []sdk.SessionUpdate {
	switch e := ev.(type) {
	case *protocol.MessageUpdate:
		return blockUpdates(e.AssistantMessageEvent)
	case *protocol.ToolExecutionStart:
		return []sdk.SessionUpdate{sdk.UpdateToolCall(sdk.ToolCallId(e.ToolCallID), sdk.WithUpdateStatus(sdk.ToolCallStatusInProgress))}
	case *protocol.ToolExecutionUpdate:
		opts := []sdk.ToolCallUpdateOpt{sdk.WithUpdateStatus(sdk.ToolCallStatusInProgress)}
		if c := toolContent(e.PartialResult.Content); len(c) > 0 {
			opts = append(opts, sdk.WithUpdateContent(c))
		}
		return []sdk.SessionUpdate{sdk.UpdateToolCall(sdk.ToolCallId(e.ToolCallID), opts...)}
	case *protocol.ToolExecutionEnd:
		status := sdk.ToolCallStatusCompleted
		if e.IsError {
			status = sdk.ToolCallStatusFailed
		}
		opts := []sdk.ToolCallUpdateOpt{sdk.WithUpdateStatus(status)}
		if c := toolContent(e.Result.Content); len(c) > 0 {
			opts = append(opts, sdk.WithUpdateContent(c))
		}
		return []sdk.SessionUpdate{sdk.UpdateToolCall(sdk.ToolCallId(e.ToolCallID), opts...)}
	}
	return nil
}

func blockUpdates(b protocol.BlockEvent) []sdk.SessionUpdate {
	switch e := b.(type) {
	case protocol.TextStartEvent:
		return textUpdate(e.Content.Text, false)
	case protocol.TextDeltaEvent:
		return textUpdate(e.Delta, false)
	case protocol.ThinkingStartEvent:
		return textUpdate(e.Content.Thinking, true)
	case protocol.ThinkingDeltaEvent:
		return textUpdate(e.Delta, true)
	case protocol.ToolCallStartEvent:
		opts := []sdk.ToolCallStartOpt{sdk.WithStartKind(sdk.ToolKindOther), sdk.WithStartStatus(sdk.ToolCallStatusPending)}
		if json.Valid(e.Arguments) {
			opts = append(opts, sdk.WithStartRawInput(json.RawMessage(e.Arguments)))
		}
		return []sdk.SessionUpdate{sdk.StartToolCall(sdk.ToolCallId(e.ID), e.ToolName, opts...)}
	case protocol.ToolCallEndEvent:
		opts := []sdk.ToolCallUpdateOpt{sdk.WithUpdateTitle(e.ToolCall.Name)}
		if json.Valid(e.ToolCall.Arguments) {
			opts = append(opts, sdk.WithUpdateRawInput(json.RawMessage(e.ToolCall.Arguments)))
		}
		return []sdk.SessionUpdate{sdk.UpdateToolCall(sdk.ToolCallId(e.ToolCall.ID), opts...)}
	}
	return nil
}

func textUpdate(text string, thought bool) []sdk.SessionUpdate {
	if text == "" {
		return nil
	}
	if thought {
		return []sdk.SessionUpdate{sdk.UpdateAgentThoughtText(text)}
	}
	return []sdk.SessionUpdate{sdk.UpdateAgentMessageText(text)}
}

// toolContent maps result blocks to tool call content.
func toolContent(blocks []protocol.UserBlock) []sdk.ToolCallContent {
	var out []sdk.ToolCallContent
	for _, b := range blocks {
		switch v := b.(type) {
		case protocol.Text:
			out = append(out, sdk.ToolContent(sdk.TextBlock(v.Text)))
		case protocol.Image:
			out = append(out, sdk.ToolContent(sdk.ImageBlock(v.Data, v.MimeType)))
		}
	}
	return out
}

// subscription is one explicit follow. One goroutine reads its follower and
// writes the events as extension notifications, after the follow result.
type subscription struct {
	a         *Adapter
	id        string
	session   *Session
	epoch     string
	follower  *bus.Follower
	ready     chan struct{}
	readyOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	mu        sync.Mutex
	written   uint64
	changed   chan struct{}
}

func (s *subscription) release() { s.readyOnce.Do(func() { close(s.ready) }) }

func newSubscriptionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sub_" + hex.EncodeToString(b), nil
}

// follow joins the event stream of a session. The result holds the cut: a
// snapshot, the open stream baseline and the cursor, or only the cursor when
// the cursor that the client gave is still valid. Live events above the cut
// follow the result on the wire.
func (a *Adapter) follow(_ context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPFollowRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	s, err := a.session(req.SessionID)
	if err != nil {
		return nil, err
	}
	var cursor agent.Cursor
	if req.Cursor != nil {
		cursor = agent.Cursor{Epoch: req.Cursor.Epoch, Seq: req.Cursor.Seq}
	}
	id, err := newSubscriptionID()
	if err != nil {
		return nil, err
	}
	f := s.Agent().Follow(cursor)
	entries, err := projectEntries(f.Entries)
	if err != nil {
		f.Events.Close()
		return nil, err
	}
	res := protocol.ACPFollowResult{
		SubscriptionID: id,
		Cursor:         protocol.ACPCursor{Epoch: f.Cursor.Epoch, Seq: f.Cursor.Seq},
		Entries:        entries,
		Resumed:        f.Resumed,
		Resync:         f.Resync,
	}
	if f.Stream != nil {
		res.Stream = &protocol.ACPStreamBaseline{AttemptID: f.Stream.AttemptID, Seq: f.Stream.Seq, Message: f.Stream.Message, Open: f.Stream.Open}
	}
	ctx, cancel := context.WithCancel(a.host.ctx)
	sub := &subscription{a: a, id: id, session: s, epoch: f.Cursor.Epoch, follower: f.Events, ready: make(chan struct{}), ctx: ctx, cancel: cancel, done: make(chan struct{}), written: f.Cursor.Seq, changed: make(chan struct{})}
	a.subMu.Lock()
	select {
	case <-a.stop:
		a.subMu.Unlock()
		cancel()
		f.Events.Close()
		return nil, ErrHostClosed
	default:
	}
	a.subs[id] = sub
	a.unready[id] = sub
	a.wg.Add(1)
	a.subMu.Unlock()
	go sub.run()
	return res, nil
}

// unfollow ends a subscription. No event of it leaves after the result.
func (a *Adapter) unfollow(_ context.Context, raw json.RawMessage) (any, error) {
	var req protocol.ACPUnfollowRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if _, err := a.session(req.SessionID); err != nil {
		return nil, err
	}
	a.subMu.Lock()
	sub := a.subs[req.SubscriptionID]
	a.subMu.Unlock()
	if sub == nil {
		return struct{}{}, nil
	}
	if sub.session.ID != req.SessionID {
		return nil, errUnknownSub
	}
	sub.cancel()
	<-sub.done
	return struct{}{}, nil
}

// frameWritten sees every frame after it was written. A follow result releases
// the live events of its subscription.
func (a *Adapter) frameWritten(frame []byte) {
	a.subMu.Lock()
	defer a.subMu.Unlock()
	if len(a.unready) == 0 || !bytes.Contains(frame, []byte(`"result"`)) {
		return
	}
	// Only a response that carries the ID in its own result counts. A request
	// or notification that quotes the ID, such as a prompt text, does not.
	var head struct {
		Method string `json:"method"`
		Result struct {
			SubscriptionID string `json:"subscriptionId"`
		} `json:"result"`
	}
	if json.Unmarshal(frame, &head) != nil || head.Method != "" {
		return
	}
	if sub, ok := a.unready[head.Result.SubscriptionID]; ok {
		delete(a.unready, head.Result.SubscriptionID)
		sub.release()
	}
}

func (s *subscription) run() {
	a := s.a
	defer a.wg.Done()
	defer close(s.done)
	defer func() {
		a.subMu.Lock()
		delete(a.subs, s.id)
		delete(a.unready, s.id)
		a.subMu.Unlock()
		s.follower.Close()
	}()
	select {
	case <-s.ready:
	case <-s.ctx.Done():
		return
	case <-a.host.failed:
		return
	}
	for {
		select {
		case item, ok := <-s.follower.Events():
			if !ok {
				if errors.Is(s.follower.Err(), bus.ErrResync) && s.ctx.Err() == nil {
					s.resync()
				}
				return
			}
			if s.ctx.Err() != nil {
				return
			}
			if err := s.send(item); err != nil {
				a.host.latch(err)
				return
			}
		case <-s.ctx.Done():
			return
		case <-a.host.failed:
			return
		}
	}
}

// send writes one event with its identity. The write uses the host context,
// so an unfollow in flight does not turn into an output failure.
func (s *subscription) send(item bus.Item) error {
	n := protocol.ACPEventNotification{SubscriptionID: s.id, SessionID: s.session.ID, Epoch: s.epoch, Seq: item.Seq, Event: json.RawMessage(item.Data)}
	if ev, err := protocol.DecodeEvent(item.Data); err == nil {
		n.RunID = ev.Env().RunID
		n.CycleID, n.AttemptID = eventKeys(ev)
	}
	out, err := s.a.waitOut(s.a.host.ctx)
	if err != nil {
		return err
	}
	if err := out.n.NotifyExtension(s.a.host.ctx, protocol.ACPEvent, n); err != nil {
		return err
	}
	s.mu.Lock()
	s.written = item.Seq
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	return nil
}

// waitWritten waits for this subscription's physical write frontier. A detach
// or explicit resync ends the obligation; it does not end the prompt.
func (s *subscription) waitWritten(ctx context.Context, seq uint64) error {
	for {
		s.mu.Lock()
		written, changed := s.written, s.changed
		s.mu.Unlock()
		if written >= seq {
			return nil
		}
		select {
		case <-changed:
		case <-s.done:
			return nil
		case <-s.a.host.failed:
			return s.a.host.Failure()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (a *Adapter) waitFollowers(ctx context.Context, sessionID, epoch string, seq uint64) error {
	a.subMu.Lock()
	var subs []*subscription
	for _, sub := range a.subs {
		if sub.session.ID == sessionID && sub.epoch == epoch {
			subs = append(subs, sub)
		}
	}
	a.subMu.Unlock()
	for _, sub := range subs {
		if err := sub.waitWritten(ctx, seq); err != nil {
			return err
		}
	}
	return nil
}

// resync tells the client that this subscription ended and where the
// conversation is now. The client follows again; nothing is sent twice.
func (s *subscription) resync() {
	f := s.session.Agent().Follow(agent.Cursor{})
	f.Events.Close()
	n := protocol.ACPResyncNotification{
		SubscriptionID: s.id, SessionID: s.session.ID,
		Cursor: protocol.ACPCursor{Epoch: f.Cursor.Epoch, Seq: f.Cursor.Seq},
		Reason: "resync_required",
	}
	out, err := s.a.waitOut(s.a.host.ctx)
	if err == nil {
		err = out.n.NotifyExtension(s.a.host.ctx, protocol.ACPResync, n)
	}
	if err != nil {
		s.a.host.latch(err)
	}
}

// eventKeys returns the cycle and attempt that an event names.
func eventKeys(ev protocol.Event) (cycleID, attemptID string) {
	switch e := ev.(type) {
	case *protocol.CycleStart:
		return e.CycleID, ""
	case *protocol.CycleEnd:
		return e.CycleID, ""
	case *protocol.TurnStart:
		return e.CycleID, ""
	case *protocol.TurnEnd:
		return e.CycleID, ""
	case *protocol.AttemptStart:
		return e.CycleID, e.AttemptID
	case *protocol.AttemptEnd:
		return "", e.AttemptID
	}
	return "", ""
}

// projectEntries returns the safe view of log entries for a follower. The
// request deltas and the system snapshots hold prompt and request data and stay
// out. The failure of an attempt is reduced to its code.
func projectEntries(entries []sessions.Entry) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(entries))
	for _, e := range entries {
		var v any
		switch e := e.(type) {
		case sessions.MessageEntry:
			v = map[string]any{"kind": "message", "message": e.Message, "inputId": e.InputID}
		case sessions.CycleOpened:
			v = map[string]any{"kind": "cycle_opened", "cycleId": e.CycleID}
		case sessions.CycleClosed:
			v = map[string]any{"kind": "cycle_closed", "cycleId": e.CycleID, "reason": e.Reason, "cause": e.Cause, "code": e.Code}
		case sessions.TurnOpened:
			v = map[string]any{"kind": "turn_opened", "cycleId": e.CycleID}
		case sessions.TurnClosed:
			v = map[string]any{"kind": "turn_closed", "cycleId": e.CycleID}
		case sessions.AttemptSettled:
			row := map[string]any{"kind": "attempt", "attemptId": e.AttemptID, "outcome": e.Outcome, "usage": e.Usage}
			if e.Failure != nil {
				row["failureCode"] = e.Failure.Code
			}
			v = row
		case sessions.ToolCall:
			v = map[string]any{"kind": "tool_call", "assistantEntry": e.AssistantEntry, "callId": e.CallID}
		case sessions.RetryScheduled:
			v = map[string]any{"kind": "retry_scheduled", "retryId": e.RetryID, "cycleId": e.CycleID, "turn": e.Turn, "retry": e.Retry, "maxRetries": e.MaxRetries, "delayMs": e.DelayMs, "failureCode": e.Failure.Code}
		case sessions.RetryStarted:
			v = map[string]any{"kind": "retry_started", "retryId": e.RetryID, "retry": e.Retry}
		case sessions.InputOutcome:
			v = map[string]any{"kind": "input_outcome", "inputId": e.InputID, "accepted": e.Accepted, "reason": e.Reason}
		default:
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
