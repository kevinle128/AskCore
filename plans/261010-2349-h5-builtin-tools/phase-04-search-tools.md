---
phase: 4
title: "Search tools: grep, find"
status: completed
priority: P1
effort: "1.5d"
dependencies: [1, 3]
---

# Phase 4: Search tools: grep, find

## Context

- Architecture: [Tool contracts (`tools`)](../architectures/h5-builtin-tools-architecture.md#tool-contracts-tools) (grep and find bullet), [Other](../architectures/h5-builtin-tools-architecture.md#other) (build versus reuse: ripgrep and fd are external with fallbacks).
- xia H5: ["What to take"](../reports/xia-261010-1727-h5-builtin-tools.md#what-to-take-no-conflict-with-the-contract) rows `grep` and `find / ls`; [Ask today](../reports/xia-261010-1727-h5-builtin-tools.md#ask-today-scout) (`fd` cannot be assumed); [Risks](../reports/xia-261010-1727-h5-builtin-tools.md#risks) (`fd` missing).
- Inventory: H-TOOL-05 `grep`, H-TOOL-06 `find`, H-TOOL-11 snippets ([inventory-harness.md](../260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md) lines 55, 56, 61); roadmap [H5](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h5-built-in-tools) ("no `find -> Glob` alias", "do not rebuild `glob`").
- Pi source (contract texts): `opensources/pi` commit `4c6fb7cfe`, `packages/coding-agent/src/core/tools/grep.ts:21-41,119-168,282-299`, `find.ts:26-41,172-216`.

## Overview

Add `grep` and `find` with Pi's parameters, limits and texts. Both run external binaries through `sandbox.Run` (phase 3) and have a fallback when the binary is missing.

## Requirements

- Functional:
  - `grep(pattern, path?, glob?, ignoreCase?, literal?, context?, limit=100)` (H-TOOL-05). Runs `rg --json --hidden` with `--glob`, `-i`, `-F`, `-C`, and `--` before the pattern so a flag-like pattern stays literal; respects `.gitignore` (rg default); path resolved by `workspace.ResolvePath`. Parses the JSON event stream in `Spec.Observe` order. Output lines `path:line: text` with paths relative to the search root; context lines `path-line- text`; each line cut at 500 chars with Pi's marker; whole output cut at 50 KB by `TruncateHead`. Limits: a per-file cap of `limit` matches and a global cap of `limit`; when the global cap is full, the tool calls `Process.Kill` and ends early; the result says `Found at least N matches; showing the first N. Refine the pattern or use path/glob.` (Grok wording, Pi limit). No match: Pi text `No matches found`. Timeout 20 s through the call context (xia "grep"); on timeout the partial matches are returned with `[search stopped after 20 s]`. When `rg` is not on `PATH`: tool error `grep needs ripgrep (rg) on PATH` (Pi has no Go fallback; the inventory note "Go fallback" is not taken, see risk).
  - `find(pattern, path?, limit=1000)` (H-TOOL-06): glob over file paths relative to `path`. Chain: `fd --glob --hidden --type f --color never <pattern>` when `fd` is on `PATH`; else `rg --files --hidden --glob <pattern>`; else a Go walk that applies `.gitignore` files (root and nested, scoped to their subtree), skips `.git`, and matches with a `**`-aware glob. Output sorted, one relative path per line, 50 KB cap, Pi's limit text `<limit> results limit reached. Use limit=<2*limit> for more, or refine pattern`; no match: `No files found matching pattern`. Timeout 5 s (xia "find / ls"); the Go walk checks `ctx` per directory.
  - Details: `kind=search`, `truncation`, `locations` of the first 20 matches for `grep` (`{path, line}`).
  - Snippets and guidelines: port Pi's `grep.ts` and `find.ts` prompt contributions.
- Non-functional: both tools implement `ConcurrencySafe` = true (P9); return promptly on `ctx` cancel (the runner kills the group).

## Architecture

```
grep: ResolvePath → argv(rg --json ...) → sandbox.Run(ctx 20 s, Observe=jsonLineParser)
      parser → matches (cap) → Kill on full budget → format → TruncateHead → details
find: ResolvePath → which(fd)? → sandbox.Run(fd ...)          (5 s)
                  → which(rg)? → sandbox.Run(rg --files ...)
                  → walk(root, gitignore stack, globMatch)
      → sort → cap → format
```

`search_walk.go` holds the Go walk, the gitignore subset (patterns, `!` negation, trailing `/` for directories, leading `/` anchors, `**`) and the glob matcher. No new module dependency (P15).

## Runtime Flow

- Actor and trigger: the model calls `grep` or `find`.
- Registered entry point: `newNativeAgentOn` registers both (phase 7).
- Internal path: coordinator (`internal/agent/tool_coordinator.go:204,264`) → tool body → `sandbox.Run` (phase 3) or the Go walk.
- Prepared data and external mocks: temp repo with `.gitignore`, nested `.gitignore`, hidden files; `PATH` without `fd` for the fallback test; no mocks.
- Observable result: match lines, file lists, cap notices.
- End-to-end proof: `TestPrintGrepFindsMatches`, `TestPrintFindWithoutFdUsesFallback` (phase 9).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/tools/search_grep.go` | create | ~200 | `search_grep_test.go` |
| `internal/tools/search_find.go` | create | ~120 | `search_find_test.go` |
| `internal/tools/search_walk.go` | create | ~180 | `search_walk_test.go` |

Existing coverage: none. `internal/tools` tests of phases 1 and 2 must stay green.

## Tests Before

`go test ./internal/tools` passes (phase 2 state, or phase 1 state when run in parallel with phase 2).

## Tests After (write first)

| Test | Scenario | Priority |
|---|---|---|
| `TestGrepFormatsMatchesWithRelativePaths` | two files, two matches | critical |
| `TestGrepKeepsFlagLikePatternLiteral` | pattern `-e` and `--files` | high |
| `TestGrepRespectsGitignoreAndShowsHidden` | ignored file not found; `.env.example` found | high |
| `TestGrepStopsEarlyAtLimit` | 500 matches, limit 10 → `Found at least 10`, process killed (group gone) | critical |
| `TestGrepCutsLongLines` | 2000-char line → 500 chars + marker | medium |
| `TestGrepContextLines` | `context: 1` | medium |
| `TestGrepNoMatchText` | `No matches found` | high |
| `TestGrepWithoutRgReportsError` | `PATH` empty dir | high |
| `TestFindUsesFdWhenPresent` | `fd` present (skip when absent) | medium |
| `TestFindFallsBackToRgFiles` | `PATH` holds only `rg` | high |
| `TestFindFallsBackToGoWalkWithGitignore` | `PATH` empty; nested `.gitignore` with `!keep.log` | critical |
| `TestFindAllChainsGiveSameResult` | same fixture through fd, rg, walk → identical sorted list | high |
| `TestFindLimitNotice` | 1500 files, limit 1000 | medium |
| `TestGlobMatchDoubleStar` | `**/*.go`, `src/**`, `*.{go,md}` (Pi `fd --glob` brace support) | high |
| `TestSearchToolsReturnOnCancelledContext` | ctx cancelled during a slow walk | high |

## Function and interface checklist

- [x] `tools.Grep`, `tools.Find` (`Decl`, `Execute`, `ConcurrencySafe`, `PromptSnippet`, `PromptGuidelines`)
- [x] `rgEvent` decoder, `formatMatches`
- [x] `findWithFd`, `findWithRg`, `findWithWalk`, `globMatch`, `gitignore` stack

## Dependency map

Needs phase 1 (`ResolvePath`, `TruncateHead`, `ToolResultDetails`) and phase 3 (`sandbox.Run`). Used by phase 7 (registration). Shares no file with phases 2, 5, 6.

## Related Code Files

- Create: the three files above and their tests.
- Modify: none.

## Implementation Steps

1. Write the tests; tests that need a binary look it up with `exec.LookPath` and set `PATH` through the tool's injected `lookPath` field (no global env change, so tests can run in parallel).
2. Implement the glob matcher and gitignore stack; then the walk.
3. Implement `find` chain, then `grep` with the JSON parser and the early stop.
4. Run the regression gate.

## Regression Gate

`go test ./internal/tools -race && go vet ./internal/tools`

## Success Criteria

- [x] All listed tests pass with `-race`.
- [x] Parameter names, defaults and texts match the cited Pi lines.
- [x] A full `grep` budget kills `rg` (group gone) instead of reading to the end.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| `rg` missing on a user machine makes `grep` unusable | L x M | Pi requires `rg` too; the error names the fix. Signal: a real-provider run or user report; response: add a Go walk plus `regexp` fallback (replan, it adds scope). |
| Go-walk gitignore subset differs from git (rare syntax) | M x L | It is the last fallback; `TestFindAllChainsGiveSameResult` pins common cases; documented in phase 10. |
| `fd` and `rg --files` order or hidden-file rules differ | M x L | Sort output; pass `--hidden` to both; the same-result test. |
| JSON event parsing breaks on a new `rg` version | L x M | Parse only `match`, `context`, `end` types and ignore unknown fields. |

## Security Considerations

- Arguments are passed as argv, never through a shell; the pattern follows `--`.
- No path restriction (D3); `--hidden` shows dotfiles such as `.env` as Pi does (H-SEC-03).
