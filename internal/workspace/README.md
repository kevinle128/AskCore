# `internal/workspace`

Workspace resolver for each run kind: agent default, subagent, cron.

## What belongs here

- Resolver and workspace context

## What does not belong here

| Code | Put it in |
|---|---|
| File tools | `internal/tools` |

## Main interfaces

- Resolver interface

## File names

`resolver.go`, `workspace_context.go`

## Imports

- Allowed: `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
