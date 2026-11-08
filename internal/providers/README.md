# `internal/providers`

This package owns model access at the wire boundary.
A wire API is a protocol, a provider is a serving endpoint, and a model belongs to that provider.
Keep authentication methods separate from provider identity; [access terms](../../docs/CONTEXT.md) own this vocabulary.

## Source owners

| Responsibility | Owner |
|---|---|
| Provider and request contracts | [types.go](types.go), [model.go](model.go), [api.go](api.go) |
| Catalog and compatibility data | [catalog.go](catalog.go), [compat.go](compat.go) |
| Family rules: which model ids Ask can send a correct request to | [catalog_rules.go](catalog_rules.go) |
| Generated rows from the embedded models.dev snapshot | [generated_catalog.go](generated_catalog.go), [catalog_snapshot.json](catalog_snapshot.json) |
| models.dev mapping and download | [modelsdev.go](modelsdev.go), [modelsdev_fetch.go](modelsdev_fetch.go) |
| The merged model list of a session | [model_set.go](model_set.go) |
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

## Model list

The model list has three layers.
The first layer is the compiled catalog in `catalog.go`: hand-written rows that win over generated rows with the same provider, id and API.
The second layer is the generated catalog: models.dev rows from the embedded snapshot, and from a newer download that the cache keeps.
The third layer is the account list that the vendor gives for the signed-in account.
`NewModelSet` merges the layers into one immutable `ModelSet`; the app holds it and swaps it after a refresh.
`Find`, `AvailableModels` and `RecommendedModel` keep their contract over the compiled catalog only.

Every shown row must match a family rule in `catalog_rules.go`.
A rule sets the wire API, the base URL, the compat flags, the default output limit and context window, and the effort words that the adapter can send.
models.dev can only narrow the effort words and give limits and prices.
An id that matches no rule, or a rule that hides its family, gets no row, because Ask cannot build a correct request for it.
A context window or output limit of 0 means unknown for display; the request then uses the default of the rule.
Add a rule only after a live request proves the shape for that family.

A known account list keeps only the rows that it names, directly or through an alias or a dated snapshot.
An unknown list (not read, or refused) keeps all rows of the provider, because inference can still work.
[auth](../auth/README.md) reads the lists, and the [cache](../settings/README.md) keeps them; this package imports neither.

The user model configuration is the last layer (`model_config.go`).
The app maps the file to `ModelConfig` and gives it to `NewModelSet`; this package does not read files.
`ModelConfig` has no key and no header field, because a row of a `ModelSet` is copied to many places.
The order is: a provider `BaseURL`, then the custom rows, then the overrides.
A custom row needs an API and no family rule; omitted fields take the defaults 128000 context, 16384 output, text input, reasoning off and zero prices.
A custom row replaces the row with the same provider, id and API, and a known account list does not remove it.
An override on a built-in row can set the name, the limits, the price and the input types, and can only turn thinking levels off; it cannot set the API or the compat.
An override price (`PricePatch`) sets only the fields it names; the other fields come from the base price of the row, and a patch that sets no price keeps the price tiers.
A provider whose `BaseURL` the configuration changes gets no account filter, because its list tells the access at the default host.
`ModelSet.Problems` gives what did not apply, with schema paths and no file values, and `ModelSet.Origin` tells if the configuration made or changed a row.

`model_pattern.go` resolves the model names that a user types: `[provider/]id[@api][:level]`.
`ParseRef` is the one split of `provider/id@api`; the ACP name parser uses it too.
`ResolvePattern` tries the whole pattern as an exact name first, so an id such as `qwen3-coder:30b` wins over a level suffix.
A valid `:level` suffix is then split off; with no valid suffix, an alias match and then a fuzzy match run on the whole pattern.
Every exact step uses the rules of `Find`, so an id on two APIs, or in two providers, is `ErrAmbiguousModel` with the choices.
`ExpandScope` turns a scope list of patterns and globs into rows; an entry that matches nothing is a problem, not an error.

To update the embedded snapshot, run `ASK_UPDATE_CATALOG=1 go test -run TestCatalogSnapshot ./internal/providers`.
The test downloads models.dev, keeps the Ask providers and the fields that the mapper reads, and writes `catalog_snapshot.json`.
Without the variable the test sends no request.
After an update, run `ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui` to regenerate [the provider reference](../../docs/reference/providers.md).

## Preparation and recovery boundary

Preparation captures safe effective request values and the serving registration's retry policy.
This record is the reason exact request rebuild can agree with the adapter instead of duplicating adapter defaults.
Registrations without a preparer retain policy capture and compute adapter values at stream start.
The Prepared and PolicyOnly contracts are owned by prepare.go.

Adapters return typed failures rather than deciding Agent lifecycle transitions.
`Relay` wraps a stream, forwards every item unchanged and calls a hook with the result error before the terminal item reaches the consumer; its producer is the only goroutine, and it ends with the inner stream.
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
