---
title: "Phase 2: Independent session adapter"
status: completed
---

# Phase 2: Independent session adapter

## Outcome and requirements

Build one composition-owned session host with an independent real Agent and writer per session.
Preserve the complete H13a scope and public Agent semantics.
Use real existing owners; never implement a second execution loop or advertise deferred capabilities.
No implementation is done during planning.

## File inventory

| Action | Absolute path | Change |
|---|---|---|
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/agent.go` | ACP Agent implementation; session registry/factory lifecycle |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/agent_test.go` | ACP Agent implementation; session registry/factory lifecycle |
| CREATE only if needed | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/host.go` | Split map/lifecycle owner only if agent.go becomes complex |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/app/agent_native.go` | Shared native Agent construction accepting session/cwd/model |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/app/agent_native_test.go` | Shared native Agent construction accepting session/cwd/model |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/cmd/tui/headless.go` | Reuse construction while preserving direct Go path |
| MODIFY only if needed | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/providers/catalog.go` | Copied available-model rows with qualified refs |
| MODIFY only if needed | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/providers/catalog_test.go` | Copied available-model rows with qualified refs |

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

Phase 1 DTO/SDK → app factory accepting SessionID/CWD/model/settings → independent Agent.New + session writer → host registry → ACP handlers.
Auth and provider resolution stay in app through typed function callbacks.
ACP imports Agent/sessions/bus/protocol/SDK, not config, leader, gateway, or provider construction.
The host mutex covers map state only; release it before factory, auth readiness, prompt, wait or disposal.

## Admission and identity binding

Agent.Prompt does not return a public run ID.
For each session, serialize admission/control transitions with an adapter-owned admission gate; do not hold the host map lock or that gate while waiting for the whole turn.
<!-- Accepted red-team correction: no-start completion. -->
Establish Follow before calling Prompt or Continue and record its cut.
Bind an executed call to its existing run envelope identity, including failure envelopes emitted before `agent_start`; never require a start event as the only binding signal.
Observe the Agent call result and the event stream together.
An admission/state rejection with no events returns its mapped error without waiting for start or settled.
An executed failure with failure/settled envelopes waits for that run's actual outbound write barrier before returning the mapped error.
Do not infer run ownership from the next unrelated event after the call returns.
Prove binding under queued next-run races with real Agent tests; if current envelope/call seams cannot establish ownership, add only a RED-proven narrow owner contract before adapter completion.
Failed ErrBusy or preparation admission must not bind a sibling run's events or produce a false final response.
Use existing envelope identity from subsequent events, not a fabricated SDK request ID as Agent run identity.
Two request IDs can coexist across different sessions; one standard prompt per active session remains the real Agent policy.
Reset starts a new epoch in the same Agent/session; session/new constructs a different Agent and writer.
<!-- Accepted red-team correction: Reset observer replacement. -->
Close prompt admission during idle Reset, fence the old observer generation/epoch, and establish the mandatory new-epoch Follow before returning Reset or reopening admission.
If replacement fails, fail the connection safely rather than admit a prompt with no completion observer.
Optional old subscriptions receive explicit resync and cannot own the mandatory observer.

## Scenario matrix

| Scenario | Required assertion |
|---|---|
| Initialize gate | reject session/new or operations before initialize; capabilities connection-local |
| Two new sessions | distinct cwd/log/model/draft-independent IDs, one Agent each |
| Concurrent/failed new | no leaked registry entry; publish only successful fully constructed session |
| Prompt/Continue | Prompt ErrBusy; Continue actual valid log or existing state error; no second execution loop |
| State/Reset | State copied safely; idle Reset same session/new epoch; different session/new never Reset another Agent |
| Model list/switch | qualified provider/model/API IDs round-trip Find; unqualified Token Plan ambiguity rejected |
| Readiness failure | old model/thinking retained; no paid fallback or entitlement inference |
| Thinking/disposal | clamp existing policy; busy/disposed controls rejected, idempotent host close |
| Unsupported inputs | unsupported content blocks and MCP config fail explicitly; no silently lost input |
| Deferred owners | session/load, compact, fork/tree unavailable without fake durable caps |

