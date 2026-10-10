package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// browserFake is a leader whose ChatGPT row starts a browser sign-in and holds
// the end of it until the test lets it go.
func browserFake() *fakeClient {
	fc := providerFake()
	fc.loginStart = protocol.ACPLoginStartResult{LoginID: "login-1", Kind: protocol.ACPLoginKindBrowser, URL: fakeURL}
	fc.loginWaitGate = make(chan struct{})
	return fc
}

// openChatGPT opens the provider box and starts the sign-in of the ChatGPT row
// through its row menu.
func (r *viewRig) openChatGPT() {
	r.t.Helper()
	r.openProviders()
	r.key("down", "down", "down", "enter")
	r.h.waitText("Sign in again")
	r.key("enter")
}

// releaseLogin ends the held wait with an outcome.
func (f *fakeClient) releaseLogin(out protocol.ACPLoginOutcome) {
	f.mu.Lock()
	f.loginOutcome = out
	gate := f.loginWaitGate
	f.loginWaitGate = nil
	f.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}

func TestBrowserBoxGoldens(t *testing.T) {
	t.Run("waiting_80x24", func(t *testing.T) {
		fc := browserFake()
		r := startView(t, 80, 24, withFake(fc))
		r.firstTurn()
		r.openChatGPT()
		r.h.waitText("This box closes by itself when you approve.")
		r.clock.advance(12 * time.Second)
		r.h.send(tickMsg{})
		r.h.waitText("┊ ◌ Waiting for the browser… 12s · esc to cancel")
		requireContentGolden(t, r.h)
		r.requireOnScreen("Open this URL in a browser and sign in to openai:", "ctrl+y copy URL  ·  esc cancel")
		fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
	})
	t.Run("narrow_40x14", func(t *testing.T) {
		fc := browserFake()
		r := startView(t, 40, 14, withFake(fc))
		r.openChatGPT()
		r.h.waitText("ctrl+y copies the full URL")
		r.clock.advance(12 * time.Second)
		r.h.send(tickMsg{})
		r.h.waitText("┊ ◌ Waiting… 12s")
		requireContentGolden(t, r.h)
		rows := r.content()
		var urlRows []string
		for _, row := range rows {
			if strings.HasPrefix(row, "│  ") && !strings.Contains(row, "Open this URL") && !strings.Contains(row, "ctrl+y") {
				urlRows = append(urlRows, strings.TrimSpace(strings.Trim(row, "│")))
			}
		}
		require.Len(t, urlRows, 4, "four URL rows")
		require.Equal(t, fakeURL[:35], urlRows[0])
		r.requireOnScreen("… ctrl+y copies the full URL", "ctrl+y copy URL · esc cancel")
		fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
	})
}

func TestURLRowsAreOneHyperlink(t *testing.T) {
	fc := browserFake()
	r := startView(t, 80, 24, withFake(fc),
		withOptions(func(o *Options) { o.Profile = colorprofile.TrueColor }),
		withProgram(tea.WithColorProfile(colorprofile.TrueColor)))
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	r.h.settle()
	raw := r.h.out.all()
	link := "\x1b]8;;" + fakeURL
	// The URL is 215 columns wide and each row holds 75, so three rows carry it.
	require.GreaterOrEqual(t, strings.Count(raw, link), 3, "each URL row carries the full URL as its link target")
	rows, capped := urlRows(fakeURL, 75, 20)
	require.False(t, capped)
	require.Equal(t, fakeURL, strings.Join(rows, ""), "no character is lost between rows")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestLinkIsOnlyForPlainURLs(t *testing.T) {
	require.True(t, linkable(fakeURL))
	require.False(t, linkable("https://x.test/\x1b]8;;"), "a control character is never put into a link sequence")
	require.False(t, linkable("javascript:alert(1)"))
	require.False(t, linkable("https://x.test/a b"))
}

func TestLoginFailedTryAgain(t *testing.T) {
	fc := browserFake()
	fc.loginWaitGate = nil
	fc.loginOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusFailed, Reason: protocol.ACPLoginReasonAccessDenied}
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openChatGPT()
	r.h.waitText("✗ Sign-in failed: the browser returned access_denied.")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Nothing was saved. You can try again.", "enter try again  ·  esc close")
	before := len(fc.callsWith("login_start:"))
	r.key("enter")
	r.waitCallCount("login_start:", before+1)
}

