# Architecture Q&A — H5 built-in tools

Running record of the architecture conversation for H5.
Each answered question is appended here before the next question is asked.
Mode: brownfield (Ask already has the tool interface, registry, pipeline control points and result contract).

## Reference docs

- Roadmap H5 section: `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` (H5: Built-in tools).
- Inventory rows H-TOOL-01 to H-TOOL-23 and H-SEC-03: `plans/260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md`.
- Xia analysis of omp and Grok CLI: `plans/reports/xia-261010-1727-h5-builtin-tools.md`.

## Decisions made before this skill started (user, 2026-10-10, during the xia analysis)

### Q1. Edit tool schema

Question: keep Pi's `edits[{oldText,newText}]` or move to Claude Code's `old_string`, `new_string`, `replace_all`?
Options: A keep Pi (multi-edit, the contract of 2026-10-06); B Claude Code names (omp and Grok); C accept both.
Answer: A.
Consequence: the edit contract stays as decided on 2026-10-06.
Consequence: the omp and Grok matching ladder and error messages are applied to Pi's schema.

### Q2. Bash output truncation

Question: which part of a long command output does the model see?
Options: A head 20 KB plus tail 30 KB inside 50 KB (omp); B tail only (Pi); C half and half (Grok).
Answer: A.
Consequence: Pi's limits (50 KB, 2000 lines) stay; the kept part changes for `bash` only.
Consequence: an elision marker and the spill file path are shown; `read` keeps Pi's behavior.

### Q3. Critical commands and the no-approval stance (D3)

Question: should H5 block catastrophic commands?
Options offered first: A keep D3, no blocking; B a small hard block list; C B with an environment switch.
The user asked instead whether the blocking could be written as an extension here, to test extension use.
Follow-up options: A compiled-in extension, off by default, enabled by an environment variable; B the same, on by default; C wait for X1.
Answer: A.
Consequence: D3 stays.
Consequence: H5 adds a compiled-in extension `command-guard` registered through `pipeline.Registry.OnBeforeTool` (`internal/pipeline/registry.go:215,334`).
Consequence: it is off by default and enabled with `ASK_COMMAND_GUARD=1`.
Consequence: a blocked call returns a tool error `blocked by command-guard: <reason>` and the run continues.
Consequence: its purpose is to exercise the extension and denied-tool path; it is not a security boundary.

### Q4. Long-running commands

Question: what happens when a command runs too long?
Options: A a 300 s default timeout (omp); B no default (Pi); C move it to the background like Grok CLI.
Answer: C, with the scope extended to work like Grok CLI.
Consequence: a command still running after a wait budget moves to the background instead of being killed (Grok `block_until_ms`, default 30 s; `0` starts it in the background; background cap 10 h; a trailing `&` is rejected with a hint).
Consequence: H5 adds Grok's companion tools: read task output, kill a task, and a monitor tool that turns each output line of a script into an event.
Consequence: the leader owns background processes; they are killed when the session ends or the leader exits, and they never block the drain of the tool call that started them.
Source notes: Grok `xai-grok-tools/src/implementations/grok_build/bash/mod.rs:427-428,898-904,1221-1257,1556-1577`, `computer/local/terminal.rs:46`, `grok_build/monitor/tool.rs:30`.

## Questions in this skill

### Q5. Intent and reference docs

Question: is the intent H5 built-in tools, with the roadmap H5 section, the inventory rows and the xia report as reference docs?
Options: confirm, add other docs, or name a different intent.
Answer: confirmed ("đúng"); no other docs.
Consequence: brownfield exploration of how H5 lands in the existing tool interface, registry, pipeline and result contract.

### Q6. Process layer: where it lives and its shape

Question: where does the process layer (foreground commands, background tasks, task output, kill, monitor) live, and what shape does it have?
Options: A a per-session process manager in `internal/sandbox` with thin tools; B each tool runs `os/exec` with a global background table; C delegate commands to the ACP client terminal.
Recommendation given: A.
Answer (custom, review gate): read Grok CLI again in depth, then decide which design fits Ask's current design best.
The solution must be easy to maintain and to extend, and use suitable architecture and design patterns.
This is a hard problem; research carefully first, including Go best practices on the Internet.
Consequence: no option is chosen yet; a research pass runs first, its findings are written here, and the user locks them before the next question.

