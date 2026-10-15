# H4 research: how Pi implements `openai-responses`

Scope: H-PROV-04. Pi checkout `4c6fb7cfe` (v1.0.1). Roadmap pin `2bbfcca4`.
Paths below are relative to `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/`.
Abbreviations: `R` = `src/api/openai-responses.ts`, `S` = `src/api/openai-responses-shared.ts`, `T` = `src/api/transform-messages.ts`, `AZ` = `src/api/azure-openai-responses.ts`, `CX` = `src/api/openai-codex-responses.ts`.

Method note: the GitNexus MCP tools were not available in this session (ToolSearch disabled). All claims come from direct file reads, `git log`, and `git show`. No call-graph tool output is used. The call graph is small and was traced by hand (section 1.1).

## 0. Key findings

1. Pi stores OpenAI reasoning as the full `ResponseReasoningItem` JSON in `thinkingSignature` (S:692) and replays it unchanged (S:264-268). The request always has `store:false`, so `include:["reasoning.encrypted_content"]` is what makes replay work (R:337, R:372).
2. The tool-call id is the composite `call_id|item_id` (S:489). The item id is dropped on replay when the model differs or the prefix is wrong (S:302-305). This is the root of most replay bugs.
3. Cross-provider replay (for example Anthropic to OpenAI): thinking becomes plain text (T:113-116), tool item ids become `fc_<shortHash>` (S:160-163, S:171), and text items get synthetic `msg_pi_N` ids (S:272-273).
4. There is no explicit "missing `output_index`" error. The inventory wording is imprecise. Pi throws "completed with an unfinished tool call" when a tool call never got `output_item.done` (S:766-775).
5. `truncation`, `parallel_tool_calls`, and `text.verbosity` are NOT sent by `openai-responses`. They exist only in Codex (CX:98, CX:559, CX:563). Grep confirmed.
6. Since the pin, only one source change touches these files: `bc2d8dc1c` (13 lines in S). It is a replay fix.

## 1. Implementation walk-through

### 1.1 Call graph (hand-traced)

| Step | Where |
|---|---|
| `streamSimple` clamps reasoning level, calls `stream` | R:238-256 |
| `stream` builds client, params, retries request | R:127-190 |
| `buildParams` calls `convertResponsesMessages` and `convertResponsesTools` | R:302-385 |
| `convertResponsesMessages` calls `transformMessages` with `normalizeToolCallId` | S:145-179 |
| `processResponsesStream` consumes events, builds `AssistantMessage` | S:433-777 |
| `processResponsesStream` calls `mapStopReason`, `calculateCost`, service tier hook | S:779-809, S:577-583 |

### 1.2 Request params

| Param | Value and rule | Ref |
|---|---|---|
| `store` | Always `false` | R:337 |
| `stream` | `true` | R:333 |
| `include` | `["reasoning.encrypted_content"]` when effort or summary is set. For `xai` it is set whenever the model has `reasoning`, even with no effort | R:372, R:378 |
| `reasoning.effort` | `model.thinkingLevelMap[level] ?? level`. Default `"medium"` if only summary is given | R:365-367 |
| `reasoning.summary` | `options.reasoningSummary \|\| "auto"` | R:370 |
| Thinking off | Sends `reasoning:{effort: thinkingLevelMap.off ?? "none"}`. Skipped for `github-copilot` and when `thinkingLevelMap.off === null` | R:373-377 |
| `streamSimple` reasoning | `clampThinkingLevel`; `"off"` becomes undefined | R:249-250 |
| `prompt_cache_key` | `clampOpenAIPromptCacheKey(sessionId)`. Truncated to 64 code points. Undefined if `cacheRetention === "none"` | R:334, `openai-prompt-cache.ts:1-8` |
| `prompt_cache_retention` | `"24h"` only if retention is `long` AND `supportsLongCacheRetention` (default true) AND NOT `supportsExplicitPromptCacheMode` | R:97-104 |
| `prompt_cache_options` | Only if `supportsExplicitPromptCacheMode` (default false). `none` gives `{mode:"explicit"}`. `long` gives `{ttl:"30m"}` (GPT-5.6+) | R:106-114 |
| Retention resolution | Option, else env `PI_CACHE_RETENTION=long`, else `"short"` | R:72-80 |
| `max_output_tokens` | `max(maxTokens, 16)`. Skipped if `supportsMaxOutputTokens=false` (default true) | R:340-342, R:33 |
| `temperature` | Passed when defined | R:344-346 |
| `service_tier` | Passed when `options.serviceTier` is defined | R:348-350 |
| `tools` | Omitted when there are no request tools | R:352-357 |
| `tool_choice` | Passed through (`options.toolChoice`) | R:359-361 |
| Custom sampling keys | `Object.assign(params, model.samplingParams, options.samplingParams)` last. They can override any named field | R:382 |
| `onPayload` hook | May replace the whole params object | R:174-177 |
| ChatGPT sign-in | If provider `openai`, base URL `https://api.openai.com/v1`, and key not starting `sk-`: omit `max_output_tokens`, `temperature`, `prompt_cache_retention`, `prompt_cache_options` | R:40-47, R:329-346 |
| Not sent | `truncation`, `parallel_tool_calls`, `text.verbosity` | grep, none in R/S/AZ |

