# H4 OAuth shared infrastructure: Pi machinery, Ask gap, roadmap cut

Date: 2026-10-05. Scope: shared machinery only. Provider flows (Anthropic, ChatGPT, xAI) are in other lanes.
Pi path prefix `pi/packages/` is omitted. Pi ref `4c6fb7cfe`. Ask root: `/Users/dale/orca/workspaces/AskCore/master-2`.
Short names: `AI:` = `ai/src/`, `C:` = `coding-agent/src/`, `RM:` = `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`, `INV:` = `.../inventory-harness.md`.

## 0. Outcome

1. OAuth for the three providers does not need new wire APIs. In Pi, OAuth is a second auth method on the same provider ids `anthropic`, `openai` and `xai` (`AI:providers/anthropic.ts:81`, `openai.ts:14`, `xai.ts:14`). The token goes out as `apiKey` (`toAuth` returns `{apiKey: access}`: `AI:auth/oauth/anthropic.ts:275`, `openai-chatgpt.ts:307`, `xai.ts:235`). xAI and OpenAI use `openai-responses`, which H4 builds anyway.
2. Ask has no credential code. `internal/settings` and `internal/crypto` are `doc.go` plus README only. The loop does call `GetAPIKey` (`internal/agent/loop_stream.go:98`), but nothing implements it.
3. The shared core is small: a locked credential store, a locked refresh, a PKCE helper, a loopback callback with paste fallback, a device-code poller, and a CLI surface. About 6 Go files plus 3 flow files.
4. Recommended cut: split H4 into H4a (wires, as planned) and H4b (auth: store, resolver, three flows, `ask login`, `ask logout`, `ask auth check`). The `/login` gateway method and the TUI dialog stay in H13 and T1.
5. This reverses three user decisions: D9 (Anthropic OAuth, A to B), the M1/M2 split (login CLI moves M2 to M1), and the H7 ordering (store moves before H6). It also pulls Anthropic OAuth identity (H-AUTH-11), which the user marked "needs an explicit decision". Section 4 lists each reversal. None is applied. The user must confirm.

## 1. Pi shared machinery

### 1.1 Credential record and store interface

| Item | Fact | Cite |
|---|---|---|
| Record | `Credential = ApiKeyCredential | OAuthCredential`. One credential per provider id. | `AI:auth/types.ts:17-37` |
| API key shape | `{type:"api_key", key?, env?}` | `types.ts:17-21` |
| OAuth shape | `{type:"oauth", access, refresh, expires (epoch ms), ...extra}`. The index signature is open. | `types.ts:24-34` |
| Extra fields in use | ChatGPT stores `clientId` and `scopes`. | `AI:auth/oauth/openai-chatgpt.ts:173-179` |
| Store API | `read`, `list`, `modify(id, fn)`, `delete`. `modify` is the only write path. `fn` sees the current value and returns the next value, or `undefined` for no change. | `types.ts:65-94` |
| Validation on read-only load | OAuth needs string `access`, string `refresh`, finite number `expires`. A bad entry fails the whole file. | `C:core/auth-storage.ts:228-252` |
| Runtime key overlay | `--api-key` becomes an in-memory `api_key` credential. It shadows the file for reads. It is never written. | `C:core/runtime-credentials.ts:12-27`, `C:main.ts:827-835` |
| Provider declares OAuth | `ProviderAuth = {apiKey?, oauth?}`. `OAuthAuth` has `login`, `refresh`, `toAuth`, plus `isSubscription` and `loginLabel`. | `types.ts:216-250` |
| `lazyOAuth` | A wrapper that defers loading the flow module until first `login`, `refresh` or `toAuth`. It exists to keep Node-only code out of browser bundles. | `AI:auth/helpers.ts:40-59`, `AI:auth/oauth/load.ts:9-12` |

Go note: `lazyOAuth` solves a bundler problem. Go does not need it. A plain registry map `provider id -> OAuthMethod` is enough.

Go note: the open index signature means a Go struct with fixed fields would drop `clientId` and `scopes` on the next read-merge-write. The Go record needs a passthrough (`map[string]json.RawMessage` for unknown keys).

### 1.2 Expiry margin and refresh

| Layer | Value | Cite |
|---|---|---|
| Stored `expires` already has skew removed: Anthropic | 5 min | `AI:auth/oauth/anthropic.ts:134,265` |
| Same: xAI | 5 min | `AI:auth/oauth/xai.ts:13,141` |
| Same: ChatGPT | 3 min | `AI:auth/oauth/openai-chatgpt.ts:25,177` |
| Resolver adds its own margin | 5 min default (`minOAuthValidityMs` can raise it) | `AI:auth/resolve.ts:102,118-119` |
| Refresh network timeout | 15 s | `resolve.ts:103,132-135` |

