---
title: H13a ACP plan and accepted review corrections
date: 2026-10-07
summary: Completed deep TDD planning with four user-approved contract and fixture corrections.
---

# H13a ACP plan and accepted review corrections

## Work

Created the H13a ACP v1 stdio plan and four TDD phases.
Research covered Grok runtime wiring, Go SDK candidates, and existing Ask owners.
Two external reviewers and the draft author's security review checked source claims.
The security review was not independent.

## Decisions

The user chose ACP v1, one Agent per session, and existing credentials with CLI login.
The user approved no-start completion, Reset observer replacement, configured auth-method constraints, and supplemental real stdio composition tests for controlled tool drain.
The plan separates production binary E2E from injected tool and writer fault fixtures.

## Checks and next step

Plan format, local links, stale-text checks, and diff checks passed.
No product tests or SDK conformance ran.
D17 remains open until Phase 1 executable conformance passes.
All four implementation phases remain pending.
The source plan is plans/261007-0700-h13a-acp-stdio/plan.md.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
