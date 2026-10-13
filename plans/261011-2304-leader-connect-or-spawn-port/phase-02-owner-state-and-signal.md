---
phase: 2
title: "Owner state"
status: completed
priority: P2
effort: "0.75d"
dependencies: [1]
---

# Phase 2: Owner state

## Context

- Plan: decision P3 (stopping marker), P4 (transient empty read is safe).
- Code: `internal/leader/lock.go:72-82` (`Owner`), `:85-97` (`WriteOwner`: truncate, then write), `:103-119` (`Release` truncates), `:123-153` (`ReadOwner`, reads 4 lines); `internal/leader/spawn.go:364-377` (`verifySignalOwner` compares the whole record); `internal/leader/server.go:383` (control shutdown calls `Close`), `:391` (`Close`); `internal/app/module_leader.go:23-35` (`LeaderParams`), `:105-110` (`NewServer`), `:155-165` (`Stop` calls `Server.Close` first); `cmd/tui/leader.go:228` (lock release last), `:261-274` (params, owner), `:296-310` (listen, then `WriteOwner`).

## Overview

The leader records whether it serves or stops, so a client can tell a leader that lost its socket from a leader that drains. No signal code changes in this phase (the signal path is phase 3).

## Requirements

- `Owner` gets `State string`; constants `OwnerServing = "serving"`, `OwnerStopping = "stopping"`. `WriteOwner` writes it as the fifth line. `ReadOwner` reads an optional fifth line; a missing line gives `""`.
- The `Lock` keeps the last `Owner` that it wrote. `Lock.MarkStopping() error` rewrites that record with `State = OwnerStopping`, under `l.mu`. After `Release` it returns `ErrLockReleased`. A lock that never wrote a record does nothing.
- `leader.Config.Stopping func()`: optional; `Server.Close` calls it once, inside `s.once.Do`, before it closes the listeners.
- `app.LeaderParams.Stopping func()` passed to `leader.Config.Stopping`.
- `cmd/tui` `serveLeader`: owner record with `State: leader.OwnerServing`; `params.Stopping = func() { _ = lock.MarkStopping() }`; a failure goes to stderr only.
- `verifySignalOwner` and `signalLeaderProcess` are unchanged. A record that changed to `stopping` fails the equality check in `verifySignalOwner`.

## Runtime Flow

- Actor and trigger: the leader starts serving; later a stop starts (control shutdown, signal, link failure, listener failure).
- Entry point: `serveLeader` → `serveLeaderRuntime` (`cmd/tui/leader.go:204,289`); every stop path reaches `Server.Close` (`module_leader.go:157`, `server.go:383`).
- Observable result: the lock file has `serving` while the leader serves and `stopping` from the moment the socket starts to refuse until the release clears it.
- Proof: `TestLeaderRecordsStoppingBeforeItStopsServing` (real runtime in process), `TestServerCloseCallsStoppingBeforeTheListenersClose`.

## File inventory

| File | Action | Test impact |
|---|---|---|
| `internal/leader/lock.go` | modify (`Owner.State`, constants, `WriteOwner`, `ReadOwner`, `MarkStopping`, last owner) | `lock_test.go`, `spawn_test.go` |
| `internal/leader/server.go` | modify (`Config.Stopping`, call in `Close`) | `server_test.go` |
| `internal/app/module_leader.go` | modify (`LeaderParams.Stopping`) | `leader_lifecycle_test.go` |
| `cmd/tui/leader.go` | modify (state, hook) | `leader_regression_test.go` |

## Tests Before

Phase 1 state: `go test ./internal/leader ./internal/app ./cmd/tui` passes.

## Tests After (write first, see them fail)

- `TestOwnerRecordKeepsItsState` (`spawn_test.go`, next to `TestOwnerMetadata`): write with `OwnerServing`, read back equal; `MarkStopping` gives `OwnerStopping` with the other fields unchanged; `Release` gives the zero `Owner`.
- `TestOwnerRecordWithoutStateReadsAsEmpty`: a 4-line file (as an older leader writes it) reads with `State == ""`.
- `TestMarkStoppingAfterReleaseFails`: `ErrLockReleased`.
- `TestServerCloseCallsStoppingBeforeTheListenersClose` (`server_test.go`): the hook dials the server socket and records that the dial succeeds; the hook runs once when `Close` is called twice.
- `TestLeaderStoppingHookReachesTheServer` (`internal/app/leader_lifecycle_test.go`): `LeaderParams.Stopping` runs when `LeaderRuntime.Stop` runs.
- `TestLeaderRecordsStoppingBeforeItStopsServing` (`cmd/tui/leader_regression_test.go`, real `serveLeaderRuntime` as the existing signal test at `:140`): while serving the state is `serving`; send a control shutdown; poll with `Eventually`: the first time the socket refuses, the record is `stopping` (an empty read for an instant is accepted, because `WriteOwner` truncates before it writes); after exit the record is empty.
- Existing `TestSignalFallbackRefusesChangedLifetimeOwner`, `TestStopRefusesUnverifiedPID`, `TestLeaderE2EStopFallsBackToVerifiedSignal` pass unchanged.

## Steps

1. Write the tests; run them; record the failures (compile failures count as failing for new symbols).
2. Implement `lock.go`, then `server.go`, `module_leader.go`, `cmd/tui/leader.go`.
3. Narrow tests, then the gates.

## Validation (gates)

`go test ./internal/leader ./internal/app ./cmd/tui`, `go vet ./...`, `GOOS=linux go vet ./internal/leader`, golangci-lint.

## Risk

| Risk | Mitigation |
|---|---|
| `MarkStopping` races with `Release` | Both hold `l.mu`; `Release` sets `file = nil` first, so a late mark returns `ErrLockReleased`. |
| The hook blocks `Close` | One small file write under `l.mu` only. |
| An older binary reads a 5-line record | It reads only four lines (`lock.go:136-151`); the fifth is ignored. |

## Rollback

Revert the commit. A leader of the reverted build writes 4 lines; a phase 3 client treats that as "no state" and never evicts. Phase 3 depends on this phase: revert phase 3 first.
