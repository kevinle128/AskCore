# `internal/memory`

Parked. Ask has no long-term memory now. Pi has none, and auto-injection into the prompt would break the byte-stable prompt prefix that the provider caches. The package holds only its `doc.go` until a feature needs it.

## What belongs here

- Nothing now. If memory comes back, it injects only as a named section outside the cached prefix

## What does not belong here

| Code | Put it in |
|---|---|
| `memory_search` / `memory_get` tools | `internal/tools` (when memory comes back) |
| Memory rows at rest | `internal/store` |

## Main interfaces

- None required

## File names

None now

## Imports

- Allowed: `store`, `providers` (embeddings), when the package is used
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
