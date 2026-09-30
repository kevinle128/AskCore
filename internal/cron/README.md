# `internal/cron`

Cron expressions, due-job detection and hand-off of due runs to the scheduler `cron` lane.

## What belongs here

- Schedule parsing and ticker

## What does not belong here

| Code | Put it in |
|---|---|
| Cron job rows at rest | `internal/store` |
| The `cron` tool | `internal/tools` |

## Main interfaces

- None required

## File names

`schedule.go`, `ticker.go`

## Imports

- Allowed: `store`, `scheduler`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
