---
phase: 1
title: "Transport and security foundation"
status: completed
priority: P1
effort: "2d"
dependencies: []
---


## Current repair state (2026-10-09)

The original descriptor and socket-cleanup gaps are repaired.
[paths.go](../../internal/leader/paths.go) opens checked private directories and uses descriptor-relative artifact opens.
[lock.go](../../internal/leader/lock.go) retains the directory handle and checks socket identity before removal while the flock is held.
[the command](../../cmd/tui/leader.go) disables automatic Unix-listener unlink.
Replacement-socket and held-directory checks are in [lock_test.go](../../internal/leader/lock_test.go) and [leader_regression_test.go](../../cmd/tui/leader_regression_test.go).
The SDK source patch is selected by the relative `replace` in [go.mod](../../go.mod); [PATCH.txt](../../third_party/acp-go-sdk/PATCH.txt) owns its provenance.
The full Linux leader test binary passed in `alpine:3` after repair.
The final exit gates passed; the [repair report](../reports/pm-261009-h13b-repairs.md) records their evidence and accepted limits.

# Phase 1: Transport and security foundation

## Goal

Build the leader's wire, identity and filesystem safety pieces as tested, router-free units, and close the route-context spike with a recorded go or fallback decision.

## Context

- Scope: roadmap H13b row and security rules; architecture 7.3 "Security of the local socket" and "Leader lifecycle".
- No router, no spawn, no app wiring in this phase.
  Nothing here is reachable from a binary yet, so built-binary evidence starts in Phase 4.
- Settings already uses raw `syscall.Flock` with `O_NOFOLLOW` (`internal/settings/lock_unix.go`) and an owner/type/mode check (`internal/settings/paths.go` `ensureHome`, `checkDirectory`, `checkEntry`).
  Copy the rules; do not export or edit `internal/settings`.
- `golang.org/x/sys v0.47.0` is already in `go.mod` as indirect and already imported by `cmd/tui/auth_input.go`.
  It provides `unix.GetsockoptXucred` (darwin) and `unix.GetsockoptUcred` (linux).
- The coder ACP SDK carries request ids as `*json.RawMessage` and handles `$/cancel_request` with a raw `requestId`, so a string id produced by a rewrite is valid and the rewrite must also cover `requestId`.

## Files to Create / Modify

- Create: `pkg/protocol/leader.go`, `pkg/protocol/leader_test.go`
- Modify: `pkg/protocol/acp.go` (live method names, codes `-32015..-32016`, `ACPRouteMeta` type and key constant)
- Create: `internal/leader/frame.go`, `internal/leader/frame_test.go`
- Create: `internal/leader/handshake.go`, `internal/leader/handshake_test.go`
- Create: `internal/leader/ids.go`, `internal/leader/ids_test.go`
- Create: `internal/leader/paths.go`, `internal/leader/paths_test.go`
- Create: `internal/leader/lock.go`, `internal/leader/lock_unix.go`, `internal/leader/lock_test.go`
- Create: `internal/leader/peer_darwin.go`, `internal/leader/peer_linux.go`, `internal/leader/peer_other.go`, `internal/leader/peer_test.go`
- Create (test only, spike): `internal/acp/route_meta_test.go`
- Modify: `.golangci.yml` (new `leader-router-boundary` rule)
- Modify: `go.mod` (only `golang.org/x/sys` moves to the direct block)

No other `internal/acp` file changes in this phase.

## Design

### Protocol types (`pkg/protocol/leader.go`)

- `LeaderProtocolVersion = 1`.
- `LeaderFrame{Type string `json:"type"`; Payload json.RawMessage `json:"payload,omitempty"`}`.
  ACP payloads stay raw nested JSON, never a double-escaped string.
- `LeaderRegister`, `LeaderRegistered`, `LeaderStatus`, `LeaderError{Kind, Message, Upgrade string}` as named in `plan.md`.
- `LeaderMaxFrame = 64 << 20` (as Grok `protocol.rs:9`).
- `ACPRouteMeta` holds `ClientID string`, `DriverGen uint64`, `LiveDriver bool` and `Capabilities json.RawMessage`.
  Its generation field uses the tag `json:"driverGen,string"`.
  `ACPRouteMetaKey = "ask.dev/route"` belongs in `acp.go`.

