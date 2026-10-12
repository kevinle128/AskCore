---
phase: 5
title: "Newer-build replacement"
status: completed
priority: P2
effort: "1.5d"
dependencies: [4]
---

# Phase 5: Newer-build replacement

## Context

- Plan: user decisions (2026-10-11): choice C (busy leader replaced; supersedes H13b "Version gate: A" for same-protocol builds), N1 A (only `SpawnedByClient` leaders; a supervised or hand-started leader stays and gets a hint), N2 A (protocol-mismatch path stays `IfIdle`), N3 B (dev stamp: compared only between dev builds, same hash keeps, other hash and later time replaces, forced); decisions P9 (rule), P10 (mechanism, no leader change), P11 (once per call, reconnect is a call), P13 (dev stamp format).
- Grok: `should_evict` semver floor, never downgrade, unknown keeps ([source anatomy 2.3, 3.1, 3.2](../reports/xia-261011-grok-connect-or-spawn-source.md)).
- Code: `internal/leader/client.go:141-159` (`replaced` flag, `outVersion`), `:144-147` (`outOK`, `hintBuild`), `:222-228` (`hintBuild`), `:288-323` (`replace`: status, `SpawnedByClient`, `IfIdle` shutdown, lock-free wait at `:311-322`), `:327-359` (`manage`: works on a registered connection); `internal/leader/server.go:356-386` (`controlFrame`: forced shutdown allowed on a registered connection, `SpawnedByClient` checked only with `IfIdle`); `spawn.go:207-230` (`ProbeAsk` returns `VersionInfo`); `cmd/tui/tools_leader_e2e_test.go:176-196` (`startToolLeader`), `:332-361` (jobs killed on shutdown); `cmd/tui/connect.go:573` (`connected instance=...` line).
- Consequence: `Server.Close` → `LeaderRuntime.Stop` → host disposes every session → each session's job table kills its process groups (`internal/app/module_leader.go:155-165`).

## Overview

After a successful register, a client whose build is newer than the leader's, and whose binary to start is also newer, stops a client-started leader with a forced shutdown and starts its own. "Newer" means a later commit time between two VCS builds, or another hash with a later modification time between two dev builds. Once per call.

## Requirements

- New `internal/leader/stamp.go`:
  - `type buildStamp struct{ CommitTime, DevStamp string }`, filled from `LeaderRegister`, `LeaderRegistered` or `VersionInfo`.
  - `parseDevStamp(s string) (mod time.Time, hash string, ok bool)`: format `<RFC 3339 nano> sha256:<64 hex>`; anything else is not ok.
  - `newerBuild(client, leader buildStamp) bool`:
    - both `CommitTime` set (and no `DevStamp` on either): both parse as RFC 3339 and client is strictly after leader;
    - both only `DevStamp`: both parse; same hash → false; other hash → client time strictly after leader time;
    - any other pair (a dev build and a VCS build, empty, unparseable) → false.
- In `run`, case `outOK`: when `spawn && !replaced && newerBuild(hello, registered)`, call `c.replaceOlder(ctx, cl)`. On success: `replaced = true`, `child = nil`, continue. On a keep reason: return `cl` and warn once with the reason (supervised, binary not newer, status failed, shutdown refused).
- `hintBuild` also hints when both are dev builds with other hashes and no replacement happened ("the leader runs another dev build; run `ask leader stop` to restart it with this build").
- `replaceOlder(ctx, cl)`:
  1. `exe, info := leaderExecutable + ProbeAsk` (stored in the connector, so the next start reuses them, as in phase 1). Keep when the probe fails or `!newerBuild(stampOf(info), registered)`.
  2. `status` through `manage`; keep when it fails; keep when `!st.SpawnedByClient` (N1 A), with the hint "the leader was not started by a client; restart it with the new binary".
  3. `manage(shutdown{InstanceID: st.InstanceID})` without `IfIdle`; keep when the reply is not `control_result`.
  4. Close `cl`; `waitReleased(ctx)` (extracted from `replace`).
- `replace` (protocol mismatch) keeps `IfIdle` (N2 A) and uses `waitReleased`.
- `Connect` never replaces. `Status` and `Stop` do not change.
- `cmd/tui/tools_leader_e2e_test.go`: `startToolLeaderFrom(t, f, bin)`; `startToolLeader` calls it with `acpBinary(t)`.
- Comments describe the rule ("a leader of an older build that a client started gives way to a newer client, also when it is busy; two dev builds compare by hash and file time; a dev build and a VCS build never replace each other"); no labels.

## Runtime Flow

- Actor and trigger: the user rebuilt `ask` (a release build or a new `go run`) and opens a TUI or `ask connect` (or a TUI reconnects), while a leader of the older build runs work for other clients.
- Entry point: `leader.ConnectOrSpawn` through `tuiclient.LeaderDial`, `ask connect`.
- Internal path: `attempt` → `outOK` → `newerBuild` → probe → `status` → forced `shutdown` → leader `Server.Close` (stopping record, sessions disposed, jobs killed) → lock free → `start` → `attempt` → `outOK` (replaced: no second check).
- Observable result: the old leader process is gone, its job process groups are gone, a new instance serves, older clients reconnect.
- Proof: `TestLeaderE2ENewerBuildReplacesBusyOlderLeader`, `TestLeaderE2ENewerDevBuildReplacesBusyOlderDevLeader`, `TestLeaderE2ESameDevBuildKeepsTheLeader`.

## File inventory

