# Phase 09 independent review

## Final verdict

The final review score is 10/10, with zero critical findings, zero open warnings, and no required suggestions.
The corrected phase 09 source is approved after independent source checks and fresh verification.
All 34 matrix assertions and the additional plan criteria are verified, with source and test mappings below.
No matrix checkbox or implementation status was changed by this reviewer.

## Scope and method

The source baseline is `/private/tmp/askcore-lifecycle-phase08-baseline`.
The reviewed file set is `/private/tmp/askcore-phase09-changed-files.json`, plus the owning package READMEs and accepted support copy fix.
The review uses the full accepted phase 09 plan, all 34 phase 09 matrix assertions, project instructions, the implementation report, and the approved support review.
The task flags are `--tdd --auto`, with the full accepted scope and no `--yagni` reduction.
Specification checks come before quality approval.
Only this review report is owned by the reviewer.

## Findings from the initial frozen source

### Resolved critical: A repair writer panic escapes the lifecycle guard

`internal/agent/agent.go:480` calls `repairTools` outside `runGuarded` after the original failure was contained.
A writer that panics on each tool-result append panics again at `internal/agent/tool_repair.go:70` during repair.
The second panic escapes `Prompt` and prevents `agent_settled` and the normal failure tail.
The same escape on the queued-run goroutine can stop the process.
`TestReviewPersistentWriterPanicIsContainedDuringRepair` proves this with an actual Agent and a writer that accepts all other entry types.
The repair failure must remain contained and follow the original failure in the returned aggregate.

### Resolved critical: A body started by an in-flight `next` can outlive the aborted run

`internal/agent/tool_coordinator.go:145` reports a worker complete when `execute` returns, while `execute` can return when its middleware returns before an already-started terminal call returns.
The public one-call `next` contract permits a call made inside the handler invocation and does not reject this in-flight case.
The probe starts `next` in a goroutine, waits for the real body to enter, aborts the Agent, and lets the middleware return a cached result while the body remains blocked.
`TestReviewInflightNextBodyCannotOutliveRun` proves that `Prompt` settles before the invoked body returns.
A later prompt can therefore overlap that body, contrary to the accepted unbounded drain and busy-state rules.
The worker must join every terminal body it started, including when its handler returns or panics first.
This finding does not request a bounded drain or a different callback contract.

### Resolved important: Explicit Cancel loses to Block

The initial `prepare` decision switch tested `Block` before `Cancel`.
A result with both fields set returned its denial text instead of the canonical aborted-before-dispatch result.
`TestReviewExplicitCancelWinsOverBlock` proves this through the real tool loop.
The existing `BeforeToolCallResult.Cancel` contract says that Cancel wins over Block.
Root cancellation with a settled denial must still preserve the denial, as required by ST13.
The final decision switch checks explicit Cancel, then Block, then root cancellation, and the permanent regression also checks that Cancel drops Block termination.

### Resolved important: Tool call signatures remain shared across hook views

`internal/agent/loop_tools.go:40` copies only argument bytes in `handlerCall` and shares `ThoughtSignature` and `Namespace` pointers.
An edit in `BeforeTool` reaches the `Call` view in `AfterTool`, although `ToolCallInfo` documents a private copy for each dispatch.
`TestReviewToolHookCallSignaturesCopied` proves the mutation through a real run with signatures supplied by an ExecuteModel outcome.
`TestReviewPreparedCallSignaturesStayPrivate` isolates the same helper defect.
The helper predates this phase, but the new coordinator uses it for all hook call views and must preserve the accepted copy contract.

### Resolved important: Legal pointer messages are skipped by history and repair

The initial history scan at `internal/agent/loop_run.go:216` reads only value assistant messages.
The repair scan at `internal/agent/tool_repair.go:37` and its second pass at line 56 read only value assistant and tool-result messages.
The public message types permit pointer forms, and `protocol.CloneMessage` preserves those forms in the session log.
`TestReviewPointerAssistantHistoryReservesCallID` proves that pointer history does not reserve an already-used call ID.
`TestReviewRepairPointerAssistantRequest` proves that a pointer assistant request gets no repair result.
`TestReviewRepairKeepsPointerToolResult` proves that an existing pointer result is ignored and gets a duplicate synthetic result.
The actual effects are ID reuse, a missing required outcome, and duplicate repair output.
The fix must read both legal message forms without changing their stored type.

