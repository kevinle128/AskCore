# Pi Anthropic OAuth: end-to-end research for Ask phase H4

Date: 2026-10-05. Pi pin: `4c6fb7cfe`. Paths relative to `pi/packages/` unless noted.

## Outcome

- Pi implements Anthropic OAuth as a Claude Code impersonation. It uses Claude Code's public OAuth client, then sends Claude Code headers, betas, a Claude Code system line and Claude Code tool names.
- A Go port is small: PKCE, loopback server, one JSON POST endpoint for exchange and refresh, and a locked refresh. The hard part is not code. It is policy.
- Anthropic's published policy says third-party tools must not route requests through Free, Pro or Max credentials. Anthropic also enforces this on the server. See section 6.
- Recommendation: keep D9 as "API keys only" unless the user accepts the policy risk in writing. If the user accepts, build it as opt-in, behind a clear warning. Details in "Recommendation".

## 1. Login and refresh (`ai/src/auth/oauth/`)

| Item | Value | Source |
|---|---|---|
| Client id | Constant `CLIENT_ID`, base64-decoded at load. It is Claude Code's public client id. Not copied here. | `anthropic.ts:13-14` |
| Authorize URL | `https://claude.ai/oauth/authorize` | `anthropic.ts:15` |
| Token URL | `https://platform.claude.com/v1/oauth/token` | `anthropic.ts:16` |
| Scopes (space separated) | `org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload` | `anthropic.ts:24-25` |
| Redirect, browser flow | `http://localhost:53692/callback` (fixed port) | `anthropic.ts:18-20` |
| Redirect, copy-code flow | `https://platform.claude.com/oauth/code/callback` | `anthropic.ts:21` |
| Callback bind host | `127.0.0.1`, override by env `PI_OAUTH_CALLBACK_HOST` | `anthropic.ts:17` |
| Extra authorize param | `code=true` | `anthropic.ts:152, 194` |
| Authorize params | `response_type=code`, `code_challenge`, `code_challenge_method=S256`, `state`, `scope`, `redirect_uri`, `client_id` | `anthropic.ts:151-160` |

### PKCE and state

- Verifier is 32 random bytes, base64url without padding. Challenge is SHA-256 of the verifier string, base64url. `pkce.ts:21-33`.
- State is set to the verifier itself. `anthropic.ts:159, 201`. This is weak design (state leaks the verifier in the URL). The code does it because Claude Code's own flow does it. A Go port may use a separate random state only if the token endpoint accepts it. Not verified.
- The local server rejects a callback when state differs (HTTP 400). `callback-server.ts:85-88`.
- Pasted input is checked too: mismatch gives "OAuth state mismatch". `anthropic.ts:178, 216`.

### Code exchange

- POST JSON (not form) to the token URL with `grant_type=authorization_code`, `client_id`, `code`, `state`, `redirect_uri`, `code_verifier`. `anthropic.ts:103-113`.
- The `redirect_uri` in the exchange must equal the one in the authorize URL. A test locks this for the manual flow. `ai/test/anthropic-oauth.test.ts:42`; CHANGELOG `ai/CHANGELOG.md:1403`.
- Request: `Content-Type` and `Accept` are `application/json`; timeout 30 s, combined with the caller signal. `anthropic.ts:74-83`.
- Response fields used: `access_token`, `refresh_token`, `expires_in` (seconds). `anthropic.ts:121-135`. Refresh also types an optional `scope`, but does not read it. `anthropic.ts:247`.
- Stored credential: `{type:"oauth", refresh, access, expires}`. `anthropic.ts:130-135`. `expires` is epoch ms.

### Expiry

- `expires = now + expires_in*1000 - 5 min`. The 5 minute margin is baked into the stored value. `anthropic.ts:134, 265`.
- The resolver adds a second 5 minute window: it refreshes when `now + 5min >= expires`. `ai/src/auth/resolve.ts:67-68, 111-112`. So effective refresh happens about 10 minutes before the real expiry.

### Refresh

