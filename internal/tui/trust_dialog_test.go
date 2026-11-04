package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The project trust prompt and its notes. The fake client answers the trust
// calls; the model, the program and the terminal emulator are the real ones.
// The texts and the sketches come from the approved trust design.

const (
	acmeRoot  = "/home/u/src/acme"
	acmeDraft = "fix the failing invoice test"
	storeWarn = "trust store /home/u/.ask/trust.json is corrupt or unsafe; no project is trusted until you fix or remove it"
	notNowB4  = "The project files of ~/src/acme are not loaded in this session. The question comes again in the next session. To save a choice, use /trust."
)

// undecided is the trust answer of a folder with gated files and no saved
// decision.
func undecided(root string, gated ...string) *protocol.ACPTrustStateResult {
	if len(gated) == 0 {
		gated = []string{".ask/settings.json", ".ask/skills", ".agents/skills"}
	}
	return &protocol.ACPTrustStateResult{Root: root, Source: "prompt", NeedsDecision: true, Gated: gated}
}

func undecidedFake() *fakeClient {
	fc := newFakeClient()
	fc.trust = undecided(acmeRoot)
	return fc
}

// startTrustView starts the session view in cwd and does not wait for the
// editor: the trust box can take the prompt pane slot.
func startTrustView(t *testing.T, width, height int, fc *fakeClient, cwd string, opts ...func(*Options)) *viewRig {
	t.Helper()
	clock := newTestClock()
	o := Options{
		Client: fc, Profile: colorprofile.NoTTY, Cwd: cwd, Home: "/home/u", Version: "0.1.0",
		Location: time.UTC, Now: clock.Now, TickInterval: -1,
	}
	for _, f := range opts {
		f(&o)
	}
	m := New(o)
	r := &viewRig{t: t, fc: fc, m: m, clock: clock}
	r.h = startScreen(t, m, width, height)
	t.Cleanup(func() { close(fc.events) })
	return r
}

// waitBox waits until the startup trust box shows.
func (r *viewRig) waitBox() {
	r.t.Helper()
	r.h.waitText("Trust this project folder?")
	r.h.waitText("Not now")
	r.h.settle()
}

// waitStarted waits until the session of the start exists and its state was
// read.
func (r *viewRig) waitStarted() {
	r.t.Helper()
	r.waitCall("new_session")
	r.waitCall("state")
	r.h.settle()
}

// paneText is the settled screen as one string.
func (r *viewRig) paneText() string { return strings.Join(r.h.settle(), "\n") }

// flatText is the settled screen with every run of white space as one space,
// so that a wrapped sentence reads as one line.
func (r *viewRig) flatText() string { return flat1(r.paneText()) }

