---
phase: 5
title: "Observation: contained listeners, output failure path and the follow path"
status: done
priority: P1
effort: 11h
dependencies: [phase-04]
---

# Phase 05: Observation, contained listeners, output failure path and the follow path

## Goal

A failing listener cannot stop the run. `Agent.Subscribe` stays synchronous, as DeepSeek's emit listeners run inline on the driver. Remote clients (dashboard, later H13 links) use a follow path modelled on DeepSeek `follow()`, with one Agent-owned snapshot and cursor cut. Headless JSON keeps Pi backpressure; the first output failure reaches the run owner; and after a signal, a write blocked on a full pipe cannot hold the exit. <!-- red-team #2, D25 -->

## Decisions (D25, source audit findings 7 and 8)

- **Subscribe contract (follow DeepSeek).** Listeners run in subscribe order on the driver, synchronously, as today (`internal/agent/emit.go:14-20`). A listener error or panic is contained: it is reported to `Config.ListenerError` (nil = dropped), the listener stays subscribed, the later listeners still run, and the run continues. DeepSeek: `packages/core/agent/src/dispatch.ts:120-137` (invokes each callback inline, contains a throw and a rejected promise, does not await a returned promise). Synchronous listener work delays the driver in both systems. There is no claim that observers never stall the driver (matrix W3). <!-- red-team #2 -->
- **Headless stdout (Q2, JSON exception).** The JSON writer stays one synchronous listener, so a slow reader stalls the run (Pi backpressure, `TestJSONSlowReaderStallsRun`). Because a listener error no longer ends the run, output failure needs its own path:
  - `protocolOut` gets a one-shot `onError func(error)`. It is called on the first sticky error from `Write` (`cmd/tui/output.go:59-62`) or from the timer flush (`output.go:66`, which today drops the error until the next `Write`), after `o.mu` is released. The JSON mode sets it to `ag.Abort`, so the first write failure stops the run. `runHeadless` still returns exit 1 through `out.failed` (`cmd/tui/headless.go:314`).
  - **A write blocked on a full pipe is not interrupted.** `Write` and `flush` hold `o.mu` while the target blocks (`output.go:53-55,73-74`). No portable deadline exists for an inherited blocking stdout, so the plan does not depend on one. Instead, after a signal, `runHeadless` waits at most `abortGrace` (`headless.go:46,338-344`) for the run to end (from phase 06: for `Dispose` to complete, started in a goroutine) and then returns the signal code. The exit path never takes `o.mu`: the final flush uses `o.mu.TryLock()` and skips the flush when the lock is held. Today `_ = out.flush()` (`headless.go:345`) takes the lock and blocks forever while the driver is stuck in `Write`; this phase removes that wait. The blocked goroutine ends when the process exits, or, in an in-process test, when the test closes the read end of the pipe.
  - When the reader keeps up, the terminal events (`message_end` aborted, `turn_end`, `cycle_end`, `agent_end`, `agent_settled`) are written in order before the grace ends. <!-- red-team #2 (Failure 3) -->
- **Follow path (follow DeepSeek `packages/api/session-controller/src/history.ts:179-232`).** DeepSeek reads the durable events and their cursor from one source observation, takes an assistant-stream baseline with an ordinal watermark, filters events at or below the cursor, and checks continuity. Ask separates commit from publication (phase 04), so the cut is an Agent-owned pair:
  - The driver keeps `published{epoch, seq, commitIndex, writer, stream}` under one small mutex `a.pubMu`. It updates the pair right after each event is published: `seq` is that event's seq, `commitIndex` is the writer length at that moment, and `stream` is the assistant-stream accumulator: a `protocol.Builder` (`pkg/protocol/builder.go:119-131`) that an assistant `message_start` opens, each `message_update` advances, and the matching `message_end` closes (then `stream` is nil). `seq` and `stream` change in the same critical section, so a delta is never counted in `seq` without being in the baseline (a gap) or in the baseline without being counted in `seq` (a duplicate). `Append` and listener calls are never made while `a.pubMu` is held.
  - `Agent.Follow(cursor)` takes `a.pubMu`, reads the pair, and registers the follower to receive events from `seq+1` (earlier events are dropped by seq). It releases the lock, then reads `writer.Entries()[:commitIndex]` as the snapshot. A message that is committed but not yet published is not in the snapshot; its `message_end` reaches the follower once. Continuity: the follower checks that each event seq is the previous seq plus one; a gap ends the follow with `resync`.
  - **Partial assistant output at the cut (follow DeepSeek `history.ts:185-192, :219-223`, matrix 71b; D25; it serves the D12 "Watch it think" dashboard).** When `stream` is open at the cut, the snapshot frame also carries an assistant-stream baseline `{attemptId, message, seq}`: `message` is a deep copy of the partial message so far (new `Builder.Snapshot()`), and `seq` is the watermark. DeepSeek has a separate frame ordinal for stream frames; in Ask every `message_update` is a published event with a seq, so the watermark is the cut seq itself. The follower then gets every event with seq above the watermark, including the later `message_update` events of the open message and its `message_end`, with no gap and no duplicate. A client that applies these updates to the baseline gets the committed message. When no assistant message is open at the cut, the frame has no baseline.
  - `Reset` changes `epoch` and `writer` under `a.pubMu`. A follower whose next event has a new epoch gets `resync`.
