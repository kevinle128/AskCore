package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestCompletionListsProviders(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.typeText("/p")
	r.h.waitText("/providers  Add or remove providers")
	r.requireNotOnScreen("/model ", "/quit")
}

func TestProvidersCommandCaseInsensitive(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.typeText("/PROVIDERS")
	r.h.waitText("/providers")
	r.key("enter")
	r.h.waitText("Type to search")
	r.requireOnScreen("╭─ Providers", "Alibaba Token Plan API key", "your draft is kept")
}

func TestProviderBoxGoldens(t *testing.T) {
	t.Run("list_80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(providerFake()))
		r.firstTurn()
		r.openProviders()
		r.key("down", "down", "down", "down", "down")
		requireContentGolden(t, r.h)
		rows := r.content()
		var box []string
		for _, row := range rows {
			if strings.HasPrefix(row, "│") {
				box = append(box, row)
			}
		}
		require.Len(t, box, 8, "seven rows and the search row")
		require.Contains(t, box[0], "Alibaba Token Plan API key")
		require.True(t, strings.HasSuffix(box[0], "api key │"), box[0])
		require.True(t, strings.HasSuffix(box[2], "env ANTHROPIC_API_KEY │"), box[2])
		require.True(t, strings.HasSuffix(box[3], "signed in │"), box[3])
		require.Contains(t, box[7], "⌕ Type to search", "the search row is the last row")
		r.key("esc")
		r.waitGone("Type to search")
		r.requireOnScreen("❯ Ask anything")
	})
	t.Run("narrow_40x14", func(t *testing.T) {
		r := startView(t, 40, 14, withFake(providerFake()))
		r.openProviders()
		r.key("down", "down", "down")
		requireContentGolden(t, r.h)
		rows := r.content()
		var box []string
		for _, row := range rows {
			if strings.HasPrefix(row, "│") {
				box = append(box, row)
			}
		}
		require.Equal(t, []string{
			"│   ● Claude Pro/Max sign-in           │",
			"│   ● Anthropic… env ANTHROPIC_API_KEY │",
			"│ ❯ ● ChatGPT Plus/Pro sign… signed in │",
			"│   … 3 more                           │",
			"│  ⌕ Type to search                    │",
		}, box)
	})
}

func TestProviderBoxSearchFilters(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.openProviders()
	r.typeText("key")
	r.h.waitText("⌕ key")
	rows := r.content()
	var names []string
	for _, row := range rows {
		if strings.HasPrefix(row, "│") && strings.Contains(row, "●") {
			names = append(names, row)
		}
	}
	require.Len(t, names, 4)
	for _, n := range names {
		require.Contains(t, n, "API key")
	}
	require.Contains(t, names[0], "❯", "the first match is selected")
	r.key("ctrl+u")
	r.typeText("zzzz")
	r.h.waitText("No match")
	// A paste goes to the search text with line breaks removed.
	r.key("ctrl+u")
	r.paste("xai\n")
	r.h.waitText("⌕ xai")
}

func TestProviderRowMenu(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.openProviders()
	r.key("down", "down", "down", "enter") // the ChatGPT sign-in row is signed in
	r.h.waitText("Sign in again")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Signed in. What do you want to do?", "Remove         Delete the saved sign-in for openai", "enter choose  ·  esc back  ·  your draft is kept")
	r.key("esc")
	r.h.waitText("Type to search")
	r.requireNotOnScreen("Sign in again")
}

func TestProviderEnterStartsFlowAtOnce(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.openProviders()
	r.key("down", "down", "down", "down", "enter") // OpenAI API key
	r.h.waitText("Paste your API key.")
	r.requireNotOnScreen("Sign in again", "Type to search")
	require.Empty(t, r.fc.callsWith("login_start"), "a key row needs no start call")
}

