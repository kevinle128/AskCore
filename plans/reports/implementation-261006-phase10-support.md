# Headless lifecycle and tool contract support

Status: DONE.

## Scope

This work adds the planned headless lifecycle and signal fixtures and the missing print retry and tool contract tests.
It uses the accepted full scope with --tdd and --auto.
The controller and independent tester own the final conformance sweep and repository gates.
No agent source, matrix, status file, dependency, generated file, changelog, or cassette was changed.

## Real and injected parts

The main lifecycle and signal fixtures use newHeadlessAgent and runHeadless with the real Token Plan adapter and JSON writer.
The HTTP transport returns a hand-written rate limit and model SSE replies.
The retry wait records the production delay instead of sleeping.
Extra fixture tools use channel gates and the real registry validation and coordinator paths.
A second listener waits one millisecond on each event to check complete ordered JSON output.
The tests use no paid live model call.

The durable lifecycle subtest uses the same HTTP fixture through the real provider registry, agent.New, app.BindAuth, auth service, and isolated auth store.
Config.NewContext supplies a MemoryLog so the test can read durable entries.
This companion composition is needed because newHeadlessAgent has no public log hook.
No production hook was added.

The faux retry fixture uses the product faux provider, registry policy capture, agent.New, builtin tools, MemoryLog, and runHeadless.
It matches the shipped credential-free faux branch and adds no auth method.
It builds those components directly because newHeadlessAgent installs its fixed fauxDemo script on every request.
The fixed demo script was not replaced.

## Assertions

TestHeadlessRetryToolsSteeringFollowUpAndSlowListener checks a failed rate-limit attempt followed by a successful attempt and a recorded one-second delay.
It checks the agent_end willRetry flag, auto_retry_start, and successful auto_retry_end in that order.
Two safe bodies overlap and the undeclared tool runs alone.
Steering enters the next request and the queued follow-up opens a second cycle.
Each JSON event matches the corresponding Go event in publication order and the stream ends with agent_settled.
The constructor and durable companion runs exit 0.
The durable companion checks one RetryScheduled, two initial AttemptSettled entries within one Ask turn, and three Ask turns across two cycles.

TestHeadlessSIGINTDuringToolBatchDisposesAndExits130 sends SIGINT after a cooperative body starts.
The body returns before the Go listener receives agent_disposed.
Both requested calls get outcomes and no further model request starts.
JSON records the aborted cycle with cause disposed and ends with agent_settled without agent_disposed.
The Go listener receives agent_disposed after agent_settled and the exit code is 130.

TestHeadlessPrintRetriesTransientFailureOnce checks two faux requests, the captured default policy, one durable RATE_LIMIT retry, the recovered print reply, and exit 0.
Its JSON subtest checks exactly one auto_retry_start and the recovered reply.
RetryScheduled is a durable entry and is not a JSON event.
The controller corrected the matrix wording for that distinction.

TestToolDeclCarriesOnlyNameDescriptionParameters checks the declaration key allowlist for a tool with a concurrency classifier and checks that returned declarations cannot change the registry.
TestValidateAllowsExtraKeysAndAppliesNoDefaults checks that an extra nested key survives Prepare and Execute and that an omitted optional property receives no schema default.

## Test-first evidence and verification

The tests were added before any proposed production correction.
The existing product behavior met the accepted criteria and no production correction was needed.
An initial fixture assertion counted Pi turn_start events as Ask turns and failed on the retry.
Source inspection showed that TurnStart carries only CycleID and opens for each attempt.
The fixture now checks Pi publication separately and reads the durable entry order for Ask turn identity.

The lifecycle, signal, and print retry fixtures passed five runs under the race detector.
The new tool contracts and existing snapshot copy test passed five race runs.
TestCassetteOneToolCall and TestCassetteParallelToolCalls passed through runHeadless under the race detector.
No cassette request body changed and no recording was made.
Scoped CLI and tools lint passed with 0 issues.
The cassette testing guide now documents the lifecycle layers and Agent retry policy.
No background process remains from this task.

## Concerns

The fixed shipping faux demo has no scripted transient-failure injection point.
The report states the direct product-component composition used for that single scenario.
The real headless constructor is covered by the HTTP lifecycle and signal fixtures.
