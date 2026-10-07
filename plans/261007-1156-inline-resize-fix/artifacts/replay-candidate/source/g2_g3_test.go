package main

import (
	"fmt"
	pty "github.com/creack/pty"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestG2Selector(t *testing.T) {
	bin := t.TempDir() + "/selector"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	f := spawnG6(t, bin, true, "g2")
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
	f.input("draft")
	f.draft("draft", 0, 0, true)
	f.sendControl("g1-shrink-prepare")
	f.sendControl("g1-release")
	f.waitLine(func(s string) bool { return s == "commit 10 10 false" })
	f.input("\x1b[D\x1b[D\x1b[D\t")
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "item 01") })
	for i := 0; i < 11; i++ {
		f.input("\x1b[B")
	}
	f.waitTerminal(func() bool {
		return strings.Contains(f.e.String(), "> item 12") && !strings.Contains(f.e.String(), "item 01")
	})
	resizeFixture(f, 10, 12)
	f.waitTerminal(func() bool {
		state := inspect(f.e)
		window := []string{}
		selectedRow := -1
		for y, line := range state.Rows {
			if strings.Contains(line, "item ") {
				window = append(window, line)
			}
			if line == "> item 12" {
				selectedRow = y
			}
		}
		return strings.Join(window, "|") == "  item 10|  item 11|> item 12" && state.X == 0 && state.Y == selectedRow
	})
	f.input("discard\x1b[200~paste\x1b[201~")
	resizeFixture(f, 40, 12)
	f.input("\x1b")
	f.sendControl("inspect")
	f.draft("draft", 0, 0, true)
	f.waitTerminal(func() bool {
		p := f.e.CursorPosition()
		row := -1
		for y, line := range inspect(f.e).Rows {
			if line == "editor> draft" {
				row = y
			}
		}
		return p.X == 10 && p.Y == row && row >= 0 && !strings.Contains(f.e.String(), "item ")
	})
	f.mu.Lock()
	state := inspect(f.e)
	raw := f.raw.String()
	f.mu.Unlock()
	ids := commitIDPattern.FindAllString(strings.Join(append(state.History, state.Rows...), "\n"), -1)
	if len(ids) != 10 {
		t.Fatalf("selector lost committed history: %v", ids)
	}
	for i, id := range ids {
		if id != fmt.Sprintf("G1[%04d]", i) {
			t.Fatalf("history order %v", ids)
		}
	}
	assertReplayGenerations(t, raw, 10, 10, 10)
	f.input("\t")
	f.waitTerminal(func() bool {
		state := inspect(f.e)
		window := []string{}
		selectedRow := -1
		for y, line := range state.Rows {
			if strings.Contains(line, "item ") {
				window = append(window, line)
			}
			if line == "> item 12" {
				selectedRow = y
			}
		}
		return strings.Join(window, "|") == "  item 10|  item 11|> item 12" && state.X == 0 && state.Y == selectedRow
	})
	resizeFixture(f, 10, 4)
	f.waitTerminal(func() bool {
		return strings.Contains(f.e.String(), "> item 12") && !strings.Contains(f.e.String(), "editor>")
	})
	f.input("\x1b")
	f.waitTerminal(func() bool { return !strings.Contains(f.e.String(), "item ") })
	f.mu.Lock()
	raw = f.raw.String()
	f.mu.Unlock()
	assertReplayGenerations(t, raw, 10, 10, 10, 10)
	f.finish("quit", 0, 0)
}

