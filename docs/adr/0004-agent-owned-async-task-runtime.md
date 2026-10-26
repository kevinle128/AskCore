---
status: accepted
---

# The Agent owns async tasks and receives their completions by push

Work that ends after the call that started it (background shell commands, sub-agents, timers, watches, remote jobs) runs as a task in a per-Agent registry.
The registry runs each task on its own goroutine on the Agent lifetime context, not on the run context, so a task outlives the run that started it.
When a task ends, the registry produces exactly one notice and gives it to `Agent.Notify`, the only delivery port.
`Agent.Notify` maps the notice onto the existing claims: steering while a run is active, follow-up with wake while a run closes, and a new run while the Agent is idle.
No new input queue is added, as `internal/scheduler/README.md` requires.
Outside sources (MCP notifications, extension messages, chat inbound messages, cron fires) use the same port through an injected function.
Background shell work keeps its own job table and uses the same port as an outside source; see [ADR 0005](0005-shell-jobs-use-the-notify-port.md).

Following the user's decision of 2026-10-24, M1 has no concurrency limit: no worker pool, no per-class lane, and no per-Agent task cap.
Each task starts one goroutine when it starts, and that goroutine ends with the task.
If a downstream resource later needs protection (processes, provider rate limits), one `semaphore.Weighted` per resource class in `internal/scheduler` is added at `Registry.Start`, without a change to tools or `Agent.Notify`.

No component sleeps or polls to detect completion.
The only allowed polling is one isolated poller for remote systems without push, plus the cross-process file lock in `internal/settings`.
The existing sleep loop in `internal/acp/host.go` is replaced by an Agent idle broadcast.

Tasks are in memory only, which follows roadmap decision D10 (no crash-resume).
The registry keeps a store seam for durable tasks in `internal/messaging` later.

The full design, diagrams, and open decisions are in [async-task-runtime.md](../async-task-runtime.md).

## Rejected alternatives

A model-facing wait or status tool that the model calls until the task is done was rejected because each call costs one full model request and keeps the run open.

A goroutine pool (a fixed set of workers, `ants`, or `pond`) was rejected because the tasks are few, heavy, and I/O-bound; idle workers protect no resource and hide a queue that the model cannot see.

The LMAX Disruptor port `smarty/go-disruptor` was rejected because its consumers wait by polling (a sleep loop by default, a busy spin as the option), which costs CPU while Ask is idle, and because its throughput target is many orders of magnitude above the few notices per minute that Ask carries.

A separate task-notification queue in the Agent was rejected because it would create a second input contract beside steering and follow-up, and the claim boundaries already give safe delivery points and coalescing.
