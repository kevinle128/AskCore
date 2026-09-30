# `internal/memory`

Agent memory: admission of new facts, recall queries (keyword and vector), auto-injection into the prompt and flush at the end of a run.

## What belongs here

- Recall and admission logic
- Embedding provider interface
- Auto-injector

## What does not belong here

| Code | Put it in |
|---|---|
| `memory_search` / `memory_get` tools | `internal/tools` |
| Memory rows at rest | `internal/store` |

## Main interfaces

- `AutoInjector` (dewee `internal/memory/auto_injector.go:10`)
- `EmbeddingProvider` (dewee `internal/memory/embeddings.go:130`)

## File names

`recall*.go`, `admission.go`, `embeddings.go`, `auto_injector*.go`

## Imports

- Allowed: `store`, `providers` (embeddings)
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
