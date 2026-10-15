# H1 implementation review (messages, events, faux)

Date: 2026-10-01. Reviewer: code-reviewer. Mode: review only, no code edits.
Contract: `plans/261001-0836-h1-messages-events-faux/plan.md` (sections 1, 3.3, 3.4, 4.2-4.8, 5.2).

## Scope

- `pkg/protocol/*.go` (types, codec, builder, stream events, agent events).
- `internal/providers/{types,model,stream,errors,assembler,convert}.go`.
- `internal/providers/faux/*`, `internal/providers/partialjson/*`, `internal/providers/sse/*`.
- README edits: `internal/providers`, `internal/sessions`, `pkg/protocol`; `AGENTS.md`; `go.mod` (goleak).
- About 10.5k lines including tests.
- Method: read all production files; compared JSON names with Pi `ai/src/types.ts:389-783`, `agent/src/types.ts:514-529` and `coding-agent/src/modes/json-event.ts`. Ran probe tests through `go test -overlay` (no file in the work tree changed).

## Commands and results

| Command | Result |
|---|---|
| `go test -race -count=1 ./pkg/protocol/... ./internal/providers/...` | PASS (protocol, providers, faux, partialjson, sse; acp has no tests) |
| `golangci-lint run ./pkg/... ./internal/providers/...` | 0 issues |
| `go mod tidy -diff` | no diff |
| Import check (`go list`) | `pkg/protocol` imports stdlib only. `internal/providers` imports only `pkg/protocol` and its own sub-packages. |
| Plan-label grep (`F1.x`, `H1`, `G1`, `E§`, `audit`, `phase N`) in code and test names | none found. Test names `V1`-`V33` come from the vector table and are acceptable. |

## 1. Acceptance criteria (plan section 1)

| # | Criterion | State | Proof (test names) |
|---|---|---|---|
| 1 | Exact 3.4 event order, final message equals normalized script | MET | `faux.TestExitSequenceEventOrder` |
| 2 | Projection to message_start / 9 updates / message_end, JSONL round trip, no terminal inside updates | MET | `faux.TestExitProjectionJSONLRoundTrip` (+ `assertNoTerminalInUpdates`), `faux.TestExitProjectionOfSetupError`, `protocol.TestOnlyNineEventsAreBlockEvents`, `protocol.TestMessageUpdateRejectsNonBlockEvents` |
| 3 | Close without terminal gives `ErrStreamIncomplete`; Result with no reader; wait can be cancelled; no leaked producer | MET | `TestStreamCloseWithoutTerminalIsIncomplete`, `faux.TestExitTruncatedReplyKeepsPartialContent`, `TestStreamResultWithoutReader`, `TestStreamResultWaitCancelBeforeSettle`, `TestStreamAbortWithFullAbandonedChannelLeaksNothing` |
| 4 | Initial content, signatures, redaction, final-only args, changing ids, mid-stream usage, partial content on failure | MET | `TestBuilderSignedEmptyBlocksAndRedactedThinking`, `TestAssemblerInitialContentSignaturesAndRedaction`, `TestAssemblerAuthoritativeFinalArgumentsWithoutDeltas`, `TestAssemblerIDChangeAtEndIsFinalizedByIndex`, `TestAssemblerUsageReachesItemsAndResult`, `TestAssemblerOpenToolSalvage` |
| 5 | Builder errors without panic; no shared mutable data in queued events and results | MET | `TestBuilderRejectsBadSequences`, `TestAssemblerBuilderErrorsFailTheStreamWithoutPanic`, `TestBuilderOwnsItsState`, `TestAssemblerQueuedItemsAreImmutable`, `TestStreamResultReturnsIndependentCopies` |
| 6 | Partial JSON V1-V33 and prefix sweep | MET | `TestParseVectors` (all V1-V33 present), `TestParsePrefixSweep`, `FuzzParse` |
| 7 | SSE 200 KiB and 10 MiB lines plus 4.6 tests | MET | `TestLargeDataLines`, `TestSplitCRLFIsOneLineEnd`, `TestBOM`, `TestEventLimit`, `TestCancelBlockedReadWithOwnedPipe`, `TestStopAfterTerminalEventClosesConnection` |
| 8 | `-race`, goleak, lint | MET | Command table above. goleak `TestMain` exists in `providers`, `faux`, `sse`. `pkg/protocol` and `partialjson` start no goroutine, so they need none. |

