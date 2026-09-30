# `internal/gateway/methods`

One file for each resource. A handler validates input, checks permissions and calls core packages or store interfaces.

## What belongs here

- WS method handlers

## What does not belong here

| Code | Put it in |
|---|---|
| Method names and frames | `pkg/protocol` |
| Database access | `internal/store` interfaces only |

## Main interfaces

- None required

## File names

`<resource>.go`

## Imports

- Allowed: `agent`, `store`, `bus`, `sessions`, `permissions`, `pkg/protocol`
- Denied: `internal/store/gormstore`, `gorm.io`, `database/sql`, `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../../docs/ask-architecture-reference.md)
