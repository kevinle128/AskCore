# H13b requirements evidence (leader over the Unix socket)

Date: 2026-10-08. Role: evidence researcher for `ak plan --deep --tdd`. Scope mode: **HOLD SCOPE** (no `--yagni`).
This report changes no plan, source, or roadmap file. The Xia report was treated as evidence to verify, not authority.
Citation form: `path:line`. `XIA` = `plans/reports/xia-261008-0828-grok-leader-h13b-port.md`. `RM` = `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`. `ARCH` = `docs/ask-architecture-reference.md`.

## 1. Verified facts

### 1.1 Outcome and requested scope

- H13b is "Leader over the Unix socket": `internal/leader` with handshake and hard version gate, id rewrite, per-session subscribers and driver, `ConnectOrSpawn`, flock, pid, log, 0700/0600 permissions, and peer-UID check. Exit: "two `ask` TUI stubs share one auto-started leader" (RM:480).
- H13b waits on H13a (RM:470). Order is T0 → H13a → H13b → T1a (RM:26, RM:107). H13a is completed (`plans/261007-0700-h13a-acp-stdio/plan.md:5`, acceptance all checked at plan.md:97-103).
- The leader is "a router, not a second agent": handshake `register` → `registered` → `leader_ready`; client-id prefixing of request ids; response routing with id restore; `session/update` to session subscribers; permission/question requests to all subscribers with first answer winning; other reverse requests only to the driver (ARCH:254-259).
- A disconnecting client does not cancel the run; a reconnecting client asks for events after its last `seq` (ARCH:261; RM:514 "a client disconnect does not cancel the run").
- Leader tests required by the roadmap (RM:514): two TUIs share one leader and see the same session updates; two clients with different cwd and capabilities create different sessions; driver disconnects during a question; `ask-server` starts first from another cwd and with `ask` missing from `PATH`; version mismatch while another client runs a tool; stale reused pid is not signalled; ids of two clients never collide; different protocol version rejected; other-UID peer rejected; stale socket replaced by the flock winner; client disconnect does not cancel the run.
- Session/driver rules (RM:484-489): one agent holds many sessions; creator or explicit taker is driver, others are subscribers; `/new` and switch act on the caller's view only; a switch on a busy session the caller does not drive is rejected; the multiplexer keeps each client's `initialize` state (capabilities, cwd) and passes only the driver's capabilities for that session; reverse calls go only to eligible clients, and a driver disconnect during a question cancels it while late or duplicate answers are ignored.
- Spawn rules (RM:490): `ConnectOrSpawn` must start `ask leader` even when `ask-server` calls it; look for `ask` next to its own executable, then on `PATH`; check version before spawn; clear failure when absent or incompatible; bounded connect and readiness waits including a live lock holder with no usable socket.
- Version mismatch (RM:491; ARCH:276): reject by default with an upgrade hint; auto-restart of a client-spawned leader only after a verified idle shutdown handshake and lock release; verify the pid belongs to an `ask leader` process before any signal.
- Lifecycle (ARCH:263-268): one function `ConnectOrSpawn`; flock on `~/.ask/leader.lock` holds the pid; the winner removes a stale socket; loser exits and the client uses the winner; `Setsid`, stdin/stdout to `/dev/null`, stderr appended to a size-rotated `~/.ask/leader.log`; spawned leader gets `--spawned-by-client`; `ask leader list | status | stop` with `shutdown` control frame first and SIGTERM through the pid file as fallback.
- Not copied from Grok: zombie eviction, acquire-slot guard, relay, auto-update relaunch (ARCH:270).
- Socket security (ARCH:272-277): `~/.ask` 0700, socket 0600, reject peer whose UID is not the owner (`SO_PEERCRED` Linux, `LOCAL_PEERCRED` macOS); local socket needs no token.
- Package contract: `internal/leader` files `server.go`, `client.go`, `lock.go`, `spawn.go`; takes the agent's ACP stream as an `io.ReadWriteCloser` built by `internal/app`; allowed imports stdlib, `pkg/protocol`, `logs`; denied `internal/agent`, `internal/acp`, gateway, http, channels, config; "A client that disconnects does not cancel the run" (`internal/leader/README.md:5-9,22-35,40-42`). Depguard denies core packages importing `acp`/`leader` (`.golangci.yml:124-127`). A leader-specific deny of agent/acp imports is not yet in `.golangci.yml` (grep shows `internal/leader` only in the TUI-boundary rule at :37 and core rule at :126).
- Command mapping: `ask leader` = `AgentModule` + ACP adapter + `LeaderServerModule`; `ask` TUI = remote `AgentClient`; headless `ask -p` direct; `cmd/server` is later a leader client (ARCH:295-296).

