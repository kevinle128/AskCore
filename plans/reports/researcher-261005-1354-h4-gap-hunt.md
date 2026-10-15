# H4 gap hunt: what "one in-memory conversation switches between Anthropic and OpenAI with correct thinking replay" needs, and the roadmap does not place

Date: 2026-10-05. Mode: read-only research. No code was changed.
Scope: what is AROUND `openai-completions.ts`, `openai-responses*.ts`, `transform-messages.ts` and fantasy. The other lanes cover those internals.

## 0. Sources and method

| Item | Value |
|---|---|
| Pi checkout | `/Users/dale/Desktop/workspace/opensources/pi`, v1.0.1 `4c6fb7cfe`. The roadmap pins `2bbfcca4`. |
| Path shorthand | `AI:` = `packages/ai/src/`, `C:` = `packages/coding-agent/src/`, `GM:` = `packages/ai/scripts/generate-models.ts`. |
| Drift check | `git diff --stat 2bbfcca4 4c6fb7cfe` on the H4 files: only `openai-responses-shared.ts`, `env-api-keys.ts` (+5 lines), `utils/retry.ts` (+1 line) and `types.ts` (1 line) changed. Those ten files are identical at both commits, so their line numbers hold at both. `GM:` (`generate-models.ts`) and `C:core/*` citations are at `4c6fb7cfe` only: both changed after the pin (`49b9df489` in `model-resolver.ts`; `28eaccb8e`, `c10bfb0d7`, `4665fafb4`, `4812cb268` in the generator). |
| Ask tree | `/Users/dale/orca/workspaces/AskCore/master-2`, branch `master-2`. H3 code (`internal/providers/tokenplan/`, `transform.go`) is uncommitted work in the tree. Claims about it are about the working tree on 2026-10-05. |
| Fantasy | `charm.land/fantasy@v0.45.2` in `/Users/dale/Desktop/workspace/go/mobules/pkg/mod/`. |
| GitNexus | The GitNexus MCP tools were not in this agent's tool list. The path was traced by direct reads and grep. See section 9. |
| Live probes | None. No call was made with the user's key. |

## 1. Outcome

The roadmap places the two OpenAI wire APIs and the replay tests in H4. It does not place the parts that make the exit reachable and safe. I found 17 gaps. Seven are high.

1. **The H4 target is not chosen (G1, user decision).** No document names the OpenAI model, the completions provider, or the key. The only provider key in this shell is `ALIBABA_TOKEN_PLAN_API_KEY`. Pi itself serves Alibaba Token Plan models over `openai-completions` on the same host. A probe can show if that route works for the user's key.
2. **A provider switch sends the first provider's `--api-key` to the second provider (G2, security).** The Go loop keeps one `Options.APIKey` for all calls. Pi scopes `--api-key` to one provider.
3. **The OpenAI Go SDK reads `OPENAI_*` env variables on its own (G3, security).** This includes `OPENAI_ORG_ID`, `OPENAI_PROJECT_ID`, `OPENAI_CUSTOM_HEADERS` and `OPENAI_BASE_URL`. They apply to every completions request, also to third-party hosts.
4. **Ask has no way to switch a model (G4).** The loop has one fixed stream function. The `Agent` has no `SetModel`. `set_model` and `/model` are in H13. The exit has no surface.
5. **The Model record has no field that H4 needs (G5).** No base URL, cost, thinking map, compat record or headers.
6. **Four inventory rows have no owner for their OpenAI half (G6).** These are the OpenAI env key (H-AUTH-05), the OpenAI quirk data (H-PROV-26), the OpenAI cache fields (H-PROV-14 vs H-PROV-04) and the thinking map data (H-PROV-12).
7. **Replay needs three storage slots in the Ask message (G9).** The Responses reasoning item, the text item id and the `call_id|item_id` pair must fit in fields that exist.

Recommended order: decide G1 (with a probe), then build G2, G3, G4 and G5 before any adapter code, then the rest.

## 2. The Pi path of `-p` on an OpenAI model, and a model switch

| Step | Pi evidence | Ask today |
|---|---|---|
| Parse `--provider`, `--model`, `--thinking`, `--api-key` | `C:cli/args.ts:114-118,160` | Parsed. `cmd/tui/args.go:60-75`. |
| Resolve model. No `--model`: first provider with auth, default `openai` -> `gpt-5.5` | `C:core/model-resolver.ts:22-28` | `openProvider` accepts only `faux` and `alibaba-token-plan` (`cmd/tui/headless_faux.go:70-79`). |
| Bind `--api-key` to the provider of the initial model only | `C:main.ts:827-834`; `C:core/runtime-credentials.ts:12-14,28-34` | One global `APIKey` (`cmd/tui/headless.go:146`). Gap G2. |
| Default level `medium`, then clamp to the model | `C:core/sdk.ts:237-262`; `AI:models.ts:1217-1247` | `medium` only for Token Plan (`headless.go:130-133`). No clamp in the tree. |
| Loop calls `getApiKey(provider)` before every request | `A:agent-loop.ts:392-402` | Hook exists (`internal/pipeline/hooks.go:119-121`). Headless does not set it. |
| `streamSimple` -> `clampThinkingLevel` -> `stream` | `AI:api/openai-responses.ts:238-262`; `AI:api/openai-completions.ts:731-750` | Not built. |
| Build request, send with `maxRetries: 0`, parse, compute cost in the adapter | `AI:api/openai-responses.ts:101-236`; `AI:utils/provider-retry.ts` | Not built. |
| Mid-session switch: auth check, re-clamp level, `model_change` entry, `model_select` event | `C:core/agent-session.ts:2430-2450,2565-2587,2622-2634` | No API (G4). |
| Next request: `transformMessages` re-keys history for the new model | `AI:api/transform-messages.ts:64-235` | Built in H3 (`internal/providers/transform.go`). |