### Frame codec (`frame.go`)

- `Reader` wraps one `io.Reader`; `Next() (LeaderFrame, error)` uses `io.ReadFull` for the 4-byte big-endian length, then the body.
  One goroutine owns a reader, so a partial frame is never abandoned.
  A read error ends the connection; parsing never restarts mid-frame.
- Reject before allocation: length 0, length > `LeaderMaxFrame`.
  Reject after read: invalid JSON envelope, unknown type, JSON-RPC batch (array) inside `acp`.
- `Writer.Write(LeaderFrame) error` encodes length plus body in one `Write` call and returns `io.ErrShortWrite` for a short count without an error.
  Reject an encoded envelope above `LeaderMaxFrame` before writing any byte, in both directions.
  The single-writer rule is the caller's (Phase 2 per-client writer).
- `LineReader` and `LineWriter` for the agent-link NDJSON side: 65 MiB without newline, with explicit checks before writes.
  Compact each raw JSON message to one physical line; a valid multiline socket payload must not become several SDK messages.

### Handshake (`handshake.go`)

- Server side: `Accept(conn, cfg) (ClientHello, error)`.
  Order: peer credential check, then read exactly one `register` frame under a deadline, then version gate, then write `registered`.
  No ACP frame is read before `registered` is written.
- Version gate: `ProtocolVersion != cfg.ProtocolVersion` gives `leader_version_mismatch` with `Upgrade: "client"` when the client is lower and `"leader"` when higher, then refuse ACP; close unless a bounded management request follows.
- A mismatch connection remains management-only for a bounded wait: only `status` and conditional idle `shutdown` are allowed, never ACP.
  This is the same version-stable control schema used by replacement in Phase 4.
- Client side: `Register(conn, hello, timeout) (LeaderRegistered, error)` and `AwaitReady(r, timeout)` for `leader_ready` when `Ready` was false.
- `cfg.ProtocolVersion` is typed config with default `protocol.LeaderProtocolVersion`.
  This is the supplemental skew mechanism; no CLI flag.

### Id rewrite (`ids.go`)

- `Table` per leader.
  `Forward(clientID string, msg json.RawMessage) (out json.RawMessage, route Route, err error)`:
  - Decode only `jsonrpc`, `id`, `method`, `params` as `json.RawMessage`.
  Never decode an id into `float64`.
  - A message with `method` and `id` is a request: internal id = JSON string `"<clientID>:<seq>"` where `seq` is a leader counter, never derived from client text.
  Store `internalID → {clientID, originalRawID, method}`.
  - Duplicate active original id from the same client is rejected (`-32600`).
  Equal ids from different clients are allowed.
  - `$/cancel_request`: rewrite `params.requestId` only through the sender's own pending entries; an unknown id is dropped, never forwarded, so A can never cancel B.
  - Reject boolean, array and object ids.
  Keep the exact response spelling; use a canonical comparison key for duplicate detection and cancellation (`1` and `1e0`, or `"a"` and `"\u0061"`, name the same id).
  - A message without `method` is a reverse response; it is not rewritten here (Phase 2 reverse table).
- `Restore(msg) (clientID string, out json.RawMessage, ok bool)` puts the exact original raw id bytes back and deletes the entry.
- `DropClient(clientID)` ends response delivery for that client.
  Keep the small cleanup records needed to process late new-session, take and Follow results until those results arrive; Phase 2 owns this lifecycle.
  Other late responses are discarded and logged.
- `"id": null` on a message with `method` is rejected with `-32600` (the SDK keys `null` as a request id).
- `Restore` returns the exact original id bytes.
  Other bytes of a forwarded message are not promised byte-exact, because Phase 2 injects route meta into `params`.

### Paths (`paths.go`)

- `Paths{Home, Socket, Lock, PID, Log string}` resolved from one home value.
  App passes the same `ASK_HOME` that native auth uses; default `~/.ask`.
