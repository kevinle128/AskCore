package pipeline

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"AskCore/pkg/protocol"
)

// nextHandlerID numbers the registrations of the process, so that a disposer
// removes exactly the handler it was returned for.
var nextHandlerID atomic.Uint64

// handlers is the typed handler list of one point. Registration and removal
// replace the slices, so a snapshot taken for one dispatch never changes.
type handlers[H any] struct {
	mu  sync.RWMutex
	ids []uint64
	hs  []H
}

// add appends h as the registration with the given ID.
func (l *handlers[H]) add(id uint64, h H) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Clip forces append to copy, so an earlier snapshot keeps its array.
	l.ids = append(slices.Clip(l.ids), id)
	l.hs = append(slices.Clip(l.hs), h)
}

// remove deletes the registration with the given ID. It does nothing when the
// registration is gone already.
func (l *handlers[H]) remove(id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.ids, id)
	if i < 0 {
		return
	}
	l.ids = slices.Concat(l.ids[:i:i], l.ids[i+1:])
	l.hs = slices.Concat(l.hs[:i:i], l.hs[i+1:])
}

func (l *handlers[H]) snapshot() []H {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.hs
}

// Owner groups the registrations of one party, for example one extension, so
// that the party removes all its handlers with one call.
type Owner struct {
	mu       sync.Mutex
	disposed bool
	// disposers holds the disposer of each live registration by key.
	disposers map[uint64]func()
}

// NewOwner returns an Owner with no registrations.
func NewOwner() *Owner { return &Owner{} }

// adopt records dispose under key to run at Dispose. It reports false, and
// records nothing, when the Owner is disposed already.
func (o *Owner) adopt(key uint64, dispose func()) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.disposed {
		return false
	}
	if o.disposers == nil {
		o.disposers = map[uint64]func(){}
	}
	o.disposers[key] = dispose
	return true
}

// forget drops the record of a registration that was removed on its own.
func (o *Owner) forget(key uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.disposers, key)
}

// Dispose removes every handler registered with o, at all points. A dispatch
// that is running keeps the handlers it started with; the next dispatch does
// not see them. A handler registered with o after Dispose never runs. A second
// call does nothing.
func (o *Owner) Dispose() {
	o.mu.Lock()
	if o.disposed {
		o.mu.Unlock()
		return
	}
	o.disposed = true
	ds := o.disposers
	o.disposers = nil
	o.mu.Unlock()
	for _, d := range ds {
		d()
	}
}

// Option changes one registration.
type Option func(*registration)

type registration struct{ owner *Owner }

// WithOwner registers the handler for o, so that o.Dispose removes it.
func WithOwner(o *Owner) Option { return func(r *registration) { r.owner = o } }

// register adds h to l and returns the disposer of the registration. The
// disposer is safe to call more than once. When the owner is disposed
// already, nothing is added and the disposer does nothing. The owner adopts the
// registration before the handler is added, so a concurrent Dispose either
// removes it or stops it from being added.
func register[H any](l *handlers[H], h H, opts []Option) (dispose func()) {
	var reg registration
	for _, o := range opts {
		o(&reg)
	}
	id := nextHandlerID.Add(1)
	if reg.owner == nil {
		l.add(id, h)
		return func() { l.remove(id) }
	}
	owner := reg.owner
	// mu orders the add against a Dispose that runs between adopt and add.
	var mu sync.Mutex
	gone := false
	remove := func() {
		mu.Lock()
		defer mu.Unlock()
		gone = true
		l.remove(id)
	}
	if !owner.adopt(id, remove) {
		return func() {}
	}
	mu.Lock()
	if !gone {
		l.add(id, h)
	}
	mu.Unlock()
	return func() {
		remove()
		owner.forget(id)
	}
}

// Registry holds the handlers of the control points, one typed list per
// point, in registration order: the first registered handler is the outermost.
// Every dispatch runs on a snapshot, so registering during a chain does not
// change that chain. A nil *Registry has no handlers: each dispatch method
// then runs its default. A Registry is safe for concurrent use.
//
// A Registry made by Scoped also runs the handlers of its parents, read at
// each dispatch.
type Registry struct {
	// parents are read at dispatch, in order, before the handlers of the
	// registry itself. They never change after construction.
	parents []*Registry

	admitStep      handlers[AdmitStepHandler]
	prepareRequest handlers[PrepareRequestHandler]
	executeModel   handlers[ExecuteModelHandler]
	recoverModel   handlers[RecoverModelHandler]
	beforeTool     handlers[BeforeToolHandler]
	executeTool    handlers[ExecuteToolHandler]
	afterTool      handlers[AfterToolHandler]
	completeStep   handlers[CompleteStepHandler]
	stopTurn       handlers[StopTurnHandler]
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{} }

