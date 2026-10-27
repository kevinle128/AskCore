---
phase: 4
title: "Print mode waits for background work"
status: completed
priority: P2
effort: "3.5d"
dependencies: [2, 3]
---

# Phase 4: Print mode waits for background work

## Goal

`ask -p` waits by signal for the background jobs that its runs started, answers once per completion, stops any job at its print-mode limit with a notice that wakes one more turn, stops the whole run at `--max-turns`, and still exits at once with 130 or 143 on a signal.

Deep mode: scout first, then ask the open items below one at a time before you write code.

## Known facts (verified on `ee53317`)

- Today (H5 N2): after the last prompt, print mode prints, `Dispose` closes the job table and `KillRemaining` sweeps (`cmd/tui/headless.go:185-188, 401-421`).
- The `done` branch of `runHeadless` does not read `sigs`; the run context is cancelled only on output failure (`cmd/tui/headless.go:389-394, 401-421`). A wait placed after `promptAll` returns would ignore Ctrl-C and SIGTERM.
- `Agent.WaitForQuiescence` waits on the task registry and the Agent by signal and compares a work counter before and after (`internal/agent/tasks.go:184-221`). The registry stays active until its hand-off returns (`internal/tasks/runner.go:74-77`, `registry.go:68`).
- The job table closes `j.done` and releases `t.mu` before `sendNotice` calls the Agent (`internal/jobs/table.go:420-430`). A hook that reports "no running job" would turn idle before the notice is queued.
- `Agent.Idle()` signals only the busy-to-idle edge (`internal/agent/idle.go:33-46`). Print mode already subscribes to Agent events for `--mode json` (`cmd/tui/headless.go:393-396`).
- Monitors default to 10 h (`internal/jobs/monitor.go:33-34`, `internal/tools/shell_monitor.go:17`); every event wakes a turn (`internal/agent/tasks.go:124-126`); the flood guard allows a burst of 10, then one every 2 s (`monitor.go:28-29`).
- The `bash` description tells the model "Background jobs end when the session closes, and when `ask -p` ends." (`internal/tools/shell_bash.go:76`). One declaration serves every mode (`cmd/tui/headless.go:233-238`). The text is in all six cassettes, and replay compares the whole request body (`internal/providers/cassette/cassette.go:208-210`). `docs/reference/tools.md:13` has the same text and a drift test.
- The composition root hard-codes `Clock: sandbox.SystemClock` (`cmd/tui/headless.go:296`).

## Design