### 1.3 Headers (session affinity)

| Rule | Ref |
|---|---|
| Default `User-Agent` is Pi's; model headers merge; option headers win last | R:267, R:289-291 |
| `sessionAffinityFormat` auto: `openrouter` if provider or URL says openrouter, else `openai` | R:64-66 |
| `openrouter` format: `x-session-id` | R:278-279 |
| `openai` format: `session_id` AND `x-client-request-id` | R:281-284 |
| `openai-nosession` format: `x-client-request-id` only | R:284 |
| No session headers when `cacheRetention === "none"` | R:159 |
| Key must exist, or an `authorization` or `cf-aig-authorization` header; else the string `"unused"` | R:58-62 |

### 1.4 `convertResponsesMessages` (S:145-354)

Pipeline: `resolveTranscript` (fold or keep mid-conversation system messages), then `transformMessages` with a custom id normalizer, then one pass that emits input items.

| Source message | Output item | Ref |
|---|---|---|
| Leading system | `{role: "developer" or "system", content}`. `developer` if `model.reasoning` and `supportsDeveloperRole !== false` | S:214-215, S:226 |
| Later system | Same role; text from `renderSystemMessageUpdate`; tool additions emitted as `additional_tools` or `tool_search_call` + `tool_search_output` | S:184-212, S:222-226 |
| User string | `{role:"user", content:[{input_text}]}` | S:230-234 |
| User blocks | `input_text` / `input_image {detail:"auto", data URL}`. Empty content is skipped | S:236-253 |
| Assistant `thinking` with `thinkingSignature` | The parsed JSON item is pushed as is (`rs_...` id, `encrypted_content`, `summary`) | S:264-268 |
| Assistant `thinking` without signature | Nothing is emitted. (`transformMessages` already turned it to text for foreign models, T:111-116) | S:265 |
| Assistant `text` | `{type:"message", role:"assistant", status:"completed", id, phase, content:[output_text, annotations:[]]}` | S:282-289 |
| Assistant `toolCall` | `function_call {id?, call_id, name, arguments: JSON string, namespace?}` or `custom_tool_call {id?, call_id, name, input}` for grammar tools | S:307-327 |
| `toolResult` | `function_call_output {call_id, output}` or `custom_tool_call_output` | S:333-348 |
| Tool result output | Text joined with `\n`; images only when the model accepts images; empty gives `"(no tool output)"`; image only without vision gives `"(see attached image)"` | S:81-108 |
| Assistant with no output | Skipped entirely | S:330 |

Signature storage:

| Field | Format | Ref |
|---|---|---|
| `thinkingSignature` | `JSON.stringify(ResponseReasoningItem)` (whole item, including `id`, `encrypted_content`, `summary`) | S:692 |
| `textSignature` | `{"v":1,"id":"msg_...","phase"?:"commentary"\|"final_answer"}`. Legacy plain string = id only | S:53-77, S:703 |
| Text id over 64 chars | Replaced by `msg_<shortHash>` | S:279-281 |
| Text with no signature | `msg_pi_<msgIndex>` or `msg_pi_<msgIndex>_<n>` | S:272-273 |

