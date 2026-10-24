package tasks

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"AskCore/pkg/protocol"
)

// outcome is the result of a task for the terminal step.
type outcome struct {
	status Status
	res    Result
	cause  error // why the task was stopped, or nil
}

// run is the goroutine of a body task. Registry.wg counts it.
func (r *Registry) run(t *task, ctx context.Context, release func()) {
	res, panicked := runBody(ctx, t)

	// Read the cause before cancel: cancel would give every task a cause. Only
	// a body that failed gets one. A body that returned without an error did its
	// work, and a stop or a deadline that landed while it ended must not change
	// that result.
	var cause error
	if (panicked || res.Err != nil) && ctx.Err() != nil {
		cause = context.Cause(ctx)
	}
	release()
	t.cancel(nil)
	// Close the file before the terminal step so that the file is complete
	// when the notice names it.
	_ = t.sink.Close()

	var status Status
	switch {
	case panicked:
		status = StatusFailed
	case errors.Is(cause, ErrDeadline):
		status = StatusTimedOut
	case cause != nil:
		status = StatusStopped
	case res.Err != nil:
		status = StatusFailed
	default:
		status = StatusCompleted
	}
	r.finish(t, outcome{status: status, res: res, cause: cause})
}

// runBody calls the body and turns a panic into a failed result. The stack
// goes to the sink, so the model can read it from the output file.
func runBody(ctx context.Context, t *task) (res Result, panicked bool) {
	defer func() {
		if v := recover(); v != nil {
			panicked = true
			res = Result{Err: fmt.Errorf("panic: %v", v)}
			_, _ = t.sink.Write(fmt.Appendf(nil, "\npanic: %v\n\n%s", v, debug.Stack()))
		}
	}()
	return t.spec.Body(ctx, t.sink), false
}

// finish runs the terminal step of a task. It returns false, and does nothing,
// if another caller already claimed the step. The order is fixed:
//
//  1. The caller released the OS resources of the body.
//  2. Under mu: the terminal state, the end time, and the claim.
//  3. Emit task_end. It has no notice outcome, because delivery has not
//     happened yet.
//  4. Call Deliver once, with no lock.
//  5. Under mu: record the outcome of the hand-off and lower the active count.
//     The registry becomes idle here, so Close and Idle never report the end
//     of a task before its hand-off. Then close done.
//
// The caller of a task without a goroutine of its own (a timer) calls wg.Done
// after finish returns true.
func (r *Registry) finish(t *task, o outcome) bool {
	r.mu.Lock()
	if t.ended {
		r.mu.Unlock()
		return false
	}
	t.ended = true
	t.status = o.status
	t.endedAt = time.Now()
	t.exitCode = o.res.ExitCode
	t.summary = o.res.Summary
	if o.res.Err != nil {
		t.errText = o.res.Err.Error()
	}
	t.cause = o.cause
	policy := t.policy
	info := t.infoLocked()
	timer, upd := t.timer, t.updTimer
	r.mu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	if upd != nil {
		upd.Stop()
	}

	<-t.announced
	end := &protocol.TaskEnd{
		TaskID:     t.id,
		Status:     string(info.Status),
		ExitCode:   info.ExitCode,
		OutputFile: info.OutputFile,
		DurationMS: info.EndedAt.Sub(info.StartedAt).Milliseconds(),
	}
	t.evMu.Lock()
	t.endPosted = true
	r.emit(end)
	t.evMu.Unlock()

	state, inputID, noticeErr := NoticeSkipped, "", ""
	if policy != NoticeSilent {
		var tail []byte
		ringCut := false
		if t.sink != nil {
			tail, ringCut = t.sink.Tail(), t.sink.Truncated()
		}
		id, err := r.cfg.Deliver(Notice{
			Origin:  "task",
			TaskID:  t.id,
			Gen:     r.cfg.Gen,
			Policy:  policy,
			Message: buildNotice(info, tail, ringCut),
		})
		inputID = id
		switch {
		case err == nil:
			state = NoticeDelivered
		case errors.Is(err, ErrNotDelivered):
			state, noticeErr = NoticeDiscarded, err.Error()
		default:
			state, noticeErr = NoticeFailed, err.Error()
		}
	}

	r.mu.Lock()
	t.notice, t.inputID, t.noticeError = state, inputID, noticeErr
	r.active--
	if r.active == 0 {
		close(r.idle)
		r.idle = nil
	}
	r.mu.Unlock()
	r.work()
	close(t.done)
	return true
}

// noteWrite arms the update timer after the sink received output. At most one
// timer is armed for a task, so at most one task_update leaves each interval.
func (r *Registry) noteWrite(t *task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.ended || t.updArmed {
		return
	}
	t.updArmed = true
	t.updTimer = time.AfterFunc(updateInterval, func() { r.flushUpdate(t) })
}

// flushUpdate emits one task_update. A task_update never follows task_end:
// finish sets endPosted under evMu before it emits task_end, and this function
// checks the flag under the same lock.
func (r *Registry) flushUpdate(t *task) {
	r.mu.Lock()
	t.updArmed = false
	ended := t.ended
	r.mu.Unlock()
	if ended {
		return
	}
	size, lines := t.sink.Stats()
	t.evMu.Lock()
	defer t.evMu.Unlock()
	if t.endPosted {
		return
	}
	r.emit(&protocol.TaskUpdate{TaskID: t.id, OutputBytes: size, OutputLines: lines})
}
