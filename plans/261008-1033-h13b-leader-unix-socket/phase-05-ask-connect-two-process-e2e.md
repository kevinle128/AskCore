---
phase: 5
title: "ask connect and two-process E2E"
status: completed
priority: P1
effort: "1d"
dependencies: [3, 4]
---


## Current repair state (2026-10-09)

[connect.go](../../cmd/tui/connect.go) commits Follow ownership on the reader path before it releases the result waiter.
This preserves the first live event and resync frame while retaining the fence for ended subscriptions.
[leader_regression_test.go](../../cmd/tui/leader_regression_test.go) checks the controlled scheduling gap.
[connect_e2e_test.go](../../cmd/tui/connect_e2e_test.go) checks a built client with a burst immediately after the Follow result and compares the complete event sequence.
The final exit gates passed; the [repair report](../reports/pm-261009-h13b-repairs.md) records their evidence and accepted limits.

# Phase 5: `ask connect` and two-process E2E

Outline.
Scout `cmd/tui/acp_e2e_test.go` helpers (`acpBinary`, `e2ePeer`, fixtures) at cook time.

## Goal

A real line-oriented client `ask connect` and built-binary E2E tests that prove the roadmap exit: two `ask` client stubs share one auto-started leader.

## Files to Create / Modify (candidate)

- Create: `cmd/tui/connect.go` (stdin lines → prompts; commands `/new`, `/sessions`, `/attach <id>`, `/detach`, `/take`, `/cancel`, `/quit`; prints session updates and results; uses only `ConnectOrSpawn`, no direct fallback)
- Create: `cmd/tui/connect_test.go` (argument and command parsing)
- Create: `cmd/tui/leader_e2e_test.go` (socket peer helper next to `e2ePeer`; two-process tests)
- Modify: `cmd/tui/headless.go`, `args.go` (dispatch and usage for `connect`)

`internal/tui` is not touched; rendering stays in T1a.

## Contracts

- `ask connect` exits 0 on `/quit` or EOF; exit detaches only; never disposes the shared host.
- Reconnect gives a new client id and explicit attach; prompts are never resent.
- Faux provider for normal runs; `ASK_FAUX_TPS` for long runs; H13a external HTTPS fixture style only where provider/auth streams are needed.

## TDD (RED list, built binary)

