package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// chooseModelWithSave picks openai/gpt-5.5 in the model selector, which sends
// set_model with saveDefault, and waits until the footer row shows the model.
func chooseModelWithSave(t *testing.T, fc *fakeClient) *viewRig {
	t.Helper()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("gpt-5.5")
	r.key("enter")
	r.waitCall("set_model:" + openaiGPT55)
	r.waitGone("Select model")
	r.h.waitFor("the new model in the footer row", func(*screenHarness) bool {
		return strings.Contains(r.footerOf(), "openai/gpt-5.5")
	})
	fc.mu.Lock()
	require.True(t, fc.savedDefault[openaiGPT55], "the choice is sent with saveDefault")
	fc.mu.Unlock()
	return r
}

// When the leader refuses to save the default, the status rail gives the
// reason as an error. The model of the session still changes.
func TestModelSelectShowsDefaultNotSavedReason(t *testing.T) {
	cases := []struct {
		reason protocol.ACPDefaultNotSaved
		row    string
	}{
		{protocol.ACPDefaultShadowed, "┊ Not saved as the default: a project setting overrides it."},
		{protocol.ACPDefaultUnsafeFile, "┊ Not saved as the default: the settings file is not safe."},
		{protocol.ACPDefaultFailed, "┊ Not saved as the default: the write failed."},
		// A reason that a newer leader adds still tells that the save did not happen.
		{"some_new_reason", "┊ Not saved as the default."},
	}
	for _, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			fc := providerFake()
			fc.defaultNotSaved = tc.reason
			r := chooseModelWithSave(t, fc)
			r.h.waitText(tc.row)
			r.requireNotOnScreen("Default model:", "✓")
			// The reason leaves after the notice time, as other notices do.
			r.clock.advance(4 * time.Second)
			r.h.send(tickMsg{})
			r.waitGone("Not saved as the default")
		})
	}
}

// A refused save is an error notice, not a success notice.
func TestDefaultNotSavedIsErrorNotice(t *testing.T) {
	fc := providerFake()
	fc.defaultNotSaved = protocol.ACPDefaultShadowed
	r := chooseModelWithSave(t, fc)
	r.h.waitText("Not saved as the default")
	m := r.stopped().sv
	require.True(t, m.notice.err)
	require.False(t, m.notice.ok)
	require.Equal(t, openaiGPT55, m.modelID, "the session model changes")
}

// A saved default keeps the success row and shows no refusal.
func TestModelSelectSavedDefaultHasNoReason(t *testing.T) {
	r := chooseModelWithSave(t, providerFake())
	r.h.waitText("┊ ✓ Default model: openai/gpt-5.5")
	r.requireNotOnScreen("Not saved as the default")
}
