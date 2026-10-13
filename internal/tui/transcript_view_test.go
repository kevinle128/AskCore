package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The viewer shows one block on the whole screen. The tests open it through
// the block list, as a user does, on the compact session of the fold tests.

// openTool selects the n-th bash step of the compact session and opens the
// viewer with Enter.
func openTool(t *testing.T, m *Model, bashIndex int) {
	t.Helper()
	press(t, m, "tab")
	m.list.selected = toolID(t, m, "bash", bashIndex)
	press(t, m, "enter")
	require.True(t, m.viewerOpen())
}

func TestViewerShowsTheCommandAndTheOutputOfATool(t *testing.T) {
	m := listModel(t, 80, 24)
	openTool(t, m, 0)
	require.True(t, m.View().AltScreen)
	rows := screenOf(m)
	require.Len(t, rows, 24)
	require.Regexp(t, `^ ← back  Ran seq 1 40 +0\.1s$`, rows[0])
	require.Equal(t, 80, displayWidth(ansi.Strip(m.viewerFrame().Content[:strings.Index(m.viewerFrame().Content, "\n")])), "the outcome ends one column before the edge")
	require.Equal(t, strings.Repeat("─", 80), rows[1])
	require.Equal(t, "    $ seq 1 40", rows[2])
	require.Equal(t, "", rows[3])
	for i := 1; i <= 18; i++ {
		require.Equal(t, fmt.Sprintf("    %d", i), rows[3+i])
	}
	require.Equal(t, strings.Repeat("─", 80), rows[22])
	require.Regexp(t, `^ ↑↓ scroll · PgUp PgDn page · Home End · Esc back +1–20 of 42$`, rows[23])
}

func TestViewerEscReturnsToTheSameListAndSelection(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "k")
	before := screenOf(m)
	sel := m.selectedBlock()
	press(t, m, "enter")
	require.True(t, m.viewerOpen())
	press(t, m, "j", "pgdown", "end")
	press(t, m, "esc")
	require.False(t, m.viewerOpen())
	require.Equal(t, before, screenOf(m))
	require.Equal(t, sel, m.selectedBlock())
	require.Equal(t, FocusBlocks, m.Focus())
}

func TestViewerShowsAFailureAtTheRightEdge(t *testing.T) {
	m := listModel(t, 80, 24)
	openTool(t, m, 1)
	rows := screenOf(m)
	require.Regexp(t, `^ ← back  Ran go test \./\.\.\. +✗ exit 1$`, rows[0])
	require.Equal(t, "    $ go test ./...", rows[2])
	require.Equal(t, "", rows[3])
	require.Equal(t, "    --- FAIL: TestCount (0.00s)", rows[4])
	require.Equal(t, "    FAIL", rows[12])
	require.Regexp(t, `1–11 of 11$`, rows[23])
	require.Equal(t, "", rows[13], "the body is shorter than the window")
}

func TestViewerShowsAnEditAsADiff(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	m.list.selected = toolID(t, m, "edit", 0)
	press(t, m, "enter")
	text := strings.Join(screenOf(m), "\n")
	require.Regexp(t, `^ ← back  Edited parser\.go  \+3 -1`, screenOf(m)[0])
	require.Regexp(t, `(?m)^\s+\d+ - `, text)
	require.Regexp(t, `(?m)^\s+\d+ \+ `, text)
}

func TestViewerShowsEveryLineOfAThinking(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	m.list.selected = blockID(t, m, isThinking)
	press(t, m, "enter")
	rows := screenOf(m)
	require.Regexp(t, `^ ← back  Thought for 0\.5s$`, rows[0])
	require.Equal(t, "    Count splits on a comma.", rows[2])
	require.Equal(t, "    An empty field must not count.", rows[3])
	require.Regexp(t, `1–2 of 2$`, rows[23])
}

func TestEnterOpensNothingForAReplyAndAPrompt(t *testing.T) {
	m := listModel(t, 80, 24)
	press(t, m, "tab", "enter")
	require.False(t, m.viewerOpen())
	press(t, m, "g", "enter")
	require.False(t, m.viewerOpen())
}

