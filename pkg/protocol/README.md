# `pkg/protocol`

Shared with clients such as `cmd/tui`. Frames are `req`, `res` and `event`; the first request is `connect`.

## What belongs here

- Frame types, method name constants, event names, error codes

## What does not belong here

| Code | Put it in |
|---|---|
| gRPC definitions | `proto/` |
| Handlers | `internal/gateway/methods` |

## Main interfaces

- None required

## File names

`frames.go`, `methods.go`, `events.go`, `errors.go`

## Imports

- Allowed: standard library
- Denied: all `AskCore/internal/*` packages

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
