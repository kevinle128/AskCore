---
title: "Leader connect-or-spawn: spawn retry, zombie eviction, newer-build replacement"
description: "Port the three remaining Grok connect_or_spawn behaviors into internal/leader: retry a leader start that dies early, evict a Linux leader that holds the lock without a socket, and replace an older leader with a newer build (VCS commit time or dev stamp) even when it is busy."
status: completed
priority: P2
effort: "6d"
branch: new-session
tags: [backend, leader, process, tdd]
blockedBy: []
blocks: []
created: 2026-10-11
mode: deep
tdd: true
---

# Leader connect-or-spawn: spawn retry, zombie eviction, newer-build replacement

## Outcome

`leader.ConnectOrSpawn` gets the three Grok behaviors that Ask does not have yet (rows 6, 7 and 8 of the [port report](../reports/xia-261011-2235-connect-or-spawn-port.md)):

1. **R1 spawn retry.** When a leader that this call started exits with an error before it serves, the call starts it again, up to 3 starts in total, inside the existing deadline. The final error keeps the log tail. A binary or probe error is never retried.
2. **R2 zombie eviction (Linux only).** A process that holds the lifetime lock, has a verified owner record in the serving state that is older than 30 s, and has no connectable socket (absent or connection refused) gets SIGTERM through the verified pidfd path, then SIGKILL after 8 s through the same path. Then the normal start path runs. A leader that is stopping is never evicted. macOS and other platforms keep the H13b rule: no signal, the existing `ErrLeaderStarting`.
3. **R3 newer-build replacement.** A client whose binary has a newer commit time than the running leader replaces that leader with a forced shutdown, also when the leader is busy. Never a downgrade; an unknown, unparseable or equal stamp keeps the leader. Only a leader that a client started is replaced (N1 A); a leader started by hand or by a supervisor stays and the client prints a hint. The protocol-mismatch path stays idle-only (N2 A).
   **Dev builds (N3 B).** A binary without a VCS stamp (`go run`, build `dev`) has a dev stamp: the modification time of its executable plus the SHA-256 of its content, computed once at process start. Dev stamps are compared only with dev stamps: a dev build never replaces, and is never replaced by, a VCS-stamped build. Same hash keeps the leader (two terminals that run the same source never replace each other). A different hash with a strictly newer client modification time replaces the leader, forced as above.

