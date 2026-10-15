---
title: "Lifecycle and Event Pipeline redesign (D24, D25)"
description: "Replace pipeline.Hooks with typed control dispatch, add cycle/turn/attempt lifecycle, entry writer, contained listeners, queues with removal and disposal, typed provider failures with a Prepare step, retry and an ordered tool coordinator, following DeepSeek 5badb15 test by test."
status: completed
priority: P1
effort: 123h
branch: master-2
tags: [agent, pipeline, lifecycle, events, deepseek-conformance, d19, d23, d24, d25, d26, d27, d28, d29]
blockedBy: []
blocks: [H8, H9, H11, H12]
created: 2026-10-06
---

# Lifecycle and Event Pipeline redesign

One redesign phase before H8 (D24, user, 2026-10-06). It implements
[the design](../reports/architecture-261006-ask-lifecycle-event-pipeline.md) as corrected by
[the audit](../reports/review-261006-0848-lifecycle-pipeline-report-audit.md), by D25-D29,
by [the red-team review](red-team-findings.md) and by
[the source audit](../reports/review-261006-1104-deepseek-lifecycle-source-audit.md).
The design report is a proposal; this plan does not mark it accepted (acceptance is a separate user decision).
Every behavior is compared with DeepSeek `5badb15` in [conformance-matrix.md](conformance-matrix.md).
Each divergence is deliberate, recorded in the matrix with its reason, and has its own Ask test.

## Fixed constraints (user decisions, do not change)

- **D25 (user, 2026-10-06): follow DeepSeek by default.** When Ask and DeepSeek differ, Ask follows DeepSeek `5badb15`. The exceptions below keep Ask/Pi behavior; each one is a `diverge-deliberate` row in the matrix with its reason and its own test:
  - D4: out-of-process hook deadlines (X1). Compiled-in handlers get no deadline.
  - D11: no retry jitter.
  - D18: `agent_settled` is the last record, after retries and queued work.
  - D20: errors from credentials, request preparation and the stream reach the Agent wrapper, which builds the error assistant message.
  - Tool registry snapshot per turn (user, 2026-10-05, `plans/261005-2059-tool-registry-snapshot/plan.md:20`).
  - `End > Continue > Proceed` for completion decisions.
  - A tool batch ends the cycle only when all results ask for it.
  - No proactive mid-turn compaction (H10, out of scope).
  - The external JSON event stream of `ask -p --mode json` stays Pi-compatible: event names, meaning and order seen by JSON readers. New events are additive only.
- D26 (user, 2026-10-06): admitted input is committed after request preparation succeeds, as DeepSeek. A preparation failure or cancel commits nothing; D20 error events are still published but the error message is not saved. The JSON stream differs from Pi on this path only.
- D19 reaffirmed (user, 2026-10-06): started tool bodies drain with no time bound; tools must honor `ctx` cancel.
- D27 (user, 2026-10-06): `Agent.Remove(inputID)` (phase 06) and non-waking added context from `AfterTool` (phase 09), as DeepSeek.
- D28 (user, 2026-10-06): the D23 request record is taken from a provider `Prepare` step that computes safe effective values once. Phase 04 logs the logical request; phase 07 adds `Prepare`, logs its result and owns the exact-body tests.
- D29 (user, 2026-10-06): `Dispose()` separate from abort, as DeepSeek; the queues are cleared (Q9 resolved by the user); headless uses it for SIGINT and SIGTERM (phase 06).
- D25 overrides D1 inside the lifecycle redesign (user, 2026-10-06). Where Pi and DeepSeek differ in lifecycle behavior, DeepSeek wins; the Pi `all` / `one-at-a-time` queue modes are removed. D1 still holds outside the lifecycle.
- D19 revised: DeepSeek tool-outcome model. Abort records an outcome for every call; repair writes not-started or outcome-unknown for the open tail only; no tool runs again.
- D23: log what each attempt sent, so that the request can be rebuilt; no credentials in the log.
- D24: one redesign before H8, compared test by test with DeepSeek.
- No adapter for the old `pipeline.Hooks` API. All in-repo callers change together.
- No new dependencies. Standard library concurrency only.
- No plan IDs, phase numbers or decision codes in Go comments, test names or migration names.

