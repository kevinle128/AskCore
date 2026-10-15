---
title: H7a Pi OAuth architecture and deep TDD plan
date: 2026-10-06
summary: Pi-backed architecture and six-phase TDD plan; all six source-backed corrections approved and applied.
---

# H7a Pi OAuth architecture and deep TDD plan

## Outcome

Created the detailed H7a architecture report and six-phase implementation plan.
Verified actual Pi production triggers and native Anthropic, ChatGPT and xAI lifecycles.
The source research used the current Ask working tree and pinned Pi source.
No product code, tests, live login, credentials or provider inference were executed by this planning task.

## Decisions

The user selected Pi as the reference for one saved account per provider and local-only logout.
Logout removes issued-client metadata with the credential record.
Ask adapts Pi interactive login/logout to headless auth commands.
The final command uses explicit method selection, with private input for keys and callbacks.
Verified ChatGPT identity, returning-client reuse and explicit new-account replacement remain required.
H4 is a partial Responses dependency; it does not block shared storage or Anthropic delivery.

## Review

Four reviewer lenses checked 96 source claims and produced six unique corrections.
The proposed corrections cover durable rotation uncertainty, shutdown commit drain, CLI test wiring, bounded private input, exact xAI denial errors and the complete option consumer inventory.
The user approved all six corrections.
The controller propagated them across all phases and the architecture/design records.
Runtime design proof is rechecked separately from implementation or provider-live acceptance.
The plan and phase statuses remain pending and todo.

## Artifacts and next step

Read [implementation plan](../261006-0157-h7a-subscription-auth/plan.md), [architecture report](../reports/xia-261006-0143-h7a-subscription-auth-architecture.md) and [review record](../261006-0157-h7a-subscription-auth/reports/review-decisions.md).
Final runtime-path validation and the whole-plan consistency sweep pass with zero unresolved design contradictions.
The implementation plan has six phases and 143 planned atomic capability rows.
The next step is implementation when requested; this planning task did not start product implementation.
AgentWiki publish skipped.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
