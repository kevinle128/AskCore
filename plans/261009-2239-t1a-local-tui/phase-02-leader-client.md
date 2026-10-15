---
phase: 2
title: "Leader client for the TUI"
status: completed
priority: P1
effort: "1.5d"
dependencies: [1]
---

# Phase 2: Leader client for the TUI

Outline. Run a scout pass and expand this file before cooking.

## Goal

A tested client that gives the TUI everything the design needs from the leader over ACP, behind an interface that `internal/tui` defines and the composition root implements.

## Evidence (scouted 2026-10-09)

- `cmd/tui/connect.go` already speaks every needed method through `leader.Client` with its own request tracking (`lineClient`, about lines 228-253); it is CLI glue, not a library.
- ACP methods exist (`pkg/protocol/acp.go:9-35`): state, models, set_model, set_thinking, steer, follow_up, remove, follow/unfollow (cursor epoch+seq, `resumed`, `resync`), usage, list_live, attach, detach, take, reset. Notifications: `session/update`, `_ask/session/event`, `_ask/session/resync`, `$/cancel_request`.
- `session/prompt` returns at settle (`internal/acp/agent.go:328`). Steer and follow-up return an input id at once (`internal/acp/ask_methods.go:300`); `input_outcome` log entries carry `accepted` (`internal/acp/updates.go:402-403`).
- Leader connect errors: `internal/leader/client.go:18-32`; `ConnectOrSpawn` at `:104`.
- No `AgentClient` type exists yet.

## Design

- `internal/tui` declares a small consumer interface (session commands, prompt, queue, model, sessions, follow stream as messages, connection status). It imports only `pkg/protocol` types.
- New package `internal/tuiclient` (depguard: may import `internal/leader`, `pkg/protocol`, ACP SDK; not imported by core packages) implements it over `leader.Client`. Shared JSON-RPC request tracking moves here; `connect.go` switches to it so both clients use one path.
- Reconnect loop: backoff 1 s doubling to 30 s, no limit, `ConnectOrSpawn` each attempt, then `list_live` → `attach` + `follow` from the last cursor, or report "session gone".
- In-process mode follows `docs/ask-architecture-reference.md:309`: a `direct` implementation of the same interface over the in-process agent Go API, built like headless mode (`newHeadlessAgent`). It lives outside `internal/tui` (composition root side). Leader-only calls (list_live, attach, detach, take) return "needs the leader".
- Never resends a prompt; outstanding requests fail with an "outcome unknown" error on disconnect.
- Prompt errors: the leader returns the same request error for a refused prompt and for an accepted prompt whose run failed (`internal/acp/agent.go:345-358`). The client classifies by the acceptance event: error before acceptance is a send failure; after acceptance it is a run error.
- Every client goroutine recovers panics and returns them as an error message, so the root can restore the terminal.
- Depguard: add a rule for `internal/tuiclient`, and change the `internal/tui` rule to an allow list (`pkg/protocol`, `pkg/askui`, `internal/tuiext`, terminal UI libraries, stdlib) so the import boundary is enforced.

## Tests first (real leader, faux provider, isolated `ASK_HOME`, built `ask` binary as in `cmd/tui/connect_e2e_test.go`)

| Test | Proves |
|---|---|
| `TestClientPromptAcceptanceSignal` | Which event marks prompt acceptance before settle; the client exposes it as one message |
| `TestClientRefusedPromptIsSendFailure` | `leader_not_driver` before acceptance arrives as a send failure with the kind |
| `TestClientRunFailureAfterAcceptanceIsRunError` | An auth failure after acceptance arrives as a run error, not a send failure |
| `TestClientGoroutinePanicBecomesError` | A panic in a reader goroutine is reported, not a crash |
| `TestClientSteerFollowUpRemove` | Queue updates and removal round trip |
| `TestClientSetModelReadinessFailure` | Readiness failure is returned and the model does not change |
| `TestClientFollowResumeAfterSocketDrop` | Same leader: attach + follow from cursor delivers missed events once |
| `TestClientSessionGoneAfterLeaderRestart` | Restarted leader: reported as gone; nothing resent |
| `TestClientInProcessAgent` | The in-process client gives the same interface: prompt, stream, cancel, queue, model; leader-only calls return "needs the leader" |
| `TestClientStartupFallbackOnlyAtStartup` | A connect error at startup selects the in-process client; a disconnect after start never does |
| `TestClientTwoClientsIsolation` | Second client lists, attaches as observer, sees the stream; request ids do not cross |
| `TestConnectUsesSharedClient` | `ask connect` E2E tests still pass on the shared path |