### 1.2 Non-goals and ownership boundaries

- H13c owns the network gateway, token/loopback/Origin rules and daemon conversion (RM:481, RM:493-496).
- H8 owns durable load, H10 owns compaction; owners incomplete return explicit unsupported (RM:474-475; plan H13a plan.md:15-16).
- T1a owns the real chat UI; H13b needs client stubs only (RM:673-690). T1a requires no silent direct fallback on its acceptance path (RM:677-678).
- Headless (`ask -p`) and editor `ask acp` keep running without a leader or listener (ARCH:238, 251; RM:482).
- No SQL migration or new dependency is implied; `golang.org/x/sys v0.47.0` is already present as an indirect dependency (`go.mod:394`), `gofrs/flock` is indirect only (`go.mod:199`), and settings already uses `syscall.Flock` with `O_NOFOLLOW` (`internal/settings/lock_unix.go:14,23`).

### 1.3 Current code state (baseline commit `213afbe`)

- `internal/leader` contains only `README.md` and `doc.go`; no server, client, lock or spawn code exists.
- `cmd/tui` has `acp.go` (`ask acp` registered before prompt parsing, `cmd/tui/acp.go:19-106`) and an old demo menu `runInteractive` (`cmd/tui/main.go:100-125`). No `leader` dispatch and no version probe exist (grep of `cmd/tui/*.go` for `leader|version` returns only the ACP `Implementation` version string "dev" at `acp.go:82`).
- `NewAdapter` builds its own host (`internal/acp/agent.go:90-101`); `Bind` attaches one SDK connection once via `bindOnce` (`agent.go:109-130`).
- `Adapter.Close` disposes every session and closes output (`agent.go:163-178`). `Adapter.Fail` calls `Host.latch`, which sets a failure, closes `failed`, and cancels every session run (`agent.go:142`; `host.go:122-140`). So a per-client socket loss must not call either on the shared runtime.
- `Initialize` stores one `atomic.Bool` and the gate rejects until it is set (`agent.go:61,184,201-202`); client capabilities are not retained.
- Run context uses `context.WithoutCancel` (`host.go:506`), separating request cancellation from run cancellation.
- ACP package contracts: the writer holds a prompt result until the settled event and active Follow writes are written; explicit follow keeps its own follower with cut and resync; `ServeStdio` closes the host on EOF (`internal/acp/README.md` "Adapter contract" and "Stdio server"). `internal/acp` denies importing `leader` (README imports section).
- `internal/app` has `ACPModule`/`ACPRuntime` for `ask acp` with `fx.ValidateApp` tests (`internal/app/README.md` "Editor connection composition"); no leader module exists.
- Home resolution: `ASK_HOME` is read by `cmd/tui` and passed to `NewNativeAuth` and ACP config (`cmd/tui/headless.go:82`, `cmd/tui/acp.go:76`); `settings.NewAuthStore` defaults to `~/.ask` (`internal/settings/auth.go:66-76`). Settings path/lock helpers are unexported (`internal/settings/lock.go`, `lock_unix.go`).
- `internal/tui` is a scaffold with `AgentClient` mentioned only as a TODO comment (`internal/tui/commands.go:4`). TUI doc: stopping a TUI detaches the client and does not dispose the shared leader agent (`docs/tui-architecture.md:165`).

### 1.4 Relationship to H13a and later work

- H13a delivers the host, per-session Agent, Follow/resync, settled-write barrier, native auth readiness. Its plan states "H13b consumes this host later, with client detach separate from shared Agent disposal" (H13a `plan.md:29`).
- T1a (RM:673-690) consumes H13b: ConnectOrSpawn path, two-client isolation, disconnect/reconnect without prompt resend, TUI exit detaches only.
- H13c (RM:470, RM:481) waits on H13b, H8, H10, H12.
- Pi timeline rule for later clients (stale frames fenced after re-attach, no auto-replay, one writer per session) is the source of RM:511 tests (`timeline.md:273`).

