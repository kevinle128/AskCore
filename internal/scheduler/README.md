# `internal/scheduler`

Lanes (`main`, `subagent`, `team`, `cron`) with bounded concurrency. The scheduler decides when a run may start. It does not decide what a run does.

## What belongs here

- `Scheduler`, lanes and concurrency limits
- The start queue for runs that wait for a free lane

## What does not belong here

| Code | Put it in |
|---|---|
| The run itself | `internal/agent` |
| Steer and follow-up queues of one session | `internal/agent` (`queue.go`) |
| Abort of a running turn | `internal/agent` (a separate call on the `Agent`) |
| Durable background jobs | `internal/messaging` (asynq) |

## Main interfaces

- `RunFunc` callback (dewee `internal/scheduler/scheduler.go:32`)

## File names

`scheduler.go`, `lanes.go`, `queue.go`

## Imports

- Allowed: standard library, `tracing`
- Denied: `internal/agent` (receive a run function instead); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- There is no `interrupt` mode. To stop a run, the caller aborts it through the `Agent`.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