func flat1(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestTrustPromptShownForUndecidedProject(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{{"prompt_80x24", 80, 24}, {"prompt_120x40", 120, 40}, {"prompt_40x14", 40, 14}} {
		t.Run(size.name, func(t *testing.T) {
			r := startTrustView(t, size.width, size.height, undecidedFake(), acmeRoot)
			r.waitBox()
			r.h.requireGolden()
			require.Zero(t, r.fc.count("new_session"), "no session starts before the choice")
			require.Equal(t, 1, r.fc.count("trust_state"))
			r.requireOnScreen("╭─ Trust this project folder? ─", "❯ Not now", "Trust this folder", "Trust parent folder", "Do not trust")
			if size.width < shortWidth {
				r.requireNotOnScreen("Do not load them now", "has project files")
				return
			}
			r.requireOnScreen("~/src/acme has project files that Ask has not read or run:",
				".ask/settings.json  .ask/skills  .agents/skills",
				"Do not load them now. Ask again in the next session.")
		})
	}
}

func TestTrustPromptTextNamesLimits(t *testing.T) {
	r := startTrustView(t, 80, 24, undecidedFake(), acmeRoot)
	r.waitBox()
	screen := r.flatText()
	require.Contains(t, screen, "Trust does not give permission to run shell commands or tools.")
	require.Contains(t, screen, "Trust does not apply to AGENTS.md or CLAUDE.md.")
	require.NotContains(t, strings.ToLower(screen), "blocked")
}

func TestTrustPromptCancelIsUntrustedSession(t *testing.T) {
	for _, keyName := range []string{"enter", "esc", "ctrl+c"} {
		t.Run(strings.ReplaceAll(keyName, "+", "_"), func(t *testing.T) {
			r := startTrustView(t, 80, 24, undecidedFake(), acmeRoot)
			r.waitBox()
			r.key(keyName)
			r.waitStarted()
			r.h.waitText("not loaded in this session")
			require.Empty(t, r.fc.sentTrust(), "Not now saves nothing")
			require.Equal(t, []protocol.ACPTrustOverride{protocol.ACPTrustDeny}, r.fc.sentOverrides())
			require.Contains(t, r.flatText(), notNowB4)
			r.requireNotOnScreen("Trust this project folder?", "has project files")
			r.requireOnScreen("Ask anything")
		})
	}
}

func TestTrustPromptSessionOnlyChoiceSendsOverride(t *testing.T) {
	r := startTrustView(t, 80, 24, undecidedFake(), acmeRoot)
	r.waitBox()
	// Down and up again: Not now is the only session-only row.
	r.key("down", "up", "enter")
	r.waitStarted()
	require.Empty(t, r.fc.sentTrust())
	require.Equal(t, []protocol.ACPTrustOverride{protocol.ACPTrustDeny}, r.fc.sentOverrides())
}

func TestTrustPromptChoiceSavesAndStartsSession(t *testing.T) {
	for _, c := range []struct {
		name  string
		keys  []string
		req   protocol.ACPTrustSetRequest
		note  string
		draft bool
	}{
		{"folder", []string{"down", "enter"},
			protocol.ACPTrustSetRequest{Cwd: acmeRoot, Target: protocol.ACPTrustTargetFolder, Decision: protocol.ACPTrustDecisionTrust},
			"✓ Trusted ~/src/acme. Its project files load in this session.", true},
		{"parent", []string{"down", "down", "enter"},
			protocol.ACPTrustSetRequest{Cwd: acmeRoot, Target: protocol.ACPTrustTargetParent, Decision: protocol.ACPTrustDecisionTrust},
			"✓ Trusted ~/src and every folder in it. The project files of ~/src/acme load in this session.", false},
		{"distrust", []string{"down", "down", "down", "enter"},
			protocol.ACPTrustSetRequest{Cwd: acmeRoot, Target: protocol.ACPTrustTargetFolder, Decision: protocol.ACPTrustDecisionDistrust},
			"Saved: do not trust ~/src/acme. Its project files are not loaded. To change it, use /trust.", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			fc := undecidedFake()
			if c.draft {
				fc.trustGate = make(chan struct{})
			}
			r := startTrustView(t, 80, 24, fc, acmeRoot)
			if c.draft {
				// A draft typed before the trust answer comes back after the choice.
				r.h.waitText("Ask anything")
				r.typeText(acmeDraft)
				r.h.waitText(acmeDraft)
				close(fc.trustGate)
			}
			r.waitBox()
			r.key(c.keys...)
			r.waitStarted()
			require.Equal(t, []protocol.ACPTrustSetRequest{c.req}, r.fc.sentTrust())
			require.Equal(t, []protocol.ACPTrustOverride{""}, r.fc.sentOverrides(), "a saved choice sends no override")
			calls := r.fc.callList()
			require.Less(t, indexOf(calls, "set_trust:"+string(c.req.Target)+":"+string(c.req.Decision)), indexOf(calls, "new_session"))
			r.h.waitText(c.note[:20])
			require.Contains(t, r.flatText(), c.note)
			if c.draft {
				r.requireOnScreen("❯ " + acmeDraft)
			}
		})
	}
}

