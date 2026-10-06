# Ask Architecture Reference

Ask (repository `AskCore`) is an agent harness. It runs as a local daemon or as a remote agent in the cloud. It has two binaries: `ask` (TUI, headless mode and the leader) and `ask-server` (the daemon with the network gateway). See section 7.3.

Ask uses the architecture of **dewee** (`github.com/nextlevelbuilder/dewee`, branch `dev`, commit `815cd9ea`): a modular monolith with packages by capability. Ask adds a small set of rules that close the dependency leaks found in dewee (section 8). Ask does **not** use Clean Architecture layers (user decision, 2026-09-30).

All statements about dewee were checked against its source code and give a `file:line` reference.

Out of scope: front-end and web UI for chat and control (dewee `ui/web`, `apps/*`, `internal/webui`, `extensions/`). One exception (user decision, 2026-09-30): a read-only local monitoring dashboard ("Watch it think"). The daemon serves it as embedded static files from `gateway`, bound to localhost with a token. It reads only the harness event stream. It has no chat and no control actions.

---

## 1. Architecture style

A **modular monolith with packages by capability**. Each capability (agent loop, pipeline, tools, providers, storage, transport, …) is one Go package. Ports and adapters are used **only at the edges**: LLM providers, chat channels, storage, sandbox and hooks have an interface, and each vendor is a separate implementation.

What Ask keeps from dewee:

| Principle | dewee evidence |
|---|---|
| Storage behind interfaces | Interfaces in `internal/store`, implementations in `internal/store/pg` and `internal/store/sqlitestore`. `store` never imports `pg`. |
| Interfaces at the edges | `Provider` (`internal/providers/types.go:54`), `Channel` (`internal/channels/channel.go:89`), `Sandbox` (`internal/sandbox/sandbox.go:185`), hooks `Handler` (`internal/hooks/dispatcher.go:23`), `SearchProvider` (`internal/tools/web_search.go:44`) |
| One composition root | All wiring in `cmd/` (`runGateway()` at `cmd/gateway.go:84`) |
| Core does not import transport | `agent`, `pipeline`, `tools`, `providers`, `store` import no `gateway` or `http` package. `pipeline` does not import `agent`. No core package imports `acp` or `leader`. |

What Ask changes (rules in section 8):

| dewee leak | Evidence | Ask rule |
|---|---|---|
| HTTP handlers use the database implementation directly | 12 files in `internal/http` import `store/pg`; 130 SQL call sites in `internal/http` | `http` and `gateway` call `store` interfaces only |
| Core packages read global config | `agent` (14 files), `tools` (8 files) and `pipeline/deps.go` import `internal/config` | Core packages receive typed config through constructors |
| Wiring by hand, about 1300 lines | `cmd/gateway*.go` | fx modules, assembled in `internal/app` |
| Optional dependencies through setter injection and type assertions | `*Aware` interfaces, `cmd/gateway_tools_wiring.go:179-185` | Constructor injection through fx is the default |

---

## 2. Layers

```
cmd/server, cmd/tui     Entry points
  │
internal/app            Composition root (fx)
  │
  ▼
Transport               gateway (HTTP echo, WS, gRPC servers, method router), http (REST handlers),
                        channels (chat platforms)
  │  pushes messages into bus, or acts as an ACP client of the leader
  ▼
Process model          leader (local router, client), acp (ACP adapter over the agent)
  │  ACP (JSON-RPC) over a Unix socket, a WebSocket or stdio
  ▼
Runtime core            agent (two-level loop, steer and follow-up queues)
                        ├─ pipeline (typed control points)
                        ├─ sessions (in-memory log; persistent tree planned), scheduler (planned lanes), bus (replay/follow), workspace
  │
  ▼
Capabilities            auth, providers, tools, mcp, skills, bootstrap, hooks, sandbox, tracing
Files under ~/.ask      settings (auth.json, settings.json)
  │
  ▼
Storage                 store (models + interfaces)  ◄──  store/gormstore (GORM implementation)

pkg/protocol            Wire contract shared with clients (frames, methods, events, errors)
proto/                  gRPC definitions (generated code only)
```

