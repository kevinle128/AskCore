# Unify background work: `internal/jobs` and `internal/tasks`

Date: 2026-10-26. Branch: `develop` at `1f9cf2f`. Status: option A chosen (user, 2026-10-26); S1 to S4 decided; next step is the implementation plan. No code changed.

## Decision log

| Date | Decision | By |
|---|---|---|
| 2026-10-26 | Option A: H5 surface, one delivery port through `Agent.Notify` | User |
| 2026-10-26 | S1 = A: a completion notice enters at the next turn boundary (`Notify` `Wake`, steering), as Claude Code; no follow-up mode is added. Monitor events and completions use the same path. On Abort, notices move to the context queue (as Grok). | User |
| 2026-10-26 | S2 = B: `ask -p` waits for background work, signal-driven (`WaitForQuiescence`, no polling); each completion runs one more turn. Per-command limits as Claude Code and design section 14.1: 10 min default in `ask -p`, up to 2 h, plus the idle-wait cap. A command at its limit stops and its notice wakes one more turn. Replaces N2 for print mode. TUI and leader keep Q9 (no background limit). Open plan item: the parameter that sets the background limit, because H5 Q9 already gives `timeout` the meaning "wait budget". | User |
| 2026-10-26 | S3: a session reset (`_ask/session/reset`) is a new session for jobs. Old jobs keep running; each job carries the generation of the conversation in which it started (captured at job start), so `Notify` refuses its notice after a reset (`ErrStaleNotice`). The new conversation does not see old jobs: `job_list`, `job_output` and `job_kill` show only jobs of the current generation. Old jobs still count toward the cap of 10 and are killed when the session closes (`Release`). | User |
| 2026-10-26 | S4 = A: `origin/master` merged into `develop` before the plan (merge commit `ee53317`; tests, race and lint pass locally). `develop` now has the CI workflow. | User |

## 1. Summary

Two systems run background work and deliver its notices to the Agent:

- `internal/jobs` (H5, 2026-10-10 to 10-12): the job table behind `bash`, `job_output`, `job_kill`, `job_list` and `monitor`. It delivers through `Agent.FollowUp`, `Agent.Steer` and `Agent.Remove`.
- `internal/tasks` (ADR 0004, 2026-10-24): a per-Agent task registry. It delivers through `Agent.Notify`. No tool uses it yet.

ADR 0004 was written on a local `develop` that did not have H5. It assumes `bash` does not exist ("`bash` gets `run_in_background` when H5 lands", `docs/async-task-runtime.md:125`). It did not intend to reverse H5; it did not know about it.

The user goal of the task runtime is: no polling and no interval or heartbeat check; completion is pushed. `internal/jobs` already meets that goal (section 2). The unification question is about three delivery defects, one delivery port against two, and five conflicts between recorded user decisions.

Recommendation: option A. `jobs` becomes the first outside source of the notify port that ADR 0004 already promises (ADR section "Outside sources ... use the same port through an injected function", not yet wired). It reverses the fewest decisions, fixes defects 1 and 2, and fixes defect 3 with a per-job generation that the plan must design.

## 2. The no-polling goal

### 2.1 `internal/jobs` is push-based (verified)

| Point | Mechanism | Evidence |
|---|---|---|
| Job end | One owner goroutine per job waits on the process; it closes `done` and sends at most one notice | `internal/jobs/table.go:400-431` |
| `job_output` with `wait: true` | `select` on `j.done`, the budget timer and `ctx.Done()` | `internal/jobs/read.go:118-121` |
| `bash` foreground | Blocks on the wait outcome channel | `internal/tools/shell_bash.go:185-198` |
| Timers | Bounded only: wait budget, 3 s kill grace, 200 ms monitor debounce, read timeout | `table.go:491,585`, `monitor.go:197`, `read.go:115` |
| Model guidance | `bash`, `job_output` and the running-job reply all say "do not poll" | `shell_bash.go:76`, `shell_jobs.go:55`, `read.go:18` |

`internal/jobs` has no `time.Sleep`, no `time.Ticker` and no retry loop.

### 2.2 Polling that notices cause (in the callers of the Agent)

A notice can start a run on an idle Agent. A prompt that arrives then gets `ErrBusy`, and two callers retry by sleeping:

| Site | Loop | Listed in design 12.2 |
|---|---|---|
| `cmd/tui/headless.go:486-511` (`promptAll`) | `time.After(5ms)` after `ErrBusy`, then retry | Same shape as the listed ACP site |
| `internal/acp/host.go:774-783` (`Session.execute`) | `time.Sleep(busyPoll)` (2 ms) after `ErrBusy`, inside `busyWindow` | Yes |

Phase A2 replaces both with `Agent.Idle()`, which A1 already added (`internal/agent/idle.go`). This fix is the same for every option below, so it does not decide between them. It belongs in the delivery plan of whichever option is chosen.

### 2.3 Interval sites outside notice delivery (separate follow-ups)

| Site | What it does | Why it is out of scope |
|---|---|---|
| `internal/leader/client.go:250` | Connect retry every 25 ms until a spawned leader serves | Cross-process start; needs a readiness signal from the child (for example a pipe), a different problem |
| `internal/leader/client.go:417,649` (`waitReleased`, `waitFree`) | Wait every 25 ms until the lock and socket are free | Cross-process lock; same class as the allowed `settings/lock.go` exception |
| `internal/tools/shell_bash.go:177` | 50 ms ticker that pushes streamed output to the view while a foreground `bash` runs | Display pacing, not completion detection; could become "update on new output" |

## 3. Defects found (verified)

| # | Defect | Evidence |
|---|---|---|
| 1 | A job notice is lost when the human queue is full. `FollowUp` counts it as human input and returns `ErrQueueFull` at `MaxQueuedInputs` (default 100). `jobs` only reports the error. | `internal/agent/queue.go:316-317`, `internal/jobs/notice.go:91-93` |
| 2 | A queued job notice is lost on Abort. `Abort` without `KeepQueued` drops human input, and a job notice is human input. The job stays in the table until the model reads its final status, but the model is never told. | `internal/agent/agent.go:229-235`, `queue.go:147-159` |
| 3 | (Revised: a design question, not a defect. Claude Code `/clear` keeps background tasks running and does not clear the notice queue, `commands/clear/conversation.ts:87-96,136-152`. In Ask only an outside ACP client can call `_ask/session/reset`; the TUI `/new` opens a new session with a new job table, `internal/tuiclient/client.go:448-460`. Decided by S3.) After a session reset, old jobs keep running and their notices enter the new conversation. `_ask/reset` calls `Agent.Reset`; the job table is closed only by `Release` at `Dispose`. `FollowUp` has no generation check. | `internal/acp/ask_methods.go:322`, `internal/acp/host.go:845`, `cmd/tui/headless.go:342` |

`Agent.Notify` was built for 1 and 2: notices do not count toward the bound and survive Abort in the context queue (`internal/agent/tasks.go:96-143`). Its generation check refuses notices from before a Reset, which fixes the delivery half of 3.

## 4. Conflicts between recorded user decisions

| # | Topic | H5 decision (original text) | ADR 0004 decision (original text) | Trade-off |
|---|---|---|---|---|
| C1 | Tool surface | Q7: "DeepSeek-style job table"; tools `job_output`, `job_kill`, `job_list`, `monitor` (Q10) | Section 11: `bash run_in_background`, `task_stop`, `read`; "There is no `task_wait`, `task_status`, or `task_output` tool." ADR: a wait or status tool "that the model calls until the task is done was rejected" | Narrower than it looks: `job_output wait=true` blocks on a channel and the tools say "do not poll". The ADR rejects a poll loop, which `job_output` is not. Spike 1 still reports that models call `job_output` on running jobs. |
| C2 | Notice shape | Q14: "notice = `user` message in `<system-reminder>`, hidden from the conversation view, kept in the log" | `CustomMessage` with `customType: task-notification` and XML-like fields | `ConvertToLLM` turns a `CustomMessage` into `UserMessage{Content, Timestamp}` (`internal/providers/convert.go:21-26`). A custom message that carries the same `<system-reminder>` text gives the model the same bytes. Only the log, the events, the TUI and print mode see role `custom`. |
| C3 | Delivery timing | Q11: "monitor events by `Steer`, completion by `FollowUp`" | `Notify` with `Wake` while a run is active appends to steering, so the notice enters at the next turn boundary | A completion notice would reach the model earlier, inside the current run, instead of after it ends. The user chose `FollowUp` on 2026-10-11. |
| C4 | `ask -p` end | N2: "wait for idle, print, dispose cancels jobs with SIGTERM then SIGKILL after 3 s, teardown notices dropped ... plus Ask's SIGKILL sweep" | D3: "`ask -p` waits for the background work that the run started, and each completion gets one more turn" | Kill at the end (short, predictable runs) against wait (the answer can use the result, as `claude -p`). |
| C5 | Background time limit | Q9: "no background time limit" | Section 14.1: `ask -p` 10 minutes default, up to 2 hours; cloud 30 minutes; TUI and leader none | Matters only if C4 picks "wait"; a wait with no limit can hang a CI run. |

