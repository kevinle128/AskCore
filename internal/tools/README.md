# `internal/tools`

Everything an agent can call as a tool. The package is flat: a filename prefix groups the files of one tool family. Tools run without an approval step. A user who wants to block a command writes an extension that handles the `tool_call` event.

## What belongs here

- The `Tool` interface, optional capability interfaces and `Result` (`types.go`, `result.go`)
- `Registry` (`registry.go`)
- Builtin tools, one prefix for each family: `filesystem_*`, `shell*`, `web_fetch*`, `web_search*`, `subagent_*`, `skill_*`, …
- Tool backends by vendor: `<tool>_<vendor>.go` (for example `web_search_brave.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Remote MCP tools | `internal/mcp` (it registers a bridge tool here) |
| LLM vendor code | `internal/providers` |
| Sandbox runtime | `internal/sandbox` |
| Role-based access for the gateway | `internal/permissions` |

## Main interfaces

- `Tool` (dewee `internal/tools/types.go:15`)
- Optional capability interfaces, for example `AsyncTool` (dewee `internal/tools/types.go:43`). Pass dependencies through the constructor, not through dewee-style `*Aware` setters.
- Tool backend interfaces, for example `SearchProvider` (dewee `internal/tools/web_search.go:44`)

## File names

`<family>_<topic>.go`; vendor backend `<tool>_<vendor>.go`; add each tool to the fx value group `tools`

## Imports

- Allowed: `providers` (tools that call a model), `store`, `sandbox`, `bus`, `skills`, `workspace`, `tracing`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- There is no built-in allow, deny or approval policy. A later policy handler will be a hook handler, not a file in this package.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
