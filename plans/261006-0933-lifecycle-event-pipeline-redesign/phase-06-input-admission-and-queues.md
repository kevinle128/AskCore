---
phase: 6
title: "Input admission, queues, removal and disposal"
status: done
priority: P1
effort: 13h
dependencies: [phase-05]
---

# Phase 06: Input admission, queues, removal and disposal

## Goal

The Agent owns the steering and follow-up queues, as DeepSeek's inbox does. Input gets an Agent-generated `inputId`, is claimed at defined points, admitted through `AdmitStep`, and acknowledged with an `InputOutcome`. Abort, idle and the abort-to-idle window follow DeepSeek (Q5, resolved by D25). No input is lost between "queue empty" and "run ended". A pending input can be removed by ID (D27). `Dispose()` ends the Agent separately from `Abort()` (D29), and headless uses it for SIGINT and SIGTERM. The user-message commit point does not move in this phase; phase 08 moves it after request preparation (D26). <!-- red-team #4, #6 -->

## Decisions (D25, DeepSeek source verified)

- **Claim shape (follow DeepSeek `packages/core/agent-loop/src/inbox.ts:103-108`).** A turn boundary claims all queued steering. A cycle boundary claims all queued steering plus one queued follow-up. The Pi delivery modes `all`/`one-at-a-time` are not built.
- **Steering claim point.** Steering is claimed at the next turn boundary, after the whole tool batch (E§24#28). A follow-up opens a new cycle in the same run when the cycle can stop (`agent.ts:388-394`).
- **Abort (follow DeepSeek `agent.ts:176-181`).** `Abort()` clears both queues; `Abort(KeepQueued)` keeps them. The aborted run claims nothing more; the poll during an aborted tool batch (`internal/agent/loop_stage.go:191-195`) is removed.
- **Input after abort (follow DeepSeek `agent.ts:154-160,214-222,255-264`).** A `Steer` or `FollowUp` that arrives while the active run is aborted goes to the follow-up queue and sets a wake latch. When the aborted run ends and the latch is set and a queue is not empty, the Agent starts a new run.
- **Input on an idle Agent (follow DeepSeek `agent.ts:224-233`).** `Steer` and `FollowUp` on an idle Agent queue the message and start a run. `WaitForIdle` returns when that run settles.
- **Busy Agent (diverge-deliberate BUSY, matrix 74).** DeepSeek queues a send to a busy agent (`agent.ts:154-160`). Ask keeps `Prompt` on an active Agent as `ErrBusy` (Pi API); `Steer` and `FollowUp` queue as DeepSeek. The DeepSeek host test `session-cold.host.spec.ts:900` maps a send exception of an idle stub to a busy transport error (`commands.ts:372`); it is not evidence for `ErrBusy`.
- **Remove by ID (follow DeepSeek `inbox.ts:152-157`, D27).** `Agent.Remove(inputID) bool` removes a pending steering or follow-up message that no claim has taken. It returns false after the claim. It publishes `queue_update`. A latched wake whose message was removed starts no cycle (matrix SA18).
- **Dispose (D29, DeepSeek `packages/core/agent-loop/src/index.ts:526-556`).** `Dispose() error` is memoized: every call returns the same result. It closes admission first, so `Prompt`, `Steer`, `FollowUp`, `Remove`, `Reset` and `SetModel` return `ErrDisposed` (DeepSeek accepts a late send into the inbox but does not latch it, `agent.ts:216-220`; matrix SA19 records the difference). It cancels the active run with cause `disposed`, which never sets the wake latch. It clears both queues, as DeepSeek: disposal cancels without `keepInbox` (`packages/core/agent-loop/src/index.ts:544`), and `cancel` clears the inbox (`agent.ts:176-177`). The clear happens at cancel time, so one `queue_update` with both queues empty is published before `agent_settled`; no queued input starts (matrix SA25b; Q9 resolved by the user on 2026-10-06). It waits for the run to settle, which includes the D19 drain of started tool bodies. Then it closes the writer and publishes `agent_disposed` to Agent listeners and the follow path (new event type). A panicking listener does not stop it (listener containment, phase 05).
- **D18 and D29 together.** D18 makes `agent_settled` the terminal record of the JSON stream, and "a JSON reader never changes" (`roadmap.md:61`); D29 publishes `agent_disposed` after the drain, so after `agent_settled`. Both hold: the headless JSON writer does not write `agent_disposed`, so the JSON stream still ends with `agent_settled`; Go listeners and followers get it.
- **Headless signals (D29).** On SIGINT and SIGTERM, `runHeadless` starts `ag.Dispose()` in a goroutine instead of calling `ag.Abort()` (`cmd/tui/headless.go:329`), because `Dispose` blocks until the drain ends. It then selects on the `Dispose` completion channel, a second signal and the `abortGrace` timer (today it selects on `promptAll`'s `done`, `headless.go:338-344`), and returns the signal code. When `Dispose` completes inside the grace, every terminal event is already written; otherwise the phase 05 exit path applies (`tryFlush`, no wait on the output lock).
- **Sole writer (matrix SS3b).** No Agent API appends to the session log. A listener that calls `Prompt`, `Steer` or `FollowUp` changes the queues or gets `ErrBusy`; the log changes only at the next claim.
- **No lost wakeup (red-team #6).** The driver's "queues empty, end the run" decision and the change to idle happen in one critical section under `a.mu`. `Steer`/`FollowUp` take the same lock: they either enqueue for the live driver (which checks again under the lock) or start a new run.
- **Admission failure (follow DeepSeek `loop.spec.ts:933`, `consumed-work.spec.ts:46`).** The claimed batch is consumed by an `AdmitStep` rejection or error (`InputOutcome{rejected}` or the run error). Messages that were still queued stay queued and are claimed by the next run.
- **Lifetimes (red-team #14).** Queues and the `inputId` counter belong to the Agent. `Reset` (idle only) clears the queues. `Dispose` ends the Agent; it is not reusable. There is no client resend API: `WithInputID` is dropped, and matrix row 72 is N/A until H13 (gateway `rpcId`). <!-- red-team #14 -->
- **Bound (diverge-deliberate BOUNDS, matrix N6).** `Config.MaxQueuedInputs` (default 100); a full queue returns `ErrQueueFull`. DeepSeek's inbox has no bound, so the 101st send behaves differently.
- **Queue events (follow DeepSeek inbox events `packages/core/agent-loop/tests/agent.spec.ts:65`, Pi wire name).** Every insert, claim and clear publishes Pi's `queue_update{steering, followUp}` with the text of the queued messages (`C:core/agent-session.ts:199-201,1019-1021`).

## Context links

- Design section 6; architecture reference 7.2
- Poll points to remove: `internal/agent/loop_run.go:119-166`, `loop_stage.go:82-106,191-195`; the phase-02 `PollSteering`/`PollFollowUp` functions
- Run begin/end: `internal/agent/agent.go:207-227`; `Abort` under `a.mu`: `agent.go:101-108`
- DeepSeek: `agent.ts:154-181,214-264,296-396`, `inbox.ts:90-114`, `docs/architecture.md:113`

## Files to Create / Modify

- Create: `internal/agent/queue.go`, `queue_test.go`, `admission_test.go`
- Modify: `pkg/protocol/events.go`, codecs, README (`queue_update`, `agent_disposed`)
- Modify: `internal/agent/agent.go` (`Steer`, `FollowUp`, `Remove`, `Abort(opts ...AbortOption)`, `Dispose`, `ErrDisposed`, wake latch, `Reset`), `internal/sessions/writer.go` (`Close`), `cmd/tui/headless.go` (signals call `Dispose`), `loop_run.go`, `loop_stage.go` (claims, no polling), `types.go` (`MaxQueuedInputs`, remove `PollSteering`/`PollFollowUp`), `internal/agent/README.md`, `internal/scheduler/README.md` (queues are in `agent`)
- Modify tests: `TestSteeringPollPoints` and `TestComposeQueues` (ported in phase 02) are replaced by the queue tests below. The phase 05 signal tests (`TestJSONAbortWithSlowReaderStillWritesAgentSettled`, `TestJSONSignalWithFullPipeExitsWithinGrace`) keep their assertions and now run through `Dispose`.

## Tasks & Steps

1. `queue.go`: two FIFO lists, `inputId` generation, `claim(target)` per the claim shape, `clear()`, bound check.
2. Public API: `Steer(msg) (inputID string, err error)`, `FollowUp(msg) (inputID string, err error)`, `Remove(inputID string) bool`, `Abort(opts ...AbortOption)` with `KeepQueued`, `Dispose() error`.
3. Driver: claim at the turn and cycle boundaries; dispatch `AdmitStep` with the claimed batch; `enter` keeps the messages for commit at today's point; `reject` writes `InputOutcome{rejected}` and closes the cycle with reason `blocked` and zero turns; an error ends the run (failure table).
4. Closing state: implement the atomic end decision and the wake latch exactly as in "Decisions".
5. Dispose: implement the order in "Decisions" (close admission, cancel with cause `disposed` and clear the queues with one `queue_update`, wait for settle, close the writer, publish `agent_disposed`); memoize with `sync.Once` plus the stored result. Switch the headless signal path to `Dispose`.
6. Lock rule: `a.mu` guards queue and run state only; never hold it while calling middleware, a provider, a tool, the writer or a listener. A handler can call `Abort`, `Steer` or `FollowUp` without deadlock.

- **AdmitStep abort case carried from phase 03 (matrix SA22).** When `AdmitStep` is dispatched, extend `TestAbortReleasesHookWaitingOnContext` with an `AdmitStep` handler that waits on its context: Abort releases it and the cycle ends `aborted(user)`.

## Tests

The phase owns every matrix row with Phase `06`; run `check-conformance-matrix.sh 06`.

- `TestSteeringWaitsForWholeToolBatch`, `TestFollowUpOpensNewCycle`, `TestTurnBoundaryClaimsAllSteering`, `TestCycleBoundaryClaimsOneFollowUp`.
- `TestRejectedInputClosesCycleWithNoTurn`, `TestRejectedInputIsAcknowledgedOnce`, `TestAdmissionErrorEndsRunWithError`, `TestAdmissionErrorKeepsQueuedSteering`, `TestOuterAdmissionHandlerKeepsDownstreamRejection`.
- `TestSteerAfterAbortRunsAsNextCycle` (positive: the message runs in a new run after the aborted one), `TestSteerAfterAbortIsNotDeliveredToAbortedCycle`, `TestAbortDuringToolBatchDoesNotClaimSteering`, `TestAbortClearsQueuesByDefault`, `TestAbortKeepQueuedKeepsQueues`, `TestAbortClearRacesLateSteerDeterministically`. <!-- red-team #6 -->
- `TestSteerOnIdleAgentStartsRun`, `TestSteerRacingRunEndIsDeliveredOrRejected` (run with `-race -count=200`). <!-- red-team #6 -->
- `TestResetClearsQueues`, `TestQueueFullReturnsError`. <!-- red-team #14 -->
- Remove (D27): `TestRemovePendingInputByID`, `TestRemoveAfterClaimChangesNothing` (counterexample: the claimed message is in the request), `TestRemovedLatchedInputStartsNoCycle`.
- Dispose (D29): `TestDisposeEndsCycleWithDisposedCause`, `TestDisposeIsMemoized`, `TestSendAfterDisposeStartsNoCycle` (returns `ErrDisposed`; counterexample: `FollowUp` after `Abort` starts a new cycle), `TestDisposeClearsQueuesAndRunsNone` (two queued follow-ups: one `queue_update` with both queues empty is published before `agent_settled` and neither follow-up runs; counterexample: `Abort(KeepQueued)` keeps them), `TestDisposeWaitsForStartedToolBody` (a tool that returns on ctx cancel after a delay: `Dispose` returns after it), `TestSIGTERMDisposesAndExits143` (the JSON stream ends with `agent_settled`, and a Go listener got `agent_disposed` after it), `TestSignalExitIsBoundedWhileDisposeWaits` (a ctx-ignoring tool: `runHeadless` returns 130 within `abortGrace` plus a margin while `Dispose` still waits; the test then releases the tool).
- Busy and sole writer: `TestFollowUpWhileBusyIsQueuedButPromptIsRejected`, `TestListenerCannotWriteToSessionLog`, `TestAdmissionHookFailureEndsCycleKeepsQueue`.
- `TestQueueUpdateEventFollowsEveryQueueChange`.
- `TestAbortFromInsideHandlerDoesNotDeadlock`, `TestPromptAfterCancelledCycleRunsNormally`, `TestWaitForIdleReturnsAfterCancelledRun`.
- goleak on every abort test.

```sh
go test ./internal/agent/... -run 'Steer|FollowUp|Queue|Admission|Abort|Reset|Idle'
go test -race -count=200 ./internal/agent/... -run 'TestSteerRacingRunEndIsDeliveredOrRejected|TestAbortClearRacesLateSteerDeterministically'
go test -race ./internal/agent/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 06
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `06`).

## Risks & rollback

- Deadlock between a handler and `a.mu` (Med x High). Mitigation: lock rule; `TestAbortFromInsideHandlerDoesNotDeadlock` under `-race` with a timeout.
- `Dispose` hangs on a tool that ignores `ctx` (Low x High). Mitigation: this is the D19 contract (tools must return on cancel); headless still exits after `abortGrace`; `TestDisposeWaitsForStartedToolBody`.
- A background run started by `Steer` on an idle Agent surprises a caller (Low x Med). Mitigation: documented in `internal/agent/README.md`; `WaitForIdle` covers it; the headless entry never calls `Steer`.
- Rollback: reset to tag `lifecycle-p06-base`.

## Done criteria

- Queue tests pass, including the 200-run race tests; no claim after abort; no input accepted without delivery, an outcome or a removal.
- `Dispose` is memoized, closes admission, and headless signals use it; `TestSIGTERMDisposesAndExits143` passes.
- Green tests, race, lint and the phase-06 matrix check.
