# H4 research: Pi cross-API replay and model switch

Date: 2026-10-05. Read-only. Pi checkout `4c6fb7cfe` (v1.0.1). Roadmap pin `2bbfcca4`. Callers found with `rg` (GitNexus tools were not exposed to this agent; see Limitations).

Path keys: **AI** = `packages/ai/src/api/`, **AIT** = `packages/ai/test/`, **CA** = `packages/coding-agent/src/core/`, **AG** = `packages/agent/src/`, **L** = Ask `master-2`. Line numbers are for `4c6fb7cfe`. Line numbers in the H3 reports are for `2bbfcca4`. They shifted in `anthropic-messages.ts` (call site now `:1134`, was `:1057`). `transform-messages.ts` did not change, so its lines match H3.

## 1. Summary

1. Pi converts history in two layers. Layer 1 is `transformMessages` (same-model test is provider + api + id). Layer 2 is each adapter's own rules, and these use other tests. The Go function covers layer 1. The gaps are in layer 2 and in model data.
2. Six adapters call layer 1 directly: Anthropic, Bedrock, Completions, Google, Mistral, and the shared Responses converter. Codex and Azure reach it through that converter. Each has its own id rule (section 3).
3. The Go `TransformMessages` has the same callback contract as Pi (`(id, model, source)`). It needs no change for the OpenAI targets. One small divergence exists in duplicate-id pairing (section 4).
4. The missing parts for H4 are: the per-API normalizers, a `shortHash`, `Model.ThinkingLevelMap` and `Model.Compat`, and the two OpenAI serializers.
5. The agent loop in Ask already has a per-request model hook (`L:internal/agent/loop_run.go:224-253`). This is the same design as Pi (`CA:agent-session.ts:760-786`). A model switch needs no loop change.
6. Pi forgets the requested thinking level on a switch. The new level comes from settings first, then the current level (`CA:agent-session.ts:2633`). Ask must decide this (Unresolved 3).
7. The inventory text "cache lost" has no code in Pi. It is a provider-side effect.
8. All Pi tests that check content across two providers are live. Only two cross-provider tests run offline (section 5). Ask needs its own offline golden-payload tests.
9. Between the two pins, no layer 1 or adapter replay code changed, except one Responses fix (`bc2d8dc1c`, section 7).

## 2. Two layers (read this first)

| Layer | Where | "Same model" test | What it does |
|---|---|---|---|
| 1 | `AI:transform-messages.ts:64-235` | `provider` and `api` and `model` all equal (`:95-98`) | Foreign thinking becomes plain text. Redacted thinking is dropped. `textSignature` and `thoughtSignature` are dropped. Ids go through the callback. Orphan calls get synthetic results. Errored and aborted assistants are dropped. Images are downgraded. |
| 2, Responses | `AI:openai-responses-shared.ts:258-260` | `isSameProviderAndApi` (provider + api). `isDifferentModel` = same provider+api, other id. `isSameModel` = all three. | `isDifferentModel` sets the function-call item id to `undefined` (`:302-305`). `namespace` is kept only if `isSameModel` (`:316, :325`). |
| 2, Google | `AI:google-shared.ts:231` | `provider` and `model` only. `api` is ignored. | Signatures are kept only if same. Cross-model thinking is plain text (`:248-262`). |
| 2, Mistral | `AI:mistral-conversations.ts:837-841` | none | All non-empty thinking becomes a `thinking` chunk. No signature exists. |
| 2, Completions | `AI:openai-completions.ts:1304-1340` | none (relies on layer 1) | Reads a signature as a field name or a `reasoning_details` JSON. Both exist only on same-model blocks. |
| 2, Anthropic | `AI:anthropic-messages.ts:1393-1448` | none (relies on layer 1) | Signed thinking is replayed. Unsigned thinking becomes text unless `allowEmptySignature`. |

Consequence: a Go test that only calls `TransformMessages` cannot prove replay is correct. Each serializer needs a golden-payload test.

## 3. Item 1: callers and id rules

### 3.1 Callers of `transformMessages` (non-test)

| Adapter | Call site | Callback passed |
|---|---|---|
| Anthropic | `AI:anthropic-messages.ts:1134` | `normalizeToolCallId` (`:1290-1292`) |
| Completions | `AI:openai-completions.ts:1220` | inline wrapper over `normalizeToolCallId` (`:1194-1217`) |
| Responses (shared) | `AI:openai-responses-shared.ts:179` | `normalizeToolCallId` (`:165-177`) |
| Google (and Vertex) | `AI:google-shared.ts:200` | `normalizeToolCallId` (`:195-198`) |
| Mistral | `AI:mistral-conversations.ts:142-144` | `createMistralToolCallIdNormalizer()` per request (`:237-257`) |
| Bedrock | `AI:bedrock-converse-stream.ts:975-979` | `normalizeToolCallId` (`:926-929`) |
| Codex | none direct. Calls `convertResponsesMessages` at `AI:openai-codex-responses.ts:542` and `:1566`, with `CODEX_TOOL_CALL_PROVIDERS` (`:61`) | Responses rule |
| Azure Responses | none direct. `AI:azure-openai-responses.ts:293`, set at `:26` | Responses rule |

