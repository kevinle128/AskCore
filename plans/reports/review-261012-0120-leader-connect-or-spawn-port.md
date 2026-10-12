# Leader connect-or-spawn requirement review

Date: 2026-10-12.
Scope: [accepted plan](../261011-2304-leader-connect-or-spawn-port/plan.md), all six phase files, their code, tests and documentation.

## Result

The three requested functions are present.
The review found four defects in their safety boundaries and fixed all four.
Each defect has a built-binary E2E test that failed before the fix and passed after it.
An independent code reviewer checked the repairs and reported no remaining findings.

## Requirement checks

| Requirement | Result | Evidence |
|---|---|---|
| Retry an early failed start, at most three starts | PASS | `internal/leader/client.go`; `TestLeaderE2ESpawnRetriesAfterEarlyExit`, `TestLeaderE2ESpawnGivesUpAfterThreeStarts` |
| Find and probe the executable once; do not retry probe or exec errors | PASS | `prepare`, `start`; `TestConnectOrSpawnDoesNotRetryAStartError`, `TestConnectOrSpawnIncompatibleAsk` |
| Keep the final start error, start count and log tail | PASS | `startupFailure`; retry unit and E2E tests |
| Record serving and stopping state; accept older records without state | PASS | `lock.go`, `Server.Close`, `LeaderParams.Stopping`; owner and lifecycle tests |
| Evict only an old serving owner with an absent or refused socket | PASS | `evict.go`, `attempt`; owner decision and Linux eviction tests |
| Verify the process and unchanged lock owner; use one pidfd for both signals | PASS | `verifiedPidfd`, `verifySignalOwner`, `evictLeaderProcess`; unverified-owner and stopping tests |
| Bound signal waits and stop escalation on deadline or cancellation | PASS after repair | `proc_linux.go`; the two new Linux E2E tests below |
| Never evict on macOS or signal a stopping or legacy owner | PASS | Platform refusal, `zombieOwner`; macOS E2E and Linux owner tests |
| Send optional commit and dev stamps without a protocol bump | PASS | `pkg/protocol/leader.go`, `handshake.go`, `router.go`, `cmd/tui/version.go`; wire, status and version tests |
| Compute dev stamps once; retain them after executable removal | PASS | `buildStamps`; `TestLeaderDevStampSurvivesARemovedBinary` |
| Replace a busy older client-started leader and stop its jobs | PASS | `replaceOlder`; VCS and dev replacement E2E tests |
| Keep supervised leaders, older or unknown builds, equal hashes and mixed stamp types | PASS | `newerBuild`, `replaceOlder`; stamp decision and client flow tests |
| Probe the next binary before stopping; replace at most once per call | PASS | `prepare`, `replaced`; client replacement tests |
| Preserve a concurrent replacement without stopping it | PASS after repair | Instance comparison in `replaceOlder`; new concurrent replacement E2E |
| Keep protocol-mismatch replacement idle-only | PASS | `replace` still sends `IfIdle`; `TestReplaceOnlyIdleSpawnedLeader` |
| Let `Connect` connect without replacing a leader | PASS after repair | `run` checks `spawn` on protocol mismatch; new E2E below |
| Update owning docs and the earlier decision supersession note | PASS | Leader README, architecture section 7.3, protocol README, `CLAUDE.md`, earlier leader plan |

## Fixed defects

1. `Connect` entered protocol replacement without checking `spawn`.
   It stopped the leader and returned `leader: no leader is running`.
   The new guard returns `ErrVersionRefused` and preserves the leader.
   Regression: `TestLeaderE2EConnectKeepsAnOlderProtocolLeader`.
2. `replaceOlder` compared the initial leader's stamp but used a later status instance for shutdown.
   A replacement that appeared during the binary probe was stopped even when it already ran the new build.
   The client now compares the two instance IDs and reconnects to a changed instance without stopping it.
   Regression: `TestLeaderE2ENewerBuildKeepsAConcurrentReplacement`.
3. Eviction sent SIGKILL after its deadline expired.
   The signal path now checks the deadline before both signals.
   Poll waits round up to milliseconds and keep the same wait end across `EINTR`.
   Regression: `TestLeaderE2EEvictionStopsAtTheDeadline`.
4. Eviction received only a deadline, so cancellation did not stop the wait or SIGKILL escalation.
   The path now receives the context, checks cancellation during waits and before signals, and checks the wall-clock deadline even if its timer callback has not run.
   Regression: `TestLeaderE2ECancelledEvictionDoesNotKill`.

## Fresh validation

- `go test ./internal/leader ./cmd/tui ./internal/app ./pkg/protocol`: PASS.
- `go test ./... -count=1`: PASS, including the new protocol and concurrent replacement E2E tests.
- `go test -race ./internal/leader -count=1`: PASS.
- Focused replacement and connection tests after the context change: PASS.
- `go test ./internal/tuiclient -count=1`: PASS.
- `go vet ./...`: PASS.
- `GOOS=linux go vet ./internal/leader ./cmd/tui`: PASS.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`: PASS, zero issues.
- `git diff --check`: PASS.

Linux full leader suite:

```sh
GOOS=linux GOARCH=arm64 go test -c -o /tmp/ask-leader-connect-audit-linux.test ./internal/leader
docker run --rm -v /tmp/ask-leader-connect-audit-linux.test:/leader.test:ro alpine:3 /leader.test -test.count=1
```

Linux E2E ran in `golang:1.27`, with the repository and module cache mounted read-only and a container-local temporary directory.
The run used `GOPROXY=off`, `GOFLAGS=-buildvcs=false` and `GOTOOLCHAIN=local`.
Retry, dev replacement, same-hash retention, concurrent replacement, read-only connection, TERM eviction, KILL escalation, stop fallback, state recording and dev stamp tests passed.
After the final signal guards, both deadline and cancellation E2E tests passed again.
The VCS-stamped E2E tests passed on macOS; the Linux worktree mount does not support VCS stamping.
The full macOS suite preceded the final private Linux context change; subsequent focused macOS tests, Linux tests, vet and lint checked that change.

## Evidence limits

The original implementation's test-first sequence cannot be proved from the current tree alone.
This review proves the four new regressions through failing E2E runs before their fixes and passing runs after them.
Linux eviction remains deliberately unavailable on macOS.
A peer that accepts a socket but never registers remains outside this plan's recovery scope.
No dependency, wire schema or public Go signature changed in this review.

## Unresolved questions

None.