func TestEnterDuringTrustCheckSendsNothing(t *testing.T) {
	fc := undecidedFake()
	fc.trustGate = make(chan struct{})
	r := startTrustView(t, 80, 24, fc, acmeRoot)
	r.h.waitText("Ask anything")
	r.typeText(acmeDraft)
	r.key("enter")
	r.h.waitText("┊ Starting, please wait…")
	r.requireOnScreen("❯ " + acmeDraft)
	require.Zero(t, r.fc.count("prompt:"), "no session exists, so nothing is sent")
	close(fc.trustGate)
	r.waitBox()
	require.Zero(t, r.fc.count("prompt:"))
}

func TestTrustPromptIgnoresTypedKeysAndMouse(t *testing.T) {
	fc := undecidedFake()
	fc.trustGate = make(chan struct{})
	r := startTrustView(t, 80, 24, fc, acmeRoot)
	r.h.waitText("Ask anything")
	r.typeText(acmeDraft)
	r.h.waitText(acmeDraft)
	close(fc.trustGate)
	r.waitBox()
	r.key("j", "k", "1", "y", "space", "tab")
	r.h.send(tea.PasteMsg{Content: "trust"})
	rows := r.h.settle()
	y := rowIndexOf(t, rows, "Trust this folder")
	r.h.send(tea.MouseClickMsg{X: 6, Y: y, Button: tea.MouseLeft})
	r.h.send(tea.MouseReleaseMsg{X: 6, Y: y, Button: tea.MouseLeft})
	r.h.settle()
	r.requireOnScreen("❯ Not now")
	require.Empty(t, r.fc.sentTrust())
	require.Zero(t, r.fc.count("new_session"))
	r.key("esc")
	r.waitStarted()
	prompt := r.h.settle()[rowIndexOf(t, r.h.settle(), "❯ "+acmeDraft)]
	require.Equal(t, " ❯ "+acmeDraft, strings.TrimRight(prompt, " "), "the paste did not reach the draft")
	require.Equal(t, acmeDraft, r.stopped().sv.editor.Text(), "the draft is the draft from before the box")
}

func TestTrustPromptQuitStartsNoSession(t *testing.T) {
	r := startTrustView(t, 80, 24, undecidedFake(), acmeRoot)
	r.waitBox()
	r.key("ctrl+d")
	r.exited()
	require.Empty(t, r.fc.sentTrust())
	require.Zero(t, r.fc.count("new_session"))
}

func TestTrustPromptSaveErrorKeepsBox(t *testing.T) {
	fc := undecidedFake()
	fc.setTrustErr = errors.New("no answer from the leader in 5s.")
	r := startTrustView(t, 80, 24, fc, acmeRoot)
	r.waitBox()
	r.key("down", "enter")
	r.h.waitText("✗ Not saved: no answer from the leader in 5s.")
	rows := r.h.settle()
	box := rowIndexOf(t, rows, "✗ Not saved: ")
	require.Contains(t, rows[box+1], "╰", "the error row is the last row of the box")
	r.requireOnScreen("❯ Trust this folder", "Trust this project folder?")
	require.Zero(t, r.fc.count("new_session"))
	// The error row goes at the next key.
	r.key("up")
	r.waitGone("Not saved")
	r.requireOnScreen("❯ Not now")
}

func TestTrustPromptSkippedWithFlag(t *testing.T) {
	t.Run("deny", func(t *testing.T) {
		fc := undecidedFake()
		r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) {
			o.Cwd, o.Session.Trust = acmeRoot, protocol.ACPTrustDeny
		}))
		r.h.waitText("The project files of ~/src/acme are not loaded (--no-approve).")
		r.requireNotOnScreen("Trust this project folder?")
		r.typeText("/new")
		r.key("enter")
		r.h.waitFor("second session", func(*screenHarness) bool { return r.fc.count("new_session") == 2 })
		require.Zero(t, r.fc.count("trust_state"), "a flag skips the trust question")
		require.Equal(t, []protocol.ACPTrustOverride{protocol.ACPTrustDeny, protocol.ACPTrustDeny}, r.fc.sentOverrides())
	})
	t.Run("approve", func(t *testing.T) {
		fc := undecidedFake()
		r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) {
			o.Cwd, o.Session.Trust = acmeRoot, protocol.ACPTrustApprove
		}))
		require.Zero(t, r.fc.count("trust_state"))
		require.Equal(t, []protocol.ACPTrustOverride{protocol.ACPTrustApprove}, r.fc.sentOverrides())
		r.requireNotOnScreen("not loaded", "Trusted", "Trust this project folder?")
	})
}

