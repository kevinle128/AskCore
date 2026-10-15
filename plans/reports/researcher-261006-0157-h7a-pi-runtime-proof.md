# H7a Pi runtime proof and TDD boundaries

Date: 2026-10-06.
Scope: read-only source research for `kk:plan --deep --tdd`.
Source: `/Users/dale/Desktop/workspace/opensources/pi` at `4c6fb7cfe8c538a668726f6f8b3554098c39faee`.
Source paths below are relative to that checkout.
No source script, test, login, credential read, or inference was executed.
The architecture report was read at [xia-261006-0143-h7a-subscription-auth-architecture.md](./xia-261006-0143-h7a-subscription-auth-architecture.md).
The user has selected one saved credential/account per provider and local-only logout.
Those decisions are fixed for this plan.
Verified identity and other accepted security requirements remain in scope.

## 1. Actual user triggers and runtime ownership

`packages/ai/src/providers/all.ts`, `builtinProviders`, lines 136–174 registers the Anthropic, OpenAI and xAI providers.
Their factories are `providers/anthropic.ts:74–89`, `providers/openai.ts:7–23` and `providers/xai.ts:7–23`.
Each exposes API-key and subscription OAuth methods with its existing Messages or Responses adapter.
`auth/oauth/load.ts:33–46,68–71` loads the native flow on demand.
`packages/coding-agent/src/core/model-runtime.ts:227` obtains those built-in provider records.

The coding-agent trigger is a submitted `/login` or `/login <provider>` command.
`modes/interactive/interactive-mode.ts:3246–3249` intercepts that text before normal conversation handling.
`handleLoginCommand`, lines 5785–5805, selects directly when one method matches and otherwise shows the method/provider selector.
For these three providers, both key and OAuth options can exist, so `/login openai` does not itself prove subscription selection.
`startProviderLogin`, lines 5809–5817, routes OAuth to `showLoginDialog` and keys to their own dialog.
`showLoginDialog`, lines 6266–6305, restores the editor on completion or error and distinguishes a committed-login synchronization failure from an uncommitted failure.
`loginProvider`, lines 6249–6263, passes dialog cancellation, prompt and event handlers plus the stable installation-ID supplier to `ModelRuntime.login`.

`notifyAuthDialog`, lines 6236–6247, displays auth URLs, device codes and progress.
`components/login-dialog.ts`, `showAuth`, lines 99–111, displays an auth URL and calls `openBrowser`.
`showDeviceCode`, lines 117–130, displays a clickable verification URL and user code; it does not automatically open the browser.
`cancel`, lines 86–94, aborts the dialog operation and rejects pending input.
`showAuthPrompt`, lines 6212–6233, supports method selection, manual callback input and prompt-specific cancellation.

`ModelRuntime.login`, `core/model-runtime.ts:817–828`, queues the provider operation and delegates to `Models.login`.
`Models.login`, `packages/ai/src/models.ts:754–810`, performs native login first and then publishes the credential through serialized storage mutation.
`ModelRuntime.synchronizeCredentialState`, lines 591–609, recomposes the provider and performs a local-only catalog refresh before it reports login success.
Its network refresh is disabled at that point.
`completeProviderAuthentication`, `interactive-mode.ts:6000` onward, updates model selection and availability after authentication.
This does not establish subscription-specific authenticated model discovery for these three static provider factories.

The coding-agent logout selector calls `ModelRuntime.logout` at `interactive-mode.ts:5973–5975`.
`ModelRuntime.logout`, lines 831–836, deletes through `Models.logout` and then synchronizes local runtime state.
`Models.logout`, `models.ts:813–821`, deletes the local provider record through the store.
No remote revocation call is part of these paths.
The dialog explains that environment/config credentials remain when no saved record exists at `interactive-mode.ts:5955`.

A separate development CLI exists at `packages/ai/src/cli.ts:84–118` with `login [provider]` and `list` commands.
It calls the OAuth method directly, prints URLs/codes, creates a new UUID per login and writes `./auth.json` directly at lines 46–81.
It does not use coding-agent locking, stable installation identity or runtime synchronization.
Do not use that development CLI as the production persistence or cancellation reference for Ask.

## 2. Provider lifecycle evidence

### Anthropic browser and copy-code login

