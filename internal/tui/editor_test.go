package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

// keyNamed builds the key message that a terminal reports for a key name such
// as "ctrl+j", "alt+enter" or "a".
func keyNamed(t testing.TB, name string) tea.KeyPressMsg {
	t.Helper()
	parts := strings.Split(name, "+")
	base := parts[len(parts)-1]
	var mod tea.KeyMod
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl":
			mod |= tea.ModCtrl
		case "alt":
			mod |= tea.ModAlt
		case "shift":
			mod |= tea.ModShift
		default:
			t.Fatalf("unknown modifier in %q", name)
		}
	}
	named := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab,
		"backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"home": tea.KeyHome, "end": tea.KeyEnd, "space": tea.KeySpace,
	}
	if code, ok := named[base]; ok {
		k := tea.KeyPressMsg{Code: code, Mod: mod}
		if base == "space" && mod == 0 {
			k.Text = " "
		}
		return k
	}
	runes := []rune(base)
	require.Len(t, runes, 1, "key %q", name)
	k := tea.KeyPressMsg{Code: runes[0], Mod: mod}
	if mod&^tea.ModShift == 0 {
		k.Text = base
	}
	return k
}

func TestKeyNamedMatchesTerminalNames(t *testing.T) {
	for _, name := range []string{"ctrl+j", "alt+enter", "shift+enter", "alt+b", "ctrl+w", "enter", "esc", "alt+up"} {
		require.Equal(t, name, keyNamed(t, name).String())
	}
}

// pressKeys sends key names to the editor and returns the last result.
func pressKeys(t testing.TB, e *Editor, names ...string) Result {
	t.Helper()
	var res Result
	for _, n := range names {
		res = e.Handle(keyNamed(t, n))
	}
	return res
}

// typeText types the text key by key; a line feed is ctrl+j.
func typeText(t testing.TB, e *Editor, s string) {
	t.Helper()
	for _, r := range s {
		if r == '\n' {
			pressKeys(t, e, "ctrl+j")
			continue
		}
		res := e.Handle(tea.KeyPressMsg{Code: r, Text: string(r)})
		require.True(t, res.Handled)
	}
}

func newTestEditor() *Editor {
	e := NewEditor(DefaultCommands())
	e.SetWidth(80)
	return e
}

func TestEditorKeysTable(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		keys       []string
		wantText   string
		wantCursor int
	}{
		{"left moves one grapheme", "a👍🏽b", []string{"left", "left"}, "a👍🏽b", 1},
		{"right moves one grapheme", "a👍🏽b", []string{"home", "right", "right"}, "a👍🏽b", len("a👍🏽")},
		{"home goes to line start", "ab\ncd", []string{"home"}, "ab\ncd", 3},
		{"ctrl+a goes to line start", "ab\ncd", []string{"ctrl+a"}, "ab\ncd", 3},
		{"end goes to line end", "ab\ncd", []string{"ctrl+a", "end"}, "ab\ncd", 5},
		{"ctrl+e goes to line end", "ab\ncd", []string{"home", "ctrl+e"}, "ab\ncd", 5},
		{"alt+b moves back one word", "foo bar baz", []string{"alt+b"}, "foo bar baz", 8},
		{"alt+b twice", "foo bar baz", []string{"alt+b", "alt+b"}, "foo bar baz", 4},
		{"alt+f moves forward one word", "foo bar baz", []string{"home", "alt+f", "alt+f"}, "foo bar baz", 7},
		{"backspace deletes one grapheme", "a👍🏽", []string{"backspace"}, "a", 1},
		{"delete removes under cursor", "abc", []string{"home", "delete"}, "bc", 0},
		{"ctrl+d deletes under cursor when draft not empty", "abc", []string{"home", "ctrl+d"}, "bc", 0},
		{"ctrl+w kills previous word", "foo bar", []string{"ctrl+w"}, "foo ", 4},
		{"ctrl+u kills to line start", "ab\ncd", []string{"ctrl+u"}, "ab\n", 3},
		{"ctrl+u at line start joins lines", "ab\ncd", []string{"ctrl+a", "ctrl+u"}, "abcd", 2},
		{"ctrl+k kills to line end", "ab\ncd", []string{"up", "ctrl+a", "right", "ctrl+k"}, "a\ncd", 1},
		{"ctrl+k at line end kills the line feed", "ab\ncd", []string{"up", "ctrl+e", "ctrl+k"}, "abcd", 2},
		{"ctrl+y yanks the last kill", "foo bar", []string{"ctrl+w", "ctrl+y"}, "foo bar", 7},
		{"consecutive kills join into one entry", "a b c", []string{"ctrl+w", "ctrl+w", "ctrl+y"}, "a b c", 5},
		{"ctrl+- undoes a typed run", "hello", []string{"ctrl+-"}, "", 0},
		{"ctrl+_ is the same key on legacy terminals", "hello", []string{"ctrl+_"}, "", 0},
		{"undo restores a kill", "foo bar", []string{"ctrl+w", "ctrl+-"}, "foo bar", 7},
		{"up keeps the column", "abcd\nef", []string{"up"}, "abcd\nef", 2},
		{"down keeps the sticky column", "abcd\nef\nghij", []string{"up", "up", "ctrl+e", "down", "down"}, "abcd\nef\nghij", 12},
		{"up then down returns to the start column", "abcd\nef", []string{"home", "up", "down"}, "abcd\nef", 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEditor()
			typeText(t, e, tc.text)
			pressKeys(t, e, tc.keys...)
			require.Equal(t, tc.wantText, e.Text())
			require.Equal(t, tc.wantCursor, e.Cursor())
		})
	}
}

