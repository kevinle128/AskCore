# omp (oh-my-pi) user model configuration and model resolution

- Date: 2026-11-05. Roadmap item: H7.
- omp source: `/Users/dale/Desktop/workspace/opensources/oh-my-pi` at commit `efbc17154a30496f629143d331b7abae19516b96`.
- Upstream Pi source: `/Users/dale/Desktop/workspace/opensources/pi` at commit `4c6fb7cfe8c538a668726f6f8b3554098c39faee` (HEAD is the same as the pin, so Pi line numbers hold).
- Path prefixes: `ca/` = `oh-my-pi/packages/coding-agent/src/`, `ai/` = `oh-my-pi/packages/ai/src/`, `tui/` = `oh-my-pi/packages/tui/src/`, `pi/` = `pi/packages/coding-agent/src/`. omp docs = `oh-my-pi/docs/models.md`.
- Every claim below was checked against the current local files. GitNexus was not used.

## Outcome (read this first)

1. omp no longer uses `models.json` as the main file. The file is `~/.omp/agent/models.yml` (or `.yaml`). A legacy `models.json` is converted to `models.yml` one time. An explicit `.json`/`.jsonc` path still loads as JSONC.
2. omp has **no** `$NAME`, `${NAME}`, `$$`, or `$!` syntax. A value is either `!command`, or a bare string that omp first reads as an exact environment variable name and else uses as a literal. Upstream Pi has the template syntax and treats a bare string as a literal. This is the largest semantic difference for Ask.
3. Credential order in omp is: runtime `--api-key` > `models.yml` `apiKey` > stored OAuth > login-stored key > extension fallback key > provider environment variable > other stored key. The config key wins over stored OAuth on purpose.
4. `modelOverrides` and custom `models` are applied **after** cached and discovered rows on every rebuild, so explicit user fields win after a remote refresh.
5. A malformed file disables **all** custom providers. omp does not keep the last valid copy. The error goes to startup notices, `omp models`, and the model picker.
6. Ambiguity never raises an error in omp. A bare id that several providers carry is resolved by a preference ranking. Fuzzy and substring matches also pick silently.

Recommendation for Ask (ranked): (1) copy omp's composition order (catalog, then discovered, then custom models, then overrides) and its mtime-gated reload when `/model` opens; (2) copy upstream Pi's value grammar (`$NAME`, `${NAME}`, `$$`, `$!`, `!cmd`) instead of omp's "env-name-else-literal" rule; (3) do not copy omp's silent drop of the whole file on one error. Keep the last valid snapshot and show the error. Reasons are in "Weaknesses".

## Anatomy

| Component | File | Role |
|---|---|---|
| Config file handle | `ca/config/config-file.ts` | Path selection (`.yml` > `.yaml`, JSON migration), parse (Bun `YAML`/`JSONC`), schema check, per-instance cache |
| Models file binding and cross-field rules | `ca/config/models-config.ts` | `ModelsConfigFile`, `validateProviderConfiguration`, unknown compat key warnings |
| Schema | `ca/config/models-config-schema-bundle.ts` | omptype schema for `providers.<id>{...}` |
| Value resolver | `ca/config/resolve-config-value.ts` | `!command` execution and cache, env-or-literal, header composition |
| Custom model builder | `ca/config/custom-models.ts` | Overlay build, defaults from a bundled reference row or local defaults |
| Registry | `ca/config/model-registry.ts` | Load, merge, overrides, reload gate, `getError()` |
| Credential cascade | `ai/auth/cascade.ts` | Key precedence and config-value resolution |
| Resolver | `ca/config/model-resolver.ts` | Pattern grammar, matching, scope (`--models`/`enabledModels`), `resolveCliModel`, default pick |
| Suffix grammar | `tui/overlays/model-selector.ts`, `tui/thinking.ts` | `splitThinkingSuffix`, accepted levels |
| CLI flags | `ca/cli/flag-tables.ts` | `--provider`, `--model`, `--models`, `--thinking`, `--api-key` |
| Startup selection | `ca/main.ts`, `ca/sdk.ts` | CLI model, scope, session restore, default role, fallback |
| Settings keys | `ca/config/model-settings.ts` | `enabledModels`, `modelRoles`, `modelProviderOrder`, `cycleOrder` |
| Picker reload | `tui/overlays/model-picker.ts`, `tui/overlays/model-browser.ts` | `refreshIfStale()` on open, error display |