func TestViewerScrollKeys(t *testing.T) {
	m := listModel(t, 80, 24)
	openTool(t, m, 0)
	pos := func() string { return regexp.MustCompile(`\d+–\d+ of \d+`).FindString(screenOf(m)[23]) }
	// The window is 20 rows; the body has 42.
	require.Equal(t, "1–20 of 42", pos())
	press(t, m, "down")
	require.Equal(t, "2–21 of 42", pos())
	press(t, m, "j", "up")
	require.Equal(t, "2–21 of 42", pos())
	press(t, m, "k")
	require.Equal(t, "1–20 of 42", pos())
	press(t, m, "pgdown")
	require.Equal(t, "20–39 of 42", pos(), "a page is one row less than the window")
	press(t, m, "space")
	require.Equal(t, "23–42 of 42", pos(), "the end stops the page")
	press(t, m, "b")
	require.Equal(t, "4–23 of 42", pos())
	press(t, m, "pgup")
	require.Equal(t, "1–20 of 42", pos())
	press(t, m, "G")
	require.Equal(t, "23–42 of 42", pos())
	press(t, m, "g")
	require.Equal(t, "1–20 of 42", pos())
	press(t, m, "end")
	require.Equal(t, "23–42 of 42", pos())
	press(t, m, "down", "pgdown")
	require.Equal(t, "23–42 of 42", pos(), "past the end nothing moves")
	press(t, m, "home")
	require.Equal(t, "1–20 of 42", pos())
}

func TestViewerDropsEveryOtherKeyAndTheDraftStays(t *testing.T) {
	m := listModel(t, 80, 24)
	typed(t, m, "draft text")
	press(t, m, "tab")
	m.list.selected = toolID(t, m, "bash", 0)
	press(t, m, "enter")
	for _, k := range []tea.KeyPressMsg{
		{Code: 'x', Text: "x"}, {Code: tea.KeyEnter}, {Code: 'd', Mod: tea.ModCtrl}, {Code: 'l', Mod: tea.ModCtrl},
		{Code: 't', Mod: tea.ModCtrl}, {Code: tea.KeyTab}, {Code: tea.KeyLeft}, {Code: tea.KeyBackspace},
		{Code: 'e', Text: "e"}, {Code: 'E', Text: "E"},
	} {
		m.Update(k)
	}
	m.Update(tea.PasteMsg{Content: "pasted"})
	require.True(t, m.viewerOpen(), "none of these keys closes the viewer")
	require.Equal(t, "draft text", m.sv.editor.Text())
	require.Zero(t, m.sv.client.(*fakeClient).count("prompt:"), "enter did not send the draft")
	m.sv.run.running = true
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.False(t, m.viewerOpen())
	require.False(t, m.sv.run.cancelling, "Esc in the viewer does not cancel a run")
}

func TestCtrlCClosesTheViewerThenAsksToQuit(t *testing.T) {
	m := listModel(t, 80, 24)
	openTool(t, m, 0)
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.False(t, m.viewerOpen())
	require.True(t, m.sv.quitConfirmed(), "the same press opens the confirmation window")
	require.Equal(t, FocusBlocks, m.Focus(), "the list is as before")
}

func TestViewerAtFortyColumnsUsesTheShortForms(t *testing.T) {
	m := listModel(t, 40, 14)
	openTool(t, m, 0)
	rows := screenOf(m)
	require.Len(t, rows, 14)
	require.Regexp(t, `^ ← back  Ran seq 1 40 +0\.1s$`, rows[0])
	require.Equal(t, 40, displayWidth(ansi.Strip(strings.Split(m.View().Content, "\n")[0])))
	require.Equal(t, "    $ seq 1 40", rows[2])
	require.Equal(t, "    1", rows[4])
	require.Equal(t, "    8", rows[11])
	require.Equal(t, " ↑↓ PgUp PgDn · Esc back", rows[13])
	for _, r := range rows {
		require.LessOrEqual(t, displayWidth(r), 40, r)
	}
}

func TestViewerTitleIsCutBeforeTheOutcome(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	got := ansi.Strip(viewerTitleRow(th, 40, "PreToolUse hook (global/no-rm) · bash rm -rf build", "✗ blocked 0.0s", RoleError))
	require.Equal(t, " ← back  PreToolUse hoo… ✗ blocked 0.0s ", got)
	for w := 1; w <= 70; w++ {
		row := ansi.Strip(viewerTitleRow(th, w, "PreToolUse hook (global/no-rm) · bash rm -rf build", "✗ blocked 0.0s", RoleError))
		require.LessOrEqual(t, displayWidth(row), w, "width %d: %q", w, row)
		row = ansi.Strip(viewerKeyRow(th, w, 100, 121, 5000))
		require.LessOrEqual(t, displayWidth(row), w)
	}
}

