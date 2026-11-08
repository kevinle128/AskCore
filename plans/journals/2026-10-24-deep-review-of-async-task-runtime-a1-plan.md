---
title: Deep review of async task runtime A1 plan
date: 2026-10-24
summary: "Deep scout, 3-reviewer red-team (15 findings applied), validation (e2e in A3, 7-day retention)"
---

# Deep review of async task runtime A1 plan

## What happened
- Ran `/ak:plan --deep --tdd` on the existing A1 plan (scope kept to A1 by user choice).
- Three Explore scouts verified every phase anchor; report `plans/reports/scout-261024-1705-async-task-runtime-a1-deep.md`.
- Red-team with three hostile reviewers gave 27 findings, 18 after deduplication; 15 accepted and applied, 2 rejected (user decisions D1/D2), 2 moved to validation. Report `plans/reports/red-team-261024-1718-async-task-runtime-a1.md`.

## Most important fixes
- Idle channel: one busy predicate, channel changes only on edges (orphaned waiter and double close found).
- `WaitForQuiescence`: wait only on open conditions plus a work counter (busy spin and race found).
- Task notices bypass `MaxQueuedInputs` (silent drop found); Reset closes the old registry after `emitMu` (stale events in new epoch found).
- Escaping with a replacer, not `xml.EscapeText` (newlines became `&#xA;`).

## Decisions (user)
- End-to-end proof through `ask -p` moves to A3; A1 proves criteria through the real Agent API.
- Task output files kept 7 days; first Agent of each process sweeps once at start.

## Next steps
- `/ak:cook plans/261024-0958-async-task-runtime-a1/plan.md --tdd` (effort 5.5d).

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