Effective refresh point is about 10 min before real expiry (Anthropic, xAI) and 8 min (ChatGPT). INV H-AUTH-07 says "5 min". That is the resolver half only. Ask must pick one layer and state it. Recommended: store the real expiry, apply one 5 min margin in the resolver. Reason: one place, and `ask auth check` can show the real expiry.

### 1.3 Per-request resolve and refresh inside the lock

`resolveProviderAuth` (`resolve.ts:33-93`) order:

1. Runtime override `apiKey` (`:56-68`).
2. Stored credential. A stored credential owns the provider (`:27-31`, `:70-87`). If the type has no matching handler, the result is `undefined`.
3. Ambient env, only when nothing is stored (`:89-92`).

`resolveStoredOAuth` (`:110-162`) is double-checked locking:

1. Optimistic expiry check without the lock (`:122`).
2. If near expiry, `credentials.modify(...)` (`:126`). Inside `fn`: if the entry is gone (logout) return `undefined` (`:129`); if another process already refreshed, return `undefined` (`:130`); else call `oauth.refresh` with a 15 s timeout (`:132-136`).
3. The rotated credential is written before the lock is released (`:105-109` comment, `:126-141`).
4. After the lock, `toAuth(credential)` gives the request auth (`:157-161`).

Failure behaviour:

| Case | Behaviour | Cite |
|---|---|---|
| Refresh throws | `ModelsError("oauth", "OAuth refresh failed for <id>")`. The stored credential is not changed. | `resolve.ts:137-138` |
| Env fallback after failed refresh | None. "No silent env fallback after a failed refresh." | `resolve.ts:30-31` |
| Store lock or write fails | `ModelsError("auth", ...)` | `resolve.ts:143-146` |
| Logged out during refresh | Returns `undefined` (not configured) | `resolve.ts:129,147` |
| Caller needs min validity (bearer export) and the new token is too short | `ModelsError("oauth", "...expires too soon")` | `resolve.ts:152-154` |
| Startup refresh failure must not crash or log out other instances | INV claim; Pi code only throws per request and never deletes the entry | INV H-AUTH-08; `resolve.ts:137-138` |

Providers whose refresh token cannot renew: Meta has no refresh endpoint, it re-mints from the identity token (`AI:auth/oauth/meta.ts:1-14,203`). xAI may omit `refresh_token` on refresh; Pi keeps the old one (`xai.ts:130-133`). Anthropic and ChatGPT return a new refresh token, so the rotated pair must be saved under the lock or a parallel process uses a dead refresh token. This is why refresh-inside-the-lock is P0.

### 1.4 Concurrency across processes

| Mechanism | Fact | Cite |
|---|---|---|
| Lock library | `proper-lockfile` on `auth.json` itself, `realpath:false` | `C:core/auth-storage.ts:9,128` |
| Async acquire | `retries:0`, `stale:30_000`, own loop with jittered exponential back-off (10 ms doubling, capped at 1 s), deadline 30 s | `auth-storage.ts:116-155` |
| Sync acquire | 10 tries, 20 ms apart | `auth-storage.ts:69-94` |
| Cancelled waiter | `signal.throwIfAborted()` each loop; if the signal fires after the lock was taken, release first, then throw. A cancelled waiter never runs `fn`. | `auth-storage.ts:125,135,149-152` |
| Lock compromised | `onCompromised` sets a flag. `throwIfCompromised()` is checked after acquire, after `fn` and after the write. | `auth-storage.ts:166-189` |
| Write | `writeFileSync(path, next, {mode:0o600})` in place. Mode applies only on create. No temp file, no rename. | `auth-storage.ts:25,186-188` |
| Dir | created `0700` | `auth-storage.ts:59` |
| Read-merge-write | `modify` parses the file inside the lock, replaces one key, writes the whole object | `auth-storage.ts:449-471` |
| `delete` | same lock, removes the key | `auth-storage.ts:473-482` |
| Read cache | reload only when the file revision changes; shared reload is aborted when the last reader leaves | `auth-storage.ts:401-439` |
| Stalled refresh releases the lock | the 15 s refresh timeout is under the 30 s stale window | `resolve.ts:103`, `auth-storage.ts:120` |

Weak points that Ask should not copy:

- In-place write. A crash mid-write leaves a torn file. Ask should write a temp file in the same directory, `fsync`, then `rename`.
- Lock on the data file. With rename-based writes the lock inode would change. Ask should lock a sidecar file `auth.json.lock`.
- Stale-lock timers exist because a Node lock file survives a crash. `flock(2)` is released by the kernel when the process dies, so stale handling and `onCompromised` are not needed in Go.

### 1.5 Flow mechanics