## Q1. User models file

**Location.** The registry opens `<agentDir>/models.yml` (`ca/config/model-registry.ts:461`). `ConfigFile` derives a `.yaml` fallback and a `.json` migration source from a `.yml` path (`ca/config/config-file.ts:144-146`). It reads `.yml` first, then `.yaml` (`config-file.ts:187-195`). If neither YAML file exists and `models.json` exists, it parses the JSON with `JSONC.parse` and writes `models.yml` (`config-file.ts:29-54`). It does not delete `models.json`. A `.json`/`.jsonc` path passed by code is read without migration (`config-file.ts:150-153`). The model DB cache sits next to the file (`model-registry.ts:462-463`).

**Format and comments.** YAML files go through `YAML.parse`; JSON files go through Bun `JSONC.parse` (`config-file.ts:239-243`). YAML allows `#` comments. JSONC allows comments; I did not run a trailing-comma test (see Limitations). Upstream Pi reads JSON only, after `stripJsonComments`, which removes `//` line comments and trailing commas but not `/* */` blocks (`pi/core/model-config.ts:297`, `pi/utils/json.ts:1-6`).

**Schema.** The provider schema has `baseUrl`, `apiKey`, `api`, `headers`, `compat`, `remoteCompaction`, `authHeader`, `auth` (`apiKey|none|oauth`), `discovery`, `models[]`, `modelOverrides{<id>: ...}`, `disableStrictTools`, and Bedrock fields (`ca/config/models-config-schema-bundle.ts:339-360`). An empty `apiKey` string is rejected (`models-config-schema-bundle.ts:381-382`). The full list of model fields is in omp docs `models.md:49-95` and `:295-298`.

**Validation library.** `@oh-my-pi/omptype`, omp's own "ArkType-compatible runtime schema validation" package (`oh-my-pi/packages/omptype/package.json:3-5`; import at `models-config-schema-bundle.ts:1`). Schema errors become `ConfigError` with dotted paths (`config-file.ts:247-255`). Cross-field rules run after the schema (`models-config.ts:197-224`):
- With `models` present: `baseUrl` is required, `apiKey` is required unless `auth` is `none` or `oauth`, and `api` is required on the provider or on each model (`models-config.ts:73-109`).
- Without `models`: at least one override field must be present (`models-config.ts:52-71`).
- `discovery` needs a provider `api` except for `proxy` (`models-config.ts:90-92`).
- `contextWindow` and `maxTokens` must be positive on custom models (`models-config.ts:116-123`).
Upstream Pi uses `typebox` `Compile` (`pi/core/model-config.ts:4-5, 245`).

**Merge with built-ins by id.** Custom models merge by provider plus id (`model-registry.ts:1191-1215`). On a match, omp patches the existing row with the "replace" transport policy, so the custom `api` and `baseUrl` replace the built-in values (`model-registry.ts:1195-1206`). Without a match it builds a new model with defaults (`model-registry.ts:1207`).

**Defaults for a new custom model.** omp first looks for a bundled reference row with the same id (`ca/config/custom-models.ts:119-121`). A field that the user omits comes from that reference row, and else from a local default:
- `cost`: reference, else all zero (`custom-models.ts:122-125`).
- `input`: reference, else `["text"]` (`custom-models.ts:126`).
- `reasoning`: reference, else `false` (`custom-models.ts:135`).
- `contextWindow`: reference, else `128000` (`custom-models.ts:143`).
- `maxTokens`: reference, else `16384` (`custom-models.ts:144`).
- `name`: else the id (`custom-models.ts:130`).
An explicit `cost` also removes inherited time-based pricing (`custom-models.ts:157-162`). Upstream Pi uses the same local defaults without the reference lookup in this function (`pi/core/provider-composer.ts:212-226`).

**Reload trigger.** The registry stores the file mtime after each load (`model-registry.ts:946`). `#reloadStaticModels` returns early when the mtime did not change (`model-registry.ts:808-814`). `refreshIfStale()` waits for any background refresh, then runs an offline refresh only when the mtime changed (`model-registry.ts:582-593`). The model picker calls `refreshIfStale()` each time it opens, unless a `--models` scope is active (`tui/overlays/model-picker.ts:197-210`). A reload clears config-sourced keys from the credential store first, so a removed `apiKey` stops working (`model-registry.ts:843-846`).

