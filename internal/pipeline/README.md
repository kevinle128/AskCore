# `internal/pipeline`

The hook points of the agent loop. `Hooks` has one typed function field for each point: `TransformContext`, `ConvertToLLM`, `GetAPIKey`, `PrepareRequest`, `FinishTurn`, `BeforeToolCall`, `AfterToolCall`, `GetSteeringMessages` and `GetFollowUpMessages`. A nil field gives Pi's default. `internal/agent` runs the loop and calls each field at its place, so a reader traces a hook in one hop.

## What belongs here

- `Hooks` and the argument and result types of each hook point (`hooks.go`): `AgentContext`, `Request`, `RequestUpdate`, `Turn`, `TurnDecision` (`Proceed`, `Continue`, `End`), `ToolCallInfo`, `ToolResultInfo`, `BeforeToolCallResult`, `AfterToolCallResult` (with `Apply`, the one copy of its override rule)
- `Compose` (`hooks_compose.go`): combines several `Hooks` into one
- Later, the hooks that fill these fields (compaction, the session projection), one file each. The H11 `hooks.Dispatcher` adapter is one file here that produces a `Hooks`

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

## Three layers

| Layer | Where | Role |
|---|---|---|
| Port | `Hooks` | One function per hook point. The loop reads each field in exactly one loop method |
| Composite | `Compose(hs ...Hooks) (Hooks, error)` | Combines N hook sets into one, with one merge rule per hook point. The first error stops a chain |
| Adapter (H11) | a file in this package that wraps `hooks.Dispatcher` | Turns extension events into `Hooks` fields. It owns the per-event error tolerance, before `Compose` sees a result |

## Merge rules of `Compose`

With no function at a point the field stays nil. With one function it is used as it is. A nil field is skipped.

| Hook point | Rule |
|---|---|
| `TransformContext` | Chain: the output of one is the input of the next |
| `ConvertToLLM` | At most one. Two or more give `ErrMultipleConvertToLLM` |
| `GetAPIKey` | The first non-empty key wins; later functions do not run |
| `PrepareRequest` | Chain: each function sees the earlier updates. Later non-nil field wins |
| `FinishTurn` | Every function runs. `End` beats `Continue` beats `Proceed` |
| `BeforeToolCall` | Chain: replaced arguments reach the next function. The first `Block` wins and stops the chain |
| `AfterToolCall` | Chain: each function sees the earlier overrides. Later non-nil field wins; later `Content` alone drops earlier `StructuredContent` |
| `GetSteeringMessages` | Every source is polled, the messages are joined in order |
| `GetFollowUpMessages` | Same as steering |

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

`hooks.go`, `hooks_compose.go`, `<name>.go` for one hook implementation

## Imports

- Allowed: `providers`, `tools`, `sessions`, `store`, `hooks`, `tracing`, `bootstrap`, `workspace`
- Denied: `internal/agent` (the agent fills the fields); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- One function per hook point in `Hooks`. Several handlers at one point are combined with `Compose`, never by a hand-written wrapper.
- `pipeline` does not run the loop. It does not own the order of model calls and tool calls.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