All original review probes are in `/private/tmp/askcore-phase09-final-review/overlay.json`.
The probes use temporary overlay files and do not change repository tests.

## Correction verification

All original probes in the unchanged reviewer overlay now pass.
`internal/agent/agent.go:480` guards repair and joins only a non-nil repair failure, so unnecessary repair preserves original error identity.
`TestPersistentWriterPanicIsContainedDuringRepair` checks the exact original-first joined panic text and one AgentSettled event.
`TestRunFailureReturnsOriginalHandlerError` checks direct sentinel identity for admission, preparation, and completion errors.
`internal/pipeline/middleware.go:53` registers an accepted next claim and its cancellation function under the same mutex that closes future claims.
The invocation closes future claims, cancels an accepted next only for a handler error or panic, and joins that next without a timer.
A successful cached wrapper result keeps the downstream context alive until next finishes, and the outer result still wins.
Permanent pipeline tests cover cached return, handler panic, next panic, both panics, acceptance before terminal entry, retained-next rejection, and failure cleanup with `context.Background()`.
Permanent Agent tests check the busy state and ErrBusy during drain, failure cleanup before result publication, and guarded body panic followed by AfterTool.
`internal/agent/loop_tools.go:37` now uses the shared assistant-block deep copy for every call field.
`internal/agent/loop_run.go:483` reads legal pointer or value assistant and tool-result records with typed-nil guards.
History ID reservation, call-intent anchoring, and both repair passes use that read helper without changing stored message types.
The permanent pointer tests check ID reservation, required repair, and preservation of an already-committed pointer result.
The source and test fixes use existing helpers and preserve the accepted public contracts.

## Specification source checks

One coordinator replaces the two old executor strategies, and the old Sequential capability has no Go caller left.
The body pool defaults to 10, counts active workers, refills a freed slot, and waits at exclusive barriers.
Classification receives copied validated arguments from the turn snapshot, treats false or panic as exclusive, and is checked again before a waiting call starts.
Call intent is committed before the start event, lookup, validation, or BeforeTool.
Validation runs once for known names, while unknown names retain raw bytes and use the full hook chain.
The prepare, execute, and post paths match the complete routing table, including mixed explicit Cancel and Block and settled denial after root cancellation.
The main coordinator alone runs pre-control, post-control, end events, and result commits in source order.
Workers use buffered completion and latest-value progress channels and do not wait for the publishing listener.
A slow post handler delays later starts and commits while running workers can finish.
The ordinary synchronous-body abort path stops starts, cancels bodies, and waits without a timer.
The shared invocation join also keeps an in-flight next body within that drain contract.
The shared aborted assistant constructor is used by the batch path and the Agent failure path, with no extra model call or attempt.
Context is queued after all results in call order, is non-waking, and can continue an otherwise terminating batch.
Context produced after abort waits for the next admitted cycle, while default Abort and Reset clear older queued context and Dispose drops late context.
Repair limits itself to the current run's open turn and uses recorded assistant-position and call-ID facts, with no body execution and no change to closed turns.
The value-message repair path keeps committed outcomes and returns ordinary repair errors after the original cause.
The corrected history and repair scans handle pointer messages, and repair panic containment preserves original-first error ordering.
The shared support copy fix covers pointer content blocks and preserves their legal pointer and typed-nil forms.
The intentional ConcurrencySafe, AfterTool context, and ToolCall entry contract changes are within the accepted scope.
No new credential-bearing entry or wire field is introduced by this phase.

## Matrix assertion mapping

All 53 named matrix tests passed in the final focused run.
The extra probes and permanent regressions verify pointer-message handling and asynchronous next drain for rows N4, 63, SS9, and N5.
The mapping records the actual tests and the assertions inspected, rather than treating a test name as proof.

