---
phase: 2
title: "Typed control dispatch: port only"
status: done
priority: P1
effort: 8h
dependencies: [phase-01]
---

# Phase 02: Typed control dispatch, port only

## Goal

Replace the `Hooks` struct and the `Compose` merge rules with typed control points. For one handler at a point there is **no behavior change**: every current hook test passes after a mechanical port. New semantics come in phase 03, so the reviewer reads the port diff alone.

**Deliberate semantic change (D25, DeepSeek waterfall) for several handlers at one point.** The Pi merge rules of `Compose` ("later non-nil field wins", "first `Block` wins", "every function sees the earlier updates") are replaced by the DeepSeek Cordis waterfall (`packages/core/agent/src/dispatch.ts:143-147`): handlers wrap in registration order, the first registered is the outermost and has the final word, a handler without `next` short-circuits the inner ones, and a handler that wants to keep a downstream decision (a `Block`) must call `next` and keep it. Each terminal honors the input passed to `next`. In this repo there is one handler per point (`projectContext` plus at most one handler of the config), so single-handler behavior is unchanged. The old multi-handler `TestCompose*` scenarios are replaced by two-handler tests that pin the new rules (`TestPrepareRequestPassedOnInputReachesTerminal`, `TestPrepareRequestOuterOverrideWinsAndShortCircuitSkipsInner`, `TestBeforeToolPassedOnArgsReachTerminalAndOuterKeepsInnerBlock`, `TestAfterToolPassedOnResultReachesTerminalAndOuterOverrideWins`). <!-- red-team #13 (split) -->

## Context links

- Design sections 7.2-7.5
- Current API: `internal/pipeline/hooks.go:135-168` (9 fields), `internal/pipeline/hooks_compose.go:11-282` (merge rules), `internal/pipeline/README.md`
- `pipeline` importers (13 files, verified by `grep -rln '"AskCore/internal/pipeline"'`): `internal/agent/agent.go`, `context_source.go`, `loop_run.go`, `loop_stage.go`, `loop_tools.go`; tests `agent_test.go`, `agent_model_test.go`, `agent_tool_changes_test.go`, `loop_helpers_test.go`, `loop_run_test.go`, `loop_stage_test.go`, `loop_stream_test.go`, `loop_tools_test.go`. `cmd/tui` and `internal/app` do not import `pipeline`.
- `LoopConfig` is constructed at (verified by `grep -rn 'LoopConfig{'`): `cmd/tui/headless.go:247`; `internal/app/module_auth_test.go:95,137,220`; `internal/app/auth_readiness_test.go:74`; `internal/agent/agent_auth_test.go:18`; `agent_model_contract_test.go:37,94,151`; `agent_model_test.go:60,113,188,298,370,394`; `loop_run_test.go:405`; `loop_helpers_test.go:129`; `agent_test.go:31`; `loop_stage_test.go:76,164,329`. <!-- red-team #13 -->
- DeepSeek: `packages/core/agent/src/dispatch.ts:54-147`, `runtime-types.ts:259-393`

## API shape (decided here) <!-- red-team #13 -->

