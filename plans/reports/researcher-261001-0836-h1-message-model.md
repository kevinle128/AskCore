# Pi message model: scout for roadmap phase H1

Date: 2026-10-01. Source: Pi commit 2bbfcca43 (read only). Scope: H-SESS-04, H-SESS-05, H-RETRY-07 (type only).
Shorthand: AI: = packages/ai/src/, A: = packages/agent/src/, C: = packages/coding-agent/src/, CD: = packages/coding-agent/docs/. CL = CHANGELOG.md of that package.
Spiral rings 0 to 5 were run. Ring 5 (history) found only role migration and the 0.86.0 system-message change; nothing new after that.

## 0. Outcome

H1 can use the inventory rows, but three rows are wrong or incomplete. Fix them before H1 starts:

1. H-SESS-05 says assistant content has an `image` block. It does not. Assistant content is `text | thinking | toolCall` only (`AI:types.ts:548`). `image` exists only in user, toolResult and custom content.
2. H-SESS-04 says "`convertToLlm` is the only mapping to model roles". This is incomplete. There are two stages. Stage 1 is `convertToLlm` (agent roles to LLM roles). Stage 2 is `transformMessages` inside each provider adapter (`AI:api/transform-messages.ts:64`). Stage 2 drops errored and aborted assistant messages, converts foreign thinking to text, and inserts synthetic tool results. Stage 2 is not in the inventory sources.
3. `SystemMessage` is a full message role with `sections`, `toolsAdded`, `toolsRemoved` (0.86.0). `Context` is only an input shorthand. Providers receive `TranscriptContext`, not `Context`. The inventory does not say this.

Also: the doc `CD:message-types.md:116` lists `SystemMessage.replace?: boolean`. No code reads or writes it (grep of `ai/src`, `agent/src`, `coding-agent/src` found no `replace?: boolean`; `AI:utils/transcript.ts:73-96` does not handle it). Treat the doc as wrong. Do not port `replace`.

## 1. Feature tree

1. Message union (`Message` = system | user | assistant | toolResult) `AI:types.ts:610`
   1. SystemMessage with prompt deltas `AI:types.ts:522-538`
      1. `content` string or TextContent[]
      2. `sections` map, `null` removes a section `:532`
      3. `toolsAdded`, `toolsRemoved` `:534-536`
      4. Replay helpers: `getCurrentSystemMessage`, `getCurrentTools`, `getCurrentSystemPrompt`, `collapseSystemMessages`, `resolveTranscript`, `resolveTranscriptTools` `AI:utils/transcript.ts:58-234`
      5. Render helpers: `contentText`, `getSystemMessageText`, `renderSystemMessageUpdate` `AI:utils/text.ts:6-40`
   2. UserMessage `AI:types.ts:540-544`
   3. AssistantMessage `AI:types.ts:546-570`
      1. Identity: api, provider, model, responseModel?, responseId?
      2. Thinking record: thinkingLevel?, providerThinkingLevel?
      3. Result: usage, stopReason, errorMessage?, rawStopReason?, endTurn?, deferred?
      4. diagnostics? (redacted runtime/provider notes) `AI:utils/diagnostics.ts:9-14`
   4. ToolResultMessage `AI:types.ts:594-608` (with usage?, nestedCalls?)
2. Content blocks
   1. TextContent (+textSignature, +TextSignatureV1) `AI:types.ts:389-399`
   2. ThinkingContent (+thinkingSignature, +redacted) `AI:types.ts:401-409`
   3. ImageContent `AI:types.ts:411-415`
   4. ToolCall (+thoughtSignature, +namespace) `AI:types.ts:417-425`
3. Usage and cost record `AI:types.ts:427-448`; calculation `AI:models.ts:1193-1213` (later phase H9)
4. Request-side types
   1. Tool declaration `AI:types.ts:715-720`, ToolReference `:722-724`
   2. Context (input shorthand) `AI:types.ts:732-736`
   3. TranscriptContext (provider input, branded) `AI:types.ts:738-749`, built only by `normalizeContext` `AI:utils/transcript.ts:30-34`
5. Stream event protocol `AssistantMessageEvent` `AI:types.ts:767-783`; stream function contract `:360-375`
6. Persistable stream frames (0.85.0) `AI:utils/assistant-message-frame.ts:6-33`, encoder `:140+`, reducer `:233+`
7. Agent layer
   1. `AgentMessage = Message | CustomAgentMessages[keyof ...]` `A:types.ts:365-374`
   2. Agent events (10 types) `A:types.ts:514-525`
   3. `transformContext` then `convertToLlm` `A:types.ts:197-244`, call site `A:agent-loop.ts:388-399`
   4. Default `convertToLlm` (role filter only) `A:agent.ts:38-46`
   5. Tool result creation `A:agent-loop.ts:922-935`
   6. Synthetic failure message `A:agent.ts:528-548`
8. Coding-agent roles `C:core/messages.ts`
   1. bashExecution `:29-40`, text form `:82-98`
   2. custom `:46-53`
   3. branchSummary `:55-60`, prefix/suffix `:19-24`
   4. compactionSummary `:62-67`, prefix/suffix `:11-17`
   5. `convertToLlm` `:148-196`
   6. Declaration merging `:70-77`
