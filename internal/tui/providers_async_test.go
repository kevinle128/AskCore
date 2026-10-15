package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestProviderListFromClosedBoxDoesNotReplaceReopenedBox(t *testing.T) {
	fc := providerFake()
	r := startView(t, 80, 24, withFake(fc))
	s := r.stopped().sv
	old := s.openProviders()().(provListedMsg)
	s.closeProviders()
	fc.providers.Options = designOptions()[:2]
	next := s.openProviders()().(provListedMsg)
	s.onProvListed(next)
	s.onProvListed(old)
	require.Len(t, s.pbox.items, 2, "the answer for the closed box must be ignored")
}

func TestProviderChangeDuringListLoadRejectsOldGeneration(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	s := r.stopped().sv
	old := s.openProviders()().(provListedMsg)
	s.onProvidersChanged(ProvidersChangedMsg{Generation: 2, Provider: "openai", Change: protocol.ACPProviderChangeRemoved})
	s.onProvListed(old)
	require.True(t, s.pbox.loading)
	require.Empty(t, s.pbox.items, "the list was read before the removal")
}

func TestOpenProviderBoxAcceptsRestartedLeaderGeneration(t *testing.T) {
	fc := providerFake()
	fc.providers.Generation = 9
	r := startView(t, 80, 24, withFake(fc))
	s := r.stopped().sv
	s.onProvListed(s.openProviders()().(provListedMsg))
	s.onClient(DisconnectedMsg{Err: errNoLeader})
	info := s.info
	info.InstanceID = "new-leader"
	s.onClient(ConnectedMsg{Info: info})
	fc.providers.Generation = 1
	fc.providers.Options = designOptions()[:2]
	s.onProvListed(s.listProvidersCmd()().(provListedMsg))
	require.Len(t, s.pbox.items, 2)
	require.Equal(t, uint64(1), s.pbox.gen)
}

func TestModelListsKeepNewestReply(t *testing.T) {
	fc := providerFake()
	r := startView(t, 80, 24, withFake(fc))
	s := r.stopped().sv
	old := s.fetchModelsCmd(nil)().(modelsFetchedMsg)
	fc.setModels(protocol.ACPModelsResult{Models: singleFormatModels(), UsableOnly: true})
	next := s.fetchModelsCmd(nil)().(modelsFetchedMsg)
	s.onModelsFetched(next)
	s.onModelsFetched(old)
	require.Equal(t, singleFormatModels(), s.models, "an older reply must not restore the old model list")
}

func TestModelListReadBeforeProviderChangeIsIgnored(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	s := r.stopped().sv
	old := s.fetchModelsCmd(nil)().(modelsFetchedMsg)
	s.onProvidersChanged(ProvidersChangedMsg{Generation: 2, Provider: "openai", Change: protocol.ACPProviderChangeRemoved})
	s.models = nil
	s.onModelsFetched(old)
	require.Empty(t, s.models, "the reply was read before the known provider change")
}

func TestNewerModelListKeepsSavedKeyNote(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSignedIn, Generation: 2}
	r := startView(t, 80, 24, withFake(fc))
	s := r.stopped().sv
	s.startFlow(provItem{opt: designOptions()[4]})
	s.onBoxPaste(tea.PasteMsg{Content: fakeKey})
	reply := s.sendSecret()().(keySavedMsg)
	old := s.onKeySaved(reply)().(modelsFetchedMsg)
	fc.setModels(protocol.ACPModelsResult{Models: singleFormatModels(), UsableOnly: true})
	next := s.fetchModelsCmd(nil)().(modelsFetchedMsg)
	s.onModelsFetched(next)
	s.onModelsFetched(old)
	require.Equal(t, "openai key saved · 2 models", s.notice.text)
	require.Equal(t, singleFormatModels(), s.models)
}

func TestPendingModelRefreshKeepsLoginModelCount(t *testing.T) {
	fc := browserFake()
	fc.setModels(protocol.ACPModelsResult{Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	s := r.stopped().sv
	started := s.startFlow(provItem{opt: designOptions()[3]})().(loginStartedMsg)
	s.onLoginStarted(started)
	out := protocol.ACPLoginOutcome{LoginID: started.res.LoginID, Status: protocol.ACPLoginStatusSignedIn, Account: "dev@example.com", Generation: 2}
	old := s.onLoginEnded(loginEndedMsg{loginID: out.LoginID, out: out})().(modelsFetchedMsg)
	s.fetchModelsCmd(nil)
	s.onModelsFetched(old)
	require.Equal(t, "openai signed in · 2 models", s.notice.text)
}

func TestModelListReplyFromLostLinkIsIgnored(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	s := r.stopped().sv
	called := false
	old := s.fetchModelsCmd(func(*protocol.ACPModelsResult) tea.Cmd {
		called = true
		return nil
	})().(modelsFetchedMsg)
	s.onClient(DisconnectedMsg{Err: errNoLeader})
	s.models = nil
	s.onClient(ConnectedMsg{Info: s.info})
	s.onModelsFetched(old)
	require.Empty(t, s.models)
	require.False(t, called, "a reply from the lost link must not add a note")
}

func TestLoginReplyAfterLostLinkKeepsStoppedBox(t *testing.T) {
	for _, isKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "browser", true: "key"}[isKey], func(t *testing.T) {
			r := startView(t, 80, 24, withFake(providerFake()))
			s := r.stopped().sv
			d := &loginDialog{id: 2, req: 1, loginID: "pending", isKey: isKey, phase: phaseChecking,
				opt: protocol.ACPLoginOption{ProviderID: "openai"}}
			s.dlg = d
			s.onClient(DisconnectedMsg{Err: errNoLeader})
			s.onClient(ConnectedMsg{Info: s.info})
			out := protocol.ACPLoginOutcome{LoginID: "pending", Status: protocol.ACPLoginStatusSignedIn, Generation: 2}
			if isKey {
				s.onKeySaved(keySavedMsg{dlg: 2, req: 1, loginID: "pending", out: out})
			} else {
				s.onLoginEnded(loginEndedMsg{loginID: "pending", out: out})
			}
			require.Same(t, d, s.dlg)
			require.Equal(t, phaseLost, d.phase)
		})
	}
}
