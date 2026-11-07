---
phase: 6
title: "CLI refresh, footer layout and TUI unknown values"
status: completed
priority: P2
effort: "2.5d"
dependencies: [5]
---

# Phase 6: CLI refresh, footer layout and TUI unknown values

<!-- Updated: user decision 2026-10-28 - status data moves back to a footer row; one design covers the layout and the new states -->

<!-- Updated: Red Team 2026-10-28 - U3 form confirmed by user; header.go not status.go; signal handling; no cost column -->

## Goal

Move the status data (link, working directory, model, thinking level, context use; no cost) from the header row back to a footer row at the bottom of the screen, as the approved design file says.

Add `ask models refresh`. Show `?` for an unknown context window in the `/model` selector and the footer row (U3 as changed by U9: no cost is shown).

## Context

- **CLI dispatch:**
  - `cmd/tui/headless.go:81` (auth), `:119` (acp), `:122` (connect), `:126` (leader), `:129` (version), then `parseArgs`.
  - Signal handling of the `auth` branch: `:100-115`.
  - `docs/cli.md` owns subcommands and exit codes.
- **TUI:**
  - `internal/tui/selector.go:320-326,394-405`. `formatWindow` returns `""` for 0.
  - Header: `internal/tui/header.go:59-64,131,149`.
  - Usage path: `internal/tui/update_session.go:818`, `pkg/protocol/acp.go:245-250` (`ACPUsageResult.Complete`).
- **Rule:** a visible TUI change needs a design file from the `ask-tui-design` skill. The user approves the file before any code.

## Files

- Create `cmd/tui/models.go` for `ask models refresh [--provider P]`.
  - It starts no leader and no database. It uses `internal/app.NewNativeAuth` and the refresher.
  - It prints one line per provider: `<provider>  <n> models  <known|unknown|no credential>`, plus `metadata  <updated|not modified|failed>`.
  - It uses the same signal handling as `ask auth`.
  - Exit codes: 0 when the command ran (also when a provider is `unknown`); 1 on a local error, such as a cache write that fails; 130, 143 and 129 on signals.
- Create `cmd/tui/models_test.go`.
- Create `plans/261028-1310-account-model-discovery/design.md`, made with `ask-tui-design`.
- Modify `internal/tui/selector.go` and `internal/tui/header.go`, and their goldens in `internal/tui/testdata/`.
- The footer row shows no session cost (user decision 2026-10-28), so the usage path does not change.

## Steps

1. Write the TUI design file with `ask-tui-design`. It covers:
   - the selector row with `?` for the context window;
   - the footer row with `?` for the context window;
   - a row marked "not in account list";
   - widths of 80 and 40 columns.

   **Hard user gate:** stop until the user approves the design. `--auto` does not skip this gate, because the project rule in `.claude/skills/ask-tui-design` requires approval before TUI code. The CLI part (steps 2 and the `cmd/tui/models.go` files) may run before the approval.
2. Tests for `ask models refresh`. Use a temp `ASK_HOME`, fake HTTP servers, a provider with no credential, a provider that is `unknown`, and a cache write error.
3. Change the TUI as the approved design says. Update the goldens with `go test ./internal/tui -run TestScreenGoldens -update`, and review the diff.

## Verification

- `go test ./cmd/tui/... ./internal/tui/... ./pkg/protocol/...` passes.
- With `OPENAI_API_KEY` set, `ASK_HOME=$(mktemp -d) go run ./cmd/tui models refresh` prints a count above 1 for openai.
