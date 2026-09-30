# `internal/cache`

Redis client.

## What belongs here

- Redis client constructor

## What does not belong here

| Code | Put it in |
|---|---|
| Business caching logic | the capability package |

## File names

`goredis.go`

## Imports

- Allowed: go-redis
- Denied: other `AskCore/internal/*` packages

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
