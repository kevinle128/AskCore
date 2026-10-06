# `internal/auth`

Native login, local logout, refresh, and account access checks belong here.
Credential files belong to [settings](../settings/README.md); final HTTP profiles belong to [providers](../providers/README.md).
This separation keeps rotating grants out of inference adapters.

[Service](service.go) owns resolution and shutdown registration; [login operations](login.go) own revision-checked replacement and local deletion.
[Native methods](http.go) connect the provider strategies to a separate private HTTP client.
[Callback handling](callback.go) owns loopback and manual authorization input.
[ChatGPT identity](identity.go) verifies signed identity through OIDC; [discovery](discovery.go) checks account access without replacing compiled model metadata.

Reuse [NewNativeAuth](../app/auth_native.go) and [BindAuth](../app/module_auth.go) for runtime composition.
The resolver accepts an explicit supported key first, a saved record second, and an environment key only when no saved record exists.
An uncertain saved refresh requires explicit recovery and cannot select a different billing route.
Operating commands, account replacement, and remote-host callback guidance are in the [root README](../../README.md#native-auth-and-headless-prompts).

## Imports

Use standard library, OIDC identity verification, `internal/settings`, and provider core types.
Do not import agent, wire adapters, config, gateway, database stores, or transport handlers.
Providers receive a request-local [AuthSnapshot](../providers/auth.go) that excludes refresh and ID tokens and has no store handle.
Installed dependency versions are owned by [go.mod](../../go.mod).