## Tests Before (RED)

Write real Agent factory/session tests with separate temporary cwds and session writers.
Hold a provider HTTP response to prove concurrent session isolation and busy errors.
Add real failing-session-writer tests for executed failures with no start event.
Test Continue on empty and assistant-tail logs, then another admission, with no hanging response or sibling-run binding.
Test idle Reset followed immediately by Prompt with and without an optional old subscription.
Protect headless JSON and native auth setup before moving constructor code.
Use a real service fixture; MockAgent is not an acceptable proof.
Run narrow checks first and save the precise failing assertion and command.
A timeout from a broken fixture or inaccessible dependency is not behavioral RED.

## Refactor (GREEN)

Extract shared native construction and existing provider registration from newHeadlessAgentWithAuth/registerOpenAIWires into app.
Retain current environment/options baseline, capture separation and request preparation.
ACP factory registers supported real wire APIs independently of its initial model so a faux-start session can switch to a real provider.
Preserve faux no-credential behavior and never look up faux in the native credential store.
The six-row compiled model catalog excludes faux; return the actual current model separately or compose one explicit faux option without creating a second catalog authority.
Test faux-to-real switching with real registered native HTTP adapters and environment credentials.
Inject session-specific values instead of os.Getwd and internal ID creation.
Construct one Agent/writer per successful session/new.
Use one host session lookup and centralized safe error mapper.
Keep initialize state per connection and future detach semantics separate from host.Dispose.
Use real provider catalog Find through app callbacks; expose copied rows without a second catalog authority.
Validate optional authMethodId against effective configured credentials before model mutation, as defined in Phase 1.
Test API-key/subscription mismatches in both directions, with unchanged state and no inference request.
If SetThinkingLevel accepts disposed state, first prove that behavior and apply only the narrow necessary guard at the correct owner.
Reuse standard library and selected SDK facilities before introducing abstractions.
One synchronous Agent listener must never block on wire output or disposal.

## Tests After

Assert per-session histories, models, queue IDs and cwd never cross.
Exercise all host error branches and creation/disposal races.
Run all existing headless, auth-binding and Agent controls after extraction.
No global mutable Agent instance or registry lock held over execution is allowed.
Every test has an owner, cancellation context, and bounded fixture cleanup.
Ordinary tool drain remains contractually unbounded; the test waits for its deliberate real release rather than inventing a product timeout.

## Regression commands

Commands name proposed tests and files; they do not claim those tests exist today.
Run from the primary worktree with a task-owned cache if the environment needs one.

```sh
go test ./internal/acp ./pkg/protocol ./internal/app ./internal/providers -run 'TestHost|TestAdapter|TestACP|TestCheckedWriter|TestQuietLogger|TestNativeAgent|TestFind|TestHeadless' -count=1
go test -race ./internal/acp ./internal/agent ./internal/bus ./pkg/protocol ./internal/app -count=1
```

## Success criteria

- [x] All listed scenarios have runnable tests and saved results.
- [x] New behavior has valid RED-to-GREEN evidence; initially correct controls retain their PASS result.
- [x] Existing Agent/headless/auth public contracts remain intact.
- [x] Planned external behavior is wired through the real built ask acp boundary in final acceptance; see the [2026-10-08 independent review](../reports/code-review-261008-h13a-plan-compliance.md).
- [x] No silent lifecycle loss, precision loss, secret exposure, or capability overclaim remains.
- [x] Narrow tests, applicable race/leak checks, regression gates, and owned-process cleanup pass.

## Risk signal and response

Signal: new sessions share writer/Agent, factory failures leave entries, or old model changes after failed readiness.
Response: stop integration, repair the owner/factory boundary, and rerun isolation and headless regressions.

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
