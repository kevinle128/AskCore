---
phase: 4
title: "Session entry writer and D23 logical request log"
status: done
priority: P1
effort: 10h
dependencies: [phase-03]
---

# Phase 04: Session entry writer and D23 logical request log

## Goal

Make every history commit an explicit driver step that writes typed entries, separate from event publication. Log what each attempt sent so that the logical request can be rebuilt (D23), with no credential in any entry. Headless keeps working with no database.

**Reconstruction boundary (D28, source audit finding 4).** This phase logs and rebuilds the logical request: messages, system prompt, tools, the model reference and the options that the Agent sets. It does not claim equality with the wire body, because adapters still compute effective values (for example `clampMaxTokens`, `internal/providers/anthropic/document.go:130`). Phase 07 adds the provider `Prepare` step, extends `RequestDelta` with the prepared effective values and the safe `AuthBinding`, and owns the exact-body tests (matrix 25b, SB9).

## Context links

- Design sections 10, 11.1; D23 (`roadmap.md:63`)
- Coupling to remove: `internal/agent/emit.go:36-45` (append on `MessageEnd`, then listeners). Today the only commit path for prompts and steering is that side effect: `loop_run.go:60-63`, `loop_stage.go:97-102`. <!-- red-team #3 -->
- Current log: `internal/sessions/memory.go:10-31` (`Messages()` shares message content with the log, :26-27); interface `internal/agent/context_source.go:11-18`
- Credential carriers: `internal/providers/model.go:58-63` (`BaseURL`, `Headers`, `SamplingParams`), `internal/providers/types.go:33-48` (`StreamOptions.APIKey`, `Auth`), `internal/providers/auth.go:10-20` (`AuthSnapshot.AccessToken`, `AccountID`)
- Raw error text: `internal/agent/agent.go:268` (`cause.Error()`), `internal/providers/fantasykit/errors.go:48-83` (`HTTPMessage`, 512-byte body, `err.Error()` fallback)
- DeepSeek: `packages/core/session/src/known-event-types.ts:22-82`, `docs/architecture.md:113,127`, `request-freeze.spec.ts:46`, `request-reconstruction.spec.ts:773`, deep freeze `packages/core/session/src/index.ts:170-185`

## Placement decision <!-- red-team #7 (Scope 5) -->

- Entry types live in `internal/sessions` and use only `pkg/protocol` types and plain structs defined in `sessions`. The `sessions` import rule (`store`, `pkg/protocol`) does not change.
- The allowlist conversion from `providers.Model`/`StreamOptions` to safe entry fields and the rebuild function live in `internal/agent` (`request_log.go`), which already imports `providers`. `rebuildRequest` is unexported until H12 needs it; tests in package `agent` call it.

## Files to Create / Modify

- Create: `internal/sessions/entry.go` (entry types), `internal/sessions/writer.go` (`Writer` interface, `CommitRef`), tests `entry_test.go`, `writer_test.go`
- Create: `internal/agent/request_log.go` (allowlist conversion, request freeze, `RequestDelta` and `SystemSnapshot` computation, `rebuildRequest`), `internal/agent/request_log_test.go`
- Create: `internal/providers/diagnostic.go` (`CleanDiagnostic`), `diagnostic_test.go` <!-- red-team #7 -->
- Modify: `internal/sessions/memory.go` (implements `Writer`; `Messages()` returns deep copies), `internal/sessions/README.md` (file list gains `writer.go`; `MemoryLog` satisfies `sessions.Writer`)
- Modify: `internal/agent/emit.go`, `context_source.go` (ContextSource becomes `sessions.Writer`), `loop_run.go`, `loop_stage.go`, `loop_stream.go`, `loop_tools.go`, `agent.go` (`fail` uses `CleanDiagnostic`), `types.go` (`NewContext func() sessions.Writer`)
- `agent.Run`/`agent.Continue` become unexported (phase 02 API decision). Move their test callers to `agent.New` + `Prompt`: `agent_model_test.go` (:298), `loop_run_test.go`, `loop_helpers_test.go`, `loop_tools_test.go`, `loop_stream_test.go`. <!-- red-team #13 -->
- Modify tests: `internal/agent/agent_test.go:322,396` (`NewContext` type) <!-- red-team #7 (Scope 5) -->

## Tasks & Steps

