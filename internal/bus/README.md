# `internal/bus`

This package owns bounded replay and followers for the harness event stream.
The Agent owns publication and the consistent snapshot cut.
This split lets slow remote observers resync without holding execution open.

## Source owners

[follow.go](follow.go) owns Ring, Follower, limits, cursor resume, epochs, and ErrResync.
[follow_test.go](follow_test.go) owns size limits, gaps, and slow-follower checks.
[Agent.Follow](../agent/follow.go) owns the snapshot and in-progress assistant baseline paired with the cursor.
Event definitions and encoding belong in [protocol](../../pkg/protocol/README.md).

## Decisions and constraints

Replay must not silently lose an event or provide a partial encoded event.
An unavailable cursor or an oversized event requires an explicit resync.
The caller then takes a new snapshot rather than resubmitting a command.
Followers are bounded and must not block the publisher.
This differs from synchronous local Agent listeners, which intentionally apply backpressure.

The current follow path is trusted in-process observation, not a dashboard redaction boundary.
Dashboard previews, redaction policy, tracing, OTel export, and hang detection remain [observability work](../../plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h12-observability-core-watch-it-think-part-1).
Channel message routing and durable jobs are separate capabilities, not part of the ring.

## Package boundaries

Event types belong in pkg/protocol; the consistent cut belongs in agent.
The extension adapter belongs in hooks and durable jobs belong in messaging.
Client transport belongs in gateway or realtime.

## File names and imports

Use follow.go for the ring and a separate topic file only for a real routing boundary.
Allowed imports are the standard library and pkg/protocol.
Do not import agent, transport packages, acp, or leader.
See [architecture](../../docs/ask-architecture-reference.md).
