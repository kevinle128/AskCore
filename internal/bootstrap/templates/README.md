# `internal/bootstrap/templates`

Data folder for the embedded prompt templates (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `USER.md`, `BOOTSTRAP.md`). It is not a Go package. `internal/bootstrap` embeds these files with `//go:embed`.

## What belongs here

- Template `.md` files only

## What does not belong here

| Code | Put it in |
|---|---|
| Go code | `internal/bootstrap` |

## File names

`<NAME>.md` in upper case, as the agent sees it

## Imports

- Allowed: n/a
- Denied: n/a

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../../docs/ask-architecture-reference.md)
