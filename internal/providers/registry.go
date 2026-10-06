package providers

import (
	"context"
	"fmt"
	"sync"

	"AskCore/pkg/protocol"
)

// Registry maps Model.API to one stream function. Register happens after
// NewRegistry. Stream is safe for concurrent calls. It does not construct HTTP.
type Registry struct {
	mu        sync.RWMutex
	streams   map[API]StreamFn
	preparers map[API]Preparer
	policies  map[API]RetryPolicy
}

func NewRegistry() *Registry {
	return &Registry{streams: make(map[API]StreamFn)}
}

// Register stores fn for api. A later Stream call with that Model.API uses fn.
func (r *Registry) Register(api API, fn StreamFn, policy ...RetryPolicy) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streams == nil {
		r.streams = make(map[API]StreamFn)
	}
	r.streams[api] = fn
	if r.policies == nil {
		r.policies = make(map[API]RetryPolicy)
	}
	captured := DefaultRetryPolicy()
	if len(policy) > 0 {
		captured = policy[0]
	}
	r.policies[api] = captured
	delete(r.preparers, api)
}

// Stream looks up Model.API. An unknown API returns a stream whose terminal
// event is an error. It does not consult the model id to pick a wire.
func (r *Registry) Stream(ctx context.Context, m Model, req TranscriptRequest, opts StreamOptions) *Stream {
	var fn StreamFn
	if r != nil {
		r.mu.RLock()
		if r.streams != nil {
			fn = r.streams[m.API]
		}
		r.mu.RUnlock()
	}
	if fn == nil {
		return unknownAPIStream(ctx, m)
	}
	return fn(ctx, m, req, opts)
}

func unknownAPIStream(ctx context.Context, m Model) *Stream {
	err := fmt.Errorf("unknown model API %q for provider %q", m.API, m.Provider)
	seed := protocol.AssistantMessage{
		API:      string(m.API),
		Provider: m.Provider,
		Model:    m.ID,
	}
	return NewStream(ctx, 0, seed, func(a *Assembler) {
		a.Fail(protocol.StopError, err.Error(), err)
	})
}
