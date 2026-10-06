# Phase 10 agent fixture implementation

## Result

Status: DONE.
The agent scope adds the required uncertain tool outcome fixture and closes five explicit assertion gaps in existing tests.
No production change was required.
The new fixture passed against the accepted phase 09 source on its first run.
There is no red-to-green production fix to report, and no failure was created to manufacture that evidence.

## Real and injected behavior

`TestWriteFailureWithUncertainToolOutcomeRunsNoToolAgain` calls the real `agent.New` through the existing `newAgent` helper and uses the shipped faux provider.
The real registry, request stream, ordered tool coordinator, session log, repair path, and later prompt path run in the fixture.
The counting tool is an injected tool body with an atomic execution count.
`Config.NewContext` provides the existing `commitGate` writer backed by `sessions.MemoryLog`.
The writer injects one error on the first `ToolResultMessage` append and allows the dispatch intent and all subsequent repair writes.
The fixture checks that the prompt returns the injected write error, the body executes once, and the committed dispatch intent identifies the assistant request.
It checks that repair writes one error result with the text `outcome unknown` and makes no further model request in that failed run.
A later real prompt receives another faux request for the same call ID.
The agent rejects that repeated ID, retains the uncertainty result in history, completes the later prompt, and keeps the body execution count at one.
The fixture uses no network inference, paid call, database, crash recovery, or real filesystem failure.

## Existing assertion gaps

`TestToolCompletionOrderAndSourceOrder` now checks the second tool result text as well as the existing exact start, end, and result event order.
`TestToolUnregisteredMidRunRunsThisTurnThenIsRemoved` now checks that request 3 contains only the original removal declaration.
`TestToolPreflightFailures` now checks serialized empty details and the absence of typed error name or code fields.
The same test records the completed Before hook, Execute middleware, and After hook order.
Its trace proves that known invalid arguments and denied calls do not reach Execute middleware.
It checks that each After hook receives the final result text and error flag.
The existing body and hook failure cases, override checks, registration checks, unchanged-tool checks, and validation hook checks already cover their required assertions and retain their original checks.
These changes add assertions and do not weaken or replace an existing contract.

## Validation

`go test ./internal/agent -run '^TestWriteFailureWithUncertainToolOutcomeRunsNoToolAgain$' -count=1` passed in 0.830 seconds.
`go test -race ./internal/agent -run '^TestWriteFailureWithUncertainToolOutcomeRunsNoToolAgain$' -count=5` passed in 2.245 seconds.
The focused ordinary gate for preflight, ordered results, registry changes, failures, overrides, validation, and repair passed in 0.829 seconds.
`go test ./internal/agent -count=1` passed in 3.320 seconds.
`go test -race ./internal/agent -run '^(TestToolPreflightFailures|TestToolCompletionOrderAndSourceOrder|TestToolUnregisteredMidRunRunsThisTurnThenIsRemoved)$' -count=5` passed in 1.730 seconds.
Goimports formatted only the three changed agent test files through the existing temporary modfile.
All test processes started for this work exited with status zero.

## Remaining gates

The controller owns the independent full test, race, lint, and final conformance gates.
The support worker owns the CLI lifecycle, disposal, real adapter cassette, and tool contract fixtures.
This report does not claim those gates have passed.
No agent acceptance gap remains in this worker's scope.

## Final conformance coverage additions

`TestWriteFailureStopsFurtherSideEffects/second_append` now injects an error on the real writer's second `Append` call.
The original assistant-message write failure case remains unchanged.
The added case checks the committed first snapshot, the returned write error, no model request, no tool body, no tool start, and `agent_settled` as the last event.
`TestFoldUsageUsesZeroBucketsAndDerivedTotal` runs the real Fantasy stream fold and assembler with injected finish usage.
It checks zero cache buckets when omitted and a derived input-plus-output total of 22 despite a supplied total of 99.
It checks both populated cache buckets and a derived total of 30 despite a supplied total of 99.
The default fold leaves the optional reasoning pointer nil, and the test preserves that contract.
The existing `TestNormalizeUsage` raw-total preservation assertion remains unchanged.
Both new cases passed on the current production source before any source change.
No production correction was required.
The focused ordinary tests passed for the agent package in 1.161 seconds and the Fantasy fold package in 0.422 seconds.
The same focused tests passed with race detection and five repetitions for the agent package in 2.287 seconds and the Fantasy fold package in 1.449 seconds.
All added test processes exited with status zero.
