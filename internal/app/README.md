# `internal/app`

Composition root. fx modules that build every package, bind implementations to interfaces with `fx.As`, and map `config.Config` to the typed config of each package.

## What belongs here

- fx modules and lifecycle hooks

## Native headless composition

[NewNativeAuth](auth_native.go) is the ordinary constructor for native credential methods and the local settings store.
[BindAuth](module_auth.go) installs the same resolver for model readiness and each inference request; `AuthWait` owns shutdown drain.
[cmd/tui](../../cmd/tui/headless.go) uses these constructors without the database, gateway, or leader.
Keep this composition reusable by later fx runtimes instead of adding command-specific auth services.
Auth and inference use separate HTTP clients so capture cannot record token exchange, identity keys, or account discovery.

## Editor connection composition

[ACPModule](module_acp.go) provides `ACPRuntime` for `ask acp` from `ACPParams`: the native auth service (`NewNativeAuth`), the adapter callbacks and `Cleanup`, which stops the credential refresh and waits for `AuthWait`.
The module reads no configuration, opens no database and starts no listener. `fx.ValidateApp` in [module_acp_test.go](module_acp_test.go) proves that the graph stands alone.
The command supplies `NewAgent`, which must call `NewNativeAgent` with the given service, so all sessions share one credential resolver and direct headless runs keep their behavior.
`Authenticate` and `ModelAuth` only check readiness through `NativeAuthReady`; they never start a sign-in. The adapter gets function fields, never the service.

## Leader composition

[LeaderModule](module_leader.go) provides `LeaderRuntime` for `ask leader` from `LeaderParams`. It builds the editor composition (`NewACPRuntime`) once, sets `RequireRoute`, and puts one adapter and host behind one in-process byte link, with the `leader.Server` router on the other end. `Serve` starts the router and serves a listener that the command opened. The command (`cmd/tui/leader.go`) owns the lock, the socket, the log and the signals.
`Stop` runs in this order: the router stops taking clients and tells them, the link closes, the host disposes every session and waits for the started tool bodies (no time bound), while the credential refresh drains in parallel.
A client that leaves never reaches this path.
`ActiveRuns` and `QuiesceIfIdle` of the adapter feed the status and the idle shutdown. The tests with a real socket, the real router and the real host are in [leader_routing_test.go](leader_routing_test.go).

## What does not belong here

| Code | Put it in |
|---|---|
| Business logic | the capability package |

## File names

`app.go`, `module_<area>.go`

## Imports

- Allowed: every package
- Denied: none

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
