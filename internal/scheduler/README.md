# `internal/scheduler`

This package is the planned owner of run lanes and concurrency limits.
The current package is a scaffold; see [doc.go](doc.go).
A scheduler decides when a run may start, not what a run does.

## Boundaries

Agent owns steering, follow-up, input claims, abort, and disposal.
Run scheduling must not create another input queue contract or an interrupt delivery mode.
A scheduled run is a callback so scheduler does not depend on Agent.
Durable background jobs belong in messaging.

## Future source placement

Use scheduler.go for the scheduler, lanes.go for lane limits, and queue.go only for waiting run slots.
These files describe intended placement, not current implementations.
Allowed imports are the standard library and tracing.
Do not import agent, gateway, http, channel vendors, acp, leader, or config.
See [architecture](../../docs/ask-architecture-reference.md).
