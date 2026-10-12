package tui

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// keyFlow drives a key through the key box of the OpenAI row.
type keyFlow struct {
	r  *viewRig
	fc *fakeClient
}

func newKeyFlow(t *testing.T, fc *fakeClient, ro ...rigOption) *keyFlow {
	t.Helper()
	r := startView(t, 80, 24, append([]rigOption{withFake(fc)}, ro...)...)
	return &keyFlow{r: r, fc: fc}
}

func (k *keyFlow) open() {
	k.r.openProviders()
	k.r.key("down", "down", "down", "down", "enter")
	k.r.h.waitText("Paste your API key.")
}

// requireNoSecret fails when the secret is anywhere in the output: the screen,
// the scrollback or any byte that the renderer wrote.
func (k *keyFlow) requireNoSecret(secrets ...string) {
	k.r.t.Helper()
	text := strings.Join(k.r.h.text(), "\n")
	raw := k.r.h.out.all()
	for _, secret := range secrets {
		require.NotContains(k.r.t, text, secret)
		require.NotContains(k.r.t, raw, secret)
		require.NotContains(k.r.t, raw, base64.StdEncoding.EncodeToString([]byte(secret)))
	}
}

func TestKeyBoxPasteShowsHidden(t *testing.T) {
	k := newKeyFlow(t, providerFake())
	k.open()
	k.r.paste(fakeKey)
	k.r.h.waitText("key  (hidden)")
	k.r.requireNotOnScreen(fakeKey, "sk-test")
	k.requireNoSecret(fakeKey)
	// Typed text is hidden the same way.
	k.r.key("ctrl+u")
	k.r.typeText(fakeKey)
	k.r.h.waitText("key  (hidden)")
	k.requireNoSecret(fakeKey)
}

func TestKeyBoxCheckingLocksInput(t *testing.T) {
	fc := providerFake()
	fc.keyGate = make(chan struct{})
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
	k := newKeyFlow(t, fc)
	k.r.firstTurn()
	k.open()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("┊ ◌ Checking the key with openai… 0s · esc to cancel")
	requireContentGolden(t, k.r.h)
	k.r.requireOnScreen("key  (hidden)", "esc cancel")
	k.r.requireNotOnScreen("enter save", "ctrl+u clear")
	// Typing, paste and enter are ignored while the check runs.
	k.r.typeText("more")
	k.r.paste("another-key-value")
	k.r.key("enter", "ctrl+u")
	k.r.h.settle()
	k.r.requireOnScreen("key  (hidden)")
	require.Equal(t, 1, len(fc.callsWith("api_key:")), "the key is sent once")
	k.requireNoSecret(fakeKey, "another-key-value", "more")
	close(fc.keyGate)
	k.r.h.waitText("✓ openai: API key saved.")
}

func TestKeyBoxRefusedClearsInput(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{
		Status: protocol.ACPLoginStatusRefused, Reason: protocol.ACPLoginReasonInvalidKey,
		HTTPStatus: 401, ProviderCode: "invalid_api_key",
	}
	k := newKeyFlow(t, fc)
	k.r.firstTurn()
	k.open()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("✗ openai refused the key: invalid_api_key (HTTP 401). Nothing was saved.")
	requireContentGolden(t, k.r.h)
	k.r.requireOnScreen("key  paste a new key", "The key is cleared from this screen. Paste a new key to try again.", "enter save  ·  esc cancel")
	k.r.requireNotOnScreen("(hidden)")
	k.requireNoSecret(fakeKey)
	// A new key goes through the same path.
	fc.mu.Lock()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
	fc.mu.Unlock()
	k.r.paste("sk-test-second-fake-key")
	k.r.key("enter")
	k.r.h.waitText("✓ openai: API key saved.")
	k.requireNoSecret(fakeKey, "sk-test-second-fake-key")
}

func TestKeyBoxSavedNoteAndRail(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	k := newKeyFlow(t, fc)
	k.r.firstTurn()
	k.open()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("✓ openai: API key saved. 2 models ready.")
	k.r.h.waitText("┊ ✓ openai key saved · 2 models")
	requireContentGolden(t, k.r.h)
	k.r.waitGone("Paste your API key.")
	k.r.requireOnScreen("❯ Ask anything")
	// A model is current, so the note has no second row.
	k.r.requireNotOnScreen("Choose a model with /model.")
	k.requireNoSecret(fakeKey)
}

func TestKeySavedOnFirstRunPointsToModel(t *testing.T) {
	fc := noModelFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
	k := newKeyFlow(t, fc)
	k.r.waitCall("usage")
	k.open()
	fc.setModels(protocol.ACPModelsResult{Models: singleFormatModels()[1:], UsableOnly: true})
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("✓ openai: API key saved. 2 models ready.")
	k.r.h.waitText("Choose a model with /model.")
	k.r.h.waitText("┊ ✓ openai key saved · 2 models · choose one with /model")
	requireContentGolden(t, k.r.h)
	require.True(t, strings.HasSuffix(k.r.headerOf(), "no model"), "the footer keeps no model")
}