D4 (move a foreground command to the background at its timeout) and Q9 agree.

## 5. Options

### Option A: H5 surface, one delivery port (recommended)

`jobs` keeps its tools, process handling, wait, reads, monitor and notice text. It stops calling `FollowUp` and `Steer` and delivers through the Agent notify port, as the first outside source.

| Area | Change |
|---|---|
| Port | `Agent.Notify` accepts an outside-source notice (origin `job`). The composition root injects a notify function into the job table instead of `Notifier{FollowUp, Steer, Remove}`. |
| Shape | A `CustomMessage` with a job custom type whose content is the current `<system-reminder>` text. Model bytes do not change (C2), so the cassettes still match. |
| Coalescing | `Agent.Remove` already removes any queued input by ID, task notices included (`queue.go:99-109`). The monitor keeps its merge logic with the ID that `Notify` returns. |
| Generation | Each job must carry the generation of the conversation in which it started, captured at job start. A generation read at send time would accept old notices after a Reset; one captured at wiring time would refuse every new job. Today `tasks.Control` exposes only `Start`, `Stop` and `List` (`internal/tasks/types.go:139-143`), so a tool call cannot read the generation. The plan must add the mechanism (for example a notify function bound to the current registry, given to the tool through `tools.Context`). What happens to the old processes is sub-decision S3. |
| Quiescence | Only if S2 picks "wait": `WaitForQuiescence` counts running jobs through a small hook. Under N2 (kill at the end) print mode needs only `WaitForIdle`, as today. |
| Monitor on Abort | Monitor events move from human origin (`Steer`) to the notice origin. On Abort they move to the context queue instead of being dropped. `Remove` still reaches them. This is a small change under Q11 and is part of S1. |
| Readers | The TUI (`internal/tui/translate.go:283`, `update_session.go:631-639`), print mode (`cmd/tui/headless_print.go:17-48`) and the protocol helpers (`pkg/protocol/job_notice.go`) also recognize the job custom type. |
| Polling | A2: `promptAll` and `acp/host.go` wait on `Agent.Idle()` (section 2.2). |
| Docs | ADR 0004 amendment: shell work uses the H5 job table; the rejection of wait tools does not cover the blocking `job_output`; phase A3 is rewritten. `docs/async-task-runtime.md` sections 4, 11, 11.1, 14, 17. `internal/jobs/README.md`, `internal/tui/README.md`. |

- Decisions touched: C2 (the role changes in the log; the model sees the same text), C3 (unless sub-decision S1 keeps follow-up timing). Q1 to Q10, Q12 to Q15, N1 and N3 stay as they are.
- Defects fixed: 1 and 2. Defect 3 is fixable with the per-job generation above; the mechanism is a plan item, and the old processes depend on S3.
- Kept as is: two lifecycle owners. Shell processes do not run in the task registry. `job_output`, `job_list` and `job_kill` stay.
- Risk: medium. The Agent queue rules change for a new origin. Tests in `internal/jobs`, `internal/tui`, `cmd/tui` (`tools_e2e_test.go`, `interactive_jobs_test.go`, `headless_print_test.go`) need updates for role `custom`. Cassettes do not.

### Option B: Replace `jobs` with `tasks` (ADR 0004 as written)

`bash` runs as a `shell` task. `job_output`, `job_list` and `job_kill` are removed; `task_stop` is added; the model reads the output file with `read`. Notices use the `task-notification` shape.

