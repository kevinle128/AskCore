# `internal/agent`

The agent runtime. `Loop` runs Pi's two-level loop: the outer loop takes the next follow-up message when the agent is idle, and the inner loop runs model calls and tool calls until no tool call and no steer message is left. One agent serves one session. The prompt builder assembles the system prompt from the context files, the skills metadata and the tool list.

## What belongs here

- `Loop` and its run logic (`loop_*.go`): the two loops, tool batches, retry, compaction trigger and abort
- The steer and follow-up queues of one session (`queue.go`). Steer messages are delivered after the current tool batch. Follow-up messages are delivered when the agent is idle. Each queue has the mode `all` or `one-at-a-time`
- System prompt assembly (`systemprompt*.go`)
- The `Agent` interface, the typed Go API, and the run request and result types (`types.go`). Headless mode and the ACP adapter both call this API
- The adapter that turns loop state into `pipeline` dependencies (`loop_pipeline_adapter.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Hook points of one turn | `internal/pipeline` |
| Tool implementations | `internal/tools` |
| LLM vendor code | `internal/providers` |
| Session log and context projection | `internal/sessions` |
| Lanes and concurrency limits | `internal/scheduler` |
| ACP mapping and the leader socket | `internal/acp`, `internal/leader` |
| HTTP/WS handlers | `internal/http`, `internal/gateway` |

## Main interfaces

- `Agent` and the run request/result types (`types.go`)
- Hook points are declared in `pipeline`. The loop calls them in order

## File names

`loop_<topic>.go`, `queue*.go`, `systemprompt*.go`, `types.go`, `loop_pipeline_adapter.go`

## Imports

- Allowed: `pipeline`, `providers`, `tools`, `store`, `sessions`, `skills`, `bootstrap`, `hooks`, `settings`, `tracing`, `bus`, `workspace`
- Denied: `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config` (receive typed config through the constructor)

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- The queues belong to the loop. There is no `interrupt` mode. Abort is a separate call on the `Agent`.
- One agent serves one session. A `Router` is not needed until multi-agent routing is.
- The loop is the only place that calls the hook points of `pipeline`.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
