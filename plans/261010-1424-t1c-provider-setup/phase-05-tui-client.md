---
phase: 5
title: "TUI client contract and tuiclient"
status: completed
priority: P1
effort: "1d"
dependencies: [4]
---

# Phase 5: TUI client contract and tuiclient

Scouted again on 2026-10-10 after phases 2 to 4 (see "Scout result"). Implemented and verified.

## Goal

The UI gets the provider calls, the usable model list and the providers-changed message through its own `Client` interface, with the implementation in `internal/tuiclient`; the in-process client answers "needs the leader" before it sends anything.

## Evidence (scouted 2026-10-10; working tree has uncommitted T1a edits in `internal/tuiclient`)

- The whole UI contract is `internal/tui/client.go`: errors `:19-39`, `Client` interface `:91-129`, messages `:139-247`. `ErrNeedsLeader` exists (`:22`).
- The implementation: `internal/tuiclient/client.go` (methods `:437-650`, `needsLeader` `:393`, `request` `:415-426`, error translation `:398-412`). rpc errors carry the method, never the params (`internal/tuiclient/rpc/conn.go:15,221,254`).
- Notifications are read in `internal/tuiclient/events.go:80-112` (`_ask/session/event`, resync); the queue to the UI is unbounded and ordered (`mailbox.go`).
- UI calls use `callContext()` with a fixed timeout (`internal/tui/session.go:235-237`). A login wait can last up to 10 min, so `WaitLogin` gets its own context that the UI cancels.
- The in-process client is built in `cmd/tui/tui_client.go:88-140` over the same `tuiclient.Client`.

## Scout result

- The evidence above still holds. Lines moved by the T1a commit and the phase 2 to 4 edits: `Client` interface now `internal/tui/client.go:91-190`, `needsLeader` `internal/tuiclient/client.go:400`, `request` `:422`. The wire types are in `pkg/protocol/acp_providers.go` (phase 2): `ACPModelsRequest{SessionID, UsableOnly}`, `ACPModelRequest.SaveDefault`, `ACPLoginOutcome`, `ACPLogoutRequest.SessionID`.
- Provider methods need no session, so the client uses `live()` and not `withSession()`. `Logout` names the current session when there is one, because the host sets `clearedModel` only for a named session.
- `SetModel` has one caller: `internal/tui/update_input.go` (`setModelCmd`). It passes `true`, as the design says for a choice in `/model`.
- A real leader started from a test binary has no hook for an auth fixture. The leader E2E tests therefore use the Token Plan key (saved without a check, no network) and the ChatGPT browser flow (loopback callback, no network). The two real-leader tests live in `cmd/tui/tui_client_e2e_test.go`, because they need the built `ask`.
- Pre-existing: `TestClientInProcessAgent` fails in about 5 percent of runs at its follow-up queue check (also on the commit before phase 2). Not caused by this phase.

### Carry-overs from phase 4

| Case | Wire result | Client behavior | For phase 6 |
|---|---|---|---|
| Sign-in ends before it shows a notice (callback port busy and no other way) | `StartLogin` fails with a `*RequestError`, Kind `internal`, fixed text | Passed through unchanged | P12a "Could not start: <reason>" needs a reason text for `internal` (for example "the sign-in could not start on the leader"). The design lists `provider_busy` only. |
| Wrong pasted address or code | `SendLoginCode` is accepted; `WaitLogin` ends `failed`, reason `internal` | Passed through | P12 text is "the browser returned <reason>". It needs a text for `internal` that does not name the browser (for example "the leader could not complete the sign-in"). |
| In-process link | none; the client answers `ErrNeedsLeader` before it sends | Checked by `ConnInfo.Mode` on every provider call | UI shows the needs-the-leader text (criterion 29). An old leader that answers `unsupported` stays a `*RequestError` and is the P3 load failure. |
| Generation per leader instance | `ProvidersChangedMsg` | Last value is reset when `ConnInfo.InstanceID` changes; a reconnect to the same instance keeps it | Phase 6 drops stale list results by its own request id. |

The design needs no edit if phase 6 picks the two `internal` texts. If the user wants other words, the design gets them in an amendment.

## Design

`internal/tui/client.go` (owned by this phase only):

| Addition | Contract |
|---|---|
| `Providers(ctx) (protocol.ACPProvidersResult, error)` | List rows and generation |
| `SaveAPIKey(ctx, loginID, provider, key string) (protocol.ACPLoginOutcome, error)` | One call with a client-made login id, so `CancelLogin` can stop the check; the client never keeps or logs `key`; the UI does not use the 30 s `callContext` for it (`internal/tui/session.go:33,235`) |
| `StartLogin(ctx, provider, method string) (protocol.ACPLoginStartResult, error)` | `provider_busy` comes back as `*RequestError` |
| `SendLoginCode(ctx, loginID, code string) error` | One call |
| `WaitLogin(ctx, loginID string) (protocol.ACPLoginOutcome, error)` | Blocks; a lost link gives `ErrOutcomeUnknown` (P12b) |
| `CancelLogin(ctx, loginID string) (cancelled bool, err error)` | `cancelled` is false when the credential was already saved; also sent on detach and close when a login is open |
| `Logout(ctx, provider, method string) (protocol.ACPLogoutResult, error)` | — |
| `Models(ctx)` | Now sends `usableOnly: true`; the result tells by its echo whether the leader filtered |
| `SetModel`, `State`, `Models` | `SetModel(ctx, modelID, saveDefault bool)`: the UI passes true for a choice in `/model`. The results carry `defaultSaved`, `modelFallback` and the saved `default` (D12) from the protocol types |
| `WaitLogin` | A sign-in that another client's removal cancelled returns status `cancelled`, reason `removed` (D15) |
| `ProvidersChangedMsg{Generation, Provider, Change}` | From `_ask/providers/changed`; an older or equal generation than the last one seen **from the same leader instance** is dropped. The last seen value resets when `ConnInfo.InstanceID` changes (a restarted leader counts from zero again; `internal/tuiclient/client.go:127,266-290`) |

