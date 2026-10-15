# H1 Pi source audit

Date: 2026-10-01.
Target: [H1 plan](../261001-0836-h1-messages-events-faux/plan.md).
Source: `/Users/dale/Desktop/workspace/opensources/pi`, commit `2bbfcca437c3aa5a21af1e4ee44ae7a051f953ad`, package version 0.99.1.
This is a stateful review record, not product documentation.

The first plan missed requirements for initial content, signature metadata, usage snapshots, failed-stream settlement and the builder state machine.
The plan now includes those requirements and their acceptance checks.
The audit also corrects the BOM claim and records a reproduced split-CRLF defect in Pi.
No product code was changed.

## Method and evidence

Spiral thinking here means returning to the same contract after each wider source ring.
The check asks whether a complete message can still be built after initial-only data, final-only data, a delayed consumer, cancellation, or early EOF.
The competing explanations were that the plan covered Pi through its final message, or that it lost data before that final message existed.
Frame tests and adapter code support the second explanation for several paths.

| Ring | Source read | Check and result |
|---|---|---|
| 0: contract | AI `types.ts`, agent `types.ts`, all three prior H1 scout reports, H1 plan | Check each role, block, optional field, event discriminator and request boundary; identify missing supporting records and conflicting signatures |
| 1: producers and reducers | Complete `providers/faux.ts`, `utils/event-stream.ts`, `utils/json-parse.ts`, `utils/assistant-message-frame.ts`, `utils/text.ts`, `utils/transcript.ts` | Trace creation, initial data, append, authoritative replacement and terminal settlement; find metadata and open-tool gaps |
| 2: callers | Agent `streamAssistantResponse`, complete proxy implementation, harness response observer and recovery functions, frame consumer searches | Verify `start` becomes `message_start`, only block events become updates, and EOF uses result; frame storage consumers are outside H1 scope |
| 3: transport and adapters | Complete `modes/json-event.ts`; Anthropic SSE reader and content/usage handlers; Completions tool-index mapping; Responses argument and output-item handlers; `transform-messages.ts` | Verify initial content, signatures at end, usage without a content delta, id/name changes and final-only arguments |
| 4: tests and failures | Complete faux and event-stream test files; frame tests; Anthropic initial-content/repair tests; proxy tests; JSON regressions 7290, 7911 and 7925; direct source checks | Add replay, ownership, state, usage and linear-growth checks; reproduce stream result and SSE edge cases |
| 5: history and local contracts | Git history for faux, frames and event-stream; Ask architecture, package READMEs, lint rules, roadmap and confirmed decisions | Verify the fixes that motivated frame semantics; keep micro-USD, delta-only events, ACP, H3 transform ownership and no M1 crash recovery |

After each ring, compare the new requirements with the message/stream/codec interfaces and the exit fixture.
The final pass checks that the added test cases have an implementation owner and that the corrected interfaces carry their data.
This audit does not claim that the whole Pi repository is fully reviewed.

### GitNexus

The initial index had the correct commit but a stale analyzer identity/schema.
The first query reported missing `Protocol`, `Category` and `isDetail` schema entries and returned partial results.
Run `npx --no-install gitnexus analyze --index-only --workers 4` in Pi to rebuild the index without changing tracked source or agent-context files.
The rebuild completed successfully.
Queries after rebuild no longer reported those schema errors.

Use these exact symbol lookups to repeat the graph checks:

```sh
npx --no-install gitnexus context -r pi streamWithDeltas -f packages/ai/src/providers/faux.ts --limit 10
npx --no-install gitnexus context -r pi streamAssistantResponse -f packages/agent/src/agent-loop.ts --limit 10
npx --no-install gitnexus context -r pi reduceAssistantMessageFrames --limit 8
npx --no-install gitnexus context -r pi toJsonEvent --limit 8
npx --no-install gitnexus context -r pi serializeContext -f packages/ai/src/providers/faux.ts --limit 5
npx --no-install gitnexus context -r pi iterateSseMessages --limit 5
npx --no-install gitnexus query -r pi 'assistant message frame recovery' --limit 3
```

