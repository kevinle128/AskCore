# H4 subscription source audit

Date: 2026-10-05.
Status: source audit complete; implementation not started.
Pi reference: local checkout `/Users/dale/Desktop/workspace/opensources/pi`, commit `4c6fb7cfe`, version 1.0.1.
Scope: Anthropic OAuth, current OpenAI ChatGPT sign-in, xAI OAuth, and their inference paths.

## Evidence and method

The four [earlier reports](./researcher-261005-2115-h4-oauth-shared-infra.md) remain research inputs.
This report corrects and extends them; it does not replace source code with inferred requirements.
GitNexus reports an up-to-date Pi index at the reference commit.
Queries and context inspection covered login, refresh, credential resolution, and Anthropic name conversion.
The exact GitNexus trace is `ModelsImpl.getAuth -> resolveProviderAuth -> resolveProviderAuthWithSignal -> resolveStoredOAuth`.
Direct source reads establish the conditions and execution order below.
All Pi source paths below are relative to the pinned checkout.
The recorded line numbers apply only to that revision.
No live subscription inference or OAuth login was run in this audit.

## 1. Anthropic changes more than the Authorization header

| Behavior | Source | Consequence for Ask |
|---|---|---|
| OAuth detection uses `apiKey.includes("sk-ant-oat")` | `packages/ai/src/api/anthropic-messages.ts:978` | Pi loses method identity at the auth boundary and reconstructs it from token text. Use explicit method metadata. |
| A supplied SDK client sets `isOAuth=false` | Same file, `:603-609` | An injected client can bypass the entire OAuth request profile. Do not let client injection choose protocol behavior. |
| OAuth sets `apiKey:null`, `authToken`, `x-app:cli`, and `claude-cli/2.1.280` | Same file, `:1014-1033` | Key authentication and OAuth need different SDK credential options and identity headers. |
| OAuth adds `claude-code-20250219` and `oauth-2025-04-20` betas | Same file, `:1080-1120` | Betas depend on auth method as well as model compatibility. |
| A case-insensitive explicit beta header replaces computed betas; null removes them | Same file, `:1080-1105` | Header merge semantics must be deliberate. Pi allows callers to disable required-looking defaults. |
| OAuth prepends the Claude Code identity system block, then the original system prompt | Same file, `:1167-1182` | Apply the prefix only to the wire request. Cache controls apply to both blocks when enabled. |
| SDK default credential resolution is suppressed by `PiAnthropic` | Same file, class `PiAnthropic` | Do not let an SDK silently load another credential when Ask has selected one. |

The dangerous-browser header also exists on the non-OAuth branch.
It is not evidence of a subscription-only requirement.
Model headers and request headers can override Pi defaults.
Transport compatibility with this path does not establish permission to use a third-party subscription integration.
The policy evidence is recorded separately in the [Anthropic research](./researcher-261005-2115-h4-oauth-anthropic.md).

### Complete tool-name path

`toClaudeCodeName` performs case normalization against a finite list of Claude Code names.
Examples are `read -> Read`, `bash -> Bash`, and `websearch -> WebSearch`.
Unmatched names stay unchanged.
It does not rename `find` to `Glob` in this revision.
Source: `packages/ai/src/api/anthropic-messages.ts:95-132`.

| Location | Forward or reverse | Source |
|---|---|---|
| Initial and current tool declarations | Forward | `convertTools`, `:1603` |
| Historical assistant `tool_use` blocks | Forward | `convertMessages`, `:1443` |
| Native `tool_addition` declarations | Forward through `convertTools` | `:1351` |
| Native `tool_removal.tool_reference.name` | Forward | `:1347` |
| Response `content_block_start.tool_use.name` | Reverse before `toolcall_start` | `:727-729` |
| Tool argument deltas and end events | Already use the mapped tool call | Stream event handling after `:727` |
| Forced `{type:"tool", name:...}` choice | No forward conversion | `:1275-1277` |
| Tool-result messages | No name field to convert | `convertToolResult` |

The reverse function compares against the request's current tool snapshot and restores the declared name.
Source: `getCurrentTools` at `:578`, then `fromClaudeCodeName` at `:125`.
The forced-choice path is a source-confirmed mismatch: `name:"read"` can coexist with a declaration named `Read`.
Server rejection was not tested.
Ask must use the same conversion for forced choice and all other name-bearing fields.
Name conversion must not alter stored transcripts or the shared tool registry.

### Related wire behavior that is not OAuth-only

Native tool changes require model support for both mid-conversation system messages and tool changes, plus an initial tool scaffold.
The scaffold includes a deferred `__pi_deferred_placeholder__` to preserve cache structure.
Tool additions, removals, historical effort changes, strict tool schemas, and eager input streaming also have model compatibility conditions.
Thinking signatures and redacted thinking are preserved when replay is compatible and removed when it is not.
These rules belong to the Anthropic wire adapter and model compatibility record, not to the login flow.
Source: `anthropic-messages.ts:1138-1234`, `convertMessages`, `convertTools`, and `transformMessages`.

## 2. OpenAI ChatGPT uses the public Responses wire API

Pi's current `openai-chatgpt.ts` flow is separate from its legacy `openai-codex` flow.
Do not substitute the legacy `chatgpt.com/backend-api` transport for the current public API.

