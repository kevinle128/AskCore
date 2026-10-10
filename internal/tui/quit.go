package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Quit flow.
//
// A user request to quit ends the program after the live region is cleared.
// The note that tells what happened to the run is not part of the live region:
// the entry prints it after the terminal is restored, so that it stays above the
// shell prompt. A stop from outside (a signal) clears the region and has no note.

// quitConfirmFor is how long a first ctrl+c on an empty draft waits for the
// second press.
const quitConfirmFor = 2 * time.Second

// StopMsg ends the program at once, as a signal does. The live region is
// cleared and no note is kept. It is the one way to stop the program from
// outside the update loop other than killing it.
type StopMsg struct{}

// quitNote is the text of the note after a user quit.
type quitNote struct{ text, next string }

// confirmQuitText is the notice of the first ctrl+c.
func (s *sessionView) confirmQuitText() string {
	switch {
	case s.info.Mode == ModeInProcess:
		return "Press ctrl+c again to quit. The run stops."
	case s.run.running:
		return "Press ctrl+c again to quit · the run continues in the leader"
	}
	return "Press ctrl+c again to quit"
}

// askQuitConfirmation starts the confirmation window. A press inside it quits.
func (s *sessionView) askQuitConfirmation() {
	s.quitUntil = s.now().Add(quitConfirmFor)
	s.setNoticeFor(s.confirmQuitText(), quitConfirmFor, false)
}

// quitConfirmed tells whether the confirmation window is open.
func (s *sessionView) quitConfirmed() bool {
	return !s.quitUntil.IsZero() && s.now().Before(s.quitUntil)
}

// requestQuit ends the program on a user request. It is the one place that
// does so. The leader keeps the run after the client closes; the in-process
// agent stops with its client, and the note says which one happened.
func (s *sessionView) requestQuit() tea.Cmd {
	s.quitNote = s.buildQuitNote()
	s.m.quitting = true
	return tea.Quit
}

func (s *sessionView) buildQuitNote() *quitNote {
	switch {
	case s.opening:
		// No client opened yet, so nothing was attached and nothing runs.
		return nil
	case s.info.Mode == ModeInProcess:
		return &quitNote{text: "Run stopped. Session ended."}
	}
	var b strings.Builder
	switch {
	case s.sess.ID != "":
		b.WriteString("Detached from session " + ShortID(s.sess.ID) + ".")
	default:
		b.WriteString("Detached from leader " + ShortID(s.info.InstanceID) + ".")
	}
	if s.run.running {
		b.WriteString(" The run continues in leader " + ShortID(s.info.InstanceID) + ".")
	}
	n := &quitNote{text: b.String()}
	if s.sess.ID != "" {
		n.next = "Run ask in this directory to see it again."
	}
	return n
}

// ExitNote returns the rows to print below the last screen after the program
// ended on a user request, or nil when it ended for another reason. Call it after
// Run returned.
func (m *Model) ExitNote() []string {
	if m.sv == nil || m.sv.quitNote == nil {
		return nil
	}
	n := m.sv.quitNote
	rows := NewDetachNote(n.text, n.next).Lines(m.renderContext())
	// The block ends with a spacer row; the shell prompt follows the note
	// directly.
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}
