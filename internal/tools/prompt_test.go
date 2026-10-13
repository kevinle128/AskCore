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

func TestPromptSectionListsOnlyToolsWithSnippets(t *testing.T) {
	snap := registerAll(t,
		tools.Echo{},
		promptTool{stubTool: stub("read", `{"type":"object"}`), snippet: "Read file contents", guidelines: []string{"Use read to examine files"}},
		promptTool{stubTool: stub("quiet", `{"type":"object"}`), guidelines: []string{"never shown"}},
	)

	got := tools.PromptSection(snap)
	assert.Equal(t, "Available tools:\n- read: Read file contents\n\nGuidelines:\n- Use read to examine files", got)
	assert.NotContains(t, got, "echo")
	assert.NotContains(t, got, "never shown")
}

func TestPromptSectionIsSortedByName(t *testing.T) {
	snap := registerAll(t,
		promptTool{stubTool: stub("write", `{"type":"object"}`), snippet: "Write a file", guidelines: []string{"B guideline", "shared"}},
		promptTool{stubTool: stub("edit", `{"type":"object"}`), snippet: "Edit a file", guidelines: []string{"A guideline", "shared"}},
		promptTool{stubTool: stub("bash", `{"type":"object"}`), snippet: "Run a command"},
	)

	want := "Available tools:\n- bash: Run a command\n- edit: Edit a file\n- write: Write a file\n\n" +
		"Guidelines:\n- A guideline\n- shared\n- B guideline"
	assert.Equal(t, want, tools.PromptSection(snap))
}

func TestPromptSectionIsEmptyWithoutSnippets(t *testing.T) {
	assert.Empty(t, tools.PromptSection(nil))
	assert.Empty(t, tools.PromptSection(registerAll(t, tools.Echo{})))
}
