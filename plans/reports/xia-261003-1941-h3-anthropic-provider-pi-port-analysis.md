# H3 port analysis: the first real provider (Anthropic), Pi to Ask

Date: 2026-10-03. Mode: `xia --port`, stopped after analysis (user request: write the report and stop; no plan, no implementation, no roadmap edits).

## 0. Source manifest

| Item | Value |
|---|---|
| Source | Pi monorepo, `/Users/dale/Desktop/workspace/opensources/pi`. All citations are at the roadmap reference commit `2bbfcca43` (GitNexus repo `pi` is indexed at the same commit). |
| Source drift | The Pi checkout was fast-forwarded to `4c6fb7cfe` (v1.0.1) at 2026-10-03 19:48 +0700, during this analysis. Two later commits touch the Anthropic adapter: `a9424cd43` (workload identity federation, OIDC, 2026-09-30) and `b271b0a52` (mid-conversation tools inline, beta `inline-tools-2026-09-15`, 2026-10-02). Both are outside H3 scope (D9 API keys only; native tool changes are not in H3). `transform-messages.ts` did not change. |
| License | MIT. A port reads behavior; no code is copied. |
| Wire layer (D22) | `charm.land/fantasy` v0.45.2, which pins `anthropic-sdk-go` v1.68.0 (mod cache `/Users/dale/Desktop/workspace/go/mobules/pkg/mod/`). Not in Ask `go.mod` yet. |
| Local | Ask `master-2` at `b0b1f5e` (H1 done: `pkg/protocol`, `internal/providers` with assembler, faux, sse, partialjson). H2 is planned (`plans/261002-1419-h2-agent-loop-print-json/plan.md`), not built. |
| Plan read | `roadmap.md` section H3 (and D6, D9, D19, D22, H4, H7, H9, H17), plus the inventory rows H3 owns. |

Detail reports (every claim there carries `file:line`):
- `researcher-261003-1941-h3-pi-anthropic-adapter.md`: request build, thinking, caching, SSE parsing, errors, retry, timeouts, hooks, auth.
- `researcher-261003-1941-h3-pi-transform-messages.md`: `transformMessages` branch by branch, sanitizer, `convertToLlm`, local type support.
- `researcher-261003-1941-h3-fantasy-anthropic-fit.md`: what fantasy can and cannot express, the `StreamPart` to `Assembler` map.
- `researcher-261003-1941-h3-gap-hunt.md`: the end-to-end Pi path for `-p`, and what H3 misses or places in a later phase.

Path shorthand: `AM` = Pi `packages/ai/src/api/anthropic-messages.ts`, `TM` = `.../api/transform-messages.ts`, `C:` = `packages/coding-agent/src/`, `FA` = fantasy `providers/anthropic/anthropic.go`.

## 1. What H3 must deliver (from the roadmap)

- **Concept:** one internal message model, one adapter for each wire API.
- **Wire layer:** one Ask adapter over fantasy (Ask messages to `fantasy.Call`, `fantasy.StreamPart` to the H1 `Assembler`); fantasy types never leave the adapter package. H4 reuses it for OpenAI.
- **Owns:** H-PROV-01, 02, 07, 12, 14, 18, 25, 26; H-AUTH-05, 08; H-PROV-10 (function part), H-PROV-27, H-TOOL-21 (replay part).
- **Tests:** signature round-trip; idle timeout vs long active stream; `transformMessages` table tests; multi-turn with an errored message and an unfinished call accepted by the API; full pairing after abort (D19).
- **Exit:** `-p` works against a real Anthropic model.

## 2. How Pi implements it (summary)

