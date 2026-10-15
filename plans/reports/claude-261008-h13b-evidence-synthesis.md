# H13b evidence synthesis and decision gate

Date: 2026-10-08. Branch `design-tui-bubbletea` at `213afbe`. Route: `ak plan --deep --tdd`, HOLD SCOPE, no `--yagni`.
This pass drafts no plan and changes no product, roadmap or existing report file. It re-read the repository and the pinned Grok checkout instead of copying the earlier summaries.
Citation form: `path:line`. `RM` is `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`. `ARCH` is `docs/ask-architecture-reference.md`. `XIA` is `plans/reports/xia-261008-0828-grok-leader-h13b-port.md`. `GROK` is `/Users/dale/Desktop/workspace/opensources/grok-build`, commit `2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8`, which matches the commit named in XIA §2.

## 1. Outcome

H13b is feasible on the current tree and needs four maintainer decisions before the plan is final. The five proposals in XIA §13 reduce to one real fork (driver loss), because documents, tests and code already settle the other four. Three more forks appear that XIA did not list: the stub client's command surface, the scope of the version gate, and the evidence standard for the other-UID check.

## 2. Evidence that was missing and is now read

| Item | Read | What it changes |
|---|---|---|
| H13a `plan.md` and phases 1-4 | Yes, in full | H13a is complete. Its record already states that "H13b consumes this host later, with client detach separate from shared Agent disposal" (`plans/261007-0700-h13a-acp-stdio/plan.md:34`), and that detach closes only client followers (`phase-04-stdio-auth-and-e2e.md:83`). `session/close` and `session/load` are unsupported (`phase-02-session-adapter.md:201`), so the ACP surface has no way to dispose a single session. |
| `plans/reports/code-review-261008-h13a-plan-compliance.md` | Yes, in full | The review changed the prompt write barrier: a prompt result now waits for active Follow write frontiers too (finding row 4, line 17), repeated Unfollow is idempotent (line 18), and the provider and OAuth E2E rows use the production binary with an external HTTPS fixture, a CONNECT proxy and a temporary CA (line 20). The H13b plan must reuse that fixture style, and the leader must not break the Follow barrier when it adds an output hop. |
| `cmd/tui/acp_e2e_test.go` (1772 lines) | Structure read: helpers `acpBinary`, `e2ePeer`, `e2eOptions`, `startE2E`, `providerFixture`, `startProductionOAuth`, `TestACPE2ENoListener` (`lsof` check) | Two-client leader E2E can reuse `acpBinary` and the fixture helpers. Those helpers are bound to one peer and one stdio pair, so the leader tests need a socket peer type that sits next to `e2ePeer`. |
| `internal/tui/README.md`, `docs/tui-architecture.md:165` | Yes | `internal/tui` is comment-only scaffolding that denies `internal/leader` and `internal/acp` (`.golangci.yml:37`). The `AgentClient` seam is a constructor argument, so the real client code and the stub must live in `cmd/tui` (the composition root) and `pkg/protocol`, not in `internal/tui`. |
| Pinned Grok source | Yes, targeted | See §3.4. |

## 3. Verified facts

### 3.1 Scope and ownership

1. H13b exit: two `ask` TUI stubs share one auto-started leader (`RM:480`). It waits on H13a only (`RM:470`), which is complete. H13c owns the network gateway and daemon conversion. T1a owns the real chat UI.
2. The leader test list is `RM:514`. The session and driver rules are `RM:484-489`. Spawn rules are `RM:490`. Version rules are `RM:491`. Lifecycle, security and "not copied from Grok" rules are `ARCH` section 7.3 (lines 254-279).
3. `internal/leader` holds only `README.md` and `doc.go`. Its README allows stdlib, `pkg/protocol` and `logs`, and denies `internal/agent`, `internal/acp`, gateway, http and config (`internal/leader/README.md` imports section). `.golangci.yml` mentions `internal/leader` only in the TUI rule (line 37) and the core rule (lines 126-127). Nothing yet stops `internal/leader` from importing the agent or ACP, so the depguard rule is a confirmed change.

### 3.2 Current code that constrains the design (re-read)

