# `internal/tracing`

Trace collector: run → model turn → tool attempt, with bounded previews and redaction. Every meaningful agent activity must be traceable, including blocked and failed attempts.

## What belongs here

- `Collector`, span types, context helpers, cost calculation

## What does not belong here

| Code | Put it in |
|---|---|
| OTLP export | `internal/tracing/otelexport` |
| Trace rows at rest | `internal/store` |

## Main interfaces

- `SpanExporter` (dewee `internal/tracing/collector.go:51`)

## File names

`collector.go`, `context.go`, `cost.go`

## Imports

- Allowed: `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
