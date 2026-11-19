# DeepSeek Harness: context overflow detection and usage accounting

Date: 2026-11-19 (Asia/Saigon). Read-only source study for AskCore roadmap row H9 (H-RETRY-06 to H-RETRY-10).

## Source manifest

| Item | Value |
|---|---|
| Repository | `/Users/dale/Desktop/workspace/opensources/deepseek-harness` |
| Commit | `ca33fd6843b205e279c7ccb6cb73a472c78f5760` |
| License | MIT (`LICENSE`, "Copyright (c) 2026 DeepSeek") |
| Local changes | `M AGENTS.md` and 7 untracked `.agents/skills/gitnexus*` directories. None of them touch a file cited in this report. |
| Second source | `@earendil-works/pi-ai` `isContextOverflow`, pinned at `1.3.0` (`pnpm-lock.yaml:14`) with a local patch that has no overflow change (`patches/@earendil-works__pi-ai@1.3.0.patch`, no `overflow` hit). The package is not installed locally, so I read upstream tag `v1.3.0` of `earendil-works/pi-mono` (`packages/ai/src/utils/overflow.ts`, MIT). That is the tag, not the installed tarball. |
| Method | `rg` plus file reads. I ran no repository script, installed nothing, and treated all source text as data. |

Path prefixes used below: `llm/` = `packages/llm/llm/src`, `ds/` = `packages/llm/llm-deepseek/src`, `pi/` = `packages/llm/llm-pi-ai/src`, `tm/` = `packages/llm/token-meter/src`, `cb/` = `packages/compaction/compaction-basic/src`, `loop/` = `packages/core/agent-loop/src`, `retry/` = `packages/llm/llm-retry/src`.

## Outcome first

- Overflow is one canonical failure code, `CONTEXT_WINDOW_EXCEEDED`. Each adapter maps to it from the HTTP status plus a shared regex classifier over the provider code, type, and message. The pi-ai route also uses pi-ai's per-provider patterns and its usage-based silent-overflow checks. The transient retry policy does not retry this code. Of the `agent/request-error` listeners in non-test source (compaction-basic, compaction-image-offload, llm-retry), only compaction-basic handles it. It force-compacts once (default `maxOverflowRetries: 1`) and retries only if the surface really shrank. In all other cases the turn ends with the error.
- Usage is counted in tokens only. First-party model calls have no monetary cost: there is no price table and no per-request currency, and pi-ai cost metadata is set to zero on purpose (`pi/catalog.ts:33-38`). The only nearby money facts are a server-reported wallet balance and a Claude Code sub-agent's `error_max_budget_usd` failure subtype (`packages/subagent/subagent-claude-code/src/run.ts:116`).
- Usage is persisted inside the existing assistant events (`assistant/message.usage` plus the `usage` chunk in the embedded stream; `assistant/attempt.stream` for failed attempts). There is no dedicated usage entry. Totals are recomputed by pure projection folds and cached as versioned checkpoints.
- The context-size estimate is "last provider usage plus a chars/4 heuristic delta of the surface since that usage". A provider number is used only when the request envelope is the same.

## 1. Overflow detection

### 1.1 Canonical codes and the shared classifier

- `llm/error.ts:17-71` lists every failure code. `retryChoice` marks the codes that a retry policy may offer: `EMPTY_RESPONSE`, `MALFORMED_RESPONSE`, `QUOTA`, `RATE_LIMIT`, `SERVER`, `STREAM_CLOSED`, `TIMEOUT`, `TRANSPORT`. `CONTEXT_WINDOW_EXCEEDED` (`:25`) is not a retry choice.
- `QUOTA` is provider-neutral exhaustion and `ACCOUNT_QUOTA` is the first-party balance that the product can top up (`llm/error.ts:85-89`). `packages/llm/llm-deepseek-account/src/index.ts:58-59` rewrites `QUOTA` to `ACCOUNT_QUOTA` for the DeepSeek account route.
- `isContextWindowExceededError(detail)` (`llm/error.ts:111-147`) has five regexes. They match structured codes (`context_length_exceeded`, `context-window-overflowed`), "maximum context length/window", "request/prompt/input too large/long for (the) (model's) context", and "input ... exceeds ... model context" within 40 characters. The regexes are written so that "input exceeds maximum allowed value" or "context window size must be positive" do not match (pinned by `packages/llm/llm/tests/service.spec.ts:129-134`).
- `isQuotaExceededError(detail)` (`llm/error.ts:155-161`) matches `insufficient_quota/balance/credits`, "quota/usage limit exceeded/exhausted/reached", "exceeded your current quota", "balance depleted", "out of credits/budget". A 429 that says "You exceeded your current quota" is QUOTA, not RATE_LIMIT. "quota resets in one minute" is not QUOTA (`service.spec.ts:136-146`).