**Malformed file.** A schema or cross-field error returns empty custom models, overrides, and discovery providers with the error (`model-registry.ts:1594-1606`). The registry stores it (`model-registry.ts:928`). omp does not keep the last valid copy: the reload path clears all state before it loads (`model-registry.ts:839-855`). The error is shown at startup (`ca/main.ts:2481-2488`), in `omp models` as "custom providers disabled" (`ca/cli/models-cli.ts:225-231`), and in the picker (`tui/overlays/model-browser.ts:386-387`). Test: `ca/../test/model-registry.test.ts:2022-2027` checks that an invalid file gives a schema error and that the custom model is missing. Unknown `compat` keys only warn (`models-config.ts:152-195`, `model-registry.ts:1619-1625`).

## Q2. Value syntax for secrets and headers

**omp grammar.** There are only two forms (`ca/config/resolve-config-value.ts:104-108`):
- `!command`: runs `command` with `/bin/sh -c` in the project directory, with a 10 s timeout, in a detached process group (`resolve-config-value.ts:65-70, 116-146`). Stdout is trimmed; empty output or a failure gives no value (`resolve-config-value.ts:140-142`).
- Anything else: `$envExact(value)` (exact-case `process.env` lookup, `oh-my-pi/packages/utils/src/env.ts:358-366`); if it is unset or empty, the string itself is the value (`resolve-config-value.ts:106-107`).

There is no `$NAME`, `${NAME}`, `$$`, or `$!` handling in this path. The registry installs this one resolver for all credentials (`model-registry.ts:464`), and the cascade uses it (`ai/auth/cascade.ts:58-61, 126-128`). The default resolver in `ai` has the same env-or-literal rule (`cascade.ts:22-28`).

**When it is evaluated.**
- Env and literal values are resolved again on each call, so they are live (`resolve-config-value.ts:100-103`).
- Command output is cached for the process lifetime. Concurrent calls share one run. A failure blocks retries for 30 s (`resolve-config-value.ts:4-8, 54-98`).
- Headers are resolved at the request boundary by a composed async resolver; later layers win; `authHeader: true` adds `Authorization: Bearer <apiKey>` (`resolve-config-value.ts:171-203`).
- Cache invalidation is per value or global (`resolve-config-value.ts:30-52`). Tests show that a 401 reruns a command-backed key or header (`test/model-registry-command-values.test.ts:203, 334, 385, 437`). Docs state that an ordinary catalog refresh does not invalidate it (omp docs `models.md:245`); the refresh code only re-mints credentials on request (`model-registry.ts:476-479`).

**Precedence with the credential store.** omp keeps credentials in SQLite `agent.db`, not `auth.json` (`oh-my-pi/packages/utils/src/dirs.ts:912-914`). The order in `KeyCascade.get` is (`ai/auth/cascade.ts:320-395`):
1. Runtime override, for example `--api-key` (`cascade.ts:327-330`).
2. `models.yml` `apiKey` (`cascade.ts:337-340`). The registry installs it in this tier, not the fallback tier (`model-registry.ts:1714`). The comment at `cascade.ts:332-336` gives the reason: a user who points a provider at a gateway supplies that gateway's key, and an upstream OAuth token would fail there.
3. Stored OAuth credential, with refresh (`cascade.ts:344-351`).
4. Stored key from `/login` (`cascade.ts:352-367`).
5. Extension "fallback" config key (`cascade.ts:375-376`; set by `setConfig(..., {fallback: true})`, `cascade.ts:93-101`).
6. Provider environment variable such as `OPENAI_API_KEY` (`cascade.ts:378-379`).
7. Other stored keys, for example a broker copy (`cascade.ts:380-393`).

So the order is runtime > models file > stored > env, not runtime > stored > models file > env. Stored keys also pass through the resolver (`cascade.ts:365, 391`). Test: `test/runtime-provider-login-key-precedence.test.ts` and `test/model-registry-command-values.test.ts:175` ("runtime API keys win without executing configured credential commands").

## Q3. Overrides against the remote or refreshed catalog

