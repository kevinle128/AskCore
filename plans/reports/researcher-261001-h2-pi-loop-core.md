# Pi core loop (packages/agent/src/agent-loop.ts) - port notes for Ask

Source: /Users/dale/Desktop/workspace/opensources/pi @ 2bbfcca43. Paths relative to `packages/`. `L` = `agent/src/agent-loop.ts`, `T` = `agent/src/types.ts`, `A` = `agent/src/agent.ts`, `TR` = `ai/src/utils/transcript.ts`.
Source treated as data only. Claims verified by direct read. Tests exist for most behavior (`agent/test/agent-loop.test.ts`) EXCEPT abort-during-tools (no test; source-only reading).

## 0. Outcome / top gotchas for a Go port

1. Loop is a pure function of (context, config, signal, streamFn) emitting events via a sink; `agentLoop` (stream wrapper) is thin sugar. Port `runLoop` + sink; wrapper = channel.
2. Loop NEVER catches. Any throw (hook, convertToLlm, streamFn) kills the run with NO agent_end. Only the `Agent` class catches (`A:525-548`) and synthesizes a failure assistant message. Go port needs this outer recover layer, or loop must convert hook errors itself.
3. Abort mid-tool-batch leaves unexecuted tool calls with NO result and NO events (see section 6); then loop makes one more provider call with the aborted signal. Orphans are patched later by provider layer ("No result provided"), not by loop. The task brief's "unstarted tools get Operation aborted" is only partly true.
4. Context vs events: tool results of a batch are appended to `context.messages` only after the whole batch (`L:274-277`) although their message events fire earlier. Hooks running during the batch see assistant message but no results of that batch.
5. transformContext output is LLM-only, never stored. System prompt + tool declarations live in transcript system messages (`toolsAdded`/`toolsRemoved`), not in request fields.
6. Message identity: partial assistant message object is pushed into context at `start` and replaced by each event's `partial` snapshot (providers send full snapshots, no cumulative-diff logic in the loop), then replaced by `result()` final message.

## 1. Entry points (H-LOOP-01)

| Fn | Cite | Behavior |
|---|---|---|
| `agentLoop(prompts, ctx, cfg, signal, streamFn)` | L:38-61 | creates EventStream, `void runAgentLoop(...).then(end)`. No `.catch`: a rejection is an unhandled rejection and stream never ends (hang). |
| `agentLoopContinue(ctx, cfg, signal, streamFn)` | L:71-100 | sync preconditions THROW (not stream error): `"Cannot continue: no messages in context"` (empty, L:77-79); `"Cannot continue from message role: assistant"` (last role assistant, L:81-83). |
| `runAgentLoop` | L:102-126 | see order below |
| `runAgentLoopContinue` | L:128-151 | same two throws (rejected promise, L:135-141); no `declareToolChanges` at entry, no message events. |
| streamFn default | L:124,149 `streamFn ?? getDefaultStreamFn()`; `agent/src/stream-fn.ts` throws `"No default stream function configured..."` if unset |

