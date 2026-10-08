---
phase: 2
title: "Router: routes, drivers, ordered output"
status: pending
priority: P1
effort: "1.5d"
dependencies: [1]
---

# Phase 2: Router: routes, drivers, ordered output

Outline.
Run a scout pass at cook time before writing the step list; confirm the Phase 1 spike result first.

## Goal

An in-process leader server that accepts many registered clients on a real Unix socket and routes ACP between them and one agent byte stream, with per-client initialize state, explicit driver rules, owner-only Follow routing, ordered output, ping, implicit subscribe and pending-question replay.

## Files to Create / Modify (candidate)

- Create: `internal/leader/server.go` (listen, accept loop, handshake, client lifecycle, ping)
- Create: `internal/leader/router.go` (session membership, driver state, pending tables, Follow ownership)
- Create: `internal/leader/writer.go` (per-client unbounded FIFO and single writer)
- Create: tests next to each; a test-only fake agent stream in `internal/leader` test files (scripted NDJSON peer).
  It is a unit fixture only: no acceptance row rests on it.
  Phase 3 re-runs the routing scenarios against the real host from `internal/app`, because depguard (which also checks test files) forbids `internal/leader` tests from importing `internal/acp`.

## Contracts

- **Initialize per client.** The leader runs one link-level `initialize` on the agent link at startup, before it sends `leader_ready`, and caches the result.
  No client request reaches the agent as `initialize`.
  Each client's `initialize` is validated, its capabilities are stored in the router, and it is answered from the cached result.
  Session methods from a client that has not initialized get `-32016`, whatever other clients did.
  This removes the race of a pending first initialize and of a first client that disconnects before the result.
- **Membership.** A successful `session/new` result makes the creator subscriber and driver before the result reaches the client.
  Failure creates no membership.
- **Driver rules (maintainer decision: take allowed when no live driver).** Driver-only: `session/prompt`, `session/cancel`, `_ask/session/{continue,reset,set_model,set_thinking,steer,follow_up,remove}`.
  Observers may call `state`, `usage`, `get_available_models`, `follow`, `unfollow`, `list_live`, `attach`, `detach`.
  An observer's driver-only request gets `-32015`.
  An observer's driver-only notification (`session/cancel` has no id, so no reply is possible) is dropped and logged.
  Route-meta injection covers notifications as well as requests.
- **Authenticate.** `authenticate` only validates the method and calls the host auth check; it keeps no per-connection state (`internal/acp/agent.go:226-243`).
  The router forwards it per client after that client's `initialize`.
  - Every `take` is forwarded to the adapter with route meta `liveDriver: true|false`.
  The host is the only authority for the driver generation: it commits the take atomically and returns the new generation in the take result.
  The router copies that value; it never mints one.
  - No live driver: the host accepts, busy or idle.
  - Live foreign driver: the host rejects when busy (`-32010`) and accepts when idle.
  - Session creation: the host returns generation 1 in the router-visible result meta; the router records it.
  - Driver disconnect: driver becomes absent; run continues; open driver question cancelled (Phase 6).
- **Views.** `/new` and attach change only the caller's view; no message ever replaces another client's session.
- **Route context.** Every forwarded session request gets `_meta[ask.dev/route]` set by the router (client id, driver generation, live-driver flag, driver capabilities); any client-supplied value is overwritten.
  Meta injection re-encodes `params`; only response id bytes are guaranteed byte-exact.
- **Follow ownership.** Record `subscriptionId → clientID` from a successful Follow result.
  Route `_ask/session/event` and `_ask/session/resync` only to that owner.
  Unfollow by another client is rejected.
  On detach or disconnect, the router sends internal unfollow requests for that client's subscriptions.
- **Broadcast.** `session/update` goes to session subscribers only.
  ID-less messages without a session are dropped and logged, never sent to a "last active" client.
- **Ordered output (user decision: per-client FIFO, unbounded as Grok).** One writer goroutine per client with an unbounded queue.
  The router enqueues a client's prompt result after that client's preceding updates.
  A write error or oversized encoded outgoing envelope closes only that client.
  Never return a client envelope-size error as an Agent-link write failure.
  No global lock is held during socket writes.
  Per-session ordering is independent.
