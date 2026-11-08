---
title: "Account model discovery"
description: "Show in /model the models that the signed-in account can use, with real metadata from a generated models.dev catalog, a runtime metadata refresh, and the vendor model-list APIs."
status: completed
priority: P1
effort: "11d"
branch: develop
tags: [feature, backend, providers, auth, tui, catalog]
blockedBy: []
blocks: []
created: 2026-10-28
mode: hard
---

# Account model discovery

## Outcome

`/model` shows the chat models that the signed-in account can use, not a fixed list of 6 rows.
Each row has real metadata (context window, output limit, prices, thinking levels), or shows `?` for a context window that no source gives.
Ask never shows a model that its adapters cannot send a correct request to: every shown row matches a family rule in Ask's code.
The list refreshes at leader start when the cache is older than 2 h, after login and logout, and with `ask models refresh`. A failed refresh keeps the last good list. A refresh never changes the selected model.

## Why

The `/model` list comes from `internal/providers/catalog.go:10`, 6 hand-written rows. The user asked for the list of the account, as oh-my-pi (omp) does.

## Authority and context

| Link | Read it for |
|---|---|
| [xia: omp model discovery gaps](../reports/xia-261028-1957-omp-model-discovery-gaps.md) | **The port analysis.** omp anatomy with citations, gaps G1-G10, dependency and decision matrices, risk. |
| [Red-team adjudication](../reports/red-team-261028-account-model-discovery.md) | 15 accepted findings with code evidence. The phases below apply them. |
| [Provider auth design](../261005-2139-provider-auth-design/plan.md) lines 490-500 | Fixed rules: discovery narrows the catalog; it does not invent prices, context or reasoning. Unknown access permits inference (Q4=A); known denial is a selection error. |
| [Roadmap H7](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h7-model-catalog-and-resolution) | H-PROV-16 (generated baseline, remote overlay, cache, offline). This plan does H-PROV-16 for 4 providers plus account lists. User `models.json` overrides (H-PROV-17) are out of scope. |
| [OpenAI: models for ChatGPT sign-in](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference) | ChatGPT list: `GET https://api.openai.com/v1/models`, keep `visibility == "list"`, do not use `backend-api`. |
| [Architecture reference](../../docs/ask-architecture-reference.md) section 3, `.golangci.yml:144-158` | `providers` owns model types and the registry role; `auth` owns account access checks; `settings` is stdlib only; `providers` must not import `settings` or `auth`. |
| [Async task runtime](../../docs/async-task-runtime.md) | No sleep and no polling loop. The refresh is event-driven. |

## Decisions

### User decisions (2026-10-28, this session)

| # | Decision |
|---|---|
| U1 | Port the omp model in 3 layers: a built-in catalog generated from models.dev, a runtime metadata overlay, and an account list from the vendor API. |
| U2 | An id that matches no family rule is hidden, because Ask cannot build a correct request for it. |
| U3 | An id that matches a family rule but has no metadata is shown, and the TUI shows `?` for unknown values, not `0` or `$0.00`. Confirmed form (after red-team, 2026-10-28): `?` for an unknown context window in the selector and the header; (cost part superseded by U9); the selector gets no cost column. |
| U4 | The runtime overlay reads models.dev directly, not `catalog.stencil.so`. |
| U5 | ChatGPT uses `api.openai.com/v1/models` per the OpenAI guide, not omp's `chatgpt.com/backend-api/codex/models`. |
| U6 | Refresh points: leader start, after login, after logout, an explicit command. TTL 2 h. Keep the last good list on error. |
| U7 | Apply all 15 red-team findings as adjudicated (2026-10-28). |
| U8 | Move the link, working directory, model, thinking level, context use and cost back from the header row to a footer row at the bottom of the screen, as before the alternate-screen change (`5b6a16a`, 2026-10-12). Write a new mockup that includes this layout and the new model-list states. (2026-10-28) |
| U9 | The footer row shows no session cost. This replaces the cost part of U3: there is no `$0.04+` marker and no usage-path "incomplete" field. `CostUnknown` stays on the model data for other consumers. (2026-10-28) |

### Plan decisions (verified)