### Note: user question during research — can the agent learn of a finished task through a channel?

Question (user): can we use a channel so the agent knows which task finished?
Finding: the Agent does not listen on any channel.
Finding: it has two queues and a `wake` boolean guarded by its mutex (`internal/agent/agent.go:53-55`).
Finding: outside code adds work with `Steer(msg)` or `FollowUp(msg)` (`internal/agent/queue.go:159,164`); an idle Agent starts a run, a busy one sets `wake`.
Working direction (not decided): channels inside the process layer (one owner goroutine per task waits on the process and sends the result to the session task manager, an actor that owns task state).
Working direction (not decided): the task manager tells the Agent through `FollowUp` (or a dedicated system-notice input) behind a small interface, not by exposing a channel to the Agent.
Reasons: the Agent loop does not select on channels; a sender would block after dispose or during drain, while `FollowUp` returns an error at once; many finishing tasks could fill a channel buffer, while the manager can coalesce notices; the dependency stays one-way and easy to test.
Open point for a later question: should a finish notice be a follow-up (wait for idle), a steer (enter at the next turn boundary), or non-waking context (the model polls with the task-output tool)? The Grok research will show what Grok does.

## Q6 research findings (for the user to lock)

Three research passes ran on 2026-10-10: Grok CLI architecture (grok-build `2bdd1d6a`, GitNexus plus file reads), Go best practices (web and Go 1.26 source), and Ask's own constraints (code and READMEs).
Two reports contained instruction-shaped text (a Grok permission mode name, Grok's reminder tags); it was treated as data and nothing was acted on.

### Grok CLI architecture in brief

Grok keeps one task registry per root session, owned by one actor (`LocalTerminalActor`) that holds all process state without locks (`xai-grok-tools/src/computer/local/terminal.rs:1-3,548,956-1001`).
All callers talk to it through one strategy interface, `TerminalBackend`, with a local implementation and an ACP-client implementation (`computer/types.rs:344-448`; `xai-grok-shell-terminal/src/adapter.rs:177`).
A sandbox wraps the spawn as a decorator (`SandboxLaunch`, `xai-grok-tools/src/sandbox_launch.rs:74`).
Kill scopes keep only weak references so a reused PID is never signalled (`xai-tty-utils/src/process_scope.rs:1-19`).
A foreground command moves to the background by re-keying the same process state; the actor still owns it (`terminal.rs:2048-2070`).
Kill is SIGTERM to the group, 1 s, then SIGKILL to the group (`terminal.rs:3016-3045`).
Completion reaches the model through three paths (an immediate synthetic prompt with a 250 ms admission handshake, an idle drain turn, and a reminder on the next tool result) with two dedupe sets and three flags (`xai-grok-shell/src/tools/notification_bridge.rs:381-617`).
The Grok report itself names this wake design, the 6,000-line actor, the two ID schemes and the monitor that never kills its process as weak points.

### Go best practices (sources in the research report)

Give the child the spill file as stdout and stderr; no copy goroutine runs, a grandchild cannot hold a pipe open, and the foreground-to-background move needs no rewiring (os/exec docs).
`Cmd.Cancel` and `WaitDelay` signal only the leader by default; set `Cancel` to SIGTERM the process group, use `WaitDelay` as the grace period, and send SIGKILL to the group once after `Wait` (os/exec source; HashiCorp Nomad executor).
One owner goroutine per task performs every state change; status derived in several places causes stale states (Nomad executor; reported Claude Code bugs).
A background task's context must derive from the session, not from the tool call, or the task dies when the call ends.
macOS has no parent-death signal; an on-disk task registry (pid, pgid, start time, task id, written before the task is reported started) plus a sweep at leader start is the baseline.
Do not let children inherit the daemon environment unchanged, because it can hold provider keys.
Patterns: strategy for the executor (local now, sandboxed later), decorator for env, limits and timeouts, actor for the task lifecycle, observer with bounded channels for monitor events.
Tests: real short-lived processes, an injected clock for timers, `goleak.VerifyTestMain`, and after each test `kill(-pgid, 0)` must return `ESRCH`.

### Ask constraints (verified in code)

Each ACP session owns its own Agent: `Host.NewSession` builds it through `h.factory(ctx, id, cwd)` (`internal/acp/host.go:259-271`) and `Session.Close` disposes it (`internal/acp/host.go:905-910`).
The driver generation is a field of the same session (`internal/acp/host.go:350-370`), so a `/take` keeps the session and must not kill background tasks.
The Agent wakes only through `Steer` and `FollowUp` (`internal/agent/queue.go:159,164`) and its `wake` flag (`internal/agent/agent.go:53-55`); `AfterTool` can add non-waking context (lifecycle decision D27).
Message roles are `system`, `user`, `assistant`, `toolResult` only (`pkg/protocol/message.go:15-18`).
`Clock` is in the Agent config (`internal/agent/types.go:72-74`) and `Wait` in the run config (`internal/agent/loop_run.go:32`); `tools.Context` has no clock yet, so the process layer needs its own injected clock.
Core packages (`agent`, `tools`, `pipeline`, `sandbox`) must not import `acp` or `leader` (`.golangci.yml`).
Orphans are a live problem on this machine: four orphan `ask leader` processes from old test runs were found and stopped earlier today.

### Reconciliations (where the sources disagree)

Output capture: Grok uses pipes plus a drain task; the Go research says file-as-stdout.
Pick: file-as-stdout.
Cost stated: live partial updates (Pi's streaming `bash`, the 50 ms cadence) and the monitor become offset tails of a local append-only file instead of reads from a pipe.
Kept from Grok: when the monitor is overloaded it must kill the process, not only stop reading.
The head 20 KB plus tail 30 KB decision (Q2) maps onto file offsets with no ring buffer.

Wake path: Grok has three paths, two dedupe sets and a handshake.
Pick: one path. The session task manager calls `FollowUp` behind a small interface, with one `delivered` flag set when the model already saw the result through a wait or a kill.
Monitor events and "N tasks still running" notes ride `AfterTool` non-waking context.
This also answers the user's channel question: channels inside the manager, `FollowUp` at the boundary to the Agent.

Lifecycle rule D19: it assumed the tool body owns the process for its whole life.
Reading: with background handoff, the `bash` body returns when its wait budget ends; its drain obligation ends at that return, and the session task manager inherits the process under a session-derived context.

### Recommended design (shape only, not decided)

One task manager per session, created by the session factory next to the Agent and closed by `Session.Close` after the Agent drains.
One owner goroutine (actor) per task; the manager keeps a small mutex-guarded map of tasks.
An `Executor` strategy interface: a local Unix implementation now; an OS sandbox or an ACP-client terminal can be added later without changing the tools.
Decorators on the executor for environment, limits and timeouts.
File-as-stdout spill files, mode 0600, under the session directory, removed with the session, with a runtime size cap smaller than Grok's 5 GiB.
Process group per task; SIGTERM to the group, grace, SIGKILL to the group, then a final group kill after `Wait`.
An on-disk task registry and a sweep at leader start for orphans.
One task ID equal to the tool call ID, for foreground and background alike.
One wake path through `FollowUp`; non-waking notes through `AfterTool`.
Thin tools (`bash`, task output, kill task, monitor) that call the manager.

### Alternatives considered

Grok-faithful design (pipes, polling actor, three wake paths): proven at scale, but large and hard to maintain by Grok's own account.
Per-tool `os/exec` with a global background table: least code at first, but kill, env and spill logic spread over many tools and the table does not follow session life.
ACP-client terminal as the main path: the client owns processes, but they die when the client leaves and `ask -p` has no client; keep it as a later executor.

### Upcoming questions (not yet asked)

1. Child environment: inherit minus a credential-shaped denylist, a strict allow-list, or a Grok-style login-shell capture filtered by a denylist.
2. Package placement: everything in `sandbox`, or `sandbox` for the executor and process (strategy and decorators) plus a new `tasks` package for the manager, state machine and output store (actor and observer).
3. Bash schema: keep Pi's `timeout` (kill) and add `block_until_ms` (background), or one parameter whose expiry means background (breaks Pi semantics).
4. PTY support: yes or no in H5.
5. Linux cgroup containment: in scope or later.
6. A completion notice's message shape, given only `system`, `user`, `assistant`, `toolResult` roles exist.

### Review of the Q6 findings (user, 2026-10-10)

User response: the design seems too complex.
User request: check how DeepSeek (deepseek-harness, the reference already used for the lifecycle redesign, commit `5badb15`) solves the same problem.
Consequence: the Q6 findings are not locked; a DeepSeek research pass runs, its findings are added here, and the user reviews again before any next question.

## DeepSeek research findings (for the user to review)

Source: deepseek-harness `5badb15`, MIT ("Copyright (c) 2026 DeepSeek", `LICENSE:1-3`), read directly from the files.

### How DeepSeek does it

Every `bash` call starts a job; the tool waits up to `timeoutMs` (default 120 s, cap 600 s) and, if the job still runs, returns its id and keeps it running (`packages/shell/tool-bash/src/index.ts:517-526`, `waitOnJob` `:305-373`).
A job that finishes inside the wait is removed and returns a normal result; the model never sees an id (`:364-372`).
If the call is cancelled during the wait, the job is killed, awaited and removed (`:321-329`).
The `timeoutMs` parameter is the wait budget; there is no second time parameter.
State is one in-memory job table for the whole process, each job fenced by its owner session (`packages/jobs/jobs-local/src/index.ts:128-160,394-410`).
A job holds a status (`running`, `stopping`, `completed`, `killed`, `failed`), a bounded output buffer, one model read cursor, waiters and a kill reason (`:76-116`).
Tools: `job_output(job_id, wait?, timeout_ms?)` returns new output since the last read and a `[status: …]` line; `job_kill(job_id, reason?)`; `job_list()` (`packages/jobs/tool-jobs/src/index.ts:310-414`).
At most 10 active jobs per owner; background jobs have no time limit (`jobs-local/src/index.ts:33,219-224`).
Process group kill: SIGTERM, then SIGKILL after 3 s, the SIGKILL timer stays armed after the main process exits, and a drain timer stops a grandchild that holds the pipes (`packages/subprocess/subprocess-local/src/spawn.ts:357-367,414,418`).
Output: a bounded in-memory tail; a spill file only on overflow (`packages/shell/bash-local/src/index.ts:104`; `subprocess-local/src/output.ts:115-213`).
Completion: one listener; skip when the model already saw the end (a wait or its own kill) or on teardown; else a follow-up turn when idle, or injection at the next step when busy (`tool-jobs/src/index.ts:271-308`).
Notice text: `background job {id} ({kind}: {label}) finished [status: {status}, {detail}]. Read its output with job_output.` (`tool-jobs/src/index.ts:122-126`).
Cleanup: when the owner Agent is disposed, all its jobs are cancelled, awaited and dropped (`jobs-local/src/index.ts:614-630`); nothing survives a restart, by design (`jobs/jobs-local/README.md:137`).
No monitor tool, no actor, no executor strategy, no on-disk registry.

### Ask check

`FollowUp` covers both the idle and the busy case: an idle Agent starts a run, a busy one queues the message for its own cycle, a disposed one returns `ErrDisposed`, a full queue returns `ErrQueueFull` (`internal/agent/queue.go:160-199`).
So one wake path is enough; no second "inject" path is needed.
Side effect: the notice appears in the TUI queue as a follow-up row while it waits; the design must decide how it is shown.

### Simplified design based on DeepSeek (replaces the draft above if accepted)

Lives inside the session that already owns one Agent; no actor, no executor interface, no disk state.
1. Process runner: `exec.Cmd` with a process group and `bash -c`, the non-interactive env, stdin closed; stdout and stderr into a mutex-guarded buffer that keeps the head 20 KB and tail 30 KB (Q2) and counts dropped bytes; full output to a spill file only on overflow; kill SIGTERM to the group, SIGKILL after 3 s, the kill timer kept armed after exit, the pipe drain bounded by the same grace.
2. Job table per session: a map and a mutex; a job holds id, label, status, exit code or signal, buffer, read cursor, a done channel, kill reason, and the flags "awaited" and "killed by the model"; at most 10 active jobs.
3. `bash`: every call starts a job and waits on done, the timer, or the call context; done gives a normal result, the timer gives the "moved to background" text, cancellation kills and returns an abort; `timeout` is the wait budget (default 120 s, cap 600 s).
4. `job_output(job_id, wait?, timeout_ms?)` and `job_kill(job_id, reason?)`; `job_list` optional.
5. Monitor (kept by the user's decision Q4): `job_output` with a wait that returns on new lines or on the end, with an optional line pattern; no separate streaming tool and no rate limit unless tests show a need.
6. Completion: skip if awaited, killed by the model, or the session is closing; else one `FollowUp` with DeepSeek's notice text.
7. Cleanup: `Session.Close` kills and waits for every job; the leader closes every session on shutdown; nothing on disk.
Estimated size: about 500 to 550 Go lines and two or three tools.

### What changes against the draft and against earlier decisions

Dropped from the draft: actor per task, executor strategy and decorators, file-as-stdout, on-disk registry and sweep.
Risk kept by dropping the on-disk registry: if the leader crashes on macOS, running children are orphaned (seen on this machine today); DeepSeek accepts this.
Earlier decision Q4 named Grok's `block_until_ms` (default 30 s) and a 10 h background cap; DeepSeek uses `timeout` as the wait budget (default 120 s) and no background cap; the user must confirm which.
Earlier decision Q4 named a separate monitor tool like Grok; the simplified design folds it into `job_output` with a line wait; the user must confirm.

### Q7. Lock of the research findings

Question: lock the simplified design based on DeepSeek, or keep or change parts?
Options: lock the DeepSeek-style design; keep parts of the earlier draft; change parts.
Answer: lock the DeepSeek-style design ("OK, làm giống DeepSeek").
Consequence: the earlier draft (actor per task, executor strategy and decorators, file-as-stdout, on-disk registry) is superseded by the simplified design in "DeepSeek research findings".
Still open from Q4 (to ask): wait budget (Grok `block_until_ms` 30 s and a 10 h cap, or DeepSeek `timeout` 120 s and no cap) and the monitor shape (separate tool, or folded into `job_output`).
User follow-up questions recorded with this answer: is there a way to clean up children orphaned when the leader crashes, and why limit active jobs to 10?

### Q8. Orphaned children after a leader crash, and the job cap

Question: how to clean up children orphaned when the leader crashes?
Options: A a lifeline pipe through a small `ask job-guard` per job (cleans up at once, needs a macOS spike); B an on-disk record per job and a sweep at leader start (cleans up only when the leader runs again); C accept it, as DeepSeek does.
Recommendation given: A.
Answer: like DeepSeek ("ok làm giống deepseek"), so C.
Consequence: no guard process, no on-disk record, no sweep; jobs are killed when their session closes and when the leader shuts down normally; a leader crash can leave children running.
Consequence: the active-job cap stays at 10 per session, as DeepSeek; the reasons (a model loop that starts jobs, memory per job, a flood of completion notices) were explained; H6 can make it a setting.

### Q9. Wait budget before a command moves to the background

Question: how long does `bash` wait before it moves a command to the background, and under which parameter?
Options: A as DeepSeek (`timeout` is the wait budget, default 120 s, cap 600 s, `run_in_background` flag, no background time limit); B as Grok (`block_until_ms`, default 30 s, 10 h background cap); C A plus a background time limit.
Recommendation given: A.
Answer: A.
Supersession note: this replaces the Grok `block_until_ms` and 10 h cap part of Q4; the move-to-background behavior of Q4 stays.
Consequence: Pi's parameter name `timeout` stays, its meaning changes from kill to move to the background; one time parameter only.

### Q10. Monitor shape

Question: is the monitor a separate tool, folded into `job_output`, or dropped?
The user asked what the tool is for; it was explained with the example of waiting for `ready` from `npm run dev`, a job that never ends.
Options: A fold into `job_output` (wait until a matching new line); B a separate `monitor` tool like Grok (lines matching a pattern become events delivered to the model without it asking); C no monitor, as DeepSeek.
Recommendation given: A.
Answer: B.
Consequence: Ask keeps a separate `monitor` tool, the one place where Ask follows Grok instead of DeepSeek.
Consequence: monitor events need a flood guard (Grok: a token bucket of 10 events refilled 1 every 2 s, with a "[N events suppressed]" note) and, unlike Grok, overload must really kill the process.
Consequence: monitor events need a delivery path to the Agent; to be decided next.

### Q11. Delivery path of monitor events

Question: how do monitor events reach the Agent?
Options: A `Steer` (enters the active run at the next turn boundary; starts a run when idle); B `FollowUp` (after the current cycle); C `AfterTool` non-waking context (an idle Agent never sees it).
Recommendation given: A.
Answer: A.
Consequence: monitor events go through `Steer`, coalesced into one message per delivery and limited by the flood guard (about 10 events, 1 refilled every 2 s).
Consequence: job completion notices still go through `FollowUp`, as DeepSeek.

### Q12. Child environment of `bash`

Question: which environment do commands get?
Options: A inherit minus credential-shaped variables, then add the non-interactive variables and `ASK_*` session variables (as DeepSeek); B a strict allow-list; C a Grok-style login-shell capture, filtered like A.
Recommendation given: A.
Answer: A.
Consequence: commands see the user's environment as the leader inherited it, without variables named like secrets (`*_API_KEY`, `*_TOKEN`, `*_SECRET`, `*PASSWORD*`, `AWS_*_KEY` and similar).
Consequence: added variables: `TERM=dumb`, `NO_COLOR=1`, pagers set to `cat`, `GIT_TERMINAL_PROMPT=0`, editors set to a no-op, and `ASK_SESSION_ID` and the other `ASK_*` session variables (D7).
Known risk: a secret with an unusual name can pass the filter.
Later option: C, if the leader starts as a system service without the user's shell environment.

### Q13. Package placement

Question: where does the code live?
Options: A `sandbox` runs processes, a new `jobs` package holds the per-session job table, monitor and notices, `tools` holds thin tools; B everything in `sandbox`; C everything in `tools`.
Recommendation given: A.
Answer: A.
Consequence: `sandbox` owns process groups, environment, the two-wave kill, the head and tail buffer and the overflow spill file.
Consequence: `jobs` owns the per-session job table, states, read cursors, the cap of 10, the monitor with its flood guard, and notices to the Agent through a small interface with `FollowUp` and `Steer`; it does not import `agent`.
Consequence: `tools` holds thin tools: `bash`, `job_output`, `job_kill`, `job_list`, `monitor`, plus `read`, `write`, `edit`, `grep`, `find`, `ls`.
Consequence: the composition root creates the job table with each session's Agent and closes it when the session closes.

### Low-cost reversible calls decided by default (user may override)

PTY: no PTY in H5; commands run on pipes, as DeepSeek and Pi.
Linux cgroup containment: not in H5; process groups only, the same on macOS and Linux.

### Q14. Message shape of system notices

Question: how do job-finished notices and monitor events look as messages, given only `system`, `user`, `assistant`, `toolResult` roles exist (`pkg/protocol/message.go:15-18`)?
Options: A a `user` message with a fixed marker tag and a log flag, shown by the TUI as a dim notice line; B a new `notice` role; C a plain `user` message as DeepSeek.
Recommendation given: A.
Answer: like Grok.
Consequence: a notice is a `user`-role message whose text is wrapped in `<system-reminder>…</system-reminder>`, as Grok does (`xai-grok-shell/src/session/reminders/mod.rs:26-38`).
Consequence: the notice is hidden from the conversation view, as Grok does; the TUI must not show it as a user prompt or as a queued follow-up row.
Consequence: job status is shown separately in the TUI (Grok shows "N commands still running"); that TUI part is designed later with the ask-tui-design rule.
Consequence: the notice stays in the session log so that observers and resume keep the same model context.

### Spikes run (user asked "Lam Spike di", 2026-10-10)

Spike 1 (notice shape): keep the Grok `<system-reminder>` shape; one run per provider, no control yet; `job_output` and `bash` descriptions must tell the model not to poll. Report: `plans/reports/spike-261010-system-reminder-notice.md`.
Spike 2 (output buffer): keep the in-memory head and tail buffer for every case; macOS only. Report: `plans/reports/spike-261010-output-buffer.md`.
New open point from spike 2: the overflow spill file needs a size cap and a rule for what happens beyond it.

### Q15. Spill file cap

Question: what limits the overflow spill file, and what happens beyond the limit?
Options: A 64 MB as DeepSeek, stop writing the file and keep the job running; B 64 MB and kill the job; C a large cap as Grok (kill past 5 GB).
Recommendation given: A.
Answer: A.
Consequence: the spill file stops at 64 MB; the in-memory head and tail buffer keeps working; the result says `[spill file capped at 64 MB]`; the job is not killed.
Spike 1 completed (user chose A, 2026-10-10): 5 tagged runs and 1 untagged control; keep the Grok shape; polling happens with or without the notice, so `job_output` must answer at once and say a notice will arrive.
