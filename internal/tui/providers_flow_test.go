package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestSignalCancelsOpenLogin(t *testing.T) {
	fc := browserFake()
	r := startView(t, 80, 24, withFake(fc))
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	r.h.send(StopMsg{})
	res := r.exited()
	require.NoError(t, res.err)
	require.Equal(t, []string{"login_cancel:login-1"}, fc.callsWith("login_cancel:"), "one cancel for the open login")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestSignalWithoutLoginSendsNoCancel(t *testing.T) {
	fc := providerFake()
	r := startView(t, 80, 24, withFake(fc))
	r.openProviders()
	r.h.send(StopMsg{})
	r.exited()
	require.Empty(t, fc.callsWith("login_cancel:"))
}

func TestProvidersChangedUpdatesOpenModelBox(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels()[:1], UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.typeText("deep")
	r.h.waitText("⌕ deep")
	r.requireNotOnScreen("gpt-5.5")
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	fc.push(ProvidersChangedMsg{Generation: 4, Provider: "openai", Change: protocol.ACPProviderChangeSaved})
	r.h.waitText("Providers changed in the leader: openai credential saved · 2 models")
	r.requireOnScreen("⌕ deep", "● alibaba-token-plan")
	r.requireNotOnScreen("gpt-5.5 ")
	// The filter stays, so the new rows show once it is cleared.
	r.key("ctrl+u")
	r.h.waitText("gpt-5.5")
}

func TestSessionEventForOtherSessionAddsNoModelLine(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	before := r.screen()
	r.fc.push(SessionEventMsg{SessionID: "sess_other", Event: &protocol.ModelUpdate{Provider: "openai", ID: "gpt-5.5", API: "responses"}})
	r.h.settle()
	require.Equal(t, before, r.screen())
}

func TestPickedModelOfTwoFormatsShowsFormatInChangeLine(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: tokenPlanFlash, Models: designModels(true), UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: tokenPlanFlash})
	r := startView(t, 140, 24, withFake(fc))
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.key("down")
	r.key("enter")
	r.waitCall("set_model:" + tokenPlanChat)
	r.h.waitText("model  alibaba-token-plan/deepseek-v4.1-flash (openai-completions) · was alibaba-token-plan/deepseek-v4.1-flash (anthropic-messages)")
}

func TestOwnGenerationResetsWithNewLeader(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	s := r.stopped().sv
	s.ownGen = 9
	s.onClient(ConnectedMsg{Info: s.info})
	require.Equal(t, uint64(9), s.ownGen, "the same leader keeps its generation")
	other := s.info
	other.InstanceID = "0f0f0f0f0f0f0f0f"
	s.onClient(ConnectedMsg{Info: other})
	require.Zero(t, s.ownGen, "a new leader counts from zero again")
}
