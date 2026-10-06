package fantasykit

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// ErrIdleTimeout is the cancel cause of a stream that sat idle too long.
var ErrIdleTimeout = errors.New("idle timeout")

// Idle is an AfterFunc timer: one extra goroutine, reset on body bytes,
// Stop before the producer returns.
type Idle struct {
	d     time.Duration
	timer *time.Timer
	mu    sync.Mutex
}

// StartIdle arms a timer that cancels with ErrIdleTimeout. d <= 0 is a no-op.
func StartIdle(d time.Duration, cancel context.CancelCauseFunc) *Idle {
	if d <= 0 || cancel == nil {
		return &Idle{}
	}
	return &Idle{
		d: d,
		timer: time.AfterFunc(d, func() {
			cancel(ErrIdleTimeout)
		}),
	}
}

func (i *Idle) Touch() {
	if i == nil || i.timer == nil {
		return
	}
	i.mu.Lock()
	i.timer.Reset(i.d)
	i.mu.Unlock()
}

func (i *Idle) Stop() {
	if i == nil || i.timer == nil {
		return
	}
	i.timer.Stop()
}

type touchBody struct {
	io.ReadCloser
	touch func()
}

func (b *touchBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 && b.touch != nil {
		b.touch()
	}
	return n, err
}

// TouchBody calls touch after every successful read of at least one byte.
func TouchBody(body io.ReadCloser, touch func()) io.ReadCloser {
	if body == nil {
		return nil
	}
	return &touchBody{ReadCloser: body, touch: touch}
}
