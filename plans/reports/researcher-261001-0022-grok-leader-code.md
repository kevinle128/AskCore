# Grok Build "leader" model: verified from source, distilled for AskCore

Date: 2026-10-01. Source: xai-org/grok-build at 2bdd1d6a (local checkout), Rust. Method: grep and read of the files cited, plus `git log`. No leader was started. The GitNexus MCP tools were not available in this session, so all evidence comes from grep, read and git on the local checkout.

Path prefixes used below (all under `crates/codegen/`):
- `SH` = `xai-grok-shell/src`
- `PG` = `xai-grok-pager/src`
- `BIN` = `xai-grok-pager-bin/src`

## 0. Outcome first

1. The earlier report is right on the big picture (optional, off by default, `-p` does not attach) and wrong on five points that matter: the code is open (Apache-2.0); auto-spawned leaders do NOT exit on last disconnect; there IS a version handshake (advisory, plus eviction of older leaders); the wire is length-prefixed JSON envelopes, not bare JSON-RPC; and there is a real env var (`GROK_LEADER_SOCKET`).
2. Most important finding for AskCore: **`grok -p` never uses the leader.** `run_single_turn` always builds an embedded agent (`PG/headless.rs:804`, `:892`). So Grok gives no code evidence for "`ask -p` is a client of the daemon". In Grok the leader serves the TUI and IDE/stdio bridges only, and only when leader mode is on.
3. Grok's leader is an ACP multiplexer inside the same binary. Auto-spawn costs about 500 lines of hard lifecycle code (flock, eviction, zombie handling, version floor) in `SH/leader/mod.rs` and `lock.rs`. The wire and handshake part is small and worth copying. The auto-spawn part is the expensive part.
4. Grok has **no peer-credential check and no explicit socket permissions** (grep: zero `peer_cred`, zero `set_permissions` in the leader code). AskCore must do better.
5. Ranking for AskCore (section 7): **B (minimal Unix socket, explicit handshake) with C kept for the H2 loop work, then D as opt-in.** This changes the earlier ranking (C, then B at H13) only in timing: B is cheap and is the only way `ask -p` can exist at all, because `cmd/tui` may not import `internal/*`.
6. License: Apache-2.0. Copying code is allowed with notice. A Go port is still a derivative work if it follows the Rust closely. Safest: copy the design, cite the source.

## 1. Earlier claims vs code