Tests that call it directly: `AIT:transform-messages-copilot-openai-to-anthropic.test.ts:80,129,150,180`, `AIT:lax-message-content.test.ts:60`, `AIT:anthropic-sse-parsing.test.ts:162`.

### 3.2 Target API to id rule

| Target | Charset | Max length | Prefix rule | Hash | Collision guard | `call_id\|item_id` becomes |
|---|---|---|---|---|---|---|
| Anthropic `:1290-1292` | `[a-zA-Z0-9_-]`, others become `_` | 64 (UTF-16 units, `slice`) | none | none | none | `call_x_fc_y` (the `\|` is a bad char). Verified by test `AIT:transform-messages-copilot-openai-to-anthropic.test.ts:143-155`. |
| Bedrock `:926-929` | same regex as Anthropic | 64 | none | none | none | same as Anthropic |
| Completions `:1194-1217` | `[a-zA-Z0-9_-]` for piped ids only | 40 for piped ids and for provider `openai`. Other providers unlimited. | none | `shortHash(rawId).slice(0,8)` only when the joined id is over 40 | none | Split at the first `\|`. Join as `callId_itemId`. If over 40: `callId.slice(0,31)` + `_` + 8 hash chars. |
| Completions, id without `\|` | none applied | 40 only if provider is `openai` | none | none | none | unchanged (`toolu_...` passes through, `:1216-1217`) |
| Responses `:154-177` | `[a-zA-Z0-9_-]`, trailing `_` stripped | 64 per part | item id must start `fc_` (`:171-174`); grammar tools need `ctc_` at serialize time (`:302`) | `fc_${shortHash(item)}` when source provider or api differs from target (`:160-163, :169-170`) | none | Keeps `call\|item` as `normCall\|fc_...`. Output can be 129 chars. Only for providers in the allowed set (`openai`, `openai-codex`, `opencode`; Azure adds `azure-openai-responses`). Other providers: whole id goes through `normalizeIdPart`, so `\|` becomes `_`. |
| Responses, id without `\|` | as above | 64 | none | none | none | `toolu_X` stays `toolu_X`. Serializer: `split("\|")` gives no item id, so `function_call.id` is omitted (`:292-305`). |
| Google `:195-198` | `[a-zA-Z0-9_-]` | 64 | none | none | none | Only if `requiresToolCallId(model.id)` (`claude-*`, `gpt-oss-*`, Gemini 3 and up; `:165-172`). Other models: identity. |
| Mistral `:237-267` | `[a-zA-Z0-9]` only | exactly 9 (`:28`) | none | `shortHash(seed)` stripped to alnum, cut to 9 | yes: per-request reverse map and retry with `:attempt` suffix | Pipe and underscore removed, then hash if not 9 chars. |
| Codex | as Responses | as Responses | as Responses | as Responses | none | as Responses |

Notes:
1. The callback runs only for blocks that are not same-model (`AI:transform-messages.ts:136-142`). A same-model id is never changed.
2. Only Mistral guards against two ids that map to one. Two foreign ids that share their first 64 characters collide in Anthropic, Bedrock, Google, and Responses.
3. `shortHash` (`AI:../utils/hash.ts:2-13`) uses `charCodeAt` (UTF-16 units), `Math.imul`, and base36. Ids from `[^a-zA-Z0-9_-]` replacement hold only ASCII, so Go output differs only for astral characters in the raw id (JS gives two `_`, Go by rune gives one). The hash of a raw id with astral characters also differs.
4. The Bedrock code allows `_` and `-`, while the edge-case report E§13 says "alphanumeric only". The code is the authority here. Not tested live.

### 3.3 Foreign signatures and redacted thinking, per target