### 1.5 Xia claims verified against live source

Verified: Adapter-per-socket would create independent stores (agent.go:90-101); Close/Fail scope (agent.go:142,163-178; host.go:122-140); single initialized flag (agent.go:61,201); `WithoutCancel` (host.go:506); empty leader package; no `ask leader` or version probe; settings uses OS flock (lock_unix.go:14); x/sys and gofrs/flock are indirect (go.mod:394,199). Grok source line claims in XIA §3 were not re-read in this pass (no Grok checkout read); treat them as secondary evidence tied to pinned commit `2bdd1d6a`.

## 2. Scope challenge (Step 0, evidence form)

**What already exists:** H13a host/adapter/Follow/write barrier; native auth composition; settings flock/no-follow pattern (private, not reusable directly); ARCH 7.3 design; package README contracts. Missing: all of `internal/leader`, `ask leader`, `ConnectOrSpawn`, version probe, leader frames in `pkg/protocol`, leader fx module, TUI client stub, leader-specific depguard rule.

**What was asked:** the full H13b row (RM:480) plus RM:484-491 and RM:514 tests, with a real two-stub exit. HOLD SCOPE: deliver all of it; no cuts.

**Additions beyond request to reject (candidates the plan writer must not smuggle in):** gateway/WS/token work (H13c); durable load or session TTL/GC (H8); zombie eviction, acquire-slot, relay (ARCH:270); T1a rendering; a leader cluster/registry; new monitoring subsystem; speculative user-facing limit flags (XIA §7 says typed config, no flags).

**Complexity (estimates, not commitments):** roughly 12+ new/changed files across `pkg/protocol`, `internal/leader` (server, client, lock, spawn, frame, platform peer-UID files), `internal/acp` (typed driver context, owned detach), `internal/app` (leader module), `cmd/tui` (leader/version dispatch, client stub), `.golangci.yml`, docs. This exceeds the >8-file and >3-phase heuristics, justified by the three-step roadmap split and `--deep`; record, do not cut.

## 3. Conflicts and uncertainties (separate from facts)

1. **TUI fallback vs acceptance.** ARCH:279 says the TUI "may build its own agent in process" on connection failure; RM:677-678 says T1a's path must not silently fall back. XIA §3.7 reconciles it by requiring the leader path in H13b acceptance. Plan should adopt: H13b stubs never fall back. Not a user decision conflict, but plan must say so.
2. **Reconnect model.** ARCH:261 says a reconnecting client "asks for events after its last `seq`"; XIA §6.5-6.6 proposes new client ID plus explicit live attach with Follow cursor, no automatic resend. Compatible, but exact methods are unspecified.
3. **Driver election.** RM:484 says creator or explicit taker drives; Grok picks an arbitrary remaining subscriber on driver loss (XIA §3.4). XIA proposes "absent driver until idle takeover". RM:489 only fixes question cancellation. This is a proposed contract, not an accepted decision.
4. **Frame format.** ARCH:256-259 describes frames carrying ACP; XIA §6.2 proposes 4-byte length plus raw nested JSON and an 8 MiB bound. XIA itself notes envelope overhead vs the 8 MiB inner limit in H13a (`internal/acp/README.md`, stdio section). Unresolved.
5. **Write barrier.** H13a completes prompt only after actual writes (`internal/acp/README.md`). XIA §6.7 proposes router acceptance plus per-client FIFO for sockets. This is a new transport contract; XIA says it needs review.
6. **Where driver context travels.** Leader must not import `acp`/`agent` (leader README imports), yet the adapter needs per-driver capabilities (RM:488; agent.go:201 retains none). Typed private context in `pkg/protocol` is proposed (XIA §6.4), not decided.
7. **Idle authority.** Replacement requires idle detection that route-table emptiness cannot give (XIA §7); the authoritative activity source from app/ACP does not exist yet.
8. **Roadmap vs XIA on `list`.** ARCH:268 lists `list | status | stop`; XIA §6.10 limits list to the single configured endpoint. Needs a one-line confirmation.
9. **Unverified:** XIA's Grok line citations; first-answer completion semantics in Grok's ACP dependency (XIA §13 says not established); whether macOS/Linux x/sys exposes peer credentials as XIA assumes (confirm with a test or build before relying).
10. Roadmap text still says H13 "Waits on" D17 closed; H13a plan states D17 closed with coder v0.13.5, so no remaining blocker.

