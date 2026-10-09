package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"AskCore/internal/bus"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

type mutationGuardKey struct{}

// WithMutationGuard checks external ownership at the commit of Prompt,
// Continue or SetModel. The guard returns a release function that keeps ownership
// stable until the Agent state is changed. It runs without Agent locks and must
// not wait for a run or call Agent methods. Other callers need no guard.
func WithMutationGuard(ctx context.Context, guard func() (func(), error)) context.Context {
	return context.WithValue(ctx, mutationGuardKey{}, guard)
}

func mutationGuard(ctx context.Context) (func(), error) {
	if guard, ok := ctx.Value(mutationGuardKey{}).(func() (func(), error)); ok {
		return guard()
	}
	return func() {}, nil
}

// Agent runs the loop on one conversation, one run at a time. It numbers
// the events of all its runs in one sequence, keeps the finished messages in
// its session log and tells its listeners every event. The Agent driver is
// the only writer of the log. It owns the steering and follow-up queues, which
// the loop claims at its turn and cycle boundaries.
type Agent struct {
	cfg        Config
	newContext func() sessions.Writer
	clock      func() time.Time

	// mu guards the fields below up to emitMu. It is never held while a
	// handler, a provider, a tool, the log or a listener runs.
	mu        sync.Mutex
	source    *trackedLog
	active    *run // nil when idle
	listeners []*listener
	in        inbox
	// wake is set when input arrived that the active run cannot take. When
	// that run ends and a queue holds input, a new run starts.
	wake bool
	// disposed closes admission: Dispose set it first.
	disposed bool
	// posted holds events that no run publishes, such as queue_update. The
	// holder of emitMu publishes them in order. publishing is set while the
	// publisher goroutine exists, and pubDone is closed when it ends.
	posted []protocol.Event
	// dispatching counts the calls of listeners that are in progress. A listener
	// cannot call Prompt, Continue or Reset: they return ErrBusy while it is
	// above zero.
	dispatching int
	publishing  bool
	pubDone     chan struct{}

	disposeOnce sync.Once
	disposeErr  error

	// emitMu serializes the publication of events, listener calls included. No
	// listener can take it: Prompt, Continue and Reset return ErrBusy while a
	// dispatch is in progress. Lock order: emitMu, then mu, then pubMu.
	emitMu sync.Mutex
	// seq and attempts are guarded by emitMu.
	seq uint64
	// pubMu guards pub, ring and the epoch inside pub. It is never held while
	// the log is appended or a listener runs. Lock order: mu, then pubMu.
	pubMu sync.Mutex
	pub   published
	// ring is made by the first Follow.
	ring             *bus.Ring
	listenerFailures atomic.Uint64
	// attempts counts the model requests of the Agent, across cycles and runs.
	attempts int
}

// run is the handle of the active run.
type run struct {
	id     string
	ctx    context.Context
	cancel context.CancelCauseFunc
	done   chan struct{}
	source *trackedLog
	// cycleID and attemptID name the scopes that the loop has opened and not
	// closed. emit keeps them, so the failure path can close exactly the
	// scopes that exist. Only the goroutine of the run touches them.
	cycleID   string
	attemptID string
	// turnOpen is set between turn_start and turn_end, so the failure path
	// commits TurnClosed only for a turn that is open.
	turnOpen           bool
	preparationFailure bool
	// closing is set, under Agent.mu, when the loop found both queues empty at
	// a cycle boundary and decided to end. Input that arrives afterwards waits
	// for the next run.
	closing bool
}

// errUserAbort is the cancel cause of Abort.
var errUserAbort = errors.New("aborted by the user")

// errDisposeAbort is the cancel cause of Dispose. It never wakes the Agent.
var errDisposeAbort = errors.New("agent disposed")

// loopFunc starts runLoop or continueLoop on the prepared context and config.
type loopFunc func(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) error

