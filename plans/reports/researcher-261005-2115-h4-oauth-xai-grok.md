# H4 research: xAI (Grok) OAuth login for Ask

Date: 2026-10-05. Pi pin: `4c6fb7cfe`. Paths below are relative to `pi/packages/` unless marked.
No secret values were read or printed. Web content was treated as data only.

## Outcome

- Pi's xAI OAuth is a plain RFC 8628 device-code flow. It has no PKCE, no callback server, and no account or team selection. The whole flow is 239 lines (`ai/src/auth/oauth/xai.ts:1-239`).
- The OAuth token is sent as a normal Bearer API key to `https://api.x.ai/v1/responses`. There is no OAuth-specific base URL, header, or model list (`ai/src/providers/xai.ts:11-22`; test at `ai/test/xai-responses.test.ts:164-200`).
- The Go port is small. Fantasy can reach the endpoint, but it needs four code-level settings (see section 7).
- Official xAI backing is only partial. xAI ships a Grok Build CLI with subscription sign-in. I found no xAI text that permits or forbids third-party harnesses. Section 8 has the facts.
- Recommendation: add xAI OAuth to H4 as P1, behind the same "subscription use policy" decision as H-AUTH-06 (unresolved Q1 in the inventory). Do not ship it as a default-on feature until the user decides.

## 1. OAuth flow (`ai/src/auth/oauth/xai.ts`)

| Item | Value | Source |
|---|---|---|
| Flow | Device code (RFC 8628). Header comment: "xAI OAuth device-code flow". | `xai.ts:2` |
| Device-code URL | `https://auth.x.ai/oauth2/device/code` | `xai.ts:10` |
| Token URL | `https://auth.x.ai/oauth2/token` | `xai.ts:11` |
| Client id constant | `XAI_CLIENT_ID` (a fixed public UUID, the Grok CLI client) | `xai.ts:8` |
| Scope constant | `XAI_SCOPE` = `openid profile email offline_access grok-cli:access api:access` | `xai.ts:9` |
| Extra form field | `referrer=pi` on the device-code request | `xai.ts:151` |
| Device request | POST form: `client_id`, `scope`, `referrer` | `xai.ts:145-154` |
| Poll request | POST form: grant `urn:ietf:params:oauth:grant-type:device_code`, `client_id`, `device_code`. No client secret. | `xai.ts:168-176` |
| First poll | Waits one interval before the first poll (`waitBeforeFirstPoll: true`) | `xai.ts:165` |
| Poll errors | `authorization_pending` continue; `slow_down` raise interval; `access_denied` or `authorization_denied` fail; `expired_token` fail; other fail with HTTP detail | `xai.ts:182-196` |
| Refresh | POST form: `grant_type=refresh_token`, `client_id`, `refresh_token` | `xai.ts:213-227` |
| Refresh token reuse | If the response has no `refresh_token`, keep the old one | `xai.ts:130-134` |
| Expiry | `expires = now + expires_in*1000 - 5 min` (`REFRESH_SKEW_MS`) | `xai.ts:13,141` |
| Default lifetime | 3600 s when `expires_in` is missing | `xai.ts:14,135-136` |
| Subscription target | Name "xAI (Grok/X subscription)", `isSubscription: true`, label "Sign in with SuperGrok or X Premium" | `xai.ts:229-232` |
| Account or team selection | None. Credential holds only `type`, `access`, `refresh`, `expires`. | `xai.ts:137-142` |
| Request auth | `toAuth` returns `{ apiKey: credential.access }` | `xai.ts:236-238` |

Edge cases in the flow:

- The login UI gets `verification_uri_complete` when xAI sends it, else `verification_uri` (`xai.ts:206`).
- Both URIs must be `https:`. Anything else throws "Untrusted verification URI" (`xai.ts:49-62,114-121`). The reason: the URI is opened in the user's browser.
- `interval` of 0 or non-numeric falls back to the poller default of 5 s (`xai.ts:109-113`; `device-code.ts:7`).
- Missing required fields throw `Invalid xAI OAuth response field: <name>` (`xai.ts:33-47`).
- Non-JSON bodies throw "returned invalid JSON (HTTP n)" (`xai.ts:83-92`).
- Abort during fetch throws "Login cancelled" (`xai.ts:76-81`).
- Error text keeps `error: error_description` for logs (`xai.ts:100-106`). Go must not log token values.

