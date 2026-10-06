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
	end   *BodyEnd
}

func (b *touchBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 && b.touch != nil {
		b.touch()
	}
	b.end.note(err)
	return n, err
}

// TouchBody calls touch after every successful read of at least one byte.
func TouchBody(body io.ReadCloser, touch func()) io.ReadCloser {
	return touchAndTrack(body, touch, nil)
}

func touchAndTrack(body io.ReadCloser, touch func(), end *BodyEnd) io.ReadCloser {
	if body == nil {
		return nil
	}
	return &touchBody{ReadCloser: body, touch: touch, end: end}
}

type bodyEndKey struct{}

// BodyEnd records how the response body of a request ended: with a clean
// end of file, or with a read error. The SDK reports both as "unexpected EOF"
// when no terminal event came, so the error alone cannot tell a cut
// connection from a body that ended on its own.
type BodyEnd struct {
	mu    sync.Mutex
	clean bool
	err   error
}

// TrackBody returns a context whose requests record the end of their body.
// Use it for the context that the SDK request gets.
func TrackBody(ctx context.Context) context.Context {
	return context.WithValue(ctx, bodyEndKey{}, &BodyEnd{})
}

func bodyEndOf(ctx context.Context) *BodyEnd {
	if ctx == nil {
		return nil
	}
	end, _ := ctx.Value(bodyEndKey{}).(*BodyEnd)
	return end
}

func (e *BodyEnd) note(err error) {
	if e == nil || err == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err == io.EOF { //nolint:errorlint // only the plain end of file is a clean end
		e.clean = true
		return
	}
	if e.err == nil {
		e.err = err
	}
}

// endedClean reports a body that reached its end of file and had no read error.
func (e *BodyEnd) endedClean() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clean && e.err == nil
}
