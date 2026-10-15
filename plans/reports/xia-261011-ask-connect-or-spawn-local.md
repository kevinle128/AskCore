# Ask Leader Connect/Spawn Path Analysis

**Date**: 2025-10-11  
**Target**: `internal/leader` client-side (`client.go`, `spawn.go`, `lock.go`) + server-side (`cmd/tui/leader.go`)  
**Scope**: ConnectOrSpawn flow, election, locking, replacement, spawn, readiness, hung leader handling, failure surface

---

## 1. ConnectOrSpawn Control Flow

**Entry point**: `leader.ConnectOrSpawn(ctx, cfg)` at `client.go:109-111`

```go
func ConnectOrSpawn(ctx context.Context, cfg ConnectConfig) (*Client, error) {
    return newConnector(cfg).run(ctx, true)  // spawn=true
}
```

### 1.1 Main Loop: `connector.run(ctx, spawn=true)` (client.go:130-195)

**Initialization** (lines 130-141):
- Creates context with timeout (default 20s from `defaultConnectDeadline` line 35; custom deadline via `cfg.Deadline`)
- Calls `EnsureHome(p.Home)` to verify home directory exists and is safe (owner, mode 0700, no symlink)
- Calls `CheckSocketPath(p.Socket)` to verify socket path is safe (same checks)
- Initializes `child *Child` and `replaced bool` flags

**Main retry loop** (lines 142-194):
1. Calls `c.attempt(ctx)` → `(*Client, outcome, error)` (lines 143)
2. Switches on `outcome`:

   **Case `outOK`** (line 145-147):
   - Success: calls `hintBuild(cl)` to warn if leader build differs from client build
   - Returns `(*Client, nil)` — connection complete

   **Case `outFatal`** (line 148-149):
   - Unrecoverable error: peer not a leader, socket owned by another user, home unsafe
   - Returns `(nil, err)` immediately

   **Case `outVersion`** (line 150-159):
   - Protocol version mismatch detected in handshake
   - Proceeds only if `!replaced` (first mismatch only)
   - Calls `c.replace(ctx, err)` to attempt eviction of old leader (see §3)
   - Sets `replaced=true`, clears `child=nil`, continues loop
   - Second mismatch returns error

   **Case `outAbsent`** (line 160-185):
   - Socket not responding (connection refused or timeout)
   - Calls `LockHeld(p)` to check if lock is held (line 161)

     **If lock held** (line 166-167):
     - Another process holds lifetime lock: leader is starting or not answering
     - Action: wait and retry (no inner action)

     **If not held and `!spawn`** (line 168-169):
     - `Connect()` mode: never start a leader
     - Returns `(nil, ErrLeaderAbsent)`

     **If not held and `spawn`** (line 170-184):
     - **Case no child yet** (line 170-174):
       - Calls `c.start(ctx)` to find, probe, and spawn leader process (see §4)
       - On error, returns immediately with wrapped error
       - On success, `child != nil` for next iteration

     - **Case child already started** (line 175-183):
       - Selects without blocking on `child.Exited()`
       - If exited with error: returns with error + log tail (max 2048 bytes)
       - If exited cleanly: leader lost lock race, winner should serve soon
       - If still running: wait

3. **Deadline/poll** (lines 186-194):
   - Selects between `ctx.Done()` and `time.After(pollInterval)` where `pollInterval = 25ms` (line 40)
   - On `ctx.Done()`:
     - If `child != nil` and has error: returns `ErrStartupFailed` + log tail
     - Otherwise: returns `ErrLeaderStarting` with deadline duration
   - On timeout: loop continues

**Deadline enforcement**: The overall context deadline (default 20s) bounds all attempts, registration, readiness, and replacement waits.

---

## 2. Attempt & Handshake: `connector.attempt(ctx)` (client.go:231-262)

Single try to dial and register:

**Step 1: Dial** (lines 232-244):
- Uses `net.Dialer{Timeout: dialTimeout}` where `dialTimeout = 1s` (line 36)
- Context also has overall deadline
- Dials `"unix"` socket at `cfg.Paths.Socket`

