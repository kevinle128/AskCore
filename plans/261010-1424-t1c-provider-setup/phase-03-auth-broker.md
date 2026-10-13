---
phase: 3
title: "Auth broker, key check, provider status and default-model store"
status: completed
priority: P1
effort: "2d"
dependencies: [1]
---

# Phase 3: Auth broker, key check, provider status and default-model store

Outline with scouted evidence. Run a scout pass and update this file before cooking. Can run in parallel with phase 2.

## Goal

`internal/auth` gives the leader everything a sign-in from a client needs, with no transport code: the seven login options with their state, a checked API-key save, an interactive login that a client starts, feeds, waits for and cancels, one login per provider, a logout that wins over a pending sign-in of another client (D15) and reports a still-set env key, and a per-provider cache reset. `internal/settings` stores the leader's default model (D12), and the catalog names one default model for each provider. `ask auth login` and `ask auth logout` keep their behavior.

## Evidence (scouted 2026-10-10)

- `Service.Login` runs one interaction and commits with a revision check; it blocks up to 10 min and talks through `Notify` and `Input` (`internal/auth/login.go:12-22,62-113`). The API-key branch stores the key with no remote check (`login.go:93-98`).
- `Service.Logout` deletes the local record only (`login.go:122-136`).
- Precedence: a saved record wins; an env key is used only when no saved record exists (`service.go:124-138`; README line 13). `Resolve` may refresh and call HTTP (`service.go:152-207`), so the status list must not use it.
- Methods: native OAuth methods in `http.go:56-60`; one `api-key` method for each provider row is appended in `internal/app/auth_native.go` (two for Token Plan, one per API), so a naive list has eight rows.
- Env names are local to `providers.LookupKey` (`internal/providers/keys.go:41-57`).
- `internal/settings` owns `auth.json` (lock sidecar `lock.go:13`, 0600 temp file, rename, directory sync: `README.md` "Credential transaction") and plans `settings.json` in `settings.go`, which does not exist yet (`internal/settings/README.md:9`, `doc.go:1`). The default model goes there, with the same lock and write rules.
- The catalog has no per-provider default today (`internal/providers/catalog.go:9-14`).
- Anthropic: the redirect is chosen by the interaction (`anthropic.go:22-28`); browser mode listens on the host and also reads input, but accepts only a full callback address (`callback.go:89-153`, `bare` at `:143`). A busy port gives `auth: callback port is unavailable` (`callback.go:92-94`).
- ChatGPT needs `Interaction: "browser"` and a host UUID (`chatgpt.go:20-25`; the CLI reads it with `Store.HostID`, `cmd/tui/auth.go:60-68`).
- xAI device flow: the notice has URL and user code but no expiry (`xai.go:47-56`); denial and expiry are plain `errors.New` texts (`xai.go:60,67,90-95`). Browser denial is the unexported `errAuthorizationDenied` (`callback.go:18`).
- The only provider cache is the ChatGPT model-access cache, keyed by credential generation (`discovery.go:13-31,68-78`).
- Provider error text never leaves `auth` (`http.go:132-139`).
- Account name (scouted 2026-10-10 from each provider's own CLI, read through GitNexus):
  - ChatGPT (as Codex CLI): the top-level `email` claim of the ID token, with `https://api.openai.com/profile`.email as fallback; the plan is `https://api.openai.com/auth`.chatgpt_plan_type (`codex-rs/login/src/token_data.rs:137-161`). No extra request. Verified on the stored sign-in (claim names only). Ask already verifies the ID token signature (`identity.go:77-86`), which Codex does not; keep the check and add `email` and `name` to the claims struct.
  - Anthropic (as Claude Code): `GET https://api.anthropic.com/api/oauth/profile` with the access token as bearer, field `account.email`, 10 s timeout (`claude-code/services/oauth/getOauthProfile.ts:37-53`); fall back to the token answer's `account.email_address` (`services/oauth/types.ts:39-52`, used only when the profile call fails, `cli/handlers/auth.ts:70-77`). The organization name needs a third call (`/api/oauth/claude_cli/roles`) that fails on limited tokens; the design shows only the account, so do not call it. The access token is opaque. One extra request, best effort: a failure never fails the sign-in.
  - xAI (as Grok CLI): the `email` claim of the ID token returned with the tokens (`grok-build/crates/codegen/xai-grok-login/src/device_code.rs:457-472`, browser path verifies it at `oidc/protocol.rs:609-687`). Grok CLI also calls its own `cli-chat-proxy.grok.com/v1/user`, a Grok CLI endpoint that Ask must not use. No extra request.
  - When no field is present, the outcome has no account and the shorter text applies. Tests: `TestChatGPTAccountFromIDToken`, `TestAnthropicAccountFromProfileThenTokenAnswer` (fixture profile endpoint; profile failure falls back, then no account), `TestXAIAccountFromIDToken`.
- Token Plan key check: none (user decision D16, 2026-10-10: the Token Plan subscription expired and no free read-only check is known). The Token Plan API key is saved without a live check.

## Scout result (2026-10-10, re-checked against the source before coding)

- All evidence items above still match the source at the cited lines; no line moved.
- Account name: the three mechanics are implementable as written. `identity.go` returned only the subject, so a struct-returning internal path (`verifyLogin`) now also reads `email`, the `https://api.openai.com/profile` e-mail and `name`; `Verify` and `VerifyRefresh` keep their signatures. The xAI ID token is read without a signature check, because the name is display-only (the comment in `account.go` states this). The Anthropic profile request runs only when the caller set `LoginRequest.Account`, so the CLI path makes no extra request. Every name is cleaned (control and format characters removed, 128 bytes) before it leaves the method.
- Default models per provider: the omp defaults are not in this tree, so the table choices stand (Token Plan deepseek on anthropic-messages, claude-sonnet-4-6, gpt-5.5, grok-4.7); the test proves each exists in the catalog.
- `goleak` is already in `go.mod`; no new dependency.
- Gate design (found while coding): `LoginRequest.BeforeCommit` runs right before `Store.Replace`. It orders a cancel against the commit for both sign-ins and key saves. A key save writes under a context that a cancel cannot end, so the write is all or nothing.
- The Anthropic Pro/Max flow tries `browser` first and falls back to `copy-code` only when the callback port fails before any notice (`ErrCallbackPort`).
- The ChatGPT sign-in passes no `Input`: the callback port is the only way in, as the design says (browser only).
- Open point for phase 4: a wrong pasted address or code ends `failed/internal`, because the fixed reason set has no `invalid_input`. A phase 2 reason would let the TUI say it better.

## Design

| Unit | File | Contract |
|---|---|---|
| Option table | `status.go` | `LoginOptions()` returns seven rows ordered by provider id, account row first: provider, method, display name (`design.md:110-118`). Dedupe by (provider, method). |
| Status | `status.go` | `Service.Status(ctx) ([]OptionState, error)`: one store read per provider, state `signed_in` (saved OAuth of this method), `api_key` (saved key, on the API-key row), `env` (no saved record and an env key, on the API-key row, with the variable name), `none`. Offline; never refreshes. |
| Default store | `internal/settings/settings.go` | `NewSettingsStore(home)`; `DefaultModel(ctx) (string, error)` and `SetDefaultModel(ctx, modelID) error` on `settings.json` (key `defaultModel`, the `provider/id@api` id). Lock sidecar, read-merge-write that keeps unknown fields, 0600 file, 0700 home, as `auth.json`. A missing file means no default. |
| Provider defaults | `internal/providers/catalog.go` | `ProviderDefault(provider) (Ref, bool)`: one default row for each provider (`alibaba-token-plan` deepseek on anthropic-messages, `anthropic` claude-sonnet-4-6, `openai` gpt-5.5, `xai` grok-4.7) [check the choices against omp's defaults in the scout]. |
| Env names | `internal/providers/keys.go` | Export `KeyEnvNames(provider) []string`; `LookupKey` uses it; a new `KeyEnvName(provider, env) string` returns the name that is set. |
| Key probe | `keycheck.go` | `KeyCheckable(provider) bool`: true for `openai`, `anthropic`, `xai`; false for `alibaba-token-plan` (D16). `NativeKeyCheck(o NativeOptions) func(ctx, provider, key) error` for checkable providers only: `GET <base>/models` with the header of the provider (Bearer; Anthropic `x-api-key` and `anthropic-version`). 2xx is valid. 401/403 gives `*KeyRefusal{HTTPStatus, Code}` where Code comes only from `error.code` or `error.type` matching `^[a-z0-9_]{1,64}$`. Other statuses and transport errors give `ErrKeyCheckUnavailable`. The body is read with a limit and dropped. |
| Sentinels | `callback.go`, `xai.go` | Export `ErrAuthorizationDenied`, `ErrCallbackPort`, `ErrDeviceExpired`, `ErrDeviceDenied`. Error texts stay the same, so the CLI prints the same lines. |
| Expiry and account | `login.go`, `xai.go`, OAuth methods | `LoginNotice.ExpiresAt` (device only). `LoginRequest.Account func(string)`, called by a method that knows a display name. |
| Cache reset | `service.go`, `http.go`, `discovery.go` | `Method.Forget func()`; `Service.Forget(provider)` calls it for the methods of that provider only. ChatGPT clears its access entries. |
| Broker | `broker.go` | `NewBroker(svc, BrokerOptions{CheckKey, HostID, OnChange})`. One instance for each leader process. Methods below. |

Broker methods (owner is the route client id; a call from another owner gets `ErrLoginNotFound`):

- `Start(owner, provider, method) (StartInfo, error)`: refuses with `ErrProviderBusy` while that provider has a login or a key check. Runs `Service.Login` on its own goroutine with a context derived from the broker root context (never from the request context: the SDK cancels a request context when its handler returns, `third_party/acp-go-sdk/connection.go:415-421`), and returns at the first notice (kind, URL, user code, expiry) or the early error. Kinds: Anthropic browser interaction returns `browser_address` (callback, or a pasted callback address); on `ErrCallbackPort` it starts again with copy-code and returns `copy_code`. ChatGPT: `browser` with the host id. xAI: `device`.
- `Input(owner, id, code) error`: one value into the waiting input channel; a second value while one is checked gives `ErrInvalidState`.
  The channel holds the value until the sign-in reads it or the broker drains it at completion.
- `Wait(ctx, owner, id) (Outcome, error)`: blocks until the login ends or ctx ends. A ctx end does not cancel the login.
- `Cancel(owner, id) (beforeCommit bool)`, `ReleaseOwner(owner)`, `Close()` (leader shutdown): cancel the context; the outcome is `cancelled`; a later result of that id is dropped. `Close` cancels and joins every login goroutine; the composition calls it before `StopRefresh` and `AuthWait`, because each login holds a refresh slot (`login.go:85-91`) and `AuthWait` waits up to 20 s (`internal/app/module_auth.go:53-58`).
- `SaveKey(ctx, owner, loginID, provider, key) Outcome`: takes the provider slot under `loginID` (so `Cancel` can reach it), `CheckKey` only when `KeyCheckable(provider)` (otherwise no request at all, and the outcome has `notChecked: true`; D16), checks for a cancel immediately before the commit, then `Service.Login(provider, "api-key")` with an `Input` that returns the key once. A cancel that comes after the commit returns `beforeCommit = false`. The key lives only in this call.
- `Logout(ctx, provider, method) (LogoutInfo, error)` (D15: removal wins): when a sign-in or key check of that provider runs (any owner), it cancels it with reason `removed`, waits until that goroutine has ended, then takes the provider slot. A key check that already committed is not undone; the removal then deletes that record. Then `Service.Logout`, then `Forget(provider)`; reports `EnvVar` when an env key still makes the provider usable. Removals stay serialized per provider: a second removal waits for the first. A late result of the cancelled sign-in is dropped, so it cannot write after the removal (`login.go:80,111` would also refuse it by revision).
- After every saved credential and every removal: `generation++` and `OnChange(generation, provider, change)`.
- Outcome mapping: `ErrAuthorizationDenied`/`ErrDeviceDenied` to `failed/access_denied`; `ErrDeviceExpired` to `expired`; `context.Canceled` after `Cancel` to `cancelled`; a cancel by a removal to `cancelled/removed`; `*KeyRefusal` to `refused` with status and code; `ErrKeyCheckUnavailable` and transport errors to `failed/network`; anything else to `failed/internal`. No error text crosses the broker.

## Files owned

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/auth/broker.go` | create | ~260 | `broker_test.go` |
| `internal/auth/broker_test.go` | create | ~400 | — |
| `internal/auth/status.go`, `status_test.go` | create | ~110, ~150 | — |
| `internal/auth/keycheck.go`, `keycheck_test.go` | create | ~90, ~160 | — |
| `internal/auth/login.go` | modify: `ExpiresAt`, `Account` | +6 | `login_test.go` stays green |
| `internal/auth/callback.go`, `xai.go`, `anthropic.go`, `chatgpt.go` | modify: sentinels, expiry, account | +20 | `native_test.go` stays green |
| `internal/auth/service.go`, `http.go`, `discovery.go` | modify: `Forget` | +15 | `refresh_test.go`, `access_commit_test.go` stay green |
| `internal/auth/README.md` | modify: broker, status, probe | +8 | — |
| `internal/providers/keys.go`, `keys_test.go` | modify: export env names | +15 | — |
| `internal/providers/catalog.go`, `catalog_test.go` | modify: `ProviderDefault` | +20 | — |
| `internal/settings/settings.go`, `settings_test.go` | create: default model store | ~120, ~150 | — |
| `internal/settings/README.md` | modify: `settings.json` default model | +4 | — |

## Tests first

### Tests before (regression)

- `go test ./internal/auth/... ./internal/providers/...` as is.
- `cmd/tui` auth command tests: `auth_command_test.go`, `auth_oauth_command_test.go`, `auth_signal_test.go`, `auth_fsync_test.go` (the CLI must not change).

### Tests after (new, failing first)

| Test | Proves |
|---|---|
| `TestStatusRowsOneForEachLoginOption` | Seven rows in design order; Token Plan has one API-key row |
| `TestStatusEnvOnlyWhenNoSavedRecord` | `env ANTHROPIC_API_KEY` shows only without a saved record; a saved key shows `api_key` |
| `TestStatusNeverRefreshes` | An expired OAuth token still shows `signed_in` and no HTTP request is made |
| `TestKeyCheckProbeRequests` | Endpoint and headers for `openai`, `anthropic`, `xai` |
| `TestKeyCheckableFlags` | `alibaba-token-plan` is not checkable; the three others are |
| `TestBrokerSavesUncheckableKeyWithoutRequest` | A Token Plan key is stored with no HTTP request (the test transport fails on any call); the outcome is `signed_in` with `notChecked: true` |
| `TestBrokerCheckedKeyReportsChecked` | A good OpenAI key gives `notChecked` false after one probe |
| `TestBrokerKeyRefusalCarriesCodeOnly` | A 401 whose body repeats the key gives `refused`, 401, `invalid_api_key`; no outcome field holds the key |
| `TestBrokerKeySavedPrivately` | A good key is stored once; `auth.json` keeps mode 0600 |
| `TestBrokerOneLoginPerProvider` | A second start or key save for the same provider gives `ErrProviderBusy`; another provider works |
| `TestBrokerCancelDropsLateResult` | After cancel, a token that arrives later is not stored and the outcome stays `cancelled` |
| `TestBrokerBrowserDenied` | A denied callback gives `failed/access_denied` |
| `TestBrokerCopyCodeInput` | A pasted code finishes the copy-code login; a second input while checking is refused |
| `TestBrokerAnthropicBrowserAcceptsPastedAddress` | Kind `browser_address`: a pasted callback address finishes the login; the callback finishing first also works; the address is never kept |
| `TestBrokerAnthropicFallsBackToCopyCode` | A busy callback port gives kind `copy_code` with the copy-code URL |
| `TestBrokerDeviceExpired` | With a fake clock the device login ends `expired`; the start result has `ExpiresAt` |
| `TestBrokerReleaseOwnerCancelsLogins` | Release of one owner cancels its logins only |
| `TestBrokerLogoutReportsEnvAndForgetsCache` | Logout with `ANTHROPIC_API_KEY` set reports it; `Forget` runs for that provider only |
| `TestBrokerGenerationAndChangeCallback` | Save and logout raise the generation by one and call `OnChange`; failures do not |
| `TestBrokerKeyCancelBeforeCommitSavesNothing` | A cancel during the probe stores nothing and reports `beforeCommit`; a cancel after the commit reports false |
| `TestBrokerLoginOutlivesStartRequest` | The login goes on after the context of the start call ends |
| `TestBrokerLogoutCancelsPendingSignIn` | Owner B removes while owner A's device login waits: A's outcome is `cancelled/removed`, nothing of A is stored, the record is gone, goleak clean |
| `TestBrokerLogoutCancelsKeyCheckBeforeCommit` | A removal during another owner's key probe cancels it; the key is not stored |
| `TestBrokerRemovalsSerializedPerProvider` | Two removals of one provider run one after the other; a removal of another provider does not wait |
| `TestDefaultModelStorePrivateAndLocked` | `settings.json` is 0600; two processes setting the default do not corrupt it (lock sidecar) |
| `TestDefaultModelStoreKeepsUnknownFields` | Other keys of `settings.json` survive a write |
| `TestCatalogOneDefaultPerProvider` | Every catalog provider has exactly one default row, and it exists in the catalog |
| `TestBrokerCloseCancelsAndJoins` | `Close` ends every login before it returns; a following `DrainRefresh` returns at once |
| `TestBrokerNoGoroutineLeak` | goleak after cancel, release and close |

### Refactor

Replace the four plain `errors.New` texts with exported sentinels of the same text.

## Steps

1. Scout the `[UNVERIFIED]` account-name item. Record the result here.
2. Tests for status and env names, then code.
3. Tests for the probe (httptest server through `NativeOptions.HTTPClient`), then code.
4. Tests for the broker with the existing OAuth fixtures of `native_test.go`, then code.
5. Tests for the default store and the provider defaults, then code.
6. Run the regression set and the CLI tests.

## Verification

```
go test -race ./internal/auth/... ./internal/providers/... ./internal/settings/...
go test ./cmd/tui/ -run 'TestAuth'
go vet ./internal/auth/... ./internal/providers/... ./internal/settings/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/auth/... ./internal/providers/... ./internal/settings/...
```

## Rollback

New files plus additive fields; the CLI path is unchanged. Revert the phase commit.

## Risks

| Risk | Mitigation |
|---|---|
| A key reaches an error text | Broker maps to fixed outcomes; `TestBrokerKeyRefusalCarriesCodeOnly` puts the key into the provider body |
| The probe costs money or has side effects | `GET /models` only, on the three checkable providers |
| A bad Token Plan key is saved | Accepted by D16; the TUI says it was not checked; the first prompt shows the auth error |
| A login goroutine leaks | Cancel context, the 10 min bound in `Service.Login`, goleak |
| The callback port of ChatGPT (1455) is busy | The login fails at start with `failed/internal`; P12a-like box with a reason. Recorded, not changed (design has no fallback for ChatGPT) |

## Security

The broker keeps a submitted code or callback address in the login input channel until the sign-in reads it.
The broker drains any unread input when the login ends; see `Broker.Input`, `login.inputFunc` and `Broker.release` in [broker.go](../../internal/auth/broker.go).
The broker outcome contains no secret.

## Todo

- [x] Scout and record the two unverified items
- [x] Status and env names
- [x] Key probe
- [x] Broker (removal cancels a pending sign-in)
- [x] Default model store and provider defaults
- [x] Regression and CLI tests green