1. Entry types: `MessageEntry`, `CycleOpened`/`CycleClosed{reason}`, `TurnOpened`/`TurnClosed`, `AttemptSettled{attemptId, outcome, usage (nil = unknown), failure{code, text}}`, `RequestDelta{attemptId, added, removed, changed messages, model ModelRef, options RequestOptions}`, `SystemSnapshot{systemPrompt, tools}`, `InputOutcome{inputId, accepted|rejected, reason}`, `ToolCall{assistantEntry, callId}` and `ToolOutcome` (phase 09), `RetryScheduled`/`RetryStarted` (phase 08), `Prepared` fields of `RequestDelta` (phase 07). `RequestDelta` is written at freeze, before the stream call; `AttemptSettled` is written only for a started attempt (phase 01 boundary). `ModelRef` = `{Provider, API, ID, BaseURLHost, HeaderNames []string}`. `RequestOptions` = `{MaxTokens, Temperature, Reasoning, ToolChoice, CacheRetention, SessionID}`. No field can hold a credential value. <!-- red-team #7 -->
2. `Writer.Append(entries ...Entry) (CommitRef, error)`: one call is atomic in memory. `Messages()` returns message entries only, as deep copies. `Entries()` returns all. **Sole writer:** the Agent driver is the only caller of `Append`. The Agent API has no append call, `State()` returns copies, and listeners run after `Append` returns (matrix SS3b, tested in phase 05).
3. **Explicit commits with today's timing** (phase 08 later moves the input commit after request preparation, D26). The driver commits prompts and pending messages at the point where `emit` appended them today (run start, `loop_run.go:60-63`; steer stage, `loop_stage.go:97-102`), then assistant messages and tool results, then the D20 wrapper message in `fail`. Each is published after `Append` returns. `emit` no longer appends. <!-- red-team #3 -->
4. **Allowlist (D23).** `RequestDelta.model` and `.options` are built field by field from the allowlist in step 1. `BaseURL` keeps only scheme and host (no path query, no userinfo). Header values, `SamplingParams`, `APIKey`, `Auth`, `AccountID` are never copied.
5. **Freeze at dispatch.** After `PrepareRequest`, the driver deep-copies the final request; the stream call and the `RequestDelta` both use that frozen copy. Handlers get copies of history messages, so an in-place edit cannot change committed history. <!-- red-team #7 -->
6. **SystemSnapshot.** Write `SystemSnapshot` before the first attempt of a session and whenever the system prompt or the tool declarations differ from the last snapshot. `RequestDelta` is computed against the baseline "last `SystemSnapshot` + selected history projection".
7. **Error-text cleaner.** `CleanDiagnostic(err) string` removes URL query strings and userinfo, replaces bearer and key patterns (`Bearer …`, `sk-…`, `x-api-key: …`), and caps the text at 512 bytes. Every committed or published error text goes through it: `fail` (`agent.go:268`), `AttemptSettled.failure.text`, and in phase 08 the retry entries and `auto_retry_*` events. <!-- red-team #7 -->
8. **Canonical tool order (follow DeepSeek `tool-order.spec.ts:70,82`).** The tool declarations of the request and of `SystemSnapshot` are sorted by name, so the registration order never changes the request. This changes request bodies with more than one tool: re-record the affected cassettes with `ASK_RECORD=1` (`docs/testing-llm-cassettes.md`) in this phase and name the reason in the commit.
9. `rebuildRequest(entries, attemptId)` returns `{messages, systemPrompt, tools, ModelRef, RequestOptions}`. In this phase it equals the logical request that the faux provider received. Phase 07 extends it to the exact body.
10. **Write failure.** On `Append` error: cancel the run, start no new side effect, return the error through the run result. Do not claim that the terminal error is saved.

## Tests

The phase owns every matrix row with Phase `04`; run `check-conformance-matrix.sh 04`.

