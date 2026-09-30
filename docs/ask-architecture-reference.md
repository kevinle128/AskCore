# Ask Architecture Reference

Ask (repository `AskCore`) is an agent harness. It runs as a local daemon or as a remote agent in the cloud. One binary supports both modes. The configuration selects the mode.

Ask uses the architecture of **dewee** (`github.com/nextlevelbuilder/dewee`, branch `dev`, commit `815cd9ea`): a modular monolith with packages by capability. Ask adds a small set of rules that close the dependency leaks found in dewee (section 8). Ask does **not** use Clean Architecture layers (user decision, 2026-09-30).

All statements about dewee were checked against its source code and give a `file:line` reference.

Out of scope: front-end and web UI (dewee `ui/web`, `apps/*`, `internal/webui`, `extensions/`).

---

## 1. Architecture style

A **modular monolith with packages by capability**. Each capability (agent loop, pipeline, tools, providers, storage, transport, …) is one Go package. Ports and adapters are used **only at the edges**: LLM providers, chat channels, storage, sandbox and hooks have an interface, and each vendor is a separate implementation.

What Ask keeps from dewee:

| Principle | dewee evidence |
|---|---|
| Storage behind interfaces | Interfaces in `internal/store`, implementations in `internal/store/pg` and `internal/store/sqlitestore`. `store` never imports `pg`. |
| Interfaces at the edges | `Provider` (`internal/providers/types.go:54`), `Channel` (`internal/channels/channel.go:89`), `Sandbox` (`internal/sandbox/sandbox.go:185`), hooks `Handler` (`internal/hooks/dispatcher.go:23`), `SearchProvider` (`internal/tools/web_search.go:44`) |
| One composition root | All wiring in `cmd/` (`runGateway()` at `cmd/gateway.go:84`) |
| Core does not import transport | `agent`, `pipeline`, `tools`, `providers`, `store` import no `gateway` or `http` package. `pipeline` does not import `agent`. |

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
  │  pushes messages into bus, or calls the agent router
  ▼
Runtime core            bus → scheduler → agent (Router, Loop) → pipeline (stages)
  │
  ▼
Capabilities            providers, tools, mcp, skills, memory, bootstrap, hooks, sandbox, tracing
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
| `internal/config` | viper config, env overlay (a `daemon`/`cloud` mode setting is added when the first feature needs it) | `internal/config` |
| `internal/logs` | zap logger factory, runtime log ring | `cmd/gateway.go:106`, `internal/logs` |
| `internal/gateway` | HTTP (echo), WS and gRPC servers, method router, rate limit, gRPC service implementations (`grpc_*.go`) | `internal/gateway` |
| `internal/gateway/methods` | WS RPC method handlers | `internal/gateway/methods` |
| `internal/http` | REST `/v1/*` handlers. The package name is `http`, as in dewee; importers use the alias `httpapi` so it does not hide `net/http` | `internal/http` |
| `internal/agent` | `Loop`, `Router`, resolver, system prompt builder | `internal/agent` |
| `internal/pipeline` | `Pipeline`, `RunState`, one file for each stage | `internal/pipeline` |
| `internal/providers` | LLM providers, registry, adapters; `acp/` for subprocess agents | `internal/providers` |
| `internal/tools` | tool registry, policy, every builtin tool | `internal/tools` |
| `internal/mcp` | MCP client bridge | `internal/mcp` |
| `internal/skills` | SKILL.md loader, search | `internal/skills` |
| `internal/bootstrap` | system prompt files; `templates/` is a data folder for embedded `.md` templates (not a Go package) | `internal/bootstrap` |
| `internal/memory` | agent memory | `internal/memory` |
| `internal/sessions` | session key and manager | `internal/sessions` |
| `internal/scheduler` | lanes and per-session queue | `internal/scheduler` |
| `internal/bus` | in-process message bus: inbound, outbound, events | `internal/bus` |
| `internal/hooks` | hook dispatcher; `handlers/` for command and HTTP handlers | `internal/hooks` |
| `internal/permissions` | RBAC | `internal/permissions` |
| `internal/sandbox` | isolated command execution | `internal/sandbox` |
| `internal/workspace` | agent workspace resolver | `internal/workspace` |
| `internal/cron` | scheduled agent runs | `internal/cron` |
| `internal/crypto` | secret encryption at rest | `internal/crypto` |
| `internal/channels` | channel manager, `BaseChannel`; one subfolder for each platform, added when needed | `internal/channels` |
| `internal/store` | data models and store interfaces, `Stores` aggregate | `internal/store` |
| `internal/store/gormstore` | GORM implementation of the store interfaces, DB connection | `internal/store/pg`, `sqlitestore` |
| `internal/tracing` | traces and spans for runs, LLM calls, tool calls; `otelexport/` for OTLP | `internal/tracing` |
| `internal/cache`, `messaging`, `realtime`, `validation`, `migrations` | Redis, asynq, centrifuge, validator, migration runner clients | infra clients in their own package, as in dewee |
| `internal/testsupport` | shared test helpers | |
| `pkg/protocol` | WS frames, method names, events, error codes | `pkg/protocol` |

