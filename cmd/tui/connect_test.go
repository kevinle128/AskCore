package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"AskCore/internal/leader"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestParseConnectArgs(t *testing.T) {
	for argv, want := range map[string]connectOptions{
		"":                {},
		"--cwd /tmp/x":    {cwd: "/tmp/x"},
		"--attach sess_1": {attach: "sess_1"},
		"--help":          {help: true},
	} {
		got, err := parseConnectArgs(strings.Fields(argv))
		require.NoError(t, err, argv)
		require.Equal(t, want, got, argv)
	}
	for _, argv := range []string{"--cwd", "--attach", "--cwd /a --attach s", "extra", "--bogus"} {
		_, err := parseConnectArgs(strings.Fields(argv))
		require.Error(t, err, argv)
	}
}

func TestParseInputLine(t *testing.T) {
	cases := map[string]inputCommand{
		"hello there":          {kind: inputPrompt, text: "hello there"},
		"  spaced prompt  ":    {kind: inputPrompt, text: "spaced prompt"},
		"/new":                 {kind: inputNew},
		"/new /tmp/work":       {kind: inputNew, args: []string{"/tmp/work"}},
		"/sessions":            {kind: inputSessions},
		"/attach sess_1":       {kind: inputAttach, args: []string{"sess_1"}},
		"/detach":              {kind: inputDetach},
		"/take":                {kind: inputTake},
		"/cancel":              {kind: inputCancel},
		"/follow":              {kind: inputFollow},
		"/follow e1 42":        {kind: inputFollow, args: []string{"e1", "42"}},
		"/unfollow":            {kind: inputUnfollow},
		"/answer q1 allow":     {kind: inputAnswer, args: []string{"q1", "allow"}},
		"/quit":                {kind: inputQuit},
		"//literal slash text": {kind: inputPrompt, text: "/literal slash text"},
	}
	for line, want := range cases {
		got, err := parseInputLine(line)
		require.NoError(t, err, line)
		require.Equal(t, want, got, line)
	}
	for _, line := range []string{"/bogus", "/attach", "/attach a b", "/follow e1", "/follow e1 x", "/answer q1", "/take now", "/quit now"} {
		_, err := parseInputLine(line)
		require.Error(t, err, line)
	}
	_, err := parseInputLine("")
	require.ErrorIs(t, err, errBlankLine)
}

func TestConnectHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	deps := runDependencies{getenv: func(string) string { return "" }}
	code := runWithDependencies([]string{"connect", "--help"}, bytes.NewReader(nil), &out, &errOut, deps)
	require.Zero(t, code)
	require.Contains(t, out.String(), "/attach ID")
	require.Contains(t, out.String(), "back to an agent of its own")
}

func TestConnectFailsVisiblyWithoutAsk(t *testing.T) {
	// No ask binary next to the caller and none on the PATH: the client says so
	// and does not fall back to an agent of its own.
	home := leaderHome(t)
	t.Setenv("PATH", t.TempDir())
	var out, errOut bytes.Buffer
	deps := runDependencies{getenv: func(k string) string {
		if k == "ASK_HOME" {
			return home
		}
		return ""
	}}
	code := runConnect(nil, bytes.NewReader(nil), &out, &errOut, deps, nil, "/nonexistent/ask-test-caller")
	require.Equal(t, 1, code)
	require.Contains(t, errOut.String(), "ask binary was not found")
	require.Empty(t, out.String())
}

// A signal ends a call that a leader never answers: the setup calls use the
// context that the first signal cancels.
func TestConnectSignalEndsAnUnansweredCall(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	c := newLineClient(&leader.Client{Registered: &leader.Registered{
		Writer: leader.NewFrameWriter(io.Discard, protocol.LeaderMaxFrame),
		Reader: leader.NewFrameReader(pr, protocol.LeaderMaxFrame),
	}}, io.Discard)
	go c.readLoop()
	sigs := make(chan os.Signal, 1)
	ctx, stop := withSignalContext(sigs)
	defer stop()
	c.ctx = ctx

	sigs <- syscall.SIGTERM
	_, f := c.call("initialize", map[string]any{})

	require.NotNil(t, f)
	code, signalled := signalExit(ctx)
	require.True(t, signalled)
	require.Equal(t, 143, code)
}

