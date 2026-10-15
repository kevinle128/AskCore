package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/creack/pty"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestResizeSupersedesInFlightRepair(t *testing.T) {
	m := model{width: 40, height: 12, resizeGeneration: 1, repairPending: true, repairSettled: true, repairInFlight: true}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 13, Height: 6, Generation: 2})
	next, cmd := updated.(model).Update(resizeSettled{2})
	if cmd == nil || !next.(model).repairInFlight {
		t.Fatal("stale repair prevented current generation from scheduling")
	}
}
func TestReplayWaitsForAcknowledgedPrefix(t *testing.T) {
	m := model{width: 13, height: 6, resizeGeneration: 2, repairPending: true, repairSettled: true}
	m.transcript.start(3, false)
	m.transcript.paused = true
	m.transcript.scheduled = 1
	m.transcript.pending = true
	if m.repairCommand() != nil {
		t.Fatal("repair passed pending physical write")
	}
	updated, cmd := m.Update(commitWrite{IDs: []string{"G1[0000]"}, N: 1, Requested: 1})
	m = updated.(model)
	if cmd == nil || m.transcript.confirmed != 1 || m.transcript.scheduled != 1 || m.transcript.pending {
		t.Fatal("snapshot does not isolate acknowledged prefix")
	}
	updated, cmd = m.Update(tea.InlineReplayResult{Generation: 2, Entries: 2})
	m = updated.(model)
	if cmd == nil || m.transcript.confirmed != 1 || m.transcript.scheduled != 2 {
		t.Fatal("replay changed frontier or did not resume next commit")
	}
}
func TestReplayFailureStopsContent(t *testing.T) {
	m := model{resizeGeneration: 2, repairPending: true, repairInFlight: true}
	updated, cmd := m.Update(tea.InlineReplayResult{Generation: 2, Err: errReplayFixture{}})
	if !updated.(model).transcript.stopped || cmd == nil {
		t.Fatal("failed replay did not stop output")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("failed replay did not quit")
	}
}

type errReplayFixture struct{}

func (errReplayFixture) Error() string { return "fixture replay failure" }

func TestReplayPartialWriteRestoresTerminal(t *testing.T) {
	bin := t.TempDir() + "/replay-fault"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	f := spawnG6(t, bin, true)
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	f.sendControl("replay-fail-partial")
	f.waitLine(func(s string) bool { return s == "replay-armed" })
	if err := pty.Setsize(f.master, &pty.Winsize{Cols: 13, Rows: 6}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.e.Resize(13, 6)
	f.mu.Unlock()
	if err := f.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	report := f.waitLine(func(s string) bool { return strings.HasPrefix(s, "writer ") })
	fields := strings.Fields(report)
	if len(fields) != 8 || fields[1] != "true" {
		t.Fatalf("failed owner: %s", report)
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n != 8 {
		t.Fatalf("partial prefix: %s", report)
	}
	f.finish("", 0, 1)
	f.mu.Lock()
	raw := f.raw.String()
	f.mu.Unlock()
	if strings.Count(raw, "\x1b[2J\x1b[3J\x1b[H") != 0 || strings.Contains(raw, "G1[") {
		t.Fatal("failed generation replayed or acknowledged content")
	}
	if strings.Count(raw, "\x1b[?2026h") < 1 || strings.LastIndex(raw, "\x1b[?2026l") < strings.LastIndex(raw, "\x1b[?2026h") {
		t.Fatal("partial replay sync cleanup absent")
	}
}
