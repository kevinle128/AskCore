# `internal/tools`

Everything an agent can call as a tool. The package is flat: a filename prefix groups the files of one tool family.

## What belongs here

- The `Tool` interface, optional capability interfaces and `Result` (`types.go`, `result.go`)
- `Registry` (`registry.go`) and tool policy: allow, deny, approval (`policy.go`)
- Builtin tools, one prefix for each family: `filesystem_*`, `shell*`, `web_fetch*`, `web_search*`, `memory*`, `subagent_*`, `skill_*`, …
- Tool backends by vendor: `<tool>_<vendor>.go` (for example `web_search_brave.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Remote MCP tools | `internal/mcp` (it registers a bridge tool here) |
| LLM vendor code | `internal/providers` |
| Sandbox runtime | `internal/sandbox` |

## Main interfaces

- `Tool` (dewee `internal/tools/types.go:15`)
- Optional capability interfaces, for example `AsyncTool` (dewee `internal/tools/types.go:43`). Pass dependencies through the constructor, not through dewee-style `*Aware` setters.
- Tool backend interfaces, for example `SearchProvider` (dewee `internal/tools/web_search.go:44`)

## File names

`<family>_<topic>.go`; vendor backend `<tool>_<vendor>.go`; add each tool to the fx value group `tools`

## Imports

- Allowed: `providers` (tools that call a model), `store`, `sandbox`, `bus`, `memory`, `skills`, `workspace`, `tracing`, `crypto`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