### 1.2 DeepSeek native route (Anthropic Messages wire)

`ds/transport.ts:34-54` uses a fixed order. The first match wins:

| Order | Rule | Code |
|---|---|---|
| 1 | 401, 403, `authentication_error`, `permission_error` | `AUTH` |
| 2 | quota text, or 402 | `QUOTA` |
| 3 | 429 or `rate_limit_error` | `RATE_LIMIT` |
| 4 | overflow text | `CONTEXT_WINDOW_EXCEEDED` |
| 5 | 400, 413, `invalid_request_error` | `INVALID_REQUEST` |
| 6 | 5xx, `api_error`, `overloaded_error` | `SERVER` |
| 7 | no status (in-band SSE error) | `SERVER`; else `HTTP_<status>` |

- `detail` is `type + code + message` (`:39`). `Retry-After` (seconds or HTTP date) becomes `providerRetryAfterMs` (`:18-26`, `:48`). The request id comes from `request-id`, `x-request-id`, or `x-deepseek-request-id` (`:49`).
- In-band SSE `error` events use the same function with `status` undefined (`ds/sse.ts:25`).
- A bare 413 is `INVALID_REQUEST`, not overflow, on this route.
- The adapter has no silent-overflow check: `ds/translate.ts` never sees the context window. `max_tokens` maps to `max-tokens` (`:101`). The official window is 1,048,576 tokens (`ds/models.ts:5`).
- `refusal` yields the billed `usage` chunk and then throws `CONTENT_REFUSED` (`ds/translate.ts:153-161`). A terminal `stop` with zero blocks is `EMPTY_RESPONSE` (`:164`).

### 1.3 pi-ai route (multi-provider library)

- `pi/stream.ts:88-101` (`mapStopReason`) checks overflow first: pi-ai `isContextOverflow(message, contextWindow)` OR the harness classifier on `errorMessage`. Either one gives `CONTEXT_WINDOW_EXCEEDED`. After that, a `stop` with no content is `EMPTY_RESPONSE` (`:103-116`), `length` is `max-tokens` (`:117`), and other errors go to `classifyPiAiError` (`:131-133`).
- `classifyPiAiError` (`pi/stream.ts:49-76`) works only on flattened text, because pi-ai drops the `cause` chain (`:41-48` comment). It reads only a leading `[45]\d\d` status so that a status quoted in the body is ignored (`:34-39`). Order: 401/403 AUTH, quota text QUOTA, 429 or "rate limit" RATE_LIMIT, 413 or "payload too large" INVALID_REQUEST, 400 or "invalid request" INVALID_REQUEST, 5xx SERVER, timeout TIMEOUT, "stream ended before/without" TRANSPORT, socket wording TRANSPORT, else `PI_AI_ERROR`.
- pi-ai `v1.3.0` `overflow.ts`:
  - `:37-63` has 25 provider patterns (lines 38-62). Examples: Anthropic `prompt is too long` and `request_too_large`, Bedrock, OpenAI `exceeds the context window`, Gemini, xAI `maximum prompt length is N`, Groq, OpenRouter, Together, llama.cpp, LM Studio, MiniMax, Kimi, Mistral, z.ai, Ollama, DashScope, and generic `too many tokens` / `token limit exceeded`. `:65` adds a Cerebras bodyless 400/413 pattern.
  - `:76-80`: `NON_OVERFLOW_PATTERNS` (`rate limit`, `too many requests`, Bedrock throttling prefixes) veto a match. This is why "ThrottlingException: Too many tokens, rate limit reached" stays RATE_LIMIT (pinned by `packages/llm/llm-pi-ai/tests/convert.spec.ts:974-983`).
  - `:152-158` (silent overflow): `stopReason === 'stop'` and `input + cacheRead > contextWindow`. Cache writes are not counted.
  - `:160-168` (length-stop overflow, Xiaomi MiMo style): `stopReason === 'length'`, `output === 0`, and `input + cacheRead >= 0.99 * contextWindow`.
  - Newer upstream `main` adds a generic `^413` pattern and LiteLLM `context window exceeded`. Version 1.3.0 does not have them.
