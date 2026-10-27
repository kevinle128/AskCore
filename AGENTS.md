# AskCore

This file provides context about the project for AI assistants. Claude Code reads it through `CLAUDE.md`, which only imports this file.

## Project Overview

- **Ecosystem**: Go

## Tech Stack

Version authority is `go.mod`.

- Web Framework: echo
- Database: gorm
- API: grpc-go
- CLI: bubbletea
- Logging: zap
- Testing: gomock, testcontainers, testify, goleak, go-vcr (LLM cassettes)
- Messaging: asynq
- Observability: opentelemetry
- Validation: validator
- Code Quality: golangci-lint
- Migrations: golang-migrate
- Protobuf Tooling: buf
- Dependency Injection: fx
- ACP: `github.com/coder/acp-go-sdk` v0.13.5 (stable schema 0.13.5, wire v1); conformance evidence in `plans/reports/conformance-261007-h13a-sdk.md`
- Native auth identity: `github.com/coreos/go-oidc/v3`; installed version authority is `go.mod`
- Model wire layer: `charm.land/fantasy` (see `internal/providers/fantasykit`)
- Local patched copies: `third_party/acp-go-sdk` and `third_party/bubbletea` (see the `replace` lines in `go.mod`)

Some stack items are not composed in the running server yet (asynq, opentelemetry, validator, Redis cache, centrifuge). The package exists; `internal/app` does not wire it.

## Architecture

Ask (this repository) is an agent harness that runs as a local daemon or as a remote agent in the cloud. It follows the dewee package model (one package for each capability) plus the Ask import rules. Read these before you add code:

- `docs/ask-architecture-reference.md`: design, import rules, "where to put new code"
- `docs/async-task-runtime.md`: background work and its notices (task registry, `Agent.Notify`, no sleep or polling)
- `internal/README.md`: package map
- `README.md` in each package: what belongs there, file names, allowed imports. A few packages with Go code have no README yet (for example `internal/tuiclient` and some `internal/providers/*` adapters).

The users/posts demo is the reference example for a new entity (see design section 11). Front-end and web UI for chat and control are out of scope. One exception is planned: a read-only local monitoring dashboard ("Watch it think"). The daemon would serve it as embedded static files, bound to localhost with a token, and it would only read the harness event stream. It is not built yet. The WebSocket endpoint and the daemon as an ACP client of the leader are also design targets, not code.

## Project Structure

```
AskCore/
├── cmd/
│   ├── server/            # Daemon: HTTP + gRPC today; `-migrate` applies migrations and exits
│   └── tui/               # `ask` binary: TUI, headless `ask -p`, `ask auth`, `ask acp`, `ask leader`, `ask connect`, `ask version`. See docs section 7.3
├── internal/              # Package map: internal/README.md (the owner)
│   ├── app/               # Composition root (fx)
│   ├── config/  logs/     # Config (viper) and zap logger
│   ├── gateway/           # HTTP (echo) and gRPC servers, gRPC services; methods/ for WS RPC (not built)
│   ├── http/              # REST handlers (import alias httpapi)
│   ├── agent/ pipeline/ sessions/ bus/ workspace/    # Runtime core (implemented; workspace in part)
│   ├── tasks/             # Per-Agent async task registry; completions wake the agent by push (docs/async-task-runtime.md)
│   ├── scheduler/ cron/                              # Runtime core (scaffold)
│   ├── auth/ settings/ providers/ tools/ sandbox/ jobs/   # Capabilities (implemented); sandbox = process runner, jobs = per-session job table
│   ├── acp/ leader/       # Process model: ACP adapter, local router
│   ├── tui/ tuiclient/ tuiext/   # Terminal UI, its client implementation, extension host (tuiext is a scaffold)
│   ├── mcp/ skills/ memory/ bootstrap/ hooks/ permissions/ crypto/ channels/ tracing/   # Scaffold or parked
│   ├── store/             # Models + store interfaces; gormstore/ = GORM implementation
│   ├── migrations/        # Migration runner
│   └── cache/ messaging/ realtime/ validation/ testsupport/
├── migrations/            # SQL files (SQLite), embedded into the binary
├── pkg/protocol/          # Wire contract
├── pkg/askui/             # Public UI author API (scaffold)
├── proto/                 # gRPC definitions and bindings
├── third_party/           # Patched copies of acp-go-sdk and bubbletea
├── e2e/tui/               # Independent terminal E2E runner
├── .claude/skills/        # Project skills
└── docs/  plans/
```

## TUI extension boundary

Before adding TUI components or UI extensions, read [docs/tui-architecture.md](docs/tui-architecture.md) and the owning package README.
Keep terminal rendering in `internal/tui`, the `tui.Client` implementation in `internal/tuiclient`, client-local UI child lifecycle in `internal/tuiext`, public UI author APIs in `pkg/askui`, and shared wire data in `pkg/protocol`. `internal/tuiext` and `pkg/askui` are scaffolds today.
Do not expose internal or Bubble Tea types in the public UI SDK.
For TUI design files, follow the skill `.claude/skills/ask-tui-design`.

