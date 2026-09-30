# `internal/gateway`

Transport entry points. It owns server lifecycle, middleware, CORS, rate limit, the WS method router and the gRPC service implementations.

## What belongs here

- HTTP server setup, middleware and the `RouteRegistrar` interface (`http_server.go`)
- WS server and method router
- gRPC server (`grpc_server.go`) and services (`grpc_<service>.go`, example: `grpc_greeter.go`)
- `Config` with the network settings (`config.go`)
- Servers expose `Start()` and `Stop(ctx)`; `internal/app` calls them from fx lifecycle hooks, so this package does not import fx
- Rate limit and client handling

## What does not belong here

| Code | Put it in |
|---|---|
| REST handler logic | `internal/http` |
| WS RPC method handlers | `internal/gateway/methods` |
| Database access | `internal/store` interfaces only |

## Main interfaces

- None required

## File names

`http_server.go`, `grpc_server.go`, `grpc_<service>.go`, `ws_*.go`, `router.go`, `ratelimit.go`

## Imports

- Allowed: `agent`, `store`, `bus`, `sessions`, `tools`, `permissions`, `gateway/methods`, `pkg/protocol`, `proto`, echo, gRPC. It does not import `internal/http`: handlers implement `gateway.RouteRegistrar`, and `internal/app` passes them in through the fx group `routes`.
- Denied: `internal/store/gormstore`, `gorm.io`, `database/sql`, `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
