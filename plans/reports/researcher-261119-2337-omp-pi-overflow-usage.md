# Overflow detection and usage accounting in oh-my-pi (omp) and Pi

Date: 2026-11-19 23:37 (Asia/Saigon). Read-only source study for AskCore roadmap row H9 (H-RETRY-06 to H-RETRY-10).

## Source manifest

| Source | Path | Commit | Local changes | License |
|---|---|---|---|---|
| Pi upstream | `/Users/dale/Desktop/workspace/opensources/pi` | `4c6fb7cfe` | `M AGENTS.md`, `?? CLAUDE.md`. Neither is a cited file. | MIT |
| oh-my-pi (omp) | `/Users/dale/Desktop/workspace/opensources/oh-my-pi` | `eb6132a38b` | `?? packages/coding-agent/src/export/html/tool-views.generated.js`. Not a cited file. | MIT |

Method: `rg` and direct reads only. I ran no scripts, built nothing, and followed no instructions in the sources. Each finding has a label: **[code]** means read in code, **[test]** means a test pins it, and **[absence]** means I searched and found nothing (lower confidence). Pi paths are relative to `pi/packages/`. omp paths are relative to `oh-my-pi/packages/`.

## Outcome first

- **Pi is the model to port.** It has one regex file for overflow (`ai/src/utils/overflow.ts`), one regex file for retryable errors (`ai/src/utils/retry.ts`), and one compact-and-retry attempt for each user prompt. Usage stays on each persisted assistant message, and totals are a scan over all entries in the session file. Failed retry attempts are counted because they stay persisted. Pi hides them from the model with a `context_edit` entry; it does not delete them.
- **omp is the same design with more evidence and more defenses.** It uses a bit-flag error classifier (`ContextOverflow`, `UsageLimit`, `PayloadRejected`, `AuthFailed`, and others). A 429 or 402 status vetoes overflow wording. It separates byte-size 413 errors from token overflow, and its silent-overflow check works with any stop reason. It promotes to a larger model before it compacts. It uses a native tokenizer for the trailing estimate and an incremental usage index. When it deletes a failed turn, it writes the billed usage again as a `model_usage` entry.
- **Most portable for AskCore, ranked:** (1) Pi's overflow regex set plus omp's 429/402 veto; (2) usage on the persisted assistant message, with totals rebuilt from all entries, and failed attempts kept; (3) Pi's estimate: the last valid assistant usage plus a chars/4 estimate of the trailing messages, with `reserveTokens=16384`; (4) a separate off-transcript usage entry for model calls that are not turns. The details and costs are in section 8.

## 1. Overflow detection and classification

### Pi

