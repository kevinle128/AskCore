package agent_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
)

func TestSectionTableIsPinned(t *testing.T) {
	t.Parallel()
	name := regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	rows := agent.SectionTable()
	require.NotEmpty(t, rows)
	seen := map[agent.SectionName]bool{}
	for i, r := range rows {
		assert.Regexp(t, name, string(r.Name))
		assert.False(t, seen[r.Name], "duplicate name %q", r.Name)
		seen[r.Name] = true
		assert.NotPanics(t, func() { agent.TextOf(r.Name, agent.PromptParts{}) }, "section %q has no text", r.Name)
		if i > 0 {
			assert.GreaterOrEqual(t, r.Order-rows[i-1].Order, 10, "%q follows %q", r.Name, rows[i-1].Name)
		}
	}
	assert.Equal(t, agent.SectionCwd, rows[len(rows)-2].Name)
	assert.Equal(t, agent.SectionDate, rows[len(rows)-1].Name)
}

func TestSystemPromptRendersSectionsInOrder(t *testing.T) {
	t.Parallel()
	got := agent.SystemPrompt(agent.PromptParts{
		Tools:    "TOOLS",
		Rules:    "RULES",
		Addendum: []string{"A1", "  ", "A2"},
		Cwd:      "/work",
		Date:     "2026-11-04",
	})
	parts := strings.Split(got, "\n\n")
	require.Len(t, parts, 8)
	assert.Contains(t, parts[0], "Ask")
	assert.Equal(t, []string{"TOOLS", "RULES"}, parts[1:3])
	assert.Contains(t, parts[3], "docs/cli.md")
	assert.Contains(t, parts[3], "docs/README.md")
	assert.Equal(t, []string{"A1", "A2"}, parts[4:6])
	assert.Contains(t, parts[6], "/work")
	assert.Contains(t, parts[7], "2026-11-04")
}

func TestSystemPromptPrefixIgnoresCwdAndDate(t *testing.T) {
	t.Parallel()
	base := agent.PromptParts{Tools: "TOOLS", Rules: "RULES", Addendum: []string{"more"}}
	a, b := base, base
	a.Cwd, a.Date = "/one", "2026-11-04"
	b.Cwd, b.Date = "/two/deeper", "2027-01-31"
	pa, pb := agent.SystemPrompt(a), agent.SystemPrompt(b)
	require.NotEqual(t, pa, pb)

	cwd := "\n\n" + agent.TextOf(agent.SectionCwd, a)
	prefix := pa[:strings.Index(pa, cwd)]
	require.NotEmpty(t, prefix)
	assert.True(t, strings.HasPrefix(pb, prefix+"\n\n"))
	assert.Equal(t, agent.SystemPrompt(base), prefix)
}

func TestSystemPromptCustomDropsBaselineSections(t *testing.T) {
	t.Parallel()
	got := agent.SystemPrompt(agent.PromptParts{
		Custom:   "my prompt",
		Tools:    "TOOLS",
		Rules:    "RULES",
		Addendum: []string{"extra"},
		Cwd:      "/work",
		Date:     "2026-11-04",
	})
	assert.Equal(t, "my prompt\n\nextra\n\nCurrent working directory: /work\n\nCurrent date: 2026-11-04", got)
}

func TestSystemPromptBlankCustomIsIgnored(t *testing.T) {
	t.Parallel()
	want := agent.SystemPrompt(agent.PromptParts{Tools: "TOOLS", Rules: "RULES"})
	for _, custom := range []string{"", " \n\t "} {
		assert.Equal(t, want, agent.SystemPrompt(agent.PromptParts{Custom: custom, Tools: "TOOLS", Rules: "RULES"}), "custom %q", custom)
	}
}

func TestSystemPromptTextOfUnknownSectionPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { agent.TextOf("nope", agent.PromptParts{}) })
}