- `NewAdapter` builds its own host (`internal/acp/agent.go:90`). `Bind` attaches one SDK connection once (`agent.go:109`).
- `Adapter.Fail` calls `Host.latch` (`agent.go:142`). `latch` sets a failure, closes `failed` and cancels every session run (`internal/acp/host.go:122`). `Adapter.Close` disposes every session (`agent.go:163`). A client socket loss must call neither.
- `Initialize` stores one `atomic.Bool` and ignores the request body (`agent.go:201-202`). The gate at `agent.go:184` and `:332` reads that flag. Client capabilities are not retained anywhere in the adapter.
- A run uses `context.WithCancelCause(context.WithoutCancel(ctx))` (`host.go:506`), so request cancellation does not stop a run.
- `defaultMaxFrame` is 8 MiB (`internal/acp/stdio.go:19`). The link is newline-delimited JSON through `LineLimitReader` (`internal/acp/wire.go:97`).
- SDK request ids are `*json.RawMessage` (`acp-go-sdk@v0.13.5/connection.go:26`), and `$/cancel_request` carries `requestId` as raw JSON that the SDK canonicalises (`connection.go:48,145,390,544-554`). A string id produced by a rewrite is therefore valid, and a rewrite must also rewrite `requestId` inside `$/cancel_request`.
- `cmd/tui/headless.go:107` dispatches `acp` before prompt parsing, which is the pattern for `leader`. `cmd/tui/main.go:125` `runInteractive` is still the demo menu. `Info.Version` is the literal `"dev"` (`cmd/tui/acp.go:82`). A search for `ldflags`, `ReadBuildInfo` and `-X main` outside `plans/` finds nothing, and no Makefile or release config exists.
- `go mod tidy -diff` shows `golang.org/x/sys v0.47.0` moving from the indirect block to the direct block, because `cmd/tui/auth_input.go:10` already imports `golang.org/x/sys/unix`. The same diff also moves `github.com/coreos/go-oidc/v3` to direct. That second change is existing drift unrelated to H13b.
- `golang.org/x/sys@v0.47.0` provides `GetsockoptXucred` for darwin (`unix/syscall_darwin.go:488`) and `GetsockoptUcred` for linux (`unix/syscall_linux.go:1283`). This closes the open assumption that the peer-credential calls exist.
- `internal/acp` has no session list, dispose or activity query. `Session.admit` checks idleness through `agent.State().Status` (`host.go:476`).

### 3.3 Baseline

`git status --short` shows only the untracked user-owned paths: `.agents/`, the Xia report and the two Claude evidence reports. This synthesis report is the only file added by this pass.

### 3.4 Upstream Grok claims (verified read-only against the pinned checkout)

| XIA claim | Result |
|---|---|
| Four-byte big-endian length, 64 MiB maximum (`protocol.rs`) | Verified: `MAX_MESSAGE_SIZE = 64 * 1024 * 1024`, `u32::from_be_bytes` read of the length. |
| `rewrite_request_id` rewrites only messages that have a method and an id; prefixes the client id to the JSON text of the original id | Verified in `leader/server.rs:376-392`. `parse_response_id` restores the original JSON value and the client (`:395-409`). |
| Driver loss picks an arbitrary remaining subscriber | Verified: `session_subscribers.get(&sid).and_then(\|s\| s.iter().next())` at `server.rs:1672-1681` area. The order is unspecified. |
| Unbounded per-client channels | Verified: `mpsc::unbounded_channel()` at `server.rs:1564`, and the outbound write at `:2441-2444` has no timeout. |
| Spawn uses a process group, not `setsid` | Verified: `cmd.process_group(0)` in `spawn_leader_subprocess` (`leader/mod.rs`). Ask's accepted `Setsid` remains an adaptation. |
| Grok has `leader list` | Not found. A search of the pager binary and leader sources finds no such command, so `list` comes only from `ARCH:268`. |

The `rewrite_request_id` doc comment says an id is namespaced in place, while the code builds `"<client>|<original json>"` as a string. Neither Grok function covers the id inside `$/cancel_request`. That gap is an Ask requirement, not a copied behavior.

