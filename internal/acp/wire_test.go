package acp

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckedWriterObservesWrittenFrames(t *testing.T) {
	var sink bytes.Buffer
	var seen []string
	var mu sync.Mutex
	w := NewCheckedWriter(&sink, nil)
	w.Observe(func(frame []byte) {
		mu.Lock()
		seen = append(seen, string(frame))
		mu.Unlock()
	})
	_, err := w.Write([]byte("one\n"))
	require.NoError(t, err)
	_, err = w.Write([]byte("two\n"))
	require.NoError(t, err)
	mu.Lock()
	require.Equal(t, []string{"one\n", "two\n"}, seen)
	mu.Unlock()
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestCheckedWriterDoesNotObserveFailedWrites(t *testing.T) {
	for _, bad := range []interface{ Write([]byte) (int, error) }{shortWriter{}, errWriter{}} {
		called := false
		w := NewCheckedWriter(bad, nil)
		w.Observe(func([]byte) { called = true })
		_, err := w.Write([]byte("frame\n"))
		require.Error(t, err)
		require.False(t, called)
	}
}

func TestQuietLoggerDropsAttributes(t *testing.T) {
	var out bytes.Buffer
	l := NewQuietLogger(&out)
	l.Error("failed to parse incoming message", "err", errors.New("secret-error-text"), "raw", `{"token":"secret-raw-line"}`)
	l.Info("connection closed", "cause", "secret-cause")
	text := out.String()
	require.Contains(t, text, "failed to parse incoming message")
	require.Contains(t, text, "connection closed")
	require.False(t, strings.Contains(text, "secret"), text)
}

// gateWriter blocks each write until release closes and reports the start of
// each write on started.
type gateWriter struct {
	started chan struct{}
	release chan struct{}
}

func (g *gateWriter) Write(p []byte) (int, error) {
	g.started <- struct{}{}
	<-g.release
	return len(p), nil
}

// A write that is already blocked when the watch starts still stalls.
func TestWatchStallFiresForWriteInProgress(t *testing.T) {
	g := &gateWriter{started: make(chan struct{}, 1), release: make(chan struct{})}
	w := NewCheckedWriter(g, nil)
	go func() { _, _ = w.Write([]byte("frame\n")) }()
	<-g.started

	stalled := make(chan struct{}, 1)
	stop := w.WatchStall(50*time.Millisecond, func() { stalled <- struct{}{} })
	defer stop()
	select {
	case <-stalled:
	case <-time.After(5 * time.Second):
		t.Fatal("a blocked write did not stall")
	}
	close(g.release)
}

// A write that starts after the watch and blocks stalls too.
func TestWatchStallFiresForLaterWrite(t *testing.T) {
	g := &gateWriter{started: make(chan struct{}, 1), release: make(chan struct{})}
	w := NewCheckedWriter(g, nil)
	stalled := make(chan struct{}, 1)
	stop := w.WatchStall(50*time.Millisecond, func() { stalled <- struct{}{} })
	defer stop()

	go func() { _, _ = w.Write([]byte("frame\n")) }()
	<-g.started
	select {
	case <-stalled:
	case <-time.After(5 * time.Second):
		t.Fatal("a blocked write did not stall")
	}
	close(g.release)
}

// A write that ends in time and an idle writer never stall, and a stopped
// watch stays silent.
func TestWatchStallIgnoresQuickWritesAndStops(t *testing.T) {
	var sink bytes.Buffer
	w := NewCheckedWriter(&sink, nil)
	var calls atomic.Int32
	stop := w.WatchStall(20*time.Millisecond, func() { calls.Add(1) })
	for range 5 {
		_, err := w.Write([]byte("frame\n"))
		require.NoError(t, err)
	}
	stop()

	g := &gateWriter{started: make(chan struct{}, 1), release: make(chan struct{})}
	w2 := NewCheckedWriter(g, nil)
	stop2 := w2.WatchStall(20*time.Millisecond, func() { calls.Add(1) })
	stop2()
	go func() { _, _ = w2.Write([]byte("frame\n")) }()
	<-g.started
	// The stall time passes with no watch: a timer of 5x the limit is the
	// failure guard for a wrong call, not a wait for completion.
	<-time.After(100 * time.Millisecond)
	close(g.release)
	require.Zero(t, calls.Load())
}