| # | Doc-based claim (earlier report) | Verdict | Evidence |
|---|---|---|---|
| 1 | "Closed source, no code" | Corrected: open source, Apache-2.0 | `LICENSE`, `Cargo.toml:120` |
| 2 | Leader is optional, off by default | Confirmed, with nuance: a remote setting `leader_mode` can turn it on in `release-dist` builds; `--leader` flag and `[cli] use_leader` also do | `PG/app/mod.rs:495-540` |
| 3 | Auto-spawned by interactive clients | Confirmed. One function does it: `connect_or_spawn` | `SH/leader/mod.rs:1376`; callers `PG/acp/mod.rs:321`, `BIN/main.rs:1446`, `BIN/main.rs:729` |
| 4 | Default: exit when last client disconnects | Corrected: the exit rule exists, but every auto-spawned leader passes `--no-exit-on-disconnect`, so it stays until killed, updated, or evicted | `SH/leader/mod.rs:1630`; rule at `SH/leader/server.rs:1703` |
| 5 | Socket `~/.grok/leader.sock`, `--leader-socket` | Confirmed, extended: `--leader-socket` just sets env `GROK_LEADER_SOCKET`; a non-prod relay URL adds a hash suffix (`leader-<8hex>.sock`); lock file is the sibling `.lock` | `SH/leader/lock.rs:16-33`, `:40`, `:62-90` |
| 6 | "No leader-specific env var found" | Corrected: `GROK_LEADER_SOCKET`, `GROK_LEADER_LOG`, plus pass-through of four debug vars to the child | `lock.rs:40`, `mod.rs:1634-1646`, `:1679` |
| 7 | `grok -p` probably does not use leader | Confirmed, strongly: no code path from `run_single_turn` to the leader. Caveat: a doc comment on `ClientMode::Stdio` says "grok agent stdio, grok -p"; that comment is stale | `PG/headless.rs:804-892`; `SH/leader/protocol.rs:167-172` |
| 8 | TUI uses leader under `use_leader` | Confirmed, plus NEW: if the leader connect fails, the TUI falls back to an embedded agent | `PG/app/mod.rs:1070-1130` |
| 9 | Tools run in the leader | Confirmed. NEW: a client may advertise `terminal`/`fs_read`/`fs_write`, then the agent routes those calls back to that client over ACP | `SH/agent/app.rs:932-990`; `protocol.rs:179-230` |
| 10 | Wire is ACP JSON-RPC (inference) | Corrected: a 4-byte big-endian length prefix plus a JSON envelope (`type`-tagged `Register`/`Acp`/`Control`/`Ping`/`Disconnect`). ACP JSON-RPC travels as a string field `payload` inside `Acp` | `protocol.rs:9-133`, `:485-500`, `:526-566` |
| 11 | "No documented version handshake" | Corrected: a handshake exists. It is advisory for ACP, hard for `Control` commands, and newer clients evict strictly older leaders | section 4 |
| 12 | Stale blank status row = old leader | Confirmed in spirit: `x.ai/leader/version_mismatch` notification becomes a toast "Restart grok to match" | `SH/leader/server.rs:1479-1500`, `PG/acp/version_mismatch.rs:24-30` |
| 13 | Sandbox refuses the leader | Confirmed, at two layers: the resolver vetoes leader mode, and `connect_or_spawn` itself refuses | `PG/app/mod.rs:526-531`; `SH/leader/mod.rs:1381-1383`; test `SH/../tests/test_leader_sandbox_confinement.rs` |
| 14 | Auth: one background attempt, stderr to `leader.log` | Refined: the leader does a bounded non-interactive auth BEFORE declaring ready; a client that registers early waits for `LeaderReady`. It never mints credentials in that step | `SH/agent/app.rs:852-876`; `server.rs:2395-2420`; `client.rs:28`, `:367` |
| 15 | `leader.log` holds stderr | Confirmed, extended: append mode, rotate to `leader.log.1` over 32 MiB, stdout and stdin are null | `mod.rs:1609-1650` |
| 16 | `leader list` "may probe sockets" | Confirmed: it lists `leader*.lock` / `leader*.sock` in `~/.grok`, then connects and sends `GetLeaderInfo` to classify each one | `mod.rs:396-470`, `:269` |
| 17 | `leader kill` stops all | Confirmed: SIGTERM after an `is_grok_process` check; also removes stale lock/socket files | `BIN/main.rs:382-420`; `shell-base/src/util/mod.rs:221-235` |
| 18 | Model option proxy, broadcast to clients | Partly confirmed: the leader snoops `session/setModel` and `setConfigOption(model)` to keep each client's `default_model`; model-list changes are broadcast | `server.rs:906-935`, `:467-476` |
| 19 | Plugins per-process dir ignored in leader mode | Confirmed | `BIN/main.rs:1412-1414` |
| 20 | Ring 4 and 5 "unobserved" | Now covered from code (sections 4, 5, 6) | - |
| 21 | New: persistent `--relay-on-demand` marker | NEW: the flag marks "auto-spawned by a client" and lets the update/policy logic kill only those leaders, never systemd-supervised ones | `mod.rs:93-95`, `:541-547` |

## 2. Ring 0: core

1. **Where is it?** Crate `xai-grok-shell`, module `SH/leader/` (about 15,000 lines including 5,130 lines of server tests):
   - `mod.rs` (2,638): discovery, `connect_or_spawn`, spawn, eviction, reconnect.
   - `server.rs` (2,626): the ACP multiplexer, routing, fan-out.
   - `client.rs` (1,260): `LeaderClient`, register, read/write loops.
   - `protocol.rs` (1,240): frames and message enums.
   - `lock.rs` (710): flock, pid file, path resolution.
   - `transport.rs` (280): `UnixStream`/`UnixListener` on Unix, named pipes on Windows.
   - The leader body itself: `run_leader` in `SH/agent/app.rs:698`.
   - Client side in the TUI: `PG/acp/mod.rs:283` (`connect_via_leader`), `PG/acp/leader_bridge.rs`.
   - Management CLI: `BIN/main.rs:335` (`run_leader_mgmt`).
2. **Entry point.** `grok agent leader` parses in `PG/app/cli.rs` (flag `no_exit_on_disconnect` at `:382`), then `BIN/main.rs:1677` builds `LeaderRunOptions` and calls `run_leader` (`SH/agent/app.rs:698`). The same binary is both leader and client.

## 3. Ring 1: lifecycle

### 3.1 Who spawns

Only `connect_or_spawn` (`mod.rs:1376`) spawns. Three callers:
- TUI: `connect_via_leader` (`PG/acp/mod.rs:321`), only when `use_leader` resolves true.
- `grok agent --leader` stdio or headless bridge (`BIN/main.rs:1446`).
- Door CLIs (`workspace`, cursor worker) (`BIN/main.rs:729`).
- `grok leader list/info/kill` never spawn.
- `grok -p` does not call it.

