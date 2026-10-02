# `internal/providers`

LLM access. The package keeps three things apart. An **Api** is a wire protocol (for example `anthropic-messages`, `openai-completions`, `openai-responses`). A **Provider** is a vendor endpoint that speaks one Api. A **Model** is one model of a provider, with its limits and prices. A vendor quirk of an Api is a compat record kept as data, not as code branches. OpenAI-compatible vendors share one adapter.

## What belongs here

- The `Provider` interface, the request types and the stream function (`types.go`)
- The provider stream with one settled result (`stream.go`), the shared message assembler (`assembler.go`), request normalization and the default role filter (`convert.go`), stream errors (`errors.go`)
- The Api, Provider and Model types and the model catalog (`api.go`, `model.go`, `catalog.go`)
- The compat record (`compat.go`)
- Optional capability interfaces (`ThinkingCapable`, `CapabilitiesAware`)
- Vendor implementations (`anthropic*.go`, `openai*.go`, …)
- Provider registry and adapter registry
- Shared SSE stream reader, provider-level retry and error classification

## What does not belong here

| Code | Put it in |
|---|---|
| Agent-level retry of a turn | `internal/agent` |
| Subprocess agents over ACP | `internal/providers/acp` |
| Tools | `internal/tools` |
| Reading `auth.json` and settings | `internal/settings` (a constructor receives a credential resolver function) |

## Main interfaces

- `Provider` (dewee `internal/providers/types.go:54`)
- `ThinkingCapable` (dewee `internal/providers/types.go:72`)
- `ProviderAdapter` (dewee `internal/providers/capabilities.go:30`)

## File names

`<vendor>.go`, `<vendor>_<topic>.go`, `adapter_<vendor>.go`, `api.go`, `model.go`, `compat.go`, `registry.go`, `types.go`, `stream.go`, `assembler.go`, `convert.go`, `errors.go`

Sub-packages: `faux/` (scripted fake provider for tests of every package), `sse/` (Server-Sent Events reader), `partialjson/` (tolerant parser for streamed tool arguments)

## Imports

- Allowed: `pkg/protocol` (message, content, usage and stream event types), `tracing`, third-party vendor SDKs
- Denied: `internal/tools`, `internal/agent`, `internal/settings` (receive the credential through a resolver function); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- Credentials come from the caller. This package never reads `auth.json` and never stores a key.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
