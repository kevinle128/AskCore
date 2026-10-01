# `internal/crypto`

Parked for credentials. Ask keeps credentials in `~/.ask/auth.json` (see `internal/settings`), as Pi does, so nothing needs encryption before it goes to the database. The AES-256-GCM helpers stay for a later feature that needs encryption at rest (for example cloud mode with per-tenant secrets).

## What belongs here

- AES-256-GCM helpers, key loading (when a feature needs them)

## What does not belong here

| Code | Put it in |
|---|---|
| Credential storage | `internal/settings` |
| Secret storage in the database | `internal/store` |

## Main interfaces

- None required

## File names

`aes.go`, `key.go`

## Imports

- Allowed: standard library
- Denied: all `AskCore/internal/*` packages

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
