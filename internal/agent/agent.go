package agent

import (
	"context"
	"slices"
	"sync"
	"time"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// Agent runs the loop on one conversation, one run at a time. It numbers
// the events of all its runs in one sequence, keeps the finished messages in
// its ContextSource and tells its listeners every event. It is Pi's Agent
// without the steering and follow-up queues.
type Agent struct {
	cfg        Config
	newContext func() ContextSource
	clock      func() time.Time

	mu        sync.Mutex
	source    ContextSource
	active    *run // nil when idle
	listeners []*listener

	seq uint64
}

// run is the handle of the active run.
type run struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	source ContextSource
}

// loopFunc starts Run or Continue on the prepared context and config.
type loopFunc func(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) error

// New returns an idle Agent with an empty context.
func New(cfg Config) (*Agent, error) {
	if cfg.Stream == nil {
		return nil, ErrNoStream
	}
	a := &Agent{cfg: cfg, newContext: cfg.NewContext, clock: cfg.Clock}
	if a.newContext == nil {
		a.newContext = func() ContextSource { return &sessions.MemoryLog{} }
	}
	if a.clock == nil {
		a.clock = time.Now
	}
	a.source = a.newContext()
	return a, nil
}

// Prompt runs the loop with msgs as new prompts and returns when the run has
// settled. It returns ErrBusy while another run is active. When the loop
// fails, the run still ends with an error assistant message, agent_end and
// agent_settled, and Prompt returns the failure.
func (a *Agent) Prompt(ctx context.Context, msgs ...protocol.Message) error {
	r, err := a.begin(ctx)
	if err != nil {
		return err
	}
	defer a.end(r)
	return a.execute(r, func(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) error {
		_, err := Run(ctx, msgs, ac, cfg, emit)
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
	return a.execute(r, func(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) error {
		_, err := Continue(ctx, ac, cfg, emit)
		return err
	})
}

// Abort cancels the active run. It does nothing when the agent is idle.
func (a *Agent) Abort() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil {
		a.active.cancel()
	}
}

// WaitForIdle returns nil once no run is active, or the error of ctx.
func (a *Agent) WaitForIdle(ctx context.Context) error {
	a.mu.Lock()
	r := a.active
	a.mu.Unlock()
	if r == nil {
		return nil
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reset starts a fresh context from Config.NewContext. It returns ErrBusy
// while a run is active.
func (a *Agent) Reset() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil {
		return ErrBusy
	}
	a.source = a.newContext()
	return nil
}

// State returns the status and a copy of the context messages.
func (a *Agent) State() State {
	a.mu.Lock()
	st, src := Idle, a.source
	if a.active != nil {
		st = Running
	}
	a.mu.Unlock()
	return State{Status: st, Messages: src.Messages()}
}

func (a *Agent) begin(ctx context.Context) (*run, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil {
		return nil, ErrBusy
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.active = &run{id: newRunID(), ctx: runCtx, cancel: cancel, done: make(chan struct{}), source: a.source}
	return a.active, nil
}

func (a *Agent) end(r *run) {
	r.cancel()
	a.mu.Lock()
	a.active = nil
	a.mu.Unlock()
	close(r.done)
}

// execute runs one loop and owns its ending: the run-failure path of Pi's
// runWithLifecycle when the loop returns an error or panics, then
// agent_settled on every path.
func (a *Agent) execute(r *run, start loopFunc) error {
	system := initialSystemMessage(a.cfg.SystemPrompt, a.cfg.Tools)
	ac := pipeline.AgentContext{Messages: append(slices.Clip(system), r.source.Messages()...), Tools: a.cfg.Tools}
	cfg := a.cfg.LoopConfig
	cfg.Hooks.PrepareRequest = projectContext(r.source, system, cfg.Hooks.PrepareRequest)
	if cfg.Options.SessionID == "" {
		cfg.Options.SessionID = a.cfg.SessionID
	}
	emit := func(ev protocol.Event) error { return a.emit(r, ev) }

	err := runGuarded(func() error { return start(r.ctx, ac, cfg, emit) })
	if err != nil {
		a.fail(r, err, emit)
	}
	if serr := emit(&protocol.AgentSettled{}); err == nil {
		err = serr
	}
	return err
}

// runGuarded turns a panic of the loop goroutine into an error. The loop
// recovers panics only inside tool goroutines; a panic in a request or turn
// hook reaches here.
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
	text := cause.Error()
	msg := protocol.AssistantMessage{
		Content:      []protocol.AssistantBlock{protocol.Text{Text: ""}},
		API:          a.cfg.Model.API,
		Provider:     a.cfg.Model.Provider,
		Model:        a.cfg.Model.ID,
		StopReason:   stop,
		ErrorMessage: &text,
		Timestamp:    a.clock().UnixMilli(),
	}
	for _, ev := range []protocol.Event{
		&protocol.MessageStart{Message: msg},
		&protocol.MessageEnd{Message: msg},
		&protocol.TurnEnd{Message: msg, ToolResults: []protocol.ToolResultMessage{}},
		&protocol.AgentEnd{Messages: []protocol.Message{msg}},
	} {
		_ = emit(ev)
	}
}
