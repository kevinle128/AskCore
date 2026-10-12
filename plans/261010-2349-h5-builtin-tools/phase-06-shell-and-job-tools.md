---
phase: 6
title: "Shell and job tools"
status: completed
priority: P1
effort: "1.5d"
dependencies: [5]
---

# Phase 6: Shell and job tools

## Context

- Architecture: [Jobs (`jobs`)](../architectures/h5-builtin-tools-architecture.md#jobs-jobs) (every `bash` call is a job; `timeout` is the wait budget; tools `job_output`, `job_kill`, `job_list`, `monitor`), [Boundaries & contracts](../architectures/h5-builtin-tools-architecture.md#boundaries--contracts) (D19 reading, result contract), [Spike results](../architectures/h5-builtin-tools-architecture.md#spike-results-2026-10-10) (`bash` and `job_output` descriptions say a notice will arrive).
- Decisions: [Q2](../architectures/h5-builtin-tools.md#q2-bash-output-truncation), [Q4](../architectures/h5-builtin-tools.md#q4-long-running-commands) (trailing `&` rejected with a hint), [Q9](../architectures/h5-builtin-tools.md#q9-wait-budget-before-a-command-moves-to-the-background), [Q10](../architectures/h5-builtin-tools.md#q10-monitor-shape), [N3 monitor contract](../architectures/h5-builtin-tools.md#n3-monitor-contract-user-2026-10-11) (Grok's shape). Grok source: `grok-build/crates/codegen/xai-grok-tools/src/implementations/grok_build/monitor/types.rs:1-110`, `tool.rs:29-36`.
- xia H5: ["What to take"](../reports/xia-261010-1727-h5-builtin-tools.md#what-to-take-no-conflict-with-the-contract) row `bash` (`exit: N` header, partial updates at most one per 50 ms, leading `cd X &&` noted), row `prompts`.
- Spike 1: [Surprises](../reports/spike-261010-system-reminder-notice.md#surprises) and [gpt-5.6-sol polling](../reports/spike-261010-system-reminder-notice.md#gpt-56-sol-polling).
- Inventory: H-TOOL-04 `bash`, H-TOOL-11 snippets ([inventory-harness.md](../260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md) lines 54, 61). Pi source `packages/coding-agent/src/core/tools/bash.ts:22-64,258,355,393` (commit `4c6fb7cfe`).

## Overview

Add the thin tools over the job table: `bash`, `job_output`, `job_kill`, `job_list` and `monitor`. They validate arguments, call `jobs`, and shape results (model text separate from UI details).

## Requirements

- Functional:
  - `bash(command, timeout?, run_in_background?)`. `timeout` in seconds is the wait budget (Q9): default 120, values above 600 are clamped to 600 and the result says so, `0` or negative is an argument error. `run_in_background: true` starts the job and returns at once. A command whose trimmed text ends with a single `&` (not `&&`) is refused before start: `Do not end the command with &. Set run_in_background to true to run it in the background.`
  - Foreground result (job done inside the budget): text `exit: N` (or `signal: SIGKILL`) on the first line, then `sandbox.View` of the output (head 20 KB + tail 30 KB inside 2000 lines and 50 KB, Q2), then the spill line when present. Non-zero exit or a signal gives `IsError=true` (H-TOOL-04). Empty output gives `(no output)`.
  - Moved to background: `Command still running after <N> s. It continues as background job bash-1. A notice will arrive when it finishes; do not poll job_output. Use job_kill to stop it.` plus the output so far (view). `IsError=false`.
  - `run_in_background`: `Started background job bash-1. A notice will arrive when it finishes; do not poll job_output.`
  - Cancelled call (`ctx` done): the table kills and waits (`jobs.Cancelled`); the body returns the error result `Command aborted` with the output so far (D19: the body returns promptly after the group is gone).
  - Partial updates: while it waits in the foreground, `bash` sends `tc.Update` with the current view at most once per 50 ms (Pi live updates); none after the move to the background.
  - Leading `cd <dir> &&`: noted in details (`cwd` field) only; the command runs as given in the session cwd (omp note; no rewrite).
  - Description (prompt text): Pi's `bash` description plus the background contract: commands run in the session cwd with a non-interactive environment, long commands move to the background after `timeout` seconds, a notice arrives when a background job finishes, do not poll, background jobs end when the session closes (and when `ask -p` ends, N2).
  - `job_output(job_id, wait?, timeout_ms?)`: calls `jobs.Read`; text = new output (view when over 50 KB) + status line; for a running job without `wait`, the "do not poll" line. Unknown id: `No job with id <id>. Use job_list.` `timeout_ms` default 30000, cap 600000 (P16).
  - `job_kill(job_id, reason?)`: calls `jobs.Kill`; text `Job bash-1 killed.` + last output view + final status line.
  - `job_list()`: one line per job `bash-1 [bash] running 12s - go test ./...`; `No jobs.` when empty.
  - `monitor(command, description, timeout_ms?, persistent?)` (N3, Grok's shape; no pattern parameter). Validation: `timeout_ms` default 36,000,000 (10 h), max 36,000,000; when `persistent` is false and `timeout_ms` is above the max, the result is the error `persistent must be true when timeout_ms exceeds 36000000 ms` and no job starts. Resolved timeout: 0 when `persistent`, else `timeout_ms` or the default. Calls `jobs.StartMonitor` and returns `Started monitor-1 (<description>). Each stdout line arrives as a notice. Stop it with job_kill.` plus details `{jobId, timeoutMs, persistent}` (Grok `MonitorOutput`: task id, resolved timeout, persistent). Description adapted from Grok's `description_template`: "Start a background monitor that streams events from a long-running script. Each stdout line is an event; you can keep working and notifications arrive in the chat. Exit ends the watch. Output volume: every stdout line wakes the agent. Print only DONE/FAILED/CANCELLED lines, no progress lines. Use `grep --line-buffered` in pipes (plain `grep` buffers and delays events by minutes). Responsiveness: print FAILED at once when any tracked item fails; never wait for unrelated work to finish. Set persistent: true for session-length watches (PR monitoring, log tails); the monitor then runs until you call job_kill or the session ends. Otherwise it stops at timeout_ms (default 10 h)." Note: a monitor has a timeout, a `bash` background job has none (Q9).
  - Details (`protocol.ToolResultDetails`): `kind=execute`, `exitCode`, `signal`, `jobId`, `status`, `spillPath`, `spillCapped`, `truncation`.
  - Concurrency (P9): `job_list` and `job_output` without `wait` are `ConcurrencySafe`; `bash`, `monitor`, `job_kill` and `job_output` with `wait` are not.
  - Snippets and guidelines: `bash` from Pi's `bash.ts` contribution plus one guideline line on background jobs; one snippet line each for the job tools and `monitor` (no Pi source; text written here and reviewed in phase 10).
- Non-functional: the tools keep no state of their own; each holds the session's `*jobs.Table` given at construction (phase 7) and `Cwd` from `tools.Context`.

## Architecture

```
bash: args → validate (&, timeout clamp) → table.Start(bash, command, tc.Cwd)
      → table.Wait(ctx, id, budget) with a 50 ms update ticker → outcome
        Done       → "exit: N" + View → IsError on non-zero
        Background → moved text + View so far
        Cancelled  → "Command aborted" + View
job_output → table.Read      job_kill → table.Kill
job_list   → table.List      monitor  → timeout validation → table.StartMonitor
```

## Runtime Flow

- Actor and trigger: the model calls the tool.
- Registered entry point: `newNativeAgentOn` builds the tools with the session table (phase 7).
- Internal path: coordinator (`internal/agent/tool_coordinator.go:204,264`) → tool body → `jobs.Table` (phase 5) → `sandbox` (phase 3).
- Prepared data and external mocks: real processes; in E2E a scripted model HTTP server.
- Observable result: result text and details; notices in the next request (phase 5).
- End-to-end proof: the `bash`, `job_*` and monitor rows of the Runtime Flow Proof in `plan.md` (phase 9 tests).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/tools/shell_bash.go` | create | ~170 | `shell_bash_test.go` |
| `internal/tools/shell_jobs.go` | create (`job_output`, `job_kill`, `job_list`) | ~150 | `shell_jobs_test.go` |
| `internal/tools/shell_monitor.go` | create | ~60 | `shell_monitor_test.go` |

## Tests Before

`go test ./internal/tools ./internal/jobs` passes (phases 2, 4, 5 state as available).

## Tests After (write first; real `jobs.Table` and real processes, fake notifier)

| Test | Scenario | Priority |
|---|---|---|
| `TestBashForegroundReportsExitAndOutput` | `echo hi; exit 2` → `exit: 2`, `IsError` | critical |
| `TestBashTimeoutMovesToBackground` | gated job, `timeout: 1` → moved text, job running | critical |
| `TestBashTimeoutAbove600IsClamped` | `timeout: 900` → note in text (fake clock) | medium |
| `TestBashRunInBackgroundReturnsAtOnce` | returns < 100 ms with job id | critical |
| `TestBashRejectsTrailingAmpersand` | `sleep 5 &` refused; `a && b` allowed | high |
| `TestBashCancelledKillsJob` | ctx cancel → `Command aborted`, group gone | critical |
| `TestBashSendsThrottledPartialUpdates` | output for 400 ms → ≤ 9 updates | medium |
| `TestBashBigOutputKeepsHeadTailAndSpill` | 300 KB → marker, spill path in details | high |
| `TestBashDetailsCarryJobAndExit` | details JSON | high |
| `TestJobOutputRunningAnswersAtOnceWithNoticeLine` | < 50 ms, "do not poll" text | critical |
| `TestJobOutputUnknownID` | text names `job_list` | medium |
| `TestJobKillReturnsFinalStatus` | `killed` | high |
| `TestJobListFormatsLines` | two jobs | medium |
| `TestMonitorRejectsTimeoutAboveMax` | `timeout_ms` 36,000,001 with `persistent` false → error, no job; same with `persistent` true → started | critical |
| `TestMonitorResultCarriesResolvedTimeout` | no `timeout_ms` → 36,000,000; `persistent` → 0 and `persistent: true` | high |
| `TestMonitorStartsJobCountedByCap` | `monitor-1` listed by `job_list`; counts toward the cap | high |
| `TestShellToolConcurrencyFlags` | P9 table | high |

## Function and interface checklist

- [x] `tools.Bash`, `tools.JobOutput`, `tools.JobKill`, `tools.JobList`, `tools.Monitor` (`Decl`, `Execute`, `PromptSnippet`, `PromptGuidelines`, `ConcurrencySafe` where P9 says)
- [x] `tools.NewShellTools(t *jobs.Table) []Tool` constructor used by phase 7

## Dependency map

Needs phase 5 (`jobs`), phase 3 (`sandbox.View`), phase 1 (details, prompt interface). Blocks phase 7. Shares no file with phases 2 and 4.

## Related Code Files

- Create: the three files above and their tests.
- Modify: none.

## Implementation Steps

1. Write the tests; they fail to compile.
2. Implement `shell_bash.go` (validation, wait loop with the update ticker, result shaping), then the job tools and `monitor`.
3. Review every model-facing text against the "do not poll" rule and Pi's `bash.ts` wording.
4. Run the gate.

## Regression Gate

`go test ./internal/tools ./internal/jobs -race && go vet ./internal/tools`

## Success Criteria

- [x] All tests pass with `-race`; no test leaves a process group.
- [x] `job_output` on a running job never blocks without `wait`.
- [x] Every result that leaves a job running tells the model that a notice will arrive.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Models still poll `job_output` (spike 1) | H x L | Answers at once and costs one short turn; the text says not to poll; signal: the phase 10 run shows a polling loop over 5 calls; response: add a cooldown that makes `job_output` without `wait` wait up to 5 s while running (replan with the user, it changes the spike-1 design input). |
| Pi parity: `timeout` meaning changes from kill to background | certain x M | User decision Q9; documented in the description and phase 10. |
| Trailing `&` check refuses a valid command such as `echo a &` inside quotes | L x L | Check only the last unquoted token; the error names `run_in_background`. |

## Security Considerations

- `bash` runs with the user's OS permissions (D3, H-SEC-03); `command-guard` (phase 7) is an opt-in example, not a boundary.
- The env filter (phase 3) applies to `bash` and `monitor` alike.
