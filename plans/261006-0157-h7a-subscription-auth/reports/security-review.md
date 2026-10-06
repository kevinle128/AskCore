# H7a security and factual review

Date: 2026-10-06.
Lens: Security Adversary; Full tier Fact Checker.
Scope: plan review only, with `--deep --tdd` and without `--yagni`.
Verdict: Request changes for two Medium findings.
No Critical or High finding has sufficient source evidence.

The plan proposes native login, durable credentials, refresh, request binding and subscription inference for three providers.
The review retains the accepted one-record policy, local-only logout, verified ChatGPT identity, returning-client reuse and explicit new-account replacement.
H4 remains a partial Responses wire dependency.
PASSED proof rows mean design coverage only; they do not mean executed tests or provider acceptance.
No Go command, build, lint, live OAuth operation, inference or credential read was performed.
Only this report was changed.

## Material findings

### Medium: The xAI denial proof row uses the wrong native error name

Plan location: `plans/261006-0157-h7a-subscription-auth/plan.md:255`, with the corresponding unnamed denial requirement in `phase-05-xai-device-authorization.md:33` and denial scenario at line 83.
The prepared response names are `access_denied` and `authorization_declined`.
The pinned source accepts `access_denied` and `authorization_denied`.
Failure scenario: An implementation follows the proof row and passes both planned denial checks, but an actual `authorization_denied` response enters the generic error path instead of the required native denial path.
This loses the promised coverage and can expose the raw server error description through generic error reporting.
Evidence: `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/src/auth/oauth/xai.ts:190` selects the two native denial names, while line 196 delegates other responses to `requestFailure` and line 105 includes its server description.
Suggested fix: Use `authorization_denied` in the main row and name both native values in the phase 5 failing test scenario.
If another name has separate provider evidence, test it as an additional value rather than replacing the pinned native value.

### Medium: Callback and private-input limits have no explicit acceptance case

Plan location: `phase-03-anthropic-and-headless-login.md:38`, line 41 and the test matrix at lines 104–113; `phase-04-chatgpt-identity-and-responses.md:112`.
The phase bounds total login and HTTP exchange responses, but does not define or test a size limit for incoming callback query data or pasted private input before parsing.
The parent security contract specifically requires bounded callback queries.
Failure scenario: A local caller or browser sends a very large callback request, or a redirected stdin stream supplies very large manual callback data, while an otherwise valid login remains pending.
An implementation can satisfy the listed wrong-state, URI, deadline and cleanup tests while allocating or parsing input up to its unrelated server or reader limit.
Evidence: `plans/reports/xia-261006-0143-h7a-subscription-auth-architecture.md:517` requires bounds on callback queries, and the pinned reference `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/src/auth/oauth/callback-server.ts:80` constructs a URL directly from request input.
The reference manual ChatGPT path also constructs a URL from the complete pasted string at `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/src/auth/oauth/openai-chatgpt.ts:67`.
This is a missing plan acceptance case, not a claim that the future Go handler already has an input vulnerability.
Suggested fix: Give the common callback/private-input owner a documented maximum size before parsing and add oversized loopback and stdin cases through the built command.
The checks must reject oversized input without token exchange or replacement and must permit the valid pending attempt to continue where safe.
Use the existing standard HTTP server and bounded reader facilities; no new parsing framework is needed.

## Factual sample: four claims from each phase

VERIFIED means the cited current source or authoritative package contract supports the claim.
FAILED means current source contradicts the claim.
UNVERIFIED means the claim describes planned implementation and cannot yet be established from current source.
Pi paths below use the source root `/Users/dale/Desktop/workspace/opensources/pi` at the revision recorded in the scout.
Ask paths use `/Users/dale/orca/workspaces/AskCore/master-2`.