| File | Action | Test impact |
|---|---|---|
| `internal/leader/stamp.go` | create (`buildStamp`, `parseDevStamp`, `newerBuild`) | `stamp_test.go` |
| `internal/leader/client.go` | modify (`replaceOlder`, `waitReleased`, `outOK` branch, `hintBuild`) | `client_test.go` |
| `internal/leader/stamp_test.go` | create | - |
| `internal/leader/client_test.go` | modify (harness and `procLeader` set stamps; fake `ask` prints them) | - |
| `cmd/tui/tools_leader_e2e_test.go` | modify (`startToolLeaderFrom`) | existing tests unchanged |
| `cmd/tui/leader_replace_e2e_test.go` | create (VCS build) | E2E |
| `cmd/tui/leader_dev_replace_e2e_test.go` | create (dev builds) | E2E |

## Tests Before

Phase 4 state passes. `TestReplaceOnlyIdleSpawnedLeader` and `TestReplacementWaitsForTheLockToBeReleased` pass and must still pass (protocol path unchanged).

## Tests After (write first, see them fail)

Unit, decision (`internal/leader/stamp_test.go`):

- `TestNewerBuildDecision`: commit time later → true; equal → false; earlier → false; empty either side → false; not RFC 3339 → false.
- `TestSameDevHashKeepsTheLeader`: same hash, client time later → false.
- `TestNewerDevBuildWithAnotherHashReplaces`: other hash, client time later → true.
- `TestOlderDevBuildKeepsTheLeader`: other hash, client time earlier or equal → false.
- `TestDevAndVCSBuildsAreNeverCompared`: dev client vs VCS leader, VCS client vs dev leader, with any times → false.
- `TestBadDevStampKeepsTheLeader`: no `sha256:`, short hash, bad time → false.

Unit, flow (`internal/leader/client_test.go`; harness leaders set `ServerConfig` stamps; the fake `ask` version script prints a stamp):

- `TestNewerBuildReplacesABusyLeader`: `SpawnedByClient` true, `QuiesceIfIdle` false (busy), older commit time; client and fake `ask` later; `StartFn = procLeader.start`; returns a client of the new leader; old server done; one start.
- `TestNewerDevBuildReplacesABusyLeader`: same with dev stamps (other hash, later time).
- `TestOlderOrUnknownBuildKeepsTheLeader`: subtests older, equal, empty client stamp, empty leader stamp, same dev hash; same instance; zero starts.
- `TestNewerClientKeepsTheLeaderWhenTheBinaryIsNotNewer`: client later, fake `ask` older; keep; warn names the binary.
- `TestNewerBuildKeepsASupervisedLeader`: `SpawnedByClient` false; keep; warn says the leader was not started by a client.
- `TestNewerBuildReplacesOncePerCall`: the new `procLeader` also reports the older stamp; one stop, one start.
- `TestConnectNeverReplaces`: `Connect` with a later stamp; same instance.

E2E (built binaries):

- `TestLeaderE2ENewerBuildReplacesBusyOlderLeader` (`leader_replace_e2e_test.go`, VCS build): `startToolLeader` with a provider fixture that starts a background `bash` job (`recordGroup(pgid)` + `trap '' TERM; sleep 300`); `requireGroupAlive`; read the old PID and instance. A wrapper `ask` in a temp dir: `version` prints the JSON of `<bin> version --json` with `commitTime` one hour later; any other command `exec`s `<bin>`. `ConnectOrSpawn` with `e2eConnect(home, wrapper)`, `CallerIsAsk` true, `Hello.CommitTime` one hour later, the fixture environment. Assert: new instance id; old PID gone; `requireGroupGoneSoon(pgid)`; the first peer's connection closed; `leaderProcesses(bin)` has one entry.
- `TestLeaderE2ENewerDevBuildReplacesBusyOlderDevLeader` (`leader_dev_replace_e2e_test.go`): DEV-OLD (modification time one hour earlier) and DEV-NEW from `devBinaries(t)` (phase 4; real builds whose bytes differ by `-trimpath`; no product flag). `startToolLeaderFrom(t, f, devOld)` with a background job. Run the real process `DEV-NEW connect` with the fixture environment and an empty stdin: its production `leaderHello` computes the dev stamp; its output has `connected instance=<new id>`. Assert: new id differs; old PID gone; job group gone; `DEV-NEW leader status --json` shows the `devStamp` that `DEV-NEW version --json` printed.
- `TestLeaderE2ESameDevBuildKeepsTheLeader`: leader from DEV-OLD; DEV-COPY (byte copy, another name, later modification time) runs `connect`; `connected instance=` shows the same id; old PID alive.
- Existing `TestLeaderE2EAutoStartStatusStop`, `TestLeaderE2ESpawnRace`, `TestTUIStartsLeaderFromABinaryWithAnotherName` pass (same binary, equal stamps: no replacement).

## Steps

1. Write the tests; see them fail.
2. Add `stamp.go`; extract `waitReleased`; add `replaceOlder` and the `outOK` branch; update `hintBuild`.
3. Narrow tests; full gates.

## Validation (gates)

`go test ./internal/leader ./cmd/tui`, `go test ./...`, `go vet ./...`, `GOOS=linux go vet ./internal/leader`, golangci-lint.

## Risk

| Risk | Mitigation |
|---|---|
| Busy work is killed | User decisions C and N3 B; only a strictly newer, probed binary and only a client-started leader (N1 A). E2E proves no job group stays behind. |
| Thrash between two builds | Never downgrade; once per call; same dev hash never replaces; an older client never replaces. |
| A dev binary written later holds older source | Accepted rule ("the binary written last wins"); documented in the README; `ask leader stop` is the fix. |
| The new binary cannot start after the old leader stopped | The probe runs before the stop; phase 1 retries an early exit. |
| The client stops a leader of another instance that started in between | `shutdown` carries `InstanceID`; the leader refuses a mismatch (`server.go:360`). |

## Rollback

Revert the commit: builds go back to the hint only. The wire fields of phase 4 stay harmless.