---

## 3. Packages

Go has no classes. A "class" is a struct with methods. The rule is **one package for each capability**. Inside a package, files are grouped by **filename prefix**, not by subfolder (dewee `internal/tools` has 178 non-test files and no subfolders).

| Package | Contains | dewee origin |
|---|---|---|
| `internal/app` | fx modules and lifecycle | `cmd/gateway*.go` |
| `internal/config` | viper start-up config, env overlay (a `daemon`/`cloud` mode setting is added when the first feature needs it) | `internal/config` |
| `internal/settings` | `~/.ask/auth.json` (credentials, mode 0600, file lock) and `settings.json` (user and trusted project) | |
| `internal/auth` | Native login, refresh, local logout, verified identity, and account access checks; see [package boundaries](../internal/auth/README.md) | |
| `internal/logs` | zap logger factory, runtime log ring | `cmd/gateway.go:106`, `internal/logs` |
| `internal/gateway` | HTTP (echo), WS and gRPC servers, method router, rate limit, gRPC service implementations (`grpc_*.go`) | `internal/gateway` |
| `internal/gateway/methods` | WS RPC method handlers | `internal/gateway/methods` |
| `internal/http` | REST `/v1/*` handlers. The package name is `http`, as in dewee; importers use the alias `httpapi` so it does not hide `net/http` | `internal/http` |
| `internal/agent` | single execution driver, input claims, tool coordinator, recovery, and typed Go API | `internal/agent` |
| `internal/acp` | ACP adapter over the agent's Go API, `_ask/*` methods | |
| `internal/leader` | local router on a Unix socket, `ConnectOrSpawn` client | |
| `internal/pipeline` | typed control contracts, around middleware, decisions, and scoped handler registries | `internal/pipeline` |
| `internal/providers` | Api, Provider and Model types, compat record, LLM vendors, registry; `acp/` for subprocess agents | `internal/providers` |
| `internal/tools` | tool registry, every builtin tool (no built-in approval policy) | `internal/tools` |
| `internal/mcp` | MCP client bridge | `internal/mcp` |
| `internal/skills` | SKILL.md metadata discovery, explicit reload | `internal/skills` |
| `internal/bootstrap` | `AGENTS.md` discovery and loading; `templates/` is a data folder for embedded `.md` templates (not a Go package) | `internal/bootstrap` |
| `internal/memory` | parked | `internal/memory` |
| `internal/sessions` | typed in-memory log; persistent tree and branch projection remain planned | `internal/sessions` |
| `internal/scheduler` | lanes and concurrency limits | `internal/scheduler` |
| `internal/bus` | bounded replay ring and followers; publication belongs to agent | `internal/bus` |
| `internal/hooks` | sync and notify event dispatch; `handlers/` for command and HTTP handlers | `internal/hooks` |
| `internal/permissions` | RBAC for gateway and HTTP callers | `internal/permissions` |
| `internal/sandbox` | isolated command execution | `internal/sandbox` |
| `internal/workspace` | session cwd, project root, project trust store | `internal/workspace` |
| `internal/cron` | scheduled agent runs | `internal/cron` |
| `internal/crypto` | AES-256-GCM helpers, parked for credentials | `internal/crypto` |
| `internal/channels` | channel manager, `BaseChannel`; one subfolder for each platform, added when needed | `internal/channels` |
| `internal/store` | data models and store interfaces, `Stores` aggregate | `internal/store` |
| `internal/store/gormstore` | GORM implementation of the store interfaces, DB connection | `internal/store/pg`, `sqlitestore` |
| `internal/tracing` | traces and spans for runs, LLM calls, tool calls; `otelexport/` for OTLP | `internal/tracing` |
| `internal/cache`, `messaging`, `realtime`, `validation`, `migrations` | Redis, asynq, centrifuge, validator, migration runner clients | infra clients in their own package, as in dewee |
| `internal/testsupport` | shared test helpers | |
| `pkg/protocol` | WS frames, leader frames, `_ask/*` methods, event types, error codes | `pkg/protocol` |