The process query found `proc_88_prepared`.
Its graph steps are `prepared → stream → encode → encodeTextDelta → block → assertContentIndex`.
Inspect the `STEP_IN_PROCESS` edges with Cypher; then read their owning files.
The reducer context also links `recoverAssistantGeneration`, `recoverCancelledAssistantEffect` and the live lane snapshot reader to frame replay.
The JSON serializer context links the three regression tests to wire projection.

The rebuilt graph still reports capped dispatch/callable candidate sets and omitted process branches.
An absent graph edge does not prove that no caller exists.
Source searches and direct file reads supplement the graph, especially for callbacks and adapter state.

## Findings and plan changes

AI paths below are under `packages/ai/src/` unless a test path is given.
A paths are under `packages/agent/src/`.

| Finding | Evidence | Corrected requirement |
|---|---|---|
| The provider `start` has no message seed in the sketch | `types.ts:751-783`; frame `cloneStartMessage`; agent loop `381-469` | Carry one immutable initial message; block updates remain delta-only |
| Initial block content can be non-empty | Anthropic `content_block_start` handling `632-680`; `packages/ai/test/anthropic-sse-parsing.test.ts:445-541` | Accept initial text/thinking/tool data and redacted thinking with no deltas |
| Signatures and redaction are missing from progress events | Frame encoder/reducer; frame tests `41-106`, `382-449`; Responses `response.output_item.done` | End events carry metadata and remove stale optional values when absent; keep explicit empty/false values |
| Supplied final arguments must override streamed bytes | Responses `638-738`; frame tests `108-228`; agent proxy test `33-75` | Separate authoritative-object and raw-buffer tool-end paths; never re-parse over a supplied final object |
| A tool id or name can change at finalization | Frame test `108-145`; Completions `ensureToolCallBlock`; proxy processing | Track by content index, finalize full metadata, and avoid generated-id collisions |
| Usage is required during progress, but raw events do not carry it | Anthropic `message_start`/`message_delta` handlers; JSON serializer; regression 7911 | Capture latest usage in each immutable provider `StreamItem`; use it for agent `MessageUpdate` |
| Mutable queued values can change after send | Frame `clone*` helpers and test `514-543`; Pi `partial` is a shared live reference | Copy ownership at seed/end/result/script/request boundaries; use buffers without whole-message copying per delta |
| Open tool JSON is lost when no `toolcall_end` arrives | Frame reducer `482-487`; recovery functions; length guard in agent loop | Salvage once on failed/length settlement; retain scratch bytes outside the final message; never execute partial failed calls |
| The unconditional result/terminal promises conflict with bounded channels | `EventStream.end`, agent EOF fallback, event-stream tests; Go channel backpressure | `Result(ctx)` waits independently and is cancellable; producer close settles without a reader; abandoned full-channel cancellation can omit delivery of its stored terminal |
| `errors.Is` cannot work with the original result signature | Original H1 `Result() AssistantMessage`; sentinel acceptance criterion | Return `(AssistantMessage, error)` and keep error identity in process; JSON carries the error text only |
| The exit fixture puts forbidden events into updates | Agent loop switch and `toJsonEvent`; original H1 step 8 | Map start to message_start, only nine block types to updates, and terminal result to message_end; include an exact sequence in the plan |
| SSE context checks cannot unblock an arbitrary Reader | Pi reader/fetch cancellation and Go Reader contract | Input owner cancels/closes the transport; test an owned pipe or context-bound HTTP body; no detached read goroutine |
| Pi BOM removal was incorrectly called absent | Anthropic `new TextDecoder()`; executed source probe | BOM removal is parity; remove it from the departure list |
| Split CRLF can split one Pi event into two | Anthropic `consumeLine`/`iterateSseMessages`; executed source probe | Keep pending-CR state across reads and test every split boundary |
| A named SSE event can have no data | Anthropic `flushSseEvent`; executed `event:error` fixture | Preserve named empty-data events; discard empty/comment-only frames and reset raw state |
| A line limit alone does not bound a multi-line event or comment state | Anthropic `state.data`/`state.raw`; `flushSseEvent` early return | Bound aggregate event/raw bytes; clear comment-only frames; keep read/limit errors distinct from EOF |
| Supporting event/model records and optional states are incomplete | AI diagnostics and nested-call records; agent `AgentToolResult`; frame absence/false tests | Define diagnostic code variants, nested records and execution results; use ordered sections and presence-aware fields |
| Provider request tools conflict with transcript tools | Complete transcript helpers; faux `serializeContext`; agent normalization call | Fold shorthand into an initial system message before the provider; full state replay stays in H6 |
| Faux estimate differs from Pi on Unicode | `estimateTokens` uses JS string length; executed emoji fixture | Record rune counting as a departure; test emoji and exact usage overrides |
| Faux cache and factory state need an exact contract | `withUsageEstimate`, `createFauxCore`, faux tests; executed cache fixture | Define prefix serialization/formula, no cache access/update with `none`, per-call state capture, and instance isolation |
| `Truncate(n)` has no reply to truncate as a standalone step | Original proposed API and exit test | Make truncation a modifier of a real scripted reply; Raw is an explicit validated seam |
| A tolerant prefix is not a validated final object | JSON parser source and prior vectors; duplicate keys V33 | Keep no-panic/finite-output/full-document tests; avoid unproved monotonic partial-value claims; validate complete protocol JSON strictly |
| Pi USD and Ask micro-USD have the same field names but different units | AI `Usage.cost`, accepted Ask cost decision | Require explicit Pi import conversion and Ask format version in H8; do not claim direct cost compatibility |

