# `internal/tracing/otelexport`

OpenTelemetry SDK setup and OTLP export.

## What belongs here

- Tracer provider, OTLP exporter, shutdown

## What does not belong here

| Code | Put it in |
|---|---|
| Agent trace records | `internal/tracing` |

## Main interfaces

- Implements `tracing.SpanExporter` when needed

## File names

`otel.go`

## Imports

- Allowed: `tracing`, OpenTelemetry SDK
- Denied: `internal/agent`, `internal/gateway`, `internal/http`, `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../../docs/ask-architecture-reference.md)
