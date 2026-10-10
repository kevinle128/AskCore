package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

const traceFile = "/tmp/ask-trace.log"

func withTrace() rigOption {
	return withOptions(func(o *Options) { o.TraceFile = traceFile })
}

// footerOf returns the last row of the content: the footer.
func (r *viewRig) footerOf() string {
	rows := r.content()
	return rows[len(rows)-1]
}

func TestFirstRunGoldens(t *testing.T) {
	t.Run("no_model_80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(noModelFake()))
		requireContentGolden(t, r.h)
		rows := r.content()
		require.Equal(t, "┊ Add a provider with /providers · then choose a model with /model", rows[2])
		footer := rows[len(rows)-1]
		require.True(t, strings.HasSuffix(footer, "no model"), footer)
		require.NotContains(t, footer, "▱")
	})
	t.Run("no_model_40x14", func(t *testing.T) {
		r := startView(t, 40, 14, withFake(noModelFake()))
		requireContentGolden(t, r.h)
		rows := r.content()
		require.Equal(t, "┊ Add a provider with /providers", rows[2])
		require.True(t, strings.HasSuffix(r.footerOf(), "no model"))
	})
}

func TestNoModelSendRefusedKeepsDraft(t *testing.T) {
	r := startView(t, 80, 24, withFake(noModelFake()))
	r.typeText("explain how the leader starts")
	r.key("enter")
	r.h.waitText("✗ Not sent: no model · draft kept · Add a provider with /providers")
	requireContentGolden(t, r.h)
	require.Empty(t, r.fc.callsWith("prompt:"))
	r.requireOnScreen("❯ explain how the leader starts")
	// The refusal stays until the draft changes.
	r.typeText("!")
	r.waitGone("Not sent: no model")
	r.requireOnScreen("Add a provider with /providers · then choose a model with /model")
}

func TestNoModelRefusesSteerAndFollowUp(t *testing.T) {
	r := startView(t, 80, 24, withFake(noModelFake()))
	// The view thinks that a run is active, as after a stale event.
	r.event(&protocol.AgentStart{})
	r.h.waitText("Working…")
	r.typeText("steer this")
	r.key("enter")
	r.h.waitText("Not sent: no model")
	r.key("ctrl+u")
	r.typeText("follow this")
	r.key("alt+enter")
	r.h.waitText("Not sent: no model")
	require.Empty(t, r.fc.callsWith("prompt:"))
	require.Empty(t, r.fc.callsWith("steer:"))
	require.Empty(t, r.fc.callsWith("followup:"))
}

