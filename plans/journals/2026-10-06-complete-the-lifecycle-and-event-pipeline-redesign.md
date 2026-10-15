---
title: Complete the lifecycle and event pipeline redesign
date: 2026-10-06
summary: "Delivered typed controls, retry, ordered tool recovery, queues, disposal, and follow observation."
---

# Complete the lifecycle and event pipeline redesign

## What happened

Completed all twelve phases of the lifecycle and event pipeline redesign.
Typed control dispatch replaced the old Hooks contract.
The agent now owns cycle, turn, and attempt records, preparation before input commit, retry with a billing identity pin, ordered tool outcomes and repair, input removal, disposal, and bounded follow observation.
No new dependency was added.

## Verification and corrections

The full ordinary suite, full race suite, build, configured lint, real headless CLI checks, cassette replay, and all 197 conformance assertions passed.
The final reviewer checked 104 explicit exclusions.
The three end-to-end fixtures passed five race runs each, and the wake-race checks passed 200 runs.
Final review found two missing assertions: failure on the second log append and a derived usage total in the real provider fold.
Both checks were added without changing production behavior.
The headless fixture separates Pi retry publications from durable Ask turn identity.
The documentation review checked 199 local links and removed stale lifecycle claims.

## Decisions and next steps

The accepted lifecycle plan is complete.
The historical design report is marked implemented, without claiming user acceptance.
SQLite durability, lease, and resume remain separate roadmap work.
The workspace changes are uncommitted.
The commit preflight excludes unrelated edits and build binaries.

Evidence: [final tests](../reports/tester-261006-phase10.md), [contract review](../reports/review-261006-phase10.md), [documentation review](../reports/review-261006-phase11.md), and [progress](../reports/pm-261006-1753-lifecycle-progress.md).

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