- The adapter passes the resolved catalog window into the stream (`pi/adapter.ts:388`, `:483`; pinned by `packages/llm/llm-pi-ai/tests/adapter.spec.ts:501`).
- `pi/stream.ts:225-247`: the `done` and `error` events both yield `usage` before `finish`. A silent-overflow response is therefore counted as billed usage and then discarded.

### 1.4 What happens after detection

1. An adapter throw (selection, dispatch, or iteration) becomes a terminal `finish {kind:'error'|'aborted', failure}` chunk with no usage chunk (`llm/index.ts:1106-1124`, `:1169-1178`). Middleware, nested-call, cleanup, and consumer failures stay thrown (`:1145-1148`). In those cases the loop writes `assistant/attempt` and rethrows without running recovery (`loop/agent.ts:505-546`).
2. The loop writes `assistant/attempt {turn, step, stream}` (the failed stream, kept out of model history) and runs the `agent/request-error` waterfall (`loop/agent.ts:547-569`). With no `{kind:'retry'}` answer it throws `LlmError.fromFailure`, and the turn ends with `turn/end {reason:{kind:'error', error: failure}}` (`loop/agent.ts:415-428`).
3. `llm-retry` (`retry/index.ts:191-239`), normal mode: it retries only `retryableCodes`. The default set is `EMPTY_RESPONSE, RATE_LIMIT, SERVER, TIMEOUT, TRANSPORT`, with `maxRetries 5`, backoff 500 ms to 10 s, and jitter 0.1 (`llm/retry-policy.ts:15-25`). `QUOTA`, `AUTH`, and overflow are not retried by default. A provider `Retry-After` larger than `maxDelayMs` makes normal mode give up at once (`retry/index.ts:228-229`). The retry count lives in a durable projection that resets on `step/start` and `turn/end` (`:128-135`), so the budget is per step, per provider and policy. The `base` and `sdk-minimal` bundles mount `llm-retry` (`packages/bundle/base/cordis.patch.yml:99-100`, `packages/bundle/sdk-minimal/cordis.patch.yml:133-134`). No YAML under `packages/bundle` or `packages/preset` sets `retryPolicy`, so these code defaults apply unless a user config changes them.
4. `compaction-basic` (`cb/index.ts:228-272`): it acts only on `CONTEXT_WINDOW_EXCEEDED`.
   - It stops when the per-agent counter reaches `maxOverflowRetries` (default 1, `cb/config.ts:106`).
   - It runs the tool-result pruner, measures, selects a range with retain = 0 (it bypasses the threshold and the retained-tail policy), and summarizes (`cb/index.ts:320-326`).
   - It returns `retry` only when `surface.replaceGeneration` advanced. A prune that lands before a summary fails is enough (`:240-257`).
   - The counter is an in-memory `WeakMap`. It resets on agent idle or on any successful `assistant/message` (`:192-194`, `:220-226`).
   - When no summarization target is configured, the summary uses the route that last answered, not the route that overflowed (`cb/index.ts:72-75`, `:288-297`; `compaction-loop-repro.spec.ts:561`).
   - A related pattern: `compaction-image-offload` handles `IMAGE_OFFLOAD_REQUIRED` in the same waterfall. It offloads the oldest images and retries without using the retry budget and without a retry event (`packages/compaction/compaction-image-offload/src/index.ts:27-31`).
   - The overflow budget and the transient retry budget are independent (`compaction-loop-repro.spec.ts:451`).