### 3.1 Third-party vendors

Ask has no `thirdparty/` folder. A vendor goes to the package of the capability that it serves:

| Vendor kind | Location | Example |
|---|---|---|
| LLM API | `internal/providers/<vendor>*.go` | `anthropic.go`, `openai.go`, `acp/` |
| Chat platform | `internal/channels/<vendor>/` | `channels/telegram/` |
| Backend of one tool | `internal/tools/<tool>_<vendor>.go` | `web_search_brave.go`, `web_search_tavily.go` |
| Infrastructure client | its own package | `cache/` (Redis), `messaging/` (asynq), `sandbox/` (Docker) |

### 3.2 Logging

`internal/logs` builds the zap logger. Packages receive `*zap.Logger` through their constructors. Security log lines use the prefix `security.`.

---

## 4. Where interfaces live

1. **A main interface is in the package that owns the concept**, usually in `types.go`: `tools.Tool`, `providers.Provider`, `agent.Agent`, `pipeline.Stage`, `channels.Channel`, `hooks.Handler`, `sandbox.Sandbox`, `tracing.SpanExporter`.
2. **Storage interfaces are in `internal/store`**, one file for each entity (`<entity>_store.go`). The implementation is in `store/gormstore`. `store.Stores` collects all store interfaces. A large interface is made from small ones (dewee `AgentStore` at `internal/store/agent_store.go:912-973`), and a consumer asks for the smallest one that it needs.
3. **A small private interface is at the consumer**, lower-case, in the file that uses it (dewee `humanHandoffAgentStore` at `internal/tools/human_handoff.go:27`).

Create an interface only when there is a real second implementation or a test seam.

---

## 5. Shared behavior ("abstract" types)

| Pattern | Use it for | Example |
|---|---|---|
| Embedded base struct | Shared state and behavior for all implementations of one kind | `channels.BaseChannel` (dewee `internal/channels/channel.go:224`); `store.BaseModel` for ID and timestamps |
| Shared helper package | Code shared by sibling implementations | dewee `internal/store/base` |
| Optional capability interface | A feature that only some implementations have, found by type assertion | `tools.AsyncTool`, `channels.StreamingChannel`, `providers.ThinkingCapable` |
| Registry | Select an implementation by name at runtime | `tools.Registry`, `providers.Registry`, `agent.Router` |

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

### 7.2 Runtime flow of one message

```
Telegram/Slack ─► channels/<vendor> (embeds BaseChannel) ─► bus.InboundMessage
WS / REST / gRPC ─► gateway ─► gateway/methods, http ──────────┐
                                                                ▼
        consumer (debounce, dedup) ─► scheduler (lanes: main / subagent / team / cron; one queue per session)
                                                                ▼
        agent.Router.Get(agentKey) ─► Loop.Run() ─► pipeline
            context → history → prompt → think (providers) → act (tools) → observe → memory → summarize
                                                                ▼
        bus.OutboundMessage ─► channels / WS event;  tracing records spans;  store writes to the database
```

Scheduler queue modes: `queue` (FIFO), `followup` (merge into the pending run), `interrupt` (cancel the active run).

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
| Config through constructors | Only `internal/app` and `cmd/*` import `internal/config` |
| The TUI is a client | `cmd/tui` imports `pkg/protocol` and `proto/` only, no `internal/*` package |

---

## 9. Where to put new code

| You are adding | Put it in | Implement or do |
|---|---|---|
| A new builtin tool | `internal/tools/<family>_*.go` | `tools.Tool`; add it to the `tools` fx group |
| A new backend of a tool | `internal/tools/<tool>_<vendor>.go` | the tool's backend interface (for example `SearchProvider`) |
| A new LLM vendor | `internal/providers/<vendor>*.go` | `providers.Provider` |
| A new chat platform | `internal/channels/<vendor>/` | `channels.Channel`, embed `BaseChannel` |
| A new pipeline stage | `internal/pipeline/<name>_stage.go` | `pipeline.Stage` |
| A new persisted entity | model and interface in `internal/store/<entity>_store.go`, implementation in `internal/store/gormstore/<entity>.go`, SQL in `migrations/` | add the interface to `store.Stores` |
| A new REST endpoint | `internal/http/<resource>.go` | call `store` interfaces or core packages; DTO only under section 6 |
| A new WS RPC method | name in `pkg/protocol`, handler in `internal/gateway/methods/<resource>.go` | |
| A new gRPC service | `.proto` in `proto/`, implementation in `internal/gateway/grpc_<service>.go` | map generated types |
| A new hook handler | `internal/hooks/handlers/` | `hooks.Handler` |
| A new infrastructure client | its own package under `internal/` | constructor registered in `internal/app` |

---

## 10. Database and entry points

- **Database:** SQLite for both daemon and cloud mode (user decision, 2026-09-30). One migration folder, `migrations/`. PostgreSQL and its own migration folder are added only when cloud mode needs them.
- **Entry points:** two binaries (user decision, 2026-09-30). `cmd/server` is the daemon; operational commands (for example `migrate`) are flags or subcommands of it. `cmd/tui` is a client that talks to the daemon over gRPC or WS.

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
