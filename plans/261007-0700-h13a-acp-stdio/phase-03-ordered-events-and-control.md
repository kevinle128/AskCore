---
title: "Phase 3: Ordered events and controls"
status: completed
---

# Phase 3: Ordered events and controls

## Outcome and requirements

Project real lifecycle events, queues and Follow into an ordered outbound path with correct v1 completion.
Preserve the complete H13a scope and public Agent semantics.
Use real existing owners; never implement a second execution loop or advertise deferred capabilities.
No implementation is done during planning.

## File inventory

| Action | Absolute path | Change |
|---|---|---|
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/updates.go` | Follow reader and standard content/tool updates |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/updates_test.go` | Follow reader and standard content/tool updates |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/meta.go` | Safe event identity and precision-safe metadata |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/meta_test.go` | Safe event identity and precision-safe metadata |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/ask_methods.go` | Typed queue/state/follow/usage controls |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/ask_methods_test.go` | Typed queue/state/follow/usage controls |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/pkg/protocol/acp.go` | Complete event/follow/request DTOs |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/pkg/protocol/acp_test.go` | Complete event/follow/request DTOs |
| MODIFY only if proved | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/agent or internal/bus affected source/tests` | Cause-aligned missing public contract; no speculative extension |

No existing file is deleted unless constructor code moves within its existing owner.
Generated SDK files are inspected, not manually edited.
Only create a helper when its responsibility requires a real boundary.

## Test baseline and caller protection

Counts are source declarations from the phase scout, not passing test results.
ACP has 0 existing tests; all ACP conformance, adapter, sender, and stdio checks below are missing.
Protocol has 114 tests; Agent 345; bus 13; app 13; auth 25; providers 94; sessions 11; CLI 81.
Read owning package tests before changes and reuse their real fixtures.

| Existing call surface | Production / test lexical sites | Protection |
|---|---:|---|
| Prompt / Continue / Abort | 1/271; 0/8; 1/58 | Preserve busy, state, cancellation and settled behavior |
| WaitForIdle / Dispose / Reset | 0/9; 1/52; 4/16 | Never dispose from sync listeners; preserve writer/epoch lifecycle |
| SetModel / SetThinkingLevel | 0/17; 0/4 | Preserve readiness atomicity, clamp and idle boundary |
| Steer / FollowUp / Remove / Follow | 0/9; 0/9; 2/12; 1/26 | Preserve queue claim, input IDs and consistent snapshot cut |
| NewNativeAuth / BindAuth / AuthWait | 2/0; 1/8; 2/0 | Preserve private auth transport and refresh drain |
| newHeadlessAgentWithAuth / runWithDependencies | 2/0; 1/2 | Move shared construction without changing direct headless behavior |
| parseArgs / runAuth / openProvider | 1/1; 1/0; 1/0 | Dispatch ACP explicitly; no raw auth stdin on protocol connection |

These counts are lexical navigation bounds, not type-resolved call graphs.
Before edits, read every typed caller in `cmd/tui`, `internal/app`, `internal/agent`, and their tests.
Do not alter Agent or bus public APIs unless a failing executable check proves the missing contract.

## Function protection checklist

- [x] Resolve typed callers and existing fixtures before changing a shared owner.
- [x] Protect Prompt busy and completion semantics, Abort versus Dispose, and unbounded started-tool drain.
- [x] Protect queue ID/claim behavior, Follow snapshot/open-stream baseline, and epoch identity.
- [x] Keep native auth/provider callbacks in app and preserve direct headless behavior.
- [x] Check cancellation and output errors at their actual physical transport seam.

## Dependency map

Phase 2 session Agent → Agent.Follow consistent cut → owned bus.Follower → mapper → bounded ordered connection sender → actual write completion → prompt terminal result.
Incoming SDK notification watermarks only describe inbound callback progress.
Maintain outbound written cursor per session/epoch from successful physical writes; neither frontier is a remote application ACK.
No state/map lock or synchronous Agent listener may perform transport writes.

## Completion and resync contract

Ordinary cancellation waits for started tool bodies and the authoritative settled boundary without a new grace timeout.
Use the session admission binding from Phase 2, including no-start failure envelopes, then identify that run's settled envelope and exact sequence.
A rejected call that emits no events returns its mapped error directly; it never waits for a nonexistent settled sequence.
The prompt result waits until the sender confirms updates through that sequence were actually written.
If a follower reports resync during the turn, do not wait forever for an undeliverable settled sequence.
For H13a, fail the connection and terminate the pending request safely if the mandatory completion stream cannot recover a required sequence.
Do not falsely complete a run or wait forever; a later fresh follow can resync state without resending Prompt.
An outbound write/error latch is observed by stdio composition even if the SDK discards an inbound handler's response write error.
Disconnect and write failure never prove a request was unadmitted; no retry can resubmit it automatically.
Usage is a read-only projection of existing committed AttemptEnd and Follow facts, not a new billing store or an H9 durable API.

## Follow cutover and sender ownership

The mandatory per-session run observer and completion barrier exist independently of optional explicit follow subscriptions.
Unfollow or an optional subscription overflow cannot strand a standard prompt response.
<!-- Accepted red-team correction: Reset observer replacement. -->
Idle Reset intentionally invalidates the old Follow stream.
Replace the mandatory observer under admission coordination, fence old-generation queued frames, and establish the new epoch before Reset returns or a new prompt enters.
Treat unexpected missing completion during active work as connection failure; do not treat expected idle Reset as that failure.
Use one actual Agent event observation stream per connection/session and fan out typed projections inside the sender; an explicit follow subscription does not duplicate standard session/update notifications.
Replacement subscriptions carry generation and epoch fences so late frames from the replaced cursor are rejected.
For explicit follow, serialize the result containing snapshot/cursor/open baseline before accepting its live events above that cursor into the output queue.
The atomic registration/cutover holds only short adapter coordination state, never a host-map or Agent lock across the physical write.
A fresh follow result is sent once; later updates begin strictly after its sequence with no baseline duplication.
One source event can produce multiple frames; annotate derived frame index/count and advance writtenSeq only after all frames for that source event write successfully.
Precision-sensitive sequences use decimal strings; preserving _meta through a float64 map is not accepted without the >2^53 round-trip test.

## Scenario matrix

| Scenario | Required assertion |
|---|---|
| Settled ordering | text/tool/usage/retry updates through agent_settled physically written before prompt result |
| Delayed/out-of-order tasks | one sender orders by existing envelope identity; no response overtakes queued final updates |
| Cancel during work | Abort clears queues, preparation/retry cancel observable, started tools drain unbounded |
| Cancel then prompt | no early success, no old run update leaked into next run; Abort separate from Dispose |
| Queue lifecycle | Steer/FollowUp admit IDs; busy/idle wake and claim rules; Remove bool before/after claim |
| Follow cut | snapshot+cursor+open StreamBaseline then events strictly after cut |
| Open tool arguments | raw unfinished argument bytes, AttemptID and block identity restore without loss/duplication |
| Resume/reset | valid retained cursor no snapshot; wrong epoch/gap/future/oversize gets explicit resync; Reset changes epoch |
| Pressure/fault | slow consumer/overflow resync, blocked writer controls still admitted; output fault terminal success forbidden |
| Usage/retry | exact per-attempt accounting preserved separately from cumulative standard session usage |

## Tests Before (RED)

Write deterministic sender tests that hold the output writer while settled is already published.
Assert prompt result stays pending until successful last write.
Inject short writes/errors and prove the result never becomes successful.
Use real Agent.Follow during open text/tool deltas, invalid cursors and ring overflow.
Queue and cancellation tests call the real inbox and started-tool drain paths, not adapter stubs.
The user-approved supplemental stdio composition test registers a controlled tool in the real registry/executor and holds its started body until deliberate release.
Assert session/cancel does not settle or return the prompt result before release; then assert settled and written-update ordering.
Use no MockAgent, production test flag, or added production tool.
Add no-start failure and Reset-to-Prompt write-order tests from Phase 2.
Run narrow checks first and save the precise failing assertion and command.
A timeout from a broken fixture or inaccessible dependency is not behavioral RED.

## Refactor (GREEN)

Create one context-owned Follow reader per subscribed stream and one ordered sender per connection.
Bound messages and bytes with cancellable pressure; lifecycle/tool/control data cannot be silently dropped.
Sender results return explicit session/run/epoch/seq write completion; one error latch fails the connection.
Prompt waits on settled plus outbound written barrier; do not wait for an invented remote ACK.
Use standard session/update for text, thinking and tool facts; preserve exact Ask lifecycle facts separately through typed extensions/_meta.
Close followers on unfollow/detach and emit explicit resync without automatic command replay.
Use Agent.Abort for session/cancel; request-context cancellation must not secretly dispose an Agent.
Reuse standard library and selected SDK facilities before introducing abstractions.
One synchronous Agent listener must never block on wire output or disposal.

## Tests After

Race/leak checks cover two sessions, interleaved runs, follow unsubscribe and reconnect/resync without prompt resend.
Replay raw output frames to assert terminal result order and metadata safety.
Assert explicit resync for oversized events and full reconstruction from snapshot/baseline.
Keep retry and queue behavior identical to direct Go API.
Every test has an owner, cancellation context, and bounded fixture cleanup.
Ordinary tool drain remains contractually unbounded; the test waits for its deliberate real release rather than inventing a product timeout.

## Regression commands

Commands name proposed tests and files; they do not claim those tests exist today.
Run from the primary worktree with a task-owned cache if the environment needs one.

```sh
go test ./internal/acp ./pkg/protocol -run 'TestACPUpdates|TestACPMeta|TestACPFollow|TestACPUnfollow|TestAdapter|TestHost' -count=1
go test -race ./internal/acp ./internal/agent ./internal/bus ./pkg/protocol -count=1
```

## Success criteria

- [x] All listed scenarios have runnable tests and saved results.
- [x] New behavior has valid RED-to-GREEN evidence; initially correct controls retain their PASS result.
- [x] Existing Agent/headless/auth public contracts remain intact.
- [x] Planned external behavior is wired through the real built ask acp boundary in final acceptance; see the [2026-10-08 independent review](../reports/code-review-261008-h13a-plan-compliance.md).
- [x] No silent lifecycle loss, precision loss, secret exposure, or capability overclaim remains.
- [x] Narrow tests, applicable race/leak checks, regression gates, and owned-process cleanup pass.

## Risk signal and response

Signal: result before settled/write barrier, deadlock under slow output, missing open arguments, or cursor from another epoch accepted.
Response: preserve frame trace and stop completion publication; fix sender/Follow ownership rather than weakening event assertions.

## Rollback

Cancel and join only phase-owned children/readers/SDK connections.
Restore only this phase's reviewed source/dependency diff after checking for user or other agent edits.
Keep failing frame traces and immutable schema/source identities for the next attempt.
Do not weaken existing checks, replay an ambiguously admitted prompt, or modify generated files to conceal a failure.

## Execution evidence (2026-10-07)

Result: `go test ./internal/acp ./pkg/protocol ./internal/app ./internal/providers -run '...' -count=1` ok; `go test -race ./internal/acp ./internal/agent ./internal/bus ./pkg/protocol ./internal/app -count=1` ok; `go test -race ./internal/acp -count=8` ok; `go test ./cmd/tui -count=1` ok; golangci-lint on `./internal/acp/... ./pkg/protocol/...` 0 issues. Every adapter test ends with `goleak.VerifyNone`.

RED to GREEN (first failing assertion, saved before the code):

| Test | Command | Failing assertion |
|---|---|---|
| `TestHostBusyCallDoesNotBindSiblingRun` | `go test ./internal/acp -run '^TestHostBusyCallDoesNotBindSiblingRun$'` | `Should be empty, but was fb30ec49...`: a refused call claimed the run of a steered input |
| `TestHostContinueStateErrorDoesNotBindSiblingRun` | same, by name | same assertion for the two Continue state errors |
| `TestHostRequestCancellationKeepsRunAndBarrier` | same, by name | `request cancellation released the barrier early` |
| `TestHostResultReasonAfterAbort` | same, by name | expected `aborted`, actual empty (the stop reason was not captured) |
| `TestHostQueueOverflowFailsConnection` | same, by name | `run did not stop after the queue overflowed` |
| `TestACPWireErrorMessages` | `go test ./pkg/protocol -run TestACPWireErrorMessages` | `unknown_session: no fixed message` |
| `TestAdapterErrorMapperKinds` | `go test ./internal/acp -run TestAdapterErrorMapper` | mapper returned no frame (stub) |
| `TestACPUpdates*`, `TestACPMeta*` | `go test ./internal/acp -run 'TestACPUpdates|TestACPMeta'` | `[] should have 1 item(s), but has 0`; meta map nil |
| `TestCheckedWriterObservesWrittenFrames`, `TestQuietLoggerDropsAttributes` | by name | observer saw no frame; log text held `secret-error-text` |
| all `TestAdapter*`, `TestACPFollow*`, `TestACPUnfollow*` | `go test ./internal/acp -run 'TestAdapter|TestACPFollow|TestACPUnfollow'` against a stub adapter | `expected: "not_initialized" actual: ""`; the SDK put `{"error":"acp: unsupported"}` into the error data of an unmapped error |

Controls that passed on their first run (no RED claimed): `TestHostResetFencesEpochOfHeldEvents` (the epoch race has no deterministic external trigger, so it drives the fence directly), `TestAdapterFailStopsRunsAndClosesOutput`, `TestAdapterToolUpdatesPrecedeResult`, `TestAdapterCancelWaitsForStartedToolBody`, `TestAdapterRequestCancellationWithStringID`, `TestAdapterNoStartFailureReturnsMappedErrorAndNextPromptIsFree`.

Decisions that the tests record:

- The SDK request context never stops a run and never releases the write barrier. A second prompt on one session cancels the first request context in the SDK, so the host ignores request cancellation and uses `session/cancel` (`Agent.Abort`) only.
- A model failure ends a run with `cycle_end` reason `error` and `Agent.Prompt` returns nil. The adapter turns it into a failure frame after the updates: `no_api_key` for AUTH, otherwise `internal` with the failure code only.
- `PromptResponse.usage` is not in the stable schema, so it is not set. Usage is `_ask/session/usage`.
- Thinking on a disposed Agent: `Agent.SetThinkingLevel` does not check disposal. No ACP path reaches a disposed Agent: `session/close` is unsupported, and `Host.Close` and `Session.Close` remove the session first, so the call gets `unknown_session`. No guard was added at the Agent.
- Follow events and snapshot messages carry the cleaned failure text that the Agent publishes. This is the existing Agent event contract, as in the headless JSON stream. Redaction for a wider audience is an input for the stdio composition.

Public surface added for the next phase: `HostOption`, `WithQueueLimit`, `ErrQueueOverflow`, `ErrOutputFailed`, `ErrAuth`; `Result.Reason`, `Result.Cause`, `Result.Code`; `Session.Queues`, `Session.Epoch`; `Adapter`, `Config`, `NewAdapter`, `Bind`, `Fail`, `CloseOutput`, `Failed`, `Failure`, `Close`; `CheckedWriter.Observe`; `NewQuietLogger`; in `pkg/protocol`: `ACPErrInvalidParams`, `ACPErrInvalidState`, `ACPCodeInvalidState`, `ACPErrorMessage`, and the `provider/id@api` model ID. `pkg/protocol/README.md` still needs a line for the new kinds (outside the file ownership of this work).