5. Proactive path: `agent/pre-step` compacts when `measure().totalTokens >= threshold` (`cb/index.ts:172-190`, `:343-360`). `threshold = floor(min(window * 0.8, window - reservedOutput - 65536))` (`cb/config.ts:172-194`, defaults at `:20` and `:75`). `reservedOutput = header.maxTokens ?? model default` (`cb/index.ts:68-70`). A model switch compacts against the new route and fails with `MODEL_SWITCH_COMPACTION_FAILED` if the history does not fit (`cb/index.ts:198-218`).

## 2. Usage accounting

### 2.1 Shape

`TokenUsage` (`llm/types.ts:182-196`) has `inputTokens`, `outputTokens`, `totalTokens?`, `cacheReadTokens?`, `cacheWriteTokens?`, and `reasoningTokens?`.

- `inputTokens` means uncached input (Anthropic semantics). The DeepSeek native total is `input + output + cacheRead + cacheWrite` (`ds/translate.ts:157`).
- Usage comes from the `message_start` and `message_delta` usage objects, last value wins per field (`ds/translate.ts:35-44`, `:120`, `:155`).
- pi-ai usage maps `input, output, totalTokens`, and cache fields only when they are not zero. Reasoning is folded into output (`pi/stream.ts:18-32`).
- `reasoningTokens` is declared (`llm/types.ts:195`), but neither built-in adapter fills it: see `ds/translate.ts:37` (four wire keys) and `pi/stream.ts:24-32`. Folds and UI read it only when it is present.

### 2.2 Sums

`tokenUsage` projection (`tm/usage-projection.ts:111-144`):

- Buckets are `uncachedInputTokens`, `outputTokens`, `cacheReadTokens`, and `cacheWriteTokens`. There is no reasoning bucket and no total.
- It reads the usage of `assistant/message` (the `data.usage` field, else the last `usage` chunk in the stream) and of `assistant/attempt` (the last stream `usage` chunk) (`:82-86`).
- Within one (turn, step) slot, a newer sample replaces the older one (`addReplacing`, `:34-43`). `llm/retry-started` closes the slot, so a retried attempt adds to the total (`:117-121`).
- Failed attempts count when they reported usage. Examples: a refusal, a pi-ai `error` event (which always yields a `usage` chunk, `pi/stream.ts:239`), or a silent overflow. An HTTP error before the stream reports nothing, so it adds nothing.
- The sum covers the whole durable log across turns. Compaction replacement does not erase it (`token-usage-projection.spec.ts:272`).

Per-turn `deriveTurnTokenUsage` (`tm/turn-usage.ts:178-274`) is a strict lifecycle state machine:

- The sequence is `turn/start`, then `step/start`, then (`assistant/attempt` | `assistant/message`), then optional `llm/retry` and `llm/retry-started`, then `step/end`, then `turn/end`.
- It checks each attempt: counts must be safe integers, `totalTokens - output >= input + cache`, and `reasoning <= output` (`:81-124`).
- It sums across attempts and steps. Optional buckets and the route list appear only when every attempt reports them (`:126-159`).
- It fails closed (returns `undefined`) when the lifecycle is wrong or an attempt has no usage (`turn-usage.spec.ts:149-156`, `:365-371` "error finish without usage", `:391-395` "retry before any usage").

Consequence: on the DeepSeek native route, a turn with one retried HTTP 429 shows no per-turn usage panel, because the 429 attempt has no usage chunk. The session total still counts the successful attempt.

A third fold exists. The headless JSON stream sums usage for each step across attempts and drops the step usage when any attempt reported none (`packages/bundle/headless/src/json-stream.ts:195-227`, `:252-280`). So the repository has three usage folds with similar but separate rules: session total, per turn, and headless per step.

### 2.3 Not counted