func TestConnectSignalExitIsAbsentWithoutASignal(t *testing.T) {
	ctx, stop := withSignalContext(make(chan os.Signal))
	_, signalled := signalExit(ctx)
	require.False(t, signalled)
	stop()
	_, signalled = signalExit(ctx)
	require.False(t, signalled, "the end of the command is not a signal")
}

// A signal during setup exits with the code of the signal, also when the
// leader holds the connection and never answers.
func TestConnectSignalDuringSetupExitsWithTheSignalCode(t *testing.T) {
	for sig, want := range map[os.Signal]int{syscall.SIGTERM: 143, os.Interrupt: 130, syscall.SIGHUP: 129} {
		home := leaderHome(t)
		paths, err := leader.ResolvePaths(home)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(paths.Socket), 0o700))
		ln, err := net.Listen("unix", paths.Socket)
		require.NoError(t, err)
		accepted := make(chan struct{}, 1)
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- struct{}{}
			_, _ = io.Copy(io.Discard, conn)
		}()

		sigs := make(chan os.Signal, 1)
		go func() { <-accepted; sigs <- sig }()
		var out, errb bytes.Buffer
		deps := runDependencies{getenv: func(k string) string {
			if k == "ASK_HOME" {
				return home
			}
			return ""
		}}
		code := runConnect(nil, strings.NewReader(""), &out, &errb, deps, sigs, filepath.Join(home, "no-such-ask"))
		_ = ln.Close()

		require.Equal(t, want, code, errb.String())
	}
}

// stuckWriter blocks every write until release is closed and tells when the
// first write starts.
type stuckWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *stuckWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}

// A prompt that is stuck in a write to a full output must not make the wait
// after the end of stdin ignore a signal.
func TestConnectFinishEndsOnSignalWhilePromptWriteIsStuck(t *testing.T) {
	pr, pw := io.Pipe()
	out := &stuckWriter{started: make(chan struct{}), release: make(chan struct{})}
	defer close(out.release)
	c := newLineClient(&leader.Client{Registered: &leader.Registered{
		Writer: leader.NewFrameWriter(io.Discard, protocol.LeaderMaxFrame),
		Reader: leader.NewFrameReader(pr, protocol.LeaderMaxFrame),
	}}, out)
	c.view = "s1"
	go c.readLoop()
	sigs := make(chan os.Signal, 1)
	ctx, stop := withSignalContext(sigs)
	defer stop()
	c.ctx = ctx

	c.prompt("hello")
	require.NoError(t, pw.Close())
	<-c.done
	<-out.started

	got := make(chan int, 1)
	go func() { got <- c.finish(ctx, io.Discard) }()
	sigs <- os.Interrupt
	select {
	case code := <-got:
		require.Equal(t, 130, code)
	case <-time.After(5 * time.Second):
		t.Fatal("finish ignored the signal while a prompt write was stuck")
	}
}

// Every way that the leader link can end fails a pending prompt, so the exit
// code is 1 also when the end is an error frame and not a plain close.
func TestConnectFinishExitsOneWhenLeaderSendsAnErrorFrame(t *testing.T) {
	pr, pw := io.Pipe()
	c := newLineClient(&leader.Client{Registered: &leader.Registered{
		Writer: leader.NewFrameWriter(io.Discard, protocol.LeaderMaxFrame),
		Reader: leader.NewFrameReader(pr, protocol.LeaderMaxFrame),
	}}, io.Discard)
	c.view = "s1"
	go c.readLoop()

	c.prompt("hello")
	payload, err := json.Marshal(protocol.LeaderError{Kind: "shutting_down", Message: "the leader stops"})
	require.NoError(t, err)
	require.NoError(t, leader.NewFrameWriter(pw, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameError, Payload: payload}))

	var stderr strings.Builder
	require.Equal(t, 1, c.finish(context.Background(), &stderr))
	require.Contains(t, stderr.String(), "the leader closed the connection")
}