**Error classification on dial**:
- `ENOENT`, `ECONNREFUSED`: → `outAbsent` (socket not ready)
- `EACCES`, `EPERM`: → `outFatal` (permission error, not safe to start)
- Timeout or `ctx.Err()`: → `outLive` (leader may be restarting)
- Other: → `outFatal`

**Step 2: Peer check** (lines 245-248):
- Calls `CheckPeer(conn, uint32(os.Geteuid()))` via SO_PEERCRED (Linux) or LOCAL_PEERCRED (macOS)
- Verifies socket is owned by current user
- On mismatch: closes conn, returns `outFatal` with `ErrUnsafeEndpoint`

**Step 3: Register** (lines 249-254):
- Calls `Register(conn, c.cfg.Hello, registerTimeout)` where `registerTimeout = 3s` (line 37)
- Sends `LeaderRegister` frame (client hello) with protocol version and build
- Reads `LeaderRegistered` frame (server hello) with status, ready flag, leader info
- On error: calls `c.classify(err)` to map error to outcome

**Step 4: Readiness** (lines 255-260):
- If `!reg.Info.Ready`:
  - Calls `reg.AwaitReady(minDuration(readyTimeout, remaining(ctx)))`
  - where `readyTimeout = 5s` (line 38)
  - Waits for `LeaderFrameReady` frame from leader
  - Closes conn on timeout, returns `outLive`

**Success** (line 261):
- Returns `(*Client{Registered, conn}, outOK, nil)`

### 2.2 Error classification: `connector.classify(err)` (client.go:265-283)

Maps handshake errors to outcomes:

**RemoteError cases** (protocol-level refusals from leader):
- `LeaderErrVersionMismatch`: → `outVersion` + error (trigger replacement if first)
- `LeaderErrShuttingDown`: → `outLive` (retry, leader is stopping)
- `LeaderErrPeerRejected`: → `outFatal` + `ErrUnsafeEndpoint` (wrong user)
- Other RemoteError: → `outFatal` + `ErrMalformedPeer`

**Network/protocol errors**:
- `io.EOF`, `io.ErrUnexpectedEOF`: → `outLive` (peer closed, may have crashed)
- Timeout, `ECONNRESET`, `EPIPE`: → `outLive` (transient)
- Other: → `outFatal` + `ErrMalformedPeer`

---

## 3. Replacement & Eviction: `connector.replace(ctx, err)` (client.go:288-323)

Called when `outVersion` outcome (protocol mismatch) on **first attempt only**.

**Preconditions**:
- Remote error must have `Upgrade == LeaderUpgradeLeader` (client must upgrade)
- If not, returns immediately with `ErrVersionRefused` + instruction to upgrade client

**Status query** (line 296):
- Calls `c.manage(ctx, LeaderControl{Command: LeaderControlStatus})`
- Uses same socket, but in a separate connection (management-only after version gate)
- Expects `LeaderFrameControlReply` with `LeaderStatus` JSON payload
- On failure: returns error with "could not be asked for its status"

**Eviction check** (lines 300-305):
- Parses `LeaderStatus` JSON
- Checks `!st.SpawnedByClient` (leader must have been started by a client)
- If false: leader runs under supervisor, refuses replacement
- Returns `ErrVersionRefused` + "restart it with the new binary"

**Shutdown request** (line 307):
- Sends `LeaderControl{Command: LeaderControlShutdown, IfIdle: true, InstanceID: st.InstanceID}`
- `IfIdle=true` means leader refuses if busy (any active runs or forwarded requests)
- On failure: returns `ErrVersionRefused` + "old leader is busy"

**Lock release wait** (lines 312-322):
- Polls `LockHeld(c.cfg.Paths)` and `!socketAnswers(c.cfg.Paths)` until both true
- Uses `time.After(pollInterval)` where `pollInterval = 25ms`
- On `ctx.Done()`: returns `ErrLeaderStarting` + "did not release its lock in time"
- Returns `nil` on success (lock free, socket dead)

