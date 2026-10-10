package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"

	"AskCore/internal/leader"
	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

const (
	termCols = 80
	termRows = 24
	// exitWait bounds the wait for `ask` to end in a test.
	exitWait = 30 * time.Second
)

// askTerm is a real `ask` process on a pseudo terminal. A virtual terminal reads
// what the process draws, so a test sees the screen a user would see. The test
// keeps the slave side open, which lets it read the termios of the terminal
// before and after the run.
type askTerm struct {
	t      *testing.T
	cmd    *exec.Cmd
	master *os.File
	slave  *os.File
	stderr *syncBuffer

	mu    sync.Mutex
	emu   *vt.Emulator
	raw   bytes.Buffer
	bytes int

	initial *term.State
	waited  chan error
	code    int
	exited  bool
}

type askTermOptions struct {
	// typeAhead is written to the terminal before the process starts.
	typeAhead string
}

// interactiveEnv is the environment of an interactive test process.
func interactiveEnv(home string, extra ...string) []string {
	// A key of the developer shell must not reach the leader of a test, or a
	// model would look signed in when the test expects it is not.
	env := withoutCredentialEnv(leaderEnv(home))
	env = append(env, "TERM=xterm-256color", "LANG=en_US.UTF-8")
	return append(env, extra...)
}

