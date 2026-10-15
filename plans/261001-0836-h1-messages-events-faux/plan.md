---
title: "H1: messages, agent-core events and a fake model (Pi port, detailed feature list)"
status: ready
priority: P1
created: 2026-10-01
mode: xia --port
parent: plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md (section 4, H1)
---

# H1: messages, agent-core events and a fake model

This plan defines the H1 message and event contracts for implementers and reviewers.
The source audit on 2026-10-01 adds requirements that were missing from the first scout.
It keeps the user decision in section 6: `transformMessages` belongs to H3.
It does not add an agent loop, a real adapter, or crash recovery to H1.
The [spiral audit](../reports/review-261001-0902-h1-pi-spiral-audit.md) records the evidence and limits.

## 0. Source manifest

| Item | Value |
|---|---|
| Source | Pi at `/Users/dale/Desktop/workspace/opensources/pi`, commit `2bbfcca43`, version 0.99.1, MIT; GitNexus repo name `pi`. |
| First scout scope | `packages/ai/src/types.ts`, `utils/event-stream.ts`, `utils/json-parse.ts`, `utils/diagnostics.ts`, `utils/transcript.ts`, `providers/faux.ts`, `api/transform-messages.ts` (read for gaps only), `packages/agent/src/types.ts`, `agent-loop.ts`, `packages/coding-agent/src/core/messages.ts`, `modes/json-event.ts`, `modes/print-mode.ts`, `docs/json.md`, `docs/message-types.md`, and the three changelogs. |
| Scout reports | `plans/reports/researcher-261001-0836-h1-message-model.md`, `researcher-261001-0836-h1-event-stream.md`, `researcher-261001-0836-h1-faux-parsing.md` |
| Method | Re-check rings 0 to 5: types, builders, callers, wire output, errors and tests, then Git history; Rebuild the stale GitNexus index with `--index-only`; inspect symbol contexts and process `proc_88_prepared`; confirm graph results in source; See the linked audit for coverage and limits. |
| Audit scope added | `AI:utils/assistant-message-frame.ts`, `utils/text.ts`, `A:proxy.ts`, harness frame consumers (read only), AI frame/event-stream/faux/SSE tests, agent proxy tests, and coding-agent JSON regressions 7290, 7911, 7925; Durable frame storage remains outside H1. |
| Local target | `pkg/protocol` and `internal/providers` hold only `README.md` and `doc.go`; No Go code exists for H1 yet. |
| Path shorthand | `AI:` = `packages/ai/src/`, `A:` = `packages/agent/src/`, `C:` = `packages/coding-agent/src/`, `CD:` = `packages/coding-agent/docs/` |

## 1. Outcome and acceptance criteria

H1 teaches one concept: **a harness is a message log plus a stream of events.**
H1 builds types, the stream, the parsers and the fake model.
It builds no agent loop (H2) and no real provider (H3).

Exit (from the roadmap, made exact):

- [x] A scripted faux stream (thinking, text, two tool calls) gives the exact event order in section 3.4 and a final `AssistantMessage` that equals the normalized script, in a unit test (identity, generated ids, usage and timestamp follow section 4.7).
- [x] The provider events map to `message_start`, the nine block update types, and `message_end`, then encode as JSONL with the envelope and decode back to equal values.
  `start`, `done` and `error` are never nested `message_update` events.
- [x] A faux stream that closes without a terminal event gives a final message with `stopReason: error` and an error that matches `ErrStreamIncomplete`.
  `Result(ctx)` settles after producer close without a channel reader; its wait can be cancelled before close.
  Abandoning a blocked stream and cancelling its request leaves no producer goroutine.
- [x] Initial block content, signatures, redaction, final-only arguments, changing tool ids, mid-stream usage and partial content on failure survive the builder and assembler.
- [x] Builder state errors give errors without panic; queued events and repeated results do not share mutable slices or raw JSON.
- [x] The partial-JSON parser passes vectors V1 to V33 (section 4.4) and a prefix sweep with no panic.
- [x] The SSE reader passes a 200 KiB line, a 10 MiB line, and the other tests in section 4.6.
- [x] `go test ./pkg/protocol/... ./internal/providers/...` passes with `-race`.
  `goleak` finds no leaked goroutine.
  `golangci-lint` passes.

## 2. Feature tree (parent, sub-features, edges)

Legend: **Own** = H1 builds and tests it. **Type** = H1 defines the type only; a later phase fills or uses it. **Defer** = a later phase owns it (named). Each row has a Pi source and the inventory id when one exists.

### F1. Message model (H-SESS-04, H-SESS-05)

