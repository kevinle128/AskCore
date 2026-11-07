package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// /trust shows the saved trust decision of the folder and the trust of the
// current session, and saves a change for the next session.

// inheritedFake is a session in ~/src/acme that is trusted through the saved
// entry of ~/src.
func inheritedFake() *fakeClient {
	fc := newFakeClient()
	fc.trust = &protocol.ACPTrustStateResult{Root: acmeRoot, Trusted: true, Source: "saved_ancestor",
		Gated: []string{".ask/settings.json"}, EntryPath: "/home/u/src"}
	fc.state.Trust = &protocol.ACPSessionTrust{Trusted: true, Source: "saved_ancestor"}
	return fc
}

// openTrustCommand types /trust and waits for the box.
func (r *viewRig) openTrustCommand() {
	r.t.Helper()
	r.typeText("/trust")
	r.key("enter")
	r.h.waitText("Project trust")
	r.h.settle()
}

func TestTrustCommandShowsInheritedEntry(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(inheritedFake()), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
		r.openTrustCommand()
		require.Equal(t, 2, r.fc.count("trust_state"), "/trust reads the trust state again")
		requireRows(t, r.h.settle(), []string{
			"╭─ Project trust ──────────────────────────────────────────────────────────────╮",
			"│  ~/src/acme                                                                  │",
			"│  Saved: trusted, from ~/src                                                  │",
			"│  This session: trusted                                                       │",
			"│                                                                              │",
			"│   Trust this folder        Save trust for ~/src/acme only.                   │",
			"│ ❯ Trust parent folder      Trust ~/src and every folder in it.      current  │",
			"│   Do not trust             Do not load the project files here.               │",
			"│   Forget the saved choice  Remove the entry of ~/src. Ask again.             │",
			"╰──────────────────────────────────────────────────────────────────────────────╯",
			"   ↑↓ choose  ·  enter save  ·  esc close  ·  applies to the next session",
		})
	})
	t.Run("40x14", func(t *testing.T) {
		r := startView(t, 40, 14, withFake(inheritedFake()), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
		r.openTrustCommand()
		requireRows(t, r.h.settle(), []string{
			"╭─ Project trust ──────────────────────╮",
			"│   Trust this folder                  │",
			"│ ❯ Trust parent folder       current  │",
			"│   Do not trust                       │",
			"│   Forget saved choice                │",
			"╰──────────────────────────────────────╯",
			"   enter save · esc close",
		})
		r.requireNotOnScreen("Saved:", "This session:")
	})
}

func TestTrustCommandAppliesToNextSession(t *testing.T) {
	t.Run("no_flag", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(inheritedFake()), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
		r.openTrustCommand()
		r.key("down", "enter")
		r.h.waitText("✓ Saved: do not trust ~/src/acme · applies to the next session (/new)")
		require.Equal(t, []protocol.ACPTrustSetRequest{{Cwd: acmeRoot, Target: protocol.ACPTrustTargetFolder, Decision: protocol.ACPTrustDecisionDistrust}},
			r.fc.sentTrust())
		require.Equal(t, 1, r.fc.count("new_session"), "the running session does not change")
		r.requireNotOnScreen("Project trust")
	})
	t.Run("flag", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(inheritedFake()), withOptions(func(o *Options) {
			o.Cwd, o.Session.Trust = acmeRoot, protocol.ACPTrustApprove
		}))
		r.openTrustCommand()
		r.requireOnScreen("This session: trusted (--approve)")
		r.key("down", "enter")
		r.h.waitText("✓ Saved: do not trust ~/src/acme · --approve still applies in this process")
	})
}

func TestTrustCommandNoChange(t *testing.T) {
	r := startView(t, 80, 24, withFake(inheritedFake()), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	r.openTrustCommand()
	r.key("enter")
	r.h.waitText("┊ No change.")
	require.Empty(t, r.fc.sentTrust())
	r.requireNotOnScreen("Project trust")
}

func TestTrustCommandForgetTarget(t *testing.T) {
	for _, c := range []struct {
		name   string
		entry  string
		target protocol.ACPTrustTarget
		shown  bool
	}{
		{"folder", acmeRoot, protocol.ACPTrustTargetFolder, true},
		{"parent", "/home/u/src", protocol.ACPTrustTargetParent, true},
		{"farther_ancestor", "/home", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			fc := newFakeClient()
			fc.trust = &protocol.ACPTrustStateResult{Root: acmeRoot, Trusted: true, Source: "saved_ancestor",
				Gated: []string{".ask/skills"}, EntryPath: c.entry}
			if c.entry == acmeRoot {
				fc.trust.Source = "saved"
			}
			r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
			r.openTrustCommand()
			if !c.shown {
				r.requireNotOnScreen("Forget the saved choice")
				return
			}
			r.requireOnScreen("Forget the saved choice")
			r.key("down", "down", "down", "enter")
			r.h.waitText("✓ Saved: removed the saved choice of")
			require.Equal(t, []protocol.ACPTrustSetRequest{{Cwd: acmeRoot, Target: c.target, Decision: protocol.ACPTrustDecisionClear}},
				r.fc.sentTrust())
		})
	}
}

