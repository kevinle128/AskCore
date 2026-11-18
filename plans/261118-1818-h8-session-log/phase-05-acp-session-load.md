---
phase: 5
title: "ACP session/load, leader routing, TUI and ask acp"
status: pending
priority: P1
effort: "12h"
dependencies: [4]
---

# Phase 5: ACP session/load, leader routing, TUI and ask acp

## Goal

The shared `acp.Adapter` serves standard `session/load`: it opens the file, takes the lock, replays history as `session/update`, and answers. The leader routes it, the TUI sends it for `-c` and `--session`, and `ask acp` saves and loads sessions with no database and no listener.

## Context

- SDK: `AgentLoader` (`third_party/acp-go-sdk/types_gen.go:9357`), `AgentCapabilities.LoadSession` (`types_gen.go:46`), `LoadSessionRequest` needs `cwd` and `mcpServers` (`types_gen.go:2470-2500`), dispatch at `agent_gen.go:374-385`.
- Adapter: `Initialize` lists only `Close` (`internal/acp/agent.go:254-256`); `NewSession` (`agent.go:301`); host `NewSessionWith` makes the id (`internal/acp/host.go:279-292`) with `newSessionID` (`host.go:347-352`).
- Update helpers to reuse: `blockUpdates`, `textUpdate`, `toolContent` (`internal/acp/updates.go:61,139,150`).
- One composition for leader, `ask acp` and the in-process TUI: `acpParams` in `cmd/tui/acp.go` (the comment at its `NewAgent` field says so); session factory in `internal/app/module_acp.go:165-200`.
- Leader: `session/load` is `callUnsupported` (`internal/leader/policy.go:81-86`); `handleNewSession` forwards with a tag (`router.go:322-330`); `finishNewSession` registers on the response (`router.go:682`); `session/update` for an unknown session is dropped (`router.go:840-845`). A load replays before its response, so the router must register first.
- TUI: `NewSessionOptions` (`internal/tui/client.go:92-99`), `tuiclient.NewSession` (`internal/tuiclient/client.go:452-490`); history renders from the follow snapshot (`internal/tui/translate_snapshot.go:20`); `session/update` is ignored (`internal/tuiclient/events.go:109-110`), so replay does not render twice.
- Per-session flags: `protocol.ACPSessionOptions` (`pkg/protocol/acp_session.go:27`), read in `internal/acp/session_options.go:27`.
- Guard to keep: `TestACPE2ENoListener` (`cmd/tui/acp_e2e_test.go:1355-1392`).

## Requirements

1. Adapter implements `AgentLoader`; `Initialize` sets `LoadSession: true`. `LoadSession` checks the cwd (absolute; must equal the header cwd after canonical form), calls a new host `LoadSessionWith`, writes the history updates through the session writer, then answers. With a route it returns the driver generation in the result `_meta`, as `session/new` does.
2. Host `LoadSessionWith(ctx, req)` calls the existing session factory with a resume field in `SessionRequest` (no second factory: the `session/new` factory also fills `trusts`, `skillRows`, `modelStarts` and `fallbacks` per id, `internal/app/module_acp.go:182-195`). It refuses an id that is live in this host with a new error kind `session_in_use` (`pkg/protocol/acp.go:322-354` kinds list, one fixed text). `internal/app` maps `store.ErrSessionOwned` to that kind and not found to `unknown_session`, because `internal/acp` does not import `store`.
3. History replay (`internal/acp/load.go`): user text and images -> `user_message_chunk`; assistant text -> `agent_message_chunk`; thinking -> `agent_thought_chunk`; each tool call -> `tool_call` with its final status, then `tool_call_update` with the result content. Custom (seed) messages and log-only entries are not sent.
4. Host ids and eager create on `session/new` already landed in phase 4; check that the id that `session/new` returns can be loaded at once.
5. `internal/app/module_acp.go`: when the request has the resume field, the factory resumes through `SessionLog.Resume(cwd, id)`, applies the restored selection and the per-session flags, and builds the agent with `AgentSpec{SessionID: id, Resume: w}`.
6. `noSession`: add `NoSession bool` to `protocol.ACPSessionOptions` (additive; old hosts ignore it). The host gives that session a `MemoryLog`.
7. Leader: new policy kind `callLoadSession`. A load of a session that the router already routes takes the existing attach path (`_ask/session/attach`, `callAttach` in `internal/leader/policy.go:77`): the caller joins the live session, gets the history replay, and no second writer opens. Else the router registers the session with the caller as driver before it forwards (tag `tagLoadSession`), so replay updates reach the caller, and forgets it when the host answers an error.
8. TUI: `NewSessionOptions.Load string`; `tuiclient.NewSession` sends `session/load` (cwd, `mcpServers: []`, same `_meta`) when it is set, then follows as today. `cmd/tui` resolves `-c` with `app.SessionLog.Latest(cwd)` and passes the id for the first session start only; `/new` and the reconnect path keep `session/new`. `--no-session` sets `Flags.NoSession`.
9. `ask acp`: no new flag. Its sessions save through the shared composition.

