# `internal/workspace`

The place where a session works. It owns the session cwd, the canonical project root and the project trust store. Every tool and every trust decision uses an explicit cwd from here.

## What belongs here

- The session cwd and the canonical project root (`resolver.go`, `workspace_context.go`)
- The project trust store: which project folders the user trusts (`trust.go`)
- A resolver for each run kind: agent default, subagent, cron

## What does not belong here

| Code | Put it in |
|---|---|
| File tools | `internal/tools` |
| Settings files | `internal/settings` (it reads the trust decision from here, or receives it through the constructor) |
| Role-based access | `internal/permissions` |

## Main interfaces

- Resolver interface

## File names

`resolver.go`, `workspace_context.go`, `trust.go`

## Imports

- Allowed: `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- Project-level files (`.ask/settings.json`, project skills, project `AGENTS.md`) load only after the project is trusted.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
