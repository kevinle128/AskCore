# `internal/tools`

Everything an agent can call as a tool. The package is flat: a filename prefix groups the files of one tool family. Tools run without an approval step. A user who wants to block a command writes an extension that handles the `tool_call` event.

## What belongs here

- The `Tool` interface, the optional `Sequential` and `ArgumentPreparer` interfaces, and the per-call `Context` (`types.go`). A tool returns `protocol.ToolExecutionResult`.
- `SourceInfo`, where a registered tool came from (`source.go`)
- `Registry` (`registry.go`), argument coercion (`coerce.go`) and validation with the model-facing error text (`validate.go`)
- Builtin tools, one prefix for each family: `echo` (`echo.go`), `filesystem_*`, `shell*`, `web_fetch*`, `web_search*`, `subagent_*`, `skill_*`, …
- Tool backends by vendor: `<tool>_<vendor>.go` (for example `web_search_brave.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Remote MCP tools | `internal/mcp` (it registers a bridge tool here) |
| LLM vendor code | `internal/providers` |
| Sandbox runtime | `internal/sandbox` |
| Role-based access for the gateway | `internal/permissions` |

## Main interfaces

- `Tool`: `Decl()` and `Execute(ctx, Context, args)`. `ctx` is the abort signal.
- Optional capability interfaces: `Sequential` (one such tool makes its whole batch sequential) and `ArgumentPreparer` (rewrites raw arguments before validation). Pass dependencies through the constructor, not through dewee-style `*Aware` setters.
- Tool backend interfaces, for example `SearchProvider` (dewee `internal/tools/web_search.go:44`)

## File names

`<family>_<topic>.go`; vendor backend `<tool>_<vendor>.go`; add each tool to the fx value group `tools`

## Registry rules

- `Register` rejects a tool with no name, no parameters schema, a schema that does not compile, or a name that is taken. The schema compiles once, at `Register`. A `$ref` to a file or URL is rejected.
- `Lookup` is exact and case-sensitive. `Decls` returns declarations in registration order.
- `Prepare` returns the arguments `Execute` sees. Missing or `null` input becomes `{}`. Then a non-required `null` property is removed when its schema rejects `null`, values are coerced, and the result is validated.
- Coercion is one table for every schema (roadmap D21, Pi's `AI:utils/validation.ts:59-131`). A string becomes a number or integer, `"true"`/`"false"`/`1`/`0` become booleans, a number or boolean becomes a string, `null` becomes the zero value of a required field. `"5.7"` is never truncated to an integer. With several types the first listed type that changes the value wins. An `anyOf` or `oneOf` arm that already validates keeps the value.
- A validation error uses Pi's text and TypeBox's messages, with the raw arguments echoed and capped at 2 KiB plus `... (truncated)`.

## Imports

- Allowed: `providers` (tools that call a model), `store`, `sandbox`, `bus`, `skills`, `workspace`, `tracing`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- There is no built-in allow, deny or approval policy. A later policy handler will be a hook handler, not a file in this package.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