func TestViewerUnderFiveRowsShowsOnlyTheBody(t *testing.T) {
	m := listModel(t, 40, 14)
	openTool(t, m, 0)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 4})
	rows := screenOf(m)
	require.Len(t, rows, 4)
	for _, r := range rows {
		require.NotContains(t, r, "back")
		require.NotContains(t, r, "PgUp")
	}
	require.Equal(t, "    $ seq 1 40", rows[0])
}

func TestViewerWrapsALongLineAndCutsNothing(t *testing.T) {
	long := strings.Repeat("long-line-", 12) + "end"
	events := flat(
		&protocol.AgentStart{},
		userAt("go", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		toolStart("c1", "bash", `{"command":"cat f"}`, 0),
		resultEnd("c1", "bash", long, protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, 100),
	)
	m := listModelAt(t, colorprofile.NoTTY, 40, 14, events...)
	press(t, m, "tab", "enter")
	rows := screenOf(m)
	for _, r := range rows {
		require.LessOrEqual(t, displayWidth(r), 40)
	}
	require.Contains(t, strings.ReplaceAll(strings.Join(rows, ""), " ", ""), long)
}

// --- streaming ---

func TestViewerFollowsTheEndOfARunningToolOnlyAtTheEnd(t *testing.T) {
	lines := func(n int) string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("line %d", i+1)
		}
		return strings.Join(out, "\n")
	}
	events := flat(
		&protocol.AgentStart{},
		userAt("go", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		toolStart("c1", "bash", `{"command":"long"}`, 0),
		resultUpdate("c1", "bash", lines(5), 50),
	)
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, events...)
	press(t, m, "tab")
	m.list.selected = toolID(t, m, "bash", 0)
	press(t, m, "enter")
	require.Regexp(t, `1–7 of 7$`, screenOf(m)[23], "command, empty row and five lines fit")

	update := func(n int) {
		// The translator of the model holds the running call; a fresh one
		// replaces the block with a longer output.
		blk := newToolCallBlock(toolCall{name: "bash", args: "long", raw: []byte(`{"command":"long"}`), startMs: baseMs}, ToolRunning, "", "",
			newToolOutput(lines(n), protocol.ToolResultDetails{}), NewTranscriptView())
		require.NoError(t, m.transcript.Replace(toolID(t, m, "bash", 0), blk))
		m.View()
	}
	// At the end: the window follows the growing output.
	update(40)
	require.Regexp(t, `23–42 of 42$`, screenOf(m)[23])
	require.Contains(t, strings.Join(screenOf(m), "\n"), "line 40")

	// Away from the end: the window stays, whatever the output does.
	press(t, m, "home")
	update(60)
	require.Regexp(t, `1–20 of 62$`, screenOf(m)[23])
	press(t, m, "end")
	update(70)
	require.Regexp(t, `53–72 of 72$`, screenOf(m)[23])
}

func TestViewerOfARunningToolShowsTheSpinnerAtTheRightEdge(t *testing.T) {
	events := flat(
		&protocol.AgentStart{},
		userAt("go", time.Date(2026, 10, 12, 15, 20, 0, 0, time.UTC)),
		toolStart("c1", "bash", `{"command":"sleep 9"}`, 0),
		resultUpdate("c1", "bash", "waiting", 50),
	)
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, events...)
	press(t, m, "tab", "enter")
	require.True(t, m.viewerOpen())
	row := screenOf(m)[0]
	require.Regexp(t, `^ ← back  Running sleep 9 +⠋$`, row)
	require.Equal(t, "    $ sleep 9", screenOf(m)[2])
	require.Equal(t, "    waiting", screenOf(m)[4])
}

func TestViewerDrawsTheBlockOnlyWhenItChanged(t *testing.T) {
	m := listModel(t, 80, 24)
	openTool(t, m, 0)
	counter := &countingViewerBlock{}
	require.NoError(t, m.transcript.Append("count", counter, true))
	m.viewer = viewerState{open: true, id: "count"}
	m.View()
	require.Equal(t, 1, counter.draws)
	m.View()
	press(t, m, "down", "up")
	require.Equal(t, 1, counter.draws, "a frame and a key draw nothing again")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m.View()
	require.Equal(t, 2, counter.draws, "a new width draws again")
	require.NoError(t, m.transcript.Replace("count", counter))
	m.View()
	require.Equal(t, 3, counter.draws, "new content draws again")
}

type countingViewerBlock struct{ draws int }

