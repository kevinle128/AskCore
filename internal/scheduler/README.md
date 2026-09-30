# `internal/scheduler`

Lanes (`main`, `subagent`, `team`, `cron`) with bounded concurrency, and a per-session queue with the modes `queue`, `followup` and `interrupt`.

## What belongs here

- `Scheduler`, lanes, per-session queue, queue modes

## What does not belong here

| Code | Put it in |
|---|---|
| The run itself | `internal/agent` |
| Durable background jobs | `internal/messaging` (asynq) |

## Main interfaces

- `RunFunc` callback (dewee `internal/scheduler/scheduler.go:32`)

## File names

`scheduler.go`, `lanes.go`, `queue.go`

## Imports

- Allowed: standard library, `tracing`
- Denied: `internal/agent` (receive a run function instead); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
