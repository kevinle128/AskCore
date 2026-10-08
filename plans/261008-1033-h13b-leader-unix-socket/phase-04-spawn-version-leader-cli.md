---
phase: 4
title: "Spawn, version and leader CLI"
status: pending
priority: P1
effort: "1.5d"
dependencies: [1, 3]
---

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