func TestEditorLeavesHostKeysAlone(t *testing.T) {
	// These keys belong to the host or to the application; the editor must
	// report them as not handled and leave the draft alone.
	for _, k := range []string{"ctrl+c", "esc", "alt+up", "ctrl+l", "ctrl+t", "shift+tab", "ctrl+d"} {
		t.Run(k, func(t *testing.T) {
			e := newTestEditor()
			rev := e.Revision()
			res := pressKeys(t, e, k)
			require.False(t, res.Handled)
			require.Equal(t, rev, e.Revision())
		})
	}
	t.Run("ctrl+d with a draft is an edit", func(t *testing.T) {
		e := newTestEditor()
		typeText(t, e, "x")
		pressKeys(t, e, "home")
		require.True(t, pressKeys(t, e, "ctrl+d").Handled)
		require.Empty(t, e.Text())
	})
}

func TestEditorEnterProducesSubmitIntents(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "hello")
	res := pressKeys(t, e, "enter")
	require.NotNil(t, res.Submit)
	require.Equal(t, "hello", res.Submit.Text)
	require.False(t, res.Submit.Alt)
	require.Equal(t, "hello", e.Text(), "the draft stays until the host accepts")

	res = pressKeys(t, e, "alt+enter")
	require.NotNil(t, res.Submit)
	require.True(t, res.Submit.Alt)
	require.Equal(t, "hello", e.Text(), "alt+enter never inserts a new line")

	empty := newTestEditor()
	typeText(t, empty, " \n ")
	res = pressKeys(t, empty, "enter")
	require.True(t, res.Handled)
	require.Nil(t, res.Submit, "a blank draft is not a prompt")
}

func TestDraftRevisionChangesOnEdit(t *testing.T) {
	e := newTestEditor()
	last := e.Revision()
	step := func(what string, f func()) {
		t.Helper()
		f()
		require.Greater(t, e.Revision(), last, what)
		last = e.Revision()
	}
	step("typing", func() { typeText(t, e, "a") })
	step("second character", func() { typeText(t, e, "b") })
	step("new line", func() { pressKeys(t, e, "ctrl+j") })
	step("backspace", func() { pressKeys(t, e, "backspace") })
	step("kill", func() { pressKeys(t, e, "ctrl+w") })
	step("yank", func() { pressKeys(t, e, "ctrl+y") })
	step("paste", func() { e.Handle(tea.PasteMsg{Content: "x"}) })
	step("undo", func() { pressKeys(t, e, "ctrl+-") })
	step("clear", func() { e.Clear() })
	step("restore queued", func() { e.RestoreQueued([]string{"q"}) })

	for _, k := range []string{"left", "right", "home", "end", "up", "down", "alt+b", "alt+f"} {
		pressKeys(t, e, k)
		require.Equal(t, last, e.Revision(), "%s moves only the cursor", k)
	}
}

