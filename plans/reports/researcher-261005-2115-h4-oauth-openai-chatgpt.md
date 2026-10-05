# H4 research: OpenAI OAuth ("Sign in with ChatGPT") for Ask

Date: 2026-10-05. Scope: read-only research. Pi paths are relative to `pi/packages/ai/` (reference commit `4c6fb7cfe`) unless stated. Fantasy fork = `/Users/dale/Desktop/workspace/opensources/fantasy` (main, 0897676). Upstream = `charm.land/fantasy@v0.45.2`.

## 1. Outcome (read this first)

1. Pi has two different ChatGPT flows. They are not two versions of one flow.
2. Current flow: `openai-chatgpt.ts`. It signs in, then sends the token to the **public** Responses API `https://api.openai.com/v1/responses`. OpenAI documents this flow.
3. Legacy flow: `openai-codex.ts`. It uses the Codex CLI client id and the private `https://chatgpt.com/backend-api/codex/responses`. Pi marks it "legacy" (`providers/openai-codex.ts:10`; `README.md:1814`; `CHANGELOG.md:86`).
4. OpenAI's own docs now say: "do not point it at ChatGPT's `backend-api` endpoints" (source in section 7).
5. **Recommendation: port only the current flow (H-AUTH-06 ChatGPT row).** Use fantasy `providers/openai` with the Responses API. Drop the legacy flow and `openai-codex-responses` (H-PROV-05) from H4. Fantasy cannot talk to the Codex backend without new code (section 6), and the docs now forbid it.
6. Fantasy already covers about 80 percent of the request shape on the fork. Four items need work: per-request token injection, `max_output_tokens` removal, `include` for reasoning replay, and error mapping (section 6).

Ranked options:

| Rank | Option | Fit | Risk | Cost |
|---|---|---|---|---|
| 1 | Sign in with ChatGPT on `api.openai.com/v1` via fantasy `openai` provider | Matches H4 stack (`openai-responses`, `gpt-5.5`). Documented by OpenAI. | Preview feature. Docs may change. Eligibility limits (section 7). | Low. OAuth code in Go plus small fantasy glue. |
| 2 | Legacy Codex flow on `chatgpt.com/backend-api` | Needs a new transport (headers, body, SSE, optional WebSocket). Fantasy has no support. | High. Private endpoint. OpenAI docs say do not use it. Client id belongs to the Codex CLI. | High (Pi file is 1697 lines). |
| 3 | Do nothing. API key only. | Safe. | User goal not met. | None. |

## 2. OAuth flows in Pi (task item 1)

Which file is current: `openai-chatgpt.ts`. Evidence: it is wired to the `openai` provider (`providers/openai.ts:14-19`), and `openai-codex` is named "OpenAI Codex (legacy)" (`providers/openai-codex.ts:10`). The ChatGPT flow was added in 1.0.0 (`CHANGELOG.md:80`).