func TestKeyBoxIgnoresHistoryKeepsDraft(t *testing.T) {
	fc := providerFake()
	r := startView(t, 80, 24, withFake(fc))
	r.typeText("earlier prompt text")
	r.h.waitText("❯ earlier prompt text")
	r.key("enter")
	r.waitCall("prompt:earlier prompt text")
	fc.push(PromptAcceptedMsg{ID: 1})
	r.waitGone("earlier prompt text")
	r.h.waitText("Ask anything")
	r.openProviders()
	r.key("down", "down", "down", "down", "enter")
	r.h.waitText("Paste your API key.")
	r.key("up", "up", "down", "left", "right", "home", "end")
	r.h.settle()
	r.requireNotOnScreen("earlier prompt text")
	r.requireOnScreen("key  paste or type the key; it stays hidden")
	r.key("esc")
	r.waitGone("Paste your API key.")
	// The editor is as it was before the box: empty, and history is not walked.
	r.requireNotOnScreen("earlier prompt text")
	r.requireOnScreen("❯ Ask anything")
}

func TestSecretNeverOnScreenGrid(t *testing.T) {
	fc := providerFake()
	fc.keyGate = make(chan struct{})
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusRefused, HTTPStatus: 401, ProviderCode: "invalid_api_key"}
	k := newKeyFlow(t, fc)
	k.open()
	k.requireNoSecret(fakeKey)
	k.r.paste(fakeKey)
	k.requireNoSecret(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("Checking the key with openai…")
	k.requireNoSecret(fakeKey)
	k.r.clock.advance(5_000_000_000)
	k.r.h.send(tickMsg{})
	k.r.h.settle()
	k.requireNoSecret(fakeKey)
	close(fc.keyGate)
	k.r.h.waitText("refused the key")
	k.requireNoSecret(fakeKey)
	fc.mu.Lock()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
	fc.mu.Unlock()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("✓ openai: API key saved.")
	k.requireNoSecret(fakeKey)
	// The screen was resized while a key was in the buffer in another run; here
	// the screen is drawn again with the note.
	k.r.h.resize(60, 20)
	k.r.h.settle()
	k.requireNoSecret(fakeKey)
}

// stoppedFlow runs a refusal and a save and returns the stopped model.
func stoppedFlow(t *testing.T) *sessionView {
	t.Helper()
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusRefused, HTTPStatus: 401, ProviderCode: "invalid_api_key"}
	k := newKeyFlow(t, fc)
	k.r.firstTurn()
	k.open()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("refused the key")
	fc.mu.Lock()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
	fc.mu.Unlock()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("✓ openai: API key saved.")
	k.r.h.settle()
	return k.r.stopped().sv
}

func TestSecretNotInTranscriptBlocks(t *testing.T) {
	s := stoppedFlow(t)
	rc := s.m.renderContext()
	var all []string
	all = append(all, finalRows(s.m.transcript, rc)...)
	all = append(all, liveRows(s.m.transcript, rc)...)
	all = append(all, s.notice.text, s.notice.text)
	if s.failure != nil {
		all = append(all, s.failure.text)
	}
	joined := strings.Join(all, "\n")
	require.NotContains(t, joined, fakeKey)
	require.NotContains(t, joined, "sk-test")
	for _, rec := range s.closed {
		require.NotContains(t, rec.loginID, "sk-test")
	}
}

func TestSecretNotInPromptHistory(t *testing.T) {
	s := stoppedFlow(t)
	for _, e := range s.editor.history.entries {
		require.NotContains(t, e, fakeKey)
	}
	require.Empty(t, s.editor.history.entries, "the box never writes to the prompt history")
	// up in the editor recalls nothing.
	_, ok := s.editor.history.older("")
	require.False(t, ok)
}

func TestSecretNotInUndoOrKillRing(t *testing.T) {
	s := stoppedFlow(t)
	for _, st := range s.editor.undo.steps {
		require.NotContains(t, st.text, fakeKey)
		require.NotContains(t, st.text, "sk-test")
	}
	for _, e := range s.editor.kill.entries {
		require.NotContains(t, e, fakeKey)
	}
	require.NotContains(t, s.editor.Text(), "sk-test")
	// Every undo step is part of the command that opened the box.
	for _, st := range s.editor.undo.steps {
		require.True(t, strings.HasPrefix("/providers", st.text) || st.text == "", "undo step %q", st.text)
	}
}