func TestAcceptClearsOnlyUneditedDraft(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "first")
	sub := pressKeys(t, e, "enter").Submit
	require.NotNil(t, sub)

	typeText(t, e, "!")
	require.False(t, e.Accept(*sub), "an edited draft is kept")
	require.Equal(t, "first!", e.Text())

	sub = pressKeys(t, e, "enter").Submit
	require.True(t, e.Accept(*sub))
	require.Empty(t, e.Text())
	require.Equal(t, 0, e.Cursor())

	pressKeys(t, e, "ctrl+-")
	require.Equal(t, "first!", e.Text(), "undo restores a cleared draft")
}

func TestPromptHistory(t *testing.T) {
	e := newTestEditor()
	for _, p := range []string{"one", "two", "two", "three"} {
		typeText(t, e, p)
		sub := pressKeys(t, e, "enter").Submit
		require.True(t, e.Accept(*sub))
	}
	typeText(t, e, "draft")

	pressKeys(t, e, "up")
	require.Equal(t, "three", e.Text())
	pressKeys(t, e, "up")
	require.Equal(t, "two", e.Text())
	pressKeys(t, e, "up")
	require.Equal(t, "one", e.Text(), "consecutive duplicates are stored once")
	pressKeys(t, e, "up")
	require.Equal(t, "one", e.Text(), "the oldest entry stays")
	pressKeys(t, e, "down", "down")
	require.Equal(t, "three", e.Text())
	pressKeys(t, e, "down")
	require.Equal(t, "draft", e.Text(), "walking past the newest entry restores the draft")
}

func TestHistoryWalksOnlyAtFirstOrLastRow(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "old")
	e.Accept(*pressKeys(t, e, "enter").Submit)
	typeText(t, e, "a\nb")
	pressKeys(t, e, "up")
	require.Equal(t, "a\nb", e.Text(), "up moves inside a multi-line draft first")
	pressKeys(t, e, "up")
	require.Equal(t, "old", e.Text())
}

func TestRestoreQueuedPrependsToDraft(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "tail")
	e.RestoreQueued([]string{"one", "two"})
	require.Equal(t, "one\ntwo\ntail", e.Text())
	require.Equal(t, len(e.Text()), e.Cursor())
	e.RestoreQueued(nil)
	require.Equal(t, "one\ntwo\ntail", e.Text())
}

func TestPasteInsertsOnceAndNormalizesLineEnds(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "x")
	rev := e.Revision()
	res := e.Handle(tea.PasteMsg{Content: "a\r\nb\rc\x1b[31m"})
	require.True(t, res.Handled)
	require.Equal(t, "xa\nb\nc[31m", e.Text(), "control characters never enter the draft")
	require.Equal(t, rev+1, e.Revision(), "one paste is one edit")
	pressKeys(t, e, "ctrl+-")
	require.Equal(t, "x", e.Text(), "one undo removes the whole paste")
}

func TestPasteMarkerThreshold(t *testing.T) {
	lines := func(n int) string {
		rows := make([]string, n)
		for i := range rows {
			rows[i] = fmt.Sprintf("row %d", i+1)
		}
		return strings.Join(rows, "\r\n")
	}
	e := newTestEditor()
	e.Handle(tea.PasteMsg{Content: lines(10)})
	require.Equal(t, 10, strings.Count(e.Text(), "\n")+1, "ten lines are pasted as they are")
	e.Clear()

	e.Handle(tea.PasteMsg{Content: lines(11)})
	require.Equal(t, "[paste #1, 11 lines]", e.Text())
	e.Handle(tea.PasteMsg{Content: lines(12)})
	require.Equal(t, "[paste #1, 11 lines][paste #2, 12 lines]", e.Text())
}