| Item | Current: `auth/oauth/openai-chatgpt.ts` | Legacy: `auth/oauth/openai-codex.ts` |
|---|---|---|
| Authorize URL | `https://auth.openai.com/api/accounts/authorize` (:19) | `https://auth.openai.com/oauth/authorize` (:24) |
| Token URL | `https://auth.openai.com/api/accounts/oauth/token` (:20) | `https://auth.openai.com/oauth/token` (:25) |
| Client id | Constant `DYNAMIC_CLIENT_ID = "dynamic_agent_client"` (:16). Used only for first registration. OpenAI issues a per-user id (`oaiapp_...`) in the callback `client_id` query param (:59-60). Pi stores it in the credential (`clientId`, :178). | Constant `CLIENT_ID = "app_EMoamEEZ73f0CkXaXp7hrann"` (:22). Fixed. Shared with the Codex CLI. |
| Scopes | `openid profile email offline_access resource.invoke chatgpt.tokens.use.direct` (:26-27). The grant must return `chatgpt.tokens.use.direct` or login fails (:169-172). | `openid profile email offline_access` (:34) |
| Extra authorize params | `agent_name_hint=Pi` (:17,253), `ext_agent_host_id=urn:uuid:<device uuid>` (:226-231,254), `resource=https://api.openai.com/v1` (:21,257), `nonce` (:240,262) | `id_token_add_organizations=true`, `codex_cli_simplified_flow=true`, `originator=pi` (:303-305) |
| Redirect | `http://127.0.0.1:1455/auth/callback` (:23-25) | `http://localhost:1455/auth/callback` (:26) |
| Listen host | `PI_OAUTH_CALLBACK_HOST` or `127.0.0.1` (:22,126). The redirect URI always uses the literal `127.0.0.1` (:25). | Same env var (:40-42) |
| PKCE | S256. `generatePKCE()` (:238), `code_challenge_method=S256` (:261). State and nonce are 32 random bytes in base64url (:49-51). | S256 (:292,301). State is 16 random bytes in hex (:66). |
| Code exchange | Form POST: `grant_type, client_id(issued), code, code_verifier, redirect_uri, resource` (:189-198). Public client, no secret (:4). | Form POST: `grant_type, client_id, code, code_verifier, redirect_uri` (:154-160) |
| id_token | Presence check only. No parsing. A missing id_token fails login (:200-204). | Not used. Pi does not read it. |
| Account id | Not needed. Pi sends only the access token (:306-308). | Decoded from the **access token** JWT, claim `https://api.openai.com/auth`.`chatgpt_account_id` (:35,310-315). Missing id fails login (:319-321). |
| Plan type | Not read anywhere in Pi. | Not read from tokens. `plan_type` appears only in the 429 error body (`api/openai-codex-responses.ts:1603-1610`). |
| Refresh | Form POST: `grant_type=refresh_token, client_id(stored), refresh_token, resource`. No `scope` param (:213-221; test `openai-chatgpt-oauth.test.ts:154-178`). Fails if no stored client id (:209-212). The response must contain a new `refresh_token` (:162-165; test :145-152). | Form POST with the fixed client id (:167-185) |
| Expiry | `Date.now() + expires_in*1000 - 3 min` (:29,177). Then the generic resolver refreshes when under 5 min remain, with a 15 s refresh timeout and a lock (`auth/resolve.ts:102-103,105-147`). | `Date.now() + expires_in*1000`, no margin (:141) |
| Device code | None. | Yes. `deviceauth/usercode`, poll `deviceauth/token`, verification page `/codex/device`, timeout 15 min (:27-31,187-287,341-357). `403/404` means pending (:262-264). `slow_down` is honored (:277). |
| Paste fallback | Yes, always offered in parallel to the callback (:271-279). Pasted URL must match origin and path of the redirect (:71-74). `error=` fails fast (:75-76). | Yes. Used when port 1455 is busy (:359-370). Accepts URL, `code#state`, `code=...&state=...`, or a bare code (:69-97). |
| Port busy | Hard error "Port 1455 is in use ..." (:243-248). Changed after `2bbfcca4` (section 5). | Silent fallback to paste (:370) |
| Device id | Required UUID from `LoginOptions.getDeviceId()` (:226-231). The coding agent persists it (`coding-agent/src/modes/interactive/interactive-mode.ts:6262`). | None |
| Label | "Sign in with ChatGPT" (:303); `isSubscription: true` (:302) | "OpenAI (ChatGPT Plus/Pro)", `isSubscription: true` (:407-408) |

Pi ignores the `earliest_refresh_at` token field. OpenAI's token page lists it (section 7).

## 3. Wire API, base URL and request shape (task item 2)

### 3.1 Current flow: `openai` provider with a ChatGPT token

| Item | Pi behavior | Evidence |
|---|---|---|
| API and base URL | `openai-responses` at `https://api.openai.com/v1` (public Responses API) | `providers/openai.ts:11,22` |
| Auth header | `Authorization: Bearer <access token>`. `toAuth` returns the access token as `apiKey`. | `openai-chatgpt.ts:306-308` |
| Detection | `isChatGPTSignIn`: provider is `openai`, base URL is the default, and the key does **not** start with `sk-`. | `api/openai-responses.ts:40-47` |
| Extra headers | `User-Agent`. With a session id: `session_id` and `x-client-request-id`. No `chatgpt-account-id`, no `OpenAI-Beta`, no `originator`. | `api/openai-responses.ts:267,277-286` |
| Body: always | `model`, `input`, `stream:true`, `store:false`, `prompt_cache_key` (not when `cacheRetention=none`) | `api/openai-responses.ts:330-338` |
| Body: omitted for ChatGPT | `max_output_tokens`, `temperature`, `prompt_cache_retention`, `prompt_cache_options` | `api/openai-responses.ts:328-346`; test `test/openai-responses-chatgpt-sign-in.test.ts:44-76` |
| Reasoning | If a level is set: `reasoning:{effort,summary}` plus `include:["reasoning.encrypted_content"]`. If off: `reasoning:{effort:"none"}`. | `api/openai-responses.ts:363-377` |
| System prompt | Sent as an `input` item with role `developer` for reasoning models (not `system`). | `api/openai-responses-shared.ts:213-215` |
| Retries | SDK `maxRetries:0`. Pi retry wrapper around the call. | `api/openai-responses.ts:178-190` |
| Transport | HTTP SSE only. No WebSocket. | `api/openai-responses.ts:183` |
| Models | Pi reuses the full `OPENAI_MODELS` catalog with no subscription filter. OpenAI docs say to call `GET /v1/models` and keep `visibility == "list"`. Pi does not do this. | `providers/openai.ts:21`; `grep isSubscription` shows no model gating in `ai/src` or `coding-agent/src` |

