# `internal/agent`

The agent runtime. `Run` and `Continue` run Pi's two-level loop: the outer loop takes the next follow-up message when the agent is idle, and the inner loop runs model calls and tool calls until no tool call and no steer message is left. One agent serves one session. The prompt builder assembles the system prompt from the context files, the skills metadata and the tool list.

`Run(ctx, prompts, context, config, emit)` and `Continue(ctx, context, config, emit)` mirror Pi's `runAgentLoop` and `runAgentLoopContinue`. They return the messages the run added. Every event goes through `emit`; an emit error ends the run with that error. A hook error from `pipeline` is returned unchanged and no `agent_end` is emitted.

## What belongs here

- The loop driver (`loop_run.go`): `Run`, `Continue`, `LoopConfig`, the driver that runs one turn after another, the idle check for follow-up messages, and the hook seam methods for the queues, `PrepareRequest` and `FinishTurn`
- The ReAct stages (`loop_stage.go`): the `stage` interface, the `flow` enum, `turnState`, and the six stages of one turn (steer, prepare, reason, act, observe, decide)
- One model call (`loop_stream.go`): context transform, conversion, API key, stream, and the partial message that the final message replaces
- One tool batch (`loop_tools.go`): the `toolExecutor` strategy (truncated, sequential, parallel), preflight, `BeforeToolCall` and `AfterToolCall`, the output-length guard, and abort
- The `Agent` wrapper (`agent.go`): one active run, `Prompt`, `Continue`, `Abort`, `WaitForIdle`, `Reset`, `State`, and the run-failure path that turns a returned error or a panic of the loop into an error assistant message, `agent_end` and `agent_settled`
- Event dispatch (`emit.go`): the envelope (`seq` across runs, `runId` per run, `ts`, `sessionId`) and the listeners, called in subscribe order on the run goroutine
- `ContextSource` and the `PrepareRequest` projection of it into every request (`context_source.go`); `sessions.MemoryLog` is the in-memory source
- `Config`, `State`, `Status` and `ErrBusy` (`types.go`)
- Tests (`loop_*_test.go`, `agent_test.go`) on the faux provider, with a goroutine-leak check in `loop_helpers_test.go`
- The steer and follow-up queues of one session (`queue.go`). Steer messages are delivered after the current tool batch. Follow-up messages are delivered when the agent is idle. Each queue has the mode `all` or `one-at-a-time`
- System prompt assembly (`systemprompt*.go`). In H2 it is Pi's initial system message: the prompt and every registry tool, timestamp 0, fixed for the run
- The typed Go API that headless mode and the ACP adapter both call (`agent.go`, `types.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Hook points of one turn | `internal/pipeline` |
| Tool implementations | `internal/tools` |
| LLM vendor code | `internal/providers` |
| Session log and context projection | `internal/sessions` |
| Lanes and concurrency limits | `internal/scheduler` |
| ACP mapping and the leader socket | `internal/acp`, `internal/leader` |
| HTTP/WS handlers | `internal/http`, `internal/gateway` |

## Main interfaces

- `Run`, `Continue`, `LoopConfig` and `Emit` (`loop_run.go`)
- `Agent`, `New`, `Config` and `ContextSource` (`agent.go`, `types.go`, `context_source.go`)
- Hook points are the fields of `pipeline.Hooks`. The loop calls them in order

## File names

`agent.go`, `emit.go`, `context_source.go`, `loop_<topic>.go`, `queue*.go`, `systemprompt*.go`, `types.go`

If a stage grows too large for `loop_stage.go`, move it to `loop_stage_<name>.go`.

## Imports

- Allowed: `pipeline`, `providers`, `tools`, `store`, `sessions`, `skills`, `bootstrap`, `hooks`, `settings`, `tracing`, `bus`, `workspace`
- Denied: `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config` (receive typed config through the constructor)

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- The queues belong to the loop. There is no `interrupt` mode. Abort is a separate call on the `Agent`.
- One agent serves one session. A `Router` is not needed until multi-agent routing is.
- The loop is the only place that calls the hook points of `pipeline`.
- Stages are unexported. The public extension surface is `pipeline.Hooks`.
- Only the seam method of a hook point reads `cfg.Hooks.<Field>`. Combine hook sets with `pipeline.Compose`, not with a hand-written wrapper.

## Design pattern map

| Pattern | Where | Why |
|---|---|---|
| Pipeline / Template Method | `turnStages` and `runTurn` (`loop_stage.go`) | One turn is a fixed sequence of named steps: steer, prepare, reason, act, observe, decide. |
| State machine | `flow` enum and the driver `switch` in `run` | The next step is one value, not a combination of flags. |
| Strategy | `toolExecutor` (`loop_tools.go`) | The batch mode changes for each assistant message: truncated after an output-length stop, sequential when a tool must run alone, else parallel. |
| Hook / Observer (port) | `pipeline.Hooks`, `Emit` | The only public extension surface. Stages call hooks; hooks never call stages. |
| Single seam | One method for each hook point: `pollSteering`, `pollFollowUps`, `prepareRequest`, `transformContext`, `convertToLLM`, `apiKey`, `finishTurn`, `beforeToolCall`, `afterToolCall` | A hook is read in one place, so you find, trace or replace it in one step. |
| Composite | `pipeline.Compose`, used in `Agent.execute` | Several hook sets act as one `Hooks`. The merge rules are in the [pipeline README](../pipeline/README.md). |
| Adapter (later) | A file in `internal/pipeline` that wraps `hooks.Dispatcher` | Extension events become `Hooks` fields, so the loop and the stages do not change. |

Hooks have three layers: the port (`pipeline.Hooks`), the composite (`pipeline.Compose`) and, later, the adapter for the extension dispatcher. See the [pipeline README](../pipeline/README.md).

## How to add a stage

1. Write an unexported type with `name() string` and `run(l *loop) (flow, error)`. Return `flowNext` unless the stage ends the turn.
2. Put per-turn data in a typed field of `turnState`. Do not use a closure or a shared map.
3. Insert the stage in `turnStages`. `decide` must stay last.
4. Update `TestTurnStagesOrder`.
5. Prove that the event order did not change: compare the JSONL of `ask -p --mode json` before and after, without `ts`, `runId`, `timestamp` and `sessionId`.

Example: a compaction stage goes between `prepare` and `reason`, returns `flowNext`, and emits its own events only where Pi does.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
