package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"
)

// Default bounds of the outbound queue of one session.
const (
	defaultQueueEvents = 4096
	defaultQueueBytes  = 32 << 20
)

// busyWindow bounds how long a call waits for an Agent that is busy with
// work the call does not own, such as the delivery of a queue_update or a run
// that a queued input started. A longer run still gives busy.
const (
	busyWindow = 250 * time.Millisecond
	busyPoll   = 2 * time.Millisecond
)

// ErrQueueOverflow means a session held more unwritten events than its bound.
var ErrQueueOverflow = errors.New("acp: outbound queue overflow")

// ErrHostClosed is returned when a host or session no longer takes work.
var ErrHostClosed = errors.New("acp: host closed")

// Factory builds one independent Agent for a new session.
type Factory func(ctx context.Context, sessionID, cwd string) (*agent.Agent, error)

// WriteEvent sends one Agent event to the client. epoch names the conversation
// epoch that the event belongs to. One session calls it from one goroutine, in
// event order. A returned error stops all output of the host.
type WriteEvent func(ctx context.Context, sessionID, epoch string, ev protocol.Event) error

// Result identifies a run that finished and whose events are all written.
type Result struct {
	RunID string
	// Epoch is the conversation epoch the run belongs to.
	Epoch string
	// Seq is the sequence number of the agent_settled event of the run.
	Seq uint64
	// Reason is the reason of the last cycle_end of the run, such as
	// "completed" or "aborted". It is empty when the run had no cycle.
	Reason string
	// Cause is the cause of the last cycle_end, set when the cycle was aborted.
	Cause string
	// Code is the failure code of the last cycle_end, set when it ended in an error.
	Code string
}

// Cut is the start of a new conversation epoch.
type Cut struct {
	Cursor agent.Cursor
}

// Host owns the sessions of one connection. Each session has its own Agent and
// its own writer goroutine. The host lock covers the session map only.
type Host struct {
	ctx     context.Context
	cancel  context.CancelFunc
	factory Factory
	write   WriteEvent

	maxEvents int
	maxBytes  int

	mu       sync.Mutex
	sessions map[string]*Session
	closed   bool
	failure  error
	// failed closes when the host latches its first output failure.
	failed   chan struct{}
	closing  sync.Once
	closeErr error
}

// HostOption changes a host.
type HostOption func(*Host)

// WithQueueLimit bounds the events a session holds for its writer, in count and
// in encoded bytes. A session that goes over the bound fails the host, because
// the listener must neither block nor drop an event. Zero keeps the default.
func WithQueueLimit(events, bytes int) HostOption {
	return func(h *Host) {
		if events > 0 {
			h.maxEvents = events
		}
		if bytes > 0 {
			h.maxBytes = bytes
		}
	}
}

// NewHost returns a host. write may be nil, then events are dropped.
func NewHost(ctx context.Context, factory Factory, write WriteEvent, opts ...HostOption) *Host {
	hctx, cancel := context.WithCancel(ctx)
	h := &Host{ctx: hctx, cancel: cancel, factory: factory, write: write, sessions: map[string]*Session{}, failed: make(chan struct{}), maxEvents: defaultQueueEvents, maxBytes: defaultQueueBytes}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Failure returns the output error that stopped the host, or nil.
func (h *Host) Failure() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failure
}

// latch records the first output failure, stops the active runs and wakes the
// callers that wait for a write barrier. Close does not count as a failure.
func (h *Host) latch(err error) {
	h.mu.Lock()
	if h.failure != nil {
		h.mu.Unlock()
		return
	}
	if errors.Is(err, ErrQueueOverflow) || errors.Is(err, ErrOutputFailed) {
		h.failure = err
	} else {
		h.failure = fmt.Errorf("%w: %w", ErrOutputFailed, err)
	}
	close(h.failed)
	list := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		list = append(list, s)
	}
	h.mu.Unlock()
	for _, s := range list {
		s.cancelRun(agent.ErrOutputFailure)
	}
}

// usable returns the error that stops new work.
func (h *Host) usable() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failure != nil {
		return h.failure
	}
	if h.closed {
		return ErrHostClosed
	}
	return nil
}

// Session returns the session with id.
func (h *Host) Session(id string) (*Session, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	return s, ok
}

