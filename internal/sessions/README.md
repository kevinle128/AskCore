# `internal/sessions`

The session log. A session is a tree of typed entries. Each entry has an `id` and a `parentId`. A fork starts a new branch at any entry. The model context is a projection of the log along one branch. It is not stored as its own copy.

## What belongs here

- The entry types and the entry tree: append, fork, branch and leaf pointer (`entry.go`, `tree.go`)
- The context builder. It projects the path from a leaf to the root into the message list for the model, and it applies compaction entries (`context.go`)
- The session manager: create, open, resume and list (`manager.go`)
- The session id and the mapping from a channel key to a session id, when a channel needs it (`key.go`)
- `MemoryLog`, the in-memory message log of a run without a session file (`memory.go`). It satisfies `agent.ContextSource` by shape; this package never imports `agent`

## What does not belong here

| Code | Put it in |
|---|---|
| Session and entry rows at rest | `internal/store` (tables `session` and `session_entry`) |
| The loop and its queues | `internal/agent` |
| Compaction decisions and summaries | `internal/pipeline` |

## Main interfaces

- None required

## File names

`entry.go`, `tree.go`, `context.go`, `manager.go`, `key.go`, `memory.go`

## Imports

- Allowed: `store`, `pkg/protocol` (message, content and event types)
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- The log is append-only. A fork adds entries. It never rewrites an old entry.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
