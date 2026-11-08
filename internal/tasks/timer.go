package tasks

// A timer task has no goroutine while it waits: time.AfterFunc starts one only
// when the timer fires. Start counts the timer in the wait group, and exactly
// one party ends it:
//
//   - Stop, StopAll, and Close call timer.Stop. If it returns true, the
//     callback will never run, so the caller owns the terminal step.
//   - If it returns false, the callback already started and owns the step.
//
// The owner calls wg.Done once after finish returns true. The claim inside
// finish is a second guard: a party that loses the claim does nothing.

// fireTimer is the callback of a timer.
func (r *Registry) fireTimer(t *task) {
	summary := "timer elapsed after " + t.spec.After.String()
	if r.finish(t, outcome{status: StatusCompleted, res: Result{Summary: summary}}) {
		r.wg.Done()
	}
}

// halt asks one task to end with cause. A body task gets a cancelled context
// and its runner writes the terminal state. A timer ends here if the timer had
// not fired yet.
func (r *Registry) halt(t *task, cause error) {
	if t.cancel != nil {
		t.cancel(cause)
		return
	}
	if t.timer.Stop() && r.finish(t, outcome{status: StatusStopped, cause: cause}) {
		r.wg.Done()
	}
}