## 4. Load-bearing assumptions

- One internal ACP link via app-owned `io.ReadWriteCloser` is sufficient (leader README and ARCH:248); an adapter per client is the fallback only if typed context fails.
- Shared Adapter can accept a typed router-owned driver context without per-client Initialize state in the SDK.
- Faux provider and external HTTPS fixtures (H13a pattern) suffice for real-Agent E2E; reverse-request methods need a disclosed peer fixture since fs/terminal tool owners are absent (`internal/acp/README.md`, H13a plan).
- Built-binary E2E with isolated `ASK_HOME` is available (H13a `cmd/tui/acp_e2e_test.go` pattern).
- Other-UID rejection needs an OS/container fixture; build-only checks cannot certify it.

## 5. Acceptance criteria (evidence-derived)

1. Two separate built `ask` client processes connect through `ConnectOrSpawn` to one auto-started leader and share a session's updates (RM:480, RM:514).
2. All RM:514 leader tests pass, plus XIA §11 gates the user accepts.
3. Client disconnect never cancels a run or disposes the host; leader stop disposes (ARCH:261; leader README).
4. Editor `ask acp` and headless remain listener-free and leader-free; each composition passes `fx.ValidateApp` (RM:482).
5. `internal/leader` imports no agent/acp; depguard enforces it; READMEs match final behavior.
6. Race and leak checks, H13a E2E regressions, lint, build pass (R: development rules, repo commands).

## 6. TDD order suggestion for the writer (not a plan)

Protocol/frame and ID-routing unit tests first; then lock/path/UID safety; then in-process server with fake byte-stream peer for routing; then app composition with real host; then spawn and version probe; then built-binary two-process E2E; keep daemon conversion out.

## 7. Open questions (for the user, one at a time)

1. Approve or amend the five XIA review items (XIA §13): single internal ACP link, explicit live attach/take/detach/discovery, absent driver after disconnect, router acceptance plus FIFO writes, and frame format/bounds. These materially shape protocol and cannot be inferred.
2. Confirm that H13b stubs must never fall back to a direct agent (conflict 1).
3. Confirm whether `ask leader list` covers only the single configured endpoint.

## 8. Handoff to the plan writer

- Mode: deep, TDD, HOLD SCOPE; do not add `--yagni`. Use title like "H13b leader over Unix socket"; slug `h13b-leader-unix-socket`.
- Carry into the plan: the outcome (§1.1), non-goals (§1.2), acceptance (§5), conflicts (§3) with open decisions marked unresolved until the user answers, risk High with rollback (XIA §12), and file ownership map (XIA §10) as candidate only.
- Phase 1 should resolve the open protocol decisions via user check-in before implementation; later phases need per-phase scout per `--deep`.
- Do not claim durable recovery, gateway behavior, or T1a UI.
- Plan code comments and file names must avoid plan/phase labels (user rule).

## 9. Source list

README.md; docs/ask-architecture-reference.md (214-296); internal/README.md; internal/leader/README.md; internal/acp/README.md; internal/app/README.md; pkg/protocol/README.md; plans/260930-2254-pi-feature-inventory-go-roadmap/{roadmap.md (22-28, 468-515, 670-709), phase-02-synthesis-roadmap.md, plan.md, timeline.md:273}; plans/261007-0700-h13a-acp-stdio/plan.md; plans/reports/xia-261008-0828-grok-leader-h13b-port.md; docs/tui-architecture.md:165; internal/acp/{agent.go,host.go}; internal/settings/{lock.go,lock_unix.go,auth.go}; cmd/tui/{main.go,acp.go,headless.go}; .golangci.yml; go.mod.
Not read in depth: H13a phase files 1-4, `code-review-261008-h13a-plan-compliance.md` body, `cmd/tui/acp_e2e_test.go`, `internal/tui` beyond the TODO comment.

## Unresolved questions

- The three questions in §7.
- Exact bound and field names for frames and leader limits (XIA §6.2, §7).