- **Liveness.** Answer `ping` with `pong`; treat `disconnect` as a clean detach.
  A socket error removes the client.
- **Implicit subscribe (as Grok).** A client that sends any session-scoped message becomes a subscriber of that session.
  `attach` subscribes without sending.
- **Pending shared-question cache (as Grok).** Cache each open shared question by session; replay it to a client that attaches; evict it when resolved or cancelled.
- **Client loss.** Remove routes, followers, membership.
  Never close the agent stream.

## TDD (RED list)

- `TestRouterUninitializedClientRejected`
- `TestRouterCreatorIsDriver`, `TestRouterObserverMutationRejected`, `TestRouterObserverCancelNotificationDropped`
- `TestRouterTakeForwardedWithLiveDriverFlag`, `TestRouterCopiesHostGeneration`
- `TestRouterClientInitializeAnsweredFromCache`, `TestRouterLinkInitializeBeforeReady`
- `TestRouterNewChangesOnlyCallerView`
- `TestRouterRouteMetaOverwritesClientValue`
- `TestRouterFollowEventsOwnerOnly`, `TestRouterUnfollowByOtherRejected`, `TestRouterDetachUnfollowsOwned`
- `TestRouterSessionUpdateToSubscribersOnly`, `TestRouterOrphanNotificationDropped`
- `TestWriterPromptResultAfterPrecedingUpdates`
- `TestServerSlowClientDoesNotBlockOthers` (non-reading client stays connected; healthy client completes; the slow client later reads every frame in order)
- `TestServerPingPong`, `TestServerDisconnectDetaches`
- `TestRouterImplicitSubscribeOnSessionTraffic`
- `TestRouterReplaysPendingQuestionOnAttach`, `TestRouterEvictsResolvedQuestion`
- `TestServerClientLossKeepsAgentStream`
- goleak on every test; `-race`.

## Verification

```sh
go test -race ./internal/leader/... -count=1
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/leader/...
```

## Risks

- Head-of-line blocking if a lock is held during writes: enforce with the slow-client test.
- Fake agent stream drifting from the real adapter: Phase 3 re-runs the routing tests against the real host.

## Routing rules required before implementation

Use one ordered router input path for client messages, agent messages, and disconnect events.
One writer owns the common agent input stream; complete NDJSON messages cannot interleave.
Do not wait for a prompt result or a socket write while this path holds router state.
Use the existing protocol method constants for a single method-policy table; unknown methods cannot gain driver or subscription authority.
Validate `jsonrpc`, envelope shape, id type, method and object params before changing any route.
Validate each client's ACP version separately from the outer leader version.
A repeated initialize cannot change capabilities while that client has sessions, pending requests or reverse resources.

Implicit subscription happens only after initialization, session existence and operation checks pass.
It does not grant driver authority or change the caller's view.
`detach`, `unfollow`, reverse responses and transport cancellation do not implicitly subscribe.
A detach ends that client's driver role, follows and reverse eligibility for the session, but does not abort the run.
A change of view is committed only after a successful operation; a busy foreign-session switch leaves the old view and follows intact.

Track follow requests before the result, including client id, session and membership generation.
If the caller disconnects or detaches before a successful new-session, take or follow result arrives, process host state and release the orphaned routes or follow before discarding the response.
Do not discard a late Follow result before sending its internal Unfollow.
Keep ended-subscription ownership long enough to preserve repeated Unfollow semantics.
A foreign caller must not use an ended subscription id to bypass the owner check.

On detach, discard queued events for the old membership and close its follows before the acknowledgment is queued.
Frames already written before the acknowledgment may arrive; frames from the old subscription must never arrive after it.
On reattach, use a new membership generation and explicit Follow cut.
Do not resend a prompt on reconnect.
The pending reverse table is the only shared-question cache; Phase 6 completes its answer and cancellation rules.

Add tests for concurrent complete agent-link writes, invalid initialize, repeated initialize, failed implicit subscription, explicit detach staying detached, busy foreign-view switch, late new/take/follow results after disconnect, repeated Unfollow, and stale events after detach then reattach.
