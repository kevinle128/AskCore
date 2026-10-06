---
phase: 9
title: "Ordered tool coordinator, abort outcomes and repair"
status: done
priority: P1
effort: 14h
dependencies: [phase-08]
---

# Phase 09: Ordered tool coordinator, abort outcomes and repair

## Goal

Replace the sequential and parallel executors with one ordered coordinator, as DeepSeek's: call record and pre-control in source order, a bounded rolling pool of bodies, exclusive barriers, post-control and commit in source order. A tool runs alone unless it declares itself safe for these arguments (D25). Implement D19 revised: every call of an aborted batch gets an outcome, started bodies drain without a time bound (D19, as DeepSeek and Pi), and repair never runs a tool again. `AfterTool` can return non-waking added context (D27). The max-tokens path is already gone (phase 01).

## Decisions (D25, DeepSeek source verified)

- **Default mode (follow DeepSeek `packages/core/tools/src/index.ts:1295-1311`, `execution-mode.spec.ts:28-107`).** A call is exclusive unless its tool implements `tools.ConcurrencySafe` and `ConcurrencySafe(args)` returns `true` for these validated arguments. A missing tool, a panicking classifier or `false` means exclusive. <!-- red-team #10 -->
- **Tool definitions stay the turn snapshot (exception, user 2026-10-05, `plans/261005-2059-tool-registry-snapshot/plan.md:20`).** DeepSeek re-reads the live registry before each start (`tool-calls.ts:199-205`). Ask re-checks the mode before each start too, but against the snapshot tool with the frozen arguments. Matrix row 48.
- **Three separate facts (source audit finding 3).**
  1. *Assistant request*: the tool-call block in the committed assistant message.
  2. *Recorded call intent*: a `ToolCall{assistantEntry, callId}` entry that the coordinator writes for each call, in source order, **before** lookup, validation and `BeforeTool` (DeepSeek appends `tool/call` before `prepare`, `tool-calls.ts:168-170`).
  3. *Body invocation*: a per-call flag set immediately before the body starts (DeepSeek `bodyInvoked`, `tools/src/index.ts:1580`).
  Normal abort classifies by fact 3: invoked gives `aborted`, not invoked gives `aborted before dispatch` (`:1549-1556`). Repair classifies by fact 2: a recorded call without a result gives `outcome unknown`, a call without a record gives `not started` (`packages/core/session/src/repair.ts:131,166`). A call can be recorded and never invoked; repair still gives it `outcome unknown` (DeepSeek `tool-calls.spec.ts:808`, call c3).
- **Routing case table (follow DeepSeek `tools/src/index.ts:1497-1537,1564-1629`, except where marked).**

  | Case | Result | `AfterTool` runs | Matrix |
  |---|---|---|---|
  | Batch aborted before it starts | `aborted before dispatch` for every call, with a `ToolCall` record | no | ST11b, 50 |
  | Unknown tool (not in the snapshot) | unknown-tool error from the body stage (`BeforeTool` and `ExecuteTool` run first) | yes | ST5c, follow (Q10 resolved by D25) |
  | Argument validation error | validation error (D21) | no (DeepSeek validates in the body, then post runs) | ST5d, diverge D21 |
  | `BeforeTool` error or panic | error result with its text | no | ST5b |
  | `BeforeTool` `Deny` | error result with the reason | yes | ST5 |
  | `BeforeTool` `Cancel` | `aborted before dispatch` | yes | ST5, ST9 |
  | Abort while `BeforeTool` runs, then allow | `aborted before dispatch` | yes | ST10 |
  | `ExecuteTool` handler error or panic | error result | no | ST27 |
  | `ExecuteTool` short circuit (no `next`) | the handler's result; body not run | yes | ST27 |
  | Body error or panic | error result | yes | ST3, ST4 |
  | Body success, no abort | the result | yes | ST2 |
  | Success (body or cached) after abort | `aborted` (replaces only a non-error result) | yes; a late `AfterTool` success also becomes `aborted` | ST12, ST13, ST27 |
  | `AfterTool` error or panic | error result with its text | n/a | ST6 |

