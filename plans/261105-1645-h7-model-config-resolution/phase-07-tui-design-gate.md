---
phase: 7
title: "TUI design gate (user approval)"
status: pending
priority: P1
effort: "0.5d"
dependencies: [4]
---

# Phase 7: TUI design gate (user approval)

## Goal

Write `design.md` in this plan folder with the `ask-tui-design` skill for the model-cycling key and for the display of a models.json problem found when `/model` opens, and stop until the user approves it.

## Hard gate

No Go code in `internal/tui`, `internal/tuiext`, `cmd/tui` interactive mode, or their tests for this feature until `design.md` has `design-status: approved` with the approver and the date. Only the user approves, in the current conversation. A plan summary or a subagent report is not approval (`.claude/skills/ask-tui-design/SKILL.md`, "Hard rule").

## Context

- Decisions: D13/Q3 (cycling key in scope), D4 (problems shown in the TUI), M9 (scope affects the cycle list and the start model only).
- Read first, as the skill says: `docs/tui-architecture.md`, `internal/tui/README.md`, `docs/tui-style-guide.md`, and the earlier design `../261028-1310-account-model-discovery/design.md` (footer row with `provider/model` and thinking level).
- Keys in use: `ctrl+p` and `ctrl+n` move in lists inside the selector and the providers box (`internal/tui/selector.go:144`, `internal/tui/providers_box.go:148,164`). Pi uses `ctrl+p` to cycle models in the prompt. The design must name a key that does not collide in the prompt pane, and say what `ctrl+p` does in each state.
<!-- Red Team 2026-11-06: Finding 8, Finding 11 -->
- Data the design can use (phases 5 and 6): `ACPModelsResult.Scope` (cycle list with levels), `ACPModelsResult.Diagnostics`, `ACPModel.NotInConfig` (the current row left the user file), and `ACPModelRequest.ThinkingLevel` (one call switches the model and the level). If phase 6 changes these names or shapes, update the design and set it back to `draft` for a new approval.
- The existing settings problem notice at session start shows file problems already (`internal/tui/update_session.go:848`; `internal/tui/trust_dialog_test.go:489` `TestSettingsDiagnosticsNotice`).

## Questions the design must answer

1. Which key cycles forward and which key cycles back; is there a key when no scope exists (Pi cycles all models with valid auth when no scope exists)?
2. What the user sees after one press: the footer row change only, or also a notice line; what shows when the session is running (refused, as `setModel` refuses a busy session); what a press does while an earlier switch is still in flight (ignored, Finding 11). <!-- Red Team 2026-11-06: Finding 11 -->
3. Whether the thinking level of a scope entry (`:level`) applies when the key selects it.
4. How a problem from a reload on `/model` open shows (notice line, a row in the selector, or both), and how it differs from the start notice.
5. Narrow widths (under 60 columns), as the earlier design did.
6. How the footer and the selector show a current row with `notInConfig`, and the request error `model configuration changed; choose the model again`. <!-- Red Team 2026-11-06: Finding 8 -->
7. How the session notice of a dropped thinking level shows (M16). <!-- Red Team 2026-11-06: reasoning notice -->

## Files

| Action | Path | Notes |
|---|---|---|
| Create | `plans/261105-1645-h7-model-config-resolution/design.md` | Sketches at exact width and height, states, keys, acceptance criteria |

## Steps

1. Load `.claude/skills/ask-tui-design/SKILL.md` and follow it.
2. Write the design with `design-status: draft`.
3. Present it to the user and wait. Record each user answer in the design file.
4. On approval, set `design-status: approved`, `design-approved-by`, `design-approved-on`.

## Tests

This phase writes no code. The approval record in the `design.md` frontmatter is its test: `design-status: approved`, `design-approved-by`, `design-approved-on`.

## Validation

- `rg -n '^design-status: approved' plans/261105-1645-h7-model-config-resolution/design.md` matches before phase 8 starts.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| The approval takes longer than the other phases | Med x Low | Phases 5 and 6 do not wait for it; only phase 8 does |

## Rollback

Delete or set `design.md` back to `draft`. No code depends on it.
