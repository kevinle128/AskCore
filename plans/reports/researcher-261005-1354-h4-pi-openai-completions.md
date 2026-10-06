# Pi `openai-completions`: how it works and what Ask H4 must keep

Date: 2026-10-05. Scope: read-only research for roadmap phase H4, item H-PROV-03.
Pi checkout: `/Users/dale/Desktop/workspace/opensources/pi` at `4c6fb7cfe` (v1.0.1). Roadmap pin: `2bbfcca4`.

Path shorthand used below:
- `OC` = `packages/ai/src/api/openai-completions.ts` (1726 lines)
- `TY` = `packages/ai/src/types.ts`
- `TM` = `packages/ai/src/api/transform-messages.ts`
- `SO` = `packages/ai/src/api/simple-options.ts`
- `GEN` = `packages/ai/scripts/generate-models.ts`
- `MD` = `packages/ai/src/models.ts`

## 0. Method and limits

- GitNexus MCP tools were not available in this session. I had no ToolSearch tool. I built the call graph with grep and direct reads. Section 1.1 holds it.
- The catalog data files (`packages/ai/src/providers/data/*.json`) are not in the checkout. They are hydrated at build time (`scripts/hydrate-model-catalog.ts`). So "which provider uses which compat value" comes from the generator code, not from the generated data. Section 2.3 holds it.
- I read the bodies of about 10 tests. For the rest I used test titles only. Section 4 marks which is which.
- I did not run any Pi code.

## 1. How the adapter works

### 1.1 Call graph (grep-derived)

```
streamSimple (OC:731)
  getClientApiKey (OC:82)
  buildBaseOptions (SO:21)  -> clampMaxTokensToContext (SO:15)
  clampThinkingLevel (MD:1228)
  stream (OC:299)
    resolveTranscript (OC:305)            // folds late system messages unless compat allows them
    getCompat (OC:1688) -> detectCompat (OC:1585)
    createClient (OC:752)                 // headers + OpenAI SDK client
    buildParams (OC:797)
      resolveTranscriptTools
      convertMessages (OC:1185) -> transformMessages (TM:64)
      convertTools (OC:1474)
      thinking branch (OC:875-972), budget (OC:978), cache control (OC:860)
    options.onPayload hook (OC:361)       // caller can replace the payload
    retryProviderRequest (OC:370)         // SDK retries are off (maxRetries: 0)
    chunk loop (OC:553-678) -> parseChunkUsage (OC:1511), mapStopReason (OC:1554)
    finishBlock for all blocks (OC:680)
    catch block (OC:702-725)
```

### 1.2 `stream` vs `streamSimple`

- `streamSimple` is the one with level-based reasoning. It resolves the API key (OC:736), builds base options (OC:738-741), clamps the level, and maps `off` to "no effort" (OC:742-743).
- `stream` takes raw `reasoningEffort` and `maxTokens`. It does no context clamp. It sets a max-token field only if `options.maxTokens` is truthy (OC:837).
- `reasoningEffort` accepts `minimal|low|medium|high|xhigh|max` (OC:151).

### 1.3 Option build (`buildParams`, OC:797-1002)

Order of work. Later steps can override earlier ones.

1. Base: `model`, `messages`, `stream: true` (OC:817-820).
2. `prompt_cache_key` when `baseUrl` contains `api.openai.com` and retention is not `none`, or when retention is `long` and compat allows long retention. The key is the session id cut to 64 code points (OC:821-825; `openai-prompt-cache.ts:1-8`).
3. `prompt_cache_retention: "24h"` when retention is `long` and compat allows it (OC:826).
4. `stream_options.include_usage = true` unless `supportsUsageInStreaming === false` (OC:829-831).
5. `store: false` when `supportsStore` (OC:833-835).
6. Max tokens. Field name from `compat.maxTokensField` (OC:837-844).
7. `temperature` when defined (OC:846-848).
8. Tools. See 1.6. Adds `tool_stream: true` when `zaiToolStream` (OC:850-854).
9. `tools: []` when there are no tools but the history has tool calls or tool results (OC:855-858). Reason in the code: Anthropic behind LiteLLM needs it. Test: `openai-completions-empty-tools.test.ts:253`.
10. Anthropic-style `cache_control` markers (OC:860-862; 1069-1183).
11. `tool_choice` pass-through (OC:864-866).
12. `priority` from `compat.vllmPriority` (OC:868-870).
13. Thinking params (1.5).
14. Thinking token budget field (OC:978-980).
15. `provider` from `model.compat.openRouterRouting` (OC:983-985).
16. `providerOptions.gateway` from `vercelGatewayRouting.only/order` (OC:988-996).
17. Last: `Object.assign(params, model.samplingParams, options.samplingParams)`. These keys can override any field above (OC:998-999).

### 1.4 Max-token clamp

- `SO:15-19`: `available = contextWindow - estimatedContextTokens - 4096`. Result is `min(maxTokens, max(1, available))`. If `contextWindow <= 0`, result is `max(1, maxTokens)`.
- `buildBaseOptions` input is `options.maxTokens ?? model.maxTokens` (SO:30).
- The estimate is a heuristic: 4 chars per token, 4800 chars per image (`utils/estimate.ts:15-16, 30-44`). It prefers the last assistant usage when present (`estimate.ts:71-90`).
- Tests: `openai-completions-empty-tools.test.ts:95-162` (default, explicit, both clamped).

### 1.5 Thinking and reasoning request formats

Reads `options.reasoningEffort` (already clamped by `streamSimple`) and `model.thinkingLevelMap`. A map value of `null` means "level not supported". A string means "send this word". `undefined` means "send the level as is". The branch is chosen by `compat.thinkingFormat`. All branches need `model.reasoning` true.

| `thinkingFormat` | Request fields | Off behavior | Lines |
|---|---|---|---|
| `openai` (default) | `reasoning_effort: map[level] ?? level`, only if `supportsReasoningEffort` | Sends `reasoning_effort: map.off` only if that is a string | OC:964-972 |
| `openrouter` | `reasoning: { effort: map[level] ?? level }` | `reasoning: { effort: map.off ?? "none" }` unless `map.off === null` | OC:933-942 |
| `deepseek` | `thinking: { type: "enabled" }` plus `reasoning_effort` if supported | `thinking: { type: "disabled" }` unless `map.off === null` | OC:923-932 |
| `zai` | `thinking: { type: "enabled", clear_thinking: false }`, plus `reasoning_effort` if supported (map string only) | `thinking: { type: "disabled" }` | OC:875-887 |
| `qwen` | top-level `enable_thinking: bool`, plus `reasoning_effort` if supported | `enable_thinking: false` | OC:888-895 |
| `qwen-chat-template` | `chat_template_kwargs: { enable_thinking, preserve_thinking: true }` | `enable_thinking: false` | OC:896-900 |
| `chat-template` | `chat_template_kwargs` built from `compat.chatTemplateKwargs` | per-key rules below | OC:901-905 |
| `baseten` | `chat_template_args` from `compat.chatTemplateArgs`, plus `reasoning_effort` if supported | maps `off` through the level map | OC:906-922 |
| `together` | `reasoning: { enabled: bool }`, plus `reasoning_effort` if supported | `enabled: false` | OC:948-956 |
| `string-thinking` | top-level `thinking: "<level>"` | `thinking: map.off ?? "none"` unless `map.off === null` | OC:957-963 |
| `ant-ling` | `reasoning: { effort }` only when the mapped value is a string | nothing | OC:943-947 |

