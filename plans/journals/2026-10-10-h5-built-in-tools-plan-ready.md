---
title: "H5 built-in tools: plan ready"
date: 2026-10-10
summary: "Ten-phase TDD plan for the seven Pi tools, the sandbox runner and the DeepSeek-style job table; all decisions made."
---

# H5 built-in tools: plan ready

## What happened

Planned H5 in deep TDD mode from the approved architecture (plans/architectures/h5-builtin-tools-architecture.md) and the Q1-Q15 decisions. The first planner run was stopped mid-way; a second run continued from the partial files. Plan: plans/261010-2349-h5-builtin-tools/ (10 phases, about 20.5 days, 46 runtime-flow rows all passed).

## Decisions made during planning (user, 2026-10-11)

- N1: a T1b design slice hides <system-reminder> notices in the TUI and lands before or with H5.
- N2: `ask -p` waits for idle, prints, disposes (jobs killed), as DeepSeek's headless runner, plus a SIGKILL sweep; a test covers a job still running at the final answer, which DeepSeek lacks.
- N3: `monitor` exactly as Grok CLI (command, description, timeout_ms default 10 h, persistent; no pattern parameter; filtering in the command).
- N4: re-record the six cassette tests on OpenAI.
- `read` images exactly like Pi (detect, convert, resize to 2000x2000 and 4.5 MiB), pulling H-TOOL-20 into H5; pure Go `golang.org/x/image`.
- Rotated OAuth tokens from the phase 10 runs are copied back with a backup.

## Lessons

- Reading the reference code before deciding changed two answers: Grok's monitor has no pattern parameter, and DeepSeek's headless runner already does exactly what option A proposed.
- A planner run that writes nothing for a long time should write each phase as soon as it is ready, so progress is visible and a stop loses less.

## Next steps

Design and approve the T1b notice-hiding slice; then implement H5 with /kk:implement.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