## 2. Findings

No Critical finding. Two Major findings. Each finding was confirmed by code reading and, where marked, by a probe test.

### Major

**M1. Faux: a `Func` step changes the chunks and tool ids of later fixed steps (scheduling-dependent).**
- Where: `internal/providers/faux/stream.go:82-84` (Reply/Raw planned at call time) versus `stream.go:244-246` (`resolveFunc` calls `planLocked` later, in the producer goroutine). Both use the shared `p.rng` and `p.toolSeq`.
- Scenario (probe confirmed): queue `Func(...)`, then `Reply(Text(40 x "b"), ToolCall("t", nil))`. If call 2 starts before the factory of call 1 runs, call 2 gets chunks `[16 12 12]` and id `tool:1`. If call 1 is drained first, call 2 gets `[4 4 12 4 8 4 4]` and id `tool:2`.
- Contract: plan 4.7 "Allocate random chunk choices and generated ids per call so goroutine scheduling cannot change a fixed script's chunks." `TestFixedScriptChunksDoNotDependOnScheduling` covers only `Say` steps, so the gap is not tested.
- Fix: derive one PRNG per call from `(seed, callNumber)` (for example `rand.NewPCG(seed, uint64(call))`) and use a per-call id namespace (for example `tool:<call>:<n>`), or reserve the call's rng/id state at call time. Add a test with a `Func` step before a `Reply` step.

**M2. `EncodeEvent` can write a raw line feed for `RawEvent` and breaks JSONL framing.**
- Where: `pkg/protocol/codec.go:104` (`encodeRawEvent` returns `bytes.TrimSpace(e.Data)`); same pattern in `message.go` `marshalRawMessage` (only safe there because `encoding/json` compacts nested `MarshalJSON` output).
- Contract: `codec.go:13-15` "The output holds no raw line feed, so it is safe for JSONL framing."
- Scenario (probe confirmed): `DecodeEvent` of an indented unknown event (`{\n "type":"queue_update", ...}`) gives a `RawEvent`; `EncodeEvent` returns bytes with `\n`. `JSONLWriter.Write` then writes several lines, and `JSONLReader` + `DecodeEvent` fail on the first partial line. H2 session events that pass through as `RawEvent` (or H8 import from a pretty-printed file) hit this path.
- Fix: `json.Compact` the data in `encodeRawEvent` and `marshalRawMessage` (this also validates). Add a test with an indented raw event through `JSONLWriter`.

### Minor

**m1. `DiagnosticError` with a whitespace-only `Code` panics on marshal.** `pkg/protocol/message.go:237-238`: `t := bytes.TrimSpace(e.Code); if t[0] == '"'` indexes an empty slice. Probe: `json.Marshal(DiagnosticError{Message:"m", Code: json.RawMessage(" ")})` panics "index out of range [0]". Not reachable from wire input, reachable from a Go producer (H3/H4). Fix: treat `len(t)==0` as absent or return an error.

**m2. Raw seam `Assembler.Emit(DoneEvent)` skips the open-block rule of departure 11.** `internal/providers/assembler.go:312-314` settles with `e.Message` directly; `Done()` (`assembler.go:257-262`) fails a non-length reason with open blocks. Probe: builder accepts `done(stop)` after an unclosed `text_start`. Also `Emit(ToolCallEndEvent)` accepts non-object arguments (builder `closeBlock` does not check), so the queued item later fails `MarshalStreamEvent` and the final message cannot encode. The builder tolerance itself is correct for clients (done replaces the partial). Fix: in `Emit`, apply the same open-block check for `DoneEvent` (non-length) and the object check for `ToolCallEndEvent.ToolCall.Arguments` that `ToolEnd` uses. Note: `faux.Raw` also passes the script's api/provider/model through (`TestRawEvents` asserts this); this is documented on `Raw`, so it is a contract question, not a defect.