Template value rules (`resolveChatTemplateKwargValue`, OC:1044-1067):
- A plain string, number, boolean, or null passes through.
- `{ $var: "thinking.enabled" }` gives `!!effort`.
- `{ $var: "thinking.budget" }` gives the clamped budget or drops the key.
- Any other object (the `thinking.effort` case) gives `map[level]` or the level; when off, `map.off`.
- `omitWhenOff: true` drops the key when thinking is off.
- A key that resolves to `undefined` is dropped. If no key is left, the whole object is omitted (OC:1026-1042).

Budget cap (OC:974-980, 1004-1024):
- Field name: `thinkingTokenBudgetField`, else `"thinking_token_budget"` when `supportsThinkingTokenBudget` is true, else none (OC:1004-1010). Allowed names: `thinking_token_budget`, `thinking_budget`, `thinking_budget_tokens` (TY:99).
- Value: `thinkingBudgetForLevel` (SO:65-69). Defaults minimal 1024, low 2048, medium 8192, high 16384 (SO:54-59). `xhigh` and `max` use the `high` budget (SO:61-63).
- Clamp: `min(budget, max(0, ceiling - 1024))`. Ceiling is `max_tokens ?? max_completion_tokens ?? model.maxTokens` (OC:1018-1022; SO:72-74). A result of 0 or less means "do not send".
- It is independent of `thinkingFormat`. Tests: `openai-completions-thinking-token-budget.test.ts:105-190`.

Level clamp before all this: `clampThinkingLevel` (MD:1228-1247). `getSupportedThinkingLevels` (MD:1217-1226) drops levels mapped to `null`. `xhigh` and `max` are supported only when the map has a non-undefined entry for them.

Quirks (all verified in code):
- `deepseek`, `openrouter`, `together`, `string-thinking`, and the `openai` branch use `map[level] ?? level`. A `null` map value falls back to the raw level, not to "skip". Only `zai`, `ant-ling`, `baseten`, and `qwen` (via a `typeof` check) skip on `null`.
- `qwen` and `deepseek` do not check that the level is supported. They rely on the earlier clamp.

### 1.6 Tools (`convertTools`, OC:1474-1509)

- Normal tool: `{ type: "function", function: { name, description, parameters, strict? } }`.
- `strict` is written only when `compat.supportsStrictMode !== false`, and its value is `strict ?? false` (OC:1505).
- `strict` is `true` only if the tool carries `constrainedSampling.type === "json_schema"` and its schema passes the strict conversion (`constrained-sampling.ts:226-250`). Strict conversion adds `additionalProperties:false`, makes every property required, and wraps optional ones in `anyOf [x, null]` (`constrained-sampling.ts:56-124`). It rejects `$ref`, `allOf`, `oneOf`, tuples, and similar.
- A tool with no `constrainedSampling` is sent with `strict: false` on strict-capable models. It is sent with no `strict` field on others.
- Grammar tool: `{ type: "custom", custom: { name, description, format: { type: "grammar", grammar: { syntax, definition } } } }`. Only when `supportsOpenAIGrammarTools` and the tool has a grammar variant (OC:1479-1495; `constrained-sampling.ts:252-285`).
- Tool schema `parameters` are passed as is when not strict.

### 1.7 `convertMessages` (OC:1185-1472)

Pre-step: `transformMessages` (TM:64-235) runs first. See section 3 for its rules.

Instruction role (OC:1225): `developer` if `model.reasoning && compat.supportsDeveloperRole`, else `system`.

System messages (OC:1240-1252):
- The first system message uses full text (`getSystemMessageText`). A later one uses `renderSystemMessageUpdate` (`utils/text.ts:28-39`).
- By default late system messages are folded into the first one before this point (`resolveTranscript`, `transcript.ts:108-120`).
- A system message with `toolsAdded` after position 0 becomes an extra `{ role: "system", tools: [...] }` message. This is a Kimi-specific shape (OC:186-189, 1241-1248). It only happens when both mid-convo compat flags are true.
- An empty system text is skipped.

User messages (OC:1253-1282):
- A string passes through (after `sanitizeSurrogates`).
- An array: empty text parts are dropped. Text becomes `{type:"text"}`. An image becomes `{type:"image_url", image_url:{url:"data:<mime>;base64,<data>"}}`.
- If no part is left, the message is skipped.
- When the model has no image input, `transformMessages` has already replaced images with the text `(image omitted: model does not support images)`. Neighbor placeholders are merged (TM:12-57).

Assistant messages (OC:1283-1397):
1. Start: `{ role:"assistant", content: requiresAssistantAfterToolResult ? "" : null }` (OC:1285-1288).
2. Text parts: drop blank (`trim().length === 0`) text blocks. Join the rest with `""` into one string (OC:1290-1300).
3. Thinking: only blocks with non-blank text count (OC:1313).
   - `requiresThinkingAsText`: content becomes an array `[ {text: thinking joined by "\n\n"}, ...text parts ]` (OC:1315-1321). This is the only case where assistant content is an array.
   - Else: content is the plain text string. Never an array (OC:1327-1329). Comment explains why (NVIDIA NIM DeepSeek mirrors array shape).
   - Reasoning field replay: if no `reasoning_details` are available, take the `thinkingSignature` of the first non-empty thinking block. If it is one of `reasoning`, `reasoning_content`, `reasoning_text`, set `assistantMsg[signature] = all thinking texts joined by "\n"` (OC:1332-1341; field list OC:268).
   - Provider `opencode-go` rewrites signature `reasoning` to `reasoning_content` (OC:1335-1337, also OC:620-623 on stream).
