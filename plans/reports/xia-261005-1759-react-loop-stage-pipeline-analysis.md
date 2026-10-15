# Xia analysis: dewee ReAct loop to Ask agent loop (`--improve`)

## Source manifest

- Source: `/Users/dale/Desktop/workspace/opensources/dewee`, branch `dev`, commit `815cd9ea1790becf92288cc424ff020c806bae84`
- Scope: `internal/pipeline/*`, `internal/agent/loop_run.go`, `internal/agent/loop_pipeline_*.go`
- Local target: `internal/agent/loop_run.go`, `loop_stream.go`, `loop_tools.go`; hook contract `internal/pipeline/hooks.go`

## Source anatomy (dewee)

| Part | Evidence | What it does |
|---|---|---|
| `Stage` interface | `internal/pipeline/stage.go:20-26` | `Name()` and `Execute(ctx, *RunState) error`. Stages hold no state. |
| `StageResult` enum | `internal/pipeline/stage.go:10-16` | `Continue`, `BreakLoop`, `AbortRun`. A stage sets the flow explicitly. |
| Stage list | `internal/pipeline/pipeline.go:40-58` | Setup: Context. Each iteration: Prune, Think, Tool, Observe, Checkpoint. Finalize: Finalize. |
| Driver | `internal/pipeline/pipeline.go:61-208` | Runs setup once, iterations up to `MaxIterations`, then finalize. Checks ctx after every stage. |
| Reason | `think_stage.go` | Builds the request, streams, retries truncated tool args and context overflow, sets `BreakLoop` when there is no tool call. |
| Act | `tool_stage.go` | Parallel I/O, then sequential state mutation in index order. Authorization preflight. |
| Observe | `observe_stage.go` | Drains injected messages, collects final content. |
| State | `run_state.go`, `substates.go` | One `RunState` with a sub-state for each stage. |
| Dependencies | `deps.go` (53 func fields) | `PipelineDeps`: closures that capture the `Loop`. |

## Local anatomy (Ask)

- `loop.run()` (`loop_run.go:105-215`) is a direct port of Pi `runLoop`: two nested loops and three flags (`hasMoreToolCalls`, `explicitContinuation`, `pending`).
- Reason is `streamAssistantResponse` (`loop_stream.go:13`). Act is `executeToolCalls` (`loop_tools.go:73`). Observe is inline (`loop_run.go:171-174`).
- Extension today is through `pipeline.Hooks` (9 hook points, `internal/pipeline/hooks.go:112`). Event order is Pi's and is fixed by tests.

## Dependency matrix

| dewee part | Ask equivalent | Status |
|---|---|---|
| `Stage` + `StageResult` | none (inline code, flags) | NEW |
| Think stage | `streamAssistantResponse` | EXISTS (wrap) |
| Tool stage, parallel then ordered mutation | `batchRun.parallel/sequential` | EXISTS (already same idea) |
| Observe stage | inline append | NEW (extract) |
| Prune stage (compaction) | none, H10 | NEW later |
| Checkpoint/Finalize stages | none, H8 session log | NEW later |
| `PipelineDeps` (53 closures) | `pipeline.Hooks` (9 fields) + `LoopConfig` | CONFLICT: keep Ask's |
| One big `RunState` | `loop` struct | CONFLICT: keep small |
| `MaxIterations` guard | none (Pi has none) | Decision |
| Advisor gate | none | Skip |

## Challenge

| Decision | dewee way | Ask way | Recommendation |
|---|---|---|---|
| Loop structure | List of stages, driver loop | Nested loops and flags | Adopt stages (Pipeline + Template Method). Each ReAct phase becomes a named type. |
| Flow control | `StageResult` enum | Boolean flags | Adopt an explicit enum (state-machine style). Removes `explicitContinuation` reasoning. |
| Dependencies | 53 closures in `PipelineDeps`, coupled to `Loop` | `Hooks` + `LoopConfig` | Keep Ask's. dewee report lists callback sprawl as its main weakness. Stages get typed fields, not closures. |
| State | One `RunState` with 8 sub-states | Small `loop` | Keep one small `turnState`; add fields only when a stage needs them. |
| Extension surface | Stage list is public in `pipeline` | Hooks are public | Keep stages unexported in `agent`. Public extension stays `pipeline.Hooks`. Make the stage list public only when H11 extensions need it. |
| Event order | dewee events | Pi events, fixed by tests | Must not change. Existing tests are the guard. |
| Tool execution | Parallel I/O then ordered mutation | Same in `batchRun` | Wrap as Strategy (`toolExecutor`: sequential, parallel). No behavior change. |
| Max iterations | Default cap | No cap | Add as optional `LoopConfig.MaxTurns` (0 = no cap) only if the user wants it. |
| Finalize errors | Logged, swallowed | Returned | Keep Ask's. |

Risk if wrong: the main risk is a silent change of Pi event order or steering poll points. Mitigation: no test edits allowed except new ones; compare JSONL event streams of `ask -p --mode json` before and after.

Risk score: medium (core loop, but pure refactor with 1,100+ lines of tests).

## Unresolved questions

1. Refactor depth (see question to user).
2. Add `MaxTurns` guard now or defer to H9.
