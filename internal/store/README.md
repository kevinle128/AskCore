# `internal/store`

One file for each entity: the model (with `json` and `gorm` tags) and its store interface. `Stores` collects all store interfaces. The rest of the code depends on these interfaces, never on `gormstore`.

## What belongs here

- Models with `json`/`gorm` tags (they may use GORM types such as `gorm.DeletedAt`)
- Store interfaces, small ones composed into large ones
- The session store: the `session` and `session_entry` models and their interfaces (`session_store.go`, added with the session tree)
- `Stores` aggregate (`stores.go`)
- Store errors such as `ErrNotFound` (`errors.go`)
- Request-scope helpers on `context.Context`

## What does not belong here

| Code | Put it in |
|---|---|
| GORM queries and the DB connection | `internal/store/gormstore` |
| SQL schema | `migrations/` |
| Credentials and settings | `internal/settings` (files, not database rows) |

## Main interfaces

- One interface for each entity; composition example: dewee `AgentStore` (`internal/store/agent_store.go:912-973`)

## File names

`<entity>_store.go` (model + interface), `stores.go`, `errors.go`. Existing files `user_store.go` and `post_store.go` are the reference example.

## Imports

- Allowed: standard library, `gorm.io/gorm` (types and tags only)
- Denied: `internal/store/gormstore`, `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
