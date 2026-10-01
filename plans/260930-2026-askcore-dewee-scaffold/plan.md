# Ask scaffold: dewee model with Ask import rules

Status: DONE (2026-09-30). All phases complete; not committed.
Design: `docs/ask-architecture-reference.md`
Reference repo: `/Users/dale/Desktop/workspace/opensources/dewee` (branch `dev`, commit `815cd9ea`)
Note: the user CLAUDE.md asks for `tasks/todo.md`. The session hook allows markdown only in `plans/` and `docs/`, so the plan is here.

## Outcome

AskCore (the "Ask agent" harness) has the dewee package layout (packages by capability) plus the Ask import rules. Each package has a `doc.go` and a `README.md` that tell a coding agent what belongs there, the file prefix convention and the allowed imports. The users/posts demo and the gRPC greeter run on the new layout as working examples. Lint enforces the import rules.

## Constraints

- Layout, rules and names follow `docs/ask-architecture-reference.md`.
- Existing demo behavior stays: same REST routes (`/health`, `/`, `/api/users*`, `/api/posts*`), same gRPC greeter, same env var names in `.env.example`.
- `go build ./...`, `go vet ./...`, `go test ./...` and `golangci-lint run` pass at the end.

## Non-goals

- Front-end and web UI.
- The agent runtime itself (agent loop, pipeline, providers, tools, channels, MCP logic). Only folders, `doc.go` and `README.md` are created for these packages.
- PostgreSQL support.
- dewee product-only features (license, tenant transfer, fleet, bitrix, CCP/RGD, i18n, vault, knowledge graph, workflow, reflex, audio).

## Confirmed decisions (user, 2026-09-30)

| # | Topic | Decision |
|---|---|---|
| 0 | Architecture | B: dewee model (packages by capability) plus Ask import rules. No Clean Architecture layers. Clean Architecture was chosen first, then withdrawn by the user. |
| 0b | Models | One shared model with `json`/`gorm` tags. DTO or separate type + mapper only when the data is really different. |
| 0c | Out of scope | Front-end and web UI. Narrowed by the user on 2026-09-30: a read-only local monitoring dashboard is allowed (see `plans/260930-2254-pi-feature-inventory-go-roadmap/plan.md`). |
| 1 | Existing boilerplate | C: rewrite users/posts demo in the new layout as a working example. |
| 2 | Entry points | A: keep `cmd/server` (daemon) and `cmd/tui` (client). |
| 3 | Soft delete | Confirmed (user, 2026-09-30): keep soft delete, no mapper; the `store` model uses `gorm.DeletedAt` with `json:"-"` (design section 6 exception). The `observability` → `tracing/otelexport` move is also confirmed. |
| 4 | Database | A: SQLite for daemon and cloud. Rewrite `000001` instead of adding `000002` (no deployed DB yet). |

Defaults taken from dewee (not asked separately; the user can change them): vendors are placed by capability (no `thirdparty/` folder); `internal/tools` is flat with a filename prefix for each tool family.

## Precheck (project CLAUDE.md)

- `bunx create-better-fullstack@2.6.8 recipes check --json` (2026-09-30): `recipes: []`, no recipe applied. The CLI prints "Recipe verification failed" because the list is empty.
- `bunx create-better-fullstack@2.6.8 doctor --json`: 3 pass, 2 warn (missing `.env`; ecosystem checks skipped in JSON mode), 0 fail.
- No managed-region markers in the repository. The phrase appears only as text in `CLAUDE.md` and `AGENTS.md`.

## Findings in the current code (fixed by this work)

- `migrations/000001_create_users.up.sql` has only `users`, without `updated_at` and `deleted_at`, and no `posts` table. It does not match the models.
- `internal/config/config.go` (viper, `APP_` prefix, default port 3000) is not used. `cmd/server/main.go` reads `PORT`, `HOST`, `GRPC_PORT` directly with default 8080.
- `internal/handlers` uses the global `database.GetDB()`.
- `proto/greeter.go` holds a service implementation inside the generated-code package.
- `internal/database/database.go:20` runs GORM `AutoMigrate` next to golang-migrate. The rewrite keeps golang-migrate only.

## File moves and rewrites

