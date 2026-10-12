---
title: "H13 slice: session/close through the leader"
description: "Serve ACP session/close in the adapter and route it through the leader, so that closing one session ends its run and background jobs."
status: completed
priority: P1
branch: new-session
tags: [acp, leader, h13, tdd]
created: 2026-10-12
tdd: true
---

# H13 slice: `session/close` through the leader

User decision 2026-10-12 (open question 3 of the [N1 design](../261012-0155-t1b-job-notices/design.md), option A): a small H13 slice right after N1, so that `TestLeaderSessionCloseKillsBackgroundJobs` stops skipping.

## Outcome

`session/close` disposes one session, through the leader and through `ask acp`: the run stops, its background jobs end and the session is gone. The adapter advertises `sessionCapabilities.close`.

## Decisions

| Decision | Reason |
|---|---|
| Only the driver may close a session (`driverOnly`), as for every other call that changes a session. An observer takes the session first. | Same authority rule as prompt, cancel and reset (`internal/leader/policy.go`). |
| The router forgets the session when the host answers: on success, or on `unknown_session` or `internal` (the host no longer holds it). Members leave it, follows end without an unfollow, open questions end; queued frames stay. | `Session.Close` removes the session from the host before it disposes the Agent, so a late error still means "closed". |
| `Dispose` waits for started tools to drain, so a close can take as long as that drain. | Same rule as leader shutdown (D19, D29). |

## Changes

- `internal/acp/agent.go`: `CloseSession` (gate, route check, driver generation guard, `Session.Close`); `initialize` advertises `sessionCapabilities.close`.
- `internal/leader/policy.go`, `router.go`: `callClose`, `tagClose`, `forgetSession`, `hostDropped`.
- Tests: `TestAdapterCloseSessionDisposesTheSession`; `TestRouterSessionCloseByTheDriverForgetsTheSession`, `TestRouterSessionCloseNeedsTheDriver`, `TestRouterSessionCloseEndsOpenQuestions`, `TestRouterSessionCloseKeepsOtherSessions`; `TestLeaderSessionCloseKillsBackgroundJobs` (no skip; it also checks that the leader then answers `unknown_session`). Updated contract tests: `TestAdapterInitializeGateAndCapabilities`, `TestAdapterDeferredMethodsAreExplicitlyUnsupported`, `TestACPE2EInitialize`, `TestACPE2EUnsupported`.
- Docs: `internal/leader/README.md`, `internal/acp/README.md`, H5 plan open items, roadmap H5 status.

## Not in scope

No TUI command closes a session (TUI exit still detaches). The TUI shows nothing special when another client closes the session it follows; it receives `agent_disposed` and later calls answer `unknown_session`.

## Evidence (2026-10-12)

- `go vet ./...`, golangci-lint (0 issues), `go test ./internal/... ./pkg/...` pass. `go test ./cmd/tui` passes except `TestClientInProcessAgent` (see below).
- `TestLeaderSessionCloseKillsBackgroundJobs` runs (no skip) and passes: the job process group is gone after the close, and the leader then answers `unknown_session`.

## Found on the way

- **A test hang, fixed (test code only).** `TestPrintJobKillStopsJobWithoutNotice` hung the `cmd/tui` package for 10 minutes about once in eight loaded runs. Two causes: (1) a step hook that fails a `require` ran on the HTTP transport goroutine of the provider; `runtime.Goexit` ended the model stream without a result, so the run waited for ever instead of failing (`runBefore` now runs the hook on its own goroutine, in `tools_e2e_test.go` and the leader fixture); (2) the real failure under it: the shell creates the `pgid` file before `ps` writes into it, so a step that waited only for the file let `job_kill` stop the group before the id was written. The six waits now use `readPgid`, which waits for a valid id. Proof: 5 × 8 loaded runs of the job tests pass.
- **Fixed (user decision 2026-10-12, option A): `TestClientInProcessAgent`.** It fails in most full `cmd/tui` runs (`tui_client_e2e_test.go:585`: `State` right after `FollowUp` returns an empty follow-up list). The host's `Session.Queues` reads the texts of the last `queue_update` event, which the Agent delivers to the session listener after `FollowUp` returns, so a `State` read can miss an input that was just queued. It fails the same way before these slices; it passes alone.

  Fix: `agent.State()` now carries `Steering` and `FollowUp`, read from the queue under the Agent lock, and the ACP host uses them for `State`, for the idle check of a quiesce and for the busy check of `take` (`Session.Queues`, `idle`, `take` in `internal/acp/host.go`). The copy that the host kept from `queue_update` events is gone. Test: `TestStateShowsTheQueueBeforeListenersSeeTheUpdate` holds the publisher and shows the queue at once. `TestHostResetFencesEpochOfHeldEvents` no longer reads queue texts from made-up events. Two full `cmd/tui` runs passed `TestClientInProcessAgent`.
- **Fixed: a notice for a foreground command.** Under load, a foreground `bash` command that ended before its tool call started to wait sent a `Background job … finished` notice, and the model got an extra turn (`TestPrintCommandGuard*` saw a third request in 8 of 60 loaded runs). `StartSpec.Foreground` now registers the wait of the call at start (`internal/jobs/table.go`, `internal/tools/shell_bash.go`). Tests: `TestForegroundJobThatEndsBeforeTheWaitSendsNoNotice` (fails without the flag), `TestForegroundJobWithoutAWaitBudgetStillGetsANotice`. 60 loaded runs of the guard and job tests: 0 failures.