## 2. Provider and wire API

| Item | Value | Source |
|---|---|---|
| Wire API | `openai-responses` (all built-in xAI models) | `ai/src/providers/xai.ts:7,22`; test `xai-responses.test.ts:129` |
| Base URL, OAuth or key | `https://api.x.ai/v1` for both. Final URL `.../v1/responses`. | `xai.ts:11`; test `xai-responses.test.ts:178` |
| API key env | `XAI_API_KEY` | `providers/xai.ts:13` |
| Auth header | `Authorization: Bearer <token>`. The OAuth access token is passed as `apiKey`. | test `xai-responses.test.ts:179` |
| Default headers | `User-Agent: <pi UA>`, `session_id: <id>` when a session exists, `x-client-request-id` | `ai/src/api/openai-responses.ts:267,278-285`; test `:179-181` |
| Body fields | `store:false`, `stream:true`, `prompt_cache_key=<session>`, `reasoning.effort`, `include:["reasoning.encrypted_content"]` | test `:183-190` |
| Encrypted reasoning | Always set for provider `xai`, even with no effort | `openai-responses.ts:378`; test `:202-215` |
| No long cache retention | `supportsLongCacheRetention: false`, so no `prompt_cache_retention` | `generate-models.ts:472-474`; test `:191` |
| System prompt role | `developer` | test `:192-199` |
| Thinking map | xAI models with no verified effort options get `off: null, minimal: null` (never send `none` or `minimal`) | `generate-models.ts:1066-1070` |
| Extra effort levels | `xhigh` for Grok 4.7 | test `:217-238` |
| Model list for subscription users | The same built-in catalog as for API keys. No OAuth-specific list. | `providers/xai.ts:21` |
| Catalog source | models.dev, entries with `tool_call: true`, mapped to `api: "openai-responses"` | `generate-models.ts:2041-2061` |
| Excluded ids | `grok-3`, `grok-3-fast`, `grok-4.20-0309-non-reasoning`, `grok-4.20-0309-reasoning`, `grok-build-0.1`, `grok-code-fast-1` | `generate-models.ts:464-471,2792` |
| Default model | Grok 4.7 (was 4.6, then 4.5) | `coding-agent/CHANGELOG.md:237,247,545,1083` |
| Catalog data file | `ai/src/providers/data/xai.json` is git-ignored and absent. I could not list the model ids from the repo. Ids named in tests: `grok-4.3`, `grok-4.5`, `grok-4.7`. | `.gitignore:11` |

Quirk: the `reasoning.encrypted_content` include is keyed on `model.provider === "xai"`, not on the token type (`openai-responses.ts:378`). It is sent for API-key users too.

## 3. Helpers, loader, registration

| Item | Finding | Source |
|---|---|---|
| `device-code.ts` | Used. `pollOAuthDeviceCodeFlow` owns the deadline, interval, `slow_down` and cancel logic. | `xai.ts:6,162`; `device-code.ts:46-98` |
| Poller rules | Min interval 1 s. Default 5 s. `slow_down` uses the server `interval` if positive, else adds 5 s. Deadline from `expires_in`. A timeout after a `slow_down` gives a clock-drift message. | `device-code.ts:5-9,51-54,76-87,97` |
| `load.ts` | `loadXaiOAuth` lazy-imports `./xai.ts`. Standalone binaries use a registered loader. | `load.ts:22,68-71` |
| Registration | In the provider factory via `lazyOAuth({... load: loadXaiOAuth})`. The name and label are duplicated there. | `providers/xai.ts:14-19` |
| `meta.ts` | Not xAI. It is the Meta provider's OAuth (`isSubscription: true` at `meta.ts:198`). Nothing xAI-related. | `ai/src/auth/oauth/meta.ts:198` |
| Refresh timing | `Models` refreshes when `now + minimumValidity >= expires`, under a lock that re-checks expiry. | `ai/src/auth/resolve.ts:107-153` |

## 4. Changelog

