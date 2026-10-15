---
phase: 3
title: "Zombie eviction on Linux"
status: completed
priority: P2
effort: "1.75d"
dependencies: [2]
---

# Phase 3: Zombie eviction on Linux

## Context

- Plan: decisions P4 (rule), P5 (signals), P6 (budget), P7 (test seams); risk rows "signal to a wrong process" and "R2 kills a leader that drains".
- Grok: [source anatomy 3.3, 3.4](../reports/xia-261011-grok-connect-or-spawn-source.md): 30 s unconnectable, SIGTERM, 8 s, SIGKILL, 8 s, 3 tries per PID, PID-keyed timer. H13b listed this as a non-goal (H13b plan line 54); user choice C (2026-10-11) brings it in.
- Code: `internal/leader/client.go:160-175` (`outAbsent` + `LockHeld` → wait), `:231-243` (ENOENT/ECONNREFUSED → `outAbsent`); `spawn.go:310-327` (`VerifyLeaderProcess`), `:340-360` (`isLeaderCommandLine` accepts `<Exe> leader`; `Exe` must equal the first command-line token exactly), `:364-377` (`verifySignalOwner`); `proc_linux.go:71-93` (`signalLeaderProcess`: pidfd, verify, POLLIN liveness poll at :82-85, owner re-check, SIGTERM), `proc_darwin.go:37-39`, `proc_other.go:16-18`; phase 2 `Owner.State`.
- Existing test pattern: `cmd/tui/leader_e2e_test.go:371-420` (`TestLeaderE2EStopFallsBackToVerifiedSignal`: one test, a Linux branch and a refusal branch).

## Overview

`ConnectOrSpawn` evicts a leader that holds the lifetime lock in state `serving`, is older than `ZombieAfter`, passes process verification, and has no connectable socket. On Linux it sends SIGTERM, waits, then SIGKILL, waits, all through the verified pidfd path; then the normal start path runs. On other platforms the signal is refused and the call waits as today.

## Requirements