Explicit user fields win after a refresh. The base stage combines bundled models, cached rows, and runtime-discovered rows (`model-registry.ts:1075-1099`). The projection stage then merges custom `models`, then extension overlays, then applies `modelOverrides` (`model-registry.ts:1119-1131`). Every snapshot goes through this projection (`model-registry.ts:1144-1152`). Overrides keyed by a retired variant id still bind (`custom-models.ts:189-209`).

The test `test/model-discovery.test.ts:1110-1158` puts a cached Ollama row with `maxTokens: 32768`, refreshes online, and expects the `modelOverrides` value `maxTokens: 4096` (`model-discovery.test.ts:1157`). That is the field that proves the override wins; the `contextWindow` value also comes from discovery, so it proves nothing.

One limit: `#mergeResolvedModels` lets a discovered row keep the older `contextWindow`/`maxTokens` only when the new row omits them (`model-registry.ts:1176-1188`). This is for discovered over bundled rows, not for user overrides.

## Q4. Model resolution

**Grammar.** The pattern can be:
- `provider/id`, matched exactly and case-insensitively (`ca/config/model-resolver.ts:706-721`).
- A bare id, matched exactly and case-insensitively (`model-resolver.ts:751-760`). Raw ids that contain `/` or `:` (OpenRouter) are tried as ids before the provider split (`model-resolver.ts:744-750`).
- A retired effort-tier alias such as `X-thinking` (`model-resolver.ts:767-781`).
- `provider/<fuzzy>`: fuzzy match inside that provider (`model-resolver.ts:790-830`).
- A substring of id or name, with a preference for alias ids (no date suffix) over dated ids, then the highest dated id (`model-resolver.ts:833-861`).
- A trailing `:level`. Accepted levels are `off`, the catalog efforts, and `auto` (`tui/thinking.ts:9`); `max` and `auto` are allowed in selectors (`tui/overlays/model-selector.ts:20, 44`). The whole pattern is first matched exactly, so a literal id that ends in `:max` wins; then the suffix is removed before any fuzzy match, so `kimi-for-coding:high` cannot match `kimi-for-coding-highspeed` (`model-resolver.ts:907-955`). An invalid suffix gives a warning and the default level (`model-resolver.ts:961-987`), except in CLI resolution, which turns that fallback off (`model-resolver.ts:2359-2366`).
- A trailing `@upstream` for OpenRouter and Vercel Gateway only (`model-resolver.ts:995-1014`).
- Role aliases such as `@smol`, through `modelRoles` (`model-resolver.ts:1950-1968`, `2267-2321`).
- Globs (`*`, `?`, `[`) in `--models` and `enabledModels` only. They use `Bun.Glob` on `provider/id` and on `id`, lower case (`model-resolver.ts:131-137, 1930-1948`); a glob may carry `:level` (`model-resolver.ts:139-175`).

**Ambiguity.** omp never returns an "ambiguous" error. Several exact bare-id matches go to `pickPreferredModel`: recently used model, then `modelProviderOrder` or catalog provider priority, then recently used provider, then deprioritized providers last, then registry order (`model-resolver.ts:588-620`). A provider-qualified selector does not move to a different provider that has the same flat id (`model-resolver.ts:753-759`; tests `test/model-resolver.test.ts:742-835`).

**Main functions.** `parseModelPattern` (`model-resolver.ts:1017-1029`), `matchModel` (`:738`), `resolveModelScope` (`:1892-2005`), `resolveCliModel` (`:2168-2204`), `pickDefaultAvailableModel` (`:86-124`). `resolveCliModel` order: exact reference or authenticated flat id, with or without a `:level` suffix (`:2237-2265`), then a configured role name (`:2267-2321`), then provider-prefixed exact (`:2323-2352`), then pattern match on the provider or available models, then on all models (`:2359-2366`). A selector that names a disabled provider is refused (`:2180-2203`).

**CLI flags** (`ca/cli/flag-tables.ts`): `--provider` (`:134-136`), `--model` (`:137-139`), `--api-key` (`:170-172`), `--models` split on commas (`:191-193`), `--thinking` validated against the effort list (`:205-214`). Also `--smol` and `--slow` set role models (`:140-145`). omp docs call `--provider` legacy (omp docs `models.md:593`).