| Target | `thinkingSignature` (same model) | `textSignature` | `thoughtSignature` on tool call | Redacted thinking | Foreign thinking |
|---|---|---|---|---|---|
| Anthropic | `thinking` block with `signature` (`AI:anthropic-messages.ts:1428-1437`). No signature: text, or `signature:""` if `allowEmptySignature` (`:1416-1427`). | not sent | not sent | `redacted_thinking`, `data = thinkingSignature` (`:1405-1410`) | text (layer 1) |
| Responses | JSON of a reasoning item, parsed with `JSON.parse` and pushed as `reasoning` item (`:264-267`). No `try/catch`. | JSON `{v:1,id,phase}` gives message `id` and `phase` (`:59-75, :271-281`). Id over 64 becomes `msg_${shortHash}`. Missing: `msg_pi_{idx}[_n]`. | not sent | not special-cased (layer 1 drops cross-model) | text (layer 1), so no reasoning item |
| Completions | field name (`reasoning`, `reasoning_content`, `reasoning_text`; `:268`) or `reasoning_details` JSON (`:215-223, :1304-1312`). The text goes into that field (`:1339`). `requiresThinkingAsText` makes all thinking plain text joined by `\n\n` (`:1315-1323`). | not sent | legacy encrypted detail read from tool call signature (`:225-235, :1308-1311`) | not special-cased | text (layer 1) |
| Google | `thought:true` part with `thoughtSignature` if valid (`AI:google-shared.ts:248-258`) | `thoughtSignature` on text part (`:235-245`) | `thoughtSignature` on functionCall (`:266-275`) | not special-cased | text (`:259-262`) |
| Mistral | no signature field. All thinking becomes a `thinking` chunk (`AI:mistral-conversations.ts:837-841`). | not sent | not sent | not special-cased | text (layer 1), then sent as text |
| Bedrock | `reasoningText.signature` only for Anthropic models (`AI:bedrock-converse-stream.ts:1043-1060`, `:902`). No signature: plain `text`. | not sent | not sent | `reasoningContent.redactedContent` from base64 payload (`:1032-1037`) | text (layer 1) |

Key trap: Completions stores a field name such as `"reasoning_content"` in `thinkingSignature`. It is not a signature. It must never reach an Anthropic `signature` field. Only layer 1 prevents this.

## 4. Item 2: Go `transform.go` against Pi

Go file: `L:internal/providers/transform.go`. Pi file: `AI:transform-messages.ts`.

| Aspect | Pi | Go | Result |
|---|---|---|---|
| Callback signature | `(id, model, source)` `:67` | `NormalizeToolCallID func(id, model, source AssistantMessage)` `transform.go:19` | match. `sourceAssistant` is supported. |
| Same-model test | provider + api + id `:95-98` | `sameModel` `transform.go:278-280` | match. `responseModel` unused in both. |
| Callback runs only on foreign blocks | `:136` | `transform.go:253-266` | match |
| Re-key results by forward map | `:84-90` | `rekeyResult` `:269-276` | match |
| Thinking order (redacted, signed, empty, same, text) | `:101-117` | `rewriteThinking` `:232-249` | match |
| Drop `thoughtSignature` on foreign call | truthy check `:131-134` | non-nil check `:256-259` | tiny difference: an empty-string signature is cleared in Go and kept in Pi. Harmless. |
| `textSignature` dropped on foreign text | `:119-125` | `:220-224` | match |
| Image placeholders and tool variant | `:12-57` | `:102-162` | match |
| Null content to `[]` | `:73` | `emptyNilContent` `:43-100` | match |
| Pairing: synthetic result, hold system, user closes calls, drop errored/aborted | `:158-235` | `pairMessages` `:287-351` | match, one divergence below |
| Pairing with two calls that have the same id | `existingToolResultIds` is a set. One result satisfies both calls (`:111, :155`). | `pending[i].done` marks the first match only (`:325-330`). The second call gets a synthetic result with the same id. | **divergence**. It shows only after an id collision. Pi's output is also invalid (two `tool_use` with one id). Fix at the normalizer with a collision guard, not here. |
| Clock | `Date.now()` | injected `now` | better in Go |
| Input not mutated | by reference reuse | copies (`transform_test.go:219`) | match |

Gaps for H4 (all outside the function):

| # | Gap | Evidence | Owner |
|---|---|---|---|
| G1 | No Anthropic-style normalizer in the shared package. Only `tokenplan` has a private copy (`L:internal/providers/tokenplan/provider.go:288-300`). It counts runes, not UTF-16 units. | local | H4: move to a shared place |
| G2 | No Completions normalizer (40-char rule with hash) | `AI:openai-completions.ts:1194-1217` | H4 |
| G3 | No Responses normalizer (`call\|fc_hash`, allowed-provider set) | `AI:openai-responses-shared.ts:154-177` | H4 |
| G4 | No `shortHash`. Both OpenAI rules need it. Bit-equality with Pi is not needed because no session file is shared and the hash is recomputed on every request. Determinism is needed for cache stability. | `AI:../utils/hash.ts` | H4 decision (Unresolved 1) |
| G5 | `Model` has no `ThinkingLevelMap`. Re-clamp cannot work without it (`AI:../models.ts:1217-1247`). | `L:internal/providers/model.go:5-17` | H4 |
| G6 | `Model` has no `Compat` (`requiresThinkingAsText`, `supportsFinishReason`, `allowEmptySignature`, `requiresAssistantAfterToolResult`, `requiresToolResultName`). | `AI:openai-completions.ts:1315, :696, :1230-1233, :1416-1418` | H4 (H-PROV-26 data) |
| G7 | No collision policy. Pi is inconsistent (only Mistral guards). | section 3.2 | H4 decision (Unresolved 2) |
| G8 | Responses serializer rules at layer 2: `isDifferentModel` drops item id, `namespace` only if same model, `msg_pi_{idx}` fallback ids, `fc_`/`ctc_` prefix check. | `AI:openai-responses-shared.ts:258-330` | H4 |
| G9 | Completions serializer rules: join of text parts has no separator (`:1300`), tool-result images go in a separate user message (`:1455`), empty assistant skipped (`:1389-1394`). | `AI:openai-completions.ts` | H4 |
| G10 | `ThinkingMax` exists in Go (`L:pkg/protocol/message.go:55`), so the clamp list can match Pi (`off,minimal,low,medium,high,xhigh,max`, `AI:../models.ts:1215`). | local | none |