Unverified and not load-bearing: XIA's other Grok line numbers (for example `app.rs:723-769`, `client.rs:21-30`) and the Grok pending-response dependency behavior that XIA §13 already says is not established. The plan must cite the Ask contracts, not these lines. Where the plan needs Grok behavior, it should cite only the rows above.

## 4. Resolved conflicts and settled items (no user question needed)

| # | Item | Resolution | Basis |
|---|---|---|---|
| R1 | Direct fallback (`ARCH:279` "may build its own agent") versus no silent fallback (`RM:677-678`) | H13b implements no direct fallback at all. The stub connects through `ConnectOrSpawn` and reports a failure visibly. The `direct` `AgentClient` implementation is not part of H13b. This satisfies both texts because `ARCH` says "may" and `RM` forbids it on the acceptance path. | `ARCH:279`, `RM:677-678`, XIA §3.7 |
| R2 | One internal ACP link versus an adapter per socket (XIA §13.1) | One app-owned `io.ReadWriteCloser` link with a typed, router-owned driver context. An adapter per socket would create independent session stores (`agent.go:90`) and was already rejected by the accepted design. | `internal/leader/README.md` main interfaces; `ARCH:248` diagram ("ACP over io.Pipe"); `RM:488` ("passes only the driver's capabilities for that session to the adapter") |
| R3 | NDJSON link versus length-framed socket (XIA §13.5) | The conversion point is the leader's two edges. The agent link stays NDJSON, because the SDK connection is newline-delimited and `internal/acp` cannot be imported. The leader implements its own small line codec for the agent link and a length-prefixed codec for sockets. App only builds the pipes. | `internal/acp/wire.go:97`; leader import rules; `ARCH:256` ("frames that carry ACP") |
| R4 | Frame bound (XIA §6.2 says 8 MiB total, which clips H13a's 8 MiB payload) | The ACP payload limit is 8 MiB, the same as H13a. The socket frame limit and the agent-link line limit are that value plus a fixed envelope and id-rewrite allowance, so every frame H13a accepts is carried without clipping. A larger frame closes only the offending client. App sets the link limit when it builds it. | `stdio.go:19`; id rewrite grows a message |
| R5 | Router acceptance plus per-client FIFO versus a completed-write barrier (XIA §13.4) | Use the FIFO. One writer per socket gives the receiving client its updates before its prompt result, so a client that has the result has the preceding updates. A socket-level barrier would make a slow observer part of the shared host's prompt completion. Editor stdio keeps its existing barrier unchanged. On a full queue or a stalled write, the leader closes that client's socket; the client reconnects with a new id and follows from its last cursor. | XIA §6.7; H13a review row 4; `ARCH:261` |
| R6 | Live attach, take, detach, discovery (XIA §13.2) | New `_ask/*` extension methods are required, because durable `session/load` is unsupported (`phase-02:201`) and `RM:484` needs an explicit taker. The plan proposes names and error codes in `pkg/protocol`. They are new public wire surface, so the plan must list them verbatim for review, but they do not need a separate question. The one real fork inside this item is D1. | `RM:484-489`; `ARCH:261` |
| R7 | `ask leader list` | `list` shows the single configured endpoint (one row with instance, build, protocol and counts). There is no cluster registry. It never spawns. Session discovery is a separate extension method. Risk is low and reversible, and it removes nothing from `ARCH:268`. | `ARCH:268`; no Grok `list`; XIA §6.10 |
| R8 | `x/sys` treatment | No new dependency. `go mod tidy` moves `x/sys` to the direct block. The plan must make that one-line change. It must not bundle the unrelated `go-oidc` move unless the maintainer wants a full tidy. Locking uses `syscall.Flock` with `O_NOFOLLOW`, as `internal/settings/lock_unix.go` does. `gofrs/flock` stays indirect. | `go.mod:394`, `go mod tidy -diff`, `auth_input.go:10` |
| R9 | Version identity source | Keep the outer leader protocol integer (in `pkg/protocol`), the ACP wire version, the SDK identity and the build identity separate. The build identity comes from `debug.ReadBuildInfo` (VCS revision when present) with a `dev` fallback, because the repository has no ldflags or release tooling. The existing `Info.Version` literal is replaced by the same source. The spawn probe is a machine-readable version command on the `ask` binary. | `acp.go:82`; no `ldflags` match; XIA §6.2 |
| R10 | Idle authority | App supplies a narrow callback pair through typed leader config: one reports whether the runtime is idle, and one closes admission only if it is idle, in one atomic step. The ACP host gains the matching method. The leader never imports the agent or ACP. Route-table emptiness is not used, because it cannot see an active run or a pending question. | XIA §7; `host.go:476` shows the host already owns idleness; leader import rules |
| R11 | Aggregate bounds and defaults | Typed leader config with no end-user flags. Starting defaults, to be justified in the plan and tested with small injected values: 32 clients, 256 live sessions, 256 pending forward requests per client, 1024 pending in total, per-client queue 1024 frames and at least 4 times the frame limit in bytes. A queue byte bound must exceed one maximum frame. The numbers are unmeasured, which XIA §7 accepts, so no throughput target is invented. At a bound the leader rejects the new admission with a safe error and never evicts a session or cancels work. | XIA §7, §11 |
| R12 | Version-skew test mechanics | Built-binary skew tests are not possible without a test-only version override, and the H13a rule forbids test-only product flags. The leader and client take their protocol version through typed config. In-process servers with a different version, speaking on a real socket, cover rejection and the replacement handshake. A built `ask` binary is the client in the replacement test. The plan must disclose this as supplemental, not as built-binary proof of the skew path. | H13a plan "no production test flag"; `RM:514` |

## 5. Remaining load-bearing assumptions

These are design bets the plan must prove with a test before the dependent work, not questions for the maintainer.

1. A typed, router-owned driver context can reach the shared adapter without per-client SDK `Initialize` state. If a spike shows it cannot, the fallback is an adapter per client around one host, which changes the app-to-leader boundary. The plan's first phase should end with this proof or the fallback decision.
2. The leader can rewrite ids and `$/cancel_request.requestId` on raw JSON without parsing into `float64`, and can route Follow subscriptions to their owner by `subscriptionId` (the subscription map is global in `internal/acp/updates.go`, per the scout).
3. A failed client must never reach `Adapter.Fail`, `Adapter.Close` or `Host.latch`. A wrong call here kills every client, so this needs a dedicated regression test.
4. The live-session bound can become permanent exhaustion, because H13a has no session dispose. With the accepted rule "reject, never evict" (XIA §7), a long-lived leader that reaches the bound refuses new sessions until `ask leader stop`. The plan should state this as a known limitation, size the default generously, and leave TTL or disposal to H8. If the maintainer wants a release path inside H13b, that is a scope addition and needs its own decision.
5. macOS `sun_path` is about 104 bytes. Tests need a short temporary `ASK_HOME` under `/tmp`, and the leader must fail clearly on a too-long path. The default path under `~/.ask` could exceed the limit for a long home directory, so the plan needs an explicit path-length check.
6. Unsupported platforms (no `unix` build tag) must return a clear error and not skip the peer check.
7. The reverse-request tests need a disclosed peer fixture, because the filesystem, terminal and permission tool owners do not exist yet (`phase-03` and the H13a README say so).

## 6. Decisions that need the maintainer

Each question stands alone. Recommended options are marked.

### D1. What happens to a busy session when its driver disconnects?

**Question:** After the driver of a running session disconnects, may a remaining observer take over the driver role while the run is still active?

**Options:**
- A. Takeover only when the session is idle (XIA §6.5 as written). An observer cannot steer or cancel until the run settles.
- B. Takeover allowed whenever there is no live driver, even if the run is busy. It is still rejected when another live client drives the session. (Recommended)
- C. Automatic promotion of a remaining subscriber (Grok's behavior, with a deterministic oldest-first rule).

**Impact:** `RM:489` fixes only question cancellation, and `RM:514` requires that a client disconnect does not cancel the run. Under A, a long or stuck run with a dead driver cannot be cancelled by any client, because driver-only operations include run cancel (XIA §6.5), and an unbounded tool drain can keep the run busy. The only remedy would be `ask leader stop`, which also kills every other client's work. B keeps `RM:484` ("creator or explicit taker is driver") and `RM:486` (a switch on a busy session the caller does not drive is rejected, which describes a live foreign driver) intact. C contradicts "explicit taker" and lets an observer gain authority without asking, which XIA's challenge table lists as a risk (`XIA §8`, "Any call takes driver?").

**Evidence:** `RM:484-489`, XIA §6.5 table, Grok `server.rs` driver transfer (arbitrary `HashSet` element, §3.4 above).

**Ask:** Keep B, or choose A or C?

### D2. How does the "TUI stub" appear in the product binary?

**Question:** What command does a user run to start a leader client stub that proves the exit "two `ask` TUI stubs share one auto-started leader"?

**Options:**
- A. Add a new explicit subcommand, proposed name `ask connect`, as a small line-oriented client that uses `ConnectOrSpawn`, creates or attaches to a session, sends prompts and prints updates. Leave bare `ask` (the demo menu, `cmd/tui/main.go:125`) untouched until T1a replaces it. (Recommended)
- B. Change bare `ask` so the existing demo menu path becomes the leader client stub.
- C. A client reachable only through a hidden or undocumented flag.

**Impact:** The user's recorded rule is that tests must run the real product entry point and never a test-only harness, so some real command must exist. A adds one documented command that T1a can later fold into the bare `ask` entry, with no regression to the demo. B changes the behavior of bare `ask` before the real UI exists, and the exit then depends on a Bubble Tea program. C hides a product contract. All three keep rendering in `internal/tui` out of scope. The choice also fixes the usage text and `CLAUDE.md` command list that the plan updates.

**Evidence:** `RM:480`, `RM:673-690` (T1a connects through `ConnectOrSpawn`), `cmd/tui/main.go:125`, `cmd/tui/headless.go:107`, memory "Tests must run the real code path".

**Ask:** Approve A with the name `ask connect`, or choose B, C, or a different name?

### D3. Does a build difference reject a client, or only a protocol difference?

**Question:** If the client and the running leader speak the same outer protocol version but come from different builds, is that a mismatch?

**Options:**
- A. The hard gate is the outer protocol version only. The build identity is shown in `status` and in a one-line stderr hint when it differs. A client-spawned idle leader is replaced automatically only when the protocol versions differ. (Recommended)
- B. The gate is protocol version and build identity together. Any rebuild is a mismatch: reject with an upgrade hint, and replace a client-spawned idle leader.

**Impact:** `RM:514` tests a different protocol version and `RM:491` says "version mismatch" without naming which version. Under A, a new build with an unchanged protocol keeps talking to an old leader process, whose agent code is stale until `ask leader stop`. Under B, the stale-code risk is gone, but a developer who rebuilds from VCS-stamped builds hits a mismatch on every commit, and the replacement handshake runs far more often (more exposure for the riskiest part of the design). `go run` builds all report `dev` and would match each other under both options. The repository has no release tooling, so the build identity source is `debug.ReadBuildInfo` either way (R9).

**Evidence:** `RM:491`, `ARCH:276`, `RM:514`, `acp.go:82`, no ldflags in the repository.

**Ask:** Choose A or B?

### D4. What evidence closes the other-UID rejection requirement?

**Question:** The test list requires "a peer with another UID is rejected", but no second UID exists on the test machine and the 0700 directory already stops another UID from reaching the socket path. What counts as sufficient proof?

**Options:**
- A. A real-socket test where the leader's expected owner UID is injected through typed config as a different value, so the real operating-system peer-credential call runs on macOS and Linux and the mismatch is rejected. Add a documented manual check with root (root can reach the socket despite 0700 and is a non-owner UID). (Recommended)
- B. A, plus a privileged fixture (Linux container or `sudo`) as a required gate. `docker` and `sudo` exist on this machine, but a container cannot easily share a Unix socket path with the macOS host, and the sudo step is interactive.
- C. A fake credential source in unit tests only.

**Impact:** The requirement is a security boundary. C does not exercise the real syscall and cannot certify it. A exercises the real syscall and the real rejection path, but the owner comparison input is injected, so it is not a literal second-user connection. B is the strongest and the least repeatable. Choosing A accepts a disclosed gap against XIA §11 ("actual non-owner rejection on macOS/Linux").

**Evidence:** `RM:514`, `ARCH:272-277`, XIA §11, `x/sys` APIs (§3.2), `id -u` 501 with `docker` and `sudo` on `PATH` on this host.

**Ask:** Approve A, or require B?

## 7. Plan-writer handoff

### 7.1 Outcome, constraints, non-goals

- **Outcome:** two separate built `ask` client processes, started through the D2 command, reach one auto-started `ask leader` over `~/.ask/leader.sock`. They share session updates, keep independent views and request ids, and a client exit never cancels a run or disposes the shared host. Plan metadata: title "H13b leader over Unix socket", slug `h13b-leader-unix-socket`, directory `plans/261008-h13b-leader-unix-socket/` (rule `{date}-{issue}-{slug}`), deep and TDD, HOLD SCOPE, task hydration on, journal on.
- **Constraints:** `internal/leader` imports only stdlib, `pkg/protocol` and `logs`. App owns the agent link, fx composition and lifetimes. Editor `ask acp` and headless `ask -p` stay listener-free and leader-free, each with its own `fx.ValidateApp` test. No SQL migration. No new dependency. No test-only product flag. Typed config, no speculative end-user flags. Code comments, file names, test names and commit messages carry no plan, phase or finding labels. Use ASD-STE100 Simplified Technical English in code comments.
- **Non-goals:** H13c gateway, token, Origin policy and daemon conversion; durable load or compaction (H8, H10); a direct-agent fallback (R1); T1a rendering; zombie eviction, acquire-slot guard, relay and auto-update relaunch (`ARCH:270`); a leader cluster or registry; a new monitoring subsystem; session TTL, GC or dispose; protocol compatibility beyond the hard gate.

### 7.2 Acceptance criteria

1. Two built client processes share one lifetime-lock owner, one leader instance and the same session updates (`RM:480`, `RM:514`).
2. Every test on `RM:514` passes, plus the gates from XIA §11 as adjusted by the decisions D1 to D4 and by R4, R5, R11 and R12.
3. A client disconnect never calls `Adapter.Fail`, `Adapter.Close` or `Host.latch`, and never cancels a run. Leader stop closes admission, disposes the host and cleans up in that order.
4. `ask acp` and `ask -p` stay listener-free (`TestACPE2ENoListener` style check on the new binary) and all H13a E2E tests still pass.
5. Depguard enforces the leader import rule. `go build`, `go vet`, lint, `go test -race` and goleak checks pass.
6. Package READMEs, `ARCH` section 7.3, usage text and `CLAUDE.md` commands match the final behavior (docs impact: major, written after the behavior exists).

### 7.3 Phase model

Keep the dependency order of the scout, with these corrections:

- **Phase 1, full detail: transport and security foundation.** Protocol DTOs and the outer version constant in `pkg/protocol`. Socket frame codec and agent-link line codec with the R4 bounds. Register and ready handshake with the hard gate. Raw id rewrite table, including `$/cancel_request`. Secure path rules (0700, 0600, no symlink, short-path check). Stable-inode flock. Peer-credential check per platform behind a `unix` build tag, with the D4 outcome. Depguard rule. Close the driver-context spike from §5 item 1 by the end of this phase, with a recorded go or fallback decision.
- **Phase 2, outline: router.** Per-client initialize snapshot, session ownership, D1 driver rules, pending-route table, scoped cancel, Follow ownership by subscription, bounded per-client FIFO with socket close.
- **Phase 3, outline: app link and ACP changes.** `internal/app` leader module, the single link, typed driver context, live attach, take, detach and discovery, the idle and quiesce callbacks (R10), and the regression that proves client loss does not fail the host.
- **Phase 4, outline: spawn and CLI.** `ConnectOrSpawn`, binary lookup (beside the caller, then `PATH`), version probe and build identity (R9, D3), `Setsid`, `--spawned-by-client`, size-rotated log, `ask leader list|status|stop`, shutdown frame first, pid identity check before any signal.
- **Phase 5, outline: stub client and two-process E2E.** The D2 command, shared socket peer fixture next to `e2ePeer`, the roadmap exit test, spawn race, startup faults.
- **Phase 6, outline: reverse requests, shutdown, regressions, docs.** Eligibility and generation checks, driver loss cancels the open question (late answers ignored), leader stop ordering, headless and editor regressions, docs.

Each later phase gets its own scout pass at cook time, because the per-phase file inventory depends on the Phase 1 results.

### 7.4 TDD obligations

- Every phase lists Tests Before (RED), Refactor (GREEN) and Tests After. A control that passes at once is recorded as a control, not as RED. A fixture timeout is not RED.
- Phase 1 RED list: frame round trip and over-limit rejection; handshake accept and reject by version; id rewrite for numeric, string, large (above 2^53) and separator-bearing ids, and for `requestId`; path mode, owner and symlink refusal; lock contention in goroutines and in a child process, with an unchanged inode across stop and start; peer credential rejection on a real socket (D4); depguard failure on a forbidden import.
- Protected seams: `internal/acp` public API and stdio behavior, `internal/settings` path and lock helpers (copy the rule, do not export), headless and editor outputs.
- Every goroutine test ends with `goleak.VerifyNone`. Race runs cover `internal/leader`, `internal/acp`, `internal/app`, `cmd/tui`.
- Built-binary evidence uses an isolated short `ASK_HOME` under `/tmp`, real sockets, and only the processes the test started. Tests never attach to the maintainer's leader, and each test stops what it spawned (`~/.claude/rules/process-management.md`).
- Supplemental (in-process) evidence must be labeled as such: version skew (R12), the reverse-request peer fixture, and the injected-owner peer check (D4).

### 7.5 Deep-mode artifacts the plan must contain

1. File inventory with action, path, size and test impact. Candidates: `.golangci.yml`; `pkg/protocol` leader frames; `internal/leader/{frame,handshake,ids,paths,lock,server,router,client,spawn}.go` plus platform peer files and tests; `internal/acp` driver context, detach and attach edits; `internal/app/module_leader.go` and test; `cmd/tui` leader and stub files, dispatch and usage; `go.mod`; docs. Mark each row confirmed or candidate.
2. Test matrix by Critical, High and Medium level (the scout's matrix, extended with R4, R5, R11 and D1 to D4).
3. Dependency map with the phase order, the shared driver-context contract between phases 2 and 3, and the idle callback dependency between phases 3 and 4.
4. Function protection list for the shared H13a surfaces (`Adapter`, `Host`, `ServeStdio`, `CheckedWriter`) and for headless and `ask acp`.
5. Runtime flow proof table (actor, trigger, entry point, internal path, observable result, test, external fixtures, prepared data) in the style of the H13a plan.
6. Risk score High, rollback (stop only an owned leader, never delete a live socket or lock), and the exit gate.

### 7.6 Choices to record verbatim

The plan must copy these into its decision section with the maintainer's answer:

- D1, D2, D3 and D4, each with the question, the chosen option and the date.
- R1 (no direct fallback in H13b), R4 (8 MiB payload preserved), R5 (FIFO transport contract, which differs from the editor barrier), R7 (`list` semantics), R11 (default numbers and the "reject, never evict" rule), R12 (disclosed supplemental skew evidence).
- The known limitation in §5 item 4 (no session release path before H8).
- The `go.mod` rule in R8 (only the `x/sys` line moves).

## 8. Unresolved questions

- D1 to D4 await the maintainer.
- The exact new method and error names for attach, take, detach, discovery and the leader control frames are for the plan writer to propose in `pkg/protocol`; the maintainer sees them when reviewing the plan (R6).
- The default numbers in R11 are unmeasured starting values.
- The Grok pending-response dependency still does not establish first-answer semantics (XIA §13), so the pending table is an Ask-native design.

Status: DONE_WITH_CONCERNS
Summary: The missing H13a, review, E2E and TUI evidence is read, the Grok claims that matter are checked against the pinned checkout, and twelve repo-answerable issues are resolved. Four maintainer decisions remain (driver loss on a busy session, the stub command, the version-gate scope, the other-UID evidence standard), each with a recommended option.
Artifacts or evidence: /Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/plans/reports/claude-261008-h13b-evidence-synthesis.md
Concerns/Blockers: The plan should not be finalized until D1 to D4 are answered. The live-session cap has no release path before H8, and the XIA §11 other-UID gate may be relaxed by D4. Other Grok line numbers in XIA stay unverified, and the plan must not depend on them.