- Same token URL. POST JSON `grant_type=refresh_token`, `client_id`, `refresh_token`. No `scope`. `anthropic.ts:234-241`.
- The test "omits scope from refresh token requests" locks this. `ai/test/anthropic-oauth.test.ts:144`. Pi fixed a bug here in `ai/CHANGELOG.md:1403`.
- The refresh token rotates. The new `refresh_token` replaces the old one. `anthropic.ts:262`.

### Errors

- Non-2xx: error with status, url and the whole response body. `anthropic.ts:87-89`. The Go port must not log that body in production. It can contain token data on odd responses.
- Invalid JSON: separate error. `anthropic.ts:125-127, 256-258`.
- Pi has no special case for `invalid_grant` in the Anthropic module. Other providers do (`ai/src/auth/oauth/kimi-coding.ts:250`). Any refresh failure becomes `ModelsError("oauth", ...)`. `resolve.ts:139`.
- Callback `error` query param: HTTP 400 page and a rejected wait. `callback-server.ts:93-98`.
- Port busy: `startOAuthCallbackServer` rejects. `anthropic.ts:148` swallows the error with `.catch(() => undefined)` and falls back to manual paste only. Test: `ai/test/oauth-callback-server.test.ts:161`.

### Manual paste and headless

| Mode | Behavior | Source |
|---|---|---|
| Method select | Prompt "Browser login (default)" or "Copy code login (headless)". | `anthropic.ts:274-281` |
| Browser | Starts loopback server and a manual prompt at once. First one wins. | `anthropic.ts:140-181`; `callback-server.ts:155-183` |
| Copy code | No server. Redirect goes to Anthropic's page, which shows `code#state`. User pastes it. | `anthropic.ts:191-226` |
| Paste parser | Accepts full URL, `code#state`, `code=...&state=...`, or a bare code. | `anthropic.ts:27-55` |
| Server shutdown | `callback?.close()` in `finally`. | `anthropic.ts:186-188` |
| Server limits | One use only (second request gets 409). Only `GET` on the path. No timeout set by Anthropic flow. | `callback-server.ts:81-91, 133` |

Copy-code was added by commit `7a11fe1c7` (changelog `ai/CHANGELOG.md:29`). Copy-code works because the Claude Code client accepts that redirect URI (commit message of `7a11fe1c7`).

## 2. How the token is used on requests

### Where the token comes from (precedence)

1. A stored credential in `auth.json` wins over everything. No silent fallback to env after a failed refresh. `ai/src/auth/resolve.ts:29-31, 72-86`.
2. If nothing is stored, the API-key resolver runs. Order: stored api_key, then `ANTHROPIC_AUTH_TOKEN` (header-owned Bearer), then `ANTHROPIC_OAUTH_TOKEN`, then `ANTHROPIC_API_KEY`, then workload identity federation. `ai/src/providers/anthropic.ts:29-79`.
3. So `ANTHROPIC_OAUTH_TOKEN` beats `ANTHROPIC_API_KEY`. Name: `ai/src/env-api-keys.ts:30`; list order `env-api-keys.ts:81`. History: `ai/CHANGELOG.md:2062`, `coding-agent/CHANGELOG.md:5120`.
4. `toAuth` returns `{apiKey: credential.access}`. `anthropic.ts:295-297`. The OAuth token then looks like an API key to the request layer.

### OAuth detection

- A token is OAuth if the string contains `sk-ant-oat`. `ai/src/api/anthropic-messages.ts:978-980`. Detection is by token prefix, not by credential type. So the env token `ANTHROPIC_OAUTH_TOKEN` gets the same shaping. Test: `ai/test/anthropic-auth-token.test.ts:167`.
- `ANTHROPIC_AUTH_TOKEN` does NOT get OAuth shaping; it sends a plain Bearer header. Test: `anthropic-auth-token.test.ts:134`.

### What changes when the token is OAuth

