# Source Analysis: `connect_or_spawn` Control Flow & Leader Election

**Analysis Date:** 2026-10-11  
**Repository:** grok-build (commit 2bdd1d6a, Apache-2.0)  
**Target:** `/crates/codegen/xai-grok-shell/src/leader/mod.rs:1376-1566`  
**Scope:** `connect_or_spawn`, all helpers, election/lock mechanics, eviction, spawn, readiness, errors

---

## 1. Control Flow: `connect_or_spawn` (lines 1376–1566)

**Entry Point:** `pub async fn connect_or_spawn(client_type, mode, env_urls, capabilities)`

### 1.1 Sandbox Check (lines 1382–1384)
- **Early Gate:** If sandbox confinement is requested, return `ConnectionError::SandboxConfinement(profile)` immediately.
- **no fallback:** this error is terminal (verified by `is_terminal_refusal` at line 1332).

### 1.2 Fast Path: Existing Socket (lines 1385–1419)
**Condition:** `listener_is_ready(&sock_path)` returns `true` (Unix: `path.exists()`; Windows: named-pipe probe).

1. **PID Liveness Check (lines 1390–1401):**
   - Read PID from lock file via `lock.read_pid()` (lock.rs:207–209).
   - If PID exists and is alive (via `crate::util::is_process_alive`): attempt connection.
   - If PID dead or missing: skip connect (socket is stale); fall through to lock acquisition.

2. **Connect Attempt (lines 1403–1418):**
   - Call `connect_to_leader(sock_path, client_type, mode, capabilities)` → `LeaderClient::connect()` (line 1693).
   - **Success case (line 1404–1410):**
     - Call `should_evict_conn(conn)` (line 1405; see §2.3).
     - If eviction NOT needed: log "Adopted leader" + elapsed time, return `Ok(conn)`.
     - **Inference:** Handoff to leader does NOT write any lock state; socket/lock remain owned by leader.
   - **Eviction case (line 1412–1413):**
     - Call `drop(conn)` to disconnect.
     - Set `replacing_stale = true` (used for telemetry at line 1491–1500).
     - Fall through to lock acquisition loop.
   - **Connection Failure (line 1415–1417):**
     - Log debug and continue to lock acquisition loop (no immediate return).

### 1.3 Lock Acquisition Loop (lines 1421–1566)
**Loop Entry:** Initialize `zombie_timer = None`, `evict_attempts = None`, `self_spawn_attempts = 0`.

#### Branch 1: Lock Successfully Acquired (`try_acquire() → Ok(true)`, lines 1426–1502)

**Race Condition Check (lines 1427–1457):**
- While holding the lock, check if **another sibling** spawned a leader:
  - Socket ready AND PID alive AND connection succeeds.
  - If no eviction needed: release lock, log "Adopted sibling-spawned leader after eviction race" + telemetry, return `Ok(conn)`.
  - If eviction needed: call `evict_leader(conn, &lock)` (line 1456; see §3.2).
- **No early socket here:** The acquiring process is responsible for spawning, not hand-off.

**Spawn Path (lines 1459–1502):**
1. **Release lock before spawn (lines 1460–1462):** Unlock but keep `was_leader = true` so `Drop` cleans up on exit.
2. **Spawn subprocess (line 1463):** Call `spawn_leader_subprocess(env_urls)` (lines 1626–1684; see §4.1).
   - Returns PID on success, `ConnectionError::SpawnFailed` on error.
   - **Process group:** Unix: `process_group(0)` (line 1667); Windows: `CREATE_NEW_PROCESS_GROUP` (line 1673).
   - **Reaping:** Spawned in a background thread (line 1681–1683) without blocking.
3. **Wait for socket & connect (lines 1464–1471):**
   - Call `wait_for_socket_connectable(sock_path, client_type, mode, capabilities)` (lines 1699–1722; see §5).
   - **Timeout Loop (lines 1472–1485):**
     - On `ConnectionError::Timeout`: increment `self_spawn_attempts`; if >= `MAX_SELF_SPAWN_ATTEMPTS` (3), return error.
     - Otherwise: continue loop (re-acquire lock and retry).
   - **Other errors:** return immediately.