- **Overflow patterns.** 25 case-insensitive regexes, each commented with its provider: Anthropic `prompt is too long` and `request_too_large`, Bedrock, OpenAI `exceeds the context window`, LiteLLM, Gemini, xAI, Groq, OpenRouter, Together, Copilot, llama.cpp, LM Studio, MiniMax, Kimi, Mistral, DS4, z.ai, Ollama, DashScope, and the generic fallbacks `context_length_exceeded`, `too many tokens`, `token limit exceeded` [code] `ai/src/utils/overflow.ts:37-63`. Cerebras returns a bare `400/413 (no body)`, which counts as overflow only when `provider === "cerebras"` [code] `overflow.ts:65,146`.
- **Non-overflow veto.** Three patterns block an overflow match: the Bedrock `Throttling error:` and `Service unavailable:` prefixes, `rate limit`, and `too many requests`. Example: Bedrock "ThrottlingException: Too many tokens" [code] `overflow.ts:76-80,141`. Pi uses no HTTP status here. It classifies by text only.
- **Silent overflow.** Case 2: `stopReason === "stop"` and `input + cacheRead > contextWindow` (z.ai). Case 3: `stopReason === "length"`, `output === 0`, and `input + cacheRead >= 0.99 * contextWindow` (Xiaomi MiMo) [code] `overflow.ts:152-168`. `cacheWrite` is not in the sum.
- **Recoverable length stop.** `isRecoverableLength` is true when `stopReason === "length"` and `usage.output < model.maxTokens`, the original output limit before context clamping [code] `overflow.ts:179-181`.
- **Retry classifier, a separate file.** `isRetryableAssistantError` first rejects quota and billing text (`insufficient_quota`, `quota exceeded`, `billing`, `GoUsageLimitError`, `subscription_sharing_usage_limit_exceeded`). Then it accepts transient text: `overloaded`, `rate.?limit`, `429`, `5xx`, network and fetch errors, early stream end, `retry delay`, and `ResourceExhausted` [code] `ai/src/utils/retry.ts:7-102,247-252`. Auth errors (401, 403, invalid key) are in no list, so they are surfaced and not retried [absence] (`rg` for `401|403|unauthori|auth` in `retry.ts` and `provider-retry.ts` found nothing).
- **Ordering.** `_isRetryableError` returns false for overflow first ("handled by compaction instead"), then calls the regex classifier [code] `coding-agent/src/core/agent-session.ts:3660-3664`.
- **Second layer at the transport.** `retryProviderRequest` copies the SDK policy: it obeys `x-should-retry`, retries 408, 409, 429, and 5xx or no status, and reads `retry-after-ms` and `retry-after`. When the server asks for a delay above `maxRetryDelayMs` (default 60 s), it fails at once [code] `ai/src/utils/provider-retry.ts:23-35,37-49,105-125`.
- **Action after detection** [code] `agent-session.ts:2900-3038`:
  - The check is skipped for an aborted message and for a message older than the latest compaction (`2909-2928`). Only the overflow and length branches need the same model (`2957-2962`); the threshold check at `3034` still runs for a message from another model.
  - Overflow on a `stop` response: compact, no retry (`2964-2970`).
  - Overflow or recoverable length on an error or length response: Pi writes a `context_edit` entry that omits the failed attempt (and its tool results), compacts with `reason: "overflow"`, and then calls `agent.continue()` (`2994-2999`).
  - Budget: one attempt. `_overflowRecoveryAttempted` blocks a second attempt and emits `compaction_end` with `willRetry: false` and "Context overflow recovery failed after one compact-and-retry attempt" (`2972-2992`). The flag resets on each new user message and on each assistant message that is not an error or length stop (`1091,1141-1143`).
- **Agent retry budget.** Defaults: `maxRetries 3`, `baseDelayMs 2000`, `maxAgentDelayMs 60000`, exponential [code] `coding-agent/src/core/settings-manager.ts:1000-1005`, `retry.ts:123-127`.

### omp

- **Classifier.** `classify` and `classifyMessage` produce a bitmask: `Transient`, `Timeout`, `UsageLimit`, `ContextOverflow`, `AuthFailed`, `PayloadRejected`, `AccountPolicy`, `ContentBlocked`, and others [code] `ai/src/error/flags.ts:21-57`. `retriable()` accepts `Transient | UsageLimit | ThinkingLoop | StaleResponsesItem | ProviderFinishError | EmptyResponse`. It rejects `ContentBlocked` and `PayloadRejected` (`flags.ts:83-89,506-512`).
- **Overflow patterns.** About 27 patterns. They overlap with Pi's and add Strata, llama.cpp `n_ctx` variants, "prompt filled the context window" (Ollama), and "chat history exceeds the N-message limit" [code] `flags.ts:91-121`.
- **Status veto.** Overflow text on a 429 or 402 is never overflow ("a throughput or quota decision, never window excess") [code] `flags.ts:654-657,1147-1151`, `ai/src/error/rate-limit.ts:436-438`. [test] `ai/test/overflow-utils.test.ts:82-96`.
- **413 split.** A byte or media 413 sets `PayloadRejected`. Token wording vetoes that flag. A bare `413 (no body)` sets both flags, and local headroom decides [code] `flags.ts:152-167,747-755,1154-1176`. [test] `overflow-utils.test.ts:116-117`, `coding-agent/test/agent-session-payload-rejection-413.test.ts:428-785`.
- **Usage limits and auth by status and code.** `usage_limit_reached`, `insufficient_quota`, and a 402 billing body set `UsageLimit`. `overloaded_error`, `rate_limit_error`, a 429 without a usage limit, and 5xx set `Transient`. 401 and 403 set `AuthFailed`, except a 403 concurrency cap [code] `flags.ts:812-847`. A concurrency cap is `Transient`, not `UsageLimit`, so it does not rotate a credential (`flags.ts:682-698,729`).
- **Silent overflow.** `isUsageBackedContextOverflow` uses `contextTokens ?? input + cacheRead + cacheWrite`, compares with `>` against the window, and applies to **any** stop reason. `contextTokens` is preferred because Cursor sums usage over its server-side tool loop [code] `flags.ts:1125-1138`. [test] `overflow-utils.test.ts:62-71`. omp has no 0.99 length-stop rule like Pi's Case 3. Length stops go to "incomplete" recovery.
- **Retry ordering.** `isRetryableError` rejects overflow. It rejects a replay that is not safe (text, image, or tool output already sent) unless every emitted tool call has proof that it did not run. Then it uses `retriable(id)` [code] `coding-agent/src/session/turn-recovery.ts:1615-1649`. A `UsageLimit` is retriable because omp rotates credentials, walks fallback chains, or waits until the quota resets (`turn-recovery.ts:3271-3308`; `retry.waitForUsageReset` defaults to false, `coding-agent/src/session/settings.ts:825-827`).
- **Agent retry budget.** Defaults: `maxRetries 10`, `baseDelayMs 500`, `maxDelayMs 300000`. A delay request above the cap fails at once unless a credential or model switch happened [code] `settings.ts:791-814`, `turn-recovery.ts:3252-3269`.
- **Action after detection** [code] `coding-agent/src/session/session-maintenance.ts:3079-3540`, four cases:
  1. Overflow with a promotion target: switch to the larger model and retry with no compaction (`3226-3242`).
  2. Overflow with no target: drop the failed turn from the branch, compact, and retry. If compaction rewrites nothing, omp restores the failed turn and schedules no continuation (`turn-recovery.ts:1392-1410`).
  3. Length stop: if the window is exhausted, compact and retry. If the turn produced nothing usable, retry as is. If a real answer was cut short, keep it with a warning. A cap of `INCOMPLETE_RECOVERY_MAX_RETRIES = 3` limits no-progress recoveries (`session-maintenance.ts:172,3385-3515`).
  4. Threshold compaction (`3542-3612`).
