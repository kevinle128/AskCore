---
title: "T1c provider setup: design approved and plan ready"
date: 2026-10-10
summary: Ported the omp /providers flow with Grok-style ACP; TUI design approved twice; eight-phase TDD plan validated.
---

# T1c provider setup: design approved and plan ready

## What happened

The user asked for a `/providers` command (add, list, remove providers) with `/model` showing only usable models, following omp for the flow and Grok CLI for ACP. Research read omp, Pi and Grok source (GitNexus was not reachable for the omp agent, so files were read directly at the indexed commit). Analysis: plans/reports/xia-261010-1416-provider-model-flow.md.

## Decisions (user, 2026-10-10)

- Command `/providers`, flat list like omp's sign-in screen, in the editor slot; removal asks for confirmation; masked input shows `(hidden)`; long URLs wrap with ctrl+y and a hyperlink.
- A start line `model  <provider>/<model>` under the header and a change line; the reply meta line and footer keep the bare id.
- Faux only with `ASK_FAUX=1`; Claude sign-in by browser with a pasted callback address, falling back to a code; choosing in `/model` saves the default (as omp); `TEA_TRACE` kept with a warning; removal cancels another client's pending sign-in.

## Lessons

- The planner's first run ended with no files and no report; check for the output directory before trusting an agent finished.
- Reading code found four gaps the design assumed away (faux always usable, no model event, `_ask` calls refused without a session, keys saved without a check). Plan before code paid off.

## Next steps

Commit the other sessions' uncommitted T1a work, then run `/ak:cook plans/261010-1424-t1c-provider-setup`.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
