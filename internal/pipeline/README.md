# `internal/pipeline`

The hook points of the agent loop. `Hooks` has one typed function field for each point: `TransformContext`, `ConvertToLLM`, `GetAPIKey`, `PrepareRequest`, `FinishTurn`, `BeforeToolCall`, `AfterToolCall`, `GetSteeringMessages` and `GetFollowUpMessages`. A nil field gives Pi's default. `internal/agent` runs the loop and calls each field at its place, so a reader traces a hook in one hop.

## What belongs here

- `Hooks` and the argument and result types of each hook point (`hooks.go`): `AgentContext`, `Request`, `RequestUpdate`, `Turn`, `TurnDecision` (`Proceed`, `Continue`, `End`), `ToolCallInfo`, `ToolResultInfo`, `BeforeToolCallResult`, `AfterToolCallResult`
- Later, the hooks that fill these fields (compaction, the session projection), one file each

## Hook points

| Field | When the loop calls it | Default |
|---|---|---|
| `TransformContext` | Before each request. The result is never stored | No change |
| `ConvertToLLM` | Before each request, after `TransformContext` | `providers.ConvertToLLM` |
| `GetAPIKey` | Before each request. An empty key falls back to the configured one | The configured key |
| `PrepareRequest` | Before each request, after the pending messages were appended. A returned context, model or thinking level stays for later requests | No change |
| `FinishTurn` | After the tool results, before `turn_end`. `End` ends the run, `Continue` makes sure one more request happens. For an error or aborted message the decision is ignored | `Proceed` |
| `BeforeToolCall` | After argument validation. It can block the call or replace the arguments | The call runs |
| `AfterToolCall` | After `Execute`, only for calls that executed. Each non-nil field overrides the result | No change |
| `GetSteeringMessages` | At run start, after each normal turn, and before a later turn when the earlier poll was empty | No messages |
| `GetFollowUpMessages` | When the agent would stop | No messages |

## Failure contract (D20)

- An error from `TransformContext`, `ConvertToLLM`, `GetAPIKey`, `PrepareRequest`, `FinishTurn` or a poll hook ends the run. The loop returns it unchanged and emits no `agent_end`; the `Agent` wrapper builds the error message.
- An error or a panic in `BeforeToolCall` or `AfterToolCall` becomes an error result of that tool call.

## What does not belong here

| Code | Put it in |
|---|---|
| The two-level loop, the tool batch and abort | `internal/agent` |
| LLM calls | `internal/providers` |
| Tool execution code | `internal/tools` |
| Event dispatch to extensions | `internal/hooks` |

## File names

`hooks.go`, `<name>.go` for one hook implementation

## Imports

- Allowed: `providers`, `tools`, `sessions`, `store`, `hooks`, `tracing`, `bootstrap`, `workspace`
- Denied: `internal/agent` (the agent fills the fields); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- One function per hook point. Ordered multi-handler steps come only when H11 needs several handlers at one point.
- `pipeline` does not run the loop. It does not own the order of model calls and tool calls.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