**No second attempt**: after replacement, the main loop continues with `replaced=true`, so a second mismatch returns error.

---

## 4. Spawn: `connector.start(ctx)` → `StartLeader(cfg)` (client.go:197-206, spawn.go:263-291)

### 4.1 Find executable: `connector.leaderExecutable()` (client.go:213-220)

Chooses which binary to run:

**If `CallerIsAsk && CallerExe != ""` and CallerExe still exists** (lines 214-217):
- Use `CallerExe` directly (absolute path after `checkExecutable`)
- Allows `ask` TUI or `go run ./cmd/tui` to start leader from same binary
- Handles case where binary name is not "ask" (e.g., `go run` creates temp binary)

**Otherwise: FindAsk** (line 219):
- Looks for `ask` binary next to `CallerExe`, then on PATH (via `cfg.LookPath` or `exec.LookPath`)
- Called for non-`ask` programs (e.g., ask-server) to find `ask` binary
- Returns error if not found

### 4.2 Probe: `ProbeAsk(ctx, exe, wantProtocol)` (spawn.go:207-230)

Verifies binary is compatible before starting:

**Execution** (lines 207-218):
- Runs `<exe> version --json` with `probeTimeout = 15s` (line 37)
- Context deadline also applies (can be shorter)
- `Setpgid=true`: creates new process group
- `cmd.Cancel`: kills entire group with `SIGKILL` if timeout
- `cmd.WaitDelay = 1s`: wait for child cleanup before parent cleanup
- Limits output to `probeMaxOutput = 64 KiB` (line 39)

**Verification** (lines 219-229):
- Parses JSON response as `VersionInfo`
- Checks `Name == "ask"`
- Checks `LeaderProtocol == wantProtocol`
- On mismatch: returns `ErrAskIncompatible` with upgrade instruction

### 4.3 Start leader process: `StartLeader(cfg)` (spawn.go:263-291)

Spawns `ask leader --spawned-by-client` in a new session:

**Setup** (lines 267-279):
- Ensures home safe (same as client)
- Opens log file with `OpenLog(cfg.Paths.Home)` (mode 0600, appending)
- Filters environment: removes any existing `ASK_HOME`, appends `ASK_HOME=<home>`
- Creates command: `<executable> leader --spawned-by-client`
- Sets working directory to home
- Redirects `stderr` to log file
- **Setsid=true**: leader runs in new session (survives if parent dies)

**Process start** (lines 282-284):
- Calls `cmd.Start()` (non-blocking)
- On error: returns wrapped error immediately

**Reaping goroutine** (lines 286-289):
- Creates `Child` struct with unbuffered `exited` channel
- Spawns goroutine that waits for exit via `cmd.Wait()` and closes `exited`
- Caller can poll `Child.Exited()` without blocking

**Return**: `(*Child, nil)` immediately (process started, but not necessarily running yet)

---

## 5. Readiness: How client waits for leader (client.go:255-260, leader.go:289-314)

### 5.1 Client side: AwaitReady (in handshake after Register)

**After successful `Register`**:
- If `Info.Ready == false`:
  - Calls `reg.AwaitReady(minDuration(readyTimeout, remaining(ctx)))`
  - Reads frames from leader until `LeaderFrameReady` arrives
  - Timeout: `readyTimeout = 5s` (line 38), capped by overall deadline
  - On timeout: closes conn, returns `outLive` (poll and retry)

### 5.2 Server side: Leader startup sequence (leader.go:204-314)

**serveLeader** (lines 204-275):
1. Ensures home (EnsureHome, CheckSocketPath)
2. **Acquires lock**: `AcquireWait(paths, acquireWait, func() bool { return leader.Serving(paths) })`
   - `acquireWait = 2s` (line 48)
   - Waits up to 2s for other process to release LOCK_EX
   - Returns `ErrLeaderRunning` if locked AND `Serving()` checks socket answering
   - Lock holder writes PID to lock file