- **Unknown tool (follow DeepSeek; Q10 resolved by D25 on 2026-10-06).** A call to a tool that is not in the turn snapshot goes through the full pipeline, as DeepSeek: `BeforeTool` runs (`packages/core/tools/src/index.ts:1400-1406` comment "so policy listeners still see every name that reaches the registry", pre-execute at `:1506`); the `ExecuteTool` around point wraps the body stage (`tools/execute` waterfall, `:1605-1607`); the body stage gives the unknown-tool error (`:1578-1579`); the error is a post result (comment "Tool and unknown-tool failures still receive post-execute", `:1595-1596`; routing `:1622-1627`), so `AfterTool` runs over it (`:1783`). The snapshot stays the lookup source (SNAPSHOT is unchanged; only the hook routing follows DeepSeek). Two facts differ from a known tool: the call has no tool definition (nil), and no schema exists, so D21 validation and coercion do not run and `BeforeTool` gets the raw argument bytes. The phase 03 rule "hooks see the coerced arguments" applies only to a known tool. A `Deny` or `Cancel` from `BeforeTool` routes as for a known tool. An `ExecuteTool` short circuit can return a result for an unknown tool, as in DeepSeek (row ST27). The unknown tool is exclusive (default mode). The model-visible error text stays `Tool <name> not found`.