| # | Decision | Evidence |
|---|---|---|
| P1 | models.dev gives `reasoning_options` (effort values, budget tokens), `tool_call`, `type`, `status`, `limit`, `cost` (with `tiers`), `modalities`. **Every row must match a local family rule.** The rule sets API, base URL, compat, request defaults for output and context, and the allowed effort words. models.dev may only narrow the effort words and give limits and prices. An unknown `reasoning_options` type hides the row. <!-- Updated: Red Team 2026-10-28 - family rule required for all rows --> | `curl https://models.dev/api.json` 2026-10-28: 200, about 5.95 MB uncompressed (603 KB gzip), strong ETag, `cache-control: max-age=0, must-revalidate`; 233 providers; anthropic 17, openai 52, xai 13, alibaba-token-plan 29 rows. License MIT (`gh api repos/sst/models.dev/license`). |
| P2 | Anthropic thinking needs `effort` in `reasoning_options`, because a thinking request sends `output_config.effort` and adaptive thinking. With thinking off, the adapter sends `thinking: {type: disabled}`. A family whose models have `budget_tokens` only is shown with `Reasoning: false` (thinking off only) if phase 1 proves that request works; otherwise it is hidden. Phase 1 decides per family generation, not per options shape. | `internal/providers/anthropic/document.go:65-74` |
| P3 | Keep rows with `tool_call: true`, `type` empty or a chat type, output modalities exactly `["text"]`, a context limit, and `status` not `deprecated`. This drops image, embedding, tts, video and realtime ids. | models.dev rows (for example `gpt-realtime-2.1` has `type: realtime`) |
| P4 | The 6 hand-written rows stay as id-level overrides: they win over generated rows with the same provider, id and API. Family rules hold family defaults only. models.dev has an `alibaba-token-plan` provider (completions URL), so Token Plan completions rows are generated when a family rule exists; the two hand-written deepseek rows (messages and completions) stay. | `internal/providers/catalog.go:36-165`; models.dev `alibaba-token-plan.api` equals `internal/providers/api.go:24` |
| P5 | A new type `providers.ModelSet` (file `model_set.go`) holds the merged view. The existing `providers.Registry` (API to stream function) does not change. `Find`, `AvailableModels` and `RecommendedModel` keep their contract over the built-in catalog. Runtime callers move to the `ModelSet`. <!-- Updated: Red Team 2026-10-28 - name collision --> | `internal/providers/registry.go:11-22`; callers `internal/app/module_acp.go:91-104,143-150`, `internal/acp/ask_methods.go:147-186`, `internal/app/default_model.go:90`, `cmd/tui/headless_faux.go:79` |
| P6 | Account lists are fetched in `internal/auth` through a GET-only allow-list transport on the native HTTP client, because only `auth` holds credentials and the provider transports allow only `POST` inference paths. The request builder and status classification are shared with `NativeKeyCheck`. | `internal/providers/openai/profiles.go:56`, `internal/auth/keycheck.go:53-100`, `internal/auth/http.go:32,45` |
| P7 | The cache is one JSON file in `ASK_HOME`, written by `internal/settings` (stdlib only, mode 0600, atomic replace, sidecar lock). It holds the raw filtered models.dev payload and plain id lists, never a token, a key, a header, or a key-derived value other than a keyed hash with a random per-home salt. No catalog fingerprint: each binary maps the raw data with its own rules. A schema version guards the format; an older binary never overwrites a newer schema; a write keeps the newer `fetchedAt` per entry. | `internal/settings/auth.go:227`, `internal/settings/lock.go` |
| P8 | No push event is needed. The TUI asks for the list each time `/model` opens and after provider setup. `models()` always includes the current session model row, so the header keeps its window and thinking control. | `internal/tui/update_input.go:325-339`, `internal/tui/update_providers.go:826-836`, `internal/tui/session.go:316-351` |
| P9 | The refresher is a field of `ACPRuntime` with its own root context; `Cleanup` stops it. All 3 composition sites (leader, `ask acp`, in-process TUI) get it. A stale cache starts one background refresh when a client asks for the list. No timer and no polling loop. `OnChange` only schedules a refresh (non-blocking), because the broker calls it under `changeMu`. | `internal/app/module_leader.go:78`, `cmd/tui/acp.go:76`, `cmd/tui/tui_client.go:96`, `internal/auth/broker.go:676-686`, `docs/async-task-runtime.md` |
| P10 | Anthropic `GET /v1/models` pages its result; the client reads at most 20 pages and stops on a repeated cursor. Phase 1 confirms the field names. | Anthropic API reference |
| P11 | Cache key: provider + method + identity (OAuth subject when the method has one; API key: keyed hash). Login and logout delete the provider's entries. `Credential.Generation` is not part of the key, because a token refresh increases it. A refresh result is discarded when the broker change generation moved after the fetch started. <!-- Updated: Red Team 2026-10-28 --> | `internal/settings/auth.go:417-419,509,578`; subject only in `internal/auth/chatgpt.go:76` |
| P12 | A list 401 or 403 is `unknown`, not `denied`, unless phase 1 proves that it equals an inference refusal for that method. `denied` is never persisted. `checkChatGPTAccess` keeps its own 1-min memory cache and shares only the parser. | `internal/auth/discovery.go:52-66`, `internal/auth/service.go:202-213` |
| P13 | A context window or output limit of 0 means "unknown" for display (shown as `?`). The request uses the family rule default. Only cost gets a new flag: `Model.CostUnknown` and `ACPModel.CostUnknown` (omitempty, so an older peer reads "known"). | `internal/tui/selector.go:320-323`, `pkg/protocol/acp.go:189-199,245-250` |