- **Quiescence hook.** The Agent config gets one hook for outside background work: an idle channel, a work counter and a count by kind (bash, monitor). `WaitForQuiescence` reads it under `a.mu` and waits on it outside the lock, as it does for `reg.Idle()`.
- **Hand-off invariant.** A job counts as outside work from `launch` until its owner's `Notify` returns, or until its notice is suppressed (awaited, killed by the model, table closing). The count goes down, and the idle channel closes, only after that point. Lock order: `t.mu` is never held while the table calls into the Agent (`Notify`, `Remove`, the work counter).
- **Signals.** The wait runs inside the prompt goroutine under a context that the signal branch cancels, so SIGINT gives 130 and SIGTERM 143 at any time during the wait, followed by `Dispose` and the `KillRemaining` sweep.
- **Limit.** The print-mode background limit applies to every job kind, monitors included: the effective limit is the smaller of the job's own timeout and the print limit (10 min default, up to 2 h). The leader and the in-process TUI keep no limit. At the limit the job group is stopped and the notice says `Background command "<label>" was stopped after reaching its background time limit`; it wakes one more turn.
- **Idle-wait cap** `ASK_PRINT_BG_WAIT_CEILING_MS` (default 10 min, 0 = none): armed on `agent_settled` and stopped on `agent_start` from an Agent listener; when it fires while the Agent is idle and only non-bash work remains (from the count by kind), it cancels the wait context. It never cancels during a run.
- **Kill switch** `ASK_DISABLE_BACKGROUND_TASKS=1` (design 14.1: "a command stops at its timeout"): a `bash` command stops at its wait budget instead of moving to the background. What `run_in_background` and `monitor` do under the switch is confirmed in the scout against design 14.1 and Claude Code, and asked if it is not clear.
- **Test seam** `ASK_PRINT_BG_LIMIT_MS` overrides the print limit, so real `ask -p` tests use a short limit through the real entry point.
- **Progress line** after 5 s on stderr, only when stderr is a terminal and, for `--mode json`, only when stdout is not a terminal: `Waiting for background work to finish` with the job labels (as Claude Code).
- **No total time ceiling** (user decision D-R5b, 2026-10-26, as Claude Code): the run ends when no work is left, at the idle-wait cap, on a signal, or at `--max-turns`.
- **`--max-turns N`** (user decision, 2026-10-26): counts every turn of the whole `ask -p` run, prompt turns and turns that a notice wakes alike (one turn per completed model answer with its tool calls; retry attempts inside a turn do not count). Default: no limit. When the count reaches N, print mode aborts the active run, stops the background jobs, prints `Error: Reached max turns (N)` and exits with code 1. The message follows Claude Code (`cli/print.ts:947-948`); the scope differs on purpose: Claude Code resets its count for every query (`query.ts:276`), so its flag does not bound the chain of notice-woken turns. Count from the Agent `turn_end` events in the print-mode listener; the scout confirms which event marks one durable turn. `--max-budget-usd` is a later plan.
- **Reference** (Claude Code docs, "Background tasks at exit"): a background command keeps the wait open and the idle cap does not end it; a monitor waits until it times out or the cap ends the wait; the cap is 10 min of continuous idle waiting (`CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS`, 0 = none) and drops partial results.
- **Output.** `--mode text` prints the result of each extra turn; `--mode json` already streams every event.
- **Description and cassettes.** Rewrite the `bash` description so it is true in every mode (decide the wording with open item 1). Re-record the six cassettes once with `ASK_RECORD=1` and the provider key from the environment (`docs/testing-llm-cassettes.md`), after open item 1 is decided. Regenerate `docs/reference`.

## Open items (ask one at a time at phase start)

1. Decided (user, 2026-10-26, P4-1 = A, as Claude Code): reuse `timeout`. With `run_in_background: true` in `ask -p` it sets the background limit (default 10 min, up to 2 h); a foreground `timeout` stays the wait budget; a command moved to the background gets the default limit from the move; the TUI and the leader have no limit and ignore it. One declaration for every mode; only the description text changes.
2. Decided (user, 2026-10-26, P4-2 = A, as Claude Code): in `ask -p` every monitor, `persistent` included, gets `min(timeout_ms, print limit)`; at the deadline it stops with one notice that wakes a turn.

## Existing tests that change

| Test | Why it breaks under S2 | Change |
|---|---|---|
| `TestPrintBashMovesToBackgroundAfterTimeout` (`cmd/tui/tools_e2e_test.go:970`) | Leaves `sleep 300` running; waits 5 min, then an unscripted request | End the job in the script (`job_kill` step) or set `ASK_PRINT_BG_LIMIT_MS` and script the extra turn |
| `TestPrintBashRunInBackgroundReturnsJobID` (`:988`) | Same | Same |
| `TestPrintEleventhJobIsRefused` (`:1136`) | Nine `sleep 300` jobs and one monitor stay running | Short limit through the seam, extra turns scripted |
| `TestPrintBackgroundBashReportsClampedTimeout` (`cmd/tui/tools_timeout_review_test.go:25`) | An endless loop waits the full limit | Short limit through the seam |
| `TestPrintEndDisposesAgentAndKillsBackgroundJobs` (`cmd/tui/native_agent_test.go:336`) | Asserts the N2 contract (group gone when `ask -p` returns) | Asserts the new contract: group gone after the wait or the limit |
| `TestPrintExitKillsBackgroundJobs` (`cmd/tui/tools_e2e_test.go:1523`) | Same | Same |
| Six cassette tests | Tool declaration changes | Re-recorded once |