func TestTrustPromptSkippedForOldLeader(t *testing.T) {
	fc := newFakeClient()
	fc.trustErr = ErrTrustNotAvailable
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	require.Equal(t, []protocol.ACPTrustOverride{""}, r.fc.sentOverrides())
	r.requireNotOnScreen("Trust this project folder?", "not loaded", "Warning", "Trusted")
}

func TestTrustCheckFailureStartsUntrusted(t *testing.T) {
	fc := newFakeClient()
	fc.trustErr = errors.New("connection reset")
	fc.state.Trust = &protocol.ACPSessionTrust{Trusted: false, Source: "no_ui"}
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	r.h.waitText("Ask could not check")
	require.Equal(t, []protocol.ACPTrustOverride{""}, r.fc.sentOverrides())
	r.requireNotOnScreen("Trust this project folder?")
	require.Contains(t, r.flatText(),
		"The project files of ~/src/acme are not loaded: Ask could not check the trust of this folder (connection reset).")
}

func TestTrustStoreWarningShown(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 24}, {40, 14}} {
		fc := newFakeClient()
		fc.trust = &protocol.ACPTrustStateResult{Root: acmeRoot, Source: "store_error", Gated: []string{".ask/settings.json"}, Warning: storeWarn}
		fc.state.Trust = &protocol.ACPSessionTrust{Trusted: false, Source: "store_error"}
		r := startView(t, size.width, size.height, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
		r.h.waitText("Warning: trust store")
		r.requireNotOnScreen("Trust this project folder?", "not loaded")
		require.Equal(t, []protocol.ACPTrustOverride{""}, r.fc.sentOverrides())
		require.Contains(t, r.flatText(), "Warning: "+storeWarn)
	}
}

func TestTrustPromptShowsCheckWarning(t *testing.T) {
	fc := newFakeClient()
	fc.trust = undecided(acmeRoot, ".ask/skills")
	fc.trust.Warning = "cannot check the project files of " + acmeRoot + "; the project needs a trust decision"
	r := startTrustView(t, 80, 24, fc, acmeRoot)
	r.waitBox()
	rows := r.h.settle()
	warn := rowIndexOf(t, rows, "Warning: cannot check the project files of /home/u/src/acme;")
	explain := rowIndexOf(t, rows, "~/src/acme has project files")
	require.Less(t, warn, explain, "the warning is above the explanation")
}

const longRoot = "/home/u/work/clients/northwind-traders/platform/services/billing-reconciliation-worker"

func longFake() *fakeClient {
	fc := newFakeClient()
	fc.trust = undecided(longRoot, ".ask/settings.json", ".ask/SYSTEM.md", ".ask/APPEND_SYSTEM.md", ".ask/mcp.json",
		".ask/models.json", ".ask/skills", ".ask/prompts", ".ask/extensions", ".agents/skills",
		"/home/u/work/clients/northwind-traders/.agents/skills")
	return fc
}

func TestTrustPromptLongPath(t *testing.T) {
	t.Run("long_80x24", func(t *testing.T) {
		r := startTrustView(t, 80, 24, longFake(), longRoot)
		r.waitBox()
		r.h.requireGolden()
		r.requireOnScreen("Trust ~/…/services and every folder in it.",
			"  ~/work/clients/northwind-traders/platform/services/billing-reconciliation-wo",
			"  rker has project files that Ask has not read or run:")
		rows := r.h.settle()
		for _, row := range rows[:rowIndexOf(t, rows, "Trust this project folder?")] {
			require.NotContains(t, row, "…", "the block pane never cuts a path")
		}
	})
	t.Run("window_40x12", func(t *testing.T) {
		r := startTrustView(t, 40, 12, longFake(), longRoot)
		r.waitBox()
		r.h.requireGolden()
		r.requireOnScreen("❯ Not now", "  … 3 more")
	})
	t.Run("window_40x12_down", func(t *testing.T) {
		r := startTrustView(t, 40, 12, longFake(), longRoot)
		r.waitBox()
		r.key("down")
		r.h.waitText("❯ Trust this folder")
		r.h.requireGolden()
		r.requireOnScreen("  … 3 more")
	})
}

