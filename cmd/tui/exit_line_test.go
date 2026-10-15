package main

import (
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// stubExit is the state of a finished program as finishRun reads it.
type stubExit struct {
	id          string
	start, fail error
	note        []string
}

func (s stubExit) SessionID() string  { return s.id }
func (s stubExit) StartError() error  { return s.start }
func (s stubExit) ClientError() error { return s.fail }
func (s stubExit) ExitNote() []string { return s.note }

func TestExitLineIsPlainTextWithTheSessionID(t *testing.T) {
	require.Equal(t, "ask: session sess_3f2a91c07b5d8e46\n", exitLine("sess_3f2a91c07b5d8e46"))
	require.NotContains(t, exitLine("sess_x"), "\x1b", "the line has no color")
}

func TestExitLineIsEmptyWithoutASession(t *testing.T) {
	require.Empty(t, exitLine(""))
}

func TestFinishRunPrintsTheLineAfterTheQuitNote(t *testing.T) {
	var out, errs strings.Builder
	code := finishRun(&out, &errs, stubExit{id: "sess_a", note: []string{"  ⏏ Detached from session a."}}, nil, nil)
	require.Zero(t, code)
	require.Equal(t, "  ⏏ Detached from session a.\nask: session sess_a\n", out.String())
	require.Empty(t, errs.String())
}

func TestFinishRunPrintsTheLineWhenTheSessionHasNoTurn(t *testing.T) {
	var out, errs strings.Builder
	require.Zero(t, finishRun(&out, &errs, stubExit{id: "sess_a"}, nil, nil))
	require.Equal(t, "ask: session sess_a\n", out.String())
}

func TestFinishRunPrintsNothingWhenNoSessionWasCreated(t *testing.T) {
	var out, errs strings.Builder
	code := finishRun(&out, &errs, stubExit{start: errors.New("leader: boom")}, nil, nil)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.Contains(t, errs.String(), "Error: leader: boom")
}

func TestFinishRunPrintsTheLineBeforeAFatalClientError(t *testing.T) {
	// One writer receives both streams, as a terminal does, so the order of the
	// bytes is the order that the user reads.
	var both strings.Builder
	code := finishRun(&both, &both, stubExit{id: "sess_a", fail: errors.New("link broke")}, nil, nil)
	require.Equal(t, 1, code)
	text := both.String()
	require.Equal(t, "ask: session sess_a\nError: link broke\n", text)
}

func TestFinishRunPrintsTheLineBeforeARunError(t *testing.T) {
	var both strings.Builder
	code := finishRun(&both, &both, stubExit{id: "sess_a"}, nil, errors.New("program failed"))
	require.Equal(t, 1, code)
	require.Equal(t, "ask: session sess_a\nError: program failed\n", both.String())
}

func TestFinishRunKeepsTheSignalExitCodeAndSkipsTheQuitNote(t *testing.T) {
	for sig, want := range map[os.Signal]int{syscall.SIGINT: 130, syscall.SIGTERM: 143, syscall.SIGHUP: 129} {
		var out, errs strings.Builder
		code := finishRun(&out, &errs, stubExit{id: "sess_a", note: []string{"note"}}, sig, nil)
		require.Equal(t, want, code, sig.String())
		require.Equal(t, "ask: session sess_a\n", out.String(), sig.String())
		require.Empty(t, errs.String(), sig.String())
	}
}

// ---- terminal state from the byte stream ----

var privateMode = regexp.MustCompile(`\x1b\[\?([0-9;]+)([hl])`)

// finalModes replays the private mode changes in a terminal output and returns
// the last state of every mode. A mode that was never changed is absent.
func finalModes(raw string) map[string]bool {
	modes := map[string]bool{}
	for _, m := range privateMode.FindAllStringSubmatch(raw, -1) {
		for _, p := range strings.Split(m[1], ";") {
			modes[p] = m[2] == "h"
		}
	}
	return modes
}

func TestFinalModesTakesTheLastChange(t *testing.T) {
	got := finalModes("\x1b[?1003;1006h\x1b[?25l\x1b[?1003l\x1b[?25h")
	require.Equal(t, map[string]bool{"1003": false, "1006": true, "25": true}, got)
}

// requireMainScreenRestored checks criterion of a clean exit on the terminal
// that the test watched: main screen, cursor visible, mouse reporting off,
// bracketed paste off, and the last output line is the exit line.
func (a *askTerm) requireMainScreenRestored(sessionID string) {
	a.t.Helper()
	require.False(a.t, a.emu.IsAltScreen(), "the main screen is back")
	modes := finalModes(a.rawOutput())
	for _, m := range []string{"1049", "1000", "1002", "1003", "1006", "2004"} {
		require.False(a.t, modes[m], "mode %s is off", m)
	}
	// A cursor that was hidden must be shown again; a program that never hid
	// it leaves the mode unset, which is visible too.
	if on, seen := modes["25"]; seen {
		require.True(a.t, on, "the cursor is visible")
	}
	a.requireLastLine("ask: session " + sessionID)
}

// requireLastLine checks the last non-empty row of the screen and scrollback.
func (a *askTerm) requireLastLine(want string) {
	a.t.Helper()
	rows := a.rows()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i] != "" {
			require.Equal(a.t, want, rows[i], "last output line")
			return
		}
	}
	a.t.Fatalf("no output; stderr:\n%s", a.stderr.String())
}

// ---- real process through the leader ----

