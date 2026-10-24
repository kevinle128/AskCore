# Changelog

All notable changes to Ask are recorded in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Ask has no release yet, so every entry is under `Unreleased`.
Changes made before this file existed are in the git history and in `plans/journals/`.

## [Unreleased]

### Added

- Async task runtime, first phase ([design](docs/async-task-runtime.md), [ADR 0004](docs/adr/0004-agent-owned-async-task-runtime.md)):
  - `internal/tasks`: a per-Agent task registry. Each task runs on a goroutine that the registry owns, ends in exactly one terminal state, and produces exactly one notice. Timer tasks hold no goroutine while they wait.
  - `Agent.Notify`: the only entry for task notices. A notice joins the next turn of a running Agent or starts a run on an idle Agent. No component sleeps or polls to detect completion.
  - `Agent.Idle` and `Agent.WaitForQuiescence`: wait for the end of runs and tasks by signal.
  - `Agent.Tasks` and `tools.Context.Tasks`: tools start background work through the registry.
  - `protocol.CustomMessage` (role `custom`): an application message that the model receives as a user message. Task notices use it with the custom type `task-notification`.
  - Events `task_start`, `task_update`, and `task_end`.
  - Task output files under `<ASK_HOME>/tasks/<session>/`, created at the first output with mode 0600 in private directories. Session directories older than 7 days are removed once when a process starts.
  - `settings.EnsurePrivateDir`, `settings.CheckPrivateDir`, and `settings.ResolveHome`.

### Changed

- An object with `"role": "custom"` now decodes to `protocol.CustomMessage`, not to `protocol.RawMessage`. Such an object without `customType` is a decode error.
- `providers.ConvertToLLM` keeps a custom message and gives it to the model as a user message. Raw messages are still dropped.
- `queue_update` shows a task notice as a preview of at most 200 bytes.
- Task notices do not count toward `MaxQueuedInputs`, so they never take the queue slots of human input.
- `Abort` keeps background tasks running and moves queued task notices to the context queue, where they ride with the next input. Human input is dropped as before.
- `Reset` stops the tasks of the old conversation and refuses their late notices and events. `Dispose` stops and waits for every task.
