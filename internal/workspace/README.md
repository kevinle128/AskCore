# `internal/workspace`

The place where a session works. It owns the session cwd, the canonical project root and the project trust store. Every tool and every trust decision uses an explicit cwd from here.

## What belongs here

Implemented: path resolution for tools (`path.go`). Planned: the cwd, project root, trust store and resolvers below.

- Path resolution: `ResolvePath` and `PathVariants` (`path.go`)
- The session cwd and the canonical project root (`resolver.go`, `workspace_context.go`)
- The project trust store: which project folders the user trusts (`trust.go`)
- A resolver for each run kind: agent default, subagent, cron

## What does not belong here

| Code | Put it in |
|---|---|
| File tools | `internal/tools` |
| Settings files | `internal/settings` (it reads the trust decision from here, or receives it through the constructor) |
| Role-based access | `internal/permissions` |

## Path resolution rules

`ResolvePath(cwd, p)` turns the path argument of a tool into a clean absolute path. Every file and search tool calls it.

1. One leading `@` is removed.
2. A `file://` URL becomes a local path. A URL with a host other than `localhost`, or without a path, is an error.
3. `~` and `~/` expand to the home directory.
4. A relative path joins the explicit `cwd`. A relative path with an empty `cwd` is an error.
5. The result is cleaned. The function does not check that the path exists and does not limit the path to `cwd`. Symbolic links are resolved by the callers that need it (`FileState` and `PathLock` in `internal/tools`).

`PathVariants(abs)` returns other spellings of a path that is missing on disk: the shell-unescaped form, a narrow no-break space before `AM` or `PM` (macOS screenshot names), decomposed Unicode (NFD) and a curly apostrophe, alone and combined.
File and search tools try the variants in order and use the first that exists.
`write` creates the given path when no existing variant is found.

## Main interfaces

- `ResolvePath`, `PathVariants`
- Resolver interface (planned)

## File names

`path.go`; planned: `resolver.go`, `workspace_context.go`, `trust.go`

## Imports

- Allowed: standard library, `golang.org/x/text/unicode/norm`, `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- Project-level files (`.ask/settings.json`, project skills, project `AGENTS.md`) load only after the project is trusted.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
