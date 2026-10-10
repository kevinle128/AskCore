package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestModelSelectorLabelsSameModelOnTwoAPIs(t *testing.T) {
	models := []protocol.ACPModel{
		{ModelID: "p/m@anthropic-messages", Provider: "p", ID: "m", API: "anthropic-messages"},
		{ModelID: "p/m@openai-completions", Provider: "p", ID: "m", API: "openai-completions"},
		{ModelID: "q/n@x", Provider: "q", ID: "n", API: "x"},
	}
	sl := newModelSelector(models, "q/n@x")
	require.Len(t, sl.items, 3)
	require.NotEqual(t, sl.items[0].label, sl.items[1].label, "two rows must not look the same")
	require.Contains(t, sl.items[0].label, "anthropic-messages")
	require.Contains(t, sl.items[1].label, "openai-completions")
	require.Equal(t, "q/n", sl.items[2].label, "a unique model keeps the short label")
}

func TestModelSelectorSkipsRepeatedModelID(t *testing.T) {
	row := protocol.ACPModel{ModelID: "p/m@a", Provider: "p", ID: "m", API: "a"}
	sl := newModelSelector([]protocol.ACPModel{row, row}, "p/m@a")
	require.Len(t, sl.items, 1, "the same model id is listed once")
	require.Equal(t, "p/m", sl.items[0].label)
}

// The history of a watched session can arrive after the attach answer, with a
// run already in progress. The status must still show that run.
func TestWatchedSessionShowsRunThatStartedBeforeAttach(t *testing.T) {
	fc := newFakeClient()
	fc.live = []protocol.ACPLiveSession{
		{SessionID: testSessionID, Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleDriver, HasDriver: true},
		{SessionID: "sess_8d07aa11bb22cc33", Cwd: "/home/u/src/askcore", Role: protocol.ACPRoleObserver, HasDriver: true},
	}
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	fc.mu.Lock()
	fc.state.Running = true
	fc.mu.Unlock()
	r.typeText("/sessions")
	r.key("enter")
	r.h.waitText("Live sessions")
	r.key("down", "enter")
	r.waitCall("attach:sess_8d07aa11bb22cc33")
	r.h.waitText("◉ observer 8d07")
	r.h.waitText("Working…") // the state read after the attach
	fc.push(SnapshotMsg{SessionID: "sess_8d07aa11bb22cc33"})
	r.h.settle()
	r.h.waitText("── session 8d07 ──")
	r.h.settle()
	require.Positive(t, r.h.count("Working…"), "the snapshot must not hide the run")
	require.Positive(t, r.h.count("another client drives this session"))
}
