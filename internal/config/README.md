# `internal/config`

Configuration: viper loads `config.yaml` (optional) and environment variables. The environment variable names are the upper-case keys, the same as in `.env.example` (`HOST`, `PORT`, `GRPC_PORT`, `DATABASE_URL`, `LOG_LEVEL`, `CORS_ORIGIN`, `REDIS_URL`, `REDIS_ADDR`). A `daemon`/`cloud` mode setting is added when the first feature needs it.

## What belongs here

- `Config` struct, loading, defaults

## What does not belong here

| Code | Put it in |
|---|---|
| Secrets in files | environment variables only |

## File names

`config.go`

## Imports

- Allowed: viper
- Denied: all other `AskCore/internal/*` packages. Only `internal/app` and `cmd/*` may import this package.

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