| Date | Version | Entry | Source |
|---|---|---|---|
| 2026-07-16 | 0.80.8 | Added xAI device-code OAuth login. Routed Grok 4.5 through Responses. (#6651) | `ai/CHANGELOG.md:551`; `coding-agent/CHANGELOG.md:1100,1117` |
| 2026-07-16 | 0.80.9 | Device OAuth opens a prefilled link. Provider-specific login labels. Trimmed models. (#6734) | `ai/CHANGELOG.md:523,531`; `coding-agent/CHANGELOG.md:1083,1092` |
| 2026-07-16 | 0.80.9 | Fixed catalog generation restoring removed xAI models. (#6736) | `ai/CHANGELOG.md:512` |
| 2026-08-16 | 0.84.3 | All xAI models use Responses with encrypted reasoning replay. Default Grok 4.6. (#8124) | `ai/CHANGELOG.md:249` |
| 2026-09-22 | 0.87.1 | Added Grok 4.7 with long-context pricing. | `ai/CHANGELOG.md:110` |
| later | n/a | Removed Grok Build 0.1 as unavailable. (#9093) | `ai/CHANGELOG.md:207` |

Git history of `xai.ts` files: `5220aba61` (add), `a01baaaea` (prefilled link), `70e878d4c` (Responses default), plus two coding-agent fixes (`fed6009cc`, `b0bd0ff9d`).
I found no changelog line for an xAI OAuth bug fix after the first week. The auth file has been stable since 2026-07-16.

## 5. Pi tests

| File | What it checks |
|---|---|
| `ai/test/xai-oauth.test.ts` (335 lines) | Device grant fields and exact client id and scope (`:99-113`). First poll delay, `pending`, `slow_down` interval 10 (`:85-157`). `interval: 0` fallback (`:158`). `verification_uri_complete` preferred (`:181`). Non-https URI rejected (`:211`). Denied (`:~255`). Cancel before first poll, one fetch only (`:260`). Refresh rotates and keeps an unrotated refresh token (`:275`). One-hour default (`:303`). Missing field (`:316`). Upstream error text on refresh failure (`:325`). |
| `ai/test/xai-responses.test.ts` (300 lines) | URL `/v1/responses`, Bearer, body fields, encrypted include, models via Responses, User-Agent defaults and override (`:164-300`). |
| `ai/test/oauth-device-code.test.ts` | Poller: immediate poll, wait first, `slow_down` +5 s, server interval, cancel (`:11-131`). |

Port these as Go table tests with a fake `httptest` server and a fake clock.

## 6. Changes between `2bbfcca4` and `4c6fb7cfe`

| File | Changed? |
|---|---|
| `ai/src/auth/oauth/xai.ts`, `device-code.ts`, `load.ts`, `meta.ts` | No (empty `git diff --stat`) |
| `ai/src/providers/xai.ts`, `xai.models.ts` | No |
| `ai/test/xai-oauth.test.ts`, `xai-responses.test.ts` | No |
| `ai/scripts/generate-models.ts` | Yes (37+, 24-). No xAI or Grok lines in the diff. |
| `ai/src/api/openai-responses.ts` | No |
| `ai/src/api/openai-responses-shared.ts` | Yes. Item id replay: drop the id when the model differs or the prefix does not match the item type (`fc_` for function calls, `ctc_` for custom tool calls). Affects xAI replay, since xAI uses Responses. |
| `ai/src/auth/oauth/anthropic.ts`, `openai-chatgpt.ts` | Yes. Not xAI. |

Conclusion: xAI OAuth did not change in the range. Only the shared Responses replay rule changed.

## 7. Fantasy reach (`/Users/dale/Desktop/workspace/opensources/fantasy`, HEAD `0897676`)

Fantasy has no xAI provider. Use `providers/openai` with a custom base URL.

| Need | Fantasy | Gap |
|---|---|---|
| Custom base URL `https://api.x.ai/v1` | `openai.WithBaseURL` (`providers/openai/openai.go:69`) | None |
| Bearer header from token | `openai.WithAPIKey` (`openai.go:76`). The SDK sends `Authorization: Bearer`. | None for static token. Token expires in about 55 min (6 h in tests), and a provider is built with one key. Need a per-request token: rebuild the provider per call or use `WithSDKOptions` with a middleware. Decision for the Ask harness. |
| Responses API | `WithUseResponsesAPI` plus `WithResponsesAPIFunc` (`openai.go:132,140`) | The default filter `IsResponsesModel` only matches `gpt-*` ids (`responses_options.go:382-388`). A `grok-*` id would fall back to Chat Completions. Pass `WithResponsesAPIFunc(func(string) bool { return true })`. |
| Always include `reasoning.encrypted_content` | `ResponsesProviderOptions.Include` accepts `IncludeReasoningEncryptedContent` (`responses_options.go:206,241`; applied at `responses_language_model.go:296-298,317-322`) | Caller must set it on every call for xAI. No automatic default. |
| `store:false` | Default is `false` (`responses_language_model.go:196-200`) | None |
| `reasoning.effort` | Sent only if `modelConfig.isReasoningModel` (`responses_language_model.go:300`). That flag comes from a `gpt-*` regex (`responses_options.go:382,391-394`). | Gap: a `grok-*` model is not a reasoning model for Fantasy, so effort is dropped. Fantasy also keeps `temperature`, which xAI reasoning models may reject (I did not verify this). Workaround: `ExtraBody` (`responses_language_model.go:390-392`) or a fork patch. |
| `xhigh` effort value | `ReasoningEffort` type; I did not find an `xhigh` constant. | Check the type. May need a string cast or a fork patch. |
| Headers: User-Agent, `session_id`, `x-client-request-id` | `WithHeaders`, `WithUserAgent` (`openai.go:104,146`) | Headers are set per provider, not per call. Session id changes per run. Build the provider per session or use a request middleware. |
| `prompt_cache_key` | `ResponsesProviderOptions.PromptCacheKey` (`responses_language_model.go:286-287`) | None |
| No `prompt_cache_retention` | Not sent by Fantasy that I found | None |
| Item id replay rules (`fc_`, `ctc_`) | Not checked | Unverified. Test against xAI replay with encrypted reasoning. |
| Device-code OAuth client | Not in Fantasy. It is out of scope for a model library. | Write it in Ask, `internal/providers` or a new auth package. About 200 lines of Go using `net/http`. |

Net: no blocker. Two fork patches are likely: Responses and reasoning detection for non-`gpt` ids, and per-request auth. I did not run any Go code, so these gaps come from reading the source.

## 8. Does xAI officially offer subscription OAuth to third parties?

Facts only. I found no official xAI statement on third-party use.

| Fact | Source |
|---|---|
| xAI docs page "Grok Build: SpaceXAI's Coding Agent" says: "On first launch, Grok opens a browser for authentication. In non-browser environments, use an API key." | https://docs.x.ai/build/overview |
| The same page contains no text on SuperGrok, X Premium, or subscription OAuth (my fetch summary). | https://docs.x.ai/build/overview |
| Third-party guides say `grok login --device-auth` exists for remote servers and that Grok Build was announced 2026-05-25 as a beta for SuperGrok and X Premium Plus. These are secondary sources. | https://mer.vin/2026/05/grok-build-cli-xai-terminal-coding-agent-with-plan-mode-subagents-and-headless-ci/ |
| Hermes Agent docs: "No `XAI_API_KEY` is required". Endpoint `https://api.x.ai/v1`, auth server `https://accounts.x.ai`. | https://hermes-agent.nousresearch.com/docs/guides/xai-grok-oauth |
| Hermes docs warn: "xAI's backend enforces its own allowlist on the OAuth API surface and has been seen to reject standard SuperGrok subscribers with `HTTP 403`". | https://hermes-agent.nousresearch.com/docs/guides/xai-grok-oauth |
| A GitHub issue (OmniRoute) says the flow is "already implemented and in active use" in Hermes Agent, OpenCode and the official Grok Build CLI. It quotes no xAI policy. | https://github.com/diegosouzapw/OmniRoute/issues/2760 |
| Community Pi extensions already use xAI OAuth (`pi-xai-oauth`, `pi-xai-supergrok`). | https://pi.dev/packages/pi-xai-oauth |
| `https://x.ai/legal/terms-of-service` returned HTTP 403. I could not read the Terms. | https://x.ai/legal/terms-of-service |

Gaps against Pi's choices:

- Pi's scope list includes `grok-cli:access` and the client id is the Grok CLI client. That means Pi presents itself with the official CLI's client id. `referrer=pi` is the only third-party marker (`xai.ts:9,151`).
- This is the same shape as the Anthropic issue in H-AUTH-11 (borrowed first-party client id). Treat it as a policy risk, not a technical one.

## 9. Edge cases a Go port must keep

1. Wait one interval before the first poll, then poll until `expires_in`. Floor the interval at 1 s. Default 5 s when absent or 0.
2. On `slow_down`, use the server `interval` if it is positive, else add 5 s. After a `slow_down` timeout, show the clock-drift message.
3. Treat `access_denied` and `authorization_denied` as denied, and `expired_token` as expired. Poll errors arrive with HTTP 400, so read the JSON body before judging the status.
4. Reject any `verification_uri` or `verification_uri_complete` that is not `https`. Show `verification_uri_complete` when present.
5. Validate each required response field. Do not accept empty strings or non-positive `expires_in`.
6. Keep the old refresh token when the refresh response omits one. Store the new one when it is rotated.
7. Expiry equals `expires_in` minus 5 minutes. Default 3600 s when `expires_in` is missing.
8. Refresh inside the credential-store write lock. Re-check expiry under the lock. In Ask, key credentials by (tenant, provider) in `store` (H-AUTH-02, H-AUTH-03).
9. Resolve the token for every model call (H-AUTH-08). A failed refresh must not log out other instances.
10. Map context cancel to "Login cancelled". Do not print to stderr while the TUI is active (H-AUTH-07).
11. Never log `access`, `refresh`, or `device_code`. Log error code and description only.
12. Request fields for xAI Responses: `store:false`, `include:["reasoning.encrypted_content"]` always, `developer` system role, no `prompt_cache_retention`, never send `none` or `minimal` effort for xAI, `session_id` and `x-client-request-id` headers.
13. A 403 on an OAuth token may mean the xAI allowlist, not a bug. Show a clear message (from Hermes docs, secondary source).

## Ranking and recommendation

| Rank | Option | Fit | Risk |
|---|---|---|---|
| 1 | Add xAI device-code OAuth to H4 as P1, after the policy decision | Small code, same Responses path as the xAI API key, covered by Pi tests | Policy: borrowed Grok CLI client id; xAI 403 allowlist reports |
| 2 | Ship xAI API key (`XAI_API_KEY`) in H4 and defer OAuth to a later phase | No policy risk | User asked for OAuth; this would defer requested scope. Needs user consent. |
| 3 | Register a dedicated Ask client id with xAI | Cleanest legally | No evidence xAI offers client registration |

Choose rank 1. Build the API-key path first inside the same change, because OAuth reuses it.

## Limitations

- `ai/src/providers/data/xai.json` is git-ignored and absent, so I could not list the real model ids or reasoning options.
- I did not run Pi or Go code. Fantasy gaps come from source reading only.
- xAI Terms were not readable (HTTP 403). Web claims about Grok Build come mostly from secondary sources.
- I did not verify the Fantasy `xhigh` effort type, temperature handling for Grok, or the item-id replay rules against the live xAI API.

## Unresolved questions

1. Does the user accept the policy risk of using the Grok CLI client id? (Same open Q1 as H-AUTH-06 and H-AUTH-11.)
2. Does xAI allow third-party tools to use subscription OAuth tokens? I found no official text.
3. Should OAuth tokens refresh per call (rebuild provider or SDK middleware) or per session in Ask?
4. Fork patch or `ExtraBody` workaround for reasoning effort on `grok-*` ids?
5. Which model ids does a subscription token actually list at `GET /v1/models`? Pi uses the static catalog. Community extensions fetch it live.

Status: DONE_WITH_CONCERNS
Summary: Pi xAI OAuth is a device-code flow against `auth.x.ai` with the Grok CLI client id, tokens used as Bearer on `https://api.x.ai/v1/responses`, unchanged in the pin range. Fantasy can reach it with the openai provider but needs Responses and reasoning overrides; xAI's official third-party policy is unconfirmed.