`auth/oauth/anthropic.ts`, `anthropicOAuth.login`, lines 269–290, first asks for browser or copy-code mode.
Browser mode uses `loginAnthropic`, lines 138–188, with PKCE and verifier-as-state.
It binds port 53692, uses `http://localhost:53692/callback`, and falls back to manual input if listener startup fails.
Its authorization endpoint is `https://claude.ai/oauth/authorize`; its JSON token endpoint is `https://platform.claude.com/v1/oauth/token`.
The manual branch checks a supplied state but accepts bare code and substitutes the pending state.
Copy-code mode uses `loginAnthropicCopyCode`, lines 191–225, with the hosted `https://platform.claude.com/oauth/code/callback` redirect and no local listener.
`exchangeAuthorizationCode`, lines 94–135, sends client ID, code, state, exact redirect and verifier.
`postJson`, lines 74–92, combines flow cancellation with a 30-second HTTP timeout.
`refreshAnthropicToken`, lines 231–267, sends the refresh grant to the same endpoint.
Its token parsing uses type assertions rather than complete runtime shape validation.
The shared callback server has an optional timeout, but this browser flow supplies none.
The listener closes in `finally`; unlike the ChatGPT flow, the shared close does not explicitly close spare connections.

### ChatGPT registration and browser/manual callback login

`auth/oauth/openai-chatgpt.ts`, `loginOpenAIChatGPT`, lines 233–298, requires installation UUID, PKCE, independent state and nonce.
Its authorization URL uses `dynamic_agent_client` on every Pi login.
The actual issued client ID must come back in the callback and is saved for refresh.
Authorization uses `https://auth.openai.com/api/accounts/authorize`; exchange/refresh uses `https://auth.openai.com/api/accounts/oauth/token`.
The resource is `https://api.openai.com/v1` and the redirect is `http://127.0.0.1:1455/auth/callback`.
Listener startup must succeed even when the user later pastes a callback URL.
Port conflict fails before exposing the URL.
`authorizationResultFromManualInput`, lines 64–78, requires the full exact-origin/path callback URL and matching state.
`authorizationResultFromCallback`, lines 53–62, requires code, state and issued client ID.
The callback and manual waiter race, and the unused waiter is canceled.
The listener and all connections close in `finally` at lines 288–296.
`credentialFromTokenResponse`, lines 162–180, validates token strings, expiry and direct-token scope, then saves client ID and scopes.
Initial exchange checks only that an ID token is present at lines 200–205.
Pi does not verify or save account identity or validate ID-token nonce/signature/issuer/audience.
`refreshAccessToken`, lines 208–223, uses the saved client ID and requires returned refresh token and scope.
Returning sign-in client reuse and verified identity are Ask additions, not behavior proved by this Pi flow.
The custom listener checks provider error before state validation and has no overall login deadline.

### xAI device lifecycle

`auth/oauth/xai.ts`, `requestDeviceCode`, lines 145–159, posts client ID, scope and `referrer=pi` to `https://auth.x.ai/oauth2/device/code`.
It validates code strings, positive expiry and HTTPS verification URLs.
`loginXai`, lines 201–211, emits the code/URL and begins polling without requesting manual input.
`pollForTokens`, lines 161–199, posts to `https://auth.x.ai/oauth2/token` with device grant, client ID and device code.
It waits before the first poll and handles pending, slow-down, both denial names and expired-token errors.
`auth/oauth/device-code.ts`, `pollOAuthDeviceCodeFlow`, lines 46–98, provides five-second default interval, one-second minimum, cancellation-aware waits and expiry checks between requests.
A supplied positive slow-down interval replaces the current interval; otherwise it adds five seconds.
`postForm`, `xai.ts:64–98`, has only the flow signal and no local HTTP timeout.
A stalled token call can therefore exceed device-code expiry.
`credentialsFromTokenResponse`, lines 128–143, defaults absent expiry to one hour and retains the old refresh token when refresh omits a replacement.
The flow does not save identity or granted scopes.

## 3. Request, refresh and discovery distinctions

