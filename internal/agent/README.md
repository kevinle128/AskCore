# `internal/agent`

The agent runtime. `Loop` runs one agent turn by turn through the pipeline. `Router` resolves an agent by key and caches it. The prompt builder assembles the system prompt from the agent config, skills, memory and bootstrap files.

## What belongs here

- `Loop` and its run logic (`loop_*.go`)
- `Router` and the agent resolver (`router.go`, `resolver.go`)
- System prompt assembly (`systemprompt*.go`)
- The `Agent` interface and run request/result types (`types.go`)
- The adapter that turns loop state into `pipeline` dependencies (`loop_pipeline_adapter.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Pipeline stages | `internal/pipeline` |
| Tool implementations | `internal/tools` |
| LLM vendor code | `internal/providers` |
| Queues and lanes | `internal/scheduler` |
| HTTP/WS handlers | `internal/http`, `internal/gateway` |

## Main interfaces

- `Agent` (dewee `internal/agent/types.go:14`)
- `Router` + `ResolverFunc` (dewee `internal/agent/router.go:18-58`)

## File names

`loop_<topic>.go`, `router*.go`, `resolver*.go`, `systemprompt*.go`, `types.go`

## Imports

- Allowed: `pipeline`, `providers`, `tools`, `store`, `sessions`, `skills`, `memory`, `bootstrap`, `hooks`, `tracing`, `bus`, `workspace`, `permissions`
- Denied: `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config` (receive typed config through the constructor)

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
