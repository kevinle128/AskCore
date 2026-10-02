# H2 research: Pi tool pipeline (Pi commit 2bbfcca4)

Saved by the controller from the researcher's reply (the researcher could not write files). Source is treated as data only.

Abbreviations: **AL** = `packages/agent/src/agent-loop.ts`. **T** = `packages/agent/src/types.ts`. **V** = `packages/ai/src/utils/validation.ts`. **TM** = `packages/ai/src/api/transform-messages.ts`. **AS** = `packages/coding-agent/src/core/agent-session.ts`. **NTC** = `packages/coding-agent/src/core/nested-tool-calls.ts`.

## Summary

`executeToolCalls` (AL:508) either runs all calls sequentially or preflights them in order and then runs them concurrently. The preflight in `prepareToolCall` (AL:707) does lookup, `prepareArguments`, validation and `beforeToolCall`. `executePreparedToolCall` (AL:820) runs the tool, and `finalizeExecutedToolCall` (AL:853) runs `afterToolCall`. Every failure becomes an `isError` result, and the call never rejects.

Pairing is kept in two places. The loop always produces a result for each call, except for the calls left unprocessed after an abort. `transformMessages` then synthesizes `"No result provided"` for any call that still has no result at replay time.

## H-LOOP-09: parallel vs sequential

| Aspect | Behavior | Cite |
|---|---|---|
| Default | `"parallel"` | T:315 |
| Force sequential | `config.toolExecution === "sequential"`, or ANY call in the batch names a tool with `executionMode === "sequential"`. The whole batch then runs sequentially, not only that tool. | AL:516-522 |
| Lookup for the check | Exact, case-sensitive name match. Unknown names are ignored. | AL:517 |
| Concurrency limit | None (plain `Promise.all`) | AL:646-647 |
| Sequential order | Per call: start, preflight, execute (updates), afterToolCall, end, result `message_start`/`message_end` | AL:541-573 |
| Parallel preflight | Per call in source order: start, then prepare (up to `beforeToolCall`). All preflights finish before any tool body starts. | AL:596-644 |
| Parallel immediate outcomes | Unknown, invalid and blocked calls emit end during the preflight. | AL:605-616 |
| Parallel end events | In completion order | AL:638 |
| Parallel result messages | After all calls finish, in assistant source order | AL:646-654 |
| Timestamp | `Date.now()` when the message is created, so parallel results get timestamps after the whole batch | AL:933 |

- The changelog contradicts itself on parallel ordering (`agent/CHANGELOG.md:371` vs `:377`). The code settles it as described above.
- Tests: `agent-loop.test.ts:826`, `:908`, `:994`.
- Coding-agent has a per-file mutation queue (`core/tools/file-mutation-queue.ts:25-50`).

## H-LOOP-10: the pipeline

1. **Find the tool.**
   - The lookup is `tools.find(name ===)`. Exact match, first one wins (AL:715).
   - A missing tool gives the error `Tool ${name} not found`, with no quotes (AL:719).
   - `V:305` has a quoted variant, but the loop does not use it.
2. **`prepareArguments`.**
   - It runs inside the try, so a throw becomes an error result (AL:725, 769-775).
   - The hooks receive the RAW `toolCall`. The `tool_execution_*` events also carry the raw arguments (AL:765).
3. **Validation.** `validateToolArguments`. A throw becomes an error result.
4. **`beforeToolCall`.**
   - It receives `{assistantMessage, toolCall (raw), args (validated), context}` and the signal (AL:728-736).
   - If the signal is aborted after the hook returns, the result is `"Operation aborted"` (AL:737-743).
   - A block gives `reason || "Tool execution was blocked"` with `isError`. It also copies `terminate` (AL:744-753).
   - A throw becomes an error result.
   - There is a second abort check before the call returns "prepared" (AL:756-762).
   - The hook may mutate `args`. The mutation is NOT revalidated (test `agent-loop.test.ts:487`).