The catalog data files are generated and git-ignored (`.gitignore:11` pattern `packages/ai/src/providers/data/`), so I could not list the `openai` model ids from this checkout.

### 3.2 Legacy flow: `openai-codex-responses`

| Item | Pi behavior | Evidence |
|---|---|---|
| Base URL and path | `https://chatgpt.com/backend-api` + `/codex/responses`. WebSocket URL is the same with `wss://`. | `api/openai-codex-responses.ts:52,641-654` |
| Headers (both transports) | `Authorization: Bearer`, `chatgpt-account-id`, `originator: pi`, `User-Agent` | :1640-1659 |
| Headers (SSE) | `OpenAI-Beta: responses=experimental`, `accept: text/event-stream`, `content-type: application/json`, `session-id`, `x-client-request-id` | :1661-1679 |
| Headers (WebSocket) | `OpenAI-Beta: responses_websockets=2026-02-06`, `x-client-request-id`, `session-id`. No `accept` or `content-type`. | :865,1681-1697 |
| Body | `model, store:false, stream:true, instructions, input, text:{verbosity}, include:["reasoning.encrypted_content"], prompt_cache_key, tool_choice, parallel_tool_calls:true`. Optional: `temperature, service_tier, tools, reasoning`. | :562-600 |
| Difference vs public API | `instructions` is required. The system prompt moves out of `input` to `instructions` (`includeSystemPrompt:false`, :540; fallback text "You are a helpful assistant." :564). `include` is always set. `text.verbosity` default is `low`. Tool strictness is `null` (:545,:592). No `max_output_tokens`. | :540-600 |
| Account id | Decoded from the access token JWT on every request. Failure: "Failed to extract accountId from token". | :270,1627-1638 |
| SSE compression | Body is zstd-compressed when Node/Bun zstd exists. Header `content-encoding: zstd`. WebSocket frames stay plain JSON. | :60,379-384 |
| SSE parsing | Reads `data:` lines. Ignores `[DONE]`. Terminal event at EOF without blank line is processed. | :778-830 |
| Event mapping | `response.done`, `response.completed`, `response.incomplete` all become `response.completed`. Status is normalized to a known set. `error` and `response.failed` become `CodexApiError`. | :730-788 |
| Shared code | Uses `convertResponsesMessages`, `convertResponsesTools`, `processResponsesStream` from `openai-responses-shared.ts` (:28 import; calls :540,:592,:668). Codex adds only its own event mapper, headers and body builder. | `api/openai-codex-responses.ts` |

### 3.3 Transport: SSE vs WebSocket (legacy provider only)

| Item | Behavior | Evidence |
|---|---|---|
| Options | `Transport = "sse" | "websocket" | "websocket-cached" | "auto"` | `types.ts:118,213` |
| Default | `auto`. WebSocket first, then SSE fallback. | `api/openai-codex-responses.ts:294` |
| Cached continuation | Used for `websocket-cached` **and** `auto` (:1518). Pi keeps the last request body and last response items per socket. If the new input extends them, Pi sends only the delta plus `previous_response_id` (:1425-1491). Request body must match except `input` (:1434-1436). |
| Message frame | `{"type":"response.create", ...body}` (:1535) |
| Why it works with `store:false` | "Store must be set to false"; continuation uses connection-scoped state (:1520-1521) |
| Connection cache | Key: session id, then account id (:910,1153-1248). TTL idle 5 min, max age 55 min (backend limit is 60 min) (:866-867). Busy socket means open a second socket. |
| Fallback rules | Connect timeout 15 s (:57). On failure before the first event: fall back to SSE for the whole session (:967-986,347-375). After the first event: the error is final (:373-374). `previous_response_not_found` and `websocket_connection_limit_reached` each get one retry (:63-64,339-352). Code 1009 is "message too big" (:62,1279). |
| Completion | Socket close after a completion event is normal. Close without completion is an error (:1366-1417). Idle timeout comes from `timeoutMs` (:1393-1406). |
| Quirk | `connectWebSocket` runs `delete wsHeaders["OpenAI-Beta"]` (:1087). `headersToRecord` returns lower-case keys (`utils/headers.ts:5-7`), so the delete does nothing. The header **is** sent on the handshake. A Go port must send `openai-beta: responses_websockets=2026-02-06`. No Pi test checks this (`test/openai-codex-stream.test.ts` checks only the SSE value, line 162). |