- `EnsureHome(home)`: parent is a real directory; home is a directory, not a symlink, owned by the effective UID, mode forced to 0700.
- `CheckSocketPath(p)`: return a clear error when `len(p) >= 104` on darwin or `>= 108` on linux (`sun_path`).
- `OpenPrivate(path, flags)`: open with `O_NOFOLLOW`, then `Fstat` the descriptor: regular file, owner UID, mode 0600 (chmod if wider).
  Used for lock, PID and log files.
  Do not truncate, append or chmod before owner and type checks pass.
  Open in nonblocking mode for the type check, so a FIFO cannot hold startup.
  Use a private directory descriptor for artifact operations and verify socket identity before cleanup.

### Lock (`lock.go`, `lock_unix.go`)

- `Acquire(paths) (*Lock, error)`: open lock file with `OpenPrivate`, non-blocking exclusive flock.
  Busy gives `ErrLeaderRunning`.
- Owner-only actions: `WritePID(pid)`, `RemoveStaleSocket()` (only if `Lstat` shows a socket owned by the UID and a dial fails; never removes a regular file or a live endpoint).
- `Release()`: truncate PID metadata, unlock, close.
  Never unlink the lock file, so the inode stays stable and two independent lock inodes cannot exist.

### Peer credentials (`peer_*.go`)

- `peerUID(conn *net.UnixConn) (uint32, error)`: darwin `unix.GetsockoptXucred(fd, SOL_LOCAL, LOCAL_PEERCRED)`, linux `unix.GetsockoptUcred(fd, SOL_SOCKET, SO_PEERCRED)`.
  Use `SyscallConn().Control`.
- `peer_other.go` (`//go:build !darwin && !linux`): returns `ErrPeerCheckUnsupported`; the server refuses to start.
  No silent skip.
- `CheckPeer(conn, ownerUID)`: mismatch gives `leader_peer_rejected` and close before any frame is read.
  `ownerUID` comes from typed config, default `os.Geteuid()`.

### Depguard (`.golangci.yml`)

Add `leader-router-boundary` (strict list mode) for `**/internal/leader/**`: allow `$gostd`, `AskCore/pkg/protocol`, `AskCore/internal/logs`, `golang.org/x/sys/unix`.
This denies `internal/agent`, `internal/acp`, `internal/config`, gateway, http and channels by construction.

### Route-context spike (exit item, test only)

Question: does the SDK deliver a router-injected `_meta[ask.dev/route]` to the `sdk.Agent` methods for `session/new`, `session/prompt`, `session/cancel` and the `_ask/session/*` extension requests, over one link after one link-level `initialize`, without per-client `Initialize` state?

The SDK declares `Meta map[string]any` on `NewSessionRequest`, `PromptRequest` and `CancelNotification`; extension methods get raw params (`internal/acp/ask_methods.go`).
Today the adapter ignores `req.Meta` (`internal/acp/agent.go` `NewSession`, `Prompt`, `Cancel`), so the spike proves delivery, not consumption.
Real consumption is proven in Phase 3 (`TestAdapterRouteMetaCapabilitiesPerSession`).

- Write `internal/acp/route_meta_test.go`: a recording `sdk.Agent` wrapper that embeds a real `*Adapter`, records the meta of each call, then forwards.
  Drive it through a real `AgentSideConnection` over pipes with one link-level `initialize`, then requests that carry two different route metas.
  Assert each recorded meta matches its sender and the adapter results are unchanged.
- Files it may touch: that test file only.
  No product change in `internal/acp` in this phase.
- Record the result in this phase's "Spike result" section: **go** (Phase 3 adds typed consumption) or **fallback** (one adapter per client around a shared host).
  A fallback stops the plan for revision before Phase 2.

## TDD

### Tests before (RED)

Each test must fail for the right reason (missing symbol or wrong behavior), not a timeout.