**Recorded divergences that are not D25 exceptions** (each is a matrix row with a reason; source audit "Report precedence"): BUSY (`Prompt` on an active Agent returns `ErrBusy`, row 74; user instruction 2026-10-06), CONT-BOUND (at most 3 empty continuations, row 59; classified as changed behavior by the user on 2026-10-06), BOUNDS (bounded follow ring, follower buffer and queue, rows B8 and N6; red-team #14), PROVIDER (a provider rejects the DeepSeek form, rows 5b and SS11), and the planner design rule of row ST21b (schema check at `Register`). Row 71b (assistant-stream baseline on follow) now follows DeepSeek (D25, 2026-10-06).

**Roadmap text that D25 makes stale** (updated in phase 11, not here): H2 H-LOOP-09 whole-batch serial rule and the test "hook mutates the arguments"; H9 "mid-stream public output cannot be transparently replayed" (D25 follows DeepSeek: a retry after visible output happens, and it is visible on the wire). <!-- D25 -->

## Vocabulary

DeepSeek "turn" (one input cycle) is Ask **cycle**; DeepSeek "step" (one model answer and its tools) is Ask **turn**, which keeps Pi's wire meaning; **attempt** is one model request. Go names match wire names (`cycle_*`, `turn_*`, `attempt_*`). An attempt is live, and `attempt_start` is published, only after the stream function returned a stream and the context is not cancelled (phase 01). <!-- red-team #9 -->

## Phases

| # | Phase | Effort | Depends on | Status |
|---|---|---|---|---|
| 00 | [Correct the design report, build the conformance matrix and check script](phase-00-design-correction-and-conformance-matrix.md) | 9h | none | pending |
| 01 | [Lifecycle vocabulary, IDs, wire events, attempt boundary and max-tokens stop](phase-01-lifecycle-vocabulary-and-wire-events.md) | 10h | 00 | pending |
| 02 | [Typed control dispatch: port only](phase-02-typed-control-dispatch-port.md) | 8h | 01 | pending |
| 03 | [Typed control dispatch: new semantics](phase-03-typed-control-dispatch-semantics.md) | 6h | 02 | pending |
| 04 | [Session entry writer and D23 logical request log](phase-04-session-entry-writer.md) | 10h | 03 | pending |
| 05 | [Observation: contained listeners, output failure path and the follow path](phase-05-observation-and-follow-path.md) | 11h | 04 | pending |
| 06 | [Input admission, queues, removal and disposal](phase-06-input-admission-and-queues.md) | 13h | 05 | pending |
| 07 | [Typed provider failures and the provider Prepare step](phase-07-typed-provider-failures.md) | 13h | 06 | pending |
| 08 | [Model attempt, retry, billing pin and cancel mid-stream](phase-08-model-attempt-and-retry.md) | 14h | 07 | pending |
| 09 | [Ordered tool coordinator, abort outcomes and repair](phase-09-tool-coordinator-and-repair.md) | 14h | 08 | pending |
| 10 | [End-to-end fixtures and final conformance sweep](phase-10-e2e-fixtures-and-conformance-sweep.md) | 10h | 09 | pending |
| 11 | [Roadmap and documentation update](phase-11-roadmap-and-docs.md) | 5h | 10 | pending |

Current progress and verification evidence are in the [progress report](../reports/pm-261006-1753-lifecycle-progress.md).
Phases 00–11 are complete (12/12, 100%).
All 197 testable matrix assertions are reviewed, the full matrix gate passed, and the independent documentation review approved the final documentation gate.
Phase 08 evidence: [approved source review](../reports/review-261006-1806-phase08.md) and [independent test report](../reports/tester-261006-1806-phase08.md).
Phase 09 evidence: [approved source review](../reports/review-261006-phase09.md) and [independent test report](../reports/tester-261006-phase09.md).
Phase 10 evidence: [approved conformance review](../reports/review-261006-phase10.md) and [independent test report](../reports/tester-261006-phase10.md).
Phase 11 evidence: [documentation delivery](../reports/docs-261006-phase11.md) and [approved documentation review](../reports/review-261006-phase11.md).
Completion journal: [local delivery record](../journals/2026-10-06-complete-the-lifecycle-and-event-pipeline-redesign.md).
Use `ak plan status plans/261006-0933-lifecycle-event-pipeline-redesign` for the status computed from the phase files.
The installed CLI does not update the Status cells in the table above.

Total: 123h (was 98h; +25h from the source audit, D27-D29 and the row 71b follow decision: phase 00 +1, 01 +1, 02 +1, 05 +4 (assistant-stream baseline +1), 06 +5 (D27 removal +2, D29 disposal +3), 07 +5 (D28 `Prepare` +4, failure fallbacks +1), 08 +3, 09 +3, 10 +1, 11 +1). Q6 is resolved (H7a is complete). Q8 is resolved by D26; phase 08 moves the input commit after request preparation.

## Dependencies and sequencing

- **Order is serial.** Every implementation phase edits `internal/agent`. No two phases run in parallel; each phase is one commit series that a reviewer reads alone.
- **Every phase ends green.** The ordering removes the red-team gaps: prompt and steering commits move with the entry writer (phase 04, today's timing) <!-- red-team #3 -->; credential binding stays in `AuthRunner`, so no phase waits on a driver-side binder, and the D26 commit move sits in phase 08 after `Prepare` (phase 07) <!-- red-team #4 -->; typed adapter failures (phase 07) land before retry (phase 08) <!-- red-team #1 -->; the max-tokens stop lands with the sticky reason (phase 01) <!-- red-team #11 -->.
- **Assertions follow their prerequisites** (source audit, "Required next revision" 5). The attempt boundary is set in phase 01 and kept by phase 08. The exact request-body equality needs `Prepare`, so it is in phase 07, not phase 04. Rows that need the queue (SA10, SS3b) are in phase 06; rows that need retry (SB4, SB8, SB11, SL14b, SL15) are in phase 08; rows that need the coordinator (19, 53, SA26) are in phase 09. The matrix Phase column decides; `check-conformance-matrix.sh` checks that every phase's review list equals its non-N/A rows.
- **Auth work: satisfied.** `e991b5c feat(auth): add native subscription login and durable refresh` is committed. Before phase 01, check that `git status` shows no Go changes.
- **Sequencing:** H7a is complete (commit `e991b5c`), so no sequencing blocker remains.

## Test strategy

Rule from `tasks/lessons.md`: tests drive the real code path. Never build a test-only harness, and never override production values in tests. Time-based behavior uses product seams (`Config.Wait`, `Config.Clock`), never changed constants. Each assertion must exclude its counterexample (source audit, "Verification record"): a test that a weaker implementation also passes does not prove the row. <!-- red-team #13 -->

| Layer | What it proves | Entry point | Real / injected |
|---|---|---|---|
| Contract tests per control point | Default, short-circuit, `next` at most once, ordering, failure rule, modes | `pipeline` package API | Real |
| Agent driver tests | Lifecycle order, attempt boundary, reasons, queues, removal, disposal, retry, coordinator | `agent.New` + `Prompt/Steer/FollowUp/Remove/Abort/Dispose` | Real agent; faux provider (product code) |
| Adapter failure tests | Real HTTP 429/5xx/quota bodies map to typed failures with `Retry-After`; unmapped statuses give `HTTP_<status>` | adapter `Stream` with injected transport | Real adapter; HTTP transport injected |
| Request-body tests (D28) | The body rebuilt from the log equals the body the real adapter sends, with defaults, after a model switch, and for an OAuth binding | `newHeadlessAgent` + `httptest` | Real adapter; HTTP transport injected |
| Race tests | Single owner; no lost wakeup; follow cut | `go test -race ./internal/... ./cmd/tui ./pkg/...`; `-count=200` for the wake race | Real |
| Goroutine-leak tests | Cancel, backoff, tool drain, follower detach, dispose | `goleak.VerifyTestMain` in `internal/agent`, `cmd/tui`, new `internal/bus` | Real |
| Headless scenarios | Events and exit codes of `ask -p` and `--mode json`; full pipe and timer-flush EPIPE | `newHeadlessAgent` + `runHeadless` | Real; faux script |
| Fault tests | 429/500, reset, clean EOF, retry, cancel mid-stream | `newHeadlessAgent` + `runHeadless` | Real; transport and retry wait injected |
| Cassette tests | Request bodies change only where a phase says so | `newHeadlessAgent` + `runHeadless` + `useCassette` | Real; recorded HTTP |
| Credential canaries | No secret in entries, events, follow ring, prepared request, rebuild, retries | `agent.New` and headless | Real; canary values in product inputs |
| End-to-end fixture | Retry, concurrent tools, steering, follow-up cycle, slow listener | headless JSON mode | Real; transport, extra tools, wait injected |
| Failure fixture | Write failure plus uncertain tool outcome; no tool runs again | `agent.New` with failing `Config.NewContext` writer | Real agent; writer fault through a product seam |
| Signal fixture | SIGINT during a tool batch disposes the Agent and exits 130 | headless JSON mode | Real; extra tool injected |
| Conformance check | Every matrix row of the phase names a test that exists, passes, and is hand-reviewed; review list equals non-N/A rows | `check-conformance-matrix.sh NN` at the end of every phase | Real |

Every phase ends with: `go test ./...`, `go test -race ./internal/... ./cmd/tui ./pkg/...`, `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`, `bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh NN`. <!-- red-team #12 -->

## Risks (summary; detail in phase files)

| Risk | L x I | Mitigation |
|---|---|---|
| JSON readers break | Low x High | Pi meaning of every existing event kept; new events additive; each golden change listed per phase |
| Retry spends more requests | Med x Med | DeepSeek retryable set only; `QUOTA`/`AUTH`/`UNKNOWN`/`HTTP_<status>` and over-cap `Retry-After` never retry; billing pin |
| A broken stdout keeps the run alive | Med x High | `onError` from `Write` and from the timer flush aborts the run (phase 05) |
| A full pipe holds the process after a signal | Med x Med | bounded grace; the exit path never takes the output lock (phase 05) |
| Big-bang `pipeline` rewrite hides regressions | Med x High | Port-only phase 02 before new semantics in phase 03 |
| D19 change leaves history without pairs | Low x High | Pairing test after every abort path; session-unique call IDs; `ToolCall` record before hooks |
| A tool or control handler ignores `ctx`, so `Dispose` waits | Low x High | D19 tool contract in `internal/tools` godoc; headless exits after `abortGrace`; H5 enforces process-group kill |
| `Prepare` refactor changes request bodies | Med x High | cassette replay; exact-body tests per adapter (phase 07) |
| Credential leak into the log | Low x High | Allowlist types, header-value allowlist, error-text cleaner, canary tests in phases 04, 05, 07, 08 |

## Rollback <!-- red-team #13 -->

Before each phase starts, tag the branch head: `git tag lifecycle-pNN-base` (for example `lifecycle-p04-base`). Phases 01-09 rewrite the same files, so a selective revert of one phase is not supported. To roll back phase N: create a branch from `lifecycle-pNN-base`, then re-land later phases on it if they are still wanted. A reset of `master-2` itself needs the user's approval. Phases 00 and 11 are documents only.

## Red Team Review

Source: [red-team-findings.md](red-team-findings.md) (4 reviewers, 41 raw findings, 15 after deduplication, all accepted). Each change in a phase file carries `<!-- red-team #N -->`.

| # | Finding | Severity | Disposition | Applied To |
|---|---|---|---|---|
| 1 | Retry classifies by typed errors that only OpenAI Responses produces; no `Retry-After` | Critical | Accept | New phase 07 (typed failures, adapter tests with real bodies); phase 08. The "OpenAI has no idle timeout" part is stale: idle timeout exists since `e991b5c` (`openai/options.go:51`); phase 07 only classifies it as `TIMEOUT` |
| 2 | Async bus breaks Subscribe timing, goleak, abort tail | High | Accept, resolved by D25 | Phase 05: Subscribe stays synchronous (DeepSeek); listener failure contained; output failure path and bounded signal exit; goleak `TestMain` in `internal/bus`; affected tests re-checked (only `TestAgentListenerError` changes) |
| 3 | Entry writer removed the prompt commit path | High | Accept | Phase 04 step 3 (explicit commits at today's timing) |
| 4 | Commit-after-auth needs a binder move; credential failure contract | High | Accept | Binding stays in `AuthRunner`; credential failure stays an assistant error; commit timing decided by D26 (phase 08) |
| 5 | Retry after `message_start` leaves an open message; per-attempt preparation | High | Accept | Phase 08: Pi wire projection closes the failed message; `PrepareRequest` and `Prepare` run per attempt |
| 6 | Lost wakeup; input after abort never runs | High | Accept | Phase 06: atomic end decision under `a.mu`; DeepSeek wake latch; idle wake; race test `-count=200` |
| 7 | D23 log leaks via Model headers/URL; no system snapshot; in-place mutation; raw error text; weak secret scan | High | Accept | Phase 04: allowlist, `SystemSnapshot`, freeze at dispatch, `CleanDiagnostic`, canary tests; `rebuildRequest` in `agent`; phase 07: header-value allowlist |
| 8 | Retry can switch subscription to billed key; second binder | High | Accept | Binding stays in `AuthRunner`; `AuthBinding` projection (phase 07); billing pin (phase 08) |
| 9 | Pi defaults labelled "follow"; two retry event families | High | Accept, resolved by D25 | Phase 08 (DeepSeek numbers, no jitter per D11, policy captured per serving registration, durable entries, single-source Pi events); matrix rows 34-35d; vocabulary fix in phase 01 |
| 10 | Tool default mode inverted vs DeepSeek | High | Accept, resolved by D25 | Phase 09 (`ConcurrencySafe(args)`, default exclusive); phase 11 roadmap H-LOOP-09 |
| 11 | TRANSPORT vs STREAM_CLOSED merged; empty response contradiction; sticky max-tokens | High | Accept | Phase 07 (split codes, `EMPTY_RESPONSE`); phase 01 (DeepSeek max-tokens stop with sticky reason); matrix rows 3, 4, 33, 38 |
| 12 | Conformance not test by test; sweep checks names only; row ownership drift | High | Accept | Phase 00 (full title sweep, assertion column, script that also checks the review list); matrix Phase column is the single source; script runs per phase; phase 10 hand review |
| 13 | Golden lists incomplete; phase 02 too large; rollback not executable; no retry clock | High | Accept | Golden lists in phases 01, 05, 08; phase 02 split into 02 and 03 with API-shape step; tag-based rollback; `Config.Wait` |
| 14 | Lifetimes and bounds; client resend API | Medium | Accept | Phase 05 (byte and count bounds, session filter, epoch on `Reset`, no truncated payload); phase 06 (queues per Agent cleared by `Reset`, bound, client resend option dropped, row 72 N/A until H13). The bounds are `diverge-deliberate` BOUNDS (source audit) |
| 15 | Coordinator details | Medium | Accept (bounded drain withdrawn: reversed D19, see source audit) | Phase 09 (one select loop, unbounded drain as D19, session-unique IDs, shared aborted-message constructor); phase 03 (frozen arguments, effective arguments published) |

## Source Audit Review (2026-10-06)

Source: [review-261006-1104-deepseek-lifecycle-source-audit.md](../reports/review-261006-1104-deepseek-lifecycle-source-audit.md). The lead spot-checked findings 1, 3, 4, 5 and 6 against the source and accepted the audit. This revision re-verified each correction against DeepSeek `5badb15` and cites `path:line` in the matrix and phase files.

| Audit item | Disposition | Applied to |
|---|---|---|
| High 1: bounded cancellation breaks exclusivity | Accept; resolved by D19 reaffirmed (no bound) | Phase 09 "Unbounded abort drain"; matrix N5; red-team #15 note |
| High 2: coordinator cannot interrupt a blocked control handler | Accept: a cancel during `BeforeTool` or `AfterTool` takes effect when the handler returns; the body drain starts after that; no timer covers control calls | Phase 09 Decisions and two separate tests; matrix 49b, ST10, ST12 |
| High 3: recorded intent vs body invocation | Accept: three facts; `ToolCall` entry before pre-control; repair by record, abort by body invocation | Phase 09 Decisions, steps 1, 3, 5, 6; phase 04 entry type; matrix B2, B5, 51, 63; test `TestRepairUsesRecordedCallNotBodyInvocation` (DeepSeek `tool-calls.spec.ts:808`) |
| High 4: request log cannot prove stated equality | Accept per D28: logical request in phase 04, `Prepare` and exact body in phase 07; binding-dependent shaping rebuilt from the logged `AuthBinding` (planner design choice inside D28); unlogged values named | Phases 04, 07; matrix 25, 25b, SB9 |
| High 5: retry policy of the serving registration | Accept: policy captured in `Prepared` and passed with the failure; live replacement N/A, consistent with SB10 and SL32 | Phase 08; matrix SL10, SL10b |
| High 6: attempt opening definitions | Accept: one boundary (after the stream returned and `ctx` checked) in phases 01 and 08 | Phases 01, 08; matrix SA5 (moved to phase 01) |
| High 7: follow snapshot and cursor cut | Accept: Agent-owned published pair; assistant-stream baseline with the cut seq as watermark, as DeepSeek (D25, 2026-10-06; matrix 71b `follow`) | Phase 05; matrix 71, 71b |
| High 8: output deadline does not release a blocked write | Accept: no write interruption (no portable deadline on inherited stdout); bounded grace; exit path never takes the lock (`TryLock`); `onError` from `Write` and timer flush; `Dispose` runs in a goroutine so the grace bound still holds | Phases 05, 06; matrix W4, W5 |
| Row 74 / SA31 | Accept: `diverge-deliberate` BUSY | Matrix 74; phase 06 |
| Phase 09 line 20 (post-control routing) | Accept: case table; validation (D21) difference named; unknown tool follows DeepSeek (Q10 resolved by D25) | Phase 09; matrix ST5-ST5d |
| ST27 false N/A | Accept: `follow` with four wrapper tests | Phase 09; matrix ST27 |
| ST11 | Accept: split direct (N/A) and batch (follow); materialization is not schema validation | Matrix ST11, ST11b; phase 09 |
| ST20 vs B1 | Accept: start event shows effective arguments; D21 coercion kept | Matrix ST20 (moved to phase 03); phase 03 |
| ST21 | Accept: split register, input-schema timing (Ask rule), output schema/timeout (N/A) | Matrix ST21, ST21b, ST21c |
| ST1 | Accept: `deferLoading` noted; Ask omits it (ST29) | Matrix ST1 |
| SA18 vs row 20 | Accept per D27: `Remove(inputID)` | Phase 06; matrix 20, 20b, SA18 |
| SA26 vs rows 19, 53 | Accept per D27: `AfterTool` added context, non-waking | Phase 09; matrix 19, 19b, 53, SA26 (moved to phase 09) |
| SA19, SA25 | Accept per D29: `Dispose()` | Phase 06; matrix SA19, SA25, SA25b, SB11, 36 |
| SL14 phase split | Accept | Matrix SL14 (07), SL14b (08) |
| A6 citation | Accept | Matrix A6 |
| Phase 08 Retry-After text | Accept: no reset-time text | Phase 08 |
| F1 fallback | Accept: `HTTP_<status>` and in-band `SERVER`; `UNKNOWN` only without provider facts | Phase 07; matrix F1 |
| Row 56 modes | Accept: split emit, serial, waterfall, one-shot `next` (Ask-new) | Matrix 56-56d, A1; phase 02 |
| Row 22 evidence | Accept: `hooks-codex/tests/coverage-cases.ts:111` | Matrix 22; phase 02 |
| W3 wording | Accept: no "never stall" claim | Matrix W3; phases 00, 05 |
| SS3 sole writer | Accept: split commit-before-publish and sole writer | Matrix SS3 (05), SS3b (06); phases 04, 06 |
| Phase 05 truncated replay | Accept: gap marker and `resync`, never a truncated payload | Phase 05; matrix N7 |
| Row 77 | Accept: split spawn and fork with their sources | Matrix 77, 77b |
| Row 70, SL18 | Accept: added the missing assertions | Matrix 70 (attempt identity), SL18 (final-usage fallback) |
| Report precedence: Ask-new vs diverge | Accept: rows 59, B8, N6, SS11 relabelled `diverge-deliberate` | Matrix; phase 00 A.4-A.5 |
| Phase 00 "accepted design" | Accept: status stays "proposal" | Phase 00 D.7; phase 11 |
| Required next revision 5 (assertion phases) | Accept: SA5 to 01; SA10, SS3b to 06; SB4, SB8, SB11, SL15 to 08; SB9, 25b to 07; ST20 to 03; 19, 53, SA26 to 09 | Matrix Phase column; review list regenerated |
| Required next revision 6 (counterexamples) | Accept: counterexample named in each changed assertion | Matrix; phase test lists |
| Audit unresolved questions 1-4 | Resolved by D19 reaffirmed, D27, D28, D29 | Fixed constraints |
| Deferred | None | - |

### Whole-Plan Consistency Sweep

Done after the last edit on 2026-10-06. Every file in this directory was re-read.

**Phase task x constraint check.** Each phase's tasks were compared with each fixed constraint and D-row above:

| Phase | Constraints touched | Result |
|---|---|---|
| 00 | D24, D25, D26-D29, report status | Agrees; the report stays a proposal |
| 01 | JSON additive, D20 tail, D18, attempt boundary | Agrees; boundary matches phase 08 |
| 02 | No `Hooks` adapter, PRECEDENCE | Agrees; the temporary poll functions are a port, removed in phase 06 |
| 03 | D21, D25 frozen arguments, D20, CONT-BOUND | Agrees; the bound is labelled a divergence |
| 04 | D23, D28 (logical level only), commit timing before D26 | Agrees; no exact-body claim before phase 07; commit moves in phase 08 |
| 05 | JSON backpressure (Q2), D18, BOUNDS, D25 (row 71b), D12 dashboard | Agrees; backpressure kept, no write interruption; the assistant-stream baseline follows DeepSeek and changes nothing in the JSON stream (the follow frame is not a JSON line) |
| 06 | D25 over D1 (no queue modes), D27 `Remove`, D29 `Dispose`, D18 terminal record, BUSY, BOUNDS, D19 drain on dispose | Agrees; `Dispose` clears the queues as DeepSeek (Q9 resolved, row SA25b `follow`). D18 vs D29 order: both hold, because the JSON writer does not write `agent_disposed` (phase 06) |
| 07 | D28, D26 credential failure after commit, D22, D23, no new dependencies | Agrees; binding stays after the commit point |
| 08 | D11, D18, D20, D26, JSON retry events, billing pin, D29 during backoff | Agrees |
| 09 | D19 unbounded drain, D27 added context, SNAPSHOT, ALL-RESULTS, D20, D21 | Agrees; an unknown tool runs `BeforeTool`, `ExecuteTool` and `AfterTool` as DeepSeek (Q10 resolved by D25, row ST5c `follow`); SNAPSHOT still decides which tools exist |
| 10 | Real code path rule | Agrees |
| 11 | Roadmap update only in this phase; no "accepted" claim | Agrees |

**Stale-term greps** (run from the plan directory over `phase-*.md` and `conformance-matrix.md`; this file and `red-team-findings.md` are excluded because they quote old terms on purpose):

| Stale term | Command | Hits |
|---|---|---|
| Bounded drain | `grep -n 'ToolAbortDrain\|bounded drain\|bounded abort drain'` | 3, all intended: "Unbounded abort drain" (phase 09), "unbounded drain" (phase 00 A.6, matrix ST15) |
| Contradictions claim | `grep -n 'Remaining contradictions: none'` | 0 |
| Accepted design | `grep -n 'accepted design'` | 2, both in phase 00 and both forbid the term |
| Attempt before stream | `grep -n 'open an attempt immediately before\|open the attempt, call'` | 0 |
| Pending Q8 | `grep -n 'pending-Q8\|Q8'` | 0 |
| Client resend option | `grep -n 'WithInputID'` | 1 (phase 06 states that it is dropped; intended) |
| Never stall | `grep -n 'observers never stall\|never stall'` | 2, both state that the claim is not made |
| Reset-time text | `grep -n 'shows the reset time'` | 0 |
| `UNKNOWN` fallback | ``grep -n 'UNKNOWN` (anything else'`` | 0 |
| Continuation bound as Ask new | `grep -n 'Ask new, 3\|Ask new, value 3\|does not reverse DeepSeek'` | 0 |
| Truncated replay | `grep -n 'stored truncated'` | 1 (phase 05 states that a payload is not stored truncated) |
| Policy lookup after failure | `grep -n 'RetryPolicy(provider)'` | 0 |
| Exact equality in phase 04 | `grep -n 'TestRequestRebuiltFromHistoryMatchesDispatchedRequest\|equals the dispatched request'` | 0 |
| Queues kept on dispose | `grep -n 'queues kept\|keeps the queues\|KeepsQueues'` | 2, both about `Abort(KeepQueued)` (phase 06 test name, matrix row 18); intended |
| No partial baseline | `grep -n 'no partial baseline\|SkipsPartial\|openAssistant'` | 0 |
| Unknown tool skips hooks | `grep -n 'diverge SNAPSHOT\|unknown tool runs no hook'` | 0 |
| Open Q9 or Q10 | `grep -n 'Q9\|Q10'` | 6, all state that the question is resolved (phase 00 item 7, phase 06 Dispose, phase 09 case table and unknown-tool decision, matrix SA25b and ST5c) |

**Matrix structure** (scripted parse of `conformance-matrix.md`): 300 rows, no duplicate ID, every row has 8 cells; 157 `follow`, 30 `diverge-deliberate`, 9 `Ask-new`, 104 `N/A`; every non-follow row has a reason; every N/A row has no test and no phase; the "Assertion review" list of each phase equals its non-N/A rows (196 in total). No planned test is created in a later phase than its row (one case, ST9, was moved to phase 03). No planned test name contains a plan code.

Remaining contradictions: none between phase tasks and the matrix. One D-row tension was found and resolved without changing either row: D18 (`agent_settled` is the JSON terminal record) and D29 (`agent_disposed` after the drain); the JSON writer omits `agent_disposed`, Go listeners get it (phase 06). The two places where the written D-rows and the DeepSeek source differed are resolved: Q9 (the user chose to clear the queues on `Dispose`) and Q10 (D25 applies to unknown-tool routing). Rows SA25b, ST5c and 71b are now `follow`.

## Unresolved questions

None remain. Q1-Q5 and Q7 are resolved by D25; Q6 is resolved (H7a complete); Q8 is resolved by D26; Q9 and Q10 are resolved below.

**Q9. Resolved (user, 2026-10-06): clear the queues.** `Dispose()` clears the queued input, as DeepSeek: disposal calls `cancel({kind: 'disposed'})` without `keepInbox` (`packages/core/agent-loop/src/index.ts:544`), and `cancel` clears the inbox (`packages/core/agent-loop/src/agent.ts:176-177`). One `queue_update` with empty queues is published; no queued input runs. Applied to phases 00 and 06 and matrix row SA25b (`follow`).

**Q10. Resolved by D25 (2026-10-06): follow DeepSeek.** No D25 exception covers it. A call to an unknown tool goes through `BeforeTool`, `ExecuteTool` and `AfterTool`; the body stage gives the unknown-tool error (`packages/core/tools/src/index.ts:1400-1406`, `:1506`, `:1578-1579`, `:1595-1596`, `:1783`). `BeforeTool` gets no tool definition and the raw arguments. Applied to phase 09 and matrix rows ST5, ST5c (`follow`) and ST8.

**Q6. Resolved (2026-10-06):** H7a is already complete (`plans/261006-0157-h7a-subscription-auth/plan.md:4`, 6/6 phases; commit `e991b5c`). The redesign has no H7a blocker and can start after plan approval.

**Q8. Resolved (user, 2026-10-06): A, recorded as D26.** Admitted input is committed after the first attempt's request preparation succeeds, as DeepSeek. On a preparation failure or cancel, nothing is committed; the Agent wrapper still publishes the D20 error events, but the error message is not saved. The JSON stream differs from Pi on this path only.