- `ConnectConfig.ZombieAfter time.Duration` (zero = 30 s) and `ConnectConfig.EvictWait time.Duration` (zero = 8 s); doc comments say what they bound, not why tests need them.
- New `internal/leader/evict.go`:
  - `zombieOwner(o Owner, now time.Time, after time.Duration) bool`: `o.State == OwnerServing && o.PID > 1 && o.Start > 0 && o.Instance != "" && now.Sub(time.Unix(o.Start, 0)) >= after`. Pure.
  - `(c *connector) evict(ctx, owner Owner) (bool, error)`: calls `evictLeaderProcess(p, owner, min(EvictWait, remaining(ctx)))`; on `ErrNotLeaderProcess` returns `false, nil` (keep waiting, today's behavior); then waits within the deadline until `LockHeld` is false (the kernel releases the flock when the process ends). Returns true when the lock is free.
  - The connector remembers the owners it already handled in this call (`map[Owner]bool` keyed by the full record) and never evicts one twice.
- `evictLeaderProcess(paths Paths, owner Owner, wait time.Duration) (evictResult, error)` in `proc_linux.go`, one pidfd for the whole sequence: `PidfdOpen` once; `VerifyLeaderProcess`; liveness poll (`Poll(0)`, as `:82-85`); `verifySignalOwner`; SIGTERM on the fd; `unix.Poll(fd, POLLIN, wait)` (POLLIN means the process ended); when it still lives: `verifySignalOwner` again (a record that changed to `stopping` refuses the kill), SIGKILL on the same fd, `unix.Poll(fd, POLLIN, wait)`. The result says which signals were sent, for the error text. Shared steps with `signalLeaderProcess` go into one small helper in the same file (DRY); `signalLeaderProcess` keeps its signature and behavior for `Stop`.
- `proc_darwin.go` and `proc_other.go`: `evictLeaderProcess` returns `ErrNotLeaderProcess` ("stable process signaling is not available"), as `signalLeaderProcess` does there.
- In `run`, branch `outAbsent` + held + `spawn`: `ReadOwner`; when `zombieOwner` and not handled: `evict`. Lock free → loop goes on to the start path. Not free when the deadline comes → `ErrLeaderStarting` with "sent SIGTERM to leader pid N that holds the lock without a socket; try again" (and "and SIGKILL" when sent).
- `Connect` (spawn false) never evicts. `Status` and `Stop` do not change.
- Only ENOENT and ECONNREFUSED lead here (`attempt`); a socket that accepts and never registers is `outLive` and is never evicted (Grok has the same gap, port report row 9).
- Comments use behavior words only ("a leader that holds the lock but lost its socket"), no "zombie row" or plan labels. The word zombie may appear as a plain description.

## Runtime Flow

- Actor and trigger: a client starts while an earlier leader holds the lock but its socket file was removed or its listener is gone.
- Entry point: `leader.ConnectOrSpawn`, through `tuiclient.LeaderDial` and `ask connect`.
- Internal path: `attempt` → `outAbsent` → `LockHeld` → `ReadOwner` → `zombieOwner` → `evict` (verify, pidfd SIGTERM, wait, pidfd SIGKILL, wait) → lock free → `start` → `attempt` → `outOK`.
- Observable result (Linux): the old PID is gone, a new leader serves with a new instance id. Other platforms: `ErrLeaderStarting` at the deadline, the old process is alive.
- Proof: `TestLeaderE2ELeaderWithoutSocketIsEvicted`, `TestLeaderE2EFrozenLeaderWithoutSocketIsKilled`, `TestStoppingLeaderIsNeverEvicted`.

## File inventory

| File | Action | Test impact |
|---|---|---|
| `internal/leader/evict.go` | create (`zombieOwner`, `evict`) | `evict_test.go`, `evict_linux_test.go`, `evict_darwin_test.go` |
| `internal/leader/proc_linux.go` | modify (`evictLeaderProcess`, shared verify helper) | `evict_linux_test.go`, existing signal tests |
| `internal/leader/proc_darwin.go`, `proc_other.go` | modify (refusing `evictLeaderProcess`) | `evict_darwin_test.go` |
| `internal/leader/client.go` | modify (config fields and defaults, `outAbsent` branch, deadline text, handled set) | `client_test.go` |
| `internal/leader/helper_process_test.go` | create (start: the test runs `exec.Command(abs, "leader")` with `abs` from `os.Executable()`, so the command line starts with the absolute path; helper body: take the lock, write the owner record with `Exe` = that absolute path and `Start` = now, state from the helper variable, optional SIGTERM ignore, optional "turn stopping on SIGTERM", no socket, sleep) | real-process tests |
| `internal/leader/main_test.go` | modify (`TestMain` runs the helper before `goleak.VerifyTestMain` when the helper variable is set) | all |
| `internal/leader/evict_test.go` | create (decision table, platform-neutral) | - |
| `internal/leader/evict_linux_test.go` | create, `//go:build linux` | Linux only |
| `internal/leader/evict_darwin_test.go` | create, `//go:build darwin` | macOS only |
| `cmd/tui/leader_evict_e2e_test.go` | create (both platforms) | E2E |
| `cmd/tui/leader_evict_linux_e2e_test.go` | create, `//go:build linux` | E2E, Linux only |

## Tests Before

Phase 2 state passes. `TestConnectOrSpawnLiveLockNoSocketIsBounded` (a lock without an owner record) passes and must still pass: no state, no eviction.

## Tests After (write first, see them fail)

Platform-neutral unit (`evict_test.go`):

- `TestZombieOwnerDecision`: table: serving + old → true; serving + young → false; stopping → false; state "" (older leader) → false; no instance → false; PID 1 → false.
- `TestConnectWithoutSpawnNeverEvicts`: helper leader in state `serving`, `ZombieAfter` 0; `Connect` returns `ErrLeaderStarting` or `ErrLeaderAbsent` path as today; helper alive.

Real process (helper = test binary run as `<binary> leader` with the helper variable; it holds the real lock):

- `TestZombieLeaderIsEvictedWithSIGTERM` (Linux): helper in state `serving`; `ZombieAfter` 0; `StartFn` = `procLeader.start`; the call succeeds; helper exited by SIGTERM; one start.
- `TestZombieLeaderIsKilledWhenItIgnoresSIGTERM` (Linux): helper ignores SIGTERM; `EvictWait` 500 ms; helper ends by SIGKILL (wait status); the call succeeds.
- `TestStoppingLeaderIsNeverEvicted` (Linux; also compiled on darwin): helper in state `stopping`; deadline 1 s; `ErrLeaderStarting`; helper alive; no start.
- `TestZombieWithUnverifiedOwnerIsNotSignalled` (Linux): the test holds the lock itself and writes a `serving` record that names a `sleep` process (command line is not a leader); no signal; `sleep` alive; `ErrLeaderStarting`.
- `TestZombieThatTurnsStoppingIsNotKilled` (Linux): helper in state `serving` that, on SIGTERM, rewrites its record to `stopping` and keeps running; `EvictWait` 300 ms; no SIGKILL (helper alive after the call); `ErrLeaderStarting`.
- `TestZombieLeaderIsNotSignalledWithoutAStableHandle` (darwin): helper in state `serving`; deadline 1 s; `ErrLeaderStarting`; helper alive; no start.

E2E (built binary):

- `TestLeaderE2ELeaderWithoutSocketIsEvicted` (`leader_evict_e2e_test.go`): `ConnectOrSpawn(e2eConnect)` starts a real leader; read the owner; `os.Remove(paths.Socket)`; second call with `ZombieAfter` 0. Linux: success, new instance id, old PID gone, `leaderProcesses(bin)` has one entry. Other: `ErrLeaderStarting` within deadline + 2 s, old PID alive, lock held; cleanup sends SIGTERM to the old PID (the test started it).
- `TestLeaderE2EFrozenLeaderWithoutSocketIsKilled` (`leader_evict_linux_e2e_test.go`): as above, then SIGSTOP the leader; `EvictWait` 1 s; success; the old PID is gone (killed); new instance id. Cleanup sends SIGCONT and SIGKILL as the existing stop test does.
- Existing `TestLeaderE2EStopFallsBackToVerifiedSignal` passes (a SIGSTOPped leader with its socket accepts connections, so it is not evicted).

## Steps

1. Write the helper and the tests; run on macOS (`go test ./internal/leader ./cmd/tui`); run the Linux tests in the container (phase 6 commands) to see them fail.
2. Implement `evict.go` and the `run` branch.
3. Narrow tests on macOS; Linux container run; gates.

## Validation (gates)

`go test ./internal/leader ./cmd/tui`, `go vet ./...`, `GOOS=linux go vet ./internal/leader ./cmd/tui`, golangci-lint; Linux container run of `-run 'Zombie|Stopping|WithoutSocket|FrozenLeader'` (commands in phase 6).

## Risk

| Risk | Mitigation |
|---|---|
| Signal to a wrong process | Both signals go through one pidfd opened before the process check; record re-read before each signal. A reused PID cannot be reached through the original fd. Tests for an unverified owner and a record that changed. |
| Kill of a draining leader | Only state `serving`; a record that changes to `stopping` after SIGTERM fails `verifySignalOwner`, so no SIGKILL. |
| Helper processes leak in tests | Each test registers a cleanup that sends SIGKILL to the helper and waits; `goleak` stays on in `TestMain` for the test process. |
| Container differences (no `/proc` access, seccomp blocks `pidfd_open`) | The H13b container run passed pidfd tests in `alpine:3`; if `pidfd_open` is blocked, record it and run with the default seccomp profile of Docker Desktop, which allows it on kernel 5.3+. |

## Rollback

Revert the commit. The leader-side state from phase 2 stays harmless without this phase.