| # | Sub-feature | H1 | Pi source | Notes and edges |
|---|---|---|---|---|
| F1.1 | Content blocks: `text` (+`textSignature`), `thinking` (+`thinkingSignature`, `redacted`), `image` (`data` base64, `mimeType`), `toolCall` (`id`, `name`, `arguments`, `thoughtSignature`, `namespace`) | Own | `AI:types.ts:389-425` | `redacted: true` puts the encrypted payload in `thinkingSignature`; visible text can be empty; Signed empty thinking and empty text must survive (ai 0.80.6, 0.84.0). |
| F1.2 | Per-role content rules: user = text or image; assistant = text, thinking, toolCall (**no image**); toolResult = text or image | Own | `AI:types.ts:540-608` | The inventory row H-SESS-05 is wrong about `image` in assistant content (section 7). |
| F1.3 | `SystemMessage` with `content`, `sections` (`null` removes), `toolsAdded`, `toolsRemoved` | Own (type and codec) | `AI:types.ts:522-538` | Inventory H-LOOP-15 says "design the transcript this way from day 1"; Replay helpers (`AI:utils/transcript.ts:58-234`) are H6/H8; H1 only folds request shorthand into the initial message; Doc-only field `replace` is not ported (no code reads it); Content is `string` or text blocks in Pi: read both, always write blocks (same rule as F1.4). |
| F1.4 | `UserMessage`: content string or blocks | Own | `AI:types.ts:540-544`; `A:agent.ts:421-429` | Read both forms; always write blocks; An image-only user message has no empty text part (ai 0.87.1). |
| F1.5 | `AssistantMessage`: `api`, `provider`, `model`, `responseModel?`, `responseId?`, `thinkingLevel?`, `providerThinkingLevel?`, `diagnostics?`, `usage`, `stopReason`, `errorMessage?`, `rawStopReason?`, `endTurn?`, `timestamp` | Own | `AI:types.ts:546-570` | `endTurn` is debug only; `rawStopReason` is filled by H3/H4; `deferred` handle: skip (H-PROV-08 is skip). |
| F1.6 | `StopReason`: `pending stop length toolUse error aborted deferred` | Own | `AI:types.ts:450` | Keep all 7 constants for file compatibility; `pending` is in memory only; the session writer (H8) rejects it. |
| F1.7 | `ToolResultMessage`: `toolCallId`, `toolName`, `content`, `details?`, `usage?`, `nestedCalls?`, `isError`, `timestamp` | Own | `AI:types.ts:573-608` | `nestedCalls` is kept in the session, never sent to the model; Define `NestedToolCalls{calls, complete}` and each record: `id`, `name`, optional `arguments`, `argumentsBytes`, `durationMs`, `error`, and `status: ok/error/unfinished`; Preserve omitted arguments separately from `{}`. |
| F1.8 | `Diagnostic` record `{type, timestamp, error{name,message,stack,code}, details}` | Type | `AI:utils/diagnostics.ts:3-14` | `error` and `details` are optional; `error.code` is a string or number; Preserve both forms; Producers come in H3/H4. |
| F1.9 | `ThinkingLevel` enum `off minimal low medium high xhigh max` | Type | `AI:types.ts:85-86` | Per-model mapping and clamp are H3 (H-PROV-12). |
| F1.10 | Open role set: custom roles kept as raw JSON, registry for decoders | Own | `A:types.ts:365-374` (declaration merging); `C:core/messages.ts:29-77` | Go has no declaration merging; Unknown role decodes to `RawMessage` and stays in the log; Coding-agent roles (`bashExecution`, `custom`, `branchSummary`, `compactionSummary`) register in H5, H11, H14, H10. |
| F1.11 | Timestamps: message = unix ms `int64`; session entry = ISO-8601 (H8) | Own | `C:core/messages.ts:100-138` | Constructors receive a clock; pure transforms receive a timestamp; Preserve an explicit zero timestamp. |
| F1.12 | Default `convertToLlm`: keep `system user assistant toolResult`, drop all other roles | Own | `A:agent.ts:38-46` | Stage 1 of 2; Stage 2 is `transformMessages` (F1.14); Converters for custom roles come with those roles. |
| F1.13 | Tool declaration `{name, description, parameters}` and `ToolReference {name}` | Own | `AI:types.ts:715-724` | `parameters` is JSON Schema as raw JSON (no TypeBox); `constrainedSampling` is deferred (not in roadmap). |
| F1.14 | Provider-side `transformMessages` (drop errored and aborted assistants, foreign thinking to text, signatures dropped across models, synthetic `No result provided` tool results, image placeholders, tool-call id rewrite) | Defer H3 (function), H4 (per-vendor id rules); decided in section 6 | `AI:api/transform-messages.ts:64-235` | The H3 Anthropic adapter is the first caller (`AI:api/anthropic-messages.ts:1057`, verified). |
| F1.15 | Context vs `TranscriptContext` (system prompt and tools travel as the leading system message) | Own (request shape) | `AI:types.ts:732-749`; `AI:utils/transcript.ts:10-34` | Distinguish raw `Request{SystemPrompt, Messages, Tools}` from provider-facing `TranscriptRequest{Messages}`; H1 supplies the small initial-message normalization needed by faux and H2/H3; H6 owns full system-section and tool-state replay, not this initial fold; An empty prompt and no tools add no message; the synthetic initial message has timestamp 0. |

Edges of F1 with other phases:
- H3/H4 fill `responseModel`, `rawStopReason`, `diagnostics`, signatures, and sanitize outbound text (`strings.ToValidUTF8`, from `AI:utils/sanitize-unicode.ts`).
- H8 persists messages only on `message_end`, so `pending` is never written (`C:core/agent-session.ts:1096-1125`). Messages have no id; session entries do (`C:core/session-manager.ts:277-284`).
- H10 and H14 add the summary roles. Their prefix and suffix text is part of the cached prompt bytes; port byte for byte (`C:core/messages.ts:11-24`).

### F2. Usage record (H-RETRY-07, type part)

| # | Sub-feature | H1 | Pi source | Notes and edges |
|---|---|---|---|---|
| F2.1 | `Usage{input, output, cacheRead, cacheWrite, cacheWrite1h?, reasoning?, totalTokens, cost}` | Own | `AI:types.ts:427-448` | `cacheWrite1h` is a subset of `cacheWrite`; `reasoning` is a subset of `output`. "Absent" differs from 0, so both are `*int64`. |
| F2.2 | `Cost{input, output, cacheRead, cacheWrite, total}` in micro-USD `int64` | Own (type) | same | Departure from Pi floats (decided earlier); The price formula, tiers and rate unit are H9. |
| F2.3 | Zero usage is an object, never `null` | Own | `A:agent.ts:48-55` | |
| F2.4 | `totalTokens` is computed by the adapter, not read from the API | Defer H3/H4 | `AI:api/anthropic-messages.ts:628-629` | Proxies can omit usage in `message_delta`; overwrite only present fields (ai 0.80.7). |

### F3. Provider stream contract (H-LOOP-16, H-PROV-11)

| # | Sub-feature | H1 | Pi source | Notes and edges |
|---|---|---|---|---|
| F3.1 | Provider-level events, delta only: `start`, `text_start/delta/end`, `thinking_start/delta/end`, `toolcall_start/delta/end`, `done`, `error` | Own | `AI:types.ts:751-783` | No shared live `partial`; A one-time `start.message` seed is needed to build identity and metadata; `toolcall_start` carries `id` and `toolName`; Initial block content and optional end metadata are required by section 3.3; these are explicit Ask extensions to Pi JSON mode; `*_end` replaces content and metadata, including removal of absent optional fields. |
| F3.2 | Stream object: one settled result; at most one terminal event; normal drained streams have exactly one; `Result(ctx)` | Own | `AI:utils/event-stream.ts:26-89` | **Pi hazard (verified):** `end()` with no result leaves `result()` unresolved, and the loop awaits it (`A:agent-loop.ts:460`); Ask: the producer close path settles the result independently of channel consumption; A raw early close gives `ErrStreamIncomplete`; cancellation gives an aborted result; See the bounded-channel exception in 4.5. |
| F3.3 | Stream function never fails with a Go error at call time; failure is a terminal `error` event with `stopReason` `error` or `aborted` | Own | `A:types.ts:26-36` | Pi's AI-level `StreamFunction` may throw for missing auth (`AI:types.ts:365-370`); Ask: the stream call always returns a stream; Invalid request-model selection is a terminal setup error; Constructor option validation can return a Go error; it is outside the stream contract. |
| F3.4 | Cancellation through `context.Context`; after cancel, one settled aborted result; try to deliver a terminal `error` without blocking on the cancelled request (reason `aborted`, `errorMessage: "Request was aborted"`) with the partial content and latest usage so far; no `*_end` for the cut block; cancel-aware pacing and result settlement | Own | `AI:providers/faux.ts:348-412` | Replaces `AI:utils/abort.ts` and `abort-signals.ts` (not ported). |
| F3.5 | Stream completeness: EOF without the protocol's terminal event is an error, and retryable | Own (rule, sentinel `ErrStreamIncomplete`); Defer per-protocol checks to H3/H4 | table in faux report section 4.1 | Keep the words "ended without" in the message so H9's text table matches (`AI:utils/retry.ts:77-81`); Unknown terminal reasons become errors, not `stop` (ai 0.83.0). |
| F3.6 | Assembler: builds the message from deltas, keeps buffers by content index, accepts initial content and authoritative ends, parses raw tool JSON once on block end or failed settlement, never leaks scratch fields | Own | Pi has this logic inside each adapter (`AI:api/anthropic-messages.ts:709, 745-748`) | One assembler for faux, H3 and H4, using the protocol builder; Adapters map vendor indexes to contiguous content indexes; Tool ends may supply final arguments, name, id, namespace and signature with no deltas; Generate a missing id only at finalization, avoid collisions, and let authoritative metadata replace the start values; Do not parse over a supplied final object. |
| F3.7 | `length` stop reason carried on every path | Own | `A:agent-loop.ts:265-272` | H2 uses it to fail all tool calls of a truncated message (agent 0.80.4). |

