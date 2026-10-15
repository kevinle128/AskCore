---
phase: 1
title: "Lifecycle vocabulary, IDs, wire events, attempt boundary and max-tokens stop"
status: done
priority: P1
effort: 10h
dependencies: [phase-00]
---

# Phase 01: Lifecycle vocabulary, IDs, wire events, attempt boundary and max-tokens stop

## Goal

Give the driver explicit cycle, turn and attempt boundaries with opaque IDs and cycle reasons, and publish them additively on the wire. The Pi meaning of `turn_start`/`turn_end` does not change (Q1, resolved by D25: the JSON stream stays Pi). Land the DeepSeek max-tokens rule here, together with the sticky reason, so that no commit has a sticky reason without the stop rule. <!-- red-team #11 -->

## Vocabulary (fixed for every later phase) <!-- red-team #9 -->

| DeepSeek | Ask Go | Ask wire |
|---|---|---|
| turn (one input cycle) | `cycle` (`cycleState`, `CycleReason`) | `cycle_start{cycleId}`, `cycle_end{cycleId, reason}` (new) |
| step (one model answer and its tools) | `turn` (existing `turnState`, `runTurn`, `turnStages` keep their names) | `turn_start`/`turn_end` (Pi, unchanged; new optional field `cycleId`) |
| attempt (one model request) | `attempt` (`attemptState`) | `attempt_start{attemptId, cycleId, number}` (`number` counts the attempts of the Agent session, as DeepSeek `agent.ts:426-428`), `attempt_end{attemptId, outcome}` (new) |

The Go name of each scope matches its wire name, so no Go identifier changes meaning (Scope finding 2). One exception, from Pi: during a retry series the wire wraps each attempt in its own `turn_*` and `agent_*` pair (phase 08, `C:core/agent-session.ts:1112`), while the Go turn and cycle stay one. Wire divergence rows: matrix W1 and 35d.

## Context links

- Current loop: `internal/agent/loop_run.go:115-150` (driver), `internal/agent/loop_stage.go:56-72` (six stages per turn), `:82-106` (steer emits `TurnStart`), `:176-201` (decide emits `TurnEnd`)
- Truncation path to remove: `internal/agent/loop_tools.go:60-78` (`truncatedExecutor`), `:94-99` (selection)
- Run-failure tail: `internal/agent/agent.go:263-295` (`fail` emits `TurnEnd` always, as Pi `A:agent.ts:532-548`)
- Wire: `pkg/protocol/events.go:5-18,54-62`; codecs `pkg/protocol/codec.go`, `codec_fast.go`
- DeepSeek: `packages/core/agent-loop/src/agent.ts:296-396` (turn), `:336-341` (sticky), `:530` (max-tokens ends the step), `packages/llm/llm/src/assembler.ts:137-138` (tool calls dropped), `packages/core/session/src/surface.ts:136-142` (empty assistant not replayed)

## Files to Create / Modify

- Create: `internal/agent/lifecycle.go` (cycle and attempt state, ID generation, `CycleReason`)
- Modify: `internal/agent/loop_run.go`, `loop_stage.go`, `loop_stream.go` (attempt open/close; strip tool calls of a `length` message before commit), `loop_tools.go` (remove `truncatedExecutor`), `emit.go`, `agent.go` (`fail` closes only opened cycle/attempt scopes)
- Modify: `internal/providers/transform.go` (skip an assistant message with no content blocks in replay)
- Modify: `pkg/protocol/events.go`, `codec.go`, `codec_fast.go`, `pkg/protocol/README.md`
- Modify tests (deliberate golden changes, each listed): <!-- red-team #13 -->
  - `cmd/tui/headless_test.go`: `TestJSONHelloLines` (:131-186, byte-exact lines and `seq` 1-N; `normalize()` at :104 masks `cycleId`, `attemptId`), `TestJSONEchoEventOrder` (:187, seq check :210), `TestJSONAssistantErrorExitsZero` (:252, last-4-labels tail at :258), `TestJSONTwoPrompts` (:265, seq check :272), `TestJSONSlowReaderStallsRun` (:335, seq check :358), `TestJSONSmallReplyWaitsForFinalFlush` (:363, line count)
  - `cmd/tui/headless_record_test.go`: `TestCassetteJSONEventOrder` (:127-154)
  - `internal/agent/agent_test.go`: `failureTail()` (:44), label goldens (`:78` and the other `eventLabels` asserts)
  - `internal/agent/loop_tools_test.go`: `TestLengthGuard` (:417) becomes `TestTruncatedMessageToolCallsAreDroppedAndNeverRun`; `TestAbortParallelBatch`, `TestAbortSequentialBatch` (new lifecycle labels only; no attempt events, because the cancel comes before the stream exists)
  - `internal/agent/loop_run_test.go`: `TestTwoTurnEventOrder`, `TestSteeringPollPoints`, `TestFinishTurnContinue`, `TestFinishTurnEnd`, `TestErrorTail`, `TestContinue` (new lifecycle labels only; no assertion removed)
  - `internal/agent/loop_stream_test.go`: `TestStreamWithoutStartEmitsFinalOnce`, `TestStreamAbortDuringPacedStream`, `TestStreamTransformAndConvertOrder` (new lifecycle labels and shifted label windows only; no assertion removed)
  - `internal/agent/loop_stage_test.go`: `TestToolExecutorSelection` (:109) loses the truncated case
  - `pkg/protocol/codec_test.go`, `events_test.go`; `internal/providers/transform_test.go`

## Tasks & Steps