// New returns an idle Agent with an empty context.
func New(cfg Config) (*Agent, error) {
	if cfg.Stream == nil && cfg.Registry != nil {
		cfg.Stream = cfg.Registry.Stream
	}
	if cfg.Prepare == nil && cfg.Registry != nil {
		cfg.Prepare = cfg.Registry.Prepare
	}
	if cfg.Stream == nil {
		return nil, ErrNoStream
	}
	a := &Agent{cfg: cfg, newContext: cfg.NewContext, clock: cfg.Clock}
	if a.newContext == nil {
		a.newContext = func() sessions.Writer { return &sessions.MemoryLog{} }
	}
	if a.clock == nil {
		a.clock = time.Now
	}
	a.source = newTrackedLog(a.newContext())
	a.pub = published{epoch: newEpoch(), commitIndex: int(a.source.end.Load()), writer: a.source}
	return a, nil
}

// Prompt runs the loop with msgs as new prompts and returns when the run has
// settled. It returns ErrBusy while another run is active or a listener is being
// called, which includes a call from a listener, and ErrDisposed after Dispose.
// A call from another goroutine that meets the delivery of an event that no run
// published, such as queue_update, can get ErrBusy as well: retry after
// WaitForIdle. The first cycle also takes the queued steering messages. Input that
// arrives while the run ends can start a later run, which WaitForIdle waits
// for. When the loop fails, the run still ends with an error assistant message,
// agent_end and agent_settled, and Prompt returns the failure.
func (a *Agent) Prompt(ctx context.Context, msgs ...protocol.Message) error {
	r, err := a.begin(ctx)
	if err != nil {
		return err
	}
	defer a.end(r)
	return a.execute(r, func(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) error {
		_, err := runLoop(ctx, msgs, ac, cfg, d, emit)
		return err
	})
}

// Continue runs the loop on the current context without a new message, like
// Prompt. It fails before any event when the context is empty or ends with
// an assistant message.
func (a *Agent) Continue(ctx context.Context) error {
	r, err := a.begin(ctx)
	if err != nil {
		return err
	}
	defer a.end(r)
	msgs := r.source.Messages()
	if len(msgs) == 0 {
		return ErrContinueEmpty
	}
	if msgs[len(msgs)-1].Role() == protocol.RoleAssistant {
		return ErrContinueFromAssistant
	}
	return a.execute(r, func(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) error {
		_, err := continueLoop(ctx, ac, cfg, d, emit)
		return err
	})
}

// Abort cancels the active run and, unless KeepQueued is given, drops the
// queued steering and follow-up messages and the pending wake. It does nothing
// to an idle Agent with empty queues. It never waits for a listener, so a call
// from a handler or a listener is safe.
func (a *Agent) Abort(opts ...AbortOption) {
	var c abortConfig
	for _, o := range opts {
		o(&c)
	}
	a.mu.Lock()
	if !c.keepQueued {
		a.wake = false
		if a.in.clear() {
			a.postLocked(a.in.update())
		}
	}
	if a.active != nil {
		a.active.cancel(errUserAbort)
	}
	a.mu.Unlock()
}