| Piece | Fact | Cite |
|---|---|---|
| PKCE | 32 random bytes, base64url verifier, SHA-256 challenge | `AI:auth/oauth/pkce.ts:21-34` |
| Shared callback server | Used by Anthropic only. Host from `PI_OAUTH_CALLBACK_HOST`, default `127.0.0.1`; Anthropic port 53692, path `/callback`. State check. 404 for wrong route, 400 on `error=`, 409 for a second hit. | `AI:auth/oauth/callback-server.ts:78-116`, `anthropic.ts:17-20` |
| Fail fast on provider `error=` | `finish({error})` and an error page | `callback-server.ts:93-98` |
| Code exchanged before the success page | `complete(code)` runs first; on failure the page is 502 with the message | `callback-server.ts:105-114` |
| Race callback vs paste | `waitForCallbackOrManualInput`. A manual prompt runs next to the server. Whichever wins cancels the other. Without a server only the prompt runs. | `callback-server.ts:155-183` |
| Port busy (Anthropic) | `startOAuthCallbackServer(...).catch(() => undefined)`, so the flow runs paste-only | `anthropic.ts:140-147`, `callback-server.ts:155-158` |
| Port busy (ChatGPT) | Own server on fixed port 1455, `127.0.0.1`. On `EADDRINUSE` it fails with "Port 1455 is in use..." and has no paste fallback. | `AI:auth/oauth/openai-chatgpt.ts:22-25,85-130,243-248` |
| ChatGPT paste | A manual prompt for the final redirect URL runs beside the server and is raced | `openai-chatgpt.ts:268-282` |
| ChatGPT close | `server.close()` then `closeAllConnections()`, because a browser spare connection can deliver the next login's callback to the old server | `openai-chatgpt.ts:290-298` |
| Anthropic headless | A separate "copy code" method with redirect `https://platform.claude.com/oauth/code/callback`, input `code#state` | `anthropic.ts:19,190-225,270-290` |
| Device code | RFC 8628. Wait before first poll (opt-in). Honour server `interval` on `slow_down`, else +5 s. Minimum 1 s. Deadline from `expires_in`. Special message when `slow_down` was seen and then timed out (clock drift in WSL or VM). | `AI:auth/oauth/device-code.ts:46-97` |
| xAI | Device code only | `AI:auth/oauth/xai.ts:161-210` |

Correction to INV H-AUTH-07: it merges all providers into one flow ("port busy falls back to manual paste"). In Pi that is true for Anthropic only. ChatGPT hard-fails on a busy port. xAI has no browser flow. Ask should choose the Anthropic behaviour for ChatGPT too (paste fallback), since the paste parser already exists in the ChatGPT flow.

## 2. Login surface, CLI and network in Pi

### 2.1 `/login` and `/logout` (TUI only)

| Item | Fact | Cite |
|---|---|---|
| Entry | `/login [provider]` and `/logout` are TUI slash commands. RPC mode has no login. | `C:modes/interactive/interactive-mode.ts:3246-3255` |
| Selector | Subscription vs API key; provider list; `loginLabel` text (for example "Sign in with SuperGrok or X Premium") | `interactive-mode.ts:5722-5740,5826-5870`, `AI:auth/types.ts:223` |
| Login call | `modelRuntime.login(id, "oauth"|"api_key", {signal, prompt, notify}, {getDeviceId})` | `interactive-mode.ts:6249-6264` |
| Interaction contract | `prompt()` types: `text`, `secret`, `select`, `manual_code`. `notify()` events: `info`, `auth_url`, `device_code`, `progress`. | `AI:auth/types.ts:125-161` |
| Persist after login | `Models.login` runs the flow, then `credentials.modify(id, async () => credential)` | `AI:models.ts:756-811` |
| Logout | `credentials.delete(id)`. It removes only stored credentials. It does not revoke the token and does not unset env or `models.json`. | `AI:models.ts:813-824`, `interactive-mode.ts:5955` |
| After login | `synchronizeCredentialState` recomposes the provider and refreshes the model list. A failure here is a distinct error: "Logged in ... but local model state could not be synchronized". | `C:core/model-runtime.ts:591-611`, `interactive-mode.ts:6288-6291` |
| Browser open | The dialog calls `openBrowser(url)` on every `auth_url`. It does not open anything for `device_code`; it shows an OSC 8 link and "Cmd+click to open". | `C:modes/interactive/components/login-dialog.ts:110,116-124` |
| Browser launcher | `open` (macOS), `rundll32 url.dll,FileProtocolHandler` (Windows), `xdg-open` (other). No shell. Errors are swallowed. | `C:utils/open-browser.ts:10-24` |
| Cancel | The message `"Login cancelled"` is matched by the UI to go back to the selector | `interactive-mode.ts:6293-6295` |
| Device id | ChatGPT login asks the app for a stable host id. It is created on first use and stored in the global `settings.json` key `deviceId`. | `C:core/settings-manager.ts:1175-1182`, `AI:auth/types.ts:202-209` |

The device id is a hidden H6 dependency for ChatGPT (`settings.json` is H6). In H4b Ask can keep it inside `auth.json` under a reserved key, or in a one-line `~/.ask/device-id` file. Needs a user answer (Q4).

### 2.2 `pi auth` CLI