4. `reasoning_details` replay (OC:1304-1311, 1375-1377). Source order: (a) a thinking block whose `thinkingSignature` is a JSON array of valid details; (b) legacy encrypted details stored in `toolCall.thoughtSignature`. When present, the raw reasoning field is not set.
5. Tool calls (OC:1352-1374): `{id, type:"function", function:{name, arguments: JSON.stringify(args)}}`. Grammar tools use `{type:"custom", custom:{name, input}}`.
6. `requiresReasoningContentOnAssistantMessages && model.reasoning`: if `reasoning_content` is still undefined, set it to `""`. This applies to every replayed assistant message, with or without tool calls (OC:1378-1384).
7. Skip rule: if content is null or empty and there are no tool calls, the message is dropped (OC:1385-1396). This also drops an assistant turn that has only thinking text and nothing else.

Tool results (OC:1398-1466):
- All consecutive `toolResult` messages are handled in one run (OC:1402).
- Each becomes `{ role:"tool", content, tool_call_id }`. Text blocks are joined with `"\n"`. Empty text with images gives `(see attached image)`. Empty with no images gives `(no tool output)` (OC:1406-1419).
- `name` is added when `requiresToolResultName` and the result has a tool name (OC:1421-1423).
- Images are collected during the run. After the whole run, one user message is added: text `Attached image(s) from tool result:` plus all images (OC:1442-1459). If `requiresAssistantAfterToolResult`, a synthetic assistant `I have processed the tool results.` goes in first (OC:1443-1448).
- `requiresAssistantAfterToolResult` also inserts that synthetic assistant before any user message that follows tool results (OC:1233-1238).

`sanitizeSurrogates` strips lone UTF-16 surrogates (`utils/sanitize-unicode.ts:21-24`). It is applied to all outgoing text. It is not applied to tool-call JSON arguments.

### 1.8 SSE chunk parsing (OC:553-678)

Per chunk, in order:
1. `onProviderStreamEvent` hook (OC:554).
2. Skip if chunk is null or not an object (OC:555). Test: `openai-completions-tool-choice.test.ts:660`.
3. `responseId ||= chunk.id` (OC:559). `responseModel ||= chunk.model` if it differs from the requested id and is non-empty (OC:560-562). Test file: `openai-completions-response-model.test.ts`.
4. `if (chunk.usage) usage = parseChunkUsage(...)`. This runs before the choices check, so a final usage-only chunk with `choices: []` works (OC:563-568). Last usage chunk wins.
5. Only `choices[0]` is read (OC:567). No choice: continue.
6. Fallback: usage inside `choice.usage` when the chunk has no `usage` (OC:572-574, Moonshot).
7. `finish_reason` (when set): store `rawStopReason`, map the reason, set `hasFinishReason` (OC:576-584).
8. `delta.content`: only non-empty strings. Appends to the single text block (OC:587-600).
9. Reasoning delta: the first non-empty of `reasoning_content`, `reasoning`, `reasoning_text` (OC:606-615). This avoids double text when a provider sends two fields (chutes.ai comment, OC:602-605). The field name becomes the block `thinkingSignature` (OC:620-624). The signature is set once, at block creation.
10. `delta.tool_calls` (OC:635-663). Described next.
11. `delta.reasoning_details` array (OC:665-677). Each valid item creates (or reuses) a thinking block with empty signature, then merges into an in-memory list. Adjacent `reasoning.text` items merge into one. Adjacent `reasoning.summary` items merge. `reasoning.encrypted` items stay separate (OC:252-266). The list is written to `thinkingSignature` as JSON when the block is finished (OC:329-333, 440). It is not shown as a delta.

Block model. This matters for the Go event model:
- There is one text block, one thinking block, and one block per tool call, for the whole stream (OC:398-401).
- Blocks are never closed when the stream switches kind. All `*_end` events are sent at the end of the stream (OC:680-682, `finishBlock` OC:427-473).
- A second burst of text after a tool call appends to the first text block. Content index order is order of first appearance.

Tool-call delta handling (`ensureToolCallBlock`, OC:494-551):
- Match order: by `index` if it is a number; else by `id`. A match by index wins even if the id changes mid-stream (OC:497-500). Test: `openai-completions-tool-choice.test.ts:810-919` (id mutates, index stable, id stays the first one, one block, content index constant).
- New block: `id: toolCall.id || ""`, `name` from `function.name` or `custom.name`, args `{}`. A `toolcall_start` event is sent (OC:501-530).
- Late `id` or `name` is filled in if the block has none (OC:535-540, 638-645).
- Missing id: the block keeps `""`. Pi does not make an id. This is a gap. See section 6, item 14.
- `function.arguments` chunks are appended to `partialArgs`. `arguments` is re-parsed on every delta with `parseStreamingJson` (OC:647-651). That function tries strict parse, then `partial-json`, then a repair pass, then `{}` (`utils/json-parse.ts:65-83`).
- `custom.input` chunks (grammar tools) build a JSON string `{"<prop>":"..."}` through `appendGrammarToolInputJsonDelta` (OC:652-655; `constrained-sampling.ts:170-200`).
- At finish: parse `partialArgs` once more, then delete scratch fields `partialArgs`, `customInput`, `streamIndex` (OC:459-465).
- Test: empty `custom` object on a function delta is ignored (`openai-completions-tool-choice.test.ts:761`).

### 1.9 Usage and cost

`parseChunkUsage` (OC:1511-1552):
- `cacheRead = prompt_tokens_details.cached_tokens ?? prompt_cache_hit_tokens ?? cached_tokens ?? 0` (OC:1523-1524). Order: OpenAI/OpenRouter, then DeepSeek, then Kimi top-level.
- `cacheWrite = prompt_tokens_details.cache_write_tokens || 0` (OC:1525). Not an OpenAI field. OpenRouter sends it.
- `input = max(0, prompt_tokens - cacheRead - cacheWrite)` (OC:1538).
- `output = completion_tokens` (includes reasoning tokens, OC:1539-1540).
- `reasoning = completion_tokens_details.reasoning_tokens || 0` (OC:1546).
- `totalTokens = input + output + cacheRead + cacheWrite` (OC:1547). Pi ignores the API `total_tokens`.
- Cost: `calculateCost` (MD:1193-1211). Rate per million. A price tier is chosen when `input + cacheRead + cacheWrite` exceeds `tier.inputTokensAbove`. The highest matching tier wins. `cacheWrite1h` is not used on this API.
- Tests: `openai-completions-tool-choice.test.ts:1641` (no double count of reasoning), `:1676` and `:1717` (cache read/write from chunk and from choice usage).

### 1.10 Stop reason map (`mapStopReason`, OC:1554-1578)