| Row | Test source | Assertion inspected |
|---|---|---|
| 19 | `TestAfterToolContextEntersNextTurnWithoutWaking` at `internal/agent/tool_context_test.go:18` | An `AfterTool` handler returns added context for call c1 of a batch whose results all ask to terminate: the cycle does not end, the context is committed after every tool result of the batch, and the next request contains it. Counterexample: the same batch with no added context ends the cycle with one request. |
| 43 | `TestToolArgs` at `internal/agent/loop_tools_test.go:222`; `TestUnparsableArgumentsGiveValidationErrorResult` at `internal/agent/tool_preabort_test.go:66` | Arguments `{"n":` give an error result that quotes the raw text, and the tool does not run. |
| 44 | `TestToolPreflightFailures` at `internal/agent/loop_tools_test.go:166` | Unknown, invalid and blocked calls each get their error text and the tool body runs only for the valid call. |
| 46 | `TestActiveBodiesNeverExceedLimit` at `internal/agent/tool_coordinator_test.go:53`; `TestFreedSlotStartsNextBodyBeforeEarlierFinishes` at `internal/agent/tool_coordinator_test.go:227` | With 12 safe calls, the peak is 10; when call 2 finishes first, call 11 starts before call 1 finishes. |
| 47 | `TestBodiesResumeAfterExclusiveBarrier` at `internal/agent/tool_coordinator_test.go:170` | Calls safe, exclusive, safe, safe: the exclusive call runs alone after the first finishes, then the last two overlap. |
| 47b | `TestUndeclaredToolRunsAloneBetweenSafeCalls` at `internal/agent/loop_tools_test.go:138`; `TestConcurrencySafeFalseForTheseArgumentsRunsAlone` at `internal/agent/tool_coordinator_test.go:149`; `TestPanickingClassifierRunsAlone` at `internal/agent/tool_coordinator_test.go:152` | Two calls of an undeclared tool never overlap; `ConcurrencySafe` returning false for these arguments or panicking runs the call alone. |
| 48 | `TestRegistryChangeDuringTurnDoesNotChangeExecutableTools` at `internal/agent/tool_control_test.go:303` | A tool unregistered during a batch still runs for this turn with its snapshot mode. |
| 49 | `TestPreAndPostControlAndCommitsFollowSourceOrder` at `internal/agent/tool_coordinator_test.go:270` | With bodies that finish in reverse order, `BeforeTool`, `AfterTool` and the result commits all follow source order. |
| 49b | `TestCancelDuringBeforeToolTakesEffectAfterHandlerReturns` at `internal/agent/tool_control_test.go:148`; `TestCancelDuringAfterToolTakesEffectAfterHandlerReturns` at `internal/agent/tool_control_test.go:207` | See rows ST10 and ST12. |
| 50 | `TestAbortBeforeBatchStartsNoBody` at `internal/agent/tool_abort_test.go:73`; `TestAbortGivesEveryCallAnOutcome` at `internal/agent/tool_abort_test.go:22` | An abort before the batch runs no body and every call gets `aborted before dispatch`; an abort mid-batch gives `aborted` to started calls and `aborted before dispatch` to the rest, in source order. |
| 50b | `TestAbortDuringToolBatchEndsWithAbortedAssistantMessageAndNoModelCall` at `internal/agent/tool_abort_test.go:196`; `TestAbortMessageMatchesRunFailureMessageShape` at `internal/agent/tool_abort_test.go:232`; `TestPrintExitsOneAfterAbortDuringToolBatch` at `cmd/tui/headless_tool_abort_test.go:37` | After an abort in a batch, the last message is aborted, the faux provider got no further request, and print mode exits 1. |
| 51 | `TestRepairWriteFailureKeepsOriginalCause` at `internal/agent/tool_control_test.go:419`; `TestRepairUsesRecordedCallNotBodyInvocation` at `internal/agent/tool_control_test.go:361` | A writer failure while call 3 is recorded and its `BeforeTool` blocks: calls 1-3 get outcome-unknown, the body of call 3 ran zero times, and the error wraps the original cause first and the repair failure second. Counterexample: classification by body invocation gives call 3 not-started. |
| 53 | `TestAfterToolContextAppendedAfterAllResults` at `internal/agent/tool_context_test.go:44` | In a two-call batch where both `AfterTool` handlers return context, the log holds result c1, result c2, context c1, context c2, in that order. Counterexample: context committed right after its own result fails the order check. |
| 54 | `TestBatchTerminatesOnlyWhenAllResultsAsk` at `internal/agent/tool_coordinator_test.go:405`; `TestToolBlockTerminate` at `internal/agent/loop_tools_test.go:211` | In a two-call batch where only one result asks to terminate, the cycle sends another request. |
| 63 | `TestRepairMarksNotStartedAndUnknownForOpenTurnOnly` at `internal/agent/tool_repair_test.go:12`; `TestRepairNeverOverwritesCommittedResult` at `internal/agent/tool_repair_test.go:31`; `TestRepairNeverRunsToolAgain` at `internal/agent/tool_control_test.go:328` | After a driver failure, a call with a `ToolCall` entry and no result gets the outcome-unknown text, a call with no entry gets the not-started text, committed results stay, and no tool runs again. |
| B2 | `TestCallIsRecordedBeforeBeforeTool` at `internal/agent/tool_control_test.go:269` | A `BeforeTool` handler that denies the call reads `Entries()` and finds the `ToolCall` entry of that call; its error result entry comes after it. Counterexample: an entry written after `BeforeTool` would not be visible to the handler. |
| B4 | `TestSlowPostHookDelaysLaterCommitsAndStarts` at `internal/agent/tool_coordinator_test.go:316` | A slow `AfterTool` for call 1 delays the commit of call 2 and the start of call 3; it never blocks a running body. |
| B5 | `TestAbortGivesEveryCallAnOutcome` at `internal/agent/tool_abort_test.go:22`; `TestAbortClassifiesByBodyInvocationNotCallRecord` at `internal/agent/tool_abort_test.go:39` | A call whose `ToolCall` entry exists and whose `BeforeTool` is blocked at abort gets `aborted before dispatch`, not `aborted`; a call whose body started gets `aborted`. |
| N4 | `TestToolCallIdReusedFromEarlierTurnIsRejected` at `internal/agent/tool_control_test.go:290`; `TestRepairMatchesByAssistantEntryAndCallId` at `internal/agent/tool_repair_test.go:61` | A call that reuses an ID from an earlier turn gets an error result and does not run; repair answers only the call of the open turn. |
| N5 | `TestAbortWaitsForUncooperativeToolAndRejectsNewPrompt` at `internal/agent/tool_abort_test.go:130`; `TestRepeatedAbortNeverExceedsMaxParallelTools` at `internal/agent/tool_abort_test.go:156` | A ctx-ignoring exclusive tool keeps the run unsettled and a second Prompt gets ErrBusy until the tool returns; repeated aborts never exceed MaxParallelTools active bodies. |
| SA26 | `TestContextAfterToolAbortWaitsForNextSend` at `internal/agent/tool_context_test.go:66` | After `Abort` in a tool body, the `AfterTool` context is in no entry of cycle 1, no cycle starts by itself, and the first request of the next cycle contains it. Current Ask appends polled messages after a tool-batch abort (`loop_stage.go:191-195`); this test must fail on that path. |
| SB20 | `TestToolParallelOverlap` at `internal/agent/loop_tools_test.go:117`; `TestSafeCallsAllStartBeforeAnyFinishes` at `internal/agent/tool_coordinator_test.go:211` | Three concurrency-safe calls in one batch all start before the first one finishes. |
| SS9 | `TestRepairMatchesResultsBySameTurnAndCallID` at `internal/agent/tool_repair_test.go:74` | A failed Ask turn whose assistant message reuses a call id that an earlier Ask turn already answered still gets one error tool result for that id; a `tool_execution_start` with no matching call adds no result; no tool runs again. |
| SS10 | `TestRepairKeepsUnansweredCallOfClosedTurn` at `internal/agent/tool_repair_test.go:42` | The log has an Ask turn that ended with an unanswered call, then a new Ask turn fails with an open call: repair writes an error result only for the new call, and the earlier assistant message stays in the log with no stored result for its call. |
| SS11 | `TestTransformMessages` at `internal/providers/transform_test.go:13`; `TestRepairKeepsUnansweredCallOfClosedTurn` at `internal/agent/tool_repair_test.go:42` | The case "orphan call at the end gets a synthetic error result" gets a `toolResult` with "No result provided" and `IsError` in the request transcript only; NEW:TestRepairKeepsUnansweredCallOfClosedTurn also checks that this result never enters the log. |
| ST5 | `TestAfterToolRunsForDeniedAndCancelledCalls` at `internal/agent/tool_coordinator_test.go:92`; `TestToolPreflightFailures` at `internal/agent/loop_tools_test.go:166` | A call blocked by `BeforeTool` gets an error result with the block reason, and `AfterTool` runs once for it with `IsError` true and cannot run the body; the same holds for a `Cancel` decision. |
| ST5b | `TestBeforeToolErrorSkipsAfterTool` at `internal/agent/tool_control_test.go:21` | A `BeforeTool` handler that returns an error gives an error result with that text, and `AfterTool` runs zero times for that call. Counterexample: a `Deny` decision runs `AfterTool` (row ST5). |
| ST5c | `TestToolPreflightFailures` at `internal/agent/loop_tools_test.go:166`; `TestUnknownToolRunsBothHooks` at `internal/agent/tool_coordinator_test.go:34` | An unknown-tool call gets the `Tool <name> not found` error result; `BeforeTool` runs once for it with no tool definition and the raw arguments, and `AfterTool` runs once with `IsError` true. Counterexample: a call that fails validation runs zero hooks (row ST5d). |
| ST5d | `TestToolPreflightFailures` at `internal/agent/loop_tools_test.go:166`; `TestValidationErrorRunsNoHook` at `internal/agent/tool_control_test.go:35` | A call that fails validation gets the "Validation failed" error result, and `BeforeTool` and `AfterTool` run zero times for it. |
| ST10 | `TestAbortDuringBeforeToolCallSkipsExecute` at `internal/agent/tool_control_test.go:184`; `TestCancelDuringBeforeToolTakesEffectAfterHandlerReturns` at `internal/agent/tool_control_test.go:148` | An abort while `BeforeTool` blocks, followed by an allow, gives an error result with the aborted-before-dispatch text, and the body runs zero times; no later call's `BeforeTool` starts before the blocked handler returns. |
| ST11b | `TestPreAbortedBatchRunsNoHookAndNoValidation` at `internal/agent/tool_preabort_test.go:29` | With the context cancelled before the batch, `BeforeTool`, the body and `AfterTool` run zero times, and each call, including one with invalid arguments, gets one aborted-before-dispatch result. Counterexample: a validation error result for the invalid call fails the test. |
| ST12 | `TestLateToolSuccessAfterAbortBecomesAborted` at `internal/agent/tool_control_test.go:258`; `TestAbortSequentialBatch` at `internal/agent/loop_tools_test.go:454`; `TestCancelDuringAfterToolTakesEffectAfterHandlerReturns` at `internal/agent/tool_control_test.go:207` | A body that returns success after the abort, and an `AfterTool` that blocks during the abort and then returns success, each give an error result with the aborted text; the `AfterTool` added context stays queued in both cases (rows 53 and SA26); the drain of other bodies starts only after the blocked `AfterTool` returns. |
| ST13 | `TestSettledToolFailureWinsOverLateAbort` at `internal/agent/tool_coordinator_test.go:109` | A BeforeToolCall block, a BeforeToolCall error, an Execute error and an AfterToolCall error that each settle after the abort keep their own text in the toolResult, not the aborted text. |
| ST27 | `TestExecuteToolShortCircuitSkipsBody` at `internal/agent/tool_control_test.go:50`; `TestExecuteToolErrorSkipsAfterTool` at `internal/agent/tool_control_test.go:70`; `TestExecuteToolCachedSuccessAfterAbortBecomesAborted` at `internal/agent/tool_control_test.go:91`; `TestExecuteToolCannotDetachBodyFromAbort` at `internal/agent/tool_control_test.go:103` | A handler that returns a result without `next` gives that result and the body runs zero times; a handler error gives an error result and `AfterTool` runs zero times; a cached success returned after `Abort` becomes the aborted result; a handler that calls `next` with `context.Background()` still gives the body a context that `Abort` cancels. |

