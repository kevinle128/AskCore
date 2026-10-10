package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"AskCore/internal/tui"
)

const (
	// inputDrainLimit bounds the wait for late terminal replies on exit.
	inputDrainLimit = time.Second
	// inputDrainQuiet is the silence on the input that ends the drain early.
	inputDrainQuiet = 50 * time.Millisecond
	// closeWait bounds the wait for the client to close after a quit. The
	// in-process agent needs up to inProcessStopWait to stop its run.
	closeWait = inProcessStopWait + time.Second
)

// terminalConfig is what the interactive session needs. open is the one seam:
// ask passes the leader client with the in-process fallback, and a test can pass
// a client that fails.
type terminalConfig struct {
	in, out *os.File
	stderr  io.Writer
	// sigs delivers SIGINT, SIGTERM and SIGHUP. Nil means no signal arrives.
	sigs    <-chan os.Signal
	cwd     string
	home    string
	version string
	open    func(ctx context.Context) (tui.Client, error)
}

// runInteractive is the entry for `ask` on a terminal without a prompt. It
// runs the inline TUI against the leader of the Ask home, or against an agent in
// this process when the leader cannot be reached at startup.
func runInteractive(stdin io.Reader, stdout, stderr io.Writer, deps runDependencies, sigs <-chan os.Signal) int {
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK {
		report(stderr, "Error: the interactive mode needs a terminal on standard input and output")
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	return runTerminal(terminalConfig{
		in: in, out: out, stderr: stderr, sigs: sigs, cwd: cwd, home: home, version: buildIdentity(),
		open: func(ctx context.Context) (tui.Client, error) {
			// No warning callback: a line written to the terminal while the
			// program draws would break the screen.
			c, err := openTUIClient(ctx, deps, tuiClientOptions{callerExe: exe})
			if err != nil {
				return nil, err
			}
			return c.Client, nil
		},
	})
}

// clientSlot holds the client that the open command produced, so the entry can
// close it after the program ends, also when the user quit during the open.
type clientSlot struct {
	mu     sync.Mutex
	c      tui.Client
	closed bool
}

// put stores the client. It reports false when the entry no longer waits for
// one; then the caller must close the client itself.
func (s *clientSlot) put(c tui.Client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.c = c
	return true
}

// take returns the stored client and refuses later ones.
func (s *clientSlot) take() tui.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.c
}

type programResult struct {
	err error
}

// watchedInput is the terminal input with a report of a failed read. Bubble Tea
// ends its read loop quietly when the terminal closes under it, and the program
// would then run on without any input. The watcher turns that into a stop.
type watchedInput struct {
	*os.File
	lost func(error)
}

func (w watchedInput) Read(p []byte) (int, error) {
	n, err := w.File.Read(p)
	if err != nil {
		w.lost(err)
	}
	return n, err
}