## Files to modify (expected)

- `internal/agent/types.go`, `internal/agent/tasks.go` (quiescence hook)
- `internal/jobs/table.go`, `internal/jobs/notice.go`, `internal/jobs/monitor.go` (hand-off accounting, limit, limit notice, count by kind)
- `internal/tools/shell_bash.go`, `internal/tools/shell_monitor.go` (description, parameter, kill switch)
- `cmd/tui/headless.go`, `cmd/tui/headless_print.go` (wait inside the prompt goroutine, cap listener, progress line, extra-turn output, env seams, turn counter)
- `cmd/tui/args.go` (`--max-turns` flag and usage text), `AGENTS.md` (command line)
- `cmd/tui/testdata/cassettes/*.yaml` (re-recorded), `docs/reference/` (regenerated)
- The tests in the table above

## Test matrix (write first)

| Test | Pass condition |
|---|---|
| `TestQuiescenceWaitsForJobHandOff` (`internal/agent` + `internal/jobs`) | A fake owner blocks `Notify` after `close(done)`; `WaitForQuiescence` does not return until the hand-off returns |
| `TestPrintWaitsForBackgroundJobAndAnswersAgain` | `ask -p` with a background job prints the first answer, waits, then prints the answer to the completion notice; exit 0 |
| `TestPrintBackgroundLimitStopsJobAndWakesTurn` | With `ASK_PRINT_BG_LIMIT_MS` short, a job past its limit is stopped; the notice text matches; one more turn runs |
| `TestPrintMonitorStopsAtBackgroundLimit` | A monitor that emits forever cannot keep `ask -p` past the print limit |
| `TestPrintIdleCeilingEndsWait` | With only monitor work left, the cap ends the wait while idle and the sweep kills what remains; the cap never fires during a run |
| `TestPrintSignalDuringBackgroundWaitExits130` and `...Exits143` | SIGINT or SIGTERM during the wait exits at once with 130 or 143, and the process groups are gone |
| `TestDisableBackgroundTasks` | With `ASK_DISABLE_BACKGROUND_TASKS=1`, a `bash` call stops at its wait budget and does not move to the background |
| `TestLeaderJobsHaveNoBackgroundLimit` | A leader session job keeps running past the print-mode limit |
| `TestPrintMaxTurnsCountsWokenTurns` | `--max-turns 2`: one prompt turn and one notice-woken turn reach the limit; the background job is stopped; stdout has `Error: Reached max turns (2)`; exit 1 |
| `TestPrintMaxTurnsUnsetIsUnlimited` | Without the flag, a run with several woken turns ends normally with exit 0 |
| `TestParseMaxTurns` (`cmd/tui`) | `--max-turns` accepts a positive integer; 0, negative and non-numeric values are refused with a usage error |

Real-process tests use the env seams and a short real limit; they do not use `testing/synctest`. New tests wait on signals; existing `time.Sleep(settle)` uses in `cmd/tui/tools_e2e_test.go` are recorded as known debt, not extended.

## Verification

