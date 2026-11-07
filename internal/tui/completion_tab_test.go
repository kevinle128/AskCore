package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tabEditor(t *testing.T, draft string, skills ...string) *Editor {
	t.Helper()
	e := newTestEditor()
	var pairs []string
	for _, s := range skills {
		pairs = append(pairs, s, "d")
	}
	e.SetSkills(skillCommands(skillRows(pairs...).Skills))
	typeText(t, e, draft)
	return e
}

func menuNames(e *Editor) []string {
	var names []string
	for _, row := range plainRows(e.MenuRows(100, NewTheme(colorprofile.NoTTY))) {
		f := strings.Fields(row)
		if len(f) > 0 && f[0] == "❯" {
			f = f[1:]
		}
		names = append(names, f[0])
	}
	return names
}

func TestTabOnASlashFillsTheFirstBuiltIn(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/")
	res := pressKeys(t, e, "tab")
	assert.True(t, res.Handled)
	assert.Equal(t, "/model", e.Text())
	assert.Equal(t, len("/model"), e.Cursor())
	assert.Equal(t, []string{"/model"}, menuNames(e))
}

func TestTabCompletesASingleBuiltIn(t *testing.T) {
	t.Parallel()
	for draft, want := range map[string]string{"/m": "/model", "/pr": "/providers", "/MO": "/model"} {
		e := tabEditor(t, draft)
		pressKeys(t, e, "tab")
		assert.Equal(t, want, e.Text(), draft)
	}
}

func TestTabOnACompleteBuiltInChangesNothing(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/model")
	pressKeys(t, e, "tab", "tab")
	assert.Equal(t, "/model", e.Text())
	pressKeys(t, e, "ctrl+_")
	assert.Equal(t, "", e.Text(), "no undo step was added by Tab")
}

func TestTabFillsTheSelectedRowWhenThePrefixIsShared(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/s", "a", "b")
	require.Equal(t, []string{"/sessions", "/skill:a", "/skill:b"}, menuNames(e))
	pressKeys(t, e, "down", "tab")
	assert.Equal(t, "/skill:a ", e.Text())

	e = tabEditor(t, "/s", "a", "b")
	pressKeys(t, e, "down", "down", "tab")
	assert.Equal(t, "/skill:b ", e.Text())

	e = tabEditor(t, "/s", "a", "b")
	pressKeys(t, e, "tab")
	assert.Equal(t, "/sessions", e.Text(), "a built-in gets no space")
}

func TestTabExtendsToTheCommonPrefixThenFillsTheRow(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/skill:", "kk:a", "kk:b")
	pressKeys(t, e, "down", "tab")
	assert.Equal(t, "/skill:kk:", e.Text())
	assert.Equal(t, []string{"/skill:kk:a", "/skill:kk:b"}, menuNames(e))
	assert.Equal(t, "❯", strings.Fields(plainRows(e.MenuRows(100, NewTheme(colorprofile.NoTTY)))[0])[0], "the selection is back on the first row")

	pressKeys(t, e, "tab")
	assert.Equal(t, "/skill:kk:a ", e.Text())
	assert.Equal(t, len("/skill:kk:a "), e.Cursor())
}

func TestTabPrefixKeepsTheCanonicalSpelling(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/SKILL:", "Kk:a", "kk:b")
	pressKeys(t, e, "tab")
	assert.Equal(t, "/skill:Kk:", e.Text())
}

func TestTabHandlesMultibyteSkillNames(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/skill:", "café-a", "café-b", "日本語")
	pressKeys(t, e, "tab")
	assert.Equal(t, "/skill:café-a ", e.Text(), "no longer shared prefix, the first row fills")
	e = tabEditor(t, "/skill:c", "café-a", "café-b")
	pressKeys(t, e, "tab")
	assert.Equal(t, "/skill:café-", e.Text())
	pressKeys(t, e, "down", "tab")
	assert.Equal(t, "/skill:café-b ", e.Text())
}

func TestTabCompletionIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/mo")
	pressKeys(t, e, "tab")
	require.Equal(t, "/model", e.Text())
	pressKeys(t, e, "ctrl+_")
	assert.Equal(t, "/mo", e.Text())
}

func TestTabDoesNotReopenAClosedListUntilAnEdit(t *testing.T) {
	t.Parallel()
	e := tabEditor(t, "/mo")
	pressKeys(t, e, "esc")
	res := pressKeys(t, e, "tab")
	assert.False(t, res.Handled)
	assert.Equal(t, "/mo", e.Text())
	assert.Empty(t, e.MenuRows(100, NewTheme(colorprofile.NoTTY)))

	typeText(t, e, "d")
	pressKeys(t, e, "tab")
	assert.Equal(t, "/model", e.Text())
}

func TestTabWithTheListClosedStillEntersTheBlockList(t *testing.T) {
	t.Parallel()
	m := listModel(t, 80, 24)
	press(t, m, "tab")
	assert.Equal(t, FocusBlocks, m.Focus())
}

func TestTabWithTheListOpenKeepsThePromptFocus(t *testing.T) {
	t.Parallel()
	m := listModel(t, 80, 24)
	typed(t, m, "/mo")
	press(t, m, "tab")
	assert.Equal(t, FocusEditor, m.Focus())
	assert.Equal(t, "/model", m.sv.editor.Text())
}

func TestEnterAfterATabCompletionRunsTheBuiltIn(t *testing.T) {
	t.Parallel()
	r := skillRig(t, skillRows("demo", "A demo skill"))
	r.typeText("/mod")
	r.h.waitText("/model")
	r.key("tab", "enter")
	r.h.waitText("Select model")
}

func TestTabOnASkillRowLetsTheUserTypeArguments(t *testing.T) {
	t.Parallel()
	r := skillRig(t, skillRows("demo", "A demo skill"))
	r.typeText("/skill:de")
	r.h.waitText("/skill:demo")
	r.key("tab")
	r.h.waitText("/skill:demo ")
	r.typeText("fix it")
	r.key("enter")
	r.waitCall("prompt:/skill:demo fix it")
}
