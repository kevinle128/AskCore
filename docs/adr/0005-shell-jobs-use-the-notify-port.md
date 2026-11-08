---
status: accepted
---

# Shell jobs deliver their notices through the Agent notify port

Ask had two systems for work that ends after the call that started it.
`internal/tasks` is the per-Agent task registry of [ADR 0004](0004-agent-owned-async-task-runtime.md), with `Agent.Notify` as its delivery port.
`internal/jobs` is the per-session job table of the built-in shell tools (`bash` with `run_in_background`, `job_output`, `job_kill`, `job_list`, `monitor`).
The job table sent its notices through its own table-wide notifier, which called `FollowUp` and `Steer` on the Agent.

Decision (user, 2026-10-26): keep the shell tool surface and the job table, and make `Agent.Notify` the one delivery port for both systems.

- The job table is the first outside source of the notify port. Each job holds the owner that started it, a `tasks.Port` bound to the conversation generation of the run (`tools.Context.Notices`). A completion notice and a monitor event go to that owner, never to a table-wide notifier.
- A completion notice that arrives during a run enters that run at the next turn boundary, as in Claude Code. A notice that arrives while the Agent is idle starts a run.
- After an Abort, a new notice waits in the context queue and goes with the next human input. It does not start a run by itself.
- A reset of the conversation (`/clear`, a new session in the same process) starts a new generation. The jobs of the old generation keep running until the session closes, but their notices are refused as stale and dropped. `job_list`, `job_output` and `job_kill` in the new conversation do not see them.
- `ask -p` waits by signal until the Agent, its tasks and its jobs are quiet, and each completion notice wakes one more turn. A background job in print mode stops after its background time limit: 10 minutes by default, up to 2 hours through the `timeout` of a `bash` call with `run_in_background`. The TUI, the leader and `ask acp` keep no background limit. Section 14.1 of [async-task-runtime.md](../async-task-runtime.md) has the details.
- A prompt that meets a run started by a notice waits on `Agent.Idle()`, in print mode and over ACP. Neither site sleeps and retries.

## Why the blocking `job_output` wait is allowed

ADR 0004 rejects a model-facing wait or status tool that the model calls until the task is done.
`job_output` with `wait: true` is not that tool.
One call blocks on the end of the job (a channel close), for at most its `timeout_ms` (30 s by default, 600 s at most), and then returns.
It costs one model request, as any tool call does, and the completion notice still arrives by itself when the model does not call it.
The `job_output` description tells the model not to poll it while a job runs.

## Rejected alternatives

Move shell work into the task registry and remove the job table: rejected because it reverses the H5 tool decisions (the tool names, the `<system-reminder>` notice shape, the monitor), and the registry has no watch kind for `monitor`.

Keep two delivery paths: rejected because the table-wide notifier did not know the conversation of a job, so a notice of an old conversation reached the new one after `/clear`, and print mode could not wait for the jobs.

Kill the background jobs when `ask -p` ends: this was the earlier print-mode rule. It was replaced because the answer could not use the result of the work, unlike `claude -p`.

## Sources

- Decision log: `plans/reports/brainstorm-261026-1357-jobs-tasks-unification.md`
- Review of the plan: `plans/reports/red-team-261026-1537-jobs-notify-plan.md`
- Claude Code: [background commands](https://code.claude.com/docs/en/tools-reference#background-commands), [headless: background tasks at exit](https://code.claude.com/docs/en/headless#background-tasks-at-exit)