**m3. `UnmarshalStreamEvent` rejects Pi JSON-mode `text_start` and `thinking_start`.** `pkg/protocol/stream_events.go:364` and `:390` call `UnmarshalJSON` on an absent `content` and fail with "unexpected end of JSON input". Pi `C:modes/json-event.ts` writes these events without `content`. Plan 3.3 and departure 9 call initial content an Ask *extension*; an extension that cannot read the base form is a silent compatibility break. Fix: absent or null `content` gives an empty `Text`/`Thinking`. Ask-to-Ask round trips are not affected.

**m4. `Builder.ApplyAgentEvent(*MessageEnd)` accepts a success message before `message_start`.** `pkg/protocol/builder.go:137-148` has no `started` check. Plan 4.3: "A setup error is valid before start; success and block updates are not." `Apply(DoneEvent)` enforces this; the agent-event path does not. Fix: before start, accept only `error`/`aborted` stop reasons.

**m5. Builder accepts a `StartEvent` whose message is not pending.** `builder.go:163-189` does not check `StopReason`. Plan 3.3: "Start{Message}: immutable seed, content [], stopReason pending". Fix: reject a non-pending (and non-empty) stop reason, or normalize it to pending.

**m6. `NormalizeRequest` doc overclaims isolation.** `internal/providers/convert.go:25-26` says "The result shares no slice or raw JSON with r", but `convert.go:38` appends the caller's messages by value; `UserMessage.Content` and other inner slices are shared. `TestNormalizeRequestDoesNotShareOrMutateCallerData` checks only the top-level slice and the tool JSON. The function does not mutate caller data, so the plan rule is met. Fix: correct the doc (as `ConvertToLLM` does), or clone each message.

**m7. `CloneMessage` changes pointer messages to value messages.** `pkg/protocol/message.go:795-823`: `CloneMessage(&UserMessage{})` returns `protocol.UserMessage` (probe confirmed); same for `*SystemMessage`, `*AssistantMessage`, `*ToolResultMessage`, `*RawMessage`. `faux.cloneTranscript` applies it to every request message, so a `Func` factory or `Requests()` reader that type-asserts `*T` fails. Fix: keep the pointer kind, or document the change.

**m8. `TestBuilderDeltaWorkScalesLinearly` uses process-global memory counters.** `pkg/protocol/builder_test.go:472-520` reads `runtime.ReadMemStats` and asserts absolute allocation counts (`< n/10`). Background allocation by another goroutine can make it fail. It passes now. Fix: use `testing.AllocsPerRun` or ratio-only assertions.

### Concurrency review (stream.go, assembler.go): no defect found

- One producer goroutine owns every send and the only `close(s.events)` (`stream.go:97-102`); the Assembler documents single-goroutine use.
- `settle` uses `sync.Once` and closes `done` after it writes `msg`/`err`; readers read only after `<-s.done`, so there is a happens-before edge. `Result` clones on every call.
- The result is settled (`seal`) before the terminal send (`deliver`), so `Result` works without a reader. After cancel, `deliver` tries one non-blocking send, so an abandoned full channel does not hold the goroutine.
- Panics in the body and in `finish` are recovered (`runBody`, `safeFinish`) and always set `terminal`, so `deliver` never sends a nil event.
- Known, documented limit (plan 4.5): if nobody reads and the request is never cancelled, a full channel holds the producer; faux raises the buffer to 32 to soften this.

### Other checks with no finding

- JSON names match Pi for content blocks, usage, cost, all four messages, stream events (`contentIndex`, `toolName`, `reason`, `error`) and agent events (`toolCallId`, `args`, `partialResult`, `isError`, `toolResults`).
- Presence: pointer fields with `omitempty`; explicit `""` signature and `redacted:false` round trip; `cacheWrite1h`/`reasoning` keep 0.
- Faux cache formula matches Pi `withUsageEstimate` (first call writes all, then common prefix read / suffix write, `input=max(0,prompt-read)`, retention `none` or no session skips cache). Exhausted queue updates the cache like Pi.
- Partial JSON: strict path rejects `1e999` through `encoding/json`; tolerant path rejects it in `jsonNumber`; NaN/Infinity give no key.
- SSE: split CRLF uses `pendingCR`; BOM is stripped only at start, also when split; line and event limits are checked as bytes arrive; errors are sticky; comment-only frames keep no raw lines.
- Tests: no assertion fails on timing. Three waits are weak under load in the false-pass direction only (`stream_test.go:278`, `sse/reader_test.go:310`, `faux_test.go:611`). The salvage case in `TestExitTruncatedReplyKeepsPartialContent` checks only `json.Valid`, not the salvaged value.