3. If spawned (`o.spawned`):
   - **Log rotation**: `RotateLog(paths, leaderLogMax)` if log > `8 << 20` bytes (8 MB)
   - **Stderr redirect**: `OpenLog()` and `RedirectStderr()` to append to log

4. **Removes stale socket**: `lock.RemoveStaleSocket()`
   - Checks if socket path exists and no process listens
   - If socket listens: returns `ErrSocketInUse` (another leader won, unexpected)
   - If not: removes socket file (clears orphaned socket from crash)

5. **serveLeaderRuntime** (lines 289-314):
   - **Listen**: `listenPrivate(paths.Socket)` creates Unix socket with mode 0600
     - Opens with `Umask(0o177)` to prevent race where another user creates socket
     - Sets mode `0600` explicitly after creation
     - Returns `identity` (FileInfo of socket inode for later cleanup verification)
   
   - **Composes app**: creates DI graph with `app.LeaderModule`, gets `*app.LeaderRuntime`
   - **Writes owner**: `lock.WriteOwner(owner)` stores PID, start time, instance ID, executable path
   - **Logs startup**: prints "ask leader: listening on <socket>"
   - **Serves**: calls `rt.Serve(ctx, ln)` with context and listener
     - During Serve: leader accepts clients, receives `LeaderRegister`, responds `LeaderRegistered`
     - Sends `LeaderFrameReady` when ready (agent initialized)
   
   - **On exit** (defers):
     - `lock.RemoveSocket(identity)`: removes socket only if inode matches, prevents removal of replacement
     - `lock.Release()`: unlocks, clears PID, closes lock file

**Client waits for**:
- Lock acquisition (2s), then startup work, then `Serve()` to respond to Register and send Ready frame
- If leader doesn't respond to Register within `registerTimeout = 3s`: classified as `outLive`, retry
- If not Ready within `readyTimeout = 5s`: classified as `outLive`, retry

---

## 6. Hung Leader Handling

### 6.1 What is "hung"?

A leader that:
- Accepts connection (socket listening) but
- Doesn't respond to `LeaderRegister` frame within `registerTimeout = 3s`, or
- Doesn't send `LeaderFrameReady` within `readyTimeout = 5s`

### 6.2 Detection & recovery

**Dial timeout** (line 36):
- `dialTimeout = 1s`: if leader accepts connections but is slow, detected here
- On timeout: classified as `outLive`, retry (poll 25ms, repeat)

**Register timeout** (line 37):
- `registerTimeout = 3s`: if leader reads frame but is hung processing it
- On timeout/EOF: classified as `outLive`, retry

**Ready timeout** (line 38):
- `readyTimeout = 5s`: if leader starts but app initialization is hung
- On timeout: classified as `outLive`, retry

**Overall deadline** (default 20s):
- Bounds all attempts + registration + readiness + replacement waits
- If deadline exceeded: returns `ErrLeaderStarting`

**Polling strategy**:
- `pollInterval = 25ms` (line 40): retries every 25ms
- Max retries in 20s: ~800 attempts
- Cost: low CPU (sleep-based), can detect hung leader eventually

### 6.3 Early-exit hung process

If a leader process starts but exits immediately (e.g., constructor error):

**Detection** (lines 177-183):
- Selects on `child.Exited()` after starting
- If closed and `child.Err() != nil`: process crashed
- Returns `ErrStartupFailed` + log tail (last 2048 bytes)

**Log tail shown to user** (lines 179, 189):
- Calls `LogTail(p, 2048)` to read end of leader log
- User sees error from leader initialization (e.g., config invalid, port in use)
- Enables diagnosis without spawning another process

**No auto-retry of crashed leader**:
- Process failed; caller must decide (TUI falls back to in-process agent)

---

## 7. Election & Locking: Lifetime lock and Owner record

### 7.1 Lock file: `Acquire` (lock.go:40-68)

