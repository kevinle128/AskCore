# Phase 08 implementation

Status: DONE.
The agent and pipeline changes implement retries inside one durable turn and cycle.
The support report is [implementation-261006-1753-phase08-support.md](implementation-261006-1753-phase08-support.md).

## Changes

`ExecuteModel` now returns a settled `pipeline.ModelOutcome` with a copied message, provider failure and safe binding.
Its terminal opens the stream, publishes attempt events, consumes every frame and drains the producer before middleware returns.
The context passed to `next` controls generation, and the run cancellation remains attached through `context.AfterFunc`.
A middleware deadline returns its own error and cannot trigger provider recovery.
Handlers can return their own settled outcome or replace the terminal outcome.
A handler that returns no outcome remains invalid.

Admitted input remains staged until both request preparation points succeed.
The outer context projection includes a private copy of staged input before the first commit.
Preparation errors and panics acknowledge claims as failed, publish the error tail and save no input or assistant message.
Cancellation before dispatch sends no request and writes no request delta.
Credential resolution remains after input commit and inside the existing stream composition where applicable.

Each retry captures the prepared serving provider and policy before the stream starts.
Budgets belong to each provider and policy key and reset at the next durable turn.
Waits follow the registered policy with no jitter.
Retries repeat preparation, request freeze and dispatch without repeating admission or input commit.
The first binding is required on each subsequent stream call.

Failed attempts that retry publish their error message and settle once without adding a history message or running tools.
Retry records are written before and after the wait.
One projection function publishes the retry events and the Pi agent and turn pairs while the durable cycle stays open.
The final `agent_settled` remains after all retries and queued work.

Interrupted messages retain meaningful text, thinking, signatures and observed usage.
They drop tool calls and whitespace-only blocks.
Cancellation from the last chunk still marks the stored message aborted and preserves replay metadata.

## Tests and TDD evidence

The initial `TestPrepareFailureCommitsNothing` failed because the existing path saved both the user input and the error wrapper.
It passes after staged input and the preparation failure marker were added.
The same test covers preparation errors and panics and verifies a later prompt sends the correct new input.

The initial `TestModelMiddlewareTimeoutCoversStreamConsumption` failed because middleware returned before stream consumption and the final message completed normally.
It passes with the complete attempt terminal and a middleware deadline error.
The final test uses a one-second scripted delay with a 15 ms deadline to avoid a timing race from token pacing.

Focused retry checks cover three attempts in one cycle, policy delays, five retries, Retry-After bounds, serving policy capture, provider budgets, turn reset, input commits, admission, failed tool calls and usage.
Focused projection checks compare retry wire fields with durable entries and verify visible failed output closes before the next attempt.
Backoff checks cover Abort, Dispose, cancellation from the retry event listener and an ignored retry decision after Abort.
Interrupted checks cover text, reasoning, unfinished tools, whitespace, final-chunk signatures, replay and observed usage.
Preparation checks cover Abort, Dispose, later Prompt, no provider request and no saved input.
The credential canary test now includes two typed server failures and a successful retry.
Every phase-08 matrix test name exists in source.

The agent and pipeline package test command is `go test ./internal/agent ./internal/pipeline -count=1`.
The final completed run passed in 3.490 s and 0.901 s.
The controller runs the independent full, race, lint and hand-reviewed matrix gates.

## Accepted contract updates

ExecuteModel handler signatures changed from `*providers.Stream` to `*pipeline.ModelOutcome`.
Existing callers and tests were migrated to the complete-attempt contract.
Preparation tests now place input events after preparation and expect no saved message on preparation failure.
Tests that previously dispatched one additional request with an already cancelled context now expect no additional request.
The provider-without-preparer request log now captures the registered retry policy instead of a nil prepared record.
Owner READMEs describe the changed middleware, preparation and retry contracts.

## Remaining work

No agent or pipeline implementation issue is known.
Independent controller verification and review remain the final delivery gates.
No process was left running.

## Auto-review fix cycle 1

