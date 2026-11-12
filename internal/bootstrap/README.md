# `internal/bootstrap`

Prompt context. The loader reads `AGENTS.md` and `CLAUDE.md` from the Ask home and from each folder between the git root and the session cwd, and returns one framed, escaped, size-capped reminder text. It also reads `SYSTEM.md` and `APPEND_SYSTEM.md`. Seeding of files for a new agent or user is parked.

## What belongs here

- Git root discovery and the folder walk (`discover.go`)
- Context file loading, dedup, the size caps and the reminder text (`load.go`, `read.go`, `truncate.go`)
- Containment: a project file whose link target is outside the project root (the git root, or the cwd without one) is skipped with `SkipOutsideProject` and nothing is read from it. Files of the user dir are not checked.
- `SYSTEM.md` and `APPEND_SYSTEM.md` precedence (`load_system.go`)
- Embedded templates in `templates/`

## What does not belong here

| Code | Put it in |
|---|---|
| Final prompt assembly | `internal/agent` (`systemprompt*.go`) |
| Seeding for a new agent or user | Parked |
| Project trust check | `internal/workspace`. `LoadContext` takes no trust input. For `LoadPromptFiles` the caller passes `ProjectDir` as `""` when the project is not trusted |

## Main interfaces

- `LoadContext`, `LoadPromptFiles`

## File names

`discover.go`, `load*.go`, `read.go`, `truncate.go`; templates in `templates/<NAME>.md`

## Imports

- Allowed: `workspace`, `store`, `pkg/protocol` (the `ReminderText` frame and escape). Enforced by the `bootstrap-boundary` depguard rule
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
