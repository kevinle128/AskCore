# AskCore

This file provides context about the project for AI assistants.

## Project Overview

- **Ecosystem**: Go

## Tech Stack

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

## Architecture

Ask (this repository) is an agent harness that runs as a local daemon or as a remote agent in the cloud. It follows the dewee package model (one package for each capability) plus the Ask import rules. Read these before you add code:

- `docs/ask-architecture-reference.md`: design, import rules, "where to put new code"
- `internal/README.md`: package map
- `README.md` in each package: what belongs there, file names, allowed imports

The users/posts demo is the reference example for a new entity (see design section 11). Front-end and web UI for chat and control are out of scope. One exception: a read-only local monitoring dashboard ("Watch it think"). The daemon serves it as embedded static files, bound to localhost with a token. It only reads the harness event stream.

## Project Structure

```
AskCore/
├── cmd/
│   ├── server/            # Daemon: HTTP/gRPC/WS gateway, dashboard; ACP client of the leader; `-migrate` applies migrations and exits
│   └── tui/               # `ask` binary: TUI (ACP client of the leader), headless `ask -p` (agent in process), `ask leader`. See docs section 7.3
├── internal/
│   ├── app/               # Composition root (fx)
│   ├── config/  logs/     # Config (viper) and zap logger
│   ├── gateway/           # HTTP (echo) and gRPC servers, gRPC services; methods/ for WS RPC
│   ├── http/              # REST handlers (import alias httpapi)
│   ├── agent/ pipeline/ scheduler/ bus/ sessions/ workspace/ cron/   # Runtime core (scaffold)
│   ├── providers/ tools/ mcp/ skills/ memory/ bootstrap/ hooks/      # Capabilities (scaffold)
│   ├── permissions/ sandbox/ jobs/ crypto/ channels/ tracing/        # sandbox = process runner, jobs = per-session job table (implemented); the rest scaffold
│   ├── store/             # Models + store interfaces; gormstore/ = GORM implementation
│   ├── migrations/        # Migration runner
│   └── cache/ messaging/ realtime/ validation/ testsupport/
├── migrations/            # SQL files (SQLite), embedded into the binary
├── pkg/protocol/          # WS wire contract
├── proto/                 # gRPC definitions and bindings
└── docs/  plans/
```

## Common Commands