## 5. Item 4: Pi cross-provider tests

Totals: `AIT` has 171 entries. Only files that touch cross-API replay are listed.

### 5.1 Live (skipped without keys)

| File | What it asserts |
|---|---|
| `cross-provider-handoff.test.ts` (524 lines; `skipIf(!hasAnyApiKey())` `:352`) | Builds a 4-message fixture live for each provider with a key. Sends all other fixtures to each target and asks it to say hello. Asserts only that no target returns `stopReason: "error"` (`:520`). No content or payload check. |
| `tool-call-id-normalization.test.ts` | Live handoff Copilot to OpenRouter to Codex (`:45, :115`). Prefilled context with the real 450-char id from issue #1022 (`:183-238`), sent to OpenRouter (`:239`) and Codex (`:265`). Asserts no "call_id too long" error. |
| `openai-responses-reasoning-replay-e2e.test.ts` (needs OpenAI and Anthropic keys) | `:19` an aborted turn with a reasoning item only must not cause a 400. `:83` same provider, other model (gpt-5-mini to gpt-5.5) with a tool call. `:183` Anthropic thinking + `toolu_` call to OpenAI, answer must contain "42". |
| `tool-call-without-result.test.ts` | Per provider: an orphan call is handled (`:93-177`+). |
| `empty.test.ts`, `abort.test.ts`, `image-tool-result.test.ts`, `unicode-surrogate.test.ts` | Per provider live checks of empty content, abort, image tool results, lone surrogates. Not read in full. |

### 5.2 Offline

| File | What it asserts |
|---|---|
| `transform-messages-copilot-openai-to-anthropic.test.ts` | 4 tests: foreign thinking becomes text; foreign `thoughtSignature` removed; trailing orphan `call_123\|fc_123` gets synthetic result `call_123_fc_123`; two calls with one result get one synthetic result. |
| `openai-responses-foreign-toolcall-id.test.ts` | Copilot call id to Codex gives `fc_<shortHash(item)>`, length 64 or less, matches `^fc_[A-Za-z0-9]+$` (`:19-65`). |
| `lax-message-content.test.ts` | Null content becomes `[]`. |
| `anthropic-sse-parsing.test.ts:142` | Signed thinking survives a proxy `responseModel` relabel. |
| `anthropic-empty-thinking-signature-compat.test.ts` | Empty signature becomes text by default; kept with `allowEmptySignature`; cross-model Fireworks thinking becomes text (`:127`). |
| `openai-responses-message-id.test.ts` | Unique fallback `msg_pi_*` ids for several text blocks. |
| `openai-responses-namespace.test.ts:178` | Namespaces dropped when the target cannot replay them. |
| `azure-openai-responses-reasoning-replay.test.ts` | `encrypted_content` backfill. |
| `openai-completions-reasoning-details.test.ts` | `reasoning_details` kept in signature; legacy tool-call signature fallback. |
| `openai-completions-thinking-as-text.test.ts:119,142` | Same-model thinking as text parts. The test at `:155` sends a request; its server was not checked. |
| `google-thinking-signature.test.ts` | `thought === true` is thinking; signature kept across deltas. |
| `bedrock-redacted-reasoning.test.ts`, `bedrock-convert-messages.test.ts` | Redacted payload replay; blank block handling. |
| `openai-responses-terminal-event.test.ts` | Stream completeness (section 8). |

Not found in Pi: any offline test that sends Anthropic history to a Responses or Completions serializer and checks the payload, any test of switching back to the first model, and any test of the thinking level after a switch.

## 6. Item 3: model switch in Pi

### 6.1 Behavior