| Command | Behaviour | Cite |
|---|---|---|
| `auth check --provider X [--json] [--credentials] [--no-refresh]` | Needs `--provider` or `--model`. Exit 0 ready, 1 not ready, 2 invalid state. `--no-refresh` uses a read-only store. | `C:cli/auth-command.ts:18-22,98-118`, `C:main.ts:166-203` |
| `auth print-api-key` | Resolves and prints the credential. 15 s overall timeout. Exit 1 on error. | `main.ts:158-173,204-207` |
| `auth print-bearer-token --min-expiry 30m` | Same, with a required remaining validity. Duration units `ms`, `s`, `m`, `h`. | `auth-command.ts:70-81`, `resolve.ts:152-154` |
| Extraction | `apiKey`, else the `Authorization: Bearer` header | `auth-command.ts:120-126` |
| Restrictions | `--api-key`, messages and file args are rejected | `auth-command.ts:105-107` |

`pi auth` has no `login`. Login exists only in the TUI. So an `ask login` CLI command has no Pi precedent. It is an Ask addition (needed because the TUI is T1 and the gateway is H13).

### 2.3 Headless and SSH

- Anthropic: browser method (callback plus paste) or "Copy code login (headless)" selected by a prompt (`anthropic.ts:270-290`).
- ChatGPT: paste the final redirect URL (`openai-chatgpt.ts:268-282`). The browser shows a connection error on `127.0.0.1:1455`; the user copies the URL from the address bar.
- xAI: device code, no port, no paste (`xai.ts:200-210`).
- `PI_OAUTH_CALLBACK_HOST` lets the server bind another interface, for Docker or WSL (`anthropic.ts:17`, `openai-chatgpt.ts:22`).

### 2.4 Proxy (H-AUTH-12)

| Item | Fact | Cite |
|---|---|---|
| Global setting | `httpProxy` sets `HTTP_PROXY` and `HTTPS_PROXY` only when unset (`??=`) | `C:core/http-dispatcher.ts:45-50` |
| Dispatcher | `undici.EnvHttpProxyAgent`, `proxyTunnel:true`, installed as global dispatcher | `http-dispatcher.ts:84-96` |
| Startup | Applied at `main.ts:594-595` and again after settings load at `:871-872` | `C:main.ts` |
| OAuth | The OAuth flows call global `fetch`, so they go through the same dispatcher. Nothing proxy-specific is in `AI:auth/oauth/*`. | grep of `proxy` in `AI:auth/oauth/` finds only a Copilot host name (`github-copilot.ts:65-79`) |

Go: `http.DefaultTransport` uses `http.ProxyFromEnvironment`, so `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` work for any client that uses `http.DefaultClient` or a clone of `DefaultTransport`. Rule for Ask: every OAuth and provider client must be built from a clone of `DefaultTransport`, never from a bare `&http.Transport{}`. The `httpProxy` setting (settings.json) is H6. Whether the fantasy SDK clients use `DefaultTransport` by default is not verified here (Q6); `WithHTTPClient` exists on both adapters (`fantasy .../anthropic/anthropic.go:250`, `.../openai/openai.go:111`), so Ask can pass its own client.

## 3. Ask today

| Area | Finding | Cite |
|---|---|---|
| `internal/settings` | Stub: `doc.go` and `README.md` only. Planned files `auth.go`, `lock.go`, `settings.go`, `merge.go`, `paths.go`. Allowed imports: stdlib only. Denied: all other `internal/*`. | `internal/settings/doc.go`, `README.md` |
| `internal/crypto` | Stub: `doc.go` and `README.md`. Roadmap parks it for credentials. | `internal/crypto/doc.go`; `RM:76` |
| `GetAPIKey` hook | `func(ctx, provider string) (string, error)`. Empty string falls back to the configured key. First non-empty wins across composed hooks. | `internal/pipeline/hooks.go:149-151`, `hooks_compose.go:52-83` |
| Hook caller | Loop calls it before each request. | `internal/agent/loop_stream.go:98` |
| Hook implementer | None outside tests (grep of non-test files finds only the hook, compose and README). | grep result |
| `--api-key` | Parsed to `options.apiKey`, passed as `StreamOptions.APIKey` in headless mode. Requires `--model`. | `cmd/tui/args.go:62,146`, `cmd/tui/headless.go:159` |
| Token Plan key lookup | `opts.APIKey`, then `ASK_ALIBABA_TOKEN_PLAN_API_KEY`, then `ALIBABA_TOKEN_PLAN_API_KEY`. Fails with a message otherwise. | `internal/providers/tokenplan/provider.go:164-177` |
| `cmd/tui` subcommands | None found. Entry is `os.Exit(run(os.Args[1:], ...))`; flags only. CLAUDE.md names `ask leader` as a future subcommand. | `cmd/tui/main.go:122` |
| Adapter auth options | fantasy Anthropic has `WithAPIKey`, `WithHeaders`, `WithHTTPClient`, no bearer-token option. | `fantasy .../anthropic/anthropic.go:193-258` |
| Hook contract limit | Returns one string. Pi's `ModelAuth` is `{apiKey, headers, baseUrl}` (`AI:auth/types.ts:7-11`). | see 0.1 |

