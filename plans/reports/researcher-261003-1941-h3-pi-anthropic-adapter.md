# H3 research: how Pi builds the Anthropic adapter

Date: 2026-10-03. Pi commit `2bbfcca4`. Read-only, direct file reads. Paths are relative to `packages/ai/src` unless they start with `agent/` or `coding-agent/`. `AM` = `api/anthropic-messages.ts`.

## 1. Outcome

- The adapter is one function `stream` (AM:515-843) plus a thin `streamSimple` (AM:870-916). It never throws after it returns the stream; failures become an assistant message with `stopReason` `error` or `aborted`.
- Pi does not use the SDK stream parser. It calls `client.beta.messages.create(...).asResponse()` (AM:592) and parses SSE itself (AM:415-513). Betas go in the `betas` request field (AM:1086).
- The idle timeout lives in `coding-agent`, not in `packages/ai`. `clampThinkingLevel` is not called by the adapter.
- System prompts travel as `system` role messages inside `messages` (AM:1055-1058, `resolveTranscript` AM:521), not as a separate field.

## 2. Wiring

- `api/anthropic-messages.lazy.ts:4` + `api/lazy.ts:73-79`: the module loads on first call. `api/lazy.ts:46-61` turns any setup failure into an `error` event (empty content, zero usage, `api/lazy.ts:4-23`).
- `streamSimple` calls `assertRequestAuth` outside a try block (AM:875); a missing key throws, and only the lazy wrapper keeps the "never throw" contract. A Go port needs one place that guarantees it.
- Provider: `providers/anthropic.ts:43-59` (id `anthropic`, base URL `https://api.anthropic.com`).
- Model type `types.ts:1097-1142`. The adapter does not read `promptCache` or `samplingParams` (OpenAI-compatible only, types.ts:201-206).
- The 64-char session-id clamp in INV H-PROV-14 is not in the Anthropic adapter (it is in `openai-responses-shared.ts:156,162`, `bedrock-converse-stream.ts:910`, `google-shared.ts:197`).

## 3. Request build (`buildParams`, AM:1047-1217)

- Base: `model`, `messages`, `max_tokens`, `stream: true`, optional `betas` (AM:1078-1087).
- **System:** first system message -> one `system` text block, sanitized, with `cache_control` when caching is on (AM:1105-1114). Later system messages: kept as `role:"system"` if `supportsMidConvoSystemMessages`, else folded into the first (AM:521, 1263-1264). Native ones are held until just before the next assistant message (AM:1248-1257, 1322, 1409). OAuth identity block AM:1089-1104 (D9, out of scope).
- **Messages:** `transformMessages(..., normalizeToolCallId)` (AM:1057); system at index 0 dropped after (AM:1058). Id rule `replace(/[^a-zA-Z0-9_-]/g,"_").slice(0,64)` (AM:1220-1222).
  - User (AM:1283-1320): blank string skipped; blank text blocks dropped; message skipped when empty; images base64, `mimeType` cast without check.
  - Assistant (AM:1321-1389): blank text skipped; redacted -> `{type:"redacted_thinking", data: thinkingSignature}` (AM:1334-1339); non-blank signature -> `thinking`+`signature` verbatim (AM:1360-1366); empty or missing signature -> `text` block unless `compat.allowEmptySignature` (AM:1347-1359); empty thinking with no signature skipped (AM:1343); tool call -> `tool_use` with `input: arguments ?? {}` (AM:1367-1373); empty assistant skipped (AM:1376).
  - Tool results (AM:1390-1406): consecutive results merged into one `user` message; `{type:"tool_result", tool_use_id, content, is_error}` (AM:1224-1231); text-only -> joined string; image-only result gets a "(see attached image)" text first (AM:132-179).