func TestPasteMarkerExpandsOnSubmit(t *testing.T) {
	pasted := make([]string, 12)
	for i := range pasted {
		pasted[i] = fmt.Sprintf("line %d", i+1)
	}
	full := strings.Join(pasted, "\n")

	e := newTestEditor()
	typeText(t, e, "see: ")
	e.Handle(tea.PasteMsg{Content: strings.Join(pasted, "\r\n")})
	require.Equal(t, "see: [paste #1, 12 lines]", e.Text())

	sub := pressKeys(t, e, "enter").Submit
	require.NotNil(t, sub)
	require.Equal(t, "see: "+full, sub.Text, "the full text is submitted")
	require.True(t, e.Accept(*sub))
	require.Empty(t, e.Text())

	pressKeys(t, e, "ctrl+-")
	require.Equal(t, "see: [paste #1, 12 lines]", e.Text(), "undo restores the marker")
	sub = pressKeys(t, e, "enter").Submit
	require.Equal(t, "see: "+full, sub.Text, "the restored marker still expands")
}

func TestPasteMarkerIsOneUnit(t *testing.T) {
	big := strings.Repeat("l\n", 20)
	e := newTestEditor()
	typeText(t, e, "a")
	e.Handle(tea.PasteMsg{Content: big})
	typeText(t, e, "b")
	marker := "[paste #1, 21 lines]"
	require.Equal(t, "a"+marker+"b", e.Text())

	pressKeys(t, e, "left", "left")
	require.Equal(t, 1, e.Cursor(), "one left press passes the whole marker")
	pressKeys(t, e, "right")
	require.Equal(t, 1+len(marker), e.Cursor())
	pressKeys(t, e, "backspace")
	require.Equal(t, "ab", e.Text(), "backspace deletes the whole marker")
}

func TestTypedMarkerTextIsNotExpanded(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "[paste #7, 40 lines]")
	sub := pressKeys(t, e, "enter").Submit
	require.Equal(t, "[paste #7, 40 lines]", sub.Text)
}

func TestControlCharactersNeverEnterTheDraft(t *testing.T) {
	e := newTestEditor()
	res := e.Handle(tea.KeyPressMsg{Code: 'x', Text: "a\x1bb"})
	require.True(t, res.Handled)
	require.Equal(t, "ab", e.Text())
}

func TestSlashCommandRunsWhenDraftIsExactCommand(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "/new")
	res := pressKeys(t, e, "esc") // closes the list; esc is the host's afterwards
	require.True(t, res.Handled)
	rev := e.Revision()
	res = pressKeys(t, e, "enter")
	require.Equal(t, "/new", res.Command)
	require.Nil(t, res.Submit)
	require.Empty(t, e.Text())
	require.Greater(t, e.Revision(), rev)

	typeText(t, e, "/model please")
	res = pressKeys(t, e, "enter")
	require.Empty(t, res.Command, "a command with extra text is a prompt")
	require.NotNil(t, res.Submit)
}

func TestEditorSlotRowsFitWidth(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	texts := []string{
		"short",
		strings.Repeat("word ", 40),
		strings.Repeat("x", 300),
		"日本語のテキストが長く続く場合でも列の幅を超えてはいけません",
		"tab\tseparated\ttext with 👍🏽 emoji and é marks",
		"a\n\n\nb\n",
		"こ" + strings.Repeat("a", 37) + "ん",
	}
	for _, width := range []int{8, 10, 24, 40, 80} {
		for _, text := range texts {
			e := NewEditor(DefaultCommands())
			e.SetWidth(width)
			e.Handle(tea.PasteMsg{Content: text})
			slot := e.Slot(width, th)
			inner := BoxInnerWidth(width)
			for _, row := range slot.Rows {
				require.LessOrEqual(t, displayWidth(row), inner, "width %d row %q", width, row)
			}
			require.NotNil(t, slot.Cursor)
			require.Less(t, slot.Cursor.Row, len(slot.Rows))
			require.LessOrEqual(t, slot.Cursor.Col, inner, "cursor stays on the row")
		}
	}
}

