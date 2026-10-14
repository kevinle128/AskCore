# Docs audit: drift, CLI inventory, changelog material

Date: 2026-10-13. Branch: `agent-capability-discovery-autodoc`. Read-only audit for phases 3 and 4 of `plans/261013-1309-docs-changelog-standardization/plan.md`.
This report is a stateful record. It is not product authority. Line numbers are those of the tree on the audit date.

Evidence labels: `code` = read in source; `run` = a command was run; `plan` = a plan or journal says so and no code check was made.

## 0. Findings that change the plan

1. **Five live links point to `README.md#native-auth-and-headless-prompts`.** The rewrite of README.md must keep that anchor, or these links must move in the same change:
   `AGENTS.md:93`, `internal/settings/README.md:46`, `internal/auth/README.md:22`, `docs/testing-llm-cassettes.md:87`, `docs/ask-architecture-reference.md:351`. (Also in the stateful record `plans/reports/docs-261006-0823-h7a-runtime.md:15-17`; leave it.)
   Recommendation: move the auth section to `docs/cli.md` or a guide only if all five links are retargeted. Smallest change: keep the section and the anchor in README.md.
2. **`go run ./cmd/tui` is wrong for every flow that needs a leader.** The client starts a leader only from a binary named `ask` that is next to the caller or on `PATH` (`internal/leader/spawn.go:150-165`). `go run` makes a temporary binary named `tui`, so the TUI falls back to the in-process agent and `/providers` is refused. `tasks/lessons.md:30-31` records the same finding. Both agent files and README.md use `go run ./cmd/tui` for TUI, `connect`, `leader` and `acp`. Use a built `ask` for these in the merged text. (`ask -p`, `ask auth`, `ask version`, `ask acp` do not need a leader and work with `go run`.)
3. **`bts.lock.json` hashes are already stale.** It stores a hash for `AGENTS.md` (`6f5118...`) and `CLAUDE.md` (`365724...`). The current files hash to `4dbb33...` and `961521...` (run: `shasum -a 256`). Editing the two files makes no new drift. Do not edit `bts.lock.json` in this plan.
4. **The empty recipes region is identical in both files** (`AGENTS.md:135-137`, `CLAUDE.md:122-124`; run: `diff` of the whole "Better Fullstack project context" block is empty). Keep it byte for byte, including `sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.
5. **Phase 2 files already exist in the working tree** (untracked): `cmd/tui/reference_docs_test.go`, `docs/reference/tools.md`, `docs/reference/providers.md`. The docs index must list `docs/reference/` as generated. `docs/reference/tools.md` lists `echo`; the agent-file tool list omits it (see section 2).
6. **`internal/tuiclient` has no README and is not in `internal/README.md` or `docs/ask-architecture-reference.md`** (grep `tuiclient` in `*.md`: only `internal/tui/README.md`). `internal/tui/README.md` links to the directory.

## 1. README.md drift

File: `README.md` (194 lines). Verdicts: **true** = matches code; **false** = contradicts code; **scaffold-only** = package exists but the running product does not use it.

### 1.1 Opening and feature list

| Claim (README line text) | Verdict | Evidence |
|---|---|---|
| "created with Better Fullstack, a high-performance Go stack" | true as history, wrong as the product description | `bts.jsonc` and `bts.lock.json` exist. The product is an agent harness: `docs/ask-architecture-reference.md:3`. Drop from the intro; keep at most one line in a "built with" note. |
| Features: Go | true | `go.mod:3` `go 1.27.0` |
| Features: Echo | true (wired) | `internal/gateway/http_server.go:10-11`, `internal/app/app.go:34-38` |
| Features: GORM | true (wired) | `internal/app/app.go:31` `newDB`, `gormstore.NewStores`; SQLite driver `go.mod:51` |
| Features: gRPC | true (wired) | `internal/app/app.go:39-40` `NewGreeterService`, `NewGRPCServer`; `proto/greeter.proto` |
| Features: Bubble Tea | true | `charm.land/bubbletea/v2` in `go.mod:6`, replaced by `third_party/bubbletea` (`go.mod:421`); used by `internal/tui` |
| Features: Zap | true (wired) | `internal/app/app.go:55` `newLogger` |
| Features: Testing gomock, testcontainers, testify | partly true | testify and goleak are used widely. gomock only in `internal/testsupport/testsupport.go`; testcontainers only in `internal/testsupport/testcontainers_test.go`. go-vcr (cassettes) is missing from the list: `internal/providers/cassette/cassette.go:25-26`. |
| Features: Messaging asynq | scaffold-only | `internal/messaging/asynq.go` is the only importer of asynq; `internal/app` does not import `messaging` (`app.Module`, `internal/app/app.go:29-49`) |
| Features: Observability opentelemetry | scaffold-only | only `internal/tracing/otelexport/otel.go` imports otel; not composed in `app.Module` |
| Features: Validation validator | scaffold-only | only `internal/validation/validation.go` imports it; no non-test importer of `internal/validation` (run: grep) |
| Features: Code Quality golangci-lint | true | `.golangci.yml`; `tools.go:6` |
| Features: Migrations golang-migrate | true (wired) | `internal/app/app.go:48` `runMigrations`; `cmd/server/main.go:30` |
| Features: Protobuf Tooling buf | true | `buf.yaml`, `buf.gen.yaml`, `tools.go:7` |
| Features: Dependency Injection fx | true | `cmd/server/main.go:36`, `internal/app/app.go:29` |
| (absent) | missing | The README never names: the `ask` binary, the TUI, `ask -p`, leader, ACP, providers, built-in tools. |

Redis, centrifuge: `internal/cache` and `internal/realtime` also exist but `app.Module` does not compose them (scaffold-only). `.env.example` has `REDIS_URL`, `REDIS_ADDR`, `OTEL_*` for them.

### 1.2 Prerequisites and Getting Started

| Claim | Verdict | Evidence |
|---|---|---|
| "Go 1.25 or higher" | **false** | `go.mod:3` is `go 1.27.0`; raised on purpose for fantasy v0.45.2 (`plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` D22). Local `go version` is go1.27.0. Write "Go 1.27 or higher". |
| Prerequisite: `protoc` and Go gRPC plugins | **false / misleading** | The repo generates with buf and remote plugins (`buf.gen.yaml:3-8`); generated files are committed (`proto/greeter.pb.go`, `proto/greeter_grpc.pb.go`). Building and running need neither. Only changing `.proto` files needs buf (`go run github.com/bufbuild/buf/cmd/buf generate`, `tools.go:7`; remote plugins need network access). |
| "copy the environment file: `cp .env.example .env`" | true, optional | `.env.example` exists at the repo root; `cmd/server/main.go:27` loads `.env` with godotenv and ignores a missing file. |
| `go mod tidy` then `go run ./cmd/server` | true | `cmd/server/main.go` |
| "The server applies pending database migrations before it starts" | true | `internal/app/app.go:46-48`; `cmd/server/main.go:11` |
| "The server will be running at http://localhost:8080" | true (default) | `internal/config/config.go:41` `port` 8080; default host `0.0.0.0` (`config.go:40`) |

### 1.3 Native auth and headless prompts (lines 52-120)

I checked the checkable facts against code. All are true unless marked.

| Claim | Verdict | Evidence |
|---|---|---|
| `go build -o ./ask ./cmd/tui`; auth and `-p` run without database, daemon or leader | true | `cmd/tui/headless.go:81-110` (auth path), `:164-170` (print path) call only `app.NewNativeAuth` |
| `ask auth --help` | true | `cmd/tui/auth_args.go:14-23` |
| Anthropic `--method anthropic-oauth --interaction browser\|copy-code` | true | `cmd/tui/auth_args.go:64`; method list `:19` |
| ChatGPT: browser by default; `--new-account` replaces the account | true | `cmd/tui/auth.go:63-67` forces `browser`; `auth_args.go:61-63` |
| `--model gpt-5.6-sol` selects a catalog model | true | `internal/providers/api.go:21` `ModelGPT56Sol`; `internal/providers/catalog.go:105` |
| xAI: verification URL and device code | true (plan) | method `xai-oauth` in `auth_args.go:19`; flow detail not re-read |
| Fixed loopback ports: Anthropic 53692, ChatGPT 1455 | true | `internal/auth/anthropic.go:23`, `internal/auth/chatgpt.go:46` |
| "It prints the sign-in URL; it does not start a browser" | true | `cmd/tui/auth.go:76-85` writes `n.URL` to stderr; no browser start in `cmd/tui` |
| Wait for `Credential saved.` | true | `cmd/tui/auth.go:100` |
| API key on private stdin; 16 KiB limit; noninteractive login needs `--method` | true | `cmd/tui/auth_input.go:14`, `internal/auth/login.go:69`, `cmd/tui/auth.go:41-44` |
| "See command help (cmd/tui/auth_args.go)" | true | file exists |
| Logout deletes the local record; env key stays eligible | true | `cmd/tui/auth.go:98` message; `auth_args.go:22` |
| "Each provider has one saved credential" | true | `docs/adr/0001-provider-auth-composition.md` |
| Link `internal/settings/README.md#credential-transaction` | true | heading `## Credential transaction` at `internal/settings/README.md:21` |