- `TestPromptIsCommittedBeforeFirstRequest`, `TestSteeringMessageIsCommittedBeforeNextRequest`. <!-- red-team #3 -->
- `TestRebuiltLogicalRequestMatchesFauxRequest` (faux records the request; compare messages, system prompt, tools and model reference with `rebuildRequest`; no claim about the adapter body).
- `TestAssistantFramesLieInsideTheirAttempt` (matrix 70: every assistant `message_*` event lies inside `attempt_start`/`attempt_end` of one `attemptId`, and `AttemptSettled` is in `Entries()` before `attempt_end` is published).
- `TestRebuildAfterSystemPromptChangeUsesLoggedPrompt`, `TestHandlerMutationDoesNotChangeCommittedHistory`, `TestPrepareRequestEditsAreLoggedNotStoredAsMessages`. <!-- red-team #7 -->
- `TestCredentialCanariesNeverReachEntriesEventsOrRebuild`: unique canary values in `AuthSnapshot.AccessToken`, `AccountID`, `Options.APIKey`, `BoundKey.Secret`, one `Model.Headers` value and one `BaseURL` query value; run one success and one auth failure; assert with `bytes.Contains` that no canary is in `Entries()`, in the JSON events, or in the rebuild output. Phases 05 and 08 extend it to the follow ring and to retries. <!-- red-team #7 -->
- `TestCleanDiagnosticRemovesQueryAndKeys` (`internal/providers`), `TestRunFailureMessageIsCleaned`.
- `TestCommitHappensBeforeMessageEndIsPublished`, `TestWriteFailureStopsFurtherSideEffects`, `TestWriterRejectsEntryThatCannotBeEncoded`.
- `TestRequestToolsAreInNameOrderNotRegistrationOrder`.
- Cassette tests in `cmd/tui` pass; only the cassettes whose tool order changed are re-recorded, and no other request body changes.

```sh
go test ./internal/sessions/... ./internal/agent/... ./internal/providers/...
go test -run 'Cassette|Captured' ./cmd/tui
go test -race ./internal/sessions/... ./internal/agent/... ./cmd/tui/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 04
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `04`).

## Risks & rollback

- Request bytes change because of the refactor (Med x High). Mitigation: cassette replay compares bodies; the only allowed change is the tool order.
- Cassette re-record needs a live key (Med x Med). Mitigation: re-record before the phase starts its other steps; without a key the phase stops and reports it.
- Credential leak into entries (Low x High). Mitigation: allowlist types that cannot hold a value, canary test.
- The prompt commit moves by accident (Med x High). Mitigation: step 3 keeps today's points; `TestPromptIsCommittedBeforeFirstRequest`.
- Rollback: reset to tag `lifecycle-p04-base`.

## Done criteria

- `emit` has no history side effect; every message commit is an explicit driver call.
- Every prepared attempt has a `RequestDelta`; the first attempt of a session has a `SystemSnapshot`; `rebuildRequest` reproduces the logical request in tests (exact body: phase 07).
- No canary in any entry, event or rebuild output; cassettes changed only for tool order; green tests, race, lint and the phase-04 matrix check.

## Implementation notes (review fixes)

- **Early save failure (step 10).** When the `SystemSnapshot` write fails at run start, no turn is open. The Agent still publishes the failure tail with `turn_end` (as Pi does) but commits no `TurnClosed`, and no cycle or attempt entry exists. `TurnClosed` is committed only while a turn is open, tracked on the run from `turn_start` and `turn_end`, so a listener failure at `turn_end` does not close the turn twice.
- **Provider error text.** The error text of the final assistant message goes through `CleanDiagnostic` once, before it is committed and published. `AttemptSettled.failure.text` uses the same text.
- **Freeze at dispatch.** The `ExecuteModel` terminal always sends the frozen request that `RequestDelta` recorded. Handlers get a copy and may wrap the call or return their own stream; their edits do not reach the provider. This follows the frozen tool arguments.
- **Test shim (deviation from "move the test callers").** `internal/agent/export_test.go` keeps `Run` and `Continue` for the loop-level tests, the standard Go export-for-test pattern. The loop under test is the real `runLoop`. The shim opens its log through `openLog`, the helper that the Agent uses, with the snapshot of the (empty) header it passes, so the loop runs on a log state that the product produces. Moving about 45 callers to `Agent.Prompt` would add the failure tail, a system message and `agent_settled` to what they assert; the phase tests use `agent.New` + `Prompt`.
- **Last snapshot.** `Writer.LastSystemSnapshot()` replaces a scan of `Entries()` at run start, so a run start copies one snapshot, not the whole log.
- **Tool order.** `providers.CurrentTools` returns tools in name order (lead decision, DeepSeek `tool-order.spec.ts:70`), so a tool added inside a run also sorts by name. `TestCurrentToolsReplaysSystemMessages` changed on purpose for this.
- **Usage.** `AttemptSettled.Usage` is nil when the provider reported no usage (the zero value), not a count of zero.