| finish_reason | stopReason | errorMessage |
|---|---|---|
| `stop`, `end` | `stop` | none |
| `length` | `length` | none |
| `tool_calls`, `function_call` | `toolUse` | none |
| `content_filter` | `error` | `Provider finish_reason: content_filter` |
| `network_error` | `error` | `Provider finish_reason: network_error` |
| any other string | `error` | `Provider finish_reason: <value>` |
| `null` | `stop` (but null never reaches the map; only truthy values do, OC:576) | none |

End-of-stream rules (OC:683-700):
1. Aborted signal: throw `Request was aborted`.
2. If no `finish_reason` was seen and `supportsFinishReason === false`: stop reason is `toolUse` if any tool call exists, else `stop` (OC:690-692).
3. `stopReason === "error"`: throw with the stored message.
4. Missing `finish_reason` with `supportsFinishReason` true, or still `pending`: throw `Stream ended without finish_reason` (OC:696-698). Test: `openai-completions-tool-choice.test.ts:702`.
5. Else push `done`.

Raw finish reason is saved on `rawStopReason` (OC:577). Test: `openai-completions-raw-stop-reason.test.ts`.

### 1.11 Errors, abort, retries

- `catch` (OC:702-725): strips scratch fields on all blocks, writes pending `reasoning_details` into thinking blocks, sets `stopReason` to `aborted` if the signal is aborted, else `error`. Message from `formatProviderError(normalizeProviderError(error))` (`utils/error-body.ts:38-117`; body capped at 4000 chars). OpenRouter `error.metadata.raw` is appended when not already in the text (OC:719-721). Partial content is kept in the final message. An `error` event carries the whole message.
- Retries: SDK retries are off. Pi runs its own loop (`utils/provider-retry.ts:105-`). It retries on `x-should-retry: true`, no status, or status 408/409/429/>=500 (`provider-retry.ts:23-35`). Delay: `retry-after-ms`, then `retry-after`, else exponential 0.5s x 2^n, max 8s, with up to 25% jitter (`provider-retry.ts:51-67`). A server delay over `maxRetryDelayMs` (default 60 s) fails at once. Retries cover only the request that opens the stream, not the stream body. Default `maxRetries` is 0 (`provider-retry.ts:109`). Tests: `openai-completions-retry.test.ts`.
- Abort: the signal goes to the SDK (OC:366). Retry sleep is abortable (`provider-retry.ts:75-95`).
- Timeout: `options.timeoutMs` goes to the SDK (OC:367).

### 1.12 Client, headers, hooks

- Headers (OC:761-786): `User-Agent`, then `model.headers`, then Copilot dynamic headers (provider `github-copilot` only, OC:762-769), then session affinity, then caller headers last (they win).
- Session affinity (OC:771-781) needs `sessionId`, retention not `none`, and `sendSessionAffinityHeaders`. Format `openrouter`: `x-session-id`. Format `openai`: `session_id`, `x-client-request-id`, `x-session-affinity`. Format `openai-nosession`: the last two only.
- API key: if missing but an `Authorization` or `cf-aig-authorization` header exists, the key is `"unused"`. Otherwise throw `No API key for provider: <p>` (OC:82-86).
- Hooks: `onPayload` can replace the payload (OC:361-364). `onResponse` gets status and headers (OC:378). `onProviderStreamEvent` gets every raw chunk (OC:554).

## 2. The compat record

### 2.1 Fields, defaults, readers

"Runtime default" is `detectCompat` (OC:1585-1682). "Catalog" means the generator bakes a value into model data (see 2.2). Types: `TY:789-866`.

| Field | Runtime default | Read at | What it does |
|---|---|---|---|
| `supportsStore` | `!isNonStandard` | OC:833 | Send `store:false` |
| `supportsDeveloperRole` | OpenRouter `anthropic/` or `openai/` models: true; else `!isNonStandard && !isOpenRouter` | OC:1225 | `developer` vs `system` role (reasoning models only) |
| `supportsReasoningEffort` | false for xAI, z.ai, Moonshot, Together, CF AI Gateway, NVIDIA, Ant Ling; else true | OC:881,890,915,929,954,964,967 | Allow `reasoning_effort` |
| `supportsUsageInStreaming` | true | OC:829 | Send `stream_options.include_usage` (only `false` disables) |
| `supportsFinishReason` | true | OC:690,696 | When false, infer stop at stream end |
| `maxTokensField` | `max_tokens` for chutes, DeepSeek, Moonshot, CF AI Gateway, Together, NVIDIA, Ant Ling, z.ai; else `max_completion_tokens` | OC:838 | Which field name |
| `requiresToolResultName` | false | OC:1421 | Add `name` to tool messages |
| `requiresAssistantAfterToolResult` | false | OC:1233,1287,1443 | Synthetic assistant between tool results and user; `""` not `null` content |
| `requiresThinkingAsText` | false | OC:1315 | Replay thinking as text part |
| `requiresReasoningContentOnAssistantMessages` | DeepSeek only | OC:1378 | Force `reasoning_content: ""` on replay |
| `thinkingFormat` | DeepSeek `deepseek`, z.ai `zai`, Together `together`, Ant Ling `ant-ling`, OpenRouter `openrouter`, else `openai` | OC:875-972 | Table in 1.5 |
| `chatTemplateKwargs` | `{}` | OC:902 | Keys for `chat-template` |
| `chatTemplateArgs` | `{}` | OC:911 | Keys for `baseten` |
| `openRouterRouting` | `{}` | OC:983 (raw `model.compat`) | `provider` request field |
| `vercelGatewayRouting` | `{}` | OC:988 (raw `model.compat`) | `providerOptions.gateway` from `only`/`order` |
| `zaiToolStream` | false | OC:852 | `tool_stream:true` when tools exist |
| `supportsThinkingTokenBudget` | false | OC:1007 | Alias for field `thinking_token_budget` |
| `thinkingTokenBudgetField` | none | OC:1006 | Name of the budget field (wins over alias) |
| `supportsStrictMode` | false | OC:1497,1505 | Emit `strict` in function tools |
| `supportsOpenAIGrammarTools` | false | OC:339,1479 | Emit `custom` grammar tools |
| `supportsMidConvoSystemMessages` | false | OC:305,1191,1223 | Keep late system messages in place |
| `supportsMidConvoToolAdditions` | false | OC:810,1223 | Tool additions via system messages (needs the flag above) |
| `cacheControlFormat` | `anthropic` for provider `openrouter` with id `anthropic/...`; else none | OC:815,1073 | `cache_control` markers |
| `sendSessionAffinityHeaders` | OpenRouter only | OC:771 | Send affinity headers |
| `sessionAffinityFormat` | `openrouter` for OpenRouter, else `openai` | OC:772-779 | Header shape |
| `supportsLongCacheRetention` | false for Together, CF Workers AI, CF AI Gateway, NVIDIA, Ant Ling; else true | OC:823-826,1077 | `24h` retention or `ttl:"1h"` |
| `vllmPriority` | none | OC:868 | `priority` field |