func TestLoginFailedWrongAddressUsesPlainFailure(t *testing.T) {
	fc := providerFake()
	fc.loginStart = protocol.ACPLoginStartResult{LoginID: "login-2", Kind: protocol.ACPLoginKindBrowserAddress, URL: fakeURL}
	fc.loginWaitGate = make(chan struct{})
	r := startView(t, 80, 24, withFake(fc))
	r.openProviders()
	r.key("down", "enter")
	r.h.waitText("address  ")
	r.paste(fakeAddress)
	r.key("enter")
	r.h.waitText("Checking the address with anthropic…")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusFailed, Reason: protocol.ACPLoginReasonInternal})
	r.h.waitText("✗ Sign-in failed.")
	r.requireNotOnScreen("the browser returned")
	r.requireNotOnScreen(fakeAddress, "fake-code-7f3a91c0")
}

func TestLoginCannotStartBusy(t *testing.T) {
	fc := browserFake()
	fc.loginStartErr = &RequestError{Code: protocol.ACPCodeProviderBusy, Kind: string(protocol.ACPErrProviderBusy), Message: "another client is signing in to this provider"}
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openChatGPT()
	r.h.waitText("✗ Could not start: another client is signing in to openai.")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Try again in a moment.", "enter try again  ·  esc close")
}

func TestLoginCannotStartOtherReasonShowsTheReason(t *testing.T) {
	fc := browserFake()
	fc.loginStartErr = &RequestError{Code: -32603, Kind: "internal", Message: "the sign-in could not start"}
	r := startView(t, 80, 24, withFake(fc))
	r.openChatGPT()
	r.h.waitText("✗ Could not start: the sign-in could not start.")
	r.requireOnScreen("Try again in a moment.")
}

func TestLoginBoxLeaderLost(t *testing.T) {
	fc := browserFake()
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	fc.push(DisconnectedMsg{Err: errNoLeader})
	r.h.waitText("✗ The sign-in stopped because the leader is gone.")
	r.h.waitText("Disconnected from the leader")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Nothing was saved. When the leader is back, start again with /providers.", "esc close")
	// After the reconnect the box is not opened again with a sign-in.
	fc.push(ConnectedMsg{Info: fc.Info()})
	r.h.settle()
	r.requireOnScreen("✗ The sign-in stopped because the leader is gone.")
	require.Equal(t, 1, len(fc.callsWith("login_start:")), "no new request after the reconnect")
	r.key("esc")
	r.waitGone("The sign-in stopped")
	fc.releaseLogin(protocol.ACPLoginOutcome{})
}

func TestLoginCancelNoteIgnoresLateResult(t *testing.T) {
	fc := browserFake()
	fc.loginCancelled = true
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	r.key("esc")
	r.h.waitText("⊘ Sign-in to openai cancelled. Nothing was saved.")
	r.waitGone("This box closes by itself")
	r.requireOnScreen("❯ Ask anything")
	require.Equal(t, 1, len(fc.callsWith("login_cancel:login-1")))
	// A late success for that id changes nothing on screen.
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSignedIn, Account: "dev@example.com"})
	r.h.settle()
	time.Sleep(50 * time.Millisecond)
	r.requireNotOnScreen("✓ openai", "signed in as")
	require.Equal(t, 1, r.h.count("⊘ Sign-in to openai cancelled."))
}