| Change | Value | Source | Why (as stated in code) |
|---|---|---|---|
| Auth | SDK `authToken` (Bearer). `apiKey` is null. | `anthropic-messages.ts:1015-1018` | OAuth uses Bearer, not `x-api-key`. |
| `anthropic-dangerous-direct-browser-access` | `true` (also sent for non-OAuth) | `anthropic-messages.ts:1024` | SDK flag `dangerouslyAllowBrowser`. Pi sets it always. |
| `user-agent` | `claude-cli/<claudeCodeVersion>`; version constant `"2.1.280"` | `anthropic-messages.ts:96, 1025` | Claude Code identity. |
| `x-app` | `cli` | `anthropic-messages.ts:1026` | Claude Code identity. |
| `accept` | `application/json` | `anthropic-messages.ts:1023` | Default. |
| Beta `claude-code-20250219` | Added | `anthropic-messages.ts:1106` | Claude Code identity. |
| Beta `oauth-2025-04-20` | Added | `anthropic-messages.ts:1106` | Needed for OAuth tokens on the API. |
| System text | First block is exactly the Claude Code identity line ("You are Claude Code, Anthropic's official CLI for Claude."). Pi's own system text follows as a second block. | `anthropic-messages.ts:1167-1182` | Code comment: "we MUST include Claude Code identity". |
| Tool names out | Rewritten to Claude Code casing if the name matches (case-insensitive): Read, Write, Edit, Bash, Grep, Glob, AskUserQuestion, EnterPlanMode, ExitPlanMode, KillShell, NotebookEdit, Skill, Task, TaskOutput, TodoWrite, WebFetch, WebSearch. | `anthropic-messages.ts:95-122, 1347, 1443, 1603` | "Stealth mode: Mimic Claude Code's tool naming exactly" (`:95`). |
| Tool names in | Mapped back to the user's tool name by case-insensitive match against the current tool list. | `anthropic-messages.ts:125-131, 727-729` | Round trip. Test: `ai/test/anthropic-tool-name-normalization.test.ts:28`. |

- The comment says "Stealth mode". The intent is that the request looks like Claude Code. Pi does not hide that it is Pi in other places: the default `User-Agent` for non-OAuth is a Pi UA (`anthropic-messages.ts:304`).
- An explicit `anthropic-beta` header set by model or options replaces the computed list entirely. `null` removes all betas. `anthropic-messages.ts:1086-1101`. Tests: `anthropic-auth-token.test.ts:219, 228`.
- User headers (`model.headers`, `optionsHeaders`) merge after the defaults, so they can override `user-agent`. `anthropic-messages.ts:1027-1030`. The `dynamicHeaders` argument is NOT passed in the OAuth branch (it is in the Copilot and api-key branches). Minor.
- The version constant drifts. Pi shipped a fix for "outdated Claude Code version" (`ai/CHANGELOG.md:115`, `coding-agent/CHANGELOG.md:254`). The constant has to be kept current by hand. Source of tool list: cchistory (`anthropic-messages.ts:99-101`).

## 3. Extra-usage warning and the 0.40.1 / 0.41.0 history