func TestTrustExplainCountsHiddenEntries(t *testing.T) {
	// Six ancestor entries give more than four file rows: three rows show and
	// the last row counts the entries that do not show.
	fc := newFakeClient()
	fc.trust = undecided(acmeRoot, ".ask/settings.json", "/home/a/.agents/skills", "/home/b/.agents/skills",
		"/home/c/.agents/skills", "/home/d/.agents/skills", "/home/e/.agents/skills")
	r := startTrustView(t, 80, 30, fc, acmeRoot)
	r.waitBox()
	r.requireOnScreen("    .ask/settings.json", "    /home/a/.agents/skills", "    /home/b/.agents/skills", "    … 3 more")
	r.requireNotOnScreen("/home/c/.agents/skills")
}

func TestTrustNoteCJKPath(t *testing.T) {
	root := "/home/u/项目/客户-acme"
	fc := newFakeClient()
	fc.trust = undecided(root)
	r := startTrustView(t, 40, 14, fc, root)
	r.waitBox()
	r.key("esc")
	r.waitStarted()
	r.h.waitText("The project files of")
	grid := r.h.settle()
	for _, row := range grid {
		require.LessOrEqual(t, displayWidth(row), 40, "no row is wider than the screen: %q", row)
	}
	requireRows(t, grid, []string{
		"  The project files of",
		"  ~/项目/客户-acme are not loaded in",
		"  this session. The question comes",
		"  again in the next session. To save a",
		"  choice, use /trust.",
	})
}

func TestNewCommandAsksTrustAgain(t *testing.T) {
	fc := newFakeClient()
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
	require.Equal(t, 1, r.fc.count("trust_state"))
	fc.mu.Lock()
	fc.trust = undecided(acmeRoot)
	fc.mu.Unlock()
	r.typeText("/new")
	r.key("enter")
	r.waitBox()
	require.Equal(t, 2, r.fc.count("trust_state"))
	require.Equal(t, 1, r.fc.count("new_session"), "the new session waits for the choice")
	r.key("esc")
	r.h.waitFor("second session", func(*screenHarness) bool { return r.fc.count("new_session") == 2 })
	require.Equal(t, []protocol.ACPTrustOverride{"", protocol.ACPTrustDeny}, r.fc.sentOverrides())
}

func TestLazySessionReusesTrustChoice(t *testing.T) {
	fc := undecidedFake()
	fc.newSessionErrs = []error{errors.New("the agent is busy")}
	r := startTrustView(t, 80, 24, fc, acmeRoot)
	r.waitBox()
	r.key("enter")
	r.h.waitText("Could not start a session")
	r.typeText("hello")
	r.key("enter")
	r.waitCall("prompt:hello")
	require.Equal(t, []protocol.ACPTrustOverride{protocol.ACPTrustDeny, protocol.ACPTrustDeny}, r.fc.sentOverrides())
	require.Equal(t, 1, r.fc.count("trust_state"), "the retry asks no trust question")
	r.requireNotOnScreen("Trust this project folder?")
}