func startAskTerm(t *testing.T, bin, home string, o askTermOptions, extraEnv ...string) *askTerm {
	t.Helper()
	master, slave, err := pty.Open()
	require.NoError(t, err)
	require.NoError(t, pty.Setsize(master, &pty.Winsize{Rows: termRows, Cols: termCols}))
	a := &askTerm{t: t, master: master, slave: slave, stderr: &syncBuffer{}, emu: vt.NewEmulator(termCols, termRows), waited: make(chan error, 1)}
	// A program starts below a shell prompt, so the cursor starts on the last row.
	_, err = a.emu.Write([]byte(strings.Repeat("\r\n", termRows-1)))
	require.NoError(t, err)
	a.initial, err = term.GetState(int(slave.Fd()))
	require.NoError(t, err)
	if o.typeAhead != "" {
		_, err = master.WriteString(o.typeAhead)
		require.NoError(t, err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = t.TempDir()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, a.stderr
	cmd.Env = interactiveEnv(home, extraEnv...)
	// The process has no controlling terminal. When the session leader of a
	// terminal exits, the system revokes the terminal and the test could not
	// read its termios afterwards. Signals are sent to the process directly, and
	// a closed terminal gives no hangup, only a failed read.
	a.cmd = cmd

	// The virtual terminal answers the queries of the program, as a terminal does.
	go func() { _, _ = io.Copy(master, a.emu) }()
	go a.readScreen()
	require.NoError(t, cmd.Start())
	go func() { a.waited <- cmd.Wait() }()
	t.Cleanup(func() {
		if !a.exited {
			_ = cmd.Process.Kill()
			<-a.waited
		}
		_ = master.Close()
		_ = slave.Close()
		// Close the reply pipe of the emulator, not the emulator: its Close
		// writes a flag that its Read reads without a lock.
		if pw, ok := a.emu.InputPipe().(*io.PipeWriter); ok {
			_ = pw.CloseWithError(io.EOF)
		}
	})
	return a
}

func (a *askTerm) readScreen() {
	buf := make([]byte, 8192)
	for {
		n, err := a.master.Read(buf)
		if n > 0 {
			a.mu.Lock()
			a.raw.Write(buf[:n])
			a.bytes += n
			_, _ = a.emu.Write(buf[:n])
			a.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// rows returns the visible screen and the scrollback, trailing spaces removed.
func (a *askTerm) rows() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, line := range a.emu.Scrollback().Lines() {
		out = append(out, strings.TrimRight(line.String(), " "))
	}
	for _, row := range strings.Split(a.emu.String(), "\n") {
		out = append(out, strings.TrimRight(row, " "))
	}
	return out
}

func (a *askTerm) screen() string { return strings.Join(a.rows(), "\n") }

func (a *askTerm) rawOutput() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.raw.String()
}

func (a *askTerm) waitFor(what string, cond func(rows []string) bool) {
	a.t.Helper()
	deadline := time.Now().Add(exitWait)
	for time.Now().Before(deadline) {
		if cond(a.rows()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.t.Fatalf("timeout waiting for %s; screen:\n%s\nstderr:\n%s", what, a.screen(), a.stderr.String())
}

func (a *askTerm) waitText(text string) {
	a.t.Helper()
	a.waitFor(text, func(rows []string) bool { return containsRow(rows, text) })
}

func containsRow(rows []string, text string) bool {
	for _, r := range rows {
		if strings.Contains(r, text) {
			return true
		}
	}
	return false
}

func (a *askTerm) write(s string) {
	a.t.Helper()
	_, err := a.master.WriteString(s)
	require.NoError(a.t, err)
}

// waitExit waits for the process to end and for the terminal output to go
// quiet, then returns the exit code.
func (a *askTerm) waitExit() int {
	a.t.Helper()
	select {
	case err := <-a.waited:
		a.exited = true
		a.code = 0
		var exit *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exit):
			a.code = exit.ExitCode()
		default:
			a.t.Fatalf("wait: %v", err)
		}
	case <-time.After(exitWait):
		_ = a.cmd.Process.Kill()
		<-a.waited
		a.exited = true
		a.t.Fatalf("ask did not exit; screen:\n%s\nstderr:\n%s", a.screen(), a.stderr.String())
	}
	// The reader may still hold the last bytes.
	for last := -1; ; {
		a.mu.Lock()
		n := a.bytes
		a.mu.Unlock()
		if n == last {
			break
		}
		last = n
		time.Sleep(100 * time.Millisecond)
	}
	return a.code
}

// requireRaw proves that the terminal is not in its start state, so the
// restore check cannot pass because nothing changed.
func (a *askTerm) requireRaw() {
	a.t.Helper()
	now, err := term.GetState(int(a.slave.Fd()))
	require.NoError(a.t, err)
	require.NotEqual(a.t, a.initial, now, "the terminal is in raw mode while ask runs")
}

// requireRestored checks echo, line mode and every other termios setting.
func (a *askTerm) requireRestored() {
	a.t.Helper()
	now, err := term.GetState(int(a.slave.Fd()))
	require.NoError(a.t, err)
	require.Equal(a.t, a.initial, now, "the terminal is restored")
}

// requireLeaderFooter checks the leader id in the footer against the id that
// the leader of the home reports, so a fallback cannot pass.
func requireLeaderFooter(t *testing.T, a *askTerm, bin, home string) string {
	t.Helper()
	// The editor shows while the leader starts; the header comes with the link.
	a.waitText("ask   v")
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	st, err := leader.Status(ctx, e2eConnect(home, bin))
	require.NoError(t, err)
	short := tui.ShortID(st.InstanceID)
	a.waitFor("the leader footer", func(rows []string) bool {
		for _, r := range rows {
			if strings.Contains(r, "⌂") && strings.Contains(r, "leader "+short) {
				return true
			}
		}
		return false
	})
	require.False(t, containsRow(a.rows(), "in-process"), "a leader test must not run in process")
	return st.InstanceID
}

func TestInteractiveStartsLeaderAndShowsEditor(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{})
	a.waitText("Ask anything")
	a.waitText("ask   v")
	require.Contains(t, a.rawOutput(), "Starting leader…", "the starting state shows before the editor")
	a.waitFor("the starting state to go", func(rows []string) bool { return !containsRow(rows, "Starting leader") })
	requireLeaderFooter(t, a, bin, home)
	require.Len(t, leaderProcesses(bin), 1, "ask started one leader")
	a.requireRaw()

	a.write("\x04") // ctrl+d on an empty draft
	require.Equal(t, 0, a.waitExit(), a.stderr.String())
	a.requireRestored()
	a.waitText("Detached from session")
	require.False(t, containsRow(a.rows(), "Ask anything"), "the editor is gone after the quit")
	require.Empty(t, a.stderr.String())
}

func TestInteractiveFallsBackInProcess(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	makeHomeUnsafe(t, home)
	a := startAskTerm(t, bin, home, askTermOptions{})
	a.waitText("○ in-process")
	a.waitText("in-process agent")
	a.write("hello there\r")
	a.waitText("◆ hello there")
	require.Empty(t, leaderProcesses(bin), "no leader process is started")

	// Quitting stops the run, and the screen says so.
	a.write("\x03")
	a.waitText("Press ctrl+c again to quit. The run stops.")
	a.write("\x03")
	require.Equal(t, 0, a.waitExit(), a.stderr.String())
	a.requireRestored()
	a.waitText("Run stopped. Session ended.")
}

func TestInteractiveSignalsRestoreTerminal(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		code int
	}{
		{"SIGINT", syscall.SIGINT, 130},
		{"SIGTERM", syscall.SIGTERM, 143},
		{"SIGHUP", syscall.SIGHUP, 129},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, home := newE2EHome(t)
			a := startAskTerm(t, bin, home, askTermOptions{})
			a.waitText("Ask anything")
			requireLeaderFooter(t, a, bin, home)
			a.requireRaw()
			require.NoError(t, a.cmd.Process.Signal(tc.sig))
			require.Equal(t, tc.code, a.waitExit(), a.stderr.String())
			a.requireRestored()
			require.False(t, containsRow(a.rows(), "Ask anything"), "the live region is cleared")
		})
	}
}