9. Provider-side message transform `AI:api/transform-messages.ts:64-235`
10. Unicode safety `AI:utils/sanitize-unicode.ts:21-25`
11. Persistence rules (message entries) `C:core/agent-session.ts:1096-1125`, `C:core/session-manager.ts:64-67,277-284,439-450`

## 2. Field-level types (TypeScript shape, from source)

### 2.1 Content blocks

```ts
interface TextSignatureV1 { v: 1; id: string; phase?: "commentary" | "final_answer" }   // AI:types.ts:389-393
interface TextContent     { type: "text"; text: string; textSignature?: string }         // :395-399; signature = legacy id string or TextSignatureV1 as JSON string
interface ThinkingContent { type: "thinking"; thinking: string; thinkingSignature?: string; redacted?: boolean } // :401-409
interface ImageContent    { type: "image"; data: string /* base64 */; mimeType: string } // :411-415
interface ToolCall        { type: "toolCall"; id: string; name: string; arguments: JsonObject;
                            thoughtSignature?: string; namespace?: string }              // :417-425
type JsonValue  = null | boolean | number | string | readonly JsonValue[] | JsonObject   // :452
type JsonObject = { [key: string]: JsonValue }                                           // :453
```

Notes:
- When `redacted` is true, `thinkingSignature` holds the opaque encrypted payload (`:405-408`). Visible `thinking` can be empty.
- `textSignature` is OpenAI Responses metadata (`id`, `phase`) encoded for replay (CL ai 0.56.2).
- `ToolCall.arguments` is a JSON object. The doc says `Record<string, any>` (`CD:message-types.md:73`); the code type is stricter.

### 2.2 Messages

```ts
interface SystemMessage {                                   // AI:types.ts:522-538
  role: "system";
  content: string | TextContent[];
  sections?: Record<string, string | null>;                 // null removes the named section
  toolsAdded?: Tool[];
  toolsRemoved?: ToolReference[];
  timestamp: number;                                        // unix ms
}
interface UserMessage {                                     // :540-544
  role: "user";
  content: string | (TextContent | ImageContent)[];
  timestamp: number;
}
interface AssistantMessage {                                // :546-570
  role: "assistant";
  content: (TextContent | ThinkingContent | ToolCall)[];    // no image
  api: Api;                    // string id, for example "anthropic-messages"
  provider: ProviderId;
  model: string;               // requested model id
  responseModel?: string;      // concrete model when different (CL ai 0.71.0)
  responseId?: string;
  providerThinkingLevel?: string;      // exact provider-native effort
  thinkingLevel?: ModelThinkingLevel;  // "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max"  (:85-86)
  diagnostics?: AssistantMessageDiagnostic[];
  usage: Usage;
  stopReason: StopReason;
  deferred?: DeferredHandle;
  errorMessage?: string;
  rawStopReason?: string;      // CL ai 0.83.0
  endTurn?: boolean;           // debug only; no control-flow effect (:564-568); CL ai 0.84.2
  timestamp: number;
}
type StopReason = "pending" | "stop" | "length" | "toolUse" | "error" | "aborted" | "deferred"; // :450
interface ToolResultMessage<TDetails = JsonValue> {         // :594-608
  role: "toolResult";
  toolCallId: string;
  toolName: string;
  content: (TextContent | ImageContent)[];
  details?: JsonRepresentation<TDetails>;   // JSON-safe by type; tool specific
  usage?: Usage;              // tool's own model work; not in main context accounting
  nestedCalls?: NestedToolCalls;  // kept in session; not sent to model
  isError: boolean;
  timestamp: number;
}
type Message = SystemMessage | UserMessage | AssistantMessage | ToolResultMessage;  // :610
```

Supporting records:

```ts
interface DeferredHandle { provider: string; modelId: string; api: string; id: string;
  expiresAt?: number; pollAfterMs?: number; data?: JsonValue }                      // :500-510
interface AssistantMessageDiagnostic { type: string; timestamp: number;
  error?: DiagnosticErrorInfo; details?: JsonObject }                               // AI:utils/diagnostics.ts:9-14
interface DiagnosticErrorInfo { name?: string; message: string; stack?: string; code?: string | number } // :3-7
interface NestedToolCallRecord { id: string; name: string; arguments?: JsonObject; argumentsBytes?: number;
  status: "ok" | "error" | "unfinished"; durationMs?: number; error?: string }      // AI:types.ts:573-585
interface NestedToolCalls { calls: NestedToolCallRecord[]; complete: boolean }      // :588-592
```

### 2.3 Usage

```ts
interface Usage {                                           // AI:types.ts:427-448
  input: number; output: number; cacheRead: number; cacheWrite: number;
  cacheWrite1h?: number;     // subset of cacheWrite; only Anthropic reports the split
  reasoning?: number;        // subset of output; undefined if provider has no breakdown
  totalTokens: number;
  cost: { input: number; output: number; cacheRead: number; cacheWrite: number; total: number }; // USD floats
}
```

