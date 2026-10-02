package main

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
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

// protocolOut is the real stdout of print and JSON mode. Each Write goes
// straight to the target, so a slow reader stalls the writer and nothing is
// left to flush at exit. The first write error sticks: later writes return
// it without touching the target.
type protocolOut struct {
	mu  sync.Mutex
	w   io.Writer
	err error
}

func (o *protocolOut) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return 0, o.err
	}
	n, err := o.w.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	o.err = err
	return n, err
}

// failed reports the sticky write error, if any, and whether it happened. A
// closed pipe means the reader is done, so it ends the run quietly with exit
// 1, as Pi does.
func (o *protocolOut) failed(stderr io.Writer) bool {
	o.mu.Lock()
	err := o.err
	o.mu.Unlock()
	if err != nil && !errors.Is(err, syscall.EPIPE) {
		report(stderr, err)
	}
	return err != nil
}