// NewSession builds an Agent and publishes the session only when it is whole.
func (h *Host) NewSession(ctx context.Context, cwd string) (*Session, error) {
	if err := h.usable(); err != nil {
		return nil, err
	}
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	a, err := h.factory(ctx, id, cwd)
	if err != nil {
		return nil, err
	}
	s := &Session{ID: id, CWD: cwd, host: h, agent: a}
	s.cond = sync.NewCond(&s.qmu)
	// The first Follow starts the replay ring and gives the first epoch. The
	// follower is not needed: events reach the writer through the listener.
	f := a.Follow(agent.Cursor{})
	f.Events.Close()
	s.epoch = f.Cursor.Epoch
	s.unsubscribe = a.Subscribe(s.observe)
	s.wdone = make(chan struct{})
	go s.writer()

	h.mu.Lock()
	if h.closed || h.failure != nil {
		err := h.failure
		if err == nil {
			err = ErrHostClosed
		}
		h.mu.Unlock()
		_ = s.Close()
		return nil, err
	}
	h.sessions[id] = s
	h.mu.Unlock()
	return s, nil
}

// Close disposes every session. It is safe to call more than once.
func (h *Host) Close() error {
	h.closing.Do(func() {
		h.mu.Lock()
		h.closed = true
		list := make([]*Session, 0, len(h.sessions))
		for _, s := range h.sessions {
			list = append(list, s)
		}
		h.mu.Unlock()
		var errs []error
		for _, s := range list {
			errs = append(errs, s.Close())
		}
		h.cancel()
		h.closeErr = errors.Join(errs...)
	})
	return h.closeErr
}

func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sess_" + hex.EncodeToString(b), nil
}

type queued struct {
	ev    protocol.Event
	epoch string
	size  int
}

// binding ties one admitted call to the run it started.
type binding struct {
	epoch  string
	runID  string
	seq    uint64
	reason string
	cause  string
	code   string
	cancel context.CancelCauseFunc
	// abortPending records a cancel that came before the run existed.
	abortPending bool
	written      chan struct{}
	once         sync.Once
}

func (b *binding) finish() { b.once.Do(func() { close(b.written) }) }

// Session is one Agent with its own output writer.
type Session struct {
	ID  string
	CWD string

	host        *Host
	agent       *agent.Agent
	unsubscribe func()

	// mu guards the admission state. It is never held over an Agent call.
	mu        sync.Mutex
	epoch     string
	binding   *binding
	resetting bool
	closed    bool
	// held keeps the events that arrive while Reset runs. Their epoch is known
	// when the new cut is known.
	held []protocol.Event
	// inputs counts the queue calls that passed admission and are not done.
	inputs sync.WaitGroup
	// steering and followUp are the texts of the last queue_update.
	steering []string
	followUp []string

	// qmu guards the outbound queue. The Agent listener only appends to it.
	qmu     sync.Mutex
	cond    *sync.Cond
	queue   []queued
	qbytes  int
	qclosed bool
	wdone   chan struct{}
	closing sync.Once
	cerr    error
}

// Agent returns the Agent of the session.
func (s *Session) Agent() *agent.Agent { return s.agent }

// observe is the synchronous Agent listener. It never blocks on output and
// never drops an event: when the queue is over its bound it fails the host.
func (s *Session) observe(ev protocol.Event) error {
	env := ev.Env()
	s.mu.Lock()
	lateAbort := false
	if b := s.binding; b != nil {
		if b.runID == "" && env.RunID != "" {
			b.runID = env.RunID
			lateAbort = b.abortPending
			b.abortPending = false
		}
		if b.runID == env.RunID {
			switch e := ev.(type) {
			case *protocol.AgentSettled:
				b.seq = env.Seq
			case *protocol.CycleEnd:
				b.reason, b.cause, b.code = e.Reason, e.Cause, e.Code
			}
		}
	}
	if s.resetting {
		s.held = append(s.held, ev)
		s.mu.Unlock()
		return nil
	}
	s.noteQueueLocked(ev)
	epoch := s.epoch
	s.mu.Unlock()
	s.enqueue(ev, epoch)
	if lateAbort {
		// Abort never waits for a listener, so it is safe to call from here.
		s.agent.Abort()
	}
	return nil
}

// noteQueueLocked keeps the queue texts of a queue_update. mu must be held.
func (s *Session) noteQueueLocked(ev protocol.Event) {
	if q, ok := ev.(*protocol.QueueUpdate); ok {
		s.steering = append([]string{}, q.Steering...)
		s.followUp = append([]string{}, q.FollowUp...)
	}
}

// enqueue adds ev to the outbound queue, or fails the host when the queue is
// over its bound.
func (s *Session) enqueue(ev protocol.Event, epoch string) {
	size := 0
	if data, err := protocol.EncodeEvent(ev); err == nil {
		size = len(data)
	}
	h := s.host
	s.qmu.Lock()
	if len(s.queue) >= h.maxEvents || s.qbytes+size > h.maxBytes {
		s.qmu.Unlock()
		h.latch(ErrQueueOverflow)
		return
	}
	s.queue = append(s.queue, queued{ev: ev, epoch: epoch, size: size})
	s.qbytes += size
	s.qmu.Unlock()
	s.cond.Signal()
}

