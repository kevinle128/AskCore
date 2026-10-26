package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

var (
	// ErrStaleNotice is returned by Notify for a notice of a registry that Reset
	// replaced. It wraps tasks.ErrNotDelivered, so the registry marks the notice
	// as discarded.
	ErrStaleNotice = fmt.Errorf("agent: stale task notice: %w", tasks.ErrNotDelivered)

	// errNoticeDisposed is what Notify returns after Dispose. It matches both
	// ErrDisposed and tasks.ErrNotDelivered.
	errNoticeDisposed = fmt.Errorf("%w: %w", ErrDisposed, tasks.ErrNotDelivered)

	// errReset is the cause that Reset gives to the tasks of the old registry.
	errReset = errors.New("agent reset")

	// errBadNotice is what Notify returns for a custom message with no type. The
	// log cannot write such a message.
	errBadNotice = errors.New("agent: task notice has no custom type")
)

// taskIDPrefix names the kind in the ID of a task.
var taskIDPrefix = map[tasks.Kind]string{
	tasks.KindShell:    "sh",
	tasks.KindSubagent: "sa",
	tasks.KindTimer:    "tm",
	tasks.KindWatch:    "wa",
	tasks.KindRemote:   "rm",
}

// Tasks returns the control of the current task registry. Tools reach the same
// control through tools.Context. Reset replaces the registry, so a caller must
// not keep the value across a Reset.
func (a *Agent) Tasks() tasks.Control {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tasks
}

// newRegistry makes the registry of generation gen. The registry takes the
// lifetime context of the Agent, so only Dispose cancels it. The Emit function
// is the event gate: it posts an event only while gen is the current
// generation, so a registry that Reset replaced can no longer reach the new
// epoch. gen is read under mu, which no registry call holds.
func (a *Agent) newRegistry(gen uint64) *tasks.Registry {
	return tasks.New(tasks.Config{
		Root:    a.life,
		Deliver: a.Notify,
		Emit: func(ev protocol.Event) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if gen == a.taskGen {
				a.postLocked(ev)
			}
		},
		Dir:        a.cfg.TaskDir,
		PrepareDir: a.cfg.PrepareTaskDir,
		Gen:        gen,
		NextID:     a.nextTaskID,
		Work:       a.noteWork,
	})
}

// nextTaskID numbers the tasks of the Agent for its whole life. Reset does not
// restart the count, so an output file name never repeats in the directory.
func (a *Agent) nextTaskID(kind tasks.Kind) string {
	return taskIDPrefix[kind] + "-" + strconv.FormatUint(a.taskSeq.Add(1), 10)
}

// Notify is the only way a task notice reaches the model. It maps the notice on
// the existing claims and never blocks. The registry calls it with no lock
// held, and the error it returns decides the fate of the notice: an error that
// wraps tasks.ErrNotDelivered marks the notice as discarded.
//
//   - After Dispose it returns an error that matches ErrDisposed.
//   - A notice of an older generation returns ErrStaleNotice.
//   - Policy Silent queues nothing.
//   - Policy Context goes to the context queue and starts no run.
//   - Policy Wake after Abort of an active run, until the next human input, or
//     on an idle Agent with no model, goes to the context queue as well.
//   - Policy Wake goes to the steering queue, and starts a run on an idle
//     Agent. While a run is active, the notice also sets the wake latch. If the
//     run claims the notice, the latch starts nothing. If the run ends first
//     (it is cancelled or closing, a turn failed, an End decision came, or an
//     admission was rejected), one more run takes the queued notices together.
//   - A nil message, a typed nil pointer, or a custom message with no type is
//     refused with a plain error, so the registry records the notice as failed.
//
// A notice never gets ErrQueueFull. It is not human input and does not count
// toward Config.MaxQueuedInputs. It returns the ID of the queued input.
func (a *Agent) Notify(n tasks.Notice) (string, error) {
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return "", errNoticeDisposed
	}
	if n.Gen != a.taskGen {
		a.mu.Unlock()
		return "", ErrStaleNotice
	}
	if n.Policy == tasks.NoticeSilent {
		a.mu.Unlock()
		return "", nil
	}
	if err := checkNoticeMessage(n.Message); err != nil {
		a.mu.Unlock()
		return "", err
	}
	msg := protocol.CloneMessage(n.Message)
	a.work++
	if n.Policy == tasks.NoticeContext {
		id := a.in.nextID()
		a.in.context = append(a.in.context, input{id: id, msg: msg, origin: originTask})
		a.mu.Unlock()
		return id, nil
	}
	if a.interrupted || (a.active == nil && a.cfg.Model.ID == "") {
		// The user stopped the Agent, or it has no model to run with. The
		// notice waits in the context queue, which rides with the next input,
		// starts no run and does not block quiescence.
		id := a.in.nextID()
		a.in.context = append(a.in.context, input{id: id, msg: msg, origin: originTask})
		a.mu.Unlock()
		return id, nil
	}
	var started *run
	if a.active == nil {
		started = a.startRunLocked(context.Background())
	} else {
		// The run claims the notice at its next turn boundary, and then end finds
		// no queued input and starts no run. A run that cannot claim it stays
		// silent about it: it is cancelled, it is closing, or a failed turn, an
		// End decision, or a rejected admission ends it with no claim. The latch
		// makes end start one run for the notice that is still queued. end clears
		// the latch, so a run that fails again does not loop.
		a.wake = true
	}
	id := a.in.addFrom(originTask, false, msg)
	a.postLocked(a.in.update())
	a.mu.Unlock()
	if started != nil {
		go a.drive(started)
	}
	return id, nil
}

