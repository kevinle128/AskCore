# `internal/sessions`

Session keys (`agent:{agentId}:{channel}:direct:{peerId}`) and session create, resume and reset.

## What belongs here

- Session key build and parse (`key.go`)
- Session manager (`manager.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Session rows at rest | `internal/store` |
| History pruning inside a run | `internal/agent`, `internal/pipeline` |

## Main interfaces

- None required

## File names

`key.go`, `manager.go`

## Imports

- Allowed: `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
