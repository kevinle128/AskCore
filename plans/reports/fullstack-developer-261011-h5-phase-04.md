# Phase 4 report: grep and find

Status: completed. All listed tests exist, were written first (compile failure on `Grep`/`Find` seen), and pass with `-race -count=3`.

## Files

- `internal/tools/search_grep.go`: `Grep`, rg JSON collector, shared helpers (`runSearch`, `lineSplitter`, `searchResult`).
- `internal/tools/search_find.go`: `Find`, fd / rg / walk chain.
- `internal/tools/search_walk.go`: `globMatch`, `findMatcher`, gitignore subset, `walker`.
- Tests: `search_grep_test.go`, `search_find_test.go`, `search_walk_test.go` (every "Tests After" row plus extras: single-file path, flag combos, glob filter, trailing context, bad regex, timeout partial, split events, fd over rg precedence, exact-limit count).

## Checks

- `go test ./internal/tools/ -race -count=1`: pass (phase 2 files compiled at that time).
- `go vet ./internal/tools/...`: pass.
- golangci-lint on `./internal/tools/...`: no issue in my files (two issues remain in `file_read.go` and `file_tools_helpers_test.go`, owned by phase 2).

## Deviations from the phase text

1. `find` via rg lists all files with `rg --files --hidden` and applies the pattern in Go. `rg --glob` is an override that wins over `.gitignore`, so `--glob '*.log'` listed ignored files and broke the same-result test.
2. `find` via fd also passes `--case-sensitive` (fd is smart-case by default, rg and the walk are not) and `--exclude .git`. All chains drop any path with a `.git` component. `--no-require-git` is passed outside a repository, as Pi does.
3. `grep` adds `--glob '!.git'` and `--no-config`, and uses `--max-count <limit>` as the per-file cap. After the budget is full, trailing context lines of the last match are kept before the kill.
4. Error texts are lowercase (`path not found: X`, `not a directory: X`) because staticcheck ST1005 flags capitals. The limit and no-match texts follow the spec.
5. `find` cap: each source stops at limit+1 paths; output is sorted, so with fd or rg the kept set is the first limit found, not the global lexical first. The notice shows only when more than `limit` paths exist.
6. Notices are separate `[...]` lines after a blank line (Pi joined them in one bracket). Includes Pi's `Some lines truncated to 500 chars...` and `50KB limit reached`.
7. Walk gitignore: root-down plus ancestors up to the repo root; no `.ignore`, global ignore or `.git/info/exclude`; symlinks are not followed or listed.

## Exported API for later phases

`tools.Grep{}`, `tools.Find{}` (zero values work; methods `Decl`, `Execute`, `ConcurrencySafe`, `PromptSnippet`, `PromptGuidelines`). Unexported seams `lookPath` and `timeout` are for tests only. Phase 7 registers `tools.Grep{}` and `tools.Find{}`.

## Unresolved questions

- Should the global gitignore and `.ignore` files be honoured by the walk? Not done; the walk is the last fallback.