`auth/resolve.ts`, `resolveStoredOAuth`, lines 102–161, checks stored expiry before inference and rechecks inside serialized `modify` before one refresh.
It combines caller cancellation with a 15-second refresh timeout.
A saved OAuth failure never falls through to ambient key auth.
Provider expiry margins plus the resolver window give ordinary early refresh of ten minutes for Anthropic/xAI and eight minutes for ChatGPT.
`models.ts`, `resolveRefreshCredential`, lines 609–627, has a different catalog-refresh threshold and no same local 15-second limit.
Ask's single actual-expiry/resolver policy is an improvement to these two source paths.

`core/auth-storage.ts`, `FileAuthStorageBackend.withLockAsync`, lines 157–200, holds a whole-file cross-process lock through refresh and direct `writeFileSync`.
It checks cancellation after the callback and before writing, which can discard a successfully rotated token.
`AuthStorage.modify`, lines 449–470, merges current provider data under that lock.
Atomic rename, generation conflict protection and commit-after-rotation cancellation behavior are Ask requirements rather than Pi proof.

All three provider constructors use static compiled model arrays.
They do not fetch account identity or subscription entitlement/model discovery.
OpenAI current-document discovery in the architecture report must remain labeled as a proposed Ask extension.
One saved account and local-only logout remain the user-selected Pi-compatible product decisions.
Do not use installation UUID, granted profile scope or ID-token presence as proof of verified account identity.

`api/anthropic-messages.ts:978–1033,1105–1106,1167–1182` owns OAuth bearer headers, CLI identity, beta flags and system identity.
`anthropic-messages.ts:124–131,1347,1443,1603` maps known Claude Code tool names and restores incoming names.
Its forced tool-choice assignment at lines 1273–1278 does not make that conversion.
`api/openai-responses.ts:40–46,327–345,381–382` detects ChatGPT by token/base-URL heuristic, omits several fields, and later permits custom parameters to restore them.
`providers/xai.ts:11–22` uses the same Responses wire with xAI origin; `openai-responses.ts:378` includes encrypted reasoning only for reasoning models.
Typed provenance and final serialized request validation are Ask improvements.

## 4. Atomic capability E2E boundaries for the TDD plan

| Capability | Actual end-user boundary | Focused failing scenario before implementation |
|---|---|---|
| Registration and command dispatch | Auth command to selected method without starting a model prompt. | A login/logout command containing text must never reach capture or inference, and unsupported method selection must retain the current record. |
| Anthropic browser login | Command, method selection, real loopback request, token exchange, durable publication, next prompt. | Occupied fixed port, wrong-state request followed by valid callback, cancellation during bind, and a spare browser connection followed by a second attempt. |
| Anthropic copy-code login | Command, explicit copy-code selection, entered code, exchange with hosted redirect, next prompt. | Bare code is allowed only in selected copy mode; pasted full URL with wrong origin or state is rejected without replacing old credentials. |
| ChatGPT registration/login | Command, stable host UUID, callback/manual race, exchange, verified identity, next prompt. | Wrong-state provider error must not terminate a valid attempt; missing client ID and invalid nonce/signature/issuer/audience must prevent publication. |
| Returning ChatGPT sign-in | Existing saved issued client, new attempt, verified same account/client, durable replacement. | Authorization must reuse issued client; changed verified subject or client must fail while preserving the prior generation. |
| xAI device login | Command, displayed code/link, real HTTP polls, publication, next prompt. | Stalled HTTP response must stop at bounded expiry; decreasing server slow-down interval must not cause early polling; both denial forms must retain prior credentials. |
| Refresh and persistence | Two independent processes issue prompts against one expired credential. | Exactly one rotation occurs; canceled lock waiter never starts later; canceled successful rotation is saved before cancellation returns. |
| Login/logout conflict | One process waits in login while another logs out or replaces credentials. | Late login cannot restore a deleted generation or overwrite a newer account; absence has durable revision protection. |
| Local-only logout | Auth command deletes saved record, next command resolves again. | No remote revocation request occurs; no-account-picker behavior remains; ambient-key eligibility after deletion is reported clearly. |
| Discovery and readiness | Verified credential, authenticated model view, selected model, next prompt. | A response for an old credential generation cannot publish after replacement; known denial blocks and unknown access follows the accepted policy. |
| Anthropic profile | Real assembled Messages HTTP request and returned tool event. | Named choice, historical tool changes and active declarations use one reversible codec; collision fails before HTTP; argument values and stored snapshots are unchanged. |
| Responses profile | Real serialized Responses request and public stream settlement. | Extra parameters cannot restore prohibited fields; subscription headers cannot be replaced; unsupported explicit options fail before sending. |
| Runtime publication | Login commits then availability synchronization fails. | Error distinguishes committed credential from failed login, and a new process can use the saved credential. |
| Live provider acceptance | Authorized login, selected model, prompt/tool event, terminal result, local logout. | A mock exchange is insufficient; each provider must prove its own route without fallback and with sanitized evidence. |