func TestEscDuringKeyCheckNoteFollowsCancelAnswer(t *testing.T) {
	t.Run("the leader cancelled", func(t *testing.T) {
		fc := providerFake()
		fc.keyGate = make(chan struct{})
		fc.loginCancelled = true
		fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled, Reason: protocol.ACPLoginReasonCancelled}
		r := startView(t, 80, 24, withFake(fc))
		r.openProviders()
		r.key("down", "down", "down", "down", "enter")
		r.paste(fakeKey)
		r.key("enter")
		r.h.waitText("Checking the key with openai…")
		r.key("esc")
		r.h.waitText("⊘ Sign-in to openai cancelled. Nothing was saved.")
		close(fc.keyGate)
		r.h.settle()
		r.requireNotOnScreen("API key saved")
	})
	t.Run("the key was already saved", func(t *testing.T) {
		fc := providerFake()
		fc.keyGate = make(chan struct{})
		fc.loginCancelled = false
		fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, Generation: 2}
		fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
		r := startView(t, 80, 24, withFake(fc))
		r.openProviders()
		r.key("down", "down", "down", "down", "enter")
		r.paste(fakeKey)
		r.key("enter")
		r.h.waitText("Checking the key with openai…")
		r.key("esc")
		r.waitCall("login_cancel:" + fc.callsWith("api_key:")[0][len("api_key:"):len("api_key:")+20])
		close(fc.keyGate)
		r.h.waitText("✓ openai: API key saved. 2 models ready.")
		r.requireNotOnScreen("⊘ Sign-in to openai cancelled")
	})
}

func TestLoginCtrlCCancelsAndDoesNotQuit(t *testing.T) {
	fc := browserFake()
	fc.loginCancelled = true
	r := startView(t, 80, 24, withFake(fc))
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	r.key("ctrl+c")
	r.h.waitText("⊘ Sign-in to openai cancelled. Nothing was saved.")
	r.waitGone("This box closes by itself")
	// A second press is the first press of the quit confirmation, not a quit.
	r.key("ctrl+c")
	r.h.waitText("Press ctrl+c again to quit")
	select {
	case <-r.h.done:
		t.Fatal("the program quit")
	default:
	}
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestLoginSuccessNamesAccount(t *testing.T) {
	fc := browserFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSignedIn, Account: "dev@example.com", Generation: 2})
	r.h.waitText("✓ openai: signed in as dev@example.com. 2 models ready.")
	r.h.waitText("┊ ✓ openai signed in · 2 models")
	requireContentGolden(t, r.h)
	r.waitGone("This box closes by itself")
}

func TestAnthropicAddressAndCodeBoxes(t *testing.T) {
	t.Run("address", func(t *testing.T) {
		fc := providerFake()
		fc.loginStart = protocol.ACPLoginStartResult{LoginID: "login-a", Kind: protocol.ACPLoginKindBrowserAddress, URL: fakeURL}
		fc.loginWaitGate = make(chan struct{})
		r := startView(t, 80, 24, withFake(fc))
		r.firstTurn()
		r.openProviders()
		r.key("down", "enter")
		r.h.waitText("address  ")
		r.h.waitText("┊ ◌ Waiting for the browser or an address… 0s")
		requireContentGolden(t, r.h)
		r.requireOnScreen("paste the address; it stays hidden", "enter send address  ·  ctrl+y copy URL  ·  esc cancel")
		r.paste(fakeAddress)
		r.h.waitText("address  (hidden)")
		r.requireNotOnScreen(fakeAddress, "fake-code")
		r.key("enter")
		r.waitCall("login_input:login-a")
		r.h.waitText("┊ ◌ Checking the address with anthropic… 0s · esc to cancel")
		r.requireOnScreen("address  (hidden)", "esc cancel")
		r.key("x")
		r.h.settle()
		r.requireNotOnScreen(fakeAddress)
		fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
	})
	t.Run("code", func(t *testing.T) {
		fc := providerFake()
		fc.loginStart = protocol.ACPLoginStartResult{LoginID: "login-c", Kind: protocol.ACPLoginKindCopyCode, URL: fakeURL}
		fc.loginWaitGate = make(chan struct{})
		r := startView(t, 80, 24, withFake(fc))
		r.firstTurn()
		r.openProviders()
		r.key("down", "enter")
		r.h.waitText("The callback port is busy, so this sign-in uses a code instead.")
		r.h.waitText("┊ ◌ Waiting for a code… 0s")
		requireContentGolden(t, r.h)
		r.requireOnScreen("code  ", "paste the code; it stays hidden", "enter send code  ·  ctrl+y copy URL  ·  esc cancel")
		r.paste(fakeCode)
		r.h.waitText("code  (hidden)")
		r.requireNotOnScreen(fakeCode)
		r.key("enter")
		r.h.waitText("┊ ◌ Checking the code with anthropic… 0s · esc to cancel")
		fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
	})
}

