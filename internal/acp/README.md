# `internal/acp`

The Agent Client Protocol adapter over the agent's Go API. It is the server side: it lets clients (the leader, an editor over stdio) talk to the agent. It is not `internal/providers/acp`, which is the client side and runs other coding agents as subprocess providers. ACP plus the `_ask/*` methods is the only protocol between the agent process and anything outside it. Inside the process, packages call each other as Go code.

## What belongs here

- The implementation of the ACP `Agent` interface on top of the `agent` Go API (`agent.go`)
- The mapping from internal events to `session/update` notifications (`updates.go`)
- `_meta` fields for Ask-only data such as `seq` and `runId` (`meta.go`)
- The `_ask/*` method handlers: steer and follow-up, compact, fork and tree, usage (`ask_methods.go`)
- The stdio server for `ask acp` (`stdio.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Frame, method and `_meta` type definitions | `pkg/protocol` |
| The agent loop | `internal/agent` |
| Subprocess agents as providers | `internal/providers/acp` |
| Socket routing | `internal/leader` |

## Main interfaces

- Implements the ACP `Agent` interface from the ACP Go SDK

## File names

`agent.go`, `updates.go`, `meta.go`, `ask_methods.go`, `stdio.go`

## Imports

- Allowed: `agent`, `sessions`, `bus`, `pkg/protocol`, the ACP Go SDK
- Denied: `internal/leader`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>`; `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- Hooks never travel over ACP. ACP has no sync-hook concept.
- Headless mode does not use this package. It calls the `agent` Go API directly.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