Not in the README but true and needed by a user: `ASK_HOME` default is `~/.ask` (`internal/leader/paths.go:30-41`, `internal/settings/auth.go:66-76`); `ASK_HOME` must be an absolute path (`internal/leader/paths.go:41`). API-key environment variables are `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `XAI_API_KEY`, and `ASK_ALIBABA_TOKEN_PLAN_API_KEY` or `ALIBABA_TOKEN_PLAN_API_KEY` (`internal/providers/keys.go:44-53`).

### 1.4 Database, gRPC, structure, commands

| Claim | Verdict | Evidence |
|---|---|---|
| "GORM with SQLite, for both daemon and cloud mode" | true for the server; "cloud mode" is design intent | SQLite only: `go.mod:28,51`. No mode switch exists in `internal/config/config.go`. |
| `DATABASE_URL` is the SQLite path, default `app.db` | true | `internal/config/config.go:43`; `config_test.go:20` |
| Schema comes only from `migrations/`, embedded | true | `migrations/embed.go`; `internal/migrations/README.md:1` |
| `-migrate` applies and exits | true | `cmd/server/main.go:22,30-37` |
| Migration pair naming `NNNNNN_<slug>.up/down.sql` | true | `migrations/000001_create_users_posts.{up,down}.sql` |
| "The server runs both HTTP and gRPC concurrently", 8080 and 50051 | true | `internal/config/config.go:41-42`; `internal/app/app.go` `startServers` |
| `protoc --go_out=. --go-grpc_out=. proto/*.proto` regenerates code | **false** | `proto/greeter.proto:5` `go_package = "AskCore/proto"`. Without `paths=source_relative` protoc writes under `./AskCore/proto/`, not into `proto/`. `buf.gen.yaml:5,8` sets `paths=source_relative`. Use only the buf command. |
| Architecture paragraph, links to `docs/ask-architecture-reference.md` and `internal/README.md` | true | both files exist |
| Project Structure tree | partly false | `cmd/tui` comment "Terminal client" is incomplete (it is the `ask` binary: TUI, `-p`, auth, acp, leader, connect, version). `cmd/server` "HTTP + gRPC" is correct. Missing real dirs: `internal/{acp,auth,leader,settings,jobs,sandbox,tui,tuiclient,tuiext}`, `pkg/askui`, `third_party/`, `.claude/skills/`. `.env.example` exists. `e2e/tui/` exists. |
| Available Commands: `go build ./...`, `go vet ./...`, `go fmt ./...`, `go test ./...`, lint command | true | commands are standard; lint entry is `tools.go:6` |
| `python3 e2e/tui/run.py`: "Run isolated T0 and built Ask terminal E2E tests" | **false** | `e2e/tui/run.py:251-253` `ASK_CASES` has only built-`ask` cases; no `t0` string in `run.py` (run: grep). Options are `--backend`, `--artifacts`, `--case` (`run.py:1159-1162`). |
| `go run ./cmd/tui`: "Run the TUI application" | misleading | works only as the in-process fallback (finding 2 in section 0) |
| `go run ./cmd/tui acp`, `leader`, `connect`, `version --json`, `-p` descriptions | true in content, see section 3 | |
| `protoc ...` and `buf generate` listed twice as alternatives | misleading | see the `protoc` row above |

### 1.5 What is true and worth keeping

Keep (rewrite in Ask terms): the migrations rule (embedded SQL, apply at start, `-migrate`); the server ports and `DATABASE_URL`/`.env` facts; "buf generate" for proto changes; the whole native auth section (it is the most accurate part and five docs link to it); the architecture paragraph with the two links; `e2e/tui/README.md` link.
Drop: Better Fullstack marketing intro; the feature list as written; the protoc prerequisite and command; "T0".
State plainly: the server is a thin demo today (health, users, posts, Greeter RPC; see `internal/http/health.go:19-20`, `users.go:23-27`, `posts.go:23-25`); the WS endpoint, the read-only dashboard and "ACP client of the leader" in `internal/gateway/README.md:3` and the agent files are design intent, not code (no WS or dashboard code in `internal/gateway/*.go`; `ls` shows `config.go grpc_greeter.go grpc_server.go http_server.go methods/` with `methods/` holding only `doc.go`).

## 2. AGENTS.md vs CLAUDE.md

`diff AGENTS.md CLAUDE.md` (run) gives these hunks: `16c16`, `24,25d23`, `49,51c47,49`, `58d55`, `62,67d58`, `74,77c65`, `80,97c68,84`, `141c128`. AGENTS.md has 148 lines, CLAUDE.md 135. Lines `1-15`, `17-23`, `26-48`, `52-57`, `68-73`, `78-79`, `99-137`, `139-140`, `142-148` are identical in both.

Rule used: when both files are right in different parts, the merged text is the union. Where one file is stale, the code decides.

### Hunk 1: Testing line (AGENTS 16 / CLAUDE 16)

- AGENTS: `- Testing: gomock, testcontainers, testify, goleak`
- CLAUDE: `- Testing: gomock, testcontainers, testify, goleak, go-vcr (LLM cassettes)`
- Verdict: **CLAUDE correct, AGENTS stale.** `go.mod:50` `gopkg.in/dnaeon/go-vcr.v4`; used in `internal/providers/cassette/cassette.go:25-26`.
- Merged: `- Testing: gomock, testcontainers, testify, goleak, go-vcr (LLM cassettes)`

### Hunk 2: two Tech Stack lines (AGENTS 24-25; absent in CLAUDE)

- AGENTS: ACP `github.com/coder/acp-go-sdk` v0.13.5 (stable schema 0.13.5, wire v1) with the conformance report; native auth identity `github.com/coreos/go-oidc/v3`.
- CLAUDE: nothing.
- Verdict: **AGENTS correct.** `go.mod:19` acp-go-sdk v0.13.5; `go.mod:20` go-oidc v3.21.0; `plans/reports/conformance-261007-h13a-sdk.md` exists. Add: `go.mod:417` replaces the SDK with `./third_party/acp-go-sdk` (one scanner limit raised, `third_party/acp-go-sdk/PATCH.txt`); `go.mod:421` replaces Bubble Tea with `./third_party/bubbletea`. Both are missing from the stack list in both files, as are `charm.land/fantasy v0.45.2` (`go.mod:7`) and the other Charm libraries.
- Merged: keep both AGENTS lines; add lines for fantasy (provider wire layer, `internal/providers/fantasykit`), and a line "Local patched copies: `third_party/acp-go-sdk`, `third_party/bubbletea` (see `go.mod` `replace`)". Keep the `go.mod` pointer for version authority.

### Hunk 3: Project Structure rows (AGENTS 49-51 / CLAUDE 47-49)

- AGENTS: `agent/ pipeline/ scheduler/ bus/ sessions/ workspace/ cron/ # Runtime core and planned capabilities`; `auth/ providers/ tools/ mcp/ skills/ memory/ bootstrap/ hooks/ # Capabilities`; `permissions/ sandbox/ crypto/ channels/ tracing/ # (scaffold)`
- CLAUDE: same dirs with `(scaffold)` on the first two rows; no `auth/`; the third row has `jobs/` and the note "sandbox = process runner, jobs = per-session job table (implemented); the rest scaffold".
- Verdict: **both partly stale.**
  - `auth/` exists and is implemented (`internal/auth/*.go`): AGENTS right, CLAUDE omits it.
  - `jobs/` exists and is implemented: CLAUDE right, AGENTS omits it. AGENTS lists `sandbox/` under "(scaffold)": **false**, `internal/sandbox/README.md:1` and `internal/tools/shell_bash.go` use it.
  - CLAUDE says `agent/ pipeline/ ... # (scaffold)`: **false** for `agent` (19 non-doc files), `pipeline` (6), `sessions` (3), `bus` (1), `workspace` (1). Scaffold (only `doc.go` plus README; run: count of non-doc, non-test `.go` files = 0): `scheduler`, `cron`, `mcp`, `skills`, `bootstrap`, `hooks`, `channels`, `tracing`, `permissions`, `memory`, `crypto`. `providers` and `tools` are implemented (CLAUDE calls them scaffold: false).
  - Dirs missing from both: `acp/`, `leader/`, `settings/`, `tui/`, `tuiclient/`, `tuiext/` (scaffold, `internal/tuiext/README.md:3-5`), `pkg/askui/` (scaffold, `pkg/askui/README.md:3`), `third_party/`.
  - `internal/cache`, `messaging`, `realtime`, `validation` are client wrappers not composed in `app.Module` (see 1.1).
- Merged (tree rows to use):

```
│   ├── agent/ pipeline/ sessions/ bus/ workspace/    # Runtime core (implemented; workspace in part)
│   ├── scheduler/ cron/                              # Runtime core (scaffold)
│   ├── auth/ settings/ providers/ tools/ sandbox/ jobs/   # Capabilities (implemented)
│   ├── acp/ leader/                                  # Process model: ACP adapter, local router
│   ├── tui/ tuiclient/ tuiext/                       # Terminal UI, its client, extension host (tuiext = scaffold)
│   ├── mcp/ skills/ memory/ bootstrap/ hooks/ permissions/ crypto/ channels/ tracing/   # (scaffold or parked)
```

  Keep `internal/README.md` as the owner of the map (it is the better source); the agent file may say "see `internal/README.md`" instead of a second tree.

### Hunk 4: e2e row (AGENTS 58; absent in CLAUDE)

- AGENTS: `├── e2e/tui/ # Independent terminal E2E runner`
- Verdict: **AGENTS correct.** `e2e/tui/run.py` exists. Keep. Also add `third_party/`, `pkg/askui/` rows.

### Hunk 5: TUI extension boundary section (AGENTS 62-67; absent in CLAUDE)

- AGENTS: three-line rule: read `docs/tui-architecture.md`; rendering in `internal/tui`, lifecycle in `internal/tuiext`, public API in `pkg/askui`, wire data in `pkg/protocol`; no Bubble Tea types in the SDK.
- Verdict: **AGENTS correct as a rule** (`docs/tui-architecture.md` exists; `internal/tuiext` and `pkg/askui` are scaffolds, so the rule is a target boundary, not shipped code). Add "(`tuiext` and `askui` are scaffolds)". Add `internal/tuiclient` (the `tui.Client` implementation, `internal/tui/README.md:9`).
- Merged: keep the section with those two additions.

### Hunk 6: test commands (AGENTS 74-77 / CLAUDE 65)

- AGENTS: `python3 e2e/tui/run.py` with link to `e2e/tui/README.md`, "Keep this runner outside the production Go module", "Headless terminal results do not certify Terminal.app or iTerm2"; plus `FSFAULT_ENGINE=docker internal/testsupport/fsfault/run-tests.sh` (optional Linux fault tests, link to `internal/testsupport/README.md#filesystem-fault-tests`).
- CLAUDE: `ASK_RECORD=1 go test -run '^TestX$' ./cmd/tui` (re-record one cassette) and `ASK_CAPTURE=<dir> ask -p ...`, link `docs/testing-llm-cassettes.md`.
- Verdict: **both correct, different content.** Evidence: `e2e/tui/README.md:3-20`; `internal/testsupport/fsfault/run-tests.sh` exists and `internal/testsupport/README.md:47` has the command; `cmd/tui/headless_cassette_test.go:24` `ASK_RECORD`; `cmd/tui/capture.go:19` `ASK_CAPTURE`; `docs/testing-llm-cassettes.md:96-103`. Anchor `#filesystem-fault-tests` exists (`internal/testsupport/README.md:33`).
- Merged: keep all three bullets.

### Hunk 7: command bullets (AGENTS 80-97 / CLAUDE 68-84)

CLAUDE places these in a different order and has more TUI detail. Per command:

| Command | AGENTS text | CLAUDE text | Verdict | Merged text |
|---|---|---|---|---|
| `go run ./cmd/tui` (TUI) | absent | long entry plus six sub-bullets (alt screen, first run, `/providers`, `/model`, blocks and keys, mouse, `TEA_TRACE`, `faux`) | **CLAUDE correct and needed**, but see checks below and finding 2 (binary name) | Use CLAUDE text. Move the long sub-bullets to `docs/cli.md` or `internal/tui/README.md` and keep a 2-line entry here; those facts are owned by `internal/tui/README.md`. |
| `python3 e2e/tui/run.py` (second entry) | one line in hunk 6 | long entry with `--driver t0\|ask` | **CLAUDE stale on `--driver`** | `run.py` has no `--driver`; it has `--backend {alacritty,ghostty,rio}`, `--case`, `--artifacts` (`e2e/tui/run.py:1159-1162`). Use `--backend` and `--case` and link `e2e/tui/README.md`. The case list in CLAUDE matches `ASK_CASES` (`run.py:251-253`). |
| `python3 e2e/tui/real_provider_scenarios.py acceptance` | absent | by-hand real-provider run | **CLAUDE correct** | `e2e/tui/real_provider_scenarios.py:6,562,573,584` (`acceptance` is a scenario name, argv[1]). Keep, "by hand only". |
| `ask acp` | 5 lines: dispatch before prompt and mode parsing; takes no other argument; `internal/app.ACPModule`; no listener, database, leader, credential input; `authenticate` only checks readiness; exit codes 0/1/130/143/129 | one line: ACP v1 over stdin/stdout, one Agent per session, sign in first, "it serves no provider-setup methods, and `faux` needs `ASK_FAUX=1`" | **both correct** | `cmd/tui/headless.go:117-119` (dispatch before parse); `cmd/tui/acp.go:24-35,40-48,75-76`; faux gate `acp.go:120`; no provider setup on the editor link: `internal/app/module_leader.go:84` comment. Union: AGENTS facts plus the two CLAUDE facts and the README link `internal/acp/README.md`. Exit codes in `acp.go:34`. |
| `ask leader` | 3 lines | 1 long line: adds "A client of a newer build replaces an older leader that a client started, also when it is busy (its sessions and background jobs end); for `go run` builds the binary written last wins unless its bytes are the same. On Linux a leader that holds the lock without a socket is stopped." | **both correct; CLAUDE has the newer facts** | Newer-build replacement: `plans/261011-2304-leader-connect-or-spawn-port/plan.md` (completed) and `cmd/tui/leader_replace_e2e_test.go`, `leader_dev_replace_e2e_test.go`, `leader_evict_linux_e2e_test.go`. The AGENTS name `leader.ConnectOrSpawn` is correct (`internal/leader/client.go:124`). Union both; and add the `FindAsk` rule (finding 2). |
| `ask connect` | one line | one line | **both correct** | `cmd/tui/connect.go:21-51`. Keep one line; point to `docs/cli.md`. |
| `ask version --json` | "Build and protocol versions on one line" | adds `commitTime` or `devStamp`, and that a client reads it before it starts a leader | **CLAUDE correct and fuller** | `cmd/tui/version.go:18-27,144`. Use CLAUDE text. |
| `ask -p` | AGENTS: `--mode json`; "`faux` remains the default provider"; link to README auth section; "Auth and headless composition must not start a database or leader"; reuse `internal/app.NewNativeAuth` and `BindAuth`; keep the private auth HTTP client outside inference capture; exit codes 0,1,130,143,129 | CLAUDE: demo script `echo <text>` / `fail <text>`; `--provider` list; `ASK_FAUX_TPS`; built-in tools list; `bash` wait budget 120 s (max 600 s) and background job plus `<system-reminder>`; job end on session end; `ASK_COMMAND_GUARD=1`; exit codes with the `--mode json` note | **both correct** | AGENTS rules: `cmd/tui/headless.go:164-170`, `docs/testing-llm-cassettes.md:80-85`. CLAUDE facts: demo `cmd/tui/headless_faux.go:21-44`; `ASK_FAUX_TPS` `:19`; wait budget `internal/tools/shell_bash.go:21,23`; guard `cmd/tui/headless.go:269,344`. Tool list is missing `echo` (registered for every agent, `headless.go:233-241`, `internal/tools/echo.go`); say "plus the demo tool `echo`" or link `docs/reference/tools.md`. `--provider` list matches `cmd/tui/args.go:195`. Exit codes: see section 3. Union both. |

Checks of CLAUDE TUI sub-bullets against code (all true unless marked):
- `/providers` has seven login options: `internal/auth/status.go:30-40` (7 rows). True.
- API key checked before save; Alibaba key cannot be checked; mode 0600: `internal/settings/auth.go:171,287` (0600). The key-check rule is in `internal/auth/keycheck.go`; Alibaba exception: plan `261010-1424-t1c-provider-setup` (plan, not re-read in code).
- `/model` saves `defaultModel` in `settings.json`: `internal/settings/settings.go:17`. True.
- `/copy` OSC 52 limit 75 KB: `internal/tui/clipboard.go:28` `75 * 1024`. True.
- Keys `Tab`, `ctrl+t`, `ctrl+End`, `shift+←/→`: `internal/tui/keys.go:19,47,101,105`. True.
- `TEA_TRACE=<file>`: `cmd/tui/interactive.go:178`; warning text `internal/tui/model_line.go:112`. True.
- "Terminal.app, iTerm2 and kitty were not hand-checked": plan evidence only.
- `ASK_FAUX=1` for leader, `ask acp`, in-process TUI: `cmd/tui/acp.go:20-22,120`. True. Whether the leader and in-process TUI read it was not re-traced (they share `acpParams`; `cmd/tui/acp.go:105-108` comment says so).

### Hunk 8: Maintenance line (AGENTS 141 / CLAUDE 128)

- AGENTS: `Keep AGENTS.md updated when:`; CLAUDE: `Keep CLAUDE.md updated when:`.
- Verdict: **name only.** Merged: `Keep AGENTS.md updated when:`. Add a bullet "Adding a user-visible change (add a line to CHANGELOG.md)" only after the user accepts the changelog rule.

### Lines identical in both files that are stale or incomplete

| Line (AGENTS / CLAUDE) | Text | Problem | Evidence |
|---|---|---|---|
| 42 / 40 | `cmd/server`: "Daemon: HTTP/gRPC/WS gateway, dashboard; ACP client of the leader; `-migrate`..." | Only HTTP, gRPC and `-migrate` exist. WS, dashboard and ACP client are design intent. | `cmd/server/main.go:1-6` says "the HTTP and gRPC servers"; no WS or dashboard code in `internal/gateway`. |
| 43 / 41 | `cmd/tui`: "TUI (ACP client of the leader), headless `ask -p`, `ask leader`" | Incomplete: also `auth`, `acp`, `connect`, `version`. | `cmd/tui/headless.go:81-130` |
| 35 / 33 | "The daemon serves it [the dashboard] as embedded static files, bound to localhost with a token." | Intent, not shipped. Label as planned. | same as above; D12 in the roadmap |
| 33 / 31 | `README.md` in each package "what belongs there, file names, allowed imports" | 11 directories with Go code have no README (list in section 5.2). | run: `find` |
| 70 / 57 | `go mod tidy` - "Install dependencies" | Works; `go mod download` is the install step. Low value. | |
| 71-72 / 58-59 | `go run ./cmd/server`, `-migrate` | True. | `cmd/server/main.go` |
| 78 / 62 | lint command | True. | `tools.go:6` |
| 12-14 / 12-14 | Tech Stack "Web Framework: echo, Database: gorm, API: grpc-go, CLI: bubbletea" | True for the server and TUI. | |
| 17 / 17 | "Messaging: asynq" | Package exists; not composed in the running server. | section 1.1 |
| 18 / 18 | "Observability: opentelemetry" | same | |
| 19 / 19 | "Validation: validator" | same | |
| 99-137 / 86-124 | Better Fullstack block | Generator record. `backend.caching:go:redis`, `backend.realtime:go:centrifuge`, `backend.jobQueue:go:asynq` are in the stack graph but not composed in `app.Module`. Keep byte for byte as the plan says; do not edit. | `bts.jsonc` |

Missing from both files and worth adding (the commands exist): `go vet ./...` (README has it); `go build -o ./ask ./cmd/tui` with the reason (finding 2); `ask auth login|logout`; `ask leader status|list|stop`; `ask connect --cwd|--attach`; where `ASK_HOME` points; `docs/reference/` (generated) and `docs/cli.md`; the `.claude/skills/ask-tui-design` skill (`plans/261009-2239-t1a-local-tui/plan.md` links it as a rule for TUI design files).

## 3. CLI inventory for `docs/cli.md`

Binary: `ask`, built from `cmd/tui` (`cmd/tui/main.go:7`). `go run ./cmd/tui` gives a binary named `tui`; leader flows need the name `ask` (finding 2). Dispatch order in `runWithDependencies` (`cmd/tui/headless.go:81-130`): `auth`, `acp`, `connect`, `leader`, `version`, then root flag parsing (`parseArgs`, `args.go:96`).

Signal exit codes for all commands: SIGINT 130, SIGTERM 143, SIGHUP 129 (`cmd/tui/headless.go:41-45`). A second signal ends `acp`/`leader` before cleanup finishes (`acp.go:35`, `leader.go:43`).

### 3.1 `ask` (root)

- Syntax: `ask [options] [--] [@files...] [messages...]` (`args.go:182-201`).
- Mode choice (`selectMode`, `cmd/tui/headless.go:194`): `--mode json` gives JSON mode; `--print`/`-p`, or stdin not a terminal, or stdout not a terminal gives print mode; otherwise the interactive TUI. `--mode text` does not force print mode.
- Flags (`args.go:76-92,131-151`):

| Flag | Purpose |
|---|---|
| `-p`, `--print` | Non-interactive: run the prompt, print the reply, exit. An immediately following token that is not `@file` and does not start with `-` is taken as the first message (`args.go:131-139`). |
| `--mode text\|json` | Output format. Any other value is an error. `json` writes one JSON line per agent event (`headless_json.go`). |
| `--provider <name>` | `faux` (default), `alibaba-token-plan`, `anthropic`, `openai`, `xai` (`args.go:195`, `internal/providers/api.go:13-16`). |
| `--model <id>` | Model id. Defaults: `faux-1`; `deepseek-v4.1-flash` (alibaba-token-plan); `claude-sonnet-4-6` (anthropic); `gpt-5.5` (openai); `grok-4.7` (xai) (`args.go:165-177`, `internal/providers/api.go:17-21`). |
| `--api-key <key>` | Key for this run. Needs `--model`, except for `alibaba-token-plan` (`args.go:162-164`). Prefer the environment or `ask auth login`; a key in argv is visible to other processes. |
| `--thinking <level>` | `off, minimal, low, medium, high, xhigh, max` (`args.go:70-74`). `alibaba-token-plan` defaults to `medium` (`headless.go:282`). |
| `--` | End of options. |
| `-h`, `--help` | Usage. |
| `@path` | A positional starting with `@` is a file: text is wrapped in `<file name="...">` and put before the first message; `~` is expanded; an empty file is skipped; a missing file is an error (`cmd/tui/input.go:23-50`). |

- Piped stdin is read, trimmed and put before the files and the first message (`input.go:55-65`). Extra messages run as follow-up prompts, in order.
- Unknown `--flag` is an error (exit 1).
- Exit codes: 0 done; 1 parse or setup error, and an assistant error in print mode; `--mode json` exits 0 on an assistant error (`headless_print.go:26-47`, `headless.go:416`, `AGENTS`/`CLAUDE` text); 130, 143, 129 on signals. Jobs still running after the last reply are stopped (`headless.go:188`, `jt.KillRemaining()`).
- Interactive (no `-p`, both ends a terminal): alternate-screen TUI against the leader; in-process agent only when the first connect fails (`cmd/tui/interactive.go`). Not a terminal on both ends: error "the interactive mode needs a terminal" (`interactive.go:59-62`), but `selectMode` sends non-terminals to print mode first.

### 3.2 `ask auth`

- Syntax (`auth_args.go:14-23`): `ask auth login --provider <id> --method <id> [--interaction browser|copy-code] [--new-account]`; `ask auth logout --provider <id> [--method <id>]`; `ask auth --help`.
- Providers: `anthropic`, `openai`, `xai`, `alibaba-token-plan`. Methods: `api-key`, `anthropic-oauth`, `openai-chatgpt`, `xai-oauth`.
- Rules (`auth_args.go:55-66`, `auth.go:41-60`): `--provider` required; `--new-account` only with `openai` + `openai-chatgpt`; `--interaction` only with `anthropic-oauth` and `browser|copy-code`; logout takes no login options; no positional arguments; without `--method` on a non-terminal stdin: error "noninteractive login requires --method"; on a terminal it asks.
- Runs without database or leader. Reads secrets from private stdin only (limit 16 KiB). Messages go to stderr (`Credential saved.`, `Local credential removed. Environment keys can still be used.`).
- Exit codes: 0, 1, 130 (private-input interrupt `auth.go:84`), signals 143 and 129.
- Env: `ASK_HOME`.

### 3.3 `ask acp`

- `ask acp` and `ask acp --help` only; any other argument is an error (`acp.go:24-48`).
- JSON-RPC frames, one per line, on stdin/stdout; diagnostics on stderr; no listener, no database; each session has its own agent; no credentials on the connection (`acp.go:30-34`).
- Env: `ASK_HOME`; `ASK_FAUX=1` lists the demo provider and makes it the first choice (`acp.go:116-126`); `ASK_FAUX_TPS`; `ASK_COMMAND_GUARD=1`.
- Exit codes: 0 input ended; 1 error or lost output; 130/143/129 (`acp.go:34,96-102`).

### 3.4 `ask leader`

- Syntax (`leader.go:26-44`): `ask leader` (foreground); `ask leader status [--json]`; `ask leader list [--json]`; `ask leader stop`; `ask leader --help`. Internal flag `--spawned-by-client` is set by a client that starts the leader (log goes to `<ASK_HOME>/leader.log`, rotated at 8 MiB: `leader.go:50-51`); it is not for users and is refused with a subcommand (`leader.go:83-84`). `--json` only with `status` and `list` (`leader.go:85-86`).
- Socket `<ASK_HOME>/leader.sock` (default `~/.ask`), mode 0600 in a 0700 directory, owner only (`leader.go:35-37`, `internal/leader/paths.go`).
- `status`, `list`, `stop` never start a leader. `stop` waits up to 15 s per step (`leader.go:54`) and uses SIGTERM only on Linux through a verified process handle (`leader.go:39-41`).
- Exit codes: the usage text says "0 stopped or nothing to do" (`leader.go:42`), but `status` exits **1** with "no leader is running" when none runs (`leader.go:131-135`); `list` and `stop` exit 0 in that case (`leader.go:174-176`, `:191-192`). Document the real behavior; the usage text is imprecise for `status`.
- Env: `ASK_HOME` (absolute path, short enough for a Unix socket path: `internal/leader/paths.go:114`).

### 3.5 `ask connect`

- Syntax (`connect.go:21-51`): `ask connect [--cwd DIR | --attach SESSION]`; `--cwd` and `--attach` exclude each other (`connect.go:79-81`); `--help`.
- Starts the leader when none runs; never falls back to its own agent; exit 1 with the reason when the leader is unreachable.
- Reads one line of stdin at a time. Commands (`connect.go:28-40`): plain text = prompt; `//text` = prompt that starts with `/`; `/new [DIR]`; `/sessions`; `/attach ID`; `/detach`; `/take`; `/cancel` (driver only); `/follow [EPOCH SEQ]`; `/unfollow`; `/answer ID OPTION`; `/quit`.
- Output: one line per event; first word is one of `connected, session, live, update, result, error, follow, event, resync, question, detached, bye` (`connect.go:47-49`).
- At end of stdin it waits for prompts in flight and leaves; leaving only detaches (the leader and its runs go on).
- Exit codes: 0 left; 1 error or lost leader; 130/143/129 (`connect.go:50`).

### 3.6 `ask version`

- `ask version` prints `ask <build>`; `ask version --json` prints one JSON line with `name`, `build`, `leaderProtocol`, `acpVersion`, and `commitTime` (VCS-stamped binary) or `devStamp` (unstamped binary, e.g. `go run`) (`cmd/tui/version.go:18-27,125-151`). `acpVersion` is 1 (`version.go:30`). Unknown argument: exit 1.
- `build` = first 12 characters of the VCS revision, `+dirty` when modified; else module version; else `dev` (`version.go:63-81`).

### 3.7 Interactive TUI slash commands (`internal/tui/completion.go:22-30`, `update_input.go:210-225`)

| Command | Purpose (completion text) | Note |
|---|---|---|
| `/model` | Select a model | lists usable providers' models only; saved as `defaultModel` in `settings.json` |
| `/providers` | Add or remove providers | needs the leader; the in-process agent says so (`needsLeaderText`) |
| `/new` | Start a new session in this directory | |
| `/sessions` | Watch another live session | needs the leader (`internal/tui/update_input.go:253-255`) |
| `/take` | Take control of this session | |
| `/copy` | Copy the last reply | native command first, then OSC 52 up to 75 KiB (`clipboard.go:28`) |
| `/quit` | Detach and quit | prints `ask: session <id>` after the terminal is restored (CLAUDE text; test `cmd/tui/exit_line_test.go`) |

Keys: see `internal/tui/keys.go` and `internal/tui/README.md`; do not copy them into `docs/cli.md` (link only).

### 3.8 Environment variables

| Variable | Used by | Effect | Evidence |
|---|---|---|---|
| `ASK_HOME` | all `ask` commands | Home directory (default `~/.ask`): `auth.json`, `settings.json`, `leader.sock`, `leader.log`. Must be absolute. | `internal/leader/paths.go:30-41`, `internal/settings/auth.go:66-76` |
| `ASK_FAUX` | leader, `ask acp`, in-process TUI | `1` lists the demo provider. `ask -p` has it as default and ignores the variable. | `cmd/tui/acp.go:20-22,120` |
| `ASK_FAUX_TPS` | faux provider | tokens per second; negative or non-number is an error | `cmd/tui/headless_faux.go:19,50-55` |
| `ASK_COMMAND_GUARD` | agents (`-p`, leader, `acp`) | `1` blocks a short list of destructive commands; off by default; not a security boundary | `cmd/tui/headless.go:269,344`; `pipeline.CommandGuard` |
| `ASK_CAPTURE` | `ask -p` | directory; records provider HTTP to a new cassette; ignored for `faux` | `cmd/tui/capture.go:19-52` |
| `TEA_TRACE` | interactive TUI | file for raw keyboard input; the TUI warns on screen | `cmd/tui/interactive.go:178`, `internal/tui/model_line.go:112`; do not set when you sign in |
| `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `XAI_API_KEY`, `ASK_ALIBABA_TOKEN_PLAN_API_KEY` / `ALIBABA_TOKEN_PLAN_API_KEY` | providers | API keys when no saved sign-in applies | `internal/providers/keys.go:44-53` |
| `ASK_RECORD=1`, `ASK_RECORD_MAX_REQUESTS`, `ASK_LIVE=1` | tests only | re-record cassettes; request limit (default 10); live smoke tests | `docs/testing-llm-cassettes.md:96-103`; `cmd/tui/headless_cassette_test.go:24-25`. Not product CLI; keep out of `docs/cli.md` or put under "tests". |
| `ASK_SESSION_ID`, `ASK_PROVIDER`, `ASK_MODEL`, `ASK_REASONING_LEVEL` | set by Ask in the environment of tool child processes | read-only for users | `cmd/tui/headless.go:300-303` |
| `FSFAULT_ENGINE`, `FSFAULT_SOURCE` | `internal/testsupport/fsfault/run-tests.sh` | optional fault tests | `internal/testsupport/README.md:47-55` |
| `ASK_AUTH_TIME_ADVANCE`, `ASK_AUTH_IGNORE_CANCEL` and similar | test helpers only | found in `*_test.go`; not product | grep |

Note: `ASK_RECORD` is not an `ask` command variable. The agent files list it next to `ASK_CAPTURE`; keep it under test commands only.

### 3.9 `cmd/server` (daemon)

- Flag: `-migrate` (apply migrations and exit); no other flags (`cmd/server/main.go:21`). Without it: apply migrations, then start HTTP and gRPC (`app.Module`).
- Env (loaded from `.env` if present, then the environment; optional `config.yaml` in the working directory): `HOST` (default `0.0.0.0`), `PORT` (8080), `GRPC_PORT` (50051), `DATABASE_URL` (`app.db`), `LOG_LEVEL`, `CORS_ORIGIN` (default `*`), `REDIS_URL`, `REDIS_ADDR` (`internal/config/config.go:37-47`). `OTEL_*` in `.env.example` are read by the unwired `otelexport` package only.
- Routes: `GET /`, `GET /health`, `/api/users` (GET, POST), `/api/users/:id` (GET, PUT, DELETE), `/api/posts` (GET, POST), `/api/posts/:id` (GET, PUT, DELETE) (`internal/http/health.go:19-20`, `users.go:23-27`, `posts.go:23-27`). gRPC: `Greeter.SayHello`, `SayHelloStream` (`proto/greeter.proto`).
- `internal/http/README.md:1` says "REST `/v1/*` handlers"; no `/v1` route exists (run: grep). Stale (section 6).

## 4. Changelog material

Format: Keep a Changelog 1.1.0, everything under `[Unreleased]`, no version invented. No git tags exist (run: `git tag` is empty). 43 commits; merge commits skipped; noise skipped: `4d2d29c initial commit` (kept only as the scaffold entry), `4c06f47`, `3c142a1`, `3ad7fe3`, `875ebb9`, `8ec0818`, `d8e2be0` (`update`), `4bde887` (`done T1`), `d9279ab` (`update roadmap`), plan/report-only commits (`90886f5`, `cbad18b`, `7ea97b4`, `25ebbea`, `2a57788`, `8f97953`, `81fabb7`).
Many features arrived inside `update` commits (for example `4c06f47` is the scaffold with `.golangci.yml`, `3c142a1` the docs and package READMEs). The plans below are the source for those.

Plan status check (frontmatter, run): completed: h7a, lifecycle, t0 gate, h13a, resize fix, h13b, connect-or-spawn, t1b job notices, session close, t1b altscreen, react loop, registry snapshot, scaffold. `in-progress` or `in_progress` in frontmatter but described as implemented in their journals and in `internal/tui/README.md:3`: t1a (`plans/261009-2239-t1a-local-tui`), t1c (`261010-1424-t1c-provider-setup`; one open item: a new OAuth sign-in run is "not recorded", phase 8 line 115), h5 (`261010-2349-h5-builtin-tools`; "phases 1 to 10 completed; two acceptance items open"), tui-e2e (`261007-1104-tui-e2e-automation`; open items G3 strict resize and iTerm2). `261012-0226-t1b-tool-output` is `superseded`. The plan statuses are stale against the code; the changelog follows the code.

### Added

- `ask -p "<prompt>"` runs one prompt without a terminal UI and prints the reply; `--mode json` writes every agent event as one JSON line. Evidence: `5847ca1`, `19dbeb9`, `64bec65`; `plans/261002-1419-h2-agent-loop-print-json/plan.md`; `cmd/tui/args.go`.
- `ask -p` accepts piped stdin, `@file` arguments, several messages, `--thinking`, `--model` and `--api-key`. Evidence: `cmd/tui/args.go:76-92`, `cmd/tui/input.go`.
- A built-in demo provider `faux` (default for `ask -p`) answers with a script, so you can try the harness with no account. Evidence: `b0b1f5e`; `cmd/tui/headless_faux.go`.
- Providers: Alibaba Token Plan, Anthropic, OpenAI and xAI through `--provider`. Evidence: `cmd/tui/args.go:195`, `docs/reference/providers.md`; Token Plan adapter first seen in `e2f07a1` (`internal/providers/tokenplan`, now `internal/providers/anthropic`); the remaining three arrive with h4/h7a (`plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`, "H4 is merged"). Exact commit of the h4 merge is inside an `update`/merge commit: use the roadmap as evidence.
- `ask auth login` and `ask auth logout` for API keys and for Claude Pro/Max, ChatGPT Plus/Pro and xAI Grok sign-in, with refresh of saved sign-ins. Evidence: `e991b5c`; `plans/261006-0157-h7a-subscription-auth/plan.md` (completed); `cmd/tui/auth_args.go`.
- The agent retries transient provider failures, can be cancelled, and recovers from failed tool calls. Evidence: `df1a27e`, `964a539`; `plans/261006-0933-lifecycle-event-pipeline-redesign/plan.md` (completed).
- `ask acp` serves the Agent Client Protocol v1 over standard input and output for editors. Evidence: `213afbe`; `plans/261007-0700-h13a-acp-stdio/plan.md` (completed).
- `ask leader` runs a local router on a Unix socket (`<ASK_HOME>/leader.sock`, owner only) that holds one agent for all local clients; `ask leader status|list|stop` manage it. Evidence: `bb1b9bc`; `plans/261008-1033-h13b-leader-unix-socket/plan.md` (completed).
- `ask connect` is a line-oriented client of the leader (sessions, attach, take, cancel, follow, answer). Evidence: `bb1b9bc`; `cmd/tui/connect.go:21-51`.
- `ask version [--json]` prints the build and protocol versions. Evidence: `bb1b9bc`; `cmd/tui/version.go`.
- `ask` with no arguments on a terminal opens a full-screen TUI that connects to the leader (and starts one if none runs); it falls back to an agent in the same process only if the first connect fails. Evidence: `8b13794`, `a54ae32`, `5b6a16a`; `plans/261009-2239-t1a-local-tui/plan.md`, `plans/261012-1337-t1b-grok-style-altscreen/plan.md` (completed).
- TUI: foldable blocks for tool calls, thinking and hooks, a full-screen block viewer, mouse wheel and click, drag-to-copy, `/copy`. Evidence: `5b6a16a`; `internal/tui/README.md:3`; `internal/tui/clipboard.go`.
- TUI slash commands `/model`, `/providers`, `/new`, `/sessions`, `/take`, `/copy`, `/quit`. Evidence: `internal/tui/completion.go:22-30`.
- `/providers` signs in to a provider (API key, browser URL, copy-code or device code) and removes a saved sign-in; with no usable provider the session starts with no model and says so. `/model` lists only models of usable providers and saves the choice. Evidence: `94a70d8`; `plans/261010-1424-t1c-provider-setup/plan.md`; `internal/auth/status.go:30-40`.
- Built-in tools `read`, `write`, `edit`, `ls`, `grep`, `find`, `bash`, `job_output`, `job_kill`, `job_list`, `monitor` for every agent, and a demo tool `echo`. Evidence: `plans/261010-2349-h5-builtin-tools/plan.md` (all ten phases marked completed, two acceptance items open); `docs/reference/tools.md`; `cmd/tui/headless.go:233-241`. Commit hash not available: the work sits in the uncommitted/`update` history of branch `new-session`. Verify with `git log --follow internal/tools/shell_bash.go` before release notes.
- A `bash` call that passes its wait budget (default 120 s, max 600 s) continues as a background job; a finished job tells the model through a `<system-reminder>` notice. Evidence: `internal/tools/shell_bash.go:21,23`; `plans/261012-0155-t1b-job-notices/plan.md` (completed).
- `ASK_COMMAND_GUARD=1` blocks a short list of destructive shell commands (not a security boundary). Evidence: `cmd/tui/headless.go:269,344`.
- `ASK_CAPTURE=<dir>` records the provider HTTP of an `ask -p` run to a cassette; tests replay recorded model replies. Evidence: `fc8dd22`; `docs/testing-llm-cassettes.md`; `plans/261005-2132-llm-cassette-testing/plan.md`.
- `python3 e2e/tui/run.py` drives the built `ask` in real terminal engines (Alacritty, Ghostty). Evidence: `03d8853`, `4bde887`; `e2e/tui/README.md`.
- Server scaffold: `cmd/server` serves HTTP (`/health`, users, posts) and gRPC (Greeter) on SQLite with embedded migrations; `-migrate` applies them and exits. Evidence: `4d2d29c`, `4c06f47`; `internal/app/app.go`. This is demo scope; say so in the entry.

### Changed

- The interactive TUI moved from inline output to the alternate screen. Evidence: `5b6a16a`; `docs/tui-architecture.md:8`.
- The Go toolchain requirement is now 1.27.0. Evidence: `b0b1f5e`; `go.mod:3`.
- A client of a newer build now replaces an older leader that a client started (sessions and background jobs of the old leader end); clients retry a failed leader start. Evidence: `plans/261011-2304-leader-connect-or-spawn-port/plan.md` (completed); `cmd/tui/leader_replace_e2e_test.go`.
- Bubble Tea v2 with a local patch is the TUI base. Evidence: `cfce15f`; `go.mod:421`. (Internal; list only if the changelog keeps a "dependencies" note. Recommended: omit.)
- Closing a session closes its jobs and frees its slot in the leader. Evidence: `plans/261012-0230-leader-session-close/plan.md` (completed). Verify the user-visible effect before writing it.

### Fixed

- A model the account cannot use is reported as its own error, not a generic failure. Evidence: `b07bba5`.
- TUI rows use the full width and session state races on reconnect are repaired. Evidence: `f62059b`.
- JSON mode no longer slows the run: stdout is buffered and flushed within 2 ms. Evidence: `64bec65`. (Fix of a feature in the same Unreleased block; list it under Added as a property of the feature, not under Fixed. Recommended: omit.)
- Terminal resize while output streams no longer corrupts the screen (inline renderer patch, later superseded by the alternate screen). Evidence: `plans/261007-1156-inline-resize-fix/plan.md`. Recommended: omit, because the inline mode is gone.

### Excluded: planned, not implemented (do not list)

- Remote model-catalog overlay (`docs/adr/0003-remote-model-catalog-overlay.md` is "accepted", but the catalog is compiled in; no overlay code: `internal/providers/catalog.go`, grep "remote|overlay" finds none).
- Durable session log, resume and continue (H8); session tree and branches. `internal/sessions` holds an in-memory typed log only (`internal/README.md`).
- Hooks runtime, MCP, skills loader, `AGENTS.md` discovery (`bootstrap`), permissions, tracing, cron, scheduler lanes, memory (all scaffold; section 2, hunk 3). `internal/tui/README.md:6` says hook blocks come from fixtures only.
- TUI extension host and public UI SDK (`internal/tuiext`, `pkg/askui`: scaffolds).
- WebSocket endpoint, read-only monitoring dashboard, daemon as ACP client of the leader (design intent only).
- Messaging (asynq), Redis cache, realtime (centrifuge), OpenTelemetry export, validation: packages exist, not composed in the server.
- Per-model `/model` features beyond the list; subagents; image input for models that lack it beyond the `read` tool.
- A new OAuth sign-in run against live providers (recorded as unverified: `plans/261010-1424-t1c-provider-setup/phase-08-real-provider-and-docs.md:115`).
- Terminal.app, iTerm2 and kitty compatibility (not hand-checked: CLAUDE text; `e2e/tui/README.md`).

Open point for the writer: the Added section is long for a first release. Group by area (CLI, Providers and auth, Agent, Leader and clients, TUI, Tools, Server, Testing) and keep one sentence each.

## 5. docs/ inventory

### 5.1 `docs/` files (for `docs/README.md`)

| File | Lines | Purpose | Audience |
|---|---|---|---|
| `docs/CONTEXT.md` | 95 | Glossary of provider-access terms (provider, model, access, auth method) that code and docs share. | contributor, agent |
| `docs/ask-architecture-reference.md` | 432 | Architecture: dewee package model, layers, package table, interfaces, wiring, process model (leader, ACP), import rules (section 8), where to put new code (section 9), reference example (section 11). | contributor, agent |
| `docs/testing-llm-cassettes.md` | 112 | The four test layers, how cassettes record and replay, `ASK_RECORD`, `ASK_CAPTURE`, native auth and capture limits. | contributor |
| `docs/tui-architecture.md` | 260 | TUI decisions: Bubble Tea, one root update loop, alternate screen, extension host boundary, validation gates. | contributor, agent |
| `docs/tui-style-guide.md` | 134 | Visual authority for the TUI: roles, palette, glyphs, block patterns (direction B4). | contributor, agent |
| `docs/adr/0001-provider-auth-composition.md` | 15 | One provider with several auth methods; one credential per provider. | contributor |
| `docs/adr/0002-minimal-login-in-provider-phase.md` | 23 | Minimal headless login ships with subscription access; TUI dialogs came later. | contributor |
| `docs/adr/0003-remote-model-catalog-overlay.md` | 13 | Accepted decision to refresh model metadata from a remote catalog. **Not implemented** (section 4, excluded). Needs a status line. | contributor |
| `docs/adr/0004-leader-drivers-output-and-sdk-copy.md` | 21 | Leader driver and take rules, output queues, version gate, patched ACP SDK copy. | contributor |
| `docs/reference/tools.md` | generated | Built-in tool reference (generated by `TestReferenceDocs`). Untracked at audit time. | user, agent |
| `docs/reference/providers.md` | generated | Provider and model reference (generated). Untracked at audit time. | user, agent |
| `docs/cli.md` | planned | CLI reference (this report, section 3). | user |

No file in `docs/` is aimed only at end users today; README.md carries user content. All ADRs are `status: accepted` front matter only; there is no date or number index.

### 5.2 Package READMEs (47 files; `find internal pkg -name README.md`)

| Path | Purpose (first paragraph) |
|---|---|
| `internal/README.md` | Package map, import rules, where to put new code |
| `internal/acp/README.md` | ACP adapter (server side) over the agent API |
| `internal/agent/README.md` | Agent: execution owner of one session |
| `internal/app/README.md` | Composition root: fx modules, bindings |
| `internal/auth/README.md` | Native login, logout, refresh, account access checks |
| `internal/bootstrap/README.md` | Prompt context files (`AGENTS.md` loader). Describes a loader; the package has no code |
| `internal/bootstrap/templates/README.md` | Embedded prompt template data folder (not a Go package) |
| `internal/bus/README.md` | Bounded replay and followers of the event stream |
| `internal/cache/README.md` | Redis client |
| `internal/channels/README.md` | Channel manager and `BaseChannel` (no code) |
| `internal/config/README.md` | Start-up config (viper, env names) |
| `internal/cron/README.md` | Cron expressions and due-job hand-off (no code) |
| `internal/crypto/README.md` | Parked credential encryption helpers |
| `internal/gateway/README.md` | Network door: HTTP, gRPC; also claims WS, dashboard (not built) |
| `internal/gateway/methods/README.md` | WS RPC handlers (no code) |
| `internal/hooks/README.md` | Lifecycle event dispatch (no code) |
| `internal/hooks/handlers/README.md` | Hook handler implementations |
| `internal/http/README.md` | REST handlers (claims `/v1/*`; code has `/api/*`) |
| `internal/jobs/README.md` | Per-session job table behind the shell tools |
| `internal/leader/README.md` | Local router process of `ask leader` |
| `internal/logs/README.md` | Zap logger factory |
| `internal/mcp/README.md` | MCP client bridge (no code) |
| `internal/memory/README.md` | Parked; no long-term memory |
| `internal/messaging/README.md` | asynq client and server |
| `internal/migrations/README.md` | golang-migrate runner |
| `internal/permissions/README.md` | Gateway RBAC decisions (no code) |
| `internal/pipeline/README.md` | Typed control contracts for the loop |
| `internal/providers/README.md` | Model access at the wire boundary |
| `internal/providers/acp/README.md` | ACP provider: subprocess agents as providers (client side) |
| `internal/realtime/README.md` | centrifuge node |
| `internal/sandbox/README.md` | Process runner of the shell tools |
| `internal/scheduler/README.md` | Planned run lanes |
| `internal/sessions/README.md` | Typed in-memory session log |
| `internal/settings/README.md` | Files under `~/.ask`: `auth.json`, `settings.json` |
| `internal/skills/README.md` | Skill discovery (no code) |
| `internal/store/README.md` | Models and store interfaces |
| `internal/store/gormstore/README.md` | GORM implementation of the stores |
| `internal/testsupport/README.md` | Shared test helpers; fsfault tests |
| `internal/tools/README.md` | What the model can call: declarations, registry, tool bodies |
| `internal/tracing/README.md` | Trace collector (no code) |
| `internal/tracing/otelexport/README.md` | OpenTelemetry SDK setup and OTLP export |
| `internal/tui/README.md` | Alternate-screen TUI: model, blocks, editor, keys |
| `internal/tuiext/README.md` | Scaffold: client-local UI extension host |
| `internal/validation/README.md` | Shared validator instance |
| `internal/workspace/README.md` | Session cwd, project root, trust store (in part) |
| `pkg/askui/README.md` | Scaffold: public UI author API |
| `pkg/protocol/README.md` | Shared message, event and wire contracts |

Directories with Go code and no README: `internal/tuiclient/`, `internal/tuiclient/rpc/`, `internal/providers/{anthropic,cassette,fantasykit,faux,openai,partialjson,sse}/`, `internal/testsupport/fsfault/` (C and shell, no Go), `internal/tui/testdata/` (data). The dewee rule "one owner README per package" is not met for these. Other docs: `e2e/tui/README.md` (runner), `.claude/skills/ask-tui-design/SKILL.md`.
"(no code)" means: no non-test `.go` file except `doc.go` (run: count per directory).

## 6. Other false or stale claims

| Doc | Claim | Problem | Evidence |
|---|---|---|---|
| `internal/README.md` package map | No row for `tuiclient` | package missing from the map | `ls internal/`; grep `tuiclient` in `*.md` |
| `internal/README.md:1-3,23` and `docs/ask-architecture-reference.md:3` | "Wire contract: `pkg/protocol` (WS)", binary `ask-server`, "HTTP (echo), WS and gRPC servers" | WS not built; the binary is built from `cmd/server` (named `server` by default); `ask-server` is a planned name | `internal/gateway/*.go`; `docs/ask-architecture-reference.md:3,69-80` |
| `internal/gateway/README.md:3` | Owns "the WS endpoint, the read-only monitoring dashboard"; "The daemon is an ACP client of the leader" | not built; label as planned | `internal/gateway` file list |
| `internal/http/README.md:1` | "REST `/v1/*` handlers" | routes are `/api/users`, `/api/posts`, `/health`, `/` | `internal/http/*.go` |
| `internal/{hooks,mcp,channels,tracing,permissions,cron}/README.md`, `bootstrap`, `skills` | Present tense ("The loader finds `AGENTS.md`...", "Trace collector...") with no "scaffold" marker | describe code that does not exist | no non-test `.go` besides `doc.go` in these dirs; marker counts: hooks 0, mcp 0, channels 0, tracing 0, permissions 0, cron 0 |
| `internal/tui/README.md:3` | "T1a, T1b ... and T1c are implemented" | true in code; the plans for t1a, t1c still say `in-progress` | plan frontmatter |
| `docs/adr/0003-remote-model-catalog-overlay.md` | accepted | no implementation; add "not implemented" note or status | section 4 |
| `docs/testing-llm-cassettes.md:87` | link to `../README.md#native-auth-and-headless-prompts` | breaks if the section moves | finding 1 |
| `docs/testing-llm-cassettes.md:100` | `ASK_LIVE=1` runs live smoke tests | true (`cmd/tui/headless_live_test.go`); not re-run | |
| `e2e/tui/README.md` vs `AGENTS.md`/`CLAUDE.md` | `--driver t0\|ask` (CLAUDE) | the runner has `--backend`, `--case` | `e2e/tui/run.py:1159-1162` |
| `plans/261012-1337-t1b-grok-style-altscreen`, `docs/tui-style-guide.md:9-10` | mention inline layout previews | labeled historical; fine | |
| `tasks/lessons.md:30` | `go run` start path | confirmed true; use it as evidence for the merged commands | `internal/leader/spawn.go:150-165` |
| `internal/README.md` "Where to put new code": "A builtin tool | `tools/<family>_*.go`" | matches `internal/tools/{file_read,shell_bash}.go`; true | |
| `README.md` and agent files: "`README.md` in each package" | 11 gaps | section 5.2 | |

Not verified in this audit (say so if a doc repeats them): the xAI device flow detail; the Alibaba "cannot check key" rule; hook/job semantics inside plans.

## Unresolved questions

1. Keep the `native-auth-and-headless-prompts` section in README.md (smallest change) or move it and retarget five links?
2. Should the merged agent file keep the long TUI sub-bullets, or point to `internal/tui/README.md` and `docs/cli.md`? (Ownership rule says link, not copy.)
3. `ask leader status` exits 1 when no leader runs while its usage text says "0 ... nothing to do". Treat as a doc note only, or fix the text in code? (Out of scope for a docs-only plan.)
4. Commit hash for H3/H4 (providers) and H5 (tools): not in `git log` subjects; changelog entries cite plans. Accept plan paths as evidence?