func resizeFixture(f *g6Fixture, width, height int) {
	f.t.Helper()
	if err := pty.Setsize(f.master, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)}); err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.e.Resize(width, height)
	f.mu.Unlock()
	if err := f.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		f.t.Fatal(err)
	}
	f.waitLine(func(s string) bool { return s == fmt.Sprintf("size %d %d", width, height) })
	f.waitLine(func(s string) bool {
		fields := strings.Fields(s)
		return len(fields) == 7 && fields[0] == "replay" && fields[5] == strconv.Itoa(width) && fields[6] == strconv.Itoa(height)
	})
}
func TestG3UnicodeResize(t *testing.T) {
	bin := t.TempDir() + "/resize"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	f := spawnG6(t, bin, true, "g3")
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
	f.input("abc界😀Z")
	f.draft("abc界😀Z", 0, 0, true)
	for _, size := range [][2]int{{13, 6}, {12, 6}, {2, 1}, {1, 1}, {40, 12}, {40, 4}, {13, 6}, {40, 12}} {
		resizeFixture(f, size[0], size[1])
		x := 16
		if size[0] == 13 {
			x = 3
		}
		if size[0] == 12 {
			x = 5
		}
		if size[0] == 2 {
			x = 1
		}
		if size[0] == 1 {
			x = 0
		}
		f.waitTerminal(func() bool { p := f.e.CursorPosition(); return p.X == x && p.Y >= 0 && p.Y < size[1] })
		f.mu.Lock()
		state := inspect(f.e)
		f.mu.Unlock()
		if size[0] == 13 || size[0] == 12 {
			found := false
			for _, row := range state.Rows {
				if row == "😀Z" && size[0] == 13 || row == "界😀Z" && size[0] == 12 {
					found = true
				}
			}
			first := "editor> abc界"
			if size[0] == 12 {
				first = "editor> abc"
			}
			firstFound := false
			for _, row := range state.Rows {
				if row == first {
					firstFound = true
				}
			}
			if !found || !firstFound {
				t.Fatalf("hand-written Unicode cells missing at %v: %#v", size, state)
			}
		}
		if size[0] == 40 && strings.Count(strings.Join(state.Rows, "\n"), "editor>") != 1 {
			t.Fatalf("duplicate editor: %#v", state)
		}
	}
	f.sendControl("inspect")
	f.draft("abc界😀Z", 0, 0, true)
	f.finish("quit", 0, 0)
}

func TestG3ResizeDuringStream(t *testing.T) {
	bin := t.TempDir() + "/stream"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	f := spawnG6(t, bin, true, "g1")
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.waitTerminal(func() bool { return strings.Contains(f.e.String(), "editor>") })
	f.input("abc界😀Z")
	f.draft("abc界😀Z", 0, 0, true)
	f.sendControl("g1-stream")
	for step, size := range [][2]int{{10, 12}, {52, 12}, {40, 12}, {40, 15}} {
		count := (step + 1) * 500
		f.waitLine(func(s string) bool { return s == fmt.Sprintf("commit %d %d false", count, count) })
		resizeFixture(f, size[0], size[1])
		x := 16
		if size[0] == 10 {
			x = 6
		}
		f.waitTerminal(func() bool {
			return f.e.CursorPosition().X == x && strings.Contains(f.e.String(), fmt.Sprintf("tail %04d", count))
		})
		if step < 3 {
			f.sendControl("g1-continue")
		}
	}
	f.mu.Lock()
	state := inspect(f.e)
	raw := f.raw.String()
	f.mu.Unlock()
	ids := commitIDPattern.FindAllString(strings.Join(append(state.History, state.Rows...), "\n"), -1)
	if len(ids) != 2000 {
		t.Fatalf("retained %d lines", len(ids))
	}
	for i, id := range ids {
		want := fmt.Sprintf("G1[%04d]", i)
		if id != want {
			t.Fatalf("line %d order/replay", i)
		}
	}
	assertReplayGenerations(t, raw, 500, 1000, 1500, 2000, 2000)
	if strings.Count(strings.Join(state.Rows, "\n"), "editor>") != 1 || state.X != 16 {
		t.Fatalf("invalid live editor %#v", state)
	}
	f.finish("quit", 0, 0)
}

func assertReplayGenerations(t *testing.T, raw string, counts ...int) {
	t.Helper()
	chunks := strings.Split(raw, "\x1b[2J\x1b[3J\x1b[H")
	if len(chunks) != len(counts) {
		t.Fatalf("repair generations %d expected%d", len(chunks)-1, len(counts)-1)
	}
	for generation, chunk := range chunks {
		if strings.Contains(chunk, "\x1b[2J") || strings.Contains(chunk, "\x1b[3J") || strings.Contains(chunk, "\x1b[?1049h") {
			t.Fatal("unscoped purge or alternate screen")
		}
		ids := commitIDPattern.FindAllString(chunk, -1)
		if len(ids) != counts[generation] {
			t.Fatalf("generation%d entries%d expected%d", generation, len(ids), counts[generation])
		}
		for i, id := range ids {
			if id != fmt.Sprintf("G1[%04d]", i) {
				t.Fatalf("generation%d missing/duplicate/reordered%d: %s", generation, i, id)
			}
		}
	}
}
