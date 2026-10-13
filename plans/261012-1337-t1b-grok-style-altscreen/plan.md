---
title: "T1b: Grok-style alternate-screen TUI"
description: "Move the interactive TUI to the alternate screen: compact blocks with per-block fold modes, selection, viewer, mouse, hook rows, bottom marker and exit line."
status: completed
priority: P1
branch: new-session
tags: [tui, altscreen, tdd]
created: 2026-10-12
tdd: true
---

# T1b: Grok-style alternate-screen TUI

Approved design: [design.md](design.md) (user, 2026-10-12), with all 14 open-question recommendations accepted. Interactive prototype: [artifacts/interactive.html](artifacts/interactive.html) (prototype test: `artifacts/proto-test.js`, 22 checks; it needs `puppeteer-core` and Chrome). Source analysis: [port report](../reports/xia-261012-1238-grok-style-display-port.md).

This plan replaces the inline screen model of the interactive TUI. It supersedes the transcript-view part of [261012-0226-t1b-tool-output](../261012-0226-t1b-tool-output/plan.md) (style C).

## Outcome

`ask` opens a full-screen TUI. Tools, thinking and hooks are one row by default and open with `e`, `→`/`←`, `E`, `ctrl+t`, a click or `Enter` (full-screen viewer). The pane scrolls by keys and wheel, follows new output, and shows `▼ End` / `▼ <N> new lines · End` when the user scrolled up. Drag-select copies on release. On exit the terminal is restored and one line `ask: session <id>` stays. Same behavior for the leader TUI and the in-process TUI.

## Source

| Source | Ref | Used for |
|---|---|---|
| Grok | `~/Desktop/workspace/opensources/grok-build` at `559751fd`, `crates/codegen/xai-grok-pager` | Fold modes, keys, expand indicator, selection highlight, bottom marker, mouse, hook row fields |
| Pi | `~/Desktop/workspace/opensources/pi` | `/copy` and clipboard order (already in Ask) |

No code is copied, so no license obligation applies.

## Steps

| # | Step | Files (main) | Status |
|---|---|---|---|
| 0 | Spike: 2000-block session, draw only visible blocks, cached row counts per width and fold mode; measure frame time on wheel scroll and on a streaming delta (open question 3). Stop and re-plan if a frame is over budget | `internal/tui` spike test, report in `plans/reports/` | done |
| 1 | Screen model: alt screen for the whole program, header row, block pane, status row, prompt pane, shortcut bar; remove the inline commit and replay path and the `ctrl+o` transcript view; keep the viewer code for one block | `internal/tui/layout.go`, `render_inline.go`, `transcript_view.go`, `model.go` | done |
| 2 | Block list: per-block fold mode (Collapsed, Truncated, Expanded), row text from section 3.4 (verbs, command fallback), Truncated form (first 3, `⋮`, last 3), Expanded with `$` row, diff and truncation line | `internal/tui/blocks*.go`, `translate.go` | done |
| 3 | Focus and keys: `Tab`, `j/k ↑↓`, `e E`, `→ ← l h`, `ctrl+t`, `ctrl+e` in list focus, `Enter`/`Esc` viewer, `End G ctrl+End`, `shift+← →` turns, printable key moves to the prompt | `internal/tui/update_input.go`, `keys.go`, `focus.go` | done |
| 4 | Selection marks and indicators: `selected` background for a Collapsed step, `┃` rail for other blocks, `›` / `⌄` on select and hover | `internal/tui/blocks_format.go`, style roles | done |
| 5 | Scroll and follow: virtualized pane, follow mode, bottom marker centered (`▼ End` tool role, `▼ <N> new lines · End` accent) | new `internal/tui/pane.go` | done |
| 6 | Mouse: `MouseModeAllMotion`; wheel, click select, click on a step's first row opens the viewer, `← back`, hover indicator, drag-select copy on release through the `/copy` path, click on the marker and the prompt | new `internal/tui/mouse.go`, `clipboard.go` | done |
| 7 | Hook rows: `⚓ <Event> hook (<scope>/<name>) · <target>`, result, description row, detail fields in Expanded and the viewer; fed by test fixtures until H11 delivers the event (protocol need list in design section 3.4) | `internal/tui/blocks_hook.go` | done |
| 8 | Exit line from `cmd/tui` after the program ends, on every exit path (normal, ctrl+c, SIGTERM, SIGHUP); terminal restore checks | `cmd/tui/tui_client.go`, `internal/tui/quit.go` | done |
| 9 | Fork cleanup: remove the inline renderer patch (`insertAbove`, `replayInline`, `ReplayInlineTranscript`), update `PATCH.txt` and `renderer_patch_test.go` (open question 8) | `third_party/bubbletea` | done |
| 10 | Screen tests for every acceptance criterion of design section 10 (80×24, 120×40 where drawn, 40×14, 28×14) | `internal/tui/*_test.go` | done |
| 11 | E2E with the built `ask` through the leader and in process; replace the inline T0/T1a scenarios that no longer apply | `e2e/tui`, `cmd/tui` | done |
| 12 | Docs from design section 12: architecture doc, style guide, the `ask-tui-design` skill, TUI README, roadmap (D15, T0 note, T1b rows), `CLAUDE.md`, `e2e/tui/README.md` | docs and plans | done |

Follow-up outside this plan: an optional `description` argument for the `bash` tool (open question 1), a tool-schema change in `internal/tools`.

## Implementation notes

Deviations from [design.md](design.md) that the implementation took on purpose. The design stays as approved; the code and the docs follow this list.

- (a) Thinking is one block for each part of the model output, not one block for the whole turn.
- (b) The viewer and Expanded text wrap, so the cut of `Does` and `Reason` in the 40x14 V3 sketch does not apply.
- (c) A selected Expanded hook shows `⌄` in place of its glyph. Sketch A7 draws `┃ ⚓`.
- (d) The shortcut bar in list focus always shows `e fold`, with `: <mode>` only for a block that can fold, and `Enter open`.
- (e) A click on block text acts on the release, on the cell of the press, because a press can start a drag.
- (f) The count in the bottom marker may be approximate for a frame or two after a resize (the pane settles it in a few frames).
- (g) The exit line prints the full session id, and the old detach note still prints above it.
- (h) A selector or dialog is capped at half of the screen height, so the provider box shows fewer list rows at 40x14.
- (i) The hyperlink rows of the login box carry one OSC 8 id for each row, because of a renderer bug at the pinned Ultraviolet version.
- (j) Hook rows come from fixtures until H11 delivers the hook event.

## Acceptance

Each criterion in design section 10 has a named test that passes. `go test ./internal/tui ./cmd/tui`, `go -C third_party/bubbletea test ./...`, `go vet ./...`, golangci-lint and `python3 e2e/tui/run.py` pass. The spike in step 0 meets its frame budget.

## Risk and rollback

Post-implementation review: [source and test evidence](../reports/review-261012-1950-t1b-grok-style-altscreen.md).
The review added terminal regression checks and repaired selection visibility and follow state across resize.
It also added the native-selection hint to the shortcut bar when the full hint fits.

- Risk: high. The change replaces the screen model that the T0 gate chose, removes the inline commit path, and touches many TUI tests and e2e scenarios. Mouse capture changes text selection for the user (bypass with shift or option + drag).
- Rollback: the change stays in `internal/tui`, `cmd/tui` (exit line), the fork and docs; the protocol and the leader do not change. Revert the slice commits to return to the inline TUI with the style C transcript view.
