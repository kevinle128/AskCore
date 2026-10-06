package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// guardStdout points os.Stdout at stderr, so a stray write from library code
// never lands in the protocol stream, and turns SIGPIPE into an EPIPE write
// error. It uses signal.Notify, not signal.Ignore: an ignored SIGPIPE is
// inherited across exec and would break the pipelines of child processes.
// The returned func undoes both.
func guardStdout(stderr io.Writer) (restore func()) {
	sigpipe := make(chan os.Signal, 1)
	signal.Notify(sigpipe, syscall.SIGPIPE)
	prev := os.Stdout
	if f, ok := stderr.(*os.File); ok {
		os.Stdout = f
	}
	return func() {
		os.Stdout = prev
		signal.Stop(sigpipe)
	}
}

// flushDelay bounds how long a written line may wait in the buffer. A
// streamed reply emits one event per chunk, and one syscall per event costs
// more than encoding it, so lines written within flushDelay share a write.
const flushDelay = 2 * time.Millisecond

// protocolOut is the real stdout of print and JSON mode. Writes are buffered
// and flushed flushDelay after the first unflushed one, or as soon as the
// buffer fills; a full buffer blocks on the target, so a slow reader still
// stalls the run. The first write error sticks: later writes return it
// without touching the target. When onError is set, the first error, from a
// Write or from the timer flush, is also passed to it once, after the lock is
// released, so the run owner learns of a failure that no caller of Write sees.
type protocolOut struct {
	mu       sync.Mutex
	buf      *bufio.Writer
	timer    *time.Timer
	err      error
	onError  func(error)
	notified bool
}

func newProtocolOut(w io.Writer) *protocolOut {
	return &protocolOut{buf: bufio.NewWriterSize(w, 64<<10)}
}

// setOnError sets the function that gets the first write error. Call it before
// the first Write.
func (o *protocolOut) setOnError(f func(error)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.onError = f
}

// noticeLocked returns the call of onError that err makes due, or nil. The
// caller holds mu and makes the call after it unlocks.
func (o *protocolOut) noticeLocked(err error) func() {
	if err == nil || o.notified || o.onError == nil {
		return nil
	}
	o.notified = true
	f := o.onError
	return func() { f(err) }
}

// fail makes err the sticky error when none is set yet, and reports it to
// onError once. It is for a failure that never reached Write, such as an event
// that cannot be encoded.
func (o *protocolOut) fail(err error) {
	o.mu.Lock()
	if o.err == nil {
		o.err = err
	}
	notice := o.noticeLocked(o.err)
	o.mu.Unlock()
	if notice != nil {
		notice()
	}
}

func (o *protocolOut) Write(p []byte) (int, error) {
	o.mu.Lock()
	if o.err != nil {
		err := o.err
		o.mu.Unlock()
		return 0, err
	}
	n, err := o.buf.Write(p)
	var notice func()
	switch {
	case err != nil:
		o.err = err
		notice = o.noticeLocked(err)
	case o.buf.Buffered() > 0 && o.timer == nil:
		// A failure of the timer flush sticks and reaches onError; a Write or
		// the final flush returns it too.
		o.timer = time.AfterFunc(flushDelay, func() { _ = o.flush() })
	}
	o.mu.Unlock()
	if notice != nil {
		notice()
	}
	return n, err
}

// flush writes out the buffer and returns the sticky error.
func (o *protocolOut) flush() error {
	o.mu.Lock()
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
	if o.err == nil {
		o.err = o.buf.Flush()
	}
	err := o.err
	notice := o.noticeLocked(err)
	o.mu.Unlock()
	if notice != nil {
		notice()
	}
	return err
}

// flushAsync flushes in its own goroutine and returns a channel that closes
// when it is done. A write that blocks on a full pipe holds the output lock, so
// the exit path waits on the channel with a bound and never on the lock. The
// goroutine ends when the write returns or the process exits.
func (o *protocolOut) flushAsync() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_ = o.flush()
		close(done)
	}()
	return done
}

// failed flushes, then reports the sticky write error, if any, and whether
// it happened. A closed pipe means the reader is done, so it ends the run
// quietly with exit 1, as Pi does.
func (o *protocolOut) failed(stderr io.Writer) bool {
	err := o.flush()
	if err != nil && !errors.Is(err, syscall.EPIPE) {
		report(stderr, err)
	}
	return err != nil
}
