---
phase: 1
title: "Spawn retry"
status: completed
priority: P2
effort: "0.5d"
dependencies: []
---

# Phase 1: Spawn retry

## Context

- Plan: [Outcome R1](plan.md#outcome), decisions P1, P2.
- Grok: `MAX_SELF_SPAWN_ATTEMPTS = 3` ([source anatomy section 1.3](../reports/xia-261011-grok-connect-or-spawn-source.md)). Grok retries on a readiness timeout; Ask retries on an early exit with an error, because Ask already waits for readiness inside one deadline.
- Code: `internal/leader/client.go:130-195` (`run`), `:170-183` (start branch and child exit), `:186-193` (deadline error), `:198-207` (`start`), `:213-220` (`leaderExecutable`); `internal/leader/spawn.go:207-230` (`ProbeAsk`), `:37` (`probeTimeout` 15 s); `cmd/tui/leader.go:214-221` (a spawned leader exits 1 when another holder does not answer).

## Overview

A leader that this call started and that exits with an error while the lock is free is started again, up to 3 starts in total per call. The binary is found and probed once and reused. The final error keeps today's form (`ErrStartupFailed`, child error, log tail) and adds the start count.

## Requirements

- `connector` gets `starts int` and `exe string`. `start` finds and probes the binary only when `exe` is empty; a later start reuses it.
- In the `outAbsent`, lock-free, child-exists branch: when the child exited with a non-nil error and `starts < maxStarts` (const 3), set `child = nil` so the next loop starts again. When `starts == maxStarts`, return `fmt.Errorf("%w: %v after %d starts; log: %s", ErrStartupFailed, child.Err(), starts, LogTail(p, 2048))`.
- A clean exit (`child.Err() == nil`) keeps today's meaning: a lock loser; wait.
- Errors from `leaderExecutable`, `ProbeAsk` and `StartFn` return at once (no retry).
- The connector keeps the error of the last failed start (`lastStartErr`), because `child` is set to nil before the next start. The deadline branch (`client.go:186-193`) uses it: when a start failed, it returns `ErrStartupFailed` with that error, the start count and the log tail, as today for one start.
- The deadline still bounds everything; no new config field.
- Comments describe behavior ("a leader that ends with an error before it serves is started again, at most three times"), no labels.

## Runtime Flow

- Actor and trigger: a client (TUI, `ask connect`, another program) calls `ConnectOrSpawn`; the first leader process exits with an error before it serves (for example a lost lock race to a leader that is still starting, P2).
- Entry point: `leader.ConnectOrSpawn` (`client.go:109`), called by `tuiclient.LeaderDial` (`internal/tuiclient/client.go:69`), `cmd/tui/connect.go:554`, `cmd/tui/testdata/spawncaller/main.go:22`.
- Observable result: the call returns a client of the third start, or `ErrStartupFailed` with "after 3 starts" and the log tail.
- End-to-end proof: `TestLeaderE2ESpawnRetriesAfterEarlyExit`, `TestLeaderE2ESpawnGivesUpAfterThreeStarts`.

## File inventory

| File | Action | Test impact |
|---|---|---|
| `internal/leader/client.go` | modify (`connector` fields, `start`, child-exit branch, error text) | `client_test.go` |
| `internal/leader/client_test.go` | modify (new tests; update `TestConnectOrSpawnStartupFailureShowsTheLog` to expect 3 starts) | - |
| `cmd/tui/leader_spawn_retry_e2e_test.go` | create | E2E |

## Tests Before

`go test ./internal/leader ./cmd/tui` passes on the current tree.

## Tests After (write first, see them fail)

Unit (`internal/leader/client_test.go`, `StartFn` fakes, real temp home):

- `TestConnectOrSpawnStartsAgainAfterAnEarlyExit`: `StartFn` returns an exited child with an error twice, then `procLeader.start`; the call succeeds; `StartFn` called 3 times; the version script ran once (count lines in a file that the fake `ask` appends to).
- `TestConnectOrSpawnStopsAfterThreeStarts`: `StartFn` always returns an exited child with an error and writes "start N failed" to the log; error is `ErrStartupFailed`, text holds "after 3 starts" and "start 3 failed"; 3 calls.
- `TestConnectOrSpawnDoesNotRetryAStartError`: `StartFn` returns `nil, err`; one call; the error is returned.
- `TestConnectOrSpawnCleanExitIsNotRetried`: existing `TestConnectOrSpawnAdoptsAWinnerThatStartsLate` keeps 1 start (assert the count).
- Updated: `TestConnectOrSpawnStartupFailureShowsTheLog` asserts 3 starts.
- Existing `TestConnectOrSpawnIncompatibleAsk`, `TestConnectOrSpawnAskMissing` keep 0 starts.

E2E (`cmd/tui/leader_spawn_retry_e2e_test.go`, built binary):

- `TestLeaderE2ESpawnRetriesAfterEarlyExit`: a temp dir holds the compiled `spawncaller` and a sibling `ask` shell wrapper: `version` → `exec <bin> version --json`; `leader` → append to a count file; when the count is below 3, write `wrapper start N failed` to stderr and exit 1; else `exec <bin> "$@"`. Run the caller with `leaderEnv(home)`; it prints an instance id; `ask leader status` (BIN) shows the same id; the count file shows 3. Stop the leader at the end (`stopLeaderAtEnd`).
- `TestLeaderE2ESpawnGivesUpAfterThreeStarts`: the wrapper always fails `leader`; the caller exits 1; stderr holds "the leader did not start", "after 3 starts" and "wrapper start 3 failed"; count 3; `leaderProcesses(bin)` empty; lock free.
- Existing `TestLeaderE2EStartupFailureIsReported` still passes (now 3 failed starts, each fast) and stays under its 15 s bound.

## Steps

1. Write the unit tests and the E2E file; run `go test -run 'StartsAgain|StopsAfterThree|DoesNotRetry|CleanExit|StartupFailure' ./internal/leader` and `go test -run 'SpawnRetries|SpawnGivesUp' ./cmd/tui`; record the failures.
2. Add the fields and the retry branch in `client.go`; reuse `exe`.
3. Run the narrow tests, then the gates.

## Validation (gates)

`go test ./internal/leader ./cmd/tui`, `go vet ./...`, `GOOS=linux go vet ./internal/leader`, `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`.

## Risk

| Risk | Mitigation |
|---|---|
| A leader that always fails costs 3 starts and 3 log entries | Bounded by 3 and by the deadline; the log tail of the last start is in the error. |
| A retry starts a second leader while the first still runs | Only after the child was reaped (`Exited` closed) and only when `LockHeld` is false. |

## Rollback

Revert the phase commit. No other phase depends on the retry, but phases 3 and 5 edit the same function: revert those first if all must go.