Contract question for `GetAPIKey`: for these three providers a string is enough, because `toAuth` returns only `apiKey`. But Anthropic sends the OAuth token as `Authorization: Bearer` plus beta headers, chosen by sniffing `sk-ant-oat` in the key (`AI:api/anthropic-messages.ts:978-979,1013-1033,1106`). With `WithAPIKey` the SDK sends `x-api-key`. So the Anthropic adapter needs a per-request header path, via the hook (richer return value) or a prefix sniff inside the adapter. Not decided here (Q3).

### Minimum Ask must build to store and refresh OAuth tokens in H4

| # | Piece | Package | Notes |
|---|---|---|---|
| 1 | Credential types: api key and OAuth, `Expires` as epoch ms, unknown-field passthrough | `settings` | Same JSON as Pi so a copied `auth.json` works (D2: "files as in Pi"). |
| 2 | `Store`: `Read`, `List`, `Modify(id, fn)`, `Delete`. File mode 0600, dir 0700. Temp file plus `fsync` plus `rename`. | `settings` (`auth.go`, `paths.go`) | `Modify` is the only write path. Never overwrite a corrupt file. |
| 3 | Cross-process lock on `auth.json.lock`, with context deadline and abort | `settings` (`lock.go`) | See section 5. |
| 4 | Resolver `GetAPIKey`: runtime `--api-key` > stored > env. Double-checked refresh under `Modify`, 5 min margin, 15 s timeout, no env fallback after refresh error. | outside `settings` (settings may not import other `internal/*`; README says "caller passes a resolver function") | Package home is open (Q2). |
| 5 | OAuth method interface `Login(ctx, Interaction)`, `Refresh(ctx, cred)` and a registry by provider id | same new home as 4 | Replaces `lazyOAuth`. |
| 6 | PKCE helper, loopback callback server with paste race, device-code poller | same | Section 5. |
| 7 | Three flow files: Anthropic, ChatGPT, xAI | same | Other lanes specify the URLs and bodies. |
| 8 | `ask login <provider>`, `ask logout <provider>`, `ask auth check` in `cmd/tui` | `cmd/tui` | Terminal `Interaction` (stdin prompt, print URL, try to open browser). No stderr noise while the TUI runs (E§14). |
| 9 | `app` wiring: build store, build resolver, set `Hooks.GetAPIKey` | `app` (fx) | `cmd/tui` headless uses the same resolver. |
| 10 | Tests: two-process concurrent refresh, fake IdP, cancelled waiter, corrupt file, read-only file, port-busy, `error=access_denied`, `slow_down` | each package | Section 6. |

## 4. Dependency analysis and options

### 4.1 What must move, by roadmap item

| Item | Today | Needed for H4b | Depends on |
|---|---|---|---|
| H-AUTH-02 (file, record) | H7 | Yes | D2 (already decided, files as in Pi) |
| H-AUTH-03 (lock, `Modify`) | H7 | Yes. Without it two `ask -p` runs can burn a rotated refresh token. | flock choice |
| H-AUTH-01 (precedence) | H7 | Partial: runtime key > stored > env for the 3 providers. `models.json` key and catalog stay H7. | H7 catalog |
| H-AUTH-08 (per-request auth) | H4 ("Also builds" `GetAPIKey` per provider) | Already in H4 scope, extended with refresh | none |
| H-AUTH-05 (env keys), OpenAI part | H4 | Unchanged. `XAI_API_KEY` is new (xAI provider data is not in H4 targets). | none |
| H-AUTH-06 (OAuth providers, P1) | H17 | Three of them | D9 for Anthropic |
| H-AUTH-07 (flow mechanics) | H17 | Yes, minus extras | stdlib http |
| H-AUTH-10, H-SLASH-10 (`/login` gateway method, TUI) | H17, gateway in H13, TUI in T1 | No. Replace with an `ask login` CLI. Gateway method and TUI dialog keep their phases. | H13, T1 |
| H-AUTH-12 (proxy) | H17 | Env part only (stdlib). `httpProxy` setting stays H6. | H6 |
| H-AUTH-09 (`auth` CLI) | P2, no phase | `auth check` only, optional | none |
| H-AUTH-11 (Anthropic identity) | P2, "needs decision" | Required for Anthropic OAuth to work | D9, ToS answer |
| Device id (ChatGPT) | H6 `settings.json` | Needed at ChatGPT login | Q4 |
| `internal/settings` package | H6 | Only `auth.go`, `lock.go`, `paths.go` | none |

### 4.2 Options

Order of ranking: 1 is best.