// WaitForIdle returns nil once no run is active, or the error of ctx. A run
// that a queued message starts when the active run ends counts as part of the
// activity: WaitForIdle returns after the last of them.
func (a *Agent) WaitForIdle(ctx context.Context) error {
	for {
		a.mu.Lock()
		r := a.active
		a.mu.Unlock()
		if r == nil {
			return nil
		}
		select {
		case <-r.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Dispose ends the Agent. It closes admission first: from then on Prompt,
// Continue, Steer, FollowUp, Remove, Reset and SetModel fail with ErrDisposed.
// It cancels the active run with the cause "disposed", which never wakes the
// Agent, and clears both queues with one queue_update. It waits until the run
// has settled, which includes the drain of the tool bodies that started, then
// closes the session log, publishes agent_disposed, ends the followers of
// Follow and joins the publisher goroutine. A panic of a listener does not stop
// it. Every call returns the result of the first. A listener or a handler of the
// run must not call it, because it waits for them.
func (a *Agent) Dispose() error {
	a.disposeOnce.Do(func() { a.disposeErr = a.dispose() })
	return a.disposeErr
}

func (a *Agent) dispose() error {
	a.mu.Lock()
	a.disposed = true
	a.wake = false
	if a.in.clear() {
		a.postLocked(a.in.update())
	}
	r, src := a.active, a.source
	if r != nil {
		r.cancel(errDisposeAbort)
	}
	a.mu.Unlock()
	if r != nil {
		<-r.done
	}
	err := src.Close()
	a.mu.Lock()
	a.postLocked(&protocol.AgentDisposed{})
	a.mu.Unlock()
	a.emitMu.Lock()
	a.flushLocked()
	a.emitMu.Unlock()
	for {
		a.mu.Lock()
		done := a.pubDone
		a.mu.Unlock()
		if done == nil {
			break
		}
		<-done
	}
	a.pubMu.Lock()
	if a.ring != nil {
		a.ring.Close()
	}
	a.pubMu.Unlock()
	return err
}

// SetModel stores model for the next run. It is idle-only: a busy agent
// returns ErrBusy and does not change the model or thinking level.
func (a *Agent) SetModel(ctx context.Context, model providers.Model) error {
	if err := providers.Validate(model); err != nil {
		return err
	}
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return ErrDisposed
	}
	if a.active != nil {
		a.mu.Unlock()
		return ErrBusy
	}
	bound := a.cfg.BoundKey
	get := a.cfg.GetAPIKey
	fallback := a.cfg.Options.APIKey
	ready := a.cfg.Ready
	a.mu.Unlock()

	key, err := providers.ResolveKey(ctx, model.Provider, bound, get, fallback)
	if err != nil {
		return err
	}
	if ready != nil {
		if err := ready(ctx, model, key); err != nil {
			return err
		}
	} else if key == "" {
		return ErrNoAPIKey
	}

	release, err := mutationGuard(ctx)
	if err != nil {
		return err
	}
	defer release()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.disposed {
		return ErrDisposed
	}
	if a.active != nil {
		return ErrBusy
	}
	a.cfg.Model = model
	a.cfg.Options.Reasoning = providers.ClampThinkingLevel(model, thinkingLevel(a.cfg.Options.Reasoning))
	return nil
}

// SetThinkingLevel stores ClampThinkingLevel of the current model. It does
// not look up a key. ErrBusy leaves the stored level alone.
func (a *Agent) SetThinkingLevel(level protocol.ThinkingLevel) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil {
		return ErrBusy
	}
	a.cfg.Options.Reasoning = providers.ClampThinkingLevel(a.cfg.Model, level)
	return nil
}

// Reset starts a fresh context from Config.NewContext, closes the old log and
// clears both queues, with one queue_update when something was queued. It
// returns ErrBusy while a run is active or a listener is being called, and
// ErrDisposed after Dispose. A queue_update that is not published yet is
// replaced by one with the state after the reset.
func (a *Agent) Reset() error {
	a.mu.Lock()
	err := a.idleLocked()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	// resetPublished reads seq, which the holder of emitMu owns.
	a.emitMu.Lock()
	defer a.emitMu.Unlock()
	a.mu.Lock()
	if err := a.idleLocked(); err != nil {
		a.mu.Unlock()
		return err
	}
	cleared := a.in.clear()
	a.wake = false
	// An unpublished update describes the old conversation. A fresh one with
	// the state after the reset replaces it, so a client never sees stale
	// content in the new epoch.
	replaced := len(a.posted) > 0
	a.posted = nil
	old := a.source
	a.source = newTrackedLog(a.newContext())
	a.resetPublished(a.source)
	if cleared || replaced {
		a.postLocked(a.in.update())
	}
	fresh := a.source
	a.mu.Unlock()
	if old.Writer != fresh.Writer {
		return old.Close()
	}
	return nil
}

// idleLocked returns the error that stops a change of an Agent that is
// disposed or running. mu must be held.
func (a *Agent) idleLocked() error {
	if a.disposed {
		return ErrDisposed
	}
	if a.active != nil || a.dispatching > 0 {
		return ErrBusy
	}
	return nil
}

// State returns the status, a copy of the context messages, and the
// stored model and thinking level.
func (a *Agent) State() State {
	a.mu.Lock()
	st, src := Idle, a.source
	if a.active != nil {
		st = Running
	}
	model := a.cfg.Model
	level := thinkingLevel(a.cfg.Options.Reasoning)
	a.mu.Unlock()
	return State{Status: st, Messages: src.Messages(), Model: model, ThinkingLevel: level}
}

func thinkingLevel(level protocol.ThinkingLevel) protocol.ThinkingLevel {
	if level == "" {
		return protocol.ThinkingOff
	}
	return level
}

func (a *Agent) begin(ctx context.Context) (*run, error) {
	release, err := mutationGuard(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.idleLocked(); err != nil {
		return nil, err
	}
	return a.startRunLocked(ctx), nil
}

// startRunLocked makes the run that the Agent is busy with. mu must be held.
func (a *Agent) startRunLocked(ctx context.Context) *run {
	runCtx, cancel := context.WithCancelCause(ctx)
	r := &run{id: newRunID(), ctx: runCtx, cancel: cancel, done: make(chan struct{}), source: a.source}
	a.active = r
	return r
}

// end finishes r. When input arrived that r could not take and a queue still
// holds it, the Agent never goes idle: in the same critical section, a new run
// takes the place of r and starts on its own goroutine. Without the latch,
// input that stays queued, for example after a failed or blocked cycle, starts
// nothing.
func (a *Agent) end(r *run) {
	r.cancel(nil)
	a.mu.Lock()
	var next *run
	if a.wake && !a.disposed && a.in.len() > 0 {
		next = a.startRunLocked(context.Background())
	} else {
		a.active = nil
	}
	a.wake = false
	a.mu.Unlock()
	close(r.done)
	if next != nil {
		go a.drive(next)
	}
}

// drive runs r on its own goroutine for the queued input that started it.
func (a *Agent) drive(r *run) {
	defer a.end(r)
	if r.ctx.Err() != nil {
		// Dispose or Abort came between the start of the run and this
		// goroutine. The run has no input to take, so it publishes nothing.
		return
	}
	_ = a.execute(r, func(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) error {
		_, err := runQueued(ctx, ac, cfg, d, emit)
		return err
	})
}

// execute runs one loop and owns its ending: the run-failure path of Pi's
// runWithLifecycle when the loop returns an error or panics, then
// agent_settled on every path.
func (a *Agent) execute(r *run, start loopFunc) error {
	snap := snapshotOf(a.cfg.SystemPrompt, a.cfg.Tools)
	system := systemMessages(snap)
	ac := pipeline.AgentContext{Messages: append(slices.Clip(system), r.source.Messages()...), Tools: a.cfg.Tools}
	cfg := a.cfg.LoopConfig
	if cfg.Options.SessionID == "" {
		cfg.Options.SessionID = a.cfg.SessionID
	}
	emit := func(ev protocol.Event) error { return a.emit(r, ev) }

	// The context projection is the outermost PrepareRequest handler, so a
	// handler of the config sees the projected context. The run reads the
	// application handlers, then the handlers of this Agent, at each dispatch;
	// the registries of the config are not changed.
	projection := pipeline.NewRegistry()
	projection.OnPrepareRequest(projectContext(r.source, system))
	cfg.Pipeline = pipeline.Scoped(projection, a.cfg.Application, cfg.Pipeline)
	repairStart := len(r.source.Entries())
	err := runGuarded(func() error {
		d, err := openLog(r.source, snap)
		if err != nil {
			return err
		}
		d.in = runInputs{a: a, r: r}
		d.clock = a.clock
		d.onPreparationFailure = func() { r.preparationFailure = true }
		return start(r.ctx, ac, cfg, d, emit)
	})
	if err != nil {
		if repairErr := runGuarded(func() error { return repairTools(r.source, repairStart) }); repairErr != nil {
			err = errors.Join(err, repairErr)
		}
		a.fail(r, err, emit)
	}
	if serr := emit(&protocol.AgentSettled{}); err == nil {
		err = serr
	}
	return err
}

// openLog prepares the driver of a run on log. It commits snap when the log has
// no system snapshot yet, or when the newest one differs, so the snapshot is
// in the log before the first request of the run. The header of the driver is
// the system message that the snapshot describes.
func openLog(log sessions.Writer, snap sessions.SystemSnapshot) (driver, error) {
	if last, ok := log.LastSystemSnapshot(); !ok || !sameSnapshot(last, snap) {
		if _, err := log.Append(snap); err != nil {
			return driver{}, fmt.Errorf("agent: write session log: %w", err)
		}
	}
	return driver{log: log, header: systemMessages(snap)}, nil
}

// runGuarded turns a panic of the loop goroutine into an error. The loop
// recovers panics only inside tool goroutines; a panic in a request or turn
// handler reaches here.
func runGuarded(fn func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = panicError(v)
		}
	}()
	return fn()
}