How providers fill it (the type is shared, the semantics differ):
- Anthropic: `input`, `output`, `cacheRead`, `cacheWrite`, `cacheWrite1h` from `message_start` (`AI:api/anthropic-messages.ts:622-626`). `totalTokens = input + output + cacheRead + cacheWrite` (`:628-629`). `message_delta` overwrites only non-null fields, because some proxies omit usage (`:768-798`, CL ai 0.80.7). `reasoning` from `output_tokens_details.thinking_tokens` (`:791-793`).
- OpenAI-compatible: `input = max(0, prompt_tokens - cacheRead - cacheWrite)`; `output = completion_tokens` (already includes reasoning); `reasoning` always set (0 if absent) (`AI:api/openai-completions.ts:1511-1552`). Cached tokens come from three vendor fields (`:1523-1525`).
- `totalTokens` is computed by Pi, not read from the API, in both adapters above.
- The cost fields are filled by `calculateCost(model, usage)` at the end of usage parsing (`openai-completions.ts:1550`). The rule is in `AI:models.ts:1193-1213` (price tiers by `inputTokensAbove`; 1h cache write = 2x input rate). This is H9.
- Empty usage: all zero, cost zero (`A:agent.ts:48-55`; faux uses `DEFAULT_USAGE`, `AI:providers/faux.ts:94`).
- Context-size estimate: `totalTokens || input + output + cacheRead + cacheWrite` (`AI:utils/estimate.ts:19-21`); image = 4800 chars; chars/4 (`:15-16`). H9.

### 2.4 Request types

```ts
interface Tool<TParameters extends TSchema = TSchema> {     // AI:types.ts:715-720
  name: string; description: string;
  parameters: TParameters;                  // TypeBox (JSON Schema) object
  constrainedSampling?: false | ConstrainedSamplingConfig;  // :705-713
}
interface ToolReference { name: string }                    // :722-724
interface Context { systemPrompt?: string; messages: Message[]; tools?: Tool[] }  // :732-736
type TranscriptContext = { messages: Message[] }  // branded; prompt and tools are in system messages :746-749
```

`ConstrainedSamplingConfig` = `{type:"json_schema"; strict:"prefer"|"require"} | {type:"grammar"; variants: GrammarVariants}` (`:705-713`). Not needed in H1.

`toToolDeclaration` keeps only name, description, parameters (JSON round trip) and `constrainedSampling` (`AI:utils/transcript.ts:123-130`).

### 2.5 Stream events (needed by the fake model in H1)

```ts
type AssistantMessageEvent =                                // AI:types.ts:767-783
 | {type:"start"; partial}
 | {type:"text_start"|"thinking_start"|"toolcall_start"; contentIndex; partial}
 | {type:"text_delta"|"thinking_delta"|"toolcall_delta"; contentIndex; delta: string; partial}
 | {type:"text_end"|"thinking_end"; contentIndex; content: string; partial}
 | {type:"toolcall_end"; contentIndex; toolCall: ToolCall; partial}
 | {type:"done"; reason: "stop"|"length"|"toolUse"|"deferred"; message}
 | {type:"error"; reason: "aborted"|"error"; error: AssistantMessage}
```

Rules in the doc comment (`:751-766`): `start` comes first; `done` or `error` ends the stream; text and thinking blocks are empty at `*_start`; redacted thinking can be complete at start with no deltas; tool-call args at `toolcall_start` depend on the provider.

### 2.6 Agent layer types

```ts
type AgentMessage = Message | CustomAgentMessages[keyof CustomAgentMessages]   // A:types.ts:374 (interface empty :365-367)
type AgentEvent =                                                              // A:types.ts:514-525
 | {type:"agent_start"} | {type:"agent_end"; messages}
 | {type:"turn_start"}  | {type:"turn_end"; message; toolResults: ToolResultMessage[]}
 | {type:"message_start"; message} | {type:"message_update"; message; assistantMessageEvent}
 | {type:"message_end"; message}
 | {type:"tool_execution_start"; toolCallId; toolName; args}
 | {type:"tool_execution_update"; toolCallId; toolName; args; partialResult}
 | {type:"tool_execution_end"; toolCallId; toolName; result; isError}
interface AgentToolResult<T> { content; details: T; structuredContent?; usage?; isError?; terminate? }  // A:types.ts:424-442
interface AgentTool extends Tool { label; prepareArguments?; outputSchema?; execute(toolCallId, params, signal?, onUpdate?); replay?: "never"|"safe"; executionMode? } // :464-495
interface AgentContext { messages: AgentMessage[]; tools?: AgentTool[] }       // :500-505
```

### 2.7 Coding-agent roles (`C:core/messages.ts`)

```ts
BashExecutionMessage  { role:"bashExecution"; command; output; exitCode: number|undefined; cancelled; truncated;
                        fullOutputPath?; timestamp; excludeFromContext? }                 // :29-40
CustomMessage<T>      { role:"custom"; customType; content: string|(Text|Image)[]; display: boolean; details?: T; timestamp } // :46-53
BranchSummaryMessage  { role:"branchSummary"; summary; fromId: string|null; timestamp }  // :55-60
CompactionSummaryMessage { role:"compactionSummary"; summary; tokensBefore: number; timestamp } // :62-67
```