### 3.1 Third-party vendors

Ask has no `thirdparty/` folder. A vendor goes to the package of the capability that it serves:

| Vendor kind | Location | Example |
|---|---|---|
| LLM API | `internal/providers/<api>/`, one adapter for each wire API; a vendor is data (provider and model records) | `anthropic/`, `openai/`, `acp/`; shared fantasy plumbing in `fantasykit/` |
| Chat platform | `internal/channels/<vendor>/` | `channels/telegram/` |
| Backend of one tool | `internal/tools/<tool>_<vendor>.go` | `web_search_brave.go`, `web_search_tavily.go` |
| Infrastructure client | its own package | `cache/` (Redis), `messaging/` (asynq), `sandbox/` (Docker) |

### 3.2 Logging

`internal/logs` builds the zap logger. Packages receive `*zap.Logger` through their constructors. Security log lines use the prefix `security.`.

---

## 4. Where interfaces live

1. **A main interface is in the package that owns the concept**, usually in `types.go`: `tools.Tool`, `providers.Provider`, `agent.Agent`, `pipeline.Around`, `channels.Channel`, `hooks.Handler`, `sandbox.Sandbox`, `tracing.SpanExporter`.
2. **Storage interfaces are in `internal/store`**, one file for each entity (`<entity>_store.go`). The implementation is in `store/gormstore`. `store.Stores` collects all store interfaces. A large interface is made from small ones (dewee `AgentStore` at `internal/store/agent_store.go:912-973`), and a consumer asks for the smallest one that it needs.
3. **A small private interface is at the consumer**, lower-case, in the file that uses it (dewee `humanHandoffAgentStore` at `internal/tools/human_handoff.go:27`).

Create an interface only when there is a real second implementation or a test seam.

---

## 5. Shared behavior ("abstract" types)

| Pattern | Use it for | Example |
|---|---|---|
| Embedded base struct | Shared state and behavior for all implementations of one kind | `channels.BaseChannel` (dewee `internal/channels/channel.go:224`); `store.BaseModel` for ID and timestamps |
| Shared helper package | Code shared by sibling implementations | dewee `internal/store/base` |
| Optional capability interface | A feature that only some implementations have, found by type assertion | `tools.ConcurrencySafe`, `channels.StreamingChannel`, `providers.ThinkingCapable` |
| Registry | Select an implementation by name at runtime | `tools.Registry`, `providers.Registry` |

---

## 6. Models and mappers

**Default:** one model is used everywhere. The model in `internal/store` carries `json` and `gorm` tags, and it may use GORM types such as `gorm.DeletedAt`. The store implementation saves it, the core uses it, and the handler returns it to the user. There is no mapper.

A separate type and a mapper are necessary **only when the data is really different**:

| Case | Example | What to do |
|---|---|---|
| The handler must remove or change fields before it returns data to the user | hide an encrypted API key or internal flags | A response DTO in the handler file, with a mapper |
| The storage shape is different from the model | one model in several tables | A storage type in `store/gormstore`, for that model only |
| The type is foreign or generated | vendor SDK types, gRPC types from `proto/` | Map at the boundary |

Do not add a DTO or a mapper "for the future". Do not hide a field from the user with `json:"-"`; that tag removes the field from every JSON use (events, job payloads, cache). Use a response DTO.

Exception: the soft-delete marker `DeletedAt gorm.DeletedAt` carries `json:"-"`. GORM filters soft-deleted rows, so a model that leaves the store always has an empty `DeletedAt`, and no consumer loses data.

