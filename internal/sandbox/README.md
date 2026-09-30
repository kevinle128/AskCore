# `internal/sandbox`

Isolated command execution for shell and code tools.

## What belongs here

- `Sandbox` and `Manager` interfaces, Docker and local implementations, file-system bridge

## What does not belong here

| Code | Put it in |
|---|---|
| The shell tool | `internal/tools` |

## Main interfaces

- `Sandbox`, `Manager` (dewee `internal/sandbox/sandbox.go:185-198`)

## File names

`sandbox.go`, `<kind>.go` (for example `docker.go`)

## Imports

- Allowed: standard library, Docker SDK
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