func TestSecretBufferClearedOnEveryExit(t *testing.T) {
	t.Run("zeroed", func(t *testing.T) {
		var in secretInput
		in.paste("abcdef")
		buf := in.buf
		in.clear()
		require.Nil(t, in.buf)
		require.Equal(t, []byte{0, 0, 0, 0, 0, 0}, buf, "the old bytes are overwritten")
		in.typed("xyz")
		buf = in.buf
		_ = in.take()
		require.Equal(t, []byte{0, 0, 0}, buf, "take drops the buffer")
	})
	exits := []struct {
		name string
		exit func(t *testing.T, r *viewRig, fc *fakeClient)
	}{
		{"esc", func(t *testing.T, r *viewRig, fc *fakeClient) { r.key("esc"); r.waitGone("Paste your API key.") }},
		{"ctrl+c", func(t *testing.T, r *viewRig, fc *fakeClient) { r.key("ctrl+c"); r.waitGone("Paste your API key.") }},
		{"ctrl+u", func(t *testing.T, r *viewRig, fc *fakeClient) { r.key("ctrl+u"); r.waitGone("(hidden)") }},
	}
	for _, e := range exits {
		t.Run(e.name, func(t *testing.T) {
			fc := providerFake()
			k := newKeyFlow(t, fc)
			k.open()
			k.r.paste(fakeKey)
			k.r.h.waitText("key  (hidden)")
			e.exit(t, k.r, fc)
			// A second open of the box starts empty.
			if e.name != "ctrl+u" {
				k.open()
			}
			k.r.requireOnScreen("key  paste or type the key; it stays hidden")
			k.r.requireNotOnScreen("(hidden)")
			require.Empty(t, fc.callsWith("api_key:"), "nothing was sent")
		})
	}
	t.Run("send and refusal", func(t *testing.T) {
		fc := providerFake()
		fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusRefused, HTTPStatus: 401}
		k := newKeyFlow(t, fc)
		k.open()
		k.r.paste(fakeKey)
		k.r.key("enter")
		k.r.h.waitText("refused the key")
		k.r.requireNotOnScreen("(hidden)")
		s := k.r.stopped().sv
		require.True(t, s.dlg.secret.empty(), "the buffer is empty after the send")
	})
	t.Run("signal", func(t *testing.T) {
		fc := providerFake()
		k := newKeyFlow(t, fc)
		k.open()
		k.r.paste(fakeKey)
		k.r.h.waitText("key  (hidden)")
		k.r.h.send(StopMsg{})
		res := k.r.exited()
		require.NoError(t, res.err)
		s := res.model.(*Model).sv
		if s.dlg != nil {
			require.True(t, s.dlg.secret.empty())
		}
	})
}

func TestKeyBufferSurvivesResizeAndIsSentOnce(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusRefused, HTTPStatus: 401}
	k := newKeyFlow(t, fc)
	k.open()
	k.r.h.resize(120, 40)
	k.r.paste(fakeKey)
	k.r.h.resize(80, 24)
	k.r.h.settle()
	k.r.requireOnScreen("key  (hidden)")
	k.r.key("enter")
	k.r.h.waitText("refused the key")
	fc.mu.Lock()
	sent := fc.sentKey
	fc.mu.Unlock()
	require.Equal(t, fakeKey, sent)
	require.Len(t, fc.callsWith("api_key:"), 1)
	k.requireNoSecret(fakeKey)
}

func TestSecretFlowKeepsDraftAndUndo(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSignedIn, Generation: 2}
	r := startView(t, 80, 24, withFake(fc))
	s := r.stopped().sv
	draft := "also check the lock timeout"
	prior := "check the lock timeout"
	s.editor.text, s.editor.cursor = draft, len(draft)
	s.editor.undo.steps = []snapshot{{text: prior, cursor: len(prior)}}
	s.editor.kill.entries = []string{"prior editor text"}
	s.openProviders()
	s.startFlow(provItem{opt: designOptions()[4]})
	s.onBoxPaste(tea.PasteMsg{Content: fakeKey})
	d := s.dlg
	buf := d.secret.buf
	reply := s.sendSecret()().(keySavedMsg)
	require.Equal(t, make([]byte, len(buf)), buf, "sending must zero the former buffer")
	s.onKeySaved(reply)
	require.Nil(t, s.dlg)
	require.Equal(t, draft, s.editor.Text())
	s.onKey(keyNamed(t, "ctrl+-"))
	require.Equal(t, prior, s.editor.Text())
	require.Equal(t, []string{"prior editor text"}, s.editor.kill.entries)
	for _, step := range s.editor.undo.steps {
		require.NotContains(t, step.text, fakeKey)
	}
}