- `TestLeaderE2ETwoClientsShareLeader` (roadmap exit)
- `TestLeaderE2ESpawnRace`
- `TestLeaderE2EIndependentSessions` (different cwd and capabilities)
- `TestLeaderE2EAttachObserve`
- `TestLeaderE2EDriverKilledRunSurvives`
- `TestLeaderE2ETakeAfterDriverLoss`
- `TestLeaderE2EBusyForeignSwitchRejected`
- `TestLeaderE2ENoClientsSessionSurvives`
- `TestLeaderE2EReconnectFollowsFromCursor` (stale cursor gives resync; no duplicate admission)
- `TestLeaderE2EIDsNeverCollide`
- `TestLeaderE2ESlowClientDoesNotBlockOthers`
- `TestLeaderE2EAttachSeesPendingQuestion` (transport peer fixture; disclosed)
- `TestLeaderE2EVersionMismatchWhileToolRuns` (supplemental skew server; existing client's tool run unaffected)

## Verification

```sh
go test ./cmd/tui/ -run 'LeaderE2E|Connect' -count=1 -v
go test -race ./cmd/tui/ -run 'LeaderE2E' -count=1
```

## Risks

- Flaky timing: use event-driven waits on output lines, not sleeps.
- Leftover processes: per-test isolated home, `t.Cleanup` stops owned leader and clients; final `lsof`/`ps` check in the test.

## Client and evidence rules

The input loop must remain usable during a prompt so `/cancel`, `/take`, `/detach` and `/quit` can be read while the result is pending.
One socket reader demultiplexes responses and notifications; one socket writer serializes complete frames.
Do not let an SDK client with a 10 MiB parser silently narrow the accepted socket contract.
Print the instance id, current session and cursor in stable output so process tests can compare them.
Expose only capabilities that this line client implements.
Use raw socket clients for differing capability bodies; keep the two actual `ask connect` processes for the runnable exit.
Resolve EOF and quit once; late responses cannot change a detached view.

Add built-client tests for cancel during a pending prompt, EOF during a run, failed attach preserving the view, and reattach that ignores events from an ended subscription.
The slow-reader case uses a raw socket peer alongside the real built client, with an explicit resume latch.
Assert that the leader stays alive, its instance stays the same, and all retained frames arrive in order.
A skew server and reverse transport peer remain supplemental evidence, even when a built client is used.
Do not label them as production tool E2E.

## Historical review (2026-10-08)

### What was built

`ask connect` (`cmd/tui/connect.go`): a client that reaches the leader only through `ConnectOrSpawn` (no agent of its own), creates a session or attaches to one, and reads one line of stdin at a time. One goroutine reads the socket and splits answers from notifications and questions; one lock keeps complete frames from interleaving on the write side. A prompt runs in its own goroutine, so `/cancel`, `/take`, `/detach` and `/quit` work while it is pending. The client uses the leader frame codec with the 64 MiB limit, not the ACP SDK, so no 10 MiB parser narrows the socket contract. It sends no file or terminal capability. Output is one line for each event with a stable first word. `ask connect --help` lists the commands.

### Evidence (built `ask` binary, isolated short home, real sockets, only processes the test started)

| Test | What it proves |
|---|---|
| `TestLeaderE2ETwoClientsShareLeader` | the roadmap exit: two `ask connect` processes, one auto-started leader, the same instance id, the same updates of one run, one leader process |
| `TestLeaderE2EIndependentSessions` | different directories give different sessions; a session that B never joined sends B nothing |
| `TestLeaderE2EObserverCannotDriveAndFailedAttachKeepsView` | an observer is refused with the not-driver code; a failed attach leaves the current session |
| `TestLeaderE2EDriverKilledRunSurvivesAndAnotherTakes` | SIGKILL of the driver in the middle of a run; the run settles, the leader is the same instance, another client takes the session and prompts |
| `TestLeaderE2EBusyForeignSwitchRejected` | a take of a busy session with a live driver is refused with the busy code; the driver finishes |
| `TestLeaderE2ECancelDuringPendingPrompt` | `/cancel` is read while the prompt is pending and the result is `cancelled` |
| `TestLeaderE2EQuitDuringRunLeavesTheRun` | `/quit` mid-run exits 0 and the run still settles for the follower |
| `TestLeaderE2EEndOfInputWaitsForThePrompt` | end of stdin waits for the result, then leaves with 0 |
| `TestLeaderE2ESessionSurvivesWithoutClients` | with no client connected the session stays; a later client sees its history |
| `TestLeaderE2EReconnectFollowsFromCursor` | a new client follows from the last cursor (`resumed=true`), no prompt is sent again, no run is admitted; a stale epoch gives `resync=true` |
| `TestLeaderE2EIDsNeverCollide` | two real clients number their requests from 1 and send 40 each; every answer reaches its sender |
| `TestLeaderE2ESlowClientDoesNotBlockOthers` | a raw socket peer attaches and stops reading; the real client finishes four runs of about 2000 updates each, the leader stays the same instance, and the slow peer later reads the same frames in the same order |
| `TestLeaderE2ESpawnRace`, `TestLeaderE2ETwoBinaryResolver`, `TestLeaderE2EHeadlessStartsNoLeader` | from Phase 4 |

`go test ./cmd/tui/...` and `golangci-lint run ./...` pass.

### Changes from the outline

- End of stdin waits for the prompts in flight, so `echo hello | ask connect` prints its result. `/quit`, a signal and SIGKILL detach at once.
- The two scenarios that need a question from the agent (`TestLeaderE2EAttachSeesPendingQuestion`) and a running tool during a version skew (`TestLeaderE2EVersionMismatchWhileToolRuns`) cannot run in the built binary: no product part issues a reverse call yet, and a built binary has one protocol version. They are covered in process with disclosed fixtures: the question rules in `internal/leader/router_reverse_test.go` (scripted agent), the version case in `internal/app/leader_routing_test.go` (`TestLeaderQuiesceThroughManagement`: a client of another protocol version asks for an idle shutdown while a run is active, and the run is unaffected). They are supplemental and not production-tool E2E.
- Two clients in a test send their prompts one at a time. A session takes one run, so a second prompt to a busy session is refused with the busy code, as designed.

### Found on the way, outside H13b

A prompt of about 130 KiB makes the faux model path slow (about 180 updates in 60 seconds) while 32 KiB takes 50 ms. It is in the agent or the faux provider, not in the leader. The tests use 32 KiB or less.
