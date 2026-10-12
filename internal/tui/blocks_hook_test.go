package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// The hook block is built from a plain struct: the agent has no hook runtime
// yet. The tests append fixtures to the compact session and drive the block
// list as a user does.

func intPtr(n int) *int { return &n }

// noRmHook is a hook that blocked "rm -rf build".
func noRmHook() HookRun {
	return HookRun{
		ID: "hk-12", CallID: "call-7",
		Event: "PreToolUse", Name: "no-rm", Scope: HookScopeGlobal,
		Description: "Blocks recursive removal (rm -rf) inside the repo",
		Matcher:     "tool bash", Source: "~/.ask/hooks.json",
		Command: "ask-hook-no-rm", Timeout: 5 * time.Second,
		Target:   "bash rm -rf build",
		Decision: HookBlocked, ExitCode: intPtr(2), Duration: 0,
		Reason: "recursive removal is not allowed in this repo",
		Output: "no-rm: blocked \"rm -rf build\"",
	}
}

// lintGuardHook is a hook that allowed an edit.
func lintGuardHook() HookRun {
	return HookRun{
		ID: "hk-11", CallID: "call-6",
		Event: "PreToolUse", Name: "lint-guard", Scope: HookScopeProject,
		Description: "Runs gofmt on the file before the agent edits it",
		Matcher:     "edit, write · *.go", Source: ".ask/hooks.json",
		Command: "gofmt -l", Timeout: 10 * time.Second,
		Target:   "edit parser.go",
		Decision: HookAllowed, ExitCode: intPtr(0), Duration: 100 * time.Millisecond,
	}
}

// hookModel returns the compact session with hook blocks at its end. The last
// hook is selected after Tab.
func hookModel(t *testing.T, p colorprofile.Profile, width, height int, runs ...HookRun) *Model {
	t.Helper()
	m := listModelAt(t, p, width, height)
	view := NewTranscriptView()
	for _, r := range runs {
		require.NoError(t, m.transcript.Append(r.ID, NewHookBlock(r, view), true))
	}
	m.View()
	return m
}

func rowIndexOf(t *testing.T, rows []string, text string) int {
	t.Helper()
	for i, r := range rows {
		if strings.Contains(r, text) {
			return i
		}
	}
	t.Fatalf("no row holds %q:\n%s", text, strings.Join(rows, "\n"))
	return -1
}

func TestBlockedHookShowsItsRowAndDescriptionAt80Columns(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "⚓ PreToolUse hook")
	require.Equal(t, "  ⚓ PreToolUse hook (global/no-rm) · bash rm -rf build          ✗ blocked 0.0s", rows[i])
	require.Equal(t, "    Blocks recursive removal (rm -rf) inside the repo", rows[i+1])

	raw := strings.Split(ansi.Strip(m.View().Content), "\n")
	require.Equal(t, 80, displayWidth(raw[i]), "the two-column glyph is reserved: the row is the terminal width")
}

func TestAllowedHookShowsASuccessResult(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, lintGuardHook())
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "⚓ PreToolUse hook")
	require.Equal(t, "  ⚓ PreToolUse hook (project/lint-guard) · edit parser.go       ✓ allowed 0.1s", rows[i])
}

func TestHookWithoutDescriptionShowsItsCommandOnTheSecondRow(t *testing.T) {
	run := noRmHook()
	run.Description = ""
	m := hookModel(t, colorprofile.NoTTY, 80, 24, run)
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "⚓ PreToolUse hook")
	require.Equal(t, "    ask-hook-no-rm", rows[i+1])
}

func TestHookRolesAreHookGlyphSuccessAndErrorResult(t *testing.T) {
	m := hookModel(t, colorprofile.TrueColor, 80, 24, lintGuardHook(), noRmHook())
	th := m.theme
	var allowed, blocked, desc string
	for _, r := range rawRows(m) {
		switch p := ansi.Strip(r); {
		case strings.Contains(p, "lint-guard"):
			allowed = r
		case strings.Contains(p, "no-rm"):
			blocked = r
		case strings.Contains(p, "Blocks recursive"):
			desc = r
		}
	}
	require.Contains(t, allowed, th.Style(RoleHook).Render("⚓"))
	require.Contains(t, allowed, th.Style(RoleSuccess).Render("✓ allowed 0.1s"))
	require.Contains(t, blocked, th.Style(RoleError).Render("✗ blocked 0.0s"))
	require.Contains(t, desc, th.Style(RoleMuted).Render("Blocks recursive removal (rm -rf) inside the repo"))
	require.NotEqual(t, th.Style(RoleHook).Render("x"), th.Style(RoleTool).Render("x"))
}