- **Tools** (`convertTools`, AM:1504-1539): legacy `input_schema {type:"object", properties, required}`; full schema only when `strict === true` (AM:1517-1528). `eager_input_streaming: true` by default (AM:1533, 213); if `compat.supportsEagerToolInputStreaming` is false, omit it and add `fine-grained-tool-streaming-2025-05-14` when tools exist (AM:1462-1467, 1030). Strict support per tool via `resolveJsonSchemaStrictSampling`; tools with unsupported keywords stay non-strict (AM:1469-1502, 1514). Deferred placeholder `__pi_deferred_placeholder__` with `defer_loading` only when native tool changes are active (AM:199-204, 1063-1067, 1127-1149; only `anthropic` models get the flags, `scripts/generate-models.ts:1217-1225`). Otherwise `getCurrentTools(context.messages)` (AM:1150-1161).
- **tool_choice** (AM:1203-1209): string -> `{type}`; object passed. `streamSimple` allows only `auto|none` (types.ts:84, 352-354).
- **Temperature** (AM:1116-1124): sent only if set, thinking not enabled, not managed-effort, and `compat.supportsTemperature` (false for opus 4.7, 4.8, 5, sonnet 5.5, generate-models.ts:635-647). Never `top_p`, `top_k`, stop sequences.
- **max_tokens:** `options.maxTokens ?? model.maxTokens` (AM:1084). `buildBaseOptions` clamps `min(maxTokens, max(1, contextWindow - estimateContextTokens - 4096))`, skipped when `contextWindow <= 0` (`api/simple-options.ts:12-19,30`); the input is an estimate. Budget path (AM:899-915): `adjustMaxTokensForThinking` = `min(base + budget, model.maxTokens)` (simple-options.ts:76-92), clamp again (AM:908), `budget = min(budget, max(0, maxTokens - 1024))` (AM:914). Hole: AM:1187 `budget_tokens || 1024` turns a 0 budget into 1024, which can be >= `max_tokens` (inference, untested).
- **Metadata:** only string `metadata.user_id` (AM:1196-1201).
- **Headers** (`createClient`, AM:975-1000), later wins: User-Agent `getPiUserAgent()`, `accept`, `anthropic-dangerous-direct-browser-access`, session affinity, `model.headers`, `options.headers`. `null` values stay in the object and the SDK drops them (types.ts:122,164; test `anthropic-auth-token.test.ts:228`). Session affinity only when `compat.sendSessionAffinityHeaders` (default true only for OpenRouter, AM:215-216, 977-981). `assertRequestAuth` needs apiKey or `authorization`/`x-api-key`/`cf-aig-authorization` header, else `No API key for provider: <id>` (AM:311-321). `baseURL` from `model.baseUrl` (AM:994). `options.fetch` replaces HTTP (AM:996).
- **Betas** (`getBetaFeatures`, AM:1003-1045): a caller `anthropic-beta` header replaces the list (`null` -> empty). Else: OAuth betas (1029); fine-grained tool streaming (1030); `interleaved-thinking-2025-05-14` when reasoning, thinking enabled, not `forceAdaptiveThinking` (1031-1038); `server-side-fallback-2026-07-01` (1039); mid-conversation effort betas (1040-1042); mid-conversation tool changes (1043). There is no 1h-cache beta.

## 4. Thinking (`streamSimple`, AM:870-916)

| Case | Sent | Line |
|---|---|---|
| Not `reasoning` | no `thinking` | AM:1172 |
| Level not set | `thinking:{type:"disabled"}` only if `thinkingLevelMap.off !== null`; omitted when `off` is null (Fable 5, managed effort) | AM:881-886, 1191-1193 |
| `forceAdaptiveThinking` | `thinking:{type:"adaptive", display}` + `output_config:{effort}` | AM:890-897, 1177-1182 |
| Other reasoning | `thinking:{type:"enabled", budget_tokens, display}` | AM:1185-1189 |
| Managed effort | adaptive, `block_binding`, `output_config:{effort:"high"}`, system messages with `output_config` | AM:1165-1171, 1446-1460 |

- Adaptive models are an id-substring match (`generate-models.ts:616-634`; test `anthropic-adaptive-thinking-models.test.ts:5-26`).
- Level -> effort: map value if string, else minimal/low -> `low`, medium -> `medium`, else `high` (AM:850-868).
- Budgets (simple-options.ts:54-69): minimal 1024, low 2048, medium 8192, high 16384; xhigh/max clamp to high; `thinkingBudgets` overrides.
- Clamp `clampThinkingLevel` (models.ts:1217-1247): non-reasoning -> `["off"]`; else ordered list minus null map values; nearest higher, then lower. Not called by the adapter.
- `thinkingDisplay` `summarized` (default) or `omitted` (AM:266, 1176); `streamSimple` cannot set it.