Note: OpenAI's public Responses API also has a WebSocket mode. The openai-go fork documents it (`openai-go/README.md:358-497`). Pi does not use it on the `openai` provider.

## 4. Rate limits, usage limits, errors, retries (task item 3)

| Case | Pi behavior | Evidence |
|---|---|---|
| `subscription_sharing_usage_limit_exceeded` (HTTP 429 or mid-stream `response.failed`) | Not retried. Error text gets `\nCheck your ChatGPT usage: https://chatgpt.com/settings/usage` appended. | `utils/retry.ts:26-28`; `api/openai-responses.ts:226-229`; tests `test/openai-responses-usage-limit.test.ts:39-67` |
| `subscription_sharing_usage_unavailable`, `subscription_sharing_user_unavailable` | Retried. Can arrive mid-stream without a 503. | `utils/retry.ts:98-101` |
| Generic retryable text | `429`, `5xx`, "overloaded", "rate limit", "timeout", "websocket closed", and so on | `utils/retry.ts:32-102` |
| Terminal-limit regex (non-retry) | `insufficient_quota`, `billing`, `quota exceeded`, `GoUsageLimitError` ... | `utils/retry.ts:7-28`; codex copy `api/openai-codex-responses.ts:123-127` |
| Backoff | `baseDelayMs * 2^(attempt-1)`, capped at 60 s | `utils/retry.ts:112-116` |
| Legacy: friendly usage message | On code `usage_limit_reached`, `usage_not_included`, `rate_limit_exceeded`, or any 429: "You have hit your ChatGPT usage limit (<plan> plan). Try again in ~N min." Uses `error.plan_type` and `error.resets_at` (epoch seconds). | `api/openai-codex-responses.ts:1596-1625` |
| Legacy: SSE retry | Default `maxRetries` is 0 (:54). `retry-after-ms`, then `retry-after` seconds, then HTTP date (:139-164). A delay over the limit raises `RetryDelayExceededError` (:166-176). Error text containing "usage limit" stops network-error retries (:453). | `api/openai-codex-responses.ts` |
| Legacy: tests | `retry-after` variants (`test/openai-codex-stream.test.ts:2433-2506`); delay over limit for 429 and 503 (:2507-2546); backoff without headers (:2620). |

OpenAI error table for the current flow (primary source, section 7): `user_not_eligible` 403 (do not repeat), `usage_limit_exceeded` 429 (pause), `usage_unavailable` 503 (retry), `unsupported_capability` 400 (remove input), `route_not_supported` 403, `invalid_user` 401 (sign in again), `user_unavailable` 503 (retry).

Pi gaps against that table: Pi has no special handling for `user_not_eligible`, `invalid_user`, `unsupported_capability`, `route_not_supported`. They surface as plain errors. A Go port should map them (section 8).

The task text mentions "R:227". I read this as `openai-responses.ts:227`, the usage-limit message rewrite. I did not find another "R:227" in the files named.

## 5. Pi tests (task item 4)

| Test file | Covers |
|---|---|
| `test/openai-chatgpt-oauth.test.ts` (179 lines) | Dynamic client registration, issued client id stored, scope grant required, no issued id rejected, device id required, refresh rotation required, refresh uses stored client id and 3 min margin |
| `test/openai-responses-chatgpt-sign-in.test.ts` (77 lines) | Omitted fields for non-`sk-` key. Kept fields for `sk-` key and for other base URLs. |
| `test/openai-responses-usage-limit.test.ts` (68 lines) | Usage-page link on HTTP 429 and on mid-stream `response.failed` |
| `test/openai-codex-oauth.test.ts` (531 lines) | Device code, method select, cancel, 15 min timeout, 403/404 pending, poll failure body, refresh stderr, paste fallback when port taken |
| `test/openai-codex-stream.test.ts` (2696 lines) | SSE and WebSocket streaming, headers, cache keys, 64-char clamp, tool choice, strict mode, retries, fallback, delta mode, zstd, account-scoped sockets |
| `test/openai-codex-cache-affinity-e2e.test.ts`, `test/codex-websocket-cached-probe.ts` | Live probes (need credentials) |
| `test/oauth-callback-server.test.ts`, `test/oauth-auth.test.ts` | Shared callback server and generic OAuth |