## Verification

`go test ./internal/tuiclient/... ./cmd/tui/...`, `golangci-lint` with a new depguard rule for `internal/tuiclient`.

## Scout result (2026-10-10)

Decisions taken after reading `cmd/tui/connect.go`, `internal/leader/client.go`, `internal/acp/*`, `pkg/protocol/acp.go`, `cmd/tui/headless.go` and the connect E2E tests.

- **Prompt acceptance.** `session/prompt` answers at settle, and `input_outcome` rows exist only in snapshots. The live signal is the `agent_start` event of the followed session: it comes before the first `message_update` and before the prompt answer (the host writes all follower frames before it answers). The client always follows the session it drives, reports `PromptAcceptedMsg` at the first `agent_start` after the send, and classifies a later error as a run error. `TestClientPromptAcceptanceSignal` pins the order.
- **Package split.** `internal/tuiclient/rpc` holds the shared request tracker (`Conn`, `Transport`, `Fault`, panic recovery, leader and line transports). It has no UI import. `internal/tuiclient` holds the `tui.Client` implementation. `ask connect` runs on `rpc.Conn`; its output lines are unchanged.
- **Follow commit.** The tracker runs a commit function on the reader before it reads the next frame. A follow answer needs this (`TestCommitRunsBeforeNextFrame`, `TestConnectE2EFollowResultCommitsBeforeNextFrames`).
- **In-process client.** Deviation from the outline, which says "over the in-process agent Go API": the in-process mode runs the same `acp.ServeStdio` host as `ask acp` on an `io.Pipe` and uses the same client code. The model catalog, readiness check, queue, follow and prompt classification are the code of the leader path, so both modes cannot drift. Leader-only calls (`ListSessions`, `Attach`, `Detach`, `Take`) return `tui.ErrNeedsLeader` before anything is sent. The factory lives in `cmd/tui/tui_client.go` because it imports `internal/app` and `internal/acp`.
- **Startup choice.** `openTUIClient` is the only place that picks in-process: a failed first leader connect or initialize. The reconnect loop never does.
- **Messages.** Stream and state reach the UI as `tui.Message` values from one ordered, unbounded queue, so the socket reader never waits for the UI (a waiting reader cannot answer leader pings).
- **Reconnect.** The first attempt is immediate, then 1 s doubling to 30 s with no limit. After a link, the client lists live sessions, attaches, takes the session back when it was the driver, and follows from the last cursor. A session that is not live gives `SessionGoneMsg`. Nothing is sent again: calls in flight fail with `ErrOutcomeUnknown`.
- **Goroutines.** Every goroutine runs through `Client.safely` (or `rpc.Conn.Run`, which recovers itself). A panic becomes `ClientErrorMsg` with the stack in `*rpc.PanicError`.
- **Depguard.** `tui-client-boundary` is an allow list; because depguard matches by prefix, `tui-no-client-implementation` denies `internal/tuiclient` for `internal/tui`. New `tuiclient-boundary` allow list. `core-no-terminal-ui` also denies `internal/tuiclient`.

## Checklist

- [x] Scout and expand this file
- [x] `internal/tui/client.go` consumer interface and messages
- [x] `internal/tuiclient/rpc` tracker, transports and tests
- [x] `internal/tuiclient` client, reconnect, restore, panic recovery
- [x] `cmd/tui/connect.go` on the shared tracker
- [x] In-process client and startup choice in `cmd/tui/tui_client.go`
- [x] All twelve tests of the table above (`cmd/tui/tui_client_e2e_test.go`, `internal/tuiclient/*_test.go`; `TestConnectUsesSharedClient` is the unchanged connect E2E suite)
- [x] Depguard rules and lint clean for `internal/tui`, `internal/tuiclient`, `cmd/tui`
