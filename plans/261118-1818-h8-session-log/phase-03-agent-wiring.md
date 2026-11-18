---
phase: 3
title: "Agent wiring"
status: pending
priority: P1
effort: "8h"
dependencies: [1]
---

# Phase 3: Agent wiring

## Goal

The Agent runs on a log that may fail to open, closes a turn that a crash left open, records model and thinking changes, applies `context_edit` in the rebuild, and stops a run when the writer is lost.

## Context

- `Config.NewContext func() sessions.Writer` (`internal/agent/types.go:56-59`); default `MemoryLog` (`agent.go:187-190`); `openSource` (`seed.go:26-29`) is reached from `New` and from `swapContext` (`agent.go:551`).
- `trackedLog` counts preloaded entries at wrap (`follow.go:21-25`), so a resumed log gives a correct follow cut.
- `seedLog` skips seed items that the log holds (`seed.go:31-48`).
- `openLog` appends a `SystemSnapshot` only on change (`agent.go:776-783`); the same pattern serves model and thinking.
- `repairTools` handles only a turn opened by this run (`tool_repair.go:15-27`); texts "outcome unknown" and "not started" (`tool_repair.go:66-69`).
- A failed `Append` ends the run (`loop_run.go:239-244`).
- Rebuild: `rebuildRequest` (`request_log.go:188`), `projectHistory` (`request_log.go:112`).

## Requirements

1. Change `NewContext` to `func() (sessions.Writer, error)`. `New` returns the error. `Reset` returns it and keeps the old log and epoch when it fails. Doc comment: the first call gives the log of the first conversation (may be preloaded); each later call gives a fresh conversation.
2. Resume repair at open: when the first log ends inside an open turn or cycle, append in one commit only what is open: a tool result for each tool call with no result ("outcome unknown" when a `ToolCall` intent exists, else "not started"), then `TurnClosed` if a turn is open, then `CycleClosed` if a cycle is open, with `ReasonAborted` and a new fixed cause word `interrupted` (add it to the cause words in `internal/agent/lifecycle.go:227-236`, and to the wire docs of `cycle_end.cause`). Reuse the `repairTools` scan through one shared helper (no second copy).
3. Model and thinking entries: in `openLog`, compare the run's model (`ModelRef` from `a.cfg.Model`) and thinking level with the last logged `ModelChange` and `ThinkingChange`; append the changed ones before the first request. The Agent keeps the last logged values in memory (one scan at open), so no run copies the whole log.
4. Expose `LoggedSelection(entries) (model ModelRef, method string, level ThinkingLevel, ok bool)` as a pure function for the composition: latest `ModelChange`/`ThinkingChange`, else the latest `RequestDelta.Model` and `Options.Reasoning`. The method is `Binding.Method` of the latest `AttemptSettled` with a binding: the Agent never learns a requested method (`authMethodId` only checks readiness, `internal/acp/ask_methods.go:286-290`).
5. Rebuild: `rebuildRequest` takes its baseline from `sessions.MessagesOf(entries[:at])` (`request_log.go:201`), so with the phase 1 projection it already sees only the edits before the attempt. Add the test that pins this; change code only if the test fails.
6. Lost writer: when `Append` returns an error that wraps `sessions.ErrWriterLost`, the run ends as today, the repair step does not try to append, and the error text says that the session file can no longer be written and must be reopened.
7. Changed context files at resume: when a preloaded log holds seed items, compare each current seed item (context file text, skill list) with the latest logged item of the same source. Keep the logged items unchanged. When the text differs, append the current text as a new seed item after the log, so the provider context gets the new text and the old history and cache prefix stay. An unchanged source appends nothing. Test: resume after an edit of `AGENTS.md` sends the new text once; resume with no edit sends no new seed item.
8. Mechanical update of all 24 test files (37 lines) that set `NewContext`. First 10: `internal/agent/session_log_test.go`, `tool_abort_test.go`, `tool_control_test.go`, `tool_context_test.go`, `queue_test.go`, `agent_test.go`, `seed_test.go`, `follow_test.go`, `internal/acp/host_test.go`, `cmd/tui/headless_lifecycle_test.go`. Find the rest with `rg -l NewContext --type go internal cmd`.

## Files owned

- Modify: `internal/agent/types.go`, `agent.go`, `seed.go`, `tool_repair.go`, `lifecycle.go`, `loop_run.go`; `request_log.go` only if the rebuild test fails
- Create: `internal/agent/selection_log.go`, `internal/agent/resume_repair_test.go`, `internal/agent/seed_resume_test.go`, `internal/agent/selection_log_test.go`, `internal/agent/context_edit_rebuild_test.go`
- Modify (signature only): the 24 test files of requirement 7

## Tests written first

- `TestOpenLogClosesTurnLeftOpenByCrash`: a log with `TurnOpened`, an assistant tool call with a `ToolCall` intent, and a second call with none; after `New`, the log holds "outcome unknown", "not started", `TurnClosed`, `CycleClosed` in that order, and a prompt then runs.
- `TestRunStartLogsModelAndThinkingChange` and `TestRunStartSkipsUnchangedSelection`.
- `TestLoggedSelectionPrefersChangeEntries` and `TestLoggedSelectionFallsBackToRequestDelta`.
- `TestRebuildIgnoresLaterContextEdit`; `TestContextEditOmitsMessageFromNextRequest` (the request of the next run lacks the target; `Entries` still has it).
- `TestResetOpensNewLogAndClosesOld`; `TestResetKeepsOldLogWhenOpenFails`.
- `TestLostWriterEndsRunWithClearError` (a writer that returns `sessions.ErrWriterLost` on the second append).

## Steps

1. Write the tests with the new signature; they fail to compile, then fail.
2. Change the signature and update the 24 test files.
3. Add the repair helper, the selection entries and `LoggedSelection`.
4. Pin the rebuild with the `ContextEdit` test.
5. Map the lost-writer error.

## Validation

```
go test ./internal/agent/... ./internal/acp/... ./cmd/tui/...
go test -race ./internal/agent/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| Repair at open runs on a live log that is between runs | L x H | repair runs only in `openSource` (New and Reset). A live run closes its turn and cycle on failure (`recordFailureStep`, `agent.go:848-876`); when that write fails the file writer is latched lost, so no later append happens in that process |
| A selection entry is lost when the user changes model and the process dies before a run | M x L | accepted planner choice (plan.md); the resume falls back to the last `RequestDelta` |
| Signature change breaks callers outside tests | L x M | `rg` finds no production caller today |

## Rollback

Revert the commits. With `NewContext` unset the default `MemoryLog` path is the same as today.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