- **Control points cannot be interrupted (source audit finding 2).** The coordinator runs `BeforeTool` and `AfterTool` itself, as DeepSeek does (`tool-calls.ts:147-160,170`). Go cannot stop a running function, and the coordinator cannot select on `ctx` while it is inside a handler. So a cancel during pre-control or post-control takes effect when the handler returns; the handler gets the cancelled `ctx` and should return promptly (the same duty as tools, D19). The drain of started bodies starts only after the in-flight control call returns. No timer and no late-result rejection exist for control calls.
- **Post-control and added context (D27; follow DeepSeek `tool-calls.ts:147-160`, `agent.ts:535-536,358-363`).** `AfterTool` may return added context (user-role messages). The coordinator queues it as non-waking next-turn input after all results of the batch, in call order. Queued context keeps the cycle going for one more turn, even when every result asks to terminate (DeepSeek `inbox.nextStep`, matrix 19, B3). When the batch was aborted, the context is not committed into the aborted cycle; it stays queued, starts no run, and enters the first request of the next admitted cycle (matrix SA26). A default `Abort()` that clears the queues before the context is produced does not clear it; context queued before the abort is cleared with the queues.
- **`ExecuteTool` around point (follow DeepSeek `tools/src/index.ts:1564-1576,1601-1629`; matrix ST27).** The terminal gives the body a context that `Abort` always cancels, even when a handler calls `next` with another context: the terminal merges the handler's context with the run context. A short circuit skips the body. A cached success returned after `Abort` becomes `aborted`.
- **Pool:** at most `Config.MaxParallelTools` bodies (default 10, `constants.ts:6`).
- **Termination:** a batch ends the cycle only when all results ask for it (exception; DeepSeek: any concluding result, `tool-calls.ts:158`). Queued added context or steering still continues the cycle.
- **One select loop (red-team #15).** The coordinator is one loop over {worker done, worker progress, next pre-control, ctx}. Workers never block on the driver: progress goes into a per-call buffer of size 1 with latest-wins coalescing; completion goes into a channel buffered to the pool size. `AfterTool` runs in this loop in source order; a slow `AfterTool` delays later commits and later starts (DeepSeek `tool-calls.ts:199-212`), never a worker. <!-- red-team #15 -->
- **Unbounded abort drain (follow DeepSeek and Pi; D19 reaffirmed by the user on 2026-10-06).** After an abort the coordinator starts nothing new, cancels started bodies, and waits for every started body to return, with no time bound (DeepSeek `tool-calls.ts:221-234`; Pi `A:agent-loop.ts:646` `Promise.all`). The Agent stays busy until then, so a new `Prompt` gets `ErrBusy` and no second body can overlap the old one; `Dispose` (phase 06) waits for the same drain. Not hanging is the tool's duty: every tool must return promptly when its `ctx` is cancelled; a tool that runs a process must kill the whole process group on cancel and may offer a per-call timeout (Pi `coding-agent/src/core/tools/bash.ts:126-144`). This tool contract is written into `internal/tools` godoc here and enforced for real tools in H5. The red-team bounded-drain proposal is withdrawn because it reversed D19. <!-- red-team #15 revised -->
- **Session-unique tool-call IDs (Ask new).** The coordinator keeps the set of tool-call IDs already in the session history (built from history at run start). A reused ID gets an error result and does not run. Repair and outcome lookups key on `(assistant entry, call ID)`. <!-- red-team #15 -->
- **Abort final message (diverge, D20 + JSON).** After the abort outcomes are committed, the driver commits one aborted assistant message (no attempt, no model call), built by the **same constructor** as the D20 wrapper message in `fail`. Extract `abortedMessage(model, clock, cause)` from `internal/agent/agent.go:263-286`; both paths call it. DeepSeek ends the cycle `aborted` with no assistant message (`tool-calls.spec.ts:522`). <!-- red-team #15 -->

## Context links

- Current: `internal/agent/loop_tools.go:84-110` (executor selection), `:161-195` (break after abort, old D19), `:197-241` (`runJobs`, unbuffered `signals`), `:316-334` (`AfterToolCall` on tool goroutines), `:361-380` (`updater.send` blocks under `u.mu`), `:432-442` (all-results termination), `:115` (per-message `seen` map)
- `tools.Sequential` and its callers (verified by `grep -rn Sequential --include='*.go'`): `internal/tools/types.go:18-21` (godoc: whole batch serial), `internal/agent/loop_tools.go:104`, `internal/agent/loop_helpers_test.go:213-216` (`sequentialTool`), `internal/agent/loop_stage_test.go:428-433` (`sequentialTestTool`), tests `TestToolSequentialSwitch` (`loop_tools_test.go:133`), `TestAbortSequentialBatch` (:496), `TestToolExecutorSelection` (`loop_stage_test.go:109`)
- Tool implementations that need a `ConcurrencySafe` decision: `internal/tools/echo.go` (pure: safe), test tools `funcTool` (`loop_helpers_test.go:194-216`), `funcTestTool` (`loop_stage_test.go:414-433`), `mathTool` (`cmd/tui/headless_live_test.go:24-40`), `stubTool` (`internal/tools/registry_test.go:19`), `addTool`/`mulTool` (`cmd/tui`)
- DeepSeek: `tool-calls.ts:85-263`, `packages/core/tools/src/index.ts:1391-1479,1493-1629`, `packages/core/session/src/repair.ts:14-197`, `constants.ts:6`

## Files to Create / Modify

- Create: `internal/agent/tool_coordinator.go`, `internal/agent/tool_repair.go`, tests `tool_coordinator_test.go`, `tool_repair_test.go`
- Modify: `internal/tools/types.go` (replace `Sequential` with `ConcurrencySafe interface{ ConcurrencySafe(args json.RawMessage) bool }`; godoc states the DeepSeek rule and the cancel contract), `internal/tools/echo.go` (declares safe), `internal/pipeline/points.go` (`AfterTool` result gets added context), `internal/sessions/entry.go` (`ToolCall` entry), `internal/agent/loop_tools.go` (remove the batch executors), `loop_stage.go`, `queue.go` (non-waking context class), `agent.go` (shared `abortedMessage`), `types.go` (`MaxParallelTools`)
- Modify tests (deliberate): `loop_tools_test.go` `TestToolParallelOverlap` (:112, tool declares safe), `TestToolSequentialSwitch` (:133, becomes `TestUndeclaredToolRunsAloneBetweenSafeCalls`), `TestAbortParallelBatch` (:452), `TestAbortSequentialBatch` (:496), `TestToolCompletionOrderAndSourceOrder` (:71), `TestToolPreflightFailures` (:161, assertions at :202-203: `before` becomes `[unknown, blocked, silent, ok]` and `after` becomes `[unknown, blocked, silent, ok]`); `loop_stage_test.go` `TestToolExecutorSelection` (:109, deleted); `loop_helpers_test.go` and `loop_stage_test.go` test tools; `cmd/tui/headless_live_test.go` `mathTool` (declares safe). `TestCassetteParallelToolCalls` (`headless_record_test.go:117`) keeps its assertions; request bodies do not change.

## Tasks & Steps

1. Coordinator, per call in source order: write the `ToolCall` entry; duplicate-ID check (session set); lookup in the turn snapshot; for a known tool, `PrepareArguments` and validation (D21); `BeforeTool` (allow/deny/cancel, phase 03; an unknown tool reaches it with no definition and the raw arguments); then classify the mode with the frozen arguments. Route each outcome by the case table.
2. Pool and barriers: start an approved parallel-safe body while fewer than `MaxParallelTools` run; an exclusive call waits for running bodies, runs alone, then the pool resumes. Re-check the mode before each start against the snapshot tool.
3. Workers run only `ExecuteTool` and the body; set the body-invoked flag immediately before the body; a panic becomes one error result.
4. `AfterTool`, commit and publish in source order inside the select loop. `tool_execution_start` and `tool_execution_update` publish as they happen; `tool_execution_end` events and result commits follow source order. Added context is queued after the last result of the batch. `TestToolSequentialSwitch` is replaced by `TestUndeclaredToolRunsAloneBetweenSafeCalls`, which asserts this interleave. <!-- red-team #10 (Scope 4c) -->
5. Abort: stop starting bodies; wait for an in-flight `BeforeTool` or `AfterTool` to return; cancel started bodies; wait for all of them with no bound; classify each started call by the body-invoked flag and apply "cancellation replaces only a non-error result" (`tools/src/index.ts:1623-1627`); every unstarted call gets a `ToolCall` record and `aborted before dispatch`, in source order. No extra model call. The cycle ends with reason `aborted`. Then the shared aborted assistant message.
6. Repair (`tool_repair.go`): after a driver failure or a writer failure inside a turn, find the calls of the open turn that have no result. A call with a `ToolCall` entry gets `outcome unknown`; a call without one gets `not started` (DeepSeek texts, `packages/core/session/src/repair.ts:33-35`). Never run a tool again; never overwrite a committed result; a closed turn keeps its unanswered call (`repair.spec.ts:209`). If the repair write fails, return both errors, original cause first.
7. Write the tool cancel contract into the `internal/tools` package godoc.

## Tests

The phase owns every matrix row with Phase `09`; run `check-conformance-matrix.sh 09`.

- Mode: `TestUndeclaredToolRunsAloneBetweenSafeCalls`, `TestConcurrencySafeFalseForTheseArgumentsRunsAlone`, `TestPanickingClassifierRunsAlone`, `TestBodiesResumeAfterExclusiveBarrier` (distinguishes the barrier from Pi's whole-batch serial rule), `TestSafeCallsAllStartBeforeAnyFinishes`. <!-- red-team #10 -->
- Pool: `TestActiveBodiesNeverExceedLimit`, `TestFreedSlotStartsNextBodyBeforeEarlierFinishes`, `TestPreAndPostControlAndCommitsFollowSourceOrder`, `TestSlowPostHookDelaysLaterCommitsAndStarts`, `TestProgressNeverBlocksWorker`. <!-- red-team #15 -->
- Snapshot: `TestRegistryChangeDuringTurnDoesNotChangeExecutableTools` (existing tool-change tests kept).
- Termination: `TestBatchTerminatesOnlyWhenAllResultsAsk` (two calls, one asks: the cycle continues). <!-- red-team #12 (row 54) -->
- Case table: `TestAfterToolRunsForDeniedAndCancelledCalls`, `TestBeforeToolErrorSkipsAfterTool` (counterexample: a `Deny` runs `AfterTool`), `TestValidationErrorRunsNoHook`, `TestToolPreflightFailures` (the unknown call runs both hooks; the invalid call runs none), `TestUnknownToolRunsBothHooks` (`BeforeTool` runs once with no tool definition and the raw arguments, `AfterTool` runs once with `IsError` true and the `Tool <name> not found` text; counterexample: a validation failure runs zero hooks, row ST5d), the phase 03 test of the `Cancel` decision (still green), `TestSettledToolFailureWinsOverLateAbort`, `TestLateToolSuccessAfterAbortBecomesAborted`.
- Control-point cancel (finding 2), separate from body cancel: `TestCancelDuringBeforeToolTakesEffectAfterHandlerReturns` (the handler blocks on a channel, ignores `ctx`, then allows: the body runs zero times, the call gets `aborted before dispatch`, and no other call's `BeforeTool` starts before it returns), `TestCancelDuringAfterToolTakesEffectAfterHandlerReturns` (the handler blocks, then returns success: the result becomes `aborted`; the drain of the other started bodies begins only after it returns), `TestAbortDuringBeforeToolCallSkipsExecute`.
- Three facts (finding 3): `TestCallIsRecordedBeforeBeforeTool` (the handler reads `Entries()` and finds the `ToolCall` entry), `TestAbortClassifiesByBodyInvocationNotCallRecord`, `TestRepairUsesRecordedCallNotBodyInvocation` (the writer fails while call 3 is recorded and its `BeforeTool` blocks: calls 1-3 get `outcome unknown`, the body of call 3 ran zero times; counterexample: classification by body invocation gives call 3 `not started`).
- `ExecuteTool` wrapper (ST27): `TestExecuteToolShortCircuitSkipsBody`, `TestExecuteToolErrorSkipsAfterTool`, `TestExecuteToolCachedSuccessAfterAbortBecomesAborted`, `TestExecuteToolCannotDetachBodyFromAbort`.
- Added context (D27): `TestAfterToolContextEntersNextTurnWithoutWaking` (a batch whose results all terminate plus added context: the cycle continues and the next request contains the context; counterexample: without context the cycle ends), `TestAfterToolContextAppendedAfterAllResults`, `TestContextAfterToolAbortWaitsForNextSend`.
- Abort: `TestAbortGivesEveryCallAnOutcome`, `TestAbortDrainsStartedBodiesBeforeSettled`, `TestAbortBeforeBatchStartsNoBody`, `TestPreAbortedBatchRunsNoHookAndNoValidation`, `TestAbortWaitsForUncooperativeToolAndRejectsNewPrompt` (a ctx-ignoring exclusive tool: the run does not settle and a second `Prompt` gets `ErrBusy` until the tool is released; goleak after release), `TestRepeatedAbortNeverExceedsMaxParallelTools`, `TestAbortDuringToolBatchEndsWithAbortedAssistantMessageAndNoModelCall`, `TestAbortMessageMatchesRunFailureMessageShape`. <!-- red-team #15 -->
- IDs: `TestToolCallIdReusedFromEarlierTurnIsRejected`, `TestRepairMatchesByAssistantEntryAndCallId`. <!-- red-team #15 -->
- Repair: `TestRepairMarksNotStartedAndUnknownForOpenTurnOnly`, `TestRepairNeverRunsToolAgain`, `TestRepairNeverOverwritesCommittedResult`, `TestRepairKeepsUnansweredCallOfClosedTurn`, `TestRepairWriteFailureKeepsOriginalCause`, `TestPairingHoldsAfterEveryAbortPath`.
- `cmd/tui`: `TestPrintExitsOneAfterAbortDuringToolBatch`.
- D20: panics in body, `BeforeTool`, `AfterTool` still give error results.

```sh
go test ./internal/agent/... ./internal/tools/... -run 'Tool|Abort|Repair|Pairing|Concurrency|Context|Cancel|Execute'
go test -race -count=20 ./internal/agent/... -run 'Tool|Abort|Cancel'
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 09
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `09`).

## Risks & rollback

- Races between workers and the coordinator (Med x High). Mitigation: workers share no state; buffered channels; `-race -count=20`.
- Fewer parallel calls for tools that do not declare safety (Low x Low). Mitigation: Echo declares safe; H5 tools decide per tool with the DeepSeek rule.
- A tool or a control handler that ignores `ctx` keeps the Agent busy until it returns (Low x High). Mitigation: the cancel contract in `internal/tools` godoc and in the `pipeline` README; H5 tests every process tool for process-group kill on cancel; `TestAbortWaitsForUncooperativeToolAndRejectsNewPrompt` and the two control-point tests document the behavior.
- Repair gives `not started` for a call that ran (Low x High). Mitigation: the `ToolCall` entry is written before any hook or body; `TestRepairUsesRecordedCallNotBodyInvocation`.
- Rollback: reset to tag `lifecycle-p09-base`.

## Done criteria

- One coordinator; `sequentialExecutor`, `parallelExecutor` and `tools.Sequential` are gone (`grep -rn 'Sequential()' --include='*.go'` returns nothing).
- Every call has a `ToolCall` entry before its hooks; abort classifies by body invocation and repair by the call record.
- No tool runs twice in any test; abort, control-point cancel, added-context and repair tests pass; green tests, race, lint and the phase-09 matrix check.
