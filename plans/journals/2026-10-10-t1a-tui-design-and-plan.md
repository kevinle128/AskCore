---
title: T1a TUI design and plan
date: 2026-10-10
summary: "Design-first skill, approved T1a design (B4, in-process fallback), TUI style guide, and a deep TDD plan with red-team fixes"
---

# T1a TUI design and plan

## What happened

- Added the project skill `.claude/skills/ask-tui-design`: a design file with exact-width sketches, states, keys and acceptance criteria, then a hard approval gate before any Go code.
- The first T1a design was raw ASCII in markdown. The user could not review it. A rendered HTML preview (`artifacts/preview.html`) fixed that, and the skill now requires it.
- The user compared six visual directions (`artifacts/style-directions.html`) and chose B4 "Soft + live rail": soft prompt bands and replies, a rounded editor, a one-row footer, and a dashed rail only for live and queued items.
- New visual authority: `docs/tui-style-guide.md` (roles with truecolor, 256-color and no-color values, glyphs, block patterns).
- `plans/261009-2239-t1a-local-tui/design.md` (24 states, 22 criteria) approved on 2026-10-10. The approval also covers the Grok-style startup fallback to an in-process agent.
- Plan `plans/261009-2239-t1a-local-tui/plan.md`: deep and TDD, 8 phases, criterion-to-test map. A red-team review found 15 issues; the verified ones are applied.

## Decisions

- Libraries: lipgloss v2.0.5 (v2.0.6 raises the D14 Ultraviolet pin), glamour v2.0.1, chroma v2. Teatest v2, x/vt and golden are used for tests.
- T-LAY-09, T-MD-10 and T-CMD-08 move to T1b with H5.
- When the leader is unavailable at startup, `ask` falls back to an in-process agent (the earlier no-fallback rule is replaced). After a successful start, ask reconnects to the leader and does not switch to an in-process agent.
- Criterion 13 is proven by a client test against a real leader. No leader control is added only for tests.

## Lessons

- Raw sketches are not a review surface. Generate the text and the colored preview from one source.
- ACP has no separate "prompt accepted" signal. `session/prompt` returns at settle, and refused and failed prompts return the same error.
- Bubble Tea handles SIGINT/SIGTERM itself (SIGTERM exits 0) and ignores SIGHUP. Use `WithoutSignalHandler`.
- `go mod tidy` drops libraries that nothing imports. Add each library in the phase that first imports it.

## Next steps

- Cook phase 1 (root Bubble Tea v2 migration with `third_party/bubbletea`).
- Commit the skill, the style guide and the plan directory when the user asks.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
