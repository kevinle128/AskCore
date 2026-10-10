package tui

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/vt"
	"github.com/stretchr/testify/require"
)

// syncBuffer is the output writer that the renderer goroutine and the test
// share. The test reads only the bytes that arrived since its last read.
type syncBuffer struct {
	mu   sync.Mutex
	data []byte
	read int
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *syncBuffer) drain() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := append([]byte(nil), b.data[b.read:]...)
	b.read = len(b.data)
	return out
}

func (b *syncBuffer) all() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// runResult is what the program returns when it stops.
type runResult struct {
	model tea.Model
	err   error
}

// screenHarness runs a model in a real Bubble Tea program and feeds the bytes
// that the renderer writes into a virtual terminal. Tests read the plain grid
// of that terminal, so they see what a user would see.
type screenHarness struct {
	t      *testing.T
	prog   *tea.Program
	out    *syncBuffer
	emu    *vt.Emulator
	done   chan runResult
	width  int
	height int
}

// startScreen starts the program with a fixed window size.
func startScreen(t *testing.T, model tea.Model, width, height int, extra ...tea.ProgramOption) *screenHarness {
	t.Helper()
	out := &syncBuffer{}
	h := &screenHarness{
		t:      t,
		out:    out,
		emu:    vt.NewEmulator(width, height),
		done:   make(chan runResult, 1),
		width:  width,
		height: height,
	}
	// A terminal in cooked output mode turns a line feed into a carriage
	// return and a line feed. The renderer writes bare line feeds when it
	// does not find a terminal, so the emulator must do the same.
	//
	// A program usually starts below a shell prompt, so the cursor starts on
	// the last row of the screen.
	_, err := h.emu.Write([]byte("\x1b[20h" + strings.Repeat("\n", height-1)))
	require.NoError(t, err)
	programOpts := []tea.ProgramOption{
		tea.WithInput(nil),
		tea.WithOutput(out),
		tea.WithWindowSize(width, height),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}),
		tea.WithoutSignalHandler(),
	}
	h.prog = tea.NewProgram(model, append(programOpts, extra...)...)
	go func() {
		final, err := h.prog.Run()
		h.done <- runResult{model: final, err: err}
	}()
	t.Cleanup(func() {
		h.prog.Kill()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
		}
		_ = h.emu.Close()
	})
	// Tests change the transcript only after the first frame is on the
	// screen, as a user does. The renderer measures scrolling against the
	// last frame, so output that comes before any frame is not a real case.
	h.waitFor("first frame", func(h *screenHarness) bool {
		for _, row := range h.grid() {
			if row != "" {
				return true
			}
		}
		return false
	})
	return h
}

func (h *screenHarness) send(msg tea.Msg) { h.prog.Send(msg) }

// stop ends the program and returns its final model. The model is safe to
// read after this call because the update loop has stopped.
func (h *screenHarness) stop() tea.Model {
	h.t.Helper()
	h.prog.Quit()
	select {
	case res := <-h.done:
		require.NoError(h.t, res.err)
		h.done <- res
		return res.model
	case <-time.After(10 * time.Second):
		h.t.Fatal("program did not stop")
		return nil
	}
}

// resize changes the terminal and tells the program, as a real terminal does.
func (h *screenHarness) resize(width, height int) {
	h.t.Helper()
	h.pump()
	h.emu.Resize(width, height)
	h.width, h.height = width, height
	h.prog.Send(tea.WindowSizeMsg{Width: width, Height: height})
}

// pump moves the new renderer bytes into the virtual terminal.
func (h *screenHarness) pump() {
	h.t.Helper()
	data := h.out.drain()
	if len(data) == 0 {
		return
	}
	_, err := h.emu.Write(data)
	require.NoError(h.t, err)
}

// grid is the visible screen: exactly height rows, trailing spaces removed.
func (h *screenHarness) grid() []string {
	h.pump()
	rows := strings.Split(h.emu.String(), "\n")
	for len(rows) < h.height {
		rows = append(rows, "")
	}
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows[:h.height]
}

