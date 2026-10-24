# Reference designs for a unified async-task runtime that wakes the agent by push

Date: 2026-10-24 (Asia/Saigon). Scope: research only, no code changed.

Pins used for local and cloned sources: Pi `4c6fb7cfe`, dewee `d949d7ea`, voocel/agentcore `d8f5722`, compforge/agentgo `237e77c`. Line numbers below refer to those commits.

## Outcome in six sentences

1. Every system that works without model polling uses the same skeleton: a registry of tasks with an ID, a terminal state, and a bounded output sink, plus a single notification message that is queued into the agent's input path when the task ends.
2. Every system delivers that notification to the LLM as a user-role message with a structured wrapper (XML tags or a bracketed prefix). None uses a real `system` role for it. Pi stores it as an app-level `custom` role, then converts it to `user` for the provider.
3. The hard part is not the registry. It is the wake: a completion that arrives while the agent is idle must start a run, and a completion that arrives mid-run must land at a safe boundary (after tool results, before the next model call). Pi, agentgo, and Claude Code handle both cases. voocel/agentcore, Codex, and the dewee subagent path do not handle the idle case inside the agent itself.
4. The dominant reported failure classes are lost or duplicate wakes (races, restarts, session ownership), unbounded wake counts, unbounded output in the notification, and tasks silently killed by an unrelated lifecycle (idle eviction, session reap, turn interrupt).
5. Persistence across restart is rare and costly. Only Pi `durable` (full checkpointed tasks) and OpenCode (`Job` registry with a durable notification marker) do it by design. Hermes added a checkpoint file and then hit several holes. dewee marks orphaned tasks failed at startup and does not re-announce.
6. dewee, our closest architectural cousin, contains four timers or polls that our "never sleep or poll" rule would forbid. They are listed in section D so we do not copy them.

## A. Pi (local)

### A1. What Pi core actually has

- Pi core has no background shell. `packages/coding-agent/src/core/tools/bash.ts:114` spawns with `detached: true` only so the whole process group can be killed on abort (`:123` tracks the pid, `:160` untracks it). The tool call blocks until the command ends.
- Output limit for tool results: 2000 lines or 50 KB, whichever first (`core/tools/truncate.ts:11-12`).
- Sub-agents are an example extension, not core. `examples/extensions/subagent/index.ts:346` spawns a separate `pi` process, `:410-419` kills it on `AbortSignal`. The tool call blocks until the child finishes, so there is no async completion there.
- Timers are extension territory. The only timer in core is the prompt-cache warmer (`core/cache-warmer.ts:296`, one `setTimeout` per session).
- File watches are extension territory. `examples/extensions/file-trigger.ts` uses `fs.watch` and calls `pi.sendMessage({customType:"file-trigger", ...}, {triggerTurn:true})`.
- Real background work exists only in the experimental `packages/durable` package (A4).

### A2. Steer, follow-up, and the queue

`packages/agent/src/agent.ts`:

- `PendingMessageQueue` (`:143-174`) has two modes: `"one-at-a-time"` (default, set at `:247-248`) and `"all"`. `drain()` removes only what `peek()` returned.
- `steer()` (`:299`) and `followUp()` (`:304`) append to separate queues. `prompt()` throws if a run is active (`:374-378`).
- `createLoopConfig` wires `getSteeringMessages` and `getFollowUpMessages` to the two queues (`:496-503`).

`packages/agent/src/agent-loop.ts` is where the delivery points live:

- `:176` polls steering once at run start.
- `:204-205` polls again after `prepareNextTurn` (which can be slow, for example compaction) only if the first poll was empty. This avoids delivering two messages in one turn in one-at-a-time mode.
- `:211-217` appends the drained messages to the context before the next model call.
- `:295` polls steering after every turn (after tool results are appended). This is the "safe boundary".
- `:302-308` polls follow-up only when the inner loop would stop (no tool calls, no steering). If it returns anything, the outer loop continues. This is the "wake while the run is closing" path: a follow-up that arrives before `agent_end` keeps the same run alive.
- `continue()` (`agent.ts:384-411`) starts a run from queued messages only when the transcript tail is an assistant message; otherwise it throws. This is the idle restart primitive and it has the tail precondition.

### A3. Extension-injected messages: `sendMessage`, `deliverAs`, `triggerTurn`

Public API: `extensions/types.ts:443-445` (`sendMessage`) and `:448-451` (`sendUserMessage`). Implementation: `core/agent-session.ts:2246-2282` (`sendCustomMessage`).

| Situation | Behavior | Source |
|---|---|---|
| `deliverAs: "nextTurn"` | Held in `_pendingNextTurnMessages`, injected beside the next user prompt. Never starts a turn. | `agent-session.ts:2259-2260`, injected at `:2041` |
| Streaming, `triggerTurn` not false, `deliverAs: "followUp"` | `agent.followUp(msg)`: delivered when the loop would stop. | `:2261-2263` |
| Streaming, `triggerTurn` not false, otherwise | `agent.steer(msg)`: delivered after current tool results, before the next model call. | `:2261-2265` |
| Streaming, `triggerTurn: false` | Held in `_pendingCustomMessages`, appended to the session only after the turn's tool results exist. Nothing is emitted until then. | `:2273-2278`, flushed at `:2300-2308` and `:1165` |
| Idle, `triggerTurn: true` | `_runAgentPrompt(msg)` starts a new run. If called from inside the `agent_settled` emission it is deferred to run after the emission finishes. | `:2267-2272`, deferral list at `:417-418`, drained at `:1061` |
| Idle, no trigger | Appended to the session, no turn. | `:2279-2280` |

Why the "streaming + no trigger" row exists: regression test `test/suite/regressions/8537-custom-message-tool-result-ordering.test.ts` shows that a background task notifying while a tool is still executing would otherwise be appended between an assistant tool call and its tool result, which providers that validate message order reject on replay. This is a direct pitfall for us (section G).

`sendUserMessage` (`:2318-2347`) always triggers a turn. When streaming, `deliverAs` maps to `streamingBehavior` and ends up in `steer()` or `followUp()` (`:1965-1972`). Extension commands cannot be queued (`:2221-2230`).

The `agent_settled` event (`:1053-1061`, regression `6363-agent-settled-event.test.ts`) fires once after retry, compaction, and queued continuations have all finished. It is the right hook for "the agent is really idle now".

### A4. Which role carries the notification