- Compaction summary calls: `compaction/summary` carries `usage?` (`packages/compaction/compaction/src/types.ts:34-53`), but the `tokenUsage` fold returns early for every event except `assistant/message` and `assistant/attempt` (`tm/usage-projection.ts:122-124`).
- Session-title LLM calls: likely not counted, for the same reason. This is inferred; I did not trace the title package.
- Sub-agents: the catalog shows each child's total from that child's session summary (`packages/client/ui-subagent/src/client/SubagentCatalog.tsx:280`). This suggests each child has its own session log, but I did not confirm where sessions are created. I found no code that adds child usage to the parent.

### 2.4 Cost

None. `pi/catalog.ts:33-38` says the harness never reads pi-ai cost metadata, and `pi/replay.ts:68` zeroes it. The only currency code is the account wallet balance (`packages/account/deepseek-account/src/types.ts:61-64`): server-reported `CNY|USD` decimal strings, not per-request cost.

## 3. Persistence and resume

- Session events (`packages/core/session/src/types.ts:353-367`):
  - `assistant/message {turn, step, message, stream, usage?, interrupted?}`
  - `assistant/attempt {turn, step, stream}`: a failed, retried, cancelled, or stream-error attempt, never model-visible
  - `llm/retry {retryId, turn, step, provider, mode, policyKey, retry, maxRetries?, delayMs, failure}` and `llm/retry-started {retryId, turn, step, retry}` (`retry/types.ts:15-48`)
  - `request/context {provider, model, contextWindow?, systemPromptUpdate?}`, appended only when it changes (`loop/agent.ts:715-729`)
- An aborted stream with visible content is kept as `assistant/message` with `interrupted: true` and its usage (`loop/agent.ts:508-525`).
- Totals are never stored as log entries. They are recomputed by projections (`tokenUsage` stateVersion 2, `contextPressure` stateVersion 5). The projection registry checkpoints each unit's state with `{ver, seq, val}`. A cold read refolds only the suffix after the checkpoint, and a `stateVersion` bump drops old rows (`packages/session/session-projection/README.md:36`, `:98-100`, `:133`). The restore paths are pinned by `token-usage-projection.spec.ts:298` and `:443`.

## 4. Context-usage estimate

There are two separate estimators. Keep them apart.

| | `measure().totalTokens` | `contextPressure.projectedTokens` |
|---|---|---|
| Used by | compaction decisions | UI occupancy meter |
| Source | `tm/index.ts:151-196` | `tm/usage-projection.ts:167-212` |
| Anchor | last successful call usage, including output (`usageTokens`, `:57-61`) | last usage sample, prompt side only (`input + cacheRead + cacheWrite`, `:77-79`) |
| Anchor used when | canonical request envelope (tools, provider, model, call config) equals the anchor's AND usage >= heuristic price of the same anchor (`:163-175`) | always (last-wins) |
| Delta | signed heuristic delta of the surface since the anchor (`:176`) | signed surface delta folded at O(1) with compaction "shadow claims" (`:193-200`) |
| Fallback | full heuristic estimate of tools + surface (`:180-185`) | absent until the first usage |

- The heuristic is `ceil(chars/4)` plus 4 per block plus 4 per role; tool schemas are `JSON.stringify(tools)/4` (`tm/estimate.ts:12-19`, `:42-102`). Images are priced from the adapter's visual-token pricing when the route declares it (`tm/route-pricing.ts`, `ds/request-pricing.ts`).
- The README admits that chars/4 underprices CJK text and JSON schemas (`packages/llm/token-meter/README.md`, "Reading the numbers").
- The UI shows `round(projectedTokens ?? pressureTokens) / contextWindow`, capped at 100 % (`packages/client/ui-conversation/src/client/context-occupancy.ts:15-25`), as "N% of context used" (`skeleton/ContextMeter.tsx:84`).
- There is no warning threshold. The only threshold is the compaction trigger in section 1.4 item 5.
- The pressure fields are explicitly not one atomic observation. After a model switch, the new window can pair with the old usage until the next request (`tm/projection.ts:22-31`).

## 5. Wire and UI surface