func TestInteractiveQuitDetachesRunContinues(t *testing.T) {
	bin, home := newE2EHome(t)
	prompt := strings.Repeat("slow words ", 10)
	a := startAskTerm(t, bin, home, askTermOptions{}, pacedEnv(8)...)
	a.waitText("Ask anything")
	instance := requireLeaderFooter(t, a, bin, home)
	a.write(prompt + "\r")
	a.waitText("Working…")

	a.write("\x04") // ctrl+d while the run goes on
	require.Equal(t, 0, a.waitExit(), a.stderr.String())
	a.requireRestored()
	short := tui.ShortID(instance)
	a.waitText("Detached from session")
	a.waitText("The run continues in leader " + short)
	require.False(t, containsRow(a.rows(), "Ask anything"))

	// The leader still runs the turn after the client is gone, and finishes it.
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	st, err := leader.Status(ctx, e2eConnect(home, bin))
	require.NoError(t, err)
	require.Equal(t, 1, st.ActiveRuns, "the run goes on without the client")
	require.Eventually(t, func() bool {
		st, err := leader.Status(ctx, e2eConnect(home, bin))
		return err == nil && st.ActiveRuns == 0
	}, clientWait, 100*time.Millisecond, "the turn finishes")

	c, col := openLeaderClient(t, bin, home)
	live, err := c.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	_, err = c.Attach(ctx, live[0].SessionID)
	require.NoError(t, err)
	_, snap := col.waitFor("the history", isType[tui.SnapshotMsg])
	var reply protocol.AssistantMessage
	for _, e := range snap.(tui.SnapshotMsg).Entries {
		if m, ok := e.Message.(protocol.AssistantMessage); ok {
			reply = m
		}
	}
	require.Equal(t, protocol.StopStop, reply.StopReason, "the turn ended normally, not cancelled")
	var text strings.Builder
	for _, b := range reply.Content {
		if tb, ok := b.(protocol.Text); ok {
			text.WriteString(tb.Text)
		}
	}
	require.Contains(t, text.String(), strings.TrimSpace(prompt), "the whole reply was produced")
}

func TestInteractiveEarlyInputKept(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{typeAhead: "typed early"})
	a.waitFor("the early text in the editor", func(rows []string) bool {
		return containsRow(rows, "❯ typed early")
	})
	requireLeaderFooter(t, a, bin, home)
	// The text is a draft and nothing was sent.
	require.False(t, containsRow(a.rows(), "Working"))
	a.write("\x03") // clears the draft
	a.waitFor("the draft cleared", func(rows []string) bool {
		return !containsRow(rows, "typed early") && containsRow(rows, "Ask anything")
	})
	a.write("\x04")
	require.Equal(t, 0, a.waitExit(), a.stderr.String())
	a.requireRestored()
}

