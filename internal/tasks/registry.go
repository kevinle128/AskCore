package tasks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"AskCore/pkg/protocol"
)

// Config holds what a Registry needs from the Agent. Root, Deliver, and NextID
// are required. The registry calls Deliver, Emit, Work, and NextID with no
// lock held.
type Config struct {
	// Root is the lifetime context of the Agent. Cancelling it stops every task.
	Root context.Context
	// Deliver hands one notice to the Agent. It returns the input ID. An error
	// that wraps ErrNotDelivered means the Agent refused the notice for good
	// (disposed, or an old generation); any other error is a failure.
	Deliver func(Notice) (string, error)
	// Emit posts an event. It must not block on a listener. It may be nil.
	Emit func(protocol.Event)
	// Dir is the directory for the output files of this session. If it is
	// empty, Start fails for every kind that has an output sink.
	Dir string
	// PrepareDir makes Dir ready for files. The registry calls it once, at the
	// first write of any task, so a session without output makes no directory.
	// It may be nil when Dir already exists.
	PrepareDir func(dir string) error
	// Gen is the generation that the registry stamps on every notice.
	Gen uint64
	// NextID makes the ID of a new task. It is the only source of IDs.
	NextID func(kind Kind) string
	// Work tells the Agent that a task started or ended. It may be nil.
	Work func()
}

// updateInterval is the shortest time between two task_update events of one
// task.
const updateInterval = time.Second

// closedChan is what Idle and Done return when there is nothing to wait for.
var closedChan = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

// Registry runs tasks and owns their goroutines. All methods are safe for
// concurrent use.
//
// Lock order: mu is never held while the registry calls Deliver, Emit, Work,
// NextID, or a body. The per-task evMu is taken alone, or after mu has been
// released.
type Registry struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	closed bool
	byID   map[string]*task
	list   []*task // start order
	active int     // tasks that have not finished their hand-off
	idle   chan struct{}

	prepareOnce sync.Once
	prepareErr  error
}

// task is the registry state of one task. Fields below mu are guarded by
// Registry.mu.
type task struct {
	id        string
	spec      Spec
	startedAt time.Time
	done      chan struct{} // closed after the hand-off
	announced chan struct{} // closed after task_start was emitted
	sink      *Sink         // nil for a timer
	cancel    context.CancelCauseFunc
	timer     *time.Timer // the timer kind only

	// evMu orders task_update and task_end events. endPosted is set, under
	// evMu, before task_end is emitted.
	evMu      sync.Mutex
	endPosted bool

	// guarded by Registry.mu
	policy      NoticePolicy
	ended       bool // the terminal step is claimed
	status      Status
	endedAt     time.Time
	exitCode    *int
	summary     string
	errText     string
	cause       error
	notice      NoticeState
	inputID     string
	noticeError string
	updArmed    bool
	updTimer    *time.Timer
}

// New returns a registry. It panics if Root, Deliver, or NextID is nil,
// because a registry without them cannot work.
func New(cfg Config) *Registry {
	if cfg.Root == nil || cfg.Deliver == nil || cfg.NextID == nil {
		panic("tasks: Config needs Root, Deliver, and NextID")
	}
	ctx, cancel := context.WithCancelCause(cfg.Root)
	return &Registry{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
		byID:   make(map[string]*task),
	}
}

// Start begins a task and returns at once. It returns ErrClosed after Close,
// ErrNoTaskDir if the kind needs an output file and Config.Dir is empty, and
// ErrInvalidSpec for a spec or an ID that the registry refuses.
func (r *Registry) Start(spec Spec) (Handle, error) {
	if err := validateSpec(spec); err != nil {
		return Handle{}, err
	}
	if spec.Kind != KindTimer && r.cfg.Dir == "" {
		return Handle{}, ErrNoTaskDir
	}
	id := r.cfg.NextID(spec.Kind)
	if !validID(id) {
		return Handle{}, fmt.Errorf("%w: task id %q is not a plain name", ErrInvalidSpec, id)
	}
	t := &task{
		id:        id,
		spec:      spec,
		startedAt: time.Now(),
		done:      make(chan struct{}),
		announced: make(chan struct{}),
		policy:    spec.Notice,
		status:    StatusRunning,
		notice:    NoticePending,
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return Handle{}, ErrClosed
	}
	if _, dup := r.byID[id]; dup {
		r.mu.Unlock()
		return Handle{}, fmt.Errorf("%w: duplicate task id %q", ErrInvalidSpec, id)
	}
	r.byID[id] = t
	r.list = append(r.list, t)
	r.active++
	if r.active == 1 {
		r.idle = make(chan struct{})
	}
	if spec.Kind == KindTimer {
		r.wg.Add(1)
		t.timer = time.AfterFunc(spec.After, func() { r.fireTimer(t) })
	} else {
		r.launchLocked(t)
	}
	r.mu.Unlock()

	// The terminal step waits for announced, so task_end never comes before
	// task_start. The deferred close also runs if Emit panics.
	defer close(t.announced)
	r.emit(&protocol.TaskStart{
		TaskID:    id,
		Kind:      string(spec.Kind),
		Title:     spec.Title,
		StartedAt: t.startedAt.UnixMilli(),
	})
	r.work()
	return Handle{ID: id, Done: t.done}, nil
}

