# `internal/http`

REST `/v1/*` handlers (and the demo `/api/users`, `/api/posts`). A handler receives store interfaces or core services through its constructor.

## What belongs here

- One file for each resource (`<resource>.go`). Each handler has `Register(e *echo.Echo)` (it implements `gateway.RouteRegistrar`). Examples: `users.go`, `posts.go`, `health.go`
- Shared helpers: `errors.go` (`errorJSON`, `storeError` maps `store.ErrNotFound` to 404), `params.go`
- Request structs, and response DTOs only when the data is really different (design section 6)

## What does not belong here

| Code | Put it in |
|---|---|
| Server setup and middleware | `internal/gateway` |
| Database access | `internal/store` interfaces only |

## Main interfaces

- None required

## File names

`<resource>.go` (for example `users.go`, `health.go`)

## Imports

- Allowed: `store`, `agent`, `sessions`, `permissions`, echo
- Denied: `internal/store/gormstore`, `gorm.io`, `database/sql`, `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