// fail emits Pi's run-failure message (agent.ts handleRunFailure). Listener
// errors here are dropped: the run already failed and the caller gets that
// error.
func (a *Agent) fail(r *run, cause error, emit Emit) {
	stop := protocol.StopError
	if r.ctx.Err() != nil {
		stop = protocol.StopAborted
	}
	text := providers.CleanDiagnostic(cause)
	msg := protocol.AssistantMessage{
		Content:      []protocol.AssistantBlock{protocol.Text{Text: ""}},
		API:          string(a.cfg.Model.API),
		Provider:     a.cfg.Model.Provider,
		Model:        a.cfg.Model.ID,
		StopReason:   stop,
		ErrorMessage: &text,
		Timestamp:    a.clock().UnixMilli(),
	}
	if stop == protocol.StopAborted {
		msg = abortedMessage(a.cfg.Model, a.clock, cause)
	}
	// The Pi tail comes first. The scopes of this wire that Pi does not have
	// close only when they are open: an attempt after the failure message, a
	// cycle after turn_end. turn_end is always published, as Pi does.
	events := []protocol.Event{
		&protocol.MessageStart{Message: msg},
		&protocol.MessageEnd{Message: msg},
	}
	if r.attemptID != "" {
		events = append(events, &protocol.AttemptEnd{AttemptID: r.attemptID, Outcome: attemptOutcomeOf(stop)})
	}
	events = append(events, &protocol.TurnEnd{CycleID: r.cycleID, Message: msg, ToolResults: []protocol.ToolResultMessage{}})
	if r.cycleID != "" {
		end := &protocol.CycleEnd{CycleID: r.cycleID, Reason: string(reasonOf(stop))}
		if stop == protocol.StopAborted {
			end.Cause = abortCause(r.ctx)
		}
		if stop == protocol.StopError {
			end.Code = providers.CodeOf(cause)
		}
		events = append(events, end)
	}
	events = append(events, &protocol.AgentEnd{Messages: []protocol.Message{msg}})
	for _, ev := range events {
		a.recordFailureStep(r, ev, cause)
		_ = emit(ev)
	}
}

