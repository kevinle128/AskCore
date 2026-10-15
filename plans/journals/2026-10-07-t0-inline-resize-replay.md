---
title: T0 inline resize replay
date: 2026-10-07
summary: Use application transcript state to repair inline terminal history after resize.
---

# T0 inline resize replay

## Change

The user accepted the Grok-style inline resize route.
The application retains committed transcript blocks.
After a 120 ms resize delay, one renderer clears the screen and native scrollback and prints the confirmed transcript and live frame again.
Replay does not advance the commit frontier or acknowledge a block again.
The default E2E runner now selects the reviewed, frozen scratch candidate.
Root production Go code and dependencies are unchanged.

## Verification

The promoted default passed 18 headless terminal cases and 43 Go checks.
Independent checks passed race, vet, build, the renderer regression, and real partial replay failure cleanup.
The five oracle controls reject incomplete or duplicate generations and alternate-screen use.
Source, old archive, and protected product hashes stayed equal during verification.
Evidence: plans/261007-1156-inline-resize-fix/artifacts/replay-verification/.

## Limits

The pinned Ghostty engine retains fewer than 2000 lines at 52 by 15, including in plain output controls without purge.
The strict capacity probe remains available and reports failure.
The default pending-write test uses the verified 13 by 6 and 40 by 12 sizes and retains all 2000 lines.
Terminal.app and iTerm2 checks and D14 remain pending.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