### F4. Agent-core events and the envelope (H-MODE-04 core part, H-MODE-05)

| # | Sub-feature | H1 | Pi source | Notes and edges |
|---|---|---|---|---|
| F4.1 | 10 agent-core events: `agent_start`, `agent_end{messages}`, `turn_start`, `turn_end{message, toolResults}`, `message_start/end{message}`, `message_update{assistantMessageEvent, usage}`, `tool_execution_start{toolCallId, toolName, args}`, `tool_execution_update{..., partialResult}`, `tool_execution_end{..., result, isError}` | Type | `A:types.ts:514-529` | H2 emits them; Core `agent_end` has **no** `willRetry` (session level, H9). |
| F4.2 | `message_update` is delta only in process and on the wire: `assistantMessageEvent` + top-level `usage` | Own | `C:modes/json-event.ts:25-61`; `CD:json.md` | Pi keeps a cumulative message in process and strips it on the wire; Ask uses one shape everywhere; `start`, `done` and `error` are not in the `message_update` union; The union contains only the nine block event types; Each provider `StreamItem` captures the latest usage so H2 does not read mutable provider state. |
| F4.3 | Envelope: `seq`, `ts`, `sessionId`, `runId`, `type`, flat JSON | Own (type) | Pi has none | Departure; The emitter that fills `seq` is H2; `message_end` carries `usage`, `model`, `provider` inside the message, not as extra fields. |
| F4.4 | Message builder for clients: applies events to rebuild the partial message (seed once, append deltas, replace content and metadata on `*_end`, replace on `message_end`) | Own | `CD:json.md` "Reconstruct streaming messages" | Lives in `pkg/protocol` so the assembler and clients share it; Raw tool JSON stays outside message fields; Buffer append is amortized linear; do not copy the full message or concatenate immutable strings on each delta; The client can retain raw preview bytes; tolerant parsing belongs to providers, not this stdlib-only package. |
| F4.5 | JSONL framing: one object per line, split on LF only, strip a CR | Own (codec helper) | coding-agent 0.57.0 | Writer and stdout guard are H2. |
| F4.6 | Session-level events (`agent_settled`, `queue_update`, `entry_appended`, `compaction_*`, `auto_retry_*`, `summarization_retry_*`, `session_info_changed`, `thinking_level_changed`, `bash_execution_update`) | Defer | `C:core/agent-session.ts:180-231` | Each comes in the phase that emits it: H8, H9, H10, H13/H15, H6/H7; `parentToolCallId` is X1. |

Ordering rules that H1 types must allow (H2 enforces them): see event report section 3.
Parallel tools give `tool_execution_end` in completion order and result messages in source order (agent 0.68.1).

### F5. Tolerant partial JSON (H-PROV-11)

| # | Sub-feature | H1 | Pi source | Notes and edges |
|---|---|---|---|---|
| F5.1 | `Parse(s) map[string]any`, never panics, never fails | Own | `AI:utils/json-parse.ts:104-124` + npm `partial-json@0.1.7` | Pi has no unit test for it; Vectors come from the scout's experiment (section 4.4). |
| F5.2 | `Repair`: raw control chars in strings become escapes; bad escape `\H` becomes `\\H`; dangling backslash | Own | `AI:utils/json-parse.ts:32-83` | Pi test vector V21 (`packages/ai/test/anthropic-sse-parsing.test.ts:374-443`). |
| F5.3 | Fall-through: an empty object from the tolerant pass also triggers the repaired retry | Own | Pi defect | V23, V24: raw tab or newline in an open string gives `{}` in Pi; Use section 4.4 of this plan; the faux report section 3.4 describes the original algorithm; its section 0 point 4 ("repair first") is outdated, because repair-first would change V6, V7 and V25. |
| F5.4 | Non-object result (scalar, array) becomes `{}`; no `NaN` or `Infinity` | Own | Pi returns scalars | V30, V31. |
| F5.5 | Parse once at raw tool end or failed settlement, not per delta | Own | Pi parses per delta (quadratic) | Follows from delta-only events; An authoritative final object bypasses parsing; Failed settlement salvages unfinished tool buffers once for the final record; it emits no false block-end event; A UI preview may call `Parse` on its own copy (T1). |
| F5.6 | Tool-argument validation against the schema | Defer H2 | `AI:utils/validation.ts` | H-LOOP-10, H-TOOL-12. |

### F6. SSE reader

| # | Sub-feature | H1 | Pi source | Notes and edges |
|---|---|---|---|---|
| F6.1 | `bufio.Reader` based reader, no `bufio.Scanner`, configurable max line/event (default 32 MiB), `ErrLineTooLong`, `ErrEventTooLong` | Own | Pi own reader `AI:api/anthropic-messages.ts:323-472` (no limit) | The prior scout found a 32 MiB Scanner cap in Go SDK main-branch sources; no SDK version is pinned here; The Go default Scanner cap is 64 KiB. |
| F6.2 | Line ends `\n`, `\r\n`, bare `\r`; field split at first `:`, one leading space removed; comments; `data` lines joined with `\n`; dispatch on blank line; named events can have empty data | Own | same | |
| F6.3 | Flush a pending event at EOF without a blank line | Own | ai 0.85.0 | |
| F6.4 | Strip a leading UTF-8 BOM | Own | `AI:api/anthropic-messages.ts:420` (`TextDecoder`) | Pi already strips it through its decoder; The direct source probe confirms this; It is not a departure. |
| F6.5 | `ctx` checks before and after each read and dispatch; the input owner unblocks Read on cancel | Own | ai 0.39.0 | |
| F6.6 | Linear time | Own | ai 0.86.0 | Pi's Anthropic reader re-scans the buffer per chunk. |
| F6.7 | Stop reading after the terminal event; idle timeout; header wait | Defer H3 | ai 0.78.0, 0.76.0 | The reader exposes what H3 needs. |
| F6.8 | `[DONE]`, unknown event names, `event: error` | Defer H3/H4 (adapter) | `AI:api/anthropic-messages.ts:486-490` | H1 gives a fixture helper. |