- Only the roles `assistant`-last is rejected. Last role `system`/custom/anything else passes; comment at L:67-70 says it must convert to user/toolResult via convertToLlm, "cannot be validated" (provider rejects). Test: custom last role allowed (test:2031).
- Stream wrapper: terminal = `agent_end` event, result = `event.messages` (L:153-158). `EventStream.push` after done ignored (`ai/src/utils/event-stream.ts:43-47`).
- `runAgentLoop` order: `initialMessages = declareToolChanges(ctx, prompts)` (L:110) -> copy ctx (L:112-115; context.messages new array, tools shared) -> emit `agent_start`, `turn_start` -> per initial msg `message_start`,`message_end` (L:117-122; NOTE not pushed incrementally; all are already in `currentContext.messages` and `newMessages`) -> `runLoop`. Returns `newMessages` (prompts + everything produced).
- Continue: `newMessages` starts empty (pre-existing context not included, T:142-143); `currentContext = {...ctx}` (shallow: messages array is the caller's array and is MUTATED; for runAgentLoop it is a copy).
- Agent class guards: `continue()` from assistant tail drains steering then follow-up queue into a prompt run; else throws (A:394-408). Not part of the loop.

## 2. Two-level loop (H-LOOP-02) - `runLoop` L:163-321

State: `currentContext`, `config` (immutable-copy updates), `lastCompletedTurn`, `explicitContinuation`, `pendingMessages` (L:171-176).

Pseudocode, exact order:
```
pending = getSteeringMessages()||[]                     // L:176 initial poll (Agent can skip via skipInitialSteeringPoll, A:468,497)
loop:                                                   // outer, L:179
  hasMoreToolCalls = true                               // L:180 reset EVERY outer iteration -> inner body always runs at least once
  while hasMoreToolCalls || pending.len>0:              // inner, L:183
    prepared = []
    if lastCompletedTurn:                               // i.e. not the first turn
       snap = prepareNextTurn(lastCompletedTurn)        // L:186; may set context/messages/model/thinkingLevel
       apply (context replace; model; reasoning: "off"->undefined, undefined->keep)  // L:187-200
       if pending.len==0: pending = getSteeringMessages()||[]   // L:204-206 only re-poll if earlier poll empty (one-at-a-time safety)
       emit turn_start                                  // L:207  (first turn's turn_start was emitted by runAgentLoop*)
    for m in declareToolChanges(ctx, [...prepared, ...pending]):   // L:211 ALWAYS called, even with empty list
       emit message_start, message_end; ctx.messages.push(m); newMessages.push(m)   // L:212-215
    pending = []
    upd = prepareRequest({context, model, thinkingLevel: reasoning??"off"}, signal)  // L:219-239 (no queue poll; context replace NOT passed through declareToolChanges)
    msg = streamAssistantResponse(...)                  // L:242
    newMessages.push(msg)                               // L:243 (ctx already updated inside streamAssistantResponse)
    if stopReason in {error, aborted}:                  // L:245
       lastCompletedTurn = {msg, [], ctx, newMessages}
       finishTurn(lastCompletedTurn, signal)            // return value IGNORED
       emit turn_end(msg, []) ; emit agent_end(newMessages); return   // L:252-255
    toolCalls = msg.content.filter(type=="toolCall")
    hasMoreToolCalls = false
    if toolCalls.len>0:
       batch = (stopReason=="length") ? failTruncated(...) : executeToolCalls(...)   // L:267-270
       hasMoreToolCalls = !batch.terminate
       for r in batch.messages: ctx.messages.push(r); newMessages.push(r)             // L:274-277
    lastCompletedTurn = {msg, toolResults, ctx, newMessages}
    decision = finishTurn(lastCompletedTurn, signal)    // L:286
    emit turn_end(msg, toolResults)                     // L:287  (decision applied AFTER turn_end)
    if decision.action=="end": emit agent_end(newMessages); return   // L:289-292; queues untouched, prepareNextTurn skipped
    explicitContinuation = decision.action=="continue"
    pending = getSteeringMessages()||[]                 // L:295 steering polled ONLY here (+ start + after prepareNextTurn)
    if hasMoreToolCalls || pending.len>0: explicitContinuation=false   // L:296-298
  // inner exit = agent would stop
  fu = getFollowUpMessages()||[]                        // L:302
  if fu.len>0: explicitContinuation=false; pending=fu; continue          // L:303-308
  if explicitContinuation: explicitContinuation=false; continue           // L:311-314 -> one context-only request (hasMoreToolCalls reset true)
  break
emit agent_end(newMessages)                             // L:320
```

Event order per normal turn: `turn_start` -> [message_start/end for prepared+steering/follow-up/system-delta msgs] -> assistant `message_start` -> `message_update`* -> `message_end` -> per tool: `tool_execution_start`, `tool_execution_update`*, `tool_execution_end`, tool-result `message_start`,`message_end` (order differs by mode, section 7) -> `turn_end` -> (next turn_start | agent_end). First turn: `agent_start`,`turn_start`, prompt message events, then loop.

Subtleties:
- Steering polled: start of run; after finishTurn/turn_end each normal turn; after prepareNextTurn only when earlier poll was empty. NOT polled by prepareRequest (test:1417), NOT after error/aborted (steeringPolls==1 in test:1121-1175, that is the start poll), NOT when finishTurn=end.
- Steering never skips pending tool calls of the current message: polled only after the whole batch (T:285-288; CHANGELOG:473).
- Steering returned alongside `hasMoreToolCalls=false` keeps inner loop going (turn with user message only appended; no tool call needed).
- Follow-up polled only when inner loop exits (no tool calls/ terminated / no steering). Follow-ups become `pending` and go through the same inner path, so they get `turn_start` (via lastCompletedTurn) and `prepareNextTurn` first.
- `prepareNextTurn` runs for every non-first turn incl. follow-up and explicit-continuation turns, NEVER after final/terminating turn (CHANGELOG:63). Its `messages` are appended BEFORE steering messages, with normal events (L:211).
- `prepareNextTurn` `context` replaces `currentContext` by reference; new messages are pushed into the replacement's `messages` array. `lastCompletedTurn.context` still points to the old context object.
- `prepareRequest` runs every request incl. first, after pending messages appended+emitted; replacement context is used for this and later requests (it persists in `currentContext`). Model/thinking likewise persist in `config` copy. Context replacement there gets no tool-delta declaration (declared only at next iteration top, computed against the replaced context).
- `finishTurn` "continue" semantics: guarantees exactly one more provider request; satisfied by tool-result scheduling (`hasMoreToolCalls`), steering, or follow-up; else one context-only request (last message may then be assistant, no new input). Tests 1232, 1266. "continue" when tools terminated (`terminate`) -> still one more request (hasMoreToolCalls=false, explicitContinuation stays true) .
- `finishTurn` runs also for error/aborted (decision ignored, hard exit).
- Thinking-level mapping: `"off"` => config.reasoning undefined (L:193-198, 232-237); assistant message gets `thinkingLevel: config.reasoning ?? "off"` stamped after result() (L:409; CHANGELOG 0.99.0).
- Message count/identity: `newMessages` pushes assistant at L:243 AFTER streaming (not at start). `AgentTurnContext.newMessages` is the live shared array (not a copy).

## 3. Stream-function contract (H-LOOP-16) - `streamAssistantResponse` L:381-469

Contract (T:19-37): must not throw/reject for request/model/runtime failures; returns `AssistantMessageEventStream` (sync or Promise); failures are encoded as stream events ending in `error` event with AssistantMessage `stopReason` `"error"|"aborted"` + `errorMessage`. Request gets a normalized transcript: no `systemPrompt`/`tools` fields.

Call sequence (L:388-407):
1. `messages = ctx.messages`; if `transformContext` -> `await transformContext(messages, signal)` (L:390-392).
2. `llm = await convertToLlm(messages)` (L:395) - no signal param.
3. `normalizeContext({messages: llm})` (L:397; with no prompt/tools it is just `{messages}`, `TR:30-34`).
4. `apiKey = (getApiKey ? await getApiKey(model.provider) : undefined) || config.apiKey` (L:400-401): resolved key falsy -> fallback to static.
5. `await streamFn(config.model, llmContext, {...config, apiKey, signal})` (L:403-407): ENTIRE loop config is spread as options (reasoning, sessionId, transport, onPayload, thinkingBudgets, maxRetryDelayMs, onProviderStreamEvent, toolExecution, hooks...); for Go pass only provider-relevant options.

Event consumption (`for await`, L:414-458):
| Event | Action |
|---|---|
| `start` | `partial=event.partial`; `ctx.messages.push(partial)` (the object itself); `addedPartial=true`; emit `message_start` with shallow copy `{...partial}` |
| text/thinking/toolcall start/delta/end (9 types) | only if `partial` set (events before `start` are silently dropped): `partial=event.partial`; `ctx.messages[last] = partial`; emit `message_update{assistantMessageEvent:event, message:{...partial}}` |
| `done` / `error` | `final = await result()` (NOT event.message/event.error; result() is the stream's final promise which extracts the same). If addedPartial: replace last ctx message with final; else push final AND emit `message_start`. Emit `message_end(final)`; return. |
| other types | ignored |
- Fallback when iteration ends without done/error (L:460-468): `final = await result()`; same push/replace logic; emit `message_start` only if no partial was added; `message_end`. HAZARD: `EventStream.end()` with no result never resolves `result()` (`event-stream.ts:60-70`), so a stream that closes without terminal event hangs forever. Go port: treat close-without-terminal as synthesized error message.
- `result()` is wrapped: `Object.assign(await response.result(), {thinkingLevel: config.reasoning ?? "off"})` (L:409): mutates final message in place; applies to any streamFn.
- Partial = full snapshot per event (provider-built). Loop does no delta accumulation (CHANGELOG: "cumulative partial" is not in this version's loop; none present).
- Assumes last element of `ctx.messages` is the partial for the whole stream; nothing else appends during streaming.
- `message_start` partial copy is shallow: `content` array is shared with later snapshots; consumers must not mutate.
- Stream throw / `streamFn` rejection / hook throw: propagates out of runLoop. `runAgentLoop` rejects, no `message_end` for a started partial, no `turn_end`/`agent_end`. `Agent.runWithLifecycle` (A:523-530, 532-548) catches and emits `message_start`,`message_end`,`turn_end`,`agent_end(messages:[failureMessage])` for synthetic assistant `{content:[{type:"text",text:""}], usage: EMPTY_USAGE, stopReason: aborted? "aborted":"error", errorMessage}`; finishTurn NOT called; `aborted` decided by signal state. State messages get it only via `message_end` (A:575-578).
- Stop reasons (`ai/src/types.ts:450`): `pending|stop|length|toolUse|error|aborted|deferred`. Loop special-cases only `error`,`aborted`,`length`; `deferred`/`pending`/`stop`/`toolUse` all flow as normal: tool calls (if any) are executed regardless of `stopReason` != `toolUse`. `done.reason` only in `stop|length|toolUse|deferred`; `error.reason` only `aborted|error` (`ai/src/types.ts:778-783`). `deferred` is handled in harness (`agent/src/harness/agent-harness.ts:168,258`), out of loop scope.

## 4. Abort (H-LOOP-08)

- Signal is a plain `AbortSignal | undefined`, passed to: transformContext, getApiKey NOT (no signal), convertToLlm NOT, streamFn options.signal, prepareRequest, finishTurn, beforeToolCall, afterToolCall, tool.execute. NOT passed to prepareNextTurn by loop (Agent wrapper injects its own `this.signal`, A:484-492), getSteering/FollowUp.
- Loop never inspects `signal` between turns or before streaming; abort during streaming relies on streamFn emitting `error{reason:"aborted"}` (contract). Resulting aborted AssistantMessage: pushed into context and newMessages (stays in transcript; provider layer skips error/aborted assistants on replay, `ai/src/api/transform-messages.ts:195-201`), then `finishTurn` -> `turn_end(msg, [])` -> `agent_end`. No tool execution for that message even if it contains tool calls.
- Abort checks in tool phase (sequential, L:541-578 / parallel, L:596-660):
  - Per call after `tool_execution_start`, preflight `prepareToolCall` returns immediate `"Operation aborted"` (isError) if `signal.aborted` after `beforeToolCall` (L:737-743) or just before returning prepared (L:756-762). Aborted check is AFTER the beforeToolCall await, so a hook that aborts yields aborted result even if hook returned block.
  - Sequential: after each call's end + result message, `if signal.aborted break` (L:575-577). Remaining calls: NO `tool_execution_start`, NO result, NO events.
  - Parallel: preflight loop `break`s on `signal.aborted` after each preflight (L:613-615 immediate case, L:641-643 prepared case). Calls already preflighted-as-prepared become closures; each closure checks `signal.aborted` at start and returns `"Operation aborted"` with `tool_execution_end` (L:619-628). Calls after the break point: no events, no result. Running tools are not forcibly stopped; they must honor the signal.
  - Result: assistant message may contain tool calls with no matching toolResult. `hasMoreToolCalls = !terminate` where terminate is computed only over finalized calls (usually false) -> `finishTurn`, `turn_end`, steering poll, next iteration calls streamFn again with the aborted signal; a compliant streamFn returns `error/aborted` immediately -> hard exit. So expect one extra (instantly aborted) assistant message after aborted tools. Orphaned toolCalls get synthetic `"No result provided"` isError results at provider-conversion layer on replay (`transform-messages.ts:158-180,192,223`), not in the loop.
  - `Operation aborted` text: `createErrorToolResult("Operation aborted")` = `{content:[{type:"text",text:"Operation aborted"}], details:{}}` (L:905-910). Go port decision: either replicate or emit "Operation aborted" results for ALL unstarted calls (cleaner; no orphans). Flag for design.
- Hooks `beforeToolCall`/`afterToolCall` are "responsible for honoring" the signal (T:325,340).
- In `executePreparedToolCall` the tool is awaited; abort does not race it (L:829). Tool throw -> error result with `error.message` (L:841-847).

## 5. Truncated-output guard (H-LOOP-11) L:264-270, 478-503

Trigger: `message.stopReason === "length"` AND `toolCalls.length > 0` (message with `length` and no tool calls is a normal end). Not for other stop reasons.
For EACH tool call in order (even ones that look complete): emit `tool_execution_start{id,name,args}` -> `tool_execution_end{result, isError:true}` -> tool-result message_start/message_end. No tool lookup/validation/hooks (before/afterToolCall NOT called), no `tool_execution_update`. Returns `terminate:false` -> loop continues so model can re-issue (test:415-486 expects 2 provider calls).
Exact text (L:493), `${toolCall.name}` interpolated:
`Tool call "<name>" was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments.`
Rationale in L:471-477: streaming arg parser uses salvage (`partialjson` analog) so truncated args can validate. Related CHANGELOG:196 (fix for harness).

## 6. Error/aborted ends run (H-LOOP-13)

`stopReason` `error|aborted` (L:245-256): context+newMessages already contain message; `lastCompletedTurn` set (toolResults `[]`); `await finishTurn` (decision ignored); `turn_end{message, toolResults:[]}`; `agent_end{messages:newMessages}`; return. No steering/follow-up poll, no prepareNextTurn. `Agent` records `errorMessage` from `turn_end` message (A:594-598). Note no retry in loop (retry is provider-layer `maxRetryDelayMs` / harness).

## 7. Tool execution details (shared with other lanes; loop-relevant parts)

- Mode selection (L:508-523): sequential if `config.toolExecution==="sequential"` OR any call's tool has `executionMode==="sequential"` (lookup by name in `currentContext.tools`); else parallel (default "parallel", T:315).
- Per call (both modes): emit `tool_execution_start{toolCallId,toolName,args: raw toolCall.arguments}` -> `prepareToolCall`.
- `prepareToolCall` (L:707-776): unknown tool => immediate error `Tool ${name} not found` (L:719; no hook call). Lookup `tools.find(name)` => first match wins for duplicate names. Then in try: `tool.prepareArguments(args)` (if present; result used only for validation, L:725; the hook/execute see validated clone, events/`toolCall` keep ORIGINAL args), `validateToolArguments` (structuredClone, null normalize, TypeBox Convert/coercion, `ai/src/utils/validation.ts:317-350`; failure message `Validation failed for tool "<name>":\n  - path: msg\n\nReceived arguments:\n<json>`), `beforeToolCall({assistantMessage, toolCall, args: validated, context}, signal)`; hook may mutate `args` (executed without revalidation, test:487). Any throw in this try (prepareArguments, validation, beforeToolCall) => error result with `error.message`. `block` => error result `reason || "Tool execution was blocked"`, `terminate` copied if `=== true`.
- `executePreparedToolCall` (L:820-851): `tool.execute(id, args, signal, onUpdate)`; `onUpdate` collected as promises, ignored after settle (`acceptingUpdates`), all awaited before returning (updates ordered before `tool_execution_end`); result `isError = result.isError === true`; thrown error => `createErrorToolResult(message)`, isError true.
- `finalizeExecutedToolCall` (L:853-903): `afterToolCall` runs only for prepared+executed calls (NOT for unknown/validation/blocked/aborted/truncated). Merge: content/details/usage/terminate replace if `!== undefined` (`??`); `isError` `??`; structuredContent kept unless `content` replaced without it. Throwing afterToolCall => error result from `error.message`, isError true (terminate/ details lost).
- Tool-result message (L:922-935): `{role:"toolResult", toolCallId, toolName, content: result.content ?? [], details, usage, isError, timestamp: Date.now()}`; NOTE `structuredContent` and `terminate` are NOT on the message (events only).
- Sequential order (L:541-578): start, [exec], `tool_execution_end`, then result `message_start/end`, per call.
- Parallel order (L:596-660): ALL starts emit during sequential preflight; immediate outcomes emit `tool_execution_end` right in preflight loop; prepared ones run concurrently, `tool_execution_end` in completion order; then ALL result messages emitted in assistant source order after `Promise.all` (L:646-654). Note start events of call N+1 follow preflight of N but may interleave with already-started? No: closures only start at `Promise.all`, after all preflights; so no execution overlaps preflight.
- `shouldTerminateToolBatch` (L:689-691): `finalized.length>0 && every(result.terminate===true)` (H-LOOP-12). Mixed batch => continue. Blocked call with `terminate` participates. Parallel passes `orderedFinalizedCalls`; sequential `finalizedCalls` (abort-truncated for sequential/parallel).
- Duplicate tool call ids: no dedupe, each executes; `Agent.pendingToolCalls` is a Set by id so events collide in UI state only.
- Empty tool call list: `toolCalls.length===0` -> no execution, `terminate` irrelevant, `toolResults=[]`, `hasMoreToolCalls=false`.
- `runToolCall` exported (L:810-818): same pipeline without events/messages for nested tool calls (hooks apply). Never rejects for tool failures.

## 8. Hook set (H-LOOP-14) - `AgentLoopConfig` T:193-342

| Hook | Signature | Called | May return | Throw contract |
|---|---|---|---|---|
| `convertToLlm` (required) | `(AgentMessage[]) => Message[]\|Promise` | every request, L:395 (after transformContext) | LLM messages; filter UI-only | MUST NOT throw (T:203-204): throw kills run w/o event sequence |
| `transformContext` | `(msgs, signal?) => Promise<AgentMessage[]>` | every request, L:391 | pruned/injected view, not persisted | MUST NOT throw (T:231) |
| `getApiKey` | `(provider) => string\|undefined\|Promise` | every request, L:400 | key; falsy => `config.apiKey` | MUST NOT throw (T:253) |
| `prepareRequest` | `(PrepareRequestContext{context,model,thinkingLevel}, signal?) => update\|void\|Promise` | before every request incl. first, after pending msgs emitted, L:219 | `{context?, model?, thinkingLevel?}` persisting for later requests | not declared; unguarded (throw = run dies) |
| `prepareNextTurn` | `(PrepareNextTurnContext) => AgentLoopTurnUpdate\|undefined` | start of non-first turn, after previous turn_end, before `turn_start`, L:186 | `{context?, messages?, model?, thinkingLevel?}` (messages emitted+appended) | unguarded. Loop does not pass signal (Agent injects) |
| `finishTurn` | `(AgentTurnContext, signal?) => {action:"continue"\|"end"}\|void` | after tool results appended, BEFORE `turn_end`; also for error/aborted | decision applied after `turn_end` | unguarded |
| `getSteeringMessages` | `() => Promise<AgentMessage[]>` | see section 2 | `[]` when none | MUST NOT throw (T:291) |
| `getFollowUpMessages` | same | inner loop exit only, L:302 | `[]` | MUST NOT throw (T:304) |
| `beforeToolCall`/`afterToolCall` | section 7 | | | before: throw => error result; after: throw => error result |
Others: `toolExecution`, `model`, `reasoning` (ThinkingLevel w/o "off", undefined = off). `AgentContext{messages, tools?}`; `AgentTurnContext{message, toolResults, context, newMessages}` (T:134-144). Return `undefined`/falsy from poll hooks is coerced to `[]` (`|| []`).
Agent queue modes: `QueueMode "all"|"one-at-a-time"` (T:55), queue drain in `A:165` (not in loop).

## 9. Tool loadout in transcript (H-LOOP-15, P1) L:323-375, TR:58-167

- `context.tools` = executable set; transcript system messages declare what model may call. Model-visible tools = replay of all system messages' `toolsRemoved` then `toolsAdded` (`getCurrentTools`, TR:58-66; order within a message: removals then additions; Map by name).
- Before EVERY request (loop top) and at run start (prompt run), `declareToolChanges(context, pendingMessages)` diffs `getCurrentTools(context.messages + pending)` vs `(context.tools).map(toToolDeclaration)` (L:347-350; `toToolDeclaration` strips execute/label, JSON round-trips `parameters`, keeps `constrainedSampling`, TR:123-130).
- `getToolStateChanges` (TR:150-167): added = new or declaration-changed; removed = gone or changed (changed = remove+add); comparison by serialized declaration equality (TR:140-142).
- If no delta and no pending system message: pass through unchanged. If delta: pending messages' last system message (search from end, L:335-340) has its tool fields REPLACED by delta (caller's tool fields treated as intent only; baseline computed with that message's tool fields zeroed, L:342-346); if that message already declares nothing and delta empty, the original object is kept (L:355). Else insert new system message `{role:"system", content:"", toolsAdded?, toolsRemoved?, timestamp: Date.now()}` before first non-system pending message, or at end (L:358-362).
- `withToolChanges` omits empty lists (L:368-375).
- Delta system messages go through normal events and ARE pushed to context and newMessages (L:211-215, L:110-122). Initial leading system message for a fresh session: `createInitialSystemMessage` (`timestamp: 0`, TR:10-23); used elsewhere (Agent initialState).
- Gap: tool changes introduced by `prepareRequest` context replacement are declared only next iteration; first request after replacement lacks delta.
- Ask port: needs system-message role with `content|sections|toolsAdded|toolsRemoved` in message model; if Ask keeps classic system prompt+tools per request, this subsystem can be collapsed, but then H-LOOP-15 transcript semantic (replay yields exactly context.tools) is lost.

## 10. Early termination (H-LOOP-12, P1)

`AgentToolResult.terminate`, `BeforeToolCallResult.terminate`, `AfterToolCallResult.terminate` (T:73,103,445). Batch terminates only if every finalized result has `terminate===true` (L:689-691). Effect: `hasMoreToolCalls=false` -> normal turn_end, then steering/follow-ups still polled and may continue; no extra LLM call otherwise. Added CHANGELOG:364 (issue 3525), blocked variant :90. Truncated-batch path always `terminate:false`.

## 11. Context-mutation rules summary

| Thing | In context.messages | In newMessages | Events |
|---|---|---|---|
| prompt msgs (+ system delta) | yes (before loop) | yes | message_start/end after turn_start |
| steering/follow-up/prepared msgs | pushed after events | yes | message_start/end |
| assistant partial | pushed at `start`, replaced per update, replaced by final | pushed after stream returns | start/update/end |
| tool results | pushed after whole batch | after whole batch | message_start/end per result (during batch) + tool_execution_* |
| transformContext output | never | never | none |
| convertToLlm output | never | never | none |
| `message_update` | n/a | n/a | assistant only, copy `{...partial}` |
Agent class state (A:565-578) rebuilds messages from `message_end` events, independent of loop's internal context array (snapshot `slice()`, A:460-465).

## 12. CHANGELOG-derived behavior history (agent/CHANGELOG.md)

- Removed `shouldStopAfterTurn` (0.87.0, :20-34); replaced by `finishTurn` (:39). Old hook ran only for normal responses; new runs for all incl. error/aborted but decision ignored there.
- `prepareNextTurn` moved to run only when another turn will start (0.84.4, :63).
- `prepareRequest` added 0.87.0 (:38); `peekQueuedMessages` (:40).
- `thinkingLevel` stamped on assistant (0.99.0, :9); `onProviderStreamEvent` option (:8).
- Streamfn required in loops; default via `setDefaultStreamFn` (:151,:159).
- Steering waits for whole tool batch (:473). Parallel tool execution + source-order results (:489). Terminate hint (:364, :90). Length-truncation guard (:196).
- "cumulative partial": grep found no occurrence in agent CHANGELOG; in this version each provider event carries a full `partial` snapshot and the loop just swaps it in (L:433-434). Nothing to port.

## 13. Port recommendations (ranked, for Ask)

1. Port `runLoop` structure 1:1 (event sink callback, nested loops, `lastCompletedTurn`, `explicitContinuation`); it is small (160 LOC) and subtle ordering is the value.
2. Wrap hook/stream failures inside the loop (recover to a synthetic error assistant message + `turn_end` + `agent_end`) rather than delegating to an outer class: removes Pi's hang/no-agent_end footgun. Deviation to record as decision.
3. Provider stream = channel of events; treat channel close without `done|error` as error message (avoid Pi hang at L:460).
4. On abort mid-batch, emit "Operation aborted" results for every unstarted call (deviates from Pi L:575-577,613,641 but avoids orphans + the extra aborted request). Decide explicitly.
5. Keep truncated-guard text byte-exact (tests may compare).
6. H-LOOP-15 system-message tool loadout: defer unless Ask's message model already has mutable system messages; P1.

## Limitations

- Did not read harness (`agent/src/harness/*`) loop usage, proxy, or provider-side `transform-messages` beyond the orphan patch lines. GitNexus MCP not used (direct file reads sufficed).
- Abort-during-tools behavior is derived from source only (no tests).

## Unresolved questions

1. Should Ask replicate Pi's orphaned tool calls on mid-batch abort, or emit "Operation aborted" for all unstarted calls? (affects transcript persistence format and replay.)
2. Should loop-internal recover (item 2) be adopted, or keep Pi's "hooks must not throw" contract with an outer Agent-layer catch?
3. Is the transcript-system-message tool loadout (H-LOOP-15) in scope for the first milestone, or does Ask use request-level system prompt + tools?
4. Does Ask want `stopReason` `deferred`/`pending` in its provider model now (loop treats them as normal ends)?
5. `finishTurn`, `prepareRequest`, `prepareNextTurn` have no "must not throw" doc but are unguarded; confirm Ask contract wording.
