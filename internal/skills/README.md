# `internal/skills`

Skill loader (skill folders with SKILL.md), search (BM25 and optional embeddings) and hot reload.

## What belongs here

- Loader and folder hierarchy
- Search index
- File watcher for hot reload

## What does not belong here

| Code | Put it in |
|---|---|
| The `skill_search` / `use_skill` tools | `internal/tools` |
| Skill metadata at rest | `internal/store` |

## Main interfaces

- `SkillEmbedder` (dewee `internal/skills/search.go:36`)

## File names

`loader*.go`, `search*.go`, `watcher.go`

## Imports

- Allowed: `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
