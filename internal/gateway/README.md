# `internal/gateway`

The daemon's network door (`ask-server`). It owns server lifecycle, middleware, CORS, rate limit, the WS endpoint, the read-only monitoring dashboard and the gRPC services. The daemon does not hold an agent: it is an ACP client of the leader (`internal/leader`) and forwards agent traffic to it.

## What belongs here

- HTTP server setup, middleware and the `RouteRegistrar` interface (`http_server.go`)
- WS server and method router
- gRPC server (`grpc_server.go`) and services (`grpc_<service>.go`, example: `grpc_greeter.go`)
- `Config` with the network settings (`config.go`)
- Servers expose `Start()` and `Stop(ctx)`; `internal/app` calls them from fx lifecycle hooks, so this package does not import fx
- Rate limit and client handling
- The WS endpoint that carries ACP JSON-RPC to the leader. Security: loopback bind by default, a client token from a 0600 file (or mTLS in cloud mode), and an `Origin` allow-list (never `*`)
- The monitoring dashboard: embedded static files on a separate `127.0.0.1` listener with a token and `Host`/`Origin` checks, fed by the leader event feed (`dashboard*.go`). It is read-only: no chat, no control
- gRPC for internal service APIs only (health, admin). Never for prompting or tool execution

## What does not belong here

| Code | Put it in |
|---|---|
| REST handler logic | `internal/http` |
| WS RPC method handlers | `internal/gateway/methods` |
| Database access | `internal/store` interfaces only |
| Agent logic, tools, sessions | the leader (`ask leader`), reached through `internal/leader` |
| ACP adapter over the agent | `internal/acp` |

## Main interfaces

- None required

## File names

`http_server.go`, `grpc_server.go`, `grpc_<service>.go`, `ws_*.go`, `dashboard*.go`, `router.go`, `ratelimit.go`

## Imports

- Allowed: `leader` (client side), `store`, `permissions` (role-based access for gateway methods), `gateway/methods`, `pkg/protocol`, `proto`, echo, gRPC. It does not import `internal/http`: handlers implement `gateway.RouteRegistrar`, and `internal/app` passes them in through the fx group `routes`.
- Denied: `internal/agent`, `internal/tools`, `internal/sessions` (the agent lives in the leader), `internal/store/gormstore`, `gorm.io`, `database/sql`, `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
