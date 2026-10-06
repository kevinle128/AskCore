package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"AskCore/pkg/protocol"
)

type listener struct{ fn func(protocol.Event) error }

// Subscribe adds l after the listeners already subscribed. Listeners run in
// subscribe order, one at a time, on the goroutine that publishes the event,
// so a slow listener stalls the run. That goroutine is the one of the run, or a
// publisher goroutine of the Agent for an event that no run publishes, such as
// queue_update. An error or a panic from a listener is
// contained: it goes to Config.ListenerError, the listener stays subscribed,
// the later listeners still get the event, and the run goes on. A listener may
// call Steer, FollowUp, Remove, Abort, State, Follow and Stats, which never wait
// for a listener. Prompt, Continue and Reset return ErrBusy from a listener, and
// for any caller while an event is being delivered, even when no run is active:
// an event that no run published, such as queue_update, is delivered by a
// publisher goroutine. A caller that meets this retries after WaitForIdle. A
// listener must not call WaitForIdle or Dispose, which wait for the listener.
// The returned func removes l and is safe to call twice.
func (a *Agent) Subscribe(l func(protocol.Event) error) (unsubscribe func()) {
	entry := &listener{fn: l}
	a.mu.Lock()
	a.listeners = append(slices.Clip(a.listeners), entry)
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.listeners = slices.DeleteFunc(slices.Clone(a.listeners), func(x *listener) bool { return x == entry })
	}
}

// emit fills the envelope of ev, keeps the open scopes of the run, calls the
// listeners and then records the event as published. It writes nothing to the
// session log: the driver commits first and publishes after. Only the
// goroutine of the active run calls it. Events that other goroutines posted
// come first, so queue_update keeps its place between the events of the run.
// It returns nil: a listener failure never reaches the loop.
func (a *Agent) emit(r *run, ev protocol.Event) error {
	a.emitMu.Lock()
	defer a.emitMu.Unlock()
	a.flushLocked()
	a.seq++
	*ev.Env() = protocol.Envelope{Seq: a.seq, TS: a.clock().UnixMilli(), SessionID: a.cfg.SessionID, RunID: r.id}
	switch e := ev.(type) {
	case *protocol.CycleStart:
		r.cycleID = e.CycleID
	case *protocol.CycleEnd:
		r.cycleID = ""
	case *protocol.TurnStart:
		r.turnOpen = true
	case *protocol.TurnEnd:
		r.turnOpen = false
	case *protocol.AttemptStart:
		a.attempts++
		e.Number = a.attempts
		r.attemptID = e.AttemptID
	case *protocol.AttemptEnd:
		r.attemptID = ""
	}
	a.dispatch(ev)
	a.publish(ev, r.attemptID)
	return nil
}

// postLocked queues an event that no run publishes, such as queue_update, and
// makes sure that a goroutine will publish it. mu must be held. The caller
// never publishes and never waits for a listener: while a run is active its
// goroutine publishes the event before its next one, and the publisher
// goroutine does it at once.
func (a *Agent) postLocked(ev protocol.Event) {
	a.posted = append(a.posted, ev)
	if a.publishing {
		return
	}
	a.publishing = true
	a.pubDone = make(chan struct{})
	go a.publisher(a.pubDone)
}

// publisher publishes the posted events in order and ends when none is left.
// It exists only while there is something to publish, so an idle Agent has no
// goroutine; Dispose waits for it.
func (a *Agent) publisher(done chan struct{}) {
	for {
		a.emitMu.Lock()
		a.flushLocked()
		a.emitMu.Unlock()
		a.mu.Lock()
		if len(a.posted) == 0 {
			a.publishing = false
			a.pubDone = nil
			a.mu.Unlock()
			close(done)
			return
		}
		a.mu.Unlock()
	}
}

// flushLocked publishes the posted events in order, and the events that their
// listeners post. emitMu must be held. An event of this kind belongs to the
// active run, if there is one, else it has no run ID.
func (a *Agent) flushLocked() {
	for {
		a.mu.Lock()
		evs := a.posted
		a.posted = nil
		runID := ""
		if a.active != nil {
			runID = a.active.id
		}
		a.mu.Unlock()
		if len(evs) == 0 {
			return
		}
		for _, ev := range evs {
			a.seq++
			*ev.Env() = protocol.Envelope{Seq: a.seq, TS: a.clock().UnixMilli(), SessionID: a.cfg.SessionID, RunID: runID}
			a.dispatch(ev)
			a.publish(ev, "")
		}
	}
}

// dispatch gives ev to every listener in subscribe order. A failure of one
// listener does not reach the others.
func (a *Agent) dispatch(ev protocol.Event) {
	a.mu.Lock()
	ls := a.listeners
	a.dispatching++
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.dispatching--
		a.mu.Unlock()
	}()
	for _, l := range ls {
		a.call(l, ev)
	}
}

// call runs one listener and contains its error or panic.
func (a *Agent) call(l *listener, ev protocol.Event) {
	defer func() {
		if v := recover(); v != nil {
			a.listenerFailed(panicError(v))
		}
	}()
	if err := l.fn(ev); err != nil {
		a.listenerFailed(err)
	}
}

// listenerFailed counts a contained failure and reports it. A panic of the
// reporter is contained as well.
func (a *Agent) listenerFailed(err error) {
	a.listenerFailures.Add(1)
	if a.cfg.ListenerError == nil {
		return
	}
	defer func() { _ = recover() }()
	a.cfg.ListenerError(err)
}

func panicError(v any) error {
	if err, ok := v.(error); ok {
		return fmt.Errorf("panic: %w", err)
	}
	return errors.New("panic: " + fmt.Sprint(v))
}

func newRunID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails; see crypto/rand.Read
	return hex.EncodeToString(b)
}
