# `internal/realtime`

centrifuge node for realtime push to clients.

## What belongs here

- centrifuge node setup

## What does not belong here

| Code | Put it in |
|---|---|
| In-process events | `internal/bus` |

## File names

`centrifuge.go`

## Imports

- Allowed: centrifuge
- Denied: other `AskCore/internal/*` packages

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