- Session layer: `CustomMessage` has `role: "custom"`, `customType`, `content`, `display`, `details` (`core/messages.ts:46-53`).
- Provider layer: `convertToLlm` maps `custom` to `role: "user"` with the same content (`core/messages.ts:162-169`). Steer and follow-up text messages are created as `role: "user"` directly (`agent-session.ts:2198`, `:2215`).
- `bashExecution` (the `!` command) is also converted to a user message with `Ran \`cmd\`` plus fenced output (`messages.ts:82-90`, `:152-161`).
- Nothing is wrapped in XML by Pi core. Wrapping is left to the extension that calls `sendMessage`.

### A5. The durable package: the only real task runtime in Pi

`packages/durable` (spec at `packages/durable/docs/spec.md`, tasks at `:1568`) is a different architecture, a SQLite-backed "harness", but it contains the best-documented design for background tasks and idle wake that I found.

- Task states: `pending`, `running`, `waiting` (parked on other tasks), `completing`, `terminal`. Outcomes: `completed`, `failed`, `aborted`, `orphaned`, `faulted` (`spec.md:1573-1620`). Each phase ends with a saved checkpoint, so a restarted process continues from the last checkpoint.
- Ownership is explicit. Every task and conversation has an owner. Abort and idle-wait traverse ownership. A `background: true` task is a boundary: the user's Esc and idle waits stop there, while `abort({background:true})` reaches past it (`spec.md:1869-1900`, example comments in `test/examples/23-subagent-background.ts`).
- Inbox: submissions to a busy conversation go to a persistent `pi.inbox` document with mode `steer` or `followUp` (`src/harness/inbox.ts:8-14`, `submissions.ts:148-207`). An idle conversation places the input and starts a run in the same commit. `whenBusy: "reject"` returns `ConversationBusy` instead of queueing (`types.ts:61`).
- Boundary application (`inbox.ts:65-106`): at `postTools` it takes steers, at `final` it also takes follow-ups, in submission-ID order, honoring the same one-at-a-time or all modes.
- Idempotency: `requestId` on a submission returns the existing submission instead of a duplicate (`submissions.ts:155-163`). The background sub-agent example uses `requestId: "subagent-report:<taskId>"` so a restarted reporter task cannot report twice.
- Completion delivery in the example (`23-subagent-background.ts`): a "reporter" task waits for the sub-agent answer, then submits a `followUp` input to the main conversation. If the main agent is idle, that starts a turn; if busy, it waits for the current answer. If the user presses Esc, queued reports are dropped with other queued input, but a report arriving after Esc starts a new turn. The report text is `[subagent <name> answered, no reply needed] <text>`, which is a user-role message.
- Replay safety: tools declare `replay: "unsafe"` so an interrupted call is not rerun after a crash.

### A6. Pi summary for Ask

Pi gives us the cleanest boundary vocabulary (`steer` at post-tools, `followUp` at stop, `nextTurn` with the next user prompt, `trigger` to wake idle) and the ordering lesson (never append a message between a tool call and its result). Pi core gives us no task registry, no background bash, and no timer service. `durable` gives us the registry/ownership/idempotency ideas but is a different persistence architecture.

## B. dewee (local)

dewee is a multi-tenant Go gateway. Its async handling is split across a lane scheduler, an in-process bus, and tool-level managers. It has no background-shell tool (no `run_in_background` or `notify_on_complete` found under `internal/`).

### B1. Scheduler, lanes, per-session serialization

- Lanes are named semaphores: `main`, `subagent`, `team`, `cron`, `workflow`, `event` with env-tunable concurrency (`internal/scheduler/lanes.go:21-29`, `:161-170`). `Lane.Submit` blocks on a token channel or context cancel and runs the function in a goroutine (`:77-106`).
- Each session gets a `SessionQueue` keyed by tenant and session key (`scheduler.go:39-50`, `:109-135`). `maxConcurrent` is 1 by default, so runs for one session are serial (`queue.go:45-56`). Near the summary threshold the queue drops concurrency to 1 (`:141-154`).
- Queue modes: `queue`, `followup`, `interrupt` (`queue.go:16-29`). In the code, `followup` falls into the same branch as `queue` (`queue.go:192`), so it has no distinct behavior. `interrupt` cancels active runs and drains the queue (`:178-191`).
- Queue limits: default cap 10, drop policy `old` (`queue.go:48-57`). `applyDropPolicy` returns `ErrQueueDropped` to the dropped caller (`:363-392`).
- Debounce: 800 ms timer reset on each enqueue (`queue.go:209-231`).
- Abort cutoff: `CancelAll` sets `abortCutoffTime`; queued items enqueued before the cutoff are skipped as stale (`queue.go:244-262`, `:430-445`). `Reset` bumps a generation counter so late completions from a cancelled generation do not corrupt state (`:473-486`).
- Graceful shutdown: `MarkDraining` rejects new work with `ErrGatewayDraining` while active runs finish (`scheduler.go:75-78`).
- Heartbeat skips agents that have active sessions (`scheduler.go:169-184`, `internal/heartbeat/ticker.go`).

### B2. Bus

`internal/bus/bus.go`: buffered inbound/outbound channels (1000) with blocking and `TryPublish` (non-blocking, drops when full) variants (`:38-63`); event subscribers by ID with local-only variants (`:117-146`); optional distributed fanout that polls every 250 ms (`:11-15`). `inbound_debounce.go` merges rapid messages from one sender into one inbound message. The bus is the transport for "system"-channel messages, including sub-agent announces.

### B3. Sub-agents and how completion reaches the parent

