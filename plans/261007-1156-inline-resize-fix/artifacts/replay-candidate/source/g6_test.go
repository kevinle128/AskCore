package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	term "github.com/charmbracelet/x/term"
	vt "github.com/charmbracelet/x/vt"
	pty "github.com/creack/pty"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type g6Fixture struct {
	changed                              chan struct{}
	t                                    *testing.T
	cmd                                  *exec.Cmd
	stderr                               *os.File
	master, slave, control, status       *os.File
	cancel                               context.CancelFunc
	e                                    *vt.Emulator
	mu, inputMu                          sync.Mutex
	raw                                  bytes.Buffer
	modes                                map[ansi.Mode]bool
	visible                              bool
	lines                                chan string
	done                                 chan error
	readerDone, responseDone, statusDone chan struct{}
	before                               *term.State
	waited                               bool
}

func spawnG6(t *testing.T, bin string, support bool, scenario ...string) *g6Fixture {
	t.Helper()
	f := &g6Fixture{t: t, modes: map[ansi.Mode]bool{}, visible: true, changed: make(chan struct{}, 1), lines: make(chan string, 256), done: make(chan error, 1), readerDone: make(chan struct{}), responseDone: make(chan struct{}), statusDone: make(chan struct{})}
	var err error
	f.master, f.slave, err = pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	cr, cw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	sr, sw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	f.control, f.status = cw, sr
	if err := pty.Setsize(f.master, &pty.Winsize{Cols: 40, Rows: 12}); err != nil {
		t.Fatal(err)
	}
	f.before, err = term.GetState(f.slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	f.cancel = cancel
	mode := "g6"
	if len(scenario) > 0 {
		mode = scenario[0]
	}
	args := []string{"--scenario", mode}
	if len(scenario) > 2 {
		args = append(args, "--draft-report", scenario[2])
	}
	f.cmd = exec.CommandContext(ctx, bin, args...)
	f.cmd.Env = append(os.Environ(), "TERM=xterm-256color", "T0_CONTROL=1")
	f.cmd.Stdin, f.cmd.Stdout = f.slave, f.slave
	f.stderr = diagnostics(t)
	f.cmd.Stderr = f.stderr
	f.cmd.ExtraFiles = []*os.File{cr, sw}
	f.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := f.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cr.Close()
	sw.Close()
	t.Logf("owned child pid=%d %s --scenario g6 support=%t", f.cmd.Process.Pid, bin, support)
	go func() { f.done <- f.cmd.Wait() }()
	f.e = vt.NewEmulator(40, 12)
	f.e.SetScrollbackSize(10000)
	f.e.SetCallbacks(vt.Callbacks{EnableMode: func(m ansi.Mode) { f.modes[m] = true }, DisableMode: func(m ansi.Mode) { f.modes[m] = false }, CursorVisibility: func(v bool) { f.visible = v }})
	go func() {
		defer close(f.responseDone)
		b := make([]byte, 4096)
		for {
			n, err := f.e.Read(b)
			if n > 0 {
				f.inputMu.Lock()
				response := b[:n]
				if bytes.Contains(response, []byte("\x1b[?2026;")) {
					response = regexp.MustCompile(`\x1b\[\?2026;[0-9]+\$y`).ReplaceAllFunc(response, func([]byte) []byte { return []byte("\x1b[?2026;2$y") })
				}
				_, werr := f.master.Write(response)
				f.inputMu.Unlock()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	fragment := ""
	if len(scenario) > 1 {
		fragment = scenario[1]
	}
	go func() {
		defer close(f.readerDone)
		random := rand.New(rand.NewSource(7))
		b := make([]byte, 4096)
		answered := false
		for {
			n, err := f.master.Read(b)
			if n > 0 {
				f.mu.Lock()
				f.raw.Write(b[:n])
				answer := support && !answered && strings.Contains(f.raw.String(), "\x1b[?u")
				if answer {
					answered = true
				}
				for i := 0; i < n; {
					size := n - i
					if fragment == "byte" {
						size = 1
					} else if fragment == "random" {
						size = min(size, 1+random.Intn(37))
					}
					if _, werr := f.e.Write(b[i : i+size]); werr != nil {
						f.mu.Unlock()
						return
					}
					i += size
				}
				f.mu.Unlock()
				select {
				case f.changed <- struct{}{}:
				default:
				}
				if answer {
					f.inputMu.Lock()
					_, werr := f.master.Write([]byte("\x1b[?3u"))
					f.inputMu.Unlock()
					if werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer close(f.statusDone)
		defer close(f.lines)
		s := bufio.NewScanner(sr)
		for s.Scan() {
			select {
			case f.lines <- s.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		f.control.Close()
		f.status.Close()
		f.slave.Close()
		f.master.Close()
		f.e.InputPipe().(io.Closer).Close()
		<-f.readerDone
		<-f.responseDone
		<-f.statusDone
		if !f.waited {
			<-f.done
		}
		f.e.Close()
		f.mu.Lock()
		data := append([]byte(nil), f.raw.Bytes()...)
		f.mu.Unlock()
		if err := os.WriteFile("evidence/"+strings.ReplaceAll(t.Name(), "/", "-")+".ansi", data, 0600); err != nil {
			t.Error(err)
		}
	})
	f.waitLine(func(s string) bool { return s == "size 40 12" })
	return f
}
func (f *g6Fixture) waitLine(pred func(string) bool) string {
	f.t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case s, ok := <-f.lines:
			if !ok {
				f.t.Fatal("status pipe closed")
			}
			if pred(s) {
				return s
			}
		case <-timer.C:
			f.mu.Lock()
			raw := f.raw.String()
			f.mu.Unlock()
			f.t.Fatalf("status predicate timeout raw=%q", raw)
		}
	}
}
func (f *g6Fixture) sendControl(s string) {
	f.t.Helper()
	if _, err := fmt.Fprintln(f.control, s); err != nil {
		f.t.Fatal(err)
	}
}
func (f *g6Fixture) input(s string) {
	f.t.Helper()
	f.inputMu.Lock()
	defer f.inputMu.Unlock()
	for i := 0; i < len(s); i += 37 {
		part := s[i:min(i+37, len(s))]
		n, err := io.WriteString(f.master, part)
		if err != nil || n != len(part) {
			f.t.Fatalf("PTY input n=%d err=%v", n, err)
		}
	}
}
func (f *g6Fixture) draft(want string, pastes, submits int, negotiated bool) {
	f.t.Helper()
	prefix := fmt.Sprintf("draft %x %d %d %d %d %t", sha256.Sum256([]byte(want)), strings.Count(want, "\n")+1, pastes, submits, len(want), negotiated)
	f.sendControl("inspect")
	f.waitLine(func(s string) bool { return s == prefix })
}
func (f *g6Fixture) finish(action string, signal syscall.Signal, wantCode int) {
	f.t.Helper()
	if signal != 0 {
		if err := f.cmd.Process.Signal(signal); err != nil {
			f.t.Fatal(err)
		}
	} else {
		f.sendControl(action)
	}
	f.waitLine(func(s string) bool {
		return s == "restored" || (action == "output-permanent" || action == "g1-permanent") && s == "termios-restored output-unavailable"
	})
	after, err := term.GetState(f.slave.Fd())
	if err != nil {
		f.t.Fatal(err)
	}
	if !reflect.DeepEqual(f.before, after) {
		f.t.Fatal("termios not restored")
	}
	f.sendControl("exit")
	select {
	case err := <-f.done:
		f.waited = true
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				f.t.Fatal(err)
			}
		}
		if code != wantCode {
			f.t.Fatalf("exit=%d want=%d err=%v", code, wantCode, err)
		}
	case <-time.After(5 * time.Second):
		f.t.Fatal("child not reaped")
	}
	f.slave.Close()
	select {
	case <-f.readerDone:
	case <-time.After(5 * time.Second):
		f.t.Fatal("master did not drain")
	}
	if action == "output-error" || action == "output-permanent" {
		if _, err := f.stderr.Seek(0, io.SeekStart); err != nil {
			f.t.Fatal(err)
		}
		diagnostic, err := io.ReadAll(io.LimitReader(f.stderr, 65536))
		if err != nil {
			f.t.Fatal(err)
		}
		if !bytes.Contains(diagnostic, []byte("fixture partial output write failure after 8 bytes")) {
			f.t.Fatalf("error cause missing: %s", diagnostic)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if action == "g1-permanent" {
		if !f.modes[ansi.ModeBracketedPaste] {
			f.t.Fatal("permanent unavailable cleanup falsely passed")
		}
		f.t.Log("terminal byte restoration unavailable; termios restored")
		return
	}
	if action == "output-permanent" {
		if !f.modes[ansi.ModeSynchronizedOutput] {
			f.t.Fatal("permanent byte restoration incorrectly reported available")
		}
		f.t.Log("terminal byte restoration unavailable; termios restored")
		return
	}
	if action == "output-error" {
		if !strings.Contains(f.raw.String(), "\x1b[?2026h") || strings.LastIndex(f.raw.String(), "\x1b[?2026l") < strings.LastIndex(f.raw.String(), "\x1b[?2026h") {
			f.t.Fatal("partial synchronized frame cleanup absent")
		}
		if strings.Contains(f.raw.String(), "failed output trigger") {
			f.t.Fatal("failed frame content replayed")
		}
	}
	if !f.visible || f.e.IsAltScreen() {
		f.t.Fatal("cursor/buffer not restored")
	}
	for _, m := range []ansi.Mode{ansi.ModeBracketedPaste, ansi.ModeMouseNormal, ansi.ModeMouseExtSgr, ansi.ModeSynchronizedOutput} {
		if f.modes[m] {
			f.t.Fatalf("mode=%v remains enabled", m)
		}
	}
	raw := f.raw.String()
	depth := 0
	for _, seq := range regexp.MustCompile(`\[([><])([0-9]*)u`).FindAllStringSubmatch(raw, -1) {
		if seq[1] == ">" {
			depth++
		} else {
			depth--
		}
		if depth < 0 {
			f.t.Fatal("keyboard stack underflow")
		}
	}
	if depth != 0 {
		f.t.Fatalf("keyboard stack depth=%d", depth)
	}
	if strings.LastIndex(raw, "\x1b[<1u") < strings.LastIndex(raw, "\x1b[>3u") {
		f.t.Fatal("keyboard flags not popped")
	}
	f.t.Logf("restore termios exact, final cursor=%v mode map=%v", f.e.CursorPosition(), f.modes)
}
func TestG6Input(t *testing.T) {
	bin := t.TempDir() + "/g6"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	for _, support := range []bool{false, true} {
		t.Run(fmt.Sprint(support), func(t *testing.T) {
			f := spawnG6(t, bin, support)
			if support {
				f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
			}
			f.input("a\x1b\rb")
			want := "a\nb"
			f.draft(want, 0, 0, support)
			f.waitTerminal(func() bool {
				p := f.e.CursorPosition()
				return p.X == 1 && p.Y == 2 && strings.Contains(f.e.String(), "editor> a") && strings.Contains(f.e.String(), "\nb")
			})
			if support {
				f.input("\x1b[13;2u\x1b[13;5u")
				want += "\n\n"
				f.draft(want, 0, 0, true)
			}
			f.input("\r")
			f.draft(want, 0, 1, support)
			var payload strings.Builder
			for i := 0; i < 2000; i++ {
				if i > 0 {
					payload.WriteString("\r\n")
				}
				fmt.Fprintf(&payload, "%04d\t界😀\x1b[13;2u", i)
			}
			f.input("\x1b[200~" + payload.String() + "\x1b[201~")
			want += strings.ReplaceAll(payload.String(), "\r\n", "\n")
			f.draft(want, 1, 1, support)
			f.waitTerminal(func() bool {
				p := f.e.CursorPosition()
				return p.X == 19 && p.Y == 3 && strings.Contains(f.e.String(), "1999")
			})
			f.finish("quit", 0, 0)
		})
	}
}
func TestRestoreRoutes(t *testing.T) {
	bin := t.TempDir() + "/g6"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	for _, c := range []struct {
		name, action string
		sig          syscall.Signal
		code         int
	}{
		{"normal", "quit", 0, 0}, {"context", "cancel", 0, 1}, {"output-error", "output-error", 0, 1}, {"output-permanent", "output-permanent", 0, 1}, {"SIGINT", "", syscall.SIGINT, 1}, {"SIGTERM", "", syscall.SIGTERM, 1}, {"SIGHUP", "", syscall.SIGHUP, 1}, {"model-panic", "model-panic", 0, 1}, {"command-panic", "command-panic", 0, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := spawnG6(t, bin, true)
			f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
			f.input("pending\x1b[200~incomplete paste")
			f.finish(c.action, c.sig, c.code)
		})
	}
}

func (f *g6Fixture) waitTerminal(pred func() bool) {
	f.t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		f.mu.Lock()
		ok := pred()
		f.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-f.changed:
		case <-timer.C:
			f.mu.Lock()
			state := inspect(f.e)
			f.mu.Unlock()
			f.t.Fatalf("visible cursor/frame predicate failed: %#v", state)
		}
	}
}
