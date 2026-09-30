# `internal/hooks`

Hook dispatcher: sync hooks can block or change input; async hooks run in a worker pool. Timeouts are fail-closed.

## What belongs here

- `Dispatcher`, `Handler`, event types, matchers, audit

## What does not belong here

| Code | Put it in |
|---|---|
| Concrete command and HTTP handlers | `internal/hooks/handlers` |
| Hook config at rest | `internal/store` |

## Main interfaces

- `Dispatcher`, `Handler` (dewee `internal/hooks/dispatcher.go:23-31`)

## File names

`dispatcher.go`, `types.go`, `matcher.go`, `audit.go`

## Imports

- Allowed: `store`, `tracing`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
