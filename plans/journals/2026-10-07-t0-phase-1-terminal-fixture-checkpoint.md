---
title: T0 Phase 1 terminal fixture checkpoint
date: 2026-10-07
summary: Isolated PTY oracle and smoke implementation reviewed; iTerm2 and independent validation remain pending.
---

# T0 Phase 1 terminal fixture checkpoint

## Result

Implemented the Phase 1 scratch module with Bubble Tea v2.0.10, creack/pty v1.1.24, and pinned x/vt.
Six worker-run tests, race, vet, and build pass.
The code review fixes separate stderr and assert the final terminal snapshot.
Source and evidence are archived with hashes under the T0 plan.
All 27 product-boundary file hashes match the opening snapshot.

## Findings

The emulator requires a query response pump before query-producing output is parsed.
On macOS, observe termios after tea.Run cleanup while the wrapper process still owns the slave; session-leader exit revokes it.
The first missing-editor failure also had a cleanup fault; the clean RED artifact is explicitly a replay against the saved baseline.

## Remaining work

User code approval is requested before independent testing and Phase 2.
Real iTerm2 smoke remains pending because Computer Use denied app access.
No blocking T0 gate or D14 decision is complete.
No process remains running and no commit was created.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