- **omp overflow-retry budget.** I found no counter like Pi's one-attempt flag in `session-maintenance.ts`, `turn-recovery.ts`, or `agent-session.ts` [absence]. The loop guard is progress-based: a compaction that rewrites nothing restores the failed turn and schedules nothing (`turn-recovery.ts:1405-1408`). A rescue with no progress returns `COMPACTION_CHECK_BLOCK_AUTOMATIC_CONTINUATION` (`session-maintenance.ts:5595-5602`). A mid-turn dead end stays suppressed until the cut point moves (`576-581,2974-2990`). omp `docs/compaction.md:126-142` describes the same flow and gives no count. A compaction that makes progress each time but still overflows has no visible cap.

**omp changed vs Pi:** Pi classifies by text with a small veto list and allows one compact-and-retry attempt. omp adds status-aware veto, splits byte rejection from token overflow, uses `contextTokens`, promotes before it compacts, and rotates credentials or waits on a usage limit. That costs about 1,240 lines in `flags.ts` alone, plus very large recovery files.

## 2. Usage shape and summation

### Pi

- **Shape.** `input`, `output`, `cacheRead`, `cacheWrite`, optional `cacheWrite1h` (a subset of `cacheWrite`), optional `reasoning` (a subset of `output`), `totalTokens`, and `cost{input, output, cacheRead, cacheWrite, total}` [code] `ai/src/types.ts:427-448`.
- **Pricing.** `calculateCost` uses USD per million tokens from `model.cost` and picks the highest `tiers[].inputTokensAbove` that `input + cacheRead + cacheWrite` exceeds. A 1-hour cache write costs 2x the input rate. Values are float64 with no rounding [code] `ai/src/models.ts:1193-1213`. Prices probably come from the generated catalog (`ai/src/providers/*.models.ts`) and the user models file [inferred; not read this run].
- **Summation.** `addUsageToTotals` sums the four token fields and `cost.total` [code] `coding-agent/src/core/usage-totals.ts:22-28`. `combineUsage` merges two `Usage` values. It is used for nested tool calls (`agent-session.ts:1082`, `nested-tool-calls.ts:88`), code mode (`extensions/codemode/execute.ts:339`), and two-part compaction summaries (`compaction/compaction.ts:1033`).
- **What counts** [code] `coding-agent/src/core/agent-session.ts:4134-4166`, `coding-agent/src/modes/interactive/components/footer.ts:120-139`: every assistant message (any stop reason), `usage` entries (cache warming), `toolResult.usage` (tools that call models), and `compaction` and `branch_summary` entries with `usage`. The scan is `sessionManager.getEntries()`, which is the **whole file, all branches** (`coding-agent/src/core/session-manager.ts:1520-1522`).
- **Retry attempts, tool loops, aborted runs.** Each attempt ends in `message_end`, and Pi persists it (`agent-session.ts:1115-1134`). On retry, Pi adds a `context_edit(target, null)`, which hides the attempt from the model but keeps it in raw history (`agent-session.ts:1208-1224,3737-3738`). The scan counts it. On abort or error, a provider sets `stopReason` to aborted or error and keeps the partial usage it has so far (example: `ai/src/api/anthropic-messages.ts:893-895`). [test] `pi/packages/coding-agent/test/suite/agent-session-retry-events.test.ts:371` pins that an aborted message is persisted. **No Pi test pins that failed-attempt usage is in the totals** [absence].
- **Sub-agents.** Pi core has no built-in task tool [absence: `rg -il subagent` in `coding-agent/src` found nothing]. Sub-agents are an example extension (`coding-agent/examples/extensions/subagent/index.ts`, not read). Model usage from a tool comes in through `toolResult.usage`.