- Registry: `SubagentManager.tasks` map of `SubagentTask` with ID, status `running|completed|failed|cancelled`, termination reason (`final_response`, `iteration_limit`, `empty_response`, `cancelled`, `provider_error`), origin routing fields, and per-task `cancelFunc` (`internal/tools/subagent.go:35-97`, `:100-116`). Only `final_response` yields `completed`, so a budget-exhausted run is never reported as success (`:43-53`).
- On finish, `runTask` builds an `AnnounceQueueItem` and enqueues it in `AnnounceQueue` under key `announce:<parent>:<chat>` (`subagent_exec.go:28-65`). Without the queue it publishes an inbound message on channel `"system"` directly (`:66-115`).
- `AnnounceQueue` coalesces per session: a 1000 ms `time.AfterFunc` reset on every enqueue, and an immediate drain at 20 items (`tools/announce_queue.go:46-126`).
- The drain publishes a `system`-channel inbound message; `cmd/gateway_consumer_handlers.go:40-159` converts it into a second per-session `BatchQueue` entry. `orchestration/batch_queue.go:9-67` is a "first enqueuer becomes the processor" pattern: `Enqueue` returns `isProcessor=true` once, `Drain` takes all, `TryFinish` atomically ends the loop when empty.
- The processor loop (`cmd/gateway_subagent_announce_queue.go:130-241`) merges entries into one text and calls `sched.Schedule(ctx, LaneSubagent, RunRequest{RunKind:"announce", HideInput:true, ...})`, then blocks for the outcome and publishes the reply on the outbound bus. So the completion starts a normal agent run on the parent's session key. It queues behind any active run through `SessionQueue`. It is not injected mid-run.
- Message text (`tools/announce_queue.go:139-247`): `[System Message] A subagent task "<label>" just completed successfully.\n\nResult:\n...\n\nStats: runtime, iterations, tokens`, followed by a deterministic roster block `[running  ] label` and a reply instruction. While others still run, the instruction says to wait for the remaining results if related, and to reply `NO_REPLY` if already delivered. When all are done, it tells the model to rewrite in its own voice and not echo the block verbatim. The roster exists "to prevent LLM hallucination" (comment at `:138`).
- Failure labelling: `SubagentFailureLabel` distinguishes `ended incomplete (iteration_limit)` from `failed (provider_error)` (`:195-203`).
- Output limit for results: `maxMessageChars` truncation exists for injected user messages (`agent/inject.go:55-60`); sub-agent results are passed through whole. I did not find a separate cap on `task.Result` in the announce path. Treat this as unverified.
- Cancellation: `CancelTask`, `CancelTasksForParent` (`subagent_control.go:56-112`). `Steer` of a sub-agent cancels it and respawns a replacement with the new message (`:171-224`).
- Persistence: tasks are written fire-and-forget (`go sm.persistStatus(...)` at `subagent_exec.go:423`). At gateway start, `MarkRunningNativeTasksInterrupted` flips all `running` native tasks to `failed` with reason `interrupted by gateway restart` (`cmd/gateway.go:1428-1435`, `store/sqlitestore/subagent-tasks.go:309-318`). I found no re-announce or resume path. The announce queue itself is in memory only.
- Archive: completed tasks are deleted from the in-memory map after 30 minutes by a sleeping goroutine (`subagent_control.go:19-31`, `ArchiveAfterMinutes` at `subagent.go:29`).

### B4. Mid-run injection (user messages, not completions)

- Each running agent has a buffered `InjectCh` of size 5 (`agent/inject.go:30`, `router.go:343-354`). `InjectMessageInTenant` does a non-blocking send and returns false when full (`router_tenant_scope.go:151-161`).
- The pipeline drains `InjectCh` in `ObserveStage` after tool execution, before the next model call (`pipeline/observe_stage.go:15`, `:149-152`).
- Injected text is scanned by the input guard, truncated, and wrapped: `[User sent a follow-up message while you were working]\n<text>`. The LLM sees the wrapped form; the session stores the original (`agent/inject.go:36-85`).
- The consumer only injects when an intent classifier says "steer"; if injection fails (channel full) it falls back to queueing as a new run (`cmd/gateway_consumer_normal.go:558-577`).
- Sub-agent announces do not use `InjectCh`. They always go through the scheduler as new runs.

### B5. Cron and heartbeat

- Cron service (`internal/cron/service_execution.go`): a 1-second ticker scans jobs (`:167-179`). Due jobs have `NextRunAtMS` cleared and the store saved before execution to prevent duplicates, then each runs in its own goroutine with panic recovery (`:192-242`). After running, "every" jobs advance from the original anchor to avoid drift (`:314-320`); one-shot `at` jobs are deleted or disabled (`:308`, `:324-326`). Retries use `ExecuteWithRetry` with backoff and jitter (`cron/retry.go`).
- Startup catch-up (`cron/service.go:50-102`): jobs whose time passed during downtime are not fired; their next run is recomputed from now, and past-due one-shot jobs are disabled. The code comments warn that after long downtime all same-interval jobs synchronize.
- The job handler (`cmd/gateway_cron.go:127-185`) wraps each run in a timeout context, resets the session before the run (unless stateless), registers an outcome recorder, and schedules a `RunRequest` through `LaneCron`, then blocks on the result or the timeout.
- Heartbeat (`internal/heartbeat/ticker.go:22-25`): 30 s poll, 5 minute minimum interval, skipped when the agent has active sessions.

### B6. dewee patterns worth taking

- One serial queue per `(tenant, session)` plus bounded lanes per work class.
- `BatchQueue` "first enqueuer is the processor" with atomic `TryFinish`.
- Generation counter to ignore late completions after a reset.
- Abort cutoff timestamp to discard stale queued work.
- Deterministic roster in the notification to stop the model inventing task state.
- Distinct failure labels (`incomplete` versus `failed`) so the model does not treat budget exhaustion as an outage.

## C. External systems

Source quality: Claude Code official docs and Codex source constants are primary. Hermes PR/source and OpenCode source are primary. aither #170 and goose #11740 are open design proposals, not shipped behavior. Issue threads are used for reported pitfalls, each is one reporter's account.

### C1. Claude Code

