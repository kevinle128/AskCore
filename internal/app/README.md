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
