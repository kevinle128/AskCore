package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

var testFlags = protocol.ACPSessionOptions{AppendSystemPrompt: []string{"be terse"}, Tools: []string{"read"}}

func startWithFlags(t *testing.T, fc *fakeClient) *viewRig {
	t.Helper()
	return startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) {
		o.Session.Flags = testFlags
	}))
}

func TestSessionFlagsAreSentOnEveryNewSession(t *testing.T) {
	r := startWithFlags(t, newFakeClient())
	r.typeText("/new")
	r.key("enter")
	r.h.waitFor("second session", func(*screenHarness) bool { return r.fc.count("new_session") == 2 })
	require.Equal(t, []NewSessionOptions{{Flags: testFlags}, {Flags: testFlags}}, r.fc.sentOptions())
	r.requireNotOnScreen("ignored the prompt and tool flags")
}

func TestSessionFlagsAreSentWithTheLazyRetry(t *testing.T) {
	fc := newFakeClient()
	fc.newSessionErrs = []error{errors.New("the agent is busy")}
	r := startTrustView(t, 80, 24, fc, acmeRoot, func(o *Options) { o.Session.Flags = testFlags })
	r.h.waitText("Could not start a session")
	r.typeText("hello")
	r.key("enter")
	r.waitCall("prompt:hello")
	require.Equal(t, []NewSessionOptions{{Flags: testFlags}, {Flags: testFlags}}, r.fc.sentOptions())
}

func TestSessionFlagsWarningWhenHostDoesNotAcknowledge(t *testing.T) {
	fc := newFakeClient()
	fc.dropFlags = true
	r := startWithFlags(t, fc)
	r.h.waitText("ignored the prompt and tool flags")
}

func TestNoFlagsWarningWithoutFlags(t *testing.T) {
	fc := newFakeClient()
	fc.dropFlags = true
	r := startView(t, 80, 24, withFake(fc))
	require.Equal(t, 1, r.fc.count("new_session"))
	r.requireNotOnScreen("ignored the prompt and tool flags")
	require.Equal(t, []NewSessionOptions{{}}, r.fc.sentOptions())
}

// The stored session of the command line is loaded by the first start only.
// /new and the retry after a failed start make new sessions.
func TestLoadIsSentWithTheFirstStartOnly(t *testing.T) {
	fc := newFakeClient()
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) {
		o.Session.Load = "1863000000000000001"
		o.Session.Flags = testFlags
	}))
	r.typeText("/new")
	r.key("enter")
	r.h.waitFor("second session", func(*screenHarness) bool { return r.fc.count("new_session") == 2 })
	require.Equal(t, []NewSessionOptions{
		{Flags: testFlags, Load: "1863000000000000001"},
		{Flags: testFlags},
	}, r.fc.sentOptions())
}

// A refused load ends the program with the reason. A new session would not be
// the one that the command line asked for.
func TestRefusedLoadEndsTheProgramWithTheReason(t *testing.T) {
	fc := newFakeClient()
	fc.newSessionErrs = []error{errors.New("session 42 is in use by another process")}
	m := New(Options{Client: fc, Profile: colorprofile.NoTTY, TickInterval: -1, Session: NewSessionOptions{Load: "42"}})
	// The program ends before a frame may show, so the screen harness, which
	// waits for the first frame, does not fit here.
	prog := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(&syncBuffer{}), tea.WithWindowSize(80, 24),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := prog.Run(); done <- err }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		prog.Kill()
		t.Fatal("the program did not end after a refused load")
	}
	require.EqualError(t, m.StartError(), "session 42 is in use by another process")
	require.Equal(t, []NewSessionOptions{{Load: "42"}}, fc.sentOptions())
	require.Empty(t, m.ExitNote(), "a failed start is not a quit")
}