| Topic | What Pi does | Evidence |
|---|---|---|
| Entry check | `setModel` calls `checkAuth`. It throws `No API key for provider/id` before any change. | `CA:agent-session.ts:2431-2433` |
| Order of writes | 1. `agent.state.model = model`. 2. `appendModelChange`. 3. optional settings write. 4. `setThinkingLevel`. | `:2437-2447` |
| Streaming guard | None in `setModel`. Any guard in the UI was not checked. | `:2430-2450` |
| Thinking target level | Explicit level, else per-model setting, else global default setting, else the current level, else `DEFAULT_THINKING_LEVEL`. | `:2622-2634` |
| Re-clamp | `setThinkingLevel` clamps with the new model. Supported levels: non-reasoning gives `["off"]`; a level is dropped if `thinkingLevelMap[level] === null`; `xhigh` and `max` need a map entry. Clamp: nearest higher level, then nearest lower. | `:2565-2567, :2636-2637`; `AI:../models.ts:1217-1247` |
| Entry for level | `thinking_level_change` is appended only if the clamped level differs from the previous one. | `:2570-2587` |
| Requested level | Not stored. Switching to a non-reasoning model writes `off`. Switching back gets the settings default or the current level (`off`). | `:2633` |
| Cycle | Scoped list filtered by available models, wraps, unknown current index becomes 0. A scoped entry can carry its own level. | `:2472-2553` |
| Per-request model | `AgentSession` installs `prepareRequest`. It returns `model: this.agent.state.model` and the current level. The loop merges it before each LLM call. | `CA:agent-session.ts:760-786`; `AG:agent-loop.ts:222-240` |
| Plain `Agent` | Snapshots the model at run start. A mid-run change waits for the next run unless a hook exists. | `AG:agent.ts:470` |
| History | Never rewritten. Each assistant message stores `api`, `provider`, `model` (and `thinkingLevel`). Conversion is per request in `transformMessages`. | `AG:agent-loop.ts:409`; `AG:agent.ts:531-545` |
| Images | Not changed on switch. Downgraded per request (layer 1). | `AI:transform-messages.ts:35-57` |
| Cache | No code runs on switch. The cache warmer reacts only to mode change and settle. Provider caches are per model, so the first request after a switch misses. | `CA:agent-session.ts:1051, :1408` |
| Resume | Model comes from the latest `model_change` on the branch (`getBranchSelection`). Thinking comes from the latest `thinking_level_change`, else the settings default. Then it clamps. If the model is gone or has no auth, a fallback message appears and `findInitialModel` picks another. | `CA:sdk.ts:194-262`; `CA:model-resolver.ts:714-783`; `CA:session-manager.ts:419-434` |
| Virtual models | A virtual selection routes each request to a physical model. This adds `routedModel` and extra logic. Out of H4 scope. | `CA:agent-session.ts:587-613, :1422-1428` |

### 6.2 Which part belongs where

| Bucket | Parts | Evidence |
|---|---|---|
| H4, in memory | Set the model and thinking level in loop state. Check auth first. Re-clamp with `ThinkingLevelMap`. Per-request model read through the `PrepareRequest` hook. Pure per-request conversion of stored history. Keep a requested level apart from the clamped level (decision). | `CA:agent-session.ts:2430-2450, :2565-2588`; `L:internal/agent/loop_run.go:224-253` |
| H8, persistence | `model_change` and `thinking_level_change` entries. Resume rule. Fallback message on a missing model. `getSessionContextSettings`. | `CA:session-manager.ts:1219, :1232, :419-434`; `CA:sdk.ts:194-262`; `CA:model-resolver.ts:714` |
| Later | `persist` option to settings, per-model level settings, `model_select` extension event, scoped cycling, `/model` UI, virtual models. | `CA:agent-session.ts:2439-2442, :2415-2421` |

Correction for the inventory: H-PROV-21 (`inventory-harness.md:117`) says "foreign thinking becomes tagged text" and "cache lost". Pi gives untagged text (H3 finding). "Cache lost" has no code. The cited lines `:2444-2500` are the thinking and cycle code.

## 7. Item 6: changes between `2bbfcca4` and `4c6fb7cfe`