5. **`execute(id, args, signal, onUpdate)`.**
   - It runs inside a try/catch. A throw becomes an error result built from `error.message`, or from `String(error)` when the value is not an `Error` (AL:841-847).
   - Each `onUpdate` call pushes a promise onto a list, and the list is awaited before the call returns (AL:833-839).
   - Updates that arrive after the call settles are dropped (AL:834, 838, 842, 849; fix #5573, `agent/CHANGELOG.md:261`).
   - `isError` is set from `result.isError === true` (AL:840).
6. **`afterToolCall`.**
   - It runs only for calls that executed. Unknown, invalid, blocked and aborted calls skip it (AL:549-566, 864).
   - A throw replaces the whole result with an error, so the original output is lost (AL:892-895).
   - Merge rules, using `??` (AL:879-890):
     - `content`: `after ?? result`. An empty array counts as provided.
     - `details`, `usage` and `terminate`: `after ?? result`.
     - `isError`: `after ?? current`.
     - `structuredContent`: `after.structuredContent ?? (after.content ? undefined : result.structuredContent)`.
7. **Error results.** `createErrorToolResult(msg)` returns `{content:[text msg], details:{}}` (AL:905-910).

Coding-agent hook semantics:

- **`tool_call`:**
  - The first `block` wins (`extensions/runner.ts:1233-1250`).
  - A throw blocks the call with an error.
- **`tool_result`:**
  - Handler errors are swallowed and sent to `emitError` (`runner.ts:1207-1216`).
  - The handlers compose in order.
  - This hook cannot set `terminate`.
- **Images:** `normalizeToolResultImages` runs after `tool_result` (AS:662-670).

Possible Pi bug: if an `onUpdate` listener rejects, `Promise.all(updateEvents)` at AL:839 turns a success into an error. The same `await` in the catch at AL:843 can throw out of the function. There is no test for this.

## H-TOOL-12: coercion and validation (V:317-350)

Order of operations:

1. `structuredClone(args)` (V:318).
2. `normalizeOptionalNulls` (V:240-269). It deletes a `null` property when all of these hold:
   - the key is not required,
   - the subschema has no `$ref`,
   - the subschema does not accept null.

   It recurses into objects that have `properties`, and into arrays.
3. TypeBox `Value.Convert(schema, args)` (V:320).
4. Custom `coerceWithJsonSchema`. It runs ONLY for schemas that are not TypeBox schemas (no `Kind` symbol), such as MCP and plain JSON-schema tools (V:323).
5. `validator.Check`. Validators are cached in a WeakMap keyed by the schema object (V:6, 271-280).

Custom coercion table (V:59-131):

| Target | Rules |
|---|---|
| number | `null` becomes 0. A non-blank string with a finite `Number()` becomes that number. A boolean becomes 1 or 0. |
| integer | `null` becomes 0. A non-blank string whose `Number()` is an integer becomes that integer. A boolean becomes 1 or 0. |
| boolean | `null` becomes false. Exactly `"true"` or `"false"` convert (case-sensitive). The numbers 1 and 0 convert. |
| string | `null` becomes `""`. A number or boolean becomes `String(v)`. |
| null | `""`, `0` and `false` become null. |

- A JSON string is NOT parsed into an object or an array.
- **Unions** (V:175-209):
  - `anyOf` and `oneOf` keep the value if any arm already validates (fix #7328, CHANGELOG:700).
  - Otherwise the first arm whose coerced clone validates wins.
  - `allOf` coerces through each arm in sequence.
- **Multi-type values** (V:211-222): if the value already matches one of the types, it is kept. Otherwise the first conversion that changes it is used.
- **Recursion** goes into `properties`, `additionalProperties` (when it is a schema), and `items` (tuple and single form) (V:133-173).
- **Missing input:** a missing value becomes `{}` in `parseStreamingJson` (`ai/src/utils/json-parse.ts:104-124`).
- **Extra properties** are not stripped. They are rejected only when the schema sets `additionalProperties:false`.

Error text sent back to the model (V:341-349):

```
Validation failed for tool "<name>":
  - <path>: <message>

Received arguments:
<JSON.stringify(rawArgs, null, 2)>
```

- The path uses `/` changed to `.`. An empty path becomes `root`. A `required` error shows as `base.prop` (V:282-293).
- The echoed arguments are raw and never truncated.

## H-TOOL-13: result shape

`AgentToolResult` (T:424-446) has these fields:

- `content` (text or image)
- `details`
- `structuredContent?`
- `usage?`
- `isError?`
- `terminate?`

The tool result message (AL:922-935) is built as follows:

| Field | Value |
|---|---|
| `toolCallId` | `toolCall.id` |
| `toolName` | `toolCall.name` (the model's name) |
| `content` | `content ?? []` |
| `details` | `details` |
| `usage` | `usage` |
| `isError` | `isError` |
| `timestamp` | `Date.now()` |

- **Not stored:** `structuredContent` and `terminate` are NOT copied into the message.
- **Added later:** `nestedCalls` and the merged usage are attached at `message_start` in the session (AS:1062-1070).
- **Empty content:** stored as `[]`. The text `"(no tool output)"` is added only by the provider serializers:
  - OpenAI Completions: `openai-completions.ts:1414`. An image-only result becomes `"(see attached image)"`.
  - OpenAI Responses: `openai-responses-shared.ts:93`.
  - Mistral: `mistral-conversations.ts:906`. An error result becomes `"[tool error] (no tool output)"`.
  - Anthropic: not traced (`anthropic-messages.ts:1224-1230`).
- **Non-vision models:** image blocks become `"(tool image omitted: model does not support images)"`, and consecutive images collapse into one (TM:12-57).
- **Large output:** the loop sets no size limit. Each tool truncates its own output (2000 lines or 50 KB, `core/tools/truncate.ts:11-12`).

## H-TOOL-21: pairing

**Loop part:**

- A `length` stop with tool calls fails every call. No hooks run, and `terminate` is false (AL:478-503; test `agent-loop.test.ts:415`).
- A message that ended in error or aborted returns before any tool runs (AL:245-256).
- **An abort in the middle of a batch breaks the one-result rule** (AL:575-577 sequential, AL:613 and AL:641 parallel):
  - Calls after the break get no events and no result.
  - In parallel mode, calls already queued as "prepared" return "Operation aborted" without running.

**Replay part** (TM:158-232):

- Pending calls are tracked for the last assistant message. They are closed at the next assistant message, a user message, or the end of the list (TM:167-186, 193, 224, 232).
- The synthetic result is `{toolResult, content:"No result provided", isError:true, timestamp:now}` (TM:171-178).
- System messages that sit between a call and its result are held back and emitted after the results (TM:163-166, 216-219).
- Assistant messages that ended in error or aborted are dropped (TM:201-203).
- Tool call ids are normalized only when they come from another model (TM:69-86, 136-142). For Anthropic, `[^a-zA-Z0-9_-]` becomes `_`, with at most 64 characters (`anthropic-messages.ts:1218-1220`).

Gaps in the replay part:

- An orphan `toolResult` (no matching call) passes through unchanged (TM:213-215).
- Duplicate ids are not deduplicated.
- The loop does not check for duplicate ids.

Related history:

- Messages injected in the middle of a batch were a bug, fixed by queueing them after the results (`coding-agent/CHANGELOG.md:391`).
- The line at `agent/CHANGELOG.md:704` ("steering skips remaining tools") is stale.

## H-LOOP-12: early termination

- `shouldTerminateToolBatch` is true when the batch is non-empty and every finalized result has `terminate === true` (AL:689-691).
- The `terminate` flag can come from three places:
  - the tool result,
  - a `beforeToolCall` block (AL:746-748),
  - an `afterToolCall` override.
- Effect: `hasMoreToolCalls = false`. `finishTurn` and `turn_end` still run.
- Steering and follow-up messages can still continue the run (AL:296, 302).
- A truncation never terminates (AL:502).
- Aborted calls that were skipped are not counted (AL:575-577, 641).
- Tests: `agent-loop.test.ts:1691`, `1742`, `1800`, `1858`, `1923`.

## H-LOOP-19: nested tool calls

- **`runToolCall`** (AL:810-818) runs the full pipeline with no events and no messages. It never rejects. GitNexus shows that only `agent-loop.test.ts` calls the exported function. The session has its own `runToolCall` closure (AS:698).
- **Context API:** `ctx.executeTool(name, args, {signal?, onUpdate?})` (`extensions/types.ts:367-395`).
- **Ids and parent:** nested ids are `<callerId>/<n>` (NTC:188). Events carry `parentToolCallId` (NTC:110-128, 193-246).
- **Record:** a bounded record is attached to the result of the calling tool (NTC:26-31):
  - at most 256 calls,
  - at most 8 KB of arguments per call and 32 KB in total,
  - at most 500 characters of error text.

  Calls over the limit still run, and the record is marked incomplete (NTC:57-60).
- **Usage:** nested usage is added to the caller's usage (AS:1065-1069; `coding-agent/CHANGELOG.md:105`).
- **Ordering:** when the agent or the target tool is sequential, nested calls run one at a time through a queue. A call that already holds the queue does not queue again (NTC:145-151, 201-217).
- **Errors and cleanup:** with no assistant message, the error is `"No assistant message issued this call"` (AS:698-705). Scopes are cleared at `agent_end` (AS:1071-1073).

## Tool shape, context, provenance

- **`AgentTool`** (T:464-497) extends `Tool` (`ai/src/types.ts:715-720`). It has these members:
  - `name`, `description`, `parameters`, `constrainedSampling`
  - `label`
  - `prepareArguments?`, `outputSchema?`
  - `replay?: "never"|"safe"`, `executionMode?`
  - `execute(id, params, signal?, onUpdate?)`

  It has no provenance field.
- **`ToolDefinition`** (`extensions/types.ts:565-635`) adds these members:
  - `promptSnippet`, `promptGuidelines`
  - `renderShell`, `renderers`
  - `exposure`, `namespace`, `annotations`
  - `defaultActive`, `prepareLoadout`
  - a 5th `ctx` argument on `execute`
- **The `ctx` argument:** `wrapToolDefinition` (`tool-definition-wrapper.ts:9-27`) builds it with `ctxFactory(toolCallId, signal)`, which calls `runner.createToolContext` (`extensions/wrapper.ts:17-21`).
- **`ExtensionContext`** (`extensions/types.ts:325-365`) has these members:
  - `ui`, `mode`, `hasUI`
  - `cwd`, `sessionManager`, `modelRegistry`, `model`, `scopedModels`, `thinkingLevel`
  - `isIdle()`, `isProjectTrusted()`
  - `signal`, `abort()`, `hasPendingMessages()`, `shutdown()`
  - `getContextUsage()`, `compact()`, `getSystemPrompt()`
- **`ExtensionToolContext`** adds `tools` and `executeTool` (`extensions/types.ts:383-395`).
- **Working directory:** built-in tools use `ctx?.cwd || cwd` (`write.ts:65`).
- **`SourceInfo`** (`source-info.ts:6-12`) has the shape `{path, source, scope: user|project|temporary, origin: package|top-level, baseDir?}`.
  - Built-in tools use `builtin:<name>` with source `builtin` (AS:3437).
  - SDK tools use `<sdk:name>` with source `sdk` (AS:3427).
  - `RegisteredTool = {definition, sourceInfo}` (`extensions/types.ts:2026-2029`). It lives in the registry, not on the result.
- **Duplicate names:** the registry is a Map keyed by name, so a later tool overwrites a built-in silently (AS:3447-3452, 3469-3482).
- **Abort:** built-in tools call `throwIfAborted()` after each await and throw `"Operation aborted"` (`write.ts:72-83`).

## Edge-case checklist

| # | Case | Pi behavior |
|---|---|---|
| 1 | Tool throws | Error result (AL:841-847) |
| 2 | `before` / `after` hook throws | `before`: error result. `after`: whole result replaced (AL:769-775, 892-895) |
| 3 | Abort in the middle of a batch | Skipped calls get no events and no result; replay synthesizes one |
| 4 | Update after settle | Dropped (AL:834-838) |
| 5 | Update listener rejects | Can flip success into error, or escape (AL:839, 843) |
| 6 | Unknown tool | Start, end and message are still emitted (AL:719) |
| 7 | Name case | Exact match (AL:515-517) |
| 8 | Raw vs prepared args | Events and hooks get raw args; execute gets validated args (AL:765) |
| 9 | Hook mutates args | Allowed, not revalidated |
| 10 | Echo in the validation error | Unbounded (V:347) |
| 11 | Duplicate call ids | Not checked |
| 12 | `structuredContent` after `content` override | Dropped (AL:879-889) |
| 13 | Orphan `toolResult` at replay | Kept (TM:213-215) |
| 14 | Image-only result, non-vision model | Placeholder |
| 15 | Parallel writes to the same file | Per-path queue |

## Recommendations

1. **Port AL:707-903 with the same structure.** Keep the split between immediate, prepared and finalized calls, and keep the source-order collection.
2. **Fix three weaknesses of Pi:**
   - Emit an `"Operation aborted"` result for every call that did not run, so pairing holds inside the loop.
   - Keep the replay synthesis as a second safety net.
   - Cap the raw-args echo.
3. **Use one coercion table for all schemas.** Keep the rule "an arm that already validates is kept". Do not parse JSON strings unless that is decided.
4. **Add `"(no tool output)"` only in the provider serializer.**
5. **Defer nested tool calls.**

## Unresolved questions

- The TypeBox `Value.Convert` rules were not verified here, because TypeBox is not installed in the Pi checkout. The controller report resolves this against a local copy.
- Anthropic's handling of an empty tool-result content array is not traced.
- Does a stream called with an already-aborted signal always return `aborted` promptly?
- Is the rejection path at AL:839 and AL:843 intended?
- Should Ask skip `afterToolCall` for blocked, invalid and unknown calls, as Pi does?