## Files owned

- Modify: `internal/acp/agent.go`, `host.go`, `session_options.go`, `errors.go`; create `internal/acp/load.go`, `internal/acp/load_test.go`
- Modify: `pkg/protocol/acp_session.go`, `pkg/protocol/acp.go` (one error kind)
- Modify: `internal/leader/policy.go`, `internal/leader/router.go`; extend `internal/leader/router_session_test.go`
- Modify: `internal/app/module_acp.go`; extend `internal/app/leader_routing_test.go`, `internal/app/module_acp_test.go`
- Modify: `internal/tui/client.go`, the first-start command in `internal/tui/update_session.go`, `internal/tuiclient/client.go`
- Modify: `cmd/tui/acp.go`, `cmd/tui/tui_client.go`, `cmd/tui/args.go` (TUI use of the flags only); extend `cmd/tui/acp_e2e_test.go`, `cmd/tui/leader_e2e_test.go`
- Modify: `e2e/tui/run.py` (new case `ask-resume`)

## Tests written first

- `TestLoadSessionReplaysHistoryBeforeResult` (stdio pipes, real Agent and real store on a temp home).
- `TestLoadSessionRefusesOtherCwd`, `TestLoadSessionUnknownID`, `TestLoadSessionLiveIDInUse`.
- `TestInitializeAdvertisesLoadSession`.
- Leader with the real host: `TestLeaderRoutesLoadAndDeliversReplay`, `TestLeaderRefusesLoadOfRoutedSession`, `TestLeaderForgetsFailedLoad`.
- Built binary: `TestACPE2ELoadSessionAfterRestart` (session/new, prompt, close the process, new `ask acp`, session/load, prompt continues); `TestACPE2ENoListener` still passes with session files present.
- Built binary with leader: `TestLeaderLoadLiveSessionAttaches` (a second client loads a session that the leader runs; it joins through attach, gets the replay, and no second lock is taken).
- Built binary with leader: `TestLeaderLoadSecondProcessHoldsLock` (an `ask -p --session` child holds the session; the leader load fails with `session_in_use`).
- TUI terminal case `ask-resume`: start `ask`, send one prompt, quit, start `ask -c`, assert the old prompt and reply are on screen.

## Steps

1. Write the tests; see them fail.
2. Error kind and `NoSession` in `pkg/protocol`.
3. Host and adapter load path; replay mapping.
4. App `LoadAgent`; eager create on `session/new`.
5. Leader policy and router.
6. TUI option, client call, `cmd/tui` wiring; e2e case.

## Validation

```
go test ./internal/acp/... ./internal/leader/... ./internal/app/... ./internal/tuiclient/... ./internal/tui/... ./cmd/tui/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
python3 e2e/tui/run.py --case ask-resume --case ask-turn --case ask-in-process
```

The runner compiles its own `ask` binary (`e2e/tui/run.py:1450`).

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| Replay updates reach no client in the leader | H x H (verified at `router.go:840-845`) | pre-register on forward (requirement 7) and test it |
| A large history makes one load very long | M x M | replay streams through the existing writer and its backpressure; frames stay under the 64 MiB leader limit per update |
| Leader replacement by a newer client kills a loaded session | L x M | existing behavior for every session; the file keeps the log; reconnect reload is a non-goal |
| An editor sends `session/load` for a session that `ask -p` holds | M x L | `session_in_use` with a fixed text |
| A hand-started older leader answers `session/load` with `unsupported` | M x L | `tuiclient` turns that kind into "the leader does not support session load; restart it with ask leader stop"; test it with the scripted router stream |

## Rollback

Revert the commits. The leader goes back to `callUnsupported` for `session/load`; headless resume from phase 4 still works.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