func TestEditorSlotPlaceholderAndPrompt(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	e := newTestEditor()
	slot := e.Slot(80, th)
	require.Equal(t, []string{"❯ Ask anything  ·  / commands  ·  ctrl+l model"}, plainRows(slot.Rows))
	require.Equal(t, &CursorPos{Row: 0, Col: 2}, slot.Cursor)
	require.Equal(t, []string{"❯ Ask anything"}, plainRows(e.Slot(50, th).Rows), "narrow placeholder is short")

	typeText(t, e, "one\ntwo")
	slot = e.Slot(80, th)
	require.Equal(t, []string{"❯ one", "  two"}, plainRows(slot.Rows))
	require.Equal(t, &CursorPos{Row: 1, Col: 5}, slot.Cursor)
}

func TestEditorSlotWrapsWords(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	e := NewEditor(nil)
	width := 4 + 2 + 8 // inner 10, text 8 columns
	e.SetWidth(width)
	typeText(t, e, "hello world")
	slot := e.Slot(width, th)
	require.Equal(t, []string{"❯ hello ", "  world"}, plainRows(slot.Rows))
	require.Equal(t, &CursorPos{Row: 1, Col: 7}, slot.Cursor)

	typeText(t, e, strings.Repeat("z", 9))
	slot = e.Slot(width, th)
	for _, r := range slot.Rows {
		require.LessOrEqual(t, displayWidth(r), 10)
	}
}

func TestEditorMovesAcrossWrappedRows(t *testing.T) {
	e := NewEditor(nil)
	e.SetWidth(4 + 2 + 8)
	typeText(t, e, "hello world")
	pressKeys(t, e, "up")
	require.Equal(t, 5, e.Cursor(), "up moves to the same column of the row above")
	pressKeys(t, e, "down")
	require.Equal(t, 11, e.Cursor())
}

// editorApp runs the editor inside the real root model and a real Bubble Tea
// program. It routes key and paste messages to the editor and everything else
// to the model, which is the routing that the session view does.
type editorApp struct {
	*Model
	ed      *Editor
	results []Result
}

func newEditorApp() *editorApp {
	a := &editorApp{ed: NewEditor(DefaultCommands())}
	a.Model = New(Options{Slots: a, Profile: colorprofile.NoTTY, SettleDelay: 20 * time.Millisecond})
	return a
}

func (a *editorApp) Slots(width int) Slots {
	return Slots{
		Editor: a.ed.Slot(width, a.Theme()),
		Menu:   a.ed.MenuRows(width, a.Theme()),
		Footer: "  footer",
	}
}

func (a *editorApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg:
		a.results = append(a.results, a.ed.Handle(msg))
		return a, nil
	case tea.WindowSizeMsg:
		a.ed.SetWidth(msg.Width)
	}
	_, cmd := a.Model.Update(msg)
	return a, cmd
}

func (a *editorApp) sendKeys(h *screenHarness, t testing.TB, names ...string) {
	for _, n := range names {
		h.send(keyNamed(t, n))
	}
}

func startEditorApp(t *testing.T, width, height int) (*editorApp, *screenHarness) {
	t.Helper()
	app := newEditorApp()
	app.ed.SetWidth(width)
	return app, startScreen(t, app, width, height)
}

func TestEditorNewlineKeys(t *testing.T) {
	app, h := startEditorApp(t, 60, 14)
	h.send(tea.KeyPressMsg{Code: 'a', Text: "a"})
	app.sendKeys(h, t, "ctrl+j")
	h.send(tea.KeyPressMsg{Code: 'b', Text: "b"})
	app.sendKeys(h, t, "shift+enter")
	h.send(tea.KeyPressMsg{Code: 'c', Text: "c"})
	h.waitText("  c")
	app.sendKeys(h, t, "alt+enter")
	h.send(tea.KeyPressMsg{Code: 'd', Text: "d"})
	h.waitText("  cd")

	h.stop()
	require.Equal(t, "a\nb\ncd", app.ed.Text(), "alt+enter inserts no line feed")
	var alt int
	for _, r := range app.results {
		if r.Submit != nil && r.Submit.Alt {
			alt++
			require.Equal(t, "a\nb\nc", r.Submit.Text)
		}
	}
	require.Equal(t, 1, alt)
}