**File**: `~/.ask/leader.lock` (or `${ASK_HOME}/leader.lock`)

**Safety**:
- Opened with `openPrivateAt` (owner 0600, current user only)
- Never removed (inode stable across boots)
- Exclusive lock (`LOCK_EX | LOCK_NB`): only one process holds at a time

**Acquire semantics**:
1. Opens lock file (or creates if missing)
2. Attempts non-blocking exclusive lock
3. On success: holds lock, returns `*Lock`
4. On `EWOULDBLOCK`/`EAGAIN`: returns `ErrLeaderRunning` (another process locked)
5. Other errors: fatal (permission, I/O, etc.)

### 7.2 Lock held check: `LockHeld(p)` (spawn.go:51-69)

Used by client to detect if leader is starting:

**Semantics**:
1. Opens lock file (or returns false if not exist)
2. Attempts shared lock (`LOCK_SH | LOCK_NB`): returns immediately
3. On success: lock is free, returns `false`, unlocks and closes
4. On `EWOULDBLOCK`/`EAGAIN`: exclusive holder exists, returns `true`
5. Other errors: fatal

**Note**: Only an instant snapshot. Holder might release immediately after check.

### 7.3 Owner record: `WriteOwner` (lock.go:84-97)

Written by leader after winning lock and before serving:

**Content** (lines 94):
```
PID
Start (Unix seconds)
Instance (e.g., "a1b2c3d4")
Exe (absolute path of leader executable)
```

**When written** (leader.go:306):
- After `AcquireWait` succeeds
- Before `Serve` (while holding lock)
- Updated each startup (PID, Start, Instance change; Exe stable or empty)

**When read** (from client Stop command and tests):
- Via `ReadOwner(p)` (lock.go:123-153)
- Parsed as `Owner` struct
- Diagnostic only: not used for ownership decisions (lock file is)

**Verification** (spawn.go:310-327):
- `VerifyLeaderProcess(o)` checks:
  - `PID` and `Start` time match process info from `/proc`
  - Command line is `ask leader` or `<exe> leader`
  - User owns process (UID matches current user)
  - Start time tolerance: `startTolerance = 10s` (line 43)
- Used by Stop command to verify PID before sending SIGTERM fallback (Linux only, via pidfd)

### 7.4 Socket ownership & cleanup

**Socket file** (`~/.ask/leader.sock`):
- Created by leader with mode 0600, owner current user
- Lock holder removes it on shutdown (lock.go:200-223 `RemoveSocket`)
- **Before serving**: lock holder removes stale socket if no process listens (lock.go:164-198 `RemoveStaleSocket`)
- **After serving**: lock holder removes socket only if inode matches (prevents race with replacement)

---

## 8. Replacement: Version-based eviction

Triggered when `outVersion` outcome (line 150).

**Constraints**:
- Only leaders started by clients can be replaced (`SpawnedByClient == true`)
- Only when leader is idle (no active runs, no pending forwarded requests)
- Newer clients replacing older leaders only

**Sequence**:
1. **Status query** (line 296): `manage(ctx, Status)`
   - Connect to socket, send management control frame
   - Leadership accepts register from old client who didn't match version
   - Responds with JSON status

2. **Checks** (lines 300-310):
   - `SpawnedByClient`: must be true (not supervised)
   - `Idle`: old leader must accept `IfIdle=true` shutdown
   - If busy: returns error "the old leader is busy, run `ask leader stop` when its work is done"

3. **Shutdown signal** (line 307):
   - Sends `LeaderControlShutdown` with `IfIdle=true` and `InstanceID`
   - Leader accepts only if all sessions idle (no running prompts)
   - If rejected: returns error (caller can still try Stop or signal)

4. **Wait for release** (lines 312-322):
   - Polls `LockHeld()` and `!socketAnswers()` until both true
   - Timeout: overall deadline (e.g., 20s)
   - Returns error if deadline exceeded

5. **Second mismatch rejected** (line 152):
   - If version mismatch again after replacement: returns error (do not recurse)

