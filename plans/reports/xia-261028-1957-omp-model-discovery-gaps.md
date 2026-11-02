# Xia port analysis: omp model list and metadata into Ask

Mode: `--port`. Date: 2026-10-28.

## Source manifest

- Source: local path `/Users/dale/Desktop/workspace/opensources/oh-my-pi`, commit `efbc17154a` (2026-10-07), clean tree.
- License: MIT (`LICENSE`). The repository has no terms for the `catalog.stencil.so` data service.
- Scope: `packages/catalog/src` (model manager, cache, discovery, compat rules, bundled `models.json`) and `packages/coding-agent/src/config/model-registry.ts`, `main.ts`, `slash-commands/helpers/logout.ts`, `modes/controllers/selector-controller.ts`.

## Source anatomy (verified)

omp has four parts. The earlier analysis covered parts 1, 3 and 4 only.

1. **Bundled catalog.** `packages/catalog/src/models.json` is 11.9 MB with 75 providers: anthropic 27 rows, openai 71, openai-codex 8, xai 35. A row has id, name, api, baseUrl, reasoning, input, cost, contextWindow, maxTokens, thinking (mode and efforts), identity (class, family, revision) and compat flags. `scripts/generate-models.ts` writes it (`bun run gen:models`) from `catalog.stencil.so`, provider endpoints and hand-written KDL seed rules.
2. **Rules engine for every row.** `buildModel` calls `resolveModelPolicy` for every row (`build.ts:347-348`), including rows that discovery finds at run time. `classifyModel` (`compat/taxonomy.ts:444`) gives a class, family and revision from the id. KDL rules (`compat/rules/providers/*.kdl`, compiled by `scripts/compile-compat.ts`) then give thinking efforts and compat flags per API (`compat/resolve.ts:1304-1330`). A new id in a known family therefore gets correct request settings without a new build.
3. **Runtime metadata overlay.** `GET https://catalog.stencil.so/models.json.zstd`, about 93 KB (`provider-models/openai-compat.ts:85`). It uses `If-None-Match` with the ETag, one shared in-flight request, and the last payload when a fetch fails (`openai-compat.ts:193-260`). With `additiveOnly`, the overlay can add ids but does not replace a bundled row.
4. **Account list (discovery).** Per provider:
   - openai, API key: `GET /v1/models`. It keeps an id when the bundled catalog has it or when `isLikelyOpenAIResponsesId` accepts it (`openai-compat.ts:740-752`, `1074-1084`).
   - anthropic, API key or OAuth: `GET /v1/models`. OAuth uses `Authorization: Bearer` and `anthropic-beta`; a key uses `x-api-key` (`openai-compat.ts:493-505`). Metadata comes from a reference map (`openai-compat.ts:6525-6575`).
   - xai, API key: `GET https://api.x.ai/v1/models` with no filter and a bundled reference join. A drop list stops cached rows with an old API from winning (`openai-compat.ts:1634-1650`). xai-oauth has its own curation (`openai-compat.ts:1779`).
   - alibaba-token-plan, API key: `GET {baseUrl}/models`. It is authoritative and keeps chat ids only. An unknown `deepseek-v4*` id gets reasoning efforts from a rule (`openai-compat.ts:3748-3800`).
   - Unknown ids get `reasoning: false`, cost 0, and contextWindow and maxTokens `null` (`discovery/openai-compatible.ts:207-218`).
5. **Cache and refresh.**
   - Cache: SQLite `models.db` with one row per cache key. The key holds a hash of key and base URL for providers whose list depends on the credential. Headers are never stored. A static fingerprint drops cached rows after the bundled catalog changes. Default TTL is 2 h; a failed refresh is retried after 5 min (`model-manager.ts:19-20`, `model-provider-discovery.ts:46-47`).
   - Refresh points:
     - At start: `online-if-uncached` (`main.ts:1001`, `2238`, `2466`).
     - After login: `online` for that provider (`selector-controller.ts:1948`).
     - After logout: `online` for that provider (`logout.ts:135-140`).
     - On request: `omp models refresh`.
   - Background refresh runs one at a time (`model-registry.ts:533-548`).

## Ask today (verified)

- The catalog has 6 rows (`internal/providers/catalog.go:10`).
- `Compat` is a sealed type per API. `ResponsesCompat` and `CompletionsCompat` carry flags for each row (`internal/providers/compat.go:1-42`).
- `ClampThinkingLevel`: with `Reasoning` false, thinking is off. With no entry in the map, low, medium and high are allowed, and xhigh and max are refused (`internal/providers/clamp.go:19-55`).
- The Anthropic adapter always sends `thinking: {type: adaptive}` and `output_config.effort` (`internal/providers/anthropic/document.go:71-73`).
- Cost has a rule for one model id: gpt-5.5 priority (`internal/providers/cost.go:127`).
- The ChatGPT OAuth token goes to `api.openai.com/v1` and only `POST /responses` (`internal/providers/openai/profiles.go:30`, `56`). OpenAI's guide uses `GET https://api.openai.com/v1/models`, keeps `visibility == "list"`, and says not to use `backend-api`.
- The TUI fetches the model list each time `/model` opens (`internal/tui/update_input.go:325-339`) and after provider setup (`update_providers.go:826-836`). A refresh in the leader is therefore visible at the next `/model` open, and no new push event is required.
- The accepted auth design (`plans/261005-2139-provider-auth-design/plan.md:490-495`) says: "Discovery narrows the usable catalog; it does not invent prices, context limits or reasoning support."