Not covered by any Pi test: port-in-use error for the ChatGPT flow (added after `2bbfcca4`), `isChatGPTSignIn` on a non-default base URL for the Codex path, the `OpenAI-Beta` WebSocket header.

## 6. Changes between `2bbfcca4` and `4c6fb7cfe` (task item 5)

Command: `git diff --stat 2bbfcca4 4c6fb7cfe -- packages/ai` filtered for OpenAI and OAuth files.

| File | Change |
|---|---|
| `src/auth/oauth/openai-chatgpt.ts` (+23 lines) | Port 1455 busy is now a hard error "Port 1455 is in use, probably by an unfinished login in another pi session or by the Codex CLI". Before: Pi continued with paste only. Reason: the browser then showed "OAuth state mismatch" (`CHANGELOG.md:20`, issue 10265). The callback server is now always required (:243-248,282). |
| `src/api/openai-responses-shared.ts` (13 lines, :294-306) | Replayed tool-call item ids are dropped unless the prefix matches the replayed type: `fc_` for function calls and `ctc_` for custom tool calls. A different-model message always drops the id. |
| `src/utils/retry.ts` (+1 line) | "model is at capacity" is retryable (commit `3874b3e98`) |
| `src/auth/oauth/anthropic.ts`, `src/utils/oauth-page.ts`, `test/anthropic-oauth.test.ts`, `test/oauth-callback-server.test.ts` | Not OpenAI-specific. Anthropic copy-code flow and callback page text. |
| `src/api/openai-codex-responses.ts`, `src/auth/oauth/openai-codex.ts`, `src/providers/openai-codex.ts`, `src/api/openai-responses.ts`, `src/providers/openai.ts`, all `openai-codex*` tests | **No change.** |

## 7. Fantasy support check (task item 6)

Sources: fork `providers/openai/*.go`; upstream `charm.land/fantasy@v0.45.2/providers/openai`; `charmbracelet/openai-go@v0.0.0-20260921175203-216db9e71b83`.

Search result: no code for `chatgpt`, `chatgpt-account-id`, a Codex base URL, or WebSocket in fantasy `providers/openai` (grep on `codex|chatgpt|websocket|wss://`). The only `codex` hits are model-name lists (`responses_options.go:216,295-328`) and the reasoning-model test (`responses_language_model.go:126`).

Verdicts are for the **current flow** (public Responses API, `gpt-5.5`).

| Need | Fantasy status | Evidence | Gap |
|---|---|---|---|
| Custom base URL | Yes | `openai.go:69` `WithBaseURL`; SDK path is `responses` (`openai-go/responses/response.go:68`) | None for `api.openai.com/v1` |
| Bearer token | Static only | `openai.go:76` `WithAPIKey` | Token rotates every hour. Provider needs a per-request token. Use `WithSDKOptions(option.WithMiddleware(...))` (`openai-go/option/requestoption.go:102`) or `WithHTTPClient` with a RoundTripper that reads the credential store. **Work needed.** |
| Extra headers | Yes | `openai.go:104` `WithHeaders`; per-call headers exist (`responses_language_model.go:1038`) | None for the current flow. Legacy flow would need `chatgpt-account-id`, `originator`, `OpenAI-Beta`. |
| `stream:true` | Yes | `NewStreaming` at `responses_language_model.go:1038` | None |
| `store:false` | Yes, default | `responses_language_model.go:196-199` | None |
| Developer role for system prompt | Yes for `gpt-5*` | `gpt-5` match `:126`; mode `"developer"` `:141`; emitted `:482-483`. The `"system"` mode is the default only for non-reasoning models (`:108`). OpenAI rejects explicit `role:"system"` items for this flow (preview-limitations page). | Do not use non-reasoning model ids on this flow. Test the `gpt-5.5` body in a unit test. |
| Omit `max_output_tokens` | Not automatic | Set whenever `call.MaxOutputTokens != nil` (`:257-259`). The reasoning branch clears `temperature` and `topP` only (`:322-337`). | Ask must pass nil, or strip the field in the middleware. **Work needed.** |
| Omit `temperature` | Yes for reasoning models | `:322-330` (with a warning) | None |
| `prompt_cache_retention` and `prompt_cache_options` | Not typed. Only via `ExtraBody`. | Fork has `ExtraBody` (`:390-391`, `responses_extra_body_test.go`). Upstream v0.45.2 has none (grep found no `ExtraBody`; no `*extra*` file). | Fork-only. Ask uses the fork, so OK. Never set these two fields for the ChatGPT flow. |
| `prompt_cache_key` | Yes | `:286-287` | None |
| `include: reasoning.encrypted_content` | Option exists, not automatic | `responses_options.go:205-206`; `:296-297` | Ask must add it when reasoning is on (Pi does: `openai-responses.ts:372`). With `store:false`, replay of reasoning items depends on it. Fork has replay tests (`responses_reasoning_replay_test.go`, fork-only). |
| `previous_response_id` | Rejected when `store=false` | `:159,210-215` | Matches OpenAI: unsupported over HTTP for this flow. No gap. |
| Mid-stream `response.failed` and `error` | Yes. `ResponsesError` has `Code`. | `responses_errors.go:16-20,36-47`; stream cases `responses_language_model.go:1336-1346` | Ask must map `Code == subscription_sharing_usage_limit_exceeded` to the usage message. Fork-only (`responses_errors.go` absent upstream). |
| Pre-stream HTTP errors | `ProviderError` with status, body, headers | `error.go:26-66` | OpenAI says pre-stream failures can be `{"detail":"..."}`. I did not find `detail` parsing. `ResponseBody` is kept, so Ask can read it. **Unverified**; test with a stub. |
| Retries | Not in provider | Ask owns the loop | Port Pi rules (section 4) |
| Model list from `/v1/models` | Not in fantasy | No list call in `providers/openai` | Ask calls `GET /v1/models` itself (optional) |
| WebSocket | No in fantasy | Zero WebSocket code in `providers/openai`. The SDK fork documents Responses WebSockets (`openai-go/README.md:358-497`). | Optional later. Not needed for H4. |
| Legacy Codex backend | Not possible without new code | Needs `instructions` mapping (fantasy has `Instructions` option, `responses_options.go:163`, `:280`, but still emits the system prompt into `input`), `chatgpt-account-id` from JWT, `/codex/responses` path, event aliasing (`response.done`), `text.verbosity`, `tool strict:null` | Large. Not recommended. |

