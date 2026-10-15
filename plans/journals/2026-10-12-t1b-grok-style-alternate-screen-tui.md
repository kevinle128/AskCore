---
title: T1b Grok-style alternate-screen TUI
date: 2026-10-12
summary: "Interactive ask TUI moved to the alternate screen with folds, viewer, mouse, hook rows and exit line; all checks green, nothing committed."
---

# T1b Grok-style alternate-screen TUI

## What happened
Implemented the approved alt-screen design (plan 261012-1337) in 13 steps with TDD: pane spike (2000 blocks, worst frame 6.3 ms), screen model, fold modes and row text, focus and keys, selection marks, scroll and bottom marker, mouse, hook rows from fixtures, exit line, fork cleanup, acceptance tests, e2e, docs.

## Decisions
- The bubbletea fork keeps only the resize guard; inline replay and the Generation counter were removed.
- The Ultraviolet OSC 8 bug (repeated link on the next row is skipped) is worked around with per-row link ids in the login box; the pin is not patched.
- Thinking is one block per part; the viewer and Expanded text wrap; the bar always shows `e fold`.
- The exit line prints the full session id; the old detach note stays above it (design silent; user may remove it).
- Independent review found three defects (marker count at a full window, no settle after fold commands, lost mouse release); fixed with tests.

## Verification
go vet, go test ./... (cmd/tui 305 s), fork tests, golangci-lint (0 issues), e2e/tui/run.py 24 of 24 on two engines.

## Next steps
Decide on the detach note; bash `description` argument; H11 hook event; H8 resume format. Rollback ref: refs/snapshots/t1b-altscreen-baseline. Nothing is committed.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.

## Post-implementation review

The [plan review](../reports/review-261012-1950-t1b-grok-style-altscreen.md) found and repaired two resize defects: the selected block could leave the visible window, and resize could turn follow on.
Three terminal regression tests now cover these paths.
The shortcut bar adds the native-selection bypass hint when it fits.
Fresh full Go tests, vet, lint, and 24 engine E2E cases pass after the repairs.