`isNonStandard` (OC:1605-1619) is true for: NVIDIA, Cerebras, xAI, Together, chutes.ai, DeepSeek, z.ai, Moonshot, opencode (provider or `opencode.ai`), CF Workers AI, CF AI Gateway, Ant Ling.

`cacheControlFormat` markers (OC:1081-1183): on the first system/developer message, on the last tool, and on the last user/assistant/tool message that has text. A string content becomes a one-element text-part array. TTL `1h` only when retention is `long` and `supportsLongCacheRetention`. Not applied when retention is `none`. Tests: `openai-completions-cache-control-format.test.ts` (four cases).

### 2.2 How compat is resolved

- Runtime (`getCompat`, OC:1688-1726): `detectCompat(model)` gives a full record. Each field in `model.compat` that is not undefined replaces the detected value. There is no deep merge for `chatTemplateKwargs`: the model value replaces the whole map.
- Two exceptions in `getCompat`: `openRouterRouting` falls back to `{}`, not to the detected value (OC:1707). `vllmPriority` is read only from `model.compat` (OC:1724).
- `buildParams` reads `model.compat?.openRouterRouting` and `model.compat?.vercelGatewayRouting` directly, not from the resolved record (OC:983, 988). Same result in practice.
- Build time: the generator has its own copy of the detection (`GEN:690-790`). It writes only the fields that differ from a fixed default table (`GEN:650-673`, `openAICompletionsCompatDelta` `GEN:792-802`) into each model's `compat`. `applyOpenAICompletionsCompatMetadata` does this (`GEN:808-815`). The explicit model values win over detected ones.
- So the shipped catalog stores most compat values as model data. The URL and provider detection stays at runtime as the fallback for user-defined models.
- The two detection copies are not the same:
  - Strict mode: runtime default false (OC:1667). Generator bakes `true` for all providers except Moonshot, Together, CF AI Gateway, NVIDIA, Cerebras (`GEN:772`). Test `openai-completions-tool-choice.test.ts:222` confirms unknown endpoints get non-strict, and `:262` confirms built-in capable models keep strict.
  - Together: generator sets `thinkingFormat` to `openai` for reasoning-only models (`GEN:759`). Runtime does not know that list.
  - OpenRouter cache control: generator regex is `^~?anthropic/` (`GEN:741`). Runtime test is `startsWith("anthropic/")` (OC:1634).
  - `sessionAffinityFormat` is detected at runtime only (OC:1673).

Conflict with the inventory row. `inventory-harness.md:99` says "Per-model compat record as data, never URL heuristics". Pi uses both. Ask should ship data for catalog models, and may keep a small detection only for user endpoints. This is a decision for the user. See Unresolved questions.

### 2.3 Which providers and models use which compat (from generator code)

Providers not listed use runtime detection only (with the generator baking the delta).

| Provider (catalog id) | Compat set by generator | Source |
|---|---|---|
| `zai`, `zai-coding-cn` | `supportsDeveloperRole:false`, `thinkingFormat:"zai"`, `supportsReasoningEffort:true` when a level map exists, `zaiToolStream:true` except for a deny list | `GEN:1477-1482` |
| `together` | base: no store, no developer role, no effort, `max_tokens`, no strict, no long retention. Variants: `thinkingFormat:"together"` (toggle), `openai` + effort (gpt-oss-20b/120b), toggle + effort (DeepSeek-V4-Pro-0813), reasoning-only models (R1, MiniMax-M2.7) use base | `GEN:175-199`, `GEN:554-560` |
| `nvidia` | no store, no developer role, no effort, `max_tokens`, no strict, no long retention | `GEN:235-242` |
| `baseten` | base: no store, no developer, no effort, `max_tokens`, strict true, affinity headers, no long retention. Variants: `thinkingFormat:"openai"` + effort; `thinkingFormat:"baseten"` with `chatTemplateArgs:{enable_thinking:{$var:"thinking.enabled"}}` (toggle); both | `GEN:1497-1525` |
| `fireworks` (GLM models, completions) | `supportsStore:false`, `supportsDeveloperRole:false`, affinity headers, no long retention | `GEN:1681-1686` |
| `fireworks` (kimi-k3) | above plus `requiresReasoningContentOnAssistantMessages:true`, `thinkingFormat:"openai"`; also mid-convo system + tool additions | `GEN:1687-1690`, `GEN:912-935` |
| `groq` | no compat. One level map for `qwen/qwen3.6-27b`: minimal/low/medium null, high `"default"` | `GEN:1130-1132` |
| `cerebras` | none baked. Runtime detection: no store, no developer role, strict off | `GEN:1889-1908`; OC:1605 |
| `cloudflare-workers-ai` | `sendSessionAffinityHeaders:true` | `GEN:1938` |
| `cloudflare-ai-gateway` (completions models) | affinity headers for anthropic and workers-ai upstreams | `GEN:1976-1982` |
| `huggingface` | `supportsDeveloperRole:false` | `GEN:2144-2146` |
| `github-copilot` (completions models) | `supportsStore:false`, `supportsDeveloperRole:false`, `supportsReasoningEffort:false` | `GEN:2381-2387` |
| `opencode`, `opencode-go` (completions) | `maxTokensField:"max_tokens"`; npm `@ai-sdk/alibaba` gives `cacheControlFormat:"anthropic"`; `grok-build-0.1` no effort; `kimi-k2.6` `thinkingFormat:"deepseek"` + no effort; opencode-go `qwen3.5-plus`/`qwen3.6-plus` `thinkingFormat:"qwen"`; some models no long retention | `GEN:2264-2308` |
| `moonshotai`, `moonshotai-cn` | no store, no developer role, no effort, `max_tokens`, no strict, `thinkingFormat:"deepseek"`. `kimi-k3`: `requiresReasoningContentOnAssistantMessages:true`, `thinkingFormat:"openai"`, effort on | `GEN:2481-2509` |
| `xiaomi`, `xiaomi-token-plan-*` | `requiresReasoningContentOnAssistantMessages:true`, `thinkingFormat:"deepseek"` | `GEN:2535-2538` |
| `qwen-token-plan*` | `thinkingFormat:"qwen"`, no developer role, no store, effort on only when a level map exists | `GEN:2594-2599`, `2643-2644` |
| `deepseek` | `requiresReasoningContentOnAssistantMessages:true`, `thinkingFormat:"deepseek"`. Any `deepseek-v4*` completions model on other providers gets the same, except OpenRouter and opencode, which get only the reasoning-content flag | `GEN:3097-3141`, `GEN:3194-3210` |
| `ant-ling` | no store, no developer role, no effort, `max_tokens`, no long retention. One model uses `thinkingFormat:"ant-ling"` | `GEN:3144-3189` |
| `openrouter` | runtime detection (`thinkingFormat:"openrouter"`, affinity headers, `cacheControlFormat` for Anthropic ids). `moonshotai/kimi-k2.6*` adds no developer role + reasoning-content flag. `inception/mercury-2*` maps `off:null`. `z-ai/glm-5.2` maps `xhigh` | `GEN:2973-2978`, `GEN:1145-1153` |
| Mid-convo system messages | Moonshot kimi-k2.6/k2.7-code(-highspeed), copilot kimi-k3, deepseek-v4-pro, some `openrouter openai/*` ids: `supportsMidConvoSystemMessages:true`. Kimi K3 also `supportsMidConvoToolAdditions:true` | `GEN:905-935` |
| Grammar tools | Generator enables only for Responses APIs, so completions get `false` unless a user sets it | `GEN:885-889` |
| Mistral | Not this API. It has its own adapter `mistral-conversations` | `GEN:2106` |
| xAI | The catalog uses Responses compat (`GEN:2053`). Completions detection for xAI applies to user endpoints only | |

