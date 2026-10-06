package providers

import (
	"context"
	"sync"

	"AskCore/pkg/protocol"
)

// Stream is one model response in progress: a bounded FIFO channel of items
// and one settled result.
//
// One producer goroutine owns every send and the only close of the channel.
// The result is settled independently of the channel, so Result works
// without a reader. The stream sends at most one terminal event (done or
// error), and it is the last item before the channel closes. A producer that
// never settles gets an error event made for it. One exception: if the
// request is cancelled while the channel is full and nobody reads, the
// terminal event is dropped so that the producer can exit. A consumer that
// sees the channel close without a terminal event must read the result with
// Result.
type Stream struct {
	events chan StreamItem
	done   chan struct{}

	once sync.Once
	msg  protocol.AssistantMessage
	err  error

	binding AuthBinding
}

// NewStream starts a producer goroutine that runs body with an Assembler and
// returns the stream at once. buffer is the capacity of the event channel
// (below zero means zero); seed carries the identity (api, provider, model,
// timestamp and optional metadata) of the message.
//
// When body returns without a terminal call, the stream settles with stop
// reason error and ErrStreamIncomplete, or with reason aborted when the
// request was cancelled; the content so far is kept. A panic in body settles
// an error and does not end the process. Cancel the request with ctx: every
// blocking send of the producer stops at cancellation, so an abandoned stream
// leaves no goroutine behind.
func NewStream(ctx context.Context, buffer int, seed protocol.AssistantMessage, body func(a *Assembler)) *Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	if buffer < 0 {
		buffer = 0
	}
	s := &Stream{
		events: make(chan StreamItem, buffer),
		done:   make(chan struct{}),
	}
	a := newAssembler(ctx, s.events, seed, s.settle)
	go s.run(ctx, a, body)
	return s
}

// WithBinding sets the credential binding that the request used and returns s.
// The function that starts the stream sets it once, before it hands the stream
// out; the producer never reads it.
func (s *Stream) WithBinding(b AuthBinding) *Stream {
	s.binding = b
	return s
}

// Binding returns the credential binding that the request used. It has no
// token and no account ID. A stream that no credential bound has the zero value.
func (s *Stream) Binding() AuthBinding { return s.binding }

// Events returns the channel of items. The producer closes it after the
// terminal event. A consumer must read until it closes, or cancel the request.
func (s *Stream) Events() <-chan StreamItem { return s.events }

// Result returns a copy of the final message and its Go error. The error is
// nil for a success reason; for stop reason error or aborted it is the cause
// (ErrStreamIncomplete, or an error that wraps the context error).
//
// A settled result wins even if ctx is already cancelled. Before the result
// exists, Result waits for it or for ctx and returns ctx.Err() in the second
// case. It does not read events and it does not cancel the request, so with a
// full channel the caller must read events while it waits.
func (s *Stream) Result(ctx context.Context) (protocol.AssistantMessage, error) {
	select {
	case <-s.done:
		return s.result()
	default:
	}
	select {
	case <-s.done:
		return s.result()
	case <-ctx.Done():
		return protocol.AssistantMessage{}, ctx.Err()
	}
}

func (s *Stream) result() (protocol.AssistantMessage, error) {
	return s.msg.Clone(), s.err
}

// settle stores the result once. Later calls do nothing.
func (s *Stream) settle(msg protocol.AssistantMessage, err error) {
	s.once.Do(func() {
		s.msg, s.err = msg, err
		close(s.done)
	})
}

// run is the producer goroutine: body, then the terminal event, then close.
func (s *Stream) run(ctx context.Context, a *Assembler, body func(*Assembler)) {
	defer close(s.events)
	a.runBody(body)
	a.seal()
	s.deliver(ctx, a.terminalItem())
}

// deliver sends the terminal item. It waits for room only while the request
// lives. After cancellation it tries once without blocking.
func (s *Stream) deliver(ctx context.Context, item StreamItem) {
	if ctx.Err() == nil {
		select {
		case s.events <- item:
			return
		case <-ctx.Done():
		}
	}
	select {
	case s.events <- item:
	default:
	}
}