### F7. Faux provider (H-PROV-09)

| # | Sub-feature | H1 | Pi source | Notes and edges |
|---|---|---|---|---|
| F7.1 | `faux.New(opts)`: one api id, provider `faux`, model `faux-1` (text+image, context 128000, max tokens 16384, cost 0) | Own | `AI:providers/faux.ts:24-50, 437-488` | Inventory says "since 0.70.0"; changelog says 0.64.0. |
| F7.2 | Script queue: `Set`, `Append`, `Pending`, `Calls`; step taken at call time; call count increments first | Own | `faux.ts:445, 507-508, 665-673` | |
| F7.3 | Steps: `Reply(blocks...)`, `Say(text)`, `Func(f)` (sees the request and state), `Fail(msg)` | Own | `faux.ts:78-116` | |
| F7.4 | Event build per block, exact Pi order; abort checks before each block and each chunk | Own | `faux.ts:339-435` | Abort before start gives exactly one event. |
| F7.5 | Errors: exhausted queue (`No more faux responses queued`), failing `Func`, scripted `error`/`aborted`, scripted `pending` (`Faux response ended without a stop reason`) | Own | `faux.ts:424-431, 513-523, 556-560` | |
| F7.6 | Identity rewrite: `api`, `provider`, `model` set from the provider | Own | `faux.ts:282-292` | |
| F7.7 | Usage estimate `ceil(runes/4)`, transcript serialization and per-session common-prefix cache simulation | Own | `faux.ts:230-268` | Cost stays 0 unless `WithUsage` supplies it; Rune counting differs from Pi UTF-16 length; record and test that departure; Cache retention `none` neither reads nor updates the cache; See 4.7 for the exact formula. |
| F7.8 | Chunking 3..5 tokens of 4 runes; pacing `tokensPerSecond` with an injected clock | Own | `faux.ts:270-280, 331-337` | |
| F7.9 | Departures: seeded chunk sizes, counter tool ids, rune-boundary cuts, `WithUsage`, `Truncate(n)` (close with no terminal event), `ToolCallRaw` (exact bytes, broken JSON allowed), `Raw(events...)`, default stop `toolUse` when a tool call exists | Own | Pi is random and has none of these | Each fixes a flaky or missing test path; `Truncate` is a step modifier so it can cut a real scripted reply; `Raw` is the explicit event-input seam; it still uses builder validation and stream settlement. |
| F7.10 | Scripted faux replies go through the shared assembler (F3.6) | Own | Pi faux never fills partial args | Raw JSON tool calls exercise the parser; supplied final object arguments exercise the authoritative-end path. |
| F7.11 | Deferred handles, `onResponse` hook | Defer (skip; H11) | `faux.ts:294-306, 512` | |

## 3. Dependency matrix and interfaces

### 3.1 Source to local

| Pi component | Ask location | State |
|---|---|---|
| Message, content, usage, stop reason, tool declaration, diagnostics types | `pkg/protocol` (`message.go`, `content.go`, `usage.go`, `tool.go`) | NEW |
| `AssistantMessageEvent` (delta only) | `pkg/protocol/stream_events.go` | NEW |
| `AgentEvent` and envelope | `pkg/protocol/events.go` | NEW (file name is in the README) |
| Message builder (client reducer) | `pkg/protocol/builder.go` | NEW |
| JSON codec (discriminators, raw unknowns, JSONL helper) | `pkg/protocol/codec.go` | NEW |
| `StreamFn`, request, minimal model identity, `Provider` interface | `internal/providers/types.go`, `model.go` | NEW; H3 extends the model record (H-PROV-18). |
| `EventStream` | `internal/providers/stream.go` | NEW |
| Minimal request normalization | `internal/providers/convert.go` | NEW; raw request to transcript, without H6 replay |
| `ErrStreamIncomplete` | `internal/providers/errors.go` | NEW |
| Assembler | `internal/providers/assembler.go` | NEW |
| Default `convertToLlm` | `internal/providers/convert.go` | NEW |
| `json-parse.ts` | `internal/providers/partialjson` | NEW |
| SSE readers | `internal/providers/sse` | NEW |
| `faux.ts` | `internal/providers/faux` | NEW |
| Type placement rule | `internal/sessions/README.md:30` says message types come from `providers` | **CONFLICT**, resolved in 3.2 |

Add `go.uber.org/goleak` only if it is not already in `go.mod`; it is test-only.
`partialjson`, `sse` and `faux` add no runtime dependency.
When H1 adds the packages and test dependency, update the owning package READMEs and `AGENTS.md` maintenance context.
Do not edit an auto-generated file.

### 3.2 Type placement (resolved from accepted scope)

- Event types live in `pkg/protocol` (roadmap Phase 0 `bus` row; `internal/bus/README.md:15,38`; `internal/hooks/README.md:16`, "event and content types").
- Events carry messages (`message_start.message`, `turn_end.toolResults`, `agent_end.messages`).
  `pkg/protocol` may import only the standard library.
  Message, content and usage types must therefore live in `pkg/protocol`.
- `internal/sessions/README.md:30` ("`providers` (message types only)") is stale.
  H1 changes it to `pkg/protocol`.
- `internal/providers/README.md` "Allowed" gets `pkg/protocol`.
  Depguard needs no change: its rules are lax deny-lists and none denies `pkg/protocol` (verified in `.golangci.yml`).
- `pkg/protocol/README.md` gets the new file names.

### 3.3 Go shapes and event contract

This is a contract sketch, not generated code.
Use camelCase JSON names and the Pi discriminators.
Use optional pointers where absent, empty and false have different meanings.