---

## 7. Wiring and runtime flow

### 7.1 Wiring

- Each package exports its constructors. `internal/app` puts them in fx modules and binds implementations to interfaces with `fx.Annotate(..., fx.As(new(Interface)))`.
- Value groups collect implementations of one kind: `group:"routes"` collects every HTTP handler as a `gateway.RouteRegistrar`; `group:"tools"` collects all `tools.Tool` values for the tool registry.
- Servers expose `Start()` and `Stop(ctx)`. `internal/app` calls them from fx lifecycle hooks, so only `internal/app` imports fx.
- `internal/app` maps `config.Config` to the typed config struct of each package.
- `cmd/server/main.go` only runs `fx.New(app.Module).Run()`, or applies migrations and exits when it gets `-migrate`.
- `internal/app/app_test.go` runs `fx.ValidateApp`, so a missing dependency fails in `go test`.

### 7.2 Runtime ownership and lifecycle

One Agent serves one session and owns one active execution driver.
Control handlers can change decisions; observers must not become a second execution owner.
The session log is the execution record, and the model context is a projection of its message entries.
SQLite persistence and branch selection remain roadmap work; the current writer is in memory.

| Boundary | Owner | Reason |
|---|---|---|
| Input admission, steering, follow-up, removal, abort, and disposal | [Agent API](../internal/agent/agent.go) and [input claims](../internal/agent/queue.go) | One owner prevents queued input from falling between a closing run and idle |
| Turn order and completion | [turn stages](../internal/agent/loop_stage.go) and [driver](../internal/agent/loop_run.go) | Lifecycle order stays visible in one driver rather than in extension callbacks |
| AdmitStep, PrepareRequest, ExecuteModel, RecoverModel, BeforeTool, ExecuteTool, AfterTool, CompleteStep, and StopTurn | [pipeline contracts](../internal/pipeline/points.go) and [dispatch](../internal/pipeline/registry.go) | Controls use typed inputs and explicit decisions; they do not own the loop |
| Model attempts and retry | [attempt owner](../internal/agent/loop_stream.go), [recovery](../internal/agent/recover.go), and [provider preparation](../internal/providers/prepare.go) | A retry uses the captured serving policy and preserves its billing binding |
| Tool bodies and uncertain outcomes | [coordinator](../internal/agent/tool_coordinator.go) and [repair](../internal/agent/tool_repair.go) | Started work must finish before the run settles; repair never repeats a tool |
| Log writes and exact request rebuild | [writer contract](../internal/sessions/writer.go) and [request log](../internal/agent/request_log.go) | The driver is the only writer and safe preparation values permit reconstruction without credentials |
| Ordered local observation and remote follow | [publication](../internal/agent/emit.go), [consistent follow cut](../internal/agent/follow.go), and [bounded ring](../internal/bus/follow.go) | Local callbacks and nonblocking followers have separate backpressure contracts |

Input uses the DeepSeek claim model.
A turn boundary claims all steering input; a cycle boundary claims all steering input and one follow-up.
AfterTool alone supplies added context that does not wake an idle Agent.
These claims belong to the Agent, not the scheduler.
The scheduler remains the planned owner of run lanes and concurrency limits.
Abort ends current work; disposal also closes future admission and waits for started bodies.

The external JSON contract keeps Pi's turn and retry projection.
A durable Ask turn can contain several model attempts, while Pi turn_start opens for each attempt.
Failed attempts record safe outcome, failure, usage, and binding facts rather than raw assistant content.
That content stays outside model history even when streamed output was already visible.
For current domain terms, dispatch constraints, and follow semantics, read the [Agent](../internal/agent/README.md), [pipeline](../internal/pipeline/README.md), and [protocol](../pkg/protocol/README.md) guides.
The [conformance matrix](../plans/261006-0933-lifecycle-event-pipeline-redesign/conformance-matrix.md) records tested behavior and deliberate upstream differences.
It is execution evidence; it does not replace these package boundaries.

