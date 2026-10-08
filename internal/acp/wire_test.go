package acp

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

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