Constructors take an ISO string and convert to unix ms with `new Date(ts).getTime()` (`:100-138`). This is the only place the two clocks meet.

## 3. Behaviors and invariants

### 3.1 Pipeline before each model call

1. `transformContext(messages, signal)` (optional, agent-level, may prune or inject) `A:agent-loop.ts:388-392`, contract `A:types.ts:225-244`.
2. `convertToLlm(messages)` `A:agent-loop.ts:395`. Contract: must not throw (`A:types.ts:197-222`).
3. `normalizeContext({messages})` adds the leading system message `A:agent-loop.ts:397`, `AI:utils/transcript.ts:10-34`. System prompt and tools are NOT on `Context` when they reach a provider.
4. Provider adapter calls `transformMessages(messages, model, normalizeToolCallId?)` (Anthropic: `AI:api/anthropic-messages.ts:1057`; completions: `AI:api/openai-completions.ts:1220`).
5. Adapter converts to the vendor wire format, with `sanitizeSurrogates` on text (used in 9 adapters, for example `AI:api/openai-responses-shared.ts:93`).

### 3.2 `convertToLlm`

Two versions exist.
- Agent default: keeps only roles system, user, assistant, toolResult. Everything else is silently dropped (`A:agent.ts:38-46`). It does not convert custom roles.
- Coding agent (`C:core/messages.ts:148-196`):
  - `bashExecution`: dropped if `excludeFromContext`; else a user message with one text block from `bashExecutionToText` (`:152-161`).
  - `custom`: a user message; string content is wrapped as one text block; `display` and `details` are lost (`:162-169`).
  - `branchSummary`: a user message with `BRANCH_SUMMARY_PREFIX + summary + "</summary>"` (`:170-175`).
  - `compactionSummary`: a user message with `COMPACTION_SUMMARY_PREFIX + summary + "\n</summary>"` (`:176-183`).
  - system, user, assistant, toolResult pass through unchanged (`:184-188`).
  - The original `timestamp` is kept on every converted message.
  - It does NOT drop error or aborted assistant messages, empty messages, or thinking blocks.

`bashExecutionToText` format (`:82-98`): ``Ran `cmd` `` newline, then a fenced output block or `(no output)`; then `(command cancelled)` OR `Command exited with code N` (only if exit code is set and not 0); then `[Output truncated. Full output: PATH]` only if truncated AND a path exists.

Prefix and suffix strings are model-visible text and part of prompt-cache bytes. Port them byte for byte (`:11-24`). Note: the compaction suffix starts with a newline; the branch suffix does not.

### 3.3 `transformMessages` (provider stage) `AI:api/transform-messages.ts:64-235`

In order:
1. `content == null` becomes `[]` for any message (lax imports) (`:71-73`, CL ai 0.80.4).
2. If the model has no `image` input, each image block in user and toolResult content becomes a text placeholder; consecutive images give one placeholder (`:12-57`). Strings: `(image omitted: model does not support images)` and `(tool image omitted: model does not support images)` (`:12-13`, CL ai 0.68.0).
3. Assistant message handling, "same model" = same provider AND api AND model id (`:95-98`):
   - Redacted thinking: kept if same model, else dropped (`:104-106`).
   - Thinking with signature, same model: kept even if text is empty (`:109`).
   - Thinking with empty or whitespace text and no kept signature: dropped (`:111`).
   - Thinking, same model, no signature: kept (`:112`). Different model: converted to a plain text block, no tags (`:113-116`, CL ai 0.39.0, 0.27.7).
   - Text: different model loses `textSignature` (rebuilt as `{type,text}`) (`:119-125`).
   - ToolCall: different model loses `thoughtSignature` (`:131-134`); the adapter callback may rewrite the id, and a map rewrites matching toolResult ids (`:136-142`, `:83-90`).
4. Second pass (`:158-235`):
   - Assistant with `stopReason` error or aborted is skipped entirely, and its tool calls are not tracked (`:195-203`; CL ai 0.48.0, 0.49.0, 0.49.2).
   - Before each assistant message and before each user message, any tool call without a result gets a synthetic result: text `No result provided`, `isError: true`, `timestamp: Date.now()` (`:167-186`, `:222-225`). Also at the end of the list (`:231-232`; CL ai 0.69.0).
   - System messages that appear between a tool call and its results are held and emitted after the results (`:163-166`, `:216-221`).

Thinking-level note: thinking is replayed as provider-native data only for the same model; otherwise it becomes plain assistant text.

Adapter-level extras (examples, not complete): Anthropic drops empty text blocks and thinking with no text and no signature (`anthropic-messages.ts:1311,1327,1342-1343`). Anthropic id rule: `[^a-zA-Z0-9_-]` becomes `_`, max 64 (`:1220-1222`). OpenAI adapters send `(no tool output)` for an empty tool result without images (`openai-completions.ts:1414`, `openai-responses-shared.ts:93`, CL ai 0.80.4); Mistral sends `[tool error] (no tool output)` for errors (`mistral-conversations.ts:906`).

### 3.4 Ordering and persistence

