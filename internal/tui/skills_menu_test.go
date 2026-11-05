package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func skillRows(pairs ...string) protocol.ACPSkillsResult {
	var res protocol.ACPSkillsResult
	for i := 0; i+1 < len(pairs); i += 2 {
		res.Skills = append(res.Skills, protocol.ACPSkill{Name: pairs[i], Description: pairs[i+1], Path: "/s/" + pairs[i] + "/SKILL.md", Scope: protocol.ACPSkillUser, ModelInvocable: true})
	}
	return res
}

func skillRig(t *testing.T, skills protocol.ACPSkillsResult) *viewRig {
	t.Helper()
	fc := newFakeClient()
	fc.skills = skills
	r := startView(t, 100, 30, withFake(fc))
	r.waitCall("skills")
	r.h.settle()
	return r
}

// screenText is the whole screen as one string.
func (r *viewRig) screenText() string { return strings.Join(r.h.grid(), "\n") }

func TestMenuListsSkillsAfterTheBuiltInCommandsWithTheirDescriptions(t *testing.T) {
	r := skillRig(t, skillRows("demo", "A demo skill", "kk:plan", "Plan a feature"))
	r.typeText("/s")
	r.h.waitText("/skill:demo")

	requireRows(t, r.h.grid(), []string{
		fmt.Sprintf("  ❯ %-14s  %s", "/sessions", "Watch another live session"),
		fmt.Sprintf("    %-14s  %s", "/skill:demo", "A demo skill"),
		fmt.Sprintf("    %-14s  %s", "/skill:kk:plan", "Plan a feature"),
	})
}

func TestBuiltInCommandsStayFirstAndUnchangedWhenSkillsArrive(t *testing.T) {
	th := NewTheme(colorprofile.NoTTY)
	plain := func(e *Editor) []string { return plainRows(e.MenuRows(100, th)) }

	e := newTestEditor()
	typeText(t, e, "/")
	before := plain(e)
	require.Len(t, before, len(DefaultCommands()))

	e.SetSkills(skillCommands(skillRows("demo", "A demo skill").Skills))
	after := plain(e)
	require.Len(t, after, maxMenuRows)
	for i := range maxMenuRows - 1 {
		assert.Equal(t, strings.Fields(before[i])[0:1], strings.Fields(after[i])[0:1], "row %d is the same command", i)
	}
	assert.Equal(t, "  … 2 more", after[maxMenuRows-1])

	pressKeys(t, e, "up")
	rows := plain(e)
	assert.Contains(t, rows[len(rows)-2], "/skill:demo")
	assert.Contains(t, rows[len(rows)-2], "A demo skill")

	e.SetSkills(nil)
	e.Clear()
	typeText(t, e, "/")
	assert.Equal(t, before, plain(e), "no skills gives the old list")
}

func TestMenuFiltersSkillRowsByPrefix(t *testing.T) {
	r := skillRig(t, skillRows("demo", "A demo skill", "kk:plan", "Plan a feature", "kk:fix", "Fix a bug"))

	r.typeText("/sk")
	r.h.waitText("/skill:demo")
	assert.Zero(t, r.h.count("/model"), "no built-in command starts with /sk")
	assert.Equal(t, 1, r.h.count("/skill:kk:plan"))

	r.typeText("ill:kk")
	r.waitGone("/skill:demo")
	assert.Equal(t, 1, r.h.count("/skill:kk:plan"))
	assert.Equal(t, 1, r.h.count("/skill:kk:fix"))

	r.key("ctrl+u")
	r.typeText("/SKILL:DE")
	r.h.waitText("/skill:demo  A demo skill")
}

func TestEnterOnASkillRowFillsTheEditorAndSendsNothing(t *testing.T) {
	r := skillRig(t, skillRows("demo", "A demo skill", "other", "Other"))
	r.typeText("/skill:de")
	r.h.waitText("/skill:demo")
	r.key("enter")
	r.h.waitText("/skill:demo ")
	r.h.settle()

	assert.Zero(t, r.fc.count("prompt:"), "choosing a row sends nothing")
	assert.Zero(t, r.fc.count("steer:"))
	assert.Zero(t, r.h.count("A demo skill"), "the list closes after the space")

	r.typeText("fix it")
	r.key("enter")
	r.waitCall("prompt:/skill:demo fix it")
}