### Scope checks

- Preserve the user's option C: H3 owns `transformMessages`; H4 owns vendor-id rules and cross-API tests.
- Preserve the delta-only decision; one-time seeds and block metadata do not restore cumulative messages in updates.
- Preserve integer micro-USD; add the missing import/version boundary rather than change its unit.
- Use frame semantics as builder evidence; do not add durable frames, checkpoint persistence or crash resume to H1.
- H1 JSONL is a codec/headless surface; the external agent protocol remains ACP plus `_ask/*`.
- Keep full system-section/tool replay in H6; H1 adds only the small raw-request fold needed before faux/H2/H3 calls.

## Runnable source checks

Run the [source check](pi-h1-source-check-261001-0902.mjs) with Node 22.19 or later:

```sh
node --experimental-strip-types plans/reports/pi-h1-source-check-261001-0902.mjs
```

The check imports Pi event-stream, transcript, text and JSON projection modules directly.
It loads the actual faux core and SSE helper bodies with TypeScript types removed; external registry/SDK imports are excluded.
It does not replace their function logic or change the Pi checkout.

The run passed assertions for unresolved `end()` without result, initial transcript tools and section order, the exact two-tool event sequence, JSON tool-start metadata/usage, UTF-16 usage counting, cache behavior, pre-start abort, BOM removal and named empty-data SSE dispatch.
It also reproduced split-CRLF output: unsplit input gives data `a\nb`; the same input split after CR gives two events `a` and `b`.
That final assertion detects the pinned Pi defect; it is not an acceptance test for the Go fix.

Git history checks include `a829de0a7` (frame reducer), `5c6655e76` (burst-safe frames), `0fdec07ba` (provider thinking level), and `b2602be77` (FIFO performance).
These support the metadata, ownership and linear-work requirements.

## Validation and limits

The source check passed after the final edit.
Local plan/report links and the quoted PI source paths were checked.
The plan has explicit requirements for each finding, an exact exit sequence and phase boundaries.
No H1 Go implementation or acceptance tests were run; those packages remain scaffolded.

The full Pi Vitest suite was not run.
V1 to V33 remain experiments from the earlier scout, not newly executed parser results in this audit.
The new plan requires executable vectors before parser acceptance, including explicit Go values and overflow checks.
Other adapters were inspected only at the boundaries relevant to the H1 contracts, not line by line in full.
Graph omissions and callback resolution limits remain; source absence was not used as proof of completeness.

## Unresolved questions

None for H1 planning.
H3 still chooses the SDK versus owned HTTP/SSE path, and H9 still chooses cost-rate precision and rounding, as the accepted plan already states.