func TestSelectedCollapsedHookIsShadedOnBothRows(t *testing.T) {
	m := hookModel(t, colorprofile.TrueColor, 80, 24, noRmHook())
	press(t, m, "tab")
	require.Equal(t, "hk-12", m.selectedBlock())
	plain, rows := screenOf(m), rawRows(m)
	var shaded []string
	for i, r := range rows {
		if hasShade(m.theme, r) {
			shaded = append(shaded, plain[i])
			require.Equal(t, 80, displayWidth(ansi.Strip(r)), "the shade covers the whole row")
		}
	}
	require.Equal(t, []string{
		"  ›  PreToolUse hook (global/no-rm) · bash rm -rf build          ✗ blocked 0.0s",
		"    Blocks recursive removal (rm -rf) inside the repo",
	}, shaded)
	for _, p := range plain {
		require.False(t, strings.HasPrefix(p, "┃"), "a shaded block has no rail")
	}
}

func TestSelectedCollapsedHookWithoutColorIsReversed(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
	press(t, m, "tab")
	n := 0
	for _, r := range rawRows(m) {
		if strings.HasPrefix(r, ansiRev) {
			n++
		}
	}
	require.Equal(t, 2, n)
}

func TestHookIndicatorReplacesTheGlyphOnHoverAndKeepsTheColumns(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, lintGuardHook(), noRmHook())
	require.Regexp(t, `^  ⚓ PreToolUse hook \(project/lint-guard\)`, rowWith(t, m, "lint-guard"))
	before := rowWith(t, m, "lint-guard")

	m.list.hover = "hk-11"
	m.View()
	after := rowWith(t, m, "lint-guard")
	require.Regexp(t, `^  ›  PreToolUse hook \(project/lint-guard\)`, after)
	require.Equal(t, displayWidth(before), displayWidth(after))
	require.Equal(t, strings.TrimPrefix(before, "  ⚓ "), strings.TrimPrefix(after, "  ›  "), "the text does not move")
	require.NotContains(t, strings.Join(screenOf(m), "\n"), "┃", "a hover is no selection")

	m.list.hover = ""
	m.View()
	require.Regexp(t, `^  ⚓ PreToolUse hook`, rowWith(t, m, "lint-guard"))
}

func TestExpandedHookShowsOpenIndicatorOnlyWhenPointed(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
	press(t, m, "tab", "right")
	require.Regexp(t, `(?m)^┃ ⌄  PreToolUse hook`, strings.Join(screenOf(m), "\n"))
	press(t, m, "esc")
	require.Regexp(t, `(?m)^  ⚓ PreToolUse hook`, strings.Join(screenOf(m), "\n"))
}

func TestExpandedHookShowsDetailFieldsThenTheOutput(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
	press(t, m, "tab", "right")
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "PreToolUse hook (global/no-rm)")
	want := []string{
		"┃ ⌄  PreToolUse hook (global/no-rm) · bash rm -rf build          ✗ blocked 0.0s",
		"┃   Blocks recursive removal (rm -rf) inside the repo",
		"┃   Event    PreToolUse",
		"┃   Matcher  tool bash",
		"┃   Does     Blocks recursive removal (rm -rf) inside the repo",
		"┃   Source   ~/.ask/hooks.json (global)",
		"┃   Command  ask-hook-no-rm · timeout 5s",
		"┃   Result   blocked · exit 2 · 0.0s",
		"┃   Reason   recursive removal is not allowed in this repo",
		"┃   ── output ──",
		`┃   no-rm: blocked "rm -rf build"`,
	}
	require.Equal(t, want, rows[i:i+len(want)])
}

