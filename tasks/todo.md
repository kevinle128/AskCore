# Todo

- [ ] LLM cassette testing: plan at `plans/261005-2132-llm-cassette-testing/plan.md` (waiting for approval)

## Independent review: lifecycle Event Pipeline report (2026-10-06)

Target: `plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md`.
Rule: do not trust the report. Verify each claim against source (DeepSeek `5badb15`, Ask working tree).

- [x] 1. Extract claim list from report (DeepSeek facts, Ask facts, design proposals)
- [x] 2. Agent A: verify DeepSeek driver, lifecycle, dispatch/middleware, recovery, inbox (source + GitNexus + DeepWiki)
- [x] 3. Agent B: verify DeepSeek tool coordinator, session log/repair, event bus/observation, hooks-codex
- [x] 4. Agent C: verify Ask current-state claims (pipeline hooks, agent loop, tools, emit, auth, bus, D19/D20)
- [x] 5. Synthesis: per-claim verdict (correct / wrong / unverifiable), gaps the report missed
- [x] 5b. Agent D: DeepSeek edge-case + feature inventory from tests, compare to report (user request: edge cases, depth, missing features)
- [x] 6. Independent judgment: is the "learn from DeepSeek" design correct and fitted to Ask? (adversarial review)
- [x] 7. Write review report to `plans/reports/review-261006-0848-lifecycle-pipeline-report-audit.md`

### Review
- Report: `plans/reports/review-261006-0848-lifecycle-pipeline-report-audit.md`.
- DeepSeek facts correct; provenance of Ask-own designs unlabelled; 4 current-state errors; 5 must-have edge cases missing; D19 reversal has no user trail (D19 was user-decided "as Pi" on 2026-10-01). Fit judgment (section 5) is the lead's own synthesis, not a separate agent.

## Lifecycle and Event Pipeline redesign plan (2026-10-06)

Plan: `plans/261006-0933-lifecycle-event-pipeline-redesign/plan.md` (12 phases, 123h, conformance matrix vs DeepSeek `5badb15`).
- [x] User decisions recorded in roadmap: D19 revised, D23, D24, D25 (DeepSeek default; overrides D1 in lifecycle), D26 (commit after prepare)
- [x] Red team (4 reviewers, 15 findings accepted and applied)
- [x] All open questions resolved (Q1-Q8); `ak plan validate` passes
- [x] User authorized implementation with `/ak:cook --tdd --auto`

### Cook progress (--tdd --auto)
- [x] Phase 00: design report corrected, matrix checked, check script added
- [x] Phase 01: cycle/turn/attempt, wire events, max-tokens stop (reviewed, 10 findings fixed)
- [x] Phase 02: typed dispatch port, DeepSeek waterfall precedence (reviewed; C1, H1, M1-M3 fixed)
- [x] Phase 03: frozen args, Cancel, StopTurn, continuation bound, scoped registries (reviewed; C1, H1, M1, L1-L6 fixed)
- [x] Phase 04: session entry writer, D23 logical request log, frozen dispatch, CleanDiagnostic (reviewed; H1, H2, M1, M2, L1-L5 fixed)
- [x] Phase 05: contained listeners, follow path with stream baseline, output failure path (reviewed; M1-M3, L1-L8 fixed)
- [x] Phase 06: Agent-owned queues, admission, Remove, Dispose, headless signals (reviewed; H1-H3, M1-M4, L1-L7 fixed; goroutine-ID lock replaced by dispatch counter)
- [x] Phase 07: typed provider failures and Prepare; independent source review complete; tests, race, lint, cassette replay, exact sent/rebuilt bodies, and Matrix07 passed
- [x] Phase 08: complete model attempts, retry, billing pin, and interrupted replay; independent review approved 10/10; tests, build, affected race, final lint, and Matrix08 passed; all 44 assertions reviewed
- [x] Phase 09: tool coordinator, abort outcomes, repair, and AfterTool context; independent review approved 10/10; final agent/pipeline tests and race, full lint, build, and Matrix09 passed; all 34 assertions reviewed
- [x] Phase 10: end-to-end, write-failure, and signal fixtures; independent review approved 10/10; full tests, full race, CLI checks, N8 race count 200, final lint, build, and Matrix all passed; all 197 assertions reviewed
- [x] Phase 11: roadmap, architecture, and package documentation complete; independent review approved 10/10; all nine steps accepted, 199 local links valid, stale API scan and whitespace checks passed
