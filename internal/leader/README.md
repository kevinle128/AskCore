# `internal/leader`

The local router process of the `ask leader` command. It holds one agent for all local clients. It is not a second agent. It accepts clients on a Unix socket, forwards their ACP requests to the agent, and sends the agent's answers and updates back. It also holds the client side: `ConnectOrSpawn` connects to the leader, or starts one. See design section 7.3. The plan and its review are in [plans/261008-1033-h13b-leader-unix-socket](../../plans/261008-1033-h13b-leader-unix-socket/plan.md).

## What belongs here

| Area | Files |
|---|---|
| Wire | `frame.go`: length-prefixed frames on the socket (64 MiB at most) and the line codec of the agent link (65 MiB at most) |
| Handshake and security | `handshake.go`, `peer*.go` (peer user check by the kernel: `SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS), `paths.go` (private home, no symlink, short socket path), `lock.go` (lifetime lock, owner record, stale socket) |
| Routing | `server.go` (listener, client goroutines, control frames, ordered shutdown), `router.go` (one goroutine that owns all routing state), `reverse.go` (calls of the agent to clients), `policy.go` (the one method table), `ids.go` (request ids), `rpc.go`, `queue.go` |
| Client and start | `client.go` (`Connect`, `ConnectOrSpawn`, `Status`, `Stop`), `spawn.go` (find and probe `ask`, start the leader, log, process identity), `proc_*.go`, `dup_*.go` |

## What does not belong here

| Code | Put it in |
|---|---|
| The ACP mapping of agent events | `internal/acp` |
| The agent and its Go API | `internal/agent` |
| Frame and method types | `pkg/protocol` |
| Network gateway for remote clients | `internal/gateway` |
| Composition of the shared host | `internal/app` (`module_leader.go`) |

## Main interfaces

- `Connect` only connects to a running leader; a protocol mismatch returns an error without stopping it.
- `ConnectOrSpawn` owns automatic starts and replacement.
- `Server` takes the ACP stream of the agent as an `io.ReadWriteCloser` (one JSON message for each line). `internal/app` builds it from the adapter. `Server.Close` closes it.
- `ServerConfig` and `Config` hold the identity, the owner user, the callbacks `ActiveRuns` and `QuiesceIfIdle`, and the register timeout. There are no end-user flags for limits.
- `ConnectConfig` holds the home paths, the register frame, the deadline and the caller executable. `StartFn` replaces the process start in tests.

## File names

`server.go`, `client.go`, `lock.go`, `spawn.go` and the files in the table above.

## Imports

- Allowed: standard library, `pkg/protocol`, `logs`, `golang.org/x/sys/unix` (peer credentials), `go.uber.org/zap` (the logger type that `logs` builds). Tests may also use testify and goleak.
- Denied: `internal/agent`, `internal/acp` (the leader only sees bytes); `internal/gateway`, `internal/http`, `internal/channels/<vendor>`; `internal/config`. Depguard enforces it (rule `leader-router-boundary`).

## How it works

- **Handshake.** `register` then `registered`. The register frame holds the outer leader protocol version. A different version is refused with the side that must upgrade. That connection may then send only a `status` or a conditional `shutdown` control frame, never ACP. The build identity (`build`) is for diagnostics: a different build only prints a hint. The register and registered frames and the status also carry `commitTime` (the commit time of a VCS-stamped build) or `devStamp` (a build without a VCS stamp: the modification time of its executable and the SHA-256 of its bytes); the replacement rule below reads them. Both are optional, so the protocol version stays the same and a peer from before them ignores them.
- **One link to the agent.** The leader sends one `initialize` to the agent at start. It answers the `initialize` of each client from that result and keeps the capabilities of each client. Every forwarded call gets a router-made id and a route context in `_meta["ask.dev/route"]` (client, the session that the router checked, driver generation, whether a driver is live, driver capabilities). The router replaces any value that a client sends under that key, and drops a key that a decoder would take for `_meta`. A call whose params hold two keys that differ only in case (`sessionId` and `sessionid`) is refused: Go decoders match keys without regard to case and take the last one, so otherwise the router could check one session while the host acts on another. The host also checks the session id of the route against the one in the params. Capabilities that a client declares are limited to 64 KiB.
- **Sessions.** The creator of a session is its driver. `_ask/session/attach` adds an observer. A read (state, usage, models) or follow call from a client that is not in the session yet adds it as an observer, after every check passed. Calls that change the session (prompt, cancel, model, thinking, steer, follow-up, remove, reset, continue, close) need the driver. When the host answers `session/close`, the router forgets the session: every member leaves it, its follows end without an unfollow (the host ended them), and its open questions end. Frames already queued for a member are still sent. `_ask/session/take` makes the caller the driver when the session has no live driver, or when it is idle. The host owns the driver generation and returns the new one.
  Detach fences a pending take even when its caller has no membership.
- **Provider setup.** The seven provider methods (`_ask/providers/list`, `api_key`, `login/start`, `input`, `wait`, `cancel`, `logout`) need an initialized client and no session. The router forwards them with the route of the client id only, so the answer returns by request id. `_ask/providers/changed` from the host goes to every initialized client, whatever its sessions. The router sends `_ask/providers/release_client` to the host, with the route of the gone client, for a client that made a provider setup call. A client cannot send that method: it is not in the policy table. Params are never logged, so a key or a code stays in the frame only.
- **Output.** Each client has one writer goroutine and an unbounded queue, as Grok has.
The router never waits for a socket.
A client that does not read keeps its place and later gets every frame in order.
A client whose frame is over the limit, or whose write fails, is closed alone.
A client that leaves never cancels a run.
The router releases that client's pending sign-ins through the provider host; see [Provider methods](../acp/README.md#provider-methods).
- **Questions.** A permission request goes to every client of the session. Each client gets an id of its own. The first valid answer wins: it must come from a client that was asked, for an open call, and select an offered option or say cancelled. The other clients are told to drop the question. A client that attaches later sees the open questions. When the driver leaves, detaches or is replaced, the open questions of that generation end with a cancelled error to the agent, and the run goes on. Calls for files and terminals go to the driver only, if its capabilities allow them; a terminal belongs to the client that made it. When nobody can answer, the agent gets a safe error at once. A question whose last recipient leaves ends with a cancelled error, so the agent never waits for a client that is not there.
- **Lifecycle.** The lifetime lock on `leader.lock` holds the owner record (PID, start time, instance). The lock file is never removed, so its inode is stable. Artifact opens and log rotation use a checked private directory descriptor.
  The lock holder keeps that directory open for its lifetime.
  Only the lock holder removes a stale socket, and only a socket that the user owns.
  The owner record has a fifth line, the state: `serving` after the socket listens, `stopping` from the first step of `Server.Close` (`Config.Stopping`), before the listeners close. A record without it (an older leader) has no state.
  Socket cleanup checks the inode through the held directory and does not remove a replacement. `ask leader` starts, takes the lock, builds everything that can fail, opens the socket with mode 0600, then serves. A client that is the ask program itself (`CallerIsAsk`: the TUI, `ask connect`, `ask leader status|stop`) starts the leader from its own binary while that file exists, whatever its name, so `go run ./cmd/tui` starts a leader too. Other programs find `ask` next to themselves, then on the PATH. The client runs `<binary> version --json` first and starts `<binary> leader --spawned-by-client` in its own session. The leader writes the absolute path it runs from into the owner record, and the process check accepts `<that path> leader` as well as `ask leader`. The process that wins the lock rotates the log. `stop` sends `shutdown` first.
  On Linux, SIGTERM fallback uses a pidfd, checks the process identity, and checks that the lock owner record did not change.
  On macOS, fallback refuses to signal because a stable process handle is not available.
- **Start retry.** A leader that this call started and that ends with an error before it serves is started again, up to three starts in a call (`maxStarts`), inside the deadline. The binary is found and probed once. A binary or probe error is not retried. The error keeps the log tail and says how many starts were made.
- **Eviction of a leader without a socket.** `ConnectOrSpawn` (never `Connect`) stops a lock holder that has no connectable socket when its record says `serving`, it is older than `ConnectConfig.ZombieAfter` (30 s), and process verification passes. On Linux it sends SIGTERM through one pidfd, waits up to `ConnectConfig.EvictWait` (8 s), re-reads the record, then sends SIGKILL through the same pidfd, and waits again. A record that turned `stopping` is never killed. A leader that stops is never evicted. macOS keeps the plain wait and `ErrLeaderStarting`.
  Cancellation or the call deadline stops the wait and prevents further signals.
- **Replacement.** A protocol mismatch replaces an older leader only when the leader was started by a client, reports idle, and agrees to stop in one step. A busy, supervised or newer leader stays.
  The idle barrier includes forwarded requests until the host answers, including requests whose callers disconnected.
  For the same protocol, a client of a newer build replaces an older leader that a client started, also when it is busy: `shutdown` without `ifIdle`, then a new start. All sessions of the old leader end and their background jobs are killed; other clients reconnect. This happens once per call, only when the binary that would start is also newer, and never as a downgrade. A leader that the user started by hand or a supervisor started stays, and the client prints a hint.
  "Newer" is a later commit time between two VCS builds. Between two builds without a VCS stamp (such as `go run`) it is another SHA-256 of the executable with a later file modification time: the binary written last wins, and the same bytes never replace each other. A VCS build and a build without a VCS stamp are never compared. An unknown or unparseable stamp keeps the leader. `ask leader stop` restarts the leader by hand.

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- The leader holds no agent logic. It must not look inside a prompt or a tool call.
- A client that disconnects does not cancel the run.
- Only the owner of `~/.ask` may connect.
- Routing tests here use a scripted agent stream and prove the router only. Tests with the real host are in `internal/app/leader_routing_test.go`, and tests with the built binary are in `cmd/tui` (`leader_e2e_test.go`, `connect_e2e_test.go`).

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