- Live: the loop emits `agent/assistant-stream` frames (`loop/agent.ts:492`). The usage chunk `{type:'usage', usage}` (`llm/types.ts:502`) travels inside them.
- Durable: the events in section 3. Client projections `tokenUsage`, `contextPressure`, and `contextBreakdown` are pushed through snapshot and change notifications. Only `tokenUsage` appears in session-list summaries; the other two carry `SessionProjectionSummaryExcluded` (`tm/projection.ts:70-78`).
- Retries are shown as a `model-retry` chat node keyed by `retryId`. Its state is `scheduled`, then started, then cancelled when its step or turn closes (`packages/client/ui-chat/src/client/conversation-nodes/retry.ts:16-55`).
- An overflow that compaction recovers produces no special event. The visible signs are `assistant/attempt` plus `compaction/start`, `compaction/summary`, and `compaction/end`. These compaction events do not record the trigger (`compaction/src/types.ts:24-72`); the trigger appears only in a log line (`cb/index.ts:269`).
- An unrecovered overflow is `turn/end` with an error reason. The turn-error node renders it.

## 6. Tests that pin the behavior

- Classifier: `packages/llm/llm/tests/service.spec.ts:120-146`. DeepSeek HTTP overflow and 429-vs-quota: `packages/llm/llm-deepseek/tests/runtime.spec.ts:1436`, `:1538`.
- pi-ai: `convert.spec.ts:974` (provider patterns and the rate-limit veto), `:985` (silent and length-stop overflow), `:1006` (cache fields only when not zero). `adapter.spec.ts:501` (catalog window used).
- Overflow recovery: `compaction-basic.spec.ts:688`, `:712`, `:2166`, `:2187`, `:2208`, `:2229`, `:2277`, `:2380`, `:2392`, `:2416`, `:2449`. `compaction-loop-repro.spec.ts:451`, `:561`.
- Retry: `packages/llm/llm-retry/tests/retry.spec.ts:207` (delay recorded before the wait), `:257` (EMPTY_RESPONSE), `:341` (budget and jitter), `:397` and `:428` (Retry-After cap in each mode), `:454` (non-transient codes delegated). `transport-recovery.spec.ts:105-309`.
- Usage folds: `token-usage-projection.spec.ts:117-298` (no double count, same-step replacement, retries add, attempt without a final message counts, checkpoint restore) and `:362-533` (pressure). `turn-usage.spec.ts` (fail-closed cases).

## 7. Trade-offs and weaknesses

Strengths:
1. One code-based contract. Consumers route on `failure.code` and never on text (`packages/llm/llm/README.md:99`).
2. Recovery is a waterfall of independent plugins with separate budgets.
3. A forced compaction retries only on proof of progress (the replacement generation advanced), so it cannot loop on an unchanged request.
4. Usage folds are pure and replay-stable, with the retry boundary made explicit in the log (`llm/retry-started`).
5. Per-turn numbers fail closed instead of guessing.

Weaknesses:
1. Overflow classification differs by route. DeepSeek native treats 413 as INVALID_REQUEST and has no silent-overflow check. pi-ai treats Anthropic `request_too_large` as overflow and checks usage against the window.
2. The text regexes are a moving target. pi-ai upstream added patterns after 1.3.0.
3. A per-turn usage panel disappears whenever one attempt in the turn has no usage, for example any retried HTTP error on the native route.
4. Compaction-summary usage, and probably title-call usage, is billed but missing from `tokenUsage`. Sub-agent usage is not added to the parent.
5. `reasoningTokens` exists in the type, but no adapter fills it and `tokenUsage` has no bucket for it.
6. The overflow retry counter is in memory only. Retries are capped at 1 per success streak, so the risk is small.
7. In `always` retry mode, `llm-retry` retries every code after downstream declines (`retry/index.ts:198-215`). An overflow that compaction cannot reduce is then sent again and again until the user cancels. No YAML under `packages/bundle` or `packages/preset` sets `always`.
8. A normal-mode 429 with `Retry-After` above 10 s fails at once instead of waiting.
9. There is no cost accounting at all.
10. The pressure estimate is chars/4. The README documents this as a known error.
11. There are three separate usage folds (section 2.2). Their rules match today, but they can drift.

