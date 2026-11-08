package tools_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

type promptTool struct {
	stubTool
	snippet    string
	guidelines []string
}

func (p promptTool) PromptSnippet() string      { return p.snippet }
func (p promptTool) PromptGuidelines() []string { return p.guidelines }

func registerAll(t *testing.T, ts ...tools.Tool) *tools.Snapshot {
	t.Helper()
	var r tools.Registry
	for _, tool := range ts {
		require.NoError(t, r.Register(tool, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	}
	return r.Snapshot()
}

func TestPromptListAndGuidelinesCoverOnlyToolsWithSnippets(t *testing.T) {
	snap := registerAll(t,
		tools.Echo{},
		promptTool{stubTool: stub("read", `{"type":"object"}`), snippet: "Read file contents", guidelines: []string{"Use read to examine files"}},
		promptTool{stubTool: stub("quiet", `{"type":"object"}`), guidelines: []string{"never shown"}},
	)

	list, rules := tools.PromptList(snap), tools.PromptGuidelines(snap)
	assert.Equal(t, "Available tools:\n- read: Read file contents", list)
	assert.Equal(t, "Guidelines:\n- Use read to examine files", rules)
	assert.NotContains(t, list+rules, "echo")
	assert.NotContains(t, list+rules, "never shown")
}

func TestPromptListAndGuidelinesAreSortedByName(t *testing.T) {
	snap := registerAll(t,
		promptTool{stubTool: stub("write", `{"type":"object"}`), snippet: "Write a file", guidelines: []string{"B guideline", "shared"}},
		promptTool{stubTool: stub("edit", `{"type":"object"}`), snippet: "Edit a file", guidelines: []string{"A guideline", "shared"}},
		promptTool{stubTool: stub("bash", `{"type":"object"}`), snippet: "Run a command"},
	)

	assert.Equal(t, "Available tools:\n- bash: Run a command\n- edit: Edit a file\n- write: Write a file", tools.PromptList(snap))
	assert.Equal(t, "Guidelines:\n- A guideline\n- shared\n- B guideline", tools.PromptGuidelines(snap))
}

func TestPromptGuidelinesIsEmptyWithoutGuidelines(t *testing.T) {
	snap := registerAll(t, promptTool{stubTool: stub("bash", `{"type":"object"}`), snippet: "Run a command"})

	assert.Equal(t, "Available tools:\n- bash: Run a command", tools.PromptList(snap))
	assert.Empty(t, tools.PromptGuidelines(snap))
}

func TestPromptListAndGuidelinesAreEmptyWithoutSnippets(t *testing.T) {
	assert.Empty(t, tools.PromptList(nil))
	assert.Empty(t, tools.PromptGuidelines(nil))
	empty := registerAll(t, tools.Echo{})
	assert.Empty(t, tools.PromptList(empty))
	assert.Empty(t, tools.PromptGuidelines(empty))
}