func TestEnterOnASelectedSkillRowFillsThatRow(t *testing.T) {
	r := skillRig(t, skillRows("alpha", "First", "beta", "Second"))
	r.typeText("/skill:")
	r.h.waitText("/skill:beta")
	r.key("down", "enter")
	r.h.waitText("/skill:beta ")
	assert.Zero(t, r.fc.count("prompt:"))
}

func TestBuiltInCommandsStillRunFromTheMenu(t *testing.T) {
	r := skillRig(t, skillRows("demo", "A demo skill"))
	r.typeText("/mod")
	r.h.waitText("/model")
	assert.Zero(t, r.h.count("/skill:demo"))
	r.key("enter")
	r.h.waitText("Select model")
}

func TestMenuHasOnlyBuiltInCommandsWhenTheLeaderPredatesSkills(t *testing.T) {
	fc := newFakeClient()
	fc.skillsErr = &RequestError{Kind: "unsupported", Message: "unsupported"}
	r := startView(t, 100, 30, withFake(fc))
	r.waitCall("skills")
	r.h.settle()

	r.typeText("/")
	r.h.waitText("/quit")
	screen := r.screenText()
	assert.NotContains(t, screen, "/skill:")
	assert.NotContains(t, strings.ToLower(screen), "unsupported")
	assert.NotContains(t, strings.ToLower(screen), "error")
}

func TestNewSessionReplacesTheSkillRows(t *testing.T) {
	r := skillRig(t, skillRows("first", "From the first session"))
	r.typeText("/sk")
	r.h.waitText("/skill:first")
	r.key("ctrl+u")

	r.fc.mu.Lock()
	r.fc.skills = skillRows("second", "From the second session")
	r.fc.mu.Unlock()
	r.typeText("/new")
	r.key("enter")
	r.waitCall("new_session")
	r.h.waitFor("the second skills read", func(*screenHarness) bool { return r.fc.count("skills") >= 2 })
	r.h.settle()

	r.typeText("/sk")
	r.h.waitText("/skill:second")
	assert.Zero(t, r.h.count("/skill:first"))
}

func TestLongSkillListScrollsWithTheSelection(t *testing.T) {
	var pairs []string
	for i := range 30 {
		pairs = append(pairs, fmt.Sprintf("skill%02d", i), fmt.Sprintf("Description %02d", i))
	}
	r := skillRig(t, skillRows(pairs...))
	r.typeText("/skill:")
	r.h.waitText("… 23 more")
	assert.Equal(t, 1, r.h.count("/skill:skill06"), "seven rows show")
	assert.Zero(t, r.h.count("/skill:skill07"))

	for range 20 {
		r.key("down")
	}
	r.h.waitText("/skill:skill20")
	assert.Positive(t, r.h.count("more"), "the count row stays")
	assert.Zero(t, r.h.count("/skill:skill00"), "the first rows scrolled out")
	r.key("enter")
	r.h.waitText("/skill:skill20 ")
}

func TestSkillRowsNeverCarryControlCharacters(t *testing.T) {
	r := skillRig(t, skillRows("demo", "red \x1b[31mtext\x1b[0m"))
	r.typeText("/skill:d")
	r.h.waitText("/skill:demo")
	assert.NotContains(t, r.screenText(), "\x1b")
}

func TestQueueRowShowsTheSkillCommandAsTyped(t *testing.T) {
	r := startView(t, 100, 30)
	r.event(&protocol.AgentStart{})
	body := protocol.SkillInput("demo", "/s/demo/SKILL.md", "DEMO STEPS", "go on")
	r.event(&protocol.QueueUpdate{Steering: []string{body}})
	r.h.waitText("┆ ⧗ steer   /skill:demo go on")
	assert.Zero(t, r.h.count("DEMO STEPS"))
}

func TestTranscriptShowsTheSkillPromptAsTyped(t *testing.T) {
	x := newTx(t)
	body := protocol.SkillInput("demo", "/s/demo/SKILL.md", "DEMO STEPS\nmore steps", "fix it\nthen test")
	x.feed(userEvents(body, 0)...)
	rows := x.rows()
	require.Contains(t, rows[0], "❯ /skill:demo fix it")
	require.Contains(t, rows[1], "then test")
	assert.NotContains(t, strings.Join(rows, "\n"), "DEMO STEPS")

	x = newTx(t)
	x.feed(userEvents("an ordinary prompt\n</skill>", 0)...)
	assert.Contains(t, x.rows()[0], "an ordinary prompt")
}