// Scoped returns a Registry whose dispatches run the handlers of each parent,
// in the order given, before its own. A parent is read at each dispatch: a
// handler that a parent gains or loses later takes effect for the next
// dispatch. This is how an Agent runs the handlers of the application, then
// its own. A nil parent is skipped.
func Scoped(parents ...*Registry) *Registry {
	r := &Registry{}
	for _, p := range parents {
		if p != nil {
			r.parents = append(r.parents, p)
		}
	}
	return r
}

// OnAdmitStep, OnPrepareRequest, OnExecuteModel, OnRecoverModel, OnBeforeTool,
// OnExecuteTool, OnAfterTool, OnCompleteStep and OnStopTurn register a handler
// at the end of the list of their point. They need a non-nil Registry and
// panic on a nil one; only the dispatch methods accept a nil Registry. Each
// returns the disposer of the registration, which removes the handler for later
// dispatches and can be called more than once. WithOwner puts the registration
// under an Owner.
func (r *Registry) OnAdmitStep(h AdmitStepHandler, opts ...Option) func() {
	return register(&r.admitStep, h, opts)
}
func (r *Registry) OnPrepareRequest(h PrepareRequestHandler, opts ...Option) func() {
	return register(&r.prepareRequest, h, opts)
}
func (r *Registry) OnExecuteModel(h ExecuteModelHandler, opts ...Option) func() {
	return register(&r.executeModel, h, opts)
}
func (r *Registry) OnRecoverModel(h RecoverModelHandler, opts ...Option) func() {
	return register(&r.recoverModel, h, opts)
}
func (r *Registry) OnBeforeTool(h BeforeToolHandler, opts ...Option) func() {
	return register(&r.beforeTool, h, opts)
}
func (r *Registry) OnExecuteTool(h ExecuteToolHandler, opts ...Option) func() {
	return register(&r.executeTool, h, opts)
}
func (r *Registry) OnAfterTool(h AfterToolHandler, opts ...Option) func() {
	return register(&r.afterTool, h, opts)
}
func (r *Registry) OnCompleteStep(h CompleteStepHandler, opts ...Option) func() {
	return register(&r.completeStep, h, opts)
}
func (r *Registry) OnStopTurn(h StopTurnHandler, opts ...Option) func() {
	return register(&r.stopTurn, h, opts)
}

// The list of each point, for snapshotOf.
func (r *Registry) admitList() *handlers[AdmitStepHandler]        { return &r.admitStep }
func (r *Registry) prepareList() *handlers[PrepareRequestHandler] { return &r.prepareRequest }
func (r *Registry) modelList() *handlers[ExecuteModelHandler]     { return &r.executeModel }
func (r *Registry) recoverList() *handlers[RecoverModelHandler]   { return &r.recoverModel }
func (r *Registry) beforeList() *handlers[BeforeToolHandler]      { return &r.beforeTool }
func (r *Registry) executeList() *handlers[ExecuteToolHandler]    { return &r.executeTool }
func (r *Registry) afterList() *handlers[AfterToolHandler]        { return &r.afterTool }
func (r *Registry) completeList() *handlers[CompleteStepHandler]  { return &r.completeStep }
func (r *Registry) stopList() *handlers[StopTurnHandler]          { return &r.stopTurn }

// snapshotOf returns the handlers of one point that a dispatch runs: those of
// the parents in order, then those of r.
func snapshotOf[H any](r *Registry, pick func(*Registry) *handlers[H]) []H {
	if r == nil {
		return nil
	}
	own := pick(r).snapshot()
	if len(r.parents) == 0 {
		return own
	}
	var out []H
	for _, p := range r.parents {
		out = append(out, snapshotOf(p, pick)...)
	}
	return append(out, own...)
}

// AdmitStep runs the admission handlers. The default enters the turn with the
// reserved messages.
func (r *Registry) AdmitStep(ctx context.Context, in AdmitInput) (AdmitDecision, error) {
	hs := snapshotOf(r, (*Registry).admitList)
	return runAround(ctx, hs, in, func(_ context.Context, in AdmitInput) (AdmitDecision, error) {
		return AdmitDecision{Messages: in.Messages}, nil
	}, nil)
}

// PrepareRequest runs the request handlers before each model request. The
// default changes nothing (a nil update).
func (r *Registry) PrepareRequest(ctx context.Context, in Request) (*RequestUpdate, error) {
	hs := snapshotOf(r, (*Registry).prepareList)
	// The terminal returns what the innermost handler changed in the request
	// it passed on, so a change made before next sticks. A field that is
	// still as it was stays out of the update, so no handler means a nil
	// update.
	orig := in
	terminal := func(_ context.Context, in Request) (*RequestUpdate, error) {
		var upd RequestUpdate
		changed := false
		if !sameMessages(in.Context.Messages, orig.Context.Messages) || in.Context.Tools != orig.Context.Tools {
			upd.Context, changed = &in.Context, true
		}
		if !reflect.DeepEqual(in.Model, orig.Model) {
			upd.Model, changed = &in.Model, true
		}
		if in.ThinkingLevel != orig.ThinkingLevel {
			upd.ThinkingLevel, changed = in.ThinkingLevel, true
		}
		if !changed {
			return nil, nil
		}
		return &upd, nil
	}
	return runAround(ctx, hs, in, terminal, nil)
}

