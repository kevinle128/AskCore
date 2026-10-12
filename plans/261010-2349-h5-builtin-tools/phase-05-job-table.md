---
phase: 5
title: "Job table, notices and monitor"
status: completed
priority: P1
effort: "3d"
dependencies: [3]
---

# Phase 5: Job table, notices and monitor

## Context

- Architecture: [Jobs (`jobs`)](../architectures/h5-builtin-tools-architecture.md#jobs-jobs) (table, states, wait budget, cap, tools, monitor, notices, cleanup), [Boundaries & contracts](../architectures/h5-builtin-tools-architecture.md#boundaries--contracts) (`jobs` reaches the Agent through a small interface; D19 reading), [Data model](../architectures/h5-builtin-tools-architecture.md#data-model), [Spike results](../architectures/h5-builtin-tools-architecture.md#spike-results-2026-10-10) (models poll `job_output`).
- Decisions: [Q7 DeepSeek-style design](../architectures/h5-builtin-tools.md#q7-lock-of-the-research-findings) with [the simplified design](../architectures/h5-builtin-tools.md#simplified-design-based-on-deepseek-replaces-the-draft-above-if-accepted) and [how DeepSeek does it](../architectures/h5-builtin-tools.md#how-deepseek-does-it); [Q8 orphans and the cap](../architectures/h5-builtin-tools.md#q8-orphaned-children-after-a-leader-crash-and-the-job-cap); [Q9 wait budget](../architectures/h5-builtin-tools.md#q9-wait-budget-before-a-command-moves-to-the-background); [Q10 monitor shape](../architectures/h5-builtin-tools.md#q10-monitor-shape) and [N3 monitor contract](../architectures/h5-builtin-tools.md#n3-monitor-contract-user-2026-10-11) (Grok's shape, user 2026-10-11); [Q11 delivery path](../architectures/h5-builtin-tools.md#q11-delivery-path-of-monitor-events); [Q14 notice shape](../architectures/h5-builtin-tools.md#q14-message-shape-of-system-notices); [Q15 spill cap](../architectures/h5-builtin-tools.md#q15-spill-file-cap).
- Spike 1: [Final conclusion](../reports/spike-261010-system-reminder-notice.md#final-conclusion) and [gpt-5.6-sol polling](../reports/spike-261010-system-reminder-notice.md#gpt-56-sol-polling) (answer a running job at once, say a notice will arrive).
- Spike 2: [Facts the plan must keep](../reports/spike-261010-output-buffer.md#facts-the-plan-must-keep) (ESRCH after cleanup; CPU under a flood bounded only by a kill).
- Roadmap: D19, D27 (`Remove`), D29 (`Dispose` drains then closes) in [section 1](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#1-decisions-in-the-order-they-block-phases); inventory H-TOOL-23.

## Overview

Add the `jobs` package: one job table per session that owns every `bash` and `monitor` process from start to end, waits with a budget, keeps read cursors, enforces the cap of 10, delivers completion notices and monitor events to the Agent, and kills everything on close. It imports `sandbox` and `pkg/protocol`, never `agent`.

## Requirements

- Functional:
  - `jobs.New(o Options) *Table`. `Options{Clock sandbox.Clock; Env func() []string; MaxActive int (10); KillGrace time.Duration (3 s); OnError func(error)}`. `Env` returns the child env built with `sandbox.ChildEnv` by the composition root (phase 7). Zero values: nil `Clock` = `sandbox.SystemClock`; nil `Env` = `sandbox.ChildEnv(os.Environ(), nil)` (filtered env, no session variables); `MaxActive` 0 = 10; `KillGrace` 0 = 3 s; nil `OnError` drops the error. So `jobs.New(jobs.Options{})` is valid (phase 7 test sites use it). The spill directory is created lazily on the first spill: `os.MkdirTemp("", "ask-jobs-*")`, mode 0700 (P5); an unused table owns no file and no goroutine.
  - `Table.SetNotifier(n Notifier)`; `Notifier{FollowUp, Steer func(protocol.Message) (string, error); Remove func(inputID string) bool}` (P3). Set once after the Agent exists (late binding); before it is set, notices are dropped through `OnError`.
  - Job: id `bash-N` or `monitor-N` (per-table counters from 1), kind, label (first line of the command cut to 80 chars), status `running` → `stopping` → `completed` | `killed` | `failed`, exit code or signal, the `sandbox.Process`, read cursor (bytes), `done` channel, kill reason, flags `awaited` and `killedByModel`, waiter count.
  - Cap (Q8): `Start` refuses when 10 jobs are `running` or `stopping`, with the error `Too many active jobs (10). Stop one with job_kill or wait for one to finish.` Finished jobs do not count.
  - `Table.Start(spec StartSpec) (*Job, error)`; `StartSpec{Kind, Command, Cwd string}`; the process runs under the table, never under the tool call context (D19 reading: the call's drain ends when the body returns; the table owns the process for the session lifetime).
  - `Table.Wait(ctx, id, budget) WaitOutcome` (Q9): returns `Done` (sets `awaited`; the job is removed from the table and its spill file stays until close because the result names it), `Background` when the budget passes (the job keeps running), or `Cancelled` when `ctx` ends (kill as by the model, wait for `Done`, remove). A budget of 0 returns `Background` at once (`run_in_background`).
  - Waiter rule against double reports: a waiter registers under the table mutex before it selects; the owner goroutine, under the same mutex at the end of the job, sets `awaited` when the waiter count is above zero; then it decides the notice. So a job that ends exactly at the budget is reported once.
  - `Table.Read(ctx, id, wait bool, timeout) ReadResult` (`job_output`): returns the output since the read cursor through `Process.ReadFrom` with the skipped-bytes note `[N bytes not kept in memory; see <spill>]`, advances the cursor, and a status line `[status: running]` or `[status: completed, exit code: 0]` / `[status: killed, reason: ...]` / `[status: failed, signal: SIGKILL]`. When the job runs and `wait` is false, it returns at once with the line `The job is still running. A notice will arrive when it finishes; do not poll.` (spike 1). With `wait`, it blocks until the end or `timeout` (default 30 s, cap 600 s, P16) or `ctx`; reading the final status sets `awaited` and removes the job.
  - `Table.Kill(id, reason)`: sets `killedByModel`, status `stopping`, `Process.Kill`; returns after `Done` (bounded by the grace plus the drain bound); final status `killed`; no notice.
  - `Table.List() []JobInfo`: id, kind, status, label, elapsed, exit code.
  - Notices (Q11, Q14): one owner goroutine per job waits on `Process.Done`, sets the final status, closes `done`, then, unless `awaited`, `killedByModel` or the table is closing, calls `FollowUp` with a `protocol.UserMessage` whose only block is `protocol.SystemReminder(text)` and text (DeepSeek `tool-jobs/src/index.ts:122-126`): `Background job bash-1 (bash: go test ./...) finished [status: completed, exit code: 1]. Read its output with job_output.` A refused notice (`ErrNoModel`, `ErrQueueFull`, `ErrDisposed`) goes to `OnError`; the job stays readable (P12).
  - Monitor (Q10, Q11, P14, N3 decided as Grok's shape, user 2026-10-11): `Table.StartMonitor(spec MonitorSpec{Command, Cwd, Description string; Timeout time.Duration; Persistent bool})` starts its own background job `monitor-N` that counts toward the cap of 10; output and spill are handled like every other job (readable with `job_output`). There is no pattern: each stdout line is an event (stderr goes to the job buffer only) and the command's exit ends the watch. Timeout: when `Persistent` is false, a `Clock.AfterFunc(Timeout)` timer kills the group with reason `timeout` (default and max 10 h are checked in the tool, phase 6); when `Persistent` is true there is no timer and the job runs until `job_kill` or session close. This differs from `bash` jobs, which have no background time limit (Q9). Batching: lines are cut at 500 chars; lines that arrive within a 200 ms debounce window form one event; an event is cut at 3,000 chars with `[... more lines cut]`. Flood guard: token bucket of 10 events, refilled 1 per 2,000 ms on the injected clock; an event without a token is counted as suppressed and the next delivered event carries `[N events suppressed by the flood guard]`. Delivery: `<monitor-event job="monitor-1" description="...">` lines `</monitor-event>`, wrapped by `protocol.SystemReminder`, sent with `Steer` (Q11, Q14); when a previous monitor message is still pending (`Remove(id)` returns true), old and new events are merged into one `Steer`. Overload: after 30,000 ms of continuous rate-limit violation the table kills the process group (`Process.Kill`, reason `overload`; Ask improvement, Grok only stops the pipeline) and sends one final event `monitor-1 stopped: output too fast for the monitor`. A monitor that ends (exit, timeout, overload) sends the normal completion notice through `FollowUp` with the same skip rules (no notice after `job_kill`).
  - Close: `Table.Close() error` is idempotent and has two steps: (1) under the mutex set `closing` (no notice or monitor event is sent after this), `Kill` every live job at once; (2) wait for all `Done` channels together, bounded by one shared grace plus the drain bound (about 6 s worst case for the whole table, not per job); then remove the spill directory. `Table.KillRemaining()` sends `Process.KillNow` to every live job and returns without waiting (print-mode exit sweep, phase 7). `Start` after `closing` returns `ErrClosed`.
- Non-functional: `goleak.VerifyTestMain`; every process test asserts ESRCH after cleanup; the mutex is never held while calling the Notifier or waiting on a process.

## Architecture

```
tools.Bash ──Start/Wait──▶ Table ──Start──▶ sandbox.Process
tools.JobOutput ─Read──▶   │  owner goroutine per job:
tools.JobKill ──Kill──▶    │    <-Process.Done → status → close(done)
tools.JobList ──List──▶    │    → unless awaited|killedByModel|closing:
tools.Monitor ─StartMonitor▶│        Notifier.FollowUp(SystemReminder(notice))
                           │  monitor Observe(stdout) → lines → 200 ms batch → bucket
                           │    → Remove(pending) + Steer(merged); timer unless persistent
composition (phase 7) ── New / SetNotifier / Close / KillRemaining
```

Lifetime: one `Table` per Agent, created in `newNativeAgentOn` (phase 7) and closed by `Agent.Dispose` through `agent.Config.Release` after the run drains (P2). State is per session; nothing is process-global.

## Runtime Flow

- Actor and trigger: the model calls `bash`, `job_*` or `monitor` (phase 6); a background job or a monitor line triggers a notice by itself.
- Registered entry point: the phase 6 tools registered in `newNativeAgentOn` (phase 7); notices enter the Agent through `Agent.FollowUp` and `Agent.Steer` (`internal/agent/queue.go:159,164`), which start a run when the Agent is idle.
- Internal path: owner goroutine → `Notifier` → Agent queue → next cycle → model request carries a `user` message with the `<system-reminder>` text.
- Prepared data and external mocks: real processes gated by files (`while [ ! -f gate ]; do sleep 0.05; done`); a fake notifier in unit tests, the real Agent in phase 9.
- Observable result: notice text in the next model request; job states; group gone after close.
- End-to-end proof: `TestPrintCompletionNoticeStartsFollowUpCycle`, `TestPrintMonitorDeliversEveryStdoutLine`, `TestPrintJobKillStopsJobWithoutNotice`, `TestPrintEleventhJobIsRefused`, `TestLeaderSessionCloseKillsBackgroundJobs` (phase 9).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/jobs/doc.go` | create | ~10 | none |
| `internal/jobs/table.go` | create (`Table`, `Options`, `Start`, `Wait`, `Kill`, `List`, `Close`, `KillRemaining`) | ~260 | `table_test.go` |
| `internal/jobs/read.go` | create (`Read`, status line) | ~90 | `read_test.go` |
| `internal/jobs/notice.go` | create (`Notifier`, notice text, skip rules) | ~70 | `notice_test.go` |
| `internal/jobs/monitor.go` | create (line split, debounce batch, bucket, timeout timer, overload) | ~200 | `monitor_test.go` |
| `internal/jobs/main_test.go` | create (`goleak.VerifyTestMain`) | ~10 | all |

## Tests Before

`go test ./internal/sandbox` passes (phase 3 state); `internal/jobs` does not exist.

## Tests After (write first)

| Test | Scenario | Priority |
|---|---|---|
| `TestWaitReturnsDoneAndRemovesJob` | `echo hi` with budget 5 s | critical |
| `TestWaitMovesToBackgroundAfterBudget` | gated job, budget 100 ms → `Background`, still running | critical |
| `TestWaitZeroBudgetStartsInBackground` | `run_in_background` shape | high |
| `TestWaitCancelledKillsAndWaits` | ctx cancel → killed, removed, group gone | critical |
| `TestJobEndingAtBudgetIsReportedOnce` | end and budget race, 200 runs → never both a result and a notice | critical |
| `TestCapRefusesEleventhActiveJob` | 10 gated jobs; 11th refused with `job_kill` text; finished jobs not counted | critical |
| `TestReadRunningJobAnswersAtOnce` | returns < 50 ms with status line and "do not poll" line | critical |
| `TestReadAdvancesCursorAndReportsSkippedBytes` | two reads; then 200 KB output | high |
| `TestReadWithWaitReturnsFinalStatusAndRemovesJob` | gate opened during wait | high |
| `TestReadWaitTimeoutCapsAt600Seconds` | timeout 900 s clamped (fake clock) | medium |
| `TestKillByModelSendsNoNotice` | `Kill` → status `killed`, notifier not called | critical |
| `TestBackgroundJobEndSendsFollowUpNotice` | text exact, wrapped by `SystemReminder`, role user | critical |
| `TestNoNoticeAfterAwaitedRead` | final status read with wait → no notice | critical |
| `TestNoNoticeWhileClosing` | `Close` with running jobs → no notice | critical |
| `TestRefusedNoticeGoesToOnErrorAndJobStaysReadable` | notifier returns `ErrQueueFull`-like error | high |
| `TestNoticeBeforeNotifierSetIsReported` | late binding order | medium |
| `TestMonitorSendsEveryStdoutLineAsSteer` | lines `a`, `b` on stdout and `err` on stderr → events `a`, `b` only; wrapped by `SystemReminder` | critical |
| `TestMonitorBatchesLinesWithin200ms` | 5 lines inside 200 ms (fake clock) → one event; a line after 300 ms → second event | critical |
| `TestMonitorExitEndsWatchWithNotice` | command exits → completion notice by `FollowUp`, no more events | high |
| `TestMonitorTimeoutKillsGroup` | `Timeout` 1 s, fake clock +1 s → group gone, status killed, reason `timeout`, notice sent | critical |
| `TestMonitorPersistentHasNoTimer` | `Persistent` true, fake clock +11 h → still running; `Kill` stops it without notice; `Close` stops it | critical |
| `TestCapCountsMonitors` | 9 bash jobs + 1 monitor → 11th start refused | high |
| `TestMonitorCoalescesPendingMessageWithRemove` | fake notifier holds id; second batch → `Remove(id)` then one merged `Steer` | critical |
| `TestMonitorFloodGuardSuppressesAndRefills` | one line per 250 ms (one event each), fake clock → first 10 delivered, then suppressed; after +2 s one refill carries `[N events suppressed ...]` | critical |
| `TestMonitorOverloadKillsProcess` | continuous events, fake clock +30,000 ms of violation → process group killed (ESRCH), final event | critical |
| `TestMonitorCutsLongLinesAndBatch` | 800-char line → 500; 4,000-char batch → 3,000 + marker | high |
| `TestCloseKillsAllJobsWithSharedGrace` | 10 jobs with `trap '' TERM` → `Close` < 7 s total, all groups gone, spill dir removed | critical |
| `TestKillRemainingSendsSigkillWithoutWait` | returns < 50 ms; groups gone after | high |
| `TestStartAfterCloseFails` | `ErrClosed` | medium |
| `TestUnusedTableOwnsNoFileOrGoroutine` | `New` then `Close`; no temp dir; goleak clean | high |

## Function and interface checklist

- [x] `Table`, `Options`, `Notifier`, `StartSpec`, `MonitorSpec`, `WaitOutcome`, `ReadResult`, `JobInfo`, `ErrClosed`, `ErrTooManyJobs`
- [x] `New`, `SetNotifier`, `Start`, `StartMonitor`, `Wait`, `Read`, `Kill`, `List`, `Close`, `KillRemaining`
- [x] notice text builder, monitor batcher, token bucket on `sandbox.Clock`

## Dependency map

Needs phase 3 (`sandbox.Start`, `Process`, `Clock`, `ReadFrom`) and phase 1 (`protocol.SystemReminder`, depguard entry). Blocks phase 6 (tools) and phase 7 (composition). Shares no file with phases 2 and 4.

## Related Code Files

- Create: `internal/jobs/*` listed above.
- Modify: none.
- Do not modify: `internal/jobs/README.md` (phase 10 creates it).

## Implementation Steps

1. Write `main_test.go`, a fake notifier (records messages, returns ids, implements `Remove`) and the fake clock from phase 3.
2. Write the tests; they fail to compile.
3. `table.go` (states, cap, owner goroutine, waiter rule, close), then `read.go`, `notice.go`, `monitor.go`.
4. Run the gate with `-race -count=3`.

## Regression Gate

`go test ./internal/jobs ./internal/sandbox -race -count=3 && go vet ./internal/jobs && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/jobs/...`

## Success Criteria

- [x] All tests pass three times with `-race`; `goleak` clean.
- [x] `internal/jobs` imports only the standard library, `internal/sandbox` and `pkg/protocol` (lint rule from phase 1).
- [x] No test leaves a live process group.
- [x] A notice is never sent for a job whose end the model already saw.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Flood guard numbers (10, 1 per 2 s, 30 s, 200 ms) do not fit real dev-server logs (architecture open question) | M x M | Constants in one place; signal: the monitor misses a `ready` line or kills a normal dev server in the phase 10 run; response: adjust the numbers (Grok values, P14), no design change. |
| A notice starts a cycle while the user prompts (ACP `session/prompt` gets busy after 250 ms, `internal/acp/host.go:28,770`; print mode handles it in phase 7) | M x M | Accepted for ACP clients: the same holds for any queued input; documented in phase 10. |
| Retained finished background jobs grow over a long session | L x L | A job is removed when its final status is read; unread finished jobs keep at most the 50 KB buffer each; the spill directory is removed on close. |
| `Close` from `Dispose` blocks leader shutdown or the in-process TUI exit | M x M | One shared grace per table (about 6 s worst case); phase 7 closes sessions in parallel (P17), under `ask leader stop` 15 s (`cmd/tui/leader.go:53`) and the in-process 10 s bound (`cmd/tui/tui_client.go:23`); signal: `TestLeaderShutdownKillsBackgroundJobs` with three sessions takes more than 8 s. |
| The model starts a chatty command and every line wakes the Agent | M x M | The description tells the model to print only `DONE`/`FAILED`/`CANCELLED` lines and to use `grep --line-buffered`; the flood guard and the overload kill bound the rest. |

## Security Considerations

- Notices carry only the job id, label, status and exit code; output reaches the model only through `job_output`.
- A process that leaves its group (`setsid`) survives close (accepted with Q8, as DeepSeek).
- A leader crash leaves children running (Q8, accepted).