func TestExpandedAllowedHookHasNoReasonRow(t *testing.T) {
	run := lintGuardHook()
	run.Reason = "ignored when the hook allowed the call"
	m := hookModel(t, colorprofile.NoTTY, 80, 24, run)
	press(t, m, "tab", "right")
	text := strings.Join(screenOf(m), "\n")
	require.Contains(t, text, "Result   allowed · exit 0 · 0.1s")
	require.NotContains(t, text, "Reason")
	require.NotContains(t, text, "── output ──", "a run without output has no output rule")
}

func TestExpandedHookFieldNamesAreMutedAndValuesAreForeground(t *testing.T) {
	m := hookModel(t, colorprofile.TrueColor, 80, 24, noRmHook())
	press(t, m, "tab", "right")
	th := m.theme
	var event, reason, rule string
	for _, r := range rawRows(m) {
		switch p := ansi.Strip(r); {
		case strings.Contains(p, "Event    PreToolUse"):
			event = r
		case strings.Contains(p, "Reason"):
			reason = r
		case strings.Contains(p, "── output ──"):
			rule = r
		}
	}
	require.Contains(t, event, th.Style(RoleMuted).Render("Event    "))
	require.Contains(t, event, th.Style(RoleFg).Render("PreToolUse"))
	require.Contains(t, reason, th.Style(RoleError).Render("recursive removal is not allowed in this repo"))
	require.Contains(t, rule, th.Style(RoleDim).Render("── output ──"))
}

func TestExpandedHookWrapsLongValuesAndCutsNothing(t *testing.T) {
	run := noRmHook()
	run.Command = "ask-hook-no-rm --policy /etc/ask/policies/no-recursive-removal.yaml --deny-glob 'build/*' --deny-glob 'dist/*'"
	run.Reason = "recursive removal is not allowed in this repo because the build directory holds the only copy of the release artifacts"
	m := hookModel(t, colorprofile.NoTTY, 80, 24, run)
	press(t, m, "tab", "right")
	rows := screenOf(m)
	for _, r := range rows {
		require.LessOrEqual(t, displayWidth(r), 80)
	}
	text := strings.Join(rows, "\n")
	// Continuation rows hold the value under its first column: 4 + 9 = 13.
	flat := strings.Join(strings.Fields(strings.ReplaceAll(text, "┃", "")), " ")
	require.Contains(t, flat, "--deny-glob 'dist/*' · timeout 5s")
	require.Contains(t, flat, "holds the only copy of the release artifacts")
	require.NotContains(t, text, "…")
	i := rowIndexOf(t, rows, "Command  ask-hook-no-rm --policy")
	require.True(t, strings.HasPrefix(rows[i+1], "┃"+strings.Repeat(" ", 12)), "continuation rows keep the value column: %q", rows[i+1])
}

func hookOutput(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

func TestTruncatedHookShowsFirstThreeHintAndLastThree(t *testing.T) {
	run := noRmHook()
	run.Output = hookOutput(10)
	m := hookModel(t, colorprofile.NoTTY, 80, 24, run)
	press(t, m, "tab", "e")
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "PreToolUse hook (global/no-rm)")
	want := []string{
		"┃ ⚓ PreToolUse hook (global/no-rm) · bash rm -rf build          ✗ blocked 0.0s",
		"┃   Blocks recursive removal (rm -rf) inside the repo",
		"┃   line 1",
		"┃   line 2",
		"┃   line 3",
		"┃   ⋮ 4 more lines · e to expand · Enter full screen",
		"┃   line 8",
		"┃   line 9",
		"┃   line 10",
	}
	require.Equal(t, want, rows[i:i+len(want)])
	require.NotContains(t, strings.Join(rows, "\n"), "Event    ", "no detail fields in Truncated")
	require.Contains(t, barOf(m), "e fold: truncated")
}

func TestTruncatedHookWithSixLinesShowsAllAndNoHint(t *testing.T) {
	run := noRmHook()
	run.Output = hookOutput(6)
	m := hookModel(t, colorprofile.NoTTY, 80, 24, run)
	press(t, m, "tab", "e")
	text := strings.Join(screenOf(m), "\n")
	for i := 1; i <= 6; i++ {
		require.Contains(t, text, fmt.Sprintf("line %d", i))
	}
	require.NotContains(t, text, "⋮")
}