### omp

- **Shape.** Pi's fields plus `contextTokens` (authoritative occupancy that is not billed), `orchestration{input, cacheRead, output}` (billed, but not in the prompt), `premiumRequests`, `reasoningTokens`, `cttl{ephemeral5m, ephemeral1h}`, `server{webSearch, webFetch}`, and `credits{cost, committedCost, acuCost}` [code] `catalog/src/types.ts:219-284`.
- **Pricing.** Tiered by prompt size, including orchestration input. Time-based peak and off-peak multipliers and dated rate cards use the request timestamp. The cost of an unattributed `cttl` remainder is computed at the 5-minute rate, so cache writes are never free [code] `catalog/src/models.ts:194-248`.
- **Load repair.** `normalizeAssistantUsage` fills missing fields with 0 for old or imported sessions [code] `coding-agent/src/session/session-loader.ts:51-90`.
- **Totals index.** `SessionEntryIndex.insert` adds usage for each entry as it arrives: assistant messages, `model_usage` entries, and `task` tool results. It also tracks `subagentCost` [code] `coding-agent/src/session/session-manager.ts:426-458,601-642`. Read cost is O(1) (`usageSnapshot`, `696-698`).
- **Failed attempts.** On a transient retry, omp removes the failed turn from active context only (`turn-recovery.ts:3290-3292,1329-1350`). The persisted entry stays on the branch. omp records it as a pending retry error and later sets `message.retryRecovery{status: recovered|superseded, attempt, supersededBy}` on that entry (`turn-recovery.ts:1038-1066,1068-1120`). For overflow recovery, `discardAssistantTurn` moves the leaf to the parent, so the entry stays in the file off-branch (`turn-recovery.ts:1462-1490`). The index counts both. When an entry is really deleted (`discardEntryDurably`), omp writes its usage again as `model_usage{purpose: "discarded-turn"}` [code] `session-manager.ts:141-142,4096-4135`. [test] `coding-agent/test/session-manager-immediate-persist.test.ts:392-427` ("keeps billed usage when durably discarding an entry").
- **Two totals with different scope.** `getUsageStatistics()` covers the whole journal and feeds the footer and ACP. `getSessionStats()` covers the **active window only** (`agent.state.messages` plus `model_usage` after the latest compaction or reset) and feeds RPC `get_session_stats` [code] `coding-agent/src/session/session-stats.ts:70-93,181-279`, `modes/rpc/rpc-mode.ts:2660-2662`, `tui/src/status-line/footer.ts:183,259`.
- **Fork.** omp: a doc comment says `forkFrom` with `resetInheritedCost` sets cost to zero and keeps tokens (`coding-agent/src/session/session-manager.ts:460-498`); I did not read `forkFrom` itself. Pi: `createBranchedSession` copies the branch entries with their usage (`pi/packages/coding-agent/src/core/session-manager.ts:1632-1660`); I did not read Pi `forkFrom` (`1815`).
- **Compaction summary usage.** `entryUsage` does not read compaction entries (`session-manager.ts:426-433`), and I found no `appendModelUsage` call on the compaction path [absence].

**omp changed vs Pi:** omp has a richer usage shape and time-aware prices, an incremental index instead of a rescan, explicit `model_usage` entries for every model call that is not a turn, and usage kept when it deletes a turn. It also has two totals with different scope, which can confuse a client. Pi has one total over all entries, and it includes compaction usage.