### 3.2 Exact spawn code (`mod.rs:1626-1680`)

- Command: `<exe> agent leader --no-exit-on-disconnect --relay-on-demand --grok-ws-url <u> --grok-ws-origin <o>`.
- The exe is `current_exe()`, except for a managed install where it prefers `~/.grok/bin/grok`, because an auto-update swaps that symlink and `current_exe()` would relaunch the old binary (`:1571-1607`).
- stdin null, stdout null, stderr is `~/.grok/leader.log` (append).
- Detach: `process_group(0)` on Unix (`:1667`), `CREATE_NEW_PROCESS_GROUP` on Windows. **No `setsid`, no double fork.** The client keeps the `Child` and reaps it in a thread (`:1681`), so the leader survives the client only because it is in its own process group and is not a session leader. It has no controlling terminal issue because stdio is redirected.
- Env: passes `GROK_LEADER_SOCKET`, four debug vars, and `RUST_LOG` (from `GROK_LEADER_LOG`, then `RUST_LOG`, then a default allowlist).

### 3.3 How a client finds and connects (`mod.rs:1376-1570`)

1. Sandbox requested: return error `SandboxConfinement` (`:1381`).
2. Compute socket and lock path (`lock.rs`). If the socket file exists (`listener_is_ready` is just `path.exists()`, `transport.rs:13-22`):
   - Read the pid in the lock file. If the pid is dead, skip the connect (stale socket).
   - Otherwise connect and register. If the leader is not older than the client, adopt it.
3. Otherwise enter a loop with `lock.try_acquire()`:
   - `Ok(true)` (we hold the flock): re-check for a sibling-spawned leader, else **release the flock** and spawn, then poll the socket for up to 10 s (`SPAWN_WAIT_TIMEOUT`, `:91`) at 100 ms steps. After 3 failed spawn attempts return `SpawnFailed` (`:1313`).
   - `Ok(false)` (someone else holds it): poll the socket and adopt; else run the zombie check (3.4).
   - `AcquireInProgress` (another process is wedged inside its own open/flock, for example a stalled network home): return an error and do NOT spawn (`:1483-1486`).
4. `--leader-socket` and `GROK_LEADER_SOCKET` replace the default path entirely and skip the URL suffix (`lock.rs:38-60`). The spawned child inherits the env, so both sides agree.

### 3.4 Missing, stale, crashed, raced

- **Missing socket:** no file means no connect attempt; go to the lock loop and spawn.
- **Stale socket (file exists, process dead):** the client does not delete it. It skips the connect when the lock-file pid is dead (`:1386-1395`), spawns, and **the new leader removes the old file**: `lock.cleanup_socket()` after it gets the flock (`SH/agent/app.rs` (`lock.cleanup_socket()` right after the lock match, about `:770`)) and `remove_file` again before `bind` (`server.rs:1520`). ECONNREFUSED is not matched by name; any connect-level failure is classified by `is_connect_level_failure` (`mod.rs:1325`) and drives retry.
- **Crashed leader:** the kernel drops the flock on process death, so the next client wins the lock and respawns. The `Drop` of `LeaderLock` also removes lock and socket files if the holder drops while `was_leader` (`lock.rs:260-270`).
- **Zombie leader (alive, holds flock, will not accept):** if the same pid stays unconnectable for 30 s (`ZOMBIE_EVICT_DEADLINE`, `:102`), SIGTERM it, at most 3 times per pid. The holder is only trusted on Linux via `/proc/locks`; on macOS the holder is unknowable and eviction is skipped (`:1224-1262`). This is a known gap, not a bug I found.
- **Two clients spawn at once:** the flock in `leader.lock` serializes the decision, but the client releases the flock before the child runs (`:1437-1441`), so two leader children can start. The child takes the flock itself (`run_leader`, `app.rs:723`); the loser sees "Another leader already holds the lock" and exits (`:735-741`), or times out after 15 s (`LEADER_ACQUIRE_TIMEOUT`, `:56`). Both clients then adopt the winner's socket. So: a flock decides the single writer, losers exit, clients tolerate a short race instead of avoiding it.

### 3.5 Which paths use the leader, under which config

Resolver `resolve_leader_mode` (`PG/app/mod.rs:499`), highest first: `--no-leader`, `--leader`, eligibility, `[cli] use_leader` in config, remote `leader_mode` (release-dist only), default off. A requested sandbox profile vetoes it last.

