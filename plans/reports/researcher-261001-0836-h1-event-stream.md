# H1 scout: Pi event streams

Date: 2026-10-01. Source: Pi commit 2bbfcca43 (read only, not run). Scope: H-LOOP-16, H-MODE-04 (agent-core part), H-MODE-05.
Shorthand: `AI:` = `packages/ai/src/`, `A:` = `packages/agent/src/`, `C:` = `packages/coding-agent/src/`, `CD:` = `packages/coding-agent/docs/`.
The research agent could not write this file, so the lead saved it from the agent's reply. The lead checked the key claims with GitNexus and grep (marked "verified").

## 0. Outcome

- Source confirms H-LOOP-16, H-MODE-04 (agent-core part) and H-MODE-05.
- The H-LOOP-16 line numbers drifted. The "no `done`" fallback to `result()` is at `A:agent-loop.ts:460` (verified by grep). `A:types.ts:26-36` and `A:types.ts:514-529` are still correct.
- Main gap G1: if a stream ends with no `done`, no `error` and no result, `result()` never resolves, so the loop hangs. Verified with GitNexus: `EventStream.end(result?)` resolves the promise only when `result` is given (`AI:utils/event-stream.ts:26-89`). Ask must close this hole in its stream type.

## 1. Feature tree

1. Provider-level stream (`AssistantMessageEvent`)
   - Union `AI:types.ts:767-783`. `EventStream<T,R>` and `AssistantMessageEventStream` `AI:utils/event-stream.ts:26-110` (`push`, `end`, async iterator, `result()`).
   - `StreamFunction` `AI:types.ts:371-375`. Provider `stream` and `streamSimple` `AI:types.ts:287-292`.
   - Options: `ProviderRequestOptions` (`:132-185`), `StreamOptions` (`:187-237`), `SimpleStreamOptions` (`:350-358`).
   - Reference emitter: faux `AI:providers/faux.ts:340-433`. Real terminal pattern: `AI:api/anthropic-messages.ts:807-838`.
2. Agent-core stream (`AgentEvent`)
   - Union `A:types.ts:514-529`. `StreamFn` `A:types.ts:26-36`. Default registry `A:stream-fn.ts:1-20`.
   - `runLoop` `A:agent-loop.ts:150-330`. `streamAssistantResponse` `A:agent-loop.ts:381-471`.
   - Tool batches: sequential `:530`, parallel `:586`, partial results `executePreparedToolCall` `:820-851`.
   - Listener dispatch and run-failure synthesis `A:agent.ts:266, 532-547, 565-612`.
3. Session-level stream (`AgentSessionEvent`) `C:core/agent-session.ts:180-231`; `_emit` `:998`, `_emitAgentSettled` `:1034-1058`.
4. Wire serializer (JSON mode): `C:modes/json-event.ts:1-61`; header and backpressure `C:modes/print-mode.ts:101-125`; stdout guard `C:core/output-guard.ts`; spec `CD:json.md`.
5. Print mode (text) `C:modes/print-mode.ts:134-150`.

## 2. Event inventory

### 2a. Provider level (`AI:types.ts:767-783`). H1 owns the type. Not wire events, except as `message_update` sub-events.

| type | Fields | Note |
|---|---|---|
| `start` | `partial` | Loop turns it into `message_start`. |
| `text_start` / `thinking_start` / `toolcall_start` | `contentIndex, partial` | |
| `text_delta` / `thinking_delta` / `toolcall_delta` | `contentIndex, delta, partial` | `toolcall_delta.delta` is a JSON text fragment. |
| `text_end` / `thinking_end` | `contentIndex, content, partial` | `content` is authoritative. |
| `toolcall_end` | `contentIndex, toolCall, partial` | Full `ToolCall`. |
| `done` | `reason: stop/length/toolUse/deferred`, `message` | Terminal. |
| `error` | `reason: aborted/error`, `error: AssistantMessage` | Terminal. Message has `errorMessage`. |

### 2b. Agent core (`A:types.ts:514-529`). H1 owns the types; H2 first emits them.

| type | Fields | When |
|---|---|---|
| `agent_start` | none | Run start. |
| `agent_end` | `messages` | Last loop event; messages created in this run. No `willRetry` (that is session level, H9). |
| `turn_start` | none | Each turn. |
| `turn_end` | `message, toolResults` | After tools. Error or abort gives `toolResults: []`. |
| `message_start` / `message_end` | `message` | Any role. |
| `message_update` | `message, assistantMessageEvent` | Assistant only, while streaming. |
| `tool_execution_start` | `toolCallId, toolName, args` | Before preparation, validation and hooks. `args` = raw arguments. |
| `tool_execution_update` | `toolCallId, toolName, args, partialResult` | Tool called `onUpdate`. |
| `tool_execution_end` | `toolCallId, toolName, result, isError` | After `afterToolCall`. |

