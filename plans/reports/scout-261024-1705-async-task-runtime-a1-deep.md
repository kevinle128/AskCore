# Deep scout: async task runtime A1

Date: 2026-10-24. Plan: [A1](../261024-0958-async-task-runtime-a1/plan.md). Three Explore passes, one per code phase, then spot checks by the planner.

## Phase 1 (protocol)

- Places that must learn `CustomMessage`: `reservedRole` (`pkg/protocol/message.go:623-629`), `MarshalMessage` (`:657-684`), `UnmarshalMessage` (`:696-740`), `CloneMessage` (`:799-848`, default case is a shallow return, so an explicit deep-copy case is required), docstrings that say "four built-in roles" (`:58-61`, `:631`, `:654-656`, `:796-798`).
- `UserMessage.UnmarshalJSON` accepts string or blocks through `decodeUserBlocks(w.Content, true)` (`message.go:505`). Pi `CustomMessage.content` is `string | blocks`. Use the same decoder for `CustomMessage`.
- Event codec: `EncodeEvent` (`codec.go:16-147`) and `DecodeEvent` (`codec.go:235-425`); pattern of `QueueUpdate` at `codec.go:22` and `:251-259`.
- `ConvertToLLM` (`internal/providers/convert.go:9-20`); the existing test `TestConvertToLLMKeepsFourRolesAndDropsTheRest` (`convert_test.go:13`) uses a `RawMessage` named `bashExecution` as "custom". It stays valid (raw messages are still dropped) but its name must change.
- `request_log.go:112-114` calls `providers.ConvertToLLM` directly, while the run uses `LoopConfig.ConvertToLLM` when set (`loop_run.go:57-59`, `loop_stream.go:199-206`). With the default, both agree. A custom converter must map `CustomMessage` itself.
- No role validation on `Steer`, `FollowUp`, or `Prompt` (`queue.go:159-164`, `agent.go:135`).
- Provider body builders ignore unknown message types with `default: return false`, so a `CustomMessage` that reached them would be dropped silently. The converter runs first, so it never reaches them.
- ACP update mapping returns nil for unknown events, so new event types do not break it.

## Phase 2 (tasks package)

- Only `internal/bus` has `main_test.go` with `goleak.VerifyTestMain` (`internal/bus/main_test.go`); `internal/agent` has it in `loop_helpers_test.go:24`. Tests use testify.
- Depguard rules are lax deny lists with `files` globs (`.golangci.yml:16-199`).
- Toolchain go1.27.0: `sync.WaitGroup.Go`, `context.WithDeadlineCause`, `context.AfterFunc`, and `testing/synctest` (`Test`, `Wait`, `Sleep`) are available. Timers created in a bubble use fake time; their callbacks run when fake time reaches them.
- `internal/tracing` is a scaffold for spans. A1 does not need it.
- Session IDs are filesystem-safe: a UUID for headless (`cmd/tui/headless.go:268-274`), `sess_` plus hex for ACP.
- `ASK_HOME` resolution exists in `internal/settings/auth.go:66-82`; directory safety checks in `internal/settings/paths.go:16-40`.
- The Agent input ID sequence survives Reset (`queue.go:41-43`). Task IDs should follow the same rule.
- A truncation helper with a UTF-8-safe cut exists in `internal/tools/validate.go:69-84`.

Rejected scout suggestions: denying `agent` from importing `tasks` (the Agent must import it); `debug.PrintStack` to stderr for panics (the stack belongs in the sink: `runtime/debug.Stack()`).

## Phase 3 (agent)

- `dispatching` changes under `a.mu` in `dispatch` (`emit.go:136-145`); listeners run without `a.mu`. The idle channel can be driven in the same critical sections.
- `enqueue` does not check `dispatching` (`queue.go:166-196`). A notice on an idle Agent starts a run even while the publisher dispatches a posted event.
- The `context` queue exists and `addToolContext` appends to it without wake (`queue.go:276-285`). `claimSteering` returns context first (`queue.go:80-85`), and the first cycle of `Prompt` and of a queued run claims steering (`agent.go:131`).
- `QueueUpdate` lists steering and follow-up only (`queue.go:107`), so notices moved to `context` by Abort do not appear in `queue_update`.
- `Abort` clears all three queues (`agent.go:179-184`); tests `TestAbortClearsQueuesByDefault` and `TestAbortKeepQueuedKeepsQueues` (`queue_test.go`) cover it and need notice cases.
- `tools.Context` is built at `tool_coordinator.go:264` from `LoopConfig` (`loop_run.go:28-60`), which `execute` builds per run (`agent.go:451-460`).
- Fixtures: `newAgent` (`agent_test.go:30-44`), faux provider `p.Set(...)` and `p.Requests()`, example `TestAgentPromptSettles` (`agent_test.go:68-115`).
- Lock order `emitMu -> mu -> pubMu` (`agent.go:57-62`). `Notify` from a runner goroutine takes only `a.mu`: safe.
