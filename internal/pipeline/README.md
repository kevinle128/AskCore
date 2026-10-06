# `internal/pipeline`

This package owns typed control contracts, not the Agent loop.
Controls can decide or wrap an operation; observation uses the separate Agent publication path.
One registry prevents extensions from rebuilding lifecycle order in hand-written wrappers.

## Source owners

| Contract | Owner |
|---|---|
| Inputs, updates, outcomes, and control decisions | [points.go](points.go) |
| Around middleware and the one-call next lifecycle | [middleware.go](middleware.go) |
| CompleteStep and StopTurn decision precedence | [decisions.go](decisions.go) |
| Typed registration, owner disposal, scopes, and dispatch snapshots | [registry.go](registry.go) |
| Default recovery and captured policy | [recovery.go](recovery.go) |

The around points are AdmitStep, PrepareRequest, ExecuteModel, RecoverModel, BeforeTool, ExecuteTool, and AfterTool.
CompleteStep and StopTurn use ordered decisions.
The current definitions and defaults live in the source owners above.
The Agent dispatch sites are in [turn stages](../agent/loop_stage.go), [attempt execution](../agent/loop_stream.go), [recovery](../agent/recover.go), and [tool coordination](../agent/tool_coordinator.go).

## Decisions and constraints

Around handlers form a waterfall: the outermost handler has the final result.
Keeping a downstream answer requires returning it or changing its copied result explicitly.
AfterTool context is not merged automatically because that would change override precedence.
Returned model outcomes and AfterTool results cross private-copy boundaries.
See [copy tests](after_tool_context_test.go) and [model dispatch](registry.go).

The accepted next call belongs to the invocation until its inner work finishes.
This prevents an asynchronous accepted call from outliving a closed operation.
Handler failure cancels that call before joining it; a cached success does not cancel it.
Panics remain with the caller rather than becoming generic dispatcher errors.
The [middleware lifecycle tests](middleware_lifecycle_test.go) own these boundaries and retained-call rejection.

Validated tool arguments and executable tools are frozen by the Agent.
BeforeTool can allow, deny, or cancel, but it cannot change those arguments.
AfterTool alone produces additional user context for later admission.
Body outcomes have no separate added-context API.
The coordinator owns the case rules, source order, and cancellation drain; see [tool coordinator](../agent/tool_coordinator.go).

Request and turn control failures end the run through the Agent failure guard.
Tool control failures produce a tool result instead.
A preparation failure publishes the error tail without committing input or an error message.
This deliberate timing rule keeps history limited to input that reached a prepared request.
See [Agent failure path](../agent/agent.go) and [preparation](../agent/loop_stream.go).

## Scope and package boundaries

Application controls can be shared, while per-Agent controls must stay isolated.
Registration ownership permits removal without changing an invocation already in flight.
The registry implementation and [scope tests](scope_test.go) own the details.

The loop, tool batches, and abort belong in agent.
Provider HTTP belongs in providers; tool bodies belong in tools.
The planned extension dispatcher adapter belongs here or in hooks, not in a second loop.

## File names and imports

Use points.go, middleware.go, decisions.go, registry.go, and `<name>.go` for a control handler.
Allowed imports include providers, tools, sessions, store, hooks, tracing, bootstrap, and workspace.
Do not import agent, transport packages, acp, leader, or config.
See [architecture import rules](../../docs/ask-architecture-reference.md#8-import-rules).
