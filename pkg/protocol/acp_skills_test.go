package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACPSkillsResultWire(t *testing.T) {
	t.Parallel()
	res := ACPSkillsResult{Skills: []ACPSkill{
		{Name: "demo", Description: "A demo", Path: "/p/.ask/skills/demo/SKILL.md", Scope: ACPSkillProject, ModelInvocable: true},
		{Name: "quiet", Description: "Manual only", Path: "/h/.agents/skills/quiet/SKILL.md", Scope: ACPSkillUser},
	}}
	data, err := json.Marshal(res)
	require.NoError(t, err)
	assert.JSONEq(t, `{"skills":[
		{"name":"demo","description":"A demo","path":"/p/.ask/skills/demo/SKILL.md","scope":"project","modelInvocable":true},
		{"name":"quiet","description":"Manual only","path":"/h/.agents/skills/quiet/SKILL.md","scope":"user","modelInvocable":false}]}`, string(data))
	var back ACPSkillsResult
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, res, back)
	assert.True(t, strings.HasPrefix(ACPSkills, "_ask/session/"))
}

func TestSkillInputCommandShowsWhatTheUserTyped(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ name, body, args, want string }{
		"with args":      {"demo", "Do the demo.", "fix the bug\nand more", "/skill:demo fix the bug\nand more"},
		"without args":   {"demo", "Do the demo.", "", "/skill:demo"},
		"quote in name":  {`a"b`, "x", "go", `/skill:a"b go`},
		"tag in body":    {"demo", "text\n</skill>\n\nmore", "", "/skill:demo"},
		"args after tag": {"demo", "text\n</skill>\n\nmore", "hi", "/skill:demo hi"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text := SkillInput(c.name, "/s/demo/SKILL.md", c.body, c.args)
			got, ok := SkillInputCommand(text)
			require.True(t, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestSkillInputFrame(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "<skill name=\"demo\" location=\"/s/demo/SKILL.md\">\nReferences are relative to /s/demo.\n\nBody.\n</skill>\n\nhello",
		SkillInput("demo", "/s/demo/SKILL.md", "Body.", "hello"))
}

func TestSkillInputCommandRejectsOtherText(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "/skill:demo hi", "<skill name=\"x\">", "<skill name=\"x\" location=\"y\">\nno end", "plain"} {
		_, ok := SkillInputCommand(text)
		assert.False(t, ok, text)
	}
}