Done means: every row of the Runtime Flow Proof Matrix has its named test passing, the Linux-only rows pass in a Linux container, and the gates in [Acceptance](#acceptance) pass.

**Plain consequence of R3 (user choice C).** A forced replacement closes every session of the old leader and kills their background jobs (the H5 job table closes with each session). Sessions are in memory only until H8, so their conversations are lost. Other TUI clients lose their link and reconnect to the new leader through the existing reconnect (`internal/tuiclient/client.go:240-279`).

## Authority

| Link | Read it for |
|---|---|
| [Port report (decision matrix)](../reports/xia-261011-2235-connect-or-spawn-port.md) | Rows 6, 7, 8; the head-to-head with Ask; what is already done (row 1, `CallerIsAsk`). |
| [Grok source anatomy](../reports/xia-261011-grok-connect-or-spawn-source.md) | `MAX_SELF_SPAWN_ATTEMPTS=3` (section 1.3), version eviction `should_evict` (2.3), zombie eviction 30 s / SIGTERM / 8 s / SIGKILL / 8 s / 3 per PID (3.3, 3.4). Apache-2.0; port behavior only, copy no code. |
| [Ask local anatomy](../reports/xia-261011-ask-connect-or-spawn-local.md) | Current `connector` state machine, owner record, process identity. |
| [H13b plan](../261008-1033-h13b-leader-unix-socket/plan.md) | Decision "Version gate: A" (line 65, superseded in part here), non-goals (line 54: zombie eviction, auto-update relaunch), risk rule "Linux fallback signals via verified pidfd, other platforms refuse" (test matrix line 205). Constraints: no test-only product flag, typed config, no plan labels in code (lines 29-48). |
| [internal/leader/README.md](../../internal/leader/README.md) | Package contract: lifecycle (lines 53-58), replacement (line 59). |
| [Architecture 7.3](../../docs/ask-architecture-reference.md) | Lines 282-307: lock, owner record, "Ask does not copy zombie-leader eviction" (line 298), version rule (line 307). |

## Decisions

### User decisions (fixed)

| Date | Decision |
|---|---|
| 2026-10-11 | **Choice C:** port all three remaining Grok behaviors R1, R2, R3 (question "stop here / port rows 6+8 / port 6+8+7"). |
| 2026-10-11 | R3 replaces an older leader **even when it is busy**. This **supersedes H13b "Version gate: A"** for same-protocol builds (A: replace only on protocol mismatch, only idle). Phase 6 adds the supersession note to the H13b decision table. |
| 2026-10-11 | **N1 A:** R3 replaces only a leader with `SpawnedByClient`; a supervised or hand-started leader stays and the client prints a hint. **N2 A:** the protocol-mismatch path stays `IfIdle`. **N3 B:** dev stamp (executable modification time plus SHA-256 of the executable) for builds without a VCS stamp, compared only between dev builds, same hash keeps, newer different hash replaces forced (busy leader stopped), never a downgrade, still only `SpawnedByClient` leaders. |
| 2026-10-08 (H13b) | Still in force: hard gate on the protocol integer; other platforms than Linux refuse a signal without a stable process handle; no test-only product flag (typed config only). |

### Plan decisions (planner, verified in code)

| # | Decision | Evidence |
|---|---|---|
| P1 | **R1 trigger.** Retry only when the started child exited with a non-nil error while the lock is free. A clean exit stays "lock loser, wait for the winner". `StartFn` errors (exec failure) and `leaderExecutable`/`ProbeAsk` errors return at once, as today. The executable is found and probed once per call and reused for every start, because one probe can take up to 15 s. | `client.go:170-183` (start branch, clean exit comment :181), `client.go:198-207`, `spawn.go:37` (`probeTimeout`) |
| P2 | **R1 also covers a real race.** A spawned leader that loses the lock to a leader that is still starting exits with status 1 ("holds the leader lock and does not answer"). Today that ends the call with `ErrStartupFailed`. With R1 the next loop sees the lock held and waits. | `cmd/tui/leader.go:214-221` |
| P3 | **R2 stopping marker.** The owner record gets a fifth line, the state: `serving` (written with the record after the socket listens) or `stopping` (written by a new `leader.Config.Stopping` hook as the first step of `Server.Close`, before the listeners close). A 4-line record (a leader from before this change) has no state and is never evicted. Reason: a healthy leader that drains after `ask leader stop` holds the lock, has an old start time and refuses connections, and its drain has no bound. Without the marker R2 would SIGKILL it mid-drain. | Owner record written after listen: `cmd/tui/leader.go:296-310`; socket removed before lock release: `:228`, `:301-305`; drain without bound: `internal/app/module_leader.go:148`; all stop paths go through `Server.Close` first: `module_leader.go:155-165`, `server.go:383` |
| P4 | **R2 zombie rule.** Evict only when all hold: `ConnectOrSpawn` (never `Connect`); dial gives ENOENT or ECONNREFUSED; `LockHeld`; owner record state `serving` with PID, start and instance; `now - Owner.Start >= ZombieAfter` (default 30 s); `VerifyLeaderProcess` passes. The owner record is written only after listen, so a serving record proves the leader had a socket; the 30 s age keeps Grok's margin. No in-call timer: `verifySignalOwner` re-reads the record just before each signal, so a leader that starts to stop (record changes to `stopping`) is not signalled. `WriteOwner` truncates and then writes (`lock.go:91-95`), so a reader can see an empty record for an instant; that is safe, because `zombieOwner` is false for a zero record and `verifySignalOwner` refuses on any change. | `client.go:160-175`, `spawn.go:310-327`, `spawn.go:364-377`, `lock.go:91-95` |
| P5 | **R2 signals through one pidfd.** A new Linux function `evictLeaderProcess(paths, owner, wait)` in `proc_linux.go` opens the pidfd once, then: `VerifyLeaderProcess`, liveness poll, `verifySignalOwner`, SIGTERM on that fd, `unix.Poll(fd, POLLIN, wait)` for the exit (POLLIN on a pidfd means exit, as `proc_linux.go:82-85` already uses), and when the process still lives: `verifySignalOwner` again (a record that turned `stopping` refuses here), SIGKILL on the same fd, poll again. A reused PID can never be addressed through the original fd. Darwin and other get a stub that returns `ErrNotLeaderProcess`; the connector then keeps today's wait and returns `ErrLeaderStarting` at the deadline. `signalLeaderProcess` and `Stop` do not change. | `proc_linux.go:71-93`, `proc_darwin.go:37-39`, `proc_other.go:16-18`; only caller of `signalLeaderProcess` `client.go:474` |
| P6 | **R2 budget.** Waits after a signal are `min(EvictWait, remaining(ctx))`, `EvictWait` default 8 s. One SIGTERM and at most one SIGKILL per owner (PID + start + instance) per call. State is per call; the Grok budget "3 attempts per PID" is not ported, because the 20 s deadline holds one sequence and a later call evaluates again from the lock file. A call that ends during eviction returns `ErrLeaderStarting` with the text "sent SIGTERM to leader pid N that holds the lock without a socket; try again". | `client.go:35` (`defaultConnectDeadline` 20 s), `client.go:186-193` |
| P7 | **R2 test seams.** `ConnectConfig.ZombieAfter` and `ConnectConfig.EvictWait` (typed config, zero means 30 s and 8 s). No environment variable, no CLI flag. | H13b constraint "no test-only product flag, typed config" |
| P8 | **R3 stamp.** `vcs.time` from `debug.ReadBuildInfo` (RFC 3339, the commit time of the revision, not the build time), in a new optional JSON field `commitTime` of `LeaderRegister`, `LeaderRegistered`, `LeaderStatus` and `VersionInfo`. `LeaderProtocolVersion` stays 1: the field is additive, every decoder in `internal/leader` uses plain `json.Unmarshal` without `DisallowUnknownFields`, so an older peer ignores it and a missing field is empty. Two dirty builds of one commit tie and keep the leader; the existing build hint stays for "other build, same stamp". | `cmd/tui/version.go:28-56`, `handshake.go:106` (Accept), `handshake.go:213` (Register), `pkg/protocol/leader.go:58-111`; grep for `DisallowUnknownFields` in `internal/leader`: none |
| P13 | **Dev stamp (N3 B).** Field `devStamp` (optional, additive, same four types as `commitTime`), text `<modification time, RFC 3339 with nanoseconds, UTC> sha256:<64 hex>`. A binary has `commitTime` or `devStamp`, never both: `devStamp` is computed only when `vcs.time` is absent. Computed once per process with `sync.OnceValue` from `os.Executable()`: `os.Stat` modification time, then SHA-256 of the file. The leader computes it as the first step of `serveLeader`, before the lock and the composition, because a `go run` binary can be removed after `go run` ends while the leader still runs (it never reads its file later). A failure to read the executable gives an empty stamp (unknown: keep). `ask version --json` computes it the same way, so the probe of the binary to start reports it. | `cmd/tui/version.go:28-56`, `cmd/tui/leader.go:204-205` (first steps of `serveLeader`), `spawn.go:207-230` |
| P9 | **R3 rule.** One comparison `newerBuild(client, leader)` over the pair (`commitTime`, `devStamp`): both `commitTime` set → later commit time wins; both have only `devStamp` → same hash keeps, a different hash with a strictly later modification time replaces; any other pair (one VCS and one dev build, empty, unparseable) keeps. Replace only when `spawn` is true, no replacement happened in this call yet, `newerBuild(Hello, registered)` is true, **and** the binary that this call would start (`leaderExecutable` + `ProbeAsk`) is also newer by `newerBuild(probe, registered)`. The probe runs before the old leader is asked to stop, so a binary that cannot start never costs the old leader. | `client.go:198-220`, `spawn.go:207-230` |
| P10 | **R3 mechanism needs no leader change.** On a same-protocol registered connection, `controlFrame` accepts a shutdown without `IfIdle` (refusal only when `management && !IfIdle`, `server.go:371`) and checks `SpawnedByClient` only with `IfIdle` (`server.go:374`). So the client sends `status`, checks `SpawnedByClient` itself (N1 A), sends `shutdown{instanceId}` without `IfIdle` through `manage`, closes its connection and reuses the lock-free wait of `replace`, extracted as one helper. Already-deployed leaders accept it. | `server.go:356-386`, `client.go:288-323`, `client.go:327-359` |
| P11 | **R3 thrash bound.** One replacement per call (the existing `replaced` flag). After it, the call adopts whatever leader serves, also an older one that another client started in the same window, and prints the build hint. An older client never replaces a newer leader. A reconnect is also a call: `tuiclient.LeaderDial` dials through `ConnectOrSpawn`, so a newer TUI that lost its link replaces an older leader at its next reconnect too (R2 also runs there). This is where "adopt an older leader now, replace it at the next reconnect" can happen. | `client.go:141,150-159`; reconnect path `internal/tuiclient/client.go:69,240-279` |
| P12 | **Hello helper.** `cmd/tui` builds every register frame through one helper `leaderHello(kind)` that sets `Build`, `CommitTime` and `DevStamp`. Today three sites build it by hand. | `cmd/tui/connect.go:556`, `cmd/tui/tui_client.go:61`, `cmd/tui/leader.go:123` |

## Needs decision

| # | Question | Options (recommended first) | Affects |
|---|---|---|---|
| N1 | **Decided (user, 2026-10-11): A, keep.** Does R3 keep the `SpawnedByClient` restriction (never replace a leader that the user started by hand or under a supervisor)? | **A (recommended): keep.** A supervisor (launchd, systemd, a terminal the user watches) restarts its own binary; a forced stop would make it start the old build again, and the client would replace it again on its next connect: a loop that kills sessions each time. The client prints the hint instead. B: drop it; a newer client also stops a supervised leader. C: keep it, but allow a forced stop of a supervised leader only when it is idle (today's `IfIdle` path). | Phase 5 (`replaceOlder`), test `TestNewerBuildKeepsASupervisedLeader` |
| N2 | **Decided (user, 2026-10-11): A, keep `IfIdle`.** When the protocol integer differs and the client is newer (the existing replacement path), does that path also become forced (busy leader stopped), for consistency with R3? | **A (recommended): no, keep `IfIdle`.** Every leader that exists today refuses a shutdown without `IfIdle` on a management connection (`server.go:371`), so a forced request can never work against them; a "yes" governs only a future protocol skew and needs a leader-side change. B: yes, make it forced when the client is newer: change `server.go:371` and `replace` at `client.go:307`; effective only from the next protocol bump. C: defer until protocol 2 exists. | Phase 5 scope; H13b supersession note wording |
| N3 | **Decided (user, 2026-10-11): B.** Dev stamp in scope: phase 4 (fields, computation), phase 5 (comparison, replacement); rules in P13 and P9. Original question: `go run` builds have no VCS stamp (build `dev`, no `commitTime`), so R3 never replaces a leader started by an older `go run`. Add a dev stamp (executable mtime plus content hash)? | **A (recommended): no; out of scope.** An mtime stamp makes two terminals that each `go run` the same source replace each other's leader on every start (each new temporary binary is "newer"), which kills sessions. The build hint and `ask leader stop` stay the dev workflow. B: dev stamp from executable mtime plus hash, compared only between two dev builds. C: a dev stamp that only replaces an idle leader. | Phase 4, phase 5 |

## Phases

| # | Phase | Status | Depends on | Effort |
|---|---|---|---|---|
| 1 | [Spawn retry](phase-01-spawn-retry.md) | completed | - | 0.5d |
| 2 | [Owner state](phase-02-owner-state-and-signal.md) | completed | 1 | 0.75d |
| 3 | [Zombie eviction on Linux](phase-03-zombie-eviction.md) | completed | 2 | 1.75d |
| 4 | [Build stamps on the wire (commit time, dev stamp)](phase-04-commit-time-stamp.md) | completed | 3 | 1d |
| 5 | [Newer-build replacement](phase-05-newer-build-replacement.md) | completed | 4 | 1.5d |
| 6 | [Linux proof, docs and supersession note](phase-06-linux-proof-and-docs.md) | completed | 5 | 0.5d |

All phases are sequential. `internal/leader/client.go` changes in phases 1, 3 and 5; `cmd/tui/leader.go` in phases 2 and 4. No parallel group.

## File ownership

| Phase | Owns (create or modify) |
|---|---|
| 1 | `internal/leader/client.go` (start loop), `internal/leader/client_test.go`, `cmd/tui/leader_spawn_retry_e2e_test.go` (new) |
| 2 | `internal/leader/lock.go`, `internal/leader/lock_test.go`, `internal/leader/spawn_test.go` (owner record cases), `internal/leader/server.go` (`Config.Stopping`, `Close`), `internal/leader/server_test.go`, `internal/app/module_leader.go` (`LeaderParams.Stopping`), `internal/app/leader_lifecycle_test.go`, `cmd/tui/leader.go` (owner state, stopping hook), `cmd/tui/leader_regression_test.go` |
| 3 | `internal/leader/evict.go` (new), `internal/leader/proc_linux.go`, `proc_darwin.go`, `proc_other.go` (`evictLeaderProcess`), `internal/leader/evict_test.go` (new), `internal/leader/evict_linux_test.go` (new, `//go:build linux`), `internal/leader/evict_darwin_test.go` (new, `//go:build darwin`), `internal/leader/helper_process_test.go` (new), `internal/leader/main_test.go`, `internal/leader/client.go` (absent branch, config fields), `cmd/tui/leader_evict_e2e_test.go` (new), `cmd/tui/leader_evict_linux_e2e_test.go` (new, `//go:build linux`) |
| 4 | `pkg/protocol/leader.go`, `pkg/protocol/leader_test.go`, `internal/leader/handshake.go` (`ServerConfig.CommitTime`, `ServerConfig.DevStamp` into registered), `internal/leader/router.go` (status), `internal/leader/handshake_test.go`, `cmd/tui/version.go` (commit time, dev stamp), `cmd/tui/version_test.go`, `cmd/tui/leader.go` (server config, stamp first in `serveLeader`, `leaderHello`), `cmd/tui/connect.go`, `cmd/tui/tui_client.go`, `cmd/tui/dev_binary_test.go` (new: DEV-OLD, DEV-NEW, DEV-COPY helpers), `plans/reports/tester-261011-*-dev-stamp-facts.md` |
| 5 | `cmd/tui/tools_leader_e2e_test.go` (`startToolLeader` takes the leader binary through a small `startToolLeaderFrom`; old callers unchanged), `internal/leader/client.go` (replacement), `internal/leader/stamp.go` (new: `newerBuild`, dev stamp parse), `internal/leader/stamp_test.go` (new), `internal/leader/client_test.go`, `cmd/tui/leader_replace_e2e_test.go` (new), `cmd/tui/leader_dev_replace_e2e_test.go` (new) |
| 6 | `internal/leader/README.md`, `pkg/protocol/README.md`, `docs/ask-architecture-reference.md` (7.3 lines 282-307), `CLAUDE.md` (TUI line 68, leader line 79, version line 81), `plans/261008-1033-h13b-leader-unix-socket/plan.md` (decision table note only), `plans/reports/tester-*-leader-connect-or-spawn-linux.md` (evidence) |

## Data flow

```
ConnectOrSpawn(cfg) ── attempt: dial + register ──┬─ outOK ── [R3] newerBuild (commit time | dev hash+mtime)? ── probe exe ── status ── spawnedByClient?
                                                  │                 └ no ── hint, return client      └ forced shutdown ─ wait free ─ loop
                                                  ├─ outVersion ── replace (IfIdle, unchanged) ─ loop
                                                  └─ outAbsent ── LockHeld?
                                                        ├ held ── [R2] owner serving, age >= 30 s, verified?
                                                        │            ├ yes (Linux) ── SIGTERM ─ wait 8 s ─ SIGKILL ─ wait 8 s ─ loop
                                                        │            └ no / refused (macOS) ── wait (today)
                                                        └ free ── child? none: start (exe probed once)
                                                                   exited with error: [R1] starts < 3 ? start again : ErrStartupFailed + log tail
```

Data in: the socket dial result, the register reply (`build`, `commitTime`, `devStamp`), the lock state, the owner record (`pid`, `start`, `instance`, `exe`, `state`), `ask version --json` of the binary to start. Data out: a registered `*Client`, or a typed error (`ErrStartupFailed`, `ErrLeaderStarting`, `ErrVersionRefused`, ...). Side effects: started leader processes, a forced shutdown request, signals to a verified leader process.

## Test matrix

| Level | What | Where |
|---|---|---|
| Unit (fake leaders, `StartFn`, real sockets in a temp home) | retry count and log tail; no retry for probe and exec errors; zombie decision table; stamp order table (commit time and dev stamp); replacement keeps/replaces | `internal/leader/client_test.go`, `evict_test.go`, `stamp_test.go` |
| Real process (helper process from the test binary, real lock, real pidfd) | SIGTERM and SIGKILL eviction; stopping leader and unverified owner never signalled; darwin refusal | `internal/leader/evict_linux_test.go`, `evict_darwin_test.go` |
| Integration | `Server.Close` calls the stopping hook before listeners close; the leader writes `serving`, then `stopping` | `internal/leader/server_test.go`, `cmd/tui/leader_regression_test.go` |
| E2E (built `ask`, `acpBinary` pattern) | rows R1, R2, R3 below | `cmd/tui/leader_*_e2e_test.go` |
| Linux proof | Linux-only rows in a container | phase 6 |

What cannot be proven on macOS: SIGTERM and SIGKILL eviction (no pidfd; signals are refused by design). These rows run in a Linux container (phase 6): `GOOS=linux GOARCH=arm64 go test -c ./internal/leader` run in `alpine:3` (the H13b method, `plans/reports/worker-261009-1057-router-safety.md:38-39`), and the `cmd/tui` E2E in a `golang` image with `GOFLAGS=-buildvcs=false` (the worktree `.git` file points outside the mount). There is no CI in this repo (no `.github`), so the container run is the Linux evidence, recorded in the phase 6 report.

## Backwards compatibility and rollback

- Wire: `commitTime` and `devStamp` are optional and additive; `LeaderProtocolVersion` stays 1. A pre-change leader has neither field, so a new client keeps it (unknown stamp). A pre-change client ignores the field.
- Owner record: the fifth line is optional. A new reader sees a 4-line record as state "" and never evicts it. A pre-change reader reads only four lines (`lock.go:136-151`), so it ignores the fifth.
- Behavior changes for users: an older, client-started leader is stopped by a newer client even when busy (user choice C), for VCS builds by commit time and for dev builds by a different hash with a newer modification time (N3 B); on Linux a leader without a socket is stopped. `Connect`, `Status`, `Stop` and `ask leader status|list|stop` keep their behavior.
- Rollback per phase: each phase is one commit; revert it. Phase 2 has a one-way effect only on the lock file content, which an older binary ignores. Phase 4 is wire-additive. Phase 5 can be reverted alone to get back the hint-only behavior for same-protocol builds. No data migration; nothing persists except the lock file text.

## Risks

| Risk | Likelihood x impact | Mitigation |
|---|---|---|
| Signal to a wrong process (PID reuse, a starting leader with a stale record from a crash) | Low x Critical | Signal only through `evictLeaderProcess`: one pidfd opened first and used for both signals, `VerifyLeaderProcess` (uid, command line, start time within 10 s), poll that the pidfd is still live, `verifySignalOwner` (lock still held, record unchanged) right before each signal. A crash leaves a record whose PID is dead or reused: verification fails. Tests `TestZombieWithUnverifiedOwnerIsNotSignalled`, `TestStoppingLeaderIsNeverEvicted`, existing `TestSignalFallbackRefusesChangedLifetimeOwner`. |
| R2 kills a leader that drains after a stop | Medium x High without P3 | `stopping` state written before the listeners close; the zombie rule needs `serving`; a SIGKILL after SIGTERM is refused when the record changed to `stopping`. Test `TestStoppingLeaderIsNeverEvicted`. |
| R3 forced replacement kills busy work | Certain when it fires x High | Accepted by the user (choice C, and N3 B for dev builds). Limits: only a strictly newer, probed binary; only a client-started leader (N1 A); once per call; the consequence is in the README and CLAUDE.md. E2E `TestLeaderE2ENewerBuildReplacesBusyOlderLeader` and `TestLeaderE2ENewerDevBuildReplacesBusyOlderDevLeader` prove the jobs end (no orphaned process group). |
| Thrash between two builds | Medium x High | Never downgrade; one replacement per call; an older client never replaces. Remaining window: an older TUI that reconnects at the instant the lock frees can start its own older leader, which the newer call then adopts (P11). Documented; `ask leader stop` resolves it. |
| Clock skew in the stamp (commit time from another machine, a rebase gives older code a later time) | Low x Medium | Strict "later" only; equal or unparseable keeps; the probe of the binary to start must agree. Documented as commit time, not build time. |
| Dev stamp: modification time oddities (a copy with `cp -p`, `touch`, a clock change, the Go build cache that reuses a `go run` binary and can refresh its modification time) | Medium x Medium | Same hash always keeps, so the same source never replaces itself. A different hash with a later time means "the binary written last wins", which can be older source (for example a cached binary of an old branch). Phase 4 checks first how `go run` of the installed Go places and dates its binary and records the facts in its report; the README says the rule in plain words; `ask leader stop` stays the manual fix. |
| Dev stamp: two worktrees or branches that share one `ASK_HOME` | Medium x Medium | The binary written last wins once; the older side never replaces back until it is written again (new source). No loop: each replacement needs a newer write. Documented. |
| Dev stamp: hash cost on a large binary | Low x Low | SHA-256 of one executable (tens of MB) once per process, only for dev builds; `ask version --json` (the probe) pays it once, inside the 15 s probe bound. Phase 4 measures it on the built binary and records the time. |
| Dev stamp: the `go run` binary is removed while its leader runs | High x Medium without a fix | The leader computes its stamp as the first step of `serveLeader` and never reads its file again; a failure to read gives an empty stamp (keep). Test `TestLeaderDevStampSurvivesARemovedBinary`. |
| R2 eviction does not fit the 20 s deadline | Low x Low | Waits bounded by the remaining time; a clear `ErrLeaderStarting` text; the next call evaluates again (P6). |
| A pre-change leader without the state line drains for a long time | Low x Low | Never evicted (state ""), so today's behavior. |

## Acceptance

- Every row of the Runtime Flow Proof Matrix has its named test, written first and seen failing, now passing. Linux-only rows pass in the container run of phase 6 and the report records the command and output.
- `go test ./internal/leader ./cmd/tui ./pkg/protocol ./internal/app`, `go test ./...`, `go vet ./...`, `GOOS=linux go vet ./internal/leader ./cmd/tui`, and `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` pass.
- Dev stamp: two terminals with the same dev binary keep one leader; a dev build with other bytes and a later modification time replaces a busy dev leader; a dev build and a VCS build never replace each other (the dev rows of the matrix pass). Phase 4 records the measured hash time and the `go run` binary facts.
- No test leaves a leader process or a process group behind (`leaderProcesses(bin)` empty; group ESRCH).
- No code comment, test name or commit message holds a plan, phase, row or decision label (R1, N1, P3, H13b, ...).
- Docs listed in phase 6 describe the implemented state; the H13b decision table carries the supersession note.

## Validation Log

### Runtime Flow Proof Matrix

Status: every row passed on macOS. Rows marked "Linux container" also passed in the Linux container run (see the Review section).

Entry points. DEV-OLD and DEV-NEW = two dev builds of `cmd/tui` (no VCS stamp) that a test helper writes with `go build -buildvcs=false` (DEV-OLD) and `go build -buildvcs=false -trimpath` (DEV-NEW): two real binaries of the same source whose bytes differ because `-trimpath` changes the embedded file paths. No product flag and no injected value: each binary computes its stamp with the production code. The test sets the modification times with `os.Chtimes` (DEV-OLD one hour earlier). DEV-COPY = a byte copy of DEV-OLD under another name with a later modification time (same hash). BIN = the built `ask` (`acpBinary`, `cmd/tui/acp_e2e_test.go:47`) as leader, with an isolated short `ASK_HOME` (`leaderHome`). CALL = `leader.ConnectOrSpawn` from the test process with `e2eConnect(home, bin)` (`cmd/tui/leader_e2e_test.go:37`), the same call the TUI makes through `tuiclient.LeaderDial` (`internal/tuiclient/client.go:69`). SPAWN = the compiled `spawncaller` (`cmd/tui/leader_e2e_test.go:196`) with a sibling `ask` wrapper script, so `FindAsk` picks the wrapper. HELPER = the `internal/leader` test binary started again as `<test binary> leader` with a helper environment value; it takes the real lock and writes a real owner record (command line passes `isLeaderCommandLine` through `Owner.Exe`).

| Feature | Actor | Runtime trigger | Entry point | Internal path | Observable result | End-to-end test | External mocks | Prepared data | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R1 retry after early exit | Client process | first two leader starts exit with status 1 | SPAWN → BIN | `run` → `start` → child exits with error → start again with the probed exe | third start serves; caller prints the instance id; wrapper counted 3 `leader` runs, 1 `version` run | `TestLeaderE2ESpawnRetriesAfterEarlyExit` | none | wrapper `ask` that fails `leader` twice, then `exec`s BIN | PASSED |
| R1 give up after 3 starts | Client process | every start exits with status 1 | SPAWN → wrapper | same; third failure ends the call | exit 1, `the leader did not start`, `after 3 starts`, log tail of the last start; 3 `leader` runs; no leader process | `TestLeaderE2ESpawnGivesUpAfterThreeStarts` | none | wrapper that always fails `leader` with a numbered line on stderr | PASSED |
| R1 no retry for a bad binary | Client | binary reports another protocol | CALL (unit) | `start` → `ProbeAsk` error | `ErrAskIncompatible`, `StartFn` never called | `TestConnectOrSpawnIncompatibleAsk` (existing), `TestConnectOrSpawnDoesNotRetryAStartError` | fake `ask` | protocol 9 script | PASSED |
| R2 evict a leader without a socket (Linux) | Client | the socket file of a serving leader is removed | CALL vs BIN | `outAbsent` + held → owner `serving`, age ≥ `ZombieAfter` (set to 0) → verify → SIGTERM by pidfd → lock free → start | new instance id; old PID gone; new socket | `TestLeaderE2ELeaderWithoutSocketIsEvicted` (Linux branch) | none | running leader, `os.Remove(socket)` | PASSED (Linux container) |
| R2 SIGKILL escalation (Linux) | Client | a leader without a socket that does not handle SIGTERM | CALL vs BIN | as above → `EvictWait` (1 s) → record unchanged → SIGKILL by pidfd → start | old PID gone (killed); new instance id | `TestLeaderE2EFrozenLeaderWithoutSocketIsKilled` (`//go:build linux`) | none | socket removed, leader SIGSTOPped | PASSED (Linux container) |
| R2 refusal on macOS | Client | as the first R2 row | CALL vs BIN | → `signalLeaderProcess` refuses → wait | `ErrLeaderStarting` after the deadline; old leader alive; lock held; no start | `TestLeaderE2ELeaderWithoutSocketIsEvicted` (non-Linux branch), `TestZombieLeaderIsNotSignalledWithoutAStableHandle` | none | as above | PASSED |
| R2 stopping leader not evicted | Client | a leader holds the lock in state `stopping` without a socket | CALL vs HELPER | owner state `stopping` → no signal | `ErrLeaderStarting`; helper alive; no `StartFn` call | `TestStoppingLeaderIsNeverEvicted` | none | helper writes state `stopping` | PASSED |
| R2 leader writes its state | Leader | serve, then shutdown | `serveLeaderRuntime` (real runtime, in process) | `WriteOwner(serving)` after listen; `Server.Close` → `Stopping` hook → `MarkStopping` | record `serving` while it serves; `stopping` before the socket refuses | `TestLeaderRecordsStoppingBeforeItStopsServing` | none | none | PASSED |
| R3 newer build replaces a busy leader | Client of a newer build | connect while an older-stamp leader runs a background job | CALL (`Hello.CommitTime` later, wrapper reports a later stamp) vs BIN | `outOK` → newer → probe → status (`spawnedByClient`) → forced shutdown → wait free → start → register | old PID gone; job process group gone (ESRCH); new instance id; the old peer's link closed | `TestLeaderE2ENewerBuildReplacesBusyOlderLeader` | model HTTP (provider fixture, scripted `bash` job) | `startToolLeader`, background `sleep 300` job | PASSED |
| R3 older, equal or unknown stamp keeps the leader | Client | connect with an older, equal, empty or bad stamp | CALL (unit, harness leader) | `outOK` → not newer | same instance; no `StartFn`; hint for another build | `TestNewerBuildDecision`, `TestOlderOrUnknownBuildKeepsTheLeader` | none | harness with `CommitTime` | PASSED |
| R3 binary to start is not newer | Client | client stamp later, probed binary older | CALL (unit) | probe → keep | same instance; hint | `TestNewerClientKeepsTheLeaderWhenTheBinaryIsNotNewer` | fake `ask` | version script with an older stamp | PASSED |
| R3 supervised leader kept (N1 A) | Client | leader started by hand | CALL (unit) | status → `spawnedByClient=false` → keep | same instance; hint names the supervisor | `TestNewerBuildKeepsASupervisedLeader` | none | harness `SpawnedByClient=false` | PASSED |
| Protocol mismatch stays idle-only (N2 A) | Client of a newer protocol | busy older-protocol leader | CALL (unit) | `outVersion` → `replace` with `IfIdle` | refused, hint "busy"; leader alive | `TestReplaceOnlyIdleSpawnedLeader` (existing, unchanged) | none | harness protocol 1, busy | PASSED |
| R3 once per call | Client | the new leader is again older than the client | CALL (unit) | replaced flag | one stop, one start, the call returns the adopted leader | `TestNewerBuildReplacesOncePerCall` | none | `procLeader` with an older stamp | PASSED |
| `ask version --json` carries the stamp | Client before a start | probe | BIN | `runVersion` | JSON has `commitTime` equal to `vcs.time` of the binary, no `devStamp` | `TestVersionJSONCarriesCommitTime` | none | built binary (VCS stamped) | PASSED |
| Dev build reports a dev stamp | Client before a start | probe | DEV-OLD | `runVersion` → dev stamp from its own file | JSON has `devStamp` with the file modification time and the SHA-256 that the test computes; no `commitTime` | `TestVersionJSONCarriesDevStamp` | none | DEV-OLD | PASSED |
| Dev leader keeps its stamp after its file is removed | Leader started from a temporary binary | the binary file is removed after start | DEV copy as leader | stamp computed first in `serveLeader` | `ask leader status --json` (DEV-OLD) still shows the original `devStamp` | `TestLeaderDevStampSurvivesARemovedBinary` | none | temp copy of DEV-OLD, removed after start | PASSED |
| Newer dev build replaces a busy older dev leader | User runs a rebuilt `go run` | `ask connect` of DEV-NEW while a DEV-OLD leader runs a background job | DEV-NEW `connect` process (production `leaderHello` computes its stamp) vs DEV-OLD leader | `outOK` → `newerBuild` (dev, other hash, later time) → probe DEV-NEW → status → forced shutdown → start DEV-NEW → register | `connected instance=` shows a new id; old PID gone; job process group gone; `ask leader status --json` shows the `devStamp` of DEV-NEW | `TestLeaderE2ENewerDevBuildReplacesBusyOlderDevLeader` | model HTTP (provider fixture, scripted `bash` job) | DEV-OLD, DEV-NEW, background `sleep 300` job | PASSED |
| Same dev build in two terminals keeps the leader | Second terminal runs the same source | `ask connect` of DEV-COPY (same bytes, newer modification time) | DEV-COPY `connect` vs DEV-OLD leader | `newerBuild` → same hash → keep | same instance id; old PID alive | `TestLeaderE2ESameDevBuildKeepsTheLeader` | none | DEV-COPY | PASSED |
| Dev and VCS builds are never compared | Client | dev client vs VCS leader, and VCS client vs dev leader | CALL (unit) | `newerBuild` → mixed pair → keep | same instance; no start | `TestDevAndVCSBuildsAreNeverCompared` | none | harness stamps | PASSED |

## Unresolved questions

1. The E2E for R3 needs a VCS-stamped `acpBinary` (`go build` inside the git worktree). If the worktree cannot be stamped on a machine, the test fails with a clear message instead of a skip; confirm that this is acceptable.

Status of this plan: implemented; see the Review section.

## Review

Implemented on branch `new-session`, in order: spawn retry, owner state line and stopping hook, Linux eviction, build stamps on the wire, newer-build replacement, docs.

Evidence:

- macOS: `go test ./...`, `go vet ./...`, `GOOS=linux go vet ./internal/leader ./cmd/tui`, golangci-lint (0 issues).
- Linux (container `alpine:3`, kernel 6.12 linuxkit, test binary of `./internal/leader`, whole suite): all pass, including the SIGTERM, SIGKILL, unverified-owner, stopping and turns-stopping eviction tests. Linux E2E of `cmd/tui` in `golang:1.27`: eviction, SIGKILL escalation, spawn retry, dev replacement and stop fallback pass. Commands and results: `plans/reports/tester-261012-0030-leader-connect-or-spawn-linux.md`.
- The VCS-stamped E2E (`TestVersionJSONCarriesCommitTime`, `TestLeaderE2ENewerBuildReplacesBusyOlderLeader`) ran on macOS only, because the container has no VCS stamp.
- Dev stamp facts (`go run` placement, hash time): `plans/reports/tester-261011-2352-dev-stamp-facts.md`.

Deviations from the plan text, none that change a decision:

- The `ask leader status --json` test of `cmd/tui` lists the status keys; it now allows the optional `commitTime` and `devStamp`.
- The zombie tests set `ZombieAfter` to one nanosecond: zero means the 30 s default, as `withDefaults` does for every duration field.
- `evictLeaderProcess` also takes the call deadline, so the two waits never run past it.
- The helper process of the Linux unit tests must run on a container-local file system. A bind-mounted host `/tmp` does not give `flock` semantics, so run the test binary with a single-file mount.

### Requirement review, 2026-10-12

The [requirement review](../reports/review-261012-0120-leader-connect-or-spawn-port.md) checked all six phases and found four defects in the connection and eviction safety boundaries.
The repairs preserve a leader on a read-only protocol mismatch, adopt a concurrent replacement, and stop signal escalation when the deadline expires or the context is cancelled.
Each defect has a failing-before and passing-after built-binary E2E test.
The report records the requirement checks, final validation and limits of the historical test-first evidence.