// onlySessionID returns the one live session of the home.
func onlySessionID(t *testing.T, bin, home string) string {
	t.Helper()
	// The header shows the leader before the session exists, so the session
	// is polled for.
	ids := liveSessionIDs(t, bin, home)
	for deadline := time.Now().Add(exitWait); len(ids) == 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		ids = liveSessionIDs(t, bin, home)
	}
	require.Len(t, ids, 1, "ask created one session")
	return ids[0]
}

func TestInteractiveExitLineAndTerminalState(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(t *testing.T, a *askTerm)
		code int
	}{
		{"quit command", func(t *testing.T, a *askTerm) { a.write("/quit\r") }, 0},
		{"ctrl+c twice", func(t *testing.T, a *askTerm) {
			a.write("\x03")
			a.waitText("Press ctrl+c again to quit")
			a.write("\x03")
		}, 0},
		{"SIGTERM", func(t *testing.T, a *askTerm) { require.NoError(t, a.cmd.Process.Signal(syscall.SIGTERM)) }, 143},
		{"SIGHUP", func(t *testing.T, a *askTerm) { require.NoError(t, a.cmd.Process.Signal(syscall.SIGHUP)) }, 129},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, home := newE2EHome(t)
			a := startAskTerm(t, bin, home, askTermOptions{})
			a.waitText("Ask anything")
			// The header shows the leader once the link is up. It must not be the
			// in-process agent.
			a.waitText("● leader")
			require.False(t, containsRow(a.rows(), "in-process"))
			a.requireRaw()
			id := onlySessionID(t, bin, home)
			tc.end(t, a)
			require.Equal(t, tc.code, a.waitExit(), a.stderr.String())
			a.requireRestored()
			a.requireMainScreenRestored(id)
			require.Empty(t, a.stderr.String())
		})
	}
}

// ---- in process, through the lifecycle function ----

// sessionClient is the smallest client that lets the program open a session.
// Any call it does not list would panic, which the test reports as a failure.
type sessionClient struct {
	tui.Client
	events chan tui.Message
	id     string
}

func (c *sessionClient) Events() <-chan tui.Message { return c.events }
func (c *sessionClient) Info() tui.ConnInfo {
	return tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: "inst_0123456789abcdef"}
}
func (c *sessionClient) Close() error { return nil }
func (c *sessionClient) NewSession(_ context.Context, cwd string) (tui.SessionInfo, error) {
	return tui.SessionInfo{ID: c.id, Role: protocol.ACPRoleDriver, Cwd: cwd}, nil
}
func (c *sessionClient) State(context.Context) (protocol.ACPStateResult, error) {
	return protocol.ACPStateResult{}, nil
}
func (c *sessionClient) Usage(context.Context) (protocol.ACPUsageResult, error) {
	return protocol.ACPUsageResult{}, nil
}
func (c *sessionClient) Models(context.Context) (protocol.ACPModelsResult, error) {
	return protocol.ACPModelsResult{}, nil
}
func (c *sessionClient) ListSessions(context.Context) ([]protocol.ACPLiveSession, error) {
	return nil, nil
}

func TestRunTerminalPrintsTheExitLineAfterTheScreenIsRestoredOnASignal(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	require.NoError(t, pty.Setsize(master, &pty.Winsize{Rows: termRows, Cols: termCols}))
	before, err := term.GetState(int(slave.Fd()))
	require.NoError(t, err)

	var raw syncBuffer
	go func() { _, _ = io.Copy(&raw, master) }()
	sigs := make(chan os.Signal, 1)
	client := &sessionClient{events: make(chan tui.Message), id: "sess_5e1f0c2a9b7d4468"}
	t.Cleanup(func() { close(client.events) })
	var stderr syncBuffer
	code := make(chan int, 1)
	go func() {
		code <- runTerminal(terminalConfig{
			in: slave, out: slave, stderr: &stderr, sigs: sigs, cwd: t.TempDir(), home: t.TempDir(), version: "test",
			open: func(context.Context) (tui.Client, error) { return client, nil },
		})
	}()
	require.Eventually(t, func() bool { return strings.Contains(raw.String(), "Ask anything") },
		exitWait, 20*time.Millisecond, "the editor shows")
	// The reply of a session start reaches the model after the first frame.
	time.Sleep(300 * time.Millisecond)
	sigs <- syscall.SIGTERM
	select {
	case c := <-code:
		require.Equal(t, 143, c, stderr.String())
	case <-time.After(exitWait):
		t.Fatal("the program did not end on SIGTERM")
	}
	after, err := term.GetState(int(slave.Fd()))
	require.NoError(t, err)
	require.Equal(t, before, after, "the terminal is restored")
	require.Eventually(t, func() bool { return strings.Contains(raw.String(), "ask: session sess_5e1f0c2a9b7d4468\r\n") },
		5*time.Second, 20*time.Millisecond, "the exit line is printed; output:\n%q", raw.String())

	out := raw.String()
	leave := strings.LastIndex(out, "\x1b[?1049l")
	require.GreaterOrEqual(t, leave, 0, "the alternate screen was left")
	require.Greater(t, strings.Index(out, "ask: session "), leave, "the line follows the screen restore")
	require.True(t, strings.HasSuffix(out, "ask: session sess_5e1f0c2a9b7d4468\r\n"), "the line ends the output: %q", out[max(0, len(out)-80):])
	require.Empty(t, stderr.String())
}
