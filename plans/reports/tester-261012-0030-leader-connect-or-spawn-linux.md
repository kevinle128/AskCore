# Leader connect-or-spawn: Linux and macOS test evidence

Date: 2026-10-12. Host: macOS arm64, Go 1.27.0. Linux: Docker Desktop VM, kernel 6.12.76 linuxkit, arm64.

## Linux commands and results

1. Unit and real-process tests of `internal/leader` (whole package), alpine:3, a single-file mount so that `/tmp` is container-local:

   ```
   GOOS=linux GOARCH=arm64 go test -c -o /tmp/ask-leader-linux.test ./internal/leader
   docker run --rm -v /tmp/ask-leader-linux.test:/leader.test:ro alpine:3 \
     sh -c 'cd /tmp && /leader.test -test.count=1 -test.v'
   ```

   Result: PASS, no failure. Includes `TestZombieLeaderIsEvictedWithSIGTERM`, `TestZombieLeaderIsKilledWhenItIgnoresSIGTERM`, `TestZombieWithUnverifiedOwnerIsNotSignalled`, `TestZombieThatTurnsStoppingIsNotKilled`, `TestStoppingLeaderIsNeverEvicted`, `TestStopSignalsAVerifiedLeaderProcess`, the newer-build and stamp tests.

2. E2E of `cmd/tui` in golang:1.27 (Go 1.27.2 linux/arm64), repository and host module cache mounted read-only, `GOPROXY=off GOFLAGS=-buildvcs=false`:

   ```
   docker run --rm -v "$PWD":/src:ro -v "$(go env GOMODCACHE)":/modcache:ro -w /src \
     -e GOMODCACHE=/modcache -e GOPROXY=off -e GOFLAGS=-buildvcs=false -e GOTOOLCHAIN=local golang:1.27 \
     go test ./cmd/tui -count=1 -v -run 'TestLeaderE2E(LeaderWithoutSocket|FrozenLeader|SpawnRetries|SpawnGivesUp|StopFallsBack|AutoStartStatusStop|NewerDevBuild|SameDevBuild)|TestLeaderDevStamp|TestDevStamp|TestVersionJSONCarriesDevStamp|TestLeaderRecordsStopping'
   ```

   Passed: `TestLeaderE2ELeaderWithoutSocketIsEvicted` (Linux branch: new instance, old PID gone), `TestLeaderE2EFrozenLeaderWithoutSocketIsKilled` (SIGSTOP, SIGKILL), `TestLeaderE2ESpawnRetriesAfterEarlyExit`, `TestLeaderE2ESpawnGivesUpAfterThreeStarts`, `TestLeaderE2EStopFallsBackToVerifiedSignal`, `TestLeaderE2EAutoStartStatusStop`, `TestLeaderE2ENewerDevBuildReplacesBusyOlderDevLeader`, `TestLeaderE2ESameDevBuildKeepsTheLeader`, `TestLeaderRecordsStoppingBeforeItStopsServing`, `TestVersionJSONCarriesDevStamp`, `TestDevStampFollowsTheFile`, `TestLeaderDevStampSurvivesARemovedBinary`.

## Not run on Linux

`TestVersionJSONCarriesCommitTime` and `TestLeaderE2ENewerBuildReplacesBusyOlderLeader` need a VCS-stamped binary. The worktree `.git` file points outside the mount, so the container runs with `-buildvcs=false`. Both passed on macOS.

## macOS

`go test ./...`, `go vet ./...`, `GOOS=linux go vet ./internal/leader ./cmd/tui` and golangci-lint (0 issues) pass. `cmd/tui` took about 280 s. Two full runs of `cmd/tui` each had one unrelated failure (`TestClientInProcessAgent` in one, `TestInteractiveSecretNeverOnScreen` in the other); both passed alone and a third full run passed. They are timing flakes under load.

## Environment fact

A first container run mounted the host `/tmp` into the container. `flock` is not honoured on that bind mount: `TestStopRefusesUnverifiedPID` and `TestStopSignalsAVerifiedLeaderProcess` failed and the eviction test hung. The single-file mount above fixes it. This is a property of the container setup, not of the product.

## Unresolved questions

1. The newer-build E2E fails with a clear message, and does not skip, when the binary has no VCS stamp. Confirm this is acceptable.
