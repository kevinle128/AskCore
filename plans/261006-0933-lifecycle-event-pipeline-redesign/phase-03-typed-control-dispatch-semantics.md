---
phase: 3
title: "Typed control dispatch: new semantics"
status: done
priority: P1
effort: 6h
dependencies: [phase-02]
---

# Phase 03: Typed control dispatch, new semantics

## Goal

Add the behavior changes of the dispatch layer on top of the phase-02 port: the failure rule per point, the frozen pre-tool decision (DeepSeek), the empty-continuation bound, and registration with an owner and a disposer. <!-- red-team #13 (split) -->

## Context links

- Design sections 7.3-7.5; phase 00 failure table
- Argument rewrite to remove: `internal/agent/loop_tools.go:286-313`; `internal/pipeline/hooks.go:79-90` (`BeforeToolCallResult.Args`, ported in phase 02)
- DeepSeek: pre-tool decision `packages/core/tools/src/index.ts:598-611` (allow/deny/cancel/ask; "Input rewriting is excluded because arguments are already logged and presented"); deep freeze of logged messages `packages/core/session/src/index.ts:170-185`; StopTurn failure `contract-regressions.spec.ts:346`; scoped registration `scope-lifecycle.spec.ts:92,542,1016`

## Files to Create / Modify

- Modify: `internal/pipeline/points.go`, `decisions.go`, `registry.go`, `README.md`
- Modify: `internal/agent/loop_tools.go`, `loop_stage.go`, `lifecycle.go`, `types.go`
- Modify tests: `internal/agent/loop_tools_test.go` `TestToolArgs` subtest "BeforeToolCall replaces the args" (:254-262) is deleted on purpose; `internal/pipeline/*_test.go`

## Tasks & Steps

1. **Pre-tool decision (follow DeepSeek).** `BeforeTool` returns `Allow`, `Deny{Reason, Terminate}` or `Cancel`. It cannot change arguments. The arguments are validated once (D21 coercion) and frozen: `ExecuteTool` receives a read-only prepared call (tool from the turn snapshot, a private copy of the validated arguments, no setter). The wire events `tool_execution_start` and `tool_execution_update` keep the raw model arguments, as Pi (D25 JSON exception; the start event is emitted first, as in Pi). Inside the process, `BeforeTool`, `ExecuteTool`, the body and `AfterTool` see the validated (coerced) arguments (matrix ST20, B1, D21). Handlers get copies of the arguments and of the tool-call arguments in their assistant message and context view. `ExecuteTool` handlers cannot change the working directory either. <!-- red-team #15, D25 -->
2. **Failure rule** (phase 00 table): an error or panic in `AdmitStep`, `PrepareRequest`, `ExecuteModel`, `RecoverModel`, `CompleteStep`, `StopTurn` ends the run through the D20 wrapper and the cycle reason is `error`. An error or panic in `BeforeTool`, `ExecuteTool`, `AfterTool` becomes an error tool result. Which of these still runs `AfterTool` is the phase 09 case table; this phase keeps today's routing.
3. **Empty continuation bound (diverge-deliberate CONT-BOUND, value 3).** Count consecutive `Continue` decisions that start a turn with no new input and no tool work. The fourth ends the cycle with reason `continuation-limit`. DeepSeek and Pi have no bound (`loop.spec.ts:1086`). The bound changes what an upstream run can do (a fourth empty continuation is not sent), so it is a divergence, not an addition (matrix row 59).
4. **Registration owner and disposer.** Each entry has an owner and a disposer; disposing an owner removes its handlers for later dispatches; an in-flight chain keeps its snapshot. Two scopes: application (one registry shared by Agents) and Agent (the Agent's own registry, dispatched after the application registry).
5. `StopTurn` cannot override `End`.

- **StopTurn after a max-tokens stop (follow DeepSeek `agent.ts:359-361`, lead decision after the phase 01 review).** A turn that stops at the length limit still dispatches `StopTurn`. If a handler queues steering, the cycle continues with a next turn; otherwise the cycle ends with reason `max-tokens`. A `Continue` decision from `CompleteStep` alone still does not continue after a length stop (the guard in `loop_stage.go` stays). Test: `TestStopTurnSteerContinuesAfterMaxTokens` (a handler that steers gives a second provider call; without the steer, one call and `cycle_end{max-tokens}`).

## Tests

The phase owns every matrix row with Phase `03`; run `check-conformance-matrix.sh 03`.

- `TestBeforeToolCannotChangeArguments` (a handler that edits the argument bytes it received does not change what the tool receives), `TestExecuteToolCannotChangeArgumentsOrTool`, `TestToolStartEventShowsRawModelArguments` (a call with `{"n":"5"}` for an integer parameter: `tool_execution_start` and `tool_execution_update` carry `{"n":"5"}`, while `BeforeTool`, the body and `AfterTool` see `{"n":5}`; counterexample: the validated 5 on the wire fails), `TestHandlersCannotEditArgumentsThroughSharedViews`, `TestExecuteToolCannotChangeWorkingDirectory`.
- `TestCompleteStepHandlerErrorEndsRunWithError`, `TestStopTurnHandlerPanicEndsTurnWithError` (also asserts that a later `Prompt` runs normally: the loop survives, DeepSeek `contract-regressions.spec.ts:346-370`), `TestStopTurnCannotOverrideEnd`.
- `TestBeforeToolCallCancelSkipsExecute` (a `Cancel` decision gives the aborted-before-dispatch result and the body runs zero times).
- `TestEmptyContinuationStopsAtLimit` (counterexample in the same test: a `Continue` that carries new input resets the count).
- `TestDisposedOwnerHandlersDoNotRun`, `TestAgentScopedHandlerDoesNotRunForSiblingAgent`, `TestInFlightChainKeepsDisposedHandler`.
- D20 tests keep passing.

```sh
go test ./internal/pipeline/... ./internal/agent/...
go test -race ./internal/pipeline/... ./internal/agent/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 03
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `03`).

## Risks & rollback

- A hook author relied on argument rewrite (Low x Med). Mitigation: no in-repo hook rewrites arguments outside the deleted subtest; `Deny` with a reason is the replacement; README states it.
- Rollback: reset to tag `lifecycle-p03-base`.

## Done criteria

- `BeforeToolCallResult.Args` (or its port) no longer exists; `ExecuteTool` input has no setter.
- Green tests, race, lint and the phase-03 matrix check.