4. **Success (lines 1489–1502):**
   - If `replacing_stale = true`: log telemetry "leader.spawn.replacement" (version_floor reason).
   - Return `Ok(conn)`.

#### Branch 2: Lock Held by Another (`try_acquire() → Ok(false)`, lines 1504–1506)
- Log debug "Lock held by another process, probing socket connectability".
- Fall through to `wait_for_socket_connectable` (line 1515).

#### Branch 3: Lock Acquisition In Progress (`try_acquire() → Err(AcquireInProgress)`, lines 1507–1509)
- **Early return:** `ConnectionError::Lock(LockError::AcquireInProgress { path, holder_pid })`.
- **Inference:** Another process is stuck trying to acquire; do not spawn yourself.
- Verified by comment at line 1508: "not spawning a leader".

#### Branch 4: Other Lock Errors (lines 1511–1512)
- Convert to `ConnectionError` via `From` trait and return.

### 1.4 Wait for Socket in Holding Loop (lines 1515–1565)
**Executed after Branch 2 (another holds lock) and Branch 1 timeout retries.**

1. **Connection attempt (lines 1515–1530):**
   - Call `wait_for_socket_connectable(...)`.
   - **Success (line 1517–1524):**
     - Clear `zombie_timer = None`.
     - If no eviction needed: return `Ok(conn)`.
     - If eviction needed: call `request_leader_vacate(conn, lock.read_pid())` (line 1526; graceful request only, no wait), drop conn, set `replacing_stale = true`, sleep `SPAWN_POLL_INTERVAL` (100ms), continue loop.

2. **Connection-level failure handling (lines 1532–1562):**
   - **Condition:** `is_connect_level_failure(&e)` (line 1325) = `Timeout` or `Client(Connect(_, ...))` (connection refused, not registered).
   - **Get live lock holder (line 1533):** Call `live_grok_lock_holder(&lock)` (lines 1224–1227):
     - Read file PID, confirm via `/proc/locks` (Linux only; macOS/BSD return `None`).
     - Verify PID is grok process (strict name-match check).
     - Return `None` if unconfirmable or not grok.
   - **Zombie decision (lines 1534–1539):**
     - Call `zombie_evict_decision(holder, now, ZOMBIE_EVICT_DEADLINE, &mut zombie_timer)` (lines 1195–1219):
       - **First sighting:** arm timer `(pid, now)`, return `ZombieAction::Wait`.
       - **Same PID after deadline (30s):** return `ZombieAction::Evict { pid, waited }`.
       - **PID change or no holder:** reset/clear timer, return `ZombieAction::Clear` or `ZombieAction::Wait`.
   - **Evict action (lines 1540–1552):**
     - Call `register_evict_attempt(&mut evict_attempts, pid, MAX_ZOMBIE_EVICT_ATTEMPTS)` (lines 1316–1322): allows 3 attempts per PID, resets on PID change.
     - If budget exhausted: return `ConnectionError::SpawnFailed("zombie leader pid {pid} could not be evicted after {MAX_ZOMBIE_EVICT_ATTEMPTS} attempts")`.
     - Otherwise: call `evict_zombie_leader(pid, &sock_path, waited)` (lines 1337–1371; see §3.3), continue loop.
   - **Wait action (lines 1554–1556):** Log debug, continue loop.
   - **Clear action (lines 1558–1560):** Log debug, continue loop (may hit timeout next iteration).

3. **Protocol/registration errors (line 1564):** Return immediately (no zombie recovery).

---

## 2. Election & Locking

### 2.1 Lock File Structure (lock.rs:106–242)

**LeaderLock struct (lines 112–120):**
- `lock_path: PathBuf` – path to flock file (default: `~/.grok/leader[SUFFIX].lock`).
- `sock_path: PathBuf` – path to Unix socket (default: `~/.grok/leader[SUFFIX].sock`).
- `lock_file: Option<LockedFile>` – file handle with acquired flock.
- `was_leader: bool` – set on acquisition; cleared by `release()`. Controls Drop cleanup.