| Behavior or gap | Source | Consequence |
|---|---|---|
| Subscription shaping requires provider `openai`, the default API base URL, and a non-`sk-` credential | `packages/ai/src/api/openai-responses.ts:40-47` | Prefix and URL heuristics are not a maintainable auth contract. |
| Omits max output tokens, temperature, cache retention, and cache options | Same file, `:329-346` | Public Responses still needs an auth-specific request profile. |
| Forces streaming and disables server storage; retains a clamped cache key | Same file, request builder | Local sessions own history. |
| Session headers are added only when caching is enabled | Same file, client creation | Session correlation and cache selection are linked in Pi. |
| Model and request sampling maps are applied after subscription omissions | Same file, `:382` | They can reintroduce unsupported fields. Validate the final body after overrides. |
| `subscription_sharing_usage_limit_exceeded` gets a usage-page hint | Same file, `:227`; `utils/retry.ts` | Classify exhausted allowance separately from transient 429 errors, including stream failures. |
| Every login starts dynamic registration; refresh uses the issued client ID | `packages/ai/src/auth/oauth/openai-chatgpt.ts`, login and refresh | Returning-account login is not fully implemented in this Pi revision. |
| Nonce is sent; the ID token is checked only for presence | Same file, `:202`, `:240-262` | Do not copy this as sufficient OIDC validation. |
| Callback port 1455 is fixed; bind failure is fatal | Same file, callback server creation | Earlier shared-report fallback advice is a proposed change, not Pi parity. |

Current OpenAI documentation requires ID-token signature and claim validation, returning-client reuse, and protected account records.
These are additions beyond pinned Pi behavior.
Source: [registration and sign-in](https://developers.openai.com/siwc/token-sharing-open-source/sign-in).
The current documented preview restrictions also extend beyond Pi's four omissions.
The implementation must keep a tested request profile aligned with that contract.
Source: [preview limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).

## 3. xAI shares Responses but has distinct auth and reasoning behavior

The device flow uses the Grok CLI client ID and includes `grok-cli:access` and `api:access` scopes.
It validates response fields and HTTPS verification URLs.
Polling waits before the first token request and handles pending, slow-down, denial, expiry, and cancellation.
If refresh omits a replacement refresh token, Pi retains the old token.
Source: `packages/ai/src/auth/oauth/xai.ts`.

Inference uses `https://api.x.ai/v1/responses` for both API keys and OAuth.
The extra `reasoning.encrypted_content` include is set for xAI only inside the reasoning-model branch.
It is not unconditional for every possible xAI model.
Reasoning levels and supported `off` values are model data.
There is no Anthropic-style tool-name rewrite in this path.
Source: `packages/ai/src/api/openai-responses.ts:364-378` and xAI model-generation records.

## 4. Shared credential lifecycle details

| Detail | Source | Finding |
|---|---|---|
| Resolution order | `packages/ai/src/auth/resolve.ts` | Runtime override, stored credential, then environment. A stored OAuth failure does not silently switch to an environment key. |
| Refresh safety | Same file, `:114-158` | Check before the lock, recheck under the lock, refresh with a 15-second limit, then persist. |
| Effective refresh lead time | OAuth modules plus resolver | Normally 10 minutes for Anthropic/xAI and 8 minutes for ChatGPT: stored expiry subtracts 5/3 minutes, then the resolver adds 5 minutes. |
| Separate catalog-refresh path | `packages/ai/src/models.ts`, `resolveRefreshCredential` | This path checks stored expiry directly and does not declare the same local 15-second refresh timeout. Do not claim one identical refresh path. |
| Cross-process modification | `packages/coding-agent/src/core/auth-storage.ts:449-473` | Read current data inside the lock and merge one provider. Logout uses the same store lock. |
| Cancellation after refresh | Same file, storage `withLockAsync` | A signal check after the refresh callback can precede persistence. A rotated-token loss window is plausible from ordering; no failure was reproduced. |
| Callback completion | Anthropic and ChatGPT OAuth modules | Browser success can precede token exchange and credential persistence. Anthropic passes a callback that only returns the code. |
| Login completion | `packages/ai/src/models.ts`, `ModelsImpl.login` | The login method returns only after store modification. Runtime synchronization follows in coding-agent model runtime. |
| Logout | Models and auth storage | Deletes local credentials; it is not server-side revocation. |
| Credential shape | Provider definitions and auth storage | One credential per provider, with OAuth extra fields retained. No required SQL tenant store is implied. |
| Cost | `packages/coding-agent/src/modes/interactive/components/footer.ts:191-195` | Pi retains estimated catalog cost and adds `(sub)`. It does not globally zero subscription cost. |

No automatic auth-bound refresh-and-retry on every 401 was found in these inspected paths.
Such behavior would be an Ask decision, not a copied Pi guarantee.
OpenAI's server-provided `earliest_refresh_at` needs explicit handling in Ask.
Source: [token reference](https://developers.openai.com/siwc/token-sharing-open-source/token-reference).

## 5. Validation and implementation implications

Anthropic tool-name tests call the actual credential resolver and are gated by available live credentials.
They are not complete offline regression coverage.
Source: `packages/ai/test/anthropic-tool-name-normalization.test.ts`.
Pi also has offline tests for header-owned Anthropic bearer tokens, beta overrides, ChatGPT payload omissions, and xAI Responses shaping.
Those tests establish that bearer authentication alone does not imply Claude Code behavior.

Ask needs offline full-path checks for outgoing JSON, final headers, incoming names, refresh rotation, and persisted state.
Live account probes then establish endpoint acceptance and allowance behavior.
The design is recorded in [the provider/auth plan](../261005-2139-provider-auth-design/plan.md).

## Unresolved questions

- Actual account eligibility and server acceptance require live probes with authorized accounts.
- The suspected cancellation/rotation window is source evidence, not a reproduced Pi defect.
