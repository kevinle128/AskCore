# `internal/auth`

Native login, local logout, refresh, and account access checks belong here.
Credential files belong to [settings](../settings/README.md); final HTTP profiles belong to [providers](../providers/README.md).
This separation keeps rotating grants out of inference adapters.

[Service](service.go) owns resolution and shutdown registration; [login operations](login.go) own revision-checked replacement and local deletion.
[Native methods](http.go) connect the provider strategies to a separate private HTTP client.
[Callback handling](callback.go) owns loopback and manual authorization input.
[ChatGPT identity](identity.go) verifies signed identity through OIDC; [discovery](discovery.go) checks account access without replacing compiled model metadata.

[Status](status.go) lists the login options with their offline credential state; it never refreshes a token.
[Key check](keycheck.go) sends one read-only model-list request for the providers that allow it (OpenAI, Anthropic, xAI) and keeps only the HTTP status and a short provider code of a refusal.
Alibaba Token Plan has no such request, so its key is saved without a live check and the result says so.

[Model lists](model_list.go) give the models that the current credential of a provider can use. `Service.ListModels` resolves the credential like `Resolve` (refresh first; an environment key counts) and calls the `ListModels` function of the method. The state is `known` only for a 2xx answer that parses. Every other result is `unknown`, which permits inference, and its error holds only the HTTP status and a short provider code, for the log.
These methods have a list: OpenAI API key and ChatGPT sign-in, Anthropic API key (all pages; the state is `unknown` when the list does not end within 20 pages or a cursor repeats), xAI API key and xAI sign-in. `anthropic-oauth` and Alibaba Token Plan have none, and they send no request.
Ids and names that are not printable ASCII, hold `@`, or are longer than 128 bytes are dropped.
`AccountKey` makes the cache key from the provider, the method and an identity: the OAuth subject, or `settings.KeyedHash` of an API key. It never holds a token, a key or the credential generation. Take the salt from `ModelCache.Update`; with no valid salt an API key gives an empty key, which the caller must not cache.
[List requests](list_request.go) hold the one request builder that the key check, the ChatGPT access check and the model lists share. Their transport sends only a `GET` to the fixed list endpoint of one provider, with one credential header, no cookie, no body and no redirect.

[Broker](broker.go) runs sign-ins for clients of one leader: one sign-in or key save for each provider, a context that outlives the request that started it, input by channel, cancel before the commit gate, and a removal that wins over a running sign-in of another client.
It has no transport code and returns fixed outcome tokens, never provider text or a secret.
Call `Broker.Close` before `StopRefresh` and the refresh drain, because a running sign-in holds a refresh slot.

Reuse [NewNativeAuth](../app/auth_native.go) and [BindAuth](../app/module_auth.go) for runtime composition.
The resolver accepts an explicit supported key first, a saved record second, and an environment key only when no saved record exists.
An uncertain saved refresh requires explicit recovery and cannot select a different billing route.
Operating commands, account replacement, and remote-host callback guidance are in the [root README](../../README.md#native-auth-and-headless-prompts).

## Imports

Use standard library, OIDC identity verification, `internal/settings`, and provider core types.
Do not import agent, wire adapters, config, gateway, database stores, or transport handlers.
Providers receive a request-local [AuthSnapshot](../providers/auth.go) that excludes refresh and ID tokens and has no store handle.
Installed dependency versions are owned by [go.mod](../../go.mod).