- Pi emits `message_start` with a `pending` partial, then `message_update` events, then `message_end` with the final message (`A:agent-loop.ts:400-460`). The partial is pushed into context at `start` and replaced at `done`/`error` (`:419-455`).
- The agent loop stamps `thinkingLevel = config.reasoning ?? "off"` on the final assistant message (`A:agent-loop.ts:~408`, CL agent 0.99.0).
- If the stream ends with no terminal event, the loop still calls `response.result()` (`:~457-465`). The H1 test "stream without terminal event is an error" must define what the Go port returns here.
- Persistence is on `message_end` only (`C:core/agent-session.ts:1096-1125`). So `pending` partials are never persisted. This is the real reason; there is no explicit filter.
- Persisted: system, user, assistant, toolResult as `message` entries; `custom` as `custom_message` entries; `bashExecution`, `compactionSummary`, `branchSummary` are "persisted elsewhere" (`:1117-1121`). The entry stores `message: AgentMessage` and wraps it with `id`, `parentId`, ISO `timestamp` (`C:core/session-manager.ts:57-67`).
- Message ids: messages have NO id field. Only session entries do: 8 hex characters of a random UUID, retried up to 100 times, else full UUID (`C:core/session-manager.ts:277-284`). Provider ids (`responseId`, `ToolCall.id`) are separate. The agent keeps a map message object to entry id in memory (`agent-session.ts:1123`).
- Stop on error or aborted: the loop emits `turn_end` and `agent_end` and returns, with no tool execution (`A:agent-loop.ts:245-256`).
- Stop `length` with tool calls: all calls fail with error results; none run (`A:agent-loop.ts:265-272`, CL agent 0.80.4).
- Run failure outside the stream: the agent builds an assistant message with one empty text block, zero usage, stopReason error or aborted, `errorMessage` (`A:agent.ts:528-548`). Note the empty text block: provider code must tolerate it, and `transformMessages` drops the message anyway.
- User prompt from a string is built as a block array, never as a bare string (`A:agent.ts:421-429`). The string form exists only for lax callers and old files.
- Load-time repair: role `hookMessage` is renamed to `custom` (`C:core/session-manager.ts:324-328`, CL coding-agent ~line 4580); null content becomes `""` or `[]` (`:439-450`).

### 3.5 System messages (0.86.0)

- First message = base prompt + tools. Later ones append content, patch sections, add or remove tools (`AI:types.ts:512-521`).
- Replay: contents joined by blank line; sections patched by name (Map keeps insertion order; `null` deletes); tools keyed by name, removals applied before additions within one message (`AI:utils/transcript.ts:58-96`).
- Models that cannot take mid-conversation system messages get them collapsed into one leading message (`:104-120`); flag `supportsMidConvoSystemMessages` (`AI:types.ts:846`) and `supportsMidConvoToolAdditions` (`:848`).
- `createInitialSystemMessage` returns nothing when the prompt is empty and there are no tools; timestamp is 0 (`AI:utils/transcript.ts:10-23`).
- Coding agent: `context` extension handlers do not see system messages; Pi restores prompt and tools after they run (CL coding-agent 0.87.0 Fixed).

### 3.6 Stream frames (0.85.0)

`AssistantMessageFrame` is a compact, replayable form of stream progress. It leaves out terminal settlement; the final message must be stored separately (`AI:utils/assistant-message-frame.ts:3-33`). The encoder rejects bad sequences (event after terminal, second start, block index not matching type) (`:140-170`); the reducer rebuilds a message from frames (`:233+`). `toolcall_checkpoint` stores partial JSON. Nothing in the roadmap uses this yet. See GAPS.

## 4. Edge cases with version