| Area | Result |
|---|---|
| `AI:transform-messages.ts`, `openai-completions.ts`, `google-shared.ts`, `mistral-conversations.ts`, `openai-codex-responses.ts`, `azure-openai-responses.ts`, `utils/hash.ts` | 0 lines changed |
| `AI:openai-responses-shared.ts` | One change, commit `bc2d8dc1c` (2026-10-01). Item id is dropped if `isDifferentModel` or if the prefix does not match the replayed type (`fc_` for function calls, `ctc_` for custom tools; `:302-305`). Before, only `fc_` ids on different models were dropped. Fixes switching OpenAI models after a grammar tool call. |
| `AI:anthropic-messages.ts` | Large diff, none in replay logic. Added: federation auth, `inline-tools-2026-09-15` beta, tool definitions by value. Adjacent only. |
| `AI:bedrock-converse-stream.ts` | Added `block_binding: drop_block` for Claude Opus 4.7+, Sonnet 5+, Fable 5 (CHANGELOG 1.0.1). Server drops stale signed thinking after a system prompt or tool change. Replay code did not change. |
| `AIT` | No cross-provider test file changed. New tests are for federation, model data, Cloudflare, Bedrock thinking payload. |
| `CA:agent-session.ts` | 76 lines changed, none in model or thinking switch (MCP and tool loading). |
| `CA:model-resolver.ts` | One line: NVIDIA default model id. |
| `AG` | `packages/agent/src/harness/**` was deleted (97k lines). Not used by `agent-loop.ts`. Not a replay change. |

## 8. Item 5: stream completeness per protocol

| Protocol | Detection | Error text | Evidence |
|---|---|---|---|
| Anthropic | A. If `message_start` was seen but `message_stop` was not: throw. B. If the stream ends with `stopReason` still `pending` (for example an empty stream): throw. C. `stopReason` error or aborted: rethrow stored message or `An unknown error occurred`. | A: `Anthropic stream ended before message_stop`. B: `Anthropic stream ended without a stop reason`. | `AI:anthropic-messages.ts:566-568, :865-870` |
| Responses (shared) | No `response.completed` or `response.incomplete` or `response.failed` seen. Then a guard: when `stopReason` is `toolUse`, any tool call with a partial buffer throws. | `OpenAI Responses stream ended before a terminal response event`. `OpenAI Responses stream completed with an unfinished tool call: {name} ({id})`. | `AI:openai-responses-shared.ts:743-775` |
| Responses (wrapper) | Second, defensive check on `pending`. Azure and Codex have the same check. | `OpenAI Responses stream ended without a stop reason`; Azure `:143`; Codex `Codex stream ended without a stop reason` `:112`; Codex WebSocket `WebSocket stream closed before response.completed` `:1415` | `AI:openai-responses.ts:206` |
| Responses, incomplete | `status incomplete`: `max_output_tokens` is a length stop. Any other reason is an error. | `Response incomplete: {reason}` or `Response incomplete without a provider reason` | `AI:openai-responses-shared.ts:~781-800` (text at `:795`) |
| Completions | If `compat.supportsFinishReason` (default true, `:1642`) and no `finish_reason` was seen: throw. If the compat flag is false: infer `toolUse` or `stop` and do not fail. | `Stream ended without finish_reason` | `AI:openai-completions.ts:576-583, :690-697` |

All three adapters turn a thrown error into an assistant message with `stopReason` `error` (or `aborted` if the signal fired) and keep partial content (`AI:openai-completions.ts:706-723`). Layer 1 then drops that message on the next request.

Pi test: `AIT:openai-responses-terminal-event.test.ts:244-448`. No Pi test for the Anthropic stream-end check by name; `anthropic-sse-parsing.test.ts` covers parsing only.

## 9. Item 7: scenario rows for the Go port

Kind: **U** = offline unit on `TransformMessages` or a normalizer. **G** = offline golden-payload test on a serializer (fake HTTP server). **I** = offline in-memory loop test with faux providers and the `PrepareRequest` hook. **L** = live only (outcome depends on the vendor).

