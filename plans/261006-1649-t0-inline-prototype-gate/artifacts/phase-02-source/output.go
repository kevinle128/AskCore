package main

import (
	"bytes"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"os"
	"sync"
)

// faultOutput preserves the terminal FD and serializes frame writes and cleanup.
type faultOutput struct {
	*os.File
	mu                       sync.Mutex
	armed, permanent, failed bool
	errors                   chan error
	prefix                   int
}

var _ term.File = (*faultOutput)(nil)

func (w *faultOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed && w.permanent {
		return 0, fmt.Errorf("fixture permanent output failure")
	}
	if w.armed {
		at := bytes.Index(p, []byte(ansi.SetModeSynchronizedOutput))
		if at >= 0 {
			w.armed = false
			w.failed = true
			n, err := w.File.Write(p[:at+len(ansi.SetModeSynchronizedOutput)])
			w.prefix = n
			if err == nil {
				err = fmt.Errorf("fixture partial output write failure after %d bytes", n)
			}
			select {
			case w.errors <- err:
			default:
			}
			return n, err
		}
	}
	return w.File.Write(p)
}
func (w *faultOutput) arm(permanent bool) {
	w.mu.Lock()
	w.armed = true
	w.permanent = permanent
	w.mu.Unlock()
}

// cleanup runs after the renderer stops and uses the same ordered output owner.
func (w *faultOutput) cleanup() error {
	w.mu.Lock()
	failed := w.failed
	w.mu.Unlock()
	if !failed {
		return nil
	}
	_, err := w.Write([]byte(ansi.ResetModeSynchronizedOutput))
	return err
}