**Derivation of paths (lock.rs:54–85):**
- WS URL hash suffix (lines 15–34): MD5-style hash for non-production URLs; empty for default.
- Lock path = `grok_home / "leader{SUFFIX}.lock"`.
- Socket path = `grok_home / "leader{SUFFIX}.sock"`.
- Override via `GROK_LEADER_SOCKET` env var (lines 36–46): exact socket path, lock is sibling `.lock`.

### 2.2 Lock Acquisition & PID Recording (lock.rs:151–204)

**`try_acquire()` (lines 155–166):**
- Calls `xai_grok_file_lock::lock_file(path, options)` with acquire slot (lines 156–158).
- **Returns:**
  - `Ok(true)`: acquired exclusive lock.
  - `Ok(false)`: lock contended (`LockError::Contended`).
  - `Err(LockError::AcquireInProgress { ... })`: sibling wedged inside slot; caller must not spawn.
  - `Err(...)`: other errors (filesystem, permissions).

**`write_pid()` (lines 197–204):**
- Truncate lock file to 0 bytes (lines 199).
- Write decimal PID string (line 200).
- Fsync (line 201).
- **Called by:** leader process at agent/app.rs:728, 748 (after successful lock acquisition).

**`read_pid()` (lines 207–209):**
- Parse lock file contents as u32 (lines 207–217).
- Returns `None` if missing, unreadable, or not an integer.

### 2.3 Version-Based Eviction Decision (lines 103–119)

**`should_evict(leader_version: Option<&str>, client_version: &str) -> bool` (lines 117–119):**
- Compare semver: return `leader < client` (lines 106–112).
- **Rule:** Client is ALWAYS >= leader (never downgrade).
  - Same version: keep.
  - Newer client + older leader: evict leader, spawn new one.
  - Newer client + newer leader: keep (anti-thrash).
  - Unparseable (e.g., "unknown", dev builds): keep (safe default).
  - Missing: keep (legacy fallback).

**`should_evict_conn(conn: &LeaderConnection) -> bool` (lines 1090–1094):**
- Extract `leader_binary_version` from registration.
- Call `should_evict()`.

**`CLIENT_LEADER_VERSION` (line 97):** `xai_grok_version::VERSION` (cargo-stamped or `0.0.0` in fastbuild).

---

## 3. Eviction

### 3.1 Stale Leader Eviction (Request-Driven, lines 1099–1140)

**`request_leader_vacate(conn: &LeaderConnection, pid: Option<u32>)` (async, lines 1099–1140):**

1. **Relaunch-capable (lines 1101–1116):**
   - If leader supports relaunch: send `ControlCommand::RelaunchForUpdate { to_version: CLIENT_LEADER_VERSION }`.
   - Outcomes: "accepted" (acknowledged), "declined" (refused), "send_failed" (error).
   - **Behavior:** Best-effort; does NOT wait for exit. Leader re-checks version floor and exits if needed.

2. **Fallback (lines 1117–1129):**
   - If relaunch unsupported or leader protocol too old: SIGTERM via `crate::util::kill_process_by_pid(pid)`.
   - Outcomes: "signaled", "signal_failed", or `None` (no PID recorded).

3. **Telemetry (lines 1130–1140):**
   - Log "leader.evict.vacate_requested" with method, outcome, PID, versions, elapsed.

### 3.2 Stale Leader Eviction (Lock-Holding Eviction, lines 1145–1179)

**`evict_leader(conn: LeaderConnection, lock: &LeaderLock)` (async, lines 1145–1179):**
- **Precondition:** Caller holds the flock (acquired at line 1426).
- **Steps:**
  1. Call `request_leader_vacate(...)` (line 1148).
  2. Drop connection (line 1149).
  3. **Grace period (lines 1152–1159):**
     - Wait up to `EVICT_WAIT_TIMEOUT` (8s) for PID to exit.
     - If still alive and SIGTERM failed: re-send SIGTERM (line 1155).
  4. **Extended wait (lines 1159–1164):**
     - If still alive after second grace: wait again, then classify outcome.
  5. **Telemetry (lines 1169–1179):**
     - Log "leader.evict.completed" with outcome ("exited", "timed_out", "resigned_sigterm"), PID, versions, total waited time.
- **Behavior:** Serialized under the flock, so the spawner reclaims the socket next.

### 3.3 Zombie Eviction (Unresponsive Lock-Holder, lines 1335–1371)