```go
// pkg/protocol
// Text: Text string; TextSignature *string
// Thinking: Thinking string; ThinkingSignature *string; Redacted *bool
// ToolCall: ID, Name string; Arguments json.RawMessage; ThoughtSignature, Namespace *string
// AssistantMessage: EndTurn *bool; optional response/error/thinking fields preserve presence
// SystemMessage: Sections []Section; Section{Name string; Value *string}
// Sections encodes as an ordered JSON object, not an array or a sorted map.
// Diagnostic: Error *DiagnosticError; Details json.RawMessage
// DiagnosticError.Code: absent, JSON string or finite JSON number
// ToolExecutionResult: content, details?, structuredContent?, usage?, isError?, terminate?
// NestedToolCallRecord: id, name, arguments?, argumentsBytes?, status, durationMs?, error?

type Message interface{ Role() string }
type UserBlock interface{ userBlock() }           // Text, Image
type AssistantBlock interface{ assistantBlock() } // Text, Thinking, ToolCall
type Usage struct{ Input, Output, CacheRead, CacheWrite int64; CacheWrite1h, Reasoning *int64; TotalTokens int64; Cost Cost }
type Cost struct{ Input, Output, CacheRead, CacheWrite, Total int64 } // micro-USD

type AssistantMessageEvent interface{ streamEvent() }
type BlockEvent interface{ AssistantMessageEvent; blockEvent() } // nine block types only
// Start{Message AssistantMessage}: immutable seed, content [], stopReason pending
// TextStart{ContentIndex, Content Text}; TextDelta{ContentIndex, Delta}; TextEnd{ContentIndex, Content string, TextSignature *string}
// ThinkingStart{ContentIndex, Content Thinking}; ThinkingDelta{ContentIndex, Delta}
// ThinkingEnd{ContentIndex, Content string, ThinkingSignature *string, Redacted *bool}
// ToolCallStart{ContentIndex, ID, ToolName, Arguments, ThoughtSignature, Namespace}
// ToolCallDelta{ContentIndex, Delta}; ToolCallEnd{ContentIndex, ToolCall}
// Done{Reason, Message}; Error{Reason, Error AssistantMessage} // wire field is "error", not "message"
// MessageUpdate{AssistantMessageEvent BlockEvent, Usage Usage}, with Envelope embedded
// MessageStart/End{Message}, TurnEnd{Message, ToolResults}, AgentEnd{Messages}
// ToolExecutionUpdate{ToolCallID, ToolName, Args, PartialResult ToolExecutionResult}
// ToolExecutionEnd{ToolCallID, ToolName, Result ToolExecutionResult, IsError bool}

type Envelope struct{ Seq uint64; TS int64; SessionID, RunID, Type string }

// internal/providers
type Request struct{ SystemPrompt string; Messages []protocol.Message; Tools []protocol.ToolDecl }
type TranscriptRequest struct{ Messages []protocol.Message }
type StreamItem struct{ Event protocol.AssistantMessageEvent; Usage protocol.Usage }
func (s *Stream) Events() <-chan StreamItem
func (s *Stream) Result(ctx context.Context) (protocol.AssistantMessage, error)
type StreamFn func(context.Context, Model, TranscriptRequest, StreamOptions) *Stream
// Provider.Stream has the same signature; a value Model cannot be nil.
// StreamOptions includes SessionID and CacheRetention; request cancellation is the context.
```

Rules:

- `contentIndex` is the JSON field name, not `index`; `toolcall_start.toolName` is not `name`.
- `done.reason` permits `stop`, `length`, `toolUse`, `deferred`; `error.reason` permits `error`, `aborted`; `pending` is never a terminal reason.
- Unknown roles remain `RawMessage`; the default converter drops them.
  Register custom decoders during setup; reject duplicate and reserved role names and prevent races with decoding.
  Unknown content types inside known roles are decode errors.
  Unknown agent event types remain raw for future session events; a raw unknown type is not a valid provider terminal.
- Known message content that is absent or null becomes an empty slice at the decode boundary.
  Empty content, messages, toolResults and nested calls encode as arrays, not null.
  Usage and cost encode as objects, not null.
  `ToolCall.arguments` must be an object; `details` and structured tool results may be any valid JSON value.
- Copy mutable slices, ordered sections, diagnostics, nested calls and raw JSON at ownership boundaries.
  Earlier queued events, final messages, scripts and repeated results must not change when a producer or client changes its own copy.
- End events are authoritative for the complete block and its optional metadata.
  Missing metadata at an end removes an earlier value; an explicit empty signature or `redacted:false` remains present.
  Preserve content supplied only at start or only at end, including redacted thinking with no deltas.
- The protocol builder retains tool delta bytes but never imports `partialjson`.
  The producer assembler supplies parsed or authoritative objects at end and salvages open tool buffers at failed settlement.
- Ask cost values are micro-USD integers; Pi cost values are USD numbers.
  Matching field names do not make the costs interchangeable.
  H8 must use an explicit Pi import conversion and an Ask format version; H1 documents its own unit and does not claim an automatic Pi cost import.
- JSONL is a codec and headless output format here.
  It does not add a new remote agent transport; ACP plus `_ask/*` remains the accepted external protocol.

### 3.4 Exact H1 exit sequence

Use `WithChunk(1,1)`, a fixed clock and explicit tool ids `a`, `b`.
Script `Thinking("go")`, `Text("ok")`, `ToolCall("echo",{},ID("a"))`, `ToolCall("echo",{},ID("b"))`.
Each block has one delta under this chunk setting.

```text
start
thinking_start(0), thinking_delta(0,"go"), thinking_end(0,"go")
text_start(1), text_delta(1,"ok"), text_end(1,"ok")
toolcall_start(2,"a","echo"), toolcall_delta(2,"{}"), toolcall_end(2)
toolcall_start(3,"b","echo"), toolcall_delta(3,"{}"), toolcall_end(3)
done(reason="toolUse")
```

Map `start` to `message_start`, the nine block types to `message_update`, and the final result to `message_end`.
Wrap only those agent events in the envelope.
A setup error without `start` maps to `message_start` and `message_end` with the same final error message; it gives no update.
H1 tests this projection as a fixture; H2 owns the live loop and emitter.

## 4. Implementation approach (steps in order)

### 4.1 Step 1: documentation alignment (no Go code)

- `internal/sessions/README.md:30`: message types come from `pkg/protocol`.
- `internal/providers/README.md`: allow `pkg/protocol`; add `assembler.go`, `stream.go`, `convert.go`, `errors.go` and the sub-packages `faux/`, `sse/`, `partialjson/` to "File names".
- `pkg/protocol/README.md`: add `message.go`, `content.go`, `usage.go`, `tool.go`, `stream_events.go`, `builder.go`, `codec.go`.
- `plans/260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md`: apply the corrections in section 7 with a dated note.

### 4.2 Step 2: `pkg/protocol` types and codec

- Implement the types and field-presence rules in 3.3, including `ToolExecutionResult`, nested call records and diagnostic code variants.
- Implement discriminator codecs for message, block, provider event and agent event unions.
  Reject invalid known role/block combinations and invalid known terminal reasons.
  Preserve raw unknown agent events and message roles without exposing internal parser state.
- Implement JSONL encoding and decoding with LF framing and one optional trailing CR.
  Do not split on U+2028/U+2029 or impose Scanner's line limit.
  A final non-empty line without LF must decode; malformed complete lines return a decode error.
- Test field names against Pi and the explicit Ask extensions in 3.3.
  Test string-to-block normalization for both user and system content, image-only content, zero usage, ordered sections with null removal, optional empty/false values, nested call omissions, diagnostic string/number codes, and unknown roles/events.
  Test that arrays and objects remain arrays and objects when empty.
  Test Ask cost round trips in micro-USD.
  The generic codec cannot infer USD versus micro-USD from an integer field; H8 must select an explicit Pi import path before decode/conversion.
  Document Go lone-surrogate decoding with a U+FFFD test.

