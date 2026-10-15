# Ordered tool coordinator implementation

Status: complete, pending controller review and full repository gates.

## Scope

The implementation changes only the agent owner surface and its tests and README.
The support worker supplies the tool safety interface, durable ToolCall record, AfterTool context copies, shared protocol copies and headless acceptance test.
The implementation preserves the prior lifecycle changes and makes no commit or conformance status edit.

## Contracts

One coordinator replaces sequential and parallel executor selection.
Known arguments are validated and frozen once, and call intent is durable before lookup and pre-control.
Unknown names use the complete hook chain and retain the canonical unknown-tool error.
The pool defaults to 10, reuses freed slots, and applies exclusive barriers from the turn snapshot.
Pre-control, post-control, end events and result commits follow source order.
Workers execute only the middleware and body, use a buffered completion channel and coalesce progress to the latest value.
The ExecuteTool terminal preserves root cancellation and keeps actual body invocation separate from recorded intent.
Abort gives every requested call a result, preserves settled errors, drains started workers without a time bound and saves one aborted assistant wrapper without another model call.
A call waiting at an exclusive barrier skips ExecuteTool after cancellation.
Added context uses normal staged admission after all results, keeps an otherwise terminating cycle active and starts no run after an abort.
Abort, Reset and Dispose clear previously queued context, and disposal drops context produced later.
Repair operates only on an open turn created by the current run and runs before the failure wrapper closes it.
Repair matches assistant entry and call ID, preserves committed outcomes and closed turns, runs no body and joins a repair failure after the original failure.
The session ID set comes from history at run start and rejects reuse in later turns and runs.

## Test first evidence

The initial runnable tests failed with zero unknown-tool hooks, zero post hooks for denied and cancelled calls, and a peak of 12 bodies instead of the required default limit of 10.
Those tests passed after the coordinator replaced the old runner.
The repair test first found one assistant message with no synthetic outcomes instead of the required three messages, then passed after repair was implemented.
The late-abort denial test first returned the pre-dispatch abort text instead of the policy failure and passed after policy failure took precedence.
The repeated repair test first appended an extra synthetic result for two assistant requests sharing an ID in one open turn and passed after chronological tuple pairing.
The pointer tool-call test first proved shared argument mutation and would omit the pointer call from dispatch, then passed after the agent used the shared deep copy and extracted both pointer and value calls.
The exclusive barrier cancellation test first entered dispatch once after cancellation and passed after the coordinator checked cancellation before starting the waiting call.
The existing completion-order assertion now requires source-order end events and commits, as accepted.
Existing unknown, denial and cancellation hook assertions follow the accepted routing table.
Existing abort assertions now require all call outcomes and one aborted assistant in the same turn, with no further model request.
The six-tool-turn continuation test uses six distinct call IDs to retain its original continuation assertion under the accepted session uniqueness rule.
The obsolete executor strategy selection test and unused helpers were removed because the accepted coordinator replaces that strategy.
No unrelated test assertion was weakened.

## Verification

All accepted agent test names are present and assert behavior through the actual coordinator or Agent driver.
The malformed raw argument test uses actual coordinator preflight because the provider assembler requires final argument objects before dispatch.
The Agent tests cover uncooperative body drain, ErrBusy, repeated abort pool limits, before and after control cancellation, detached contexts, driver writer panic drain, durable repair and context queue lifecycle.
The full agent ordinary suite passed before final review with 3.49 seconds reported.
The targeted race run with count 20 passed with 22.81 seconds reported before the final pointer and barrier checks.
Scoped configured lint initially found unchecked cleanup, obsolete private helpers and two static checks; those were fixed and the next run reported zero issues.
Latest ordinary, repeated race and scoped lint outcomes are recorded below.

## Remaining gates

The controller owns the required simplification review, independent full repository tests and race checks, and the phase conformance review.
No implementation blocker remains.

The latest full agent ordinary suite passed with 3.205 seconds reported, and scoped configured lint reported zero issues.
A repeated race run exposed a test synchronization defect in the post-control cancellation fixture, which waited for sibling pre-control instead of its actual body start.
The captured stack showed that cancellation correctly skipped the sibling body, while the test waited for a completion signal that that body could not send.
The fixture now waits for the actual sibling body start before cancellation, so it proves drain and synchronous post-control behavior without a schedule assumption.

