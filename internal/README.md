# `internal/`

Ask uses the dewee package model: one package for each capability. Each package has a `README.md` that says what belongs there, the file name convention and the allowed imports. Read it before you add a file.

Full design: [docs/ask-architecture-reference.md](../docs/ask-architecture-reference.md)

## Package map

| Group | Packages |
|---|---|
| Composition root | `app` (fx), `config`, `logs` |
| Files under `~/.ask` | `settings` (`auth.json`, `settings.json`) |
| Transport | `gateway` (+ `methods`), `http`, `channels` |
| Runtime core | `agent` (two-level loop, queues), `pipeline` (typed control points), `sessions` (typed in-memory log; persistent tree planned), `scheduler` (planned lanes), `bus` (replay ring and followers), `workspace` (cwd, project root, trust), `cron` |
| Process model | `leader` (local router and client), `acp` (ACP adapter over the agent) |
| Capabilities | [auth](auth/README.md) (native credentials), `providers` (+ `acp`: subprocess agents), `tools`, `mcp`, `skills`, `bootstrap`, `hooks` (+ `handlers`), `permissions` (gateway RBAC), `sandbox`, `tracing` (+ `otelexport`). Parked: `memory`, `crypto` |
| Storage | `store` (models + interfaces), `store/gormstore` (GORM implementation), `migrations` (runner) |
| Infrastructure clients | `cache` (Redis), `messaging` (asynq), `realtime` (centrifuge), `validation` |
| Tests | `testsupport` |

Wire contract: `pkg/protocol` (WS) and `proto/` (gRPC, generated code only).

## Import rules

`depguard` in `.golangci.yml` enforces these rules.

| Rule | Detail |
|---|---|
| Core does not import `agent` | `tools`, `pipeline`, `providers`, `store` must not import `agent` |
| Core does not import transport | `agent`, `pipeline`, `tools`, `providers`, `store` must not import `gateway`, `http`, `channels/<vendor>` |
| Core does not import adapters | `agent`, `pipeline`, `tools`, `providers`, `store`, `sessions`, `hooks`, `bus` must not import `acp`, `leader` |
| `providers` does not import `tools` | `tools` may import `providers` |
| Providers receive resolved credentials | Providers must not import auth or settings |
| Settings uses standard library only | Settings must not import other internal packages or dependencies |
| `store` does not import `store/gormstore` | Interfaces never depend on their implementation |
| Handlers call store interfaces only | `http`, `gateway`, `gateway/methods` must not import `store/gormstore`, `gorm.io`, `database/sql` |
| One composition root | Only `app` and `cmd/server` import `store/gormstore` |
| Config through constructors | Only `app` and `cmd/*` import `config`. Runtime settings and credentials are files that `settings` owns |

## Where to put new code

| You are adding | Put it in |
|---|---|
| A builtin tool | `tools/<family>_*.go` |
| A tool backend by vendor | `tools/<tool>_<vendor>.go` |
| An LLM vendor | `providers/<vendor>*.go` |
| A chat platform | `channels/<vendor>/` |
| A typed control handler | `pipeline/<name>.go`; register it with `pipeline.Registry` |
| A session entry type | `sessions/entry.go` |
| An event type | `pkg/protocol`; agent publishes lifecycle events and bus owns replay/follow |
| A user setting or the credentials file | `settings/` |
| Native login, refresh, or account access | [auth/](auth/README.md), composed in [app](app/README.md) |
| An ACP method or `session/update` mapping | `acp/` (types in `pkg/protocol`) |
| Leader socket, lock or spawn code | `leader/` |
| Project trust or cwd rules | `workspace/` |
| An `AGENTS.md` discovery rule | `bootstrap/` |
| A persisted entity (database) | `store/<entity>_store.go` + `store/gormstore/<entity>.go` + SQL in `migrations/` |
| A REST endpoint | `http/<resource>.go` |
| A WS RPC method | `pkg/protocol` + `gateway/methods/<resource>.go` |
| A gRPC service | `proto/` + `gateway/grpc_<service>.go` |
| A hook handler | `hooks/handlers/<kind>.go` |
| An infrastructure client | a new package under `internal/`, wired in `app` |