| Rank | Option | Content | Effort (rough) | Risk |
|---|---|---|---|---|
| 1 | **Split: H4a wires, H4b auth** | H4a as planned. H4b: items 1 to 10 above, with loopback callback, paste fallback, device code, `ask login/logout/auth check`, env proxy. Ships before H5. | 3 flows plus about 6 core files. Largest of the three. | Moderate. H4a is never blocked by auth. |
| 2 | **Minimal: paste-only login plus locked store** | Same store, lock and resolver (items 1 to 5, 9). No loopback server and no device-code abstraction: Anthropic copy-code, ChatGPT redirect-URL paste, xAI device flow as a short loop. `ask login <provider>` only. | Smaller by the callback server and a generic poller (about 150 LOC). | Low. The browser shows a connection error on ChatGPT, and the user copies the URL. Worse UX. Same reversals as option 1. |
| 3 | **Full move into H4** | H4 owns all of H-AUTH-01/02/03/05/06/07/10/12 and `/login` as a gateway method and TUI dialog. | Not possible in order. The gateway is H13 and the TUI is T1. | High. Needs H6 (`httpProxy`, `deviceId`) and H13 first. Reject. |

Why not "lock-free store" for option 2: Anthropic and ChatGPT return a new refresh token on each refresh (`xai.ts:130-133` shows xAI is the exception that may keep it). Two processes that refresh the same token race, and the loser may be left with a dead refresh token. Lock plus refresh-inside-`Modify` is the minimum. This keeps H-AUTH-03 P0 as in the roadmap.

### 4.3 Decisions each option reverses

| Decision | Source | Option 1 | Option 2 | Option 3 |
|---|---|---|---|---|
| D9: Anthropic OAuth with Claude Code identity "A: API keys only. Anthropic OAuth with Claude Code identity is not built." | `RM:43` | Reversed (A to B) if Anthropic is included | Same | Same |
| Login is M2 ("`/login` is M2... the user sets an API key with an environment variable or in `~/.ask/auth.json`") | `RM:25,678` | Login CLI moves to M1. TUI `/login` stays M2. | Same | Also moves TUI and gateway |
| H7 owns H-AUTH-02, H-AUTH-03 | `RM:331-332` | Moves earlier to H4b. H7 keeps catalog and `models.json`. | Same | Same |
| H17 owns H-AUTH-06/07/10/12 and exit "OAuth login through the gateway" | `RM:600,613` | Subset moves. H17 keeps Copilot, other vendors, gateway `/login`, `httpProxy`. | Smaller subset | All moves |
| H4 targets: OpenAI key "from `OPENAI_API_KEY` only (user, 2026-10-05, plain Pi name)" | `RM:231` | Extended, not reversed: OAuth adds a second source for `openai`. API-key rule stays. | Same | Same |
| D2: `~/.ask/auth.json`, 0600, lock, read-merge-write, `internal/settings` owns it | `RM:36` | Kept. The package starts earlier than H6. | Same | Same |
| D7: `~/.ask/`, `ASK_HOME` | `RM:41` | Kept | Kept | Kept |
| `crypto` parked for credentials | `RM:76` | Kept | Kept | Kept |

Ranking reason for option 1 over 2: the callback server is about 100 LOC (`callback-server.ts` is 183 lines in Pi, most of it HTML pages and cancellation), and the paste-only UX for ChatGPT is poor. The device poller is needed anyway for xAI. Option 2 only wins if the user wants the smallest possible H4 slip.

Anthropic caution, independent of option: the token only works with Claude Code identity (`anthropic-messages.ts:1013-1033`: Bearer plus identity headers; `:1106`: beta features `claude-code-20250219`, `oauth-2025-04-20`; `:95-98,727`: tool-name mimicry). INV H-AUTH-11 marks this "Legal or ToS risk" and the inventory Q1 is open (`INV:347`). The Anthropic flow can be built as a flow file, but the user needs to answer Q1 before the Anthropic lane starts. ChatGPT and xAI have no such dependency in Pi (they send the token as a plain key).

## 5. Go libraries

Checked against `/Users/dale/orca/workspaces/AskCore/master-2/go.mod` and the module cache.