// runTerminal runs the TUI program and ends with the terminal restored on every
// path: a quit, an error, a signal and a panic. It returns the exit code.
//
// The terminal goes to raw mode before anything else. Keys that the user types
// while ask starts then wait unechoed in the input queue and reach the editor,
// and nothing is written to the screen before the first frame.
func runTerminal(c terminalConfig) (code int) {
	fd := int(c.in.Fd())
	saved, err := term.MakeRaw(fd)
	if err != nil {
		report(c.stderr, "Error: cannot prepare the terminal:", err)
		return 1
	}
	var restoreOnce sync.Once
	restore := func() {
		restoreOnce.Do(func() {
			drainInput(fd, inputDrainQuiet, inputDrainLimit)
			if err := term.Restore(fd, saved); err != nil {
				report(c.stderr, "Error: cannot restore the terminal:", err)
			}
		})
	}
	defer func() {
		// A panic outside the Bubble Tea program, in this function.
		if v := recover(); v != nil {
			restore()
			report(c.stderr, fmt.Sprintf("Error: ask stopped on a panic: %v\n%s", v, debug.Stack()))
			code = 1
			return
		}
		restore()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var slot clientSlot
	open := func(context.Context) (tui.Client, error) {
		cl, err := c.open(ctx)
		if err != nil {
			return nil, err
		}
		if !slot.put(cl) {
			_ = cl.Close()
			return nil, context.Canceled
		}
		return cl, nil
	}

	m := tui.New(tui.Options{
		Open: open, Cwd: c.cwd, Home: c.home, Version: c.version,
		Profile: colorprofile.Detect(c.out, os.Environ()),
		// Bubble Tea writes raw input to this file when it is set. The TUI
		// warns about it where a key or a code is typed.
		TraceFile: os.Getenv("TEA_TRACE"),
	})
	// Bubble Tea would end SIGTERM with exit code 0 and ignores SIGHUP, so ask
	// handles the signals itself.
	var (
		prog      *tea.Program
		inputLost atomic.Pointer[error]
	)
	input := watchedInput{File: c.in, lost: func(err error) {
		if inputLost.CompareAndSwap(nil, &err) {
			go prog.Send(tui.StopMsg{})
		}
	}}
	prog = tea.NewProgram(m, tea.WithInput(input), tea.WithOutput(c.out), tea.WithoutSignalHandler())

	done := make(chan programResult, 1)
	go func() {
		var res programResult
		defer func() {
			if v := recover(); v != nil {
				res.err = fmt.Errorf("panic: %v\n%s", v, debug.Stack())
			}
			done <- res
		}()
		_, res.err = prog.Run()
	}()

	sig, res := waitProgram(prog, done, c.sigs)
	cancel()
	restore()

	runErr := res.err
	if lost := inputLost.Load(); runErr == nil && lost != nil {
		// There is no signal code for this: the terminal closed without a hangup
		// signal and the read failed, so it is an error exit.
		runErr = fmt.Errorf("the terminal input closed: %w", *lost)
	}
	code = exitCode(c.stderr, m, sig, runErr)
	bound := closeWait
	if sig != nil {
		bound = abortGrace
	}
	if cl := slot.take(); cl != nil {
		if err := closeWithin(cl, bound, c.sigs); err != nil {
			report(c.stderr, "Error:", err)
		}
	}
	if code == 0 {
		if rows := m.ExitNote(); len(rows) > 0 {
			if _, err := io.WriteString(c.out, strings.Join(rows, "\n")+"\n"); err != nil {
				report(c.stderr, "Error:", err)
			}
		}
	}
	return code
}

// waitProgram waits for the program to end. A signal asks it to stop and gives
// it abortGrace; a second signal or the end of the grace kills it.
func waitProgram(prog *tea.Program, done <-chan programResult, sigs <-chan os.Signal) (os.Signal, programResult) {
	select {
	case res := <-done:
		return nil, res
	case sig := <-sigs:
		go prog.Send(tui.StopMsg{})
		grace := time.NewTimer(abortGrace)
		defer grace.Stop()
		select {
		case res := <-done:
			return sig, res
		case <-sigs:
		case <-grace.C:
		}
		prog.Kill()
		select {
		case res := <-done:
			return sig, res
		case <-time.After(abortGrace):
			return sig, programResult{err: fmt.Errorf("the program did not stop")}
		}
	}
}

// exitCode reports the reason for a failed exit on stderr and returns the code.
// The terminal is restored before this runs, so the text is readable.
func exitCode(stderr io.Writer, m *tui.Model, sig os.Signal, runErr error) int {
	switch {
	case sig != nil:
		if code, ok := signalExitCodes[sig]; ok {
			return code
		}
		return 1
	case m.StartError() != nil:
		report(stderr, "Error:", m.StartError())
	case m.ClientError() != nil:
		report(stderr, "Error:", m.ClientError())
	case runErr != nil:
		report(stderr, "Error:", runErr)
	default:
		return 0
	}
	return 1
}

// closeWithin closes the client and waits at most bound, or until a signal comes.
func closeWithin(cl tui.Client, bound time.Duration, sigs <-chan os.Signal) error {
	closed := make(chan error, 1)
	go func() { closed <- cl.Close() }()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case err := <-closed:
		return err
	case <-sigs:
		return nil
	case <-timer.C:
		return fmt.Errorf("the connection did not close in time")
	}
}

// drainInput reads and drops what is waiting on the terminal input, until it
// stays quiet for the quiet time or the limit passes. A terminal may answer a
// query of the program after the program has stopped reading; the shell would
// show the answer as typed text.
func drainInput(fd int, quiet, limit time.Duration) {
	deadline := time.Now().Add(limit)
	buf := make([]byte, 256)
	for {
		wait := min(quiet, time.Until(deadline))
		if wait <= 0 {
			return
		}
		var set unix.FdSet
		set.Set(fd)
		tv := unix.NsecToTimeval(wait.Nanoseconds())
		n, err := unix.Select(fd+1, &set, nil, nil, &tv)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return
		}
		if m, err := unix.Read(fd, buf); err != nil || m == 0 {
			return
		}
	}
}
