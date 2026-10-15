package main

import (
	"bufio"
	"bytes"
	"context"
	"github.com/charmbracelet/x/ansi"
	term "github.com/charmbracelet/x/term"
	vt "github.com/charmbracelet/x/vt"
	pty "github.com/creack/pty"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestStartupNormalExit(t *testing.T) {
	bin := t.TempDir() + "/smoke"
	b := exec.Command("go", "build", "-o", bin, ".")
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 12, Cols: 40}); err != nil {
		t.Fatal(err)
	}
	before, err := term.GetState(slave.Fd())
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
	defer cw.Close()
	defer sr.Close()
	defer cr.Close()
	defer sw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--scenario", "smoke")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "T0_CONTROL=1")
	cmd.Stdin, cmd.Stdout = slave, slave
	cmd.Stderr = diagnostics(t)
	cmd.ExtraFiles = []*os.File{cr, sw}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cr.Close()
	sw.Close()
	t.Logf("child pid=%d command=%s --scenario smoke", cmd.Process.Pid, bin)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waited := false
	defer func() {
		if !waited {
			cancel()
			<-done
		}
	}()
	e := vt.NewEmulator(40, 12)
	e.SetScrollbackSize(100)
	defer e.Close()
	var inputMu sync.Mutex
	responseDone := make(chan struct{})
	go func() {
		defer close(responseDone)
		buf := make([]byte, 4096)
		for {
			n, err := e.Read(buf)
			if n > 0 {
				inputMu.Lock()
				_, writeErr := master.Write(buf[:n])
				inputMu.Unlock()
				if writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { e.InputPipe().(io.Closer).Close(); <-responseDone; e.Close() }()
	var mu sync.Mutex
	modes := map[ansi.Mode]bool{}
	visible := true
	e.SetCallbacks(vt.Callbacks{EnableMode: func(m ansi.Mode) { modes[m] = true }, DisableMode: func(m ansi.Mode) { modes[m] = false }, CursorVisibility: func(v bool) { visible = v }})
	var raw bytes.Buffer
	changed := make(chan struct{}, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				mu.Lock()
				raw.Write(buf[:n])
				if n, err := e.Write(buf[:n]); err != nil || n != len(buf[:n]) {
					mu.Unlock()
					return
				}
				mu.Unlock()
				select {
				case changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { cancel(); slave.Close(); master.Close(); e.InputPipe().(io.Closer).Close(); <-readDone }()
	wait := func(label string, predicate func() bool) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			if predicate() {
				return
			}
			select {
			case <-changed:
			case <-timer.C:
				mu.Lock()
				s := inspect(e)
				mu.Unlock()
				t.Fatalf("%s missing; terminal=%#v", label, s)
			}
		}
	}
	wait("live editor", func() bool { mu.Lock(); defer mu.Unlock(); return strings.Contains(e.String(), "editor>") })
	during, err := term.GetState(slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, during) {
		t.Fatal("slave did not enter raw mode")
	}
	wait("startup block", func() bool { mu.Lock(); defer mu.Unlock(); return strings.Contains(e.String(), "T0 startup") })
	inputMu.Lock()
	_, err = master.Write([]byte("hello"))
	inputMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	wait("typed editor", func() bool { mu.Lock(); defer mu.Unlock(); return strings.Contains(e.String(), "editor> hello") })
	mu.Lock()
	cursor := e.CursorPosition()
	if cursor.X != 13 || cursor.Y != 1 {
		mu.Unlock()
		t.Fatalf("live cursor=%v want (13,1)", cursor)
	}
	t.Logf("raw termios before=%#v during=%#v modes=%v cursor=%v", before, during, modes, cursor)
	mu.Unlock()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 15, Cols: 52}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	e.Resize(52, 15)
	mu.Unlock()
	if err := cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 10)
	statusDone := make(chan struct{})
	defer func() { sr.Close(); <-statusDone }()
	go func() {
		defer close(statusDone)
		s := bufio.NewScanner(sr)
		for s.Scan() {
			select {
			case lines <- s.Text():
			case <-ctx.Done():
				return
			}
		}
		close(lines)
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	sized := false
	for !sized {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("status closed")
			}
			sized = line == "size 52 15"
		case <-timer.C:
			t.Fatal("ioctl/SIGWINCH did not reach model")
		}
	}
	if _, err := cw.Write([]byte("quit\n")); err != nil {
		t.Fatal(err)
	}
	restored := false
	for !restored {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("status closed before restoration")
			}
			restored = line == "restored"
		case <-ctx.Done():
			t.Fatal("child did not restore")
		}
	}
	after, err := term.GetState(slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("termios not restored: before=%#v after=%#v", before, after)
	}
	if _, err := cw.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		waited = true
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("child did not exit")
	}
	slave.Close()
	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatal("capture did not stop")
	}
	mu.Lock()
	data := append([]byte(nil), raw.Bytes()...)
	state := inspect(e)
	mu.Unlock()
	mu.Lock()
	if !visible {
		mu.Unlock()
		t.Fatal("cursor not restored")
	}
	for _, m := range []ansi.Mode{ansi.ModeBracketedPaste, ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent, ansi.ModeMouseExtSgr, ansi.ModeSynchronizedOutput} {
		if modes[m] {
			mu.Unlock()
			t.Fatalf("mode %v not restored", m)
		}
	}
	t.Logf("final modes=%v visible=%v termios=%#v", modes, visible, after)
	mu.Unlock()
	want := snapshot{Rows: []string{"T0 startup", "", "", "", "", "", "", "", "", "", "", "", "", "", ""}, History: []string{""}, X: 0, Y: 1, Alt: false}
	if err := compare(state, want); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("evidence/smoke-output.ansi", data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("restored termios; final terminal=%#v", state)
}

func TestChildCancellation(t *testing.T) {
	bin := t.TempDir() + "/smoke"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	before, err := term.GetState(slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout = slave, slave
	cmd.Stderr = diagnostics(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	reaped := false
	defer func() {
		cancel()
		slave.Close()
		master.Close()
		if !reaped {
			<-done
		}
	}()
	drainDone := make(chan struct{})
	go func() { io.Copy(io.Discard, master); close(drainDone) }()
	defer func() { cancel(); slave.Close(); master.Close(); <-drainDone }()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	raw := false
	for !raw {
		select {
		case <-tick.C:
			state, err := term.GetState(slave.Fd())
			if err != nil {
				cancel()
				<-done
				reaped = true
				t.Fatal(err)
			}
			raw = !reflect.DeepEqual(state, before)
		case <-deadline.C:
			cancel()
			<-done
			reaped = true
			t.Fatal("child never entered raw mode")
		}
	}
	t.Logf("cancel owned child pid=%d command=%s", cmd.Process.Pid, bin)
	cancel()
	select {
	case err := <-done:
		reaped = true
		if err == nil {
			t.Fatal("cancelled child returned success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled child was not reaped")
	}
}

func diagnostics(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer f.Close()
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Error(err)
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil {
			t.Error(err)
			return
		}
		if len(data) > 65536 {
			t.Error("child diagnostics exceed 64KiB")
			data = data[:65536]
		}
		if err := os.WriteFile("evidence/"+strings.ReplaceAll(t.Name(), "/", "-")+"-stderr.txt", data, 0600); err != nil {
			t.Error(err)
		}
		if len(data) > 0 {
			t.Logf("separate child stderr: %s", data)
		}
	})
	return f
}