## 3. Recommended actions (priority order)

1. Fix M1 (per-call rng and id namespace) and add a Func-then-Reply scheduling test.
2. Fix M2 (compact raw event and raw message data) and add an indented-raw-event JSONL test.
3. Fix m1 (empty `Code` guard) and m3 (absent initial `content` decodes).
4. Align `Emit` with `Done`/`ToolEnd` validation (m2); add the `message_end`-before-start and pending-seed checks (m4, m5).
5. Correct docs or behavior for m6 and m7; make m8 robust.

## Plan status

All eight acceptance criteria have proving tests. The plan steps 4.1-4.8 appear complete. M1 is a gap against step 4.7. M2 is a gap against the JSONL contract in step 4.2. No plan file was edited.

## Unresolved questions

1. Must `faux.Raw` rewrite api/provider/model like scripted replies (plan 4.7), or does the documented pass-through stay (m2 note)?
2. Must Ask decode Pi JSON-mode event streams (m3), or is Ask-to-Ask the only H1 target?

Status: DONE_WITH_CONCERNS
Summary: Tests (-race), goleak and lint pass, and all eight acceptance criteria have named proving tests. Two Major defects (faux Func-step nondeterminism; RawEvent can break JSONL framing) and eight Minor items need fixes or decisions.

## Fixes applied

Date: 2026-10-01. Each fix has a regression test. Verified: `go test -race -count=3`, `go vet`, golangci-lint and `gofmt -l` are clean for `pkg/protocol` and `internal/providers`.

| Finding | Fix | Test |
|---|---|---|
| M1 | Chunk sizes use a generator seeded with (seed, call number). Generated tool ids are `tool:<call>:<n>`. Planning needs no lock. Existing id assertions updated to the new format. | `faux.TestFuncStepDoesNotChangeLaterReplyChunksAndIds` |
| M2 | `encodeRawEvent` and `marshalRawMessage` use `json.Compact` (`compactObject`). | `protocol.TestIndentedUnknownEventStaysOneJSONLLine`, `protocol.TestIndentedUnknownRoleStaysOneLine` |
| m1 | `DiagnosticError.validate` returns an error for a blank code. | `protocol.TestDiagnosticErrorWithBlankCodeIsAnError` |
| m2 | `Assembler.Emit` fails the stream for `done` with open blocks (reason not length) and for `toolcall_end` with non-object arguments (`checkRaw`). | `providers.TestAssemblerEmitDoneWithOpenBlockFailsTheStream`, `TestAssemblerEmitDoneLengthKeepsOpenBlock`, `TestAssemblerEmitToolEndWithNonObjectArgumentsFailsTheStream` |
| m3 | `text_start` and `thinking_start` decode without `content` (or null) as an empty block. Old malformed-case test now uses a wrong block type. | `protocol.TestDecodeStartEventsWithoutInitialContent` |
| m4 | `ApplyAgentEvent(*MessageEnd)` before start accepts only error or aborted. | `protocol.TestBuilderMessageEndBeforeStartAcceptsOnlyFailures` |
| m5 | Builder rejects a start message whose stop reason is not pending (empty is accepted). The faux setup-error projection in `exit_test.go` now uses a pending seed. | `protocol.TestBuilderRejectsStartWithTerminalStopReason` |
| m6 | `NormalizeRequest` doc states that inner message values are shared and never mutated. | doc only (`TestNormalizeRequestDoesNotShareOrMutateCallerData` unchanged) |
| m7 | `CloneMessage` keeps pointer kind for all pointer message types (nil pointer returned as is). | `protocol.TestCloneMessageKeepsPointerKind`, `faux.TestRequestRecordKeepsPointerMessages` |
| m8 | `minMeasure` takes the minimum of 7 readings, since background allocation can only add to a reading. | `protocol.TestBuilderDeltaWorkScalesLinearly` |
| Raw note | `faux.Raw` doc says script api/provider/model pass through unchanged. Behavior unchanged. | `faux.TestRawEvents` |
