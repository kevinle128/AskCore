# Phase 6: README file list and design pattern map

## File
`internal/agent/README.md` only. The pipeline README is owned by phase 4.

## Changes
1. "What belongs here": change the loop bullet to name `loop_run.go` (Run, Continue, LoopConfig, driver, idle check) and add `loop_stage.go` (the `stage` interface, the `flow` enum, `turnState`, the six ReAct stages). Update the tool bullet to mention the `toolExecutor` strategy (truncated, sequential, parallel).
2. "File names": `loop_<topic>.go` already covers `loop_stage.go`; add no new prefix. If later stages get their own files, name them `loop_stage_<name>.go`; say so in one line.
3. New section "Design pattern map" (table):

| Pattern | Where | Why |
|---|---|---|
| Pipeline / Template Method | `turnStages` and `runTurn` | one turn is a fixed sequence of named steps |
| State machine | `flow` enum, driver `switch` | the next step is a value, not a combination of flags |
| Strategy | `toolExecutor` | the batch run mode changes per assistant message |
| Hook / Observer (port) | `pipeline.Hooks`, `Emit` | the only public extension surface; stages call hooks, never the reverse |
| Single seam | one loop method per hook point (`pollSteering`, `pollFollowUps`, `prepareRequest`, `transformContext`, `convertToLLM`, `apiKey`, `finishTurn`, `beforeToolCall`, `afterToolCall`) | a hook is read in one place, so tracing and replacing it is one hop |
| Composite (chain of responsibility) | `pipeline.Compose` (in `internal/pipeline`), used in `Agent.execute` | several hook sets act as one `Hooks`; merge rules live in the pipeline README |
| Adapter (H11) | a file in `internal/pipeline` that wraps `hooks.Dispatcher` | extension events become `Hooks` fields; loop and stages do not change |

4. "How to add a stage" (short how-to): write an unexported type with `name()` and `run(*loop) (flow, error)`; put typed per-turn data in `turnState`, never a closure; insert it in `turnStages`; update `TestTurnStagesOrder`; prove event order with the JSONL diff. Example: a compaction stage goes between `prepare` and `reason`, returns `flowNext`, and may emit its own events only if Pi does.
5. Rules: add "Stages are unexported. The public extension surface is `pipeline.Hooks`." and "Only the seam method of a hook point reads `cfg.Hooks.<Field>`. Combine hook sets with `pipeline.Compose`, not with a hand-written wrapper."
6. Three-layer note (one short paragraph, link to the pipeline README): port `pipeline.Hooks`, composite `pipeline.Compose`, adapter for the H11 Dispatcher.

## Validation
Every file and symbol named in the README exists (`grep -n` each). Links resolve.

## Rollback
Revert the README commit (`docs(agent): describe the stage pipeline and pattern map`).