| Need | Option | Status in repo | Fit |
|---|---|---|---|
| PKCE verifier and S256 challenge | `golang.org/x/oauth2` `GenerateVerifier`, `S256ChallengeOption`, `VerifierOption` (`oauth2@v0.36.0/pkce.go:27,42,57`) | `go.mod:385` indirect; build graph path `tokenplan -> fantasy/providers/anthropic -> x/oauth2`. Promote to direct, no new module. | Good. Replaces `pkce.ts`. |
| Auth-code URL and exchange | `oauth2.Config.AuthCodeURL`, `Exchange` | same | Partial. Pi sends provider-specific bodies: Anthropic posts JSON with `state` in the body (`anthropic.ts:104-115`), ChatGPT posts a form with extra `resource` and a dynamic client id (`openai-chatgpt.ts:16,252-260`). Hand-written `net/http` POST is simpler than bending `Config`. Use x/oauth2 for PKCE only. |
| Device flow | `Config.DeviceAuth`, `DeviceAccessToken` (`deviceauth.go:83,169`) | same | Partial. It polls on a ticker (waits before first poll: good), adds 5 s on `slow_down` (`:213-216`), stops on `access_denied` and `expired_token`. It does not use a server `interval` given in `slow_down` (Pi does, `device-code.ts:78-86`) and has no `expires_in` deadline except `ctx`. Fixable with `context.WithTimeout(ExpiresIn)`. A hand-written 50-line poller matching `device-code.ts` is the safer port; E§14 has RFC 8628 fixtures. |
| Refresh decision | `oauth2.Token.Valid` / `TokenSource` | same | Do not use. Default expiry delta is 10 s (`token.go:22`), and `TokenSource` has no cross-process lock. Ask needs its own 5 min margin inside `Modify`. |
| File lock | `github.com/gofrs/flock` v0.13.0 (`TryLockContext(ctx, retryDelay)`: `flock.go:152`) | `go.mod:192` indirect, only through golangci-lint (tool dep). New runtime dep, but already in `go.sum` and the module cache. | Good. Context-aware and cross-platform (Windows is M2). |
| File lock, alternative | `golang.org/x/sys/unix.Flock` | `x/sys` indirect at `go.mod:387` | No new module, but Unix only, and a manual retry loop. Use only if the user dislikes a new dep. |
| Atomic write | stdlib `os.CreateTemp` + `Sync` + `os.Rename` | stdlib | Required (see 1.4). |
| Browser open | `exec.Command("open"|"xdg-open", url)` or `rundll32 url.dll,FileProtocolHandler`; no shell | stdlib. `pkg/browser` is not in `go.mod`. | Mirror `C:utils/open-browser.ts:10-24`. About 15 LOC. Skip it when `ask -p` is headless or stdin is not a TTY (Ask already has a TTY check: `cmd/tui/headless.go:105`). |
| Proxy | stdlib `http.ProxyFromEnvironment`; `golang.org/x/net/http/httpproxy` for explicit config | `x/net` indirect `go.mod:384` | Stdlib is enough for env. E§14 test: `NO_PROXY` root and subdomain. |
| JWT parse | not needed by the shared core | n/a | Provider lanes may need it to read ChatGPT account ids. |

Lock design: lock file `auth.json.lock` next to the data file. `flock` is released by the kernel on process exit, so Pi's 30 s stale window and `onCompromised` are not needed. Keep a context deadline (30 s, as Pi) so a hung holder does not hang a CLI forever. Keep the rule "a cancelled waiter never runs `fn`": check `ctx.Err()` after acquiring and release first.

## 6. Edge cases (E§14) mapped to Go tests

Source: `plans/reports/researcher-260930-2254-pi-edge-cases.md:336-348`.

| E§14 case | Where it lands | Test |
|---|---|---|
| `auth.json` shared by parallel processes; preserve external edits; surface load/persist errors | Store | Multi-process `Modify` test (build test binary, run N copies); an external edit between two `Modify` calls survives |
| `/login` claimed success while `auth.json` was locked (0.80.4) | `ask login` | Read-only file: command exits non-zero and prints the write error |
| Refresh failure at startup must not crash or log out; 5 min window; stalled refresh releases the lock | Resolver | Fake IdP that hangs: refresh ends at 15 s, lock released, entry unchanged, other process still reads it |
| Callback port busy falls back to paste; provider `error=` fails fast; exchange before success page | Callback server | Bind conflict; `error=access_denied`; token endpoint returns 500 and the page shows failure |
| OAuth needs proxy env; no stderr write while TUI is active | Flows, CLI | Proxy test with `httptest` proxy; login output goes through the `Interaction`, never `os.Stderr` while a TUI runs |
| Device code `slow_down`, first-poll wait, no auto-open in headless | Poller | RFC 8628 fixtures; assert no poll before the first interval; assert no browser launch for `device_code` (Pi does not launch: `login-dialog.ts:116-124`) |
| OAuth over settings key; `ANTHROPIC_OAUTH_TOKEN` over `ANTHROPIC_API_KEY`; env var is not deleted after OAuth use | Resolver | Precedence table. Note Pi's `anthropicApiKeyAuth` checks `ANTHROPIC_AUTH_TOKEN`, then `ANTHROPIC_OAUTH_TOKEN`, then `ANTHROPIC_API_KEY` (`AI:providers/anthropic.ts:36-45`). A stored credential beats all env vars (`resolve.ts:27-31`). |
| Ambient `GH_TOKEN` must not enable Copilot | Not in H4b (Copilot is H17) | n/a |
| Startup without keys allowed | Resolver | Empty `auth.json`: no crash, "not configured" |
| Auth uses request-scoped `apiKey` and `env` | Hook | Per-request override wins over the store |
| `NO_PROXY` root and subdomains; CONNECT for plain HTTP | HTTP client | `httpproxy` table test |