The stalled run was `go test -race -count=20 ./internal/agent -run 'Tool|Abort|Cancel'`, tracked as exec session 23946.
Its stack and failure output are at `/tmp/askcore-phase09-race-latest.txt`.
The owned test process PID 97060 was stopped with SIGQUIT to capture the blocked fixture stack; the owning go process PID 96646 then exited with status 1.
No process from that run remains active.
After the fixture correction, `go test -race -count=50 -timeout=30s ./internal/agent -run '^TestCancelDuringAfterToolTakesEffectAfterHandlerReturns$'` passed in 2.020 seconds.
The fresh `go test -race -count=20 -timeout=90s ./internal/agent -run 'Tool|Abort|Cancel'` passed in 23.237 seconds, with output at `/tmp/askcore-phase09-race-final.txt`.
The parallel abort fixture now declares safety and waits for an actual first body start before the second call's pre-control cancels the batch.
Its updated assertion proves an invoked body's aborted outcome and both undispatched sibling outcomes.
The final focused race check of that fixture and the post-control cancellation fixture with count 20 passed in 2.345 seconds.

The final full agent ordinary suite passed with 3.496 seconds reported.
The final scoped configured lint run reported zero issues.
All owned test and lint processes have finished.
The agent source and tests are frozen for the controller's simplification and independent gates.

## Review correction: explicit cancellation precedence

The original reviewer overlay `TestReviewExplicitCancelWinsOverBlock` failed before the fix because an explicit Cancel plus Block returned the denial text.
The permanent `TestExplicitToolCancelWinsOverBlockAndTerminate` failed before the fix and also detected that the Block termination flag ended the cycle.
The coordinator is the only agent caller that interprets BeforeTool decisions.
Its decision order is now explicit Cancel, then Block, then root-context cancellation.
This preserves the documented explicit Cancel precedence over Block and Terminate while a Block that returns after a root abort keeps its own failure text.
Both explicit cancellation and denial still run AfterTool.
The original overlay, permanent regression, late-abort failure checks and full agent suite passed after the fix.
The full agent suite reported 3.274 seconds.
The related cancellation and routing tests passed under the race detector with count 20 in 2.928 seconds.
Scoped configured lint reported zero issues.
All owned check processes have finished, and the corrected agent source is frozen for the final independent review.

## Review correction: frozen calls, pointer records and failure drain

The original reviewer probes reproduced shared ThoughtSignature and Namespace mutation, missed pointer assistant history, missing repair for a pointer assistant request, and duplicate repair after a committed pointer tool result.
All probes were promoted to permanent runnable tests before the agent fixes, with red output at `/tmp/askcore-phase09-review-correction-red.txt`.
Prepared and extracted calls now use the existing protocol assistant-block deep copy for all fields.
History, intent anchoring and repair share a pointer-or-value record read helper, including typed-nil guards.
Message copies retain their pointer forms through the protocol copy helper.
Repair now runs under the existing run guard, so a persistent tool-result writer panic is returned after the original writer panic and cannot skip AgentSettled.
The persistent writer test checks the exact original-first joined panic text.
Admission, preparation and completion handler sentinel tests reproduced an errors.Join wrapper where the original error identity was required.
The Agent now joins only a non-nil repair failure, so successful or unnecessary repair leaves the original handler error unchanged.

The actual Agent in-flight Next probe reproduced premature settlement before an invoked body returned.
The permanent test covers cached handler return and handler panic, checks the Agent remains running and rejects a prompt until the body returns, and has red output at `/tmp/askcore-phase09-inflight-next-red.txt`.
The shared middleware owner repaired accepted-Next completion and failure cancellation without banning asynchronous calls inside the handler invocation.
An actual Agent counterpart covers handler error and panic with Next called using context.Background and requires the body to return before a tool result is published.
That counterpart failed against the prior middleware through a read-only overlay, with red output at `/tmp/askcore-phase09-inflight-failure-red.txt`, and passed with the shared fix.
The tool body already has its own guard inside the terminal, independent of the worker goroutine guard.
The async body-panic test proves that this guard turns the panic into an error result and runs AfterTool without escaping the caller goroutine.
The refreshed reviewer overlay and all permanent focused regressions pass.

After all review corrections and the shared middleware join, the full agent ordinary suite passed in 3.275 seconds.
The agent race run with count 20 covering tools, abort, cancellation, in-flight Next, pointer records, repair, signatures and handler error identity passed in 24.680 seconds.
Its output is at `/tmp/askcore-phase09-review-correction-race.txt`.
Scoped configured lint reported zero issues.
All owned check processes have finished, and the corrected agent source and tests are frozen for independent final gates.