Primary evidence: the Bash tool contract visible to this agent session says `run_in_background` "keeps running across turns and re-invokes you when it exits", with a default 30 minute and max 2 hour limit, and that foreground `sleep` is blocked in favor of the Monitor tool. Official docs: [Tools reference](https://code.claude.com/docs/en/tools-reference), [Interactive mode: how backgrounding works](https://code.claude.com/docs/en/interactive-mode), [Scheduled tasks](https://code.claude.com/docs/en/scheduled-tasks), [Headless: background tasks at exit](https://code.claude.com/docs/en/headless).

- Registry shape: task ID, type (shell, Monitor watch, subagent/agent, workflow), `/tasks` listing, `TaskStop` by ID or name. `TaskOutput` was removed in v2.1.277; the model now reads an output file with the normal `Read` tool.
- Output: stdout and stderr redirect to a file, so output never enters context unless the model reads it. A task whose output passes 5 GB is killed with a note in stderr ([docs](https://code.claude.com/docs/en/interactive-mode), [summary](https://backgroundclaude.com/blog/background-commands)).
- Triggering: model sets `run_in_background`, or user presses Ctrl+B, or a foreground command that hits its timeout is moved to the background automatically (not when it starts with `sleep`). The move result states "did not complete within its 120s timeout and was moved to the background" plus task ID and output path, and warns that `cd` inside the moved command does not carry over.
- Time limits: none in an interactive local session; 30 minutes default and 2 hours max for unattended runs (`-p`, SDK, CI, cloud), 10 minutes in `-p` text mode. At the limit the command is stopped and the model is told why.
- Completion: "When a background task completes, fails, or is stopped, Claude Code sends Claude a task notification." Idle wake is confirmed by two official sources: the live Bash tool contract ("re-invokes you when it exits") and the headless page, which says the run waits for the command and "Claude then takes one more turn with the outcome". Unverified: where a completion that arrives mid-turn lands (tool-result boundary versus end of turn); the pages I read do not say.
- Notification format: Anthropic does not publish a schema. Third-party parsers report a user-role `<task-notification>` block with `task-id`, `tool-use-id`, `output-file`, `status`, and `summary`, plus `result`, usage, and duration for sub-agents ([aegis #699](https://github.com/apiad/aegis/issues/699), [Trail-Code #500](https://github.com/trailhq/Trail-Code/issues/500), [ClaudeCodeUI #4](https://github.com/Atypical-Consulting/ClaudeCodeUI/issues/4)). Treat the exact field list as observed, not specified.
- Monitor tool: runs a script in the background and feeds each output line to the model as an event. It can also take a WebSocket source (one event per text message, ends on messages over 1 MiB). Every watch has a deadline (5 minutes default, 30 minutes max) after which the model gets one notice. This is the pattern for "react to a pattern in output" without polling.
- Cron: `CronCreate/List/Delete`, up to 50 per session. The scheduler checks every second and enqueues due prompts at low priority. A scheduled prompt "fires between your turns, not while Claude is mid-response"; if busy it waits for the turn to end. Missed fires are not caught up: if time passes during a long turn, it fires once when idle. Recurring tasks expire after 7 days. Jitter spreads load (up to half the interval, max 30 minutes late). `--resume` restores unexpired cron tasks but does not restart background shells or monitors.
- Lifecycle: tasks die with the session unless the session is itself backgrounded. A command started by a foreground sub-agent stops when that sub-agent ends; one started by the main agent or a background sub-agent keeps running. `-p` runs stay open after the result until background commands end, then run one more turn with the outcome.
- Reported pitfalls: finished jobs generated endless "still running" reminders, about 3,200 tokens each ([#11190](https://github.com/anthropics/claude-code/issues/11190), [#11716](https://github.com/anthropics/claude-code/issues/11716)). Three parallel agents finished but only one notification arrived ([#20754](https://github.com/anthropics/claude-code/issues/20754)). A `completed` notification arrived while the task was still alive; closed as a duplicate of #77284 ([#81048](https://github.com/anthropics/claude-code/issues/81048)). Hooks fire on notification text as if it were a user prompt ([Trail-Code #500](https://github.com/trailhq/Trail-Code/issues/500)).

### C2. OpenAI Codex CLI

Primary evidence: `codex-rs/core/src/unified_exec/mod.rs:68-77`.

- Shape: `exec_command` starts a PTY or pipe process and waits up to `yield_time_ms`. If still running it returns the output so far plus a `session_id`. The model continues with `write_stdin(session_id, chars, yield_time_ms)`. An empty `chars` is a poll.
- Constants: yield clamp 250 ms to 30 s; minimum empty-poll wait 5 s; default max background wait 300 s; default 10,000 output tokens per call; 1 MiB output buffer; at most 64 processes.
- States are implicit (running or exited, with the process pruned). `/ps` shows each terminal with up to three recent output lines. `/stop` stops all background terminals for the thread; a request to stop one by ID was closed as not planned ([#17821](https://github.com/openai/codex/issues/17821)).
- Completion delivery: none to the model. Codex emits an `ExecCommandEnd` event to the UI only. The model must poll `write_stdin`. Reported cost: each poll is a full turn that resends the whole context, one session burned 21.6M input tokens in 90 polls for an already-finished command ([#38495](https://github.com/openai/codex/issues/38495), [#13733](https://github.com/openai/codex/issues/13733)).
- Proposed fix, still open: `on_exit: "wake"` on `exec_command` ([#32188](https://github.com/openai/codex/issues/32188)). The proposal lists the requirements well: wake an idle thread exactly once, never lose a completion at the answer boundary, no duplicate if the model already saw it via `write_stdin`, strictly bounded injected output, no unbounded continuation turns for processes finishing together, no stale wake after cancel or shutdown. A `monitor` tool is also requested ([#29922](https://github.com/openai/codex/issues/29922)). A community fork commit implements wake-on-completion ([tekacs/codex@9ffcf8d](https://github.com/tekacs/codex/commit/9ffcf8db9078eae43d4111ff94259795c1e962c9)); I saw only its description in search results, not the diff.
- Interrupt: pressing Stop ends the turn but leaves the process running ([#42717](https://github.com/openai/codex/issues/42717)). Model reliability drops once a command outlasts the default yield ([#33816](https://github.com/openai/codex/issues/33816)).
- Persistence: none found.

### C3. OpenCode

Repo moved from `sst/opencode` to `anomalyco/opencode`. PR [#33310](https://github.com/anomalyco/opencode/pull/33310) (bash `run_in_background`) is closed, not merged; a later shell tool with `background=true` exists on current code, evidenced by [#58049](https://github.com/anomalyco/opencode/issues/58049).

Primary source: `packages/core/src/job.ts`, `packages/core/src/session/subagent-job.ts`, `subagent-completion.ts`.

- Registry: one process-local `Job` service with `start`, `wait`, `block`, `background`, `backgroundAll`, `cancel`, `pendingBackground`, `runningBackground`, `completeBackground`. Status is `running|completed|error|cancelled`. A job started in the foreground can be promoted with `background()` while a caller is blocked on it. This is the same "foreground that becomes background" idea as Claude Code's timeout move.
- A `recovery` union (`shell` with sessionID, shellID, command; `subagent` with parent/child session, agent, description) plus a `notificationID` is persisted to KV before the result is delivered (`job.ts` `persistBackground`). `completeBackground(notificationID)` clears the marker after the notification is admitted. After a restart, pending markers say which notifications still owe delivery. The 25-entry consumed-history cap bounds memory.
- Delivery: `sessions.synthetic({id: notificationID, sessionID: parent, resume, text, metadata})` creates a synthetic session message. The sub-agent text is `<subagent sessionID="..." state="completed|error|cancelled" description="...">\n...\n</subagent>`. The `resume` flag lets a caller deliver without waking the agent. Using `notificationID` as the message ID gives idempotency. One observer per job generation prevents duplicate observers (`subagent-job.ts` `notifications` set).
- The shell notification text format was not located; `session/shell.ts` shows `metadata: { background: true }` at line 17 but I did not find the message text. Unverified.
- Reported pitfall ([#58049](https://github.com/anomalyco/opencode/issues/58049)): an idle sweep evicted the "location", killed a running background shell, deleted its output file, and never delivered the promised completion. Fix shipped in v2.1.0: the idle sweep no longer evicts a location with running jobs ([#57416](https://github.com/anomalyco/opencode/issues/57416), as cited by the reporter).
- Third-party plugins confirm the same shape and a "stall watchdog that notifies once and never kills" ([RohanAwhad/opencode-background-shell](https://github.com/RohanAwhad/opencode-background-shell)).

### C4. Hermes agent (`notify_on_complete`)

[PR #5779](https://github.com/NousResearch/hermes-agent/pull/5779), merged 2026-04-07, plus current `tools/process_registry.py` (constants and fields below are from main at fetch time).

- Registry: `ProcessRegistry` with `ProcessSession` (`proc_<12 hex>`, `notify_on_complete`, `watch_patterns`, `exit_code`, rolling `output_buffer`, `detached` for sessions recovered from checkpoint). Running, then exited. Finished entries are kept 1800 s; at most 64 tracked with LRU pruning.
- Output: 200,000 character rolling buffer; the completion notification carries only the last 2,000 characters (`COMPLETION_OUTPUT_CHARS`), with an `output_cut` count when trimmed. A spawner whose output is the payload can raise this per process.
- Completion path: a reader thread detects exit, `_move_to_finished()` puts a dict on `completion_queue`. CLI: the queue is drained after a turn and in the idle loop and becomes the next `_pending_input` item. Gateway: a watcher (5 s poll interval, per the PR) builds a synthetic `MessageEvent` and calls the platform adapter, which runs a full agent turn. The PR chose a new turn over mid-loop injection explicitly to keep the prompt cache valid.
- Format: `[SYSTEM: Background process proc_abc123 completed (exit code 0).\nCommand: pytest tests/\nOutput:\n42 passed in 3.2s]`.
- Coalescing: gateway fans completions into a 0.1 s window per route key and merges them into one turn ([#117555](https://github.com/NousResearch/hermes-agent/issues/117555) cites `gateway/run_notifications.py:1583`).
- Mid-output signals: `watch_patterns` with a per-session rate limit, 3 consecutive strike windows or 8 lifetime hits disables watching and falls back to notify-on-complete, plus a global breaker of 15 per window.
- Persistence: a checkpoint file `processes.json` stores sessions including `notify_on_complete`; recovery re-adopts live PIDs (checked with start time to avoid PID reuse) and prunes dead ones. Pitfall reports: the Desktop/serve backend never runs recovery, so restarts silence all pending notifications ([#143923](https://github.com/NousResearch/hermes-agent/issues/143923)); a reaped session's wake is discarded by whichever poller drains the shared queue first ([#129071](https://github.com/NousResearch/hermes-agent/issues/129071)); the ACP adapter never drains the queue, so completions after `session/prompt` returns are lost ([#62548](https://github.com/NousResearch/hermes-agent/issues/62548)).
- Other reported pitfalls: no per-session budget on completion wakes, and a foreground timeout auto-promotes to background with `notify_on_complete=true`, so the agent can accrue wakes it never asked for ([#117555](https://github.com/NousResearch/hermes-agent/issues/117555)); a `poll` that already handled the result still leaves a queued notification that later produces a stale turn ([#94455](https://github.com/NousResearch/hermes-agent/issues/94455)); `terminal(background=true)` bypassed the dangerous-command consent gate ([#90789](https://github.com/NousResearch/hermes-agent/issues/90789)); a delegate child's completion ping re-pinned the Slack thread to the child session ([#132360](https://github.com/NousResearch/hermes-agent/issues/132360)).

### C5. aither issue #170 (open design, not shipped)

[lexoliu/aither#170](https://github.com/lexoliu/aither/issues/170) describes the exact problem we are solving.

- Current behavior (to be removed): after the model ends its turn with jobs running, the agent polls a channel with `recv_timeout` every 100 ms for up to 300 s, keeping the run open. A job finishing after 300 s wakes nothing and sits in a channel until the next user prompt.
- Proposed design: a run ends when the model ends its turn. Mid-run delivery stays (inject after the next tool call). Completions when no run is open are exposed to the host as an async event stream (`Agent::job_events()`). Each event has task ID, script, final status (exit code, failure, or kill with cause), and output preview or path. Events consumed by mid-run injection are not repeated. A run entry point accepts a batch of inputs (user messages plus job events) rendered in one request. The host turns one coalesced batch into exactly one run. No timers or polling remain in the agent.
- Acceptance tests listed there are directly reusable as our test list: run returns at once with a job running; mid-run completion appears in the next request and not on the stream; post-run completion is yielded with status and output; two events plus one user message yield one request.
- Note: it speaks of "system messages"; the injected form still has to be a role the provider accepts (see section E).

### C6. goose issue #11740 (open proposal)

[aaif-goose/goose#11740](https://github.com/aaif-goose/goose/issues/11740).

- Proposal: a single task registry in the runtime with `TaskSource` reserved for `Subagent`, `Process`, `McpResource`, `McpTool`, `SwarmAgent`, `Timer`. First surface is process and timer. Tools: `start_task`, `read_output`, `send_input`, `wait_for` (pattern match wakes the model), `cancel_task`, `get_task`, `list_tasks`. States: Working, Complete, Failed, Cancelled, plus InputRequired, mapped one-to-one to the MCP Tasks extension (`working`, `input_required`, `completed`, `failed`, `cancelled`).
- Notification: a `TaskNotificationOperation` in the state machine drains finished tasks between turns and can steer an active run. Per-task interrupt policy is proposed.
- Honest limit reported by the author: the goose CLI input loop is blocking, so a background event cannot wake an idle prompt. The prototype prepends pending completion notifications to the user's next message. Out-of-prompt updates over ACP are dropped ([goose #11320](https://github.com/aaif-goose/goose/issues/11320)). A maintainer asked to land sub-agents in the core state machine first ([#11539](https://github.com/aaif-goose/goose/issues/11539)).
- Output: truncated with line and byte limits and the on-disk log path. Per-session temp log dirs with GC.

### C7. voocel/agentcore (Go)

Source read at `d8f5722`: `task/task.go`, `task/tools.go`, `agent.go`, `tools/bash.go`, README lines 190-197.

- Registry: `task.Runtime{dir, notify, jobs map[string]*job, active int, idle *sync.Cond}`. `Entry` carries ID (`shell-1`, `subagent-2`), type, description, status, start and end time, `OutputFile`, error, plus shell fields (command, exit code) and sub-agent fields (agent, prompt, result, tool count, tokens) (`task.go:40-63`).
- States: `running`, `completed`, `failed`, `killed` (`:25-31`).
- `Start` (`:128-148`) allocates ID, creates a temp output file, registers the job, then runs `work` on a goroutine with a context that keeps values but not the caller's cancellation (`context.WithoutCancel`). So a task outlives the tool call or run that started it.
- `end` (`:171-196`) sets the terminal state (killed if `Stop` was called, failed if the work returned an error, else completed), calls `notify(notification(ended))`, and only then closes `done` and decrements `active`. The announcement therefore counts as active work, so `Wait()` (`:267`) at shutdown cannot return before the notification was handed off.
- Notification (`:299-325`): user-role message tagged `Kind = "task_notification"`, body `<task-notification><task-id/><type/><status/><summary/>[command|agent,result,usage]<error/><output-file/></task-notification>`. Every field is XML-escaped so task output cannot forge a notification.
- Delivery: the README wires `notify` to `agent.FollowUp(m)`. `Agent.FollowUp` (`agent.go:114`) appends to a mutex-protected slice that the loop pulls "once the run under way, or else the next, would stop". If no run is open, nothing starts: the host has to call `Prompt` itself, and `Prompt` returns `ErrBusy` during a run (`agent.go:92`). `ClearQueues` (`:123`) exists so a host can find messages that arrived as a run ended. This is the idle-wake gap.
- Output limits: bash tail 2000 lines or 50 KB; foreground timeout default and max 10 minutes ("longer belongs in the background", `tools/bash.go:22-27`); background commands have no timeout unless requested; `task_output` returns a 32 KB tail (`task/tools.go:19`).
- Model-facing tools: `task_output(task_id, wait, timeout)` and `task_stop(task_id)`. `task_output wait` blocks inside a tool call for up to 2 minutes by default (`tools.go:17`, `:66`) on `WaitFor`, a channel wait on `done`, not a sleep loop. It is bounded, but it is a model-driven wait, so it is the thing our rule wants to discourage.
- Cancellation: per-job cancel func; `Stop` marks `stopped` first so the end state is `killed` (`:224-237`). `StopAll` exists.
- Persistence: none. Output files only.

### C8. compforge/agentgo/task (Go)

Source at `237e77c`; docs on [pkg.go.dev](https://pkg.go.dev/github.com/compforge/agentgo/task). Reads as a derivative of the agentcore shape with extensions.

- Registry: `Runtime{tasks map[string]*Entry}` with `Register`, `Get`, `List`, `Stop`, `StopAll`, `Update`, `Done`, `Wait` (`task/task.go:131-300`). Types add `teammate`. Status is `running|completed|failed|killed`, with `IsTerminal()` (`:127`). Depth limit `MaxAgentDepth = 5` via context (`:55`).
- Quiescence: a task stays `active` until its owner calls `Done(id)` "after all completion work, including notifications" (`:157-163`, `:240`). `Wait()` blocks on a `sync.Cond` for zero active tasks and includes work registered while waiting, "so shutdown can wait for nested work without polling" (`:268`).
- Parent-to-child messages: `AppendPending(id, msg)` returns `AppendOK`, `AppendNotFound`, or `AppendTerminal`; the child drains at its steering tick (`:290-330`). Terminal tasks are not auto-resumed.
- Notification: `NotificationFromEntry(e)` then `.ToAgentMessage()` yields a user message wrapped in `<background-task-completed>` with `task-id`, `type`, `status`, `summary`, optional `agent`, `command`, `exit-code`, `error`, `result`, `usage`, `output-file` (`:350`, `:383`, `:423-462`). Unlike agentcore, the values are not escaped, so a result containing a closing tag can break or forge the block. This is a defect to avoid.
- Bash wiring: `BashTool.SetNotifyFn` is "typically bound to Agent.FollowUp" (`tools/bash.go:38-42`).
- Idle wake is built in: `Agent.Inject(ctx, msg)` returns one of three dispositions (constants at `inject.go:7-14`, function at `:29`): `steered_current_run` (running), `resumed_idle_run` (idle and the transcript tail is an assistant message), or `queued` (idle without an assistant tail). `HoldRuns` makes inject return `ErrRunsHeld` and queue nothing, which prevents double delivery while a caller reroutes (`inject.go:36-46`). Runs are guarded by a mutex so check-and-resume is atomic (`:30-31`).

### C9. MCP Tasks extension (relevant to MCP notifications)

[MCP Tasks overview](https://modelcontextprotocol.io/extensions/tasks/overview). The server returns a durable `taskId` with `ttlMs` and `pollIntervalMs`; the client polls `tasks/get`; states `working`, `input_required`, `completed`, `failed`, `cancelled` (terminal: the last three); cancellation is cooperative. Servers "can push status updates via `notifications/tasks`" if the client subscribes with `subscriptions/listen`, each carrying the full task state; "polling is the default". For us this means an MCP task source will need either a subscription or a bounded poller owned by the runtime (outside the model loop).

## D. Where reference systems still poll or sleep

If the Ask rule is "completion wakes the agent by push, never by sleep or polling", note these in systems we may copy from:

| System | Location | What it does | Verdict |
|---|---|---|---|
| dewee | `internal/tools/subagent_control.go:232` | `WaitForChildren` ticks every 500 ms | Replace with a done-channel wait |
| dewee | `subagent_control.go:20` | `scheduleArchive` sleeps in a goroutine | Replace with a timer owned by the registry, stopped at shutdown |
| dewee | `cmd/gateway_subagent_announce_queue.go:158` | 50 ms sleep inside the batch processor loop to avoid spin | Avoid by blocking on the queue's wake channel |
| dewee | `internal/bus/bus.go:12` | Distributed fanout polls every 250 ms | Only relevant if we add multi-process fanout |
| dewee | cron 1 s ticker (`service_execution.go:167`) and heartbeat 30 s poll | Timer scan of due jobs | Acceptable in a host; better as a single `time.Timer` armed at the earliest due time |
| Hermes | PR #5779 gateway watcher | 5 s poll for process exit | Host poll of a local process; avoidable with a wait goroutine |
| agentcore | `task_output wait` | Model-driven bounded wait up to 2 min | Same cost as polling if used in a loop |
| Claude Code | prior BashOutput 30 s polling guidance | Model polls | Replaced by notifications in current versions |
| aither (before #170) | `recv_timeout` 100 ms for 300 s | Run held open | The anti-pattern #170 removes |

## E. Synthesis

### E1. Design elements shared by the working systems

1. A registry owned by the runtime, not by a tool. Entries have a stable ID, a type, a small fixed set of states (`running` plus terminal states: completed, failed, killed/cancelled), start and end times, a cancel function, and an output reference. Claude Code, OpenCode, agentcore, agentgo, Hermes, goose, and Pi `durable` all have this. Pi core, dewee, and Codex do not.
2. Tool call returns immediately with the task ID and the output location. Work continues on a context detached from the caller's cancellation but tied to a registry-owned cancel (agentcore `context.WithoutCancel`, Claude Code tasks, OpenCode `Job.start`).
3. Output goes to a file (or rolling buffer) and the model gets only a bounded tail plus the path: Claude Code (5 GB cap, file read on demand), Hermes (200 K buffer, 2 K tail), agentcore (50 KB tail, 32 KB `task_output`), Codex (10 K tokens per call, 1 MiB buffer), Pi (2000 lines or 50 KB for tool results).
4. Exactly one terminal notification per task, produced by the registry on every terminal state, including killed and failed. agentcore and agentgo build it inside `end`. OpenCode persists a notification marker first.
5. The notification is a user-role message with a structured wrapper: `<task-notification>` (Claude Code, agentcore), `<background-task-completed>` (agentgo), `<subagent ...>` (OpenCode), `[SYSTEM: ...]` (Hermes), `[System Message]` (dewee), `[subagent X answered, no reply needed]` (Pi durable example). No system examined uses a real `system` role. Pi converts an internal `custom` role to `user` at the provider boundary.
6. Two delivery paths decided by the agent state at completion time: running goes to a steering queue drained after tool results and before the next model call; idle starts a new run. Pi (`steer`/`followUp`/`triggerTurn`), agentgo (`Inject` dispositions), aither (event stream plus mid-run injection), Claude Code, and Hermes (CLI idle loop) all do this.
7. Coalescing of near-simultaneous completions into one run: dewee (1000 ms `AfterFunc` plus cap of 20, then `BatchQueue`), Hermes (0.1 s window), aither (one batch is one run), Pi's `"all"` queue mode, Codex #32188 requirement.
8. A run ends when the model ends its turn, regardless of live tasks. Holding the run open is called out as the anti-pattern in aither #170. Claude Code `-p` is the one deliberate exception, with a ceiling and an explicit final turn.
9. Cancellation through the registry: `Stop(id)`, `StopAll()`, and ownership-based cascade (Pi durable, Claude Code foreground sub-agent rule).
10. Shutdown waits for active tasks including their pending notification handoff (agentcore `end` ordering, agentgo `Done`).

### E2. Where they differ

| Dimension | Range seen | Notes |
|---|---|---|
| Idle wake | In-agent (agentgo `Inject`, Pi `triggerTurn`, Claude Code, Hermes CLI/gateway) versus host must start the run (agentcore, aither design, goose CLI, Codex today) | The first group needs an atomic "is the agent idle, and is the tail resumable" check |
| Mid-run boundary | After tool results (Pi, dewee InjectCh, agentgo, aither) versus new run after completion (dewee announces, Hermes) | Hermes chose new turns to keep the prompt cache stable |
| Persistence | None (agentcore, agentgo, Codex, Claude Code shells) versus checkpoint file (Hermes) versus KV marker plus recovery union (OpenCode) versus fully durable tasks (Pi durable) versus mark-failed-at-start (dewee) | Claude Code docs say `--resume` does not restart shells or monitors, but does restore cron |
| Timer model | Session cron fired only between turns, low priority, no catch-up (Claude Code) versus 1 s scan with persisted `NextRunAtMS`, anchor-based advance (dewee) | Both skip missed fires on restart rather than replay them |
| Output sink | Disk file (Claude Code, agentcore, OpenCode) versus in-memory rolling buffer (Hermes, Codex) | Disk file survives for the model to read later |
| Time limits | None locally, hard limits unattended (Claude Code); 10 min foreground max (agentcore); watch deadline 5 to 30 min (Claude Code Monitor) | Unbounded background tasks were acceptable only with a kill path |
| Pattern or line events | Monitor line events (Claude Code), `watch_patterns` with rate limits (Hermes), `wait_for` (goose proposal), `monitor` (Codex request) | Hermes shows this needs rate limits and a lifetime cap |
| Escaping | Escaped fields (agentcore), unescaped (agentgo, dewee) | Escaping is the only forge defense seen |
| Foreground-to-background | Auto-move on timeout (Claude Code, Hermes), Ctrl+B (Claude Code), `background()` promotion (OpenCode) | Hermes reports this causes unrequested wakes |

### E3. Pitfalls reported or visible in source

1. Lost wake on session ownership: events in a shared global queue are drained by the wrong poller or dropped for a non-live owner ([Hermes #129071](https://github.com/NousResearch/hermes-agent/issues/129071), [#62548](https://github.com/NousResearch/hermes-agent/issues/62548), [#132360](https://github.com/NousResearch/hermes-agent/issues/132360)). Route by owning session key and requeue what is not yours.
2. Lost wake on restart: Hermes serve mode, OpenCode idle-eviction, and dewee all lose pending completions in some paths ([#143923](https://github.com/NousResearch/hermes-agent/issues/143923), [#58049](https://github.com/anomalyco/opencode/issues/58049)). If persistence is not built, say so in the contract and mark orphaned tasks explicitly at startup, as dewee does.
3. Lost or duplicate wake in races: three agents finished, one notification ([Claude Code #20754](https://github.com/anthropics/claude-code/issues/20754)); a notification for a task the model already polled produces a stale turn ([Hermes #94455](https://github.com/NousResearch/hermes-agent/issues/94455)); Codex #32188 lists the same two cases as requirements. Use a per-task "delivered" flag set atomically with queueing and cleared only by explicit consume.
4. False terminal state: a `completed` notification while the task still ran ([Claude Code #81048](https://github.com/anthropics/claude-code/issues/81048)). Notify only from the registry's single terminal transition.
5. Endless reminders: Claude Code's old "still running" reminders cost about 3,200 tokens per turn ([#11190](https://github.com/anthropics/claude-code/issues/11190), [#11716](https://github.com/anthropics/claude-code/issues/11716)). Do not remind; notify once.
6. Unbounded wake count: no per-session budget on completion wakes, and implicit auto-backgrounding creates wakes the model did not request ([Hermes #117555](https://github.com/NousResearch/hermes-agent/issues/117555)). Hermes later added caps for watch patterns only.
7. Message ordering: a notification appended between an assistant tool call and its result breaks provider replay (Pi regression 8537). Deliver only at turn boundaries after tool results.
8. Full-context cost of each wake: every wake is a full model turn; coalescing and caps matter (Hermes 0.1 s window, dewee debounce, Codex 34.6M-token report [#38495](https://github.com/openai/codex/issues/38495)).
9. Unbounded or forged content: unbounded output in the notification; unescaped output forging the wrapper (agentgo, dewee). Third-party hooks reacting to notification text as user input (Trail-Code #500).
10. Hidden kill or leak at lifecycle edges: Codex Stop leaves the process alive ([#42717](https://github.com/openai/codex/issues/42717)); OpenCode idle eviction killed running jobs; a Hermes recoverable close killed work. Tie task lifetime to explicit owner rules, not to incidental session objects.
11. Security bypass: background mode skipping the approval gate ([Hermes #90789](https://github.com/NousResearch/hermes-agent/issues/90789)). The background path must call the same permission check as the foreground path.
12. Queue drop policy applied to notifications: dewee's session queue defaults to cap 10 with drop-oldest (`queue.go:48-57`, `:363-392`), and announces go through it. By reading the code, a burst of queued work could drop an announce. I did not test this. Completion notifications should never be subject to a drop-oldest policy.
13. Cron semantics: no catch-up and jitter are deliberate (Claude Code docs); dewee's after-downtime synchronization of same-interval jobs is a known side effect noted in its own comment (`cron/service.go:57-62`).
14. Idle wake preconditions: Pi `continue()` and agentgo `Inject` start a run only when the transcript tail is an assistant message; otherwise they queue. A completion arriving when the tail is a user or tool result message must queue, not start.

### E4. What this implies for Ask (ranked, for the owning design)

These are inferences from the evidence above, not prior decisions.

1. Make the registry the single producer of notifications, with one terminal transition per task and a `delivered` flag, as in agentcore `end` ordering plus OpenCode's `notificationID` idempotency key. Rationale: every reported lost or duplicate wake traces to multiple producers or consumers.
2. Make the delivery decision in one place that is atomic with the agent's run state, with the three outcomes of agentgo `Inject` (steered into current run, resumed idle run, queued) and a `HoldRuns` equivalent for shutdown and reroute. Use Pi's boundary vocabulary: tool-result boundary for steer, stop boundary for follow-up, and "deliver but do not trigger" for the nextTurn case.
3. Coalesce at the point where the idle run starts (aither #170 shape: a batch of events plus user messages makes exactly one run), using a short window, with a per-session cap on wake runs and a shared cap across all sources (tasks, timers, MCP, watches).
4. Wrap the notification as a user-role message with escaped structured fields, a stable `Kind` field on the app-level message (Pi `customType`, agentcore `Kind`) so the UI and hooks can tell it from user input, and a bounded tail plus an output path.
5. Keep output on disk with an in-memory bound, and cap per-notification text (Hermes 2,000 characters is a defensible default; Pi's 2000 lines or 50 KB is the existing tool-result limit).
6. Model-facing tools should be few: start (via the existing shell tool flag), stop, and a read-output path. Avoid any `wait` tool; agentcore's bounded `task_output wait` is the thing to leave out under our rule.
7. Timers and watches should be registry entries with source type, firing into the same notification path, not a second mechanism. Use one `time.Timer` armed at the earliest deadline rather than a ticker. Decide the missed-fire policy explicitly (both reference systems skip missed fires).
8. Decide persistence now. The minimum honest version is dewee's: persist task rows, mark `running` rows as orphaned/failed at startup, and emit one notification for them. OpenCode's durable marker is the next step. Pi `durable` level is a different architecture and not recommended.
9. Cancellation and shutdown: ownership-based cascade (Pi durable, Claude Code), `Stop` marking intent before cancel, and `Wait` that counts the notification handoff as active work.

## F. Sources and credibility

- Primary, read directly: Pi source and tests (local), dewee source (local), voocel/agentcore and compforge/agentgo source (cloned), OpenCode `job.ts`, `subagent-job.ts`, `subagent-completion.ts`, Hermes `process_registry.py`, Codex `unified_exec/mod.rs`, Claude Code official docs pages (tools reference, interactive mode, scheduled tasks, headless).
- Design proposals, not shipped behavior: aither #170, goose #11740, Codex #32188 and #29922, tekacs fork.
- Single-reporter issue threads, used only as pitfalls: Hermes #117555, #94455, #129071, #143923, #62548, #90789, #132360; Claude Code #11190, #11716, #20754, #81048; Codex #13733, #38495, #42717, #33816; OpenCode #58049.
- Third-party descriptions: `<task-notification>` field list for Claude Code, backgroundclaude.com summary (it says Claude Code has no push notifications, which contradicts the official docs page I read; I weighted the docs and the live tool contract).

## G. Limitations and unresolved questions

1. Claude Code's `<task-notification>` schema is not officially published. The field list is observed by third-party tools and mirrored by agentcore and agentgo. If we want byte-compatibility with Claude Code conventions, we need to capture a live transcript.
2. OpenCode's shell completion message text was not located; only the sub-agent `<subagent ...>` form was verified.
3. The tekacs/codex wake commit was seen only through a search summary, not its diff.
4. I did not verify whether dewee caps sub-agent result size in the announce path, nor whether a dropped announce (queue cap) is reproducible. Both are readings of the code.
5. Hermes line numbers come from issue text and main at fetch time; the registry has since been split into mixins (`process_registry_checkpoint.py`), so re-check before citing in a plan.
6. GitNexus was not used; direct grep and reads were sufficient for the questions asked.
7. Not covered: Ask's own current queue and `FollowUp` code, Windows process-group behavior, MCP notification handling beyond the Tasks overview page, and sandboxed execution.
8. Open question for the owner: should Ask persist tasks across daemon restart in the first version (section E4 item 8), given that the daemon can run as a long-lived local service or a remote agent?
9. Open question for the owner: is a hard per-session cap on wake-triggered runs acceptable, or must every completion always wake the agent? Hermes #117555 shows the unbounded choice is a known weakness.

Status: DONE_WITH_CONCERNS
Summary: Report covers Pi, dewee, and nine external systems with file and line citations, a synthesis, and pitfalls. Concerns: the Claude Code notification schema and OpenCode's shell notification text are not primary-verified, and the dewee announce-drop behavior is inferred from code, not tested.