- `go mod tidy` - Install dependencies
- `go run ./cmd/server` - Start the server (applies pending migrations first)
- `go run ./cmd/server -migrate` - Apply migrations and exit
- `go test ./...` - Run tests
- `ASK_RECORD=1 go test -run '^TestX$' ./cmd/tui` - Re-record the LLM cassette of one test. `ASK_CAPTURE=<dir> ask -p ...` records a real run to a cassette. See `docs/testing-llm-cassettes.md`
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` - Lint, including the depguard import rules
- `go fmt ./...` - Format code
- `go run ./cmd/tui` - `ask` with no arguments on a terminal opens the full-screen TUI on the alternate screen. It connects to the local leader (and starts one when none runs). Only when that connect fails at startup does it run an agent in process, and the header row shows `in-process` instead of `leader <id>`; a leader lost later is reconnected (the TUI never switches to an in-process agent). On exit the terminal is restored and one line `ask: session <id>` (the full session id) stays in the shell, below the detach note when there is one. See `internal/tui/README.md`
  - First run: with no usable provider the session has no model. The header row shows `no model`, the status row says `Add a provider with /providers`, and a prompt is refused (`Not sent: no model`) and stays in the editor.
  - `/providers` opens the sign-in box in the prompt pane slot: seven login options (API key, browser URL, copy-code, device code); a saved sign-in is removed after a confirmation. It needs the leader; with the in-process agent it says so. The leader checks an API key before it saves it (mode 0600 `auth.json`); the Alibaba Token Plan key cannot be checked and is saved as it is. A key is pasted into a masked input, shown as `(hidden)`.
  - `/model` lists only the models of usable providers, and the choice is saved as the default in `settings.json` (`defaultModel`). A new session uses it when its provider is still usable. A session shows `model  <provider>/<model>` under the header and a change line when the model changes.
  - Tools, thinking and hook rows are one row each. `Tab` moves focus from the prompt to the block list; there `j`/`k` select, `e` cycles the fold mode (collapsed, truncated, expanded), `→`/`l` and `←`/`h` expand and collapse, `E` folds all steps, `Enter` opens the block in a full-screen viewer (`Esc` returns), `g`/`G`, `PgUp`/`PgDn` and `shift+←`/`shift+→` move, and a printable key moves focus back to the prompt and types it. `ctrl+t` toggles all thinking and `ctrl+End` jumps to the end. The pane follows new output; after you scroll up, `▼ End` or `▼ <N> new lines · End` shows at the bottom. See the key list in `internal/tui/README.md`
  - The mouse is on: the wheel scrolls, a click selects a block (the first row of a step opens the viewer), a drag selects text and copies it on release through the `/copy` path. The terminal keeps its own selection with `shift`+drag (`option`+drag on macOS). `/copy` copies the last assistant reply (native command first, then OSC 52 up to 75 KB) and reports the real result. Terminal.app, iTerm2 and kitty were not hand-checked
  - `TEA_TRACE=<file>` writes raw keyboard input, pasted keys included, to that file. The TUI warns on screen when it is set. Do not set it when you sign in.
  - The demo provider `faux` is opt-in: set `ASK_FAUX=1` for the leader, `ask acp` and the in-process TUI agent (the tests and `e2e/tui/run.py` do). Without it, only real providers are listed.
- `python3 e2e/tui/run.py` - tui-test checks of the built `ask` on real terminal engines (Alacritty, Ghostty): idle, a turn with a tool and the viewer, resize while streaming, model selector, provider setup, mouse wheel, copy, in-process, and the exit paths (`/quit`, ctrl+c, SIGTERM, SIGHUP).
  `--driver t0|ask` selects one.
  See `e2e/tui/README.md`.
- `python3 e2e/tui/real_provider_scenarios.py acceptance` - by hand only: the provider-setup run of the built `ask` against real providers (`OPENAI_API_KEY` in the environment, a copy of an OAuth home, one short prompt). The runner removes every credential variable from the child and scans its output for key values. See `e2e/tui/README.md`
- `go run ./cmd/tui acp` - `ask acp`: serve ACP v1 over stdin/stdout (one Agent per session, no listener, no database; sign in first with `ask auth login`; it serves no provider-setup methods, and `faux` needs `ASK_FAUX=1`). See `internal/acp/README.md`
- `go run ./cmd/tui leader` - `ask leader`: the local router that holds one agent for all local clients (Unix socket `<ASK_HOME>/leader.sock`, owner only). `ask leader status|list|stop` manage it and never start one. Clients start it on demand. A client of a newer build replaces an older leader that a client started, also when it is busy (its sessions and background jobs end); for `go run` builds the binary written last wins unless its bytes are the same. On Linux a leader that holds the lock without a socket is stopped. See `internal/leader/README.md`
- `go run ./cmd/tui connect` - `ask connect`: line-oriented client of the leader (starts it when none runs, no direct-agent fallback). `ask connect --help` lists the commands
- `go run ./cmd/tui version --json` - Build, protocol versions and `commitTime` (or `devStamp` for a build without a VCS stamp) on one line; a client reads it before it starts a leader from that binary
- `go run ./cmd/tui -p "hello"` - Headless print mode: runs one prompt in process and prints the reply. `--mode json` writes every agent event as one JSON line instead. `ask -p` keeps `faux` as its default provider (no `ASK_FAUX` needed), a scripted demo: `echo <text>` calls the echo tool, `fail <text>` ends in an assistant error, anything else is said back. `--provider` also takes `alibaba-token-plan`, `anthropic`, `openai` and `xai`; keys and sign-in come from `ask auth login` or the environment. `ASK_FAUX_TPS` paces it in tokens per second. Every agent (print mode, leader, `ask acp`, in-process TUI) has the built-in tools `read`, `write`, `edit`, `ls`, `grep` (needs `rg`), `find`, `bash`, `job_output`, `job_kill`, `job_list` and `monitor`; see `internal/tools/README.md`. A `bash` call that runs past its wait budget (default 120 s, maximum 600 s) continues as a background job, and a finished job sends the model a `<system-reminder>` notice. Background jobs end when the session ends: `ask -p` stops the jobs that still run after the last reply. `ASK_COMMAND_GUARD=1` (leader, `ask acp` or `ask -p` environment) blocks a short list of destructive commands (`rm -rf /`, `mkfs`, a fork bomb); it is off by default and is not a security boundary. Exit codes: 0 done, 1 error (print mode also on an assistant error; `--mode json` exits 0 on an assistant error), 130 SIGINT, 143 SIGTERM, 129 SIGHUP

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

Keep CLAUDE.md updated when:

- Adding/removing dependencies
- Changing project structure
- Adding new features or services
- Modifying build/dev workflows

AI assistants should suggest updates to this file when they notice relevant changes.