| Test | File | Asserts |
|---|---|---|
| `TestFrameRoundTrip` | `frame_test.go` | register/acp frames survive encode/decode byte-exact for nested raw ACP |
| `TestFrameRejectsOversize` | `frame_test.go` | length `LeaderMaxFrame+1` rejected before allocation |
| `TestFrameAcceptsMaxFrame` | `frame_test.go` | a frame of exactly `LeaderMaxFrame` is accepted |
| `TestFrameRejectsBatchAndMalformed` | `frame_test.go` | array payload, bad JSON, unknown type rejected; `ping`, `pong`, `disconnect` accepted |
| `TestFramePartialReadNoResync` | `frame_test.go` | reader on a slow `io.Pipe` returns the full frame; a mid-frame error ends the reader |
| `FuzzFrameReader` | `frame_test.go` | no panic, no allocation above the bound |
| `TestHandshakeAccepts` | `handshake_test.go` | real Unix socket pair; `registered` carries client id, instance id, build, version |
| `TestHandshakeRejectsVersion` | `handshake_test.go` | lower and higher client version give `leader_version_mismatch` with the correct `Upgrade` side; no ACP frame read |
| `TestHandshakeDeadline` | `handshake_test.go` | silent client closed after the register deadline |
| `TestIDRoundTripExact` | `ids_test.go` | ids `7`, `"7"`, `9007199254740993`, `"a:b|c"`, `"é"`, `-0`, `1e3` restore byte-exact; `id: null` request rejected |
| `TestIDEqualAcrossClients` | `ids_test.go` | A and B both send id 1; responses route to the right client |
| `TestIDDuplicateActiveRejected` | `ids_test.go` | second active id 1 from A rejected |
| `TestCancelRequestScoped` | `ids_test.go` | A's `$/cancel_request` rewrites its own id; A naming B's original or internal id is dropped |
| `TestIDDropClient` | `ids_test.go` | after drop, a late response is discarded |
| `TestEnsureHomeModes` | `paths_test.go` | creates 0700; tightens 0755 to 0700; refuses symlink and foreign type |
| `TestOpenPrivateRefusesSymlink` | `paths_test.go` | symlinked lock/log path refused; wider mode tightened to 0600 |
| `TestSocketPathTooLong` | `paths_test.go` | over-length path gives the clear error |
| `TestLockContentionGoroutines` | `lock_test.go` | second `Acquire` gives `ErrLeaderRunning` |
| `TestLockContentionChildProcess` | `lock_test.go` | a child process (re-exec of the test binary as a helper, not a product flag) holds the lock; parent gets busy |
| `TestLockInodeStableAcrossRestart` | `lock_test.go` | inode equal before acquire, after release, after re-acquire |
| `TestStaleSocketRemovedOnlyByOwner` | `lock_test.go` | owner removes a dead socket; refuses a regular file and a live listener |
| `TestPeerAcceptsOwner` | `peer_test.go` | real socket, default owner UID accepted |
| `TestPeerRejectsOtherUID` | `peer_test.go` | real socket, real syscall, injected owner UID = euid+1 → `leader_peer_rejected` before any frame |
| `TestLeaderFrameJSONNames` | `pkg/protocol/leader_test.go` | wire field and type names exactly as listed in `plan.md` |
| `TestRouteMetaReachesAdapter` | `internal/acp/route_meta_test.go` | SDK delivers per-request route meta to `sdk.Agent` methods after one link-level initialize |

### Green

Implement the minimum in each file to pass its tests, in this order: protocol types, frame, ids, paths, lock, peer, handshake, depguard, spike.

### Tests after

- `go test -race ./internal/leader/... ./pkg/protocol/... -count=1` with `goleak.VerifyTestMain` in `internal/leader`.
- Depguard negative control: temporarily add `import _ "AskCore/internal/acp"` to a scratch file in `internal/leader`, confirm lint fails, remove it.
  Record the output.
- Linux build of peer code: `GOOS=linux go vet ./internal/leader/...` and, where Docker is available, `docker run --rm -v "$PWD":/src -w /src golang:1.x go test ./internal/leader/ -run 'Peer|Lock|Paths'`.
  If Docker is unavailable, record it as a gap.
- `go build ./... && go vet ./...`; existing `go test ./internal/acp/... ./cmd/tui/...` still pass (no product change there).

