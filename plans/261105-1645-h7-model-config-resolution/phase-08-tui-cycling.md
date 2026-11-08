---
phase: 8
title: "TUI model cycling and reload problems"
status: pending
priority: P2
effort: "1d"
dependencies: [6, 7]
---

# Phase 8: TUI model cycling and reload problems

## Goal

Build the approved `design.md`: the cycling key over `ACPModelsResult.Scope` and the display of `ACPModelsResult.Diagnostics`.

## Gate

Start only when `design.md` has `design-status: approved`. If the design cannot work as written, stop, set it to `draft`, explain the conflict, and wait (`.claude/skills/ask-tui-design/SKILL.md`).

## Context

- `/model` opens through `openModelSelector`, which asks the client for the list (`internal/tui/update_input.go:223,342-358`); the reply arrives in `onModels`.
- Model switch path: the client `SetModel` call, served by `setModel` (`internal/acp/ask_methods.go:226-285`), refuses a busy session.
- The TUI imports only `pkg/protocol`, `pkg/askui`, `internal/tuiext` and terminal libraries (`.golangci.yml:28-48`).
- Footer row and notices: the approved design of the account model discovery plan.

## Requirements

1. The key and its states follow `design.md` exactly.
2. The cycle list comes from the last models result; the key asks for the list when the TUI has none, so a press never acts on stale data older than the last `/model` or the session start.
<!-- Red Team 2026-11-06: Finding 11 -->
3. A press sends one `SetModel` call that carries the thinking level of the entry in `ACPModelRequest.ThinkingLevel` when the design says so, so the model and the level change under one guard. A refusal changes nothing on the screen except the notice that the design names.
4. A press while an earlier switch is in flight is ignored; the next press after the reply acts on the new current row.
5. Diagnostics from a models result, a `notInConfig` current row and a dropped-level notice show as the design says, once per distinct problem set. <!-- Red Team 2026-11-06: Finding 8 -->

## Files

| Action | Path | Notes |
|---|---|---|
| Modify | `internal/tui/update_input.go`, `internal/tui/session.go` (or the files the design names) | Key and state |
| Modify | `internal/tui/client.go` (the `tui.Client` interface) only if a new call is needed | Prefer the existing `Models` and `SetModel` |
| Modify | `internal/tuiclient/*.go` | Only to pass the new result fields |
| Create | `internal/tui/model_cycle_test.go` | Golden and update tests |
| Modify | `internal/tui/README.md`, `docs/cli.md` keys table | The new key |

## Steps

1. Read the approved `design.md` and list each state and key as a test name first.
2. Pass `Scope` and `Diagnostics` through `internal/tuiclient` to the `tui.Client` result type (`internal/tui/client.go:108`).
3. Add the key handling and the state in the files the design names; render with the existing footer and notice helpers.
4. Write the golden frames from the design sketches, then the update tests.
5. Update `internal/tui/README.md` and the keys table in `docs/cli.md`.

## Tests

- Golden frames for each sketch of `design.md` at its exact size.
- `TestModelCycleKeyMovesThroughScope`, `TestModelCycleKeyRefusedWhileRunning`, `TestModelCycleKeyWithoutScope`, `TestModelCycleKeyIgnoredWhileSwitching`, `TestModelsReloadProblemShownOnce` (names follow the design states). <!-- Red Team 2026-11-06: Finding 11 -->
- One real-terminal case in `e2e/tui` when the design adds a key (`python3 e2e/tui/run.py --case <name>`), by hand, as the repository describes.

```
go test ./internal/tui -run 'TestModelCycle|TestModelsReload' -count=1
go test ./internal/tui ./internal/tuiclient -count=1
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/tui/... ./internal/tuiclient/...
```

## Validation

- Each acceptance criterion of `design.md` has a test or an e2e case.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| The key collides with a list key | Med x Med | The design answers it before code (phase 7 question 1) |
| Golden drift in unrelated frames | Low x Low | Run the whole `internal/tui` suite |

## Rollback

Revert the commit. The host fields stay and an older TUI ignores them.

## Implementation checklist (2026-11-07)

- [x] `tui.Client.SetModel` takes the thinking level and sends it in `ACPModelRequest.ThinkingLevel` (`internal/tuiclient`, test `TestClientSetModelSendsThinkingLevel`). The `cmd/tui` e2e callers pass `""`.
- [x] Keys `ctrl+p` / `ctrl+shift+p` in `onShortcut`; state and notices in `internal/tui/model_cycle.go`.
- [x] Cycle list: `Scope`, else the `/model` rows without `NotInConfig`; the list of another generation is read again first.
- [x] Busy, in-flight, nothing-to-cycle, failure, dropped-level and resize states.
- [x] `/model`: summary row, `not in models.json` / `changed`, same-id choice of a changed row sends `SetModel`.
- [x] Pane block `Problems in models.json, read again when /model opened`, once per set; no path row for a problem without a path.
- [x] `MODEL_CONFIG` error block next action; `LevelDropped` start notice.
- [x] Goldens and tests: `internal/tui/model_cycle_test.go`, subtests in `TestSettingsDiagnosticsNotice`.
- [x] `internal/tui/README.md`.
- [x] `docs/cli.md` keys table and `CHANGELOG.md` line (done in phase 9, 2026-11-07).
- [x] e2e case `ask-model-cycle` (added in phase 9, 2026-11-07; pass on Alacritty and Ghostty).
