---
phase: 3
title: "App link and ACP route context"
status: pending
priority: P1
effort: "1.5d"
dependencies: [1, 2]
---

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
