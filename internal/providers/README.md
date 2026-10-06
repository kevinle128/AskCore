# `internal/providers`

This package owns model access at the wire boundary.
A wire API is a protocol, a provider is a serving endpoint, and a model belongs to that provider.
Keep authentication methods separate from provider identity; [access terms](../../docs/CONTEXT.md) own this vocabulary.

## Source owners

| Responsibility | Owner |
|---|---|
| Provider and request contracts | [types.go](types.go), [model.go](model.go), [api.go](api.go) |
| Catalog and compatibility data | [catalog.go](catalog.go), [compat.go](compat.go) |
| One settled stream result and message assembly | [stream.go](stream.go), [assembler.go](assembler.go) |
| Typed failure codes and transport classification | [failure.go](failure.go), [errors.go](errors.go) |
| Serving registration and captured retry policy | [registry.go](registry.go), [retry.go](retry.go) |
| Safe effective preparation and real wire encoding | [prepare.go](prepare.go) |
| History conversion, interrupted replay, and tool declarations | [convert.go](convert.go), [transform.go](transform.go), [transcript.go](transcript.go) |
| Request-local credentials and safe binding | [auth.go](auth.go) |

Wire adapters are [anthropic](anthropic/) and [openai](openai/).
A vendor using an existing API is catalog and compatibility data, not a second adapter package.
Each adapter chooses its own libraries.
[fantasykit](fantasykit/) is adapter infrastructure; its types must not reach the core contract.
[Faux](faux/) supplies scripted product behavior for offline tests.
[Cassettes](cassette/) record and replay provider HTTP; see [test guidance](../../docs/testing-llm-cassettes.md).

## Preparation and recovery boundary

Preparation captures safe effective request values and the serving registration's retry policy.
This record is the reason exact request rebuild can agree with the adapter instead of duplicating adapter defaults.
Registrations without a preparer retain policy capture and compute adapter values at stream start.
The Prepared and PolicyOnly contracts are owned by prepare.go.

Adapters return typed failures rather than deciding Agent lifecycle transitions.
The failure codes and Retry-After parsing are owned by failure.go.
The Agent [recovery owner](../agent/recover.go) selects eligible retries and [sessions](../sessions/entry.go) owns their safe records.
Provider SDK retries stay disabled so one recorded Agent attempt does not hide multiple wire requests.

Interrupted replay must not execute tool calls from an incomplete answer.
The replay owner is transform.go, including unsigned thinking projected as plain text.
Visible failed output is not erased; the Agent's Pi retry events explain the next request.

## Request authentication

Providers receive resolved request-local credentials, never an auth service or store handle.
Keep credentials and account identity out of Prepared and session records.
Retry must pin method, profile, and billing class; token refresh can preserve that binding.
A subscription failure must not silently select a billed API-key route.
The [app resolver](../app/module_auth.go) owns the binding check and [auth](../auth/README.md) owns login and refresh.

Final HTTP guards belong in [Messages profiles](anthropic/profile.go) and [Responses profiles](openai/profiles.go).
They prevent access material from reaching the wrong destination or mixing with ambient headers.
Do not add a second subscription adapter for the same wire API.
The compiled catalog describes capabilities; account discovery separately establishes access.

## File names and imports

Keep core contracts in existing topic files and wire implementation in the API sub-package.
Use `<api>/<topic>.go` for real adapter boundaries; vendors remain data where the wire is shared.
Allowed imports are pkg/protocol and tracing.
Vendor SDKs and fantasy belong only in adapter packages and fantasykit.
Do not import tools, agent, settings, auth, transport packages, acp, leader, or config.
See [architecture import rules](../../docs/ask-architecture-reference.md#8-import-rules).
