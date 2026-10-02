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
// subscribe order, one at a time, on the goroutine of the run, so a slow
// listener stalls the run. An error or a panic from a listener ends the run
// through the run-failure path. A listener must not call Prompt, Continue,
// Reset or WaitForIdle synchronously: they would fail with ErrBusy or wait on
// the listener itself. The returned func removes l and is safe to call twice.
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

// emit fills the envelope of ev, appends a finished message to the run's
// context source, and calls the listeners. Only the goroutine of the active
// run calls it, so seq needs no lock.
func (a *Agent) emit(r *run, ev protocol.Event) error {
	a.seq++
	*ev.Env() = protocol.Envelope{Seq: a.seq, TS: a.clock().UnixMilli(), SessionID: a.cfg.SessionID, RunID: r.id}
	if end, ok := ev.(*protocol.MessageEnd); ok {
		if err := r.source.Append(end.Message); err != nil {
			return err
		}
	}
	return a.dispatch(ev)
}

// dispatch turns a listener panic into an error, so the loop unwinds through
// its emit-error path and stops its tool goroutines.
func (a *Agent) dispatch(ev protocol.Event) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = panicError(v)
		}
	}()
	a.mu.Lock()
	ls := a.listeners
	a.mu.Unlock()
	for _, l := range ls {
		if err := l.fn(ev); err != nil {
			return err
		}
	}
	return nil
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