**Startup selection (actual path).**
1. `--model`: `resolveCliModel`; if found, it pins the model and records it as the `default` role for the session (`ca/main.ts:1406-1452`). If not found and no provider was given, resolution is deferred until extensions register (`main.ts:1430-1437`); else exit 1.
2. Scope from `--models`, else `enabledModels` (`main.ts:992-1002`). If not resuming, the saved `default` role is chosen when it is in scope, else the first scoped model (`main.ts:1453-1497`).
3. Session restore when continuing (`ca/sdk.ts:1945-1965`).
4. The `default` role in `modelRoles`, resolved against the allowed models (`sdk.ts:1904, 1931-1936`), and again after extensions and discovery (`sdk.ts:3102-3124, 3144-3155`).
5. `pickDefaultAvailableModel`: the first provider default from `DEFAULT_MODEL_PER_PROVIDER`, with providers that have concrete credentials first (`model-resolver.ts:86-124`, `sdk.ts:3127-3165`).

`findInitialModel` (`model-resolver.ts:2409-2483`) has no caller in `src` or `test`, and `src/index.ts` does not export `model-resolver`. omp docs still describe it as the order (omp docs `models.md:614-620`).

**Settings keys.** omp has no `defaultModel` or `defaultProvider` key. It uses `modelRoles` (`model-settings.ts:109`), `enabledModels` with path scopes (`:63-68`), `enabledProviders`/`disabledProviders` (`:70-82`), `modelProviderOrder` (`:120`), `cycleOrder` (`:122`), and `defaultThinkingLevel` (`:27`). Upstream Pi has `defaultProvider`, `defaultModel`, and `enabledModels` (`pi/core/settings-manager.ts:135-136, 167`).

## Q5. Differences from upstream Pi (4c6fb7c)

| Area | Upstream Pi | omp |
|---|---|---|
| File | `~/.pi/agent/models.json` (`pi/config.ts:565-581`) | `~/.omp/agent/models.yml`, JSON migrated (`config-file.ts:29-54`) |
| Parse | JSON after `//` and trailing-comma strip (`pi/core/model-config.ts:297`) | YAML or Bun JSONC (`config-file.ts:239-243`) |
| Schema | typebox (`pi/core/model-config.ts:4-5`) | omptype (`models-config-schema-bundle.ts:1`) |
| Value grammar | `$NAME`, `${NAME}`, `$$`, `$!`, `!cmd`; bare string is a literal (`pi/core/resolve-config-value.ts:28-86, 138-151`) | `!cmd`, else env name, else literal (`resolve-config-value.ts:104-108`) |
| Missing env | Template gives `undefined`; `...OrThrow` names the missing variable (`pi/core/resolve-config-value.ts:101-113, 228-251`) | Falls back to the literal string (`resolve-config-value.ts:107`) |
| Command | Sync `execSync`; caches failures for the process lifetime (`pi/core/resolve-config-value.ts:208-216`) | Async, 30 s failure backoff, dedup, 401 invalidation (`resolve-config-value.ts:54-98`) |
| Custom defaults | Fixed defaults (`pi/core/provider-composer.ts:212-226`) | Bundled reference row first, then the same defaults (`custom-models.ts:117-156`) |
| Exact bare id on many providers | Returns `undefined`, then substring match picks by id sort (`pi/core/model-resolver.ts:126-165`) | Preference ranking (`model-resolver.ts:588-620`) |
| Glob | `minimatch` (`pi/core/model-resolver.ts:14, 318`) | `Bun.Glob` (`model-resolver.ts:131-137`) |
| Default model | `defaultProvider` + `defaultModel` (`pi/core/settings-manager.ts:135-136`) | `modelRoles.default` (`sdk.ts:1904`) |

## Q6. Weaknesses and bugs