### 4.3 Step 3: message builder

- Seed identity, timestamp and available metadata once at `start.message` or agent `message_start`.
  Start blocks in contiguous content-index order and permit interleaved updates to different active blocks.
- Use append buffers with amortized linear work for text, thinking and raw tool JSON.
  Build immutable strings at end or on an explicit snapshot; do not rebuild the full message per delta.
  Expose raw preview bytes without a dependency on `internal/providers`.
- Replace the full block at an authoritative end, including supplied final tool arguments and optional-field removal.
  A terminal message or agent `message_end.message` replaces the full partial message.
  A setup error is valid before start; success and block updates are not.
- Test the sequence in 3.4, initial text/thinking/tool content with no deltas, signed empty blocks, redacted thinking, authoritative content only at end, final tool id/name/namespace changes, and interleaved blocks.
  Test negative and gap indexes, duplicate starts, wrong block kinds, deltas after end, duplicate ends, updates before start, and invalid terminal reasons.
  Test queued event immutability and result-copy ownership.
- Measure bytes and allocations for fixed-size deltas at two input sizes.
  Total serialized update bytes and append work must scale with the input size; do not use a flaky wall-clock threshold as a unit-test gate.

### 4.4 Step 4: `partialjson`

- Use strict parse; repaired strict parse; tolerant parse; repaired tolerant parse when the tolerant pass fails or returns an empty object.
  Non-object or null results become a non-nil empty map.
  Preserve Pi tolerance unless the departure is listed in 5.2.
- Implement `Repair` and a strict complete-JSON-with-repair helper for H3 SSE payloads.
  Do not use tolerant salvage to accept malformed complete protocol events.
- Port vectors V1 to V33 from the faux report section 3.3 into executable table tests before accepting the parser.
  V14 uses Go U+FFFD; V23/V24 retain the repaired control characters; V30 drops the invalid value (empty object for its single-key cases); V31 scalar and array inputs become `{}`.
  Strict and tolerant paths must reject non-finite number values, including exponent overflow.
- Test every byte prefix of valid object documents, nested arrays/objects, escapes, split UTF-8 and malformed input.
  The complete valid object equals a strict decode using the same number representation.
  Duplicate keys use last-key-wins semantics; do not assert that all partial values or nested keys are present in the final value.
  Use a fuzz target for no panic, finite values and valid JSON output.
- A salvaged partial object is never evidence that a tool call is safe to execute.
  H2 rejects all calls from a `length`, `error` or `aborted` response before tool execution.

### 4.5 Step 5: stream, errors, assembler, convert

- Use one producer owner, a bounded FIFO event channel, one final-result notification and one close path.
  `sync.Once` settles the message and Go error together; it does not by itself make concurrent channel send/close safe.
  Publish the result independently of event consumption and before a terminal send can block.
  Every data send and blocking wait must permit request cancellation.
- `Result(ctx)` returns the settled message and error if a result exists, even if the wait context is already cancelled.
  Before settlement it waits on the result notification or its own context.
  Calling it does not drain events or cancel the provider request.
  With backpressure, callers must drain events while waiting, or cancel the wait/request; a bounded channel cannot promise completion for an unconsumed unlimited stream.
- Normal streams emit one `done` or `error`, then close.
  Events after settlement are ignored, and a second close is harmless.
  On early producer close, settle the partial message with `stopReason:error`, error text `stream ended without a terminal event`, and `ErrStreamIncomplete`.
  The `Truncate` fixture deliberately sends no terminal event.
- On request cancellation, settle an aborted partial message with the latest usage and `errors.Is(err, context.Canceled)` (or `context.DeadlineExceeded`).
  Try to enqueue the single error event without waiting on a cancelled request.
  If the channel is full, terminal delivery can stop and the channel closes, even if the consumer later resumes draining.
  The result remains authoritative, so H2 must use its EOF fallback instead of requiring a terminal event to exist.
  This exception is required to avoid a leaked producer; never promise an always-delivered terminal event together with bounded buffering and an absent consumer.
- The assembler accepts an initial message seed, initial blocks, deltas by content index, full block ends, usage updates and response metadata updates.
  It emits immutable `StreamItem{Event, Usage}` values.
  Late response id/model/thinking level/diagnostics/raw stop/end-turn metadata reaches the terminal message.
  Usage updates need not emit an extra block event; the next item and the result carry the latest values.
- Tool end has two paths: a supplied final `ToolCall` wins; otherwise parse its raw buffer once.
  Failure, abort, early close and length settlement salvage each open tool buffer once, without an artificial `*_end` event.
  Preserve completed blocks and keep all scratch buffers out of the final record.
  Generate missing ids without colliding with supplied ids; identity and name changes are finalized by index.
- `ConvertToLLM` filters roles only; it does not do H3 replay repair.
  The minimal request normalizer folds shorthand prompt/tools into an initial system message with timestamp 0 and no mutation of caller data.
  Full system-section/tool-state replay stays in H6.
- Test close without terminal, result after close with no reader, wait cancellation, all second-terminal/second-close cases, active-drain abort, abort with a full abandoned channel, no scratch fields, authoritative final arguments without deltas, open-tool salvage, usage changes, and late metadata.
  Run these tests with `-race` and `goleak`.

### 4.6 Step 6: `sse` reader

- API: `NewReader(r io.Reader, opts...)`, `WithMaxLineBytes(n)` and `WithMaxEventBytes(n)` (each default 32 MiB), `Next(ctx) (Event, error)`, `ErrLineTooLong`, `ErrEventTooLong`.
  The caller owns the reader and makes blocked reads cancellable, for example with a context-bound HTTP body or a pipe closed by `context.AfterFunc`.
  `io.Reader` itself has no cancellation contract; do not hide a blocked read in a goroutine.
- `Event` has name, data and bounded raw lines for adapter diagnostics.
  Preserve named events with empty data, including `event:error`; do not confuse them with empty/comment-only frames.
  Reset frame state on every blank line, including a frame with only comments.
- Keep a pending-CR state across reads so split CRLF is one line ending.
  Strip the BOM only at stream start, even if its bytes are split.
  Emit a pending final event on clean EOF, including a final unterminated line.
  A non-EOF read error is an error, not successful completion.
- Bound aggregate frame/raw storage as well as single lines, using a configurable event limit with a 32 MiB default and `ErrEventTooLong`.
  Check limits as bytes arrive and check cancellation before every dispatch, including buffered events.
  After a limit/read error the reader stays failed.
- Test 200 KiB and 10 MiB lines, exact and over-limit line/event sizes, all line endings, every split around CRLF/BOM/UTF-8, one-byte reads, comments, one leading space, multiple data lines, field without colon, named empty-data events, EOF flush and non-EOF read failure.
  Test that a comment-only stream does not retain all raw lines.