| Current | New | Change |
|---|---|---|
| `internal/models/models.go` (User, Post) | `internal/store/user_store.go`, `internal/store/post_store.go` | Model + store interface in the same file; keeps `gorm.DeletedAt` |
| (none) | `internal/store/stores.go`, `internal/store/errors.go` | `Stores` aggregate; `ErrNotFound` |
| `internal/database/database.go` | `internal/store/gormstore/db.go` | Constructor that returns `*gorm.DB`, no global, no `AutoMigrate` |
| (none) | `internal/store/gormstore/user.go`, `post.go` | GORM implementations of the store interfaces |
| `internal/handlers/handlers.go` | `internal/http/users.go`, `internal/http/posts.go`, `internal/http/health.go` | Handlers take store interfaces through constructors; request structs (UserCreate, …) stay next to their handlers |
| `cmd/server/main.go` (echo setup) | `internal/gateway/http_server.go` | echo server, middleware, CORS, route registration, fx lifecycle |
| `proto/greeter.go` + `proto/greeter_test.go` | `internal/gateway/grpc_greeter.go` + test | `proto/` keeps only generated code |
| `cmd/server/main.go` (gRPC setup) | `internal/gateway/grpc_server.go` | gRPC server, fx lifecycle |
| `cmd/server/main.go` (`initLogger`) | `internal/logs/logger.go` | zap logger factory |
| `internal/config/config.go` | `internal/config/config.go` | Binds the existing env names (`HOST`, `PORT`, `GRPC_PORT`, `DATABASE_URL`, `LOG_LEVEL`, `CORS_ORIGIN`, `REDIS_URL`, `REDIS_ADDR`) so `.env.example` stays valid |
| `internal/observability/otel.go` | `internal/tracing/otelexport/otel.go` | Package change only. Not needed by the demo; included so the layout has no leftover package (confirmed) |
| `internal/app/container.go` | `internal/app/app.go` | fx modules for config, logs, store, gateway, http |
| `cmd/server/main.go` | `cmd/server/main.go` | Only `fx.New(app.Module).Run()`; `migrate` as a flag |
| `migrations/000001_create_users.*` | `migrations/000001_create_users_posts.*` | `users` and `posts` with all columns, SQLite |
| `internal/cache`, `messaging`, `realtime`, `validation`, `migrations`, `testsupport` | no change | Infrastructure clients in their own package, as in dewee |
| `cmd/tui` | no change | Already imports no `internal/*` package |

## Phases

### Phase 1: packages, docs, lint rules (DONE 2026-09-30)

- [x] `doc.go` + `README.md` for 29 new packages; `internal/bootstrap/templates` has a README only (data folder)
- [x] `README.md` for the existing packages: `app`, `config`, `cache`, `messaging`, `realtime`, `validation`, `migrations`, `testsupport`
- [x] README content: purpose; what belongs here and what does not; main interfaces with dewee reference; file names; allowed and denied imports; model rule; design link
- [x] `internal/README.md`: package map, import rules, where to put new code
- [x] `depguard` in `.golangci.yml`: 8 rules (design section 8)
- [x] Validation:
  - `go build ./...`, `go vet ./...`: pass
  - `golangci-lint run` (v2.12.2): the same 10 issues as before Phase 1, no new issue. The 10 issues are in `cmd/server/main.go`, `internal/migrations`, `proto/greeter*` and are fixed or removed in Phases 2 and 3.
  - Rule proof: one temporary bad import for each rule. All 8 rules reported the violation; the allowed imports in `internal/app` were not reported. Temporary files removed.

### Phase 2: move infrastructure code (DONE 2026-09-30)

- [x] `internal/logs/logger.go`: `New(level)`, same behavior as the old `initLogger` ("production" gives the production logger)
- [x] `internal/tracing/otelexport/otel.go`: moved with `git mv`, package renamed; usage notes moved to `doc.go`. No caller yet (the old package had none either)
- [x] `internal/store/gormstore/db.go`: `Open(dsn)` for SQLite, no `AutoMigrate`, no global
- [x] `internal/config/config.go`: flat keys; env names match `.env.example`; defaults match the old `main.go` (port 8080, gRPC 50051, CORS `*`); DB default `app.db`
- [x] `cmd/server/main.go`: uses `config.Load` and `logs.New` instead of `os.Getenv`; `gofmt` applied
- [x] Tests: `internal/config` (defaults, env names), `internal/logs`, `internal/store/gormstore` (in-memory SQLite)
- [x] Validation: `go build`, `go vet`, `go test ./...` pass; lint 7 issues (was 10: two errcheck and one gofmt in `main.go` fixed); smoke run with `PORT=18080 GRPC_PORT=50061`: `/health` OK, both ports listened, server stopped
- Deviation: `internal/database` stays until Phase 3, because `internal/handlers` still uses its global `GetDB()`. It is removed with the handlers in Phase 3, together with `AutoMigrate`.

### Phase 3: rewrite the demo (DONE 2026-09-30)