func TestHookCyclesThroughThreeFoldsAndArrowsSetThem(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
	press(t, m, "tab")
	for _, want := range []FoldMode{FoldTruncated, FoldExpanded, FoldCollapsed} {
		press(t, m, "e")
		require.Equal(t, want, m.pane.Fold("hk-12"))
	}
	press(t, m, "right")
	require.Equal(t, FoldExpanded, m.pane.Fold("hk-12"))
	press(t, m, "left")
	require.Equal(t, FoldCollapsed, m.pane.Fold("hk-12"))
}

func TestFoldAllMovesHooksWithTheTools(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, lintGuardHook(), noRmHook())
	press(t, m, "tab", "E")
	require.Equal(t, FoldExpanded, m.pane.Fold("hk-11"))
	require.Equal(t, FoldExpanded, m.pane.Fold("hk-12"))
	require.Equal(t, FoldExpanded, m.pane.Fold(toolID(t, m, "bash", 0)))
	press(t, m, "E")
	require.Equal(t, FoldCollapsed, m.pane.Fold("hk-11"))
	require.Equal(t, FoldCollapsed, m.pane.Fold("hk-12"))
}

func TestHookViewerShowsTheDetailFieldsAndNoDescriptionRow(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
	press(t, m, "tab", "enter")
	require.True(t, m.viewerOpen())
	rows := screenOf(m)
	require.Len(t, rows, 24)
	require.Equal(t, " ← back  PreToolUse hook (global/no-rm) · bash rm -rf build      ✗ blocked 0.0s", rows[0])
	require.Equal(t, strings.Repeat("─", 80), rows[1])
	want := []string{
		"    Event    PreToolUse",
		"    Matcher  tool bash",
		"    Does     Blocks recursive removal (rm -rf) inside the repo",
		"    Source   ~/.ask/hooks.json (global)",
		"    Command  ask-hook-no-rm · timeout 5s",
		"    Result   blocked · exit 2 · 0.0s",
		"    Reason   recursive removal is not allowed in this repo",
		"    ── output ──",
		`    no-rm: blocked "rm -rf build"`,
	}
	require.Equal(t, want, rows[2:2+len(want)])
	require.Regexp(t, `^ ↑↓ scroll · PgUp PgDn page · Home End · Esc back +1–9 of 9$`, rows[23])

	press(t, m, "esc")
	require.False(t, m.viewerOpen())
	require.Equal(t, "hk-12", m.selectedBlock())
}

func TestClickOnTheFirstRowOfAHookOpensTheViewerAndTheSecondRowOnlySelects(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
	y := rowIndexOf(t, screenOf(m), "⚓ PreToolUse hook")
	clickAt(m, 20, y+1)
	require.False(t, m.viewerOpen(), "the description row selects only")
	require.Equal(t, "hk-12", m.selectedBlock())
	clickAt(m, 20, y)
	require.True(t, m.viewerOpen())
	require.Contains(t, screenOf(m)[0], "← back  PreToolUse hook (global/no-rm)")
}

func TestRunningHookShowsTheSpinnerAndSeconds(t *testing.T) {
	run := noRmHook()
	run.Running, run.Decision, run.ExitCode, run.StartMs, run.Output = true, "", nil, 1_000, ""
	m := listModelAt(t, colorprofile.NoTTY, 80, 24)
	view := NewTranscriptView()
	view.SetClock(4_000)
	view.SetSpinnerFrame(1)
	require.NoError(t, m.transcript.Append(run.ID, NewHookBlock(run, view), true))
	m.View()
	row := rowWith(t, m, "⚓ PreToolUse hook")
	require.Regexp(t, `\(global/no-rm\) · bash rm -rf build +⠙ 3s$`, strings.TrimRight(row, " "))

	press(t, m, "tab", "right")
	require.Contains(t, strings.Join(screenOf(m), "\n"), "Result   running")
}

// hookRowAt draws the header row of a hook at a width.
func hookRowAt(width int, run HookRun) string {
	rc := RenderContext{Width: width, Theme: NewTheme(colorprofile.NoTTY), Fold: FoldCollapsed}
	return strings.TrimRight(ansi.Strip(NewHookBlock(run, NewTranscriptView()).Lines(rc)[0]), " ")
}

