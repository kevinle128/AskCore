package tui

import (
	"errors"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
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
