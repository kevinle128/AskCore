# `internal/sessions`

The typed log is the execution record.
Model history is a projection of message entries, not a separately mutable Agent state.
Current storage is in memory; SQLite persistence, lease ownership, resume, and a persistent entry tree remain roadmap work.

## Source owners

| Responsibility | Owner |
|---|---|
| Closed entry types and private copies | [entry.go](entry.go) |
| Atomic append, commit positions, and sole-writer boundary | [writer.go](writer.go) |
| Current in-memory implementation | [memory.go](memory.go) |
| Exact model request reconstruction | [Agent request log](../agent/request_log.go) |

Lifecycle, input outcome, request delta, retry, and tool intent records are already implemented.
They are log facts rather than ordinary model messages.
Read entry.go for the machine contract; do not define duplicate fields in a persistence adapter.

## Decisions and constraints

The Agent driver is the sole writer.
Observers read copies after commit and must not append lifecycle entries.
An append is one atomic unit so a message and its associated acknowledgment cannot disagree.

Request records contain safe effective preparation values, not credentials or account identity.
This permits in-memory reconstruction without turning the log into a second credential store.
The serving retry policy belongs to the captured request preparation.

Tool intent identifies the requesting assistant entry and call ID.
It proves intent, not body invocation; repair must preserve that distinction when an outcome is uncertain.
Retry scheduling and retry start are separate facts because cancellation can occur during the wait.

The planned entry tree must preserve append-only history and branch-specific projection.
At-rest rows belong in store and its implementation; lease checks must share the write transaction.
See [remaining session work](../../plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h8-the-session-log).

## Package boundaries

Execution, queues, repair decisions, and request preparation order belong in agent.
Compaction decisions belong in the control layer; sessions owns their record and projection.
Receive dependencies through constructors and keep this package independent of Agent and transport.

## File names and imports

Use entry.go, writer.go, memory.go, and a topic file for a real persistence or projection boundary.
Allowed imports are store and pkg/protocol.
Do not import agent, gateway, http, channel vendors, acp, leader, or config.
See [architecture](../../docs/ask-architecture-reference.md).