func TestProviderBoxHintDropsFromRight(t *testing.T) {
	// A saved row shows manage and remove.
	r := startView(t, 80, 24, withFake(providerFake()))
	r.openProviders()
	r.key("down", "down", "down")
	r.h.waitText("enter manage  ·  delete remove  ·  esc close  ·  your draft is kept")

	pb := &providerBox{}
	pb.setItems(designOptions())
	pb.sel = 3
	for _, c := range []struct {
		width int
		want  string
	}{
		{80, "enter manage  ·  delete remove  ·  esc close  ·  your draft is kept"},
		{60, "enter manage  ·  delete remove  ·  esc close"},
		{40, "enter manage · esc close"},
		{20, "enter manage · esc close"},
	} {
		require.Equal(t, c.want, pb.hint(c.width), "width %d", c.width)
	}
	pb.sel = 4
	require.Equal(t, "enter sign in  ·  esc close  ·  your draft is kept", pb.hint(80))
	require.Equal(t, "enter sign in · esc close", pb.hint(40))
}

func TestRemoveConfirmFlow(t *testing.T) {
	fc := providerFake()
	fc.mu.Lock()
	fc.logout = protocol.ACPLogoutResult{Removed: true, Generation: 2}
	fc.mu.Unlock()
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openProviders()
	r.key("down", "down", "down", "delete")
	r.h.waitText("Remove ChatGPT Plus/Pro sign-in?")
	requireContentGolden(t, r.h)
	r.requireOnScreen("This deletes the saved sign-in for openai from this leader.", "Environment variables are not changed.", "enter remove  ·  esc keep it")
	// esc keeps the credential and sends nothing.
	r.key("esc")
	r.waitGone("Remove ChatGPT")
	require.Empty(t, fc.callsWith("logout:"))
	// Again, with enter.
	r.openProviders()
	r.key("down", "down", "down", "delete", "enter")
	r.waitCall("logout:openai/openai-chatgpt")
	r.h.waitText("✓ Removed the saved credential for openai.")
	r.waitGone("Remove ChatGPT")
}

func TestRemoveConfirmNamesActiveRun(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.event(&protocol.AgentStart{})
	r.openProviders()
	r.key("down", "down", "down", "delete")
	r.h.waitText("A run is active and may use this provider.")
}

func TestRemoveNoteEnvStillSet(t *testing.T) {
	fc := providerFake()
	fc.mu.Lock()
	fc.logout = protocol.ACPLogoutResult{Removed: true, EnvVar: "ANTHROPIC_API_KEY", Generation: 2}
	fc.mu.Unlock()
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openProviders()
	r.key("down", "down", "down")
	// Make the ChatGPT row the anthropic one: use the menu on a saved row.
	r.key("esc")
	r.waitGone("Type to search")
	fc.mu.Lock()
	fc.providers.Options[2].State = protocol.ACPLoginStateAPIKey
	fc.mu.Unlock()
	r.openProviders()
	r.key("down", "down", "delete", "enter")
	r.waitCall("logout:anthropic/api-key")
	r.h.waitText("ANTHROPIC_API_KEY is still set, so anthropic stays usable.")
	requireContentGolden(t, r.h)
	r.requireOnScreen("✓ Removed the saved credential for anthropic.")
}

func TestRemoveEnvOnlyRowSendsNothing(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.firstTurn()
	r.openProviders()
	r.key("down", "down", "delete")
	r.h.waitText("Nothing to remove: anthropic uses ANTHROPIC_API_KEY from the environment.")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Unset the variable in your shell to stop using it.", "Type to search")
	require.Empty(t, r.fc.callsWith("logout:"))
}

