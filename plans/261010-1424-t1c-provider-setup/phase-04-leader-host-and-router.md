---
phase: 4
title: "Leader host and router"
status: completed
priority: P1
effort: "2d"
dependencies: [2, 3]
---

# Phase 4: Leader host and router

Outline with scouted evidence. Run a scout pass and update this file before cooking.

## Goal

The leader serves the provider methods to every client over the socket, broadcasts provider changes, cancels the logins of a client that leaves, lists only usable models on request, starts each session on the saved default model or the next usable choice (D12), saves the model chosen in `/model` as the default, lets a removal cancel another client's pending sign-in (D15), and keeps the faux demo provider behind an opt-in (D10). `ask connect` and old clients keep working.

## Evidence (scouted 2026-10-10)

- Ask methods are dispatched in `internal/acp/ask_methods.go:19-54`, handler table `:58-74`. Every handler goes through `checkRouteSession`, which on the leader link refuses a route without a session (`internal/acp/route.go:64-75`). **Provider methods need their own dispatch path**: route and client id required, no session.
- `modelID` of a zero model gives `"/@"` (`ask_methods.go:87-89`); `state` and `models` use it (`:113-141`). Both must answer `""` for no model.
- The adapter sends extension notifications through `notifier.NotifyExtension` (`internal/acp/agent.go:22-26`).
- Events with no standard form give no `session/update` frame and reach followers whole (`internal/acp/updates.go:20-22`, `agent.go:454-465`), so `model_update` needs no change in `updates.go`.
- Composition: `NewACPRuntime` prepends `Initial` to every model list and resolves it in `FindModel` (`internal/app/module_acp.go:71-99`). `NewNativeAgent` requires `InitialStream` (`internal/app/agent_native.go:44-48`) and routes faux outside auth (`:63-68`).
- A `_meta` route is parsed on every link (`internal/acp/route.go:27-39`), and only the leader sets `RequireRoute` (`internal/app/module_leader.go:73-79`). A route therefore proves nothing on `ask acp` or the in-process link: an editor can forge one.
- The leader, `ask acp` and the in-process TUI agent all use `acpParams`, with fixed faux options (`cmd/tui/acp.go:103-124`, `args.go:58-59`; leader at `cmd/tui/leader.go:245`, in-process at `cmd/tui/tui_client.go:88-96`). Headless `ask -p` uses the same defaults through `parseArgs` and must not change.
- Router: one method table (`internal/leader/policy.go:32-70`); unknown methods are refused (`router.go:250-253`); `authenticate` is forwarded with only the client id (`router.go:264-265`); agent notifications other than session events are dropped (`router.go:778-804`); a gone client only marks its ids dropped (`router.go:470-485`, `ids.go:206-218`); the router can send its own requests (`router.go:603-617`), and its own unfollow already carries `_meta` with the route of the gone owner (`router.go:574-585`); the release uses the same form.
- The idle check counts forwarded requests in flight (`router.go:810-815`), so a pending `login/wait` keeps the leader from a quiet stop. `ask leader stop` and shutdown must cancel logins (broker `Close`).
- Real-host routing tests live in `internal/app/leader_routing_test.go`; scripted router tests in `internal/leader/router_*_test.go` (`internal/leader/README.md:64`).
- E2E env helpers that rely on the faux default: `interactiveEnv` (`cmd/tui/interactive_test.go:61`), `leaderEnv` (`cmd/tui/leader_e2e_test.go:25`), `withoutCredentialEnv` (`cmd/tui/tui_client_e2e_test.go:313`), `pacedEnv` (`cmd/tui/connect_e2e_test.go:28`). [Scout: list every test that starts a leader and add `ASK_FAUX=1` where it needs faux.]

## Scout result (2026-10-10, before coding)

