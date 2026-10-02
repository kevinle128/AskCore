# `pkg/protocol`

Shared with clients such as `cmd/tui`. Frames are `req`, `res` and `event`; the first request is `connect`.

## What belongs here

- Frame types, method name constants, event names, error codes
- Message, content block, usage and tool declaration types (`message.go`, `content.go`, `usage.go`, `tool.go`)
- Provider stream events (`stream_events.go`), agent events and the event envelope (`events.go`)
- The message builder that rebuilds a message from stream events (`builder.go`), and the JSON and JSONL codec (`codec.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| gRPC definitions | `proto/` |
| Handlers | `internal/gateway/methods` |

## Main interfaces

- None required

## File names

`frames.go`, `methods.go`, `events.go`, `errors.go`, `message.go`, `content.go`, `usage.go`, `tool.go`, `stream_events.go`, `builder.go`, `codec.go`

## Imports

- Allowed: standard library
- Denied: all `AskCore/internal/*` packages

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