| Edge case | Version | Source |
|---|---|---|
| Empty assistant message after abort breaks Mistral (400) | ai 0.18.1 | CL ai L2151 |
| Usage input excluded cached tokens for OpenAI (double count) | ai 0.12.10 | CL ai L2193 |
| Thinking turned to text without `<thinking>` tags (model mimicked tags) | ai 0.39.0, 0.27.7 | CL ai L1913, L2055 |
| Empty error assistant messages (429/500) broke tool_use to tool_result chain | ai 0.48.0 | CL ai L1797 |
| Tool results orphaned after errored assistant | ai 0.49.0 | CL ai L1785 |
| OpenAI 400 "reasoning without following item": skip errored or aborted assistants | ai 0.49.2 | CL ai L1757 |
| Cross-provider ids: pipe-separated Responses ids, oversized ids, duplicate ids when calls share a provider id | ai 0.49.1, 0.50.2, 0.60.0, 0.81.0 | CL ai L1775, L1699, L1341, L458 |
| Trailing unresolved tool calls need synthetic results | ai 0.69.0 | CL ai L1136 |
| `redacted_thinking` was dropped in streaming; must be kept and replayed | ai 0.55.2 | CL ai L1472 |
| Redacted reasoning from non-Anthropic models on Bedrock | ai 0.84.3 | CL ai L220 |
| Signed empty thinking and empty text must be kept (Anthropic, Google) | ai 0.80.6, 0.84.0 | CL ai L562, L357 |
| Providers that return empty thinking signatures: opt-in `allowEmptySignature`; replay was converting to text | ai 0.77.0, 0.99.0 | CL ai L861, L60 |
| Thinking signature must be serialized once, after it is complete | ai 0.84.4 | CL ai L190 |
| Gemini: `thoughtSignature` is replay data, not "is thinking"; Gemini 3 tool call ids dropped; unsigned tool calls | ai 0.43.0, 0.84.0, 0.56.2, 0.71.0 | CL ai L1865, L360, L1435, L1042 |
| `textSignature` encodes `id` and `phase` (OpenAI) | ai 0.56.2 | CL ai L1430 |
| Namespaces survive streaming, proxy, replay | ai 0.84.2, agent 0.84.2 | CL ai L249, CL agent L84 |
| Image-only user message must not carry an empty text part | ai 0.87.1 | CL ai L77 |
| Non-vision models: placeholder text, not silent drop | ai 0.68.0 | CL ai L1163 |
| Empty tool result: `(no tool output)`, not `(see attached image)` | ai 0.80.4 | CL ai L582 |
| Tool-result images lost in `function_call_output`; Gemini 3 needs inline multimodal tool result | ai 0.58.0, 0.60.0, 0.25.1 | CL ai L1393, L1336, L2065 |
| Gemini tool result shape `{output}` / `{error}` | ai 0.23.5 | CL ai L2083 |
| `null` message content from lax files | ai 0.80.4, agent 0.80.4 | CL ai L571, CL agent L197 |
| Tool calls from a `length` stop must fail, not wait | agent 0.80.4 | CL agent L196 |
| Reasoning tokens double counted (already in `completion_tokens`) | ai 0.70.0 | CL ai L1117 |
| Usage in `choice.usage`; cache read vs write semantics; DeepSeek hit tokens; Kimi top-level `cached_tokens` | ai 0.58.0, 0.65.1, 0.71.0, 0.84.3 | CL ai L1392, L1256, L1048, L225 |
| Proxies omit `usage` in `message_delta` | ai 0.80.7 | CL ai L548 |
| All-zero assistant usage after truncation must not drive estimates | agent 0.80.0 | CL agent L231 |
| Stale usage before compaction boundary | ai 0.80.6 | CL ai L559 |
| `rawStopReason` added; unmapped terminal reasons become provider errors, not stops | ai 0.83.0 | CL ai L376 |
| `responseModel` added (openai-completions) | ai 0.71.0 | CL ai L1038 |
| `Usage.reasoning` added | ai 0.80.3 | CL ai L602 |
| `thinkingLevel` on assistant message added | ai 0.99.0, agent 0.99.0 | CL ai L45, CL agent L12 |
| Transport diagnostics attached to assistant message (WebSocket fallback; Bedrock failure details) | ai 0.73.0, 0.84.0 | CL ai L993, L336 |
| `message_update` stopped carrying cumulative `message` and `partial` (quadratic output); later fix kept cumulative usage | coding-agent 0.84.0, 0.84.2 | CL coding-agent L590, L527 |
| Role rename `hookMessage` to `custom`; `CustomMessages` renamed `CustomAgentMessages` | coding-agent 0.31.0 era, agent 0.31.0 | CL coding-agent L4580, CL agent L732 |
| System messages and `TranscriptContext` introduced (breaking for custom providers) | ai 0.86.0 | CL ai L105, L110 |
| `ToolResultMessage.addedToolNames` existed in 0.80.7; it is gone from current types (replaced by system-message tool deltas) | ai 0.80.7 to 0.86.0 | CL ai L534, `AI:types.ts:594-608` |
| Extensions may replace the finalized `message_end` message | coding-agent 0.71.0 | CL coding-agent L1932 |
| `bashExecution.excludeFromContext` via RPC | coding-agent 0.76.0 | CL coding-agent L1620 |
| Short entry id used timestamp prefix; now random tail | agent 0.80.4 | CL agent L199 |
| `toolResult` persisted before its assistant message | coding-agent 0.55.4 | edge report section 10 |

Edge report sections 10, 11, 13 (`plans/reports/researcher-260930-2254-pi-edge-cases.md`) agree with the above. Section 13 says "Missing trailing tool results are synthesized ... (0.69.0)" and "Errored or aborted assistant messages are dropped" - both verified at `transform-messages.ts:167-203`.

## 5. GAPS: items the H1 rows miss

Own in H1 (needed for the exit test or for a stable wire contract):

