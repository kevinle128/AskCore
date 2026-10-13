package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

// Write creates a file or replaces its content. It writes through a temp file,
// so a reader never sees half of the new content.
type Write struct {
	fs *FileState
}

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (*Write) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name:        "write",
		Description: "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"Path to the file to write (relative or absolute)"},` +
			`"content":{"type":"string","description":"Content to write to the file"}},` +
			`"required":["path","content"]}`),
	}
}

func (*Write) PromptSnippet() string { return "Create or overwrite files" }

func (*Write) PromptGuidelines() []string {
	return []string{"Use write only for new files or complete rewrites."}
}

func (w *Write) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in writeInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("write: %w", err)
	}
	abs, err := workspace.ResolvePath(tc.Cwd, in.Path)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("write: %w", err)
	}
	abs, _, err = locateExisting(abs)
	if err != nil && !os.IsNotExist(err) {
		return protocol.ToolExecutionResult{}, failf("Cannot write %s: %s.", in.Path, reason(err))
	}

	unlock := PathLock(abs)
	defer unlock()

	// The previous content feeds the generated-file guard and the diff.
	var previous []byte
	switch info, err := os.Stat(abs); {
	case err == nil && info.IsDir():
		return protocol.ToolExecutionResult{}, failf("Cannot write %s: it is a directory.", in.Path)
	case err == nil && !info.Mode().IsRegular():
		return protocol.ToolExecutionResult{}, failf("Cannot write %s: it is not a regular file.", in.Path)
	case err == nil:
		previous, err = os.ReadFile(abs)
		if err != nil {
			return protocol.ToolExecutionResult{}, failf("Cannot write %s: %s.", in.Path, reason(err))
		}
		if marker, ok := isGenerated(previous); ok {
			return protocol.ToolExecutionResult{}, failf("Refusing to overwrite %s: its first lines carry the generator marker %q. Change the generator input and run the generator instead.", in.Path, marker)
		}
	case !os.IsNotExist(err):
		return protocol.ToolExecutionResult{}, failf("Cannot write %s: %s.", in.Path, reason(err))
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return protocol.ToolExecutionResult{}, failf("Cannot write %s: %s.", in.Path, reason(err))
	}
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	if err := WriteFileAtomic(abs, []byte(in.Content), 0o644); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	if strings.HasPrefix(in.Content, "#!") {
		if err := makeExecutable(abs); err != nil {
			return protocol.ToolExecutionResult{}, failf("Wrote %s but could not make it executable: %s.", in.Path, reason(err))
		}
	}
	if info, err := os.Stat(abs); err == nil {
		w.fs.Record(abs, info)
	}

	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: "Successfully wrote to " + in.Path}},
		Details: mustDetails(protocol.ToolResultDetails{
			Kind:      protocol.ToolKindEdit,
			Path:      abs,
			Diff:      &protocol.ToolDiff{Path: abs, OldText: string(previous), NewText: in.Content},
			Locations: []protocol.ToolLocation{{Path: abs, Line: 1}},
		}),
	}, nil
}

// makeExecutable sets the execute bit of each class that has the read bit.
func makeExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	want := mode | (mode&0o444)>>2
	if want == mode {
		return nil
	}
	return os.Chmod(path, want)
}