- `LoopConfig` keeps its fields. `Hooks pipeline.Hooks` becomes `Pipeline *pipeline.Registry`. `Hooks.GetAPIKey` becomes `LoopConfig.GetAPIKey func(ctx, provider string) (string, error)` (one function, same signature role as Pi `getApiKey`; not middleware). `Hooks.ConvertToLLM` becomes `LoopConfig.ConvertToLLM` (at most one, as today).
- The free functions `agent.Run` and `agent.Continue` keep their signatures except the `LoopConfig` field change. Phase 04 makes them unexported (the driver then needs the Agent's writer and cycle state) and moves their five test files to `agent.New` + `Prompt`.
- Field placement rule for later phases: per-run settings go into `LoopConfig`; per-Agent settings (writer factory, listener-error callback, retry wait, tool pool size) go into `Config`; retry policy goes on the provider registration (phase 08), never into `LoopConfig`.
- No new credential path: `BoundKey`, `Options.APIKey`, `GetAPIKey` and the stream-side `AuthRunner` stay the only four, as today. The planned `Config.Credentials` is dropped, and with it `TestCredentialsCannotChangeThroughPrepareRequest` (the types already prevent it: `pipeline.RequestUpdate` has no credential field, `internal/pipeline/hooks.go:54-59`). <!-- red-team #8 (Scope 6) -->

## Files to Create / Modify

- Delete: `internal/pipeline/hooks_compose.go`, `internal/pipeline/hooks_compose_test.go`
- Rewrite: `internal/pipeline/hooks.go` into `points.go` (input/result types per point), `middleware.go` (chain, one-shot `next`), `decisions.go` (ordered handlers), `registry.go` (typed slices, snapshot per dispatch)
- Create: `internal/pipeline/middleware_test.go`, `decisions_test.go`, `registry_test.go`
- Modify: `internal/pipeline/README.md`, `internal/agent/types.go`, the five non-test `internal/agent` files and the eight test files listed above, `cmd/tui/headless.go` (only if it sets `Hooks`; today it does not), `internal/app` tests (only the `LoopConfig` literal if a field moves)

## Tasks & Steps

1. **Port tests first.** For each test in `hooks_compose_test.go` and each `internal/agent` test that sets `Hooks`, write the same scenario against the new API. Keep scenario names; drop "Compose" from names. Review this test diff alone before the code.
2. Points and modes (same behavior as today):
   - Around: `PrepareRequest`, `ExecuteModel` (wraps today's stream call), `BeforeTool`, `ExecuteTool`, `AfterTool`. `BeforeTool` keeps Pi argument replacement in this phase (`loop_tools.go:286-313`).
   - Ordered decisions: `CompleteStep` (results `Proceed`, `Continue`, `End`; rule `End > Continue > Proceed`, today's `FinishTurn` merge), `StopTurn`.
   - `TransformContext` becomes `RequestUpdate.RequestMessages`, the messages of this request only (never stored). Side effects of the move: handlers no longer chain a transform (the outermost answer wins); the handler sees `Request.Context.Messages` during `PrepareRequest`, which is before the update of the same call is applied (the old transform saw the context after the update); nil keeps the context messages and an empty non-nil slice sends an empty request (`TestPrepareRequestMessagesNilKeepsAndEmptySendsNothing`). The poll hooks (`GetSteeringMessages`, `GetFollowUpMessages`) become the two poll functions `LoopConfig.PollSteering`, `LoopConfig.PollFollowUp` with today's call sites, so `TestSteeringPollPoints` keeps passing. Phase 06 removes them.
   - `AdmitStep` and `RecoverModel` exist with pass-through defaults only.
3. `next`: call at most once, synchronously; a second or late call returns `ErrNextReused` and does not run the terminal. This is an Ask addition at every point (matrix 56d); DeepSeek enforces it only for a prepared model call (row A1).
4. Modes (matrix 56, 56b, 56c): around points are a waterfall (each handler wraps the rest through `next`, outer to inner, and unwinds inner to outer); decision points keep the Ask precedence rule instead of DeepSeek's first-decision serial mode (PRECEDENCE, row A2); listeners stay emit mode (phase 05).
5. Registration: typed slice per point; dispatch takes a snapshot; registration during a chain does not change that chain.
6. The Agent registers its context projection as the first `PrepareRequest` handler (`agent.go:243`, `context_source.go:24-31`).
7. Update `internal/pipeline/README.md` (point table, merge rules now expressed per point).

## Tests

The phase owns every matrix row with Phase `02`; run `check-conformance-matrix.sh 02`.

- Contract: `TestNextCalledTwiceRunsTerminalOnce`, `TestNextCalledAfterHandlerReturnsIsRejected`, `TestZeroNextNeedsValidTerminalResult`, `TestOuterHandlerSeesDownstreamRejection` (the outer handler adds context, delegates, and returns the inner rejection unchanged; DeepSeek evidence `hooks-codex/tests/coverage-cases.ts:111`), `TestRegistrationDuringChainDoesNotChangeChain`.
- Modes: `TestAroundHandlersRunOuterToInnerAndUnwind` (records `A in, B in, terminal, B out, A out`; a handler that returns without `next` stops the chain, and the terminal does not run).
- Decisions: `TestCompleteStepEndBeatsContinueBeatsProceed`.
- Ported behavior (one per old merge rule): `TestPrepareRequestSeesProjectedContextFirst`, `TestPrepareRequestModelSwitchStaysForLaterRequests`, `TestBeforeToolBlockWithReason`, `TestAfterToolReplacedContentDropsStructuredContent`, and the renamed former `TestCompose*` tests.
- All existing `internal/agent` tests pass unchanged except the mechanical API port.
- Dropped with a reason: `TestSecondConvertToLLMIsRejected` and the "`TestComposeQueues` keeps passing" requirement. `ConvertToLLM`, `GetAPIKey` and the polls are single `LoopConfig` fields, so there is no second converter and no merge surface to test.
- Robustness: `TestExecuteModelHandlerThatDropsTheStreamFailsTheRun`, `TestExecuteModelHandlerCannotDetachStreamFromAbort`, `TestPanickingHandlerLeavesRetainedNextDead`.

```sh
go test ./internal/pipeline/... ./internal/agent/... ./internal/app/... ./cmd/tui/...
go test -race ./internal/pipeline/... ./internal/agent/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 02
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `02`).

## Risks & rollback

- A merge rule is lost in the port (Med x High). Mitigation: one ported test per rule in the old README table; the test diff is reviewed first.
- Rollback: reset to tag `lifecycle-p02-base`.

## Done criteria

- `Hooks` and `Compose` no longer exist; `grep -rn 'pipeline.Hooks' --include='*.go' .` returns nothing.
- No test changed its assertions; only API calls changed.
- Green tests, race, lint and the phase-02 matrix check.
