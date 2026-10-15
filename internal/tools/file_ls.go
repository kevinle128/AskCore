package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

// defaultLsLimit is the number of entries that ls returns when the model gives
// no limit.
const defaultLsLimit = 500

// Ls lists the entries of one directory.
type Ls struct{}

type lsInput struct {
	Path  string   `json:"path"`
	Limit *float64 `json:"limit"`
}

func (*Ls) ConcurrencySafe(json.RawMessage) bool { return true }

func (*Ls) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "ls",
		Description: fmt.Sprintf("List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. "+
			"Includes dotfiles. Output is truncated to %d entries or %dKB (whichever is hit first).", defaultLsLimit, DefaultMaxBytes/1024),
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"Directory to list (default: current directory)"},` +
			`"limit":{"type":"number","description":"Maximum number of entries to return (default: 500)"}}}`),
	}
}

func (*Ls) PromptSnippet() string { return "List directory contents" }

func (*Ls) PromptGuidelines() []string { return nil }

func (*Ls) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in lsInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("ls: %w", err)
	}
	given := in.Path
	if given == "" {
		given = "."
	}
	abs, err := workspace.ResolvePath(tc.Cwd, given)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("ls: %w", err)
	}
	abs, info, err := locateExisting(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return protocol.ToolExecutionResult{}, failf("Path not found: %s", abs)
		}
		return protocol.ToolExecutionResult{}, failf("Cannot read directory: %s", reason(err))
	}
	if !info.IsDir() {
		return protocol.ToolExecutionResult{}, failf("Not a directory: %s", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return protocol.ToolExecutionResult{}, failf("Cannot read directory: %s", reason(err))
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	slices.SortFunc(names, func(a, b string) int {
		if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})

	limit := defaultLsLimit
	if in.Limit != nil && *in.Limit >= 1 {
		limit = int(math.Min(*in.Limit, math.MaxInt32))
	}
	var results []string
	limitReached := false
	for _, name := range names {
		if len(results) >= limit {
			limitReached = true
			break
		}
		if err := ctx.Err(); err != nil {
			return protocol.ToolExecutionResult{}, err
		}
		st, err := os.Stat(filepath.Join(abs, name))
		if err != nil {
			continue // a broken link or an entry that vanished
		}
		if st.IsDir() {
			name += "/"
		}
		results = append(results, name)
	}

	details := protocol.ToolResultDetails{Kind: protocol.ToolKindSearch, Path: abs}
	if len(results) == 0 {
		return protocol.ToolExecutionResult{
			Content: []protocol.UserBlock{protocol.Text{Text: "(empty directory)"}},
			Details: mustDetails(details),
		}, nil
	}
	// The entry limit already bounds the lines, so only bytes are cut here.
	head := TruncateHead(strings.Join(results, "\n"), math.MaxInt32, 0)
	out := head.Text
	var notices []string
	if limitReached {
		notices = append(notices, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", limit, limit*2))
	}
	if head.Truncated {
		notices = append(notices, fmt.Sprintf("%s limit reached", formatSize(DefaultMaxBytes)))
		details.Truncation = &head.Truncation
	}
	if len(notices) > 0 {
		out += "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: out}},
		Details: mustDetails(details),
	}, nil
}