**`evict_zombie_leader(pid: u32, sock_path: &Path, waited: Duration)` (async, lines 1335–1371):**
- **Trigger:** PID holds flock but socket is not connectable for >= 30s (ZOMBIE_EVICT_DEADLINE).
- **Steps:**
  1. **SIGTERM (lines 1344–1346):** Try to kill with SIGTERM; log warning on error.
  2. **Wait grace (line 1347):** Wait `EVICT_WAIT_TIMEOUT` (8s) for exit.
  3. **SIGKILL escalation (lines 1350–1359):**
     - If still alive: try SIGKILL.
     - On SIGKILL error: log warning, outcome = "sigkill_failed".
     - On success: wait again, then classify outcome ("sigkilled", "survived_sigkill").
  4. **Telemetry (lines 1361–1371):**
     - Log "leader.zombie.evicted" with zombie_pid, socket_path, outcome, client_version, waited_ms.
- **No respawn:** Eviction only removes the obstacle; the caller loops to reacquire lock and spawn.

### 3.4 Zombie Detection State Machine (lines 1182–1219)

**ZombieTimer (line 1182):** `Option<(u32, Instant)>` – PID and first-sighting timestamp.

**`zombie_evict_decision(holder, now, deadline, timer) -> ZombieAction` (lines 1195–1219):**

| Holder | Timer State | Action |
|--------|-------------|--------|
| `None` | `Some(_)` or `None` | `Clear`, reset timer to `None` |
| `Some(pid1)` | `None` | `Wait`, arm timer with `(pid1, now)` |
| `Some(pid1)` | `Some((pid1, since))`, waited < deadline | `Wait`, keep timer |
| `Some(pid1)` | `Some((pid1, since))`, waited >= deadline | `Evict { pid1, waited }`, clear timer |
| `Some(pid1)` | `Some((pid2, _))`, pid1 ≠ pid2 | `Wait`, re-key timer to `(pid1, now)` |

**Design:** PID-keyed so a timer accrued against an old zombie cannot evict a fresh leader.

---

## 4. Spawn Path

### 4.1 Binary Resolution (lines 1571–1607)

**`resolve_exe_for_spawn() -> Result<PathBuf>` (lines 1571–1572):**
- Calls `resolve_binary_with_home(grok_home())`.

**`resolve_binary_impl(grok_home, current_exe) -> Result<PathBuf>` (lines 1582–1601):**

1. **Managed install check (lines 1586–1591):**
   - If `current_exe` is under `grok_home` AND `~/.grok/bin/grok[.exe]` exists: use managed symlink.
   - **Rationale:** After `grok update`, the symlink points to the fresh binary; `current_exe()` still resolves to the old version.

2. **Fallback to current_exe (lines 1593–1594):**
   - If available, use it (out-of-tree or dev builds).

3. **Fallback to managed bin (lines 1596–1597):**
   - If managed binary exists, use it.

4. **Error (lines 1599–1601):**
   - Return `ConnectionError::SpawnFailed("could not determine binary path for leader spawn")`.

### 4.2 Subprocess Invocation (lines 1626–1684)

**`spawn_leader_subprocess(env_urls: &LeaderEnvUrls) -> Result<u32, ConnectionError>` (lines 1626–1684):**

**Binary & Arguments (lines 1627–1632):**
- Binary: `resolve_exe_for_spawn()`.
- Args: `["agent", "leader", "--no-exit-on-disconnect", "--relay-on-demand", "--grok-ws-url", "{ws_url}", "--grok-ws-origin", "{origin}"]`.

**Environment Variables (lines 1634–1645):**
- Inherit `GROK_LEADER_SOCKET` (if set).
- Inherit sampling/instrumentation flags: `GROK_DEBUG_LOG`, `GROK_HOOKS_LOG`, `GROK_LOG_SAMPLING`, `GROK_INSTRUMENTATION`.
- **RUST_LOG:** Priority: `GROK_LEADER_LOG` > `RUST_LOG` > fallback (lines 1660–1662).
  - Fallback (line 1625): `"xai_grok_shell=info,xai_grok_gateway=info,xai_grok_config=info,xai_grok_cloud_config=info,xai_grok_agent_config=info,xai_grok_external_agent_migration=info,xai_grok_login=info,xai_acp_lib=warn,xai_grok_mcp=warn"`.

