# `internal/pipeline`

The stage-based run pipeline: context → history → prompt → think → act → observe → memory → summarize. Stages are stateless; all mutable state is in `RunState`.

## What belongs here

- `Pipeline` and `NewDefaultPipeline` (`pipeline.go`)
- `RunState` (`run_state.go`)
- `Stage` and `StageWithResult` interfaces (`stage.go`)
- One file for each stage (`<name>_stage.go`)
- `PipelineDeps`, the dependency bundle that stages receive (`deps.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| The agent loop and router | `internal/agent` |
| LLM calls | `internal/providers` (stages receive a provider) |
| Tool execution code | `internal/tools` |

## Main interfaces

- `Stage`, `StageWithResult` (dewee `internal/pipeline/stage.go:20-33`)

## File names

`<name>_stage.go` for each stage, `pipeline.go`, `run_state.go`, `deps.go`

## Imports

- Allowed: `providers`, `tools`, `store`, `hooks`, `tracing`, `bootstrap`, `workspace`
- Denied: `internal/agent` (the agent adapts itself into `PipelineDeps`); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