func TestAddressBoxPasteShowsHidden(t *testing.T) {
	fc := providerFake()
	fc.loginStart = protocol.ACPLoginStartResult{LoginID: "login-a", Kind: protocol.ACPLoginKindBrowserAddress, URL: fakeURL}
	fc.loginWaitGate = make(chan struct{})
	r := startView(t, 80, 24, withFake(fc))
	r.openProviders()
	r.key("down", "enter")
	r.h.waitText("address  ")
	r.paste(fakeAddress)
	r.requireOnScreen("address  (hidden)")
	r.requireNotOnScreen("fake-code-7f3a91c0", "127.0.0.1:1455/callback?")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestCodeBoxPasteShowsHidden(t *testing.T) {
	fc := providerFake()
	fc.loginStart = protocol.ACPLoginStartResult{LoginID: "login-c", Kind: protocol.ACPLoginKindCopyCode, URL: fakeURL}
	fc.loginWaitGate = make(chan struct{})
	r := startView(t, 80, 24, withFake(fc))
	r.openProviders()
	r.key("down", "enter")
	r.h.waitText("code  ")
	r.paste(fakeCode)
	r.requireOnScreen("code  (hidden)")
	r.requireNotOnScreen(fakeCode)
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestLoginCancelledByRemovalShowsReason(t *testing.T) {
	fc := browserFake()
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled, Reason: protocol.ACPLoginReasonRemoved})
	r.h.waitText("✗ Sign-in cancelled: another client removed openai.")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Nothing was saved. You can sign in again.", "enter try again  ·  esc close")
	r.key("esc")
	r.h.waitText("⊘ Sign-in to openai cancelled: another client removed openai.")
	r.requireNotOnScreen("Nothing was saved. You can sign in again.")
	require.Empty(t, fc.callsWith("login_cancel:"), "the leader already ended it")
}

func deviceFake() *fakeClient {
	fc := providerFake()
	expires := time.UnixMilli(clockBaseMs).Add(8*time.Minute + 36*time.Second).UTC().Format(time.RFC3339)
	fc.loginStart = protocol.ACPLoginStartResult{
		LoginID: "login-d", Kind: protocol.ACPLoginKindDevice,
		URL: "https://auth.example.test/device", UserCode: "WDJB-MJHT", ExpiresAt: expires,
	}
	fc.loginWaitGate = make(chan struct{})
	return fc
}