## 3. Persistence

- **Pi.** Usage lives in `message.usage` of each assistant `message` entry, in `UsageEntry{type: "usage", kind, provider, model, usage, note}` for usage outside the context (only cache warming today), and in the `usage` field of compaction and branch-summary entries [code] `coding-agent/src/core/session-manager.ts:80-89,1244-1258`, `coding-agent/src/core/cache-warmer.ts:342`. Totals are **always recomputed** by a scan. Nothing is stored as a total. Resume and branch switch need no rebuild step. The footer caches the scan by session, leaf, entry count, and model (`coding-agent/src/modes/interactive/components/footer.ts:108-118`).
- **omp.** Usage lives in the assistant message and in `ModelUsageEntry{type: "model_usage", purpose, role, api, provider, model, usage, stopReason, errorMessage}` [code] `session-entries.ts:113-130`. `appendModelUsage` records late usage on the branch that started the call and keeps the active leaf (`session-manager.ts:3641-3666`). Totals are recomputed on load through `index.rebuild` (`594-599`) and then kept up to date incrementally. Nothing is stored as a total.

**omp changed vs Pi:** both recompute and store no totals. omp writes a `purpose` and a `stopReason` on each off-transcript record, and it makes sure deleted turns leave a usage record behind.

## 4. Context-usage estimate and compaction trigger

### Pi

- **Context tokens from one usage.** `calculateContextTokens = totalTokens || input + output + cacheRead + cacheWrite` [code] `compaction/compaction.ts:140-142`.
- **Anchor.** The last assistant message that is not aborted, not an error, and has non-zero usage (`148-161,184-190`).
- **Estimate.** Anchor tokens plus the sum of `estimateTokens` over the messages after it. With no anchor, Pi sums all messages (`196-224`). `estimateTokens` uses chars/4, rounded up. An image counts as 4,800 chars, and a tool call counts its name plus its JSON arguments (`276-349`).
- **Stale anchor rule.** An anchor older than the latest `context_edit` or `compaction` is not used, and Pi estimates the whole projection (`227-262`). After a compaction, the footer shows `tokens: null` (`?%`) until a new assistant response arrives (`agent-session.ts:4195-4221`). [test] `pi/packages/coding-agent/test/agent-session-stats.test.ts:119,142,293`.
- **Trigger.** `shouldCompact`: `contextTokens > contextWindow - reserveTokens`. Defaults: `reserveTokens: 16384`, `keepRecentTokens: 20000` [code] `compaction.ts:126-130,267-270`. For an error or zero-usage message, Pi uses the estimate (`agent-session.ts:3006-3036`). [test] `pi/packages/coding-agent/test/suite/agent-session-compaction.test.ts:900-994`.

### omp

- **Context tokens.** `contextTokens` when the provider sends it. Otherwise `totalTokens` minus orchestration tokens [code] `agent/src/compaction/compaction.ts:265-280`.
- **Tokenizer.** Messages after the anchor are counted with a native Rust tokenizer chosen by model family (Claude, Qwen, DeepSeek, Kimi, GLM, cl100k, o200k fallback), not chars/4 [code] `agent/src/tokenizer.ts:18-42`, `agent/src/compaction/transcript-tokens.ts:157-172`. The anchor rules match Pi's, and omp also skips anchors made before a pruned tool result or a history rewrite (`transcript-tokens.ts:43-125`). [test] `agent/test/transcript-tokens.test.ts:48-119`.
- **Floor rule.** The trigger value is `max(provider usage, local estimate of the stored conversation)`. A request hook that shrinks the request (compression) therefore cannot deflate the trigger [code] `compaction.ts:384-386`, `session-maintenance.ts:2740-2768,3558-3579`.
- **Reserve.** `max(15% of window, reserveTokens ?? 16384)`. A default reserve that is impossible for a small window falls back to 15% [code] `compaction.ts:194,333-358`. [test] `agent/test/compaction-reserve-provenance.test.ts:12-77`.
- **Threshold.** An absolute `thresholdTokens` wins. Otherwise `baseWindowTokens` first clamps the window, and then `thresholdPercent` applies (default -1, which means "use window minus reserve") (`compaction.ts:224-227,399-429`).
- **Footer.** `getContextBreakdown` uses the anchored usage, corrected for removed history, plus tokenized trailing and pending messages. It splits the result into categories: system prompt, tools, skills, instruction files, messages [code] `session-stats.ts:282-394`, `tui/src/status-line/context-usage.ts:503-584`.