Level-map overrides that touch completions models: `deepseek-v4*` maps (`GEN:1115-1128`), Kimi K2.7 Code `off:null` (`GEN:1137-1143`).

## 3. Tool-call id normalization and `transformMessages`

Callback passed from `convertMessages` (OC:1194-1218, 1220):

1. If the id contains `|` (OpenAI Responses form `call_id|item_id`, up to 400+ chars with `+ / =`): replace every char outside `[a-zA-Z0-9_-]` with `_` in both parts. Join as `<callId>_<itemId>` (or just `<callId>` if the item part is empty). If that is 40 chars or shorter, use it. Otherwise use `<callId cut to 31 chars>_<8-char hash>` where the hash is `shortHash(originalId).slice(0, 8)` (OC:1202-1213). `shortHash` is a custom 2x32-bit mix printed in base 36 (`utils/hash.ts:1-13`). It does not need byte-for-byte parity in Go. It only needs to be deterministic and give unique ids per run.
2. Else, if `model.provider === "openai"`: cut to 40 chars (OC:1216).
3. Else: unchanged (OC:1217).

When it runs (TM:127-145): only for tool calls in assistant messages from a different model (`provider`, `api`, or `model` differs). Same-model ids are never touched. The old-to-new id map is applied to later `toolResult` messages (TM:83-90). The map is built in message order, so results must come after their call.

Per-vendor rules:
- There is no Mistral rule in this API. The 9-char Mistral rule lives in `mistral-conversations.ts:28, 141-143, 261-266`. That belongs to a later adapter.
- There is no explicit Anthropic rule here. Anthropic ids (`toolu_...`, charset `[a-zA-Z0-9_-]`, max 64) pass through unchanged to non-OpenAI vendors.
- The other direction (completions to Anthropic) is handled by the Anthropic adapter's own callback. I did not read it.
- Tests: `tool-call-id-normalization.test.ts` (live, skipped without keys; plus a prefilled-context case at line 183 with a failing pipe id), `cross-provider-handoff.test.ts`, `transform-messages-copilot-openai-to-anthropic.test.ts`.

`transformMessages` rules that decide replay (TM:64-235). These are the core of the H4 exit:
1. `isSameModel` = same `provider`, `api`, and `model` id (TM:95-98).
2. Thinking blocks (TM:101-117): redacted blocks are kept only for the same model. Same model with a signature: kept even if text is empty. Blank text: dropped. Same model without signature: kept. Other model: converted to a plain `text` block with the thinking text and no tags.
3. Text blocks from another model are kept as plain text (TM:119-125).
4. Tool calls from another model lose `thoughtSignature` (TM:131-134) and get the id callback.
5. Assistant turns with `stopReason` `error` or `aborted` are dropped (TM:195-203).
6. Orphan tool calls get a synthetic result: text `No result provided`, `isError: true` (TM:167-180). This happens before the next assistant or user turn and at the end.
7. System messages that appear between a call and its results are held and emitted after the results (TM:163-166, 216-221).
8. Images are replaced with a placeholder text when `model.input` has no `image` (TM:35-57).
9. Null `content` is changed to `[]` (TM:73).

For this API the "signature" of a thinking block is not a crypto signature. It is the name of the reasoning field (`reasoning_content`, `reasoning`, `reasoning_text`) or a JSON array of `reasoning_details` (OC:620-624, 329-333).

## 4. Pi tests for this adapter (`packages/ai/test/`)

"Body" means I read the test code. "Title" means I only read the test names.