**I/O Redirection (lines 1647–1657):**
- stdin: `/dev/null` (or Windows equivalent).
- stdout: `/dev/null`.
- stderr: leader log file (rotated on > 32 MB), or `/dev/null` on error.

**Log File (lines 1649–1657):**
- Path: `~/.grok/leader.log`.
- Rotation: if > 32 MB, rename to `.log.1` and truncate (lines 1612–1616).
- Append mode (no truncation on spawn).

**Process Group (lines 1664–1673):**
- Unix: `process_group(0)` (child's own process group).
- Windows: `CREATE_NEW_PROCESS_GROUP` flag.
- **Effect:** Signals to the group do not cascade to the parent.

**Spawn & Reaping (lines 1676–1683):**
- Spawn child process (line 1677).
- Extract PID (line 1679).
- Spawn background thread to reap child (lines 1681–1683); no blocking wait in the async context.

**Return:** PID on success, `ConnectionError::SpawnFailed(msg)` on spawn failure.

---

## 5. Readiness & Connection

### 5.1 Socket Readiness Check (transport.rs:15–23)

**`listener_is_ready(path: &Path) -> bool` (lines 15–23):**
- **Unix:** `path.exists()` (stat syscall).
- **Windows:** `WaitNamedPipeW(pipe_name, 1ms)` (probe without connecting); `true` if exists, `false` if `ERROR_FILE_NOT_FOUND`.
- **Used:** Fast path check (line 1389), zombie loop (line 1427), wait loop (line 1708).

### 5.2 Wait for Socket Connectable (lines 1699–1722)

**`wait_for_socket_connectable(sock_path, client_type, mode, capabilities) -> Result<LeaderConnection>` (lines 1699–1722):**

1. **Deadline:** `tokio::time::Instant::now() + SPAWN_WAIT_TIMEOUT` (10s, line 1705).
2. **Poll loop (lines 1707–1718):**
   - Check `listener_is_ready(sock_path)`.
   - If true: call `connect_to_leader(...)`.
     - On success: return immediately.
     - On error: log debug, save error, sleep `SPAWN_POLL_INTERVAL` (100ms), retry.
   - If false: sleep `SPAWN_POLL_INTERVAL`, retry.
3. **Timeout (lines 1719–1721):**
   - On deadline exceeded: return last error or `ConnectionError::Timeout`.

### 5.3 Connect to Leader (lines 1686–1694)

**`connect_to_leader(sock_path, client_type, mode, capabilities) -> Result<LeaderConnection>` (async, lines 1686–1694):**
- Calls `LeaderClient::connect(...)` (client.rs).
- Wraps `LeaderClient` in `LeaderConnection`.
- Returns `ConnectionError::Client(...)` on failure (registration timeout, protocol error, etc.).

---

## 6. Failure Surface & Caller Handling

### 6.1 Error Variants (mod.rs:808–829)

```rust
pub enum ConnectionError {
    Lock(LockError),  // from xai_grok_file_lock
    Client(ClientError),  // from LeaderClient
    Server(ServerError),
    SpawnFailed(String),
    Timeout,  // socket poll timeout
    Cancelled,  // caller cancelled
    SandboxConfinement(&'static str),  // terminal refusal
}
```

**Classification:**
- **Connect-level failures (lines 1325–1329):** `Timeout`, `Client(Connect(...))` → drive zombie eviction.
- **Terminal refusals (lines 1332–1333):** `SandboxConfinement(...)` → no retry, fail immediately.
- **Others:** surface to caller (registration error, connection closed, spawn error, lock error).

### 6.2 Caller Usage (LeaderReconnector, lines 960–1040)

**`reconnect()` (async, lines 975–991):**
- Calls `reconnect_with(policy, cancel, || connect_or_spawn(...))`.
- Returns `(tx, rx, disconnect_reason_rx)` on success.
- On error: publishes `ConnectionStatus::Failed { error }` and returns error to caller.

**Reconnect policies:**
- **Unbounded:** infinite retries (TUI mode, user expects persistence).
- **Bounded:** up to 5 attempts with exponential backoff (headless/`-p` mode).

**Backoff:** Base 1s, max 30s, per attempt with jitter.

---

## 7. Known Gaps & Design Notes

### 7.1 Hung Leader Without Eviction (lines 2033–2056)

**Test:** `connect_to_hung_leader_times_out_with_no_fallback` (line 2036).

**Comment (line 2034):** "Today there is no eviction/respawn fallback on this path, so every client adopting the hung leader parks and then errors."

**Scenario:** Leader accepts connection but never sends `Registered` (hangs mid-protocol).

**Behavior:**
- `connect_to_leader()` waits for registration response for `REGISTRATION_RESPONSE_TIMEOUT`.
- On timeout: returns `ClientError::Timeout(REGISTRATION_RESPONSE_TIMEOUT)`.
- In `wait_for_socket_connectable` loop (line 1515): this is a registration error (not connect-level), so no zombie eviction.
- Result: Client times out, no respawn attempted.

**Impact:** Every client adopting the hung leader will park and error in sequence. No recovery without external intervention (killing the hung leader).

### 7.2 Reconnect Hung Leader (lines 2057–2100)

**Test:** `reconnect_against_hung_leader_exhausts_attempts_without_respawn` (line 2060).

**Behavior:** Reconnector does NOT evict or respawn hung leader; it exhausts bounded attempts and publishes `Failed`.

---

## 8. Timeouts & Constants (Summary)

| Constant | Value | Use |
|----------|-------|-----|
| `SPAWN_WAIT_TIMEOUT` | 10s | max time to poll socket for readiness |
| `SPAWN_POLL_INTERVAL` | 100ms | polling frequency |
| `ZOMBIE_EVICT_DEADLINE` | 30s | max unconnectable time before eviction |
| `EVICT_WAIT_TIMEOUT` | 8s | grace period for process to exit |
| `MAX_ZOMBIE_EVICT_ATTEMPTS` | 3 | max auto-kill attempts per PID |
| `MAX_SELF_SPAWN_ATTEMPTS` | 3 | max spawn/bind-failure retries |
| `CLIENT_LEADER_VERSION` | cargo version | version floor for eviction |
| `RECONNECT_BASE_DELAY` | 1s | initial backoff |
| `RECONNECT_MAX_DELAY` | 30s | max backoff |
| `RECONNECT_MAX_ATTEMPTS_BOUNDED` | 5 | headless retry limit |

---

## 9. Test Coverage

**Key behaviors verified:**
- Zombie decision state machine (lines 1750–1813): clear, arm, wait, evict, rekey on PID change.
- Evictability gates (lines 1815–1859): `/proc/locks` parsing, file PID vs flock holder match, stale PID handling.
- Version floor (lines 2105–2173): newer/older/dev/missing versions evicted or kept correctly.
- Connection timeouts (client.rs:524–607): registration timeout, readiness timeout, partial frame handling.
- Socket & spawn behavior (lines 2198–2636): real server spawn and connection, PID alive checks.

**Gaps noted:**
- No test for hung leader eviction from `connect_or_spawn` (documented as missing at line 2034).
- No test for reconnect fallback to respawn (documented as missing at line 2058).

---

## Status: DONE

**Confidence Level:** High (95%) – verified by source, tests, and traced flow.

**Key Claims Verified:**
- Lock path: mod.rs:1387, lock.rs:69-85 → socket/lock derivation from WS URL hash.
- PID write on leader side: agent/app.rs:728, 748 → write_pid() after lock acquisition.
- Eviction decision: lines 117-119 → semver comparison, keep on unparseable.
- Zombie timeout: 30s (line 102), eviction escalation: SIGTERM + 8s, then SIGKILL + 8s (lines 1344–1359).
- Spawn: process group isolation (lines 1667, 1673), stderr to rotated log (lines 1612–1621), reaping in background thread (lines 1681–1683).
- Readiness: Unix stat, Windows named-pipe probe (transport.rs:15–23).
- Socket poll timeout: 10s (line 91), interval: 100ms (line 92).
- Spawn retries: 3 attempts, per-PID zombie eviction budget: 3 (lines 1309, 1313).
- No fallback for hung leader without lock (test comment line 2034).

