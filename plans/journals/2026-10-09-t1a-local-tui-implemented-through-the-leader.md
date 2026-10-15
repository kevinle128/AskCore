---
title: T1a local TUI implemented through the leader
date: 2026-10-09
summary: Phases 1-8 of the inline Ask TUI are implemented and verified; manual terminal run and design decisions remain open.
---

# T1a local TUI implemented through the leader

## What happened

The inline Ask TUI now starts with `ask` (no prompt), talks to the local leader through the Unix socket and ACP, and falls back to an in-process agent only at startup. Phases 1 to 8 ran with failing tests first.

- Phase 1: Bubble Tea v2.0.10 with the local renderer patch, lipgloss v2.0.5, pin guard tests.
- Phase 2: `internal/tuiclient` with reconnect, acceptance signal, in-process client.
- Phases 3 to 6: render core, editor with slash completion, transcript with markdown streaming, session view (status, queue, footer, selectors, connection states).
- Phase 7: terminal lifecycle (signals 130/143/129, restore on every exit path, early input, quit and detach notes).
- Phase 8: 13 Go E2E tests on the built `ask`, a `tui-test` driver in `e2e/tui/run.py` (24 of 24 cases), docs, and a real-provider run (21 model calls: OpenAI key, ChatGPT OAuth, Anthropic OAuth).

## Problems found and how they were closed

- Render core: a commit sized from the model's frame height scrolled a stale frame row into scrollback when the loop was slow. Fix: one row per commit write, so the row that scrolls off is always history.
- Two intermittent tests were test races (a read during a commit drain, and a golden that read only the visible grid). `TestAltUpDequeuesIntoDraft` typed before the draft cleared, which the editor keeps on purpose; the test now waits for the clear.
- Real provider run: the auth error block printed the raw provider JSON (with masked key text) and an empty reasoning summary drew an empty thinking row. Both fixed with tests.
- Slash command case: I said Pi and Grok are case-sensitive after reading one lookup each; the user ran `/MEMORY` and it worked. Missing paths: Pi applies the highlighted completion on Enter; Grok passes unknown commands to the shell, which lowercases them. Decision: command names are case-insensitive like Grok; arguments keep their case.
- OAuth: the first real run refreshed the ChatGPT token in a copy and rotated the refresh token. The refreshed `auth.json` was copied back over `~/.ask-t1a-oauth/auth.json`; the old file is kept as `auth.json.before-restore`.

## Decision

Login hint in the auth error block is now `ask auth login --provider <id>` (user, 2026-10-10); the old text failed because `--provider` is required.

## Next steps

- Manual Terminal.app or iTerm2 run of S3, S6, S7, S11 and resize.
- User decisions listed in the plan's unresolved questions (`not_driver` text, retry count, cost decimals, idle quit wording, ChatGPT model access error kind, renderer right-margin patch).
- Commit in phase-sized commits after the user answers.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
