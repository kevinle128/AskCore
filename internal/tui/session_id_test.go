package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

func TestSessionIDIsTheCurrentSessionAfterTheProgramEnds(t *testing.T) {
	r := startView(t, 80, 24)
	r.key("ctrl+d")
	require.Equal(t, testSessionID, r.finalModel().SessionID())
}

func TestSessionIDIsTheCurrentSessionAfterAStopSignal(t *testing.T) {
	r := startView(t, 80, 24)
	r.h.send(StopMsg{})
	require.Equal(t, testSessionID, r.finalModel().SessionID())
}

func TestSessionIDIsEmptyWhenTheStartFailed(t *testing.T) {
	m := New(Options{Profile: colorprofile.NoTTY, TickInterval: -1, Open: func(context.Context) (Client, error) {
		return nil, errors.New("boom")
	}})
	prog := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(&syncBuffer{}), tea.WithWindowSize(80, 24),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := prog.Run(); done <- err }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		prog.Kill()
		t.Fatal("the program did not end after a failed open")
	}
	require.Empty(t, m.SessionID())
}

func TestSessionIDIsEmptyOnAModelWithoutAView(t *testing.T) {
	require.Empty(t, (&Model{}).SessionID())
}
