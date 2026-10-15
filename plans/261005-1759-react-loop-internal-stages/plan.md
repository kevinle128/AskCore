---
title: "ReAct loop as internal stages in internal/agent"
description: "Refactor loop.run into unexported ReAct stages with an explicit flow enum and a tool-executor strategy, with no change to Pi event order."
status: done
priority: P2
effort: 9h
branch: master-2
tags: [agent, pipeline, loop, refactor, react, design-patterns, hooks]
created: 2026-10-05
analysis: plans/reports/xia-261005-1759-react-loop-stage-pipeline-analysis.md
---

# ReAct loop internal stages

`loop.run` (`internal/agent/loop_run.go:105-215`) is a 1:1 port of Pi `runLoop` with two nested loops and three flags (`hasMoreToolCalls`, `explicitContinuation`, `pending`). This plan splits one turn into six unexported stages (steer, prepare, reason, act, observe, decide), drives them with a small loop that reads an explicit `flow` value, moves tool batch selection behind a `toolExecutor` strategy, routes every hook call through one loop method per hook point, and adds `pipeline.Compose` so several hook sets act as one `Hooks`. It is a pure refactor: the event stream, poll points, hook call order, and error semantics stay byte-identical (ignoring `ts`, `runId`, `timestamp`).

User decision (locked, 2026-10-05): option A, internal stages. Public extension stays `pipeline.Hooks`. No `PipelineDeps` closure bag, no god `RunState`.

User decisions (locked, 2026-10-05, second round): (1) single hook seam: one loop method per hook point (phase 2, phase 3 for `BeforeToolCall`); (2) add `pipeline.Compose` now with a merge rule per hook point derived from Pi (phase 4); (3) replace the manual wrap at `internal/agent/agent.go:171` with `Compose`, no behavior change; (4) H11: the `hooks.Dispatcher` becomes an adapter in `internal/pipeline` that produces `pipeline.Hooks`. Three layers: port (`Hooks`), composite (`Compose`), adapter (H11).

## Phases

| # | Phase | Status | Depends on | Files owned |
|---|---|---|---|---|
| 1 | [Baseline capture](phase-01-baseline-capture.md) | done | none | scratchpad only |
| 2 | [Stage abstraction, flow enum, driver, hook seams](phase-02-stages-flow-driver.md) | done | 1 | `internal/agent/loop_run.go`, `internal/agent/loop_stream.go`, new `internal/agent/loop_stage.go` |
| 3 | [Tool executor strategy, BeforeToolCall seam](phase-03-tool-executor-strategy.md) | done | 2 | `internal/agent/loop_tools.go` |
| 4 | [Hook composite (`pipeline.Compose`)](phase-04-hooks-compose.md) | done | 3 | new `internal/pipeline/hooks_compose.go`, new `internal/pipeline/hooks_compose_test.go`, `internal/pipeline/hooks.go`, `internal/pipeline/README.md`, `internal/agent/agent.go`, `internal/agent/context_source.go`, `internal/agent/types.go`, `internal/agent/loop_tools.go` (after phase 3) |
| 5 | [New tests](phase-05-new-tests.md) | done | 2, 3 | new `internal/agent/loop_stage_test.go` |
| 6 | [README: file list and design pattern map](phase-06-readme-pattern-map.md) | done | 2, 3, 4 | `internal/agent/README.md` |
| 7 | [Verification and JSONL diff](phase-07-verification.md) | done | 1-6 | none (read-only) |

Phases run in order. Phase 3 depends on phase 2 because the act stage is the only caller of the executor selection. Phase 4 runs after 3 because it moves the AfterToolCall apply step out of `loop_tools.go`, which phase 3 owns. Phases 5 and 6 own different files and may run in parallel after 4.

## Data flow (one run)

`Run/Continue` emit `agent_start`, `turn_start` (and prompts), then call `l.run()`.
`l.run()` polls steering once, then loops: for each stage in `turnStages`, call `s.run(l)` and read its `flow`.
- `flowNext`: go to the next stage.
- `flowNextTurn`: start the stage list again (inner loop of Pi).
- `flowIdle` / `flowIdleContinue`: poll follow-ups. Follow-ups found means set pending and next turn. Else `flowIdleContinue` gives one more turn. Else emit `agent_end`.
- `flowEndRun`: emit `agent_end` and return.
Per-turn data lives in a typed `turnState` field of `loop` (assistant message, tool results, whether tools want another turn). Cross-turn data stays in `loop` (`pending`, `turns`).