Same-model versus cross-model branches:

| Case | Rule | Ref |
|---|---|---|
| `isSameModel` (provider + api + model id) | Blocks kept as is. Tool-call ids not re-normalized. Namespace kept | T:95-98, T:109, T:120, T:136, S:316 |
| `isDifferentModel` (same provider and api, other model id) | Reasoning items and message ids still replayed. Function-call `id` set to undefined (avoids `fc_` to `rs_` pairing validation). Namespace dropped | S:259-260, S:303-305 |
| Foreign provider or api | `transformMessages` converts thinking to plain text and text to plain text (signatures lost). Tool ids run through the normalizer | T:113-125, T:136-142 |
| Redacted thinking | Dropped unless same model | T:104-106 |
| Errored or aborted assistant message | Skipped entirely (no partial reasoning replay) | T:195-203 |
| Orphan tool calls | Synthetic error result `"No result provided"` | T:167-186, T:231-232 |

Tool-call id composite and normalization (S:154-177):

| Rule | Ref |
|---|---|
| Stream builds id as `${call_id}\|${item.id}` | S:489, S:510 |
| `normalizeIdPart`: replace `[^a-zA-Z0-9_-]` with `_`, cut to 64, strip trailing `_` | S:154-158 |
| If provider is not in the allowed set (`openai`, `openai-codex`, `opencode`; Azure adds `azure-openai-responses`), id is only normalized as one part | S:166, R:31, AZ:26 |
| Id without `\|`: normalized as one part | S:167 |
| Foreign source (other provider or api): item id becomes `fc_<shortHash(itemId)>` | S:160-163, S:170-171 |
| Same source: item id normalized; forced to start with `fc_` | S:171-175 |
| Replay: `call_id` from the part before `\|`; `id` from the part after, then dropped if model differs or prefix mismatch (`fc_` for function calls, `ctc_` for custom calls) | S:292-305 |
| Tool results use only the part before `\|` | S:333 |
| Because normalization runs only for non-same-model blocks, `toolCallIdMap` rewrites the matching `toolResult` ids | T:69, T:83-90, T:136-142 |

### 1.5 `convertResponsesTools` (S:360-397)

| Rule | Ref |
|---|---|
| Grammar tool becomes `{type:"custom", format:{type:"grammar", syntax, definition}}` | S:366-379 |
| Otherwise `{type:"function", name, description, parameters}` | S:383-391 |
| `strict` set only if `supportsStrictMode`. OpenAI default false; Azure default true | S:392-394, R:82-95, AZ:297 |
| `defer_loading:true` for tool-search results | S:377, S:390 |

### 1.6 Event parsing (`processResponsesStream`, S:433-777)

State: `outputSlots: Map<output_index, slot>` (S:441), `reasoningBlocksById` (S:442). Slots are keyed by `output_index`. Slot type is checked on each lookup (S:448-454).

| Event | Handling | Ref |
|---|---|---|
| `response.created` | Save `responseId` | S:601-602 |
| `output_item.added` | `createSlot` for `reasoning`, `message`, `function_call`, `custom_tool_call`; emits `*_start` | S:603-604, S:464-530 |
| `reasoning_summary_text.delta` and `reasoning_text.delta` | Append to thinking, emit `thinking_delta` | S:605-634 |
| `reasoning_summary_part.done` | Append `"\n\n"` | S:615-624 |
| `output_text.delta` and `refusal.delta` | Append to text | S:635-654 |
| `function_call_arguments.delta` | Append `partialJson`, parse with `parseStreamingJson` | S:655-660 |
| `function_call_arguments.done` | Replace buffer; emit tail delta only if the new string extends the old | S:661-671 |
| `custom_tool_call_input.delta/.done` | Grammar tool input buffer | S:672-682 |
| `output_item.done` reasoning | Thinking text = joined `summary` (or `content`); `thinkingSignature = JSON.stringify(item)`; delete slot | S:688-700 |
| `output_item.done` message | Text rebuilt from item content; `textSignature` with id and phase | S:701-710 |
| `output_item.done` function_call | Final args parsed; namespace set; `partialJson` deleted; `toolcall_end` | S:711-727 |
| `output_item.done` custom_tool_call | Same with `customInput` deleted | S:728-742 |
| `response.completed` or `incomplete` | `finalizeResponse` | S:743-744 |
| `error` | Throw `Error Code {code}: {message}` | S:745-746 |
| `response.failed` | Throw `code: message`, or `incomplete: reason`, or a fallback | S:747-757 |
| Stream ends with no terminal event | Throw "ended before a terminal response event" | S:760-762 |
| Unknown or missing `output_index` | `getSlot` returns undefined and deltas are skipped (`continue`). No error here | S:448-454, S:606-607 |
| Completed with `toolUse` and a tool call still has `partialJson` or `customInput` | Throw "completed with an unfinished tool call: name (id)" | S:763-776 |
| Phase `final_answer` on a message item | Sets provisional `stopReason="stop"` (terminal event can replace it) | S:443-447, S:478 |

