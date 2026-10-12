package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"AskCore/internal/sandbox"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

const (
	findDefaultLimit = 1000
	findTimeout      = 5 * time.Second
)

// Find lists files by glob pattern. It runs fd when fd is on PATH, else
// ripgrep with --files, else a walk in Go that applies .gitignore files.
type Find struct {
	// lookPath finds a program on PATH. Nil means exec.LookPath.
	lookPath func(string) (string, error)
	// timeout bounds one search. Zero means the default.
	timeout time.Duration
}

var _ ConcurrencySafe = Find{}
var _ PromptContributor = Find{}

func (f Find) look(name string) (string, error) {
	if f.lookPath != nil {
		return f.lookPath(name)
	}
	return exec.LookPath(name)
}

// ConcurrencySafe returns true because a search only reads.
func (Find) ConcurrencySafe(json.RawMessage) bool { return true }

func (Find) PromptSnippet() string {
	return "Find files by glob pattern (respects .gitignore)"
}

func (Find) PromptGuidelines() []string { return nil }

func (Find) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "find",
		Description: fmt.Sprintf("Search for files by glob pattern. Returns matching file paths relative to the search directory. "+
			"Respects .gitignore. Output is truncated to %d results or %dKB (whichever is hit first).",
			findDefaultLimit, DefaultMaxBytes/1024),
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"pattern":{"type":"string","description":"Glob pattern to match files, e.g. '*.ts', '**/*.json', or 'src/**/*.spec.ts'"},` +
			`"path":{"type":"string","description":"Directory to search in (default: current directory)"},` +
			`"limit":{"type":"number","description":"Maximum number of results (default: 1000)"}` +
			`},"required":["pattern"]}`),
	}
}

type findArgs struct {
	Pattern string   `json:"pattern"`
	Path    string   `json:"path"`
	Limit   *float64 `json:"limit"`
}

func (f Find) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	var in findArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("find: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	root, err := workspace.ResolvePath(tc.Cwd, searchPathArg(in.Path))
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("find: %w", err)
	}
	root, st, err := locateExisting(root)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("path not found: %s", root)
	}
	if !st.IsDir() {
		return protocol.ToolExecutionResult{}, fmt.Errorf("not a directory: %s", root)
	}
	limit := searchLimit(in.Limit, findDefaultLimit)

	timeout := f.timeout
	if timeout <= 0 {
		timeout = findTimeout
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Each source returns at most limit+1 paths: the extra one tells that the
	// list is cut.
	var paths []string
	var timedOut bool
	if fd, err := f.look("fd"); err == nil {
		paths, timedOut, err = findWithFd(sctx, fd, root, in.Pattern, limit)
		if err != nil {
			return protocol.ToolExecutionResult{}, err
		}
	} else if rg, err := f.look("rg"); err == nil {
		paths, timedOut, err = findWithRg(sctx, rg, root, in.Pattern, limit)
		if err != nil {
			return protocol.ToolExecutionResult{}, err
		}
	} else {
		paths, err = findWithWalk(sctx, root, in.Pattern, limit)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return protocol.ToolExecutionResult{}, err
		}
		timedOut = err != nil
	}
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}

	paths = sortedUnique(paths)
	more := len(paths) > limit
	if more {
		paths = paths[:limit]
	}

	var notices []string
	details := protocol.ToolResultDetails{Path: root, Kind: protocol.ToolKindSearch}
	text := ""
	if len(paths) > 0 {
		capped := TruncateHead(strings.Join(paths, "\n"), 1<<30, DefaultMaxBytes)
		text = capped.Text
		if capped.Truncated {
			details.Truncation = &capped.Truncation
		}
		if more {
			notices = append(notices, fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", limit, limit*2))
		}
		if capped.Truncated {
			notices = append(notices, fmt.Sprintf("%dKB limit reached", DefaultMaxBytes/1024))
		}
	}
	if timedOut {
		notices = append(notices, "search stopped after "+secondsText(timeout)+" s")
	} else if len(paths) == 0 {
		text = "No files found matching pattern"
	}
	return searchResult(text, notices, details)
}

// cleanFindPath normalizes one line of a file lister. It returns "" for an
// empty line and for a path inside a .git directory.
func cleanFindPath(line string) string {
	p := strings.TrimPrefix(line, "./")
	if p == "" || slices.Contains(strings.Split(p, "/"), ".git") {
		return ""
	}
	return p
}

// sortedUnique sorts paths and drops repeats.
func sortedUnique(paths []string) []string {
	slices.Sort(paths)
	return slices.Compact(paths)
}

// findWithFd runs fd. It matches like Pi's call: a pattern with "/" is matched
// against the whole path, with a leading "**/" so it matches at any depth.
func findWithFd(ctx context.Context, fd, root, pattern string, limit int) (paths []string, timedOut bool, err error) {
	argv := []string{fd, "--glob", "--color", "never", "--hidden", "--type", "f", "--case-sensitive", "--exclude", ".git"}
	if gitRepoRoot(root) == "" {
		argv = append(argv, "--no-require-git")
	}
	if strings.Contains(pattern, "/") {
		argv = append(argv, "--full-path")
		pattern = fullPathPattern(pattern)
	}
	argv = append(argv, "--", pattern)
	return runFindProgram(ctx, "fd", argv, root, limit, nil)
}

// findWithRg lists every file with ripgrep and applies the pattern in Go.
// rg --glob is an override that wins over .gitignore, so it cannot narrow the
// list without also listing ignored files that match.
func findWithRg(ctx context.Context, rg, root, pattern string, limit int) (paths []string, timedOut bool, err error) {
	argv := []string{rg, "--files", "--hidden", "--no-config", "--color", "never", "--glob", "!.git"}
	if gitRepoRoot(root) == "" {
		argv = append(argv, "--no-require-git")
	}
	return runFindProgram(ctx, "rg", argv, root, limit, findMatcher(pattern))
}

// runFindProgram runs a file lister in root and collects its output lines that
// keep accepts (nil accepts all). It stops the program once more than limit
// lines were accepted.
func runFindProgram(ctx context.Context, name string, argv []string, root string, limit int, keep func(string) bool) (paths []string, timedOut bool, err error) {
	var stderr bytes.Buffer
	stop := make(chan struct{})
	var once sync.Once
	var lines lineSplitter
	lines.handle = func(line []byte) {
		p := cleanFindPath(string(line))
		if p == "" || (keep != nil && !keep(p)) {
			return
		}
		paths = append(paths, p)
		if len(paths) > limit {
			once.Do(func() { close(stop) })
		}
	}
	spec := sandbox.Spec{Argv: argv, Dir: root, Observe: func(stream sandbox.Stream, chunk []byte) {
		if stream == sandbox.StreamStderr {
			if room := searchStderrKeep - stderr.Len(); room > 0 {
				stderr.Write(chunk[:min(room, len(chunk))])
			}
			return
		}
		lines.write(chunk)
	}}
	res, end, err := runSearch(ctx, spec, stop)
	if err != nil {
		return nil, false, fmt.Errorf("find: %w", err)
	}
	lines.flush()
	if end == endContext {
		if err := context.Cause(ctx); errors.Is(err, context.Canceled) {
			return nil, false, context.Canceled
		}
		return paths, true, nil
	}
	if end == endExited && res.ExitCode != nil && *res.ExitCode != 0 && len(paths) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = fmt.Sprintf("%s exited with code %d", name, *res.ExitCode)
		}
		return nil, false, errors.New(msg)
	}
	return paths, false, nil
}

// findWithWalk is the fallback for a machine with neither fd nor rg.
func findWithWalk(ctx context.Context, root, pattern string, limit int) ([]string, error) {
	return walkFind(ctx, root, findMatcher(pattern), limit)
}