func TestCompletionFiltersAndRuns(t *testing.T) {
	app, h := startEditorApp(t, 80, 16)
	h.send(tea.KeyPressMsg{Code: '/', Text: "/"})
	h.waitText("/model      Select a model")
	for _, want := range []string{"/new ", "/sessions   Watch another live session", "/take ", "/quit "} {
		require.GreaterOrEqual(t, h.count(want), 1, want)
	}
	require.GreaterOrEqual(t, h.count("❯ /model"), 1, "the first command is selected")

	h.send(tea.KeyPressMsg{Code: 's', Text: "s"})
	h.waitFor("filtered list", func(h *screenHarness) bool { return h.count("/model") == 0 })
	require.GreaterOrEqual(t, h.count("/sessions"), 1)
	require.Zero(t, h.count("/quit"))

	app.sendKeys(h, t, "esc")
	h.waitFor("closed list", func(h *screenHarness) bool { return h.count("/sessions") == 0 })
	require.GreaterOrEqual(t, h.count("❯ /s"), 1, "esc keeps the draft")

	app.sendKeys(h, t, "backspace", "down", "down", "down", "enter")
	h.stop()
	require.Empty(t, app.ed.Text(), "a command clears the draft")
	last := app.results[len(app.results)-1]
	require.Equal(t, "/sessions", last.Command, "enter runs the selected command")
	require.Nil(t, last.Submit)
}

func TestCompletionClosesOnSpaceAndNoMatch(t *testing.T) {
	_, h := startEditorApp(t, 80, 16)
	h.send(tea.KeyPressMsg{Code: '/', Text: "/"})
	h.waitText("/take")
	h.send(tea.KeyPressMsg{Code: 'x', Text: "x"})
	h.waitFor("no match", func(h *screenHarness) bool { return h.count("/take") == 0 })

	h.send(keyNamed(t, "backspace"))
	h.waitText("/take")
	h.send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	h.send(tea.KeyPressMsg{Code: ' ', Text: " "})
	h.waitFor("space", func(h *screenHarness) bool { return h.count("/quit") == 0 })
}

func TestCompletionOnlyAtDraftStart(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "hi /")
	require.Empty(t, e.MenuRows(80, NewTheme(colorprofile.NoTTY)))
	e.Clear()
	typeText(t, e, "/")
	require.Len(t, e.MenuRows(80, NewTheme(colorprofile.NoTTY)), len(DefaultCommands()))
}

func TestCompletionArrowKeysStayInEditorWhenClosed(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "a\nb")
	pressKeys(t, e, "up")
	require.Equal(t, 1, e.Cursor())
}

func TestPasteMarkerShownOnScreen(t *testing.T) {
	app, h := startEditorApp(t, 60, 14)
	h.send(tea.PasteMsg{Content: strings.Repeat("line\r\n", 41) + "end"})
	h.waitText("[paste #1, 42 lines]")
	app.sendKeys(h, t, "enter")
	h.stop()
	last := app.results[len(app.results)-1]
	require.NotNil(t, last.Submit)
	require.Equal(t, strings.Repeat("line\n", 41)+"end", last.Submit.Text)
}