## 5. Prompt caching

- `resolveCacheRetention` (AM:64-72): option, else `PI_CACHE_RETENTION=long`, else `short`; per-request `env` wins over process env (types.ts:120).
- `none`: no `cache_control` (AM:80-82). `short`: `{type:"ephemeral"}`. `long`: `ttl:"1h"` if `compat.supportsLongCacheRetention` (default true) (AM:83-86).
- Breakpoints: every system text block (AM:1095,1102,1111); last tool if `compat.supportsCacheControlOnTools` (AM:1126,1536); last block of the last message when it is user or system (types `text, image, tool_result, tool_addition, tool_removal`; string wrapped) (AM:1411-1437). No marker when the last message is assistant.
- Session id is not a cache key for Anthropic.
- Cost: `cacheWrite1h` from `usage.cache_creation.ephemeral_1h_input_tokens` (AM:626, 783-789); `(cacheWrite*short + input*2*long)/1e6` (models.ts:1204-1211); tier threshold `input + cacheRead + cacheWrite` (models.ts:1194-1202).

## 6. Stream parsing

- SSE decoder AM:415-472 (abort checked before each read).
- `iterateAnthropicEvents` (AM:474-513): SSE `error` event -> throw raw data (no `overloaded_error` mapping) (486-488); only six event types handled, `ping` and others skipped (335-342, 490); JSON via `parseJsonWithRepair`, failure -> `Could not parse Anthropic SSE event ...` (495-506); `message_start` without `message_stop` -> `Anthropic stream ended before message_stop` (510-512).
- `message_start` (607-630): `responseId`, `input_transformations`, `responseModel` (only if different), fallback pricing, usage.
- `content_block_start` (631-678): text, thinking (`signature ?? ""`), redacted (`thinking:"[Reasoning redacted]"`, signature = data, `redacted:true`), tool_use (empty `partialJson`), `fallback` (error after output began). Others ignored. Blocks matched by `index` (602-603).
- `content_block_delta` (679-724): `text_delta`, `thinking_delta`, `input_json_delta` (append + `parseStreamingJson`, `utils/json-parse.ts:104-124`, falls back to `{}`), `signature_delta`. Others ignored. No server-tool handlers.
- `content_block_stop` (725-756): final JSON parse, removes scratch fields, emits `*_end`.
- `message_delta` (757-800): stop reason, `rawStopReason` (761), usage fields only when non-null (768-795), `usage.reasoning` from `output_tokens_details.thinking_tokens` (790-794), cost recalculated.
- Stop reason map (AM:1541-1566): `end_turn`/`pause_turn`/`stop_sequence` -> `stop`; `max_tokens` -> `length`; `tool_use` -> `toolUse`; `refusal` -> `error` (text from `stop_details.explanation`); `sensitive` -> `error`; anything else (incl. `model_context_window_exceeded`) throws `Unhandled stop reason`.
- No stop reason at end -> `Anthropic stream ended without a stop reason` (AM:807-809). Output starts with `stopReason:"pending"` (AM:541).
- `totalTokens` = input+output+cacheRead+cacheWrite (AM:622-630).

## 7. Errors, aborts, retries

- Catch block AM:829-839: removes scratch fields, `aborted` if signal aborted else `error`, `errorMessage` = message (or JSON), pushes `error` event with partial message. Abort also checked after the loop (AM:803-805). After the loop, `error`/`aborted` stop reasons throw so refusal goes through the catch (AM:810-812).
- SDK `maxRetries: 0` (AM:589). Own wrapper `retryProviderRequest` (`utils/provider-retry.ts:105-125`) because the SDK sleep ignores abort (97-104). Default `options.maxRetries ?? 0` (109). Rules: `x-should-retry`, no status, 408/409/429/5xx (23-35). Delay: `retry-after-ms`, `retry-after`, else `min(0.5*2^n, 8)` s minus 0-25% jitter; server delay above `maxRetryDelayMs` (60 s) throws (37-67). Only up to response headers. Agent-level retry is separate.

## 8. Timeouts