- Decisions reversed: Q7, Q14, Q11, N2 (unless C4 picks "kill"), possibly Q10 and N3 (`monitor` would need a `watch` task kind that does not exist yet).
- Work: process bodies in `tasks` (process group, two-wave kill, spill caps), a port of the monitor flood guard, a rewrite of the TUI job lines (`translate_jobs.go`, `session_jobs.go`), new tool descriptions, all job tests, and re-recorded cassettes (the tool list and notice text change).
- Defects fixed: 1, 2, 3.
- Risk: high. It discards working, tested code and several fixed decisions ("Fixed: do not reopen", `plans/261010-2349-h5-builtin-tools/plan.md:32`).

### Option C: One registry, H5 surface (jobs registered as tasks)

Each job is also registered as a `shell` task, for lifecycle only: Reset, Dispose and quiescence. `jobs` keeps the process, wait, reads, monitor and tools. Notices are rendered as in A.

- Benefit: one place that knows every piece of background work. Reset and Dispose stop jobs and tasks with one rule.
- Cost: two exactly-once state machines for one process (the job `awaited` and `noticeStarted` flags and the task terminal claim). `tasks.Body` returns a `Result`; a job has its own owner goroutine and output buffer. The adapter must keep both in step, and every race test of `jobs` (`TestJobEndingAtBudgetIsReportedOnce`) must hold through it.
- Decisions touched: as A.
- Risk: medium to high. More code than A for a benefit that A gets with the quiescence hook and S3.

### Option D: Keep both, scope them apart

Amend ADR 0004: shell work belongs to `jobs`; `tasks` is for sub-agents, timers, watches and remote work. Fix the three defects inside the current `FollowUp` path (for example a `FollowUp` variant that does not count toward the bound, survives Abort and checks a generation).

- Decisions touched: none of H5. ADR 0004 loses "the only delivery port".
- Cost: the variant re-creates `Notify` beside it. Two delivery ports stay, which ADR 0004 rejected ("a second input contract").
- Risk: low now; the duplication grows with each new source.

### Comparison

| | A | B | C | D |
|---|---|---|---|---|
| No polling (goal) | Yes, with A2 | Yes, with A2 | Yes, with A2 | Yes, with A2 |
| Delivery ports | 1 | 1 | 1 | 2 |
| Lifecycle owners | 2 | 1 | 1 (with an adapter) | 2 |
| Defects 1, 2, 3 | 1, 2 fixed; 3 with a per-job generation (plan item) and S3 | Fixed | Fixed | Fixed with new code |
| User decisions reversed | C2 role only; C3 unless S1 | Q7, Q11, Q14, N2, maybe Q10/N3 | As A | None |
| Cassette re-record | No | Yes | No | No |
| Size | Medium | Large | Medium to large | Small to medium |

## 6. Sub-decisions after the option (one per turn)

| ID | Question | Applies to |
|---|---|---|
| S1 | Delivery timing (C3): keep follow-up timing for completions (add a follow-up delivery to `Notify`) or accept steering at the next turn boundary; and accept that monitor events survive Abort in the context queue? | A, C |
| S2 | `ask -p` end (C4) and, if "wait", the background time limit (C5) | All |
| S3 | On a session reset, kill the old jobs (as Reset stops old tasks) or keep them running without notices? | A, C, D |
| S4 | Order of work: merge `origin/master` into `develop` first (no overlap with the target files was found) | All |

## 6a. Reference harnesses for S1 (read with GitNexus, key lines checked)