| Mode | Uses leader? | Evidence |
|---|---|---|
| `grok` (TUI) | Only when resolved true; on connect failure falls back to embedded | `PG/app/mod.rs:1070-1130` |
| `grok -p` / single turn | Never | `PG/headless.rs:804-892` |
| `grok agent stdio` / headless with `--leader` | Yes (IDE bridge, reads ACP lines from stdin and forwards) | `BIN/main.rs:1411-1480` |
| `grok --chat` with leader on | Refused as a conflict | `PG/app/session_startup.rs:314-318` |
| Sandbox profile requested | No; in-process | `mod.rs:1381` |

### 3.6 What runs where

| Concern | Runs in | Evidence |
|---|---|---|
| Model/provider calls, tool execution, sessions, compaction | Leader (`MvpAgent` inside `run_leader`) | `SH/agent/app.rs:932-990` |
| MCP and config file watching | Leader | `app.rs:1000+` (`config_watcher_path_tx`); doc claim confirmed |
| Auth refresh | Leader: one shared `AuthManager`, `start_proactive_refresh` | `app.rs:889-890` |
| Rendering, input, `default_model`, yolo/auto flag | Client; sent as `ClientCapabilities` at Register | `protocol.rs:179-230` |
| Terminal and file read/write when advertised | Client, called back over ACP | `protocol.rs:195-215` |
| Auto-update check, grok.com relay, cursor worker | Leader | `BIN/main.rs:1640-1665` |

### 3.7 How the leader stops

- Client-last-disconnect exit exists, but only if `--no-exit-on-disconnect` is absent and at least one client ever connected (`server.rs:1703`, `had_clients`). Auto-spawn always sets the flag, so it never applies to spawned leaders.
- SIGTERM or cancel: server broadcasts `ShuttingDown{Manual}` then `Shutdown`, drains client queues, removes the socket (`server.rs:1553-1559`, `:2527`, `:2300`). The signal handler itself is `agent_command::spawn_signal_flush` (`BIN/main.rs:1235`), which I did not read.
- Auto-update: `ShuttingDown{AutoUpdate}` so clients reconnect at once, via `connect_or_spawn` (`protocol.rs:505-515`).
- `RelaunchForUpdate` control command from a newer client or `grok update` (`BIN/main.rs:2738-2780`).
- `IdleTimeout` is reserved and never sent (`protocol.rs:507-513`).
- `grok leader kill`: SIGTERM (`shell-base util/mod.rs:221`).

## 4. Ring 2: surface

### 4.1 Wire protocol

- **Framing:** `u32` big-endian length, then JSON bytes; max 64 MiB (`protocol.rs:9`, `:26-42`, `:118`). The reader is cancel-safe (keeps a partial frame in a buffer) (`:47-84`).
- **Envelope:** `ClientMessage` = `Register{client_type, mode, capabilities}`, `Acp{payload: String}`, `Control{request_id, command}`, `Ping`, `Disconnect`. `ServerMessage` = `Registered`, `Acp`, `ControlResult`, `Pong`, `Error{code,message}`, `ShuttingDown`, `Shutdown`, `LeaderReady` (`protocol.rs:485-566`).
- **Inside `Acp.payload`** is one ACP JSON-RPC message as a JSON string (double encoding). The leader rewrites request ids to `"<clientId>|<originalIdJson>"` and restores them on the response (`server.rs:35`, `:378-405`).
- **Control commands:** `GetLeaderInfo`, CPU profile start/stop/status, workspace start/pause/resume/stop/status, cursor worker start/stop/status, `RelaunchForUpdate` (`protocol.rs:365-395`).
- **Extension methods:** `x.ai/...` notifications, for example `x.ai/sessions/changed`, `x.ai/models/update`, `x.ai/leader/version_mismatch`, and internal `x.ai/internal/evict_sessions`.
- **Handshake:** first client frame must be `Register`, within 30 s (`server.rs:34`, `:2345-2395`). Anything else gets `Error{code:1,"Expected Register message"}`. The server answers `Registered{client_id, ready, leader_protocol_version, leader_binary_version, leader_capabilities}`. If `ready=false` the client waits for `LeaderReady`; ACP sent before ready gets a structured `leader_starting` JSON-RPC error (`server.rs:1800-1820`).

### 4.2 Version check, exactly