// cancelRun stops the run of the bound call with cause.
func (s *Session) cancelRun(cause error) {
	s.mu.Lock()
	var cancel context.CancelCauseFunc
	if s.binding != nil {
		cancel = s.binding.cancel
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel(cause)
	}
}

// Abort stops the active run. A call that was admitted but has no run yet
// keeps the request, and the run stops as soon as it binds.
func (s *Session) Abort() {
	s.mu.Lock()
	if b := s.binding; b != nil && b.runID == "" {
		b.abortPending = true
	}
	s.mu.Unlock()
	s.agent.Abort()
}

// Steer queues a steering message. It returns ErrBusy while a reset runs,
// because an input could start a run before the epoch cut is known.
func (s *Session) Steer(msg protocol.Message) (string, error) {
	return s.input(func() (string, error) { return s.agent.Steer(msg) })
}

// FollowUp queues a follow-up message. It has the admission rule of Steer.
func (s *Session) FollowUp(msg protocol.Message) (string, error) {
	return s.input(func() (string, error) { return s.agent.FollowUp(msg) })
}

// input runs one queue call. Reset waits for the calls that it let in.
func (s *Session) input(call func() (string, error)) (string, error) {
	s.mu.Lock()
	if s.resetting {
		s.mu.Unlock()
		return "", agent.ErrBusy
	}
	s.inputs.Add(1)
	s.mu.Unlock()
	defer s.inputs.Done()
	return call()
}

// Queues returns copies of the steering and follow-up texts that the last
// queue_update named.
func (s *Session) Queues() (steering, followUp []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.steering...), append([]string{}, s.followUp...)
}

// Epoch returns the epoch of the conversation that new events belong to.
func (s *Session) Epoch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.epoch
}

// writer sends queued events in order. After an output failure it only
// completes the barriers, so no caller waits for a write that cannot happen.
func (s *Session) writer() {
	defer close(s.wdone)
	for {
		s.qmu.Lock()
		for len(s.queue) == 0 && !s.qclosed {
			s.cond.Wait()
		}
		if len(s.queue) == 0 {
			s.qmu.Unlock()
			return
		}
		item := s.queue[0]
		s.queue[0] = queued{}
		s.queue = s.queue[1:]
		s.qbytes -= item.size
		s.qmu.Unlock()

		h := s.host
		if h.write != nil && h.Failure() == nil {
			if err := h.write(h.ctx, s.ID, item.epoch, item.ev); err != nil {
				h.latch(err)
			}
		}
		failed := h.Failure() != nil
		s.mu.Lock()
		if b := s.binding; b != nil && (failed || (b.runID == item.ev.Env().RunID && item.ev.EventType() == protocol.TypeAgentSettled)) {
			b.finish()
		}
		s.mu.Unlock()
	}
}