`isChatGPTSignIn` does not port. It inspects the key prefix inside the provider (`openai-responses.ts:40-47`). Ask should decide from the **auth source** (OAuth credential vs `OPENAI_API_KEY`) before it builds the call, and then apply the field omissions.

## 8. OpenAI statements on third-party use (task item 7)

Facts only. Fetched 2026-10-05.

| Statement | URL |
|---|---|
| "ChatGPT plan usage is available to open-source projects, personal projects that run locally, and selected private apps." Commercial apps must "join the waitlist to request access before offering it to users." (quoted by fetch summary; wording of the second sentence is paraphrased by the summarizer) | https://developers.openai.com/cookbook/articles/sign-in-with-chatgpt |
| "In addition to identity scopes, your open-source app can request permission to use the user's ChatGPT plan for eligible Responses API requests." | https://developers.openai.com/siwc/token-sharing-open-source |
| Endpoint is `POST https://api.openai.com/v1/responses`. "Use the public Responses API endpoint above for this ChatGPT plan usage flow; do not point it at ChatGPT's `backend-api` endpoints." Set `store:false`, `stream:true`. List models with `GET https://api.openai.com/v1/models`, keep `visibility == "list"`, pass `slug` as `model`. | https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference |
| Registration: "Start first-time registration with `client_id=dynamic_agent_client`, include the host's `ext_agent_host_id`, and set `agent_name_hint` to your app's actual name." Scopes: `openid profile email` plus `offline_access resource.invoke chatgpt.tokens.use.direct`. Resource `https://api.openai.com/v1`. Example redirect `http://127.0.0.1:1455/auth/callback`; only the port may vary. Public client, no secret. Store credentials with owner-only permissions. Do not store `dynamic_agent_client` as the issued id. | https://developers.openai.com/siwc/token-sharing-open-source/sign-in |
| Host id formats: `urn:ietf:params:oauth:jwk-thumbprint:...` (recommended), `urn:uuid:...`, `did:key:...`. Pi uses `urn:uuid`. | https://developers.openai.com/siwc/token-sharing-open-source |
| Access token valid 1 hour. Refresh token valid 30 days, each refresh issues a replacement with a fresh 30 days. Token response includes `earliest_refresh_at`. | https://developers.openai.com/siwc/token-sharing-open-source/token-reference |
| Preview limits: omit `background`, `conversation`, `max_output_tokens`; explicit `role:"system"` message items are rejected; no image generation, file search, Code Interpreter, native computer use, hosted MCP or connectors; `previous_response_id` over HTTP unsupported; WebSocket continuation only on the same connection. | https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations |
| Errors and refresh failures (`invalid_grant`, `refresh_token_reused`, and so on mean re-authenticate). "OpenAI does not currently notify your tool when a user disconnects." | https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery |
| Codex CLI: tokens are cached at `~/.codex/auth.json` or the OS credential store. "Treat `~/.codex/auth.json` like a password". | https://learn.chatgpt.com/docs/auth |
| News coverage: ChatGPT subscribers can use plan allowance in third-party tools. Users set weekly usage caps per app. (Search snippet only, secondary source.) | https://thenewstack.io/sign-in-with-chatgpt/ (page body not retrievable; snippet from search result) |