- Test blocked-read cancellation with an owned cancellable pipe or context-bound `httptest` request.
  Test that a small adapter fixture stops after its terminal event while the server keeps the connection open; this is a transport fixture, not the H3 adapter implementation.
  Keep idle/header timeout and protocol-specific terminal mapping in H3/H4.
  Benchmark linear append/scan work and fuzz split versus unsplit input.

### 4.7 Step 7: `faux`

- Keep the API from the faux report section 2.5, with these corrections: `Truncate(n)` is a modifier of a scripted step; `Raw(events...)` is the explicit event-input seam.
  `Func` receives the request context, a captured call number and the selected model/options.
  `Requests` captures model, options and transcript data as immutable records, not just the message slice.
- Validate constructor options and model definitions; reject duplicate/empty model ids and invalid chunk ranges.
  Unknown model lookup must return an explicit miss, not a zero model.
  Preserve per-model reasoning, input capabilities and limits; no global registry is needed in H1.
- Take the step, assign the call number and record the request synchronously in call order, including exhausted calls.
  Synchronize queue, counters, cache and PRNG state.
  Allocate random chunk choices and generated ids per call so goroutine scheduling cannot change a fixed script's chunks.
  `Set` replaces only pending steps; it does not reset call count, in-flight replies or the session cache.
- Script replies use the assembler; `Raw` uses the same builder validation and settlement but bypasses scripted block generation.
  Preserve immutable input scripts and message metadata; overwrite only api/provider/model plus generated defaults.
  Preserve explicit timestamp 0, empty text and thinking, and the one empty delta that Pi emits for an empty scripted block.
  Pacing/Delay use cancel-aware waits on the injected clock.
- Define usage from the normalized transcript, not from the stale `tools:` formula in the Pi faux test.
  Serialize message roles with `role:` and blank lines; serialize system sections in order, `tool-:` removals and `tool+:` additions, user/result images as `[image:mimeType:dataLength]`, assistant tool calls as `name:JSON(arguments)`, and tool results with the tool name.
  Count runes in that serialized text and output; image base64 length is its ASCII length.
  Test ASCII and emoji explicitly because Pi counts UTF-16 units.
- Cache is local to a faux instance and keyed by session id.
  First call: `input=promptTokens`, `cacheRead=0`, `cacheWrite=promptTokens`.
  Later calls: use the common prefix of the prior/current serialized prompt; estimate its read tokens and suffix write tokens; `input=max(0,promptTokens-cacheRead)`.
  `totalTokens=input+output+cacheRead+cacheWrite`; do not count `reasoning` or `cacheWrite1h` twice.
  No session id or retention `none` means zero cache fields and no cache update.
  `WithUsage` returns exactly the supplied usage and cost, including optional fields; it does not change the cache-estimate policy.
- Test queue/exhaustion, replacement/append, per-call factory state, selected model/options, factory error, setup error with no start, exact event order, two tools, scripted error/aborted/pending/length, abort before start and within each block, and abort during delay.
  Test identity rewrite, unchanged input script, empty blocks, metadata, exact usage override, common-prefix change/shrink, disabled cache, model lookup, concurrent calls and provider-instance isolation.
  Test truncated scripted content, raw broken JSON, authoritative final object replacement, fixed seed/chunk choices and valid UTF-8 emoji chunks.

### 4.8 Step 8: H1 exit test

- Use the sequence in 3.4 and the test-only projection from provider items to agent events.
  Seed `message_start`; wrap only block events in `MessageUpdate` with their captured usage; emit `message_end` with the result.
  Encode JSONL, decode and rebuild; compare all message fields with the normalized script and `Result(ctx)`.
- Cut the same reply after 0, 1, 3 and mid-tool events with its `Truncate` modifier.
  Check `ErrStreamIncomplete`, preserved completed/partial content, identity and usage, and no blocked result after producer close.
- Run focused package tests first, then `go test -race ./pkg/protocol/... ./internal/providers/...` and the repository lint command.
  Product tests are future H1 acceptance checks; the source audit does not claim these packages already exist.

## 5. Decisions (resolved from accepted scope) and departures from Pi

### 5.1 Decision matrix

| Decision | Pi's way | Ask's way | Basis |
|---|---|---|---|
| Type home | `packages/ai` types | `pkg/protocol` for message, content, usage, events; `internal/providers` for stream, assembler, faux, parsers | Roadmap Phase 0 `bus` row; `internal/hooks/README.md:16`; `pkg/protocol` imports only stdlib |
| Faux home | `packages/ai` (production code) | `internal/providers/faux` | Roadmap H1 "Packages: providers (types, SSE reader, faux provider)"; `internal/testsupport/README.md` forbids production code; The inventory note "copy into testsupport" is superseded. |
| System message model | Delta `SystemMessage` (0.86.0) | Same type in H1; replay helpers in H6/H8 | D1 = Pi semantics; inventory H-LOOP-15 "design the transcript this way from day 1" |
| Event shape in process | Cumulative partial in process, stripped on the wire | Delta only everywhere | H-MODE-05, roadmap "Do not rebuild cumulative message/partial" |
| Partial-JSON parse time | Live adapters parse per delta; frame replay salvages open calls | Once at raw tool end or failed settlement; authoritative objects bypass parse | Delta-only events plus `AI:utils/assistant-message-frame.ts:482-487` |
| SSE reading | Own reader for Anthropic, SDK for OpenAI | Own reader, 32 MiB default limit | Roadmap H1 "SSE reader without `bufio.Scanner`"; The Go libraries report (GL§2) leans to official SDKs: re-check at H3, does not block H1. |
| Stream API | Async iterable + `result()` promise | Bounded channel plus independently settled `Result(ctx)` | Pi hazard F3.2; explicit backpressure and cancellation rules in 4.5 |
| `deferred` | Stop reason + `DeferredHandle` | Keep the constant, skip the handle | File compatibility; H-PROV-08 is skip |
| Unknown role | Declaration merging | Raw JSON kept, registry for decoders | Go has no declaration merging |

### 5.2 Departures from Pi (one place, like D4)

1. Cost in micro-USD integers, not float USD.
2. Event envelope (`seq`, `ts`, `sessionId`, `runId`).
3. Delta-only events in process, not only on the wire.
4. `Result(ctx)` supplies a typed error and a final message on early close; cancellation can end terminal delivery to an abandoned full channel.
   Callers retrieve the stored result after EOF.
5. Faux: seeded per-call chunks, per-call counter ids (`tool:<call>:<n>`), rune-boundary cuts, `WithUsage`, `Truncate`, `ToolCallRaw`, `Raw`, default stop `toolUse` when a tool call exists.
6. Partial JSON: repaired retry also on an empty tolerant result; scalars become `{}`; no `NaN`/`Infinity`.
7. SSE: configurable line/event limits with typed errors, split-CRLF handling and bounded comment/raw state.
   BOM removal is parity with Pi, not a departure.