## 3. Gap list

Severity: **High** = the exit fails, or a secret or wrong data leaves the process. **Medium** = wrong result in a real case, or rework later. **Low** = text or small item.
"Decision" = a user decision under `CLAUDE.md` rule 3.

| ID | Sev | Gap | Owner today | Suggested owner | Decision |
|---|---|---|---|---|---|
| G1 | High | H4 target provider, model and key are undefined | none | H4 start | Yes |
| G2 | High | `--api-key` is global and leaks across a switch | none | H4 | No |
| G3 | High | OpenAI SDK ambient env (`OPENAI_*`) | none | H4 | No |
| G4 | High | No switch API, one fixed stream function | H13 (`set_model`) | H4 (core), H13 (surface) | Partly |
| G5 | High | Model record lacks the H4 fields | H3 (Anthropic only) | H4 | No |
| G6 | High | Four inventory rows have no OpenAI owner | H3, H17 | H4 | No |
| G7 | Medium | Env name for OpenAI keys vs D7 | none | H4 | Yes |
| G8 | Medium | Thinking: default, clamp, `off`, summary, switch rule | H3 (Anthropic) | H4 | Yes (switch rule) |
| G9 | High | Replay slots in the Ask message for Responses | H4 (implied) | H4 | No |
| G10 | Medium | Usage, cache read, service tier, cost for OpenAI | H9 | H4 keeps data, H9 prices | Yes (same as H3-G9) |
| G11 | Medium | Error and stream-end data for H9, and retry lock-in | H9 | H4 keeps, H9 classifies | No |
| G12 | Medium | No session id in headless, so no cache key | H8 | H2 headless or H4 | No |
| G13 | Medium | H4 tests list is too thin, and live tests prove little | H4 (2 lines) | H4 | No |
| G14 | Medium | Package shape: provider-per-package vs adapter-per-API | H3 | H3/H4 | Yes |
| G15 | Low | `samplingParams` and `ExtraBody` merge has no owner | H3, H7 | H7 | No |
| G16 | Low | ChatGPT sign-in heuristic and tool-id provider set | none | H4 (document, do not copy) | No |
| G17 | Low | Stale or wrong text (section 7) | n/a | user applies | n/a |

## 4. Details

### G1. The H4 target is not chosen (high, decision)

Evidence:
- Roadmap H4 (`roadmap.md:222-243`) names no model, no provider and no key. H3 now targets `deepseek-v4.1-flash` on Alibaba Token Plan over the Anthropic wire (H3 analysis, section 10).
- Pi's `openai` provider is **Responses only** (`AI:providers/openai.ts:1-24`, base URL `https://api.openai.com/v1`, key `OPENAI_API_KEY`). Pi has no provider named "OpenAI" on completions. Completions is the wire of 26 other providers (`grep openAICompletionsApi AI:providers/*.ts`): DeepSeek, OpenRouter, Groq, Qwen Token Plan, and others.
- Pi's default OpenAI model is `gpt-5.5` (`C:core/model-resolver.ts:22-28`).
- This shell has `ALIBABA_TOKEN_PLAN_API_KEY` set and no `OPENAI_API_KEY`, `OPENROUTER_API_KEY` or `DEEPSEEK_API_KEY`. (Only the shell of this research. The user may keep keys elsewhere.)
- **Token Plan has an OpenAI-compatible route.** Pi's `qwen-token-plan` provider is `openai-completions` at `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1` (`AI:providers/qwen-token-plan.ts:8-12`; `GM:2593-2615`). It is the same host as the H3 Anthropic route (`.../apps/anthropic`). Pi's compat for it: `thinkingFormat: "qwen"`, `supportsDeveloperRole: false`, `supportsStore: false` (`GM:2594-2599`). Alibaba's docs list the same base URL (web search, section 9).
- Pi's **Individual-plan allowlist** has `deepseek-v4-flash-0731` and `deepseek-v4-pro` (`QWEN_TOKEN_PLAN_INDIVIDUAL_MODEL_IDS`, `GM:322-332`), not the user's id `deepseek-v4.1-flash`. The team `qwen-token-plan` provider has no allowlist (`GM:2600-2605`). It takes every tool-capable model of the models.dev `alibaba-token-plan` source, which is not in the checkout, so its list cannot be read offline. Whether `deepseek-v4.1-flash` is served on `/compatible-mode/v1` is **not verified**.
- Pi also has a native DeepSeek record on completions: id `deepseek-flash`, "DeepSeek V4.1 Flash", base URL `https://api.deepseek.com`, compat `thinkingFormat: "deepseek"` and `requiresReasoningContentOnAssistantMessages: true` (`GM:3099-3115`). This needs a DeepSeek platform key.

Options:

| Option | Completions target | Responses target | Cost to user | Risk |
|---|---|---|---|---|
| A | Token Plan `/compatible-mode/v1` (same key) | OpenAI `gpt-5.5` (needs a new OpenAI key) | one OpenAI key | Model id may differ on the compat route. Token Plan terms limit it to interactive use. |
| B | Token Plan compat route | Offline tests only for Responses | none | Weak exit: `openai-responses` replay is never run against a real server. This is the API with the 5 replay regressions (E§3 #2). |
| C | DeepSeek native `api.deepseek.com` | OpenAI `gpt-5.5` | two new keys | Most keys. Best fidelity to Pi's own records. |

Recommendation: **A**, after one probe of `/compatible-mode/v1` with the user's key and the model id `deepseek-v4.1-flash`. The probe is a decision for the user because it spends their paid key (the H3 probe was done at their request). If the user has no OpenAI key, say so now: the exit then becomes B and the roadmap text must say that the Responses wire is proven by golden wire tests only.

Same-model note: the H3 Token Plan Anthropic wire and the Token Plan completions wire carry the same model id but different `API`. `sameModel` compares API, provider and id (`internal/providers/transform.go:278-280`), so a switch between them is a cross-model replay. This is a cheap real test of "thinking replay across wires" with one key (option A or B).

### G2. `--api-key` leaks across a provider switch (high, security)

Evidence:
- `cmd/tui/headless.go:146` builds `Options: providers.StreamOptions{..., APIKey: o.apiKey}` once for the whole agent.
- `internal/agent/loop_stream.go:32-41` copies `l.cfg.Options` for each request. `GetAPIKey` overrides it only when the hook is set **and** returns a non-empty key. The headless agent sets no hook.
- `internal/agent/loop_run.go:243-244`: a `PrepareRequest` update can change `Model` only. `Options.APIKey` stays.
- After H4's switch, the Anthropic or Token Plan key goes to `api.openai.com`. A secret goes to a third party.
- Pi binds the flag to one provider id: `C:main.ts:827-834` calls `setRuntimeApiKey(sessionOptions.model.provider, parsed.apiKey)`. `RuntimeCredentials` is a per-provider map (`C:core/runtime-credentials.ts:12-14,28-34`).
- H7 owns H-AUTH-01 (precedence). H7 comes after H4, so it does not protect the H4 test.

Fix: resolve the key per provider in a `GetAPIKey` hook (`--api-key` only for the initial provider, then env). Remove the global `APIKey` fallback for a model whose provider differs. Test: switch providers with `--api-key` set and assert on both `httptest` servers that the second one never sees the first key.

### G3. OpenAI SDK ambient env (high, silent)

Evidence:
- The SDK that fantasy uses is `github.com/charmbracelet/openai-go@v0.0.0-20260921175203-216db9e71b83` (`fantasy@v0.45.2/go.mod:13`). Its `DefaultClientOptions` read `OPENAI_BASE_URL`, `OPENAI_API_KEY`, `OPENAI_ADMIN_KEY`, `OPENAI_ORG_ID`, `OPENAI_PROJECT_ID`, `OPENAI_WEBHOOK_SECRET` and `OPENAI_CUSTOM_HEADERS` (`client.go:74-95` in that module).
- Fantasy passes `WithAPIKey` and `WithBaseURL` only when the value is non-empty (`fantasy@v0.45.2/providers/openai/openai.go:170-175`).
- Effect for H4: a user who has `OPENAI_ORG_ID` or `OPENAI_PROJECT_ID` set sends them as headers to Token Plan, DeepSeek or OpenRouter. `OPENAI_CUSTOM_HEADERS` goes the same way. With an empty key, `OPENAI_API_KEY` goes to the third-party host. With an empty base URL, `OPENAI_BASE_URL` redirects the request.
- This is the same class as H3-G2 for Anthropic (`ANTHROPIC_AUTH_TOKEN`). Pi pins both `apiKey` and `baseURL` in the client constructor (`AI:api/openai-completions.ts:752-793`). Pi's `openai` npm SDK may also read `OPENAI_ORG_ID`. This was not verified (no `node_modules` in the checkout).

Fix: always set a non-empty key and a base URL from the record; fail fast on an empty key; neutralize the org, project and custom-header env through `WithSDKOptions` (the option list exists, `openai.go:118`). Test with all `OPENAI_*` env set and an `httptest` server that asserts the request headers and the host.

### G4. No switch API, one fixed stream function (high)

Evidence:
- `LoopConfig` has one `Stream` and one `Model` (`internal/agent/loop_run.go:22-27`).
- `Agent` exposes `Prompt`, `Continue`, `Abort`, `WaitForIdle`, `Reset`, `State` (`internal/agent/agent.go:63-135`). It has no `SetModel` and no `SetThinkingLevel`.
- `openProvider` returns one `StreamFn` for one provider (`cmd/tui/headless_faux.go:70-79`).
- Roadmap: `set_model` is in H13 (`roadmap.md:471`), `/model` in H13 (`roadmap.md:476`), the `model_change` entry in H8 (H-SESS-03, `roadmap.md:316`), `cycle_model` in H17. H4 owns H-PROV-21 "in memory" with no surface.
- Pi's `setModel` does four things: auth check (`agent-session.ts:2431-2433`), re-clamp the thinking level (`:2436-2447`), append `model_change` (`:2438`), emit `model_select` (`:2449`).

Fix, smallest: (a) a provider registry that returns the `StreamFn` for `Model.API` or `Model.Provider`, used as the loop's `Stream`; (b) `Agent.SetModel(model)` and `SetThinkingLevel(level)` that re-clamp and apply between runs; (c) per-provider key and cache options (G2, G12).
How to show the exit without `/model` or H8: a Go integration test with two `httptest` servers (one Anthropic-shaped, one OpenAI-shaped) and the same agent value, plus one env-gated live test. Do **not** add a CLI flag for it. `--provider` is per process, and H13 gives `set_model`. User decision only if the user wants a visible demo before H13. Then the options are a hidden `ASK_` test command or a scripted live test.

### G5. The Model record lacks the H4 fields (high)

Evidence:
- `internal/providers/model.go:5-17` has `ID, Name, API, Provider, Reasoning, Input, ContextWindow, MaxTokens`. The Token Plan base URL is a package constant (`tokenplan/provider.go:25`), not a field.
- H3 owns H-PROV-18 (`roadmap.md:205`) for Anthropic. H4 needs more.

Fields H4 must carry:

| Field | Why | Evidence |
|---|---|---|
| `BaseURL` | one completions adapter serves many hosts | `AI:api/openai-completions.ts:790` (`baseURL: model.baseUrl`) |
| `Headers` | per-model headers | `AI:api/openai-completions.ts:761` |
| `ThinkingLevelMap` (`string` or unsupported) | `off`->`none`, `minimal` unsupported, `xhigh`, vendor names | `AI:types.ts:85-87,1119-1123`; `GM:1060-1081` |
| `Cost` with `Tiers` | JSON mode shows cost | `AI:models.ts:1193-1213` |
| Compat for completions (26 fields) | request shape per vendor | `AI:types.ts:789-868`; `AI:api/openai-completions.ts:1585-1726` |
| Compat for responses (10 fields) | `supportsLongCacheRetention`, `sessionAffinityFormat`, `supportsMaxOutputTokens`, `supportsStrictMode` | `AI:api/openai-responses.ts:82-95` |
| `SamplingParams` | last-wins body merge | `AI:api/openai-responses.ts:382`; `AI:api/openai-completions.ts:999` |

Values for a `gpt-5.5` record (from `GM`, because the Pi catalog data is not in the checkout: `.gitignore:11` ignores `packages/ai/src/providers/data/`, and `AI:providers/openai.models.ts` imports it):
- `contextWindow` 272000 and `maxTokens` 128000 (`GM:387-397,2941-2944`). The window is capped on purpose to stay in the short-context price tier.
- Cost tier above 272000 input tokens: input x2, output x1.5, cache x2 (`GM:416-427`).
- `thinkingLevelMap`: `off: "none"` (`GM:1060-1066`), `minimal: null` (`GM:1075-1077`), `xhigh: "xhigh"` (`GM:1071-1073`).
- Price numbers for `gpt-5.5` come from models.dev at generation time (`OPENAI_STANDARD_COSTS` has no `gpt-5.5` row, `GM:440-449`). Verify them against OpenAI's price page before you hard-code them.

For the Token Plan completions record (option A): `thinkingFormat: "qwen"`, `supportsDeveloperRole: false`, `supportsStore: false`, `supportsReasoningEffort: true` and a map such as `high: "high"`, `max: "max"`, others null for models with no effort metadata (`GM:303-310,2594-2599`). Check this against the live endpoint. Pi's `qwen` format sends `enable_thinking` (`AI:api/openai-completions.ts:888-894`). A DeepSeek model on that route may need the `deepseek` format. This is not verified.

### G6. Four inventory rows have no OpenAI owner (high)

| Row | Inventory text | Roadmap owner | Missing |
|---|---|---|---|
| H-AUTH-05 | "P0 (Anthropic, OpenAI), P1 (rest)" (`inventory-harness.md:134`) | H3 owns "the Anthropic env key"; H17 owns "the other vendors" (`roadmap.md:569`) | `OPENAI_API_KEY` and the chosen completions key. H4 "Owns" lists neither. |
| H-PROV-26 | "P0 (chosen vendors)" (`inventory-harness.md:122`) | H3: "the Anthropic quirk data" (`roadmap.md:207`) | OpenAI vendor rules: `store:false`, developer role, token-limit field, tool-id charset. |
| H-PROV-14 | "P0 (Anthropic), P1 (others)" (`inventory-harness.md:110`); H17 owns "other vendors" (`roadmap.md:569`) | H17 | H-PROV-04 (owned by H4) lists `prompt_cache_key` and retention (`inventory-harness.md:100`). The two rows conflict. See G12. |
| H-PROV-12 | thinking levels and clamp | H3 | The OpenAI `thinkingLevelMap` data and the per-API effort mapping. See G5 and G8. |

Fix: add all four to the H4 "Owns" list. Not a decision.

### G7. Env name for OpenAI-family keys (medium, decision)

H3 code reads `ASK_ALIBABA_TOKEN_PLAN_API_KEY` first, then `ALIBABA_TOKEN_PLAN_API_KEY` (`internal/providers/tokenplan/provider.go:30-31,145-150`). D7 sets the `ASK_*` prefix. Pi uses plain names: `OPENAI_API_KEY`, `DEEPSEEK_API_KEY`, `OPENROUTER_API_KEY`, `QWEN_TOKEN_PLAN_API_KEY` (`AI:env-api-keys.ts:86-100`).
Question: do OpenAI-family providers use the same two-name pattern (`ASK_OPENAI_API_KEY` then `OPENAI_API_KEY`)? Recommendation: yes. The `ASK_` name lets a user keep the SDK-ambient `OPENAI_*` variables away from Ask (G3). Option: plain Pi names only (simpler, but conflicts with G3 hygiene).

### G8. Thinking: default, clamp, `off`, summary, switch rule (medium, partly decision)

Evidence:
- Default `medium` for every reasoning model, then clamp (`C:core/sdk.ts:237-262`). Ask sets `medium` only for Token Plan (`headless.go:130-133`). No clamp exists in the Go tree.
- Responses `off`: sends `reasoning:{effort: map.off ?? "none"}` unless `map.off === null` (no `reasoning` field at all), and `provider !== github-copilot` (`AI:api/openai-responses.ts:361-377`). With effort set: `reasoning:{effort, summary:"auto"}` and `include:["reasoning.encrypted_content"]` (`:361-372`). Pi's `streamSimple` never passes a summary option, so `"auto"` is the shipping value.
- Completions: 10 thinking formats (`openai`, `zai`, `qwen`, `qwen-chat-template`, `chat-template`, `baseten`, `deepseek`, `openrouter`, `ant-ling`, `together`, `string-thinking`) at `AI:api/openai-completions.ts:875-985`. The chosen target decides which are needed (G1).
- Switch rule: on a switch Pi takes `settings default ?? current level ?? medium` (`C:core/agent-session.ts:2622-2634`) and then clamps. The in-session level is overwritten (`:2573`). A clamp through a non-reasoning model is restored only from a persisted settings default (changelog `C:CHANGELOG.md:3124`; E§13 handoff rules). With no settings in H4, Anthropic(high) -> non-reasoning -> Anthropic gives `off`.
- Ask `StreamOptions.Reasoning` uses `""` for off (`internal/agent/loop_run.go:249,279-282`).

Decision for the user: keep Pi's rule (level is lost after a non-reasoning model unless a settings default exists), or keep a separate "requested level" that survives the switch (a deviation, small). Recommendation: the deviation, because H4 has no settings (H6), so Pi's rule would show a surprising `off` in the exit demo. Owner: H4.

### G9. Replay storage slots in the Ask message (high)

"Correct thinking replay" for Responses needs three values that Pi stores in message fields:
- Reasoning item JSON in the thinking signature (`AI:api/openai-responses-shared.ts`, `thinkingSignature`).
- Text item id and phase as `textSignature` v1 JSON (`AI:api/openai-responses-shared.ts:53-59,703`).
- Tool-call id as `callId|itemId` (`:168,292,333`).

Ask has `Thinking.ThinkingSignature *string`, `Text.TextSignature *string`, `ToolCall.ID`, `ToolCall.ThoughtSignature`, `ToolCall.Namespace` (`pkg/protocol/content.go:28-55`) and `AssistantMessage.ResponseID` (`message.go:169`). The slots exist. Fantasy keeps the same data in typed provider metadata (`ResponsesReasoningMetadata{ItemID, EncryptedContent, Summary}`, `fantasy@v0.45.2/providers/openai/responses_options.go:98-102`). The adapter must serialize it into the Ask strings and read it back. If it does not, the replay silently loses the encrypted reasoning, and OpenAI returns the "reasoning without following item" 400 (E§13).
New rule since the pin: drop the item id when the model differs **or** the id prefix does not match the replayed item type (`fc_` for function calls, `ctc_` for custom tool calls) (`AI:api/openai-responses-shared.ts`, commit `bc2d8dc1c`, 2026-10-01).
Owner: H4, shared with the fantasy lane. Test: round trip of all three slots through a stored and re-read message, then a replay to the same model, then to another model.

### G10. Usage, cost, cache read, service tier (medium, decision)

Evidence:
- Completions: `input = prompt_tokens - cached - cacheWrite`; `totalTokens = input + output + cacheRead + cacheWrite`; `reasoning` is a subset of output (`AI:api/openai-completions.ts:1511-1552`).
- Responses: same subtraction, but `totalTokens` is the provider's `total_tokens` (`AI:api/openai-responses-shared.ts:559-578`).
- Cache hits in other places: DeepSeek `prompt_cache_hit_tokens`, Kimi top-level `cached_tokens` (`AI:api/openai-completions.ts:1522-1523`). Fantasy reads only `prompt_tokens_details.cached_tokens` (`fantasy@v0.45.2/providers/openai/language_model_hooks.go:244-259`). DeepSeek cache hits would count as full input with `cacheRead` 0. Cost would be too high for DeepSeek.
- Service tier: Pi multiplies cost by 0.5 (`flex`), 2 (`priority`, `fast`) or 2.5 (`gpt-5.5` priority) using the tier echoed in the response, or the requested tier (`AI:api/openai-responses.ts:389-415`; `openai-responses-shared.ts:578-583`). The coding agent never sets `serviceTier` (no match in `C:` or `A:`), so only the echo matters. Fantasy has the request tier only (`responses_language_model.go:275,354-371`). An echoed `priority` or `flex` tier cannot be priced without an SSE tap.
- Tier thresholds use `input + cacheRead + cacheWrite` (`AI:models.ts:1194`).
- Cost is micro-USD integers in Ask (`pkg/protocol/usage.go:13-25`) and USD floats in Pi. The price table needs conversion.
- Fantasy's `FoldDisjointReasoning` (`language_model_hooks.go:216-221`) is sound for the OpenAI shape.

H9 owns pricing (H-RETRY-07, `roadmap.md:357`). JSON mode (H2) already shows `usage` on `message_end`, so the first real OpenAI run shows cost 0 unless H4 fills it. Decision: same as H3-G9. Recommendation: H4 carries token counts and cache read correctly, with a plain price table for the chosen records. H9 adds tiers, the echoed service tier and the returned model.

### G11. Error and stream-end data for H9 (medium)

H9 classifies by text (`AI:utils/retry.ts`, `overflow.ts`). What H4 must keep, so that H9 can work:
- Status and body in the error text. Fantasy's Responses failure text carries message and code (`responses_language_model.go:1336-1340`). A mid-stream `error` event can come with HTTP 200 (E§8 "provider error returned as a successful message").
- Stream end without a terminal event is an error for each protocol: completions "Stream ended without finish_reason" (`AI:api/openai-completions.ts:697`), Responses "ended without a stop reason" (`AI:api/openai-responses.ts:206`). Retry pattern `ended without` matches them (`AI:utils/retry.ts:80`). Map to H1 `ErrStreamIncomplete` (as H3-G4).
- OpenAI-family strings that exist in Pi's tables and need fixtures: overflow `exceeds the context window` (`AI:utils/overflow.ts:42`), `range of input length should be` for Token Plan (`:59`), generic `context_length_exceeded` (`:60`); non-retryable `insufficient_quota`, `billing`, `subscription_sharing_usage_limit_exceeded`; retryable `model is at capacity` (new, `3874b3e98`), `you can retry your request` (Responses mid-stream, `AI:utils/retry.ts:91`).
- Provider-level retry in Pi has jitter (x0.75 to x1.0, `AI:utils/provider-retry.ts:66`) and honors `x-should-retry` (`:24`). D11 ("no jitter") is about agent-level retry (`AI:utils/retry.ts:122-126`). It is off while provider `maxRetries` is 0, which is the H-RETRY-04 default. No action, but D11 text should say "agent level".
- Fantasy sets `WithMaxRetries(0)` in the `openai` provider (`providers/openai/openai.go:168`). Add a lock-in test for the completions and Responses clients, as H3 did.

### G12. No session id in headless (medium)

Evidence:
- `Agent.Config.SessionID` goes to `Options.SessionID` only when set (`internal/agent/agent.go:172`; `types.go:31-33`). `cmd/tui` sets none (no match for `SessionID` in `cmd/tui/*.go`). Pi always has an id: `sessionId: sessionManager.getSessionId()` (`C:core/sdk.ts:411`).
- Effect: no `prompt_cache_key` (`AI:api/openai-responses.ts:334`), no `session_id`, `x-client-request-id` headers (`:276-290`; completions also `x-session-affinity`, `AI:api/openai-completions.ts:769-780`). OpenAI caches the prefix by itself, but the key raises the hit rate.
- Completions sends the key only for `api.openai.com` or long retention on a supporting vendor (`AI:api/openai-completions.ts:821-823`). `supportsLongCacheRetention` is false for Together, Cloudflare, NVIDIA, Ant Ling (`:1670-1676`). Key length is clamped to 64 characters (`AI:api/openai-prompt-cache.ts:1-8`).
- `cacheRetention` is never set by the coding agent. The default is `short`, `none` removes the key (`AI:api/openai-responses.ts:159,334`). The env name `PI_CACHE_RETENTION` has no Ask name yet (H3-G10).

Fix: create one UUID per process in headless (Pi does the same for in-memory sessions). H8 replaces it. Resolve the H-PROV-04 vs H-PROV-14 conflict (G6): recommendation is that H4 owns the OpenAI cache fields, because H-PROV-04 lists them. Not a decision.

### G13. H4 tests are two lines, and live tests prove little (medium)

Roadmap H4 tests: "Replay across all chosen APIs" and "Stream completeness for each protocol". Missing cases, each with evidence above:
- Golden wire JSON per compat flag (developer role vs system, `max_tokens` vs `max_completion_tokens`, `store`, `stream_options.include_usage`, `reasoning_effort` per map, `thinking` format per vendor, `tools: []` when history has tool calls (`AI:api/openai-completions.ts:857`)).
- Reasoning item pairing: `rs_` item with its `fc_` item; errored and aborted assistant messages skipped; ids omitted across models (G9).
- Responses `max_output_tokens` minimum of 16 (`AI:api/openai-responses.ts:30-31,339-341`). Only `max_output_tokens` is a length stop.
- Usage fixtures: cached subtraction, reasoning subset, DeepSeek `prompt_cache_hit_tokens`, missing usage (local servers).
- Missing `output_index` (llama.cpp) ends with an error, not a run (E§7).
- Same-model switch test between the two Token Plan wires (G1).
- Third-party completions servers are lenient, so live acceptance proves little (the H3 probe showed the same for orphan tool calls). Keep assertions on request JSON.
- Live tests: env-gated, manual (Token Plan terms limit it to interactive use).

### G14. Package shape (medium, decision)

`internal/providers/tokenplan/` is a per-provider package. It imports fantasy's Anthropic provider (`tokenplan/provider.go:14-18`). The roadmap says one adapter is reused for OpenAI (`roadmap.md:197`, H3 "Wire layer"). `docs/ask-architecture-reference.md:115` says `internal/providers/<vendor>*.go`. H4 adds two wires and several hosts. Question: where do the shared parts live (idle reader, error mapping, key resolution, price calculation, registry)? Recommendation: one package for the fantasy adapter code with one file per wire, plus records as data. Keep `tokenplan` as a record and base URL only. The user owns the dewee layout, so this is a decision.

### G15. `samplingParams` and extra body (low)

Pi merges `model.samplingParams` and request `samplingParams` into the body last, so custom keys can override named fields (`AI:api/openai-responses.ts:382`; `AI:api/openai-completions.ts:999`). H-PROV-18 and H-PROV-07 list the fields (H3) and `models.json` is H7. No phase builds the merge. Fantasy has `openaicompat.ProviderOptions.ExtraBody` (`providers/openaicompat/provider_options.go:40`), which is enough. Pi has no temperature flag (`C:cli/args.ts`) and sends `temperature` only when a caller sets it, so reasoning-model temperature rules do not arise in H4. Keep the field, build the merge in H7.

### G16. Low items

- `isChatGPTSignIn`: a key that does not start with `sk-`, sent to `https://api.openai.com/v1`, is treated as a ChatGPT token. Pi then omits `max_output_tokens`, `temperature` and cache fields (`AI:api/openai-responses.ts:40-47,329-333`). Ask has no ChatGPT OAuth in M1 (H-AUTH-06 is H17, M2). Do not copy the heuristic.
- `OPENAI_TOOL_CALL_PROVIDERS = {openai, openai-codex, opencode}` keeps the full tool-call ids only for these (`AI:api/openai-responses.ts:30`). Put it in the record as a flag.
- `getClientApiKey` accepts an empty key when an `Authorization` header is set (`AI:api/openai-completions.ts:82-86`). Keyless local servers (Ollama) need a dummy key, `"apiKey": "ollama"` (`C:docs/models.md:55-64`). Both are H7 (`models.json`). `openai-completions` cannot reach a user endpoint before H7, so H4 exits on built-in records only.
- Tool schemas: `constrained-sampling.ts` is **strict JSON schema conversion**, not sampling (`AI:api/constrained-sampling.ts:1-60`). Strict mode is off by default for both APIs (`supportsStrictMode` default `false`, `AI:api/openai-responses.ts:88`; `AI:api/openai-completions.ts:1667`). H4 needs no strict conversion. Unlike the Anthropic path (H3-G11), fantasy's OpenAI path passes the schema map as is (`providers/openai/language_model.go:814`).

## 5. Answers to the ten checks

| # | Check | Answer |
|---|---|---|
| 1 | Auth for OpenAI-family | Env map `AI:env-api-keys.ts:73-111`; rows H-AUTH-05 (H3 Anthropic only, H17 rest), H-AUTH-01/02/03 (H7), H-PROV-17 `models.json` (H7). No row covers `OPENAI_API_KEY` in H4 (G6). Keyless servers need a dummy key (H7). Per-request key: hook exists, headless does not use it (G2). Headers: `model.headers`, merged last with request headers (`AI:api/openai-completions.ts:760-790`). |
| 2 | Model records | `gpt-5.5` on Responses is Pi's OpenAI default. Fields and values in G5. Target not decided (G1). Token Plan has a completions route (Pi `GM:2593-2615`). |
| 3 | Thinking, sampling | Level map and clamp: G5, G8. `constrained-sampling.ts` is strict-schema conversion (G16). `samplingParams`: G15. Temperature: no flag in Pi, not an H4 issue. |
| 4 | Usage and cost | G10. |
| 5 | Retry and errors | G11. H9 needs: status and body in text, stream-end errors, SDK retry 0, vendor strings as data. |
| 6 | Session id and cache | G12. |
| 7 | Switch surface | G4. Test harness only, no CLI flag. Open: if the user wants a visible demo before H13. |
| 8 | Edge cases E§3, E§7, E§13, E§15, E§24 | E§13 "model switch through a non-reasoning model" is not placed (G8). E§13 Responses rows (min `max_output_tokens`, out-of-order items, namespaces) are not in the H4 test list (G13). E§7 "missing `output_index`" is not placed (G13). E§15 idle timeout is H3 (H-PROV-25) and must also run on the OpenAI clients: pass the same transport (G14). E§24#8 "estimate fallback when usage is missing or zero" is H9, but local completions servers make it real in H4 (G13). |
| 9 | Pi drift | Section 6. |
| 10 | Stale text | Section 7. |

## 6. Pi changes between `2bbfcca4` and `4c6fb7cfe` that touch H4

| Commit | Change | H4 effect |
|---|---|---|
| `bc2d8dc1c` (2026-10-01) | Responses replay drops an item id when the model differs or the id prefix does not match the item type (`fc_` or `ctc_`) | Replay rule for G9. The pin predates it. |
| `3874b3e98` (2026-10-02) | Retryable pattern `model is at capacity` | Add to the H9 pattern data. |
| `a9424cd43` (2026-09-30), `b271b0a52` (2026-10-02) | Anthropic federation env keys (+5 lines in `env-api-keys.ts`); Anthropic inline tools beta | None for OpenAI. The federation env names make the H3-G2 ambient-chain risk wider. |
| `28eaccb8e`, `c10bfb0d7`, `4665fafb4`, `4812cb268`, `49b9df489` | Together model id, Cloudflare ids, Bedrock pricing, Cloudflare classifiers, NVIDIA default | None. Outside D6. |
| (none) `openai-completions.ts`, `models.ts`, `simple-options.ts`, `transform-messages.ts`, `overflow.ts` | No change since the pin | The pin is still valid for these files. |

Decision for the user (open since H3): keep the pin `2bbfcca4` or move it to `4c6fb7cfe`. For H4 the move costs one rule (`bc2d8dc1c`) and one pattern.

## 7. Stale or wrong text, verified against source

| Where | Text | Verdict |
|---|---|---|
| `roadmap.md:236` H4 "Do not rebuild" | `reasoningEffortMap` and `sendSessionIdHeader` | Correct. They were removed (`AI:CHANGELOG.md:567,1038`). Their replacements, `thinkingLevelMap` and `compat.sessionAffinityFormat`, are what H4 must build. The roadmap does not name them. Add them to the H4 "Owns" list (G5, G12). |
| `inventory-harness.md:99` H-PROV-03 | "About 25 providers" | 26 provider files (`grep openAICompletionsApi AI:providers/*.ts`), including `github-copilot` (three APIs). Fine as text. |
| `inventory-harness.md:100` H-PROV-04 | "openai, xai, meta, copilot GPT" | Incomplete. Also `opencode`, `opencode-go`, `cloudflare-ai-gateway` use Responses. |
| `inventory-harness.md:100` H-PROV-04 | "`prompt_cache_key` from session id" | Correct for Responses. In completions the key is sent only for `api.openai.com` or long retention on a supporting vendor (`AI:api/openai-completions.ts:821-823`). |
| `inventory-harness.md:117` H-PROV-21 | `C:core/agent-session.ts:2444-2500` | Now `:2430-2450` (set), `:2472-2550` (cycle). Line drift. |
| `inventory-harness.md:130,134` | `AI:env-api-keys.ts:29-148` | Now 195 lines. `getApiKeyEnvVars` is `:73`, the env map is `:84-111` and `getEnvApiKey` is `:153`. |
| `inventory-harness.md:106` H-PROV-10 | "foreign thinking becomes `<thinking>` text" | Already corrected for H3: plain text, no tags. Applies to the OpenAI replay too. |
| `inventory-harness.md:147-148` H-RETRY-01 | "no jitter" (`AI:utils/retry.ts:122-126`) | True for the agent-level delay. The provider-level delay has jitter (`AI:utils/provider-retry.ts:66`). Say "agent level" in D11. |
| `roadmap.md:197` H3 "H4 reuses the same adapter for OpenAI" | One adapter | The working tree has a per-provider package (G14). |
| `roadmap.md:222` H4 "Waits on D6" | D6 = B | D6 does not name a provider or a key. G1. |

## 8. Ranked recommendation

1. **Ask the user G1** (one question): option A, B or C, and whether an OpenAI key exists. Run the Token Plan compat probe first if the user agrees.
2. **Add to H4 before adapter code:** G2 (per-provider key), G3 (ambient env guard), G5 (record fields), G6 (four owner moves).
3. **Add to H4 with the adapter:** G4 (registry, `SetModel`, switch test), G9 (replay slots), G12 (session id), G8 (clamp and default level), G13 (test list).
4. **Keep for H9:** G10 (prices, tiers, echoed tier), G11 (classifier data). H4 only keeps the data.
5. **Ask the user, one per turn, after G1:** G7 (env names), G8 (switch rule), G14 (package shape).

Risk if H4 starts unchanged: **medium-high (7/10)**. G2 and G3 can leak a key or an org id. G4 and G5 are rework that grows after the first adapter lands.

## 9. Limitations

- GitNexus MCP (`mcp__gitnexus__*`) was not in this agent's tool list. The `-p` and switch path was traced with `grep`, `git` and direct reads. The H3 analysis did use GitNexus for the Anthropic call graph. Callers of `clampThinkingLevel` and `calculateCost` for the OpenAI adapters were read from source, not from the graph.
- No live call was made. The Token Plan compat route is shown by Pi's generator and by Alibaba docs found by web search (corroboration only: [Token Plan FAQ](https://www.alibabacloud.com/help/pt-br/model-studio/token-plan-faq), [more tools](https://help.aliyun.com/en/model-studio/more-tools), [OpenClaw provider page](https://docs.openclaw.ai/providers/modelstudio)). The model id on that route is unverified.
- The Pi model catalog data (`packages/ai/src/providers/data/*.json`) is generated and git-ignored. Prices and windows for `gpt-5.5` and the Token Plan models are not in the checkout. Only the generator rules were read.
- Pi's `openai` npm SDK env behavior was not checked (no `node_modules`).
- Fantasy internals (reasoning item replay, stream parts, error types) belong to the other lanes. This report cites them only where an H4 decision depends on them.
- The shell env check shows only this shell.

## Unresolved questions

1. G1: which H4 target, A, B or C? Does the user have an OpenAI key? May the research spend one Token Plan call on `/compatible-mode/v1` with `deepseek-v4.1-flash`?
2. G7: `ASK_OPENAI_API_KEY` then `OPENAI_API_KEY`, or plain Pi names?
3. G8: keep Pi's rule for the thinking level after a switch through a non-reasoning model, or keep a separate requested level?
4. G14: one fantasy adapter package with a file per wire, or one package per provider?
5. G4: is a test-only demo of the switch acceptable until H13, or does the user want a visible demo?
6. Keep the Pi pin `2bbfcca4` or move to `4c6fb7cfe`? (open since H3)
7. G10: accept zero or plain-table cost in H4 and defer tiers, echoed service tier and returned model to H9? (same as H3-G9)

Status: DONE_WITH_CONCERNS
Summary: 17 gaps found; 7 high (target undefined, key leak across a switch, OpenAI SDK ambient env, no switch API, thin Model record, unowned OpenAI rows, replay slots). Concerns: GitNexus tools were not available, no live probe was run, and the Pi catalog data and the Token Plan model id on the OpenAI route are unverified.