| File | Basis | Asserts |
|---|---|---|
| `openai-completions-tool-choice.test.ts` (1956 lines, 57 tests) | Body for ~10, title for rest | `toolChoice` goes into the payload, also with no tools. `strict` omitted when compat disables it; unknown endpoints non-strict; built-in capable models strict. Groq Qwen effort map. z.ai: `tool_stream` on/off/override, effort metadata, GLM-5.2 level map, `thinking` kept on `reasoning_content` replay. Non-standard `finish_reason` gives error (`network_error` exact message). Null chunks ignored. Stream with only null finish reasons errors with `Stream ended without finish_reason`. No-finish stream accepted when compat disables it. Empty `custom` object on function delta ignored. Tool-call deltas merge by index when ids change (content index stays 0; id stays first; no scratch fields). Mixed content, reasoning and parallel tool deltas stay separate. Developer vs system role for OpenRouter and OpenAI reasoning models. Kimi K2.6, Xiaomi, Qwen Token Plan compat in catalog. Xiaomi replay gets `reasoning_content:""` and `thinking:{type:"enabled"}`. OpenCode Go reasoning field becomes `reasoning_content` on stream and replay; other providers keep the original field. OpenCode Kimi K2.6 thinking on/off; Moonshot K2.7 Code omits disabled thinking; K2.6 keeps it. `max_tokens` for OpenCode, DeepSeek, z.ai. Grok Build no effort. Reasoning tokens not double counted. Cache read/write from chunk usage and from `choice.usage`. OpenRouter uses `reasoning` object, not `reasoning_effort`. Chat-template kwargs: boolean, Qwen template, effort with static keys. Ant Ling mapping and omission. |
| `openai-completions-thinking-token-budget.test.ts` | Title | Budget per level; omitted without field or alias; omitted when off; `xhigh`/`max` use high budget; answer room kept; caller `max_tokens` as ceiling; the three field names; field beats alias; `$var thinking.budget` in `chat_template_kwargs`; dropped when off |
| `openai-completions-reasoning-details.test.ts` | Title | `reasoning_details` kept in the signature; legacy encrypted tool-call signature fallback; signed text and summary order kept; adjacent text/summary deltas merged |
| `openai-completions-thinking-as-text.test.ts` | Title | Same-model thinking plus text replay as text parts; thinking-only replay; request reaches endpoint |
| `openai-completions-tool-result-images.test.ts` | Title | Empty text parts dropped from user image messages; tool-result images batched after consecutive results; `(no tool output)` placeholder |
| `openai-completions-empty-tools.test.ts` | Title | `tools` omitted for empty or undefined; default and explicit `maxTokens`; both clamped to context; CF AI Gateway conservative fields and auth; Workers AI session affinity; `tools: []` with tool history |
| `openai-completions-prompt-cache.test.ts` | Title | `prompt_cache_key` for direct OpenAI; 24h retention; 64-char clamp; none disables; non-OpenAI URLs omit; `PI_CACHE_RETENTION`; affinity headers per format; Fireworks, Baseten, OpenRouter defaults; header override |
| `openai-completions-cache-control-format.test.ts` | Title | Anthropic-style markers on system, last tool, last message; OpenRouter Anthropic aliases; marker moves to tool result; none when retention is `none` |
| `openai-completions-provider-stream-event.test.ts` | Title | Raw provider chunks (with OpenRouter metadata) reach the hook |
| `openai-completions-raw-stop-reason.test.ts` | Title | `rawStopReason` kept for stop and error |
| `openai-completions-response-model.test.ts` | Title | Routed `chunk.model` goes to `responseModel`; unset when equal, empty, or missing |
| `openai-completions-retry.test.ts` | Title | SDK retries off by default; Pi retries honored; long server delay fails fast |
| `openai-completions-vllm-priority.test.ts` | Title | `priority` sent from compat; omitted otherwise |
| Other files that touch this API | Title | `tool-call-id-normalization`, `cross-provider-handoff`, `transform-messages-copilot-openai-to-anthropic`, `abort`, `max-thinking`, `image-tool-result`, `tool-call-without-result`, `total-tokens`, `tokens`, `sampling-options`, `cache-retention`, `context-overflow`, `fireworks-models`, `together-models`, `zai-coding-plan-models`, `openrouter-*`, `model-data-validation` |

Most tests mock the OpenAI SDK and assert on the payload or on the final message. Live handoff tests skip without keys.

## 5. Changes between `2bbfcca4` and `4c6fb7cfe`

Command: `git log -p 2bbfcca4..4c6fb7cfe -- packages/ai/src/api/openai-completions.ts packages/ai/src/types.ts`.

- `openai-completions.ts`: no change. `git diff --stat` for the file is empty.
- `types.ts`: one commit, `b271b0a52` ("define Anthropic mid-conversation tools inline"). It changes one doc comment on `AnthropicMessagesCompat.supportsMidConvoToolChanges` (TY:946). `OpenAICompletionsCompat` is unchanged.
- Nearby files in the range (24 commits in `packages/ai` in total): `utils/transcript.ts` deprecates `hasToolRedefinitions` (comment and tag only). `scripts/generate-models.ts` changes the Together DeepSeek V4 Pro id to `DeepSeek-V4-Pro-0813` (`GEN:` set `TOGETHER_TOGGLE_REASONING_EFFORT_MODELS`), plus Bedrock pricing tiers and a Cloudflare Claude id. None changes completions behavior.
- Conclusion: reading the `4c6fb7cfe` file is the same as reading the pinned `2bbfcca4` file for this adapter.

## 6. Edge cases and sub-features a Go port must keep

Ranking basis: H4 exit is "one in-memory conversation switches between Anthropic and OpenAI with correct thinking replay" (`roadmap.md:222-243`).

P0 = needed for H4 exit. Later = keep, but not for the exit.