```sh
go test -race -count=1 ./internal/agent/... ./internal/jobs/... ./internal/tools/...
go test -count=1 ./cmd/tui/...
go test -count=1 ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

Pass: all commands exit 0, including the re-recorded cassettes and the reference drift test.

## Rollback

Revert the phase commits (cassettes included); print mode returns to the N2 kill-at-end behavior.

## Result (2026-10-27)

- `jobs.Options.BackgroundLimit` (`Default`, `Max`) stops a background job with `killReason` "background time limit" and a notice that names the limit. The limit counts from a background start or from the move of a foreground job. Zero (TUI, leader, `ask acp`) means no limit.
- `jobs.Options.DisableBackground` (`ASK_DISABLE_BACKGROUND_TASKS=1`): a foreground command stops at its wait budget; a background start or a monitor is refused with `ErrBackgroundDisabled`.
- `Table.Idle()` closes when every job has handed off its completion notice; `agent.Config.Background` makes `WaitForQuiescence` wait for it, so a quiescence check never sees the end of a job before its notice is queued.
- In print mode a monitor deadline is capped at the default limit (10 min), persistent monitors too.
- `ask -p` prints the first reply, then waits by signal (`WaitForQuiescence`) for the background work and prints the reply of each run that a notice wakes. The idle-wait cap (`ASK_PRINT_BG_WAIT_CEILING_MS`, default 10 min) runs only while the Agent is idle and no bash job runs. A progress line goes to stderr after 5 s when stderr is a terminal.
- `bash` with `run_in_background`: in print mode `timeout` sets the background limit (default 10 min, at most 2 h; `ASK_PRINT_BG_LIMIT_MS` overrides the default). A foreground `timeout` stays the wait budget. The `bash` and `monitor` descriptions say this; the six cassettes were re-recorded once and `docs/reference/tools.md` regenerated.
- `--max-turns N` counts every durable turn of the whole run, the woken ones too; a retried attempt does not count. When the limit is hit, stdout gets `Error: Reached max turns (N)` and the exit code is 1.
- Signals during the wait: SIGINT exits 130 and SIGTERM 143; the Agent is disposed and its job groups are killed; no "Request was aborted" line is printed.
- Test names that differ from the matrix: the quiescence hand-off is `TestTableIdleWaitsForNoticeHandOff` (`jobs`) and `TestQuiescenceWaitsForBackgroundWork` (`agent`); the bash limit and its notice are `TestBackgroundLimitStopsJobWithNotice`, `TestRequestedBackgroundLimitIsCappedAtMax` and `TestMovedJobGetsDefaultLimitFromTheMove` (`jobs`); the leader case is `TestNativeSessionHasNoBackgroundLimit`; the two signal cases are subtests of `TestPrintSignalDuringBackgroundWait`; "max turns unset" is covered by `TestPrintWaitsForBackgroundJobAndAnswersAgain` (woken turn, no flag, exit 0).
- Found during verification: `TestJobEndingAtBudgetIsReportedOnce` failed once under heavy machine load. A background job could end and start its notice before `Wait` took the lock; `Wait` then returned `Done` as well. This race was older than this phase; production waits only on foreground jobs, which cannot hit it. `Wait` now returns `Background` when the notice already started (`TestWaitAfterNoticeStartedDoesNotReportAgain` fails without the fix).
- Known debt: the existing `time.Sleep(settle)` uses in `cmd/tui/tools_e2e_test.go` stay; new tests wait on signals.

## Code review (2026-10-27)

A code review of the print-mode layer and the job limits found no problem in the job table locking, the busy counter or the notice hand-off. It found these issues, all fixed:

- A SIGTERM could hang `ask -p` when stdout was not read: the reply printer held its lock while a notice reply waited for the output lock, and the signal path waited for the printer lock. The printer is now disarmed without a lock and writes without holding its lock (`TestPrintSignalWhileReplyWriteBlocks`; the old code hangs).
- The reply of the last prompt was read from the Agent state after the prompts returned, so a notice run that started in between took its place. The printer now prints each reply at the `agent_settled` of its own run; the last prompt reports its run ID through `agent.WithRunStart` (`TestPrintNoticeRightAfterLastPromptKeepsPromptReply`; the old code fails 10 of 10).
- The idle-wait cap was armed only when the Agent was idle at the start of the wait, and it could end a run that had just started. It now always arms, and at expiry it checks that the Agent is idle; if not, it starts again when the Agent becomes idle.
- A normal exit now disarms the printer before `Dispose`, so a run that `Dispose` aborts prints nothing.
- A very large or tiny `timeout` with `run_in_background` is clamped before the conversion to a duration.
- The `Background` wait outcome documents the case of a job whose notice already reports its end.
- `TestPrintEleventhJobIsRefused` uses a 10 s background limit, so no job stops before the eleventh call on a loaded machine.

Kept as is: the exit code of print mode is the code of the last printed reply, as `docs/cli.md` says and as Claude Code reports its final result.
