package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
)

// faultOutput is the ordered output owner for frames, insertions, and cleanup.
type faultOutput struct {
	pauseNext bool
	pause     chan struct{}
	done      <-chan struct{}
	status    *os.File
	*os.File
	mu                                        sync.Mutex
	armed, permanent, failed                  bool
	errors                                    chan error
	commits                                   chan commitWrite
	prefix, requested, blocked, cleanupWrites int
	failureKind                               string
	lastPrefix                                []byte
}

var _ term.File = (*faultOutput)(nil)

var cleanupSequence = regexp.MustCompile(`\x1b\[[0-9;:?> <]*[A-Za-z]`)

func cleanupPayload(p []byte) ([]byte, bool) {
	if bytes.Equal(p, []byte(ansi.ResetModeSynchronizedOutput)) {
		return p, true
	}
	if !bytes.Contains(p, []byte(ansi.PopKittyKeyboard(1))) || !bytes.Contains(p, []byte(ansi.ResetModeBracketedPaste)) {
		return nil, false
	}
	rest := cleanupSequence.ReplaceAll(p, nil)
	if strings.Trim(string(rest), "\r\n") != "" {
		return nil, false
	}
	var payload []byte
	for _, seq := range cleanupSequence.FindAll(p, -1) {
		s := string(seq)
		switch s {
		case ansi.ResetModifyOtherKeys, ansi.PopKittyKeyboard(1), ansi.ResetModeBracketedPaste, ansi.SetModeTextCursorEnable, ansi.ResetModeFocusEvent, ansi.ResetModeMouseButtonEvent, ansi.ResetModeMouseAnyEvent, ansi.ResetModeMouseExtSgr:
			payload = append(payload, seq...)
		case ansi.EraseScreenBelow: // Consume the renderer close erase without writing it after failure.
		default:
			if !regexp.MustCompile(`^\x1b\[[0-9;]*[ABCDGdHf]$`).Match(seq) {
				return nil, false
			}
		}
	}
	return payload, true
}
func cleanupBytes(p []byte) bool                   { _, ok := cleanupPayload(p); return ok }
func (w *faultOutput) Write(p []byte) (int, error) { return w.write(p, false) }
func (w *faultOutput) write(p []byte, stringInsertion bool) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed {
		payload, allowed := cleanupPayload(p)
		if w.permanent || !allowed {
			w.blocked++
			return 0, fmt.Errorf("output stopped after failure")
		}
		w.cleanupWrites++
		n, err := w.File.Write(payload)
		if err == nil && n == len(payload) {
			return len(p), nil
		}
		if err == nil {
			err = io.ErrShortWrite
		}
		return 0, err
	}
	n := 0
	var err error
	insertion := stringInsertion && bytes.Contains(p, []byte("G1["))
	injected := false
	if insertion && w.pauseNext {
		w.pauseNext = false
		if statusErr := writeStatus(w.status, "pending-write\n"); statusErr != nil {
			err = statusErr
			injected = true
		} else {
			select {
			case <-w.pause:
			case <-w.done:
				err = context.Canceled
				injected = true
			}
		}
	}
	if w.armed && !injected {
		at := bytes.Index(p, []byte(ansi.SetModeSynchronizedOutput))
		switch {
		case w.failureKind != "" && insertion:
			injected = true
			limit := len(p) / 2
			if w.failureKind == "zero" {
				limit = 0
			}
			if limit > 0 {
				n, err = w.File.Write(p[:limit])
			}
			if err == nil && w.failureKind != "short" {
				err = fmt.Errorf("fixture %s insertion write failure", w.failureKind)
			}
		case w.failureKind == "" && at >= 0:
			injected = true
			n, err = w.File.Write(p[:at+len(ansi.SetModeSynchronizedOutput)])
			if err == nil {
				err = fmt.Errorf("fixture partial output write failure after %d bytes", n)
			}
		}
	}
	if !injected {
		n, err = w.File.Write(p)
	}
	observedErr := err
	if n != len(p) && observedErr == nil {
		observedErr = io.ErrShortWrite
	}
	if observedErr != nil {
		w.failed = true
		w.armed = false
		w.prefix = n
		w.requested = len(p)
		w.lastPrefix = append([]byte(nil), p[:n]...)
		select {
		case w.errors <- observedErr:
		default:
		}
	}
	if insertion && w.commits != nil {
		w.commits <- commitWrite{IDs: commitIDPattern.FindAllString(string(p[:n]), -1), N: n, Requested: len(p), Err: observedErr}
	}
	return n, err
}
func (w *faultOutput) WriteString(s string) (int, error) { return w.write([]byte(s), true) }
func (w *faultOutput) arm(permanent bool) {
	w.mu.Lock()
	w.armed = true
	w.permanent = permanent
	w.failureKind = ""
	w.mu.Unlock()
}
func (w *faultOutput) armCommit(kind string) {
	w.mu.Lock()
	w.armed = true
	w.failureKind = kind
	w.permanent = kind == "permanent"
	w.mu.Unlock()
}

// cleanup runs after renderer stop through the same ordered output owner.
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
func (w *faultOutput) report(status *os.File) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if status == nil {
		return nil
	}
	return writeStatus(status, "writer %t %d %d %d %d %x %x\n", w.failed, w.prefix, w.requested, w.blocked, w.cleanupWrites, sha256.Sum256(w.lastPrefix), w.lastPrefix)
}

func (w *faultOutput) holdNext() { w.mu.Lock(); w.pauseNext = true; w.mu.Unlock() }
func (w *faultOutput) release() {
	select {
	case w.pause <- struct{}{}:
	default:
	}
}
