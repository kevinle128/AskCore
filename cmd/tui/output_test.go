package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardSendsStrayStdoutToStderr(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout

	restore := guardStdout(w)
	fmt.Println("stray library output")
	restore()

	require.NoError(t, w.Close())
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "stray library output\n", string(got))
	assert.Same(t, realStdout, os.Stdout)
}

// countingWriter fails every write with err, when set, and counts the calls.
type countingWriter struct {
	err   error
	calls int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.calls++
	if c.err != nil {
		return 0, c.err
	}
	return len(p), nil
}

func TestGuardWriteErrorSticks(t *testing.T) {
	target := &countingWriter{err: syscall.EPIPE}
	out := newProtocolOut(target)

	_, buffered := out.Write([]byte("a\n"))
	flushed := out.flush()
	_, after := out.Write([]byte("b\n"))

	assert.NoError(t, buffered)
	assert.ErrorIs(t, flushed, syscall.EPIPE)
	assert.ErrorIs(t, after, syscall.EPIPE)
	assert.Equal(t, 1, target.calls)
}

// syncBuffer is a bytes.Buffer safe for the flush timer's goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestGuardFlushesWithinDelay(t *testing.T) {
	var target syncBuffer
	out := newProtocolOut(&target)

	_, err := out.Write([]byte("a\n"))
	require.NoError(t, err)
	_, err = out.Write([]byte("b\n"))
	require.NoError(t, err)

	assert.Equal(t, "", target.String())
	assert.Eventually(t, func() bool { return target.String() == "a\nb\n" }, time.Second, flushDelay)
	require.NoError(t, out.flush())
}

func TestGuardWritesThroughWhenFull(t *testing.T) {
	target := &countingWriter{}
	out := newProtocolOut(target)

	_, err := out.Write(bytes.Repeat([]byte("x"), 128<<10))

	require.NoError(t, err)
	assert.Equal(t, 1, target.calls)
	require.NoError(t, out.flush())
}

func TestGuardReportsWriteErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		failed bool
		stderr string
	}{
		{"no write error", nil, false, ""},
		{"closed pipe is quiet", &os.PathError{Op: "write", Path: "/dev/stdout", Err: syscall.EPIPE}, true, ""},
		{"other errors are reported", errors.New("no space left on device"), true, "no space left on device\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := newProtocolOut(&countingWriter{err: tc.err})
			_, _ = out.Write([]byte("x"))
			var errb bytes.Buffer

			failed := out.failed(&errb)

			assert.Equal(t, tc.failed, failed)
			assert.Equal(t, tc.stderr, errb.String())
		})
	}
}