### 7.3 Process model: leader, clients and headless mode (ACP)

User decisions, 2026-10-01. The design follows Grok Build (`xai-org/grok-build`, Apache-2.0, commit `2bdd1d6a`). It was checked in the Grok source; the `SH`, `PG` and `BIN` paths below are under `crates/codegen/xai-grok-shell/src`, `crates/codegen/xai-grok-pager/src` and `crates/codegen/xai-grok-pager-bin/src`.

**One external protocol: ACP (user decision, 2026-10-01).** The Ask agent speaks the Agent Client Protocol (ACP, <https://agentclientprotocol.com>), which is JSON-RPC 2.0 with JSON messages. ACP, plus Ask extension methods named `_ask/*`, is the **only** agent protocol between the agent process and anything outside it. Two links are not agent links and are out of this rule: the extension-host link to external extensions (roadmap X1, its own stdio contract) and internal service APIs such as gRPC health or admin (never prompting or tool execution).

| Consumer | Transport |
|---|---|
| TUI and daemon on the same machine | Unix socket to the leader |
| Remote agent (later) | WebSocket with a token |
| Editors (for example Zed) | stdio (`ask acp`) |

Inside the agent process there is no ACP. The loop, providers, tools, sessions, hooks and internal extensions call each other as Go packages. Hooks never travel over ACP; ACP has no sync-hook concept. Internal extensions run hooks as direct Go calls. External extensions run hooks over the X1 extension-host contract. The agent exposes a full, typed Go API. The ACP layer is a thin adapter over that API. Headless mode calls the Go API directly, because it runs in the same process.

Standard ACP covers only a small core: `initialize`, `authenticate`, `session/new`, `session/load`, `session/prompt`, `session/cancel`, `session/set_mode`, the reverse calls `session/request_permission`, `fs/*`, `terminal/*`, and `session/update` notifications. The harness needs much more (steer and follow-up queues, compact, fork and tree, usage and cost, `agent_settled`, retry and compaction events). These go through `_ask/*` methods and `_meta` fields, defined once in `pkg/protocol`. Grok does the same with about 314 `x.ai/*` names (for example `x.ai/queue/interject`, `x.ai/compact_conversation`, `x.ai/session/fork`).

Evidence from Grok: its leader connects to its agent with `acp::AgentSideConnection::new(agent, outgoing, incoming)` over in-memory pipes (`SH/agent/app.rs:948-964`). Messages from its remote relay WebSocket go into the same ACP pipe (`SH/agent/app.rs:998-1008`). Its leader frames and ACP payloads are JSON (`SH/leader/protocol.rs:83,125,133`). Grok uses protobuf only for internal service APIs (`xai-grok-tools-api/proto/grok-tools.proto`, a gRPC tools runtime) and for OTLP telemetry. Ask does the same: protobuf and gRPC (`proto/`) stay for Ask's own internal service APIs, never for the agent protocol. Grok's headless mode sends ACP messages to its in-process agent (`PG/acp/spawn.rs:338,364`); Ask's headless mode calls the Go API instead.

**Processes and roles.**

| Process | Command | Role | Holds an agent |
|---|---|---|---|
| Leader | `ask leader` (binary `ask`, `cmd/tui`) | Holds one agent instance for all local clients. Accepts clients on a Unix socket and connects them to the agent over ACP. | Yes, one instance |
| TUI | `ask` (binary `ask`) | Interactive terminal UI. An ACP client of the leader. | No (see "Fallback") |
| Headless | `ask -p "..."`, `ask --mode json` (binary `ask`) | Runs one prompt and exits. It builds its own agent in process and calls the agent's Go API directly (no ACP). It does not use the leader. | Yes, its own instance |
| Daemon | `cmd/server` | Network door: HTTP, gRPC and WS gateway, the monitoring dashboard, chat channels. An ACP client of the leader. | No |

```
 ask (TUI) ───────┐  ~/.ask/leader.sock                     ┌─── remote clients, web dashboard
                  │  frames that carry ACP                  │    (token, loopback, Origin check)
                  ▼                                         ▼
        ┌──────────────────────────────────┐        ┌──────────────┐
        │ ask leader                        │◄───────│  cmd/server  │
        │  leader server (route, fan-out)   │  ACP   │  (daemon)    │
        │   └─ACP over io.Pipe─► agent      │        └──────────────┘
        └──────────────────────────────────┘

 ask -p "..." ── Go API, direct call ──► agent in the same process (no ACP, no leader, no socket)
```

**The leader is a router, not a second agent.** The leader holds no agent logic. It does four things, as Grok's `run_leader_server` does (`SH/leader/server.rs`):

1. It accepts a client and does a handshake: `register` → `registered` → `leader_ready` when startup work (settings, auth) is done.
2. It forwards each client's ACP requests to the agent pipe. It prefixes each request `id` with the client id (Grok: `rewrite_request_id`, `server.rs:376-392`) so that ids of two clients do not collide.
3. It reads each ACP message that the agent writes. It routes responses back to the requesting client and restores the original id (`parse_response_id`, `server.rs:395`). It sends `session/update` notifications to every client that subscribes to that session.
4. It sends reverse requests to clients. It sends permission and question requests to all subscribers, and the first answer wins (`server.rs:506-507`). It sends other reverse requests only to the session's driver client.

A client that disconnects does not cancel the run. A client that reconnects asks for events after its last `seq`.

**Leader lifecycle.** Ask copies these rules from Grok:

- The TUI and the daemon call one function, `ConnectOrSpawn` (Grok: `connect_or_spawn`, `SH/leader/mod.rs:1376`). It connects to `~/.ask/leader.sock`. If there is no leader, it starts `ask leader` in the background and connects again.
- An flock on `~/.ask/leader.lock` holds the pid and makes sure that only one leader runs. The leader that gets the lock removes a stale socket. A leader that does not get the lock exits, and the client uses the leader that won.
- The leader detaches from the terminal (`Setsid`). Stdin and stdout go to `/dev/null`. Stderr is appended to `~/.ask/leader.log`, which rotates by size. A leader that a client starts gets a `--spawned-by-client` flag, so that cleanup never stops a leader that the user runs under a supervisor.
- `ask leader list | status | stop` manage the leader. `stop` sends a `shutdown` control frame first and uses SIGTERM through the pid file as a fallback.

Ask does **not** copy these Grok parts: zombie-leader eviction, the acquire-slot guard, the grok.com relay and auto-update relaunch.

**Security of the local socket.** Ask adds these rules. Grok does not have them: its leader code has no peer-credential check and no explicit socket permissions.

- `~/.ask` has mode 0700 and the socket has mode 0600.
- The leader rejects a peer whose UID is not the owner (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS).
- The leader rejects a client whose protocol version is different. The error tells which side must upgrade. A leader started by a client may be stopped and started again at the new version. Grok only warns.
- The local socket needs no token. The token, the loopback bind and the Origin check apply to the daemon's network gateway (roadmap H13).

**Fallback.** When the TUI cannot connect to the leader, it may build its own agent in process, as headless mode does. Grok does this (`PG/app/mod.rs:1107-1113`). The TUI uses one `AgentClient` interface with two implementations: `remote` (ACP over the leader socket, or over WebSocket to a remote agent) and `direct` (the in-process Go API). The UI code is the same for both.

**Concurrent credential writers.** The [credential transaction](../internal/settings/README.md#credential-transaction) owns cross-process auth writes on one authoritative local filesystem.
Session locking for concurrent headless and leader use remains planned under H8.

**Editors.** Because the agent speaks standard ACP, an ACP editor (for example Zed) can run Ask as an agent over stdio (`ask acp`), as Grok does with `grok agent stdio`.

**Where the code goes (planned).**

| Place | Content |
|---|---|
| `internal/agent` | The agent and its typed Go API. Internal events stay Pi-style. |
| `internal/acp` (new) | The ACP adapter over the Go API: implements the ACP `Agent` interface, maps internal events to `session/update`, puts Ask-only fields (`seq`, `runId`) in `_meta`, and serves the `_ask/*` methods. |
| `internal/leader` (new) | `server.go` (listen, handshake, id rewrite, routing, fan-out), `client.go` (`Connect`, `ConnectOrSpawn`), `lock.go` (flock, pid, paths), `spawn.go` |
| `pkg/protocol` | The leader frames (register, control, ACP payload), the `_ask/*` method and notification types, and the `_meta` fields. The standard ACP types come from an ACP Go SDK (roadmap decision). The `AgentClient` interface for the TUI and the daemon. |
| `internal/app` | fx modules: `AgentModule` (agent, providers, tools, sessions, settings, hooks), `LeaderServerModule`, `GatewayModule` |
| `cmd/tui` (`ask`) | `ask` = TUI + `AgentClient` (remote). `ask -p` = `AgentModule` + `AgentClient` (direct). `ask leader` = `AgentModule` + ACP adapter + `LeaderServerModule`. `ask acp` = `AgentModule` + ACP adapter on stdio. |
| `cmd/server` | `GatewayModule` + leader client |

---

### 7.4 Native credentials

The headless auth route is an Ask adaptation of Pi's interactive login and logout.
[cmd/tui](../cmd/tui/headless.go) dispatches it before prompt parsing or inference capture.
[NewNativeAuth](../internal/app/auth_native.go) composes the real local store and native protocols without database or leader setup.
[BindAuth](../internal/app/module_auth.go) connects one resolver to idle model readiness and each final model request.
The agent owns canonical messages and events; auth does not own a second agent loop.

Keep rotating grants, identity validation, and account discovery in [auth](../internal/auth/README.md).
Keep file transactions in [settings](../internal/settings/README.md) and final HTTP guards in [provider adapters](../internal/providers/README.md#request-authentication).
Providers receive a request-local snapshot with access material and destination binding, without refresh tokens, ID tokens, or a store handle.
Auth HTTP is private and separate from inference capture.

The product keeps one saved credential/account per provider and uses local-only logout.
Deleting a ChatGPT record also removes the saved issued client; no client registration is retained for later sign-in.
This policy does not establish compliance with all OpenAI account or session guidance.
See the [operating guide](../README.md#native-auth-and-headless-prompts) for method selection and callback forwarding, and the [storage guide](../internal/settings/README.md#credential-transaction) for refresh recovery and shutdown limits.

---

## 8. Import rules

`depguard` in `.golangci.yml` enforces these rules.

### 8.1 Rules kept from dewee (verified: zero matches in dewee)

| Package | Must not import |
|---|---|
| `tools`, `pipeline`, `providers`, `store` | `agent` |
| `store` | `store/gormstore` |
| `providers` | `tools` |
| `agent`, `pipeline`, `tools`, `providers`, `store` | `gateway`, `http`, `channels/<vendor>` |

Allowed: `tools` imports `providers`.

### 8.2 Rules added by Ask

| Rule | Denied imports |
|---|---|
| Handlers call store interfaces only | `http`, `gateway`, `gateway/methods` must not import `store/gormstore`, `gorm.io`, `database/sql` |
| One composition root | Only `internal/app` and `cmd/server` import `store/gormstore` |
| Core does not import adapters | `agent`, `pipeline`, `tools`, `providers`, `store`, `sessions`, `hooks`, `bus` must not import `acp`, `leader` |
| Config through constructors | Only `internal/app` and `cmd/*` import `internal/config`. Runtime settings and credentials are files that `internal/settings` owns |
| Providers receive resolved credentials | Providers must not import `internal/auth` or `internal/settings` |
| Settings uses standard library only | `internal/settings` must not import other internal packages or dependencies |

---

## 9. Where to put new code

| You are adding | Put it in | Implement or do |
|---|---|---|
| A new builtin tool | `internal/tools/<family>_*.go` | `tools.Tool`; add it to the `tools` fx group |
| A new backend of a tool | `internal/tools/<tool>_<vendor>.go` | the tool's backend interface (for example `SearchProvider`) |
| A new LLM vendor | its provider and model records (data); a new adapter `internal/providers/<api>/` only for a new wire API | `providers.Provider` for a new adapter |
| A new chat platform | `internal/channels/<vendor>/` | `channels.Channel`, embed `BaseChannel` |
| A new control handler | `internal/pipeline/<name>.go` | Register a typed handler with `pipeline.Registry`; stages stay private to agent |
| A new session entry type | `internal/sessions/entry.go` | |
| A new event type | `pkg/protocol` | Agent publishes lifecycle events; bus owns replay and follow; hooks owns the planned extension adapter |
| A new user setting or credential | `internal/settings/` | |
| Native login, refresh, or account access | `internal/auth/` | Reuse the app composition and existing provider adapters |
| A new ACP method or update mapping | `internal/acp/`, types in `pkg/protocol` | |
| A new persisted entity | model and interface in `internal/store/<entity>_store.go`, implementation in `internal/store/gormstore/<entity>.go`, SQL in `migrations/` | add the interface to `store.Stores` |
| A new REST endpoint | `internal/http/<resource>.go` | call `store` interfaces or core packages; DTO only under section 6 |
| A new WS RPC method | name in `pkg/protocol`, handler in `internal/gateway/methods/<resource>.go` | |
| A new gRPC service | `.proto` in `proto/`, implementation in `internal/gateway/grpc_<service>.go` | map generated types |
| A new hook handler | `internal/hooks/handlers/` | `hooks.Handler` |
| A new infrastructure client | its own package under `internal/` | constructor registered in `internal/app` |

---

## 10. Database and entry points

- **Database:** SQLite for both daemon and cloud mode (user decision, 2026-09-30). One migration folder, `migrations/`. PostgreSQL and its own migration folder are added only when cloud mode needs them.
- **Entry points:** two binaries (user decision, 2026-09-30).
  `cmd/server` is the daemon (`ask-server`); operational commands (for example `migrate`) are flags or subcommands of it.
  `cmd/tui` is the `ask` binary (user decision, 2026-10-01, Grok model).
  It has the interactive TUI, headless mode (`ask -p`), leader (`ask leader`), and native auth commands (`ask auth`).
  The TUI and the daemon are ACP clients of the leader.
  Headless mode calls the agent's Go API in process.
  See section 7.3.

---

## 11. Reference example

The users/posts demo is rewritten in this structure as a working example. Read it before you add a new entity.

| Part | Files |
|---|---|
| Models + interfaces | `internal/store/user_store.go`, `post_store.go`, `stores.go`, `errors.go` |
| GORM implementation | `internal/store/gormstore/user.go`, `post.go`, `stores.go`, `db.go` |
| SQL | `migrations/000001_create_users_posts.up.sql` / `.down.sql` |
| REST handlers | `internal/http/users.go`, `posts.go`, `health.go` |
| Servers and gRPC | `internal/gateway/http_server.go`, `grpc_server.go`, `grpc_greeter.go` |
| Wiring | `internal/app/app.go` |
| Tests | `internal/store/gormstore/store_test.go` (real SQLite), `internal/http/users_test.go` (fake store), `internal/gateway/grpc_greeter_test.go`, `internal/app/app_test.go` | The models use `gorm.DeletedAt` for soft delete directly, with no mapper. The schema comes only from `migrations/`; the demo does not call GORM `AutoMigrate`.
