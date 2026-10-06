# `internal/agent`

The Agent is the execution owner for one session.
It owns input delivery, lifecycle order, model recovery, tool coordination, and local observation.
Control handlers and observers must not become a second loop or writer.

## Source owners

| Responsibility | Owner |
|---|---|
| Public API, one active run, failure guard, abort, and disposal | [agent.go](agent.go), [types.go](types.go) |
| Turn stages and transitions | [loop_stage.go](loop_stage.go), [loop_run.go](loop_run.go) |
| Steering, follow-up, removal, and non-waking tool context | [queue.go](queue.go) |
| Preparation, staged input commit, and settled model attempts | [loop_stream.go](loop_stream.go), [attempt.go](attempt.go), [context_source.go](context_source.go) |
| Captured retry policy and Pi projection | [recover.go](recover.go), [retry_events.go](retry_events.go) |
| Safe request deltas and exact in-memory rebuild | [request_log.go](request_log.go) |
| Tool snapshot, body pool, exclusive barriers, and ordered outcomes | [tool_coordinator.go](tool_coordinator.go), [loop_tools.go](loop_tools.go) |
| Repair of an uncertain open turn without re-execution | [tool_repair.go](tool_repair.go) |
| Cycle identity and termination reasons | [lifecycle.go](lifecycle.go) |
| Ordered local listeners and posted events | [emit.go](emit.go) |
| Consistent follow snapshot, stream baseline, cursor, and epoch | [follow.go](follow.go) |
| System prompt and tool declaration changes | [systemprompt.go](systemprompt.go), [loop_tool_changes.go](loop_tool_changes.go) |

## Lifecycle decisions

A cycle is the work for one admitted input batch.
A durable Ask turn can contain retry attempts; the Pi event projection starts a wire turn for each attempt.
This distinction preserves the public JSON contract without making retry repeat admission.
See [protocol terminology](../../pkg/protocol/README.md).

Preparation must succeed before admitted input enters history.
A preparation failure publishes the failure tail but saves neither the staged input nor its error wrapper.
The sole-writer and safe request-record constraints are in [sessions](../sessions/README.md).
Failed attempts record safe outcome, failure, usage, and binding facts.
Their assistant content stays outside model history; visible streamed output remains visible through the Pi retry sequence.
Retry must preserve the selected credential's method, profile, and billing class.

Input uses claims rather than delivery modes.
Steering is claimed at a turn boundary; a cycle claim also takes one follow-up.
AfterTool context uses the same staged admission path without waking an idle Agent.
Abort and disposal are separate operations because stopping current work must not always end the session.

A tool body that started must finish before the run settles or disposal completes.
Drain has no time bound; a tool must cooperate with cancellation.
Repair records uncertainty rather than repeating a potentially external side effect.
See [coordinator](tool_coordinator.go), [repair](tool_repair.go), and [tool cancellation contract](../tools/types.go).

## Extension and observation boundaries

The public control surface is [pipeline.Registry](../pipeline/registry.go).
Turn stages stay private to this package.
A new control handler registers with that surface; it does not insert a second execution driver.
The application registry and each Agent registry have separate ownership.

Local listeners are synchronous and ordered; the follow path provides bounded nonblocking observation.
A listener failure must not alter the model or tool outcome.
Callbacks can queue input or abort, but must not call blocking Dispose or WaitForIdle on their own invocation.
There is no runtime goroutine guard for those blocking calls.
The lock and re-entry contract is owned by [emit.go](emit.go) and [agent.go](agent.go).
Never hold the Agent state lock while calling handlers, providers, tools, the log, or listeners.

## Package boundaries

Tool implementations belong in tools; wire adapters belong in providers.
Control contracts belong in pipeline; log types belong in sessions; replay buffering belongs in bus.
Run lanes remain the planned responsibility of scheduler.
Transport and ACP adapters wrap this typed API and must not be imported here.

## File names and imports

Keep loop topics in `loop_<topic>.go` and use existing topic files for queues, recovery, repair, and publication.
Stages stay unexported and their order is owned by loop_stage.go.
Allowed imports include pipeline, providers, tools, sessions, bus, store, settings, skills, bootstrap, hooks, tracing, and workspace.
Do not import gateway, http, channel vendors, acp, leader, or config.
The [architecture import rules](../../docs/ask-architecture-reference.md#8-import-rules) and depguard own enforcement.

[Architecture](../../docs/ask-architecture-reference.md) owns package decisions.
The [conformance matrix](../../plans/261006-0933-lifecycle-event-pipeline-redesign/conformance-matrix.md) owns tested upstream differences.