func TestSecretExitOverwritesFormerBuffer(t *testing.T) {
	for _, exit := range []string{"close", "save", "refuse", "disconnect"} {
		t.Run(exit, func(t *testing.T) {
			r := startView(t, 80, 24, withFake(providerFake()))
			s := r.stopped().sv
			s.startFlow(provItem{opt: designOptions()[4]})
			s.onBoxPaste(tea.PasteMsg{Content: fakeKey})
			d := s.dlg
			buf := d.secret.buf
			switch exit {
			case "close":
				s.dialogEsc()
			case "save":
				s.onOutcome(d, protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSignedIn, Generation: 2})
			case "refuse":
				s.onOutcome(d, protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusRefused, HTTPStatus: 401})
			case "disconnect":
				d.phase, d.loginID = phaseChecking, "checking"
				s.onClient(DisconnectedMsg{Err: errNoLeader})
			}
			require.Equal(t, make([]byte, len(buf)), buf)
			require.Empty(t, d.secret.buf)
		})
	}
}

func TestSecretPasteRefusedWithoutEcho(t *testing.T) {
	k := newKeyFlow(t, providerFake())
	k.open()
	k.r.paste("part-one part-two-secret")
	k.r.h.waitText("The text has a line break or space inside. Paste it again.")
	k.r.requireNotOnScreen("part-one", "part-two-secret", "(hidden)")
	k.requireNoSecret("part-one", "part-two-secret")
	k.r.paste("first-line-secret\nsecond-line-secret")
	k.r.h.waitText("The text has a line break or space inside. Paste it again.")
	k.requireNoSecret("first-line-secret", "second-line-secret")
	// A paste above the limit.
	k.r.paste(strings.Repeat("k", secretLimit+1))
	k.r.h.waitText("That is too long to be a key.")
	k.r.requireNotOnScreen("kkkkkkkk", "(hidden)")
	// A trailing line break is removed and does not send by itself.
	k.r.paste(fakeKey + "\r\n")
	k.r.h.waitText("key  (hidden)")
	require.Empty(t, k.fc.callsWith("api_key:"))
	k.requireNoSecret(fakeKey)
}

var oscClipboard = regexp.MustCompile(`\x1b\]52;[a-z]*;([A-Za-z0-9+/=]*)(?:\x07|\x1b\\)`)

func TestClipboardCopiesOnlyURLOrCode(t *testing.T) {
	copied := func(raw string) []string {
		var out []string
		for _, m := range oscClipboard.FindAllStringSubmatch(raw, -1) {
			b, err := base64.StdEncoding.DecodeString(m[1])
			require.NoError(t, err)
			out = append(out, string(b))
		}
		return out
	}
	t.Run("key box", func(t *testing.T) {
		k := newKeyFlow(t, providerFake())
		k.open()
		k.r.paste(fakeKey)
		k.r.key("ctrl+y")
		k.r.h.settle()
		require.Empty(t, copied(k.r.h.out.all()), "ctrl+y copies nothing in the key box")
		k.requireNoSecret(fakeKey)
	})
	t.Run("browser box", func(t *testing.T) {
		fc := browserFake()
		r := startView(t, 80, 24, withFake(fc))
		r.openChatGPT()
		r.h.waitText("This box closes by itself when you approve.")
		r.key("ctrl+y")
		r.h.waitText("Copied")
		r.h.settle()
		require.Equal(t, []string{fakeURL}, copied(r.h.out.all()))
		fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
	})
	t.Run("device box", func(t *testing.T) {
		fc := deviceFake()
		r := startView(t, 80, 24, withFake(fc))
		r.openProviders()
		r.key("down", "down", "down", "down", "down", "enter")
		r.h.waitText("2. Enter this code:")
		r.key("ctrl+y")
		r.h.waitText("Copied")
		r.h.settle()
		require.Equal(t, []string{"WDJB-MJHT"}, copied(r.h.out.all()))
		fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
	})
	t.Run("address box copies the URL and never the address", func(t *testing.T) {
		fc := providerFake()
		fc.loginStart = protocol.ACPLoginStartResult{LoginID: "login-a", Kind: protocol.ACPLoginKindBrowserAddress, URL: fakeURL}
		fc.loginWaitGate = make(chan struct{})
		r := startView(t, 80, 24, withFake(fc))
		r.openProviders()
		r.key("down", "enter")
		r.h.waitText("address  ")
		r.paste(fakeAddress)
		r.key("ctrl+y")
		r.h.waitText("Copied")
		r.h.settle()
		require.Equal(t, []string{fakeURL}, copied(r.h.out.all()))
		raw := r.h.out.all()
		require.NotContains(t, raw, fakeAddress)
		fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
	})
}

func TestNoTitleOrBellInOutput(t *testing.T) {
	k := newKeyFlow(t, providerFake())
	k.open()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.settle()
	raw := k.r.h.out.all()
	require.NotContains(t, raw, "\x1b]0;")
	require.NotContains(t, raw, "\x1b]1;")
	require.NotContains(t, raw, "\x1b]2;")
	stripped := regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`).ReplaceAllString(raw, "")
	require.NotContains(t, stripped, "\x07", "no bell")
}