## 8. Most portable ideas for AskCore H9

AskCore already has `sessions.AttemptSettled{Outcome, Usage *protocol.Usage (nil = unknown), Failure{Code, Text}}` (`internal/sessions/entry.go:52-71`). H4 owns pricing (micro-USD `protocol.Usage`, `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md:162`, `:248`). I did not study AskCore in depth. Ranked by value against effort:

| Rank | Row | Idea | Value | Effort | Risk |
|---|---|---|---|---|---|
| 1 | H-RETRY-06 | One canonical `CONTEXT_WINDOW_EXCEEDED` code, decided at the adapter boundary in this order: auth, quota, rate limit, overflow text, 400/413 invalid, 5xx server. Port the harness regexes plus pi-ai v1.3.0 `OVERFLOW_PATTERNS` and `NON_OVERFLOW_PATTERNS` as table-driven tests. Add the usage checks (`stop` with `input + cacheRead > window`; `length` with `output == 0` and `>= 0.99 * window`) where AskCore knows the model window. Keep overflow out of the transient retry set. | High | Low | Text patterns drift; take the provider list from the AskCore roster only (OpenAI, Anthropic, xAI, Alibaba). |
| 2 | H-RETRY-06 | Recover with one forced compaction. Retry only if the log really changed. Keep a counter per success streak, separate from the transient budget, and summarize with the route that last answered. | High | Medium (needs compaction) | Without a progress check, a retry can loop on the same request. |
| 3 | H-RETRY-07/08 | Compute totals as a pure fold over `AttemptSettled.Usage`: replace samples inside an attempt and add across attempts. AskCore's per-attempt entry already gives the boundary that DeepSeek gets from `llm/retry-started`. Treat `nil` as unknown and show a per-turn total as partial instead of hiding it (avoids weakness 3). Decide on purpose whether compaction and title calls count (weakness 4). | High | Low | Possible double count: `protocol.AssistantMessage` also carries `Usage` (`internal/sessions/codec_test.go:33`). Confirm which entry is the source of truth before you fold both. |
| 4 | H-RETRY-09 | Estimate context as last usage (prompt plus output) plus the chars/4 estimate of entries added after it. Fall back to a full estimate when tools, model, or provider change. Use `max(usage, heuristic of the same prefix)` as the floor. Keep the display number (prompt side) separate from the compaction number. | Medium | Low | chars/4 underprices CJK text and JSON. The usage anchor limits the error to the delta. |
| 5 | H-RETRY-10 | Option for the row owner, not a recommendation. DeepSeek writes no total entries: it recomputes totals and caches checkpoints. Option A: keep usage only on attempt and message entries, and fold on resume (no new entry type; resume is O(log)). Option B: the roadmap's persistent usage entries (fast resume; a second source of truth that must agree with the fold). | Medium | A: Low, B: Medium | B can drift from the fold. A costs a full log scan on resume. |

## Limitations

- I did not run tests, so behavior is as written in code and test expectations.
- The pi-ai behavior comes from the upstream tag, not the installed tarball. The local patch was checked only for overflow edits.
- I did not trace the session-title and telemetry packages (`session-telemetry`, `session-stats`), so "title usage not counted" is an inference.
- I did not read the DeepSeek runtime test bodies at `runtime.spec.ts:1436` and `:1538`, only their names.

## Unresolved questions

1. Does any host-side telemetry (`packages/session/session-telemetry*`, `session-stats`) sum compaction or title usage that `tokenUsage` drops?
2. Does `LlmRuntime` or another listener ever give a native HTTP error a synthetic zero usage chunk? I found none, but `llm/stream` waterfall listeners could do so.
3. For AskCore: should failed attempts with unknown usage make the per-turn total "partial" or "unknown"? This is a product decision for H9.
4. Does every sub-agent driver (in-process, fork, ACP, Claude Code, Codex) write a separate child session? Its usage would then never reach the parent total.
5. For AskCore: which entry is the source of truth for usage, `AttemptSettled.Usage` or `AssistantMessage.Usage`?