func TestHookRowCutsTargetThenOwnerThenResultWhenNarrow(t *testing.T) {
	run := noRmHook()
	cases := []struct {
		width int
		want  string
	}{
		{80, "  ⚓ PreToolUse hook (global/no-rm) · bash rm -rf build          ✗ blocked 0.0s"},
		// The target is cut with an ellipsis first.
		{60, "  ⚓ PreToolUse hook (global/no-rm) · bash … ✗ blocked 0.0s"},
		{57, "  ⚓ PreToolUse hook (global/no-rm) · ba… ✗ blocked 0.0s"},
		// Under three columns for the target, it goes; then the owner is cut.
		{50, "  ⚓ PreToolUse hook (global/no-r… ✗ blocked 0.0s"},
		{40, "  ⚓ PreToolUse hook (g… ✗ blocked 0.0s"},
		// The owner goes when no room is left; the result stays down to 30.
		{36, "  ⚓ PreToolUse hook ✗ blocked 0.0s"},
		// Under 30 columns the result is dropped and the owner has room again.
		{29, "  ⚓ PreToolUse hook (global…"},
		{20, "  ⚓ PreToolUse hook"},
	}
	for _, c := range cases {
		got := hookRowAt(c.width, run)
		require.LessOrEqual(t, displayWidth(got), c.width, "width %d: %q", c.width, got)
		require.Equal(t, c.want, got, "width %d", c.width)
	}
}

func TestHookScreenAt40x14HoldsEveryRowInsideTheWidth(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 40, 14, lintGuardHook(), noRmHook())
	for _, r := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(r, "⚓ PreToolUse hook") {
			require.Equal(t, 40, displayWidth(r), "the header row is the whole width")
		}
	}
	checkWidth := func() {
		for _, r := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			require.LessOrEqual(t, displayWidth(r), 40, "%q", r)
		}
	}
	checkWidth()
	press(t, m, "tab", "right")
	checkWidth()
	press(t, m, "enter")
	checkWidth()
}

func TestHookDescriptionRowIsCutWithAnEllipsisAtNarrowWidth(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 40, 14, noRmHook())
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "⚓ PreToolUse hook")
	require.Equal(t, "    Blocks recursive removal (rm -rf) i…", rows[i+1])
}

func TestHookViewerAt40ColumnsCutsTheTitleAndWrapsTheFields(t *testing.T) {
	m := hookModel(t, colorprofile.NoTTY, 40, 14, noRmHook())
	press(t, m, "tab", "enter")
	rows := screenOf(m)
	require.Equal(t, " ← back  PreToolUse hoo… ✗ blocked 0.0s", rows[0])
	require.Equal(t, []string{
		"    Does     Blocks recursive removal",
		"             (rm -rf) inside the repo",
	}, rows[4:6], "a long field value wraps under its own column and is not cut")
}

func TestHookIsNotPartOfTheCopiedReply(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24)
	trl := NewTranslator(TranslatorOptions{Location: time.UTC})
	before := trl.LastReply()
	require.NoError(t, m.transcript.Append("hk-12", NewHookBlock(noRmHook(), NewTranscriptView()), true))
	require.Equal(t, before, trl.LastReply(), "/copy reads the translator's reply text, never a block")
}

func TestHookBlocksGoldens(t *testing.T) {
	t.Run("expanded_80x24", func(t *testing.T) {
		m := hookModel(t, colorprofile.NoTTY, 80, 24, lintGuardHook(), noRmHook())
		press(t, m, "tab", "right")
		requireScreenGolden(t, m)
	})
	t.Run("viewer_80x24", func(t *testing.T) {
		m := hookModel(t, colorprofile.NoTTY, 80, 24, noRmHook())
		press(t, m, "tab", "enter")
		requireScreenGolden(t, m)
	})
	t.Run("viewer_40x14", func(t *testing.T) {
		m := hookModel(t, colorprofile.NoTTY, 40, 14, noRmHook())
		press(t, m, "tab", "enter")
		requireScreenGolden(t, m)
	})
	t.Run("collapsed_hover_80x24", func(t *testing.T) {
		m := hookModel(t, colorprofile.NoTTY, 80, 24, lintGuardHook(), noRmHook())
		m.list.hover = "hk-11"
		m.View()
		requireScreenGolden(t, m)
	})
}
