# `internal/permissions`

Role-based access decisions for gateway methods and HTTP endpoints. It does not decide whether a tool may run, and it does not hold the project trust store.

## What belongs here

- Roles, policy and decision functions

## What does not belong here

| Code | Put it in |
|---|---|
| Authentication | `internal/gateway` |
| Tool allow/deny policy | No built-in policy. A user extension handles `tool_call` |
| Project trust store | `internal/workspace` |

## Main interfaces

- None required

## File names

`policy.go`, `<scope>_scope.go`

## Imports

- Allowed: `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse)

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