- [x] `internal/store`: `User`, `Post` (with `gorm.DeletedAt`, `json:"-"`), `UserStore`, `PostStore`, `Stores`, `ErrNotFound`
- [x] `internal/store/gormstore`: `UserStore`, `PostStore`, `NewStores`; `Update` uses `Omit(clause.Associations)` so it writes only its own row
- [x] `internal/http`: `UserHandler`, `PostHandler`, `HealthHandler`; each has `Register(*echo.Echo)`; not-found maps to 404, other store errors to 500
- [x] `internal/gateway`: `HTTPServer` (request logger with zap replaces the deprecated `middleware.Logger`), `GRPCServer`, `GreeterService`, `Config`, `RouteRegistrar`; servers have `Start`/`Stop`, no fx import
- [x] Migration `000001_create_users_posts` (users + posts, all columns, indexes); SQL embedded by `migrations/embed.go`; runner uses `iofs`
- [x] `internal/app/app.go`: fx module; migrations run before servers start; DB closed and logger synced on stop
- [x] `cmd/server/main.go`: `fx.New(app.Module).Run()`; `-migrate` applies migrations and exits
- [x] Removed `internal/handlers`, `internal/models`, `internal/database`, `internal/app/container.go`, `proto/greeter.go`; `go mod tidy` removed the postgres driver
- [x] Tests: migrations (idempotent), gormstore on real SQLite (CRUD, soft delete, author preload, update does not change author), HTTP handlers with a fake store, greeter over bufconn with `grpc.NewClient`, `fx.ValidateApp`
- [x] Lint: `proto/*.pb.go` excluded (hand-written bindings without a "Code generated" header). Result: 0 issues
- [x] Validation: `go build`, `go vet`, `go test ./...`, `golangci-lint run` pass. Smoke run on real ports: health, create/update/list, soft delete (row kept with `deleted_at`), 404 and 400 paths, gRPC port listening, clean stop. `server -migrate` exits 0 and creates both tables.
- Changes of behavior (on purpose):
  - `GET /api/users/:id` and `/api/posts/:id` return 500 (not 404) when the store fails for a reason other than not-found.
  - `PUT /api/posts/:id` returns the post with `author` loaded.
  - The server applies migrations at start (it used GORM `AutoMigrate` before).
- Correction during the work: I first said that GORM `Save` would overwrite the loaded author. A test without `Omit` passed, and the GORM source (`callbacks/associations.go`, `onConflictOption`) shows `ON CONFLICT DO NOTHING`. `Omit` stays to avoid the extra INSERT; the comment says this.
- Follow-up (not in scope): request bodies are not validated (the old `binding:` tags were for gin and had no effect in echo). `internal/validation` exists for this.

### Phase 4: runtime check and project docs (DONE 2026-09-30)

- [x] Runtime check (done at the end of Phase 3): health, create/update/list, soft delete, 404/400 paths, gRPC port, clean stop, `-migrate`
- [x] `CLAUDE.md` and `AGENTS.md`: new Architecture section, Project Structure and Common Commands (the two files stay identical except their own name)
- [x] `README.md`: Getting Started, Database Setup (SQLite only, embedded migrations), Architecture, Project Structure, Available Commands
- [x] Relative links in all package READMEs and `README.md` resolve

## Risks

| Risk | Mitigation |
|---|---|
| A depguard rule does not match as intended and lets a bad import through | Add one temporary bad import for each rule, confirm that lint fails, then remove it |
| Env var names change and break the local setup | config binds the existing names; `.env.example` stays the same |
| The SQLite GORM driver and the golang-migrate sqlite3 driver need CGO | Keep the drivers that the project already uses; check `go build` on this machine |

## Review

Outcome against the acceptance criteria:

| Criterion | Result |
|---|---|
| dewee package layout plus Ask import rules | Done. 29 new packages, 8 existing packages documented |
| Each package has `doc.go` + `README.md` | Done. 29 `doc.go`, 39 `README.md` (templates folder has README only) |
| Users/posts demo and gRPC greeter run on the new layout | Done. Unit tests and a smoke run on real ports |
| Lint enforces the import rules | Done. 8 depguard rules; each rule proved with a temporary bad import |
| `go build`, `go vet`, `go test ./...`, `golangci-lint run` pass | Done. Lint went from 10 issues to 0 |
| Existing REST routes, gRPC greeter and env names stay | Done, with the three intended behavior changes listed in Phase 3 |

Deviations from the plan:

- `.env.example`: the `DATABASE_URL` value changed from `file:../../local.db` to `app.db` (the name stays). The old value broke the new migration runner (`parse "sqlite3://file:../../local.db": invalid port`), so `cp .env.example .env` + start failed. Found in the final review. `internal/migrations/env_example_test.go` runs the migrations with the value from `.env.example`; it fails with the old value and passes with the new one. The Getting Started flow (`.env` from the example, `server -migrate`) was run and exits 0.

- `internal/database` was removed in Phase 3, not Phase 2, because the old handlers used its global.
- `proto/*.pb.go` are excluded from lint because they have no "Code generated" header. Regenerating them with buf needs the buf.build remote plugins.
- The server runs migrations at start in addition to the `-migrate` flag, to keep the old "works out of the box" behavior that `AutoMigrate` gave.

Open items:

- Request body validation for the demo (`internal/validation` exists but is not used).
- Nothing is committed yet; the work is on `master`.