1. `main.ts` resolves model and thinking level. With no `--model`, the first provider with auth wins; the Anthropic default is `claude-opus-4-8` (`C:core/model-resolver.ts:22,693-700`). The level defaults to `medium` and is clamped by `clampThinkingLevel` in `C:core/sdk.ts:261`, not in the adapter (GitNexus: callers of `clampThinkingLevel` are `sdk.ts`, `model-runtime.ts:1024`, `agent-session.ts:2637` and other adapters' `streamSimple`; the Anthropic adapter is not one of them).
2. The loop calls `getApiKey(provider)` for every model call (`A:agent-loop.ts:392-402`).
3. `streamSimple` (`AM:870-916`) checks auth, builds options (`maxTokens` clamp to `contextWindow - estimate - 4096`), and picks adaptive (effort) or budget thinking.
4. `stream` (`AM:515-843`) builds params (`AM:1047-1217`): `transformMessages` (`AM:1057`), `convertMessages` (`AM:1238-1425`), `convertTools` (`AM:1504-1539`), betas, cache markers. It sends through its own retry wrapper with SDK `maxRetries: 0` (`AM:589`), parses SSE itself, fills the message, and calls `calculateCost` inside the adapter.
5. Every failure after the stream returns becomes an assistant message with `stopReason` `error` or `aborted` (`AM:829-839`).
6. The idle timeout is a global undici dispatcher setting in `C:core/http-dispatcher.ts:81-100`, not in the adapter.

## 3. H3 row coverage

| Row | Pi evidence | Fantasy | Gap |
|---|---|---|---|
| H-PROV-01 layers, failure as data | `api/lazy.ts:46-79`; `AM:829-839` | Errors arrive as an `error` part (`FA:1467-1469`) | Missing-key check must become an error message, not a Go error out of `Stream` (Pi relies on the lazy wrapper, `AM:875`). |
| H-PROV-02 `anthropic-messages` | `AM` whole file | Thinking, signatures, redacted supported; no strict/eager/defer; `required` dropped if `[]any` | G4, G6, G13 |
| H-PROV-07 request options | `types.ts:132-240`; `simple-options.ts:12-19` | `max_tokens` nil -> 4096 (`FA:404`); no stop sequences/metadata in `Call`; no payload/response hooks | G8, G9 |
| H-PROV-12 thinking levels | `models.ts:1217-1247`; `AM:850-868` | `Effort` and budget options; no `disabled`; display default only for newer families (`FA:73-90`) | G5 |
| H-PROV-14 caching | `AM:64-111,1411-1437,1536` | Per-part markers; no 1h TTL (`PO:204-207`) | G10 |
| H-PROV-18 Model struct | `types.ts:1097-1142` | n/a | G1 (record undefined); H1 `Model` lacks cost, level map, compat (`internal/providers/model.go:5-17`) |
| H-PROV-25 idle timeout | `C:core/http-dispatcher.ts:81-100` | Custom `*http.Client` allowed; SDK skips `ping` (`ssestream.go:208-209`) | G7 |
| H-PROV-26 quirk data | `AM:211-224` (`getAnthropicCompat`); `generate-models.ts:616-647` | Fantasy has its own model-name heuristics (`FA:53-112`) | Ask compat data must decide, not fantasy |
| H-AUTH-05 env key | `env-api-keys.ts:29-31,148-153`; `providers/anthropic.ts:18-39` | SDK ambient chain reads `ANTHROPIC_AUTH_TOKEN`, profile, federation when key is empty (`SDK client.go:35-80`) | G2 |
| H-AUTH-08 per-request auth | `A:agent-loop.ts:392-395` | Key baked into a client per `LanguageModel()` (`FA:275-348`) | Build one client per request |
| H-PROV-10 fn, H-PROV-27, H-TOOL-21 replay | `TM:12-235`; adapter `AM:1238-1425` | Fantasy merges same-role messages, keeps empty text, drops thinking-only and unsigned thinking (`FA:540-586,948-950,1126-1132,1187-1193`) | G3, G6 |

## 4. Gaps in the plan

Severity: could stop the exit or give a silently wrong real result.

### G1. The H3 model record and the `-p` default are not defined (high, user decision)

`roadmap.md` H3 says "one hard-coded model record" but names no model and no data source. Pi's catalog is generated and git-ignored (`AI:providers/anthropic.models.ts:5`, `.gitignore:11`), so the numbers are not in the checkout. The record needs: id, context window, max tokens, price (input, output, cache read, cache write), `reasoning`, `thinkingLevelMap` (with `off: null` where it applies), the adaptive flag, and compat flags (`supportsTemperature`, `supportsLongCacheRetention`, `allowEmptySignature`, `supportsEagerToolInputStreaming`). This choice decides G5, G8 and the price table. Also open: is `anthropic` the default provider when a key resolves, while `faux` stays explicit (`H2 plan:451`)?

### G2. Credential minimum and the SDK ambient chain (high)

H-AUTH-05 is in H3 and H-AUTH-01 (precedence) is in H7; the minimum between them is not written. If the adapter calls fantasy with an empty key, the Anthropic Go SDK uses `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, a profile file and OIDC federation on its own (`SDK client.go:35-80,209-216`; fantasy adds `WithAPIKey` only when non-empty, `FA:279-281`). This bypasses D9. Recommendation: key from `--api-key`, then `ANTHROPIC_API_KEY`; fail fast with an error message naming both and `~/.ask/auth.json`; never call fantasy with an empty key; test that no HTTP request leaves when only `ANTHROPIC_AUTH_TOKEN` is set.

### G3. `transformMessages` test list is too narrow (medium-high)

The roadmap lists five cases. Pi's function also has: redacted thinking (kept same-model, dropped cross-model, `TM:104-106`); empty/whitespace thinking dropped (`TM:111`); signed thinking with empty text kept (`TM:109`); "same model" = provider + api + model id, `responseModel` ignored (`TM:95-98`); system messages held while calls are pending (`TM:163-166,216-221`); a user message closes pending calls (`TM:222-225`); tool-result re-keying after id normalization (`TM:84-90,136-142`); image placeholder de-duplication and the tool-result variant `(tool image omitted: ...)` (`TM:12-57`); null content -> `[]` (`TM:73`); `textSignature` and `thoughtSignature` dropped cross-model (`TM:121-134`); orphan tool results passed through, not dropped (`TM:213-215`). Pi itself has unit tests for only four of these (`transform-messages-copilot-openai-to-anthropic.test.ts`, `lax-message-content.test.ts`, `anthropic-sse-parsing.test.ts:~136-170`). The function needs an injected clock (synthetic timestamp) and the `(id, model, sourceAssistant)` normalize callback; neither exists locally. All other fields it reads exist in H1 (`pkg/protocol/message.go:165-192`, `content.go:28-56`).

### G4. Error data that H9 needs must be kept by H3 (high)

H9 owns retry and overflow (H-RETRY-01..06), but it classifies by message text (`AI:utils/retry.ts:34-100`). H1 has only `ErrorMessage` and `RawStopReason` (`pkg/protocol/message.go:174-176`). Fantasy's incomplete-stream text is `stream transport error: unexpected EOF` (`F:errors.go:125-131`), which Pi's `ended without` rule does not match, and a mid-stream SSE error may carry status 200. Recommendation: the adapter builds `ErrorMessage` from status, status text and body; wraps the typed `*fantasy.ProviderError` with `%w`; maps `io.ErrUnexpectedEOF` to the H1 `ErrStreamIncomplete`; checks `ctx.Err()` first for `aborted` (`AM:835`). Test with fake 429, 529 `overloaded_error`, 413, 401 and a mid-stream error event. (The `ProviderError.RequestBody` key-leak concern is dropped by the user, 2026-10-03.) With the Token Plan target (section 10), error bodies are not Anthropic-shaped, so text and status must carry the classification.

### G5. Thinking path (medium, tied to G1)

- Adaptive models use `effort`; other models use `budget_tokens` (`AM:890-915`). Budgets (H-PROV-13) are deferred to H17 (`roadmap.md:569`). This is safe only if the H3 record is adaptive. If H3 accepts a budget model, pull in the four defaults (1024, 2048, 8192, 16384) and the answer-room clamp (`simple-options.ts:44-92`).
- Pi has a hole: `budget_tokens || 1024` (`AM:1187`) can give a budget >= `max_tokens`; fantasy rejects a zero budget (`FA:431-434`). The Ask adapter must enforce `max_tokens > budget_tokens`.
- Display: Pi sends `display: "summarized"` always (`AM:1174-1176`); fantasy sets it only for newer families (`FA:73-90`). Without it, Opus 4.7/4.8 return empty thinking text. Set it explicitly.
- `off`: Pi sends `thinking:{type:"disabled"}` unless the map says `off: null` (`AM:1191-1193`); fantasy never sends it (needs `ExtraBody`). Live check needed on the chosen model.
- Interleaved beta: Pi sends `interleaved-thinking-2025-05-14` for non-adaptive reasoning models (`AM:1031-1038`); fantasy needs `Call.Headers`.
- Temperature: Pi omits it with thinking and for models with `supportsTemperature=false` (`AM:1116-1124`); fantasy removes it only in the budget branch (`FA:447-470`).
- The thinking clamp lives above the adapter in Pi (`C:core/sdk.ts:261`). Ask needs it in the H2/H3 CLI path (`--thinking` flag, default `medium`).

### G6. Wire conversion rules live in the adapter, not in `transformMessages` (medium-high)

Pi's `convertMessages` filters after the transform: blank user/assistant text skipped (`AM:1286-1310,1325-1327`), unsigned thinking -> text (`AM:1347-1360`), empty assistant skipped (`AM:1378`), consecutive tool results merged into one user message (`AM:1393-1405`), image-only tool result gets a text first (`AM:132-179`), system messages held until the next assistant (`AM:1246-1252`). Fantasy differs: it keeps empty text blocks (API rejects them), drops unsigned and thinking-only content with only a warning, and takes one image plus text per tool result, image first (`FA:1028-1045`). The H3 tests list has no wire-JSON test. Recommendation: golden wire-JSON tests on an `httptest` server for each rule.

### G7. Abort, idle timeout and leaks over real HTTP (medium)

Fantasy never calls `Close` on the SDK stream; the SDK drops `ping` before fantasy, so `keepalive` parts do not prove liveness. Recommendation: a derived `context.WithCancelCause` per request (tells idle timeout from user abort), a body-level idle reader (default 300000 ms, 0 disables), never `http.Client.Timeout`, a transport that keeps `HTTP(S)_PROXY`, and goleak tests on a real HTTP fake server. The H2 leak test runs on faux only.

### G8. `maxTokens` clamp needs an estimator owned by H9 (medium, decision)

Pi clamps with `estimateContextTokens` (`AI:utils/estimate.ts:96-110`, about 40 lines). H-RETRY-09 is H9. Fantasy sends 4096 when unset (`FA:404`). Options: pull a minimal estimator into H3, or send `model.maxTokens` and record the deviation. Either way always set `max_tokens`.

### G9. Usage, cost and stop reason through fantasy (medium-high, partly decision)

Pi computes cost in the adapter (GitNexus: `calculateCost` is called by `stream` in `AM`). `totalTokens` = input + output + cache read + cache write (`AM:622-630`). Fantasy gives usage only on `finish`, `TotalTokens` without cache (`FA:1732`), no 1h split, no response model, no raw stop reason, no `stop_details`, nothing on abort (`FA:1302-1318,1704-1737`). JSON mode shows `usage` and `cost` on `message_end`; a real run with cost 0 looks free. Recommendation: pull Pi's `totalTokens` formula and the plain price table into H3; tiers and 1h price stay in H9. Decision: build an SSE tap in a custom `RoundTripper` (reuse the H1 SSE reader) for raw stop reason, response model, 1h split, reasoning tokens and usage on abort; or accept the loss and record it.

### G10. Caching: 1h TTL unreachable through fantasy (medium, decision)

`roadmap.md` H3 owns H-PROV-14 with `none/short/long`. Fantasy's `CacheControl` has only `Type` (`PO:204-207`); `ttl:"1h"` needs an `ExtraBody` sjson path or a body patch, both untested upstream. Options: defer `long` to H9/H17, or patch. Breakpoints (system blocks, last tool, last block of the last user message) must be placed by the adapter. The env name for `PI_CACHE_RETENTION` is open (suggest `ASK_CACHE_RETENTION`, D7).

### G11. Tool schema and names (high, silent)

Fantasy reads `required` only as `[]string` (`FA:728-731`, verified); `ToolDecl.Parameters` decodes to `[]any`, so every argument becomes optional with no error. `additionalProperties` and `$defs` are dropped too. Recommendation: convert in the adapter, or send tools through `ExtraBody["tools"]`; golden test. Validate tool names against `^[a-zA-Z0-9_-]{1,64}$` (MCP and extension names in H16/X1 can break it).

### G12. Stop reasons (medium)

Pi: `end_turn`, `pause_turn`, `stop_sequence` -> `stop`; `max_tokens` -> `length`; `tool_use` -> `toolUse`; `refusal`, `sensitive` -> `error`; unknown, including `model_context_window_exceeded`, -> error `Unhandled stop reason` (`AM:1541-1566`). Fantasy maps `model_context_window_exceeded` to `length` and unknown to `unknown` (`FA:1302-1318`, verified). Decide the Ask mapping on purpose; a context-exceeded stop is input for H9 overflow handling, not a plain length stop. `pause_turn` is not a gap: it occurs only with server tools, which Ask does not send.

### G13. Eager tool-input streaming and strict tools (low-medium, decision)

Pi sends `eager_input_streaming: true` on every tool (`AM:1533`) and strict schemas when supported (`AM:1469-1502`). Fantasy cannot set either. Options: `ExtraBody["tools"]` override, or accept the loss in H3 (tool arguments then arrive in larger chunks; no correctness loss).

### G14. Test strategy is not written (medium)

Pi gates live tests on env keys and builds SSE by hand; it has no recorded cassettes. Recommendation: `httptest` SSE fixtures offline, plus one env-gated live file (`ASK_LIVE_ANTHROPIC=1` or similar). Do not record cassettes unless the key header is stripped.

### G15. Smaller items (no phase change)

- Surrogates: Go cannot hold a lone surrogate in a string; `encoding/json` replaces invalid UTF-8 and lone surrogate escapes with U+FFFD, so Ask never emits an unpaired surrogate on the wire. No sanitizer is needed.
- Fantasy merges same-role messages (`FA:540-586`) and drops later system messages (`FA:904-909`). Ask sends only a leading system message (`internal/providers/convert.go:30-41`), so this is safe in H3. A `SystemMessage.ToolsAdded` must go to `Call.Tools`.
- Local `ConvertToLLM` drops custom messages; Pi maps them to user text (`C:core/messages.ts:148-196`). Owner phase (H8 or H11) must be named; not an H3 blocker.
- Image size cap (5 MB at Anthropic) vs H-TOOL-20 resize in H17: H5 should refuse large images until then.

## 5. Proposed text corrections (not applied)

These are wrong today. They are listed for the user and not edited, as with the H2 report.

1. INV H-PROV-10 and H-PROV-21: foreign thinking becomes plain text with no `<thinking>` tags (`TM:113-116`; `AI:CHANGELOG.md:1913,2055`). Surrogate stripping is not in `transformMessages`. The "empty error messages filtered" rule is now a `stopReason` filter.
2. INV H-TOOL-21: synthesis is at `TM:158-235`, not `:64-140`.
3. INV H-PROV-27: the "no empty text part" rule lives in the adapter (`AM:1286-1310`), not in `transformMessages`.
4. INV H-PROV-14: the session id is not an Anthropic cache key; the 64-char clamp is in other adapters.
5. Roadmap D22 note "fantasy's Anthropic provider leaves the SDK's default retries on, so the adapter must turn them off" is stale: fantasy v0.45.2 sets `option.WithMaxRetries(0)` (`FA:276-277`, verified). The decision stands; only the note changes. Keep a lock-in test. The same claim is in the H2 plan (`plan.md:453`).
6. Roadmap reference: Pi is now at v1.0.1 (`4c6fb7cfe`); the roadmap pins `2bbfcca4`. Decide whether to keep the pin.

## 6. Dependency matrix (Pi component to Ask)

| Pi component | Ask target | Status |
|---|---|---|
| `Model` record (`types.ts:1097-1142`) | `internal/providers/model.go` | CONFLICT (fields missing: cost, level map, compat) |
| `transformMessages` (`TM`) | `internal/providers` (pure function) | NEW |
| `normalizeToolCallId` (`AM:1220-1222`) | adapter callback | NEW |
| `convertMessages`, `convertTools` (`AM:1238-1539`) | fantasy adapter (`Call` mapping) | NEW |
| SSE parsing, block assembly (`AM:415-800`) | fantasy + H1 `Assembler` | EXISTS (assembler), NEW (part mapping) |
| Stop-reason map (`AM:1541-1566`) | adapter | NEW (decision G12) |
| `calculateCost` call in adapter | adapter + `pkg/protocol/usage.go` | EXISTS (type), NEW (formula, prices) |
| Retry wrapper (`provider-retry.ts`) | H9 | deferred; SDK retries already 0 in fantasy |
| Idle timeout (`http-dispatcher.ts`) | adapter `http.Client` / body reader | NEW |
| `getApiKey` per call | H2 hook `GetAPIKey` | EXISTS in H2 plan; NEW env lookup |
| `clampThinkingLevel` (`models.ts:1228-1247`) | CLI / agent path | NEW |
| `onPayload`, `onResponse`, `onProviderStreamEvent` | `RoundTripper` (P1) | NEW, can wait (wired to extension events in `C:core/sdk.ts:358-385`, H11/X1) |
| `charm.land/fantasy` | `go.mod` | NEW (first importing PR) |

## 7. Decision matrix

| Decision | Pi's way | Ask options | Recommendation |
|---|---|---|---|
| Model record (G1) | Generated catalog, default `claude-opus-4-8` | One adaptive model; or adaptive + one budget model | **User, 2026-10-03: `deepseek-v4.1-flash` on the Alibaba Token Plan Anthropic endpoint** (section 10) |
| Credential minimum (G2) | `--api-key` > stored > env chain | env + flag only; or full chain | `--api-key`, then `ANTHROPIC_API_KEY`; fail fast; never an empty key into fantasy |
| Missing fantasy data (G9) | Adapter reads raw SSE | SSE tap; accept loss; patch fantasy | Accept loss in H3 except `totalTokens` and base cost; record it; revisit in H9 |
| Cache `long` (G10) | `ttl:"1h"` | Defer; `ExtraBody` patch | Defer `long`, ship `none`/`short` |
| `maxTokens` clamp (G8) | Estimate-based clamp | Minimal estimator; `model.maxTokens` | Minimal estimator (chars/4) in H3, so H9 reuses it |
| Stop reasons (G12) | Unknown -> error | Follow Pi; fantasy map | Follow Pi; map `model_context_window_exceeded` to an error that H9 can detect |
| Eager/strict tools (G13) | On by default | Override tools; accept loss | Accept loss in H3 |
| Error text (G4) | SDK message text | Full text + typed `%w` | Full text + typed `%w` |

## 8. Risk score

**Medium-high (7/10).**

- The H3 adapter is mostly mapping, but fantasy silently changes the wire in at least six places (G6, G11).
- G1 is answered (section 10). The chosen endpoint is lenient, so live acceptance proves less than it would against Anthropic.
- Sequencing: H2 is not built, so the exit (`-p` against a real model) cannot run until H2-C/D land. The adapter and `transformMessages` can be built and tested earlier against the H1 `Assembler` on an `httptest` SSE server.

## 9. GitNexus check (as asked)

- **`context` on `transformMessages`:** callers are `buildParams` (Anthropic), `convertResponsesMessages`, Google `convertMessages`, Mistral `stream`, Bedrock `convertMessages`, OpenAI Completions `convertMessages`, plus three test files. This confirms the H3/H4 split: one shared function, per-vendor callbacks.
- **`context` on `clampThinkingLevel`:** called by every other adapter's `streamSimple`, by `ModelRuntime.resolveModel`, by `sdk.ts` and by the experimental runtimes, but not by the Anthropic adapter. Ask must clamp above the adapter.
- **Cypher, callees of `stream`/`buildParams`/`streamSimple`/`convertMessages`/`createClient` in `AM`:** the list matches the adapter lane, with four names no lane covered in depth:
  - `getAnthropicFederation` (OIDC workload identity, from env, when no key is set): out of scope (D9), but the Go SDK has the same chain, which is why G2 matters.
  - `insertThinkingLevelMessages` (managed-effort `system` messages with `output_config`): out of H3 scope.
  - `appendAssistantMessageDiagnostic` (`anthropic_input_transformations` diagnostics): optional; maps to `Metadata.Diagnostics`.
  - `transformProviderPayload`, `handleProviderResponse`, `handleProviderStreamEvent` in `C:core/sdk.ts:358-385`: the three hooks become extension events `before_provider_request`, `after_provider_response`, `provider_stream_event`. That is the real consumer of H-PROV-07's hooks, so they can wait for H11/X1.
- **Cypher, callers of `calculateCost`, `retryProviderRequest`, `configureHttpDispatcher`:** cost is computed inside each adapter's stream (supports G9); retry wraps only the request; the idle timeout is set in `main`, `setupCli`, `rpc-entry` and interactive mode, never in `packages/ai` (supports G7).
- **Coverage:** the four lanes read files directly. GitNexus was used by the lead for the call graph above.

## 10. User decision on G1 and live probe (2026-10-03)

**Decision (user, 2026-10-03):** H3 uses `deepseek-v4.1-flash` from the Alibaba Cloud Model Studio Token Plan. The wire API stays `anthropic-messages` (the Token Plan exposes an Anthropic-compatible endpoint), so D22, the fantasy adapter and the H3 row list do not change. Only the provider record, the endpoint and the quirk data change. Interpretation to confirm: this replaces "a real Anthropic model" as the H3 exit target. Whether a real Anthropic model stays as a second target is open.

**Sources:** Alibaba docs [Anthropic API compatibility](https://www.alibabacloud.com/help/zh/model-studio/anthropic-api-messages), [Claude Code with Token Plan](https://www.alibabacloud.com/help/en/model-studio/claude-code), [Token Plan team overview](https://help.aliyun.com/zh/model-studio/token-plan-team-overview); Pi community package [pi-alibaba-models](https://pi.dev/packages/pi-alibaba-models).

**Live probe.** Twenty curl calls with the key in `ALIBABA_TOKEN_PLAN_API_KEY` (value never printed). Raw outputs are in the session scratchpad, not in the repo.

| # | Probe | Result |
|---|---|---|
| 1 | Endpoint | `https://token-plan.ap-southeast-1.maas.aliyuncs.com/apps/anthropic/v1/messages` works with `x-api-key`. The `cn-beijing` host returns 401 `InvalidApiKey` for this key. The docs say `Authorization: Bearer` also works (Claude Code uses `ANTHROPIC_AUTH_TOKEN`). |
| 2 | Latency | Time to first byte 4.6 to 16 s for small requests. No hang in any call (the pi-alibaba-models claim that DeepSeek "often hangs" on this path was not reproduced in this sample). |
| 3 | Thinking default | With no `thinking` field, the model thinks. Every response starts with a `thinking` block. |
| 4 | Thinking signature | Always `""`. The docs say "currently fixed as an empty string". The stream sends one `signature_delta` with an empty value. |
| 5 | `thinking:{type:"disabled"}` | Accepted. The response still has an empty `thinking` block (`thinking:""`, `signature:""`) before the text. |
| 6 | `thinking:{type:"adaptive"}` + `output_config.effort` | Accepted. The docs call `budget_tokens` deprecated and recommend `output_config.effort`. |
| 7 | `budget_tokens` | Accepted, also with `budget_tokens` (2000) > `max_tokens` (1000). The docs say `max_tokens` must exceed the budget; the endpoint does not enforce it. |
| 8 | Streaming with tools | Events: `message_start`, `ping`, `content_block_start/delta/stop`, `message_delta`, `message_stop`. Deltas: `thinking_delta`, `signature_delta`, `input_json_delta`. Lines are `event:` and `data:` with no space after the colon. Stop reason `tool_use`. Tool id `toolu_...`. |
| 9 | Usage | `message_start` reports `input_tokens` 40; `message_delta` reports 299 for the same request. The final count is only in `message_delta`. Extra field `prompt_tokens_details.cached_tokens`. |
| 10 | Replay after a tool call | Accepted in all three forms: thinking with `signature:""`, thinking turned into text, thinking dropped. |
| 11 | Strictness | An empty text block, an empty tool-result text, and a `tool_use` with no `tool_result` (orphan) are all accepted. Real Anthropic rejects the last one. |
| 12 | Caching | Automatic prefix cache. `cache_creation_input_tokens` is always 0. The second call reported `cache_read_input_tokens` 6144, and `input_tokens` excludes cached tokens (463 + 6144 = 6607 of the first call). `cache_control` with `ttl:"1h"` is accepted (likely ignored). |
| 13 | Errors | A bad model returns HTTP 400 with `{"request_id","code":"InvalidParameter","message":"Model not exist."}`; a bad key returns 401 `{"code":"InvalidApiKey"}`. This is not the Anthropic `{"type":"error","error":{...}}` shape. |

**Model facts (third-party pages, not Alibaba docs; verify before hard-coding):** 1M context, up to 384K output, thinking and non-thinking modes, tool calling, JSON output, native vision since 2026-09-11 ([SiliconFlow](https://www.siliconflow.com/zh/models/deepseek-v4-1-flash), [ProPakistani](https://propakistani.pk/2026/09/11/deepseek-v4-1-flash-launches-with-lower-prices-and-native-vision/)). The Alibaba docs say `max_tokens` covers thinking plus output for this model. The Token Plan has no per-token price (subscription credits; a 50% night discount from 22:00 to 08:00 for this model).

**Impact on the gaps:**

- **G1 closed** for the model id; the record numbers (context window, max output, image input) still need a check against the Alibaba model page.
- **G2 changes:** the key env name. Pi has no Token Plan provider. Suggest provider id `alibaba-token-plan` (or similar), base URL as row 1, key from `--api-key`, then an `ASK_*`-scoped or provider-scoped env var (the user already uses `ALIBABA_TOKEN_PLAN_API_KEY`). The "never pass an empty key" rule still applies, because the Anthropic SDK falls back to `ANTHROPIC_*` env.
- **G3/G6 change (new, high):** every thinking block has `signature:""`. In Pi this needs the compat flag `allowEmptySignature` (`AM:1347-1359`, changelog `:861`); without it, Pi replays the thinking as text. Fantasy drops empty-signature thinking with a warning (`FA:1126-1132`). Replay works in all three forms (row 10), so the choice is about fidelity, not acceptance. Recommendation: follow Pi with `allowEmptySignature: true` in the record, and bypass the fantasy drop (patch or body override), or accept the drop and record it. This makes `allowEmptySignature` (H-PROV-26) an H3 item.
- **G5 changes:** the model is not "adaptive" in Pi's Anthropic sense, but the endpoint accepts both `effort` and `budget_tokens`, and recommends `effort`. Recommendation: use the fantasy `Effort` path; H-PROV-13 (budgets) stays in H17. `off` must send `thinking:{type:"disabled"}` (default is thinking on), which fantasy cannot send without `ExtraBody`. The empty thinking block on `off` (row 5) must be dropped by `transformMessages` (empty thinking rule, `TM:111`) and must not show in print output.
- **G7:** the idle timeout default (300 s) is safe for the observed 16 s first byte.
- **G9 changes:** cost cannot come from a per-token price table (subscription). Store usage only; cost stays 0 or "unknown" for this provider. `cache_creation_input_tokens` is always 0, so the 1h split question is moot here. The usage values must come from `message_delta`, not `message_start` (row 9); fantasy uses the final accumulator, which is correct.
- **G10 mostly moot:** caching is automatic. `cache_control` markers are harmless. `long` retention has no effect here.
- **G4 changes:** error bodies are not Anthropic-shaped (row 13), so the SDK type and fantasy `TransientError` will likely be empty. H9 must classify by status and message text. Probe 429 and 5xx shapes later (not reproduced here).
- **Test validity (new, high):** the endpoint accepts orphan tool calls and empty text (row 11). The roadmap H3 test "the API accepts it" therefore proves nothing on this provider. The pairing invariant and the empty-text rules must be asserted on the request JSON offline (golden wire tests), not by live acceptance.
- **Terms of use (new, decision):** the Token Plan is "limited to interactive use in compatible AI programming and agent tools; prohibited for automated scripts or application backends" (team overview). Interactive `ask` use fits. Live tests in CI and a daemon serving many sessions may not fit. Keep live tests manual and env-gated.

## Unresolved questions

1. Does `deepseek-v4.1-flash` on the Token Plan replace the real Anthropic model as the H3 exit target, or is a real Anthropic model still a second target (for strict-API validation)?
2. Empty signatures: follow Pi (`allowEmptySignature`, keep thinking on replay) and work around fantasy, or accept the fantasy drop?
3. Provider id and env var name for the Token Plan key (`ALIBABA_TOKEN_PLAN_API_KEY` as is, or an `ASK_*` name per D7)?
4. G9: accept the loss of raw stop reason, response model and usage on abort in H3, or build the SSE tap now? (The 1h split is moot for this provider.)
5. G8: minimal token estimator in H3, or clamp to `model.maxTokens` and record the deviation?
6. G12: how to map `model_context_window_exceeded` and unknown stop reasons.
7. G10: is `long` cache retention dropped from H3, since caching on this provider is automatic?
8. Apply the six text corrections in section 5, and keep or move the Pi pin (`2bbfcca4` vs v1.0.1)?
9. Still unprobed: the shape of 429 and 5xx errors on the Token Plan endpoint.