The review probes were first reproduced with the review overlay.
Each of the five failing probes was then promoted to a repository regression test before the implementation changed.
The promoted tests failed for replacement length tool dispatch, a missing replacement failure code, an unclosed retry after preparation failure, a false failure after a supplied successful retry and saved terminal partial content.
The additional visible QUOTA and wait-error tests also failed before the fix.
The red evidence is recorded in `/tmp/phase08-fix-cycle1-red.log` and `/tmp/phase08-fix-cycle1-additional-red.log`.

Final stop-reason normalization now runs after the full ExecuteModel middleware chain.
A replacement length outcome drops tool calls before it can reach tool execution.
The final accepted error supplies the cycle failure code.
Interrupted content and cleaned error text follow the same final boundary.

All retry-series exits now use one deferred completion path.
The path covers preparation errors after RetryStarted, wait errors, middleware errors, cancellation and successful outcomes supplied without dispatch.
Completion uses the current accepted outcome or error instead of the previous attempt settlement outcome.
The durable schedule must succeed before it becomes the current retry series.

The controller resolved the terminal-message contract from row 31 and D20.
All failed attempt content remains log-only.
A terminal error saves an empty D20 assistant error wrapper with its cleaned error text and original usage.
The visible QUOTA test verifies published partial text, no tool execution, one request, the saved empty error wrapper and preserved usage.
Existing credential tests keep their saved-error contract.

The cancel-before-content test now verifies both assistant wire boundaries and the next model-facing request through the production transcript transformer.
The two-failure cycle test observes actual Agent.State transitions and requires exactly Running then Idle.
The observed-usage test also verifies a successful tool step has its completed settlement and usage before the first turn_end, with one tool result.

The focused regression group passed in 0.864 s.
The full agent and pipeline tests passed in 3.645 s and 0.414 s.
The full agent and pipeline race tests passed in 5.397 s and 1.443 s.
After the durable schedule ordering adjustment, the focused retry race test passed in 1.999 s.
After the final matrix assertion additions, the focused cancel and tool-usage tests passed in 0.834 s.
The support agent corrected the real refusal test's durable-entry coverage in its own report.
Source is ready for controller simplification and independent verification.
No process was left running.

## Auto-review fix cycle 2

The original review overlay reproduced a token leak in the fallback message_start of two failures before provider start.
The runnable probe was promoted to `TestPrestartFailureTextIsCleanBeforeWire` before the fix.
The promoted test failed twice on the published errorMessage.
The red output is recorded in `/tmp/phase08-fix-cycle2-red.log`.
The final regression drives the production `fantasykit.Fail` helper before Start and then a successful reply.

The driver now cleans assistant ErrorMessage fields before both provider StartEvent publication and the settled fallback message_start.
The final middleware outcome still receives error cleaning and stop-reason normalization after the chain returns.
The same helper changes only ErrorMessage and leaves model Content intact.
The content-preservation test verifies an ordinary model answer containing a bearer-like phrase is unchanged.

The publication trace covered the provider start, settled fallback, middleware-supplied start, final message_end, retry messages and Agent failure wrapper.
The supplied and final outcomes already pass the final normalization boundary.
Retry messages carry the cleaned accepted error.
The Agent failure wrapper uses CleanDiagnostic before publication.

The credential canary test now includes a real Anthropic HTTP adapter receiving two 503 responses with bearer and query canaries before a successful streamed reply.
It checks every entry, wire event and reconstructed request, and verifies the successful answer stays intact.
The native Anthropic and OpenAI adapters publish Start before their HTTP call, so this real adapter fixture covers the started failure path.
The pre-start regression covers the production failure helper and the driver fallback directly.
No production adapter start order was changed.

The original overlay probe passed in 1.233 s after the fix.
The full agent and pipeline tests passed in 3.772 s and 0.332 s.
The affected agent race tests passed in 1.962 s.
Goimports formatted retry_events_test.go and the changed regression files.
The final goimports listing returned no files.
Source is ready for the independent verification and matrix gates.
No process was left running.
