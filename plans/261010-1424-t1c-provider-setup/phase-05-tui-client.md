---
phase: 5
title: "TUI client contract and tuiclient"
status: pending
priority: P1
effort: "1d"
dependencies: [4]
---

# Phase 5: TUI client contract and tuiclient

Outline with scouted evidence. Run a scout pass and update this file before cooking.

## Goal

The UI gets the provider calls, the usable model list and the providers-changed message through its own `Client` interface, with the implementation in `internal/tuiclient`; the in-process client answers "needs the leader" before it sends anything.

## Evidence (scouted 2026-10-10; working tree has uncommitted T1a edits in `internal/tuiclient`)

- The whole UI contract is `internal/tui/client.go`: errors `:19-39`, `Client` interface `:91-129`, messages `:139-247`. `ErrNeedsLeader` exists (`:22`).
- The implementation: `internal/tuiclient/client.go` (methods `:437-650`, `needsLeader` `:393`, `request` `:415-426`, error translation `:398-412`). rpc errors carry the method, never the params (`internal/tuiclient/rpc/conn.go:15,221,254`).
- Notifications are read in `internal/tuiclient/events.go:80-112` (`_ask/session/event`, resync); the queue to the UI is unbounded and ordered (`mailbox.go`).
- UI calls use `callContext()` with a fixed timeout (`internal/tui/session.go:235-237`). A login wait can last up to 10 min, so `WaitLogin` gets its own context that the UI cancels.
- The in-process client is built in `cmd/tui/tui_client.go:88-140` over the same `tuiclient.Client`.

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
| `internal/tuiclient/providers.go` | create | ~120 | — |
| `internal/tuiclient/providers_test.go` | create | ~250 | — |
| `internal/tuiclient/events.go` | modify: providers changed | +20 | — |
| `internal/tuiclient/client.go` | modify: models flag, release on close | +20 | `client_test.go` |
| `cmd/tui/tui_client_e2e_test.go` | modify: provider calls on a real leader | +80 | — |

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
| `TestClientWaitLoginCancelledByRemoval` (real leader) | A second client's removal ends this client's `WaitLogin` with `cancelled/removed` |
| `TestClientCancelsOpenLoginOnClose` | Close with an open login sends one cancel first |
| `TestClientProviderCallsOnLeader` (E2E, real leader, fixture) | List, key save, logout and the changed message on a second client |

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

- [ ] Scout
- [ ] Interface and fake
- [ ] tuiclient provider calls, message, models flag
- [ ] Tests green, lint clean (depguard boundaries unchanged)