| # | Item | Why H1 owns it | Source |
|---|---|---|---|
| G1 | Correct H-SESS-05: no `image` in assistant content | Wrong inventory row would add a dead type | `AI:types.ts:548` |
| G2 | `ToolResultMessage.usage` and `nestedCalls`; `NestedToolCall*` types | Fields on the persisted type; adding later changes session JSON | `AI:types.ts:573-608` |
| G3 | `TextSignatureV1` and `textSignature` | Needed for OpenAI Responses replay (H4). Field costs nothing now | `AI:types.ts:389-399` |
| G4 | `SystemMessage` with `sections`, `toolsAdded`, `toolsRemoved` and the replay helpers | Decide now: copy the delta model, or keep a plain prompt string. Providers in H3 and H4 read it. Model design is a user decision (see Unresolved Q1) | `AI:types.ts:522-538`, `AI:utils/transcript.ts` |
| G5 | `Context` vs `TranscriptContext` and `normalizeContext` | Defines the provider interface | `AI:types.ts:732-749` |
| G6 | `Tool` declaration type (name, description, JSON Schema `parameters`) | Part of the request; Go needs a JSON Schema carrier, not TypeBox | `AI:types.ts:715-720` |
| G7 | `AgentEvent` payload shapes with the message types | H1 already builds events; `turn_end.toolResults`, `agent_end.messages` carry messages | `A:types.ts:514-525` |
| G8 | `AssistantMessage.diagnostics` type | Row names the field but not the type | `AI:utils/diagnostics.ts` |
| G9 | `JsonObject` type for `ToolCall.arguments` and `details` | Go: `json.RawMessage` vs `map[string]any` choice affects round trip | `AI:types.ts:452-453` |
| G10 | Message timestamp rules: unix ms on messages; synthetic results use call-time clock | Replay must be deterministic in tests | `transform-messages.ts:177` |

Own in H2 or the provider phases (state the deferral in H1 notes):

| # | Item | Owner | Reason |
|---|---|---|---|
| D1 | `transformMessages` (drop error and aborted, thinking to text, synthetic tool results, image placeholders, id map) | H2 for pure function, H3 for per-vendor id callbacks | Needs a model record (`input`, provider, api, id). It is a pure function; H1 can ship it with unit tests, but the inventory sources do not cite it. Recommend H1 includes it (it is about 170 lines and defines message validity) |
| D2 | `sanitizeSurrogates` | H3 | Go strings are UTF-8; invalid UTF-8 needs a different rule (section 6) |
| D3 | `calculateCost`, tiers, 1h cache rule | H9 | Row H-RETRY-07 and roadmap say H9 |
| D4 | `estimateMessageTokens` | H9 | `AI:utils/estimate.ts` |
| D5 | `AssistantMessageFrame` encoder and reducer | Defer (post-M1) | No consumer in the roadmap. Inventory has no row. Used by durable runs, not by the JSONL session |
| D6 | `deferred` stop reason and `DeferredHandle` | Keep the field; defer behavior | Faux provider supports it (`AI:providers/faux.ts:80-98`), no roadmap phase needs it. Decide if the enum value ships in H1 (Unresolved Q2) |
| D7 | `endTurn`, `rawStopReason` | H3 | Providers fill them |
| D8 | Coding-agent roles and `convertToLlm` | H5/H6 (session and compaction) | `compactionSummary` and `branchSummary` exist only after those phases; H1 should define the extension point (interface or registry) and the default role filter |
| D9 | `context_edit`, `context_with_system`, message-replace by extension | Extension phases | Not message-model |
| D10 | `ThinkingLevel` / `ModelThinkingLevel` enums and `thinkingLevelMap` | H1 for the enum only | Needed for `AssistantMessage.thinkingLevel`; mapping per model is a provider phase |
| D11 | Provider compat flags (`supportsMidConvoSystemMessages`, `allowEmptySignature`, `requiresThinkingAsText`) | H3, H4 | Data on the model record |

Doc and code differences to record (do not port the doc):
- `replace?: boolean` on SystemMessage: doc only (`CD:message-types.md:116,128`).
- Doc `ToolResultMessage` lacks `nestedCalls` (`CD:message-types.md:156-166` vs `AI:types.ts:604`).
- Doc `AssistantMessage` lacks `thinkingLevel` (`CD:message-types.md:179-196` vs `AI:types.ts:557`).
- Doc `ToolCall.arguments` is `Record<string, any>`; code is `JsonObject`.

## 6. Go port notes

