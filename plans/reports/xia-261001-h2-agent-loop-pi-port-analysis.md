# H2 port analysis: the agent loop, print mode and JSON mode (Pi to Ask)

Date: 2026-10-01. Mode: `xia --port`, stopped after analysis (user request: write the report and stop; no plan, no implementation).

## 0. Source manifest

| Item | Value |
|---|---|
| Source | Pi monorepo, `/Users/dale/Desktop/workspace/opensources/pi`, commit `2bbfcca437c3`. GitNexus repo `pi`, indexed at the same commit. |
| License | MIT (`LICENSE`, "Copyright (c) 2025 Mario Zechner"). A port reads behavior; no code is copied. |
| Scope | `packages/agent/src/agent-loop.ts`, `types.ts`, `agent.ts`; `packages/ai/src/utils/validation.ts`, `api/transform-messages.ts`; `packages/coding-agent/src/{main.ts, cli/args.ts, cli/file-processor.ts, cli/initial-message.ts, modes/print-mode.ts, modes/json-event.ts, core/output-guard.ts, core/agent-session.ts (parts)}` |
| Local | Ask `master-2`. H1 is done (`pkg/protocol`, `internal/providers` with faux, sse, partialjson). `go build ./...` passes. `internal/logs/` is still untracked (not ignored any more). |
| Plan read | `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` section H2, plus the inventory rows that H2 owns |

Detail reports (all claims there carry `file:line`):
- `researcher-261001-h2-pi-loop-core.md`: the two-level loop, the stream contract, abort, the guards, the hooks, the tool loadout.
- `researcher-261001-h2-pi-tool-pipeline.md`: tool execution, coercion, the result shape, pairing.
- `researcher-261001-h2-pi-agent-wrapper-modes.md`: the `Agent` wrapper, mode selection, print mode, JSON mode, the CLI.

Path shorthand in this report: `AL` = `agent/src/agent-loop.ts`, `A` = `agent/src/agent.ts`, `T` = `agent/src/types.ts`, `V` = `ai/src/utils/validation.ts`, `TM` = `ai/src/api/transform-messages.ts`, `AS` = `coding-agent/src/core/agent-session.ts`, `PM` = `coding-agent/src/modes/print-mode.ts`.

## 1. What H2 must deliver (from the roadmap)

- **Concept:** call the model, run the tool calls, feed back the results, stop when the model stops.
- **Owns:** H-LOOP-01, 02, 08, 09, 10, 11, 13, 14 (core set), 17 (state, listeners, `WaitForIdle`); H-TOOL-12, 13, 21 (loop part); H-MODE-01, 02, 03 (P1, pulled); H-CONF-09 (basic flags).
- **Also builds:** a tool-context struct, `SourceInfo` on tools, a context-source interface with an in-memory log.
- **Packages:** `agent`, `pipeline`, `tools` (interface, registry, `echo`), `cmd/tui` (headless `ask -p`).
- **Exit:** `ask -p "hello"` and `ask --mode json` run in process against faux plus echo, with the right exit codes.

## 2. How Pi implements it (summary)

### 2.1 The loop (`AL:163-321`)

The loop is a pure function of `(context, config, signal, streamFn)` that emits events to a sink. It is about 160 lines, and its ordering is the important part.

- **Outer loop:** follow-ups after a natural stop. **Inner loop:** runs while there are tool calls or pending steering messages.
- **Steering is polled at three points:** at run start, after each normal `turn_end`, and after `prepareNextTurn` only if the earlier poll was empty. It is never polled after error or aborted, and never when `finishTurn` returns `end`.
- **`finishTurn`** runs before `turn_end`, and its decision is applied after `turn_end`. It also runs for error or aborted, but there its decision is ignored. `continue` guarantees exactly one more request, even with no new input (a context-only request).
- **`prepareNextTurn`** runs only before a non-first turn, before `turn_start`. Its messages are emitted before the steering messages.
- **`prepareRequest`** runs before every request, including the first. A context it returns replaces the current context for this request and all later ones. Pi's session uses this hook to project the session log into the request (`AS:746-769`). **This is the hook that the roadmap's "context-source interface" maps to.** (GitNexus: `_installAgentRequestProjection`.)
- **Thinking level:** `"off"` maps to no reasoning option. The final assistant message is stamped with `thinkingLevel` (`AL:409`).