func TestNoModelRefusalPointsToModelWhenProviderUsable(t *testing.T) {
	fc := noModelFake()
	fc.setModels(protocol.ACPModelsResult{Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.waitCall("usage")
	r.typeText("hello")
	r.key("enter")
	r.h.waitText("✗ Not sent: no model · draft kept · Choose one with /model")
	r.h.settle()
	require.Empty(t, fc.callsWith("prompt:"))
}

func TestRailNoticePriority(t *testing.T) {
	r := startView(t, 80, 24, withFake(noModelFake()))
	s := r.stopped().sv
	status := func() string { return ansi.Strip(s.statusText(80)) }
	require.Contains(t, status(), "Add a provider with /providers")

	s.setResult("openai signed in · 2 models")
	require.Contains(t, status(), "✓ openai signed in")
	require.NotContains(t, status(), "Add a provider")

	s.dlg = &loginDialog{opt: protocol.ACPLoginOption{ProviderID: "openai", MethodID: "api-key"}, isKey: true, phase: phaseChecking, since: s.now()}
	require.Contains(t, status(), "Checking the key with openai…", "a pending check is above a result")

	s.failure = &failureView{text: s.noModelRefusal(), rev: s.editor.Revision()}
	require.Contains(t, status(), "Not sent: no model", "a refusal is above a pending check")

	s.failure, s.dlg = nil, nil
	require.Contains(t, status(), "✓ openai signed in")
	s.notice = noticeView{}
	require.Contains(t, status(), "Add a provider with /providers", "the notice comes back when a higher one ends")
}

func TestProvidersNeedsLeaderInProcess(t *testing.T) {
	fc := noModelFake()
	fc.setInfo(ConnInfo{Mode: ModeInProcess})
	r := startView(t, 80, 24, withFake(fc))
	r.waitCall("usage")
	r.typeText("/providers")
	r.key("enter")
	r.h.waitText("Sign in from a shell: ask auth login --provider <id> --method <id>")
	rows := r.content()
	require.Equal(t, "┊ Needs the leader; this session runs in process.", rows[2])
	require.Equal(t, "┊ Sign in from a shell: ask auth login --provider <id> --method <id>", rows[3])
	require.Empty(t, fc.callsWith("providers"), "nothing is sent in process")
	r.requireNotOnScreen("Type to search")
	require.True(t, strings.HasSuffix(r.footerOf(), "no model"))
}

func TestCurrentModelRemovedShowsNoModel(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: openaiGPT55, Models: singleFormatModels()[1:], UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: openaiGPT55})
	r := startView(t, 80, 24, withFake(fc))
	fc.mu.Lock()
	fc.logout = protocol.ACPLogoutResult{Removed: true, ClearedModel: openaiGPT55, Generation: 2}
	fc.mu.Unlock()
	r.openProviders()
	r.key("down", "down", "down") // ChatGPT subscription
	r.key("delete")
	r.h.waitText("Remove ChatGPT Plus/Pro sign-in?")
	// After the removal nothing is usable.
	fc.setModels(protocol.ACPModelsResult{UsableOnly: true})
	r.key("enter")
	r.h.waitText("✓ Removed the saved credential for openai.")
	r.h.waitText("The current model gpt-5.5 is no longer available. Choose one with /model.")
	r.h.waitText("┊ No model. Choose one with /model")
	require.True(t, strings.HasSuffix(r.footerOf(), "no model"))
	r.typeText("hello")
	r.key("enter")
	r.h.waitText("✗ Not sent: no model · draft kept · Choose one with /model")
	require.Empty(t, fc.callsWith("prompt:"))
}

func TestCurrentModelRemovedKeepsModelRailWhenOthersUsable(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: openaiGPT55, Models: singleFormatModels()[1:], UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: openaiGPT55})
	r := startView(t, 80, 24, withFake(fc))
	fc.mu.Lock()
	fc.logout = protocol.ACPLogoutResult{Removed: true, ClearedModel: openaiGPT55, Generation: 2}
	fc.mu.Unlock()
	r.openProviders()
	r.key("down", "down", "down", "delete")
	r.h.waitText("Remove ChatGPT Plus/Pro sign-in?")
	fc.setModels(protocol.ACPModelsResult{Models: singleFormatModels()[:1], UsableOnly: true})
	r.key("enter")
	r.h.waitText("┊ No model. Choose one with /model")
	r.typeText("hello")
	r.key("enter")
	r.h.waitText("✗ Not sent: no model · draft kept · Choose one with /model")
}

func TestRemovedModelNoticeEndsWithSession(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: openaiGPT55, Models: singleFormatModels()[1:], UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: openaiGPT55})
	r := startView(t, 80, 24, withFake(fc))
	s := r.stopped().sv
	s.onModelUpdate(SessionEventMsg{}, &protocol.ModelUpdate{})
	s.models = nil
	require.Equal(t, "No model. Choose one with /model", s.noModelNotice(false))
	require.Contains(t, s.noModelRefusal(), "Choose one with /model")
	s.useSession(SessionInfo{ID: "another-session"})
	require.Equal(t, "Add a provider with /providers", s.noModelNotice(true))
}

