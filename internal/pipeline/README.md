# `internal/pipeline`

The ordered hook points of one turn. The hook points are `transformContext`, `prepareRequest`, `beforeToolCall`, `afterToolCall` and `finishTurn`. `internal/agent` runs the loop and calls each hook point at its place. `pipeline` decides what runs at each hook point and in which order. Summarizing old context is compaction, and it runs at a hook point.

## What belongs here

- The ordered hook point list and the code that runs the registered steps of one hook point (`pipeline.go`)
- `TurnState`, the data that the steps of one turn share (`turn_state.go`)
- The `Step` interface for one hook-point step (`step.go`)
- One file for each step (`<name>_step.go`), for example context transform, request preparation and compaction
- `PipelineDeps`, the dependency bundle that steps receive (`deps.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| The two-level loop, queues and abort | `internal/agent` |
| LLM calls | `internal/providers` (steps receive a provider) |
| Tool execution code | `internal/tools` |
| Event dispatch to extensions | `internal/hooks` |

## Main interfaces

- `Step` (`step.go`)

## File names

`pipeline.go`, `turn_state.go`, `step.go`, `deps.go`, `<name>_step.go`

## Imports

- Allowed: `providers`, `tools`, `sessions`, `store`, `hooks`, `tracing`, `bootstrap`, `workspace`
- Denied: `internal/agent` (the agent adapts itself into `PipelineDeps`); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- A step is stateless. All mutable state of a turn is in `TurnState`.
- `pipeline` does not run the loop. It does not own the order of model calls and tool calls.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
