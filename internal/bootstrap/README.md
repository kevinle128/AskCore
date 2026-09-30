# `internal/bootstrap`

Prompt context files: embedded default templates, seeding for a new agent or user, loading and truncation.

## What belongs here

- Load and seed logic (`files.go`, `seed*.go`, `load*.go`)
- Truncation of long context files
- Embedded templates in `templates/`

## What does not belong here

| Code | Put it in |
|---|---|
| Final prompt assembly | `internal/agent` (`systemprompt*.go`) |

## Main interfaces

- None required

## File names

`seed*.go`, `load*.go`, `truncate.go`; templates in `templates/<NAME>.md`

## Imports

- Allowed: `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