Extra cases found in code, not in E§14:

| Case | Cite | Test |
|---|---|---|
| ChatGPT server must drop spare keep-alive connections on close, or the next login's callback hits the old server | `openai-chatgpt.ts:290-298` | Two logins in one process; second callback is handled |
| State mismatch in pasted input fails; missing `state` in pasted input is accepted | `anthropic.ts:178,216`, `openai-chatgpt.ts:56-58` | Both pasted forms |
| `login` and `logout` for one provider are serialised in the runtime; a login that started refresh while logout ran must not resurrect the entry | `model-runtime.ts:817-837`, `resolve.ts:129,147` | Logout during refresh: entry stays deleted |
| A stale token endpoint response with a missing `scope` is rejected for ChatGPT | `openai-chatgpt.ts:157-176` | Missing `chatgpt.tokens.use.direct` scope fails login |
| Token strings must never reach logs or events | Ask rule `internal/settings/README.md` "Credentials are never logged" | Log capture test on error paths |
| Pi's `Failed to read auth.json` invalid-entry error stops the whole file | `auth-storage.ts:228-252` | Decide for Ask: fail the one provider and keep others (recommended) or the whole file |

## 7. Recommendation

1. Choose option 1: split H4 into H4a (wires) and H4b (auth), with H4b before H5.
2. In H4b put these Pi rows: H-AUTH-02, H-AUTH-03, H-AUTH-08 (with refresh), H-AUTH-01 (the stored-over-env part), H-AUTH-06 (three providers), H-AUTH-07, H-AUTH-12 (env part). Add one Ask-only item: `ask login`, `ask logout`, `ask auth check`.
3. Keep in later phases: gateway `/login` (H13), TUI `/login` and `/logout` dialog (T1, M2), `httpProxy` setting (H6), Copilot and other vendors (H17), `auth print-*` (P2).
4. Pull only `auth.go`, `lock.go`, `paths.go` of `internal/settings` forward. `settings.json` and `merge.go` stay in H6.
5. Libraries: `x/oauth2` for PKCE only; `gofrs/flock` on a sidecar lock file; stdlib for atomic write, HTTP, browser launch.
6. Do not start the Anthropic flow until the user answers the ToS question (Q1). Build ChatGPT and xAI first; they carry no identity risk in Pi.
7. Update after approval: `RM` sections 0b, H4, H7, H17, section 9 and the `settings`, `providers` READMEs. Not done here (read-only task).

## 8. Limitations

- I did not read `radius.ts`, `kimi-coding.ts`, `openrouter.ts`, `github-copilot.ts` (not in scope).
- No live IdP call. Refresh-token rotation behaviour per provider is read from Pi code only, not tested.
- Whether fantasy's default HTTP client uses `DefaultTransport` is not verified.
- Effort numbers are rough LOC estimates from Pi file sizes, not measured.
- Pi sources checked: one repo at one commit. No second source for Pi behaviour beyond code and inventory; the Go library facts come from the module cache source, not release notes.

## Unresolved questions

1. Q1 (user decision): Is Anthropic OAuth with Claude Code identity (D9 to B, H-AUTH-11) accepted, given the ToS risk in `INV:140,347`? Until answered, Anthropic OAuth stays out of the first H4b slice.
2. Q2: Which package holds the resolver, flows and callback server? `internal/settings` may import only the stdlib (`internal/settings/README.md`), and `internal/providers` has no auth sub-package. Candidates: `internal/providers/oauth` (new) or a new `internal/auth`. Needs an import-rule check against `docs/ask-architecture-reference.md`.
3. Q3: Should `GetAPIKey` return a richer value (`{APIKey, Headers, BaseURL}`) now, or stay a string with a prefix sniff in the Anthropic adapter as Pi does (`anthropic-messages.ts:978`)? H4 is about to build on this hook.
4. Q4: Where does the ChatGPT host id live before H6 (a reserved key in `auth.json`, or a `~/.ask/device-id` file)?
5. Q5: Which margin layer: store the real expiry and apply one 5 min margin in the resolver (recommended), or copy Pi's two layers?
6. Q6: Do the fantasy Anthropic and OpenAI clients use `http.DefaultTransport` when no client is passed (proxy for free)? Needs a 5-minute check in the fork.
7. Q7: Does one invalid entry in `auth.json` fail the whole file (Pi) or only that provider (recommended)?
8. Q8: Does the user want the H4a/H4b split, or all of this inside one H4 with a longer duration? This changes the H4 exit line.

Status: DONE_WITH_CONCERNS
Summary: Pi's OAuth core is a locked read-merge-write store, a refresh inside the lock, PKCE, a loopback callback with paste fallback and a device poller. Ask has none of it today. Recommended cut: H4a wires plus H4b auth with `ask login`. This reverses D9 for Anthropic, the M1/M2 login split and the H7 order, so the user must confirm. Concern: the Anthropic ToS question (Q1) is open.