## Plan criteria outside the matrix

The phase test-name scan found every test named in the plan Tests section.
The review also checked the default pool cap, validation before every hook, classifier panic containment, progress coalescing, body invocation timing, worker drain on driver failure, original-error ordering, shared abort shape, pointer tool-call extraction, and retained snapshot behavior.
`TestProgressNeverBlocksWorker` sends 1000 updates while the coordinator is inside sibling pre-control.
`TestToolWriterPanicDrainsBodiesBeforeRepairAndFailureTail` checks driver panic drain and unknown outcomes after a one-time writer panic.
`TestAbortWhileExclusiveCallWaitsSkipsDispatch` checks that a call waiting at a barrier never enters middleware after cancellation.
`TestQueuedToolContextClearsOnAbortResetAndDispose` checks each queue-clearing operation.
`TestDisposeDropsContextProducedByLatePostHook` checks context finalized after disposal begins.
`TestRepairSkipsHistoricalOpenTurnAndDurablyClosedTurn` checks the current-run and open-tail limits.
`TestPairingHoldsAfterEveryAbortPath` checks pre-batch, pre-control, middleware, body, and post-control cancellation.
Existing body, BeforeTool, and AfterTool panic and error tests still check error-result content and continued execution.
The plan-required removal of obsolete executor-selection tests is intentional.

