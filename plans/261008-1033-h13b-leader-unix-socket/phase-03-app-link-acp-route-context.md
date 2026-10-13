---
phase: 3
title: "App link and ACP route context"
status: completed
priority: P1
effort: "1.5d"
dependencies: [1, 2]
---


## Current repair state (2026-10-09)

[host.go](../../internal/acp/host.go) checks driver generation at each mutation commit boundary.
Model readiness occurs before that final check, so a completed take prevents the old driver from committing the prepared change.
Authenticate registers work with host admission.
The host checks idleness under the admission write lock and sets the quiesce flag only after the check succeeds.
The router also checks requests that it accepted before SDK admission.
[driver_commit_test.go](../../internal/acp/driver_commit_test.go) checks model preparation, cancel and the other driver mutations, auth admission, run admission and take ordering.
[leader_frame_test.go](../../internal/app/leader_frame_test.go) sends frames above 10 MiB in both directions through the real shared host.
It also sends an exact 64 MiB socket frame with 64 KiB capabilities, then checks that an oversized client cannot stop another client or the common link.
[router_safety_test.go](../../internal/leader/router_safety_test.go) checks transformed lines at the internal size boundary.
The focused app race checks passed in 36.182 s.
The final exit gates passed; the [repair report](../reports/pm-261009-h13b-repairs.md) records their evidence and accepted limits.

# Phase 3: App link and ACP route context

Outline.
Scout `internal/acp` and `internal/app` at cook time; read the Phase 1 spike result and Phase 2 router contracts first.

## Goal

`internal/app` builds the leader composition: one shared ACP adapter and host behind one internal byte link, the leader server on top, and the idle callback; `internal/acp` consumes the typed route context and serves the live methods, without changing editor stdio behavior.

## Files to Create / Modify (candidate)

- Create: `internal/app/module_leader.go`, `internal/app/module_leader_test.go`
- Create: `internal/app/leader_routing_test.go` (real leader server over a real Unix socket into the real adapter and host; `internal/app` may import both packages)
- Modify: `internal/acp/agent.go` (read `ACPRouteMeta`; driver capabilities per session; stdio path unchanged when meta is absent)
- Modify: `internal/acp/host.go` (driver generation per session; atomic take; `QuiesceIfIdle`)
- Modify: `internal/acp/ask_methods.go` (`_ask/session/take`; adapter-side checks of driver generation on mutations)
- Modify: `internal/acp/updates.go` (only if Follow needs an owner hint; prefer router ownership)
- Possibly extract shared link setup from `internal/acp/stdio.go` into an exported helper used by both stdio and app (keep `ServeStdio` behavior byte-identical)

## Contracts

- App owns: pipes in both directions, SDK `AgentSideConnection`, `CheckedWriter`, `LineLimitReader`, binding, quiet logger, native auth, cleanup.
- The internal link closes only on leader stop or internal link failure.
  Internal link failure is leader-wide: `Adapter.Fail`, admission closed, clients told `leader_shutting_down`.