Reasoning backfill (S:534-551): Azure may omit `encrypted_content` on `output_item.done`. `finalizeResponse` copies it from `response.completed.response.output` into the stored signature, matched by reasoning item id (S:556).

### 1.7 Usage, cost, stop reason

| Item | Rule | Ref |
|---|---|---|
| `input` | `input_tokens - cached_tokens - cache_write_tokens` (min 0). OpenAI counts cached in input | S:564-568 |
| `cacheRead` / `cacheWrite` | `input_tokens_details.cached_tokens` / `cache_write_tokens` | S:561-571 |
| `reasoning` | `output_tokens_details.reasoning_tokens` (subset of `output`) | S:572 |
| `totalTokens` | `usage.total_tokens` | S:573 |
| Cost | `calculateCost(model, usage)`, then service-tier multiplier | S:577-583 |
| Tier used | `response.service_tier ?? options.serviceTier` (response wins) | S:581 |
| Multipliers | `flex` 0.5; `priority` or `fast` 2 (2.5 for model `gpt-5.5`); else 1. Applies to input, output, cacheRead, cacheWrite; total recomputed | R:387-415 |
| Stop map | `completed` to `stop`; `incomplete`+`max_output_tokens` to `length`; other `incomplete` to `error` with message; `failed`/`cancelled` to `error`; `in_progress`/`queued`/missing to `stop` | S:779-808 |
| Raw reason | `rawStopReason = status` or `status.incompleteReason` | S:586-589 |
| Tool use | If any toolCall block and reason is `stop`, set `toolUse` | S:594-596 |
| Abort | Signal aborted throws "Request was aborted"; catch sets `stopReason: "aborted"` | R:201-203, R:221 |
| Errors | Catch strips scratch fields (`index`, `partialJson`, `customInput`), sets `errorMessage`, emits `error` event | R:214-232 |
| ChatGPT limit | Message containing `subscription_sharing_usage_limit_exceeded` gets a usage URL appended | R:227-229 |
| Retry | `retryProviderRequest` wraps request; SDK `maxRetries:0` | R:178-190 |

## 2. The "5 replay bugs"

The inventory number "5 times" is a roadmap summary. The git log shows at least eight replay-related fixes in these files. The five that shaped the current design are in the first table. Commit messages are short; lessons are my reading of the diffs.

