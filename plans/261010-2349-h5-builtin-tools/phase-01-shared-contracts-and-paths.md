---
phase: 1
title: "Shared contracts, paths and import rules"
status: completed
priority: P1
effort: "1.5d"
dependencies: []
---

# Phase 1: Shared contracts, paths and import rules

## Context

- Architecture: [Tool contracts (`tools`)](../architectures/h5-builtin-tools-architecture.md#tool-contracts-tools) (paths bullet), [Boundaries & contracts](../architectures/h5-builtin-tools-architecture.md#boundaries--contracts) (import rules, result contract), [Missing pieces](../architectures/h5-builtin-tools-architecture.md#missing-pieces).
- Decisions: [Q13 package placement](../architectures/h5-builtin-tools.md#q13-package-placement), [Q14 notice shape](../architectures/h5-builtin-tools.md#q14-message-shape-of-system-notices).
- xia H5: ["What to take"](../reports/xia-261010-1727-h5-builtin-tools.md#what-to-take-no-conflict-with-the-contract) rows `paths`, `result`, `prompts`, `write` (atomic temp file and rename); [dependency matrix](../reports/xia-261010-1727-h5-builtin-tools.md#dependency-matrix) (per-path mutation queue, session cwd resolver, prompt snippets).
- Inventory: H-TOOL-19 path handling, H-TOOL-11 prompt contribution, H-TOOL-02 per-path mutex, H-TOOL-13 result shape ([inventory-harness.md](../260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md) rows at lines 52, 61, 63, 69).
- Spike 1: [Final conclusion](../reports/spike-261010-system-reminder-notice.md#final-conclusion) (the wrapper shape that `IsSystemReminder` detects).

## Overview

Add the small shared pieces that later phases need, so no later phase edits a shared file: the UI details type and the reminder helper in `pkg/protocol`, path resolution in `workspace`, truncation, prompt section, per-path lock, read state and atomic write in `tools`, and depguard rules for `sandbox`, `jobs` and `workspace`.

## Requirements

- Functional:
  - `protocol.ToolResultDetails` (JSON): `path`, `diff{path,oldText,newText}`, `locations[{path,line}]`, `truncation{truncatedBy,totalLines,totalBytes,outputLines,outputBytes}`, `nextOffset`, `spillPath`, `spillCapped`, `exitCode`, `signal`, `jobId`, `status`, `cwd` (directory named by a leading `cd X &&` of a `bash` command), `kind` (`read`, `edit`, `execute`, `search`, `other`). All fields optional.
  - `protocol.SystemReminder(text) string` returns `"<system-reminder>\n" + text + "\n</system-reminder>"` (Grok shape, `reminders/mod.rs`); `protocol.IsSystemReminder(UserMessage) bool` is true when the message is one text block in that shape.
  - `workspace.ResolvePath(cwd, p string) (string, error)`: strip one leading `@`; expand `~` and `~/`; accept `file://` URLs; join a relative path with `cwd`; clean. `workspace.PathVariants(abs)` gives the macOS screenshot variants (U+202F before `AM`/`PM`, NFD form, curly apostrophe) and shell-unescaped form; callers try them only when the path does not exist (Pi `path-utils.ts:5-72`, omp `path-utils.ts`).
  - `tools.TruncateHead(text, maxLines=2000, maxBytes=50*1024)` returns kept text, `truncation` fields and the next offset; never cuts inside a UTF-8 rune.
  - `tools.PromptContributor` interface and `tools.PromptSection(*Snapshot) string` (P8): sorted by tool name; tools without a snippet are not listed.
  - `tools.PathLock(path) (unlock func())`: process-wide mutex map keyed by `filepath.EvalSymlinks` (parent dir plus base name for a new file); entries are removed when no holder is left.
  - `tools.FileState` (per session, `tools.NewFileState()`): `Record(path, info)` after read/write/edit; `Check(path) error` returns a stale error when mtime (ns) or size changed since the last record; no record means no check.
  - `tools.WriteFileAtomic(path, data, perm)`: temp file in the same directory, `fsync`, `rename`; keeps the mode of an existing file; removes the temp file on error.
- Non-functional: no new third-party dependency (`golang.org/x/text/unicode/norm` is already in `go.mod:47`); every new function has a unit test.

## Architecture

Data flow: tools call `workspace.ResolvePath` on every path argument; file tools take `PathLock`, check `FileState`, write with `WriteFileAtomic`, and return `ToolExecutionResult{Content, Details: json(ToolResultDetails)}`. `acp` (phase 8) and a later TUI decode `Details`. `jobs` (phase 5) wraps notices with `protocol.SystemReminder`; the TUI (N1) detects them with `IsSystemReminder`.

## Runtime Flow

- Actor and trigger: none on its own; these are building blocks.
- Registered entry point: reached through the tools of phases 2, 4, 6 and the composition of phase 7.
- Internal path: tool body → helpers here.
- Prepared data and external mocks: temp dirs and files; no mocks.
- Observable result: unit results here; runtime proof in phase 9 rows `read`, `write`, `edit`, prompt section.
- End-to-end proof: `TestPrintReadsFileWithLineNumbers`, `TestPrintRequestCarriesToolPromptSection` (phase 9).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `pkg/protocol/tool.go` | modify (add `ToolResultDetails`, `SystemReminder`, `IsSystemReminder`) | +70 | new `tool_test.go` cases |
| `internal/workspace/path.go` | create | ~120 | `path_test.go` |
| `internal/tools/truncate.go` | create | ~80 | `truncate_test.go` |
| `internal/tools/prompt.go` | create | ~50 | `prompt_test.go` |
| `internal/tools/file_lock.go` | create | ~50 | `file_lock_test.go` |
| `internal/tools/file_state.go` | create | ~50 | `file_state_test.go` |
| `internal/tools/file_atomic.go` | create | ~50 | `file_atomic_test.go` |
| `.golangci.yml` | modify (add `internal/sandbox/**`, `internal/jobs/**`, `internal/workspace/**` to `core-no-agent` :116, `core-no-transport` :127, `core-no-adapters` :142, `core-no-terminal-ui` :85) | +12 | lint gate |

Existing tests that must stay green: `internal/tools` (6 test files: `registry_test.go`, `registry_snapshot_test.go`, `registry_dispose_test.go`, `validate_test.go`, `coerce_test.go`, `echo_test.go`), `pkg/protocol` tests.

## Tests Before

Regression: `go test ./internal/tools ./pkg/protocol` passes on the unchanged tree (record the count).

## Tests After (write first, see them fail)

| Test | Scenario | Priority |
|---|---|---|
| `TestResolvePathStripsAtAndExpandsHome` | `@src/a.go`, `~/x` | critical |
| `TestResolvePathAcceptsFileURL` | `file:///tmp/a%20b` | high |
| `TestResolvePathJoinsRelativeWithCwd` | `a/../b` under cwd | critical |
| `TestPathVariantsCoverScreenshotNames` | `Screenshot 2026-10-10 at 9.41.12 PM.png` with U+202F, NFD, curly quote | medium |
| `TestTruncateHeadStopsAtLineLimit` | 3000 lines | critical |
| `TestTruncateHeadStopsAtByteLimit` | 60 KB in 10 lines | critical |
| `TestTruncateHeadNeverSplitsRune` | multibyte at the cut | high |
| `TestPromptSectionListsOnlyToolsWithSnippets` | echo has no snippet | high |
| `TestPromptSectionIsSortedByName` | registration order differs | medium |
| `TestPathLockSerializesSymlinkAliases` | lock via link and via target | high |
| `TestFileStateRejectsChangeSinceRead` | change size, then mtime only | critical |
| `TestFileStateAllowsFileNeverRead` | no record | high |
| `TestWriteFileAtomicKeepsModeAndLeavesNoTemp` | existing 0755 file; failing write | high |
| `TestToolResultDetailsRoundTrip` | JSON encode/decode | high |
| `TestIsSystemReminderMatchesWrappedText` | wrapped, plain, two blocks | high |

## Function and interface checklist

- [x] `protocol.ToolResultDetails`, `protocol.SystemReminder`, `protocol.IsSystemReminder`
- [x] `workspace.ResolvePath`, `workspace.PathVariants`
- [x] `tools.TruncateHead`, `tools.PromptContributor`, `tools.PromptSection`
- [x] `tools.PathLock`, `tools.FileState` (`NewFileState`, `Record`, `Check`), `tools.WriteFileAtomic`

## Dependency map

Blocks phases 2, 3 (depguard only), 4, 5 (`SystemReminder`), 6, 7 (`PromptSection`, `IsSystemReminder` in `printReply`, `NewFileState`), 8 (`ToolResultDetails`). Depends on nothing.

## Related Code Files

- Create: `internal/workspace/path.go`, `internal/tools/truncate.go`, `internal/tools/prompt.go`, `internal/tools/file_lock.go`, `internal/tools/file_state.go`, `internal/tools/file_atomic.go` and their `_test.go`.
- Modify: `pkg/protocol/tool.go`, `.golangci.yml`.

## Implementation Steps

1. Write the tests above; run them; they fail to compile.
2. Add the protocol type and helpers next to `ToolDecl` (`pkg/protocol/tool.go:7`).
3. Add `workspace/path.go` (`workspace` imports only the standard library and `golang.org/x/text`).
4. Add the `tools` helpers. `PromptSection` reads `Snapshot` entries in name order (same order rule as `sortedDecls`, `internal/agent/systemprompt.go:23`).
5. Extend the four depguard rules.
6. Run the regression gate.

## Regression Gate

`go test ./pkg/protocol ./internal/tools ./internal/workspace && go vet ./... && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`

## Success Criteria

- [x] All tests in "Tests After" pass; existing `internal/tools` and `pkg/protocol` tests pass.
- [x] Lint passes with the new depguard entries.
- [x] No existing call site changes behavior (no tool is registered yet).

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| `EvalSymlinks` fails for a new file | M x M | Resolve the parent dir, join the base; test with a new file under a symlinked dir. |
| mtime resolution too coarse on some file systems (stale check misses a change) | L x M | Compare size too; signal: a test on the CI file system fails; response: add a SHA-256 of the content read (the architecture allows "or a hash"). |
| `IsSystemReminder` matches a user who types the tags | L x L | Only text the Agent queued from `jobs` uses it; a user can hide own text only from their own view. Documented in phase 10. |

## Security Considerations

- Path resolution does not restrict paths to the cwd: Ask has no approval layer (D3, H-SEC-03). It only makes paths explicit (never ambient cwd).