### 2.2 Stream contract (`AL:381-469`)

- `transformContext`, then `convertToLlm`, then normalize, then `getApiKey(provider)`, which falls back to `config.apiKey` when the result is empty. Then `streamFn`.
- On `start`, the partial message is pushed into the context. Each update replaces it, and `result()` replaces it at the end. Events that arrive before `start` are dropped.
- On `done` or `error`, the final message comes from `result()`, not from the event.
- If the stream closes without a terminal event, the code falls back to `result()`. **Pi hangs here** when the stream ended without a result. Ask H1 already avoids this: `Stream.Result(ctx)` never hangs.

### 2.3 Tool batch (`AL:508-935`)

- **Mode:** `parallel` by default. If any call's tool is `sequential`, the whole batch runs sequentially. There is no concurrency limit.
- **Parallel:** preflight runs in source order and ends at `beforeToolCall`. Then all prepared calls run together. `tool_execution_end` arrives in completion order. The result messages are emitted after the whole batch, in source order.
- **Pipeline:** find the tool (`Tool X not found`), then `prepareArguments`, then validate, then `beforeToolCall` (block: `reason || "Tool execution was blocked"`), then `execute(id, args, signal, onUpdate)`, then `afterToolCall`, which overrides fields one by one with `??`. Every exception becomes an error result. `afterToolCall` runs only for calls that executed.
- **Events and hooks** see the raw arguments. `execute` sees the validated arguments. A hook may mutate the arguments, and they are not validated again.
- **Late `onUpdate`** after the call settles is ignored.
- **Context:** the batch's tool results are appended to the context only after the whole batch, although their message events fire earlier.
- **Message:** the tool result message holds `content ?? []`, `details`, `usage`, `isError` and `timestamp`. `structuredContent` and `terminate` are in events only.

### 2.4 Guards

- **Length guard (`AL:478-503`):** applies when `stopReason == "length"` and the message has tool calls. Each call gets start and end events and an error result with the exact text: `Tool call "<name>" was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments.` No hooks run, and the loop continues.
- **Error or aborted (`AL:245-256`):** `finishTurn` (decision ignored), then `turn_end(msg, [])`, then `agent_end`. Tool calls in that message are not run.

### 2.5 Wrapper and modes

- **`Agent`:**
  - It allows one active run. `prompt()` while running throws. `reset()` while running throws.
  - Listeners are awaited in order. A throwing listener leads to a synthesized error assistant message plus `agent_end`.
  - `state.messages` is updated only on `message_end`.
- **Mode precedence:** `--mode rpc`, then `--mode json`, then print (when `-p` is set or stdin or stdout is not a TTY), then interactive. `--mode text` does not force print mode.
- **Output guard:** in non-interactive modes, `process.stdout.write` is redirected to stderr. Protocol output goes through `writeRawStdout`. EPIPE gives exit 1.
- **Print mode:**
  - Output: the text blocks of the last assistant message, each followed by `\n`.
  - Error or aborted: stderr message, exit 1. A thrown error also gives exit 1.
  - Signals: SIGTERM gives 143 and SIGHUP gives 129. There is no SIGINT handler.
- **JSON mode:**
  - Output: one record per line, ended by `\n`. The first record is the session header. Each prompt ends with `agent_settled`.
  - Exit code: 0 even when the assistant ends in error.
  - Backpressure: a listener waits until stdout drains, so a slow reader stalls the loop.
- **Input:** piped stdin, then `@file` text, then `messages[0]`, joined with no separator. The other messages run as further prompts in sequence.

## 3. H2 row coverage