1. **One error drops all custom providers.** There is no last-valid snapshot (`model-registry.ts:839-855, 1594-1606`). A typo while `/model` is open removes custom models from the picker until the user fixes the file. Severity: medium for Ask, because an agent session can lose its model at the next reload.
2. **Literal fallback for an unset env variable** (`resolve-config-value.ts:107`). `apiKey: MY_KEY` with `MY_KEY` unset sends `Bearer MY_KEY`. omp documents this (omp docs `models.md:550-553`), but it still sends the variable name as a secret and gives a confusing 401. Do not copy.
3. **Stored keys are references too** (`cascade.ts:365, 391`). This is intentional (`cascade.ts:121-123`), but a stored literal that starts with `!` or equals an env variable name is reinterpreted. Low practical risk.
4. **Silent ambiguity.** Bare ids and fuzzy patterns never ask or warn (`model-resolver.ts:751-760, 833-861`); a new provider login can change which model `--model gpt-x` picks, because provider-usage rank is part of the order (`model-resolver.ts:604-608`).
5. **Unused and stale code and docs.** `findInitialModel` has no caller but has `process.exit(1)` inside a library function (`model-resolver.ts:2436-2438`), and its comment lists a session-restore step that it does not implement (`model-resolver.ts:2402-2408`). omp docs describe it as the live order (omp docs `models.md:614-620`). The CLI error text still says "add models to models.json" (`model-resolver.ts:2217`).
6. **Complexity.** `model-resolver.ts` is 2617 lines and `model-registry.ts` is 3509 lines; upstream Pi's resolver is 783 lines. Much of the extra code exists to fix startup races between discovery, extensions, and the default role (`sdk.ts:3088-3103, 3131-3143`). Ask should keep one resolution pass after all providers register, so that it does not need these retries.

## Test files that cover resolution and config (omp, `packages/coding-agent/test/`)

- `model-resolver.test.ts` — `pickDefaultAvailableModel`, `parseModelPattern`, thinking suffixes, OpenRouter colons, provider lock, preference order, roles.
- `cli-model-role-thinking.test.ts` — CLI role selectors with thinking levels.
- `main-model-scope-notification.test.ts` — scope notices.
- `repro-issue-1022-disabled-default-model.test.ts`, `default-model-ambient-bedrock.test.ts` — default model fallback.
- `sdk-model-selection.test.ts`, `sdk-deferred-role-discovery.test.ts` — startup selection and deferred roles.
- `config/models-config-validation.test.ts` — auth rules, compat booleans, kind rules.
- `models-config-lazy-validator.test.ts` — deferred schema.
- `model-registry.test.ts`, `config/model-registry.test.ts` — merge, overrides, invalid file (`model-registry.test.ts:2005-2028`).
- `model-registry-command-values.test.ts` — `!command` keys and headers, cache, backoff, 401 refresh.
- `config-value-fd-inheritance.test.ts` — command process stdio.
- `runtime-provider-login-key-precedence.test.ts` — key precedence.
- `model-registry-default-config.test.ts`, `model-registry-create.test.ts`, `model-registry-lazy-loading.test.ts`, `model-registry-cache-headers.test.ts`, `model-registry-runtime-provider.test.ts`, `model-registry-runtime-cleanup.test.ts` — registry life cycle.
- `model-discovery.test.ts` — discovery, `refreshIfStale` (`:315-337`), override after discovery (`:1110-1158`).
- `oh-my-pi/packages/tui/test/model-picker-native.test.ts`, `test/model-picker.test.ts` — picker refresh on open.

## Limitations

- I did not run omp or its tests. Bun `JSONC.parse` trailing-comma support is from the API name, not from a test run.
- I did not trace upstream Pi's full credential precedence (`pi/core/model-runtime.ts`); the Pi column covers syntax, parse, defaults, and matching only.
- The shared remote catalog (`catalog.stencil.so`) and per-provider discovery details were out of scope except where they meet user overrides.
- GitNexus was not used; all citations come from the current local checkouts.

## Unresolved questions

1. For Ask, should a bare string mean a literal (Pi) or an env name (omp)? This report recommends Pi's rule, but it is a user decision because it changes how existing Pi users' files behave.
2. Should Ask use YAML like omp or JSON with comments like Pi? This report does not rank the two; the parse step is small either way.
3. Should an ambiguous bare id in Ask fail with a list of choices, or use a preference rank like omp?

Status: DONE
Summary: omp loads `~/.omp/agent/models.yml` (migrated from `models.json`), resolves values as `!cmd` or env-name-else-literal (no `$NAME` grammar, unlike Pi), ranks the models-file key above stored OAuth, applies user overrides after discovery, and drops all custom providers on one file error. Its resolver matches exact, alias, fuzzy, and substring patterns with `:level` and globs, and picks silently on ambiguity.
