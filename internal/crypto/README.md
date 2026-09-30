# `internal/crypto`

Encryption and decryption of API keys and other secrets before they go to the database.

## What belongs here

- AES-256-GCM helpers, key loading

## What does not belong here

| Code | Put it in |
|---|---|
| Secret storage | `internal/store` |

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
