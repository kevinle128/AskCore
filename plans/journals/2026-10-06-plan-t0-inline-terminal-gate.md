---
title: Plan T0 inline terminal gate
date: 2026-10-06
summary: Create five TDD phases for an isolated terminal prototype; confirm internal Crush reuse and iTerm2 acceptance.
---

# Plan T0 inline terminal gate

## Work

Created the T0 plan and five phase files through the AgentKit planning workflow.
Read source research and reviewed the plan with three independent lenses.
Plan format and local link checks pass.
No terminal gate tests ran and no gate passed.

## Decisions

The user confirmed internal-only use and iTerm2 as the real terminal.
Crush source can be considered for copy or adaptation under the inspected internal-use grant with provenance and applicable notices.
Use a separate runtime scratch module and preserve source and evidence with the plan.
Root source and dependency integration remain outside T0 planning.

## Review

Three proposed corrections cover the PTY output FD, synchronized-output cleanup after partial writes, and valid TDD baseline evidence.
User review of these corrections was requested.
The user approved all three corrections, and they are applied in the canonical plan.

## Next step

Review propagation and the consistency sweep are complete.
Use the plan for tests-first T0 implementation when requested.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