| # | Behavior | Ref | Priority |
|---|---|---|---|
| 1 | Compat record as per-model data, with a resolve step (model value over default, field by field) | OC:1688-1726 | P0 |
| 2 | Wire shape: roles, user text and `image_url` data URLs, assistant string content, `tool_calls` with JSON-string args, `tool` messages with `tool_call_id` | OC:1253-1466 | P0 |
| 3 | Tool-result text join, placeholders `(see attached image)` and `(no tool output)`; tool-result images moved to one following user message | OC:1406-1459 | P0 |
| 4 | Skip assistant messages with no content and no tool calls (also drops thinking-only turns) | OC:1385-1396 | P0 |
| 5 | Drop blank text blocks; blank thinking is not replayed | OC:1290-1313 | P0 |
| 6 | `transformMessages` replay rules: same-model test, cross-model thinking to plain text, drop errored/aborted turns, synthetic result for orphan calls, held system messages, image placeholder | TM:64-235 | P0 (H3 owns the function; H4 adds this API's callback and tests) |
| 7 | Reasoning replay by field name: store the field name on the thinking block; on replay write all thinking texts joined with `"\n"` into that field; do not use text parts | OC:620-624, 1332-1341 | P0 |
| 8 | Reasoning delta source: first non-empty of `reasoning_content`, `reasoning`, `reasoning_text` | OC:606-615 | P0 |
| 9 | Single text block and single thinking block per stream; `*_end` events at stream end; tool blocks matched by index first, then id | OC:398-551, 680 | P0 |
| 10 | Tool-call id normalization callback (pipe form, 40-char cap for provider `openai`) for foreign-model calls only | OC:1194-1218 | P0 |
| 11 | `finish_reason` map; `Stream ended without finish_reason` error; `supportsFinishReason:false` inference | OC:683-700, 1554-1578 | P0 |
| 12 | `stream_options.include_usage`; usage in a chunk with empty `choices`; last usage wins; fallback to `choice.usage` | OC:563-574, 829 | P0 |
| 13 | Usage math (`input = prompt - read - write`, three cache-read field names, `cache_write_tokens`, reasoning tokens, computed total) and cost with price tiers | OC:1511-1552; MD:1193-1211 | P0 |
| 14 | Missing tool-call id: Pi leaves `""`. Ask must make a stable id, or tool results cannot match. This is a gap in Pi, not behavior to copy | OC:509 | P0 (Ask-side fix) |
| 15 | `maxTokensField` switch and the context clamp (4096 safety margin) | OC:837-844; SO:15-19 | P0 |
| 16 | `reasoning_effort` format (`openai`) with `thinkingLevelMap` and the off value; `supportsReasoningEffort` gate; level clamp | OC:964-972; MD:1217-1247 | P0 |
| 17 | Abort and error finalization: partial blocks kept, scratch fields removed, `aborted` vs `error`, error body text with cap | OC:702-725 | P0 |
| 18 | `developer` role switch (`model.reasoning && supportsDeveloperRole`) | OC:1225 | P0 (OpenAI reasoning models fail or warn without it) |
| 19 | `requiresReasoningContentOnAssistantMessages` (`reasoning_content:""` on every replayed assistant message) | OC:1378-1384 | P0 only if the exit includes DeepSeek, Kimi, Xiaomi; else later |
| 20 | `supportsStore`, `supportsUsageInStreaming` flags | OC:829-835 | P0 (cheap, same code path) |
| 21 | Other thinking formats: `openrouter`, `deepseek`, `zai`, `qwen`, `qwen-chat-template`, `chat-template`, `baseten`, `together`, `string-thinking`, `ant-ling` | OC:875-963 | Later (`openrouter` first, it is the largest provider) |
| 22 | Thinking token budget field and clamp | OC:978, 1004-1024 | Later |
| 23 | `requiresThinkingAsText`, `requiresAssistantAfterToolResult`, `requiresToolResultName` | OC:1233, 1315, 1421 | Later |
| 24 | `reasoning_details` stream merge and replay (OpenRouter) | OC:215-266, 665-677, 1304-1311, 1375 | Later (needed once OpenRouter reasoning models are in scope) |
| 25 | `opencode-go` reasoning-field rewrite | OC:620-623, 1335 | Later |
| 26 | `tools: []` when history has tools but none are declared | OC:855-858 | Later (proxy quirk) |
| 27 | Strict tool schema conversion and `strict` field | OC:1497-1505; `constrained-sampling.ts:56-124` | Later |
| 28 | Grammar (custom) tools and custom-input streaming | OC:339, 405-426, 1354-1364, 1479-1495 | Later |
| 29 | Anthropic-style `cache_control` markers | OC:1069-1183 | Later |
| 30 | `prompt_cache_key`, `prompt_cache_retention`, session affinity headers | OC:771-781, 821-826 | Later |
| 31 | `zaiToolStream`, `vllmPriority`, `openRouterRouting`, `vercelGatewayRouting` | OC:852, 868, 983-996 | Later |
| 32 | Mid-conversation system messages and Kimi `system.tools` | OC:1240-1248 | Later |
| 33 | `samplingParams` override map (model, then request) | OC:998-999 | Later |
| 34 | `responseId`, `responseModel`, `rawStopReason`; raw-chunk and payload hooks | OC:559-562, 577, 361, 554 | Later |
| 35 | Retry loop and `Retry-After` handling | `provider-retry.ts` | Later (fantasy has `retry.go`; check overlap before porting) |
| 36 | Copilot dynamic headers; Cloudflare gateway auth | OC:762-769 | Later |
| 37 | OpenRouter `error.metadata.raw` appended to error text | OC:719-721 | Later |
| 38 | Strip lone surrogates. In Go: replace invalid UTF-8 in outgoing strings | `sanitize-unicode.ts` | P0 (a bad byte breaks the request) |

Pi quirks that Ask should decide on, not copy blindly:
- `null` in `thinkingLevelMap` falls back to the raw level in five of the formats (1.5).
- Only `choices[0]` is read (OC:567). Fine for agent use. Note it.
- `arguments` is re-parsed on every delta (OC:651). Cost grows with argument size. In Go, parse once at the end and use a partial parser only if the UI needs live args.
- A tool-result run is closed by the first non-`toolResult` message. Empty-skipped user or assistant messages leave `lastRole` stale for the `requiresAssistantAfterToolResult` check (OC:1277, 1396 `continue` before OC:1468). Low risk.

## 7. Fit with D22 (fantasy v0.45.2 behind one Ask adapter)

This is a short check, not a full audit. I grepped `providers/openaicompat` in `/Users/dale/Desktop/workspace/go/mobules/pkg/mod/charm.land/fantasy@v0.45.2/`.

- fantasy sets `stream_options.include_usage` (`providers/openai/language_model.go:462, 1037`).
- fantasy maps `reasoning_effort` including `xhigh` and `max` (`providers/openaicompat/language_model_hooks.go:48-63`).
- fantasy replays reasoning through "extra fields" (`language_model_hooks.go:27-32`; `replay_test.go`). I did not check which field names it writes or reads.
- I found no sign of the other thinking formats, `max_tokens` vs `max_completion_tokens`, the 40-char id rule, or `reasoning_details`. I did not read the provider code end to end. Treat this as "not found by grep", not as "absent".
- Likely split: Ask adapter owns the compat record, thinking params, id callback and replay mapping. fantasy owns HTTP, SSE, and basic message shapes. A thin "request mutator" hook is the likely seam for extra top-level body fields (`thinking`, `enable_thinking`, `chat_template_kwargs`, budget field, `provider`). Check that fantasy exposes one before H4 starts.

## Unresolved questions

1. Which "OpenAI" does the H4 exit mean: `openai-completions` or `openai-responses`? OpenAI's own Chat Completions endpoint does not return reasoning text (my background knowledge, not checked in Pi). Real "thinking replay" with direct OpenAI is a Responses API matter. For completions, thinking replay matters for vendors that send `reasoning_content` or `reasoning` (DeepSeek, Kimi, llama.cpp, OpenRouter). This changes which P0 rows (7, 8, 19, 24) are truly needed.
2. Inventory row H-PROV-03 says "never URL heuristics". Pi keeps a URL/provider detection fallback for user endpoints. Keep a small detection for custom endpoints, or require an explicit compat record for them?
3. Does fantasy v0.45.2 expose a hook to add arbitrary top-level request fields and to read non-standard delta fields (`reasoning_content`, `reasoning`, `reasoning_text`, `reasoning_details`, `choice.usage`)? Not verified.
4. The catalog data files are missing from the Pi checkout. Per-model values (thinking level maps, exact compat deltas) need the hydrated catalog or a run of the generator. Where will Ask get its model data?
5. I did not read the Anthropic adapter's id callback, so the completions-to-Anthropic direction of id normalization is unverified.
6. GitNexus was not reachable from this session. Call graph is grep-based.

Status: DONE_WITH_CONCERNS
Summary: Full report written. Pi `openai-completions` is unchanged between the pin and v1.0.1. Concerns: GitNexus unavailable (grep used), catalog data files not in the checkout (compat per provider taken from generator code), and the H4 exit wording ("OpenAI") needs a decision.
Concerns/Blockers: see Unresolved questions 1-4.