## Quality, caller, and public contract checks

The final specification review passes before quality approval.
The implementation keeps one source-order coordinator and uses existing package boundaries and helpers.
The support DTOs and entry fields expose only the intended added context and recorded identity facts.
Concurrency approval is host metadata and does not enter the model tool declaration.
The default exclusivity and removal of Sequential are intentional changes in the accepted plan.
Argument copies, call metadata copies, result copies, queue copies, and pointer-message reads prevent the reproduced cross-caller side effects.
The new middleware completion join applies to all around points and preserves one-call rejection, outer precedence, panic ownership, context values, and cancellation signals.
A running handler or body can still delay the Agent if it ignores cancellation, as the accepted unbounded drain contract requires.
No time bound, async callback ban, credential log field, extra approval flow, or compatibility shim was added.
The existing snapshot, headless cassette, error-result, admission, preparation, and completion contracts remain covered by source checks and tests.
No open quality, security, build, or caller regression was found in the corrected scope.

## Fresh verification

The focused ordinary tests passed for agent, tools, pipeline, sessions, and protocol with `Tool|Abort|Repair|Pairing|Concurrency|Context|Cancel|Execute|ClonePointer|CloneMessageCopiesPointer`.
The real headless `TestPrintExitsOneAfterAbortDuringToolBatch` passed without a paid live call.
The original probes reproduced all five finding groups on the initial frozen source and pass unchanged on the final corrected source.
The earlier support review independently passed its original copy overlay, permanent pointer copy tests, focused race tests, vet, and scoped lint.
The final exact matrix test run passed for agent, providers, and cmd/tui with all 53 matrix test names.
The final focused race run with count 3 passed for agent, pipeline, and protocol copy, join, panic, pointer, repair, and error-identity regressions.
The controller's tester owns full repository gates and the final broad race and lint checks after corrections.

## Status

Status: DONE.
Summary: All 34 matrix assertions and every accepted phase criterion are verified against the corrected source, and all reproduced findings are resolved.
Concerns/Blockers: None in the reviewed scope; repository-wide gate results remain owned by the controller and tester.
