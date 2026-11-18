# `internal/sessions`

The typed log is the execution record.
Model history is a projection of message entries, not a separately mutable Agent state.
The log is saved by a backend behind `store.SessionStore` (today the JSONL files of [jsonlstore](../store/jsonlstore/README.md)); `MemoryLog` is the in-memory writer for tests and for `--no-session`. Fork into a child session and tree navigation are not built.

## Source owners

| Responsibility | Owner |
|---|---|
| Closed entry types and private copies | [entry.go](entry.go) |
| Atomic append, commit positions, and sole-writer boundary | [writer.go](writer.go) |
| In-memory writer (tests, `--no-session`) | [memory.go](memory.go) |
| Line format of a session file: header, one envelope for each entry, wire type names, `ignorable`, format version | [codec.go](codec.go) |
| Session id: the decimal text of a snowflake id | [id.go](id.go) |
| Exact model request reconstruction | [Agent request log](../agent/request_log.go) |

Lifecycle, input outcome, request delta, retry, and tool intent records are log facts rather than ordinary model messages.
`ModelChange` and `ThinkingChange` record the selection that later requests use, so a resumed session continues on the same model and level. `ContextEdit` changes what the model sees of an earlier message and keeps the message in the history; no code writes it yet.
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

The log is linear: `seq` equals the position of the entry, and an append whose first `seq` is not the stored count fails. A branch is a later fork into a child session (header `parentSession` and `parentSeq`), not a tree inside one file.
The codec lives here so that every backend writes and reads the same entries. At-rest storage belongs in `store` and its implementations; the decision is in [ADR 0006](../../docs/adr/0006-session-logs-as-jsonl-files.md).
An id is a string everywhere. Callers compare ids as strings and never parse them: for 19-digit ids the string order is the time order.

## Package boundaries

Execution, queues, repair decisions, and request preparation order belong in agent.
Compaction decisions belong in the control layer; sessions owns their record and projection.
Receive dependencies through constructors and keep this package independent of Agent and transport.

## File names and imports

Use entry.go, writer.go, memory.go, codec.go, id.go, and a topic file for a real persistence or projection boundary.
Allowed imports are `pkg/protocol`, `github.com/bwmarrin/snowflake`, and the standard library. `store` imports this package, never the reverse.
Do not import agent, gateway, http, channel vendors, acp, leader, store, or config.
See [architecture](../../docs/ask-architecture-reference.md).
