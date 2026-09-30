# `internal/providers/acp`

ACP provider: process pool for subprocess lifecycle, JSON-RPC session handling, and the bridge that answers file-system and terminal requests from the subprocess.

## What belongs here

- Process pool
- JSON-RPC 2.0 client over stdio
- Tool bridge for fs/terminal requests

## What does not belong here

| Code | Put it in |
|---|---|
| HTTP LLM vendors | `internal/providers` |

## Main interfaces

- Implements `providers.Provider`

## File names

`pool.go`, `session.go`, `tool_bridge.go`

## Imports

- Allowed: `providers`, `sandbox`
- Denied: `internal/tools`, `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../../docs/ask-architecture-reference.md)
