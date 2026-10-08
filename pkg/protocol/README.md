# `pkg/protocol`

This package owns shared message, event, and wire contracts.
It does not own execution or transport handlers.
JSON compatibility is deliberate: the public headless event projection remains Pi-style while internal lifecycle records use Ask's scopes.

## Source owners

| Contract | Owner |
|---|---|
| Planned UI extension wire contracts | [TUI architecture](../../docs/tui-architecture.md); public author API belongs in [askui](../askui/README.md) |
| ACP method names, `_ask/*` DTOs, error kinds and codes, event identity | [acp.go](acp.go), tests in [acp_test.go](acp_test.go); SDK and schema pins in [conformance report](../../plans/reports/conformance-261007-h13a-sdk.md); roadmap: [H13](../../plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md) |
| Messages, content, usage, and tool declarations | [message.go](message.go), [content.go](content.go), [usage.go](usage.go), [tool.go](tool.go) |
| Agent event types and sequencing envelope | [events.go](events.go) |
| Provider stream events and message reconstruction | [stream_events.go](stream_events.go), [builder.go](builder.go) |
| Exact JSON and JSONL projection | [codec.go](codec.go) |

## ACP wire rules

The package holds no SDK type. Sequence counters are decimal strings. A turn has no ID of its own: name it with `cycleId` and the sequence.
Errors carry one `ACPErrorKind` and no provider text. `ACPErrorMessage` returns the one fixed text of a kind, and `ACPErrorCode` its JSON-RPC code.
The kinds are `unknown_session`, `not_initialized`, `busy`, `disposed`, `no_api_key`, `queue_full`, `invalid_model`, `invalid_content`, `invalid_params`, `invalid_state`, `cancelled`, `unsupported`, `output_failure` and `internal`.
`invalid_params` means a request that cannot be decoded or whose value is out of range. `invalid_state` means a well-formed call that the current log does not allow, such as continuing an empty log or one that ends with an assistant message.

## Lifecycle terminology

A cycle is the work for one admitted input batch.
A durable Ask turn is an admission and execution boundary and can include retry attempts.
A Pi wire turn is one model response and its tool results, so retry opens another wire turn inside the same cycle.
An attempt is one model request that actually starts.
Keep these terms distinct when reading an event trace against the session log.

The Agent remains the event publication owner; bus owns bounded replay and follow.
The event definitions and codec own field names, reasons, causes, and order-sensitive projections.
[Agent retry projection](../../internal/agent/retry_events.go) owns agent_end.willRetry and auto_retry events.
Retry records are log facts, not additional JSON event types.
A visible failed attempt can be followed by a retry sequence while its content stays outside model history.

The final message is authoritative when streaming updates contained unfinished tool calls.
A max-tokens completion cannot execute those incomplete calls.
The [attempt owner](../../internal/agent/loop_stream.go) and [codec tests](codec_test.go) own this boundary.

The headless JSON stream ends with agent_settled.
Disposal then reaches Go observers as agent_disposed, which is omitted from that JSON stream.
See [headless JSON writer](../../cmd/tui/headless_json.go) and [disposal fixture](../../cmd/tui/headless_lifecycle_test.go).
Preparation failure is the deliberate compatibility exception: no user input is committed or published before the failure wrapper.

## Copy and safety boundaries

Public content types permit both value and pointer blocks.
Shared clone helpers must preserve those forms and copy nested signatures and tool arguments.
Private hook views must not become aliases of retained history or another handler's result.
The clone owners are content.go and message.go.

Credentials do not belong in message or event envelopes.
Dashboard redaction remains a separate policy; an internal event contract is not proof of safe public exposure.

## Package boundaries and imports

Gateway handlers belong in internal/gateway/methods and gRPC definitions belong in proto.
This package uses only the standard library and never imports AskCore/internal packages.
Keep a single shared contract rather than a transport-specific copy.
See [architecture](../../docs/ask-architecture-reference.md).
