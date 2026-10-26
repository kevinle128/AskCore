---
phase: 5
title: "Design records and docs"
status: completed
priority: P2
effort: "0.5d"
dependencies: [1, 2, 3, 4]
---

# Phase 5: Design records and docs

## Goal

The ADRs, the design doc, the H5 decision record, the package READMEs and the changelog describe the code as it is after phases 1 to 4.

Each earlier phase already updates the README of the packages it changes. This phase covers the cross-cutting records.

## Files to modify

- Create `docs/adr/0005-shell-jobs-use-the-notify-port.md` (status: accepted): jobs are the first outside source of the notify port; the blocking `job_output` wait is not the rejected poll tool of ADR 0004; reset scoping (S3); print-mode wait (S2). Link the decision report.
- Modify `docs/adr/0004-agent-owned-async-task-runtime.md`: one line under the decision that points to ADR 0005 for shell work.
- Modify `docs/async-task-runtime.md`: sections 4 (sources), 11 and 11.1 (shell work is the H5 job table; no `task_stop` for shell), 12.2 (both sites done), 14 and 14.1 (implemented contract and parameter name), 17 (phase A3 rewritten as this plan; A2 done).
- Modify `plans/architectures/h5-builtin-tools.md`: supersession notes for the Q11 consequence (completion now by `Notify` at the next turn boundary), N2 (print mode now waits), and Q9 (print mode only gets a background limit).
- Modify `internal/jobs/README.md`, `internal/agent/README.md`, `internal/tools/README.md`, `internal/tui/README.md` only if the earlier phases left a gap.
- Modify `AGENTS.md` if a command or environment variable changed (`ASK_PRINT_BG_WAIT_CEILING_MS`, the print-mode wait).
- Modify `CHANGELOG.md` under `[Unreleased]` with the `ask-docs-sync` skill.

## Tasks & steps

1. Run the `ask-docs-sync` skill on the diff of phases 1 to 4.
2. Write ADR 0005 and the pointer in ADR 0004.
3. Update the design doc sections and the H5 supersession notes.
4. Run the reference drift test and the link check of the docs.

## Verification

```sh
go test -count=1 ./... 
rg -n "when H5 lands|SetNotifier|FollowUp\(" docs internal/jobs/README.md internal/tools/README.md
```

Pass: tests exit 0 (the reference drift test included), and the `rg` command prints nothing.

## Result (2026-10-27)

- New `docs/adr/0005-shell-jobs-use-the-notify-port.md`; ADR 0004 points to it; `docs/README.md` now lists both ADRs and `docs/async-task-runtime.md`.
- `docs/async-task-runtime.md`: sections 4, 5, 7.3, 11, 11.1, 14, 14.1 and 17 describe the built state. The A2 stdio watchdog ticker and the gRPC demo sleep are still open and say so. The `sleep` and `cd` rules of section 11.1 are recorded as not built.
- H5 records: supersession notes on Q9, Q11, Q13 and the N3 monitor limit (`plans/architectures/h5-builtin-tools.md`) and on N2 (`plans/261010-2349-h5-builtin-tools/plan.md`).
- The `ask-docs-sync` pass updated `docs/cli.md` (new flag, three env vars, a "Background work in print mode" section), `AGENTS.md`, the `jobs` and `tools` READMEs, and the `[Unreleased]` changelog.
- Found during the docs pass and fixed: with `--mode json` the max-turns line went to stdout and broke the JSON event stream; it now goes to stderr (`TestPrintMaxTurnsJSONKeepsStdoutJSON`). `jobs.ErrNoNotifier` is renamed `ErrNoOwner`, and the package comment names the owner instead of the removed notifier.
- Not fixed, outside this plan: two ADR files share the number 0004.
