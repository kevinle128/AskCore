# `internal/providers`

One implementation of `Provider` for each LLM vendor, plus the registry that selects a provider by name. OpenAI-compatible vendors share one adapter.

## What belongs here

- The `Provider` interface and chat request/response types (`types.go`)
- Optional capability interfaces (`ThinkingCapable`, `CapabilitiesAware`)
- Vendor implementations (`anthropic*.go`, `openai*.go`, …)
- Provider registry and adapter registry
- Shared SSE stream reader, retry and error classification

## What does not belong here

| Code | Put it in |
|---|---|
| Subprocess agents over ACP | `internal/providers/acp` |
| Tools | `internal/tools` |
| API keys at rest | `internal/crypto` + `internal/store` |

## Main interfaces

- `Provider` (dewee `internal/providers/types.go:54`)
- `ThinkingCapable` (dewee `internal/providers/types.go:72`)
- `ProviderAdapter` (dewee `internal/providers/capabilities.go:30`)

## File names

`<vendor>.go`, `<vendor>_<topic>.go`, `adapter_<vendor>.go`, `registry.go`, `types.go`

## Imports

- Allowed: `store` (read provider settings), `tracing`, third-party vendor SDKs
- Denied: `internal/tools`, `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
