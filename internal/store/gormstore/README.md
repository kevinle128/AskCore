# `internal/store/gormstore`

The only package that talks to the database. It opens the connection and implements every interface of `internal/store`.

## What belongs here

- DB connection constructor (`db.go`)
- One file for each entity (`<entity>.go`)
- A separate storage type + mapper only when the storage shape differs from the model (design section 6)

## What does not belong here

| Code | Put it in |
|---|---|
| Models and interfaces | `internal/store` |
| Schema changes | `migrations/` (no `AutoMigrate`) |

## Main interfaces

- Implements the `internal/store` interfaces

## File names

`db.go`, `<entity>.go`

## Imports

- Allowed: `store`, `gorm.io/*`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`. Only `internal/app` and `cmd/server` may import this package.

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../../docs/ask-architecture-reference.md)
