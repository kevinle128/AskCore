# `internal/bus`

Decouples message sources (channels, gateway) from the runtime. Inbound messages go to the consumer, outbound messages go to channels and WS clients, and events notify subscribers (for example cache invalidation).

## What belongs here

- `MessageBus`, `InboundMessage`, `OutboundMessage`, `Event` and payload types
- Dedup and debounce helpers

## What does not belong here

| Code | Put it in |
|---|---|
| Durable jobs | `internal/messaging` |
| Realtime push to clients | `internal/realtime` |

## Main interfaces

- `EventPublisher`, `MessageRouter` (dewee `internal/bus/types.go:203-231`)

## File names

`bus.go`, `types.go`, `dedupe.go`, `debounce.go`

## Imports

- Allowed: standard library
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport)

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
