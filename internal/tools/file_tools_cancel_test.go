package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func TestFileToolsReturnOnCancelledContext(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "alpha\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := map[string]any{
		"read":  map[string]any{"path": "a.txt"},
		"write": map[string]any{"path": "b.txt", "content": "x"},
		"edit":  edit("a.txt", "alpha", "beta"),
		"ls":    map[string]any{},
	}
	for name, args := range calls {
		t.Run(name, func(t *testing.T) {
			res, err := h.runCtx(ctx, name, args)
			require.ErrorIs(t, err, context.Canceled)
			assert.Empty(t, res.Content)
		})
	}
	assert.Equal(t, "alpha\n", h.content("a.txt"))
	assert.NoFileExists(t, h.path("b.txt"))
}

func TestFileToolsDeclareConcurrencyAndPromptText(t *testing.T) {
	h := newFileHarness(t, nil)
	args := json.RawMessage(`{}`)

	for name, wantSafe := range map[string]bool{"read": true, "ls": true, "write": false, "edit": false} {
		safe, ok := h.set[name].(tools.ConcurrencySafe)
		assert.Equal(t, wantSafe, ok && safe.ConcurrencySafe(args), name)
	}
	for _, name := range []string{"read", "write", "edit", "ls"} {
		pc, ok := h.set[name].(tools.PromptContributor)
		require.True(t, ok, name)
		assert.NotEmpty(t, pc.PromptSnippet(), name)
	}
}

func TestFileToolsPromptSectionListsAllFour(t *testing.T) {
	h := newFileHarness(t, nil)

	section := tools.PromptSection(h.reg.Snapshot())

	for _, want := range []string{"- read: Read file contents", "- write: Create or overwrite files", "- ls: List directory contents", "- edit: "} {
		assert.Contains(t, section, want)
	}
	assert.Contains(t, section, "Use read to examine files instead of cat or sed.")
}
