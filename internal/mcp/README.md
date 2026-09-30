# `internal/mcp`

Model Context Protocol client bridge: connection manager (stdio, SSE, streamable HTTP), tool listing and a `tools.Tool` bridge for each remote tool.

## What belongs here

- Connection manager and pool
- Bridge tool that wraps a remote MCP tool
- Grant checks for MCP servers

## What does not belong here

| Code | Put it in |
|---|---|
| Builtin tools | `internal/tools` |
| MCP server settings at rest | `internal/store` |

## Main interfaces

- Implements `tools.Tool` for remote tools

## File names

`manager*.go`, `bridge_tool.go`, `transport_<kind>.go`

## Imports

- Allowed: `tools`, `store`, `tracing`, MCP SDK
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