### 2c. Session level (`C:core/agent-session.ts:180-231`). Later phases only.

| Event | Owner phase |
|---|---|
| `agent_end.willRetry`, `agent_settled`, `queue_update`, `auto_retry_start/end` | H9 |
| `entry_appended`, `session_info_changed` | H8 (H11 for extension `appendEntry`, H15 for names) |
| `thinking_level_changed` | H6/H7 (owner not found) |
| `compaction_start/end`, `summarization_retry_*` | H10 (H14 for branch summary) |
| `bash_execution_update` | H13/H15 |
| `parentToolCallId` on `tool_execution_*` | X1 |
| RPC `extension_error`, `extension_ui_request` | H11/X1, M2 |

## 3. Ordering invariants (from `A:agent-loop.ts`)

1. `agent_start`, then `turn_start`, come first.
2. Prompt messages: `message_start` then `message_end`, no updates.
3. Assistant: `message_start` comes from provider `start` (`:416`); if no `start` came, it is emitted just before `message_end` (`:445-452`, `:460-468`). `message_update` only for the nine block events. Provider `start`, `done`, `error` are never updates. `message_end` carries the final message from `result()`.
4. Error or abort ends the run: `turn_end{message, toolResults: []}`, then `agent_end` (`:245-258`). `finishTurn` still runs; its decision is ignored (agent 0.87.0).
5. Tools run only after the assistant `message_end`.
6. Sequential tools: `tool_execution_start`, updates, `tool_execution_end`, then the result `message_start`/`message_end`.
7. Parallel tools: starts in source order; `tool_execution_end` in completion order; result messages afterwards in source order (agent 0.68.1).
8. `stopReason: length` fails every tool call without running it (`:265-272`, agent 0.80.4).
9. `tool_execution_update` after the tool settled is dropped (agent 0.79.2).
10. `agent_end` is last. The agent is idle only after awaited listeners of `agent_end` finish (agent 0.65.0).

Sequence for one turn with thinking, text and two parallel tool calls:

```
agent_start
turn_start
message_start{user}  message_end{user}
message_start{assistant, content:[], stopReason:"pending"}
  message_update thinking_start{0} / thinking_delta* / thinking_end{0}
  message_update text_start{1}     / text_delta*     / text_end{1}
  message_update toolcall_start{2} / toolcall_delta* / toolcall_end{2}
  message_update toolcall_start{3} / toolcall_delta* / toolcall_end{3}
message_end{assistant, stopReason:"toolUse", usage}
tool_execution_start{A}  tool_execution_start{B}
  tool_execution_update{A}*
tool_execution_end{B}  tool_execution_end{A}        (completion order)
message_start/end{toolResult A}  message_start/end{toolResult B}   (source order)
turn_end{message, toolResults:[A,B]}
turn_start ... message_end{assistant, stopReason:"stop"}  turn_end{toolResults:[]}
agent_end{messages:[user, asst1, resA, resB, asst2]}
```

## 4. Wire format (JSON mode, delta only)

- Strict JSONL. Split on LF only; strip an optional CR (coding-agent 0.57.0: U+2028/U+2029 broke `readline`).
- Stdout carries only the protocol (0.62.0). First line is the session header `{"type":"session","version":3,"id":..,"timestamp":..,"cwd":..}` (`print-mode.ts:101-107`). Pi has no envelope (`seq`, `ts`, `sessionId`, `runId`).
- `json-event.ts:25-61`: only `message_update` changes. The cumulative `message` is dropped and replaced by top-level `usage`. Every nested `partial` is dropped. `toolcall_start` gains `id` and `toolName`, read from `partial.content[contentIndex]` (throws if not a tool call).
- Coding-agent 0.84.0 (CHANGELOG line 590, verified) made `message_update` delta only. 0.84.2 (line 527, verified) restored cumulative usage as a top-level field. The agent CHANGELOG 0.84.0 does not mention it; cite the coding-agent changelog.
- Client rules (`CD:json.md`): append `delta`; replace a block with `*_end` content; replace the whole message with `message_end.message`.
- `usage` on `message_update` is the latest cumulative provider usage; it can stay zero until the end.
- Print text mode prints the text blocks of the last assistant message. Exit 1 when `stopReason` is `error` or `aborted` (`print-mode.ts:134-150`).

Example lines:

```json
{"type":"message_update","usage":{...},"assistantMessageEvent":{"type":"toolcall_start","contentIndex":2,"id":"call_1","toolName":"bash"}}
{"type":"message_update","usage":{...},"assistantMessageEvent":{"type":"toolcall_delta","contentIndex":2,"delta":"{\"command\":"}}
{"type":"message_update","usage":{...},"assistantMessageEvent":{"type":"toolcall_end","contentIndex":2,"toolCall":{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"ls"}}}}
```

## 5. Edge cases

| Case | Version |
|---|---|
| JSON mode exited before flushing final events | coding-agent 0.24.0 |
| `tool_execution_update` and `onUpdate` added | ai 0.22.3 |
| Listeners awaited and given the abort signal | agent 0.65.0 |
| Parallel tools: end in completion order, results in source order | agent 0.68.1 |
| Anthropic stream ending before `message_stop` is an error | ai 0.71.0 |
| Completions stream with no `finish_reason` is an error | ai 0.74.1 |
| `partialJson` scratch buffer leaked into saved tool calls | ai 0.67.2 |
| `toolcall_delta` when args arrive only in `.done` | ai 0.65.0 |
| `willRetry` on `agent_end`; settlement uses the awaited lifecycle | coding-agent 0.75.4 |
| Follow-ups queued by `agent_end` handlers drain before idle | coding-agent 0.77.0 |
| Late tool progress after settlement ignored | agent 0.79.2 |
| Strict LF framing | coding-agent 0.57.0 |
| `pending` stop reason for partial messages | ai 0.83.0 |
| Delta-only `message_update` (breaking) | coding-agent 0.84.0 |
| Top-level `usage` on `message_update` | coding-agent 0.84.2 |
| Quadratic CPU draining a buffered `EventStream` | ai 0.86.0 |
| `finishTurn` runs on error and abort, decision ignored | agent 0.87.0 |
| `RpcClient` skipped a listener when one unsubscribed during dispatch | coding-agent 0.99.0 |

## 6. Gaps against the H1 rows

| # | Gap | H1 action |
|---|---|---|
| G1 | Stream closed with no terminal event and no result hangs `result()` | Own: Go stream always yields a final message; early close gives an `error` message ("stream ended without a terminal event"). |
| G2 | Two stream contracts: agent `StreamFn` forbids throwing; AI `StreamFunction` allows a sync throw for missing auth. Agent has a run-failure safety net (`A:agent.ts:532-547`). | Own: Go stream function returns no Go error for runtime failures. The loop safety net is H2. |
| G3 | H-MODE-04 row lists `agent_end{messages, willRetry}`; core has only `messages` | Own: core `agent_end` without `willRetry`; H9 adds it. |
| G4 | Pi has no envelope; Ask adds one | Own in `pkg/protocol`. Emitter that assigns `seq` is H2. |
| G5 | H-MODE-05 lists `done`/`error` as wire sub-events; the loop never sends them as updates | Own: leave them out of the wire union of `message_update`. |
| G6 | "`message_end` carries usage, model, provider" | These are fields of the assistant message. Do not add separate event fields. |
| G7 | Listener order and error policy | Pi: ordered, awaited, no try/catch. Dispatch contract belongs to H2 (emit sink) and H11 (bus). |
| G8 | Backpressure: `EventStream` is unbounded; JSON mode gets backpressure from an awaited listener | Own for the stream type; sink backpressure in H2 and H13. |
| G9 | Tool-change system messages, `length` truncation path, `parentToolCallId` | Defer: H5/H6/H8, H2, X1. |

## 7. Go port notes

- Provider stream: a struct with a receive-only event channel plus `Result()`. Producer goroutine sends exactly one terminal event, then closes. `Result()` comes from the terminal event, or a synthesized `error` message when the channel closes without one. Every send selects on `ctx.Done()`.
- Agent-core emit: a callback sink `func(ctx, Event) error`, awaited, in order. This keeps Pi's awaited-listener behavior and gives JSON mode backpressure. Bus fan-out (H11) wraps the sink.
- Envelope in `pkg/protocol`, flat JSON with `type` discriminator (same shape as Pi lines, so `jq` filters keep working). Unknown types decode to a raw event.
- Delta only from day 1: providers send deltas; one accumulator builds the partial message. Put `id` and `toolName` on the provider `toolcall_start` event, so the serializer never reads `partial`.
- Zero `usage` serializes as an object, not `null`.
- JSONL writer: one write per line, a mutex, flush on exit.

## Unresolved questions

1. Keep Pi's run-failure safety net (H2)?
2. Who assigns `seq` and `runId`: the agent emit sink (H2) or the bus (H11)?
3. On a listener error: abort the run (Pi) or log and continue?
4. Which phase emits `thinking_level_changed`?

Status: DONE_WITH_CONCERNS
Summary: The three H1 rows are confirmed against source. Nine gaps listed.
