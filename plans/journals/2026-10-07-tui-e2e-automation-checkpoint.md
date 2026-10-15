---
title: TUI E2E automation checkpoint
date: 2026-10-07
summary: Added isolated terminal checks; strict native reflow checks expose stale editor rows in T0.
---

# TUI E2E automation checkpoint

Added isolated terminal checks; strict native reflow checks expose stale editor rows in T0.

The test project uses Python standard-library code and the pinned tui-test CLI in `e2e/tui`.
The runner builds a temporary copy of the immutable T0 archive and keeps production dependencies unchanged.
Independent checks confirmed six passing cases and two failing resize cases across Alacritty and Ghostty.
The 39 archived Go checks and four assertion controls passed.
Paused output replay showed correct engine reflow before the renderer redraw left stale editor rows.
Keep G3 failed until the renderer handles the live-frame origin after resize.
Keep the real iTerm2 gate and D14 readiness pending.
See [independent results](../reports/tester-261007-tui-e2e.md) and [diagnosis](../reports/debugger-261007-tui-e2e.md).

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
