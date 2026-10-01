# `internal/bus`

The event stream of the harness. The publisher sends each event to a bounded fan-out, so one slow watcher cannot block the agent. Watchers are the TUI inspector, the monitoring dashboard and the tracing export. The bus also carries inbound and outbound channel messages.

## What belongs here

- `MessageBus`, `InboundMessage`, `OutboundMessage`
- The event publisher and the bounded fan-out to subscribers (`publisher.go`)
- Dedup and debounce helpers

## What does not belong here

| Code | Put it in |
|---|---|
| Event type definitions | `pkg/protocol` |
| Synchronous dispatch to hooks | `internal/hooks` |
| Durable jobs | `internal/messaging` |
| Realtime push to clients | `internal/realtime` |

## Main interfaces

- `EventPublisher`, `MessageRouter` (dewee `internal/bus/types.go:203-231`)

## File names

`bus.go`, `types.go`, `publisher.go`, `dedupe.go`, `debounce.go`

## Imports

- Allowed: standard library, `pkg/protocol`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse)

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- Event types live in `pkg/protocol`. The publisher and fan-out live here. Sync dispatch lives in `hooks`. Do not define an event type in two places.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