func TestProvidersChangedUpdatesOpenBox(t *testing.T) {
	fc := providerFake()
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openProviders()
	r.key("down", "down", "down", "down", "down", "down") // xAI API key
	r.typeText("xai")
	r.h.waitText("⌕ xai")
	// Another client signs in to xai.
	fc.mu.Lock()
	fc.providers = protocol.ACPProvidersResult{Generation: 3, Options: designOptions()}
	fc.providers.Options[5].State = protocol.ACPLoginStateSignedIn
	fc.models = protocol.ACPModelsResult{Current: deepseekChat, UsableOnly: true, Models: append(singleFormatModels()[:1],
		protocol.ACPModel{ModelID: "xai/grok-5-code@chat", Provider: "xai", ID: "grok-5-code", API: "chat", ContextWindow: 256_000})}
	fc.mu.Unlock()
	fc.push(ProvidersChangedMsg{Generation: 3, Provider: "xai", Change: protocol.ACPProviderChangeSaved})
	r.h.waitText("Providers changed in the leader: xai credential saved · 1 model")
	rows := r.content()
	var grok string
	for _, row := range rows {
		if strings.Contains(row, "xAI Grok sign-in") {
			grok = row
		}
	}
	require.Contains(t, grok, "signed in")
	require.Contains(t, r.screen(), "⌕ xai", "the filter stays")
	// No note goes to the scrollback of this client.
	require.Zero(t, r.h.count("✓ xai"))
}

func TestOwnChangeShowsNoChangedRail(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 5}
	fc.keyGate = make(chan struct{})
	r := startView(t, 80, 24, withFake(fc))
	r.openProviders()
	r.key("down", "down", "down", "down", "enter")
	r.paste(fakeKey)
	r.key("enter")
	r.h.waitText("Checking the key with openai…")
	// The broadcast of this change arrives before the answer.
	fc.push(ProvidersChangedMsg{Generation: 5, Provider: "openai", Change: protocol.ACPProviderChangeSaved})
	r.h.settle()
	close(fc.keyGate)
	r.h.waitText("✓ openai: API key saved.")
	r.h.settle()
	require.Zero(t, r.h.count("Providers changed in the leader"))
	require.Equal(t, 1, r.h.count("✓ openai: API key saved."), "no second note")
	// A newer change of the same provider that arrives during a check is not
	// this client's own, so it shows once the check is over.
	fc2 := providerFake()
	fc2.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 5}
	fc2.keyGate = make(chan struct{})
	r2 := startView(t, 80, 24, withFake(fc2))
	r2.openProviders()
	r2.key("down", "down", "down", "down", "enter")
	r2.paste(fakeKey)
	r2.key("enter")
	r2.h.waitText("Checking the key with openai…")
	fc2.push(ProvidersChangedMsg{Generation: 5, Provider: "openai", Change: protocol.ACPProviderChangeSaved},
		ProvidersChangedMsg{Generation: 6, Provider: "openai", Change: protocol.ACPProviderChangeRemoved})
	r2.h.settle()
	require.Zero(t, r2.h.count("Providers changed in the leader"), "nothing shows while the check runs")
	close(fc2.keyGate)
	r2.h.waitText("Providers changed in the leader: openai removed")
	// A change of another client with a newer generation shows.
	fc.push(ProvidersChangedMsg{Generation: 6, Provider: "xai", Change: protocol.ACPProviderChangeRemoved})
	r.h.waitText("Providers changed in the leader: xai removed")
}

func TestProvidersOpenDuringRunModelWaits(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.event(&protocol.AgentStart{})
	r.h.waitText("Working…")
	r.key("ctrl+l")
	r.h.waitText("Model can change when the run ends.")
	r.requireNotOnScreen("Select model")
	r.typeText("/providers")
	r.key("enter")
	r.h.waitText("Type to search")
	r.requireOnScreen("Alibaba Token Plan API key")
}

func TestProvidersConnectionLostKeepsList(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.openProviders()
	r.fc.push(DisconnectedMsg{Err: errNoLeader})
	r.h.waitText("Disconnected from the leader")
	r.requireOnScreen("Alibaba Token Plan API key", "OpenAI API key")
	r.key("down", "down", "down", "down", "enter")
	r.h.waitText("Reconnecting, please wait…")
	r.requireNotOnScreen("Paste your API key.")
	require.Empty(t, r.fc.callsWith("login_start"))
	// After the reconnect the list loads again.
	before := len(r.fc.callsWith("providers"))
	r.fc.push(ConnectedMsg{Info: r.fc.Info()})
	r.waitCallCount("providers", before+1)
}