func TestInteractiveShiftEnterInsertsNewline(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startAskTerm(t, bin, home, askTermOptions{})
	requireLeaderFooter(t, a, bin, home)
	// The program asks the terminal for key disambiguation, which is what
	// makes shift+enter reach it as its own key.
	require.Contains(t, a.rawOutput(), "\x1b[>1u")
	a.write("first")
	a.write("\x1b[13;2u") // shift+enter as the terminal reports it
	a.write("second")
	a.waitFor("two draft rows", func(rows []string) bool {
		return containsRow(rows, "❯ first") && containsRow(rows, "second") && !containsRow(rows, "Working")
	})
	require.False(t, containsRow(a.rows(), "Sending"), "shift+enter does not send")
	a.write("\x03")
	a.write("\x03")
	a.write("\x03")
	require.Equal(t, 0, a.waitExit(), a.stderr.String())
}

func TestInteractiveErrorExitRestores(t *testing.T) {
	t.Run("start error", func(t *testing.T) {
		// The leader cannot start and the in-process agent cannot start either:
		// the faux pace is not a number, which both of them refuse.
		bin := acpBinary(t)
		home := leaderHome(t)
		makeHomeUnsafe(t, home)
		a := startAskTerm(t, bin, home, askTermOptions{}, "ASK_FAUX_TPS=fast")
		require.Equal(t, 1, a.waitExit())
		a.requireRestored()
		require.Contains(t, a.stderr.String(), "ASK_FAUX_TPS")
		require.Contains(t, a.stderr.String(), "Error:")
		require.False(t, containsRow(a.rows(), "Ask anything"))
	})

	t.Run("stdin lost", func(t *testing.T) {
		// The process has no controlling terminal, so no hangup signal comes;
		// the only sign that the user is gone is the failed read.
		bin, home := newE2EHome(t)
		a := startAskTerm(t, bin, home, askTermOptions{})
		a.waitText("Ask anything")
		_ = a.master.Close()
		require.NotEqual(t, 0, a.waitExit(), "ask ends when its input is gone")
	})
}

// panicClient is a client whose first call panics, as a bug in a client would.
type panicClient struct {
	tui.Client
	events chan tui.Message
}

func (p *panicClient) Events() <-chan tui.Message { return p.events }
func (p *panicClient) Info() tui.ConnInfo         { return tui.ConnInfo{Mode: tui.ModeInProcess} }
func (p *panicClient) Close() error               { return nil }
func (p *panicClient) NewSession(context.Context, string) (tui.SessionInfo, error) {
	panic("client bug")
}

// A panic inside the program is recovered by Bubble Tea. The lifecycle code must
// still end with a restored terminal and exit code 1. The built binary has no
// way to cause such a panic, so this test runs the same lifecycle function on a
// real pseudo terminal with a client that panics.
func TestInteractiveErrorExitRestoresAfterPanic(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	require.NoError(t, pty.Setsize(master, &pty.Winsize{Rows: termRows, Cols: termCols}))
	before, err := term.GetState(int(slave.Fd()))
	require.NoError(t, err)
	go func() { _, _ = io.Copy(io.Discard, master) }()

	var stderr syncBuffer
	pc := &panicClient{events: make(chan tui.Message)}
	t.Cleanup(func() { close(pc.events) })
	code := make(chan int, 1)
	go func() {
		code <- runTerminal(terminalConfig{
			in: slave, out: slave, stderr: &stderr, cwd: t.TempDir(), home: t.TempDir(), version: "test",
			open: func(context.Context) (tui.Client, error) { return pc, nil },
		})
	}()
	select {
	case c := <-code:
		require.Equal(t, 1, c)
	case <-time.After(exitWait):
		t.Fatal("the program did not end after the panic")
	}
	after, err := term.GetState(int(slave.Fd()))
	require.NoError(t, err)
	require.Equal(t, before, after, "the terminal is restored after a panic")
	require.Contains(t, stderr.String(), "Error:")
}