1. Content blocks. Use a closed sum type. Recommended shape: an interface `ContentBlock` with an unexported marker method, concrete structs `Text`, `Thinking`, `Image`, `ToolCall`, and custom `MarshalJSON`/`UnmarshalJSON` on a wrapper that reads `"type"` first and switches. Reason: `type` is the wire discriminator in Pi files; a tagged struct with optional pointers would allow impossible states. Keep three separate slice types so the compiler enforces Pi's rules: `UserContent = []UserBlock` (Text, Image) and `AssistantContent = []AssistantBlock` (Text, Thinking, ToolCall). Unknown `type` must return an error for assistant content and be kept as raw JSON for forward compatibility (decide, Q3).
2. Messages. Same pattern, discriminator `"role"`. The agent union must stay open (extensions, custom roles). In Go use `type Message interface{ Role() string }` plus a registry `RegisterRole(name, decoder)` instead of Pi declaration merging. Unknown role: load as `RawMessage` and keep it in the log; skip it in the default `convertToLlm` (same as Pi, `A:agent.ts:38-46`).
3. User content: Pi allows string or blocks. Accept both on read (old files), always write blocks, as Pi does in `A:agent.ts:421-429`. Same for `SystemMessage.content` and `CustomMessage.content`.
4. Optional fields: use `omitempty` with pointer or zero-value rules chosen per field. `Usage.reasoning` and `cacheWrite1h` need "absent" to differ from 0 (Pi: absent = provider does not report, `AI:types.ts:439`): use `*int64`. `exitCode` can be `nil` (`C:core/messages.ts:33`): use `*int`.
5. Time. Message `timestamp`: `int64` unix ms (matches Pi file format, import compatibility). Entry timestamp: RFC 3339 string in JSON, `time.Time` in Go. Convert only in the constructors that mirror `C:core/messages.ts:100-138`. Do not call the clock inside pure transforms (Pi's synthetic result uses `Date.now()`, `transform-messages.ts:177`); pass a `now func() int64`.
6. Usage and cost. Token fields `int64`. Cost: integer micro-USD for the five cost fields (decided in the earlier report; roadmap H1 "Also builds"). The Pi price tables are USD per million tokens as floats (`AI:models.ts:1207-1210`). A rate of `$0.30 / Mtok` is 0.3 micro-USD per token, which is not an integer; H9 must pick the rate unit (for example nano-USD per token) and a rounding rule. H1 only needs `int64` fields and a note. `totalTokens` is computed by the adapter, not read from the API (`anthropic-messages.ts:628-629`; `openai-completions.ts:1547`).
7. `StopReason`: typed string constants with the 7 values. `pending` exists only in memory. Enforce in the session writer: reject `pending` on persist (Pi only avoids it by writing on `message_end`).
8. `ToolCall.arguments` and `ToolResult.details`: use `json.RawMessage` in the message record. Decode with the tool's schema in the tool layer. Partial JSON during streaming stays in the stream accumulator, not the message (Pi keeps parsed partial args in the block; `toolcall_end` gives the final value, `AI:types.ts:777`).
9. Text safety. Go strings are bytes. Pi's `sanitizeSurrogates` removes lone UTF-16 surrogates (`AI:utils/sanitize-unicode.ts:21-25`). In Go, the equivalent is `strings.ToValidUTF8(s, "")` before JSON encoding. Go's `encoding/json` already replaces invalid UTF-8 with U+FFFD, which differs from Pi (Pi removes). Decide which one (Q4). When reading old Pi files, `\ud83d` lone escapes in JSON: Go's decoder turns them into U+FFFD silently; test it.
10. Stream events: Go channel of a sum type, or an iterator `iter.Seq`. Do not put the full `partial` in each event (Pi did this; the JSON mode removed it in 0.84.0 for quadratic growth; roadmap says "Do not rebuild"). Give the consumer an accumulator that builds the message from deltas, and give `message_end` the final message.
11. `transformMessages` in Go: a pure function `func Transform(msgs []Message, m ModelInfo, normID func(id string, src *Assistant) string, now int64) []Message` returning new slices, no mutation. "Same model" key is the triple provider, api, model id. Keep an id map for toolResult rewrites. Table-driven tests from section 3.3 (one case per bullet).
12. Faux provider (H1 exit): scripted steps are whole `AssistantMessage` values or factories; usage estimated as chars/4 (`AI:providers/faux.ts:158-159,237-249`). Provide `FauxAssistantMessage(content, opts)` with the default fields shown at `:80-98`.
13. Validate on decode, not only encode: Pi does not validate session files (`C:core/session-manager.ts:439-441` comment). Go should normalize null content at one boundary (the loader) and keep core code free of nil checks.

## 7. Limitations

- I read type files, transform code, the agent loop at the points cited, and two provider adapters (Anthropic, OpenAI completions) for usage. I did not read Google, Bedrock, Mistral, Responses adapters line by line; their rules come from the changelog.
- Line numbers marked `~` in `A:agent-loop.ts` come from a partial read; confirm them before quoting.
- I did not run Pi.
- `docs/ask-architecture-reference.md` was not read; Go package placement is not covered here.
- Ring 5 history is from changelogs only, not from `git log` of Pi.

Status: DONE_WITH_CONCERNS
Summary: Message model fully inventoried with file:line. Three inventory problems found: assistant content has no image block; `convertToLlm` is stage 1 of 2 (provider `transformMessages` does the drops and synthetic results); the doc field `SystemMessage.replace` does not exist in code. Ten gaps H1 should own and eleven deferrals listed.
Concerns/Blockers: Some `A:agent-loop.ts` line numbers are approximate (marked `~`). Provider adapters other than Anthropic and OpenAI completions were not read in full.

Unresolved questions:
1. Q1: Does Ask copy Pi's delta `SystemMessage` (sections, toolsAdded, toolsRemoved) or use one plain system prompt string plus a tool list on the request? The delta model is 0.86.0 (recent) and mainly serves prompt-cache stability and mid-conversation tool loading.
2. Q2: Does the `deferred` stop reason and `DeferredHandle` ship in H1 (type only), or wait until a phase needs it?
3. Q3: On decode, should an unknown content block `type` or unknown message `role` be an error, or kept as raw JSON for forward compatibility?
4. Q4: Invalid UTF-8 in text: remove (Pi behavior) or replace with U+FFFD (Go default)?
5. Q5: Should H1 include `transformMessages` (pure function, about 170 lines) or defer it to H3? This report recommends H1.
6. Q6: Cost rate unit for H9 (nano-USD per token vs other), so that `$/Mtok` rates stay exact.