func (r *viewRig) waitCallCount(prefix string, n int) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(r.fc.callsWith(prefix)) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.t.Fatalf("fewer than %d calls %q: %v", n, prefix, r.fc.callList())
}

func TestStaleProviderResultsIgnored(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.openProviders()
	r.key("down", "down", "down", "down", "enter") // OpenAI API key
	r.h.waitText("Paste your API key.")
	s := r.stopped().sv
	d := s.dlg
	require.NotNil(t, d)
	d.loginID, d.phase, d.req = "login-now", phaseChecking, 3
	blocks := s.m.transcript.Len()
	// An old request number, an old login id and a result without a box.
	s.onKeySaved(keySavedMsg{dlg: d.id, req: 2, loginID: "login-now", out: protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved}})
	s.onKeySaved(keySavedMsg{dlg: d.id, req: 3, loginID: "login-old", out: protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved}})
	s.onLoginEnded(loginEndedMsg{loginID: "login-old", out: protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSignedIn}})
	require.Same(t, d, s.dlg, "the box is unchanged")
	require.Equal(t, phaseChecking, d.phase)
	require.Equal(t, blocks, s.m.transcript.Len(), "no note was added")
	// A list answer with an old request number or an old generation changes nothing.
	s.pbox = &providerBox{gen: 5, req: 2}
	s.pbox.setItems(designOptions()[:2])
	s.onProvListed(provListedMsg{gen: s.gen, req: 1, res: protocol.ACPProvidersResult{Generation: 9, Options: designOptions()}})
	require.Len(t, s.pbox.items, 2)
	s.onProvListed(provListedMsg{gen: s.gen, req: 2, res: protocol.ACPProvidersResult{Generation: 4, Options: designOptions()}})
	require.Len(t, s.pbox.items, 2, "an older generation is dropped")
	s.onProvListed(provListedMsg{gen: s.gen, req: 2, res: protocol.ACPProvidersResult{Generation: 6, Options: designOptions()}})
	require.Len(t, s.pbox.items, 7)
}

func TestSignInCountComesFromRefreshedList(t *testing.T) {
	t.Run("count of the new list", func(t *testing.T) {
		fc := providerFake()
		fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
		r := startView(t, 80, 24, withFake(fc))
		r.openProviders()
		r.key("down", "down", "down", "down", "enter")
		// The list that the leader returns after the save holds two models of openai.
		fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
		r.paste(fakeKey)
		r.key("enter")
		r.h.waitText("✓ openai: API key saved. 2 models ready.")
		r.h.waitText("┊ ✓ openai key saved · 2 models")
	})
	t.Run("list not refreshed", func(t *testing.T) {
		fc := providerFake()
		fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
		fc.modelsErrAfter = 1
		r := startView(t, 80, 24, withFake(fc))
		r.openProviders()
		r.key("down", "down", "down", "down", "enter")
		r.paste(fakeKey)
		r.key("enter")
		r.h.waitText("┊ ✓ openai key saved · model list not refreshed")
	})
}

func TestCtrlDIgnoredWhileBoxOpen(t *testing.T) {
	r := startView(t, 80, 24, withFake(providerFake()))
	r.openProviders()
	r.key("ctrl+d")
	r.h.settle()
	r.requireOnScreen("Type to search")
	r.key("down", "down", "down", "down", "enter")
	r.h.waitText("Paste your API key.")
	r.key("ctrl+d")
	r.h.settle()
	r.requireOnScreen("Paste your API key.")
	select {
	case res := <-r.h.done:
		r.h.done <- res
		t.Fatal("the program quit")
	default:
	}
}