1. **Rename commit (no behavior change).** Rename only text that uses "turn" for the input cycle (comments in `internal/agent`, `pkg/protocol/README.md`). Go identifiers keep their names. If nothing needs renaming, skip this commit. Land it before any new type. <!-- red-team #9 (Scope 2) -->
2. `lifecycle.go`: `cycleState{id, reason CycleReason, turns int}`, `attemptState{id, cycleID string, number int}`. `CycleReason` values: `completed`, `blocked`, `max-tokens`, `aborted`, `error`, `continuation-limit`. IDs are random hex like `newRunID` (`emit.go:73-77`).
3. Driver: open a cycle when a run starts and when a follow-up starts a new input cycle. **Attempt boundary (follow DeepSeek `agent.ts:434-439,445-446`; source audit finding 6).** The attempt is live only after the stream function returned a stream (`loop_stream.go:35`) and `ctx.Err() == nil`; then publish `attempt_start`, before the first event is read. A failure before that point (today `l.apiKey()`, `loop_stream.go:25-29`; later `PrepareRequest` and the provider `Prepare`) and a cancel between the stream call and the check publish no attempt event; the driver cancels and drains the returned stream. A stream that fails on its first event is a started attempt and gets `attempt_end{failed}`. Close the attempt when the stream settles. Phases 07 and 08 keep this one boundary.
4. **Max-tokens (follow DeepSeek).** When the final assistant message has `StopLength`: remove every tool-call block before the message is committed and published as `message_end`; run no tool and write no tool result; the turn ends with reason `max-tokens`; the cycle stops unless steering is queued (in this phase: unless the existing steering poll returns messages). Remove `truncatedExecutor` and its selection branch. The cycle reason `max-tokens` is sticky for the cycle; a new cycle starts with no reason.
5. Replay: `transform.go` skips an assistant message that has no content blocks (a truncated message whose only content was a tool call), as DeepSeek does (`surface.ts:136-142`).
6. Wire events (additive): add `cycle_start`, `cycle_end`, `attempt_start`, `attempt_end` (constants, structs, `EventType`, codec and fast-codec paths); add optional `cycleId` to `turn_start`/`turn_end` (omitted when empty). Order: `agent_start`, `cycle_start`, `turn_start`, …, `attempt_start` before the assistant `message_start`, `attempt_end` after its `message_end`, `turn_end`, `cycle_end`, `agent_end`, `agent_settled`.
7. **Unopened scopes.** Emit `cycle_end` and `attempt_end` only for a scope that opened. `fail` keeps its Pi tail (`message_start`, `message_end`, `turn_end`, `agent_end`) unchanged, because the JSON stream stays Pi (`A:agent.ts:532-548` emits `turn_end` unconditionally); it adds `cycle_end{reason}` only when a cycle is open. <!-- red-team #13 -->
8. Update `pkg/protocol/README.md` with the event table and the vocabulary map.

## Tests

The phase owns every matrix row with Phase `01`; run `bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 01`.

New tests:
- `TestCycleReasonStaysMaxTokensAfterLaterCompletedTurn` (a steering message forces a second turn after truncation; reason stays `max-tokens`), `TestMaxTokensReasonDoesNotLeakIntoNextCycle`.
- `TestTruncatedMessageToolCallsAreDroppedAndNeverRun` (counting tool stays at 0, no tool result, no `tool_execution_start`, reason `max-tokens`, one request).
- `TestTruncatedToolOnlyMessageIsNotReplayed` (`internal/providers`).
- `TestOneInputCycleWithTwoTurnsHasOneCycleID`, `TestEachModelCallOpensOneAttempt`, `TestNoCycleOrAttemptEndForUnopenedScope`, `TestRunFailureTailKeepsPiTurnEnd`.
- Attempt boundary (matrix SA5): `TestFailureBeforeStreamReturnsEmitsNoAttemptEvents` (a `GetAPIKey` error: no `attempt_start`, no `attempt_end`, cycle `error`), `TestCancelAfterStreamReturnsBeforeFirstReadOpensNoAttempt` (the `Config.Stream` function calls `Abort` after it built the faux stream: no attempt events, cycle `aborted`, goleak finds no producer), `TestStreamThatFailsOnFirstEventIsSettledAttempt` (counterexample: `attempt_start` and `attempt_end{failed}` are both present).
- `TestLifecycleEventsRoundTripThroughCodec`, `TestLifecycleEventBytes`.
- Golden updates listed under "Modify tests". Each updated golden gets a one-line reason in the commit message.

```sh
go test ./pkg/protocol/... ./internal/agent/... ./internal/providers/... ./cmd/tui/...
go test -race ./internal/agent/... ./cmd/tui/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 01
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `01`).

## Risks & rollback

- JSON readers see new event types (Low x Med). Mitigation: additive only; Pi readers ignore unknown types; `turn_*` keeps its meaning; codec golden tests.
- Max-tokens behavior change: a truncated tool call no longer gets an error result and a re-issue turn (Med x Med). Mitigation: D25 decision; dedicated tests; the cycle stops with reason `max-tokens`, which clients can show.
- Rollback: reset to tag `lifecycle-p01-base` and re-land later phases (plan.md "Rollback").

## Done criteria

- Every started model call has exactly one attempt, and no attempt event exists for a call that failed or was cancelled before the stream returned; every turn belongs to one cycle; the reason appears in `cycle_end`.
- `truncatedExecutor` no longer exists; `grep -n truncatedExecutor internal/agent` returns nothing.
- Only the listed goldens changed; `go test ./...`, race, lint and the phase-01 matrix check are green.