// sameMessages reports whether a and b are the same slice view. A handler that
// copies equal messages counts as a change, which is harmless.
func sameMessages(a, b []protocol.Message) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// ExecuteModel runs handlers around a complete, settled model attempt.
// A handler may return its own settled outcome without calling next.
func (r *Registry) ExecuteModel(ctx context.Context, in ModelCall, terminal Next[ModelCall, *ModelOutcome]) (*ModelOutcome, error) {
	hs := snapshotOf(r, (*Registry).modelList)
	copies := make([]ExecuteModelHandler, len(hs))
	for i, h := range hs {
		copies[i] = func(ctx context.Context, in ModelCall, next Next[ModelCall, *ModelOutcome]) (*ModelOutcome, error) {
			out, err := h(ctx, in, func(ctx context.Context, in ModelCall) (*ModelOutcome, error) {
				out, err := next(ctx, in)
				return out.Clone(), err
			})
			return out.Clone(), err
		}
	}
	return runAround(ctx, copies, in, terminal, func(out *ModelOutcome) error {
		if out == nil || out.Message.StopReason == "" {
			return fmt.Errorf("%w: ExecuteModel needs a settled outcome", ErrInvalidResult)
		}
		return nil
	})
}

// RecoverModel runs the recovery handlers. The default stops with the
// original failure.
func (r *Registry) RecoverModel(ctx context.Context, in RecoverInput) (RecoverAction, error) {
	hs := snapshotOf(r, (*Registry).recoverList)
	return runAround(ctx, hs, in, defaultRecovery, nil)
}

// BeforeTool runs the handlers before a valid tool call executes. The default
// lets the call run (a nil result).
func (r *Registry) BeforeTool(ctx context.Context, in ToolCallInfo) (*BeforeToolCallResult, error) {
	hs := snapshotOf(r, (*Registry).beforeList)
	// The terminal lets the call run. Arguments that a handler passed on do
	// not change the call: they are frozen after validation.
	terminal := func(context.Context, ToolCallInfo) (*BeforeToolCallResult, error) { return nil, nil }
	return runAround(ctx, hs, in, terminal, nil)
}

// ExecuteTool runs the handlers around terminal, which calls the tool body.
func (r *Registry) ExecuteTool(ctx context.Context, in ExecuteToolInput, terminal Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
	hs := snapshotOf(r, (*Registry).executeList)
	return runAround(ctx, hs, in, terminal, nil)
}

// AfterTool runs the handlers after a tool call executed. The default keeps
// the result (a nil override).
func (r *Registry) AfterTool(ctx context.Context, in ToolResultInfo) (*AfterToolCallResult, error) {
	hs := snapshotOf(r, (*Registry).afterList)
	copies := make([]AfterToolHandler, len(hs))
	for i, handler := range hs {
		copies[i] = func(ctx context.Context, in ToolResultInfo, next Next[ToolResultInfo, *AfterToolCallResult]) (*AfterToolCallResult, error) {
			out, err := handler(ctx, in, func(ctx context.Context, in ToolResultInfo) (*AfterToolCallResult, error) {
				out, err := next(ctx, in)
				return out.Clone(), err
			})
			return out.Clone(), err
		}
	}
	// The terminal keeps the result that the innermost handler passed on.
	orig := in
	terminal := func(_ context.Context, in ToolResultInfo) (*AfterToolCallResult, error) {
		if in.IsError == orig.IsError && reflect.DeepEqual(in.Result, orig.Result) {
			return nil, nil
		}
		content := in.Result.Content
		if content == nil {
			content = []protocol.UserBlock{}
		}
		isErr := in.IsError
		return (&AfterToolCallResult{
			Content:           content,
			Details:           in.Result.Details,
			StructuredContent: in.Result.StructuredContent,
			IsError:           &isErr,
			Usage:             in.Result.Usage,
			Terminate:         in.Result.Terminate,
		}).Clone(), nil
	}
	return runAround(ctx, copies, in, terminal, nil)
}

// CompleteStep runs the decision handlers after the tool results and before
// turn_end. The default is Proceed.
func (r *Registry) CompleteStep(ctx context.Context, in Turn) (TurnDecision, error) {
	return decide(ctx, snapshotOf(r, (*Registry).completeList), in)
}

// StopTurn runs the decision handlers when a cycle has no required turn work.
// The default is Proceed.
func (r *Registry) StopTurn(ctx context.Context, in StopInput) (TurnDecision, error) {
	return decide(ctx, snapshotOf(r, (*Registry).stopList), in)
}
