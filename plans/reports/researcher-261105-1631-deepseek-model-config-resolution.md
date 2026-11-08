# DeepSeek harness: model catalog, provider configuration, and model resolution (input for Ask H7)

Source: `/Users/dale/Desktop/workspace/opensources/deepseek-harness` at commit `5badb15009ae1756c3afe0ae0cef1faafc290ccc`. Paths are relative to that checkout. Pi comparison anchors use `/Users/dale/Desktop/workspace/opensources/pi` at `4c6fb7cfe` (prefix `pi:`). Date: 2026-11-05. Method: direct reads and `rg`; no scripts were run.

## Bottom line

1. DSH has **two independent model catalogs**, one per adapter family. The multi-provider catalog is the compiled catalog that ships inside the npm package `@earendil-works/pi-ai@0.87.1` (Pi's own library). The DeepSeek catalog is a hand-written list of two models. DSH has **no remote catalog fetch, no cache, and no refresh**. To get new catalog data, you must upgrade the pi-ai dependency.
2. User model configuration is **not a separate `models.json`**. It is the `providers` dictionary of the `llm-pi-ai` plugin row in the profile patch `$DSH_HOME/profiles/<profile>/cordis.patch.yml`. Its fields are almost the same as Pi's `models.json` (`api`, `baseURL`, `headers`, `models`, `modelOverrides`, `compat`). DSH adds stricter validation, `reasoningEfforts` wire maps, and route-level fallbacks.
3. DSH has **no model-id grammar**. A selection is a struct `{provider, model, reasoningEffort?}`. DSH has no `provider/model` string, no aliases, no fuzzy match, no `:thinking` suffix, and no `--model` CLI flag. Users select models in the web picker or through ACP config options.
4. Unknown-model behavior **is different on each route**. pi-ai routes fail with `UNKNOWN_MODEL`. The DeepSeek routes accept any id and give it guessed metadata. The web entry point requires catalog membership. Core routing does not require it.
5. Error handling is careful but **not one policy**. A changed provider is validated strictly before the write. A stored provider that went bad keeps diagnostics for each model, and its other models continue to work. The DeepSeek route fails closed. The credentials file keeps the last good snapshot.

Recommendation for Ask H7 (ranked):
- (1) Use Pi's `models.json` shape, which DSH confirms. Add DSH's strict write-time validation, the explicit `reasoningEfforts` level map, and drift gates.
- (2) Keep Ask's planned models.dev overlay with ETag and offline fallback. DSH has nothing in this area, so do not copy DSH for it.
- (3) Do not copy DSH's lack of an id grammar. Ask needs `--model` and a `/model` text entry, so Pi's resolver is the better reference.

## Anatomy

| Component | File | Role |
|---|---|---|
| pi-ai profile schema | `packages/llm/llm-pi-ai/src/config.ts` | `providers` dict schema, defaults, `resolveProfiles` strict/deferred, `assertServiceable` |
| pi-ai catalog materializer | `packages/llm/llm-pi-ai/src/catalog.ts` | Merges installed catalog + profile per field; compat/level drift gates; per-model diagnostics |
| pi-ai adapter | `packages/llm/llm-pi-ai/src/adapter.ts` | Immutable snapshot per config; `listModels`, `resolveModel`, `UNKNOWN_MODEL`, effort validation |
| pi-ai plugin entry | `packages/llm/llm-pi-ai/src/index.ts` | Write-time hook, credential resolution, route + directory registration, live update |
| pi-ai discovery | `packages/llm/llm-pi-ai/src/discovery.ts` | "Fetch available models" for a draft; never stored |
| pi-ai model helpers | `packages/llm/llm-pi-ai/src/models.ts` | Supported thinking levels from pi-ai metadata |
| DeepSeek catalog | `packages/llm/llm-deepseek/src/models.ts` | Hand-written `DEFAULT_MODELS` (2 entries) |
| DeepSeek config | `packages/llm/llm-deepseek/src/config.ts` | Schema, `resolveModels`, `resolveAdapterOptions`, base URL from env |
| DeepSeek model info | `packages/llm/llm-deepseek/src/model-info.ts` | Metadata for any id; fixed effort set off/low/high/max |
| DeepSeek routes | `packages/llm/llm-deepseek-api-key/src/index.ts`, `packages/llm/llm-deepseek-account/src/index.ts` | Register `deepseek-official` and `deepseek-account`; discovery = configured list |
| Default model | `packages/core/agent-default-model/src/index.ts` | `{provider, model, reasoningEffort}` config row; `saveSelection` writes profile patch |
| LLM runtime | `packages/llm/llm/src/index.ts` | Registry, `listModels` validation, `resolveCallConfig` (no clamp), `prepareCall` |
| Web catalog gate | `packages/api/session-controller/src/catalog.ts`, `commands.ts` | Grouped catalog with provider failures; `modelAvailable` gate; `selectModel` |
| ACP model control | `packages/acp/acp/src/model-control.ts` | `model` and `reasoning_effort` session config options |
| Credentials | `packages/credentials/credentials-local/README.md` | `$DSH_HOME/.credentials.yaml`, precedence, reload |
| Env denylist | `packages/boot/app-boot/src/index.ts` | Names a discovered `.env` cannot set (incl. `DEEPSEEK_BASE_URL`) |

## 1. Built-in model catalog

### pi-ai catalog (multi-provider)

- Source: the installed pi-ai package. `catalogModels(provider)` reads `getBuiltinModels(provider)` from `@earendil-works/pi-ai/providers/all` (`packages/llm/llm-pi-ai/src/catalog.ts:15,201-205`). The provider index is built one time per process (`catalog.ts:166-177`).
- Version: `"@earendil-works/pi-ai": "^0.87.1"` (`packages/llm/llm-pi-ai/package.json:44`). A local patch is applied (`pnpm-workspace.yaml:101`). pi-ai is exempt from the release-age wait because "Fresh pi-ai releases carry the model catalog updates that are the whole point of bumping it" (`pnpm-workspace.yaml:65-67`). So catalog freshness depends on a dependency upgrade and a new DSH release.
- Fields used: id, name, api, baseUrl, input modalities, contextWindow, maxTokens, reasoning + `thinkingLevelMap`, compat. Every other pi-ai `Model` field rides through a spread (`catalog.ts:925-943`; comment "Spread, never enumerate", `catalog.ts:926-930`).
- Pricing: **DSH does not use it.** "The harness never reads pi-ai's cost metadata — `replay.ts` zeroes it and no consumer reports spend" (`catalog.ts:33-38`). A custom model gets `NO_COST` (`catalog.ts:938`). `request-pricing.ts` and token-meter `route-pricing.ts` are token counts for images, not money (`packages/llm/llm-deepseek/src/request-pricing.ts:90-111`; `packages/llm/token-meter/src/route-pricing.ts:1-6`).
- Thinking levels: `off, minimal, low, medium, high, xhigh, max` with a compile-time drift gate (`catalog.ts:75-86`). Supported levels come from `model.reasoning` and `thinkingLevelMap`. `xhigh`/`max` are offered only when mapped (`models.ts:59-67`). A non-reasoning model shows no effort control, not a fake `off` (`adapter.ts:171-201`).
- Drift gates: `Record<UpstreamType, …>` tables fail compilation when a pi-ai upgrade adds a modality, level, thinking format, or compat field (`catalog.ts:48-51,100-112,232-260`; type assertions `catalog.ts:453-481`).
- Remote fetch / cache / offline: **none.** Discovery is "not a catalog refresh. Nothing here is stored" (`packages/llm/llm-pi-ai/src/discovery.ts:11-14`). The catalog works offline because it is compiled into the dependency.

### DeepSeek catalog (direct adapter)

- Hand-written: `deepseek-flash` (name `DeepSeek-V41-Flash`, text+image, `systemPromptUpdate: in-history`, `toolUpdate: addition-only`) and `deepseek-v4-pro` (`packages/llm/llm-deepseek/src/models.ts:6-21`). Both use `DEFAULT_CONTEXT_WINDOW = 1_000_000`. Default output cap is `256_000` (`packages/llm/llm-deepseek/src/defaults.ts:6-8`).
- Fields: id, name, description, contextWindow, maxTokens, inputModalities, imagePixelBudget, imageMaxBytes, systemPromptUpdate, toolUpdate (`packages/llm/llm-deepseek/src/config.ts:68-79`). There is **no pricing field and no per-model reasoning field**.
- Reasoning: one fixed set `off/low/high/max` for every model, with default `high` (`packages/llm/llm-deepseek/src/model-info.ts:10-31,87-97`). `thinking: disabled` reduces it to `off` only (`model-info.ts:80-86`).
- The list is "Advisory … requests remain unrestricted" (`packages/llm/llm-deepseek/src/types.ts:60`). Both routes advertise the configured list as discovery (`packages/llm/llm-deepseek-api-key/src/index.ts:42-45`). The account route does the same, and returns `[]` before sign-in (`packages/llm/llm-deepseek-account/src/index.ts:44-51`).

## 2. User configuration of providers and models

### Files and layers

- The model configuration is a plugin row in the profile patch. The user guide says the Models page writes `$DSH_HOME/profiles/<profile>/cordis.patch.yml` (`docs/user/guide/providers.md:46-48`).
- Layer order: bundle layers < profile patch < `$DSH_HOME/cordis.patch.yml` < `--patch` files (`apps/cli/src/profile-boot.ts:184-191`).
- Merge happens in two different places. Do not mix them up:
  - The loader does not deep-merge. A raw patch layer replaces the complete `config` of the row ("A Cordis config override replaces the complete entry config", `docs/user/guide/providers.md:57`). So a hand-edited override must restate every provider, or it drops the inherited ones (`packages/llm/llm-pi-ai/README.md:221`).
  - The settings editor computes the complete config as `change(current, inherited)` and writes it (`packages/boot/config-editor/src/index.ts:100`). So a UI edit keeps the other providers. The "merge per provider" comment (`packages/llm/llm-pi-ai/src/config.ts:2-4`) describes the dictionary shape that makes these per-provider edits possible. It is not a loader deep-merge.
- **No project layer.** No project-level model or provider file exists, so trust gating of project model config does not apply. The project can contribute only a `.env`. That file can supply a key value for an `apiKeyEnv` name (credential order row 3, `packages/credentials/credentials-local/README.md:71-78`). It cannot set `DEEPSEEK_BASE_URL` (`packages/boot/app-boot/src/index.ts:148-149`). The DeepSeek base-URL env var is "honored only from trusted layers" (`packages/llm/llm-deepseek/src/config.ts:108-109`).
- The pi-ai row is mounted dormant in the base bundle. Routes register when the settings supply profiles (`packages/bundle/base/cordis.patch.yml:120-128`; `packages/llm/llm-pi-ai/src/index.ts:283-307`).

### Schema (pi-ai provider profile)

Keys are route names (`config.ts:91,221-229`). Fields (`config.ts:92-183`, schema `config.ts:326-349`):

| Field | Default | Notes |
|---|---|---|
| `apiKeyEnv` | absent | Env-var **name**, role `credential-ref` (`config.ts:327`) |
| `displayName` | route key | Empty string refused (`config.ts:425-427,461`) |
| `api` | catalog protocol | One of `supportedProtocols()` (`config.ts:329`) |
| `baseURL` | catalog endpoint | Empty refused (`config.ts:422-424`) |
| `models[]` | installed catalog | **Replaces** the catalog; each entry defaults from the catalog model with the same id (`config.ts:105-110`) |
| `modelOverrides{id: …}` | none | Changes single catalog models; refused beside `models`, on a custom route, or for an unknown id (`catalog.ts:856-877`) |
| `compat` | catalog, then pi-ai detection | Route default; model `compat` wins per field (`catalog.ts:780-811`) |
| `defaultContextWindow` / `defaultMaxTokens` / `defaultInput` | 262,144 / 32,768 / `[text]` | Fallbacks only for models that neither entry nor catalog sizes (`config.ts:64-80,334-336`) |
| `headers`, `reasoning`, `thinkingBudgets`, `cacheRetention`, `transport`, timeouts, image limits, `retryPolicy` | various | `config.ts:151-182,337-348` |

Model entry fields: `id` (required), `name`, `contextWindow`, `maxTokens`, `input`, `reasoningEfforts` (`false` or a level→wire map), `compat` (`config.ts:303-324`).

### Custom OpenAI- or Anthropic-compatible endpoints without a rebuild

- A route key that pi-ai does not ship is a full declaration. It must set `api`, `baseURL`, and at least one model (`catalog.ts:884-909`; `packages/llm/llm-pi-ai/README.md:117`). The UI offers `openai-completions`, `openai-responses`, and `anthropic-messages` (`docs/user/guide/providers.md:23`).
- Changes apply on the next request without a restart. Each operation captures a snapshot, and a config change builds a new collection (`packages/llm/llm-pi-ai/src/adapter.ts:221-239`; `index.ts:163-170,310-316`).
- Discovery for a custom route calls `GET {baseURL}/models` (bearer auth) or Anthropic `GET /v1/models?limit=1000`. A catalog route answers from the catalog with no network call (`packages/llm/llm-pi-ai/README.md:113`).

### Merge and defaults for unknown fields

- Merge order for each field is entry value, then installed catalog model, then route fallback (`catalog.ts:914-937`). An absent or empty `input` means "no answer here", so the next level answers (`catalog.ts:56-67`).
- `reasoningEfforts`: absent means inherit the catalog capability, else none. `false` means non-reasoning. A map declares the offered levels and their wire spelling, and only `off` may be empty (`catalog.ts:703-757`). A custom model with no map gets **no** effort control (`docs/user/guide/providers.md:113`).
- Custom models get guessed capacities (262,144 / 32,768) on purpose. The code says "A guess by construction, so a deployment … corrects it here" (`config.ts:128-132`). Modalities default to text only, because a wrong image claim costs more than a missing one (`config.ts:70-80`).

### Secret and header value syntax

- Only env-var **names**, through `apiKeyEnv`. These are resolved for each request through the credential seam (`index.ts:183-206`). DSH has **no `!command` value, no `$VAR` interpolation, and no literal key in config.** Compare Pi, where a value can be a shell command (`!…`) or an env reference (`pi:packages/coding-agent/src/core/resolve-config-value.ts:140-145`). Helper-command providers are "not shipped" (`packages/credentials/credentials-local/README.md:211`).
- A named reference that resolves to nothing fails with `MISSING_CREDENTIAL`. It does not fall back to ambient env, which prevents billing another account (`index.ts:188-205`). If `apiKeyEnv` is absent, DSH uses pi-ai ambient discovery (`packages/llm/llm-pi-ai/README.md:38`).
- `headers` are plain strings, checked only for Fetch validity (`config.ts:388-400`). The README warns that `headers` can carry a credential that the redactor never sees (`packages/llm/llm-pi-ai/README.md:222`).

### Precedence versus credential store and env

- Key values: launching env (read-only) > `$DSH_HOME/.credentials.yaml` > `<cwd>/.env` > `$DSH_HOME/.env` (`packages/credentials/credentials-local/README.md:71-78`). The Models page writes only the credentials file. Settings keep the reference (`docs/user/guide/providers.md:13`).
- OAuth or interactive sign-in records live at `llm-pi-ai/<provider id>` in the same store. A sign-in authenticates "beneath any `apiKeyEnv` override" (`packages/llm/llm-pi-ai/README.md:95`).

## 3. Model resolution

- **Id grammar: none.** The default is a config row with separate `provider`, `model`, and `reasoningEffort` fields (`packages/core/agent-default-model/src/index.ts:24-31,51-55`). Base default is `deepseek-official` / `deepseek-flash` (`packages/bundle/base/cordis.patch.yml:82-86`). ACP encodes a choice as the opaque value `JSON.stringify([provider, model])` (`packages/acp/acp/src/model-control.ts:234-237`). DSH has no aliases, no fuzzy match, and no effort suffix. The runtime states "no clamping or aliasing is performed" (`packages/llm/llm/src/index.ts:860-870`). Compare Pi: `parseModelPattern` splits on the last `:` for thinking (`pi:packages/coding-agent/src/core/model-resolver.ts:204,216`), and it does a substring match on id/name (`model-resolver.ts:145-146`).
- **CLI**: no `--model`, `--provider`, or effort flag. The full flag set is profile, patch, and dump flags (`apps/cli/src/args.ts:168-173`). To change the default model, you edit the `agent-default-model` row or use the web picker.
- **Effort validation**: `resolveCallConfig` rejects an unsupported explicit effort with `UNSUPPORTED_REASONING_EFFORT` before any I/O, and it fills in the adapter default (`packages/llm/llm/src/index.ts:886-918`). The pi-ai adapter also refuses levels and does not use pi-ai's clamp (`adapter.ts:157-169`).
- **Unknown id, by route**:
  - pi-ai route: `UNKNOWN_MODEL` "has no configured model" (`adapter.ts:250-262`). An unknown route gives `NO_ADAPTER` (`adapter.ts:243-249`; `packages/llm/llm/src/index.ts:980`).
  - DeepSeek routes: any id is served. An uncatalogued id resolves as text-only with `defaultContextWindow` (`packages/llm/llm-deepseek/src/model-info.ts:66-77`; `packages/llm/llm-deepseek/src/adapter.ts:37-38`).
  - Core contract: "Core routing accepts unlisted model ids; catalog-driven entry points such as the GUI may require membership" (`packages/llm/llm/src/index.ts:242-246`). The agent loop ignores only `NO_ADAPTER`, so `UNKNOWN_MODEL`/`INVALID_CONFIG` stop the request (`packages/core/agent-loop/src/agent.ts:586-592`).
  - Web: `modelAvailable` requires catalog membership (`packages/api/session-controller/src/catalog.ts:80-90`). A prompt to an unlisted model fails with "Select an available model before sending a message." (`commands.ts:379-384`).
- **Ambiguity**: not applicable, because there is no fuzzy match. Duplicate ids in one route are refused (`catalog.ts:898`). One route key belongs to one adapter, and a collision fails registration (`packages/llm/llm-pi-ai/README.md:34`).
- **Runtime switch**: web `selectModel` validates membership and effort, installs the selection for the next request, and saves it as the new default in the background (`commands.ts:151-176`). ACP exposes `model` and `reasoning_effort` select options. A change is serialized and validated (`model-control.ts:103-134`).
- **Configured model vanishes**:
  - Web: a deleted default provider shows **Select model** and blocks input (`docs/user/guide/providers.md:39`). A session keeps the selection in its own log (`docs/user/guide/providers.md:37`; test `session-models.host.spec.ts:586`).
  - ACP: the current route is added to the list even when no provider lists it (`model-control.ts:178-187`). A catalog failure after the first good resolve keeps the selection and hides the effort option (`model-control.ts:147-156,196-198`).
  - Request: the pi-ai route then fails `UNKNOWN_MODEL` at the next request. The DeepSeek route continues with guessed metadata.
- **Inconsistency found**: the ACP bundle default is `deepseek-v4-flash` (`packages/bundle/acp-app/cordis.patch.yml:20-21`). That id is not in `DEFAULT_MODELS` (`packages/llm/llm-deepseek/src/models.ts:8,16`), and the ACP bundle does not override `models`. So ACP sessions run it as an uncatalogued text-only model with a 1M context. Nothing reports this. The `agent-default-model` README example `provider: deepseek, model: deepseek-chat` is also stale (`packages/core/agent-default-model/README.md`, "Configure the default").

## 4. Error handling

| Case | Behavior | Evidence |
|---|---|---|
| Write of a changed provider (UI or `ConfigEditor`) | `internal/config` hook runs `assertServiceable` on changed providers only, strict; failure refuses the write before disk | `packages/llm/llm-pi-ai/src/index.ts:172-181`; `config.ts:364-368`; `packages/boot/config-editor/src/index.ts:103-104` |
| Hand edit, HMR reload | Malformed/invalid live patch rejected; running config unchanged; existing entry keeps prior config | `packages/boot/app-boot/README.md:94-97` |
| Stored pi-ai provider that went bad (e.g. catalog upgrade removed a model) | `deferred` resolve: per-model diagnostics kept, other models serve; failing model throws `INVALID_CONFIG` before network | `config.ts:410-413,486-489`; `catalog.ts:946-958`; `adapter.ts:254-256` |
| Unchanged bad provider | Does not block edits to other providers; whole provider must validate to save | `packages/llm/llm-pi-ai/README.md:119-121` |
| Removed legacy fields | Refused with a named replacement | `config.ts:370-386`; array form `config.ts:414-416` |
| Typo in a compat key, or empty value | Refused; message lists the valid switches | `catalog.ts:550-584` |
| Typo in other profile keys | **Not caught.** The schema passes unknown keys through (comment `catalog.ts:871-873`); only `id` in an override is checked | `catalog.ts:874-876` |
| DeepSeek route beyond-schema failure (e.g. duplicate ids) | **Fails closed**: catalog reads and requests fail until repair | `packages/llm/llm-deepseek/tests/dynamic-config.spec.ts:277-290,292` |
| Credentials file malformed or group-readable | Boot fails; live reload keeps last good snapshot with a warning | `packages/credentials/credentials-local/README.md:115,121` |
| Diagnostics to the user | Web catalog returns `failures[]` per provider; directory entries carry `error` | `packages/api/session-controller/src/catalog.ts:53-71`; `index.ts:130-145` |

Messages name the route, model, and field, and they say what to do. For example: "`model "x" needs an api; … set the route's api`" (`catalog.ts:902-905`), and the `MISSING_CREDENTIAL` text (`index.ts:200-205`). DSH has a composed-config dump (`--dump-config`, `apps/cli/src/args.ts:171`). It has **no `models` list command**.

## 5. Tests

| Area | Test file (case count) | Examples |
|---|---|---|
| Catalog merge, overrides, modalities, reasoning | `packages/llm/llm-pi-ai/tests/catalog.spec.ts` (67) | `:73` undeclared route; `:151` route fallbacks; `:184` modality order; `:451` override one field; `:765` refuse dead overrides |
| Schema | `packages/llm/llm-pi-ai/tests/config.spec.ts` (8) | `:31` unknown level; `:36` `false` vs absent |
| Live config | `packages/llm/llm-pi-ai/tests/dynamic-config.spec.ts` (11) | `:79` stored failures stay editable; `:143` dormant mount; `:212` credential rotation |
| Upgrade drift | `packages/llm/llm-pi-ai/tests/compat-upgrade.spec.ts` (3) | `:68` new upstream fields stay absent |
| Discovery | `packages/llm/llm-pi-ai/tests/discovery.spec.ts` (24) | `:85` catalog answer, no network; `:118` OpenAI listing; `:176` Anthropic listing |
| DeepSeek route | `packages/llm/llm-deepseek/tests/dynamic-config.spec.ts` (8), `model-info.spec.ts` (1) | `:277`, `:292` fail closed; `model-info.spec.ts:5` minimal entry |
| Default model | `packages/core/agent-default-model/tests/agent-default-model.spec.ts` (3) | `:20` persists via profile; `:35` serialized saves |
| Web selection | `packages/api/session-controller/tests/session-models.host.spec.ts` | `:408` unlisted current selection left out; `:489` rejects unlisted models; `:586` logged selection wins |
| ACP selection | `packages/acp/acp/tests/model-control.spec.ts` | `:46` synthesizes unlisted current route; `:75` keeps route when catalog is down; `:88` rejects unknown effort |

Gaps: I found no test for an unknown-key typo in a profile, and no test that checks the ACP bundle default against the DeepSeek catalog.

## 6. Strengths and weaknesses versus a Pi-style `models.json`

Context: Pi keeps a separate `~/.pi/agent/models.json` that loads as one immutable snapshot. A bad file gives an empty config plus an error string (`pi:packages/coding-agent/src/core/model-config.ts:271-311`). DSH's pi-ai profile is Pi's `models.json` placed inside a plugin row.

| Dimension | DSH | Pi-style `models.json` | Take for Ask |
|---|---|---|---|
| Layering | Each layer replaces the whole row; the editor merges per provider | One file, no layers | Decide whether `models.json` has user and project layers. If it does, merge per provider id |
| Catalog source | Pinned dependency; no fetch | Compiled + models.json | Ask's models.dev overlay is beyond both |
| Custom endpoint | Same fields; strict write validation | Same fields | Copy the shape |
| Override semantics | `models` replaces, `modelOverrides` patches; dead overrides refused | Similar | Copy the "refuse an override that lands nowhere" rule |
| Reasoning | Explicit level→wire map; `false`; no fake `off` | Per-model thinking flags | Copy the map; it maps to Ask's `ReasoningLevels` |
| Unknown-field default | Guessed 262k/32k, text-only, cost 0 (cost unused) | Similar | Ask's rule "do not invent metadata" is stricter; keep it, and mark guessed values |
| Secrets | Env name only; store + `.env` | `!cmd`, env, literal | Pi is more flexible; DSH is safer. Ask decision |
| Id grammar | None (struct) | `provider/model`, `:level`, fuzzy | Use Pi |
| Bad config | Strict on write, deferred per model on read | Whole file fails to empty | DSH is better: copy per-model diagnostics |
| Unknown keys | Silently ignored | TypeBox schema; `model-config.ts` sets no `additionalProperties: false`, so it is probably open too (not tested) | A weakness in DSH, and possibly in Pi too; Ask must decide |
| Consistency | Two catalogs, two unknown-id policies, stale defaults | One registry | Avoid the split |

Strengths worth copying: drift gates on upstream enums; write-time `assertServiceable` that checks only changed providers; snapshot per operation; `MISSING_CREDENTIAL` instead of a silent ambient fallback; discovery that proposes models but never writes them.

Weaknesses to avoid: no id grammar or CLI flag; different unknown-model rules on different routes; unknown keys ignored; secrets allowed in `headers`; catalog freshness tied to a dependency release; a whole-row override that can drop inherited providers (`packages/llm/llm-pi-ai/README.md:221`).

## Limitations

- I did not read the web `ModelSelect.tsx` UI (656 lines) or the Models-page form code. UI behavior comes from the user guide.
- I did not open the pi-ai package source, so the exact pi-ai catalog fields and their generator are not verified here.
- I inferred the HMR chain for a changed unserviceable route from the hook and the app-boot failure matrix. I did not trace it in a test.
- The Pi comparison uses only a few anchors. Pi's `models.json` details are covered in earlier Ask reports.

## Unresolved questions

1. Does Ask want Pi's `!command` secret values, or DSH's rule of env names only? This is a security and UX decision for the user.
2. For unknown ids on a custom route, should Ask refuse them (pi-ai style) or serve them with guessed metadata (DeepSeek style)? The Ask skill note says to hide uncatalogued models by default. This must be confirmed for typed `--model` ids.
3. Should Ask's `models.json` reject unknown keys (closed schema) or keep them (forward compatibility)?

Status: DONE
Summary: DSH uses the pinned pi-ai compiled catalog plus a 2-entry hand-written DeepSeek list, with no remote refresh. User providers live in a Pi-models.json-like `providers` dict in the profile patch, with strict write validation and per-model diagnostics. It has no id grammar and no CLI model flag, and the two adapter families handle unknown ids differently.
