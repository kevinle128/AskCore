---
phase: 6
title: "Linux proof, docs and supersession note"
status: completed
priority: P2
effort: "0.5d"
dependencies: [5]
---

# Phase 6: Linux proof, docs and supersession note

## Context

- Plan: [Test matrix](plan.md#test-matrix) (what macOS cannot prove), [Acceptance](plan.md#acceptance).
- Linux method from H13b: `plans/reports/worker-261009-1057-router-safety.md:38-39` (`GOOS=linux GOARCH=arm64 go test -c ./internal/leader` run in `alpine:3`), `plans/reports/pm-261009-h13b-repairs.md:44,65`.
- Docs to update: `internal/leader/README.md:41` (handshake: build only a hint), `:53-59` (lifecycle, replacement); `docs/ask-architecture-reference.md:282-307` (line 298 "Ask does not copy zombie-leader eviction", line 307 version rule); `CLAUDE.md:79` (`ask leader` line); `pkg/protocol/README.md:14,29` (leader payloads); H13b plan decision table line 65.

## Overview

Run the Linux-only rows in containers and record the result; update the docs to the implemented behavior; add the supersession note to the H13b decision table.

## Requirements

Linux proof (no command holds the word for compiling a binary):

1. `GOOS=linux GOARCH=arm64 go test -c -o /tmp/ask-leader-linux.test ./internal/leader`, then `docker run --rm -v /tmp:/tmp alpine:3 /tmp/ask-leader-linux.test -test.v -test.run 'Zombie|Stopping|Signal|Verify|Spawn|Replace|Newer'`, then the full suite of that binary.
2. `cmd/tui` E2E: `docker run --rm -v "$PWD":/src -v ask-gomod:/go/pkg/mod -w /src/cmd/tui -e GOFLAGS=-buildvcs=false golang:1.27 go test -run 'TestLeaderE2E(LeaderWithoutSocket|FrozenLeader|SpawnRetries|SpawnGivesUp|StopFallsBack|AutoStartStatusStop)' -count=1 .` (`-buildvcs=false` because the worktree `.git` file names a path outside the mount; the R3 E2E needs a stamp and runs on macOS).
3. Write `plans/reports/tester-261011-<time>-leader-connect-or-spawn-linux.md`: commands, Go and kernel versions, pass list, any skip with its reason. No secret, no home path of the user beyond the repo.

Docs (read each file before the edit; smallest owning surface):

- `internal/leader/README.md`: handshake line: build identity plus `commitTime`; lifecycle: owner record state line (`serving`, `stopping`), a start that ends with an error is retried up to three times, a lock holder without a socket in state `serving` older than 30 s is stopped on Linux (SIGTERM, then SIGKILL, by pidfd) and refused on macOS; replacement: protocol mismatch stays idle-only; a newer build replaces a client-started leader also when busy, which closes its sessions and kills their jobs; once per call; never a downgrade; a leader started by hand or by a supervisor stays and the client prints a hint. "Newer": a later commit time between two VCS builds; between two dev builds (no VCS stamp, such as `go run`) another SHA-256 of the executable and a later file modification time ("the binary written last wins"; the same bytes never replace each other); a dev build and a VCS build never replace each other.
- `docs/ask-architecture-reference.md` 7.3: replace line 298's "zombie-leader eviction" item with the Linux eviction rule and keep the rest of the list (acquire-slot guard, relay, auto-update relaunch); update line 307 with the build-stamp rule (commit time, dev stamp).
- `CLAUDE.md` line 68 says "a leader lost later is reconnected, never replaced" (meaning: the TUI never swaps to the in-process agent). Keep the meaning and make it exact: "a leader lost later is reconnected (the TUI never switches to an in-process agent)", because after this plan a reconnect can start a new leader.
- `CLAUDE.md` line 79: one sentence: "A newer build replaces an older leader that a client started, also when it is busy (its sessions end); for `go run` builds the binary written last wins unless its bytes are the same; on Linux a leader that holds the lock without a socket is stopped." Keep the rest.
- `CLAUDE.md` line 81 (`version --json`): add `commitTime` or `devStamp` to the listed output.
- `pkg/protocol/README.md`: name `commitTime` and `devStamp` among the leader payload fields; the control schema sentence stays.
- H13b plan, decision table row "Version gate": append "**Superseded in part (user, 2026-10-11, choice C):** for the same protocol, a client whose binary is newer (later commit time, or for builds without a VCS stamp another hash and a later file time) replaces a leader that a client started, also when it is busy. The protocol-mismatch path stays as decided here (see N2 of [leader connect-or-spawn plan](../261011-2304-leader-connect-or-spawn-port/plan.md))." Also add to the non-goals line 54 after "zombie eviction": "(brought in for Linux by the same plan)". No other change to the completed H13b plan.

## Runtime Flow

Not a runtime change. Evidence for the Linux rows of the Runtime Flow Proof Matrix: `TestLeaderE2ELeaderWithoutSocketIsEvicted` (Linux branch), `TestLeaderE2EFrozenLeaderWithoutSocketIsKilled`, `TestZombieLeaderIsEvictedWithSIGTERM`, `TestZombieLeaderIsKilledWhenItIgnoresSIGTERM`, `TestStoppingLeaderIsNeverEvicted`, `TestZombieWithUnverifiedOwnerIsNotSignalled`, `TestZombieThatTurnsStoppingIsNotKilled`.

## File inventory

| File | Action |
|---|---|
| `internal/leader/README.md` | modify |
| `docs/ask-architecture-reference.md` | modify (7.3) |
| `CLAUDE.md` | modify (leader line) |
| `pkg/protocol/README.md` | modify |
| `plans/261008-1033-h13b-leader-unix-socket/plan.md` | modify (one note, one parenthesis) |
| `plans/reports/tester-261011-*-leader-connect-or-spawn-linux.md` | create |

## Tests Before

Phases 1-5 pass on macOS.

## Tests After

- The Linux container runs pass (both commands above).
- Full gates on macOS: `go test ./...`, `go vet ./...`, `GOOS=linux go vet ./internal/leader ./cmd/tui`, `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`.
- Docs claims checked against code: each new README sentence names a symbol that exists (`Owner.State`, `ZombieAfter`, `EvictWait`, `CommitTime`, `DevStamp`).

## Steps

1. Linux runs; write the report.
2. Docs edits; check links.
3. Final gates; update the plan Runtime Flow statuses to PASSED with the test names (through the plan CLI when it covers row status; else edit the table only).

## Risk

| Risk | Mitigation |
|---|---|
| The `golang:1.27` image is not available | Use the newest image that satisfies `go.mod`; record it. |
| Docker Desktop seccomp blocks `pidfd_open` | Record it; H13b ran pidfd tests in the same setup with success. |

## Rollback

Revert the docs commit. The report is evidence and stays.
