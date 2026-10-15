package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	pty "github.com/creack/pty"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestG1Stock(t *testing.T) {
	bin := t.TempDir() + "/g1"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, c := range []struct {
		name, action  string
		lines, blocks int
	}{{"oversized", "g1-oversized", 30, 1}, {"ordered2000", "g1-start", 2000, 2000}, {"shrink", "g1-shrink-prepare", 10, 10}} {
		t.Run(c.name, func(t *testing.T) {
			f := spawnG6(t, bin, true, "g1")
			f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
			f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
			f.input("draft")
			f.draft("draft", 0, 0, true)
			f.sendControl(c.action)
			if c.name == "shrink" {
				f.waitTerminal(func() bool { return strings.Count(f.e.String(), "tail extra") == 2 })
				f.sendControl("g1-release")
			}
			f.waitLine(func(s string) bool { return s == fmt.Sprintf("commit %d %d false", c.blocks, c.blocks) })
			f.waitTerminal(func() bool { return strings.Contains(f.e.String(), fmt.Sprintf("tail %04d", c.blocks)) })
			f.mu.Lock()
			state := inspect(f.e)
			f.mu.Unlock()
			var got []string
			for _, row := range append(state.History, state.Rows...) {
				got = append(got, commitIDPattern.FindAllString(row, -1)...)
			}
			if len(got) != c.lines {
				t.Fatalf("terminal retains %d of %d lines; history=%d rows=%#v", len(got), c.lines, len(state.History), state.Rows)
			}
			for i, id := range got {
				if id != fmt.Sprintf("G1[%04d]", i) {
					t.Fatalf("line %d=%q", i, id)
				}
			}
			editorRow := -1
			for y, row := range state.Rows {
				if row == "editor> draft" {
					editorRow = y
				}
			}
			if state.X != 13 || state.Y != editorRow {
				t.Fatalf("cursor=%d,%d want x13 in draft", state.X, state.Y)
			}
			if strings.Contains(strings.Join(state.Rows, "\n"), "tail extra") {
				t.Fatal("stale rows after live shrink")
			}
			if !strings.Contains(strings.Join(state.Rows, "\n"), "editor> draft") {
				t.Fatal("live editor missing")
			}
			f.finish("quit", 0, 0)
		})
	}
}

func TestG1ReadyOrder(t *testing.T) {
	bin := t.TempDir() + "/g1"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	f := spawnG6(t, bin, true, "g1")
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.sendControl("g1-out-of-order")
	f.waitLine(func(s string) bool { return s == "commit 0 0 false" })
	f.sendControl("ready 2")
	f.sendControl("inspect")
	f.waitLine(func(s string) bool { return s == "commit 0 0 false" })
	f.sendControl("ready 0")
	f.waitLine(func(s string) bool { return s == "commit 1 1 false" })
	f.sendControl("ready 1")
	f.waitLine(func(s string) bool { return s == "commit 3 3 false" })
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "tail 0003") })
	f.mu.Lock()
	state := inspect(f.e)
	f.mu.Unlock()
	got := commitIDPattern.FindAllString(strings.Join(append(state.History, state.Rows...), "\n"), -1)
	if strings.Join(got, ",") != "G1[0000],G1[0001],G1[0002]" {
		t.Fatalf("completion reorder=%v", got)
	}
	f.finish("quit", 0, 0)
}
func TestG1StreamingEditor(t *testing.T) {
	bin := t.TempDir() + "/g1"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	f := spawnG6(t, bin, true, "g1")
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.input("draft")
	f.draft("draft", 0, 0, true)
	f.sendControl("g1-stream")
	draft := "draft"
	for n := 500; n <= 2000; n += 500 {
		f.waitLine(func(s string) bool { return s == fmt.Sprintf("commit %d %d false", n, n) })
		f.waitTerminal(func() bool { return strings.Contains(f.e.String(), fmt.Sprintf("tail %04d", n)) })
		f.input("x")
		draft += "x"
		f.draft(draft, 0, 0, true)
		f.waitTerminal(func() bool {
			p := f.e.CursorPosition()
			for y := 0; y < f.e.Height(); y++ {
				var row strings.Builder
				for x := 0; x < f.e.Width(); x++ {
					if c := f.e.CellAt(x, y); c != nil {
						row.WriteString(c.Content)
					}
				}
				if strings.TrimRight(row.String(), " ") == "editor> "+draft {
					return p.X == 8+len(draft) && p.Y == y
				}
			}
			return false
		})
		if n < 2000 {
			f.sendControl("g1-continue")
		}
	}
	f.mu.Lock()
	state := inspect(f.e)
	f.mu.Unlock()
	got := commitIDPattern.FindAllString(strings.Join(append(state.History, state.Rows...), "\n"), -1)
	if len(got) != 2000 {
		t.Fatalf("streamed lines=%d", len(got))
	}
	for i, id := range got {
		if id != fmt.Sprintf("G1[%04d]", i) {
			t.Fatalf("ordered line %d=%s", i, id)
		}
	}
	f.finish("quit", 0, 0)
}