All provider calls in process return `ErrNeedsLeader` before sending (P24). `model_update` needs no new message: it arrives in `SessionEventMsg` as a `protocol.Event`.

## Files owned

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/tui/client.go` | modify | +60 | compile of `internal/tui` fakes |
| `internal/tui/session_rig_test.go` (fake client) | modify: new methods | +60 | — |
| `internal/tui/update_input.go` | modify: one call site of `SetModel` (compile; passes `saveDefault` true) | +1 | — |
| `internal/tuiclient/providers.go` | create | ~210 | — |
| `internal/tuiclient/providers_test.go` | create | ~520 | — |
| `internal/tuiclient/events.go` | modify: providers changed | +20 | — |
| `internal/tuiclient/client.go` | modify: models flag, release on close | +30 | `client_test.go` |
| `cmd/tui/tui_client_e2e_test.go` | modify: provider calls on a real leader, `SetModel` signature, readiness test takes its model from the catalog | +150 | — |
| `cmd/tui/interactive_flow_test.go` | modify: one env entry (a placeholder Token Plan key), because `/model` now lists usable providers only | +2 | — |

## Tests first

### Tests before (regression)

`go test ./internal/tuiclient/... ./internal/tui/... ./cmd/tui/ -run 'TestClient|TestConnect'`.

### Tests after (new, failing first)

| Test | Proves |
|---|---|
| `TestClientProviderCallsRoundTrip` | Each call sends its method and decodes its result (scripted transport) |
| `TestClientProvidersNeedLeaderInProcess` | In process, every provider call returns `ErrNeedsLeader` and nothing reaches the pipe |
| `TestClientProvidersChangedMessage` | The notification becomes one `ProvidersChangedMsg` in order with session events |
| `TestClientDropsStaleProvidersChanged` | A notification with an older generation is dropped |
| `TestClientResetsProvidersGenerationOnNewLeader` | After a reconnect to a new leader instance, generation 1 is delivered again |
| `TestClientCancelKeySaveReportsBeforeCommit` | `CancelLogin` during `SaveAPIKey` returns `cancelled` from the leader answer |
| `TestClientWaitLoginLinkLost` | A lost link during `WaitLogin` gives `ErrOutcomeUnknown`; nothing is sent again |
| `TestClientSecretNotInErrors` | Errors of `SaveAPIKey` and `SendLoginCode` (closed link, refused, bad answer) do not hold the value |
| `TestClientModelsAsksUsableOnly` | `Models` sends `usableOnly: true` |
| `TestClientPassesDefaultSavedAndFallback` | `SetModel` sends `saveDefault`; the results keep `defaultSaved`, `modelFallback` and `default` |
| `TestClientWaitLoginCancelledByRemoval` (real leader, in `cmd/tui`) | A second client's removal ends this client's `WaitLogin` with `cancelled/removed` |
| `TestClientCancelsOpenLoginOnClose` | Close with an open login sends one cancel first |
| `TestClientProviderCallsOnLeader` (E2E, real leader, Token Plan key) | List, key save, logout and the changed message on a second client |
| `TestClientLogoutNamesCurrentSession`, `TestClientProviderBusyIsRequestError`, `TestClientSameInstanceAfterReconnectKeepsGeneration`, `TestClientEndedLoginIsNotCancelledOnClose`, `TestClientCancelLoginRequestError` | Added: session id in logout, busy kind, generation kept on the same instance, no cancel for an ended login, cancel error |

## Steps

1. Scout the uncommitted T1a changes in `internal/tuiclient` (they must be committed first, see plan constraints).
2. Interface additions and fake client.
3. Tests, then `providers.go` and the events case.
4. Real-leader E2E test.

## Verification

```
go test -race ./internal/tuiclient/... ./internal/tui/...
go test ./cmd/tui/ -run 'TestClient|TestConnect'
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/tui/... ./internal/tuiclient/... ./cmd/tui/...
```

## Rollback

Revert the phase commit; the leader side stays compatible with the old client (additive methods).

## Risks

| Risk | Mitigation |
|---|---|
| The fake client in `internal/tui` drifts from the real one | One interface; the real-leader E2E test covers the same calls |
| A key string stays in memory | Go cannot wipe strings; the client holds no reference after the call. Threat model: same-user local process (socket mode 0600) |

## Todo

- [x] Scout
- [x] Interface and fake
- [x] tuiclient provider calls, message, models flag
- [x] Tests green, lint clean (depguard boundaries unchanged)