// scrollback is the text that scrolled above the visible screen.
func (h *screenHarness) scrollback() []string {
	h.pump()
	var rows []string
	for _, line := range h.emu.Scrollback().Lines() {
		rows = append(rows, strings.TrimRight(line.String(), " "))
	}
	return rows
}

// text is scrollback and screen together, as one list of rows.
func (h *screenHarness) text() []string {
	return append(h.scrollback(), h.grid()...)
}

// waitFor polls the screen until the condition holds.
func (h *screenHarness) waitFor(what string, cond func(h *screenHarness) bool) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond(h) {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.t.Fatalf("timeout waiting for %s; screen:\n%s", what, strings.Join(h.text(), "\n"))
}

// waitText waits until a row of the screen or scrollback contains the text.
func (h *screenHarness) waitText(text string) {
	h.t.Helper()
	h.waitFor(text, func(h *screenHarness) bool {
		for _, row := range h.text() {
			if strings.Contains(row, text) {
				return true
			}
		}
		return false
	})
}

// count is the number of rows that contain the text.
func (h *screenHarness) count(text string) int {
	n := 0
	for _, row := range h.text() {
		if strings.Contains(row, text) {
			n++
		}
	}
	return n
}

// settle waits until the screen stays the same for a quiet period. It is for
// goldens, where no single marker proves that the last frame arrived.
func (h *screenHarness) settle() []string {
	h.t.Helper()
	prev := strings.Join(h.grid(), "\n")
	stable := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
		cur := strings.Join(h.grid(), "\n")
		if cur == prev {
			stable++
			if stable >= 3 {
				return strings.Split(cur, "\n")
			}
			continue
		}
		stable, prev = 0, cur
	}
	h.t.Fatalf("screen did not settle:\n%s", prev)
	return nil
}

// eachOnceInOrder reports whether every token shows exactly once in the
// scrollback and the screen together, in the given order. The second result
// says what is wrong, for the failure message.
func eachOnceInOrder(rows []string, tokens []string) (bool, string) {
	text := strings.Join(rows, "\n")
	last := -1
	for _, tok := range tokens {
		if n := strings.Count(text, tok); n != 1 {
			return false, fmt.Sprintf("%q shows %d times", tok, n)
		}
		idx := strings.Index(text, tok)
		if idx < last {
			return false, fmt.Sprintf("%q is out of order", tok)
		}
		last = idx
	}
	return true, ""
}

// requireEachOnce waits until every token shows exactly once and in order,
// then checks that this still holds after the screen stops changing.
//
// The wait is needed because a commit writes a row above the frame while the
// frame on screen still shows that row in its tail. The renderer draws the
// shorter frame on its own timer, so for up to one draw interval a row shows
// twice. A row that stays twice is a defect: the wait then times out, and the
// final check fails if it comes back after the screen is quiet.
func (h *screenHarness) requireEachOnce(tokens ...string) {
	h.t.Helper()
	var why string
	h.waitFor("every row once and in order", func(h *screenHarness) bool {
		ok, w := eachOnceInOrder(h.text(), tokens)
		why = w
		return ok
	})
	h.settle()
	ok, w := eachOnceInOrder(h.text(), tokens)
	if !ok {
		h.t.Fatalf("%s after the screen stopped changing (was: %s):\n%s", w, why, strings.Join(h.text(), "\n"))
	}
}

// settleText waits until the scrollback and the screen together stop
// changing, and returns them as one list of rows.
func (h *screenHarness) settleText() []string {
	h.t.Helper()
	prev := strings.Join(h.text(), "\n")
	stable := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
		cur := strings.Join(h.text(), "\n")
		if cur == prev {
			stable++
			if stable >= 3 {
				return strings.Split(cur, "\n")
			}
			continue
		}
		stable, prev = 0, cur
	}
	h.t.Fatalf("screen did not settle:\n%s", prev)
	return nil
}

// requireGolden compares the settled grid with the golden file of the test.
// Run the tests with -update to refresh the files.
func (h *screenHarness) requireGolden() {
	h.t.Helper()
	golden.RequireEqual(h.t, strings.Join(h.settle(), "\n")+"\n")
}