func TestG1WriterFailures(t *testing.T) {
	bin := t.TempDir() + "/g1"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, kind := range []string{"zero", "partial", "short", "permanent"} {
		t.Run(kind, func(t *testing.T) {
			f := spawnG6(t, bin, true, "g1")
			f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
			f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
			f.sendControl("g1-fail-" + kind)
			f.waitLine(func(s string) bool { return s == "commit 1 0 true" })
			line := f.waitLine(func(s string) bool { return strings.HasPrefix(s, "writer true ") })
			fields := strings.Fields(line)
			if len(fields) < 7 {
				t.Fatalf("failure metadata %q", line)
			}
			n, err := strconv.Atoi(fields[2])
			if err != nil {
				t.Fatal(err)
			}
			requested, err := strconv.Atoi(fields[3])
			if err != nil {
				t.Fatal(err)
			}
			want := requested / 2
			if kind == "zero" {
				want = 0
			}
			if n != want || requested <= n {
				t.Fatalf("written prefix n=%d requested=%d want=%d", n, requested, want)
			}
			var prefix []byte
			if len(fields) > 7 {
				prefix, err = hex.DecodeString(fields[7])
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(prefix) != n || fmt.Sprintf("%x", sha256.Sum256(prefix)) != fields[6] {
				t.Fatal("prefix length/hash metadata differs")
			}
			f.mu.Lock()
			raw := append([]byte(nil), f.raw.Bytes()...)
			f.mu.Unlock()
			if !bytes.Contains(raw, prefix) {
				t.Fatal("reported written prefix absent from real PTY capture")
			}
			if bytes.Contains(raw, []byte("G1[0001]")) || bytes.Contains(raw, []byte("tail 0001")) {
				t.Fatal("transcript/frame advanced after failure")
			}
			action := "g1-failed"
			if kind == "permanent" {
				action = "g1-permanent"
			}
			f.finish(action, 0, 1)
		})
	}
}

func TestG1PendingResize(t *testing.T) {
	bin := t.TempDir() + "/g1"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	f := spawnG6(t, bin, true, "g1")
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
	f.input("draft")
	f.draft("draft", 0, 0, true)
	f.sendControl("g1-pending-resize")
	f.waitLine(func(s string) bool { return s == "pending-write" })
	if err := pty.Setsize(f.master, &pty.Winsize{Cols: 52, Rows: 15}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.e.Resize(52, 15)
	f.mu.Unlock()
	if err := f.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	f.sendControl("release-write")
	f.waitLine(func(s string) bool { return s == "size 52 15" })
	f.waitLine(func(s string) bool { return s == "commit 2000 2000 false" })
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "tail 2000") })
	f.mu.Lock()
	state := inspect(f.e)
	f.mu.Unlock()
	ids := commitIDPattern.FindAllString(strings.Join(append(state.History, state.Rows...), "\n"), -1)
	if len(ids) != 2000 {
		t.Fatalf("resize committed lines=%d", len(ids))
	}
	for i, id := range ids {
		if id != fmt.Sprintf("G1[%04d]", i) {
			t.Fatalf("resize line %d=%s", i, id)
		}
	}
	editorRow := -1
	for y, row := range state.Rows {
		if row == "editor> draft" {
			editorRow = y
		}
	}
	if state.X != 13 || state.Y != editorRow {
		t.Fatalf("resized cursor=%d,%d editorRow=%d", state.X, state.Y, editorRow)
	}
	f.finish("quit", 0, 0)
}
func TestG1OutputFragments(t *testing.T) {
	bin := t.TempDir() + "/g1"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, fragment := range []string{"byte", "random"} {
		t.Run(fragment, func(t *testing.T) {
			f := spawnG6(t, bin, true, "g1", fragment)
			f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
			f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
			f.sendControl("g1-start")
			f.waitLine(func(s string) bool { return s == "commit 2000 2000 false" })
			f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "tail 2000") })
			f.mu.Lock()
			state := inspect(f.e)
			f.mu.Unlock()
			ids := commitIDPattern.FindAllString(strings.Join(append(state.History, state.Rows...), "\n"), -1)
			if len(ids) != 2000 {
				t.Fatalf("fragment %s retained=%d", fragment, len(ids))
			}
			for i, id := range ids {
				if id != fmt.Sprintf("G1[%04d]", i) {
					t.Fatalf("fragment %s line %d=%s", fragment, i, id)
				}
			}
			f.finish("quit", 0, 0)
		})
	}
}

func TestG1SyncFailure(t *testing.T) {
	bin := t.TempDir() + "/g1"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	f := spawnG6(t, bin, true, "g1")
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
	// The same partial-frame fault is exercised with the transcript owner active.
	f.sendControl("g1-fail-sync")
	f.finish("", 0, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	raw := f.raw.String()
	if !strings.Contains(raw, "\x1b[?2026h") || !strings.Contains(raw, "\x1b[?2026l") || strings.Contains(raw, "failed output trigger") {
		t.Fatal("partial sync frame was not reset without content replay")
	}
}
