# `internal/hooks`

Event dispatch for the lifecycle of a turn. A sync hook can block or change its input. A notify hook only observes, and it runs in a worker pool. Only `tool_call` and `user_bash` fail closed. All other events fail open: an error is logged and the run continues.

## What belongs here

- `Dispatcher`, `Handler`, matchers and audit
- The split between sync events and notify events
- The deadline for out-of-process handlers. A compiled-in Go handler has no deadline. When the deadline of an out-of-process handler expires, `tool_call` and `user_bash` fail closed and the dispatcher only logs the other events

## What does not belong here

| Code | Put it in |
|---|---|
| Concrete command and HTTP handlers | `internal/hooks/handlers` |
| Event and content types | `pkg/protocol` |
| Hook config at rest | `internal/store` or `internal/settings` |

## Main interfaces

- `Dispatcher`, `Handler` (dewee `internal/hooks/dispatcher.go:23-31`)

## File names

`dispatcher.go`, `types.go`, `matcher.go`, `audit.go`

## Imports

- Allowed: `pkg/protocol` (event and content types), `store`, `tracing`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
