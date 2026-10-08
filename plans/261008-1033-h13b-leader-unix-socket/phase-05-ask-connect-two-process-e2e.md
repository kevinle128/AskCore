---
phase: 5
title: "ask connect and two-process E2E"
status: pending
priority: P1
effort: "1d"
dependencies: [3, 4]
---

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