**omp changed vs Pi:** omp uses a real tokenizer instead of chars/4, a proportional reserve, a floor against compression hooks, and a footer split by category. Pi shows "unknown" right after a compaction. omp uses a pending snapshot instead.

## 5. Wire and UI

- **Pi events** [code] `agent-session.ts:203-216`: `compaction_start{reason: manual|threshold|overflow}`, `compaction_end{reason, result, aborted, willRetry, errorMessage?}`, `auto_retry_start{attempt, maxAttempts, delayMs, errorMessage}`, `auto_retry_end{success, attempt, finalError?}`, and `summarization_retry_*` for retries of compaction calls. JSON mode sends the cumulative `usage` on each assistant `message_update` without the full snapshot, and passes `message_end` through unchanged, so its final message carries the authoritative `usage` [code] `coding-agent/src/modes/json-event.ts:40-62`, [test] `coding-agent/test/suite/regressions/7911-json-stream-usage.test.ts:13`. RPC `get_session_stats` returns `{tokens{input, output, cacheRead, cacheWrite, total}, cost, contextUsage{tokens, contextWindow, percent}}` [code] `coding-agent/docs/rpc-commands.md:526-565`. The footer shows `↑in ↓out R W CH% $cost (sub)` and context percent with `(auto)` [code] `pi/packages/coding-agent/src/modes/interactive/components/footer.ts:157-200`.
- **omp events** [code] `coding-agent/src/session/agent-session-events.ts:44-95`: `auto_compaction_start{reason: threshold|overflow|idle|incomplete, action}`, `auto_compaction_end{action, result, aborted, willRetry, errorMessage?, skipped?}`, `auto_retry_start{..., errorId?}`, `auto_retry_end{..., retryErrors?}`, and `retry_fallback_applied` and `retry_fallback_succeeded`. omp ACP mode sends `usage_update{size: contextWindow, used: contextUsage.tokens, cost{amount, currency: "USD"}}` at the end of each turn, and a per-turn `PromptResponse.usage` computed as the difference between the `getUsageStatistics()` values before and after the turn [code] `coding-agent/src/modes/acp/acp-agent.ts:1285,1614,2882-2897,2961-2984`.

**omp changed vs Pi:** omp renames the compaction events (`auto_compaction_*`), adds an `action` and the reasons `idle` and `incomplete`, and puts the classifier `errorId` on retry events. The ACP `usage_update` and the before-and-after turn usage map directly onto AskCore's ACP surface.

## 6. Tests that pin behavior

| Behavior | Pi | omp |
|---|---|---|
| Overflow regexes, throttling veto, Cerebras | `ai/test/overflow.test.ts:32-120` | `ai/test/overflow-utils.test.ts:27-117` |
| Silent and length overflow, recoverable length | `overflow.test.ts:154-193` | `overflow-utils.test.ts:62-71` (usage-backed, `contextTokens`) |
| 429 status veto | (none) | `overflow-utils.test.ts:82-96` |
| 413 payload split | (none) | `agent-session-payload-rejection-413.test.ts:428-785` |
| Live provider overflow | `ai/test/context-overflow.test.ts:97-189` | `ai/test/context-overflow.test.ts` |
| Retry classification and loop | `ai/test/retry.test.ts:18-262`, `coding-agent/test/suite/agent-session-retry-events.test.ts:33-371` | `coding-agent/test/turn-recovery-replay-unsafe.test.ts` (grep hit for `isContextOverflow`; test names not read) |
| One compact-and-retry, stale usage | `coding-agent/test/suite/agent-session-compaction.test.ts:442-994` | (no budget test found) |
| Totals: summaries, cache warming, tools | `coding-agent/test/agent-session-stats.test.ts:101-293` | `coding-agent/test/agent-session-stats.test.ts:72-515` |
| Deleted-turn usage kept | (not applicable) | `coding-agent/test/session-manager-immediate-persist.test.ts:392-427` |
| Anchor plus tail estimate, reserve | (covered by the stats tests) | `agent/test/transcript-tokens.test.ts`, `compaction-reserve-provenance.test.ts` |

## 7. Trade-offs and weaknesses

