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
// without touching the target.
type protocolOut struct {
	mu    sync.Mutex
	buf   *bufio.Writer
	timer *time.Timer
	err   error
}

func newProtocolOut(w io.Writer) *protocolOut {
	return &protocolOut{buf: bufio.NewWriterSize(w, 64<<10)}
}

func (o *protocolOut) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return 0, o.err
	}
	n, err := o.buf.Write(p)
	if err != nil {
		o.err = err
		return n, err
	}
	if o.buf.Buffered() > 0 && o.timer == nil {
		// The error sticks; the next Write or the final flush returns it.
		o.timer = time.AfterFunc(flushDelay, func() { _ = o.flush() })
	}
	return n, nil
}

// flush writes out the buffer and returns the sticky error.
func (o *protocolOut) flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
	if o.err == nil {
		o.err = o.buf.Flush()
	}
	return o.err
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
