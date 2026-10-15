package pipeline

import (
	"context"
	"errors"
	"sync"

	"AskCore/pkg/protocol"
)

var (
	// ErrNextReused is returned by next when it was called a second time or
	// after its handler returned. The terminal operation does not run again.
	ErrNextReused = errors.New("pipeline: next called more than once or after its handler returned")
	// ErrInvalidResult is returned when a handler returns without calling next
	// and its result is not a valid result for that point.
	ErrInvalidResult = errors.New("pipeline: handler returned an invalid result without calling next")
)

// Next runs the rest of the chain: the inner handlers, then the terminal
// operation. It can be called at most once, inside the handler invocation.
// An accepted call finishes before the invocation closes, even if the handler
// returns or panics while next runs in another goroutine.
type Next[In, Out any] func(ctx context.Context, in In) (Out, error)

// Around is a handler of an around point. It gets the input and next. It can
// change the input before next, inspect or replace the result after it, or
// return without calling next to stop the chain. The handler registered first
// is the outermost: it has the final word on the result.
type Around[In, Out any] func(ctx context.Context, in In, next Next[In, Out]) (Out, error)

// Handler types of the around points, in dispatch order of one operation.
type (
	AdmitStepHandler      = Around[AdmitInput, AdmitDecision]
	PrepareRequestHandler = Around[Request, *RequestUpdate]
	ExecuteModelHandler   = Around[ModelCall, *ModelOutcome]
	RecoverModelHandler   = Around[RecoverInput, RecoverAction]
	BeforeToolHandler     = Around[ToolCallInfo, *BeforeToolCallResult]
	ExecuteToolHandler    = Around[ExecuteToolInput, protocol.ToolExecutionResult]
	AfterToolHandler      = Around[ToolResultInfo, *AfterToolCallResult]
)

// nextState tracks one handler invocation: 0 next unused, 1 next claimed,
// 2 the handler returned or panicked and no further claim is allowed.
type nextState struct {
	mu     sync.Mutex
	v      int
	done   chan struct{}
	cancel context.CancelFunc
}

func (s *nextState) claim(cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v != 0 {
		return false
	}
	s.cancel = cancel
	s.v = 1
	return true
}

// finish rejects later claims, cancels next on handler failure, and joins it.
func (s *nextState) finish(failed bool) {
	s.mu.Lock()
	claimed, cancel := s.v == 1, s.cancel
	s.v = 2
	s.mu.Unlock()
	if claimed {
		if failed {
			cancel()
		}
		<-s.done
	}
}

// runAround runs hs from outer to inner around terminal. valid checks the
// result of every handler that returned without an error, whether or not it
// called next, so a handler cannot hide an invalid result behind next; nil
// accepts every result. hs is a snapshot: the caller must not change it during the call.
func runAround[In, Out any](ctx context.Context, hs []Around[In, Out], in In, terminal Next[In, Out], valid func(Out) error) (Out, error) {
	return link(hs, terminal, valid)(ctx, in)
}

func link[In, Out any](hs []Around[In, Out], terminal Next[In, Out], valid func(Out) error) Next[In, Out] {
	if len(hs) == 0 {
		return terminal
	}
	h, rest := hs[0], link(hs[1:], terminal, valid)
	return func(ctx context.Context, in In) (Out, error) {
		st := nextState{done: make(chan struct{})}
		next := func(ctx context.Context, in In) (Out, error) {
			nextCtx, cancel := context.WithCancel(ctx)
			if !st.claim(cancel) {
				cancel()
				var zero Out
				return zero, ErrNextReused
			}
			defer close(st.done)
			defer cancel()
			return rest(nextCtx, in)
		}
		var out Out
		var err error
		// A panic also rejects later claims and joins an accepted next.
		func() {
			returned := false
			defer func() { st.finish(!returned || err != nil) }()
			out, err = h(ctx, in, next)
			returned = true
		}()
		if err == nil && valid != nil {
			if verr := valid(out); verr != nil {
				var zero Out
				return zero, verr
			}
		}
		return out, err
	}
}
