package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// fileHarness runs the file tools the way the coordinator does: the tool's
// argument preparer, then registry validation and coercion, then Execute.
type fileHarness struct {
	t   *testing.T
	reg *tools.Registry
	set map[string]tools.Tool
	fs  *tools.FileState
	cwd string
}

func newFileHarness(t *testing.T, modelInput func() []string) *fileHarness {
	t.Helper()
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	h := &fileHarness{t: t, reg: &tools.Registry{}, set: map[string]tools.Tool{}, fs: tools.NewFileState(), cwd: cwd}
	for _, tool := range tools.NewFileTools(h.fs, modelInput) {
		require.NoError(t, h.reg.Register(tool, tools.SourceInfo{Kind: tools.SourceBuiltin}))
		h.set[tool.Decl().Name] = tool
	}
	return h
}

// runCtx calls the named tool with args, which is a JSON string or a value.
func (h *fileHarness) runCtx(ctx context.Context, name string, args any) (protocol.ToolExecutionResult, error) {
	h.t.Helper()
	var raw json.RawMessage
	switch v := args.(type) {
	case string:
		raw = json.RawMessage(v)
	default:
		b, err := json.Marshal(v)
		require.NoError(h.t, err)
		raw = b
	}
	tool, ok := h.set[name]
	require.True(h.t, ok, "tool %s", name)
	if ap, ok := tool.(tools.ArgumentPreparer); ok {
		var err error
		raw, err = ap.PrepareArguments(raw)
		require.NoError(h.t, err)
	}
	prepared, err := h.reg.Prepare(name, raw)
	require.NoError(h.t, err)
	return tool.Execute(ctx, tools.Context{CallID: "c1", Cwd: h.cwd}, prepared)
}

func (h *fileHarness) run(name string, args any) (protocol.ToolExecutionResult, error) {
	h.t.Helper()
	return h.runCtx(context.Background(), name, args)
}

func (h *fileHarness) mustRun(name string, args any) protocol.ToolExecutionResult {
	h.t.Helper()
	res, err := h.run(name, args)
	require.NoError(h.t, err)
	return res
}

// path joins rel with the working directory.
func (h *fileHarness) path(rel string) string { return filepath.Join(h.cwd, rel) }

func (h *fileHarness) put(rel, content string) string {
	h.t.Helper()
	p := h.path(rel)
	require.NoError(h.t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(h.t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func (h *fileHarness) content(rel string) string {
	h.t.Helper()
	b, err := os.ReadFile(h.path(rel))
	require.NoError(h.t, err)
	return string(b)
}

func resultText(t *testing.T, res protocol.ToolExecutionResult) string {
	t.Helper()
	var parts []string
	for _, b := range res.Content {
		if tb, ok := b.(protocol.Text); ok {
			parts = append(parts, tb.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func resultImages(res protocol.ToolExecutionResult) []protocol.Image {
	var out []protocol.Image
	for _, b := range res.Content {
		if im, ok := b.(protocol.Image); ok {
			out = append(out, im)
		}
	}
	return out
}

func resultDetails(t *testing.T, res protocol.ToolExecutionResult) protocol.ToolResultDetails {
	t.Helper()
	var d protocol.ToolResultDetails
	require.NotEmpty(t, res.Details, "result has no details")
	require.NoError(t, json.Unmarshal(res.Details, &d))
	return d
}