| Fact | Source |
|---|---|
| Setting `warnings.anthropicExtraUsage`, default `true`. | `coding-agent/src/core/settings-manager.ts:91`; `coding-agent/docs/settings.md:169` |
| Warning text: third-party harness usage "draws from extra usage and is billed per token, not your Claude plan limits"; links to `claude.ai/settings/usage`. | `coding-agent/src/modes/interactive/interactive-mode.ts:317-318` |
| Logic: skip if setting is `false`, if already shown, or model provider is not `anthropic`. Warn if stored credential type is `oauth`, or if resolved key starts with `sk-ant-oat`. Shown once per session. Lookup errors are ignored. | `interactive-mode.ts:5195-5222`, `:320-322` |
| `isSubscription: true` marks the provider as subscription-backed (footer shows `(sub)`). | `ai/src/providers/anthropic.ts:83`; `ai/src/auth/oauth/anthropic.ts:271`; `coding-agent/src/core/model-runtime.ts:540` |
| Warning added in the changelog entries around 0.6x (text at `coding-agent/CHANGELOG.md:2562, 2569`), made suppressible later (`:2132, 2139`, issue #3808). | CHANGELOG |
| Anthropic OAuth removed in 0.40.1 ("Use API keys instead"), restored one release later in 0.41.0 ("is back"). Both dated 2026-01-09. No reason is recorded in the changelog. | `coding-agent/CHANGELOG.md:4296-4307` |

Pi has no billing logic. Pi cannot see whether a request used plan limits or extra usage. The warning is static text.

## 4. Pi tests

| File | Covers |
|---|---|
| `ai/test/anthropic-oauth.test.ts` | Manual login keeps localhost redirect (`:42`); method select and copy code (`:80`); cancel (`:132`); refresh without scope (`:144`); manual_code prompt abort (`:176`); browser callback success page (`:212`). |
| `ai/test/oauth-callback-server.test.ts` | State, stray requests, errors, single-use, cancel, abort, timeout, port taken (`:161`), manual vs browser race (`:175-237`). |
| `ai/test/anthropic-auth-token.test.ts` | `ANTHROPIC_OAUTH_TOKEN` gets OAuth shaping (`:114, 167`); `ANTHROPIC_AUTH_TOKEN` does not (`:93, 134`); beta override and suppression (`:219, 228`). |
| `ai/test/anthropic-tool-name-normalization.test.ts` | Tool name round trip (`:28, 70, 111, 164`). |
| `ai/test/oauth-auth.test.ts` | `isSubscription` metadata (`:41-56`). |
| `coding-agent/test/interactive-mode-anthropic-warning.test.ts` | Warn once (`:18`); warn when stored even if refresh would fail (`:38`); no warn for other providers (`:55`); no warn when disabled (`:72`). |
| `coding-agent/test/auth-storage.test.ts` | File-backed credential store and lock. |

Gap: no test of headers `x-app`, `user-agent`, or the identity system line in the OAuth branch was found by name. Not verified by a full read of every test.

## 5. Changes `2bbfcca4..4c6fb7cfe`

Command: `git log --oneline 2bbfcca4..4c6fb7cfe -- <anthropic.ts, pkce.ts, callback-server.ts, providers/anthropic.ts, api/anthropic-messages.ts, auth/helpers.ts>`

| Commit | Change | Related to OAuth? |
|---|---|---|
| `7a11fe1c7` | Copy-code login method. Adds `COPY_CODE_REDIRECT_URI`. | Yes. |
| `a9424cd43` | Workload identity federation (SDK env vars `ANTHROPIC_FEDERATION_RULE_ID`, `ANTHROPIC_ORGANIZATION_ID`, `ANTHROPIC_IDENTITY_TOKEN_FILE`). Last in key order. `isOAuthToken` is false on this path. | No. It is an enterprise API credential path. It does not use the subscription flow. |
| `b271b0a52` | Inline tool definitions beta. | No. It touches the same file only. |

`pkce.ts` and `callback-server.ts` have no commits in this range. Note: `claudeCodeVersion` was fixed in an earlier changelog entry (`ai/CHANGELOG.md:115`), not in this range's file list.

## 6. Terms of service (facts, not legal advice)

Primary source, fetched 2026-10-05: https://code.claude.com/docs/en/legal-and-compliance

- Section "Authentication and credential use". OAuth is "intended exclusively for purchasers of Claude Free, Pro, Max, Team, and Enterprise subscription plans" and for "ordinary use of Claude Code and other native Anthropic applications".
- Same section: "Anthropic does not permit third-party developers to offer Claude.ai login into their own applications, or to route requests through Free, Pro, or Max plan credentials on behalf of their users."
- Same section: developers "may not collect, store, or intermediate Claude.ai credentials or session tokens". This is a direct conflict with Pi's design, which stores the refresh token in `auth.json`.
- Same section: "Anthropic reserves the right to take measures to enforce these restrictions and may do so without prior notice."
- Exception text: an end user may sign in "to the unmodified Claude Code binary with their own Claude subscription". Ask is not that binary.
- Consumer Terms (effective 2025-10-08), https://www.anthropic.com/legal/consumer-terms: prohibit access "through automated or non-human means, whether through a bot, script, or otherwise" except with an API key or where Anthropic explicitly permits. Also prohibit sharing "Account login information ... or Account credentials".
- Same page says advertised Pro and Max limits "assume ordinary, individual usage of Claude Code and the Agent SDK".

Secondary sources (press, not Anthropic primary text; treat as lower confidence):

- Docs update dated 2026-02-19 and server-side rejections with the message "This credential is only authorized for use with Claude Code": https://gigazine.net/gsc_news/en/20260220-anthropic-third-party-block , https://alternativeto.net/news/2026/2/anthropic-officially-bans-using-subscription-authentication-for-third-party-claude-use
- From 2026-04-04 12pm PT, third-party harnesses no longer count against plan limits; use draws "extra usage" at API rates: https://www.hongkiat.com/blog/anthropic-closed-openclaw-loophole/ , https://letsdatascience.com/news/anthropic-charges-extra-for-openclaw-third-party-use-58c1f477 . This matches Pi's warning text in section 3.

Reading of the facts: the policy text and the enforcement history agree. Pi's own warning (extra usage billed per token) shows the maintainers see that a Pi subscription login does not get plan limits. If so, the main user benefit (use the flat subscription) may not exist. This is not confirmed by a primary Anthropic page. Open question 1.

## 7. Edge cases a Go port must keep

| Edge case | What Pi does | Source | Go port rule |
|---|---|---|---|
| Token rotation | New refresh token replaces old one in the same locked write. | `anthropic.ts:262`; `resolve.ts:121-139` | Persist before releasing the lock. If persist fails after refresh, the old refresh token may be dead. Write atomically (temp file, rename), mode 0600. |
| Concurrent refresh, many processes | Double-checked lock: optimistic expiry check, then lock, re-read, re-check expiry, refresh once. | `resolve.ts:107-148` | Use a file lock (flock) on `auth.json` or a sibling lock. Re-read after lock. Do not refresh if another process already did. |
| Lock staleness | Async lock: stale 30 s, retry up to deadline, `onCompromised` aborts the write. | `coding-agent/src/core/auth-storage.ts:118-200` | Refresh timeout (15 s) must be less than lock stale time. Keep both. |
| Refresh timeout | 15 s, combined with caller signal. | `resolve.ts:107-108, 128-131` | Same. |
| Clock skew | None handled beyond the 5+5 minute margin. Uses local `Date.now()`. | `anthropic.ts:134`; `resolve.ts:112` | Keep margin. Skew over about 10 minutes causes 401 or needless refresh. Add reactive refresh (see 401). |
| Revoked or invalid refresh token | Refresh throws, becomes `ModelsError("oauth")`. No fallback to env or API key. Credential stays in store. | `resolve.ts:29-31, 139` | Return a typed error "login required". Do not delete the credential automatically. Do not fall back to an API key silently. |
| 401 on an API request | No 401-triggered refresh found in `ai/src/auth` or in `model-runtime.ts` (grep for 401 found only Meta and Kimi). Pi refreshes only by expiry before the call. | grep results; `ai/src/auth/oauth/meta.ts:159` | Add one retry: on 401 with OAuth, force refresh once (under the lock), retry once. This is new work beyond Pi. Mark as Ask-specific. |
| Logged out during refresh | Returns undefined if the store no longer holds an oauth credential. | `resolve.ts:78, 106` | Same. |
| Scope changes | Refresh omits `scope`, so the server keeps the original scopes. Pi never checks granted scopes. | `anthropic.ts:234-241`; test `:144` | Do not send `scope` on refresh. Optionally read response `scope` and log a diff. |
| Token shape | Treat token as OAuth if it contains `sk-ant-oat`. | `anthropic-messages.ts:978-980` | In Go, decide by credential type, not by string match. Keep string match only for the env var path. |
| Env var vs stored | Stored credential wins; env used only if nothing is stored. | `resolve.ts:29-31` | Keep. State it in docs. |
| Fixed callback port | 53692. Busy port degrades to paste-only. | `anthropic.ts:18, 148` | Same. Use the same port because the redirect URI must match what the client id allows. |
| Callback bind | Loopback only, `127.0.0.1`. | `anthropic.ts:17` | Same. Never bind `0.0.0.0`. |
| Secrets in errors | Error text includes full response body. | `anthropic.ts:88` | Redact bodies in logs. |
| Sub-agent and parallel sessions | Lock covers all processes using the same `auth.json`. | `auth-storage.ts` | Ask has a leader daemon plus `ask -p` processes. Use one lock file for all. |
| Version drift of identity | Version constant, beta names and tool list must track Claude Code. Server can reject stale values. | `anthropic-messages.ts:96`; `ai/CHANGELOG.md:115` | Put them in one config struct. Plan upkeep. |

## Architectural fit for Ask

- `charm.land/fantasy` Anthropic adapter must support: Bearer auth instead of `x-api-key`, per-request custom headers (`user-agent`, `x-app`, `anthropic-beta`), a two-block system prompt, and tool-name rewrite on both directions. Not checked in this research whether the fork supports each. Open question 2.
- Tool-name rewrite is a provider-level concern. Put it in `internal/providers/anthropic/` only, not in `internal/tools/`. This matches the dewee import rules (one package per capability).
- Credential store and lock belong to a new auth capability, not inside the provider package. Not designed here.
- Pi `auth.json` stores plain tokens. Ask should use file mode 0600 at minimum.

## Trade-offs and ranking

| Rank | Option | Pro | Con | Risk |
|---|---|---|---|---|
| 1 | Keep D9: API keys only. Add workload-identity-style or `ANTHROPIC_AUTH_TOKEN` Bearer passthrough if needed. | Within Anthropic policy. No impersonation upkeep. | User cannot use the subscription. | Low. |
| 2 | Opt-in Pi-compatible OAuth, off by default, warning on login, documented as against Anthropic policy for third-party tools. | Matches Pi, matches user wish. | Account action risk "without prior notice". Stores Claude.ai refresh token. Per-token billing may apply anyway. Identity upkeep. | High. |
| 3 | Delegate to the real `claude` binary (spawn the unmodified Claude Code CLI as a provider). | Matches the policy exception for the unmodified binary. | Different architecture (no direct Messages API control, tools handled by Claude Code). Not researched. | Medium, unknown. |

Recommendation: rank 1 for H4 unless the user decides otherwise. If the user insists, ship rank 2 behind a flag and with the edge-case table above. This is a business and risk decision. Per the user's rule, I do not change D9 myself.

## Limitations

- Did not run Pi or hit any endpoint. All claims come from source and docs.
- Did not verify which scopes or redirect URIs the server really accepts today.
- Press sources for the April 2026 billing change are secondary. No Anthropic primary page was found for the extra-usage change.
- Did not review the `fantasy` fork code.
- Source credibility: Pi repo code and tests are primary for behavior. `code.claude.com` and `anthropic.com/legal` are primary for policy. Press articles are secondary.

## Unresolved questions

1. Does a Pi-style subscription login still draw from plan limits, or only from paid extra usage? Pi's own warning says extra usage. If so, is the feature worth its risk?
2. Does the `fantasy` fork support Bearer auth, custom headers, multi-block system prompts and a tool-name hook for Anthropic?
3. Does the token endpoint accept a random `state` that differs from the PKCE verifier?
4. Why did Pi remove then restore Anthropic OAuth on 2026-01-09? The changelog gives no reason.
5. Will the user accept the policy risk (account action without notice), or prefer rank 3 (spawn real Claude Code)?
6. Where should Ask store the credential: shared `auth.json`-style file, OS keychain, or both?

Status: DONE_WITH_CONCERNS
Summary: Pi's Anthropic OAuth flow is fully mapped with file:line. The technical port is small, but Anthropic's published policy forbids routing third-party requests through Pro/Max credentials. I recommend keeping D9 until the user decides on the policy risk.