| Row | Pi evidence | Local status | Gap or note |
|---|---|---|---|
| H-LOOP-01 | `AL:38-151`. `Continue` with an assistant tail throws. Continue mutates the caller's array. | `agent` is a scaffold | Note: Pi only rejects an assistant tail. A system or custom tail passes. |
| H-LOOP-02 | `AL:163-321` | — | Port the structure 1:1 (`lastCompletedTurn`, `explicitContinuation`). |
| H-LOOP-08 | `AL:575-577, 613-643, 737-762` | — | **The inventory text is wrong.** See G2. |
| H-LOOP-09 | `AL:508-660` | errgroup is available | Keep both orders (end events and result messages). |
| H-LOOP-10 | `AL:707-903` | — | See section 2.3. |
| H-LOOP-11 | `AL:478-503` | — | Keep the text byte-exact. |
| H-LOOP-13 | `AL:245-256` | — | `finishTurn` still runs. |
| H-LOOP-14 | `T:193-342` | `pipeline` is a scaffold | The roadmap list leaves out `prepareNextTurn`, `getSteeringMessages` and `getFollowUpMessages`. See G5. |
| H-LOOP-16 | `T:19-37`, `AL:381-469` | `providers.Stream`, `Result(ctx)` ready | H1 already fixes Pi's hang. |
| H-LOOP-17 | `A:70-612` | — | Listener failure policy is still open (H1 plan 5.3). See G4. |
| H-TOOL-12 | `V:59-350` | `santhosh-tekuri/jsonschema/v6` in go.mod (indirect) | Pi has two coercion layers. See G7. |
| H-TOOL-13 | `T:424-446`, `AL:922-935` | `protocol.ToolExecutionResult` has `StructuredContent` and `Terminate`. `ToolResultMessage` does not (matches Pi). | OK. |
| H-TOOL-21 (loop) | `AL:267-270, 575-577` | — | See G2. |
| H-MODE-01 | `main.ts:112-122`, `output-guard.ts` | `cmd/tui/main.go` is a bubbletea scaffold | The output guard is not listed in H2. See G6. |
| H-MODE-02 | `PM:33-169` | — | Exit codes, signals. |
| H-MODE-03 | `PM:105-127`, `json-event.ts` | The `JSONLWriter` codec is ready. `message_update` is `{assistantMessageEvent, usage}`. `toolcall_start` carries `ID` and `ToolName` (verified at `pkg/protocol/stream_events.go:88-95` and `codec.go:61-62`). | The terminal record `agent_settled` is missing (G1). The header record is optional until H8: Pi skips it when there is no header (`PM:122-127`). |
| H-CONF-09 (basic) | `args.ts:95-255` | cobra is an indirect dependency | Several silent-fallback rules. See G8. |

## 4. Gaps in the plan (what the roadmap misses or states wrongly)

### G1. JSON mode has no terminal record in H2

- **Pi:** each prompt ends with `agent_settled`, and readers wait for it (`json.md:48`, `cli-integration.md:55`). The first record is the session header.
- **Roadmap:** `agent_settled` belongs to H-LOOP-21 in H9, and the header comes in H8. H2 owns H-MODE-03 as "(P1, pulled)", but its terminal record does not exist yet.
- **Options:**
  - **(a)** Pull a minimal `agent_settled` into H2, emitted right after `agent_end`. There is no retry or compaction yet. Mark it "(H9 row, pulled: minimal)", and let H9 move the emit point.
  - **(b)** H2 JSON ends at `agent_end`, and the contract changes in H9.
  - **(c)** Defer JSON mode to H8.
- **Recommendation: (a).** A JSON reader then never needs to change. It costs one event type in `pkg/protocol` and one emit call.

### G2. H-LOOP-08 and the pairing invariant: Pi does not do what the inventory says