- `LEADER_PROTOCOL_VERSION = 1` (`protocol.rs:174`). The client checks it only inside `send_control` (`client.rs:150-180`). It does NOT reject a leader with another protocol version for ACP traffic.
- New fields must be `#[serde(default)]`, because "the leader and client can run different binary versions" (`protocol.rs:523`). Compatibility is by additive change, not by version gate.
- Binary version: the client sends `client_version`. If it differs from the leader's (and the leader is not a dev build reporting `unknown`), the leader sends `x.ai/leader/version_mismatch` and the TUI shows a toast (`server.rs:1625-1640`).
- **Eviction:** if the leader's semver is strictly older than the client's, the client replaces it: `RelaunchForUpdate` if supported, else SIGTERM, then waits up to 8 s, re-signals, then respawns (`mod.rs:105-120`, `:1090-1190`). A newer leader is never replaced by an older client, so the machine converges to the newest version. Unparseable versions are left alone.

### 4.3 Socket permissions and peers

- No explicit permission call in the leader code (grep `set_permissions`, `PermissionsExt`, `0o7`, `0o6` in `SH/leader`: none). `UnixListener::bind` makes the socket with the process umask.
- `~/.grok` is created by `create_dir_all` with default mode (`xai-dirs/src/lib.rs:86`).
- No peer-credential check: grep `peer_cred`, `SO_PEERCRED`, `getpeereid` across all crates: zero hits.
- Consequence: access control is whatever umask and the home directory give. Any local process that can connect can register with `yolo_mode: true` (a client capability the leader honours for that client's sessions, `protocol.rs:179-184`) and drive an agent with shell tools.
- Windows: named pipe `\\.\pipe\grok-leader-<siphash of path>`, with `first_pipe_instance(true)` to stop squatting (`transport.rs` Windows part).
- Whether macOS enforces socket-file write permission on `connect` is not verified here.

### 4.4 `grok leader list/info/kill`

- Discovery is `read_dir(~/.grok)` for `leader*.lock` and `leader*.sock`, grouped by the middle part of the name (`mod.rs:396-470`). Not a glob on `leader-*.sock` only; `leader.sock` and `leader-x.sock` both match. A `--leader-socket` path outside `~/.grok` is not found.
- Classification: `Reachable` (connected, got info), `Stale` (lock only), `Unreachable`, `UnsupportedProtocol`, `Ambiguous` (`mod.rs:126-133`).
- `info` connects and sends `GetLeaderInfo`; it needs `control_v1` capability, else prints "legacy".
- `kill`: for each descriptor with a pid, if not a grok process then delete its lock and socket files as stale; else SIGTERM (`BIN/main.rs:382-420`).

## 5. Ring 3: interactions

- **Session ownership:** per session the leader tracks `session_subscribers` (set of clients) and `session_driver` (one client). The first client that sends or receives a message for a session becomes its driver; the rest are subscribers (`server.rs:1526-1535`, `:1821-1832`, `:1942-1960`).
- **Routing of agent output:** session events go to all subscribers; reverse requests (for example file or terminal calls) go to the driver only; blocking interactions (permission prompts, questions, plan approval) are shared with all subscribers (`server.rs:506-520`, `:2160-2200`). Notifications with no session id that are not machine-wide go to the `last_active_client` (`:2281-2290`).
- **Broadcast:** `x.ai/sessions/changed`, `x.ai/models/update`, `x.ai/mcp/servers_updated`, `x.ai/announcements/update` go to every client (`server.rs:467-476`, `:2046-2053`). Model changes per client are tracked by snooping (`:906-935`). Plugin dirs and most agent config are fixed at leader start; `warn_ignored_flags` tells the client which flags it cannot change (`PG/acp/mod.rs:300-303`).
- **Abort routing:** there is no special case for `session/cancel` in the server (grep found no cancel method in `server.rs`); it is forwarded like any other ACP message. So any client can cancel any session it names. No ownership check was found; what the agent does after receiving it is unverified.
- **Client disconnect mid-run:** the run continues. On disconnect, sessions with no remaining subscriber are reported to the agent via `x.ai/internal/evict_sessions`; the agent keeps busy ones resident and unloads only idle ones ("keep working sessions resident, idle-unload the rest (never destroy)", `SH/agent/mvp_agent/agent_ops.rs:2522-2560`). If the disconnected client was the driver and others remain, the driver moves to another subscriber (`server.rs:1670-1686`). RPC responses for a gone client are dropped and logged as orphaned (`:1924-1937`). Tests named `leader_reattach_*_roundtrips_durable_log` suggest re-attach replays a durable log; not read.
- **Reconnect:** `LeaderReconnector` backs off 1 s to 30 s; bounded to 5 attempts for headless, unlimited for the TUI (`mod.rs:121-126`). Each attempt goes through `connect_or_spawn`, so a dead leader is respawned (`mod.rs:984`).

## 6. Ring 4: edge cases in code

| Case | Handling | Evidence |
|---|---|---|
| Stale socket file | Leader removes it after taking the flock and again before bind | `app.rs` after the lock match (about `:770`); `server.rs:1520` |
| Dead pid in lock file | Client skips the connect and goes to spawn | `mod.rs:1386-1395` |
| Version mismatch | Advisory toast; strictly older leader evicted by a newer client; dev `unknown` exempt | section 4.2 |
| Crashed leader | Flock dies with it; next client respawns; reconnector retries | `mod.rs:1437`, `:984` |
| Hung leader (alive, not accepting) | 30 s zombie timer, SIGTERM, max 3 tries; Linux only | `mod.rs:1337-1375` |
| Spawn never becomes connectable | 3 tries of 10 s, then `SpawnFailed` | `mod.rs:1313`, `:1420-1432` |
| Stalled home filesystem | Acquire-slot guard; second process gets `AcquireInProgress` and does not spawn | `lock.rs:100-140`, `mod.rs:1483` |
| Auth before ready | Bounded non-interactive auth, then `ready`; early clients wait for `LeaderReady` | `app.rs:852-876`; `client.rs:367` |
| Auth refresh | One shared `AuthManager`, proactive refresh, hot swap | `app.rs:889-895` |
| Sandbox requested | `connect_or_spawn` refuses, resolver vetoes, TUI prints a note | `mod.rs:1381`; `PG/app/mod.rs:526` |
| Leader connect fails in the TUI | Falls back to an embedded agent | `PG/app/mod.rs:1105` |
| Client registers too slowly | 30 s timeout, `Error{code:3}` | `server.rs:34`, `:2350-2370` |
| Client sends a non-Register first | `Error{code:1}` and close | `server.rs:2385-2395` |
| Leader log grows | Rotate at 32 MiB | `mod.rs:1609-1623` |
| Supervised leader vs client-spawned | `--relay-on-demand` in argv marks "safe to reclaim" | `mod.rs:541-547` |

## 7. Ring 5: history

- The repo has 51 commits, all "Synced from monorepo". First commit `c68e39f6` dated 2026-07-16 ("Publish harness and TUI open-source"); HEAD `2bdd1d6a` dated 2026-09-29. Real authorship and pre-July history are not recoverable.
- The leader existed at the first commit: the protocol version constant, `should_evict` and zombie logic were already there (`git show c68e39f6:.../leader/mod.rs`: 25 hits for eviction terms).
- `git log` on `SH/leader/`: 26 commits; 7 files in the first commit.
- Changes after publication (by `git log -S`):
  - 2026-08-13 and 2026-09-08: the `no_exit_on_disconnect` handling in `server.rs` was changed (it also existed at the first commit, as did `RelaunchForUpdate`).
  - 2026-09-22: `SlotPolicy` (acquire-slot guard against stalled filesystems) added to `lock.rs`.
  - 2026-09-15 and 2026-08-31: large rewrites of `mod.rs` (+1,594 and +783 lines).
- Reading: the trouble spots were upgrades (relaunch, eviction), zombie leaders and stalled filesystems. All come from auto-spawn plus long-lived processes. The wire and register part barely moved (`leader_protocol_version` has been 1 since the first commit).

## 8. Distillation for AskCore

### 8.1 Mapping

| Grok | AskCore |
|---|---|
| Leader = `grok agent leader` in the same binary | `cmd/server` (already a separate binary; the import rule is enforced by the compiler instead of by convention) |
| Client = TUI, IDE bridge | `cmd/tui` (`ask`). Must speak `pkg/protocol` only |
| `grok -p` = embedded in-process | No equivalent: `cmd/tui` cannot import `internal/*`. `ask -p` must be a client, or the harness must leave `internal/` |
| `~/.grok/leader.sock`, `.lock`, `leader.log` | `~/.ask/server.sock`, `server.lock`, `server.log` (D7 names) |
| `GROK_LEADER_SOCKET` / `--leader-socket` | `ASK_SERVER_SOCKET` / `--socket` |
| `use_leader` off by default | `autostart_server` off by default, only if D is built |
| `x.ai/leader/version_mismatch` | first frame carries protocol version; hard reject on mismatch |

### 8.2 Transport for H2-H12 vs the H13 gateway

Grok's local leader never uses a token; it relies on the local socket. Its network side (relay to grok.com) is a separate path. Copy that split: the Unix socket is the local transport and needs filesystem protection, not a token; the H13 gateway (loopback TCP, WS, gRPC) needs the token, the loopback bind and the Origin check. One frame format for both. Do not let the socket become a way around the token for remote clients; it is local by construction.

### 8.3 Options re-evaluated with code evidence

| Option | Code evidence | Verdict |
|---|---|---|
| **C: `cmd/server -p` until H13, then `ask -p` as client** | Matches Grok's own `-p`: embedded, no socket, no lifecycle. Zero extra code. But Grok can do this because the TUI and the agent are one binary; AskCore cannot | Keep for H2 loop work only. It does not satisfy "`ask -p` is the CLI" |
| **B: minimal Unix socket from H2** | Grok's socket core is small: `protocol.rs` framing is about 130 lines, `Register`/`Registered` handshake about 100, the client read/write loops a few hundred. A Go equivalent on `net.Listen("unix")` with NDJSON is about 300-400 lines. The cost is not the wire; it is deciding the frames early | **Rank 1 for the `ask -p` goal.** Needs the H2 test reworded to "no TCP/HTTP/gRPC listener" |
| **D: Grok-style auto-spawn** | The expensive part of Grok. Flock, pid file, stale socket cleanup, spawn, poll, 3 retries, eviction, zombie timer, version floor, slot guard, log rotation, reconnect. `mod.rs` is 2,638 lines, about half of it lifecycle. 26 commits since publication, mostly here. Grok's own default is off | Opt-in, after B, and only a subset (8.4). Never default |
| A: harness in `cmd/tui` | Not used by Grok for the leader path; breaks the import rule | Reject |

Ranking: **B (with C as the H2 dev path), then D subset as opt-in, then A rejected.** This is a rank change from the earlier report only in timing: start B at H2 exit instead of H13. If the user prefers to keep D5 = A (`cmd/server -p` until H13), C stays valid; state that as the user's choice, not a technical necessity.

### 8.4 Copy, fix, skip

Copy:
1. A `Register` first frame within a timeout (30 s), then `Registered{ready, versions, capabilities}`, then `LeaderReady` when startup work (auth, config) is done. Send a structured "starting" error for early requests instead of hanging.
2. Flock on `server.lock` for the single-writer decision, pid inside the file, and the new server (not the client) removes the stale socket after it wins the flock.
3. Losing servers exit with "already running"; clients adopt the winner. Tolerate the short race instead of a perfect lock.
4. `process_group(0)`-style detach, stdin/stdout null, stderr appended to `server.log` with size rotation. In Go: `SysProcAttr{Setpgid: true}` (Setsid is stronger and equally cheap).
5. Session fan-out model: subscribers plus one driver, reverse requests to the driver only, permission prompts shared. Needed once H9-H13 allow two clients.
6. Client disconnect does not cancel the run; busy sessions stay resident.
7. Mark auto-spawned servers with an argv flag so a later cleanup never kills a server a user runs under a supervisor.
8. Refuse auto-spawn when a sandbox is requested, loudly.

Fix (Grok does worse):
1. **Hard version gate at Register.** Grok checks the protocol number only for `Control` and never rejects. AskCore: client sends `protocol`, server replies with its own and closes with an error that says which side must upgrade. Keep `build` as information only.
2. **Explicit permissions:** create `~/.ask` with 0700, the socket with 0600 (set umask around `Listen`, or chmod right after, with the directory already 0700 so there is no window), and reject a non-owner peer via `SO_PEERCRED` (Linux, `x/sys/unix.GetsockoptUcred`) or `LOCAL_PEERCRED` (macOS, `GetsockoptXucred`). Grok has neither.
3. Do not let a client flip `yolo_mode` per connection by default; make permission mode a server decision or require an explicit server-side allow. Grok trusts the client's capability.
4. Do not JSON-encode frames inside strings. Embed typed `json.RawMessage` so there is one decode.
5. Do not log the whole argv as client type (`BIN/main.rs:1443` joins all args). Log a fixed client name.
6. A `shutdown` control frame for `ask server stop`, with SIGTERM via pid file as fallback. Grok only has SIGTERM plus `is_grok_process`.
7. Define behavior for missing pid-file holder on macOS. Grok skips zombie eviction there; AskCore should either skip the feature or use a ping timeout.

Skip:
- Zombie eviction, the acquire-slot guard, the relay, and the auto-update relaunch. Each solved a real incident for Grok (see section 7) but costs lines and needs an install story AskCore does not have.
- Windows named pipes (not stated as a target).
- Length-prefix framing. NDJSON via `json.Decoder` is already the roadmap choice (no line limit) and is simpler in Go; the length prefix is only needed for binary payloads.

### 8.5 Security (H13 token, loopback, Origin)

- Unix socket: owner-only by directory (0700), socket (0600) and peer UID. No token. Same-user malware can already read `~/.ask` data, so a token adds little here. This is an inference and a product choice.
- TCP/WS/gRPC at H13: loopback bind by default, bearer token from a 0600 file, Origin allow-list on WS upgrade, refuse non-loopback bind without the token. Grok gives no code evidence for these, since its leader has no network listener; take them from the roadmap.
- The socket is a full-privilege door (shell tools, credentials in the server). Treat "can connect" as "can run code as the user".

## 9. License (read from `LICENSE`, `Cargo.toml`)

- Apache License 2.0; header "Copyright 2023-2026 SpaceXAI"; `Cargo.toml:120` says `license = "Apache-2.0"`. `THIRD-PARTY-NOTICES` and `third_party/NOTICE` list vendored parts with their own terms.
- Allowed: copy, modify, redistribute code, in source or compiled form, including commercial use, with a patent grant.
- Required: keep the license text and copyright notice, mark changed files, keep any NOTICE content. No trademark right to "Grok", "xAI" or "SpaceXAI" names.
- For AskCore: designing from the behavior described here needs no attribution. A line-by-line Go translation of `lock.rs` or `mod.rs` is a derivative work: add a notice file and a header comment. Pi, the main reference for AskCore, has its own license; check that before mixing code.

## 10. Recommendation (ranked)

1. **Build B at the end of H2 (or start of H3):** `cmd/server` listens on `~/.ask/server.sock`; `cmd/tui` is a thin client with `ask -p "..."` and later the TUI. Hard version gate, 0700/0600, peer UID, NDJSON frames, `Register`/`Registered`/`LeaderReady`, `prompt`, `abort`, event stream with `seq`. If the daemon is not running, print one line: `ask server not running; start it with: ask-server` and exit 2.
2. **Keep `cmd/server -p` as the dev and test path** (Grok's own embedded `-p`); it costs nothing and needs no socket.
3. **Add D as opt-in** (`autostart_server = true` or `--autostart`) after H13, with the subset in 8.4: flock, pid, stale socket removal by the server, detach, log rotation, marker flag, sandbox refusal. Skip zombie, slot and relaunch code until a real incident asks for them.
4. Reject A.

Adoption risk: the wire I recommend is a design copy, not a spec. `LEADER_PROTOCOL_VERSION` has stayed at 1 and compatibility relies on additive fields; AskCore should do the same and keep a hard gate only for breaking changes. Risk of B: frames become public early. Mitigation: the `protocol` field and additive-only rule.

## 11. Limitations

- No leader was run. All behavior is from reading code, not observed. The PTY tests (`leader_pty_e2e`) and `server_tests.rs` were listed but not read.
- Not read: `agent_command::spawn_signal_flush`, `PG/app/leader_cluster/` (a multi-leader cluster used for tests or multi-environment; purpose unclear), the `x.ai/internal` method list beyond `evict_sessions`, what the agent does after `session/cancel`, the `release-dist` remote-settings source.
- macOS socket permission semantics and the macOS zombie gap are noted but not tested.
- Windows path (named pipes) only skimmed.
- The git history is squashed, so the date the leader first appeared upstream is unknown; only the post-2026-07-16 changes are dated.
- The roadmap sections (D5, H2, H13, T1) were taken from the task brief, not re-read.

## 12. Unresolved questions

1. Does the user keep D5 = A (`cmd/server -p` until H13), or start the Unix socket at the end of H2 so `ask -p` exists early? Recommendation: start it early (B).
2. Socket name: `~/.ask/server.sock` (recommended, AskCore has no "non-leader" mode) or `leader.sock`?
3. Should `ask -p` with no running daemon fail with a hint (recommended) or auto-spawn once?
4. Should a connecting client be allowed to set its own permission/yolo mode (Grok does), or only the server config?
5. Is same-user 0600 plus peer UID enough for the local socket, or does the token also apply on the socket?
6. Does the user accept a Go translation that follows `lock.rs` closely, with an Apache-2.0 notice, or prefer a clean-room design from this report?

Status: DONE_WITH_CONCERNS
Summary: The leader model was verified from source (Apache-2.0, Rust, `xai-grok-shell/src/leader/`); `grok -p` never uses the leader, and the earlier report is corrected on 5 points (open source, exit rule, version handshake, wire format, env var). Recommendation: a minimal Unix socket with a hard version gate and explicit 0700/0600 plus peer UID (B), keep `cmd/server -p` for dev, auto-spawn only as a later opt-in subset.
Concerns: GitNexus tools were not available so grep/git were used; no leader was run; cancel routing inside the agent, signal handling and the `leader_cluster` module were not read; git history is squashed.