| # | Scenario | Expected result | Evidence | Kind |
|---|---|---|---|---|
| S1 | Anthropic thinking (signed) + text + `toolu_` call + result, then Responses model | No `reasoning` item. Thinking text becomes an `output_text` message (id `msg_pi_{idx}`). `function_call` has `call_id=toolu_...` and no `id`. Result uses the same `call_id`. | `AI:transform-messages.ts:113-116`; `AI:openai-responses-shared.ts:266, :273, :292-305, :333` | U+G; L (`AIT:openai-responses-reasoning-replay-e2e.test.ts:183`) |
| S2 | Same history, then Completions model | Thinking text and answer text are two text blocks, joined with no separator (`"plan" + "answer"` gives `"plananswer"`). Call id unchanged (`toolu_...`, cut to 40 only for provider `openai`). | `AI:openai-completions.ts:1300, :1216-1217` | G |
| S3 | Responses reasoning item (JSON signature, empty summary) + call `call_x\|fc_y`, then Anthropic | Empty-summary thinking dropped. Call id `call_x_fc_y`. Result re-keyed. No `signature` sent. | `AI:transform-messages.ts:111`; transform test `:143-155` | U+G |
| S4 | Same, with a non-empty reasoning summary | Summary becomes a text block. Never a `thinking` block with a JSON signature. | `:113-116` | U+G |
| S5 | Responses `call_x\|<400-char item>` to Anthropic, to Completions, to Codex | Anthropic: `call_x_` + item chars, cut to 64. Completions: `call_x_<item>` if 40 or less, else 31 chars + `_` + 8 hash chars. Codex from Copilot: `call_x\|fc_<hash>`, 64 or less per part. | section 3.2; `AIT:openai-responses-foreign-toolcall-id.test.ts` | U |
| S6 | Two parallel calls with the same `call_id` and different item ids (Responses) to Completions | Two distinct ids. | `AI:openai-completions.ts:1189-1192` comment and `:1204-1205` | U |
| S7 | Two parallel calls whose long ids share the first 64 chars, to Anthropic | Pi: both get one id (bug). Ask: must give two distinct ids (collision guard) or fail loudly. | section 3.2 note 2 | U (decision) |
| S8 | Completions `reasoning_content` thinking (signature `"reasoning_content"`), then Anthropic | Text block. Field name never reaches `signature`. | `AI:openai-completions.ts:1339`; `:113-116` | U+G |
| S9 | Completions reasoning, then Responses | Text, no reasoning item. | same | U+G |
| S10 | Round trip Anthropic, Completions, Anthropic | Stored history unchanged (no mutation). On return, the first assistant message is same-model: signed thinking and original `toolu_` ids replay verbatim. The Completions message gives text. | `transform.go:24-41` copies; `AI:transform-messages.ts:95-109` | U+I |
| S11 | Switch back to the original Anthropic model after a system prompt or tool change | Request keeps the stored signatures. The server may drop them (`block_binding drop_block`, only models with `supportsMidConvoEffort`; Pi stores `anthropic_input_transformations`). | `AI:anthropic-messages.ts:1234-1240, :871-880` | L |
| S12 | Redacted thinking, Anthropic to Responses or Completions | Dropped. | `AI:transform-messages.ts:104-106` | U |
| S13 | Redacted thinking, same Anthropic model | `redacted_thinking` with `data` = signature. | `AI:anthropic-messages.ts:1405-1410` | G |
| S14 | Aborted turn (reasoning only, no signature or with signature), then switch | Message dropped. No synthetic results for its calls. | `AI:transform-messages.ts:201-203`; live `AIT:...e2e.test.ts:19` | U+G |
| S15 | Errored turn (empty text block, `errorMessage` set) then switch | Dropped. `errorMessage` never sent. | same; `AG:agent.ts:531-545` | U |
| S16 | Tool result that follows an aborted assistant's call | Kept as orphan (Pi passes it through). Decide: keep or drop. Anthropic rejects an unmatched `tool_result`. | `AI:transform-messages.ts:213-215` | U (decision) |
| S17 | Three parallel calls, results out of order, then switch | Results keep stored order. Anthropic merges consecutive results into one user message. | `AI:anthropic-messages.ts:1463-1475` | G |
| S18 | Three parallel calls, one without result (batch aborted), then switch | Synthetic `No result provided` (`isError:true`) is placed after the real results and before the next assistant or user message. It uses the normalized id. | `AI:transform-messages.ts:108-127, :134, :165`; the second pass reads transformed ids | U+G |
| S19 | Image in a user message and in a tool result, target model without image input | Placeholders. Image-only user message becomes one non-empty text block. Consecutive images give one placeholder. | `AI:transform-messages.ts:12-57` | U |
| S20 | Same, target with image input, Completions | Tool result text is `(see attached image)` if no text. Images go in a separate user message `Attached image(s) from tool result:`. | `AI:openai-completions.ts:1413-1414, :1455` | G |
| S21 | Same provider and api, other model (gpt-5-mini to gpt-5.5) with reasoning + call | Layer 1 treats it as foreign: thinking is text, id normalized. Layer 2 omits `function_call.id`. No `reasoning` item. | `AI:openai-responses-shared.ts:258-260, :302-305` | U+G; L (`:83`) |
| S22 | Same, with a grammar tool (`ctc_` id) | Item id dropped if prefix does not match the replayed type. | `:302-305`; commit `bc2d8dc1c` | G |
| S23 | Same model but proxy sets `responseModel` | Still same model. Signed thinking kept. | `AIT:anthropic-sse-parsing.test.ts:142` | U |
| S24 | Same Anthropic model, `stop` turn, thinking has no signature (proxy) | Text block, unless `allowEmptySignature` then `signature:""`. | `AI:anthropic-messages.ts:1416-1427` | G |
| S25 | Same Anthropic model, signature present, thinking text empty | Block kept. | `AI:transform-messages.ts:109` | U |
| S26 | Completions with `requiresThinkingAsText` | All thinking joined by `\n\n` and put first as a text part. | `AI:openai-completions.ts:1315-1323` | G (needs `Compat`) |
| S27 | Responses text block with signature `{v:1,id,phase}`, same model | Message `id` and `phase` kept. Id over 64 becomes `msg_${hash}`. Cross-model: `msg_pi_{idx}`. | `AI:openai-responses-shared.ts:59-75, :271-281` | G |
| S28 | Tool call with `namespace`, same model and other model | Kept only if same model. | `AI:openai-responses-shared.ts:316, :325` | G |
| S29 | Switch while a run is active: turn 1 on Anthropic returns a tool call; hook sets model to Responses before turn 2 | Turn 2 goes to the Responses adapter with a normalized id. Turn 1 message is not changed. Result re-keyed. | `L:internal/agent/loop_run.go:224-253`; `CA:agent-session.ts:760-786` | I |
| S30 | Thinking level: Anthropic `high`, switch to non-reasoning, switch back | Pi: `off` after the first switch. After the return: settings default if set, else `off`. Ask must choose (Unresolved 3). Entries: `thinking_level_change` only when the clamped level changes. | `CA:agent-session.ts:2570-2587, :2633` | I |
| S31 | Switch to a model whose level map has `max` null | Clamp up first, then down. | `AI:../models.ts:1228-1247` | U |
| S32 | Switch without auth for the target | Error before any state change. | `CA:agent-session.ts:2431-2433` | I |
| S33 | Stream ends without terminal event, for each of three protocols | Error message text per section 8. The partial message is dropped on the next request. | section 8 | G |
| S34 | Empty assistant after filtering (no text, no calls) | Skipped by Completions, Anthropic, Responses. | `AI:openai-completions.ts:1389-1394`; `AI:anthropic-messages.ts:1448`; `AI:openai-responses-shared.ts:330` | G |
| S35 | Raw id with astral characters | JS counts two UTF-16 units. Go by rune counts one. Pick one rule and fix a golden value. | `AI:anthropic-messages.ts:1291` | U (decision) |

