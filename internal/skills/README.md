# `internal/skills`

Skill discovery. The loader finds skill folders with a `SKILL.md` file and returns their metadata (name and description). Only metadata goes into the system prompt. The full skill text is read when the agent needs it. The skill list changes only on an explicit `/reload`.

## What belongs here

- Loader and folder hierarchy (user and project skill folders)
- Metadata parsing of `SKILL.md`
- An explicit `Reload` call that rescans the folders

## What does not belong here

| Code | Put it in |
|---|---|
| File watcher for hot reload | Not built. Reload is explicit |
| BM25 and embedding search | Parked |
| The tools that read a skill | `internal/tools` |
| Project trust check | `internal/workspace` |

## Main interfaces

- None required

## File names

`loader*.go`

## Imports

- Allowed: `workspace`, `tracing`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
