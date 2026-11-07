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
An uncertain saved refresh requires explicit recovery and cannot select a different billing route.

## Key order

`Service.ResolveWith` takes a `ResolveRequest`: the model row, the `--api-key` override and the `models.json` apiKey of the provider (`ConfigKey`, nil when the user file sets none). `Resolve(ctx, m, key)` is `ResolveWith` with no `ConfigKey`, so it keeps the earlier behavior.
One function, `selectSource`, holds the order for `ResolveWith` and for `KeySource`:

1. The override. It binds through a registered `api-key` method (or a fixed OpenAI or Token Plan endpoint), or through the `ConfigKey` when its endpoint is the row `BaseURL`. No other endpoint receives it. Its snapshot source is `override`.
2. The saved credential, when it binds to `(Provider, API, BaseURL)` of the row. A saved credential that does not bind is skipped only when the `ConfigKey` binds to the row; otherwise the result is `ErrMethod`. Thus a saved key never goes to a changed base URL.
3. The `models.json` key, only when `ConfigKey.Endpoint` is the row `BaseURL`. Its snapshot does not use the method table, so a provider that only the user file defines can bind. Its source is `config`. A value error stops the resolution; it does not fall through to the environment key.
4. The environment key, only when no saved record exists and a registered `api-key` method binds the row.

`KeySource` runs the same order as a dry run and returns a safe name: `override`, `saved:<method>`, `config:$NAME`, `config:command`, `config:literal` or `env:<NAME>`. A template with more than one variable names all of them in order (`config:$A,$B`). The dry run reads the store and the environment, but it never runs a command, never refreshes a token, never checks account access and never returns a secret.

## Value grammar

[Values](config_value.go) resolves a `models.json` value (an apiKey or a header value) with `Values.Expand`:

- A value that starts with `!` is a command: the rest of the string. `$!` at the start gives a literal `!`.
- `$NAME` and `${NAME}` read the environment (`[A-Za-z_][A-Za-z0-9_]*`). `$$` gives a literal `$`. Any other `$` form is an error.
- A variable that is unset or blank is an error that names the variable, never a value. Any other text is a literal.

`Service.Values` returns the one resolver of the service; it reads `Env`, `Now` and `RunCommand` of the service.
`RunCommand` comes from the composition root. It returns stdout, an `*ExitError` for a non-zero exit, and `ctx.Err()` after it stops the process when its context ends. With no runner, every command value fails.

The command cache is keyed by the command string and lasts for the process:

- The cache entry owns the run, on a 10 s timeout that no caller cancellation ends. Each caller waits on the run and on its own context; a caller whose context ends gets `ctx.Err()`, and the run continues. A caller cancellation is never cached.
- Trimmed stdout is cached. Empty stdout, a non-zero exit, a start error and the timeout are failures. After a failure, the same error returns for 30 s from the failure time (`Now`) with no new run; this is a time compare on the next call, not a timer.
- `Forget(command)` and `ForgetAll()` clear entries. The key is the command text without the leading `!`. A run that is still going when `Forget` is called gives its result to the callers that wait, but the cache keeps nothing.
- Stdout, stderr and the runner error text never go to a log or an error. An error says, for example, `command for local apiKey failed (exit 1)` or `command for local apiKey timed out`, and it matches `ErrValue`.
Operating commands, account replacement, and remote-host callback guidance are in the [root README](../../README.md#native-auth-and-headless-prompts).

## Imports

Use standard library, OIDC identity verification, `internal/settings`, and provider core types.
Do not import agent, wire adapters, config, gateway, database stores, or transport handlers.
Providers receive a request-local [AuthSnapshot](../providers/auth.go) that excludes refresh and ID tokens and has no store handle.
Installed dependency versions are owned by [go.mod](../../go.mod).