### Manual check (documented, not a gate)

As root on macOS: `sudo nc -U <socket>` against a test leader socket must be rejected with `leader_peer_rejected` in the log.
Record the result in the phase review section.

## Tasks & Steps

1. [x] Add protocol types and `TestLeaderFrameJSONNames` (RED → GREEN).
2. [x] Frame codec tests, then implementation; add fuzz seed corpus.
3. [x] Id table tests, then implementation.
4. [x] Path and private-open tests, then implementation.
5. [x] Lock tests (goroutine, child process, inode, stale socket), then implementation.
6. [x] Peer credential tests on darwin; implementation for darwin, linux, other.
7. [x] Handshake tests on a real socket, then implementation.
8. [x] Depguard rule plus negative control.
9. [x] `go.mod`: move only `golang.org/x/sys` to direct; confirm `go build ./...`.
  Do not bundle other tidy drift.
10. [x] Route-context spike; write the result below.
11. [x] Run tests-after commands; record outputs.

## Verification

```sh
go test ./pkg/protocol/... ./internal/leader/... -count=1
go test -race ./internal/leader/... -count=1
go test ./internal/acp/ -run TestRouteMetaReachesAdapter -count=1 -v
GOOS=linux go vet ./internal/leader/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
go build ./... && go vet ./...
```

## Exit criteria

- All RED tests pass; race and goleak clean.
- Depguard rule present with a recorded negative control.
- Route-context spike result recorded as go or fallback.
- No change to `internal/acp` product code, `internal/settings`, `cmd/tui`, or `internal/app`.

## Risks

| Risk | Mitigation |
|---|---|
| macOS `sun_path` limit breaks long homes | `CheckSocketPath` with clear error; tests use `/tmp/askl-XXXX` |
| Float id rounding | Raw-byte rewrite only; large-integer test |
| Lock unlink creates two inodes | Never unlink; inode test |
| Linux peer code untested on macOS host | `GOOS=linux go vet`; Docker run when available; gap recorded otherwise |
| Spike fails | Stop and revise plan before Phase 2 |

## Spike result

**Go.** `TestRouteMetaReachesAdapter` (`internal/acp/route_meta_test.go`) drives a real `AgentSideConnection` over pipes with one link-level `initialize`. It sends `session/new` (twice), `session/prompt`, `session/cancel` and `_ask/session/state` with two different route metas. A recording `sdk.Agent` wrapper around a real `*Adapter` sees each meta exactly as sent, including a `driverGen` of `9007199254740993` that stays exact as decimal text. The adapter results are unchanged (two independent sessions, `end_turn`). This proves SDK delivery only. The adapter still ignores `req.Meta`; Phase 3 proves consumption with `TestAdapterRouteMetaCapabilitiesPerSession`.

## Historical review

### Evidence (2026-10-08)

| Check | Result |
|---|---|
| `go test -race ./internal/leader/... ./pkg/protocol/... -count=1` | pass (goleak on the whole leader package) |
| `go test -race ./internal/acp/... ./internal/app/... -count=1` | pass |
| `go test ./cmd/tui/... -count=1` | pass (61 s); editor and headless unchanged |
| `GOOS=linux go vet` and `GOOS=freebsd go vet ./internal/leader/...` | pass |
| Linux test binary of `internal/leader` run in an `alpine:3` arm64 container (root) | pass: peer check with the real `SO_PEERCRED` call, lock, paths, handshake |
| `golangci-lint run ./...` | 0 issues |
| Depguard negative control | a scratch file importing `AskCore/internal/acp` in `internal/leader` fails with `not allowed from list 'leader-router-boundary'`; the file was removed |
| `go vet ./...` | pass |
| `go mod tidy -diff` | only the existing `go-oidc` drift remains; `x/sys` moved to the direct block |

### Changes from the design text

