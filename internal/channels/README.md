# `internal/channels`

Channel manager and `BaseChannel`. Each platform is a subfolder `channels/<vendor>/` that embeds `BaseChannel`, receives messages, publishes `bus.InboundMessage` and sends replies.

## What belongs here

- `Channel` interface and optional capability interfaces
- `BaseChannel` (allowlist, pairing, health)
- `Manager`
- Platform subfolders `<vendor>/`, added when needed

## What does not belong here

| Code | Put it in |
|---|---|
| WS and HTTP clients | `internal/gateway` |
| Channel settings at rest | `internal/store` |

## Main interfaces

- `Channel` (dewee `internal/channels/channel.go:89`)
- `BaseChannel` (dewee `internal/channels/channel.go:224`)
- Optional: `StreamingChannel`, `ReactionChannel`, `WebhookChannel`, …

## File names

`channel.go`, `manager.go`; platform code in `<vendor>/`

## Imports

- Allowed: `bus`, `store`, `tracing`, platform SDKs (in `<vendor>/` only)
- Denied: `internal/agent`, `internal/gateway`, `internal/http`, `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