func TestTraceWarningOnceAtStart(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()), withTrace())
	requireContentGolden(t, r.h)
	rows := r.content()
	require.Equal(t, "  model  alibaba-token-plan/deepseek-v4.1-flash", rows[1])
	require.Equal(t, "  TEA_TRACE is on: typed and pasted text, keys included, is written to", rows[2])
	require.Equal(t, "  /tmp/ask-trace.log.", rows[3])
	require.Equal(t, 1, r.h.count("TEA_TRACE is on"))
	r.h.resize(60, 20)
	r.h.settle()
	require.Equal(t, 1, r.h.count("TEA_TRACE is on"), "a rebuild draws the rows once")
	r.requireOnScreen("/tmp/ask-trace.log.")
}

func TestTraceWarningOnceAtStartWithNoModel(t *testing.T) {
	r := startView(t, 80, 24, withFake(noModelFake()), withTrace())
	rows := r.content()
	require.True(t, strings.HasPrefix(rows[0], "◢◤ ask"))
	require.Equal(t, "  TEA_TRACE is on: typed and pasted text, keys included, is written to", rows[1])
	require.Equal(t, "  /tmp/ask-trace.log.", rows[2])
}

func TestTraceWarningOnKeyBox(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()), withTrace())
	r.openProviders()
	r.key("down", "down", "down", "down", "enter") // OpenAI API key
	r.h.waitText("OpenAI API key")
	r.h.waitText("TEA_TRACE is on")
	requireContentGolden(t, r.h)
	rows := r.content()
	idx := -1
	for i, row := range rows {
		if strings.Contains(row, "TEA_TRACE is on") {
			idx = i
		}
	}
	require.Positive(t, idx)
	require.Contains(t, rows[idx+1], "/tmp/ask-trace.log.")
	require.Contains(t, rows[idx+3], "key  paste or type the key; it stays hidden", "the warning is above the input")
}

func TestTraceWarningOnCodeInput(t *testing.T) {
	fc := providerFake()
	fc.loginStart = protocol.ACPLoginStartResult{LoginID: "l1", Kind: protocol.ACPLoginKindBrowserAddress, URL: fakeURL}
	fc.loginWaitGate = make(chan struct{})
	r := startView(t, 80, 24, withFake(fc), withTrace())
	r.openProviders()
	r.key("down", "enter") // Claude Pro/Max sign-in
	r.h.waitText("address")
	r.h.waitText("TEA_TRACE is on")

	fc.mu.Lock()
	fc.loginStart = protocol.ACPLoginStartResult{LoginID: "l2", Kind: protocol.ACPLoginKindCopyCode, URL: fakeURL}
	fc.mu.Unlock()
	r.key("esc")
	r.openProviders()
	r.key("down", "enter")
	r.h.waitText("The callback port is busy")
	r.h.waitText("TEA_TRACE is on")
	r.requireOnScreen("code  ")
	close(fc.loginWaitGate)
}

func TestNoTraceWarningWhenUnset(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.requireNotOnScreen("TEA_TRACE")
	r.openProviders()
	r.key("down", "down", "down", "down", "enter")
	r.h.waitText("OpenAI API key")
	r.requireNotOnScreen("TEA_TRACE")
	requireContentGolden(t, r.h)
}

func TestFallbackNoticeNamesModelInUse(t *testing.T) {
	fallback := &protocol.ACPModelFallback{SavedDefault: openaiGPT55, Reason: protocol.ACPFallbackNoCredential}
	build := func() *fakeClient {
		fc := providerFake()
		fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: deepseekChat, ModelFallback: fallback})
		return fc
	}
	t.Run("80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(build()))
		requireContentGolden(t, r.h)
		rows := r.content()
		require.Equal(t, "  model  alibaba-token-plan/deepseek-v4.1-flash", rows[1])
		require.Equal(t, "         Default model openai/gpt-5.5 is not usable (no credential).", rows[2])
	})
	t.Run("40x14", func(t *testing.T) {
		r := startView(t, 40, 14, withFake(build()))
		requireContentGolden(t, r.h)
		rows := r.content()
		require.Equal(t, "  alibaba-token-plan/deepseek-v4.1-flash", rows[1])
		require.Equal(t, "  Default model openai/gpt-5.5 is not", rows[2])
		require.Equal(t, "  usable (no credential).", rows[3])
	})
}