| Phase | Claim sampled | Result | Source evidence |
| --- | --- | --- | --- |
| 1 | Settings permits standard-library imports and denies other internal packages. | VERIFIED. | `internal/settings/README.md:30` and line 31 state the package contract. |
| 1 | StreamFn and Provider.Stream take the existing model, transcript and StreamOptions and return a Stream. | VERIFIED. | `internal/providers/types.go:48` and line 55 define those signatures. |
| 1 | The existing runtime options contain APIKey without a typed auth snapshot. | VERIFIED. | `internal/providers/types.go:33` through line 44 define the current options. |
| 1 | The proposed secure settings credential transaction will enforce the new durable write rules. | UNVERIFIED; planned implementation. | `phase-01-start.md:61` explicitly proposes new files; `internal/settings/doc.go:1` describes ownership only and provides no transaction implementation. |
| 2 | ResolveKey has the two stated production callers in SetModel and loop.apiKey. | VERIFIED. | `internal/agent/agent.go:142` and `internal/agent/loop_stream.go:98` call it. |
| 2 | PrepareRequest runs before the stream stage. | VERIFIED. | `internal/agent/loop_stage.go:114` invokes prepareRequest and line 123 invokes streamAssistantResponse. |
| 2 | SetModel performs a second idle check after key resolution. | VERIFIED. | `internal/agent/agent.go:142` resolves the key and line 152 checks active state again. |
| 2 | The loop reads Stream.Result after Events closes and emits the final public MessageEnd. | VERIFIED. | `internal/agent/loop_stream.go:39` drains events, line 63 reads Result and line 74 emits MessageEnd. |
| 3 | Auth dispatch must be inserted before current parsing, capture and prompt reads. | VERIFIED. | `cmd/tui/headless.go:48` parses, line 72 starts capture and line 81 reads prompts. |
| 3 | The existing Anthropic adapter constants describe Token Plan. | VERIFIED. | `internal/providers/anthropic/provider.go:22` selects ProviderTokenPlan and line 26 selects TokenPlanMessagesURL. |
| 3 | The existing options do not contain forced named tool choice. | VERIFIED. | `internal/providers/types.go:33` through line 44 contain the complete current StreamOptions fields. |
| 3 | Fold publishes ToolCallName directly, while its Tool callback is for IDs. | VERIFIED. | `internal/providers/fantasykit/fold.go:153` calls ToolStart with part.ToolCallName; `internal/providers/fantasykit/fold.go:242` defines toolID. |
| 4 | The reference ChatGPT callback URI is loopback port 1455 with the auth callback path. | VERIFIED. | `Pi/packages/ai/src/auth/oauth/openai-chatgpt.ts:25` defines REDIRECT_URI from port 1455 and the auth callback path; line 195 sends the same URI in exchange. |
| 4 | Model headers can currently replace SDK Authorization. | VERIFIED. | `internal/providers/openai/isolate.go:24` prepares extra headers and line 61 overwrites the cloned request headers. |
| 4 | Responses uses store=false and can send max output tokens and long cache retention. | VERIFIED. | `internal/providers/openai/responses_prompt.go:26` disables storage, line 41 adds long retention and line 54 assigns MaxOutputTokens. |
| 4 | Current Responses tools are flat function tools and require a separate ChatGPT grouping check. | VERIFIED. | `internal/providers/openai/responses_prompt.go:86` appends fantasy.FunctionTool values. |
| 5 | The native xAI device and token endpoints are the named auth.x.ai URLs. | VERIFIED. | `Pi/packages/ai/src/auth/oauth/xai.ts:10` and line 11 define them. |
| 5 | The device flow waits before its first poll. | VERIFIED. | `Pi/packages/ai/src/auth/oauth/xai.ts:165` enables waitBeforeFirstPoll and `Pi/packages/ai/src/auth/oauth/device-code.ts:57` performs that wait. |
| 5 | xAI can retain its prior refresh token when the response omits a replacement. | VERIFIED. | `Pi/packages/ai/src/auth/oauth/xai.ts:132` uses the previous token only for an omitted refresh_token. |
| 5 | The main proof row's native denial pair is access_denied plus authorization_declined. | FAILED. | `plan.md:255` gives that pair, but `Pi/packages/ai/src/auth/oauth/xai.ts:190` gives access_denied plus authorization_denied. |
| 6 | Print and JSON modes have existing SIGINT, SIGTERM and SIGHUP exit conventions. | VERIFIED. | `cmd/tui/headless.go:37` defines exit codes 130, 143 and 129. |
| 6 | Current app graph validation does not execute constructors or start servers. | VERIFIED. | `internal/app/app_test.go:10` states that boundary and line 13 calls fx.ValidateApp. |
| 6 | Capture drops credential headers but retains request bodies, so auth exchange exclusion is necessary. | VERIFIED. | `internal/providers/cassette/cassette.go:44` permits only three request headers and line 246 preserves the complete request body. |
| 6 | Existing opt-in provider live checks use API keys rather than proving ChatGPT subscription access. | VERIFIED. | `internal/providers/openai/responses_test.go:124` reads OPENAI_API_KEY and line 134 supplies APIKey; `cmd/tui/headless_live_test.go:63` describes its Token Plan gate. |

Sample result: 22 VERIFIED, 1 FAILED and 1 UNVERIFIED planned implementation.
The UNVERIFIED row is not a missing existing dependency and is not an implementation-completion claim in the plan.
The new auth, profile and app constructor files are named as new work or as files created by earlier phases.
Their absence today is expected.

## Controls that hold at the design level

The store plan names fresh read/merge, a stable sidecar OS lock, absence-aware revision checks, owner-only modes and separate pre-rename versus post-rename failures.
The resolver plan forbids billed fallback and requires bounded commit after validated token rotation even when the caller cancels.
Final request validation covers origins, paths, redirects and protected credential headers after SDK and model options.
OIDC requirements retain a maintained verifier, algorithm and issuer restrictions, bounded key refresh and no token-provided key URL.
The main capture row and phase 6 retain canary scans and exclusion of OAuth exchanges rather than relying only on header redaction.
These are sound requirements, not executed security proof.

## Rejected concerns

Multiple saved accounts, retained client registrations and remote revocation would reverse accepted product decisions, so they are not findings.
The working-tree SDK and final header bypass are identified owners in the plan, so their current existence is not a missed design requirement.
No evidence establishes that the future OIDC verifier follows token-provided URLs or that the future store follows symlinks; those remain explicit forbidden behavior rather than proven defects.
No current code supports a claim of secret access through the new immutable snapshot, which is planned work.

## Unresolved questions

None for product scope.
Provider acceptance, eligible live accounts and the selected verifier remain planned implementation validation, as the plan already states.

Status: DONE_WITH_CONCERNS.
Summary: Reviewed all six phases and their authorities, with 24 source samples and two Medium corrections.
Concerns: Correct the xAI denial name and add explicit bounded callback/private-input acceptance.
