# `internal/hooks/handlers`

Implementations of `hooks.Handler`.

## What belongs here

- `CommandHandler`, `HTTPHandler` and later handler kinds

## What does not belong here

| Code | Put it in |
|---|---|
| The dispatcher | `internal/hooks` |

## Main interfaces

- Implements `hooks.Handler`

## File names

`<kind>.go` (for example `command.go`, `http.go`)

## Imports

- Allowed: `hooks`, `crypto`, `sandbox`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../../docs/ask-architecture-reference.md)