These are planned checks, not executed tests.
Use isolated homes, actual loopback HTTP and subprocess coordination for cross-process claims.
Use deterministic barriers for rotation/cancellation/crash points rather than sleeps.
Live login belongs to the inference host and requires authorized user participation; callback URLs or bearer tokens must not pass through command arguments.

## 5. Existing source checks and additional coverage

`packages/ai/test/anthropic-oauth.test.ts` has six named cases for manual redirect, copy mode, selection cancellation, refresh body, manual waiter cleanup and real callback delivery.
`openai-chatgpt-oauth.test.ts` has six named cases for issued client/scope storage, missing client, missing direct scope, required device ID and refresh rotation/client use.
Those ChatGPT tests stub token HTTP and feed callback URL through manual input; they are not verified OIDC or provider-live proof.
`xai-oauth.test.ts` has nine single cases plus parameterized invalid-URL and two denial cases.
`oauth-callback-server.test.ts` has twelve cases covering stray requests, single callback, timeout/abort, occupied port and manual/callback arbitration.
`oauth-device-code.test.ts` has five cases for immediate/deferred first poll, slowdown forms and canceled sleep.
`anthropic-tool-name-normalization.test.ts` has four mapping cases; `openai-responses-chatgpt-sign-in.test.ts` has two request-omission cases.
`packages/coding-agent/test/model-runtime-credential-sync.test.ts` covers local publication order, provider operation queues and committed mutation versus synchronization cancellation.
`auth-storage.test.ts` deliberately tests canceled active callback without committing it, which confirms a different policy from Ask's required rotated-token commit.

Additional H7a checks must cover verified OIDC, returning-client reuse, stale discovery generations, wrong-state error callbacks, spare connections, hanging device requests, atomic durable writes, absence generations, final parameter reintroduction, forced-name choice and CLI-to-next-prompt behavior.
None of those claims can be replaced by a count of source unit tests.

## 6. Pi-referenced headless command decision

The production coding-agent has no built-in provider login/logout subcommands.
`packages/coding-agent/README.md:54` tells the user to run `/login` inside Pi for subscription or API-key login.
Its `cli/auth-command.ts:5,18–21,48–63` accepts only `auth check`, `auth print-api-key` and `auth print-bearer-token`; `auth login` and `auth logout` are unknown commands.
`src/main.ts:582` dispatches this existing auth namespace before normal session setup.
The RPC command types and RPC mode do not expose built-in provider login/logout.
The separate pi-ai development CLI offers `login [provider]`, but no logout command or production credential-store behavior.

The closest H7a headless adaptation is `ask auth login --provider <provider>` and `ask auth logout --provider <provider>`.
This extends Pi's production auth namespace and reuses its provider-neutral native interaction, rather than sending slash commands as prompts.
State explicitly that this is an Ask adaptation because production Pi only exposes these lifecycle operations through its interactive slash UI.
A subscription login command must choose OAuth directly; if key login is exposed, select it explicitly with a method option rather than making a headless command ambiguous.
Anthropic can select browser or copy-code through the native method prompt; xAI displays the device link/code; ChatGPT uses loopback or documented forwarding with full manual callback input.
Do not add credential-print/export commands to H7a merely because Pi has them.
Tests must prove command dispatch occurs before prompt parsing/capture, require a known provider, reject secret-bearing arguments, and leave login errors as auth errors rather than model responses.

## 7. Open questions

No account-picker or remote-logout decision remains open.
Provider approval, real account eligibility and current endpoint/tool acceptance still require the live gates already identified in the architecture report.
The final maintained OIDC verifier belongs to the plan implementation decision; the CLI recommendation above follows the user instruction to use Pi as the reference.
