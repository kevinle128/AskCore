# `internal/hooks/handlers`

Implementations of `hooks.Handler`.

## What belongs here

- `CommandHandler`, `HTTPHandler`, the handler for external extensions (over the extension-host link) and later handler kinds

## What does not belong here

| Code | Put it in |
|---|---|
| The dispatcher | `internal/hooks` |

## Main interfaces

- Implements `hooks.Handler`

## File names

`<kind>.go` (for example `command.go`, `http.go`)

## Imports

- Allowed: `hooks`, `pkg/protocol`, `sandbox`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- An out-of-process handler sets its own deadline. See `internal/hooks` for what happens when it expires.

Design reference: [docs/ask-architecture-reference.md](../../../docs/ask-architecture-reference.md)