Hooks: `Agent.execute` builds `cfg.Hooks, err = pipeline.Compose(Hooks{PrepareRequest: projection}, userHooks)`. The loop reads each field only in its seam method. With N hook sets, `Compose` rejects more than one ConvertToLLM (user decision 2026-10-05) and applies the merge rule of each point (table in `phase-04-hooks-compose.md`); with one set it returns the same functions.

## Acceptance criteria

- [x] `loop.run` no longer has the `hasMoreToolCalls` and `explicitContinuation` locals; flow is a `flow` enum value returned by stages.
- [x] Six stages exist as unexported types with `name()` and `run(*loop) (flow, error)`; stage list is one slice literal.
- [x] Tool batch selection is a `toolExecutor` interface with `truncated`, `sequential`, `parallel` implementations; `batchRun` code is reused, not duplicated.
- [x] `git diff --stat -- 'internal/agent/*_test.go' 'internal/pipeline/*_test.go'` shows only added files; no existing test file changed.
- [x] `go test ./internal/agent/... ./internal/pipeline/... -race -count=1` passes; `go test ./... -count=1` passes; `golangci-lint run ./...` reports 0 issues.
- [x] JSONL of `ask -p "echo hi" --mode json` and `ask -p "hello" --mode json` (also `fail x`) are identical to the baseline after removing `ts`, `runId`, `timestamp`.
- [x] Exit codes of the three prompts are unchanged.
- [x] `internal/agent/README.md` lists the new file and has a design pattern map with a "how to add a stage" example (compaction before reason).
- [x] Non-test `internal/agent` files read `cfg.Hooks.<Field>` in exactly 9 places, one per seam method.
- [x] `pipeline.Compose` exists in `internal/pipeline/hooks_compose.go`; one table-driven test per merge rule passes in the new `hooks_compose_test.go`; `Compose(h)` returns the same function pointers as `h`.
- [x] `agent.go` uses `pipeline.Compose` and has no hand-written hook wrap; `AfterToolCallResult.Apply` is the only copy of the override rule.
- [x] `internal/pipeline/README.md` documents the three layers and the merge rules.
- [x] No code comment in `internal/agent` or `internal/pipeline` references plan, phase, or finding IDs.

## Out of scope

MaxTurns guard (H9), compaction stage (H10), exporting stages (H11), the H11 Dispatcher adapter itself, advisor gate. The fields and types of `pipeline.Hooks` do not change; the package only gains `Compose` and `AfterToolCallResult.Apply`.

## Rollback

The work is focused commits in `internal/agent` (phases 2, 3), two in `internal/pipeline` plus `agent.go` (phase 4: the `Apply` move, then `Compose`), and README commits. Rollback is `git revert` of those commits. No data, schema, wire format, or public API changes, so nothing downstream needs migration. If phase 6 finds a JSONL diff that cannot be fixed inside the refactor, revert the phase 4, 3 and 2 commits in that order and keep only the new tests that pass on the old code.

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| Silent change of steering poll points (first-turn skip, "poll only if pending empty") | M x H | `TestSteeringPollPoints` is unchanged; decide/steer rules copied verbatim (phase 2 table); new flow-matrix test |
| `ToolResults` changes from `[]` to `null` in `turn_end` | M x M | keep `[]protocol.ToolResultMessage{}` default; JSONL diff catches it |
| Error tail (`StopError`/`StopAborted`) runs act or polls queues | L x H | act and observe skip on a failed message; decide ends run without poll; `TestErrorTail` guards |
| `FinishTurn` `Continue` lost across the idle check | M x M | carried as `flowIdleContinue`, not a field; `TestFinishTurnContinue*` guard |
| Composed `PrepareRequest` differs from `projectContext` | L x H | four-case check in phase 4; existing agent tests; JSONL diff |
| A seam method changes a hook default (empty API key, nil ConvertToLLM) | L x M | seam bodies are moved code; `loop_stream_test.go` and JSONL diff |
| Goroutine leak from executor change | L x M | executor wraps existing `runJobs`; `goleak` in `loop_helpers_test.go` |