| Dimension | Pi | omp |
|---|---|---|
| Overflow precision | Text only. A 429 worded as overflow is blocked only by three veto phrases. A 413 byte rejection compacts with no benefit. | Status-aware and splits 413. Much more code and many issue-specific branches. |
| Retry loop safety | One overflow attempt, reset for each prompt. Simple and visible. | No explicit overflow counter found. It depends on compaction making progress. Default `maxRetries 10` and a 5-minute delay cap are long for a headless run. |
| Usage completeness | Failed attempts count because they stay persisted. Totals include abandoned branches, which is billing-correct but can surprise a user. A fork inherits the parent's cost. | Same, plus deleted-turn records. Two totals with different scope (RPC versus footer). Compaction summary usage may be missing. |
| Estimate accuracy | chars/4 overestimates text and is inaccurate for code and CJK. No floor against compression hooks. | Native tokenizer for each family (needs Rust natives), floor rule, proportional reserve. |
| Cost | Rescan of O(entries) on each footer render, with a cache. Float64 USD. | O(1) index. Float64 USD, time-based prices. |
| Model switch | Skips the overflow check for a message from another model. | Same, plus recovery when a promotion lands mid-flight. |

## 8. Portable ideas for AskCore (ranked)

I checked only that AskCore already has `protocol.Usage` with cache fields (`pkg/protocol/usage.go:8-25`), an `AutoRetryStart` event (`pkg/protocol/events.go:63-64`), and usage summation in ACP (`internal/acp/ask_methods.go:528-533`). I did not study more.

1. **H-RETRY-06: one overflow classifier with Pi's regex list plus omp's 429/402 status veto. Check overflow before the retry classifier. Allow one compact-and-retry attempt, reset on each user prompt.** Cost: low, one Go file and a table test that ports `overflow.test.ts`. Add the silent check (`input + cacheRead (+ cacheWrite) > window`) only when the window is known. Defer the 413 split until a provider shows the problem.
2. **H-RETRY-07/08: keep usage on each persisted assistant record and keep failed attempts, hidden from the model but not deleted. Compute totals by a scan over all session records.** Cost: low if the JSONL session log already keeps error turns. Add the test that Pi lacks: a transient failure followed by success gives totals equal to the sum of both attempts.
3. **H-RETRY-09: estimate = last valid assistant usage + chars/4 of the trailing messages. Never use an anchor older than the latest compaction or edit. Trigger at `window - 16384`.** Cost: low. omp's floor rule and tokenizer are an optional later step. Its 15% proportional reserve is a cheap protection for small windows.
4. **H-RETRY-10: a separate usage record type (`purpose`, provider, model, usage, stopReason) for model calls outside the transcript (compaction, summaries, classifiers), plus a "discarded" record when a turn is deleted.** Cost: medium, because it changes the session log schema. Use one total scope everywhere, so the omp RPC-versus-footer split does not repeat.
5. **ACP surface: send `usage_update{size, used, cost}` at the end of each turn, with turn usage as the before-and-after difference of the totals (omp).** Cost: low once item 2 exists.

## Limitations

- I did not run any test, so the [test] labels mean the test exists and has that name, not that it passes at these commits.
- I did not read the Pi `durable` package (`packages/durable/src/harness/usage.ts`, `generation.ts:457`). It is a separate harness that also calls `isContextOverflow`.
- I sampled omp's very large recovery files (`session-maintenance.ts`, `turn-recovery.ts`, `agent-session.ts`, each over 3,000 lines). I did not read them in full.
- Price data is generated or remote in both repos. I did not check any actual prices.
- I did not use GitNexus or DeepWiki. All evidence is from the local checkouts.

## Unresolved questions

1. Does omp cap overflow compact-and-retry cycles when each compaction makes some progress but the retry still overflows? I found only progress-based guards, no counter.
2. Where, if anywhere, does omp record the usage of a compaction or summary model call? `entryUsage` ignores compaction entries, and I found no `appendModelUsage` on that path.
3. Should AskCore count abandoned branches in session totals (Pi and omp, billing view) or only the current branch (user view)? This is a product decision.
4. Should a forked AskCore session inherit the parent's cost (Pi) or reset it (omp `resetInheritedCost`)?
5. Should AskCore add omp's credential rotation or quota-reset wait for a `UsageLimit` in H9, or surface the error as Pi does? H7a already says not to retry a spent allowance silently.