### Decisions from phase 1 evidence (2026-10-28, cook `--auto`)

Source: [spike report](../reports/spike-261028-model-request-shapes.md), section "Family rule table". The plan rules decide each item; none reverses a user decision.

| # | Decision | Evidence |
|---|---|---|
| P14 | Show Anthropic 4.5 rows with `Reasoning: false`. Phase 2 makes the Anthropic prepare step apply `ClampThinkingLevel`, so a row with `Reasoning: false` always sends `thinking: disabled`. Today an empty level becomes `medium`. | spike: off 200 and low 400 on all 4.5 rows; `internal/providers/anthropic/document.go:102-115` |
| P15 | Families that reject thinking off (Fable 5 and 5.1, Opus 5.5, Sonnet 5.5) get an explicit `nil` for off. `ClampThinkingLevel` walks up to the lowest supported word when no level at or below the request is supported. This also changes the built-in `grok-4.7` row (it has `off: nil`): off becomes `low` in the header. Grok docs say its reasoning cannot be turned off. | spike troubleshooting doc rows; `internal/providers/clamp.go:19-55`; `internal/providers/catalog.go` grok row |
| P16 | `gpt-6*` rows show with `Reasoning: false`, because the fantasy id rule drops their effort. Generated xAI and Token Plan rows are hidden (no live evidence). `gpt-daybreak-*` is hidden. The hand-written grok and deepseek rows stay. | spike rule table |
| P17 | Phase 4 has no list function for `anthropic-oauth` (vendor terms for subscription tokens) or for Token Plan (its `/models` is the plan catalog, not the account's access, and the vendor allows only interactive use). Their state is `unknown`. The ChatGPT list follows the OpenAI guide with the existing parser; there is no live evidence, because the token in the copied home had expired. | spike policy section; `internal/auth/discovery.go` |
| P18 | A list 401 means a bad or expired credential, and inference fails the same way. A list 403 and any other refusal is `unknown`. No list result is `denied`. | spike "401 and 403 meaning" section |

Follow-up outside this plan: the fantasy fork drops the effort word for `gpt-6*` and every `grok-*` id (an existing defect for `grok-4.7`). The vendor terms for `anthropic-oauth` and Token Plan headless use need a user decision.

## Phases

| # | Phase | Effort | Depends on | Status |
|---|---|---|---|---|
| 1 | [Request-shape and endpoint spike](./phase-01-request-shape-spike.md) | 1.5d | none | Completed |
| 2 | [Generated catalog and family rules](./phase-02-generated-catalog.md) | 2d | 1 | Completed |
| 3 | [Model cache store and metadata overlay](./phase-03-cache-and-overlay.md) | 1d | 2 | Completed |
| 4 | [Account model lists](./phase-04-account-lists.md) | 1.5d | 1, 3 | Completed |
| 5 | [Model set, composition and refresh](./phase-05-registry-and-refresh.md) | 2.5d | 2, 3, 4 | Completed |
| 6 | [CLI refresh, footer layout and TUI unknown values](./phase-06-cli-and-tui.md) | 2.5d | 5 | Completed |
| 7 | [Acceptance and docs](./phase-07-acceptance-and-docs.md) | 1d | 6 | Completed |

## Success criteria

- [ ] With an OpenAI key, `/model` shows the chat models of the account that pass the rules, more than 1 OpenAI row, and no `tts`, `embedding`, `image`, `realtime` or deprecated id.
- [ ] With ChatGPT sign-in, the OpenAI rows are the `visibility == "list"` slugs that pass the rules.
- [ ] With Anthropic sign-in (OAuth or key), the rows come from `/v1/models` (all pages) and pass the rules.
- [ ] Every shown row matches a family rule; every shown Anthropic and Completions row sends `max_tokens` above 1.
- [ ] An id with a family rule and no context metadata shows `?` for the context window in the selector and the footer row. An id with no rule is not shown.
- [ ] The list refreshes at leader start (cache older than 2 h), after login, after logout and with `ask models refresh`. Inside one process, each refresh runs once per account key at a time.
- [ ] A failed refresh keeps the last good list. Offline, the built-in catalog and the cache still give a list.
- [ ] A refresh never changes the model of a running session, and the header keeps the window and thinking control of that model. A saved default that left the list falls back with the reason "not in account list".
- [ ] The cache file has mode 0600 and holds no secret (a test scans it for the credential values).
- [ ] `go test ./...` sends no request to models.dev or a vendor list endpoint.
- [ ] `go vet ./...`, `go test ./...`, the lint command and the reference doc drift test pass. No `time.Sleep` or ticker in product code of this change.

## Out of scope

- User `models.json` overrides and custom providers (H-PROV-17).
- A subscription label in place of API prices for OAuth use (xia G9).
- A refresh key inside the `/model` selector, and a cost column in the selector.
- Anthropic budget-token thinking for older models (only if phase 1 shows a need; then it is a separate plan).
- The Anthropic adapter computes no cost today, so the header shows `$0.00` for Anthropic sessions. This existing defect is not fixed here.

## Risks

| Risk | Effect | Mitigation |
|---|---|---|
| A listed model refuses the request shape | First prompt fails | Phase 1 proves the shape per family generation; the family rules hide what is not proved. |
| models.dev changes, is down, or carries bad data | Stale or wrong metadata | Family rules own API, URL, compat and effort words; models.dev only narrows and gives numbers. Built-in snapshot plus last good cache. |
| Account A list shown for account B | Wrong models | Identity-based key; login and logout delete the provider's entries; results from before a credential change are discarded. |
| Several processes write the cache | Lost update | Sidecar lock, newer `fetchedAt` wins per entry, schema guard. |
| Large list in the selector | Harder to choose | The selector filter exists; list order is fixed by Ask, not by the vendor. |

## Rollback

Each phase is its own commit. Phases 1-4 add code without a change in behavior: phase 2 adds `GeneratedCatalog()` and leaves `catalog()` unchanged. Phase 5 switches the runtime to the `ModelSet` and adds the wire fields `ACPModel.CostUnknown` and `NotInAccount`; phase 6 adds the CLI command and the TUI change. To roll back, revert phases 6 and 5 together: the built-in catalog and `providers.Find` keep working, and the cache file is ignored. A default saved from a generated row then falls back with the existing "not in catalog" note.

<!-- Updated: Red Team 2026-10-28 - rollback unit corrected -->

## Red Team Review

### Session — 2026-10-28

**Findings:** 15 after merging 39 raw findings (15 accepted, 2 raw findings rejected).
**Severity breakdown:** 2 Critical, 9 High, 4 Medium.
Full table and evidence: [red-team adjudication](../reports/red-team-261028-account-model-discovery.md).

| # | Finding | Severity | Disposition | Applied to |
|---|---|---|---|---|
| 1 | `providers.Registry` name collision | Critical | Accept | P5, phase 5 |
| 2 | Unknown limits send `max_tokens: 1` | Critical | Accept | P1, phase 2, 5 |
| 3 | Remote data decides request shape; endpoint fields | High | Accept | P1, phase 2, 5 |
| 4 | Cache key on generation; no subject for some OAuth | High | Accept | P11, phase 3, 4, 5 |
| 5 | `OnChange` under lock; stale join | High | Accept | P9, P11, phase 5 |
| 6 | Refresher has no owner; third site | High | Accept | P9, phase 5 |
| 7 | Tests would hit the network | High | Accept | phase 5, criteria |
| 8 | TUI loses running model data | High | Accept | P8, phase 5 |
| 9 | Phase 2 behavior change; rollback | High | Accept | Rollback, phase 2 |
| 10 | 401/403 as denied; ChatGPT gate TTL | High | Accept | P12, phase 4, 5 |
| 11 | Phase 1 cannot run via `ask -p` | High | Accept | phase 1 |
| 12 | Wrong facts: Token Plan, payload size | Medium | Accept | P1, P4, phase 2, 3 |
| 13 | Filter, cost tiers, effort map | Medium | Accept | P3, phase 2 |
| 14 | Unknown-value flags and cost surface | Medium | Accept (modified, U3 confirmed by user) | U3, P13, phase 2, 6 |
| 15 | Escapes, client, paging, aliases, duplicate request, Authenticate loop, phase 7 logout, e2e picker | Medium | Accept | phase 1, 4, 5, 7 |

### Whole-Plan Consistency Sweep

- Old terms searched in all plan files: `providers.Registry` (new type), `registry.go` (create), `MetadataUnknown`, `ContextKnown`, `CostKnown`, `fingerprint`, `credential generation` (as key), `denied` (as persisted state), `status.go`, `~600 KB`, `models.dev has no Token Plan`, `13 test files`, `ask -p --provider anthropic --model`.
- Result: each term is removed or rewritten in `plan.md` and phases 1-7. The phase 5 file name stays `phase-05-registry-and-refresh.md` for link stability; its content uses `ModelSet`.
- No unresolved contradiction.

## Validation Log

### Session 1 — 2026-10-28

- The interactive validation interview was skipped: the user asked to start `/ak-cook --auto` when the plan is ready ("xong thì ak-cook --auto luôn nhé").
- Verification came from the red-team pass, at the Full tier with 4 roles. Code claims were checked again during adjudication; see the "Verified" column of the [adjudication](../reports/red-team-261028-account-model-discovery.md).
- Open items that phase 1 decides from evidence, without a stop (no evidence means hide or `unknown`):
  - P2: the show or hide rule for each Anthropic family.
  - P12: what a list 401 or 403 means for each method.
  - Anthropic OAuth listing against the vendor terms.
  - The Token Plan `/models` endpoint.
  - Alias and dated id forms.
- Phase 6 has a hard gate: the user must approve the TUI design file before any TUI code.

### Design review — 2026-10-28 (phase 6 `design.md`, in progress)

- Q1 (footer row above the bar, or one merged row): user chose a separate footer row above the shortcut bar (two bottom rows).
- Q2 (all status data to the footer): already decided by U8.
- Q4 (one space of left padding), Q8 (`not in account list`, the user's wording) and Q9 (keep `design.md`): lead decisions, following the design recommendations.
- Q6 (cost on the reply meta line): user chose to keep it (A). Only the footer row shows no cost.
- Q3 (logo): user chose the first segment of the footer row (A).
- Q5 (unknown context window): user chose a lone muted `?` (A).
- New (user, 2026-10-28): the footer shows the model as `provider/model` (for example `anthropic/claude-opus-4-9`). When space is short, the directory shortens, then hides, before the model name; under 60 columns the provider stays and the end of the model id is cut with `…`.
- Q7 (`not in account list` note under 60 columns): user chose (B): the tag `unlisted` replaces `current` on that row under 60 columns.
- New (user, 2026-10-28): remove the model line from the block pane: no start line (`model  <provider>/<model>`) and no change line, because the footer always shows `provider/model`. Keep the fallback note as its own notice line.

## Paused — 2026-10-28 23:2x (user request "tạm dừng implement")

- Phases 1-5 are implemented in the working tree, not committed.
- Phase 5 additions that need user confirmation: the env switch `ASK_DISABLE_MODEL_REFRESH=1` (used by tests that run the real `ask`), the re-recorded cassette `cmd/tui/testdata/cassettes/TestCassetteThinkingReply.yaml` (one live OpenAI call; key scan clean), and cache deletion on save as well as removal.
- Phase 6 waits for the user's approval of `design.md`. Phase 7 has not started.
- Known flaky terminal tests under full-suite load; `TestLeaderMaximumFrameAndOversizeClientIsolation` fails under `-race` on a clean HEAD copy too.
- Design approved by the user on 2026-10-29 ("duyệt"); `design.md` frontmatter and its template record it. Phase 6 TUI code may start.
- Design change (user, 2026-10-29): the footer row always shows the directory and shows no leader part (no dot, no label, no id) in the healthy state. This replaces the earlier narrowing order and fixes criterion 11. `design.md` goes back to `draft` until the user approves again.
- Design change (user, 2026-10-29): the footer shows no link state in any state (option B). The logo `◢◤ ask` moves to a slim header row at the top of the screen; the footer row keeps directory, `provider/model`, thinking level and context. `design.md` stays `draft` until the user approves again.
- Header rule question (user, 2026-10-31): option A, keep a `─` rule under the slim header row. `design.md` stays `draft` until the user approves it as a whole.
- Design approved by the user on 2026-10-31 ("ok duyệt") with D13-D16. The footer/header code must be changed to match.
