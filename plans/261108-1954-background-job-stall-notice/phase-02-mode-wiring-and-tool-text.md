---
phase: 2
title: "Wire both settings in every mode"
status: pending
effort: 2h
---

# Phase 2: Wire both settings in every mode

## Context

- Every mode builds its job table in `newNativeAgentOn` (`cmd/tui/headless.go:443`, `jobs.New` at `:463-481`). The architecture reference names `cmd/tui/headless.go` as "the one place all entry points share" (`docs/ask-architecture-reference.md:390`). Print mode reaches it through `newPrintAgent` (`headless.go:310-315`). The leader (`cmd/tui/leader.go:260`), the in-process TUI (`cmd/tui/tui_client.go:104`), and `ask acp` (`cmd/tui/acp.go:102`) reach it through `acpParams` (`acp.go:159-194`), whose `NewAgent` closure calls `newNativeAgentOn` (`acp.go:190-193`).
- Print mode sets its limit at `headless.go:191` with `printBackgroundLimit` (`cmd/tui/headless_wait.go:45-52`). `acpParams` builds `options` at `acp.go:160-166` and sets no `bgLimit`; the comment at `cmd/tui/args.go:58-60` says only print mode sets it.
- Existing millisecond env parser: `printWaitCeiling` (`headless_wait.go:54-66`): empty, negative, or invalid gives the default; `0` means none. These are the exact rules that `ASK_BG_STALL_MS` and `ASK_BG_LIMIT_MS` need.
- `ASK_DISABLE_BACKGROUND_TASKS` is read at `headless.go:469`; env names live in `headless_wait.go:19-28`.
- Bash tool text: `internal/tools/shell_bash.go:73-83` (limit only "in a non-interactive run"), guideline at `:91-93`, comment at `:114-116`. Hand-written tools README: `internal/tools/README.md:56`, `:62`.
- `TestNativeSessionHasNoBackgroundLimit` (`cmd/tui/print_wait_test.go:183-189`) goes through `newNativeAgent`, not `acpParams`, so it proves nothing for the interactive path.

## Decisions (accepted)

- Interactive limit: `jobs.BackgroundLimit{Default: 2h, Max: 2h, BashOnly: true}`. Max = 2 h is the print-mode cap (`printBgMax`, `headless_wait.go:30`). `BashOnly` keeps the monitor contract (10 h default, `persistent` until session end) in interactive sessions.
- `ASK_BG_LIMIT_MS` (leader, `ask acp`, in-process TUI): a whole number above 0 sets `Default` and raises `Max` to at least that value. `0` turns the interactive limit off. Empty, negative, or invalid gives 2 h. `ASK_PRINT_BG_LIMIT_MS` ignores values <= 0; the docs state the difference. `ask -p` does not read `ASK_BG_LIMIT_MS`.
- `ASK_BG_STALL_MS` (all modes): whole number >= 0; `0` off; empty, negative, or invalid gives 5 min.

## Design (no second mechanism)

- One env parser: rename `printWaitCeiling` to `envMillis(getenv, name string, def time.Duration) time.Duration` with the same body. `printWaitCeiling` call site (`headless.go:210`) becomes `envMillis(deps.getenv, printBgCeilingEnv, printCeiling)`. The stall value is `envMillis(getenv, bgStallEnv, bgStallDefault)`. The interactive limit is `d := envMillis(getenv, bgLimitEnv, interactiveBgLimit)`, then `BackgroundLimit{Default: d, Max: max(interactiveBgLimit, d), BashOnly: true}` (zero when `d == 0`). `printBackgroundLimit` stays as it is.
- The interactive limit is set once in `acpParams` on the `options` value that the `NewAgent` closure captures. The stall value is set once in `newNativeAgentOn`, next to `DisableBackground`. Both reuse the existing `options.bgLimit` field and `jobs.Options`; no new composition path.

## Files

