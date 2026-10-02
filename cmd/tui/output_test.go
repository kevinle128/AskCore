package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"

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
	out := &protocolOut{w: target}

	_, first := out.Write([]byte("a\n"))
	_, second := out.Write([]byte("b\n"))

	assert.ErrorIs(t, first, syscall.EPIPE)
	assert.ErrorIs(t, second, syscall.EPIPE)
	assert.Equal(t, 1, target.calls)
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
			out := &protocolOut{w: &countingWriter{err: tc.err}}
			_, _ = out.Write([]byte("x"))
			var errb bytes.Buffer

			failed := out.failed(&errb)

			assert.Equal(t, tc.failed, failed)
			assert.Equal(t, tc.stderr, errb.String())
		})
	}
}