func TestUntrustedNoticeShowsSource(t *testing.T) {
	for _, c := range []struct {
		name  string
		trust *protocol.ACPTrustStateResult
		flag  protocol.ACPTrustOverride
		state *protocol.ACPSessionTrust
		err   error
		want  string
	}{
		{name: "flag_no_approve", flag: protocol.ACPTrustDeny, state: &protocol.ACPSessionTrust{Source: "flag"},
			want: "The project files of ~/src/acme are not loaded (--no-approve)."},
		{name: "saved", trust: &protocol.ACPTrustStateResult{Root: acmeRoot, Source: "saved", EntryPath: acmeRoot},
			state: &protocol.ACPSessionTrust{Source: "saved"},
			want:  "The project files of ~/src/acme are not loaded: this folder is not trusted. To change it, use /trust."},
		{name: "saved_ancestor", trust: &protocol.ACPTrustStateResult{Root: acmeRoot, Source: "saved_ancestor", EntryPath: "/home/u/src"},
			state: &protocol.ACPSessionTrust{Source: "saved_ancestor"},
			want:  "The project files of ~/src/acme are not loaded: ~/src is not trusted. To change it, use /trust."},
		{name: "default_never", trust: &protocol.ACPTrustStateResult{Root: acmeRoot, Source: "default_never"},
			state: &protocol.ACPSessionTrust{Source: "default_never"},
			want:  `The project files of ~/src/acme are not loaded: defaultProjectTrust is "never". To trust this folder, use /trust.`},
		{name: "no_ui", err: errors.New("deadline exceeded"), state: &protocol.ACPSessionTrust{Source: "no_ui"},
			want: "The project files of ~/src/acme are not loaded: Ask could not check the trust of this folder (deadline exceeded)."},
		{name: "trusted_by_flag", flag: protocol.ACPTrustApprove, state: &protocol.ACPSessionTrust{Trusted: true, Source: "flag"}},
		{name: "trusted_saved", trust: &protocol.ACPTrustStateResult{Root: acmeRoot, Trusted: true, Source: "saved", EntryPath: acmeRoot},
			state: &protocol.ACPSessionTrust{Trusted: true, Source: "saved"}},
		{name: "trusted_default_always", trust: &protocol.ACPTrustStateResult{Root: acmeRoot, Trusted: true, Source: "default_always"},
			state: &protocol.ACPSessionTrust{Trusted: true, Source: "default_always"}},
		{name: "no_gated_files", state: &protocol.ACPSessionTrust{Trusted: true, Source: "no_gated_files"}},
		{name: "host_omits_trust", trust: &protocol.ACPTrustStateResult{Root: acmeRoot, Source: "default_never"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fc := newFakeClient()
			fc.trust, fc.trustErr, fc.state.Trust = c.trust, c.err, c.state
			r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Cwd, o.Session.Trust = acmeRoot, c.flag }))
			if c.want == "" {
				r.requireNotOnScreen("not loaded", "Trusted", "Warning")
				return
			}
			r.h.waitText("The project files of")
			require.Contains(t, r.flatText(), c.want)
		})
	}
}

// sketchDiagnostics are the settings problems of sketch D1.
func sketchDiagnostics(root string) []protocol.ACPDiagnostic {
	return []protocol.ACPDiagnostic{
		{Layer: "project", Path: root + "/.ask/settings.json", Key: "defaultProjectTrust", Text: "a project file cannot set this key; it is ignored"},
		{Layer: "project", Path: root + "/.ask/settings.json", Key: "DefaultModel", Text: "unknown key; did you mean defaultModel?"},
		{Layer: "user", Path: "/home/u/.ask/settings.json", Key: "thinkingFold", Text: `must be "expanded", "truncated" or "collapsed"; the default is used`},
	}
}

