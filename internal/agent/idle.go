package agent

// closedIdle is what Idle returns while the Agent has nothing to wait for.
var closedIdle = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

// busyLocked is the one predicate of activity: a run is active, or a listener
// call is in progress. It is the condition under which Prompt, Continue and
// Reset return ErrBusy. mu must be held.
func (a *Agent) busyLocked() bool { return a.active != nil || a.dispatching > 0 }

// syncIdleLocked keeps the idle channel in step with busyLocked. It makes a new
// channel only when the Agent turns busy, and closes it only when the Agent
// turns idle. A run that ends and hands over to the next run in the same
// critical section never changes the predicate, so no waiter is orphaned and
// no channel closes twice. Every site that changes active or dispatching calls
// it before it releases mu.
func (a *Agent) syncIdleLocked() {
	switch busy := a.busyLocked(); {
	case busy && a.idle == nil:
		a.idle = make(chan struct{})
		a.work++
	case !busy && a.idle != nil:
		close(a.idle)
		a.idle = nil
		a.work++
	}
}

// Idle returns a channel that is closed while no run is active and no listener
// call is in progress. When the Agent turns busy, it makes a new open channel;
// it closes that channel when the Agent turns idle. A run that a queued
// message starts when the active run ends keeps the Agent busy, so the channel
// stays open across the hand-over. A caller that got ErrBusy waits on this
// channel and then tries again.
func (a *Agent) Idle() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.idle == nil {
		return closedIdle
	}
	return a.idle
}

// noteWork records that something changed which a quiescence check must see
// again: a task started or ended, or input was queued.
func (a *Agent) noteWork() {
	a.mu.Lock()
	a.work++
	a.mu.Unlock()
}
