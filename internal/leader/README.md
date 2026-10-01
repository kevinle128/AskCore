# `internal/leader`

The local router process of the `ask leader` command. It holds one agent for all local clients. It is not a second agent. It accepts clients on a Unix socket, forwards their ACP requests to the agent, and sends the agent's answers and updates back. It also holds the client side: `ConnectOrSpawn` connects to the leader, or starts one. See design section 7.3.

## What belongs here

- The leader server: listen, handshake, request id rewrite, routing and fan-out (`server.go`)
- The client: `Connect` and `ConnectOrSpawn` (`client.go`)
- The flock, the pid file and the socket paths (`lock.go`)
- Start of a background leader (`spawn.go`)
- Socket safety: mode 0700 and 0600, peer UID check, protocol version check

## What does not belong here

| Code | Put it in |
|---|---|
| The ACP mapping of agent events | `internal/acp` |
| The agent and its Go API | `internal/agent` |
| Frame and method types | `pkg/protocol` |
| Network gateway for remote clients | `internal/gateway` |

## Main interfaces

- None required. The server receives the agent's ACP stream as an `io.ReadWriteCloser`; `internal/app` builds it

## File names

`server.go`, `client.go`, `lock.go`, `spawn.go`

## Imports

- Allowed: standard library, `pkg/protocol`, `logs`
- Denied: `internal/agent`, `internal/acp` (the leader only sees bytes); `internal/gateway`, `internal/http`, `internal/channels/<vendor>`; `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- The leader holds no agent logic. It must not look inside a prompt or a tool call.
- A client that disconnects does not cancel the run.
- Only the owner of `~/.ask` may connect.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
