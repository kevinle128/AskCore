# Phase 09 support implementation

Status: DONE

## Changes

`tools.ConcurrencySafe` replaces `Sequential`.
Its method receives validated arguments and returns whether the call can overlap with other safe calls.
A missing interface, false answer, or panic means the call runs alone.
Echo declares itself safe because it has no shared mutable state.
The CLI add and multiply test tools also declare themselves safe.
Their focused test checks validated arguments and real arithmetic results.

The tool interface, package godoc, and owner README state the cancellation contract.
Started bodies drain without a time limit after cancellation.
Each tool must return promptly when its context is cancelled.
A tool that starts a process must stop its entire process group.
The Agent stays busy until the bodies return.

`sessions.ToolCall` records AssistantEntry and CallID before lookup, validation, or control handlers.
AssistantEntry is the log position of the requesting assistant message.
The pair preserves the assistant scope during repair.
The entry is a plain value and adds no model message.
The session test checks its exact JSON, log-only projection, value clone, and separate assistant scopes with the same call ID.

`AfterToolCallResult.AddedContext` holds user messages for the next turn.
`AfterToolCallResult.Clone` copies all override fields and added messages.
The dispatch copies results at terminal, next-return, and handler-return boundaries.
Copies include content blocks, nested text signatures, raw JSON, usage, and pointer fields.
The Apply signature and override rules are unchanged.
The outermost returned result still wins; no automatic context merge was added.
The owner README records that the coordinator queues context in call order after all results and does not wake an idle Agent.
Only AfterTool supplies added context.

## TDD evidence

The first focused run failed because ConcurrencySafe, ToolCall, and AddedContext were absent.
After the fields were added, the isolation test failed because an outer handler changed the inner handler's retained result.
The same test passed after result-boundary copies were added.
It checks content, added context, signatures, raw JSON, error and termination pointers, and usage.
Further tests check pass-through context, outer result precedence, terminal result isolation, and nil versus empty overrides.

## Validation

`go test ./internal/tools ./internal/sessions ./internal/pipeline` passed.
`go test -race ./internal/tools ./internal/sessions ./internal/pipeline` passed.
`go test -race ./internal/pipeline -run 'AfterTool' -count=1` passed.
`go vet ./internal/tools ./internal/sessions ./internal/pipeline` passed.
Scoped lint for tools, sessions, and pipeline passed with 0 issues.
The CLI math and cassette tool gate could not compile while the agent worker's old executor still referenced tools.Sequential.
The controller accepted this temporary integration concern while the agent coordinator migration continues.
No compatibility shim was added.
No cassette, dependency, generated file, changelog, status file, or agent source was changed by this support task.
No background process remains from this task.

## CLI integration gate

After the agent coordinator migration compiled, `go test -race ./cmd/tui -run 'HeadlessMathToolsDeclareConcurrencySafe|Cassette.*ToolCall' -count=1` passed in 1.986 seconds.
This checks both declared-safe arithmetic helpers and the existing one-call and parallel-call cassette assertions.
No cassette change or support source correction was needed.
The controller owns the final integrated phase gates.

## Concerns

None for the support scope.

## Pointer block isolation correction

The review test `TestReviewAfterToolPointerBlockIsolation` failed before the correction because an outer AfterTool handler changed the inner handler's pointer text block.
Permanent tests then showed the same defect in pointer text and image blocks in content and added context.
Protocol tests also showed shared pointer text, thinking, and tool call blocks in assistant messages.
The shared protocol cloners now copy these legal pointer block types and their nested signatures and arguments.
The copies preserve each pointer type and typed nil value.
No separate pipeline cloner was added.
The permanent tests check handler and caller boundaries and all user and tool result message forms.
The review test passed after the correction.
`go test -race ./internal/pipeline ./pkg/protocol -count=1` passed after the final correction.
The session and provider package race tests passed during the shared-clone check.
Scoped lint for protocol, pipeline, sessions, and tools passed with 0 issues.
No agent source or cassette was changed.
No background process remains from this correction.

## Print mode abort acceptance

`TestPrintExitsOneAfterAbortDuringToolBatch` uses newHeadlessAgent and runHeadless in print mode with an injected cooperative tool.
The real Anthropic adapter reads the existing HTTP SSE fixture with two tool calls.
The test aborts after the first exclusive body starts and waits for the body to return on cancellation.
It checks exactly one body invocation and one model request.
It checks both tool outcomes in source order, with an aborted result for the started call and an aborted-before-dispatch result for the second call.
The last message is the aborted assistant wrapper, the cycle reason is aborted, stdout is empty, and print mode exits 1.
The test was added before any proposed production change.
The existing coordinator and print path met the assertions, so no production correction was needed.
The first run used the cycle cause instead of its reason; the test was corrected to check the reason field.
The focused race test passed five runs.
Scoped CLI lint passed with 0 issues.
No cassette, matrix, status file, or agent source was changed.

## Accepted next call drain correction

The final review found that an accepted asynchronous next call could continue after its outer handler returned.
The shared around dispatcher is used by AdmitStep, PrepareRequest, ExecuteModel, RecoverModel, BeforeTool, ExecuteTool, and AfterTool.
`TestAcceptedNextFinishesBeforeHandlerInvocationCloses` was added before the source correction.
All five cases failed because the invocation closed while the accepted next call was blocked.
The cases cover a cached return, a handler panic, a next panic, both panics, and an accepted call blocked in an inner handler before terminal entry.
The shared next state now has a completion channel created before the handler starts.
An accepted next call closes that channel when the inner chain returns or panics.
Handler completion first rejects future claims, then waits without a time limit when a call was accepted.
This preserves a legitimate accepted call even when terminal entry has not yet occurred.
The dispatcher still leaves each panic with its calling goroutine and preserves the outer handler result.
The owner README states this lifecycle contract.
The main worker was told to contain tool-body panics independently because an asynchronous next caller can run outside the worker's panic guard.
The new test and retained-next, one-call, waterfall, and error tests passed after the correction.
`go test ./internal/pipeline -count=1` passed.
`go test -race ./internal/pipeline -count=1` passed.
Scoped pipeline lint passed with 0 issues.
No agent source, public result contract, cassette, matrix, or status file was changed by this correction.
No background process remains from this correction.

## Accepted next call failure cancellation

The controller required a check of an outer handler error or panic without global Abort.
`TestHandlerFailureCancelsAndJoinsAcceptedNext` was added before the failure cancellation correction.
Both cases failed because the accepted next call used context.Background() and its cooperative body waited for cancellation that never arrived.
The state lock now records the derived next context cancel function and accepts its claim in one operation.
Handler completion closes admission, cancels an accepted call on error or panic, and then waits without a time limit for its completion.
The regression holds the cancelled body during cleanup and checks that the dispatcher cannot return early.
The cached-return regression checks that ordinary success does not cancel its accepted next call.
Panics still pass to the calling goroutine, retained next calls stay rejected, and the outer result still wins.
The owner README records the failure cancellation rule.
The full ordinary and race pipeline suites passed after this correction.
Scoped pipeline lint passed with 0 issues.
The main worker owns the corresponding actual Agent regressions and the terminal tool-body panic guard.