// admit opens a binding when the session is idle.
func (s *Session) admit() (*binding, error) {
	if err := s.host.usable(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	switch {
	case s.closed:
		s.mu.Unlock()
		return nil, agent.ErrDisposed
	case s.binding != nil || s.resetting:
		s.mu.Unlock()
		return nil, agent.ErrBusy
	}
	s.mu.Unlock()
	// Let a delivery or a run that the call does not own end first.
	wctx, wcancel := context.WithTimeout(s.host.ctx, busyWindow)
	_ = s.agent.WaitForIdle(wctx)
	wcancel()
	// With no active run no earlier event can arrive, so the first event with
	// a run ID after this point belongs to the call.
	if s.agent.State().Status != agent.Idle {
		return nil, agent.ErrBusy
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, agent.ErrDisposed
	}
	if s.binding != nil || s.resetting {
		return nil, agent.ErrBusy
	}
	s.binding = &binding{epoch: s.epoch, written: make(chan struct{})}
	return s.binding, nil
}

// refused reports an error that the Agent returns before it runs anything, so
// no event of the call can follow it.
func refused(err error) bool {
	return errors.Is(err, agent.ErrBusy) || errors.Is(err, agent.ErrDisposed) ||
		errors.Is(err, agent.ErrContinueEmpty) || errors.Is(err, agent.ErrContinueFromAssistant)
}

// execute runs call as one admitted call. The request context carries values
// only: its cancellation neither stops the run nor releases the write barrier,
// because a client request is not the run. Stop a run with Agent.Abort.
func (s *Session) execute(ctx context.Context, call func(runCtx context.Context) error) (Result, error) {
	b, err := s.admit()
	if err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	defer cancel(nil)
	s.mu.Lock()
	b.cancel = cancel
	s.mu.Unlock()
	callErr := call(runCtx)
	for until := time.Now().Add(busyWindow); errors.Is(callErr, agent.ErrBusy) && time.Now().Before(until) && s.host.usable() == nil; {
		s.mu.Lock()
		bound := b.runID != ""
		s.mu.Unlock()
		if bound {
			// The busy Agent runs work that a sibling input started.
			break
		}
		time.Sleep(busyPoll)
		callErr = call(runCtx)
	}
	if refused(callErr) {
		// The Agent refused the call. Events that follow belong to other work.
		s.mu.Lock()
		s.binding = nil
		s.mu.Unlock()
		return Result{}, callErr
	}
	s.mu.Lock()
	runID, seq := b.runID, b.seq
	s.mu.Unlock()
	var waitErr error
	if runID != "" {
		select {
		case <-b.written:
		case <-s.host.failed:
		case <-s.host.ctx.Done():
			waitErr = s.host.ctx.Err()
		}
	}
	s.mu.Lock()
	s.binding = nil
	reason, cause, code := b.reason, b.cause, b.code
	s.mu.Unlock()
	err = errors.Join(callErr, waitErr, s.host.Failure())
	if runID == "" {
		return Result{}, err
	}
	return Result{RunID: runID, Epoch: b.epoch, Seq: seq, Reason: reason, Cause: cause, Code: code}, err
}

// Prompt runs msgs as a new turn. It returns after the run settled and every
// event of the run was written.
func (s *Session) Prompt(ctx context.Context, msgs ...protocol.Message) (Result, error) {
	return s.execute(ctx, func(runCtx context.Context) error { return s.agent.Prompt(runCtx, msgs...) })
}

// Continue runs the loop on the current context.
func (s *Session) Continue(ctx context.Context) (Result, error) {
	return s.execute(ctx, func(runCtx context.Context) error { return s.agent.Continue(runCtx) })
}

// Reset starts a new epoch in the same Agent. Admission stays closed until the
// new epoch is known.
func (s *Session) Reset(ctx context.Context) (Cut, error) {
	if err := s.host.usable(); err != nil {
		return Cut{}, err
	}
	s.mu.Lock()
	switch {
	case s.closed:
		s.mu.Unlock()
		return Cut{}, agent.ErrDisposed
	case s.binding != nil || s.resetting:
		s.mu.Unlock()
		return Cut{}, agent.ErrBusy
	}
	s.resetting = true
	s.mu.Unlock()
	// A queue call that passed admission before the flag ends first.
	s.inputs.Wait()
	err := s.agent.Reset()
	var cut Cut
	if err == nil {
		f := s.agent.Follow(agent.Cursor{})
		f.Events.Close()
		cut = Cut{Cursor: f.Cursor}
	}
	s.releaseHeld(cut, err)
	if err != nil {
		return Cut{}, err
	}
	return cut, nil
}

// releaseHeld ends a reset: it names the epoch of each held event and queues
// it. An event that Reset published carries a seq above the cut and belongs to
// the new epoch. Every earlier event belongs to the old one. A failed reset
// keeps the old epoch for all of them.
func (s *Session) releaseHeld(cut Cut, resetErr error) {
	s.mu.Lock()
	oldEpoch := s.epoch
	if resetErr == nil {
		s.epoch = cut.Cursor.Epoch
		s.steering, s.followUp = []string{}, []string{}
	}
	s.mu.Unlock()
	// Admission stays closed until the queue holds every held event. Events that
	// arrive during the enqueue join the held list and leave in the next round,
	// so the order of the Agent is kept.
	for {
		s.mu.Lock()
		held := s.held
		s.held = nil
		if len(held) == 0 {
			s.resetting = false
			s.mu.Unlock()
			return
		}
		type item struct {
			ev    protocol.Event
			epoch string
		}
		items := make([]item, 0, len(held))
		for _, ev := range held {
			epoch := oldEpoch
			if resetErr == nil && ev.Env().Seq > cut.Cursor.Seq {
				epoch = cut.Cursor.Epoch
			}
			if epoch == s.epoch {
				s.noteQueueLocked(ev)
			}
			items = append(items, item{ev, epoch})
		}
		s.mu.Unlock()
		for _, it := range items {
			s.enqueue(it.ev, it.epoch)
		}
	}
}

// Close disposes the Agent and joins the writer. It is safe to call twice.
func (s *Session) Close() error {
	s.closing.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.host.mu.Lock()
		delete(s.host.sessions, s.ID)
		s.host.mu.Unlock()
		s.cerr = s.agent.Dispose()
		s.unsubscribe()
		s.qmu.Lock()
		s.qclosed = true
		s.qmu.Unlock()
		s.cond.Broadcast()
		<-s.wdone
	})
	return s.cerr
}