func TestEditorGrowsThenScrolls(t *testing.T) {
	typeLines := func(app *editorApp, h *screenHarness, n int) {
		for i := 1; i <= n; i++ {
			if i > 1 {
				app.sendKeys(h, t, "ctrl+j")
			}
			for _, r := range fmt.Sprintf("line %d", i) {
				h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
	t.Run("grows", func(t *testing.T) {
		app, h := startEditorApp(t, 40, 20)
		typeLines(app, h, 3)
		h.waitText("  line 3")
		h.requireGolden()
	})
	t.Run("scrolls", func(t *testing.T) {
		app, h := startEditorApp(t, 40, 20)
		typeLines(app, h, 9)
		h.waitText("  line 9")
		h.requireGolden()
		require.Zero(t, h.count("❯ line 1"))
	})
}

func TestEditorHardwareCursorFollowsText(t *testing.T) {
	_, h := startEditorApp(t, 40, 12)
	for _, r := range "日本a" {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	h.waitText("❯ 日本a")
	var row int
	h.waitFor("cursor on the draft row", func(h *screenHarness) bool {
		for i, g := range h.grid() {
			if strings.Contains(g, "❯ 日本a") {
				row = i
			}
		}
		pos := h.emu.CursorPosition()
		return pos.Y == row && row > 0
	})
	// "│ ❯ " is 4 columns, "日本a" is 5.
	require.Equal(t, 9, h.emu.CursorPosition().X)
}

func TestCompletionListGolden(t *testing.T) {
	_, h := startEditorApp(t, 80, 16)
	h.send(tea.KeyPressMsg{Code: '/', Text: "/"})
	h.waitText("/quit")
	h.requireGolden()
}

func TestEditorCommandNameCaseInsensitive(t *testing.T) {
	for _, typed := range []string{"/MODEL", "/Model", "/model"} {
		t.Run(typed, func(t *testing.T) {
			e := newTestEditor()
			typeText(t, e, typed)
			res := pressKeys(t, e, "enter")
			require.Equal(t, "/model", res.Command, "the list is open and shows the match")
			require.Empty(t, e.Text())
		})
		t.Run(typed+" after esc", func(t *testing.T) {
			e := newTestEditor()
			typeText(t, e, typed)
			require.True(t, pressKeys(t, e, "esc").Handled)
			require.Empty(t, e.MenuRows(80, NewTheme(colorprofile.NoTTY)))
			res := pressKeys(t, e, "alt+enter")
			require.Equal(t, "/model", res.Command, "a closed list still finds the command")
			require.Nil(t, res.Submit)
		})
	}

	t.Run("unknown name stays a prompt", func(t *testing.T) {
		e := newTestEditor()
		typeText(t, e, "/Foo")
		res := pressKeys(t, e, "enter")
		require.Empty(t, res.Command)
		require.NotNil(t, res.Submit)
		require.Equal(t, "/Foo", res.Submit.Text)
	})
	t.Run("arguments keep their case and the text stays a prompt", func(t *testing.T) {
		e := newTestEditor()
		typeText(t, e, "/Model Please KEEP")
		res := pressKeys(t, e, "enter")
		require.Empty(t, res.Command)
		require.Equal(t, "/Model Please KEEP", res.Submit.Text)
	})
}

func TestCompletionListShowsRowForUppercaseQuery(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "/MOD")
	rows := plainRows(e.MenuRows(80, NewTheme(colorprofile.NoTTY)))
	require.Len(t, rows, 1)
	require.Contains(t, rows[0], "/model")
}

func TestPartialCommandWithOpenListRunsHighlightedRow(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "/se")
	res := pressKeys(t, e, "enter")
	require.Equal(t, "/sessions", res.Command)
	require.Nil(t, res.Submit)
	require.Empty(t, e.Text())
}

func TestCompletionListClosesForMultiLineDraft(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "/a\nb")
	require.Empty(t, e.MenuRows(80, NewTheme(colorprofile.NoTTY)))
	e.Clear()
	typeText(t, e, "/\nb")
	require.Empty(t, e.MenuRows(80, NewTheme(colorprofile.NoTTY)))
}

func TestCommandWithTrailingSpaceStillRuns(t *testing.T) {
	e := newTestEditor()
	typeText(t, e, "/MODEL ")
	require.Empty(t, e.MenuRows(80, NewTheme(colorprofile.NoTTY)), "a space closes the list")
	res := pressKeys(t, e, "enter")
	require.Equal(t, "/model", res.Command)
	require.Nil(t, res.Submit)
}