- A client socket loss never reaches `Adapter.Fail`, `Adapter.Close` or `Host.latch`.
- The host is the single driver-generation authority: it sets generation 1 on session creation, commits every take atomically (reject busy with a live foreign driver), and returns the new generation.
  Mutations carry the generation in route meta; a stale generation is rejected atomically inside the host (prevents a former driver's in-flight prompt winning after a take).
  Without route meta (editor stdio) no generation check runs.
- `list_live`, `attach`, `detach` are router-handled; `take` is router-checked and adapter-committed.
- Durable `session/load` and `session/close` stay unsupported.
- Leader composition passes `fx.ValidateApp`; editor and headless compositions unchanged and still pass theirs.

## TDD (RED list)

- `TestLeaderModuleValidateApp`
- `TestLeaderClientLossDoesNotFailHost` (dedicated regression: kill a client socket mid-run, assert host not latched, run settles)
- `TestHostTakeBusyWithLiveDriverRejected`, `TestHostTakeIdleBumpsGeneration`, `TestHostStaleGenerationMutationRejected`
- `TestQuiesceIfIdleAtomic` (no admission between check and close)
- `TestAdapterRouteMetaCapabilitiesPerSession` (two clients, different capabilities, separate sessions)
- `internal/app/leader_routing_test.go`: re-run the Phase 2 routing scenarios (initialize cache, driver rules, take generation, Follow ownership, slow client) through a real socket, the real leader server and the real host
- All existing `internal/acp` tests and H13a conformance unchanged

## Verification

```sh
go test -race ./internal/acp/... ./internal/app/... ./internal/leader/... -count=1
go test ./cmd/tui/ -run 'ACP' -count=1
```

## Risks

- Accidental change to stdio barrier: protected by existing H13a E2E (`TestACPE2EPromptIncludesFollowCompletion`).
- Generation check missing on one mutation method: table-driven test over all driver-only methods.

## Admission and driver state

Generation checks must use the same admission lock as the mutation or take commit.
Do not check a generation, release the lock, and then call an unguarded Agent mutation.
Track admitted model/auth operations and session creation, including the factory call before session publication, in the host idle check.
Count queued follow-up or steering work and pending reverse calls as busy.
Do not hold the host map lock during a factory, auth call, socket write or Agent drain.
Keep lock order explicit: host admission, then session admission; no reverse acquisition.

Serialize takes per session until each host result is applied to the router.
Fence client loss and detach against a pending take; an absent result recipient must not become a live driver.
The router applies the host result before forwarding a new driver's mutation.
Send decimal text for `driverGen` through SDK `_meta` and reject missing or malformed fields on the leader link.
A malformed present route context must not fall back to the trusted editor path.
Keep the no-meta editor path only for the app-owned stdio composition.

The 64 MiB parser gate from Phase 1 must pass before this composition is usable.
Test frames above 10 MiB in both directions through the real server and real adapter.
Test transformed input against the 65 MiB line cap before it reaches the shared stream.
A client-side size error must leave another client's active run and the common link usable.
Keep the existing host event-queue safeguards; the user decision removes socket FIFO and aggregate bounds, not the H13a host safeguards.
The common link reader must keep draining into socket queues when a client stops reading.

Add deterministic tests for two concurrent takes, take racing every driver-only mutation, driver loss while take waits, new-session factory racing idle shutdown, auth/model change racing idle shutdown, generation above 2^53, malformed route meta, and slow-client input that does not overflow the host event queue.

## Historical review (2026-10-08)

### What was built

- **SDK parser gate closed.** `third_party/acp-go-sdk` holds the non-test Go files, `go.mod`, `LICENSE`, `version` and the `schema` folder of upstream v0.13.5, with one change in `connection.go`: the scanner maximum is `(65 << 20) + 1`. A relative `replace` line in `go.mod` selects it. `third_party/acp-go-sdk/PATCH.txt` records source, version, module sum, licence, the change, the reason and the update steps. The note is plain text because markdown files belong in `plans/` and `docs/` only. `TestSDKAcceptsLeaderLineLimit` passes a line at the limit and fails one byte above it; it fails when the patch is removed.
- **`internal/acp`.** `route.go` parses the route context from the `_meta` of standard calls and from the params of `_ask` calls. A route that is present but malformed is refused on every link, so it never falls back to the editor path. `Config.RequireRoute` makes the leader link refuse a call without a route. The host owns the driver generation: it starts at 1, `Session.guard` checks it and counts the change as in flight under the admission lock, and `Session.Take` commits a take under the same lock (refused as busy only when the caller says a live driver exists). `session/new` returns the generation in its result `_meta` when the call carried a route; the editor result is unchanged. `Host.QuiesceIfIdle` closes admission and counts sessions being built, runs, queued inputs and changes in flight. `Adapter.Session`, `QuiesceIfIdle` and `ActiveRuns` give the composition what it needs.
- **`internal/app/module_leader.go`.** `LeaderModule` and `NewLeaderRuntime` build one adapter and host behind one internal pipe link, with the `leader.Server` on top. `Serve` starts the router and serves a listener. `Stop` runs in this order: server close (clients told, link closed), host disposal (started tool bodies drain), credential refresh drain.
- **`internal/leader`.** The control path asks the router to quiesce on the router goroutine. An open question blocks the idle shutdown, and after a quiesce the router takes no new request.

### Evidence

| Check | Result |
|---|---|
| `go test -race ./internal/acp ./internal/app ./internal/leader -count=3` | pass |
| `go test ./cmd/tui/...` (existing H13a editor and headless E2E, 60 s) | pass |
| `golangci-lint run ./...` | 0 issues |
| `go vet ./...` | pass |
| Real server, real socket, real adapter and host, faux model (`internal/app/leader_routing_test.go`) | independent sessions, shared run with an observer, take and generation, private Follow, client loss during a run, idle shutdown through a version-mismatched client, stop disposes sessions |

### Changes from the outline

- The conformance test of H13a asserted that the SDK folder name ends with `@v0.13.5` (module cache layout). It now reads the `version` file of the copy and accepts the `third_party/acp-go-sdk` location. The pinned identity is unchanged.
- The original flag-first idle check was replaced after review.
  The current check holds the admission write lock, checks work first and sets the flag only when idle.
- The route is validated once for all `_ask` methods in `HandleExtensionMethod` and travels to the handlers in the context.

### Open items passed on

- The command that listens on `~/.ask/leader.sock`, takes the lock, spawns and manages the leader is Phase 4.
- Pending reverse calls from the host do not exist yet; the router's own open questions block an idle shutdown.