- `Paths` has no `PID` field. The PID lives in the lock file, as architecture 7.3 says. `ReadPID(paths)` reads it.
- No `lock_unix.go`. `internal/leader` is unix-only through `syscall`, like `internal/settings`, so one `lock.go` holds the flock code.
- `EnsureHome` keeps the strict parent rule of `internal/settings` (the parent must be a real directory, not a symlink). A probe confirmed that settings refuses `ASK_HOME=/tmp/x` on macOS and accepts `/tmp/<real directory>/x`, so the leader and native auth now share one policy. Tests build their home under `filepath.EvalSymlinks` of a `/tmp` directory (`/private/tmp/...` on macOS).
- `FrameReader` reads the body with `io.ReadAll(io.LimitReader)` after the size check. A peer that claims 64 MiB and sends nothing cannot make the leader allocate 64 MiB. `TestFrameHostileLengthDoesNotPreallocate` covers it.
- `ServerConfig.OwnerUID` is a pointer; nil means the current user, so UID 0 stays a valid owner.
- `Accept(conn, cfg, clientID)` returns reader and writer for the caller. After a version mismatch it returns a management-only connection (`Accepted.Management`); `NextManagement` allows only `status` and `shutdown` control frames and refuses ACP.
- `Accept` returns the management-only connection together with `ErrVersionMismatch`, so a caller cannot take a mismatch for success.
- `IDTable.Forward` refuses a client id outside letters, digits, `-` and `_` (`ErrInvalidClientID`). `Restore` finds the echoed id by exact bytes, and a character such as `<` would change under JSON escaping.
- `LineReader` returns a copy of each line. A first version returned the reuse buffer, and a test showed a held line turning into the next one.
- `LeaderControl`, `LeaderControlStatus` and `LeaderControlShutdown` were added to `pkg/protocol` for that management path.

### Gaps and open gates

- The manual root check (`sudo nc -U`) was not run: `sudo` is interactive. The automated tests run the real syscall with an injected owner UID, as decided.
- **SDK parser gate (blocks Phase 3, not Phase 1):** the installed SDK `receive` scanner stops at 10 MiB. The leader codecs already accept 64 MiB frames and 65 MiB internal lines, but the common ACP connection needs a pinned SDK source patch before app integration. Run `artifacts/verify-sdk-frame-limit.py`, then record provenance, license and update procedure.
- Frame-writer tests at exactly 64 MiB run in the normal suite (about 3 s). Line tests run at the real `MaxLine` (65 MiB) with the newline at the boundary.
- Socket replacement during cleanup had no executed test at this checkpoint.
  The current repair state above supersedes this gap.
- Private directory descriptors were omitted at this checkpoint.
  This did not meet the original requirement.
  The current repair state above closes the omission.
- **Not run:** `go build ./...` (a local command hook blocks the word). `go vet ./...`, `go test -c` and the `cmd/tui` test suite, which builds the binary, cover compilation.

## SDK parser gate before app integration

The installed `github.com/coder/acp-go-sdk` v0.13.5 uses a 10 MiB scanner buffer in `connection.go` `receive`.
The user confirmed 64 MiB support on 2026-10-08.
Increasing `LineLimitReader` alone does not fix the parser.
Run `python3 plans/261008-1033-h13b-leader-unix-socket/artifacts/verify-sdk-frame-limit.py` from the repository root.
The probe changes only the scanner maximum in a temporary copy, to 65 MiB plus one byte for the newline.
It verifies the old failure and the proposed buffer change without changing generated files or the module cache.
Before Phase 3, pin a reviewed SDK source patch or an upstream release with this behavior and record its provenance, license and update procedure.
Never use a machine-local module-cache edit or an absolute-path `replace` as the production fix.
Keep editor stdio's 8 MiB input guard.
Re-run H13a conformance against the selected source.

Add focused tests for short writes, multiline ACP compaction, invalid id types, equivalent id spellings, private-open FIFO rejection, and socket replacement during cleanup.
Add frame tests at 64 MiB minus one, exactly 64 MiB, and 64 MiB plus one for the complete envelope, including JSON escaping overhead.
The internal line tests must include metadata expansion and a newline at the scanner boundary.

Add writer-boundary tests for exactly 64 MiB and 64 MiB plus one after full envelope encoding.
Add a router test for an oversized outgoing response after id restoration: only its recipient closes; another client and the common link remain usable.