## Documentation

- `docs/README.md` is the index of all documents and says who each one is for.
- `docs/reference/` is generated from code (built-in tools and providers). Do not edit it by hand. After a change to a tool or the provider catalog, run `ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui`. The test fails when the files drift.
- `docs/cli.md` is hand-written. Update it when a subcommand, flag, environment variable, exit code, or TUI command changes.
- Every user-visible change adds one line under `[Unreleased]` in `CHANGELOG.md`.
- After a feature, run the `ask-docs-sync` skill (`.claude/skills/ask-docs-sync`). It updates the owning docs and the changelog.
- Write docs in ASD-STE100 Simplified Technical English. See `CONTRIBUTING.md`.
- CI (`.github/workflows/ci.yml`, GitHub-hosted `ubuntu-latest`) runs `go vet`, `go test` (including the drift test), and lint on every pull request and push to `master`.

## Common Commands

- `go mod tidy` - Install dependencies
- `go run ./cmd/server` - Start the server (applies pending migrations first)
- `go run ./cmd/server -migrate` - Apply migrations and exit
- `go test ./...` - Run tests
- `go vet ./...` - Run static analysis
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` - Lint, including the depguard import rules
- `go fmt ./...` - Format code
- `go run github.com/bufbuild/buf/cmd/buf generate` - Regenerate protobuf code after a `.proto` change
- `ASK_RECORD=1 go test -run '^TestX$' ./cmd/tui` - Re-record the LLM cassette of one test. `ASK_CAPTURE=<dir> ask -p ...` records a real run to a cassette. See `docs/testing-llm-cassettes.md`
- `python3 e2e/tui/run.py` - Run terminal E2E checks of the built `ask` on real terminal engines (Alacritty, Ghostty); follow [setup and results](e2e/tui/README.md). `--backend` selects an engine and `--case` selects cases.
  Keep this runner outside the production Go module.
  Headless terminal results do not certify Terminal.app, iTerm2, or kitty.
- `python3 e2e/tui/real_provider_scenarios.py acceptance` - By hand only: the provider-setup run of the built `ask` against real providers (`OPENAI_API_KEY` in the environment, a copy of an OAuth home, one short prompt). The runner removes every credential variable from the child and scans its output for key values. See `e2e/tui/README.md`
- `FSFAULT_ENGINE=docker internal/testsupport/fsfault/run-tests.sh` - Run optional Linux filesystem fault tests; follow the [fixture setup](internal/testsupport/README.md#filesystem-fault-tests).

### The `ask` binary

The leader flows need a binary named `ask`. A client starts the leader from a binary named `ask` that is next to the caller or on the `PATH` (`internal/leader/spawn.go`). `go run ./cmd/tui` makes a binary named `tui`, so the TUI falls back to the in-process agent and `/providers` is refused. Build a named binary for the TUI, `connect`, and `leader`: `go build -o ask ./cmd/tui`. `ask -p`, `ask auth`, `ask version`, and `ask acp` need no leader and work with `go run ./cmd/tui`. The full CLI reference is `docs/cli.md`.

- `ask` (no arguments, on a terminal) - Open the full-screen TUI on the alternate screen. It connects to the local leader and starts one when none runs. Only when that connect fails at startup does it run an agent in process; the header row then shows `in-process`. With no usable provider the session has no model: use `/providers` to sign in. Keys, folding, mouse, `/copy`, and `TEA_TRACE` are in `internal/tui/README.md` and `docs/cli.md`.
- `ask acp` (`go run ./cmd/tui acp`) - Serve ACP v1 over stdio. One Agent per session.
  It dispatches before prompt and mode parsing and takes no other argument.
  It uses `internal/app.ACPModule` with the shared native auth composition: no listener, no database, no leader, and no credential input on the protocol connection.
  Sign in on the host with `ask auth login`; `authenticate` only checks readiness. It serves no provider-setup methods, and `faux` needs `ASK_FAUX=1`.
  Exit codes: 0 input ended, 1 error or lost output, 130 SIGINT, 143 SIGTERM, 129 SIGHUP. See [the ACP owner](internal/acp/README.md#stdio-server).
- `ask leader` - Run the local leader: one agent for all local clients on `<ASK_HOME>/leader.sock`, owner only.
  `ask leader status|list|stop` never start a leader. Clients start it on demand through `leader.ConnectOrSpawn`.
  A client of a newer build replaces an older leader that a client started, also when it is busy (its sessions and background jobs end). For `go run` builds the binary written last wins unless its bytes are the same. On Linux a leader that holds the lock without a socket is stopped.
  See [the leader owner](internal/leader/README.md).
- `ask connect` - Line-oriented leader client (`ask connect --help`). It starts the leader when none runs and never falls back to an agent of its own.
- `ask version --json` - Build, protocol versions and `commitTime` (or `devStamp` for a build without a VCS stamp) on one line; a client reads it before it starts a leader from that binary.
- `ask -p "hello"` - Run a headless prompt in process and print the reply.
  `--mode json` writes each agent event as one JSON line instead.
  `faux` is the default provider (no `ASK_FAUX` needed), a scripted demo: `echo <text>` calls the echo tool, `fail <text>` ends in an assistant error, anything else is said back. `ASK_FAUX_TPS` paces it in tokens per second. `--provider` also takes `alibaba-token-plan`, `anthropic`, `openai`, and `xai`.
  Use [native auth and headless guidance](README.md#native-auth-and-headless-prompts) for provider login, host selection, and local logout.
  Auth and headless composition must not start a database or leader.
  Reuse `internal/app.NewNativeAuth` and `BindAuth`; do not add a command-specific auth service.
  Keep the private auth HTTP client outside inference capture.
  Every agent (print mode, leader, `ask acp`, in-process TUI) has the built-in tools `read`, `write`, `edit`, `ls`, `grep` (needs `rg`), `find`, `bash`, `job_output`, `job_kill`, `job_list`, and `monitor`, plus the demo tool `echo`; see `docs/reference/tools.md` and `internal/tools/README.md`. A `bash` call that runs past its wait budget (default 120 s, maximum 600 s) continues as a background job, and a finished job sends the model a `<system-reminder>` notice. A job belongs to the conversation that started it: after a conversation reset the old jobs run on, hidden, and their notices are dropped. Background jobs end when the session ends. `ask -p` does not stop them after the last reply: it waits for them, prints the reply of each run that a notice starts, and stops the jobs that still run only when it exits. In print mode a background job has a time limit (10 min by default, 2 h at most; `ASK_PRINT_BG_LIMIT_MS`), the wait has an idle cap (`ASK_PRINT_BG_WAIT_CEILING_MS`), and `--max-turns N` ends the run after N turns. `ASK_DISABLE_BACKGROUND_TASKS=1` turns background jobs off in every mode. See [print mode background work](docs/cli.md#background-work-in-print-mode). `ASK_COMMAND_GUARD=1` blocks a short list of destructive commands (`rm -rf /`, `mkfs`, a fork bomb); it is off by default and is not a security boundary.
  Exit codes: 0 done, 1 error (print mode also on an assistant error; `--mode json` exits 0 on an assistant error), 130 SIGINT, 143 SIGTERM, 129 SIGHUP.

## Better Fullstack project context

`bts.jsonc` is the authority for the current Stack Graph. Its `stackParts` array owns role selection and `ownerPartId` bindings. Top-level option fields are a compatibility projection and must not become a second mutation path.

### Stack Parts, ownership, and evidence

- `backend.api:go:grpc-go`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.buildTool:go:buf`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.caching:go:redis`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.cli:go:bubbletea`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.codeQuality:go:golangci-lint`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.config:go:viper`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.jobQueue:go:asynq`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.libraries:go:fx`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.logging:go:zap`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.migrations:go:golang-migrate`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.observability:go:opentelemetry`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.orm:go:gorm`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.realtime:go:centrifuge`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.testing:go:gomock`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.testing:go:testcontainers`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.testing:go:testify`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.validation:go:validator`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend:go:echo`. Its generated target is `apps/server`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `database:universal:sqlite`. Its generated target is `packages/db`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.

### Installed-version authority

Use `bts.jsonc` for the generator and schema version. Use local package manifests and lockfiles for installed dependency versions. Do not assume that documentation for a newer Better Fullstack release matches this project.

### Compatibility and lifecycle safety

Run `create-better-fullstack context --json` for bounded roles, capabilities, evidence, compatibility issues, and safe next actions. Run `create-better-fullstack doctor --json` before repairing graph drift. Existing-project writes must start with a plan and use the exact review token. Use `create-better-fullstack recipes check --json` before editing recipe-owned paths or managed regions, and use recipe history plus project recovery commands to undo a reviewed operation.

User code outside an explicit Better Fullstack managed region is not generator-owned. Missing or changed managed-region hashes stop recipe planning for manual review.

<!-- <better-fullstack:recipes sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855> -->

<!-- </better-fullstack:recipes> -->


## Maintenance

Keep AGENTS.md updated when:

- Adding/removing dependencies
- Changing project structure
- Adding new features or services
- Modifying build/dev workflows
- Making a user-visible change (also add a line to CHANGELOG.md)

AI assistants should suggest updates to this file when they notice relevant changes.