func (b *countingViewerBlock) Lines(RenderContext) []string { return []string{"x"} }
func (b *countingViewerBlock) viewerHead(RenderContext) (string, string, Role) {
	return "Counted", "", RoleFg
}
func (b *countingViewerBlock) viewerBody(RenderContext) []string {
	b.draws++
	return []string{"    body"}
}

// --- exit paths ---

func TestStopSignalLeavesTheAlternateScreen(t *testing.T) {
	r := startViewHistory(t, 80, 24)
	r.h.send(StopMsg{})
	res := r.exited()
	require.NoError(t, res.err)
	r.h.pump()
	require.False(t, r.h.emu.IsAltScreen(), "the program ended on the main screen")
	out := r.h.out.all()
	require.Greater(t, strings.LastIndex(out, "\x1b[?1049l"), strings.LastIndex(out, "\x1b[?1049h"))
	final := res.model.(*Model)
	require.False(t, final.View().AltScreen, "the last frame is an empty main-screen view")
	require.Equal(t, "", final.View().Content)
}

func TestFatalClientErrorLeavesTheAlternateScreen(t *testing.T) {
	r := startViewHistory(t, 80, 24)
	r.fc.push(ClientErrorMsg{Err: errNoLeader, Fatal: true})
	res := r.exited()
	require.NoError(t, res.err)
	r.h.pump()
	require.False(t, r.h.emu.IsAltScreen())
	require.ErrorIs(t, res.model.(*Model).ClientError(), errNoLeader)
}

// --- the viewer does not change the transcript ---

func TestViewerLeavesTheTranscriptAndTheFoldsAlone(t *testing.T) {
	m := listModel(t, 80, 24)
	n := m.Transcript().Len()
	press(t, m, "tab", "k")
	id := m.selectedBlock()
	fold := m.pane.Fold(id)
	press(t, m, "enter", "pgdown", "pgup", "esc")
	require.Equal(t, n, m.Transcript().Len())
	require.Equal(t, fold, m.pane.Fold(id))
}

// --- history of the session for the screen tests ---

// statusGoText is a read result of 172 lines.
func statusGoText() string {
	lines := make([]string, 172)
	for i := range lines {
		lines[i] = fmt.Sprintf("// source line %d", i+1)
	}
	lines[0] = "package tui"
	return strings.Join(lines, "\n")
}

// editedFile returns two versions of a file that differ in line 93 only.
func editedFile() (oldText, newText string) {
	lines := make([]string, 120)
	for i := range lines {
		lines[i] = fmt.Sprintf("row %d", i+1)
	}
	oldText = strings.Join(lines, "\n") + "\n"
	lines[92] = "row 93 changed"
	return oldText, strings.Join(lines, "\n") + "\n"
}

func bashLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("test output %d", i+1)
	}
	return strings.Join(lines, "\n")
}

// viewHistory is a session with a read, an edit, a bash call and a reply.
func viewHistory() []protocol.Event {
	oldText, newText := editedFile()
	diff := protocol.ToolResultDetails{Kind: protocol.ToolKindEdit, Diff: &protocol.ToolDiff{Path: "status.go", OldText: oldText, NewText: newText}}
	return flat(
		&protocol.AgentStart{},
		userAt("the status test fails, please fix it", sketchTime(15, 20)),
		toolStart("c1", "read", `{"path":"internal/tui/status.go"}`, 0),
		resultEnd("c1", "read", statusGoText(), protocol.ToolResultDetails{Kind: protocol.ToolKindRead}, false, 100),
		toolStart("c2", "edit", `{"path":"internal/tui/status.go"}`, 200),
		resultEnd("c2", "edit", "Edited internal/tui/status.go.", diff, false, 300),
		toolStart("c3", "bash", `{"command":"go test"}`, 400),
		resultEnd("c3", "bash", bashLines(12), protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, false, 500),
		assistantStart("deepseek-v4.1-flash", 600),
		textParts(0, "The copy note had no ok mark."),
		assistantEndUsage("deepseek-v4.1-flash", protocol.StopStop, 700, sketchUsage(), protocol.Text{Text: "The copy note had no ok mark."}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)
}

// startViewHistory starts a session of the given size and plays the history.
func startViewHistory(t *testing.T, width, height int) *viewRig {
	t.Helper()
	r := startView(t, width, height)
	r.event(viewHistory()...)
	r.h.waitText("The copy note had no ok mark.")
	r.h.settle()
	return r
}