## 10. Ranked recommendation for H4

1. Put the Anthropic, Completions, and Responses normalizers in `internal/providers` as pure functions with the `NormalizeToolCallID` type. Add a deterministic Go `shortHash`. Rank 1 because every cross-API test needs them.
2. Add `ThinkingLevelMap` and `Compat` to `Model` as data before the serializers. Rank 2 because re-clamp and three serializer branches need them.
3. Write the golden-payload tests (kind G) before the live tests. Pi has none, and its live tests only check "no 400".
4. Add one in-memory test (S29) that uses the existing `PrepareRequest` hook. This proves the H4 exit without loop changes.
5. Add a collision guard in the shared normalizer layer (Mistral style). Do not copy Pi's unguarded rules.
6. Keep a requested thinking level separate from the clamped level.

Risk: Pi regressed Responses replay at least five times (E§3 row 2). The adapter-level rules in G8 are the most likely source of repeats.

## 11. Limitations

1. GitNexus tools were not available to this agent. Callers come from `rg` over `packages` (excluding `node_modules`, `dist`) and direct reads. The list in 3.1 matches the grep for `transformMessages(`.
2. Live-test files `empty`, `abort`, `image-tool-result`, `unicode-surrogate`, and `tool-call-without-result` were read for structure only.
3. Not read: Responses `convertToolResultOutput` (`AI:openai-responses-shared.ts:81`), Google Gemini 3 unsigned-call rules, `AIT:google-shared-*` tests.
4. No code was run. Anthropic and OpenAI server behavior (for example `block_binding`) is not verified.
5. UI guards against switching during a stream were not checked.

## Unresolved questions

1. Go `shortHash`: any stable hash, or port the Pi hash for equal output? (Recommendation: any stable hash; no shared sessions.)
2. Collision policy: guard in all normalizers (Mistral style) or follow Pi? (Recommendation: guard.)
3. Thinking level on switch: follow Pi (settings default first), or keep the session's requested level and clamp each time? E§13 says the level must survive a detour through a non-reasoning model. The Pi code does not guarantee it.
4. Orphan tool results (S16): keep Pi pass-through or drop?
5. Does the OpenAI adapter use `fantasy` or hand-written HTTP? This changes where layer 2 rules live. Not decided in the files read.
6. Bedrock id charset: the code allows `_` and `-`; E§13 says alphanumeric only. Is Bedrock in the chosen vendor set?

Status: DONE_WITH_CONCERNS
Summary: All seven items answered with file:line evidence. The Go `TransformMessages` matches Pi's layer 1 and supports the OpenAI targets with no change. The gaps are the normalizers, `shortHash`, `ThinkingLevelMap`, `Compat`, and the layer 2 serializer rules. 35 scenario rows are given.
Concerns/Blockers: GitNexus was not available; some live tests were read for structure only; six decisions are open (above).