What I did not find: any OpenAI statement that allows or forbids third-party tools to reuse the **Codex CLI client id** (`app_EMoamEEZ73f0CkXaXp7hrann`) with `chatgpt.com/backend-api`. Third-party docs (assistant-ui, OpenClaw) describe this as personal use; they are not OpenAI sources. The help-center article for Codex plans returned HTTP 403 and was not read.

Fact on Pi's own claim: Pi says the ChatGPT flow is a public-client flow with "no client secret" (`openai-chatgpt.ts:4`). OpenAI's sign-in page says the same.

## 9. Edge cases a Go port must keep (task item 8)

Current flow (must keep):

| # | Edge case | Source |
|---|---|---|
| 1 | Persist a stable device UUID before first login. Reject a missing or non-UUID id before opening the browser. Format `urn:uuid:<lowercase>`. | `openai-chatgpt.ts:18,226-231`; test :130-143 |
| 2 | First login uses `client_id=dynamic_agent_client`. Read the issued `client_id` from the callback. Error if absent. Store it per credential. Never store the dynamic id. | :16,59-60,178; test :111-118 |
| 3 | Fixed redirect `http://127.0.0.1:1455/auth/callback`. The same string in authorize and exchange. If port 1455 is busy, fail with a clear error (do not continue). OpenAI allows other ports; Pi does not use them. | :23-25,243-248 |
| 4 | Verify `state`. Reject missing or different state. Reject missing `code`. Fail fast on `error=` in the callback. | :53-62,102-107 |
| 5 | Paste fallback races the callback server. Pasted URL must match origin and path of the redirect. | :64-78,271-282 |
| 6 | Callback server: close it and force-close idle keep-alive connections after login. A browser spare connection can deliver the next login's callback to the old server and cause a false state mismatch. | :289-297 |
| 7 | Require `chatgpt.tokens.use.direct` in the returned `scope`. Require `access_token`, `refresh_token`, `scope`, positive numeric `expires_in`, and an `id_token` on the first exchange. | :162-181,200-204; test :120-128 |
| 8 | Refresh: send the stored `client_id` and `resource`; send no `scope`. The reply must carry a new `refresh_token` (rotation). Store the replaced scopes. | :208-223; tests :145-178 |
| 9 | Expire 3 min early. Refresh when under 5 min remain. Refresh under a lock, re-check expiry inside the lock, 15 s timeout, persist the rotated credential before releasing. | `openai-chatgpt.ts:29,177`; `auth/resolve.ts:102-147` |
| 10 | Apply the field omissions (`max_output_tokens`, `temperature`, `prompt_cache_retention`, `prompt_cache_options`) based on auth source, not key prefix. Keep `store:false`, `stream:true`. | `openai-responses.ts:328-346` |
| 11 | System prompt as `developer` role for reasoning models. | `openai-responses-shared.ts:213-215` |
| 12 | Usage-limit code: no retry, add usage link. `usage_unavailable` and `user_unavailable`: retry with backoff, also when mid-stream. | `retry.ts:26-28,98-101`; `openai-responses.ts:226-229` |
| 13 | Replayed tool-call ids: keep only if prefix matches (`fc_` or `ctc_`) and the model is the same. | `openai-responses-shared.ts:294-306` |
| 14 | Credential file: owner-only permissions. Never print tokens. | OpenAI sign-in page |
| 15 | Add (not in Pi): handle `invalid_grant`, `refresh_token_reused`, `refresh_token_invalidated`, `invalid_refresh_token`, `token_expired`, `refresh_token_expired` by asking the user to sign in again, and keep the credential until a real API failure confirms disconnect. Handle `user_not_eligible` (do not repeat) and `invalid_user` (sign in again). | OpenAI errors page |
| 16 | Add (not in Pi): the model list may need `GET /v1/models` with `visibility == "list"`. Pi shows the full catalog, so a user can pick a model the plan does not offer. | OpenAI models page; `providers/openai.ts:21` |

Legacy flow (only if the legacy flow is ever ported; not recommended):