// launchLocked starts the runner goroutine of a body task. The caller holds
// r.mu and has checked that the registry is open, so wg.Go cannot race with
// the wait in Close.
func (r *Registry) launchLocked(t *task) {
	t.sink = newSink(filepath.Join(r.cfg.Dir, t.id+".log"), r.prepareDir, func() { r.noteWrite(t) })
	ctx, cancel := context.WithCancelCause(r.ctx)
	t.cancel = cancel
	release := func() {}
	if t.spec.Deadline > 0 {
		var dcancel context.CancelFunc
		ctx, dcancel = context.WithDeadlineCause(ctx, time.Now().Add(t.spec.Deadline), ErrDeadline)
		release = dcancel
	}
	r.wg.Go(func() { r.run(t, ctx, release) })
}

// Stop asks a task to end. It records ErrStopped as the cause and cancels the
// task; the runner writes the terminal state. The notice of a stopped task
// uses NoticeContext, because the caller asked for the stop, unless the policy
// is NoticeSilent. Stop of a task that already ended does nothing.
func (r *Registry) Stop(id string) error {
	r.mu.Lock()
	t := r.byID[id]
	if t == nil {
		r.mu.Unlock()
		return ErrUnknownTask
	}
	if t.ended {
		r.mu.Unlock()
		return nil
	}
	if t.policy != NoticeSilent {
		t.policy = NoticeContext
	}
	r.mu.Unlock()
	r.halt(t, ErrStopped)
	return nil
}

// StopAll stops every running task with cause and sets the notice policy of
// each of them to policy.
func (r *Registry) StopAll(cause error, policy NoticePolicy) {
	cause = stopCause(cause)
	r.mu.Lock()
	var running []*task
	for _, t := range r.list {
		if !t.ended {
			t.policy = policy
			running = append(running, t)
		}
	}
	r.mu.Unlock()
	for _, t := range running {
		r.halt(t, cause)
	}
}

// Close stops admission, stops every task with cause, and waits until each
// task finished its hand-off. The notice policy of each task stays as it is,
// so the Agent decides what to do with a notice after it is disposed. Close is
// safe to call more than once.
func (r *Registry) Close(cause error) {
	cause = stopCause(cause)
	r.mu.Lock()
	r.closed = true
	var running []*task
	for _, t := range r.list {
		if !t.ended {
			running = append(running, t)
		}
	}
	r.mu.Unlock()
	for _, t := range running {
		r.halt(t, cause)
	}
	r.cancel(cause)
	r.wg.Wait()
}

// List returns a copy of the state of every task, in start order. Ended tasks
// stay in the list.
func (r *Registry) List() []Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Info, 0, len(r.list))
	for _, t := range r.list {
		out = append(out, t.infoLocked())
	}
	return out
}

// Done returns a channel that closes after the task finished its hand-off.
// For an unknown ID it returns a closed channel, because there is nothing to
// wait for.
func (r *Registry) Done(id string) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t := r.byID[id]; t != nil {
		return t.done
	}
	return closedChan
}

// Idle returns a channel that is closed while no task is active. When the
// first task starts, the registry makes a new open channel. It closes that
// channel when the last task finished its hand-off. A second start while busy
// returns the same channel.
func (r *Registry) Idle() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idle == nil {
		return closedChan
	}
	return r.idle
}

func (r *Registry) emit(ev protocol.Event) {
	if r.cfg.Emit != nil {
		r.cfg.Emit(ev)
	}
}

func (r *Registry) work() {
	if r.cfg.Work != nil {
		r.cfg.Work()
	}
}

// infoLocked copies the state of a task. The caller holds r.mu.
func (t *task) infoLocked() Info {
	in := Info{
		ID:          t.id,
		Kind:        t.spec.Kind,
		Title:       t.spec.Title,
		Status:      t.status,
		Policy:      t.policy,
		StartedAt:   t.startedAt,
		EndedAt:     t.endedAt,
		Summary:     t.summary,
		Error:       t.errText,
		Cause:       t.cause,
		Notice:      t.notice,
		InputID:     t.inputID,
		NoticeError: t.noticeError,
	}
	if t.exitCode != nil {
		code := *t.exitCode
		in.ExitCode = &code
	}
	if t.sink != nil {
		in.OutputFile = t.sink.FilePath()
		if err := t.sink.Err(); err != nil {
			in.OutputError = err.Error()
		}
	}
	return in
}

// stopCause gives a stop a cause even when the caller passes nil.
func stopCause(cause error) error {
	if cause == nil {
		return ErrClosed
	}
	return cause
}

func validateSpec(spec Spec) error {
	switch {
	case !spec.Kind.valid():
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidSpec, spec.Kind)
	case spec.Kind == KindTimer && spec.Body != nil:
		return fmt.Errorf("%w: a timer has no body", ErrInvalidSpec)
	case spec.Kind != KindTimer && spec.Body == nil:
		return fmt.Errorf("%w: kind %q needs a body", ErrInvalidSpec, spec.Kind)
	case spec.After < 0 || spec.Deadline < 0:
		return fmt.Errorf("%w: negative duration", ErrInvalidSpec)
	}
	return nil
}

// validID accepts only a plain file name, because the ID names the output file.
func validID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\\x00")
}

// prepareDir makes the output directory ready, once for the registry. A sink
// calls it before it opens its file.
func (r *Registry) prepareDir() error {
	r.prepareOnce.Do(func() {
		if r.cfg.PrepareDir != nil {
			r.prepareErr = r.cfg.PrepareDir(r.cfg.Dir)
		}
	})
	return r.prepareErr
}