| # | Commit, date | Bug | Fix | Lesson for Go port |
|---|---|---|---|---|
| 1 | `d327b9c76` 2026-01-22 (#886) | Switch between two OpenAI models: `fc_` ids trigger pairing validation errors, because OpenAI pairs `fc_` ids with `rs_` reasoning items | Omit `id` on function_call from a different model; keep `call_id` | Replay validation is per model. Same-model replay needs ids; other-model replay must drop them (S:303) |
| 2 | `b2548ce48` 2026-03-18 (#2328), `b21b42d03` 2026-03-22 | Raw ids from other providers (450+ chars, special chars, trailing `_`) rejected | Normalize to `[A-Za-z0-9_-]`, 64 max, strip trailing `_`; foreign item ids hash to `fc_<hash>` | Hash foreign ids. Truncation alone can collide and breaks pairing |
| 3 | `8fc2b7682` 2026-03-05, reverted by `b4e7d5c44` 2026-03-06 (#1878) | Dropping empty-text thinking blocks removed signed reasoning (empty summary, encrypted content only) and broke replay | Keep a thinking block if it has a signature, even when text is empty (T:107-109) | Replay key is the signature, not the visible text. Never filter by text length when a signature exists |
| 4 | `d1fb34bc8` 2026-05-28 (#5148), `3f1ce9b6e` | Anthropic-to-Codex switch: duplicate fallback message item ids for converted blocks, and an invalid id prefix | Unique `msg_pi_<msg>_<n>` ids | Message items need unique valid ids even when the source has none |
| 5 | `8c9dbffa3` 2026-06-25 (#6009) | Output items finishing out of order lost reasoning replay state (single "current item" tracking) | `outputSlots` keyed by `output_index`; reasoning stored on `output_item.done` | Track items by `output_index`, not by a single cursor |
| 6 | `bc2d8dc1c` 2026-10-01 (after pin; radius#115) | Foreign ids are normalized to `fc_*`, but grammar tools replay as `custom_tool_call`, which needs `ctc_*`. Error: `Expected an ID that begins with 'ctc'` | Drop `id` unless it matches the replayed type prefix; always drop for different model (S:302-305) | Item id prefix must match item type. A call can switch between function and custom type when grammar support differs |

Related fixes (same family, lower rank):

| Commit | Lesson |
|---|---|
| `1f0dbc008` 2026-07-13 (#6608) | Azure omits `encrypted_content` on `output_item.done`; backfill from `response.completed` (S:538-551) |
| `d43930c81` 2026-01-18 (#768), removed later (`2d27a2c72`) | Azure needs strict reasoning-to-item pairing; fixed more generally by skipping errored or aborted turns (T:195-203) |
| `2d27a2c72` 2026-01-19, `0f3a0f78b` | Never replay errored or aborted assistant messages (reasoning without following item gives 400) |
| `a23fab469` 2026-04-22 (#3555) | Synthesize results for trailing unresolved tool calls |
| `1b2aa0ca0` 2026-09-28 (#9974) | Servers that omit `output_index` (llama.cpp) merged parallel calls. Now refuse unfinished calls (S:763-776) |
| `bab58f821` 2026-03-24 (#2567) | Copilot rejects a default `reasoning` object; skip when thinking is off (R:373) |
| `cd95c2749`, `e5ef8d065`, `32850ef7c` | Require terminal event; keep raw status; only `max_output_tokens` means `length` |

Teaching summary for H4: the three rules that matter most are (a) store reasoning as the opaque full item and replay it only for the same model, (b) strip or rewrite every item id on cross-model replay, (c) never replay errored or aborted turns.

## 3. Providers using `openai-responses`

Registered through `openAIResponsesApi()` (lazy wrapper `src/api/openai-responses.lazy.ts`).

| Provider | File | Differences |
|---|---|---|
| `openai` | `src/providers/openai.ts` | Reference behavior. Allowed tool-call provider. ChatGPT sign-in path (R:40-47) |
| `xai` | `src/providers/xai.ts`, base `https://api.x.ai/v1` | `include:["reasoning.encrypted_content"]` always for reasoning models (R:378). Test: no `reasoning` field without effort, no `prompt_cache_retention` (test `xai-responses.test.ts:164-215`). xhigh effort accepted. Bearer auth or device OAuth |
| `meta` | `src/providers/meta.ts` | Plain Responses provider |
| `github-copilot` | `src/providers/github-copilot.ts:31` | Per-model api choice (Anthropic, Completions, Responses for GPT). Dynamic headers: `X-Initiator` (`agent` if last message not user), `Openai-Intent: conversation-edits`, `Copilot-Vision-Request: true` when images exist (`src/api/github-copilot-headers.ts:5-37`, R:268-275). No default `reasoning` when thinking off (R:373). NOT in allowed tool-call set, so ids use the plain one-part normalizer (S:166) |
| `opencode`, `opencode-go` | `src/providers/opencode*.ts` | Wrapped with `withOpenCodeSessionHeader`; `session-id` header omitted (changelog 0.8x #6645). Allowed tool-call provider |
| `cloudflare-ai-gateway` | `src/providers/cloudflare-ai-gateway.ts:24` | Wrapped with `cloudflareStreams`; strict mode set explicitly (test `openai-responses-compat.test.ts:157`) |
| OpenRouter and other proxies | via compat | `x-session-id` header when URL or provider matches (R:64-66) |
| `azure-openai-responses` | `AZ` | Separate adapter, shared convert and stream code (below) |
| `openai-codex` | `CX` | Separate adapter, shared code (below) |

Azure differences (AZ):

| Item | Azure | Ref |
|---|---|---|
| Client | `AzureOpenAI`, deployment name map, api-version `v1` | AZ:26, AZ:36, AZ:207 |
| Allowed tool-call providers | Adds `azure-openai-responses` | AZ:26 |
| `prompt_cache_key` | Sent; no `prompt_cache_retention` or `prompt_cache_options` | AZ:308 |
| `service_tier` | Not sent; no tier pricing hook | AZ:133-136 |
| `strict` default | true | AZ:297 |
| Thinking off with `reasoning` | Always sends off-effort (no Copilot check) | AZ:341 |
| Session headers | None (no cache-affinity headers) | AZ (no such code) |
| Needs backfill of `encrypted_content` | Yes, why S:538 exists | S:534-537 |

Codex (`CX`, out of H4 scope) uses `convertResponsesMessages` (CX:542, CX:1566) and `processResponsesStream` (CX:668, CX:1543) from `S`. Differences: `store:false`, always `include` encrypted reasoning (CX:555-560), `text.verbosity` default `low` (CX:559), `parallel_tool_calls:true` (CX:563), `prompt_cache_key` without clamp shown at CX:561, WebSocket and SSE transports, `session-id` header, `websocket-cached` mode sends only new items (CX:1566-1580), own service tier resolver (CX:604-640), `includeSystemPrompt:false` (CX:543).

Shared-code risk: any change to `S` (id handling, slot tracking, stop reason, usage) changes Codex and Azure at the same time. Codex tests (`openai-codex-stream.test.ts`) and the Azure replay test cover `S`. If Ask later adds Codex, the Go converter must keep the same options surface (`includeSystemPrompt`, allowed-provider set, grammar map).

## 4. Pi tests (`test/`)

| File | Asserts |
|---|---|
| `openai-responses-compat.test.ts` | Reasoning omitted when not requested (:75); required tool choice (:110); strict mode (:157); cache headers per format, including openrouter, nosession, override, and none (:292-473); `prompt_cache_key` clamp to 64 (:299); `max_output_tokens` default and off (:537-574); service tier multiplier table, `priority` becomes `fast` (:484-522) |
| `cache-retention.test.ts` | `prompt_cache_key` undefined for `none`; `prompt_cache_retention`/`prompt_cache_options` per retention and compat (:400-457) |
| `openai-responses-chatgpt-sign-in.test.ts` | Sign-in omits `max_output_tokens`, `temperature`, retention, options; `sk-` and gateway keys keep them |
| `openai-responses-terminal-event.test.ts` | Missing terminal event rejects (:244); unfinished tool call rejects (:254); parallel calls without `output_index` reject (:268); provider event forwarding order (:281); provisional `final_answer` stop replaced by `length` (:355); completed to `stop`; incomplete to `length`; content filter and unknown incomplete reasons to non-retryable errors; `response.failed` error (:377-449) |
| `openai-responses-namespace.test.ts` | Namespace round trip on function and custom calls; dropped when target cannot replay; absent error message omitted |
| `openai-responses-partial-json-cleanup.test.ts` | `partialJson` removed at `output_item.done` |
| `openai-responses-message-id.test.ts` | Unique fallback ids for multiple text blocks |
| `openai-responses-foreign-toolcall-id.test.ts` | Copilot raw id becomes `fc_<shortHash>`, max 64, `^fc_[A-Za-z0-9]+$` |
| `constrained-sampling.test.ts` (+34 lines, `bc2d8dc1c`) | Foreign `ctc_` id dropped on `custom_tool_call` replay |
| `openai-responses-empty-tool-result.test.ts` | `"(no tool output)"` placeholder |
| `openai-responses-tool-result-images.test.ts` | Images in `function_call_output` (E2E, needs keys) |
| `openai-responses-usage-limit.test.ts` | ChatGPT usage URL appended on rejection and on stream failure |
| `azure-openai-responses-reasoning-replay.test.ts` | Keep existing `encrypted_content`; backfill when missing |
| `xai-responses.test.ts` | `/responses` URL, bearer, `store:false`, `prompt_cache_key`, no retention, `include` encrypted without effort, xhigh, UA header |
| `openai-responses-reasoning-replay-e2e.test.ts` | E2E (live keys): skip reasoning-only history after abort (:19); same-provider different-model handoff with tools (:83); Anthropic to Codex handoff (:183) |
| `openai-responses-cache-affinity-e2e.test.ts` | E2E aligned cache-affinity ids |
| `transform-messages-copilot-openai-to-anthropic.test.ts` | Copilot OpenAI to Anthropic id and thinking conversion |

Gap: the replay tests that decide H4 exit are E2E and need live keys. The Go port needs recorded-fixture versions of the three E2E replay cases.

## 5. Changes between `2bbfcca4` and `4c6fb7cfe`

| File | Change |
|---|---|
| `S` | One commit, `bc2d8dc1c` (+6, -7 lines at S:296-305): id prefix must match item type; always drop for different model |
| `R`, `AZ`, `CX`, `T`, `openai-prompt-cache.ts`, `github-copilot-headers.ts` | No diff (git log empty for `2bbfcca4..HEAD`) |
| `test/constrained-sampling.test.ts` | +34 (the new test) |
| Other test diffs | Unrelated to Responses (Anthropic federation, Bedrock, Together, model data) except small edits in `stream.test.ts` and `transcript-tool-changes.test.ts` (not read in detail) |

Roadmap impact: pinning `2bbfcca4` misses the fix for custom (grammar) tool call replay. If Ask does not ship grammar tools in H4, the impact is small. Port the fixed rule anyway (one line).

## 6. Edge cases a Go port must keep

P0 means needed for the H4 exit: one in-memory conversation switches between Anthropic and OpenAI with correct thinking replay. Fantasy v0.45.2 behavior was not audited here (see unresolved questions).

| # | Edge case | Pri | Ref |
|---|---|---|---|
| 1 | `store:false` and `include:["reasoning.encrypted_content"]` whenever thinking is on | P0 | R:337, R:372 |
| 2 | Persist the whole reasoning item (id, summary, encrypted_content) as an opaque string on the thinking block; replay for the same model | P0 | S:692, S:264-268 |
| 3 | Keep thinking blocks with signature even when the text is empty | P0 | T:107-109 |
| 4 | Cross-model: thinking to plain text; text to plain text; drop signatures | P0 | T:113-125 |
| 5 | Anthropic to OpenAI: tool ids to `call|fc_<hash>`; tool results re-keyed through the same map | P0 | S:160-177, T:83-90 |
| 6 | OpenAI to Anthropic: `call|item` ids normalized to `[A-Za-z0-9_-]{1,64}` | P0 | T:60-63, S:154-158 (Anthropic side is H3) |
| 7 | Same provider, different model: replay reasoning and message ids, drop function_call `id` | P0 | S:303 |
| 8 | Message items: id from `textSignature`, fallback `msg_pi_N[_k]`, hash if over 64; keep `phase` | P0 | S:272-289 |
| 9 | Skip errored and aborted assistant messages on replay | P0 | T:201-203 |
| 10 | Synthetic `"No result provided"` for orphan tool calls | P0 | T:167-186 |
| 11 | Tool-call id composite `call_id|item_id`; results use `call_id` only | P0 | S:489, S:292, S:333 |
| 12 | Event handling by `output_index`; finalize reasoning and tool calls on `output_item.done` | P0 | S:441, S:683-742 |
| 13 | Terminal event required; `response.failed`, `error` become errors; stream end without terminal is an error | P0 | S:745-762 |
| 14 | Reject unfinished tool calls at completion (missing `output_index`) | P0 | S:763-776 |
| 15 | Stop map including `incomplete` + `max_output_tokens` to length; tool calls force `toolUse` | P0 | S:779-808, S:594 |
| 16 | Usage: subtract cached and cache-write from input; reasoning token subset | P0 | S:564-572 |
| 17 | Reasoning effort and summary params; off gives `effort:"none"` unless the model map says null | P0 | R:363-379 |
| 18 | `developer` role for reasoning models, `system` otherwise | P0 | S:214-215 |
| 19 | `prompt_cache_key` from session id, clamp 64 code points; `none` removes it | later (H4 spec row, not exit) | R:334, `openai-prompt-cache.ts` |
| 20 | `prompt_cache_retention:"24h"` for `long`; `prompt_cache_options` ttl 30m for GPT-5.6+ | later | R:97-114 |
| 21 | Session affinity headers (`session_id`, `x-client-request-id`, `x-session-id`) | later | R:277-286 |
| 22 | Service tier request field and cost multiplier (0.5, 2, 2.5 for gpt-5.5); response tier wins | later | R:387-415, S:581 |
| 23 | `max_output_tokens` floor of 16; skip when compat flag off | later | R:340-342 |
| 24 | Backfill `encrypted_content` from `response.completed` (Azure) | later (needed when Azure ships) | S:538-551 |
| 25 | xAI: always include encrypted reasoning | later (when xAI ships) | R:378 |
| 26 | Copilot headers, no default reasoning when off | later | R:268-275, R:373 |
| 27 | ChatGPT sign-in field omission and usage-limit message | later | R:40-47, R:227 |
| 28 | Grammar (custom) tool replay: `ctc_` id rule | later (when grammar tools ship) | S:302-305, S:307-317 |
| 29 | Namespaces, `additional_tools`, `tool_search_*` | later | S:184-212, S:316 |
| 30 | Tool result images and placeholders (`"(no tool output)"`, `"(see attached image)"`) | P0 for text placeholder; image path later | S:81-108 |
| 31 | Refusal deltas appended as text; `reasoning_text.delta` (LM Studio) | later | S:625-654 |
| 32 | `"\n\n"` between reasoning summary parts | later | S:615-624 |
| 33 | Surrogate sanitization of all text | later | S:98, S:226 |
| 34 | Provider error body surfaced with HTTP status prefix (needed for retry classifier) | later | R:222-225 |
| 35 | Do not send `tools: []`; omit when empty | later | R:352 |
| 36 | `samplingParams` override last | later | R:382 |

## Unresolved questions

1. Does `charm.land/fantasy` v0.45.2 expose the full reasoning item (id, summary, encrypted_content) and let the caller pass back message-item ids, `phase`, and per-call `fc_` ids? Not checked here. If it does not, edge cases 2, 7, 8 cannot be met through fantasy alone.
2. Does fantasy send `store:false` and `include` by default, and does it surface `prompt_cache_key`, `prompt_cache_retention`, `service_tier`? Not checked.
3. The inventory row says "errors when `output_index` is missing". Pi has no such explicit check. Should the Go spec use the real rule (unfinished tool call at completion)? Recommended: yes.
4. Which "5 times" does the roadmap count? The table in section 2 gives six candidates with the first five before the pin. The exact source of "E§3 #2" was not found in the plan folder.
5. Pin choice: keep `2bbfcca4` or move to `4c6fb7cfe` for the custom tool replay fix? Only one source change differs. Recommended: port the post-pin rule.
6. `stream.test.ts` and `transcript-tool-changes.test.ts` diffs between the pins were not read. They may touch mid-conversation system messages.
7. `PI_CACHE_RETENTION` env and `supportsExplicitPromptCacheMode` model data (which models have it) were not traced to `models.generated.ts`.

Status: DONE_WITH_CONCERNS
Summary: Full behavior map of Pi `openai-responses` with `file:line` refs, replay-bug history, provider table, tests, pin diff, and 36 ranked edge cases. Concern: GitNexus tools were unavailable (direct reads used), and fantasy fit was not audited.