- Modify: `cmd/tui/headless_wait.go` (env constants `bgStallEnv = "ASK_BG_STALL_MS"`, `bgLimitEnv = "ASK_BG_LIMIT_MS"`, defaults `bgStallDefault = 5 * time.Minute`, `interactiveBgLimit = 2 * time.Hour`; rename `printWaitCeiling` to `envMillis`).
- Modify: `cmd/tui/acp.go` (lines 160-166: set `o.bgLimit`).
- Modify: `cmd/tui/headless.go` (line 210: `envMillis` call; line 469 area: `StallAfter: envMillis(getenv, bgStallEnv, bgStallDefault)`).
- Modify: `cmd/tui/args.go` (comment at lines 58-60).
- Modify: `cmd/tui/print_wait_test.go` (replace `TestNativeSessionHasNoBackgroundLimit`; add stall tests). No test calls `printWaitCeiling` today, so the rename touches no test.
- Modify: `internal/tools/shell_bash.go` (description, `timeout` parameter text, guideline, comment at `:114-116`).
- Modify: `internal/tools/README.md:56` (limit in every session, interactive 2 h; the stall notice) and `:62` (the monitor cap applies to `ask -p` only).
- Regenerate: `docs/reference/tools.md` through the drift test.

## Steps

1. Rename the env parser and add the constants.
2. Set the interactive limit in `acpParams`. To make it testable, move the `options` build of `acp.go:160-166` into `sessionOptions(deps runDependencies) options` (unexported); `acpParams` calls it.
3. Set `StallAfter` in `newNativeAgentOn`. It covers print mode too.
4. Bash tool text. Proposed: "A background job stops after a time limit (by default 2 hours; 10 minutes in a non-interactive run); with run_in_background, `timeout` sets that limit, up to 2 hours. When a background job prints no output for 5 minutes, a notice says so; the job still runs." Change the `timeout` parameter text to name both defaults. Keep the 600 s caps of `bash` and `job_output`.
5. Fix the comments in `args.go` and `shell_bash.go:114-116`; update `internal/tools/README.md`.
6. Run `ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui`.

## Tests

- `TestEnvMillis`: table test (unset, valid, `0`, negative, text) for the renamed parser; it keeps the old ceiling cases.
- `TestSessionOptionsSetInteractiveLimit`: `sessionOptions(deps).bgLimit == {2h, 2h, BashOnly: true}`; with `ASK_BG_LIMIT_MS=0` it is the zero value; with `ASK_BG_LIMIT_MS=10800000` it is `{3h, 3h, true}`. Replaces `TestNativeSessionHasNoBackgroundLimit`.
- `TestPrintStallNoticeReachesModel`: pattern of `TestPrintWaitsForBackgroundJobAndAnswersAgain` (`print_wait_test.go:21-35`) with `ASK_BG_STALL_MS=200` and `sleep 1; echo late`. The model gets `printed no output for 200ms` before the completion notice.
- `TestPrintStallOffWithZero`: `ASK_BG_STALL_MS=0`; only the completion notice.

```bash
go test -run 'EnvMillis|SessionOptions|PrintStall|PrintWaits|PrintMonitor|PrintIdleCeiling' ./cmd/tui/
ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui
go test ./cmd/tui/ ./internal/tools/
go vet ./cmd/tui/ ./internal/tools/
```

## Risks

| Risk | L x I | Mitigation |
|------|-------|-----------|
| A long dev server in the TUI stops after 2 h | M x M | The limit notice tells the model; `ASK_BG_LIMIT_MS=0` turns it off; documented in phase 3. |
| A stall notice wakes an idle agent and costs one model run | H x L | User decision. One notice for each silence period. |
| `ask -p` gets a stall notice before its 10 min limit and prints one more reply | M x L | Expected; `--max-turns` counts the woken turn as today. |

## Rollback

Revert the commit, then regenerate `docs/reference/tools.md` with the drift test if phase 1 stays.