// checkNoticeMessage refuses a message that the log or the model could not use.
// The error is plain: it does not match tasks.ErrNotDelivered, so the registry
// records the notice as failed and keeps the text.
func checkNoticeMessage(m protocol.Message) error {
	switch v := m.(type) {
	case nil:
		return ErrNoInput
	case *protocol.CustomMessage:
		if v == nil {
			return ErrNoInput
		}
		if v.CustomType == "" {
			return errBadNotice
		}
	case protocol.CustomMessage:
		if v.CustomType == "" {
			return errBadNotice
		}
	}
	return nil
}

// WaitForQuiescence returns nil when no task is active, no run is active, and
// no steering or follow-up input is queued. It returns the error of ctx if ctx
// ends first. It waits only on a signal whose condition does not hold yet and
// it never sleeps, so it cannot spin. A work counter that the Agent and the
// registry raise on every start, end, enqueue, and busy edge is read before
// and after the checks; a change means the checks run again.
//
// A task notice that stays queued after the run that it woke also failed does
// not block the call either. One wake gives one run: that run ends with the
// notice unclaimed only if it fails, ends by decision, or is rejected, and
// nothing would start a third run. The same rule holds for any input that stays
// queued on an idle Agent.
//
// A notice with policy Context does not block the call. If it is still queued
// when the caller exits, the model never sees it, which is what that policy
// means. Input that stays queued on an idle Agent, for example after
// Abort(KeepQueued), does not block either: nothing would start a run for it.
func (a *Agent) WaitForQuiescence(ctx context.Context) error {
	for {
		a.mu.Lock()
		before, reg := a.work, a.tasks
		a.mu.Unlock()

		if idle := reg.Idle(); !isClosed(idle) {
			select {
			case <-idle:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		a.mu.Lock()
		busy := a.idle // nil while the Agent is idle
		a.mu.Unlock()
		if busy != nil {
			select {
			case <-busy:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		if a.quiescenceProbe != nil {
			a.quiescenceProbe()
		}
		a.mu.Lock()
		after := a.work
		a.mu.Unlock()
		if after == before {
			return nil
		}
	}
}

// isClosed reports whether ch is closed, without waiting.
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// noticePort is the tasks.Port of one generation. The Agent gives each run the
// port of the generation the run started in, so a notice of work that a tool
// call started is refused after a Reset.
type noticePort struct {
	a   *Agent
	gen uint64
}

func (p noticePort) Gen() uint64 { return p.gen }

func (p noticePort) Notify(policy tasks.NoticePolicy, msg protocol.Message) (string, error) {
	return p.a.Notify(tasks.Notice{Gen: p.gen, Policy: policy, Message: msg})
}

// Remove deletes a queued notice of the port's generation. Human input stays.
func (p noticePort) Remove(id string) bool {
	a := p.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.disposed || p.gen != a.taskGen || !a.in.removeNotice(id) {
		return false
	}
	a.postLocked(a.in.update())
	return true
}
