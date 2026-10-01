# `internal/bootstrap`

Prompt context files. The loader finds `AGENTS.md` in the session cwd and in each ancestor folder, and returns the files in a fixed order. Seeding of files for a new agent or user is parked.

## What belongs here

- `AGENTS.md` discovery from the cwd up to the project root and the user folder (`discover*.go`)
- Loading and truncation of long context files (`load*.go`, `truncate.go`)
- Embedded templates in `templates/`

## What does not belong here

| Code | Put it in |
|---|---|
| Final prompt assembly | `internal/agent` (`systemprompt*.go`) |
| Seeding for a new agent or user | Parked |
| Project trust check | `internal/workspace` |

## Main interfaces

- None required

## File names

`discover*.go`, `load*.go`, `truncate.go`; templates in `templates/<NAME>.md`

## Imports

- Allowed: `workspace`, `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
