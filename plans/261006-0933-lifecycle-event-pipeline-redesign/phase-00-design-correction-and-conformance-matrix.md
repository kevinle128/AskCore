---
phase: 0
title: "Correct the design report and build the conformance matrix"
status: pending
priority: P1
effort: 9h
dependencies: []
---

# Phase 00: Correct the design report and build the conformance matrix

## Goal

Make the design report accurate under D25 (follow DeepSeek by default) and D26-D29, so that every later phase implements a reviewed text. Finish the test-by-test conformance matrix and the per-phase check script. Documents and one plan-local script only; no Go change.

The report stays a proposal. This phase does not mark it accepted. Acceptance is a separate user decision (source audit, "Report precedence and plan readiness").

## Context links

- Design: `plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md` (status line at :4)
- Audit section 7 (corrections): `plans/reports/review-261006-0848-lifecycle-pipeline-report-audit.md:120-126`
- Source audit: `plans/reports/review-261006-1104-deepseek-lifecycle-source-audit.md` (high findings 1-8, correction table, "Report precedence")
- Red-team findings: [red-team-findings.md](red-team-findings.md)
- Roadmap decisions D4, D11, D18, D19, D20, D23, D24, D25, D26, D27, D28, D29: `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md:47,54,61,62,63,66,67,68,69,70,71,72`
- DeepSeek checkout: `/Users/dale/Desktop/workspace/opensources/deepseek-harness` at `5badb15009ae1756c3afe0ae0cef1faafc290ccc`

## Files to Create / Modify

- Modify: `plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md`
- Modify: `plans/261006-0933-lifecycle-event-pipeline-redesign/conformance-matrix.md` (complete it)
- Create: `plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh` <!-- red-team #12 -->

## Tasks & Steps

### A. Vocabulary and provenance (audit 7.1, D25)

1. Add the vocabulary map from `conformance-matrix.md` ("Vocabulary") to section 3 of the report. DeepSeek "turn" is Ask "cycle"; DeepSeek "step" is Ask "turn" (Pi meaning, unchanged on the wire); "attempt" is the same word. Replace the report's "Turn/Step" terms with "cycle/turn" everywhere. <!-- red-team #9 (Scope 2 naming) -->
2. Add a "Provenance" column with the values `DeepSeek`, `Pi exception (D-number)`, `Ask divergence (reason)`, `Ask new` to the tables in sections 7.3, 9, 11 and 13.
3. Label these rows as `Pi exception`: ordered decision handlers with `End > Continue > Proceed` (DeepSeek is data-driven, `packages/core/agent-loop/src/agent.ts:316-363`); tool definitions from the per-turn snapshot (user 2026-10-05; DeepSeek reads the live registry, `packages/core/agent-loop/src/tool-calls.ts:199-205`); all-results tool termination; the external JSON stream (Pi names, meaning and order); `Prompt` on an active Agent returns `ErrBusy` (BUSY, matrix row 74). <!-- D25 -->
4. Label these rows as `Ask divergence` with a one-line reason, because each changes what an upstream run can do (source audit, "Report precedence"): empty continuation bound 3 (CONT-BOUND); bounded follow ring, follower buffer and queue bound (BOUNDS); disposal closes admission (D29; the queues are cleared as in DeepSeek, so that part is not a divergence). <!-- red-team #14 -->
5. Label these rows as `Ask new` with a one-line reason (added capability, no change to an upstream outcome): one-shot `next` at every point (DeepSeek: only prepared model calls, `packages/llm/llm/src/index.ts:946-960`); session-unique tool-call IDs; D23 credential allowlist and error-text cleaner; the signal drain bound of the headless output (matrix W4). <!-- red-team #7, #15 -->
6. Label these rows as `DeepSeek` (no longer Ask new): listeners run inline on the driver with contained failures, and synchronous listener work delays the driver (`packages/core/agent/src/dispatch.ts:120-137`); pre-tool decision is allow/deny/cancel with frozen arguments (`packages/core/tools/src/index.ts:598-611`); tool default mode exclusive unless declared safe for the arguments (`packages/core/tools/src/index.ts:1295-1311`); retry numbers (`packages/llm/llm/src/retry-policy.ts:14-24`); unbounded drain of started tool bodies (D19, `tool-calls.ts:221-234`). <!-- D25 -->
7. Credit DeepSeek for the ordered coordinator, rolling pool and exclusive barrier (`tool-calls.ts:85-241`) and the checkpoint before model dispatch and tool body (`packages/session/session-checkpoint-policy/src/index.ts:63-83`).

### B. Four current-state errors (audit 7.2)