8. Unknown roles kept as raw JSON; unknown block types inside a known role are an error.
9. Immutable seed/block metadata fields extend Pi JSON updates so delta-only producers can keep signatures and initial block content.
   Pi durable frame storage and checkpoints are not ported in H1.
10. Faux usage and chunk boundaries count runes; Pi counts UTF-16 units.
    Exact `WithUsage` bypasses the estimate.
11. The assembler fails the stream when `Done` is called with an unfinished block and a reason other than `length`.
    Pi would keep a tool call with partial arguments; real Pi adapters and the Pi faux close every block before `done`, so a valid producer never reaches this path.
    `Done` with `error` or `aborted` is also rejected; `Fail` is the only failure path, so a failed result always carries a Go error.
12. A terminal event whose `reason` differs from its message `stopReason` is rejected on encode, on decode and in the builder.

### 5.3 Notes for later phases (not H1 decisions)

- H2: use the stored result after stream EOF, including the abandoned-consumer cancellation exception; never wait for a missing terminal event.
- H2: who assigns `seq` and `runId` (recommend the session emit sink, one counter per session); listener error policy (Pi: awaited, ordered, an error ends the run); keep a run-failure safety net like `A:agent.ts:532-547`.
- H3: own HTTP + SSE vs official SDKs (re-check GL§2); outbound UTF-8 rule (`strings.ToValidUTF8(s, "")`, Pi removes bad text); `error-body` 4000-char cap.
- H9: cost rate unit that keeps `$/Mtok` exact (for example nano-USD per token) and the rounding rule; retry by type first (`ErrStreamIncomplete`), text second.
- T1: preview of large `write`/`edit` arguments parses its own buffer on a timer.

## 6. Resolved user decision: `transformMessages` ownership

The original roadmap put the replay function in H4.
The Anthropic adapter in H3 already calls `transformMessages` (`AI:api/anthropic-messages.ts:1057`, verified).
It repairs errored/aborted history and missing tool results before the request is sent.
The user chose H3 for this function and its replay repair; the current roadmap records that decision.

| Option | Effect |
|---|---|
| A; Keep H4 | H3 works only on the happy path until H4. |
| B; Move the pure function and its table tests into H1 | H1 grows by about 170 lines plus tests and needs model identity data early. |
| C; Move the pure function to H3, its first caller; H4 keeps the cross-API replay tests and the per-vendor id rules | H3 exit is safe; H1 stays focused. |

**Decided (user, 2026-10-01): C.**
`roadmap.md` H3 and H4 and the confirmed-decisions table in the roadmap `plan.md` record the change.
F1.14 is owned by H3.

## 7. Inventory corrections (source verified; record in `inventory-harness.md`)

| Row | Correction | Evidence |
|---|---|---|
| H-SESS-05 | Assistant content has no `image` block | `AI:types.ts:548` |
| H-SESS-04 | `convertToLlm` is stage 1 of 2; stage 2 is `transformMessages` in each adapter | `AI:api/transform-messages.ts:64-235` |
| H-PROV-09 | Since 0.64.0, not 0.70.0; home is `internal/providers/faux`, not `testsupport` | ai CHANGELOG line 1274; roadmap H1 packages |
| H-MODE-04 | Core `agent_end` has only `messages`; `willRetry` is session level | `A:types.ts:514-529`; `C:core/agent-session.ts:180-231` |
| H-MODE-05 | Cite coding-agent 0.84.0 (delta only), 0.84.2 (`usage`), 0.84.3 (`toolcall_start` id and name), not the agent changelog | coding-agent CHANGELOG lines 590, 527, 449 |
| H-LOOP-16 | The `result()` fallback is at `A:agent-loop.ts:460`; add the hang hazard | grep; GitNexus `EventStream` |
| (doc) | `SystemMessage.replace` exists only in `CD:message-types.md`; no code reads it | grep of `ai/src`, `agent/src`, `coding-agent/src` |

## 8. Risks

H1 does not yet touch a runtime path, but it defines shared contracts for all later phases.
The main risks are lost metadata, mutation of queued data, incomplete result settlement, quadratic buffer work, and Pi cost-unit confusion.
Use the contract tests in section 4 and the audit evidence to control these risks.
The SSE reader may be unused if H3 chooses an SDK reader; re-check that choice in H3.

## 9. Rollback

All work is new files in `pkg/protocol` and `internal/providers` plus the owning documentation and inventory notes.
Review the final commit list before a revert.
Roll back with `git revert` of the H1 commits.
There are no migrations, no config changes and no data.

## 10. Handoff

Plan ready at `plans/261001-0836-h1-messages-events-faux/plan.md`.
To implement, run `/ak:cook plans/261001-0836-h1-messages-events-faux/plan.md`.

Read the audit with this plan.
The older scout reports are evidence records; this revised plan owns the H1 implementation requirements when those reports conflict with it.

## 11. Review (implementation, 2026-10-01)

Implemented with `/ak:cook --auto --advice`. All acceptance criteria in section 1 are met; each has a named test (see the code review report).

- Code: `pkg/protocol` (types, stream and agent events, envelope, JSON/JSONL codec, builder), `internal/providers` (`types.go`, `model.go`, `stream.go`, `errors.go`, `assembler.go`, `convert.go`), `internal/providers/{partialjson,sse,faux}`. About 5,400 lines of production code and 5,000 lines of tests.
- Docs: `internal/providers/README.md`, `internal/sessions/README.md`, `pkg/protocol/README.md`, `CLAUDE.md` and `AGENTS.md` (goleak), `inventory-harness.md` section 18.
- Verification: `go test -race -count=3 ./pkg/protocol/... ./internal/providers/...` passes; `go vet` clean; `golangci-lint run ./pkg/... ./internal/providers/...` reports 0 issues; `go mod tidy -diff` is clean. goleak runs in `providers`, `faux` and `sse`.
- Review: advisory checkpoints after wave 1, after the stream step and at the end; one code review (`plans/reports/code-reviewer-261001-1115-h1-implementation-review.md`: 2 Major and 8 Minor findings, all fixed with regression tests). The last checkpoint found that `RawEvent` did not write envelope changes back; fixed with `TestRawEventEnvelopeChangesAreWrittenBack`.
- Departures added during implementation: 5.2 items 11 and 12; per-call faux tool ids; lenient decode of `text_start` and `thinking_start` without `content` (Pi JSON mode); `faux.Raw` passes the script identity through.
- Agent reports: `plans/reports/fullstack-developer-261001-1036-h1-{protocol,partialjson,sse,stream-assembler,faux}.md`.
- Out of scope, not fixed: `go build ./...` fails in this worktree because `internal/logs` is not tracked. `.gitignore:44` (`logs/`) also matches `internal/logs`. The package exists in the main checkout.

## Unresolved questions

None for H1. The `internal/logs` build failure needs an owner decision (see section 11).
