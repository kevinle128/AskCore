# `internal/`

Ask uses the dewee package model: one package for each capability. Each package has a `README.md` that says what belongs there, the file name convention and the allowed imports. Read it before you add a file.

Full design: [docs/ask-architecture-reference.md](../docs/ask-architecture-reference.md)

## Package map

| Group | Packages |
|---|---|
| Composition root | `app` (fx), `config`, `logs` |
| Transport | `gateway` (+ `methods`), `http`, `channels` |
| Runtime core | `bus`, `scheduler`, `agent`, `pipeline`, `sessions`, `workspace`, `cron` |
| Capabilities | `providers` (+ `acp`), `tools`, `mcp`, `skills`, `memory`, `bootstrap`, `hooks` (+ `handlers`), `permissions`, `sandbox`, `crypto`, `tracing` (+ `otelexport`) |
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
| `providers` does not import `tools` | `tools` may import `providers` |
| `store` does not import `store/gormstore` | Interfaces never depend on their implementation |
| Handlers call store interfaces only | `http`, `gateway`, `gateway/methods` must not import `store/gormstore`, `gorm.io`, `database/sql` |
| One composition root | Only `app` and `cmd/server` import `store/gormstore` |
| Config through constructors | Only `app` and `cmd/*` import `config` |
| The TUI is a client | `cmd/tui` imports no `internal/*` package |

## Where to put new code

| You are adding | Put it in |
|---|---|
| A builtin tool | `tools/<family>_*.go` |
| A tool backend by vendor | `tools/<tool>_<vendor>.go` |
| An LLM vendor | `providers/<vendor>*.go` |
| A chat platform | `channels/<vendor>/` |
| A pipeline stage | `pipeline/<name>_stage.go` |
| A persisted entity | `store/<entity>_store.go` + `store/gormstore/<entity>.go` + SQL in `migrations/` |
| A REST endpoint | `http/<resource>.go` |
| A WS RPC method | `pkg/protocol` + `gateway/methods/<resource>.go` |
| A gRPC service | `proto/` + `gateway/grpc_<service>.go` |
| A hook handler | `hooks/handlers/<kind>.go` |
| An infrastructure client | a new package under `internal/`, wired in `app` |