- `timeoutMs` -> SDK per-request `timeout` (AM:588).
- Idle timeout: `coding-agent/src/core/http-dispatcher.ts:81-100` sets undici `headersTimeout` and `bodyTimeout` = `httpIdleTimeoutMs` (default 300000, 0 disables) on the global dispatcher (`main.ts:866`).
- `coding-agent/src/core/sdk.ts:321-326`: request `timeoutMs = options.timeoutMs ?? retry.provider.timeoutMs ?? httpIdleTimeoutMs`; 0 -> 2147483647.

## 9. Hooks

- `onPayload(params, model)` after `buildParams`; a return value replaces params and `stream:true` is forced again (AM:581-585; test `anthropic-sse-parsing.test.ts:246`).
- `onResponse({status, headers})` before `start` (AM:599-600).
- `onProviderStreamEvent(event, model)` for each parsed event (AM:606).
- A throwing hook becomes an error message.

## 10. Auth

- `agent/src/agent-loop.ts:392-402`: `getApiKey(provider)` awaited for every model call, fallback `config.apiKey`.
- `env-api-keys.ts:29-31`: `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`. `getEnvApiKey` skips `ANTHROPIC_AUTH_TOKEN` (148-153), which goes as `Authorization: Bearer` (`providers/anthropic.ts:18-39`).
- Minimum for Ask H-AUTH-05: `ANTHROPIC_API_KEY` as `x-api-key`.

## 11. In the adapter but not in the H3 roadmap list

OAuth and Copilot branches (D9); own SSE decoder; injected client; session affinity; strict tool schemas; fine-grained beta fallback; mid-conversation system messages and native tool changes; managed effort and thinking binding; server-side fallback models; `input_transformations` diagnostics; `rawStopReason`, `usage.reasoning`, 1h-write price; `allowEmptySignature`; `supportsTemperature` and `thinkingDisplay`; cost tiers; User-Agent; `sanitizeSurrogates`; beta API surface; `ANTHROPIC_AUTH_TOKEN` Bearer and `cf-aig-authorization`.

## 12. Pi tests (`packages/ai/test/`, from titles)

`anthropic-sse-parsing.test.ts` (event order, relabel, fallback cost and error, onPayload, no interleaved beta when thinking off, malformed SSE and tool JSON repair, refusal details, `sensitive`, delta without usage, events after `message_stop` ignored); `anthropic-thinking-disable.test.ts`; `anthropic-force-adaptive-thinking.test.ts`; `anthropic-adaptive-thinking-models.test.ts`; `anthropic-temperature-compat.test.ts`; `anthropic-cache-write-1h-cost.test.ts`; `anthropic-long-cache-retention-e2e.test.ts` (live); `anthropic-eager-tool-input-compat(.e2e).test.ts`; `anthropic-strict-tool-schema.test.ts`; `anthropic-empty-thinking-signature-compat.test.ts`; `anthropic-mid-conversation-effort.test.ts`, `anthropic-thinking-binding-e2e.test.ts`; `anthropic-auth-token.test.ts`; OAuth tests (D9); `anthropic-opus-4-8-smoke.test.ts` (live); `transform-messages-copilot-openai-to-anthropic.test.ts`.

## 13. Gaps (ranked)

1. No test for a stream with no stop reason or no `message_stop` (AM:510-512, 807-809).
2. Stop-reason mapping not listed; `model_context_window_exceeded` is unhandled in Pi.
3. `pause_turn` -> `stop` with no resubmit.
4. Idle timeout is outside the adapter in Pi; Ask must wrap the fantasy stream itself.
5. Retry default 0 with an abort-aware wrapper; fantasy behavior to check.
6. Tool-result merging, blank-content filtering, image-only placeholder not named.
7. Unsigned thinking -> text (aborted streams produce such blocks).
8. Temperature rule with thinking and new models not named.
9. Budget hole `budget_tokens || 1024`.
10. SSE `error` events surface raw JSON; no typed overloaded error.
11. `maxTokens` clamp uses an estimate.
12. Missing-key check must become an error message, not a panic or plain error.
13. INV H-PROV-14 wording: session id is not an Anthropic cache key.
14. Server-tool blocks ignored.
15. The hard-coded model record must carry `reasoning`, `thinkingLevelMap` (with `off: null`), adaptive flag, `supportsTemperature`.
16. Unverified: SDK error text, SDK null-header drop, generate-models.ts:631-633, `clampThinkingLevel` call site.

Status: DONE_WITH_CONCERNS (SDK details and undici semantics unverified).
