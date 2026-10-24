---
title: Async task runtime A1 implemented
date: 2026-10-24
summary: "Tasks registry, Agent.Notify, idle signals, custom notice message shipped test-first; review fixes applied"
---

# Async task runtime A1 implemented

## What happened
- Implemented plan `plans/261024-0958-async-task-runtime-a1` with `/ak:cook --auto` in TDD order: protocol `CustomMessage` and task events; `internal/tasks` (registry, runner, timer, sink, notice, sweep); Agent integration (`Notify`, lifetime context, idle edges, `WaitForQuiescence`, Abort/Reset/Dispose rules).
- Independent code review found two High issues, both reproduced with probes:
  - A Wake notice was lost when its turn ended with an error or an End decision.
  - `Sweep` followed a symlinked tasks root and could remove user directories.
  - Fixed with `wake` on every active-run notice, and with checked roots and session-shaped, file-by-file removal.

## Lessons
- Eager per-session directories created 265 empty dirs in one test run; preparation is now lazy at first output.
- A phantom race test passed with the guard removed; a test seam between the two reads made it real.
- `KeepQueued` keeps the wake flag, so a queued Wake notice starts one run after such an abort.

## Evidence
- `go test ./...` green, `-race` on agent/tasks/settings/app/acp/cmd-tui/protocol, vet clean, lint 0 issues, no sleep or ticker in new non-test code, real `~/.ask/tasks` untouched.

## Next steps
- A2: replace the ACP busy poll and stdio watchdog with `Agent.Idle()`; fix ACP run attribution for notice-started runs.
- A3: `bash run_in_background`, move to background, `ask -p` wait, faux `bg`, end-to-end proof.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