| Event while the agent is busy | Claude Code (`/Users/dale/Desktop/workspace/claude-code`) | Grok CLI (`grok-build`) | Ask today | Ask with `Notify` `Wake` |
|---|---|---|---|---|
| Shell completion | Same turn, at the next model request after the current tool batch. Shell notices have priority `'next'` while `MONITOR_TOOL` is on (`tasks/LocalShellTask/LocalShellTask.tsx:222`); one drain point before each API call (`query.ts:1550-1570`) | After the turn ends, as a hidden synthetic turn. `TaskCompleted` has `QueuePolicy::Hidden` (`xai-grok-shell/src/session/mod.rs:197-207`); mid-turn promotion takes only editable items (`prompt_queue.rs:706`); drain after the turn (`run_loop.rs:2400-2402`) | After the run ends (`FollowUp`) | Next turn boundary (steering) |
| Sub-agent completion | After the turn ends (`'later'`, `messageQueueManager.ts:143`) | After the turn ends | n/a | n/a |
| Monitor event | Docs: the model is interrupted per event (line streaming is stubbed in this snapshot) | Next model request in the same run (`turn.rs` `inject_pending_monitor_events`) | Next turn boundary (`Steer`) | Next turn boundary |
| User interrupt | Notices stay queued and start a new turn after the query ends | Wakes become deferred; no auto-turn; they fold into the next user prompt | Notices are dropped (defect 2) | Notices move to the context queue and ride with the next input (as Grok) |
| Completion detection | Promise callback (push) | SIGCHLD plus a 100 ms ticker while processes exist (`terminal.rs:39`) | `cmd.Wait` in an owner goroutine (push) | unchanged |
| Interval timers found | `TaskOutput` `block=true` uses a `sleep(100)` loop; a 5 s stall watchdog; `sleep(100)` at `-p` exit | 100 ms process ticker | 50 ms output pacing in `bash` | unchanged |
| Wait tool | `TaskOutput`, marked deprecated: "Prefer using the Read tool on the task's output file" | `get_task_output` with `timeout_ms` | `job_output` with `wait` | unchanged |

Claude Code moved shell completions from `'later'` to `'next'` so they "drain without Sleep" (comment at `query.ts:1550-1553`). Grok keeps completions for the end of the turn and sends only monitor events into the run.

## 6b. Reference harnesses for S2: background work at the end of print mode

| | Claude Code `claude -p` | Grok `grok -p` | Ask today (N2) | Ask design (D3, section 14.1) |
|---|---|---|---|---|
| Running background work at the final answer | Waits; each completion runs one more turn | Waits; each completion runs one more turn (auto-wake on) | Kills it (table `Close`, then `KillRemaining`) | Waits; each completion runs one more turn |
| How it waits | Polling: `do { drainCommandQueue(); ... await sleep(100) }` (`cli/print.ts:2366-2400`) | Event-driven `tokio::select!` on the ACP channel; a timer only re-checks the ceiling (`xai-grok-pager/src/headless.rs:1196-1331`) | n/a | Signal-driven `WaitForQuiescence` |
| Limit | Docs: 10 min default per background command in `-p`, `timeout` up to 2 h; 10 min idle-wait ceiling for other work | One wall-clock ceiling: 600 s from the end of the first turn (`--background-wait-timeout`, `cli.rs:692-699`); `--no-wait-for-background` exits after the first turn | n/a | Per-task limit 10 min default (up to 2 h) plus `ASK_PRINT_BG_WAIT_CEILING_MS` idle cap |
| At the limit | Command stops; the model gets a "stopped after reaching its background time limit" notice and one more turn | Kills what is left: SIGTERM, 1 s grace, SIGKILL; no notices (`headless.rs:1336-1350`, `terminal.rs:44`) | n/a | Status `timed_out`; notice wakes a turn |
| Interactive limit | None | 10 h (`BACKGROUND_MAX_RUNTIME`, `terminal.rs:46`) | None (Q9) | None |
| Known gap | Polling loop | The last wake turn can be cut off: the loop exits when the last task ends, before its wake turn finishes (from code order, not run) | Answers cannot use late results | none found |

## 7. Corrections to the investigation reports

- "Wake behavior handles coalescing automatically": wrong. Coalescing needs `Remove`; it works because `Remove` accepts any queued input ID.
- "Changing the role to `custom` requires re-recorded cassettes": wrong. `ConvertToLLM` gives the model a `user` message with the same content.
- "origin/master changes only `internal/tui/deps_pin_test.go`": incomplete. It changes 44 files (CI, leader, settings, acp tests, changelog), but none of the files in this scope.

## 8. Unresolved questions

- S1 to S4 above.
- Whether the job custom type is shown to ACP clients other than the Ask TUI. `standardUpdates` sends no user frames today (H5 N1), so standard clients see nothing either way; this needs a check in the A plan.