func TestSettingsDiagnosticsNotice(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		fc := undecidedFake()
		fc.state.Diagnostics = sketchDiagnostics(acmeRoot)
		r := startTrustView(t, 80, 24, fc, acmeRoot)
		r.waitBox()
		r.key("down", "enter")
		r.waitStarted()
		r.h.waitText("Settings problems")
		requireRows(t, r.h.settle(), []string{
			"  ✓ Trusted ~/src/acme. Its project files load in this session.",
			"",
			"  Settings problems",
			"    ~/src/acme/.ask/settings.json",
			"      defaultProjectTrust: a project file cannot set this key; it is ignored",
			"      DefaultModel: unknown key; did you mean defaultModel?",
			"    ~/.ask/settings.json",
			`      thinkingFold: must be "expanded", "truncated" or "collapsed"; the`,
			"        default is used",
		})
	})
	t.Run("40x14", func(t *testing.T) {
		root := "/home/u/项目/客户-acme"
		fc := newFakeClient()
		fc.state.Diagnostics = sketchDiagnostics(root)[:1]
		r := startView(t, 40, 14, withFake(fc), withOptions(func(o *Options) { o.Cwd = root }))
		r.h.waitText("Settings problems")
		requireRows(t, r.h.settle(), []string{
			"  Settings problems",
			"    ~/项目/客户-acme/.ask/settings.jso",
			"    n",
			"      defaultProjectTrust: a project",
			"        file cannot set this key; it",
			"        is ignored",
		})
	})
	t.Run("cap", func(t *testing.T) {
		fc := newFakeClient()
		for i := range 9 {
			fc.state.Diagnostics = append(fc.state.Diagnostics, protocol.ACPDiagnostic{Layer: "user", Path: "/home/u/.ask/settings.json",
				Key: "key" + string(rune('a'+i)), Text: "unknown key"})
		}
		r := startView(t, 80, 30, withFake(fc), withOptions(func(o *Options) { o.Cwd = acmeRoot }))
		r.h.waitText("Settings problems")
		r.requireOnScreen("      keyh: unknown key", "    … 1 more · ask settings show")
		r.requireNotOnScreen("keyi:")
	})
}

func TestTrustPromptResizeKeepsHighlight(t *testing.T) {
	r := startTrustView(t, 80, 24, undecidedFake(), acmeRoot)
	r.waitBox()
	r.key("down")
	r.h.waitText("❯ Trust this folder")
	r.h.resize(40, 14)
	r.h.waitFor("narrow box", func(h *screenHarness) bool { return h.count("Load them now") == 0 })
	r.requireOnScreen("❯ Trust this folder", "↑↓ · enter · esc not now")
	r.h.resize(80, 24)
	r.h.waitText("Load them now and in later sessions.")
	r.requireOnScreen("❯ Trust this folder")
	require.Zero(t, r.fc.count("new_session"))
}

func TestThinkingFoldFromSessionState(t *testing.T) {
	t.Run("project_value_wins", func(t *testing.T) {
		fc := newFakeClient()
		fc.state.ThinkingFold = "collapsed"
		r := startView(t, 80, 30, withFake(fc), withOptions(func(o *Options) { o.ThinkingFold = ThinkingExpanded }))
		r.event(secondTurnEvents()...)
		r.h.waitText("Added TestCountTrailingComma")
		rows := r.h.settle()
		requireOneRowStep(t, rows, "Thought for")
		requireNotInRows(t, rows, twoThoughts[0])
	})
	t.Run("user_value_without_state", func(t *testing.T) {
		fc := newFakeClient()
		r := startView(t, 80, 30, withFake(fc), withOptions(func(o *Options) { o.ThinkingFold = ThinkingExpanded }))
		r.event(secondTurnEvents()...)
		r.h.waitText("Added TestCountTrailingComma")
		r.requireOnScreen(twoThoughts[0])
	})
	t.Run("ctrl_t_survives_later_state", func(t *testing.T) {
		fc := newFakeClient()
		fc.state.ThinkingFold = "collapsed"
		r := startView(t, 80, 30, withFake(fc))
		r.event(secondTurnEvents()...)
		r.h.waitText("Added TestCountTrailingComma")
		r.key("ctrl+t")
		r.h.waitText("Thinking blocks: expanded")
		// A settled run reads the state again; the mode of ctrl+t stays.
		before := r.fc.count("state")
		r.event(&protocol.AgentStart{}, &protocol.AgentSettled{})
		r.h.waitFor("state read again", func(*screenHarness) bool { return r.fc.count("state") > before })
		r.h.settle()
		require.Equal(t, FoldExpanded, r.stopped().thinkingFold)
	})
}