| # | Edge case | Source |
|---|---|---|
| L1 | JWT segments are base64url. Pi decodes with `atob`, which rejects `-` and `_`. A token whose payload has those characters fails with "Failed to extract accountId". A Go port should use `base64.RawURLEncoding`. | `openai-codex.ts:99-109`; `openai-codex-responses.ts:1627-1638` |
| L2 | The `OpenAI-Beta` WebSocket header is sent although the code tries to delete it. Keep the sent value. | `openai-codex-responses.ts:1087` (section 3.3) |
| L3 | Account-scoped socket cache. Rotate sockets at 55 min. Fall back to SSE per session after a pre-start failure. | :866-867,967-986 |
| L4 | `session_id` and `prompt_cache_key` clamp to 64 characters. Sessionless WebSocket requests use UUIDv7 as request id (UUIDv4 is rejected by some models). | :282; test `openai-codex-stream.test.ts:702,752`; `CHANGELOG.md:499` |
| L5 | `store:true` is rejected: "Store must be set to false". | :1520 |
| L6 | Model ids differ from the `openai` catalog and are hardcoded (`gpt-6.1-sol`, `gpt-6-astra`, `gpt-6-sol`, `gpt-6-luna`, `gpt-5.3-codex-spark`, `gpt-5.5`, `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`; context 272000, max tokens 128000, Spark 128000 context). GPT-5.4 was removed when the backend stopped serving it. | `scripts/generate-models.ts:3225-3340`; `CHANGELOG.md:177` |

## 10. Suggested H4 inventory changes (for the user to decide)

| Row | Current | Suggested |
|---|---|---|
| H-AUTH-06 | P1 for Anthropic, ChatGPT, Copilot | Keep ChatGPT at P1. Define it as the current flow only. Mark `openai-codex.ts` out of scope. |
| H-AUTH-07 | Port-busy falls back to manual paste | For ChatGPT: port-busy is a hard error. Anthropic and others keep the fallback. |
| H-PROV-05 | `openai-codex-responses` P2 | Move to "won't port" or keep P3. Reason: OpenAI docs forbid `backend-api` for this flow. WebSocket stays only as an optional later feature on the public API. |
| H4 scope | `openai-completions`, `openai-responses` with API key | Add: ChatGPT OAuth credential, token-injection middleware, field omission by auth source, usage-limit mapping. |

I did not change any file. These are proposals.

## Limitations

- OpenAI docs are a preview. They can change. I read each page once through a summarizing fetch tool, so quotes are short and some are paraphrased. Re-read the primary pages before coding.
- I did not run Pi or any live request. No live check of the `openai` plan model list, `/v1/models` output, or the `{"detail":...}` error shape.
- Pi catalog data files are git-ignored and absent, so the `openai` model ids for this flow are not listed.
- Only one secondary source (news snippet) was available for the usage-cap claim. The OpenAI help article returned HTTP 403.
- I did not review `callback-server.ts`, `device-code.ts`, `pkce.ts` line by line. They are shared by other providers.

## Unresolved questions

1. Does OpenAI allow third-party tools to use the Codex CLI client id with `chatgpt.com/backend-api`? I found no primary source either way. The new docs say to use the public endpoint instead.
2. Is Ask eligible under the "open-source projects, personal projects that run locally" rule, or does it need the commercial waitlist? Ask runs as a local daemon and also as a cloud agent (project `CLAUDE.md`). The cloud mode may count as a "private app" or commercial use.
3. Which redirect port does Ask use? Pi fixes 1455 and fails if busy. OpenAI says only the port may vary. Ask could try more ports. This is a design decision for the user.
4. Which host id format does Ask use: `urn:uuid` (as Pi) or the recommended JWK thumbprint?
5. Should Ask filter the model picker with `GET /v1/models` (`visibility == "list"`), or show the static catalog like Pi?
6. Does the fantasy `openai` provider map a pre-stream `{"detail": "..."}` body to a readable message? Needs a stub test.
7. Should Ask keep `earliest_refresh_at` to schedule refresh? Pi ignores it.

Status: DONE_WITH_CONCERNS
Summary: Pi's current ChatGPT flow (`openai-chatgpt.ts`) sends the token to the public Responses API and fantasy's fork can carry it with small glue; the legacy Codex backend flow is not supported by fantasy and OpenAI docs now forbid `backend-api`.
Concerns/Blockers: OpenAI's feature is a preview with eligibility limits (open-source, local personal, or waitlist); no primary source found on the legacy Codex client id; Ask's cloud-agent mode may need the commercial waitlist.