1. Section 2, row "Auth": per-request auth after the final request configuration is current behavior. Binding happens inside the stream function (`internal/app/module_auth.go:14-42`) and stays there (red-team #8). Only the billing pin on a retry is new. <!-- red-team #8 -->
2. Sections 4, 5.2, 9.3: external hooks and the process-stop path are a decided design (D4), not code. `internal/hooks` has only `doc.go` and `README.md`.
3. Line 165: replace "Do not add implicit hook deadlines" with "Compiled-in handlers get no deadline. Out-of-process hooks keep the D4 deadline (X1)."
4. Section 6: no queue exists today. Only two poll hooks exist, with no non-test implementation (`internal/agent/agent.go:15-18`, `internal/pipeline/hooks.go:162-167`). State which H9 items this redesign takes: queues, retry, `agent_settled` placement per D18.

### C. Edge cases (audit 7.3), sections 8, 12 and 15, rewritten under D25 <!-- D25 -->

1. Max-tokens (follow DeepSeek, `agent.ts:336-341,530`, `packages/llm/llm/src/assembler.ts:137-138`, `loop.spec.ts:1291-1430`): the tool-call blocks of a truncated message are removed before commit, no call runs, no tool result is written, the turn ends with reason `max-tokens`, and the cycle stops unless steering is queued. The cycle reason `max-tokens` is sticky for the cycle and does not leak into the next cycle. A truncated message with no content left is committed but is not sent to the model again (`packages/core/session/src/surface.ts:136-142`). The Pi truncation path (`internal/agent/loop_tools.go:60-78`) is removed. <!-- red-team #11 -->
2. Cancel mid-stream (follow DeepSeek, `cancel.spec.ts:571-605,664`): the interrupted message keeps the completed text and reasoning prefix and drops every tool-call block. The next request sends it to the model. An unsigned thinking block of an interrupted message is sent as plain text (provider signature rule).
3. Failure rule per control point. One table: `AdmitStep`, `PrepareRequest`, `ExecuteModel`, `RecoverModel`, `CompleteStep`, `StopTurn` errors and panics end the run through the D20 wrapper (cycle reason `error`); `BeforeTool`, `ExecuteTool`, `AfterTool` errors and panics become error tool results (D20). A `BeforeTool` or `ExecuteTool` error skips `AfterTool` (DeepSeek `tools/src/index.ts:1536-1537,1628-1629`); a deny or cancel decision runs it (`:1516-1530`). DeepSeek evidence for `StopTurn`: `contract-regressions.spec.ts:346`.
4. Steering after abort (follow DeepSeek, `agent.ts:154-160,214-233`): the aborted run claims nothing more. A `Steer` or `FollowUp` that arrives after the abort goes to the next cycle and wakes a new run after the aborted run ends. `Abort()` clears both queues by default; `Abort(KeepQueued)` keeps them. `Remove(inputID)` takes back a pending input (D27).
5. Input-commit timing (D26, follow DeepSeek): admitted input is committed after the first attempt's request preparation succeeds. A preparation failure or cancel commits nothing; D20 error events are published but the error message is not saved. State the one JSON-stream difference from Pi. <!-- red-team #4 -->
6. Attempt boundary (follow DeepSeek `agent.ts:434-439,445-446`): `attempt_start` is published only after the stream function returned a stream and the context is not cancelled. A prepared request (and its `RequestDelta`) can exist without a started attempt. A stream that fails on its first event is a started attempt.
7. Disposal (D29, `packages/core/agent-loop/src/index.ts:526-556`): `Dispose()` is separate from `Abort()`; it is memoized, closes admission (`ErrDisposed`), cancels with cause `disposed` (no wake latch; queues cleared, as DeepSeek `index.ts:544` cancels without `keepInbox` and `agent.ts:176-177` clears the inbox; user 2026-10-06, Q9), waits for the D19 drain, closes the writer, and publishes `agent_disposed` to Agent listeners; the headless JSON writer does not write it, so `agent_settled` stays the JSON terminal record (D18). Headless runs it in a goroutine for SIGINT and SIGTERM, bounded by `abortGrace`.

### D. Other required statements

1. Sections 7.3 and 10 (D23, D28): this redesign defines the in-memory entry types (including `SystemSnapshot`, `ToolCall`, retry entries and `AttemptSettled`), and H8 makes them durable. The request record has two levels. The logical request (messages, system prompt, tools, model reference) is logged from phase 04. The exact wire body is rebuilt from the provider `Prepare` result and the safe `AuthBinding` from phase 07. The values that are never logged are named: API key, OAuth access token, account ID, and the values of the `Authorization`, `x-api-key`, `Cookie` and account headers. <!-- red-team #7 -->
2. Section 14: close "Effective argument validation" as **forbidden rewrite** (follow DeepSeek: pre-tool decisions are allow/deny/cancel, arguments are frozen after validation, `tools/src/index.ts:598-611`). Close "Retry defaults" (DeepSeek numbers, no jitter per D11), "Event names" (Q1 resolved: Pi `turn_*` meaning kept, `cycle_*`/`attempt_*` added), "Pending input on Abort" (DeepSeek), "Empty continuation bound" (Ask divergence CONT-BOUND, 3). <!-- D25 -->
3. Section 9.1: pool default 10 (`constants.ts:6`); tool default mode exclusive unless declared safe for these arguments.
4. Section 9.3: `AfterTool` moves from tool goroutines (`loop_tools.go:316-334`) to the coordinator loop, in source order. Workers never block on the driver: progress goes through a per-call buffer with latest-wins coalescing. The coordinator runs `BeforeTool` and `AfterTool` itself and cannot stop them: a cancel during either takes effect when the handler returns, and the drain of started bodies starts after that. <!-- red-team #15 -->
5. Section 9.4: three separate facts. (a) The assistant request: the tool-call block in the committed assistant message. (b) The recorded call intent: a `ToolCall` entry written before `BeforeTool` (DeepSeek `tool-calls.ts:168-170`). (c) The body invocation: set just before the body starts (`tools/src/index.ts:1580`). Normal abort classifies by (c); repair classifies by (b) (`repair.ts:131,166`).
6. Section 11: `Agent.Subscribe` stays synchronous (DeepSeek emit, `dispatch.ts:120-137`); a listener failure is contained and the run continues; a slow synchronous listener delays the driver (no claim that observers never stall). Remote clients use a follow path (DeepSeek `packages/api/session-controller/src/history.ts:114-275`). The follow cut is one Agent-owned pair (last published seq, commit index at that publication); a follower that joins during a model stream gets an assistant-stream baseline (the partial message so far) whose watermark is the cut seq, as DeepSeek `history.ts:185-192, :219-223` (matrix row 71b, `follow`). <!-- red-team #2 -->
7. Header "Status" line: "proposal, corrected per the audit, D25-D29 and the source audit; not accepted; acceptance is a separate user decision; implementation plan: `plans/261006-0933-lifecycle-event-pipeline-redesign/`". Do not write "accepted design".

### E. Conformance matrix and check script <!-- red-team #12 -->

1. Keep the seed rows 1-77, A1-A9, B1-B11 and the rows the source-audit revision added or split (19b, 20b, 25b, 49b, 56b-56d, 71b, 77b, SA25b, SL10b, SL14b, SS3b, ST5b-ST5d, ST11b, ST21b, ST21c); keep the statuses already written in the matrix.
2. Title sweep: one row per behavior for every title of these DeepSeek spec files (group titles that prove one behavior; group N/A titles by reason):
   `cd /Users/dale/Desktop/workspace/opensources/deepseek-harness && grep -nE "^\s*(it|test)(\.each\([^)]*\))?\(" packages/core/agent-loop/tests/*.spec.ts packages/core/agent/tests/*.spec.ts packages/core/session/tests/*.spec.ts packages/core/tools/tests/*.spec.ts packages/llm/llm-retry/tests/*.spec.ts packages/llm/token-meter/tests/*.spec.ts packages/llm/llm/tests/*.spec.ts`
3. Columns: `# | DeepSeek behavior | DeepSeek test path:line | Ask status | Reason | Ask Go test | Ask assertion | Phase`. The Phase column is the single source of truth. Phase files name rows by number only.
4. Every non-N/A row's assertion names the counterexample it excludes when a weaker test could pass (source audit, "Verification record"). Example: `ErrBusy` does not prove session append protection (row SS3b), and a faux request does not prove adapter-default materialization (row 25b).
5. Write `check-conformance-matrix.sh [phase|--dry-run]`: `--dry-run` only parses the matrix and lists the rows per phase. It also fails when the set of non-N/A rows of a phase differs from the IDs in that phase's "Assertion review" line, when a row has a status outside `follow`, `diverge-deliberate`, `Ask-new`, `N/A`, or when a non-follow row has no reason. Otherwise: parse the matrix; for rows of the given phase (or all rows), split the "Ask Go test" cell on `;`, strip every parenthetical note and the `NEW:` prefix, skip `-` and `see row …` cells; run `go test -list` per package and fail on a missing name; then run those tests. It prints the rows whose "Ask assertion" checkbox is not ticked in the hand-review list and fails when any is open.

## Tests

No Go change. Check that the matrix has no empty cells and the links resolve:

```sh
grep -nE '\|\s*\|' plans/261006-0933-lifecycle-event-pipeline-redesign/conformance-matrix.md   # expect no hits
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh --dry-run    # parses, lists rows per phase, checks the review list
grep -n 'accepted design' plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md        # expect only "not an accepted design"
go test ./... && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...           # unchanged baseline
```

## DeepSeek conformance rows covered

All rows: this phase writes them. Final statuses come from the matrix.

## Risks & rollback

- Risk: the sweep is large (about 650 titles). Mitigation: rows group titles by behavior; N/A titles grouped by reason.
- Risk: a correction silently changes a user decision. Mitigation: every change cites D25-D29, a roadmap decision or a verified DeepSeek line; a contradiction with a D-row goes to plan.md "Unresolved questions".
- Risk: the report reads as accepted. Mitigation: step D.7 and the grep above.
- Rollback: reset to tag `lifecycle-p00-base` (see plan.md "Rollback").

## Done criteria

- [x] The report uses the cycle/turn/attempt vocabulary, has the provenance column, the corrections, the edge cases rewritten under D25-D29, the failure table, the three tool-call facts and the attempt boundary.
- [x] Line 165 no longer conflicts with D4.
  The status line says the report is a proposal and is not accepted.
- [x] Every matrix row has a status, a reason for every non-follow row, an Ask test and an Ask assertion for every non-N/A row, and a phase; every phase's review line matches its non-N/A rows.
- [x] `check-conformance-matrix.sh --dry-run` passes.