func TestDeviceBoxGoldens(t *testing.T) {
	fc := deviceFake()
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openProviders()
	r.key("down", "down", "down", "down", "down", "enter") // xAI Grok sign-in
	r.h.waitText("2. Enter this code:")
	r.clock.advance(24 * time.Second)
	r.h.send(tickMsg{})
	r.h.waitText("┊ ◌ Waiting for approval… 24s · esc to cancel")
	t.Run("waiting_80x24", func(t *testing.T) { requireContentGolden(t, r.h) })
	r.requireOnScreen("WDJB-MJHT", "The code expires in 8:12.", "ctrl+y copy code  ·  esc cancel")

	// The leader ends it as expired. enter asks for a new code.
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusExpired, Reason: protocol.ACPLoginReasonExpired})
	r.h.waitText("✗ The code expired. No sign-in happened.")
	t.Run("expired_80x24", func(t *testing.T) { requireContentGolden(t, r.h) })
	r.requireOnScreen("Get a new code to try again.", "enter get a new code  ·  esc close")
	before := len(fc.callsWith("login_start:"))
	fc.mu.Lock()
	fc.loginWaitGate = make(chan struct{})
	fc.mu.Unlock()
	r.key("enter")
	r.waitCallCount("login_start:", before+1)
	r.h.waitText("2. Enter this code:")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestLoginBoxResizeKeepsLogin(t *testing.T) {
	fc := browserFake()
	r := startView(t, 120, 40, withFake(fc))
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	before := len(fc.callList())
	rowsBefore := 0
	for _, row := range r.content() {
		if strings.HasPrefix(row, "│  ") && strings.Contains(fakeURL, strings.TrimSpace(strings.Trim(row, "│ "))) && len(strings.TrimSpace(strings.Trim(row, "│ "))) > 20 {
			rowsBefore++
		}
	}
	r.h.resize(80, 24)
	r.h.settle()
	r.h.waitText("┊ ◌ Waiting for the browser…")
	rowsAfter := 0
	var joined strings.Builder
	for _, row := range r.content() {
		inner := strings.TrimSpace(strings.Trim(row, "│ "))
		if strings.HasPrefix(row, "│  ") && len(inner) > 20 && strings.Contains(fakeURL, inner) {
			rowsAfter++
			joined.WriteString(inner)
		}
	}
	require.Greater(t, rowsAfter, rowsBefore, "the URL is wrapped again for the new width")
	require.Equal(t, fakeURL, joined.String())
	after := fc.callList()
	require.Equal(t, before, len(after), "no new request goes to the leader: %v", after[before:])
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestNarrowAddressBoxKeepsInputAndCancel(t *testing.T) {
	fc := browserFake()
	fc.loginStart.Kind = protocol.ACPLoginKindBrowserAddress
	r := startView(t, 80, 24, withFake(fc))
	r.openChatGPT()
	r.h.waitText("address  ")
	r.paste(fakeAddress)
	r.h.resize(40, 14)
	r.h.settle()
	r.requireOnScreen("address  (hidden)", "esc cancel", "╰")
	r.requireNotOnScreen(fakeAddress)
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestAddressBufferSurvivesResizeAndIsSentOnce(t *testing.T) {
	fc := browserFake()
	fc.loginStart.Kind = protocol.ACPLoginKindBrowserAddress
	r := startView(t, 120, 40, withFake(fc))
	r.openChatGPT()
	r.h.waitText("address  ")
	r.paste(fakeAddress)
	r.h.resize(80, 24)
	r.h.settle()
	r.requireOnScreen("address  (hidden)", "Waiting for the browser or an address")
	r.key("enter")
	r.waitCall("login_input:login-1")
	fc.mu.Lock()
	sent := fc.sentCode
	fc.mu.Unlock()
	require.Equal(t, fakeAddress, sent)
	require.Len(t, fc.callsWith("login_input:"), 1)
	require.Len(t, fc.callsWith("login_start:"), 1)
	r.requireNotOnScreen(fakeAddress)
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestCheckingAddressIgnoresCopyKey(t *testing.T) {
	fc := browserFake()
	fc.loginStart.Kind = protocol.ACPLoginKindBrowserAddress
	r := startView(t, 80, 24, withFake(fc))
	r.openChatGPT()
	r.h.waitText("address  ")
	r.paste(fakeAddress)
	r.key("enter")
	r.h.waitText("Checking the address")
	r.key("ctrl+y")
	r.h.settle()
	r.requireNotOnScreen("Copied")
	r.requireOnScreen("Checking the address")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestNarrowAddressTraceWarningKeepsInputAndCancel(t *testing.T) {
	fc := browserFake()
	fc.loginStart.Kind = protocol.ACPLoginKindBrowserAddress
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.TraceFile = "/tmp/ask-trace.log" }))
	r.openChatGPT()
	r.h.waitText("address  ")
	r.h.resize(40, 14)
	r.h.settle()
	r.requireOnScreen("TEA_TRACE is on:", "/tmp/ask-trace.log.", "address  ", "esc cancel", "╰")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestNarrowKeyRefusalKeepsInputAndCancel(t *testing.T) {
	fc := providerFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusRefused, HTTPStatus: 401, ProviderCode: "invalid_api_key"}
	k := newKeyFlow(t, fc)
	k.open()
	k.r.paste(fakeKey)
	k.r.key("enter")
	k.r.h.waitText("refused the key")
	k.r.h.resize(40, 14)
	k.r.h.settle()
	k.r.requireOnScreen("key  paste a new key", "esc cancel", "╰")
}

func TestShortKeyBoxTitleKeepsCancel(t *testing.T) {
	k := newKeyFlow(t, providerFake())
	k.open()
	k.r.h.resize(80, 5)
	k.r.h.settle()
	k.r.requireOnScreen("Sign in to openai · esc cancel", "╰")
}

func TestLoginBoxTitleCarriesCancelWhenLow(t *testing.T) {
	fc := browserFake()
	r := startView(t, 80, 24, withFake(fc))
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	r.requireNotOnScreen("Sign in to openai · esc cancel")
	r.h.resize(80, 5)
	r.h.waitText("Sign in to openai · esc cancel")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestCopyKeyIsOnlyForURLOrCode(t *testing.T) {
	fc := browserFake()
	r := startView(t, 80, 24, withFake(fc))
	r.openChatGPT()
	r.h.waitText("This box closes by itself when you approve.")
	r.key("ctrl+y")
	r.h.waitText("┊ ✓ Copied")
	fc.releaseLogin(protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusCancelled})
}

func TestAlibabaKeyBoxSaysNotChecked(t *testing.T) {
	fc := providerFake()
	fc.providers.Options[0].State = protocol.ACPLoginStateNone
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	r.openProviders()
	r.key("enter") // Alibaba Token Plan API key
	r.h.waitText("This provider cannot be checked before use; the key is saved as it is.")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Alibaba Token Plan API key", "Paste your API key. It is sent once to the leader and is never shown.")
	r.paste(fakeKey)
	r.h.waitText("key  (hidden)")
	require.Zero(t, r.h.count("Checking"))
}

func TestKeySavedNotCheckedNote(t *testing.T) {
	fc := noModelFake()
	fc.keyOutcome = protocol.ACPLoginOutcome{Status: protocol.ACPLoginStatusSaved, NotChecked: true, Generation: 2}
	r := startView(t, 80, 24, withFake(fc))
	r.waitCall("usage")
	r.typeText("/providers")
	r.key("enter")
	r.h.waitText("Type to search")
	r.key("enter") // Alibaba Token Plan API key
	r.h.waitText("This provider cannot be checked before use")
	fc.setModels(protocol.ACPModelsResult{Models: singleFormatModels()[:1], UsableOnly: true})
	r.paste(fakeKey)
	r.key("enter")
	r.h.waitText("✓ alibaba-token-plan: Saved. This provider cannot be checked before use.")
	r.h.waitText("┊ ✓ alibaba-token-plan key saved · 1 model · choose one with /model")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Choose a model with /model.")
	r.requireNotOnScreen("Checking")
	// The row now reads api key.
	fc.mu.Lock()
	fc.providers = protocol.ACPProvidersResult{Generation: 2, Options: designOptions()}
	fc.mu.Unlock()
	r.openProviders()
	rows := r.content()
	var row string
	for _, line := range rows {
		if strings.Contains(line, "Alibaba Token Plan API key") {
			row = line
		}
	}
	require.True(t, strings.HasSuffix(row, "api key │"), row)
}

func TestModelSelectorGroupsUsableProviders(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: tokenPlanFlash, Models: designModels(true), UsableOnly: true})
	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: tokenPlanFlash})
	r := startView(t, 80, 24, withFake(fc))
	r.event(firstTurnEvents()...)
	r.h.waitText("2%")
	r.key("ctrl+l")
	r.h.waitText("Select model")
	r.key("down", "down")
	requireContentGolden(t, r.h)
	rows := r.content()
	var box []string
	for _, row := range rows {
		if strings.HasPrefix(row, "│") {
			box = append(box, row)
		}
	}
	require.Len(t, box, 7)
	require.Contains(t, box[1], "● alibaba-token-plan")
	require.Contains(t, box[2], "deepseek-v4.1-flash (anthropic-messages)")
	require.Contains(t, box[2], "current")
	require.Contains(t, box[4], "● openai")
	require.Contains(t, box[5], "❯   gpt-5.5", "the cursor skipped the header")
	// A filter shows a header only with a matching row.
	r.typeText("sol")
	r.h.waitText("gpt-5.6-sol")
	r.h.settle()
	r.requireNotOnScreen("alibaba-token-plan\n")
	for _, row := range r.content() {
		if strings.HasPrefix(row, "│") {
			require.NotContains(t, row, "alibaba-token-plan")
		}
	}
	r.requireOnScreen("● openai")
}

func TestModelSelectorEmptyHint(t *testing.T) {
	r := startView(t, 80, 24, withFake(noModelFake()))
	r.waitCall("usage")
	r.key("ctrl+l")
	r.h.waitText("No usable models yet.")
	requireContentGolden(t, r.h)
	r.requireOnScreen("Use /providers to add providers.", "esc close  ·  your draft is kept")
	r.requireNotOnScreen("type to filter")
	r.key("esc")
	r.waitGone("No usable models yet.")
}
