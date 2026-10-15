---
phase: 4
title: "Spawn, version and leader CLI"
status: completed
priority: P1
effort: "1.5d"
dependencies: [1, 3]
---


## Current repair state (2026-10-09)

The socket shutdown control is the first stop path.
[proc_linux.go](../../internal/leader/proc_linux.go) uses a pidfd, verifies the process and current lock owner, and signals only through that handle.
Linux refuses fallback when the handle cannot be obtained or verified.
[proc_darwin.go](../../internal/leader/proc_darwin.go) and [proc_other.go](../../internal/leader/proc_other.go) refuse PID signal fallback because they have no stable process handle.
There is no current macOS check-to-kill fallback.
[leader_regression_test.go](../../cmd/tui/leader_regression_test.go) checks replacement-socket preservation and the complete command path for a second signal during a started tool drain.
The foreground SIGTERM E2E test now uses synchronized output collection.
The full Linux leader test binary passed in `alpine:3` after repair.
The final exit gates passed; the [repair report](../reports/pm-261009-h13b-repairs.md) records their evidence and accepted limits.

# Phase 4: Spawn, version and leader CLI

Outline.
Scout `cmd/tui` dispatch and usage at cook time.

## Goal

`ask leader` runs the Phase 3 composition; `ConnectOrSpawn` connects or starts it safely from either binary; `ask leader list|status|stop` manage it; `ask version --json` gives a machine-readable identity.

## Files to Create / Modify (candidate)

- Create: `internal/leader/client.go` (`Connect`, `ConnectOrSpawn` with bounded deadlines and error classes)
- Create: `internal/leader/spawn.go`, `spawn_unix.go` (binary lookup, version probe, `Setsid`, null stdin/stdout, private append log, size rotation, child reaping)
- Create: `cmd/tui/leader.go` (`ask leader [--spawned-by-client]`, `list`, `status`, `stop`)
- Create: `cmd/tui/version.go` (`ask version --json`; build identity from `debug.ReadBuildInfo`)
- Modify: `cmd/tui/headless.go` (dispatch `leader`, `version` before prompt parsing, as `acp`), `cmd/tui/args.go` (usage), `cmd/tui/acp.go` (ACP `Implementation.Version` from the same build source)

## Contracts

- **ConnectOrSpawn.** Try connect, register, ready under one overall deadline.
  Error classes: absent endpoint, live startup, incompatible version, unsafe access, malformed peer, startup failure.
  Only absent or stale endpoints spawn.
  Unsafe access and malformed peers never spawn.
  A version mismatch never starts a competing leader; it may use the explicit conditional replacement path below.
  A live flock with no usable socket is bounded by the deadline.
- **Binary lookup.** `ask` beside the caller's executable, then `PATH`.
  Probe `ask version --json` before spawn; missing or incompatible binary gives a clear error.
- **Spawn race.** Competing children: one wins flock; losers exit without touching winner artifacts; clients adopt the winner.
- **Version (maintainer decision: protocol only).** Mismatch rejects with an upgrade hint.
  Automatic replacement only on protocol mismatch, only for a `--spawned-by-client` leader, only after `QuiesceIfIdle` succeeds over the `shutdown` control and flock release is observed.
  Build difference prints a one-line stderr hint.
- **Stop.** `shutdown` control first.
  Ordinary stop may stop a busy owned leader; replacement requires an idle-only conditional shutdown.
  PID fallback only after verifying the PID's UID and that its command is `ask leader`; refuse a signal otherwise.
  Stale reused PID is never signalled.
- **Log.** `~/.ask/leader.log`, 0600, append, rotate by size at spawn to `leader.log.1`; never overwrite the previous failure.

## TDD (RED list)

- `TestConnectOrSpawnStartsOneLeader`, `TestConnectOrSpawnNoSpawnOnVersionOrPermissionError`
- `TestConnectOrSpawnLiveLockNoSocketBounded`
- `TestConnectOrSpawnFindsSiblingAsk`, `TestConnectOrSpawnAskMissingFromPATH` (helper caller binary in another cwd stands in for `ask-server`; daemon conversion stays in H13c)
- `TestSpawnRaceOneWinner`
- `TestReplaceOnlyIdleSpawnedLeader` (busy and supervised leaders kept; supplemental in-process version skew)
- `TestStopRefusesUnverifiedPID` (stale PID pointing at an unrelated process)
- `TestLeaderLogRotation`
- `TestVersionJSON`, `TestLeaderStatusRedacted`
- Built-binary: `TestLeaderE2EStop` (socket removed, lock inode kept, PID cleared)

## Verification

```sh
go test -race ./internal/leader/... -count=1
go test ./cmd/tui/ -run 'Leader|Version' -count=1
```

## Risks

- Orphaned test leaders: every test records child PID and home and stops its own leader in `t.Cleanup`.
- Signalling a wrong process: identity check test with a real unrelated PID.

## Management and process safety

The Phase 1 handshake must expose the minimal version-stable management path after a mismatch.
No ACP is accepted on that connection.
An idle replacement request carries the expected instance id and `idleOnly: true`.
The server checks instance, spawned ownership and host admission in one operation.
A status response followed by an unconditional shutdown is not an idle handshake.
Wait for shutdown acknowledgment, socket closure and lock release under the same overall deadline, then re-connect to detect a winner before spawning.
Busy or supervised leaders return an upgrade hint and stay alive.