func TestModelSelectShowsDefaultSavedStatus(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("gpt-5.5")
	r.key("enter")
	r.waitCall("set_model:" + openaiGPT55)
	r.h.waitText("┊ ✓ Default model: openai/gpt-5.5")
	fc.mu.Lock()
	require.True(t, fc.savedDefault[openaiGPT55], "the choice is sent with saveDefault")
	fc.mu.Unlock()
	// The status leaves after 3 s.
	r.clock.advance(4 * time.Second)
	r.h.send(tickMsg{})
	r.waitGone("Default model: openai/gpt-5.5")
}

func TestNoDefaultStatusWithoutDefaultSaved(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	m := r.stopped().sv
	m.models = singleFormatModels()
	m.sel = newModelSelectorFor(protocol.ACPModelsResult{Models: m.models, UsableOnly: true}, m.modelID)
	m.modelReq = 1
	m.gen = 1
	m.sel.gen = 1
	m.onSetModel(setModelMsg{gen: 1, req: 1, id: openaiGPT55, res: protocol.ACPSetModelResult{ModelID: openaiGPT55}})
	require.NotContains(t, m.notice.text, "Default model")
	require.False(t, m.notice.ok)
}

func TestModelSelectorTagsDefault(t *testing.T) {
	t.Run("default differs from current", func(t *testing.T) {
		fc := providerFake()
		fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true, Default: openaiGPT55})
		r := startView(t, 80, 24, withFake(fc))
		r.key("ctrl+l")
		r.h.waitText("Select model")
		requireContentGolden(t, r.h)
		rows := r.content()
		var cur, def string
		for _, row := range rows {
			switch {
			case strings.Contains(row, "deepseek-v4.1-flash") && strings.Contains(row, "128k"):
				cur = row
			case strings.Contains(row, "gpt-5.5 ") && strings.Contains(row, "1M"):
				def = row
			}
		}
		require.Contains(t, cur, "current")
		require.Contains(t, def, "default")
		require.NotContains(t, def, "current")
	})
	t.Run("same model shows one tag", func(t *testing.T) {
		fc := providerFake()
		fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true, Default: deepseekChat})
		r := startView(t, 80, 24, withFake(fc))
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.h.settle()
		require.Equal(t, 1, r.h.count("current"))
		require.Zero(t, r.h.count("default"))
	})
}

func TestProvidersSignInDoesNotChangeDefault(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
	r := startView(t, 80, 24, withFake(fc))
	r.openProviders()
	r.key("down", "down", "down", "down", "enter")
	r.h.waitText("OpenAI API key")
	r.paste(fakeKey)
	r.key("enter")
	r.h.waitText("✓ openai: API key saved.")
	r.requireNotOnScreen("Default model")
	require.Empty(t, fc.callsWith("set_model:"), "a sign-in does not choose a model")
	require.Contains(t, r.content()[1], "alibaba-token-plan/deepseek-v4.1-flash")
}

func TestUncheckedKeyRefusedAtFirstPrompt(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.typeText("explain how the leader starts")
	r.key("enter")
	r.waitCall("prompt:explain how the leader starts")
	r.fc.push(PromptAcceptedMsg{ID: 1})
	body := "HTTP 401: {\"error\":\"bad key\"}"
	m := protocol.AssistantMessage{Provider: "alibaba-token-plan", Model: "deepseek-v4.1-flash", StopReason: protocol.StopError, ErrorMessage: &body}
	r.event(flat(
		&protocol.AgentStart{},
		userAt("explain how the leader starts", sketchTime(14, 5)),
		&protocol.MessageStart{Message: protocol.AssistantMessage{Provider: "alibaba-token-plan", Model: "deepseek-v4.1-flash"}},
		&protocol.MessageEnd{Message: m},
		&protocol.CycleEnd{Reason: "error", Code: "AUTH"},
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
	r.h.waitText("✗ Error (auth): alibaba-token-plan refused the key (HTTP 401).")
	r.h.waitText("Replace the key with /providers. Then send again.")
	requireContentGolden(t, r.h)
}
