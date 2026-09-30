# `internal/migrations`

Runs the SQL migrations with golang-migrate. The SQL files are in `migrations/` at the repository root and are embedded into the binary by `migrations/embed.go`, so the server can run from any working directory. This is the only way the schema changes (no GORM `AutoMigrate`).

The server applies pending migrations at start. `server -migrate` applies them and exits.

## What belongs here

- Migration runner

## What does not belong here

| Code | Put it in |
|---|---|
| SQL files | `migrations/` at the repository root |

## File names

`migrations.go`

## Imports

- Allowed: golang-migrate
- Denied: other `AskCore/internal/*` packages

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