func TestTrustCommandStoreError(t *testing.T) {
	fc := newFakeClient()
	fc.trust = &protocol.ACPTrustStateResult{Root: acmeRoot, Source: "store_error", Gated: []string{".ask/skills"}, Warning: storeWarn}
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	r.openTrustCommand()
	requireRows(t, r.h.settle(), []string{
		"╭─ Project trust ──────────────────────────────────────────────────────────────╮",
		"│  ✗ trust store /home/u/.ask/trust.json is corrupt or unsafe; no project is   │",
		"│    trusted until you fix or remove it.                                       │",
		"│  Ask does not change it. Fix or remove it; the next session reads it again.  │",
		"╰──────────────────────────────────────────────────────────────────────────────╯",
		"   esc close",
	})
	r.requireNotOnScreen("Trust this folder")
	r.key("enter", "down", "up")
	r.h.settle()
	require.Empty(t, r.fc.sentTrust())
	r.requireOnScreen("Project trust")
	r.key("esc")
	r.waitGone("Project trust")
}

func TestTrustCommandStoreErrorOnSave(t *testing.T) {
	fc := inheritedFake()
	fc.setTrustErr = &RequestError{Code: -32000, Kind: string(protocol.ACPErrInvalidState), Message: storeWarn}
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	r.openTrustCommand()
	r.key("down", "enter")
	r.h.waitText("Ask does not change it.")
	r.requireOnScreen("✗ trust store /home/u/.ask/trust.json is corrupt or unsafe")
	r.requireNotOnScreen("Trust this folder")
}

func TestTrustCommandNotAvailable(t *testing.T) {
	fc := newFakeClient()
	fc.trustErr = ErrTrustNotAvailable
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	r.typeText("/trust")
	r.key("enter")
	r.h.waitText("┊ /trust is not available: the leader is an older version.")
	r.requireNotOnScreen("Project trust")
}

func TestTrustCommandWaitsWithRailRow(t *testing.T) {
	r := startView(t, 80, 24, withFake(inheritedFake()), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	r.fc.mu.Lock()
	r.fc.trustGate = make(chan struct{})
	gate := r.fc.trustGate
	r.fc.mu.Unlock()
	r.typeText("/trust")
	r.key("enter")
	r.h.waitText("┊ Reading project trust…")
	close(gate)
	r.h.waitText("Project trust")
	r.waitGone("Reading project trust…")
}

func TestTrustCommandSaveError(t *testing.T) {
	fc := newFakeClient()
	fc.trust = &protocol.ACPTrustStateResult{Root: acmeRoot, Source: "prompt", NeedsDecision: true, Gated: []string{".ask/skills"}}
	fc.setTrustErr = errors.New("no answer from the leader in 5s.")
	r := startTrustView(t, 80, 24, fc, acmeRoot)
	r.waitBox()
	r.key("esc")
	r.waitStarted()
	r.openTrustCommand()
	r.requireOnScreen("Saved: none", "This session: unknown", "❯ Do not trust")
	r.key("up", "up", "enter")
	r.h.waitText("✗ Not saved: no answer from the leader in 5s.")
	r.requireOnScreen("❯ Trust this folder", "Project trust")
	r.requireNotOnScreen("Forget")
}

func TestTrustCommandSessionRowNamesNotNow(t *testing.T) {
	fc := undecidedFake()
	fc.state.Trust = &protocol.ACPSessionTrust{Trusted: false, Source: "flag"}
	r := startTrustView(t, 80, 24, fc, acmeRoot)
	r.waitBox()
	r.key("esc")
	r.waitStarted()
	r.openTrustCommand()
	r.requireOnScreen("This session: not trusted (not now)")
}

func TestTrustParentHiddenAtHomeAndRoot(t *testing.T) {
	for _, root := range []string{"/srv", "/home/u/acme"} {
		t.Run(root, func(t *testing.T) {
			fc := newFakeClient()
			fc.trust = undecided(root)
			r := startTrustView(t, 80, 24, fc, root)
			r.waitBox()
			r.requireNotOnScreen("Trust parent folder")
			r.requireOnScreen("Not now", "Trust this folder", "Do not trust")
			r.key("esc")
			r.waitStarted()
			r.openTrustCommand()
			r.requireNotOnScreen("Trust parent folder")
			r.requireOnScreen("Trust this folder", "❯ Do not trust")
		})
	}
}