// recordFailureStep commits what the failure event ev ends, before the event
// is published. A write error is dropped: the run already failed and the
// caller gets that error. The terminal error is not claimed to be saved.
func (a *Agent) recordFailureStep(r *run, ev protocol.Event, cause error) {
	var entry sessions.Entry
	switch e := ev.(type) {
	case *protocol.MessageEnd:
		if r.preparationFailure {
			return
		}
		entry = sessions.MessageEntry{Message: e.Message}
	case *protocol.AttemptEnd:
		settled := sessions.AttemptSettled{AttemptID: e.AttemptID, Outcome: e.Outcome}
		if e.Outcome == attemptFailed {
			settled.Failure = &sessions.Failure{Code: providers.CodeOf(cause), Text: providers.CleanDiagnostic(cause)}
		}
		entry = settled
	case *protocol.TurnEnd:
		// turn_end is always published, but a turn that was never opened, or
		// that closed already, has nothing to close in the log. emit clears
		// turnOpen, so the flag is read before the event is published.
		if !r.turnOpen {
			return
		}
		entry = sessions.TurnClosed{CycleID: e.CycleID}
	case *protocol.CycleEnd:
		entry = sessions.CycleClosed{CycleID: e.CycleID, Reason: e.Reason, Cause: e.Cause, Code: e.Code}
	default:
		return
	}
	_, _ = r.source.Append(entry)
}

// abortedMessage is the saved assistant outcome for a cancelled run.
func abortedMessage(model providers.Model, clock func() time.Time, cause error) protocol.AssistantMessage {
	if clock == nil {
		clock = time.Now
	}
	text := providers.CleanDiagnostic(cause)
	return protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: ""}}, API: string(model.API), Provider: model.Provider, Model: model.ID, StopReason: protocol.StopAborted, ErrorMessage: &text, Timestamp: clock().UnixMilli()}
}