## Gaps the first analysis missed

| # | Gap | Evidence | Effect on Ask |
|---|---|---|---|
| G1 | Rules engine for new ids (thinking efforts, compat) | `build.ts:347`, `compat/resolve.ts:1304`, `compat/taxonomy.ts:444` | A row from models.dev alone has no `ThinkingLevelMap` or `Compat` flags. Ask needs a small family rule table. |
| G2 | Request shape per model generation | omp `thinking.mode` (for example `anthropic-adaptive`); Ask `anthropic/document.go:71-73` | Ask's Anthropic adapter only sends adaptive thinking. Older Claude ids may refuse it. A row must be shown only when its family rule says the adapter can drive it. |
| G3 | Non-chat ids in OpenAI and xAI lists | `openai-compat.ts:740-752` | Without a filter, `/model` shows tts, embedding and image ids. |
| G4 | Token Plan list | `openai-compat.ts:3748-3800` | omp lists Token Plan from `{baseUrl}/models`. Ask has two wires per id, so one listed id can give two rows. |
| G5 | Refresh after logout and credential change | `logout.ts:135-140` | Ask has no hook after the login commit (`internal/auth/login.go:154`) or after logout. |
| G6 | Cache key per credential | `cache-provider-id.ts` | Account A's list must not show for account B. Key the cache by provider, method and credential generation or subject. Store no secrets. |
| G7 | Static fingerprint | `model-manager.ts` fingerprint | A new Ask build must drop cached rows that the new built-in catalog changed. |
| G8 | Runtime metadata service | `openai-compat.ts:85` | omp uses `catalog.stencil.so` (Stencil Labs; terms unknown), not models.dev. Ask should read models.dev directly. The URL, size and license must be checked. |
| G9 | Cost for subscription use | omp and Ask both ignore the billing class | The header shows API prices for OAuth use. This is not new; record it as out of scope or as a decision. |
| G10 | Saved model no longer in the account list | omp picks a default at next start; Ask has `ACPFallbackNotInCatalog` | With discovery, "not in the account list" differs from "not in the catalog". Accepted design Q4=A: unknown access permits inference, and known denial is an error. |

## Dependency matrix

| omp part | Ask equivalent | Status |
|---|---|---|
| `models.json` plus generator | `internal/providers/catalog.go` (6 rows) | CONFLICT: replace with a generated catalog and keep the hand-written rows as overrides |
| KDL rules plus `resolveModelPolicy` | none | NEW: Go family rule table in `internal/providers` |
| stencil overlay | none | NEW: models.dev overlay with ETag and cache |
| `/v1/models` discovery | `internal/auth/discovery.go` (ChatGPT, used as a block) and `keycheck.go` | EXISTS in part: extend in `internal/auth` |
| `models.db` cache | none (`ASK_HOME` has `auth.json`, `settings.json`, leader files) | NEW: JSON cache file with the settings atomic write pattern |
| refresh points | login commit `auth/login.go:154`; leader start | NEW: hooks after login and logout, plus a refresh at start |
| selector revision | `/model` fetches the list at each open | EXISTS: no push needed |

## Decision matrix

| Decision | omp | Ask proposal | Recommendation |
|---|---|---|---|
| Built-in metadata | 11.9 MB generated `models.json`, 75 providers | Generated file for 4 providers from models.dev, plus the 6 hand-written rows as overrides | Port, smaller |
| New id request settings | KDL class, family and revision rules | Go table: id pattern gives a thinking map, compat and an "adapter can drive" flag | Port the idea, not KDL |
| Id with no matching rule | Shown with defaults | Hidden, because Ask cannot build a correct request | Ask way |
| Known family, no metadata | context `null`, cost 0 | Shown; header shows `?`, not `$0.00` | Ask way, consistent with the accepted design |
| Runtime overlay | `catalog.stencil.so` | models.dev directly (check URL and license) | Adapt |
| ChatGPT list | `chatgpt.com/backend-api/codex/models` | `api.openai.com/v1/models`, `visibility == "list"` (current `discovery.go`) | Ask way, per OpenAI guide |
| Cache | SQLite `models.db` | One JSON file in `ASK_HOME`, owner only, atomic write | Adapt |
| Refresh | start, login, logout, command, 2 h TTL | Same | Port |
| Client update | settings revision | Existing re-fetch at `/model` open | Ask way |

## Risk score

Medium-high. The change touches `providers`, `auth`, `app`, the TUI selector and the reference doc generation. The highest risk is G1 and G2: a listed model that the adapter sends with wrong thinking settings fails at request time.

## Unresolved questions

1. Which Claude and OpenAI ids refuse adaptive thinking or reasoning effort? This needs a vendor doc check or a live call per family.
2. What are the models.dev API URL, response size and license, and does it support ETag?
3. What does the xAI OAuth token return from `GET /v1/models`?
4. Should the header show a "subscription" label instead of API prices for OAuth use (G9)?