- **Bounds (diverge-deliberate BOUNDS, red-team #14; matrix B8, N7).** The replay ring is bounded by count and by bytes; followers are filtered by `sessionId`; a cursor with a foreign epoch, an evicted `seq` or a `seq` above the head gets `resync`; a follower that falls behind its buffer gets `resync`. **An event payload above the size cap is not stored truncated.** The ring stores a gap marker at that seq instead, and a follower that reaches it gets `resync` and takes a new snapshot. One cursor never maps to two different complete-looking payloads (DeepSeek follow delivers full entries, `history.ts:232`). Live delivery to local listeners is whole. The epoch is carried in the follow frame, not in `protocol.Envelope`, so the JSON lines do not change. <!-- red-team #14 -->

## Affected tests (re-checked after the Subscribe decision) <!-- red-team #2 -->

Because `Subscribe` stays synchronous, the 19 `.Subscribe(` sites in tests (16 in `internal/agent/agent_test.go`, 1 in `internal/app/module_auth_test.go:225`, 2 in `cmd/tui/headless_test.go`) keep their timing and do not change. Tests that change on purpose:
- `internal/agent/agent_test.go:193` `TestAgentListenerError` becomes `TestFailingListenerDoesNotStopRun` (the run completes, later listeners get every event, `ListenerError` receives the error once).
- `cmd/tui/headless_test.go:382` `TestEPIPEInProcess` and `:511` `TestEPIPE`: assertions unchanged (exit 1, empty stderr); they now also pass through `onError`.
- `TestAgentListenerOrder` (`agent_test.go:163`) is unchanged and still pins the in-order interleave.

## Implementation notes (recorded deviations)

- **Final flush on the signal path.** The phase text says `tryFlush` with `o.mu.TryLock()`. The code uses `flushAsync` instead: the flush runs in its own goroutine and the exit waits on it only until the same `abortGrace` deadline. Reason: `TryLock` skips the flush whenever a timer flush is in flight, so a reader that is slow but inside the grace could lose the last lines (`agent_settled`), and `TestJSONAbortWithSlowReaderStillWritesAgentSettled` could not hold. The exit path still never waits on the output lock, and `TestJSONSignalWithFullPipeExitsWithinGrace` covers a blocked write.
- **`onError` also stops the prompts that have not started.** The JSON mode calls `cancel(agent.ErrOutputFailure)` and then `ag.Abort()`, so a failed write does not start the next prompt. The cycle ends `aborted` with cause `output`.
- **Any failure of the JSON listener is an output failure.** A write error, and an event that cannot be encoded, becomes the sticky error of `protocolOut` and calls `onError`; the exit code is 1.
- **`Builder.Snapshot()` already existed.** The follow baseline uses the new `Builder.Partial()` and `Builder.ResumeFrom()`, which also carry the raw bytes of open blocks (tool-call arguments).
- **The replay ring starts with the first `Follow`.** A cursor from before that call is always refused with `resync`.

## Context links

- Current: `internal/agent/emit.go:15-64`, `Seq` per Agent (`emit.go:37`, `agent.go:29`)
- JSON mode: `cmd/tui/headless_json.go:14-16`; signal path `cmd/tui/headless.go:321-346`; output `cmd/tui/output.go:37-94`
- `internal/bus/README.md` (allowed imports: stdlib, `pkg/protocol`); `internal/bus` holds only `doc.go`, `README.md`
- Roadmap H12 replay contract (`roadmap.md`, H12 section)
- DeepSeek: `packages/api/session-controller/src/history.ts:123-232`, `packages/core/agent/src/dispatch.ts:120-137`

## Files to Create / Modify

- Create: `internal/bus/follow.go` (ring, follower, gap marker, resync), `internal/bus/follow_test.go`, `internal/bus/main_test.go` (`goleak.VerifyTestMain`) <!-- red-team #2 -->
- Modify: `pkg/protocol/builder.go` (`Snapshot()`: deep copy of the partial message), `internal/agent/emit.go` (containment, published pair with the stream accumulator), `agent.go` (`Reset` bumps epoch under `a.pubMu`; `Follow(cursor)`), `types.go` (`ListenerError func(error)`), `internal/agent/README.md`, `internal/bus/README.md`
- Modify: `cmd/tui/output.go` (`onError`, `tryFlush`), `cmd/tui/headless_json.go` (sets `onError` to `ag.Abort`), `cmd/tui/headless.go` (exit path uses `tryFlush` after the grace)
- Modify tests: `internal/agent/agent_test.go` (`TestAgentListenerError`), `cmd/tui/headless_test.go` (new tests below), `cmd/tui/output_test.go` if present (new cases)

## Tasks & Steps

1. Containment in `emit`: recover per listener, call `ListenerError`, continue with the next listener; `emit` returns no listener error to the loop. Remove the listener-error path that ends the run.
2. `protocolOut.onError`: set once; called outside `o.mu` with the first sticky error from `Write` or from the timer flush. JSON mode passes `ag.Abort`.
3. Exit path: after a signal, wait for the run up to `abortGrace`; then `tryFlush` (skips when `o.mu` is held) and return the signal code. The normal path keeps `out.failed`.
4. Published pair: maintain it in `emit` after each publish, under `a.pubMu`, as in "Decisions". Apply each assistant `message_start`, `message_update` and `message_end` to the stream accumulator in the same critical section that sets `seq`.
5. Follow path in `internal/bus`: `Ring` (count and byte bounds, payload cap with gap marker), `Follower` (bounded buffer that the internal listener fills without blocking; overflow ends the follower with `resync`), continuity check. One internal listener per Agent feeds the ring. `Agent.Follow(cursor)` implements the cut and adds the assistant-stream baseline when a message is open.
6. Session filter: the ring stores `sessionId` per event; a follower sees only its session.
7. Epoch: a random value set by `agent.New` and changed by `Reset`; a cursor is `(epoch, seq)`.
8. Counters: gaps, detached followers, contained listener failures.

## Tests

The phase owns every matrix row with Phase `05`; run `check-conformance-matrix.sh 05`.

- `TestFailingListenerDoesNotStopRun`, `TestPanickingListenerDoesNotStopLaterListeners`, `TestListenerSeesCommittedMessageAtMessageEnd`.
- Output: `TestJSONSlowReaderStallsRun` (kept), `TestJSONAbortWithSlowReaderStillWritesAgentSettled` (reader slow but inside the grace: last line is `agent_settled`), `TestJSONSignalWithFullPipeExitsWithinGrace` (the pipe is filled before SIGINT and the reader never reads: `runHeadless` returns 130 within `abortGrace` plus a margin while the driver is still blocked in `Write`; the test then closes the read end and goleak finds no goroutine), `TestJSONWriteFailureAbortsRun` (no further model call after EPIPE on a direct write), `TestJSONTimerFlushEPIPEAbortsRun` (the only failing write is the timer flush: the run is aborted, no further model call, exit 1). <!-- red-team #2 -->
- Follow: `TestFollowGivesSnapshotThenEventsWithoutGapOrDuplicate`, `TestFollowJoinBetweenCommitAndPublishGetsMessageOnce` (the `Config.NewContext` writer calls `Follow` right after it commits a message, before the driver publishes `message_end`: the snapshot lacks the message and the follower gets `message_end` once; counterexample: a snapshot read from the writer head would deliver it twice), `TestFollowJoinDuringStreamGetsBaselineThenDeltas` (a faux reply with five text deltas; the follower joins between delta 2 and delta 3: the baseline holds the text of deltas 1-2 and its watermark is the seq of delta 2; the follower gets deltas 3-5 and `message_end` once each; the baseline plus the deltas, applied with `protocol.Builder`, equals the committed message; counterexample: without the baseline, the follower shows only the text of deltas 3-5), `TestFollowJoinBetweenMessagesHasNoBaseline`, `TestFollowJoinDuringResetGetsResync`, `TestFollowerOverflowEndsWithResync`, `TestOldCursorGetsResync`, `TestFutureCursorGetsResync`, `TestResetForcesResyncForOldCursor`, `TestReplayNeverCrossesSessions`, `TestRingEvictsByBytes`, `TestOversizedEventGivesResyncNotTruncatedPayload`, `TestFollowRingHoldsNoCredentialCanary`. <!-- red-team #14 -->
- goleak: `internal/bus/main_test.go`; a detached follower leaves no goroutine.

```sh
go test ./internal/bus/... ./internal/agent/... ./cmd/tui/...
go test -race ./internal/bus/... ./internal/agent/... ./cmd/tui/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 05
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `05`).

## Risks & rollback

- A run that should stop on a broken pipe keeps spending tokens (Med x High). Mitigation: `onError` from `Write` and from the timer flush; two EPIPE tests.
- A stuck reader holds the process after a signal (Med x Med). Mitigation: bounded grace and `tryFlush`; `TestJSONSignalWithFullPipeExitsWithinGrace`.
- A follower gets a message or a delta twice, or misses it (Med x Med). Mitigation: the published pair and the stream accumulator change in one critical section; the join-between-commit-and-publish test and the join-during-stream test.
- Rollback: reset to tag `lifecycle-p05-base`.

## Done criteria

- A listener error or panic does not change the run result.
- Headless JSON keeps every event in order; EPIPE from a write or from the timer flush gives exit 1 and stops the run; a SIGINT run ends with `agent_settled` when the reader keeps up, and exits within the grace when the pipe is full.
- Follow tests cover the cut, the assistant-stream baseline and its watermark, Reset, overflow, resync, epoch, session filter, byte bounds and oversized events; green tests, race, lint, goleak and the phase-05 matrix check.