- **Inventory:** "Unstarted parallel tools get 'Operation aborted'".
- **Pi:** after an abort, the loop stops preflighting. Calls after the break point get **no events and no result** (`AL:575-577, 613, 641`). Only calls already prepared get "Operation aborted" (`AL:619-628`). The loop then makes one more `streamFn` call with the aborted signal. The orphan calls are fixed only at replay, by `transformMessages`, which writes "No result provided" (`TM:158-180`). That function belongs to H3. Both research lanes confirmed this independently. Pi has no test for this path.
- **Effect on H2:** the H2 test "pairing invariant after abort" (E§24#1) cannot pass inside the loop if Pi is copied exactly.
- **Options:**
  - **(a)** Deviate: emit an "Operation aborted" result (with start and end events) for every unstarted call, and skip the extra request after an abort. Pairing then holds in the loop, and the replay synthesis in H3 stays as a second safety net.
  - **(b)** Copy Pi exactly, test only the loop part in H2, and move the full invariant test to H3.
- **Recommendation: (a).** Record it as a deliberate departure, like D4. Correct the H-LOOP-08 inventory row text.

### G3. H-LOOP-15 (tool loadout in the transcript): the type exists, the delta algorithm does not

- **Local:** `protocol.SystemMessage` already has `Sections`, `ToolsAdded` and `ToolsRemoved`. `providers.NormalizeRequest` folds the system prompt and the tools into the leading system message (`internal/providers/convert.go:30-33`).
- **Pi:** `declareToolChanges` runs at run start and before every request (`AL:110, 211, 323-375`). It diffs the declared tools against `context.tools` and adds or edits a system message.
- **Roadmap:** the delta part is in H8 "(P1, pulled)". For H2, a run that starts in memory with a fixed tool set does the same as Pi's fresh session: the first system message declares all tools.
- **Recommendation:** H2 builds the initial system message the same way Pi does (`createInitialSystemMessage`, timestamp 0). Add a note to the H2 text: "tool set is fixed for the run; delta in H8". No change in phase ownership.

### G4. Hook and listener failure contract (a decision, not a given)

- **Pi:** the loop catches nothing. A throw from `convertToLlm`, `prepareRequest`, `finishTurn` or `streamFn` ends the loop with no `agent_end`. Only `Agent.runWithLifecycle` catches and builds the error message (`A:523-548`). The H1 plan section 5.3 left this open for H2.
- **Go:** a panic in a goroutine crashes the process. An error returned by a hook must be handled somewhere.
- **Options:**
  - **(a)** Recover inside the loop. Convert a hook error or panic into an error assistant message, then `turn_end` and `agent_end`.
  - **(b)** Copy Pi: the loop returns the error, and the `Agent` wrapper synthesizes the message.
- **Recommendation: (a) for errors returned by hooks and for panics in the tool goroutines. (b) for listener errors.** Pi also uses (b) for listeners.

### G5. The H-LOOP-14 hook list in the roadmap is incomplete for the two-level loop

- **Roadmap H2 core set:** `transformContext`, `convertToLlm`, `getApiKey`, `prepareRequest`, `finishTurn`, `beforeToolCall`, `afterToolCall`.
- **What the loop structure also needs:** `getSteeringMessages` and `getFollowUpMessages`, the polling points of the loop. Without them, the loop's outer level cannot exist. The queues come in H9, so H2 can pass functions that return empty lists.
- **`prepareNextTurn`** is P1 and can wait.
- **Recommendation:** add the two poll hooks to H2 as functions that return empty lists. H9 fills them.

### G6. The output guard (stdout only for protocol output) is not listed as a sub-feature

- **Pi:** `takeOverStdout` redirects all stdout writes to stderr. EPIPE gives exit 1, and stdout is flushed before exit (`output-guard.ts:45-108`). Changelog fixes 2729 and 5347 were regressions here.
- **Go:** the risk is lower, because there is no global `console.log`. Still, a library that writes to `os.Stdout`, or a zap logger set up for stdout, breaks JSON mode.
- **Recommendation:** in print and JSON mode, protocol output goes through one writer. Logs always go to stderr. Add a test: an EPIPE on stdout gives exit 1. A test that "print mode opens no listener" is already in H2.

### G7. Argument coercion: Pi has two layers, and the inventory names only one

- **Pi:** for TypeBox schemas (every built-in tool), `Value.Convert` runs first, then validation. The custom table (`V:59-131`) runs only for plain JSON schemas (MCP).
- **TypeBox `Value.Convert` rules** are indicative: they were read from a local `@sinclair/typebox` 0.34.49, while Pi pins `typebox` 1.3.27 (`packages/ai/package.json:79`). They are looser than the custom table:

| Target | TypeBox `Value.Convert` | Pi custom table |
|---|---|---|
| boolean | `"true"`, `"TRUE"`, `"1"`, `1` → true; `"false"`, `"0"`, `"-0"`, `0` → false (case-insensitive) | only `"true"`/`"false"` exact, 1/0, null→false |
| integer | numeric string → `parseInt` (truncates `"5.7"`→5); number → `Math.trunc` | only strings that are integers; null→0 |
| number | numeric string → `parseFloat`; booleans → 1/0 | finite `Number()`; null→0 |
| null | the string `"null"` (case-insensitive) → null | `""`, 0, false → null |
| string | number, boolean, bigint → string | number, boolean → string; null → `""` |

- **Ask uses JSON Schema, not TypeBox.** So one Go table must be chosen.
- **Recommendation:** use one table for all schemas, based on the Pi custom table plus the union rule (an arm that already validates is kept). Do **not** copy the truncation of `"5.7"` to 5, because it loses data silently. Record this as a decision.
- **Also:** cap the echo of the raw arguments in the validation error. Pi does not cap it.

### G8. Small CLI and mode decisions that the roadmap leaves open

| # | Pi behavior | Recommendation for H2 |
|---|---|---|
| a | A prompt that starts with `/` can be consumed by extension commands, skills or templates | Pass it through as literal text (there are no commands in H2). Record this. |
| b | No SIGINT handler in print or JSON mode | Add a clean abort on SIGINT (cancel the ctx, then exit 130). This is safer in Go, and the goleak test covers it. |
| c | `--thinking` with an invalid value gives only a warning | Copy Pi (warning). |
| d | `--provider`, `--model` and `--api-key` with no value silently become unknown flags. Unknown `--flags` are tolerated for extensions. | Use an error and exit 1 in H2. There are no extension flags until X1. |
| e | If stdin is not a TTY and never closes, startup blocks | Copy Pi (read until EOF). Document it. |
| f | Stdin, `@file` text and `messages[0]` are joined with no separator. The other messages run as sequential prompts. | Copy Pi exactly. A test pins it. |
| g | `@file`: an image goes in as an attachment. A binary file that is not an image is injected as lossy UTF-8. | H2: text files only. Images wait for H3 (H-PROV-27). |
| h | `--api-key` with no model gives exit 1 | Copy Pi. |
| i | Multiple messages: a thrown error stops the rest. An assistant error does not. | Copy Pi. |

### G9. Smaller items that a Go port could miss (not in the roadmap tests)

1. Partial messages and the context: the partial is replaced, not appended. Tool results enter the context only after the batch. A `transformContext` result is never stored.
2. Tool lookup is by exact, case-sensitive name, and the first match wins. Duplicate call ids are not checked. Recommendation: detect duplicates, and give the second call an error result.
3. `toolName` in the result message is the model's name, not the tool's name.
4. Messages injected in the middle of a batch (steering or a custom message) must land after all results (`coding-agent/CHANGELOG.md:391`).
5. In parallel mode, the result messages get timestamps after the whole batch. Tests must not assume start-time timestamps.
6. A schema-less tool must be rejected when it is registered (E§4, 0.86.0). An empty `tools` array must not be sent (E§4, 0.70.3): this applies to the H3 request builder, but the registry rule is H2.
7. Listener backpressure: a slow JSON reader stalls the loop. This is intended. The goleak test must cover a reader that closes the pipe.
8. Pi's `onUpdate` rejection bug (`AL:839, 843`). In Go, `onUpdate` returns nothing, and update delivery must never change the tool's result.
9. `finishTurn` `continue` without new input sends a context-only request whose last message is the assistant message. Some providers reject that. This is a test for H3.

## 5. Dependency matrix (Pi component to Ask)

| Pi component | Ask target | Status |
|---|---|---|
| `AssistantMessageEventStream`, `result()` | `providers.Stream`, `Result(ctx)` | EXISTS |
| Message model, events, envelope | `pkg/protocol` | EXISTS (missing: `agent_settled`, per G1) |
| `convertToLlm` default, `normalizeContext` | `providers.ConvertToLLM`, `NormalizeRequest` | EXISTS |
| Faux provider | `providers/faux` | EXISTS |
| `parseStreamingJson` | `providers/partialjson` | EXISTS |
| `runLoop`, `streamAssistantResponse`, tool batch | `internal/agent/loop_*.go` | NEW |
| `AgentLoopConfig` hooks | `internal/pipeline` hook points | NEW. **CONFLICT to settle:** the `pipeline` README describes "steps + TurnState + PipelineDeps", while Pi uses single-function hooks. Recommendation: a typed function field for each hook point in H2; ordered steps only when H11 adds many handlers. |
| `Agent` wrapper | `internal/agent` (`types.go`, Go API) | NEW |
| `AgentTool`, registry, `SourceInfo` | `internal/tools` | NEW |
| `validateToolArguments` | `internal/tools` (or a small `jsonschema` adapter) | NEW. The validator library is already in go.mod. |
| print mode, JSON mode, output guard, args | `cmd/tui` (headless path) | NEW. The bubbletea scaffold stays for T1. |
| Session header, `agent_settled` with retry | H8, H9 | LATER |

## 6. Decision matrix

| Decision | Pi's way | Ask's way (proposed) | Recommendation |
|---|---|---|---|
| Abort in the middle of a batch | Orphans, plus one extra aborted request | Same as Pi; replay repairs orphans in H3 | **Follow Pi (user, 2026-10-01)** |
| Hook error or panic | The loop does not catch; the wrapper catches | Same as Pi: tools and tool hooks become error results; loop hooks and stream errors go to the wrapper | **Follow Pi (user, 2026-10-01)** |
| Stream without a terminal event | `result()` hangs | `Result(ctx)` returns `ErrStreamIncomplete`, which becomes an error message | Already done in H1 |
| JSON terminal record | `agent_settled` from the session | Minimal `agent_settled` in H2 | Pull forward (G1) |
| Coercion | TypeBox plus a custom table | One Go table, no truncation | Adapt (G7) |
| Hook form | Fields on a config object | Typed function fields; ordered steps later | Adapt (section 5) |
| Listeners | Ordered and awaited | Ordered, synchronous calls in the loop goroutine | Keep |
| Parallel tools | `Promise.all`, no limit | errgroup, no limit in H2 | Keep. Add a limit later if needed. |
| SIGINT in print mode | No handler | Cancel the ctx, exit 130 | Deviate (G8b) |
| Unknown flags | Tolerated | Error until X1 | Deviate (G8d) |
| Tool loadout | Delta system messages | Initial system message only; the delta comes in H8 | Keep the roadmap order (G3) |

## 7. Edge-case test list for H2 (Pi tests plus the gaps above)

| Area | Test |
|---|---|
| Loop | Two turns with tools, then stop. Check the full event order: agent_start, turn_start, prompt message events, assistant start/update/end, tool events, result messages, turn_end, ..., agent_end. |
| Loop | `finishTurn` returns `continue` with no tools: exactly one extra request. `end`: no steering poll and no follow-up poll. |
| Loop | An error or aborted assistant message: `finishTurn` is called, then `turn_end(msg, [])` and `agent_end`, and its tool calls are not run. |
| Loop | `Continue` with an assistant tail and empty queues gives an error. With an empty context, it gives an error. |
| Stream | The stream closes with no terminal event: the result is an error message, not a hang. |
| Abort | Abort during streaming: an aborted message. Abort during preflight: no sibling starts. Abort in the middle of the parallel batch: prepared calls get "Operation aborted", later calls get no events, and one more stream call returns aborted (G2, as in Pi). The full pairing test moves to H3. |
| Abort | goleak: no goroutines leak after abort, after a normal end, or after the JSON reader closes. |
| Parallel | The second tool finishes first: `tool_execution_end` events follow completion order, and the result messages follow source order. |
| Sequential | One tool marked `sequential` makes the whole batch sequential. |
| Pipeline | Unknown tool. Validation error (check the text format). A `beforeToolCall` block, with and without a reason. A panic in a tool, in `before` and in `after`. An update after settle is ignored. A hook mutates the arguments. `afterToolCall` does not run for blocked or invalid calls. |
| Length | `stopReason=length` with two tool calls: two error results with the exact text, no hooks, and the loop goes on. |
| Coercion | `"5"`→5 for an integer; null in `anyOf` is kept; a missing optional null is removed; a missing input becomes `{}`; an arm that is already valid is kept. |
| Result | Empty content is stored as `[]`. `structuredContent` and `terminate` appear in events only. |
| Print | Exit 0 with the text blocks of the last assistant message. Exit 1 with a stderr message on error or abort. A thrown error gives exit 1. SIGTERM gives 143. SIGINT gives a clean abort. |
| JSON | LF framing. A line longer than 64 KB. U+2028 inside a string. A final `agent_settled` for each prompt. Exit 0 on an assistant error. A stray stdout write goes to stderr. EPIPE gives exit 1. |
| CLI | Piped stdin plus a message are joined with no separator. `-p` consumes the next token. `--` allows a message that starts with `-`. `--mode x` is an error. `--model` with no value is an error (G8d). |
| Isolation | Print mode opens no network listener. |

## 8. Risk score

**Medium (6/10).**

- The loop is small, but its ordering rules are subtle.
- Four deviations from Pi need explicit decisions: G1, G2, G4 and G7.
- The H1 base is solid and already fixes Pi's stream hang.
- The main risks:
  - event ordering under parallel tools, covered by `-race` and order tests;
  - goroutine leaks on abort, covered by goleak;
  - the `pipeline` README shape conflicts with Pi's single-function hooks.

## 9. GitNexus check (as asked)

- **`query` on the tool-batch flow** returned `executeToolCalls`, `runToolCall`, the session hook installers and a second tool executor in the experimental v4 harness (`agent/src/harness/execution/tools.ts`, `harness/runtime/drive/tools.ts`). The roadmap skips the harness, so nothing new for H2.
- **`context` on `runToolCall`:** the exported loop function is called only by `agent-loop.test.ts`. The session has its own nested `runToolCall` (AS:698). This confirms that nested tool calls can be deferred.
- **`context` on `_installAgentToolHooks`:** the session wires `beforeToolCall` and `afterToolCall` once, in its constructor. Following that wiring led to the `prepareRequest` projection (section 2.1), the mapping for the context-source interface.
- **Coverage:** the GitNexus process index for this area is thin (only codemode flows ranked), so direct file reads did most of the work.

## User decisions (2026-10-01)

- **G1: accepted.** H2 emits a minimal `agent_settled` right after `agent_end`. It is marked "(H9 row, pulled: minimal)", and H9 moves the emit point when retry and compaction exist.
- **G4: follow Pi. The recommendation is rejected.** Pi's contract is ported as it is:
  - A throw or panic inside a tool, `prepareArguments`, validation, `beforeToolCall` or `afterToolCall` becomes an error tool result (`AL:769-775, 841-847, 892-895`). In Go, this means `recover` in each tool goroutine, which is what Pi does.
  - An error from `transformContext`, `convertToLlm`, `prepareRequest`, `finishTurn` or the stream function is not caught in the loop. The loop returns the error. The `Agent` wrapper then builds the error assistant message plus `message_start`, `message_end`, `turn_end` and `agent_end` (`A:523-548`).
- **G7: accepted.** One Go coercion table for all schemas, with no truncation of floats to integers.

- **G2: follow Pi (option B). The recommendation is rejected.** After an abort in the middle of a batch, the loop behaves exactly as Pi does:
  - Calls that are already prepared get "Operation aborted".
  - Calls after the break point get no events and no result.
  - The loop then makes one more stream call with the aborted signal.

  The orphan calls are repaired at replay by `transformMessages` in H3 ("No result provided"). Consequences for the plan:
  - The H2 pairing test covers only the loop part: the length guard, error or aborted messages, and calls that are already prepared.
  - The full pairing test after an abort moves to H3.
  - The H-LOOP-08 inventory text must be corrected to match Pi.

- **G3, G5, G6, G8, G9 and the `pipeline` hook form: the recommendations are accepted (user, 2026-10-01, "theo bạn").** They are applied to the roadmap as D18 to D21 and to the H2, H3 and H9 text (revision 4).
- **Correction:** the earlier note about Go 1.26.0 in D14 was wrong. D14 proposes a bump to Go 1.26.0 for bubbletea v2. That bump is still open until the T0 gate, so it does not conflict with `go 1.25.10` in `go.mod`.

## Unresolved questions

1. Should `internal/logs/` be committed? It is untracked, and the build passes. This is outside the roadmap.
