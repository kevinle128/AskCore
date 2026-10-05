# `internal/providers`

LLM access. The package keeps three things apart. An **Api** is a wire protocol (for example `anthropic-messages`, `openai-completions`, `openai-responses`). A **Provider** is a vendor endpoint that speaks one Api. A **Model** is one model of a provider, with its limits and prices. A vendor quirk of an Api is a compat record kept as data, not as code branches. There is one adapter for each Api, in its own sub-package; OpenAI-compatible vendors share the `openai` adapter. A vendor (for example Alibaba Token Plan) is data: provider and model records, base URL and key env name, not a package.

## What belongs here

- The `Provider` interface, the request types and the stream function (`types.go`)
- The provider stream with one settled result (`stream.go`), the shared message assembler (`assembler.go`), request normalization and the default role filter (`convert.go`), stream errors (`errors.go`)
- The Api, Provider and Model types and the model catalog (`api.go`, `model.go`, `catalog.go`)
- The compat record (`compat.go`)
- Optional capability interfaces (`ThinkingCapable`, `CapabilitiesAware`)
- Wire adapters, one sub-package for each Api: `anthropic/` (`anthropic-messages`), `openai/` (`openai-completions`, `openai-responses`). Each adapter chooses its own libraries.
- The shared fantasy plumbing for the adapters (`fantasykit/`: StreamPart-to-Assembler fold, idle timeout, error mapping, witness). Fantasy is infrastructure for the adapters only.
- Tool-call id normalizers for each Api, the shared `calculateCost`, and the thinking clamp
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

Core files: `api.go`, `model.go`, `compat.go`, `registry.go`, `types.go`, `stream.go`, `assembler.go`, `convert.go`, `transform.go`, `transcript.go` (`CurrentTools`, `ToolChanges`: replay and diff of the tool declarations in system messages), `errors.go`, `cost.go`, `normalize.go`

Sub-packages: `anthropic/` and `openai/` (wire adapters, one file for each Api or topic, for example `openai/completions.go`, `openai/responses.go`), `fantasykit/` (shared fantasy plumbing), `faux/` (scripted fake provider for tests of every package), `cassette/` (records provider HTTP to YAML and replays it in order, for tests and `ASK_CAPTURE`; see [docs/testing-llm-cassettes.md](../../docs/testing-llm-cassettes.md)), `sse/` (Server-Sent Events reader), `partialjson/` (tolerant parser for streamed tool arguments), `acp/` (subprocess agents)

## Imports

- Allowed: `pkg/protocol` (message, content, usage and stream event types), `tracing`. Third-party vendor SDKs and `charm.land/fantasy` only in the adapter sub-packages and `fantasykit/` (depguard); the core files never import them
- Denied: `internal/tools`, `internal/agent`, `internal/settings` (receive the credential through a resolver function); `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- Credentials come from the caller. This package never reads `auth.json` and never stores a key.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