One overall deadline includes binary discovery, version probe, child start, register, readiness and replacement.
Kill and reap a hung version-probe child that the caller started.
A viable sibling is selected before PATH; an incompatible or unsafe sibling gives a clear error rather than silently running a different executable.
Version output must be valid bounded JSON with a supported protocol integer.
Reuse the validated absolute executable path for spawn.

Do not signal solely from a PID and command-name snapshot.
Include instance identity and process start identity in owner metadata; refuse fallback when identity cannot be verified safely on the platform.
On Linux, use a verified process handle when supported; on macOS, disclose the check-to-signal limitation and fail closed when a stable target cannot be established.
The shutdown control is the normal stop path.
Never unlink a socket after releasing the flock, and never unlink a socket inode that no longer belongs to this instance.
Only the flock winner rotates the log; concurrent spawning callers must not rotate the live leader's log.
Keep stderr log ownership checks before append and rotation.

Add tests for mismatch management controls with ACP refused, a run admitted between status and idle shutdown, supervised leader replacement refused, hung probe reaped, ready timeout, unsafe endpoint with no child spawned, socket identity changed during cleanup, and losing spawns that cannot rotate the winner log.

## Historical review (2026-10-08)

### What was built

- **`internal/leader`.**
  - `client.go`: `Connect`, `ConnectOrSpawn`, `Status`, `Stop`. One deadline covers connect, binary discovery, probe, start, register, readiness and replacement. A failure is sorted: absent or stale endpoint (may start a leader), a leader that holds the lock but does not answer (waits, never starts a second one), unsafe access, a peer that is not a leader, a version refusal, a startup failure with the log tail. Only the first sort starts a process.
  - `spawn.go`: `FindAsk` (sibling of the caller before the PATH; an unusable sibling is an error), `ProbeAsk` (bounded `ask version --json`, hung child killed with its process group and reaped), `StartLeader` (own session, null input and output, private append log, reaped), `LockHeld`, `AcquireWait`, `RotateLog`, `LogTail`, `VerifyLeaderProcess`.
  - `proc_darwin.go` (ps), `proc_linux.go` (/proc), `dup_*.go`: process identity and the redirect of stderr to the log.
  - Lock metadata now holds PID, start time and instance id.
- **`cmd/tui`.** `ask leader` (foreground; `--spawned-by-client`), `ask leader status|list|stop`, `ask version [--json]`. `ask acp` reports the same build identity. The shared `acpParams` feeds both `ask acp` and `ask leader`.
- **Server.** A conditional shutdown (`ifIdle`) is accepted only from a leader that a client started, and only after the router quiesces on its own goroutine.

### Evidence

| Check | Result |
|---|---|
| `go test -race ./internal/leader ./internal/acp ./internal/app ./pkg/protocol` | pass; `internal/leader` also passed 12 runs in a row |
| `go test ./cmd/tui/...` | pass (65 s) |
| `golangci-lint run ./...` and `go vet ./...` | clean |
| `internal/leader` test binary in an `alpine:3` arm64 container (Linux `/proc`, `Dup3`) | pass, 7 runs |
| Built binary: auto start, status, list, stop (socket 0600, home 0700, lock and log 0600, lock inode kept, PID record cleared, process gone, clients told) | `TestLeaderE2EAutoStartStatusStop` |
| Built binary: spawn race of four clients, one lock owner | `TestLeaderE2ESpawnRace` |
| Built binary: a second binary (stands in for `ask-server`) finds `ask` next to itself, then on the PATH, from another directory, and fails with a clear message when it is nowhere | `TestLeaderE2ETwoBinaryResolver` |
| Built binary: startup failure reported with the reason from the log; log rotation by the lock winner; foreground leader exit 143 on SIGTERM and a second leader exits 0 | `TestLeaderE2E*` |
| Headless mode starts no leader | `TestLeaderE2EHeadlessStartsNoLeader` |
| Signal fallback: a process that is `ask leader` by command line and by recorded start gets SIGTERM; an unrelated live process or a stale PID never does | `TestStopSignalsAVerifiedLeaderProcess`, `TestStopRefusesUnverifiedPID`, `TestStopIgnoresAStalePIDInTheLockFile` |
| Version skew (supplemental, in process): only an idle leader that a client started is replaced; busy, supervised and newer leaders stay | `TestReplaceOnlyIdleSpawnedLeader`, `TestVersionMismatchOlderClientIsRefusedWithoutReplacement` |

### Changes from the outline

- The mismatch path does not keep one connection for several commands. Each refusal closes the management connection, and a client sends one command for each connection.
- `ask leader` builds the whole composition before it opens the socket, so a leader that cannot start never has a socket (`ASK_FAUX_TPS=fast` is the real fault used in the test).
- A status request registers as a normal client, so it counts itself in `clients`.
- The leader that a person starts is not touched by a client: `SpawnedByClient` is false and the conditional shutdown is refused.

### Known limits

- The original macOS fallback had a check-to-signal gap.
  That fallback is removed.
  Current macOS stop requires the socket shutdown control and refuses PID signaling.
- Linux fallback now requires the verified pidfd path described above.
- `go build ./...` could not be run because of a local command hook; `go vet ./...` and the test suites compile every package.
