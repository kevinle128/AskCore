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