- Baseline: `go test ./...` was green with the phase 2 and 3 code in the tree.
- Evidence items above still match the source; the lines moved only by the phase 2 and 3 edits. Re-checked: `ask_methods.go` dispatch and `modelID` (now `""` for a zero model), `route.go` (`needRoute`, `checkRouteSession`), `agent.go` config and `Initialize`, `host.go` (`refused`, `noteQueueLocked`), `module_acp.go`, `agent_native.go` (`InitialStream` was required), `policy.go`, `router.go` (`onAgentNotification`, `removeClient`, `internalRequest`).
- `ACPProvidersReleaseClient` (`_ask/providers/release_client`) is not in the router policy table, so a client gets `unsupported`.
- The SDK serves each request on its own goroutine (`third_party/acp-go-sdk/connection.go:415`), so a blocking `login/wait` handler does not stop other calls.
- `Factory` keeps its signature. The runtime picks the start model inside its factory closure and hands it to `ACPParams.NewAgent` (new fourth parameter `start`). The fallback note reaches the session through the optional `Config.StartFallback`, read once when the adapter publishes the session. The start model is set through `agent.Config.Model`, not `SetModel`, so no readiness call (token refresh, HTTP) can leave a usable provider with no model.
- The router sends the release only for a client that made an account call (`client.usedAccounts`), so closing any other client adds no agent-side request.
- A start that ends before it shows a notice (`auth.StartError`, for example a busy ChatGPT callback port) has no wire outcome today; the host answers it as a fixed `internal` error. A wrong pasted address or code ends `failed/internal`, as phase 3 noted. Neither needs a new text now; both are recorded for the TUI phases.
- `ACPLogoutResult.generation` is the broker generation after the removal (it can be higher when a save followed at once; clients drop stale values anyway).

## Design

Host (`internal/acp`):

- `Config.Accounts ProviderAccounts`: an interface with `List`, `SaveKey`, `LoginStart`, `LoginInput`, `LoginWait`, `LoginCancel`, `Logout`, `ReleaseClient`, `Usable(provider) bool`. It is the test seam (fake in acp tests); `internal/app` implements it over `auth.Broker`. Nil means the methods answer `unsupported`.
- New `provider_methods.go`: a second handler table checked before the session table. It is served only when `Config.RequireRoute` is true (the leader link) **and** `Config.Accounts` is set; otherwise every provider method answers `unsupported`, whatever `_meta` holds. `Accounts` is set only in `NewLeaderRuntime`, never in `NewACPRuntime`, so `ask acp` and the in-process link never take a secret. Each call passes `route.ClientID` as the owner.
- `models`: decode `ACPModelsRequest`; with `usableOnly`, keep rows whose provider is usable (faux counts as usable when it is in the rows) and echo `usableOnly: true`. `Current` and `state.ModelID` are `""` with no model.
- Prompt, continue, steer and follow-up map `agent.ErrNoModel` to `no_model` (`errors.go`).
- Logout with the caller's `sessionId` reports `clearedModel` (and `clearPending` for a running session) in its result, so the caller's note does not depend on the order of the reply and the `model_update` event.
- Logout cancels a pending sign-in or key check of that provider by any client (broker, D15); that client's `login/wait` answers `cancelled` with reason `removed`, and `providers_changed` follows the removal.
- `setModel`: when the request has `saveDefault` (a choice in `/model`), after a successful switch call `Config.SaveDefault(model)` and report `defaultSaved`. Without the flag (`ask connect`, a fallback, a cycle) nothing is saved. A save failure does not undo the switch (`defaultSaved: false`). Any link may save (leader, `ask acp`, in-process share one `ASK_HOME`). A sign-in through `/providers` never sets the default.
- `models`: report the saved default id in `default` (design P25a).
- `state`: report `modelFallback` while the session still runs on its fallback start model.
- On a saved credential or a removal (`OnChange` of the broker): send `_ask/providers/changed`. On a removal: clear the model of every live session whose provider is no longer usable (new `Host.Sessions()`), which posts `model_update` to its followers.

Composition (`internal/app`, `cmd/tui`):

- `provider_accounts.go`: implements `ProviderAccounts`; list rows from `auth.Service.Status` plus the model count of each provider from the catalog; owns the `auth.Broker` with `NativeKeyCheck` and the host id. Built in `NewLeaderRuntime` only. Cleanup order: broker `Close` (cancel and join), then `StopRefresh`, then `AuthWait` (`module_acp.go:101-104`).
- The faux rule applies to the leader, `ask acp` and the in-process agent alike, because they share `acpParams` (`cmd/tui/acp.go:100-102`).
- `ACPParams.Initial` becomes optional (`InitialStream` optional in `NewNativeAgent`). Without `ASK_FAUX=1` the catalog has no faux row.
- New `internal/app/default_model.go`: `StartModel(ctx) (model, fallback)` runs once for each new session, in this order (D12): (1) the saved default from `settings.json` when its provider is usable; (2) the provider default (`providers.ProviderDefault`) of the first usable provider in catalog order; (3) the first usable row in catalog order; (4) no model. When a saved default exists but is not usable, the result carries the fallback (saved id, reason `not_signed_in` or `not_in_catalog`; design P28c). With `ASK_FAUX=1` faux is the first catalog provider, so a session starts on faux-1 unless a usable saved default exists; tests use fresh homes, so they start on faux-1 as today.
- `ask -p` keeps faux as its default.