---

## 9. Error surface: returned errors and fallback

### 9.1 ConnectOrSpawn errors

**ErrLeaderAbsent** (line 20):
- No leader running AND `spawn=false` (Connect mode)
- Caller: TUI falls back to in-process agent (tui_client.go:72-82)

**ErrLeaderStarting** (line 22):
- Deadline exceeded waiting for leader to start or respond
- Includes deadline duration in message
- Caller: TUI shows error and falls back to in-process agent

**ErrUnsafeEndpoint** (line 24):
- Home, socket, or peer failed safety check (permissions, ownership, symlink)
- No leader started (safety error is never recovered by starting new leader)
- Caller: TUI shows error and falls back to in-process agent

**ErrMalformedPeer** (line 26):
- Socket responded but not as a leader (unknown protocol, parse error)
- No leader started (not a real leader, unsafe to start another)
- Caller: TUI shows error and falls back to in-process agent

**ErrStartupFailed** (line 28):
- Leader process started but exited with error before serving
- Includes process exit error and log tail (last 2048 bytes)
- Caller: TUI shows error + log tail, falls back to in-process agent

**ErrVersionRefused** (line 31):
- Protocol mismatch: leader or client must upgrade
- Includes message naming the side that must upgrade
- Does NOT start new leader (version mismatch is not resolved by spawning)
- Caller: TUI shows error, falls back to in-process agent

### 9.2 Server-side errors

**Leader startup**:
- Failure during lock acquire: returns exit code 1
- Failure during socket creation: returns exit code 1
- Failure during app composition: returns exit code 1
- All logged to stderr and leader log

**Signal handling**:
- SIGTERM: stops serving, cleans up socket and lock, exit code 143
- SIGINT: same, exit code 130
- Second signal during cleanup: forced exit, exit code 130/143 (without draining)

### 9.3 Stop command errors

**leader.Stop** (client.go:442-482):

**Paths**:
1. **Control shutdown** (line 454-456):
   - `Status()` to get instance ID
   - `requestShutdown()` with that instance
   - On success: `WasRunning=true`, `Signalled=false`

2. **Fallback signal** (Linux only, lines 469-477):
   - `ReadOwner()` to get PID and identity
   - `signalLeaderProcess()` with pidfd:
     - `PidfdOpen()` for stable handle
     - `VerifyLeaderProcess()` checks identity
     - Sends SIGTERM
   - On success: `Signalled=true`
   - On macOS: refuses to signal (no stable handle)

3. **Errors**:
   - `ErrLeaderAbsent`: no leader running
   - `ErrVersionRefused`: leader refused shutdown (busy, different version, supervised)
   - `ErrLeaderStarting`: timeout waiting for lock/socket to be free
   - `ErrUnsafeEndpoint`: home/lock unsafe

### 9.4 TUI fallback (tui_client.go:53-82)

```go
c, err = tuiclient.Open(ctx, tuiclient.Config{
    Dial: tuiclient.LeaderDial(leader.ConnectConfig{...}),
    ...
})
if err != nil {
    reason = err  // save ConnectOrSpawn error
    c, ierr = openInProcessClient(ctx, deps)  // run agent in process
    if ierr != nil {
        return nil, fmt.Errorf("leader: %v; in-process agent: %w", reason, ierr)
    }
    return &openedClient{Client: c, FallbackReason: reason}, nil
}
return &openedClient{Client: c}, nil
```

**Behavior**:
- If ConnectOrSpawn succeeds: use leader client
- If fails at startup: try in-process agent
- Footer shows `in-process` instead of `leader <id>` when running in-process
- If leader is later lost: TUI reconnects with exponential backoff, does NOT fall back to in-process

---

## 10. Key invariants & implementation notes

### 10.1 Lock file inode stability
- Lock file is never removed (lock.go:28-30)
- Same inode persists across boots, leader starts/stops, and socket cleanup
- Enables reliable stale-socket detection via inode match

