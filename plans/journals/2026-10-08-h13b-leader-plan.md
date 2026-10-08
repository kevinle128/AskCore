---
title: H13b leader plan
date: 2026-10-08
summary: "Deep TDD plan for the leader over a Unix socket, with five maintainer decisions and red-team fixes"
---

# H13b leader plan

## What happened
Created plans/261008-1033-h13b-leader-unix-socket (6 phases; Phase 1 in full detail, the others in outline).

## Decisions
- Other-UID proof: injected owner UID with the real syscall, plus a manual root check.
- Explicit take is allowed when the session has no live driver, also when it is busy.
- Per-client FIFO output with an unbounded socket queue.
  The later user decision keeps slow clients connected; socket errors or failed liveness checks remove dead clients.
- The version gate checks the protocol integer only.
- Client stub command: `ask connect`.

## Red-team
- Applied: the host is the only authority for the driver generation.
- Applied: one link-level initialize at startup.
- Applied: agent-to-client $/cancel_request is rewritten and sent to all clients that got the request.
- Applied: `id: null` is rejected.
- Rejected: "observers cannot answer permission requests". It contradicts ARCH 7.3 item 4.

## Next steps
The maintainer reviews the wire names and limits, then /ak:cook Phase 1.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