Router (`internal/leader`):

- `policy.go`: new kind `callAccount` for the seven provider methods; `ACPProvidersReleaseClient` is not in the table, so a client cannot send it.
- `router.go`: `callAccount` forwards with `ACPRouteMeta{ClientID}` like `authenticate`; `onAgentNotification` sends `_ask/providers/changed` to every initialized client; `removeClient` sends `ACPProvidersReleaseClient` as an internal request with the route of the gone client.

## Files owned

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/acp/provider_methods.go`, `provider_methods_test.go` | create | ~200, ~300 | fake accounts |
| `internal/acp/ask_methods.go` | modify: dispatch order, models flag, empty model id | +40 | `ask_methods_test.go` |
| `internal/acp/agent.go`, `host.go`, `errors.go` | modify: config field, `Sessions()`, `no_model` and `provider_busy` mapping | +40 | `errors_test.go`, `host_test.go` |
| `internal/acp/README.md` | modify: provider methods, no model, models flag | +10 | — |
| `internal/app/provider_accounts.go`, `provider_accounts_test.go` | create | ~180, ~220 | — |
| `internal/app/default_model.go`, `default_model_test.go` | create: start order, save on select | ~90, ~200 | — |
| `internal/app/module_acp.go`, `agent_native.go`, `auth_native.go`, `module_leader.go` | modify | +60 | `leader_routing_test.go` |
| `internal/app/leader_routing_test.go` | modify: provider routes | +120 | — |
| `internal/leader/policy.go`, `router.go` | modify | +45 | router tests |
| `internal/leader/router_accounts_test.go` | create | ~200 | — |
| `internal/leader/README.md` | modify: account calls, broadcast, release | +6 | — |
| `cmd/tui/acp.go`, `headless_faux.go`, `tui_client.go` | modify: faux opt-in, initial model | +40 | — |
| `cmd/tui/interactive_test.go`, `leader_e2e_test.go`, `tui_client_e2e_test.go`, `connect_e2e_test.go` env helpers | modify: `ASK_FAUX=1` | +10 | must stay green |
| `cmd/tui/acp_e2e_test.go` | modify: `ASK_FAUX=1` in the `ask acp` env; faux expectations at `:711,811-812` kept under it | +10 | must stay green |
| `e2e/tui/run.py`, `e2e/tui/test-assertions.py` | modify: `ASK_FAUX=1` in `ask_environment` and `ASK_ENV_NAMES`; credential variables removed from the child env (`run.py:506,545,659-665`; `test-assertions.py:67-83`) | +15 | `python3 e2e/tui/run.py` green, no billed call |
| `cmd/tui/leader_accounts_e2e_test.go` | create | ~200 | — |

`cmd/tui/interactive_flow_test.go` is not in this list: it uses `interactiveEnv`, so it needs no edit (it has uncommitted work from another session).

## Tests first

### Tests before (regression)

- `go test ./internal/acp/... ./internal/app/... ./internal/leader/...`
- `cmd/tui` connect, leader, client and interactive E2E suites (after the env helper change in the same commit).

### Tests after (new, failing first)

| Test | Proves |
|---|---|
| `TestProviderMethodsNeedLeaderRoute` | On the leader link a route with a client id and no session is served |
| `TestProviderMethodsRefuseForgedRouteOnStdio` | On a stdio adapter (no `RequireRoute`, no `Accounts`) a call with a forged `_meta` route gets `unsupported` and reaches no broker |
| `TestSteerFollowUpWithoutModelAnswerNoModel` | Steer and follow-up on an idle session with no model give `no_model` |
| `TestLogoutReportsClearedModelOfCallerSession` | The result names the caller's cleared model; `clearPending` while its run is active |
| `TestLeaderStopWithPendingLoginIsQuick` | `ask leader stop` with a waiting device login ends without the 20 s drain |
| `TestProviderMethodsMapErrorsWithoutText` | `provider_busy`, unknown login and refusals reach the wire with fixed texts only |
| `TestModelsUsableOnly` | `usableOnly` keeps usable providers and echoes; no flag gives the full catalog |
| `TestModelsAndStateEmptyWhenNoModel` | `current` and `modelId` are `""`, never `"/@"` |
| `TestPromptWithoutModelAnswersNoModel` | Prompt gives `no_model`; no `agent_start` follows |
| `TestHostLogoutClearsUnusableModels` | After a removal, sessions on that provider get `model_update` with no model; others keep theirs |
| `TestFollowResumeCarriesModelUpdate` | A follower that resumes from a cursor gets a missed `model_update` once |
| `TestAccountsListRowsAndModelCounts` | Seven rows, states, env names; model counts from the catalog |
| `TestAccountsLogoutReportsEnv` | Removal with the env key set reports it (criterion 24) |
| `TestRouterBroadcastsProvidersChanged` | Every initialized client gets the notification; a client before initialize does not |
| `TestRouterReleasesLoginsOfGoneClient` | Closing a client sends one release with its client id |
| `TestRouterRefusesReleaseClientFromClient` | A client sending the release method gets `unsupported` |
| `TestLeaderRoutingProviderCalls` (app, real host) | List, key save (fixture), start, wait, cancel and logout round-trip through the router |
| `TestHostNeverLogsSecrets` | A key save and a code input with fake values through the leader: the leader log file and diagnostics hold neither value |
| `TestLeaderStartsWithoutModelWhenNothingUsable` (E2E, built `ask`, no env keys, no `ASK_FAUX`) | `ask connect` sees `modelId` empty and a refused prompt with `no_model` |
| `TestDefaultModelSavedOnSelect` | A successful `set_model` with `saveDefault` writes `settings.json` and answers `defaultSaved: true`; without the flag, or refused, it writes nothing |
| `TestModelsReportsSavedDefault` | `get_available_models` names the saved default |
| `TestDefaultNotSetBySignIn` | A key save or OAuth sign-in leaves `settings.json` unchanged |
| `TestSessionStartUsesSavedDefault` | A saved default with a usable provider is the start model of a new session |
| `TestSessionStartFallsBackWhenDefaultUnusable` | Saved default on a removed provider: the session starts on the provider default of a usable provider and `state.modelFallback` names the saved default |
| `TestSessionStartUsesProviderDefault` | No saved default: the provider default of the first usable provider, not just its first row |
| `TestSessionStartNoModelWhenNothingUsable` | No usable provider: no model and no fallback |
| `TestHostLogoutCancelsOtherClientSignIn` (app, real host, two route clients) | Client A waits on a device login; client B removes the provider; A's wait answers `cancelled/removed`; B's removal succeeds; both get `providers_changed` |
| `TestLeaderDefaultModelSurvivesRestart` (E2E) | `set_model` through `ask connect`, `ask leader stop`, new leader: the new session starts on the saved default |
| `TestFauxOnlyWithOptIn` (E2E) | Faux rows and the faux initial model appear only with `ASK_FAUX=1` |

### Refactor

Split the extension dispatch in `HandleExtensionMethod` into the provider table and the session table; behavior of session methods is unchanged (existing tests prove it).

## Steps

1. Scout: env helpers, `module_leader.go` wiring, `Host` session iteration. Update this file.
2. Host tests and code (fake accounts).
3. App accounts tests and code; composition and faux opt-in with the env helpers in the same commit.
4. Router tests and code.
5. Real-host routing and E2E tests.

## Verification

```
go test -race ./internal/acp/... ./internal/app/... ./internal/leader/...
go test ./cmd/tui/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Rollback

Revert the phase commit. `auth.json` does not change; a `settings.json` written by this phase holds only `defaultModel` and is ignored after a revert. If only the faux default must come back, revert the `cmd/tui` composition change and the env helpers together.

## Risks

| Risk | Mitigation |
|---|---|
| The faux default change breaks a test that the scout missed | Full `go test ./cmd/tui/...` in the same commit |
| A gone client's `login/wait` answer is dropped while the login goes on | Release on client gone; broker cancels; `TestRouterReleasesLoginsOfGoneClient` |
| Shutdown waits for a login | Broker `Close` in `Cleanup`; leader stop E2E test |
| Clearing models while a run is active | Agent keeps the run's model (phase 2 test); removal box warns (P19) |

## Security

The key and the code cross the router as request params only. The router logs methods, never params (`router.go:211-219,611`). The host maps every error to fixed texts. `TestHostNeverLogsSecrets` reads the leader log.

## Todo

- [x] Scout and update this file
- [x] Host provider methods, models flag, no model
- [x] App accounts, composition, faux opt-in, env helpers
- [x] Router policy, broadcast, release
- [x] Routing and E2E tests green
- [x] Package READMEs of `acp`, `leader` updated