### 10.2 Socket cleanup with inode check
- Leader records socket identity (FileInfo) at startup (leader.go:296)
- On shutdown, removes only that exact inode (lock.go:200-223)
- Prevents race: if leader crashed and restart socket created a new inode, old leader shutdown won't remove it

### 10.3 Spawn coordination: Exclusive lock winner
- First lock holder wins the race
- Losers fail to acquire lock (AcquireWait returns ErrLeaderRunning)
- All clients poll socket until winner accepts connections
- No need for elaborate election protocol: flock is atomic

### 10.4 Dial timeout vs. context timeout
- Dial uses `dialTimeout = 1s`
- Overall context has larger deadline (default 20s)
- Each retry loop uses `pollInterval = 25ms`
- If dial timeout expires, context still has time for retry

### 10.5 Handshake version gate
- Client sends protocol version in `LeaderRegister`
- Server checks version in `Accept` (handshake.go)
- On mismatch: server responds with `LeaderErrVersionMismatch` and sends `RegistrationRefused` (does NOT close connection)
- Mismatched connection can still send/receive control frames (Status, Shutdown) for replacement
- Prevents dead-end: old client connecting to new server can ask new server to stop

### 10.6 Replacement waits for lock, not socket
- After shutdown request accepted, client polls both `LockHeld()` and `socketAnswers()`
- Requires BOTH to be false to proceed
- Ensures lock holder (old leader) has fully released and stopped
- New leader can then acquire lock and start serving

### 10.7 Probe before start
- Client runs `ask version --json` before spawning leader
- Verifies binary is compatible and executable
- On probe failure: returns error immediately (no spawn)
- On success: spawns with confidence binary is runnable

### 10.8 Newt session (Setsid=true)
- Leader runs in new session: `SysProcAttr.Setsid = true` (spawn.go:281)
- Survives if client dies (different process group)
- Reaping goroutine in client (spawn.go:286-289) waits but doesn't keep process alive

### 10.9 Log tail on failure
- If spawned leader exits with error: last 2048 bytes of log appended to error message
- Caller sees reason (e.g., "ASK_FAUX_TPS: strconv.Atoi: parsing \"fast\": invalid syntax")
- Enables diagnosis without spawning another process to tail log

### 10.10 Instance ID for leader identity
- Generated at startup (cmd/tui/leader.go:390-395): 8 random bytes, hex-encoded
- Stored in owner record (lock file) and reported in status
- Used to match shutdown commands to current instance (prevents confusion if restarted)
- Fallback if rand fails: `pidXXXX` format

---

## 11. Unresolved questions

- Does `ProbeAsk` detect hung binaries (e.g., an `ask` binary that hangs in `version --json`)? Yes, via `probeTimeout = 15s` + `cmd.Cancel` with process group kill.
- If leader is writing to log when `RotateLog` is called, does rotation cause data loss? No, rotation is called while holding lock and after leader won lock, so leader is either running (log open by leader, append by both works) or stopped (safe to rotate).
- What if two clients simultaneously try to replace different versions of the same old leader? The first replacement to acquire management connection wins the status query; the second sees the lock and socket free (after first's replacement), then spawns. Both eventually reach same winner.
- Is there a race between socket cleanup (unlock.go) and new leader socket creation? No, because new leader acquires lock first (exclusive), so old leader's cleanup defer runs only after new leader is ready (holds lock and has open socket).

---

## Summary of timings

| Component | Timeout | Purpose |
|-----------|---------|---------|
| Dial | 1 s | Accept connection |
| Register | 3 s | Handshake exchange |
| Ready | 5 s | Startup completion |
| Probe | 15 s | Binary compatibility check |
| Overall deadline | 20 s (default) | Entire ConnectOrSpawn |
| Lock acquire wait | 2 s | Spawn startup competition |
| Poll interval | 25 ms | Retry frequency |
| Start tolerance | 10 s | Process start time matching |
| Log tail size | 2048 B | Error context on failure |

